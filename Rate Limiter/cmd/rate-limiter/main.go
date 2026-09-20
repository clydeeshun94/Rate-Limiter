package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	limiter "rate-limiter/internal/limiter"
	rate "rate-limiter/internal/storage"
	"rate-limiter/internal/metrics"
	"rate-limiter/internal/config"
	"rate-limiter/internal/adjuster"
	"rate-limiter/internal/analytics"
	"rate-limiter/pkg/logging"
	"rate-limiter/middleware"
)

type service struct {
	limiters  map[string]limiter.RateLimiter
	limits    map[string]int
	collector *metrics.Collector
	adjuster  *adjuster.Adjuster
	logger    logging.Logger
	config    config.ServerConfig
	monitorLimiter limiter.RateLimiter
}

type checkRequest struct {
	Identity   string         `json:"identity"`
	Algorithm  string         `json:"algorithm"`
	Policy     limiter.Policy `json:"policy"`
}

type checkResponse struct {
	Allowed    bool          `json:"allowed"`
	Limit      int           `json:"limit"`
	Remaining  int           `json:"remaining"`
	RetryAfter time.Duration `json:"retry_after"`
	ResetTime  string        `json:"reset_time"`
}

type limitRequest struct {
	Algorithm string `json:"algorithm"`
	Limit     int    `json:"limit"`
}

type limitsRequest struct {
	Limits []limitRequest `json:"limits"`
}

type limitsResponse struct {
	Limits []limitInfo `json:"limits"`
}

type limitInfo struct {
	Algorithm string `json:"algorithm"`
	Limit     int    `json:"limit"`
}

type configResponse struct {
	Port         int      `json:"port"`
	DefaultLimit int      `json:"default_limit"`
	Algorithms   []string `json:"algorithms"`
}

type configUpdateRequest struct {
	Port         int    `json:"port"`
	DefaultLimit int    `json:"default_limit"`
	Algorithms   []string `json:"algorithms"`
}

func newService() *service {
	collector := metrics.NewCollector()
	cfg := config.LoadConfig()
	defaultLimit := cfg.DefaultLimit
	algorithms := cfg.Algorithms

	l := &service{
		limiters:  make(map[string]limiter.RateLimiter),
		limits:    make(map[string]int),
		collector: collector,
		logger:    config.NewLogger(cfg.LogLevel),
		config:    cfg,
	}

	storage := rate.NewMemoryStorage()
	for _, algo := range algorithms {
		var rl limiter.RateLimiter
		switch algo {
		case "fixed_window":
			rl = limiter.NewFixedWindowWithLimit(storage, defaultLimit)
		case "sliding_window_counter":
			rl = limiter.NewSlidingWindowCounterWithLimit(storage, defaultLimit)
		case "sliding_window_log":
			rl = limiter.NewSlidingWindowLogWithLimit(storage, defaultLimit)
		case "token_bucket":
			rl = limiter.NewTokenBucketWithLimit(storage, defaultLimit)
		case "leaky_bucket":
			rl = limiter.NewLeakyBucketWithLimit(storage, defaultLimit)
		}
		l.limiters[algo] = metrics.Wrap(rl, collector, algo)
		l.limits[algo] = defaultLimit
	}

	l.monitorLimiter = limiter.NewFixedWindowWithLimit(rate.NewMemoryStorage(), 100)

	limiters := make([]limiter.LimitSetter, 0, len(l.limiters))
	for _, rl := range l.limiters {
		if setter, ok := rl.(limiter.LimitSetter); ok {
			limiters = append(limiters, setter)
		}
	}

	// Adjuster: automatically adjusts rate limits based on traffic patterns.
	// Reads ErrorRate and ResponseTime from CollectorProvider every 30s.
	// Starts at conservative strategy (10% max change), 5min between adjustments.
	// Max limit = defaultLimit * 2 (allows doubling under good conditions).
	// If initialization fails, rate limiter runs without auto-adjustment.
	provider := analytics.NewCollectorProvider(collector)
	logger := config.NewLogger(cfg.LogLevel)
	adj, err := adjuster.NewAdjuster(limiters, provider, adjuster.AdjusterConfig{
		MetricsToWatch: []string{"ErrorRate", "ResponseTime"},
		Interval:       30 * time.Second,
		StableDuration: 5 * time.Minute,
		MinLimit:       1,
		MaxLimit:       defaultLimit * 2,
		Logger:         logger,
	})
	if err == nil {
		adj.Start()
		l.adjuster = adj
	} else {
		logger.Warn("adjuster disabled", map[string]interface{}{"error": err.Error()})
	}

	return l
}

