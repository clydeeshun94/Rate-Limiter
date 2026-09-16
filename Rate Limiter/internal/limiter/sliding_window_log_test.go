package limiter_test

import (
	"testing"
	"time"

	limiter "rate-limiter/internal/limiter"
)

func TestSlidingWindowLog_AllowsWithinLimit(t *testing.T) {
	l := limiter.NewSlidingWindowLog()
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

func TestSlidingWindowLog_DeniesOverLimit(t *testing.T) {
	l := limiter.NewSlidingWindowLog()
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

func TestSlidingWindowLog_EvictsOldTimestamps(t *testing.T) {
	l := limiter.NewSlidingWindowLog()
	policy := limiter.Policy{Limit: 3, Window: 2 * time.Second}

	for i := 0; i < 3; i++ {
		l.Check("bob", policy)
	}

	time.Sleep(3 * time.Second)

	result, err := l.Check("bob", policy)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Fatal("expected allowed after window expired, got denied")
	}
}
