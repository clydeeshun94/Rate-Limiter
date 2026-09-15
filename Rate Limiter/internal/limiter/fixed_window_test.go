package limiter_test

import (
	"testing"
	"time"

	limiter "rate-limiter/internal/limiter"
	rate "rate-limiter/internal/storage"
)

func TestFixedWindow_AllowsWithinLimit(t *testing.T) {
	storage := rate.NewMemoryStorage()
	l := limiter.NewFixedWindow(storage)
	policy := limiter.Policy{Limit: 3, Window: 60 * time.Second}

	for i := 0; i < 3; i++ {
		result, err := l.Check("alice", policy)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Allowed {
			t.Fatalf("request %d: expected allowed, got denied", i+1)
		}
	}
}

func TestFixedWindow_DeniesOverLimit(t *testing.T) {
	storage := rate.NewMemoryStorage()
	l := limiter.NewFixedWindow(storage)
	policy := limiter.Policy{Limit: 3, Window: 60 * time.Second}

	for i := 0; i < 3; i++ {
		l.Check("alice", policy)
	}

	result, err := l.Check("alice", policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Fatal("expected denied, got allowed")
	}
}
