package limiter_test

import (
	"testing"
	"time"

	limiter "rate-limiter/internal/limiter"
	rate "rate-limiter/internal/storage"
)

func TestLeakyBucket_AllowsWithinLimit(t *testing.T) {
	storage := rate.NewMemoryStorage()
	l := limiter.NewLeakyBucket(storage)
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

func TestLeakyBucket_DeniesOverLimit(t *testing.T) {
	storage := rate.NewMemoryStorage()
	l := limiter.NewLeakyBucket(storage)
	policy := limiter.Policy{Limit: 2, Window: 60 * time.Second}

	for i := 0; i < 2; i++ {
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

func TestLeakyBucket_DrainsOverTime(t *testing.T) {
	storage := rate.NewMemoryStorage()
	l := limiter.NewLeakyBucket(storage)
	policy := limiter.Policy{Limit: 2, Window: 2 * time.Second}

	l.Check("bob", policy)
	l.Check("bob", policy)

	result, err := l.Check("bob", policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Fatal("expected denied, bucket full, got allowed")
	}

	time.Sleep(3 * time.Second)

	result, err = l.Check("bob", policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Fatal("expected allowed after draining, got denied")
	}
}
