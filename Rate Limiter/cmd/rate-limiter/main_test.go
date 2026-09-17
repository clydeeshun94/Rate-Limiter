package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
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
	resp, err := http.Get(health.URL + "/health")
	if err != nil {
		t.Fatalf("health request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health: expected 200, got %d", resp.StatusCode)
	}

	metrics := httptest.NewServer(http.HandlerFunc(svc.handleMetrics))
	defer metrics.Close()
	resp, err = http.Get(metrics.URL + "/metrics")
	if err != nil {
		t.Fatalf("metrics request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("metrics: expected 200, got %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if len(body) == 0 {
		t.Fatal("metrics: expected non-empty response")
	}
}

func TestAdmin_GetLimits(t *testing.T) {
	svc := newService()

	server := httptest.NewServer(http.HandlerFunc(svc.handleGetLimits))
	defer server.Close()

	resp, err := http.Get(server.URL + "/limits")
	if err != nil {
		t.Fatalf("limits request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result limitsResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if len(result.Limits) != len(svc.config.Algorithms) {
		t.Fatalf("expected %d limits, got %d", len(svc.config.Algorithms), len(result.Limits))
	}

	for _, li := range result.Limits {
		if li.Limit != svc.limits[li.Algorithm] {
			t.Fatalf("limit mismatch for %s: expected %d, got %d", li.Algorithm, svc.limits[li.Algorithm], li.Limit)
		}
		if li.Limit != svc.config.DefaultLimit {
			t.Fatalf("limit for %s should be default %d, got %d", li.Algorithm, svc.config.DefaultLimit, li.Limit)
		}
	}
}

func TestAdmin_GetLimitsMethodNotAllowed(t *testing.T) {
	svc := newService()

	server := httptest.NewServer(http.HandlerFunc(svc.handleGetLimits))
	defer server.Close()

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/limits", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", resp.StatusCode)
	}
}

func TestAdmin_SetLimits(t *testing.T) {
	svc := newService()

	server := httptest.NewServer(http.HandlerFunc(svc.handleSetLimits))
	defer server.Close()

	body, _ := json.Marshal(limitsRequest{
		Limits: []limitRequest{
			{Algorithm: "fixed_window", Limit: 200},
			{Algorithm: "token_bucket", Limit: 50},
		},
	})

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/limits", bytes.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	if svc.limits["fixed_window"] != 200 {
		t.Fatalf("fixed_window limit should be 200, got %d", svc.limits["fixed_window"])
	}
	if svc.limits["token_bucket"] != 50 {
		t.Fatalf("token_bucket limit should be 50, got %d", svc.limits["token_bucket"])
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if result["status"] != "limits updated" {
		t.Fatalf("expected status 'limits updated', got %s", result["status"])
	}
}

func TestAdmin_SetLimitsUnknownAlgorithm(t *testing.T) {
	svc := newService()

	server := httptest.NewServer(http.HandlerFunc(svc.handleSetLimits))
	defer server.Close()

	body, _ := json.Marshal(limitsRequest{
		Limits: []limitRequest{
			{Algorithm: "nonexistent", Limit: 100},
		},
	})

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/limits", bytes.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestAdmin_GetConfig(t *testing.T) {
	svc := newService()

	server := httptest.NewServer(http.HandlerFunc(svc.handleGetConfig))
	defer server.Close()

	resp, err := http.Get(server.URL + "/config")
	if err != nil {
		t.Fatalf("config request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result configResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if result.Port != svc.config.Port {
		t.Fatalf("port mismatch: expected %d, got %d", svc.config.Port, result.Port)
	}
	if result.DefaultLimit != svc.config.DefaultLimit {
		t.Fatalf("default_limit mismatch: expected %d, got %d", svc.config.DefaultLimit, result.DefaultLimit)
	}
	if len(result.Algorithms) != len(svc.config.Algorithms) {
		t.Fatalf("algorithms count mismatch: expected %d, got %d", len(svc.config.Algorithms), len(result.Algorithms))
	}
}

func TestAdmin_GetConfigMethodNotAllowed(t *testing.T) {
	svc := newService()

	server := httptest.NewServer(http.HandlerFunc(svc.handleGetConfig))
	defer server.Close()

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/config", nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", resp.StatusCode)
	}
}

