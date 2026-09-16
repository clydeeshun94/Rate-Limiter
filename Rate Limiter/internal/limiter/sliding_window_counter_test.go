package limiter_test

import (
	"testing"
	"time"

	limiter "rate-limiter/internal/limiter"
	rate "rate-limiter/internal/storage"
)

func TestSlidingWindowCounter_AllowsWithinLimit(t *testing.T) {
	storage := rate.NewMemoryStorage()
	l := limiter.NewSlidingWindowCounterWithLimit(storage, 3)
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

func TestSlidingWindowCounter_DeniesOverLimit(t *testing.T) {
	storage := rate.NewMemoryStorage()
	l := limiter.NewSlidingWindowCounterWithLimit(storage, 3)
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

func TestSlidingWindowCounter_AvoidsBoundarySpike(t *testing.T) {
	storage := rate.NewMemoryStorage()
	l := limiter.NewSlidingWindowCounterWithLimit(storage, 3)
	policy := limiter.Policy{Limit: 3, Window: 5 * time.Second}

	for i := 0; i < 3; i++ {
		l.Check("bob", policy)
	}

	time.Sleep(6 * time.Second)

	allowed := 0
	for i := 0; i < 3; i++ {
		result, err := l.Check("bob", policy)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Allowed {
			allowed++
		}
	}

	if allowed == 0 {
		t.Fatal("expected some requests allowed after partial window elapsed, got none")
	}
}
