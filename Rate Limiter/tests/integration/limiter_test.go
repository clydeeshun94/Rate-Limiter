package integration_test // integration_test: cross-algorithm and storage persistence tests

import ( // import: testing, time, and rate limiter packages
	"testing" // testing: Go test framework
	"time" // time: duration and timestamp types

	limiter "rate-limiter/internal/limiter" // limiter: all rate limiting algorithms
	rate "rate-limiter/internal/storage" // rate: storage implementations
)

// TestFixedWindow_StoragePersistence verifies that FixedWindow persists state in shared storage. // TestFixedWindow_StoragePersistence: storage sharing across limiter instances
func TestFixedWindow_StoragePersistence(t *testing.T) { // TestFixedWindow_StoragePersistence: 3 requests exhaust limit, 4th denied
	storage := rate.NewMemoryStorage() // storage: fresh in-memory storage
	policy := limiter.Policy{Limit: 3, Window: 60 * time.Second} // policy: 3 requests per 60 seconds

	l1 := limiter.NewFixedWindowWithLimit(storage, 3) // l1: first limiter instance (same storage)
	l1.Check("alice", policy) // alice: request 1 (allowed)
	l1.Check("alice", policy) // alice: request 2 (allowed)
	l1.Check("alice", policy) // alice: request 3 (allowed)

	l2 := limiter.NewFixedWindowWithLimit(storage, 3) // l2: second limiter instance (SHARED storage)
	result, err := l2.Check("alice", policy) // alice: request 4 via different limiter instance
	if err != nil { // error handling
		t.Fatalf("unexpected error: %v", err) // fail on unexpected error
	}
	if result.Allowed { // alice should be denied (3 of 3 used, tracked via shared storage)
		t.Fatal("expected denied (3 of 3 used), got allowed") // fail: storage not shared properly
	}
}

// TestSlidingWindowCounter_StoragePersistence verifies SlidingWindowCounter persists state. // TestSlidingWindowCounter_StoragePersistence: cross-instance storage sharing
func TestSlidingWindowCounter_StoragePersistence(t *testing.T) { // TestSlidingWindowCounter_StoragePersistence: 3 requests in shared storage, 4th denied
	storage := rate.NewMemoryStorage() // storage: fresh in-memory storage
	policy := limiter.Policy{Limit: 3, Window: 60 * time.Second} // policy: 3 requests per 60 seconds

	l1 := limiter.NewSlidingWindowCounterWithLimit(storage, 3) // l1: first limiter instance
	l1.Check("bob", policy) // bob: request 1 (allowed)
	l1.Check("bob", policy) // bob: request 2 (allowed)
	l1.Check("bob", policy) // bob: request 3 (allowed)

	l2 := limiter.NewSlidingWindowCounterWithLimit(storage, 3) // l2: second limiter (SHARED storage)
	result, err := l2.Check("bob", policy) // bob: request 4 via different instance
	if err != nil { // error handling
		t.Fatalf("unexpected error: %v", err) // fail on unexpected error
	}
	if result.Allowed { // bob should be denied (3 of 3 used, tracked via shared storage)
		t.Fatal("expected denied (3 of 3 used), got allowed") // fail: storage not shared properly
	}
}

// TestLeakyBucket_StoragePersistsRecord verifies LeakyBucket writes to storage. // TestLeakyBucket_StoragePersistsRecord: storage write verification
func TestLeakyBucket_StoragePersistsRecord(t *testing.T) { // TestLeakyBucket_StoragePersistsRecord: checks that storage has bob's record
	storage := rate.NewMemoryStorage() // storage: fresh in-memory storage
	policy := limiter.Policy{Limit: 3, Window: 60 * time.Second} // policy: 3 requests per 60 seconds

	l1 := limiter.NewLeakyBucketWithLimit(storage, 3) // l1: limiter instance
	l1.Check("carol", policy) // carol: one request (allowed)

	_, exists, _ := storage.Get("carol") // check: does storage have a record for carol?
	if !exists { // if no record was persisted
		t.Fatal("expected storage to have record for carol") // fail: storage not updated
	}
}

// TestTokenBucket_StoragePersistsRecord verifies TokenBucket writes to storage. // TestTokenBucket_StoragePersistsRecord: storage write verification
func TestTokenBucket_StoragePersistsRecord(t *testing.T) { // TestTokenBucket_StoragePersistsRecord: checks that storage has dave's record
	storage := rate.NewMemoryStorage() // storage: fresh in-memory storage
	policy := limiter.Policy{Limit: 2, Window: 60 * time.Second} // policy: 2 requests per 60 seconds

	l1 := limiter.NewTokenBucketWithLimit(storage, 2) // l1: limiter instance
	l1.Check("dave", policy) // dave: request 1 (allowed)
	l1.Check("dave", policy) // dave: request 2 (allowed)

	_, exists, _ := storage.Get("dave") // check: does storage have a record for dave?
	if !exists { // if no record was persisted
		t.Fatal("expected storage to have record for dave") // fail: storage not updated
	}
}

// TestSlidingWindowLog_StoragePersistence verifies SlidingWindowLog persists state. // TestSlidingWindowLog_StoragePersistence: cross-instance storage sharing
func TestSlidingWindowLog_StoragePersistence(t *testing.T) { // TestSlidingWindowLog_StoragePersistence: 3 requests exhaust limit, 4th denied via different instance
	storage := rate.NewMemoryStorage() // storage: fresh in-memory storage
	policy := limiter.Policy{Limit: 3, Window: 60 * time.Second} // policy: 3 requests per 60 seconds

	l1 := limiter.NewSlidingWindowLogWithLimit(storage, 3) // l1: first limiter instance
	l1.Check("eve", policy) // eve: request 1 (allowed)
	l1.Check("eve", policy) // eve: request 2 (allowed)
	l1.Check("eve", policy) // eve: request 3 (allowed)

	l2 := limiter.NewSlidingWindowLogWithLimit(storage, 3) // l2: second limiter (SHARED storage)
	result, err := l2.Check("eve", policy) // eve: request 4 via different instance
	if err != nil { // error handling
		t.Fatalf("unexpected error: %v", err) // fail on unexpected error
	}
	if result.Allowed { // eve should be denied (3 of 3 used, tracked via shared storage)
		t.Fatal("expected denied (3 of 3 used), got allowed") // fail: storage not shared properly
	}
}

