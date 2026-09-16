package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	limiter "rate-limiter/internal/limiter"
)

func TestCheck_ReturnsRateLimitHeaders(t *testing.T) {
	svc := newService()

	server := httptest.NewServer(http.HandlerFunc(svc.handleCheck))
	defer server.Close()

	body, _ := json.Marshal(map[string]interface{}{
		"identity":  "header-test",
		"algorithm": "fixed_window",
		"policy":    map[string]interface{}{"limit": 5, "window": int64(60 * time.Second)},
	})

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/check", bytes.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("X-RateLimit-Limit") != "100" {
		t.Fatalf("expected X-RateLimit-Limit=100, got %s", resp.Header.Get("X-RateLimit-Limit"))
	}
	if resp.Header.Get("X-RateLimit-Remaining") == "" {
		t.Fatal("expected X-RateLimit-Remaining to be set")
	}
	if resp.Header.Get("X-RateLimit-Reset") == "" {
		t.Fatal("expected X-RateLimit-Reset to be set")
	}
	if _, err := time.Parse(time.RFC3339, resp.Header.Get("X-RateLimit-Reset")); err != nil {
		t.Fatalf("X-RateLimit-Reset not valid RFC3339: %s", resp.Header.Get("X-RateLimit-Reset"))
	}
}

func TestCheck_DeniedReturnsRetryAfter(t *testing.T) {
	svc := newService()
	if setter, ok := svc.limiters["fixed_window"].(limiter.LimitSetter); ok {
		setter.SetLimit(1)
	}
	for i := 0; i < 2; i++ {
		svc.limiters["fixed_window"].Check("deny-test", limiter.Policy{Limit: 1, Window: 60 * time.Second})
	}

	server := httptest.NewServer(http.HandlerFunc(svc.handleCheck))
	defer server.Close()

	body, _ := json.Marshal(map[string]interface{}{
		"identity":  "deny-test",
		"algorithm": "fixed_window",
		"policy":    map[string]interface{}{"limit": 1, "window": int64(60 * time.Second)},
	})

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/check", bytes.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Retry-After") == "" {
		t.Fatal("expected Retry-After header on denied request")
	}
	if seconds, _ := strconv.Atoi(resp.Header.Get("Retry-After")); seconds < 0 {
		t.Fatalf("Retry-After should be non-negative, got %d", seconds)
	}
}

func TestCheck_AllowedNoRetryAfter(t *testing.T) {
	svc := newService()

	server := httptest.NewServer(http.HandlerFunc(svc.handleCheck))
	defer server.Close()

	body, _ := json.Marshal(map[string]interface{}{
		"identity":  "allow-test",
		"algorithm": "fixed_window",
		"policy":    map[string]interface{}{"limit": 100, "window": int64(60 * time.Second)},
	})

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/check", bytes.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.Header.Get("Retry-After") != "" {
		t.Fatalf("expected no Retry-After on allowed request, got %s", resp.Header.Get("Retry-After"))
	}
}

func TestHealthAndMetrics(t *testing.T) {
	svc := newService()

	health := httptest.NewServer(http.HandlerFunc(svc.handleHealth))
	defer health.Close()
	resp, _ := http.Get(health.URL + "/health")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health: expected 200, got %d", resp.StatusCode)
	}

	metrics := httptest.NewServer(http.HandlerFunc(svc.handleMetrics))
	defer metrics.Close()
	resp, _ = http.Get(metrics.URL + "/metrics")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("metrics: expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) == 0 {
		t.Fatal("metrics: expected non-empty response")
	}
}
