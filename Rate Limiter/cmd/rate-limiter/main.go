package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"net/http/pprof"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"rate-limiter/internal/policy"
	"time"

	"github.com/joho/godotenv"
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
	mu        sync.RWMutex
	limiters  map[string]limiter.RateLimiter
	limits    map[string]int
	collector *metrics.Collector
	adjuster  *adjuster.Adjuster
	logger    logging.Logger
	config    config.ServerConfig
	monitorLimiter limiter.RateLimiter
	enabled   int32
	wsHub     *wsHub
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
		enabled:   1,
		wsHub:     newWSHub(collector),
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

	s.mu.RLock()
	l, ok := s.limiters[req.Algorithm]
	configuredLimit := s.limits[req.Algorithm]
	s.mu.RUnlock()
	if !ok {
		http.Error(w, fmt.Sprintf("unknown algorithm: %s", req.Algorithm), http.StatusBadRequest)
	return
	}

	req.Policy.Limit = configuredLimit
	if atomic.LoadInt32(&s.enabled) == 0 {
		s.collector.RecordAllowed(req.Identity)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(checkResponse{
			Allowed:   true,
				Limit:     configuredLimit,
			Remaining: configuredLimit,
		})
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
		http.Error(w, "rate limiter check timed out", http.StatusGatewayTimeout)
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
	if err := policy.Validate(req.Limit, time.Second); err != nil {
		http.Error(w, fmt.Sprintf("invalid limit: %v", err), http.StatusBadRequest)
	return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
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
	s.mu.RLock()
	for algo := range s.limiters {
	infos = append(infos, limitInfo{
	Algorithm: algo,
	Limit:     s.limits[algo],
	})
	}
	s.mu.RUnlock()

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

	s.mu.Lock()
	defer s.mu.Unlock()
	for _, lr := range req.Limits {
	if err := policy.Validate(lr.Limit, time.Second); err != nil {
			http.Error(w, fmt.Sprintf("invalid limit for %s: %v", lr.Algorithm, err), http.StatusBadRequest)
			return
		}
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

	s.mu.RLock()
	response := configResponse{
	Port:         s.config.Port,
	DefaultLimit: s.config.DefaultLimit,
	Algorithms:   append([]string(nil), s.config.Algorithms...),
	}
	s.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
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

	s.mu.Lock()
	defer s.mu.Unlock()
	if req.Port > 0 && req.Port != s.config.Port {
		http.Error(w, "port cannot be changed at runtime; restart the service", http.StatusBadRequest)
	return
	}
	if req.DefaultLimit > 0 {
	if err := policy.Validate(req.DefaultLimit, time.Second); err != nil {
		http.Error(w, fmt.Sprintf("invalid default_limit: %v", err), http.StatusBadRequest)
	return
	}
		s.config.DefaultLimit = req.DefaultLimit
	for algorithm, current := range s.limiters {
	if setter, ok := current.(limiter.LimitSetter); ok {
	setter.SetLimit(req.DefaultLimit)
		s.limits[algorithm] = req.DefaultLimit
	}
	}
	}
	if len(req.Algorithms) > 0 {
	if !sameAlgorithms(req.Algorithms, s.config.Algorithms) {
		http.Error(w, "algorithms cannot be changed at runtime", http.StatusBadRequest)
	return
	}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "config updated"})
}

func sameAlgorithms(left, right []string) bool {
	if len(left) != len(right) {
	return false
	}
	seen := make(map[string]int, len(left))
	for _, algorithm := range left {
	seen[algorithm]++
	}
	for _, algorithm := range right {
	seen[algorithm]--
	if seen[algorithm] < 0 {
	return false
	}
	}
	return true
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

func (s *service) handleAdminDisable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	atomic.StoreInt32(&s.enabled, 0)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "disabled"})
}

func (s *service) handleAdminEnable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	atomic.StoreInt32(&s.enabled, 1)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "enabled"})
}

