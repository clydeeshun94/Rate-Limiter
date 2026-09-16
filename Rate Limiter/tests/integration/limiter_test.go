package integration_test

import (
	"testing"
	"time"

	limiter "rate-limiter/internal/limiter"
	rate "rate-limiter/internal/storage"
)

func TestFixedWindow_StoragePersistence(t *testing.T) {
	storage := rate.NewMemoryStorage()
	policy := limiter.Policy{Limit: 3, Window: 60 * time.Second}

	l1 := limiter.NewFixedWindowWithLimit(storage, 3)
	l1.Check("alice", policy)
	l1.Check("alice", policy)
	l1.Check("alice", policy)

	l2 := limiter.NewFixedWindowWithLimit(storage, 3)
	result, err := l2.Check("alice", policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Fatal("expected denied (3 of 3 used), got allowed")
	}
}

func TestSlidingWindowCounter_StoragePersistence(t *testing.T) {
	storage := rate.NewMemoryStorage()
	policy := limiter.Policy{Limit: 3, Window: 60 * time.Second}

	l1 := limiter.NewSlidingWindowCounterWithLimit(storage, 3)
	l1.Check("bob", policy)
	l1.Check("bob", policy)
	l1.Check("bob", policy)

	l2 := limiter.NewSlidingWindowCounterWithLimit(storage, 3)
	result, err := l2.Check("bob", policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Fatal("expected denied (3 of 3 used), got allowed")
	}
}

func TestLeakyBucket_StoragePersistsRecord(t *testing.T) {
	storage := rate.NewMemoryStorage()
	policy := limiter.Policy{Limit: 3, Window: 60 * time.Second}

	l1 := limiter.NewLeakyBucketWithLimit(storage, 3)
	l1.Check("carol", policy)

	_, exists := storage.Get("carol")
	if !exists {
		t.Fatal("expected storage to have record for carol")
	}
}

func TestTokenBucket_StoragePersistsRecord(t *testing.T) {
	storage := rate.NewMemoryStorage()
	policy := limiter.Policy{Limit: 2, Window: 60 * time.Second}

	l1 := limiter.NewTokenBucketWithLimit(storage, 2)
	l1.Check("dave", policy)
	l1.Check("dave", policy)

	_, exists := storage.Get("dave")
	if !exists {
		t.Fatal("expected storage to have record for dave")
	}
}

func TestSlidingWindowLog_StoragePersistence(t *testing.T) {
	storage := rate.NewMemoryStorage()
	policy := limiter.Policy{Limit: 3, Window: 60 * time.Second}

	l1 := limiter.NewSlidingWindowLogWithLimit(storage, 3)
	l1.Check("eve", policy)
	l1.Check("eve", policy)
	l1.Check("eve", policy)

	l2 := limiter.NewSlidingWindowLogWithLimit(storage, 3)
	result, err := l2.Check("eve", policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Fatal("expected denied (3 of 3 used), got allowed")
	}
}

func TestAllFiveAlgorithms_WithStorage(t *testing.T) {
	storage := rate.NewMemoryStorage()
	policy := limiter.Policy{Limit: 5, Window: 60 * time.Second}

	tests := []struct {
		name    string
		make    func() limiter.RateLimiter
		identity string
	}{
		{"fixed_window", func() limiter.RateLimiter { return limiter.NewFixedWindowWithLimit(storage, 5) }, "fw_user"},
		{"sliding_window_counter", func() limiter.RateLimiter { return limiter.NewSlidingWindowCounterWithLimit(storage, 5) }, "swc_user"},
		{"token_bucket", func() limiter.RateLimiter { return limiter.NewTokenBucketWithLimit(storage, 5) }, "tb_user"},
		{"leaky_bucket", func() limiter.RateLimiter { return limiter.NewLeakyBucketWithLimit(storage, 5) }, "lb_user"},
		{"sliding_window_log", func() limiter.RateLimiter { return limiter.NewSlidingWindowLogWithLimit(storage, 5) }, "swl_user"},
	}

	for _, tt := range tests {
		l := tt.make()
		for i := 0; i < 5; i++ {
			result, err := l.Check(tt.identity, policy)
			if err != nil {
				t.Fatalf("%s: unexpected error on check %d: %v", tt.name, i, err)
			}
			if !result.Allowed {
				t.Fatalf("%s: expected allowed on check %d, got denied", tt.name, i)
			}
		}

		result, err := l.Check(tt.identity, policy)
		if err != nil {
			t.Fatalf("%s: unexpected error on check 6: %v", tt.name, err)
		}
		if result.Allowed {
			t.Fatalf("%s: expected denied on check 6, got allowed", tt.name)
		}
	}
}
