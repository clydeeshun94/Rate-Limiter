package limiter // limiter: rate limiting algorithm package

import ( // import: standard library imports
	"sync" // sync: provides Mutex for thread-safe access
	"time" // time: provides time-related functions for window calculations

	rate "rate-limiter/internal/storage" // rate: import storage package as rate for Record type
	pol "rate-limiter/internal/policy" // pol: policy validation package
)

type SlidingWindowCounter struct { // SlidingWindowCounter: sliding window counter rate limiter implementation
	storage rate.Storage // storage: persistent storage for rate records
	limit   int          // limit: maximum requests allowed per window
	mu      sync.Mutex  // mu: mutex for thread-safe access to limit field
}

func NewSlidingWindowCounterWithLimit(storage rate.Storage, limit int) *SlidingWindowCounter { // NewSlidingWindowCounterWithLimit: constructor, creates a new SlidingWindowCounter with given storage and limit
	return &SlidingWindowCounter{ // return: return pointer to new SlidingWindowCounter instance
		storage: storage, // storage: assign storage implementation
		limit:   limit,   // limit: assign request limit per window
	}
}

func (sw *SlidingWindowCounter) SetLimit(limit int) { // SetLimit: updates the rate limit dynamically (thread-safe)
	sw.mu.Lock() // sw.mu.Lock(): acquire lock to protect limit field from concurrent writes
	defer sw.mu.Unlock() // defer sw.mu.Unlock(): ensure lock is released when function exits
	sw.limit = limit // sw.limit = limit: update the rate limit value
}

func (sw *SlidingWindowCounter) Check(identity string, policy Policy) (Result, error) { // Check: main method to check if a request is allowed for the given identity using sliding window counter
	if err := pol.Validate(policy.Limit, policy.Window); err != nil {
		return Result{}, err
	}

	sw.mu.Lock() // sw.mu.Lock(): acquire lock to protect limit reads and storage operations
	defer sw.mu.Unlock() // defer sw.mu.Unlock(): ensure lock is released after check completes

	now := time.Now() // now: current time
	nowUnix := now.Unix() // nowUnix: current Unix timestamp in seconds
	windowSeconds := int64(policy.Window.Seconds()) // windowSeconds: duration of the rate limit window in seconds
	windowStart := nowUnix - (nowUnix % windowSeconds) // windowStart: start of the current time window (truncate to window boundary)
	prevWindowStart := windowStart - windowSeconds // prevWindowStart: start of the previous time window

	key := identity // key: use identity string as the storage key (e.g., "create:alice")
	record, exists := sw.storage.Get(key) // record, exists: retrieve existing record from storage for this identity

	var currentCount int // currentCount: number of requests in the current window, calculated by blending windows
	if !exists { // if no record exists for this identity
		currentCount = 0 // currentCount = 0: no previous requests, start fresh
	} else if record.WindowStart == windowStart { // if record is from the current window
		currentCount = record.Count // currentCount = record.Count: use stored count directly (same window)
	} else if record.WindowStart == prevWindowStart { // if record is from the previous window (transition period)
		ratio := float64(nowUnix-prevWindowStart) / float64(windowSeconds) // ratio: fraction of the previous window that has elapsed (0.0 to 1.0)
		currentCount = int(float64(record.Count) * (1.0 - ratio)) // currentCount: decay previous count by the elapsed ratio (weighted blend between windows)
		if currentCount < 0 { // if calculation results in negative (shouldn't happen, but safety check)
			currentCount = 0 // currentCount = 0: clamp to zero
		}
	} else { // if record is from an older window (more than one window ago)
		currentCount = 0 // currentCount = 0: reset completely, too old to be relevant
	}

	if currentCount >= sw.limit { // if current request count has reached or exceeded the limit
		resetTime := time.Unix(windowStart+windowSeconds, 0) // resetTime: when the current window resets (next window start)
		retryAfter := time.Until(resetTime) // retryAfter: duration until reset (for Retry-After header)
		if retryAfter < 0 { // if retryAfter is negative (clock skew or past reset), clamp to zero
			retryAfter = 0 // retryAfter = 0: no wait time
		}
		return Result{ // return: deny the request with rate limit info
			Allowed:    false, // Allowed: request denied
			Limit:      sw.limit, // Limit: the configured limit
			Remaining:  sw.limit - currentCount, // Remaining: how many requests are left in current window
			RetryAfter: retryAfter, // RetryAfter: seconds until window resets
			ResetTime:  resetTime, // ResetTime: when the window resets
		}, nil // nil: no error
	}

	currentCount++ // currentCount++: increment request count (allow the request)
	sw.storage.Set(key, rate.Record{WindowStart: windowStart, Count: currentCount}) // sw.storage.Set: persist updated record to storage with current window

	resetTime := time.Unix(windowStart+windowSeconds, 0) // resetTime: calculate when the current window resets
	return Result{ // return: allow the request with rate limit info
		Allowed:    true, // Allowed: request permitted
		Limit:      sw.limit, // Limit: the configured limit
		Remaining:  sw.limit - currentCount, // Remaining: remaining requests in current window
		RetryAfter: 0, // RetryAfter: no wait needed (request allowed)
		ResetTime:  resetTime, // ResetTime: when the window resets
	}, nil // nil: no error
}

/*
================================================================================
ARCHITECTURAL / ENGINEERING DECISIONS
================================================================================

1. SLIDING WINDOW COUNTER ALGORITHM
   - Improvement over fixed window: smooths boundary bursts by blending counts from adjacent windows.
   - How it works: when checking from a new window, it calculates a weighted count based on how much of the previous window has elapsed.
   - Formula: currentCount = prevCount * (1 - elapsedRatio). At window boundary, prevCount is 100% weighted. One second before new window, prevCount is ~0% weighted.
   - Pros: No boundary burst (smooth transition), O(1) time complexity, low memory (just a counter).
   - Cons: Slight inaccuracy due to linear interpolation (assumes uniform request distribution).
   - Best for: General-purpose rate limiting where smooth behavior is needed without storing timestamps.

2. WINDOW BLENDING (prevWindow logic)
   - Decision: when the current window is different from the stored record, check if it's the IMMEDIATELY previous window.
   - If yes: blend counts using time ratio. This provides smooth rate limiting across window boundaries.
   - If no (skipped windows): reset to zero. Old data is irrelevant.
   - Why: avoids unbounded memory growth from storing counts for many historical windows.

3. MUTEX LOCKING (sync.Mutex)
   - Same strategy as FixedWindow: single mutex protects limit and storage.
   - Decision: the Check() operation is a simple Get + Compare + Set, very fast. No need for complex concurrency patterns.

4. STORAGE ABSTRACTION
   - Storage injected via interface (rate.Storage), same as FixedWindow.
   - Decision: consistent abstraction across all algorithms. Each algorithm only cares about Record{WindowStart, Count}, not storage implementation.

5. LINEAR DECAY ASSUMPTION
   - The ratio calculation assumes requests are uniformly distributed across the window.
   - Decision: this is a reasonable approximation for most use cases. More accurate approaches (sliding window log) store timestamps at higher memory cost.
   - Tradeoff: accuracy vs memory. Sliding window counter chooses low memory with acceptable accuracy.

6. DIFFERENCE FROM FIXED WINDOW
   - Fixed window resets count at window boundary (hard reset).
   - Sliding window counter DECAYS count gradually across boundaries (smooth reset).
   - This eliminates the "burst at boundary" problem of fixed windows.
*/
