package limiter // limiter: rate limiting algorithm package

import ( // import: standard library imports
	"sync" // sync: provides Mutex for thread-safe access
	"time" // time: provides time-related functions for window calculations

	rate "rate-limiter/internal/storage" // rate: import storage package as rate for Record type
	pol "rate-limiter/internal/policy" // pol: policy validation package
)

type SlidingWindowLog struct { // SlidingWindowLog: sliding window log rate limiter implementation (most accurate, stores all timestamps)
	storage rate.Storage // storage: persistent storage for rate records
	limit   int          // limit: maximum requests allowed per window
	mu      sync.Mutex  // mu: mutex for thread-safe access to limit field
}

func NewSlidingWindowLogWithLimit(storage rate.Storage, limit int) *SlidingWindowLog { // NewSlidingWindowLogWithLimit: constructor, creates a new SlidingWindowLog with given storage and limit
	return &SlidingWindowLog{ // return: return pointer to new SlidingWindowLog instance
		storage: storage, // storage: assign storage implementation
		limit:   limit,   // limit: assign request limit per window
	}
}

func (swl *SlidingWindowLog) SetLimit(limit int) { // SetLimit: updates the rate limit dynamically (thread-safe)
	swl.mu.Lock() // swl.mu.Lock(): acquire lock to protect limit field from concurrent writes
	defer swl.mu.Unlock() // defer swl.mu.Unlock(): ensure lock is released when function exits
	swl.limit = limit // swl.limit = limit: update the rate limit value
}

func (swl *SlidingWindowLog) Check(identity string, policy Policy) (Result, error) { // Check: main method to check if a request is allowed for the given identity using sliding window log
	if err := pol.Validate(policy.Limit, policy.Window); err != nil {
		return Result{}, err
	}

	swl.mu.Lock() // swl.mu.Lock(): acquire lock to protect limit reads and storage operations
	defer swl.mu.Unlock() // defer swl.mu.Unlock(): ensure lock is released after check completes

	now := time.Now().Unix() // now: current Unix timestamp in seconds
	windowSeconds := int64(policy.Window.Seconds()) // windowSeconds: duration of the sliding window in seconds
	cutoff := now - windowSeconds // cutoff: oldest timestamp that is still within the window (everything before cutoff is expired)

	key := identity // key: use identity string as the storage key
	record, exists := swl.storage.Get(key) // record, exists: retrieve existing record from storage for this identity

	if !exists { // if no record exists for this identity
		record = rate.Record{WindowStart: now, Timestamps: []int64{}} // record: create new empty record with current window start
	}

	var valid []int64 // valid: slice to hold timestamps that fall within the current window
	for _, ts := range record.Timestamps { // iterate over all stored timestamps for this identity
		if ts >= cutoff { // if timestamp is within the current window (not expired)
			valid = append(valid, ts) // valid: keep this timestamp (still valid)
		}
	}

	if len(valid) >= swl.limit { // if valid request count has reached or exceeded the limit
		oldest := valid[0] // oldest: earliest valid timestamp in the window
		retryAfter := time.Duration(oldest+windowSeconds-now) * time.Second // retryAfter: time until the oldest request expires and space opens up
		if retryAfter < 0 { // if retryAfter is negative (edge case with clock skew), clamp to zero
			retryAfter = 0 // retryAfter = 0: no wait time
		}
		return Result{ // return: deny the request with rate limit info
			Allowed:    false, // Allowed: request denied (window full)
			Limit:      swl.limit, // Limit: the configured limit
			Remaining:  swl.limit - len(valid), // Remaining: slots left before hitting limit
			RetryAfter: retryAfter, // RetryAfter: time until oldest request expires
			ResetTime:  time.Unix(oldest+windowSeconds, 0), // ResetTime: when the oldest request exits the window
		}, nil // nil: no error
	}

	valid = append(valid, now) // valid: add current request timestamp to the log
	record.Timestamps = valid // record.Timestamps: update timestamps in record
	record.Count = len(valid) // record.Count: update count to match timestamps length
	record.WindowStart = now // record.WindowStart: update window start to current time
	swl.storage.Set(key, record) // swl.storage.Set: persist updated record (all timestamps) to storage

	return Result{ // return: allow the request with rate limit info
		Allowed:    true, // Allowed: request permitted (timestamp logged)
		Limit:      swl.limit, // Limit: the configured limit
		Remaining:  swl.limit - len(valid), // Remaining: slots remaining in window
		RetryAfter: 0, // RetryAfter: no wait needed (request allowed)
		ResetTime:  time.Unix(now+windowSeconds, 0), // ResetTime: end of current window
	}, nil // nil: no error
}

/*
================================================================================
ARCHITECTURAL / ENGINEERING DECISIONS
================================================================================

1. SLIDING WINDOW LOG ALGORITHM
   - Most accurate rate limiting algorithm: stores EVERY request timestamp.
   - How it works: each request logs its timestamp. On check, filter timestamps within the window, count them.
   - Pros: Perfect accuracy, no interpolation errors, no boundary bursts (inherently smooth), no decay assumptions.
   - Cons: High memory usage (stores one timestamp per request), slower than counter-based approaches (O(n) filter).
   - Best for: High-value endpoints where accuracy is critical (e.g., financial transactions, security-sensitive APIs).

2. TIMESTAMP STORAGE (record.Timestamps []int64)
   - All request timestamps stored in storage, not just a count.
   - Decision: enables precise calculation of which requests are within/outside the window.
   - Memory concern: one int64 (8 bytes) per request. At 1000 req/sec with 60-second window = ~480KB per identity.
   - Mitigation: expired timestamps are pruned on every Check() call (the `valid` filtering step).

3. WINDOW FILTERING (cutoff-based pruning)
   - Each Check() filters timestamps: keep only those >= cutoff (now - windowSeconds).
   - Decision: pruning happens on every check, ensuring storage doesn't grow unboundedly.
   - Tradeoff: O(n) filter on each check vs O(1) for counter-based approaches. For high-traffic identities, this could be slow.
   - Optimization potential: could use sorted timestamps + binary search for O(log n) filtering. Not implemented for simplicity.

4. RETRY-AFTER BASED ON OLDEST REQUEST
   - retryAfter = (oldest_valid_timestamp + windowSeconds - now) seconds.
   - Decision: this is the most accurate Retry-After — it tells the client exactly when the next slot will open.
   - Contrast with FixedWindow: fixed window Retry-After is time until window reset, which may be longer than needed.

5. NO IN-MEMORY CACHE
   - Unlike other algorithms (TokenBucket, LeakyBucket), SlidingWindowLog has no in-memory map for state.
   - Decision: all state lives in storage (Timestamps slice). This avoids memory leaks from unbounded maps.
   - Tradeoff: every Check() requires a storage Get() and Set(). Slower but more memory-safe.
   - Risk: high-traffic identities cause storage load (Get + filter + Set on every request).

6. MUTEX LOCKING (sync.Mutex)
   - Single mutex protects limit field only (no in-memory state to protect).
   - Decision: storage operations (Get/Set) are not covered by this mutex. In production, storage layer should handle its own concurrency.
   - Note: for in-memory storage implementations, the storage itself would need synchronization.

7. ACCURACY GUARANTEE
   - SlidingWindowLog is the ONLY algorithm that provides exact request count within the window.
   - Decision: use this when accuracy matters more than performance. All other algorithms approximate.
   - Example: TokenBucket approximates refill with linear math; SlidingWindowLog counts actual requests.
*/