func (s *service) handleCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req checkRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	l, ok := s.limiters[req.Algorithm]
	if !ok {
		http.Error(w, fmt.Sprintf("unknown algorithm: %s", req.Algorithm), http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	type checkResult struct {
		result limiter.Result
		err    error
	}
	ch := make(chan checkResult, 1)
	go func() {
		res, err := l.Check(req.Identity, req.Policy)
		ch <- checkResult{result: res, err: err}
	}()

	var res limiter.Result
	var err error
	select {
	case cr := <-ch:
		res = cr.result
		err = cr.err
	case <-ctx.Done():
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusGatewayTimeout)
		json.NewEncoder(w).Encode(checkResponse{Allowed: true})
		return
	}

	if err != nil {
		http.Error(w, fmt.Sprintf("check failed: %v", err), http.StatusInternalServerError)
		return
	}

	resp := checkResponse{
		Allowed:    res.Allowed,
		Limit:      res.Limit,
		Remaining:  res.Remaining,
		RetryAfter: res.RetryAfter,
		ResetTime:  res.ResetTime.Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-RateLimit-Limit", strconv.Itoa(res.Limit))
	w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(res.Remaining))
	w.Header().Set("X-RateLimit-Reset", res.ResetTime.Format(time.RFC3339))
	if res.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.FormatInt(int64(res.RetryAfter.Seconds()), 10))
	}

	json.NewEncoder(w).Encode(resp)
}

func (s *service) handleLimit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req limitRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	l, ok := s.limiters[req.Algorithm]
	if !ok {
		http.Error(w, fmt.Sprintf("unknown algorithm: %s", req.Algorithm), http.StatusBadRequest)
		return
	}

	if setter, ok := l.(limiter.LimitSetter); ok {
		setter.SetLimit(req.Limit)
		s.limits[req.Algorithm] = req.Limit
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "limit updated"})
}

func (s *service) handleGetLimits(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var infos []limitInfo
	for algo := range s.limiters {
		infos = append(infos, limitInfo{
			Algorithm: algo,
			Limit:     s.limits[algo],
		})
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(limitsResponse{Limits: infos})
}

func (s *service) handleSetLimits(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req limitsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	for _, lr := range req.Limits {
		l, ok := s.limiters[lr.Algorithm]
		if !ok {
			http.Error(w, fmt.Sprintf("unknown algorithm: %s", lr.Algorithm), http.StatusBadRequest)
			return
		}
		if setter, ok := l.(limiter.LimitSetter); ok {
			setter.SetLimit(lr.Limit)
			s.limits[lr.Algorithm] = lr.Limit
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "limits updated"})
}

func (s *service) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(configResponse{
		Port:         s.config.Port,
		DefaultLimit: s.config.DefaultLimit,
		Algorithms:   s.config.Algorithms,
	})
}

func (s *service) handleSetConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req configUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("invalid JSON: %v", err), http.StatusBadRequest)
		return
	}

	if req.Port > 0 {
		s.config.Port = req.Port
	}
	if req.DefaultLimit > 0 {
		s.config.DefaultLimit = req.DefaultLimit
	}
	if len(req.Algorithms) > 0 {
		s.config.Algorithms = req.Algorithms
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "config updated"})
}

