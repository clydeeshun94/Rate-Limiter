package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"time"

	limiter "rate-limiter/internal/limiter"
	rate "rate-limiter/internal/storage"
	"rate-limiter/internal/metrics"
)

type service struct {
	limiters  map[string]limiter.RateLimiter
	collector *metrics.Collector
}

type checkRequest struct {
	Identity   string         `json:"identity"`
	Algorithm  string         `json:"algorithm"`
	Policy     limiter.Policy `json:"policy"`
}

type checkResponse struct {
	Allowed    bool          `json:"allowed"`
	Remaining  int           `json:"remaining"`
	RetryAfter time.Duration `json:"retry_after"`
	ResetTime  string        `json:"reset_time"`
}

type limitRequest struct {
	Algorithm string `json:"algorithm"`
	Limit     int    `json:"limit"`
}

func newService() *service {
	collector := metrics.NewCollector()
	defaultLimit := 100

	l := &service{
		limiters:  make(map[string]limiter.RateLimiter),
		collector: collector,
	}

	storage := rate.NewMemoryStorage()
	l.limiters["fixed_window"] = metrics.Wrap(
		limiter.NewFixedWindowWithLimit(storage, defaultLimit),
		collector, "fixed_window",
	)
	l.limiters["sliding_window_counter"] = metrics.Wrap(
		limiter.NewSlidingWindowCounterWithLimit(storage, defaultLimit),
		collector, "sliding_window_counter",
	)
	l.limiters["sliding_window_log"] = metrics.Wrap(
		limiter.NewSlidingWindowLogWithLimit(storage, defaultLimit),
		collector, "sliding_window_log",
	)
	l.limiters["token_bucket"] = metrics.Wrap(
		limiter.NewTokenBucketWithLimit(storage, defaultLimit),
		collector, "token_bucket",
	)
	l.limiters["leaky_bucket"] = metrics.Wrap(
		limiter.NewLeakyBucketWithLimit(storage, defaultLimit),
		collector, "leaky_bucket",
	)

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
		Remaining:  result.Remaining,
		RetryAfter: result.RetryAfter,
		ResetTime:  result.ResetTime.Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
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
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "limit updated"})
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
	http.HandleFunc("/metrics", svc.handleMetrics)
	http.HandleFunc("/health", svc.handleHealth)

	port := 8080
	addr := ":" + strconv.Itoa(port)
	log.Printf("rate-limiter service starting on %s", addr)
	log.Fatal(http.ListenAndServe(addr, nil))
}
