package limiter_test

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	limiter "rate-limiter/internal/limiter"
	rate "rate-limiter/internal/storage"
)

func TestFixedWindow_Concurrent(t *testing.T) {
	storage := rate.NewMemoryStorage()
	l := limiter.NewFixedWindowWithLimit(storage, 100)
	policy := limiter.Policy{Limit: 100, Window: 60 * time.Second}

	var wg sync.WaitGroup
	var allowed int64
	var mu sync.Mutex

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := l.Check("alice", policy)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if result.Allowed {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if allowed > 100 {
		t.Fatalf("expected at most 100 allowed, got %d", allowed)
	}
}

func TestFixedWindow_ConcurrentParallel(t *testing.T) {
	t.Parallel()
	TestFixedWindow_Concurrent(t)
}

func TestSlidingWindowCounter_Concurrent(t *testing.T) {
	storage := rate.NewMemoryStorage()
	l := limiter.NewSlidingWindowCounterWithLimit(storage, 100)
	policy := limiter.Policy{Limit: 100, Window: 60 * time.Second}

	var wg sync.WaitGroup
	var allowed int64
	var mu sync.Mutex

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := l.Check("bob", policy)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if result.Allowed {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if allowed > 100 {
		t.Fatalf("expected at most 100 allowed, got %d", allowed)
	}
}

func TestSlidingWindowCounter_ConcurrentParallel(t *testing.T) {
	t.Parallel()
	TestSlidingWindowCounter_Concurrent(t)
}

func TestTokenBucket_Concurrent(t *testing.T) {
	storage := rate.NewMemoryStorage()
	l := limiter.NewTokenBucketWithLimit(storage, 100)
	policy := limiter.Policy{Limit: 100, Window: 60 * time.Second}

	var wg sync.WaitGroup
	var allowed int64
	var mu sync.Mutex

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := l.Check("carol", policy)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if result.Allowed {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if allowed > 100 {
		t.Fatalf("expected at most 100 allowed, got %d", allowed)
	}
}

func TestTokenBucket_ConcurrentParallel(t *testing.T) {
	t.Parallel()
	TestTokenBucket_Concurrent(t)
}

func TestLeakyBucket_Concurrent(t *testing.T) {
	storage := rate.NewMemoryStorage()
	l := limiter.NewLeakyBucketWithLimit(storage, 100)
	policy := limiter.Policy{Limit: 100, Window: 60 * time.Second}

	var wg sync.WaitGroup
	var allowed int64
	var mu sync.Mutex

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := l.Check("dave", policy)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if result.Allowed {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if allowed > 100 {
		t.Fatalf("expected at most 100 allowed, got %d", allowed)
	}
}

func TestLeakyBucket_ConcurrentParallel(t *testing.T) {
	t.Parallel()
	TestLeakyBucket_Concurrent(t)
}

func TestSlidingWindowLog_Concurrent(t *testing.T) {
	storage := rate.NewMemoryStorage()
	l := limiter.NewSlidingWindowLogWithLimit(storage, 100)
	policy := limiter.Policy{Limit: 100, Window: 60 * time.Second}

	var wg sync.WaitGroup
	var allowed int64
	var mu sync.Mutex

	for i := 0; i < 1000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := l.Check("eve", policy)
			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}
			if result.Allowed {
				mu.Lock()
				allowed++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if allowed > 100 {
		t.Fatalf("expected at most 100 allowed, got %d", allowed)
	}
}

func TestSlidingWindowLog_ConcurrentParallel(t *testing.T) {
	t.Parallel()
	TestSlidingWindowLog_Concurrent(t)
}

func TestFixedWindow_ConcurrentMultipleIdentities(t *testing.T) {
	storage := rate.NewMemoryStorage()
	l := limiter.NewFixedWindowWithLimit(storage, 10)
	policy := limiter.Policy{Limit: 10, Window: 60 * time.Second}

	var wg sync.WaitGroup
	identities := []string{"user1", "user2", "user3", "user4", "user5"}
	allowedCounts := make(map[string]int64, len(identities))
	var mu sync.Mutex

	for _, id := range identities {
		allowedCounts[id] = 0
	}

	for _, id := range identities {
		for i := 0; i < 200; i++ {
			wg.Add(1)
			go func(identity string) {
				defer wg.Done()
				result, err := l.Check(identity, policy)
				if err != nil {
					t.Errorf("unexpected error: %v", err)
					return
				}
				if result.Allowed {
					mu.Lock()
					allowedCounts[identity]++
					mu.Unlock()
				}
			}(id)
		}
	}
	wg.Wait()

	for _, id := range identities {
		if allowedCounts[id] > 10 {
			t.Fatalf("identity %s: expected at most 10 allowed, got %d", id, allowedCounts[id])
		}
	}
}

func TestSlidingWindowCounter_ConcurrentMultipleIdentities(t *testing.T) {
	storage := rate.NewMemoryStorage()
	l := limiter.NewSlidingWindowCounterWithLimit(storage, 10)
	policy := limiter.Policy{Limit: 10, Window: 60 * time.Second}

	var wg sync.WaitGroup
	identities := []string{"user1", "user2", "user3", "user4", "user5"}
	allowedCounts := make(map[string]int64, len(identities))
	var mu sync.Mutex

	for _, id := range identities {
		allowedCounts[id] = 0
	}

	for _, id := range identities {
		for i := 0; i < 200; i++ {
			wg.Add(1)
			go func(identity string) {
				defer wg.Done()
				result, err := l.Check(identity, policy)
				if err != nil {
					t.Errorf("unexpected error: %v", err)
					return
				}
				if result.Allowed {
					mu.Lock()
					allowedCounts[identity]++
					mu.Unlock()
				}
			}(id)
		}
	}
	wg.Wait()

	for _, id := range identities {
		if allowedCounts[id] > 10 {
			t.Fatalf("identity %s: expected at most 10 allowed, got %d", id, allowedCounts[id])
		}
	}
}

func TestFixedWindow_ConcurrentUsesAtomicCounter(t *testing.T) {
	storage := rate.NewMemoryStorage()
	l := limiter.NewFixedWindowWithLimit(storage, 50)
	policy := limiter.Policy{Limit: 50, Window: 60 * time.Second}

	var wg sync.WaitGroup
	var allowed int64

	for i := 0; i < 500; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := l.Check("frank", policy)
			if err != nil {
				return
			}
			if result.Allowed {
				atomic.AddInt64(&allowed, 1)
			}
		}()
	}
	wg.Wait()

	if allowed > 50 {
		t.Fatalf("expected at most 50 allowed, got %d", allowed)
	}
}

func TestSlidingWindowLog_ConcurrentStress(t *testing.T) {
	storage := rate.NewMemoryStorage()
	l := limiter.NewSlidingWindowLogWithLimit(storage, 200)
	policy := limiter.Policy{Limit: 200, Window: 60 * time.Second}

	var wg sync.WaitGroup
	var allowed int64

	for i := 0; i < 2000; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := l.Check("grace", policy)
			if err != nil {
				return
			}
			if result.Allowed {
				atomic.AddInt64(&allowed, 1)
			}
		}()
	}
	wg.Wait()

	if allowed > 200 {
		t.Fatalf("expected at most 200 allowed, got %d", allowed)
	}
}

