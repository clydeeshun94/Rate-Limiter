package storage_test

import (
	"testing"
	"time"

	rate "rate-limiter/internal/storage"
)

func TestRedisStorage_New(t *testing.T) {
	s := rate.NewRedisStorage("localhost:6379", "", 0, 0)
	if s == nil {
		t.Fatal("expected non-nil RedisStorage")
	}
}

func TestRedisStorage_WindowsOnly(t *testing.T) {
	s := rate.NewRedisStorage("localhost:6379", "", 0, 0)
	if s == nil {
		t.Fatal("expected non-nil RedisStorage")
	}
	t.Skip("requires running Redis instance")
}

func TestRedisStorage_RetryOnFailure(t *testing.T) {
	s := rate.NewRedisStorage("localhost:9999", "", 0, 0)
	if s == nil {
		t.Fatal("expected non-nil RedisStorage")
	}

	start := time.Now()
	err := s.Set("test-retry", rate.Record{Count: 1})
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error when Redis is unavailable")
	}

	minExpected := 200 * time.Millisecond
	if elapsed < minExpected {
		t.Fatalf("expected retries to take at least %v, took %v", minExpected, elapsed)
	}
}
