package limiter_test

import (
	"testing"
	"time"

	limiter "rate-limiter/internal/limiter"
	rate "rate-limiter/internal/storage"
)

func TestSlidingWindowLog_AllowsWithinLimit(t *testing.T) {
	storage := rate.NewMemoryStorage()
	l := limiter.NewSlidingWindowLogWithLimit(storage, 3)
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
	storage := rate.NewMemoryStorage()
	l := limiter.NewSlidingWindowLogWithLimit(storage, 3)
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
	storage := rate.NewMemoryStorage()
	l := limiter.NewSlidingWindowLogWithLimit(storage, 3)
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

func TestSlidingWindowLog_UsesStorageInterface(t *testing.T) {
	storage := rate.NewMemoryStorage()
	l := limiter.NewSlidingWindowLogWithLimit(storage, 2)
	policy := limiter.Policy{Limit: 2, Window: 60 * time.Second}

	l.Check("charlie", policy)
	l.Check("charlie", policy)

	record, exists, _ := storage.Get("charlie")
	if !exists {
		t.Fatal("expected storage to have record for charlie")
	}
	if len(record.Timestamps) != 2 {
		t.Fatalf("expected 2 timestamps in storage, got %d", len(record.Timestamps))
	}
}

func TestSlidingWindowLog_StoragePersistsTimestamps(t *testing.T) {
	storage := rate.NewMemoryStorage()
	l := limiter.NewSlidingWindowLogWithLimit(storage, 3)
	policy := limiter.Policy{Limit: 3, Window: 60 * time.Second}

	for i := 0; i < 3; i++ {
		l.Check("dave", policy)
	}

	record, exists, _ := storage.Get("dave")
	if !exists {
		t.Fatal("expected storage to have record for dave")
	}
	if record.Count != 3 {
		t.Fatalf("expected count=3, got %d", record.Count)
	}
	if len(record.Timestamps) != 3 {
		t.Fatalf("expected 3 timestamps, got %d", len(record.Timestamps))
	}
}
