package limiter_test

import (
	"testing"
	"time"

	limiter "rate-limiter/internal/limiter"
	rate "rate-limiter/internal/storage"
)

func TestTokenBucket_AllowsWithinLimit(t *testing.T) {
	storage := rate.NewMemoryStorage()
	l := limiter.NewTokenBucket(storage)
	policy := limiter.Policy{Limit: 5, Window: 60 * time.Second}

	for i := 0; i < 5; i++ {
		result, err := l.Check("alice", policy)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !result.Allowed {
			t.Fatalf("request %d: expected allowed, got denied", i+1)
		}
	}
}

func TestTokenBucket_RefillsOverTime(t *testing.T) {
	storage := rate.NewMemoryStorage()
	l := limiter.NewTokenBucket(storage)
	policy := limiter.Policy{Limit: 2, Window: 2 * time.Second}

	l.Check("bob", policy)
	l.Check("bob", policy)

	result, err := l.Check("bob", policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Fatal("expected denied, bucket empty, got allowed")
	}

	time.Sleep(3 * time.Second)

	result, err = l.Check("bob", policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Fatal("expected allowed after refill, got denied")
	}
}
