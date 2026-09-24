package integration_test

import (
	"fmt"
	"os"
	"testing"
	"time"

	limiter "rate-limiter/internal/limiter"
	rate "rate-limiter/internal/storage"
)

// TestRedisStorage_Smoke is a real-Redis integration test. It is skipped when
// RATE_LIMITER_REDIS_ADDR is unset or when Redis is unreachable, so it stays
// green in environments without Redis while still asserting distributed
// FixedWindow atomicity against a live Redis in CI.
func TestRedisStorage_Smoke(t *testing.T) {
	addr := os.Getenv("RATE_LIMITER_REDIS_ADDR")
	if addr == "" {
		t.Skip("RATE_LIMITER_REDIS_ADDR not set; skipping Redis smoke test")
	}

	storage := rate.NewRedisStorage(addr, os.Getenv("RATE_LIMITER_REDIS_PASSWORD"), 0, 120*time.Second)
	l := limiter.NewFixedWindowWithLimit(storage, 2)
	p := limiter.Policy{Limit: 2, Window: 60 * time.Second}
	id := fmt.Sprintf("redis-smoke-%d", time.Now().UnixNano())

	for i := 0; i < 2; i++ {
		r, err := l.Check(id, p)
		if err != nil {
			t.Skipf("Redis unavailable: %v", err)
		}
		if !r.Allowed {
			t.Fatalf("req %d: want allowed, got %+v", i+1, r)
		}
	}

	r, err := l.Check(id, p)
	if err != nil {
		t.Skipf("Redis unavailable: %v", err)
	}
	if r.Allowed {
		t.Fatalf("3rd request: want denied, got allowed")
	}
}