func (s *service) handleLimits(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetLimits(w, r)
	case http.MethodPost:
		s.handleSetLimits(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *service) handleConfig(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.handleGetConfig(w, r)
	case http.MethodPost:
		s.handleSetConfig(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *service) handleMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain")
	fmt.Fprint(w, s.collector.ToPrometheusText())
}

func (s *service) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// handleAnalytics returns a JSON snapshot of rate limiter health.
// Includes collector metrics (allowed/denied counts, total checks, avg duration)
// and adjuster status (whether auto-adjustment is active).
// Designed for monitoring dashboards and external observability tools.
func (s *service) handleAnalytics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	snap := s.collector.Snapshot()
	result := map[string]interface{}{
		"collector": snap,
		"adjuster":  map[string]interface{}{"running": s.adjuster != nil},
	}
	if s.adjuster != nil {
		result["adjuster"] = map[string]interface{}{
			"running": true,
		}
	}
	json.NewEncoder(w).Encode(result)
}

func (s *service) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if s.config.AuthToken == "" {
            next(w, r)
            return
        }
        token := r.Header.Get("Authorization")
        if token != s.config.AuthToken {
            http.Error(w, "unauthorized", http.StatusUnauthorized)
            return
        }
        next(w, r)
    }
}

func main() {
	svc := newService()

	corsOrigin := os.Getenv("CORS_ORIGIN")
	if corsOrigin == "" {
		corsOrigin = "*"
	}
	corsMethods := os.Getenv("CORS_METHODS")
	if corsMethods == "" {
		corsMethods = "GET, POST, OPTIONS"
	}
	corsHeaders := os.Getenv("CORS_HEADERS")
	if corsHeaders == "" {
		corsHeaders = "Content-Type, Authorization, X-RateLimit-*"
	}

	router := http.NewServeMux()

	router.HandleFunc("/check", svc.handleCheck)
	router.HandleFunc("/limit", svc.authMiddleware(svc.handleLimit))
	router.HandleFunc("/limits", svc.authMiddleware(svc.handleLimits))
	router.HandleFunc("/config", svc.authMiddleware(svc.handleConfig))
	router.HandleFunc("/analytics", svc.handleAnalytics)

	healthLimiter := limiter.NewFixedWindowWithLimit(rate.NewMemoryStorage(), 1000)
	router.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		allowed, _ := healthLimiter.Check("health-check", limiter.Policy{Limit: 1000, Window: 60 * time.Second})
		if !allowed.Allowed {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		svc.handleHealth(w, r)
	})

	metricsLimiter := limiter.NewFixedWindowWithLimit(rate.NewMemoryStorage(), 100)
	router.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		allowed, _ := metricsLimiter.Check("metrics-scraper", limiter.Policy{Limit: 100, Window: 60 * time.Second})
		if !allowed.Allowed {
			w.Header().Set("Retry-After", "1")
			http.Error(w, "too many requests", http.StatusTooManyRequests)
			return
		}
		svc.handleMetrics(w, r)
	})

	enablePprof := os.Getenv("ENABLE_PPROF") == "true"
	if enablePprof {
		router.HandleFunc("/debug/pprof/", pprof.Index)
		router.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		router.HandleFunc("/debug/pprof/profile", pprof.Profile)
		router.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		router.HandleFunc("/debug/pprof/trace", pprof.Trace)
	}

	corsHandler := middleware.CORS(corsOrigin, corsMethods, corsHeaders)(router)

	port := svc.config.Port
	addr := ":" + strconv.Itoa(port)
	log.Printf("rate-limiter service starting on %s", addr)

	srv := &http.Server{
		Addr:              addr,
		Handler:           corsHandler,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		if tlsCert := os.Getenv("TLS_CERT"); tlsCert != "" {
			if tlsKey := os.Getenv("TLS_KEY"); tlsKey != "" {
				log.Fatal(srv.ListenAndServeTLS(tlsCert, tlsKey))
			}
		}
		log.Fatal(srv.ListenAndServe())
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	<-stop

	log.Println("shutting down gracefully...")
	ctxShutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	srv.Shutdown(ctxShutdown)
	log.Println("shutdown complete")
}