func TestAdmin_SetConfig(t *testing.T) {
	svc := newService()

	server := httptest.NewServer(http.HandlerFunc(svc.handleSetConfig))
	defer server.Close()

	body, _ := json.Marshal(configUpdateRequest{
		Port:         9090,
		DefaultLimit: 250,
	})

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/config", bytes.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	if svc.config.Port != 9090 {
		t.Fatalf("port should be 9090, got %d", svc.config.Port)
	}
	if svc.config.DefaultLimit != 250 {
		t.Fatalf("default_limit should be 250, got %d", svc.config.DefaultLimit)
	}

	var result map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if result["status"] != "config updated" {
		t.Fatalf("expected status 'config updated', got %s", result["status"])
	}
}

func TestAdmin_SetConfigPartialUpdate(t *testing.T) {
	svc := newService()

	server := httptest.NewServer(http.HandlerFunc(svc.handleSetConfig))
	defer server.Close()

	body, _ := json.Marshal(configUpdateRequest{
		Port: 0,
		DefaultLimit: 300,
	})

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/config", bytes.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if svc.config.Port != 8080 {
		t.Fatalf("port should remain 8080, got %d", svc.config.Port)
	}
	if svc.config.DefaultLimit != 300 {
		t.Fatalf("default_limit should be 300, got %d", svc.config.DefaultLimit)
	}
}

func TestAdmin_SetConfigInvalidJSON(t *testing.T) {
	svc := newService()

	server := httptest.NewServer(http.HandlerFunc(svc.handleSetConfig))
	defer server.Close()

	req, _ := http.NewRequest(http.MethodPost, server.URL+"/config", bytes.NewReader([]byte("invalid json")))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.StatusCode)
	}
}

func TestLoadConfig_Defaults(t *testing.T) {
	os.Unsetenv("RLIMITER_PORT")
	os.Unsetenv("RLIMITER_DEFAULT_LIMIT")
	os.Unsetenv("RLIMITER_ALGORITHMS")
	defer os.Unsetenv("RLIMITER_PORT")
	defer os.Unsetenv("RLIMITER_DEFAULT_LIMIT")
	defer os.Unsetenv("RLIMITER_ALGORITHMS")

	cfg := loadConfig()

	if cfg.Port != 8080 {
		t.Fatalf("expected default port 8080, got %d", cfg.Port)
	}
	if cfg.DefaultLimit != 100 {
		t.Fatalf("expected default limit 100, got %d", cfg.DefaultLimit)
	}
	if len(cfg.Algorithms) != 5 {
		t.Fatalf("expected 5 algorithms, got %d", len(cfg.Algorithms))
	}
}

func TestLoadConfig_PortFromEnv(t *testing.T) {
	os.Setenv("RLIMITER_PORT", "9090")
	defer os.Unsetenv("RLIMITER_PORT")

	cfg := loadConfig()
	if cfg.Port != 9090 {
		t.Fatalf("expected port 9090, got %d", cfg.Port)
	}
}

func TestLoadConfig_DefaultLimitFromEnv(t *testing.T) {
	os.Setenv("RLIMITER_DEFAULT_LIMIT", "250")
	defer os.Unsetenv("RLIMITER_DEFAULT_LIMIT")

	cfg := loadConfig()
	if cfg.DefaultLimit != 250 {
		t.Fatalf("expected limit 250, got %d", cfg.DefaultLimit)
	}
}

func TestLoadConfig_AlgorithmsFromEnv(t *testing.T) {
	os.Setenv("RLIMITER_ALGORITHMS", "fixed_window,token_bucket")
	defer os.Unsetenv("RLIMITER_ALGORITHMS")

	cfg := loadConfig()
	if len(cfg.Algorithms) != 2 {
		t.Fatalf("expected 2 algorithms, got %d", len(cfg.Algorithms))
	}
	if cfg.Algorithms[0] != "fixed_window" {
		t.Fatalf("expected fixed_window, got %s", cfg.Algorithms[0])
	}
	if cfg.Algorithms[1] != "token_bucket" {
		t.Fatalf("expected token_bucket, got %s", cfg.Algorithms[1])
	}
}

func TestLoadConfig_InvalidPortIgnored(t *testing.T) {
	os.Setenv("RLIMITER_PORT", "not_a_number")
	defer os.Unsetenv("RLIMITER_PORT")

	cfg := loadConfig()
	if cfg.Port != 8080 {
		t.Fatalf("expected fallback port 8080, got %d", cfg.Port)
	}
}

func TestLoadConfig_InvalidLimitIgnored(t *testing.T) {
	os.Setenv("RLIMITER_DEFAULT_LIMIT", "abc")
	defer os.Unsetenv("RLIMITER_DEFAULT_LIMIT")

	cfg := loadConfig()
	if cfg.DefaultLimit != 100 {
		t.Fatalf("expected fallback limit 100, got %d", cfg.DefaultLimit)
	}
}
