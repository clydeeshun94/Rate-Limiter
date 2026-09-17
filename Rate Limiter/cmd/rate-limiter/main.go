package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	limiter "rate-limiter/internal/limiter"
	rate "rate-limiter/internal/storage"
	"rate-limiter/internal/metrics"
)

type service struct {
	limiters  map[string]limiter.RateLimiter
	limits    map[string]int
	collector *metrics.Collector
	config    serverConfig
}

type serverConfig struct {
	Port         int      `json:"port"`
	DefaultLimit int      `json:"default_limit"`
	Algorithms   []string `json:"algorithms"`
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

func loadConfig() serverConfig {
    cfg := serverConfig{
        Port:         8080,
        DefaultLimit: 100,
        Algorithms: []string{
            "fixed_window",
            "sliding_window_counter",
            "sliding_window_log",
            "token_bucket",
            "leaky_bucket",
        },
    }

    if portStr := os.Getenv("RLIMITER_PORT"); portStr != "" {
        if port, err := strconv.Atoi(portStr); err == nil {
            cfg.Port = port
        }
    }

    if limitStr := os.Getenv("RLIMITER_DEFAULT_LIMIT"); limitStr != "" {
        if limit, err := strconv.Atoi(limitStr); err == nil {
            cfg.DefaultLimit = limit
        }
    }

    if algosStr := os.Getenv("RLIMITER_ALGORITHMS"); algosStr != "" {
        cfg.Algorithms = strings.Split(algosStr, ",")
    }

    return cfg
}

func newService() *service {
    collector := metrics.NewCollector()
    config := loadConfig()
    defaultLimit := config.DefaultLimit
    algorithms := config.Algorithms

	l := &service{
		limiters:  make(map[string]limiter.RateLimiter),
		limits:    make(map[string]int),
		collector: collector,
		config: serverConfig{
			Port:         config.Port,
			DefaultLimit: defaultLimit,
			Algorithms:   algorithms,
		},
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

	result, err := l.Check(req.Identity, req.Policy)
	if err != nil {
		http.Error(w, fmt.Sprintf("check failed: %v", err), http.StatusInternalServerError)
		return
	}

	resp := checkResponse{
		Allowed:    result.Allowed,
		Limit:      result.Limit,
		Remaining:  result.Remaining,
		RetryAfter: result.RetryAfter,
		ResetTime:  result.ResetTime.Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-RateLimit-Limit", strconv.Itoa(result.Limit))
	w.Header().Set("X-RateLimit-Remaining", strconv.Itoa(result.Remaining))
	w.Header().Set("X-RateLimit-Reset", result.ResetTime.Format(time.RFC3339))
	if result.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.FormatInt(int64(result.RetryAfter.Seconds()), 10))
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

func main() {
	svc := newService()

	http.HandleFunc("/check", svc.handleCheck)
	http.HandleFunc("/limit", svc.handleLimit)
	http.HandleFunc("/limits", svc.handleLimits)
	http.HandleFunc("/config", svc.handleConfig)
	http.HandleFunc("/metrics", svc.handleMetrics)
	http.HandleFunc("/health", svc.handleHealth)

	port := svc.config.Port
	addr := ":" + strconv.Itoa(port)
	log.Printf("rate-limiter service starting on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