func (s *service) handleCrash(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	initialConcurrent := 50
	if c := r.URL.Query().Get("concurrent"); c != "" {
		if n, err := strconv.Atoi(c); err == nil && n > 0 {
			initialConcurrent = n
		}
	}

	burstDuration := 5 * time.Second
	if d := r.URL.Query().Get("duration"); d != "" {
		if parsed, err := time.ParseDuration(d); err == nil && parsed > 0 {
			burstDuration = parsed
		}
	}

	target := r.URL.Query().Get("url")
	if target == "" {
		target = "http://localhost:5175/api/shorten"
	}

	maxConcurrent := 800
	pauseBetweenRounds := 3 * time.Second
	checkTimeout := 3 * time.Second

	type roundResult struct {
		round          int `json:"round"`
		concurrent     int `json:"concurrent"`
		totalRequests  int64 `json:"total_requests"`
		errors         int64 `json:"errors"`
		targetAlive    bool `json:"target_alive"`
	}

	var results []roundResult
	var totalAll int64
	var errorsAll int64

	concurrent := initialConcurrent
	for round := 1; concurrent <= maxConcurrent; round++ {
		var total int64
		var errs int64

		ctx, cancel := context.WithTimeout(r.Context(), burstDuration)
		var wg sync.WaitGroup
		for i := 0; i < concurrent; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				client := &http.Client{Timeout: 3 * time.Second}
				body := fmt.Sprintf(`{"url":"https://barrage-test-%d.com"}`, rand.Int63())
				for {
					select {
					case <-ctx.Done():
						return
					default:
						req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, strings.NewReader(body))
						if err != nil {
							atomic.AddInt64(&errs, 1)
							continue
						}
						req.Header.Set("Content-Type", "application/json")
						resp, err := client.Do(req)
						if err != nil {
							atomic.AddInt64(&errs, 1)
							continue
						}
						resp.Body.Close()
						atomic.AddInt64(&total, 1)
					}
				}
			}()
		}
		wg.Wait()
		cancel()

		targetURL, _ := url.Parse(target)
		targetURL.Path = "health"
		alive := s.isTargetAlive(targetURL.String(), checkTimeout)

		results = append(results, roundResult{
			round:         round,
			concurrent:    concurrent,
			totalRequests: total,
			errors:        errs,
			targetAlive:   alive,
		})
		totalAll += total
		errorsAll += errs

		if !alive {
			break
		}

		concurrent *= 2
		if concurrent <= maxConcurrent {
			time.Sleep(pauseBetweenRounds)
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":    "complete",
		"total_requests": totalAll,
		"errors":    errorsAll,
		"rounds":    results,
	})
}

func (s *service) isTargetAlive(url string, timeout time.Duration) bool {
	client := &http.Client{Timeout: timeout}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	resp.Body.Close()
	return resp.StatusCode < 500
}

func (s *service) authMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		s.mu.RLock()
		expected := s.config.AuthToken
		s.mu.RUnlock()
	if expected == "" {
		next(w, r)
	return
	}
		token := r.Header.Get("Authorization")
	if strings.HasPrefix(token, "Bearer ") {
		token = strings.TrimSpace(strings.TrimPrefix(token, "Bearer "))
	}
	if token == "" || len(token) != len(expected) || subtle.ConstantTimeCompare([]byte(token), []byte(expected)) != 1 {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	return
	}
		next(w, r)
	}
}

func main() {
	godotenv.Load()
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
	router.HandleFunc("/admin/disable", svc.authMiddleware(svc.handleAdminDisable))
	router.HandleFunc("/admin/enable", svc.authMiddleware(svc.handleAdminEnable))
	router.HandleFunc("/crash", svc.authMiddleware(svc.handleCrash))
	router.HandleFunc("/limit", svc.authMiddleware(svc.handleLimit))
	router.HandleFunc("/limits", svc.authMiddleware(svc.handleLimits))
	router.HandleFunc("/config", svc.authMiddleware(svc.handleConfig))
	router.HandleFunc("/analytics", svc.handleAnalytics)
	router.HandleFunc("/ws/metrics", svc.handleWSMetrics)

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
		WriteTimeout:      120 * time.Second,
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
	svc.wsHub.stopHub()
	ctxShutdown, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	srv.Shutdown(ctxShutdown)
	log.Println("shutdown complete")
}