// TestAllFiveAlgorithms_WithStorage verifies all 5 algorithms correctly limit to their configured limit. // TestAllFiveAlgorithms_WithStorage: comprehensive test with shared storage
func TestAllFiveAlgorithms_WithStorage(t *testing.T) { // TestAllFiveAlgorithms_WithStorage: each algorithm allows limit, denies on next
	storage := rate.NewMemoryStorage() // storage: ONE shared storage for ALL algorithms (key isolation by identity)
	policy := limiter.Policy{Limit: 5, Window: 60 * time.Second} // policy: 5 requests per 60 seconds

	// Each algorithm tested with same limit but unique identity (storage key isolation)
	tests := []struct { // tests: table-driven test cases
		name    string // name: algorithm name for error messages
		make    func() limiter.RateLimiter // make: factory function creating a fresh limiter instance
		identity string // identity: unique identity for this test case (storage key)
	}{
		{"fixed_window", func() limiter.RateLimiter { return limiter.NewFixedWindowWithLimit(storage, 5) }, "fw_user"}, // FixedWindow test
		{"sliding_window_counter", func() limiter.RateLimiter { return limiter.NewSlidingWindowCounterWithLimit(storage, 5) }, "swc_user"}, // SlidingWindowCounter test
		{"token_bucket", func() limiter.RateLimiter { return limiter.NewTokenBucketWithLimit(storage, 5) }, "tb_user"}, // TokenBucket test
		{"leaky_bucket", func() limiter.RateLimiter { return limiter.NewLeakyBucketWithLimit(storage, 5) }, "lb_user"}, // LeakyBucket test
		{"sliding_window_log", func() limiter.RateLimiter { return limiter.NewSlidingWindowLogWithLimit(storage, 5) }, "swl_user"}, // SlidingWindowLog test
	}

	for _, tt := range tests { // tt: each test case
		l := tt.make() // l: fresh limiter instance for this algorithm
		for i := 0; i < 5; i++ { // i: 5 allowed requests
			result, err := l.Check(tt.identity, policy) // Check: request with shared storage
			if err != nil { // error handling
				t.Fatalf("%s: unexpected error on check %d: %v", tt.name, i, err) // fail with algorithm name
			}
			if !result.Allowed { // first 5 requests must all be allowed
				t.Fatalf("%s: expected allowed on check %d, got denied", tt.name, i) // fail: incorrectly denied
			}
		}

		// 6th request should be denied (limit reached)
		result, err := l.Check(tt.identity, policy) // Check: 6th request (should be denied)
		if err != nil { // error handling
			t.Fatalf("%s: unexpected error on check 6: %v", tt.name, err) // fail with algorithm name
		}
		if result.Allowed { // 6th request must be denied
			t.Fatalf("%s: expected denied on check 6, got allowed", tt.name) // fail: not properly limited
		}
	}
}

/*
================================================================================
ARCHITECTURAL / ENGINEERING DECISIONS
================================================================================

1. SHARED STORAGE ACROSS INSTANCES
   - All tests create limiter instances that share the same MemoryStorage.
   - Decision: verifies that storage is the source of truth, not the limiter instance.
   - Real-world scenario: multiple server instances share RedisStorage; state must be consistent.

2. KEY ISOLATION PER IDENTITY
   - Each test uses a unique identity ("fw_user", "swc_user", etc.).
   - Decision: prevents cross-test interference within the shared storage map.
   - If all tests used "alice", earlier tests' data would affect later tests.

3. TABLE-DRIVEN TEST (TestAllFiveAlgorithms_WithStorage)
   - All 5 algorithms tested in a single loop with shared storage.
   - Decision: DRY principle. Adding a 6th algorithm requires one new struct entry.
   - Unique identities prevent state contamination between algorithms.

4. INCREMENTAL COUNT VERIFICATION
   - Tests check both "allowed" (5x) and "denied" (1x) for each algorithm.
   - Decision: verifies both sides of the Check() contract (allow when under limit, deny when at limit).
   - Also verifies storage has records (LeakyBucket, TokenBucket tests use storage.Get()).

5. CROSS-INSTANCE STATE TEST (FixedWindow, SlidingWindowCounter, SlidingWindowLog)
   - These 3 tests create l1 and l2 (different instances) with shared storage.
   - l1 exhausts the limit, l2 checks and is denied.
   - Decision: proves that limiter instances delegate to storage, not internal state.
   - This is critical for distributed deployments (multiple process instances).

6. STORAGE PERSISTENCE VERIFICATION (LeakyBucket, TokenBucket)
   - These tests call storage.Get() after Check() to verify data was written.
   - Decision: direct verification of storage contract (not just behavior).
   - Ensures Set() is called during Check() and data survives.

7. POLICY STRUCTURE
   - All tests use Policy{Limit, Window} with 60-second windows.
   - Decision: consistent test conditions. Window duration doesn't matter for these tests
     because no time passes between requests.

8. t.Fatalf vs t.Errorf
   - Tests use t.Fatalf (stops test immediately) not t.Errorf (continues).
   - Decision: if one assertion fails, subsequent assertions would be misleading.
     Fail fast, fix fast.
*/
