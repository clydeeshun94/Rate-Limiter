package limiter // limiter: rate limiting algorithm package

import ( // import: standard library imports
	"sync" // sync: provides Mutex for thread-safe access
	"time" // time: provides time-related functions for window calculations

	pol "rate-limiter/internal/policy"   // pol: policy validation package
	rate "rate-limiter/internal/storage" // rate: import storage package as rate for Record type
)

type FixedWindow struct { // FixedWindow: fixed window rate limiter implementation
	storage rate.Storage // storage: persistent storage for rate records
	limit   int          // limit: maximum requests allowed per window
	mu      sync.Mutex   // mu: mutex for thread-safe access to limit field
	clock   Clock
}

func NewFixedWindowWithLimit(storage rate.Storage, limit int) *FixedWindow { // NewFixedWindowWithLimit: constructor, creates a new FixedWindow with given storage and limit
	return &FixedWindow{ // return: return pointer to new FixedWindow instance
		storage: storage, // storage: assign storage implementation
		limit:   limit,   // limit: assign request limit per window
		clock:   RealClock{},
	}
}

func maxRemaining(limit, count int) int {
	remaining := limit - count
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (fw *FixedWindow) SetLimit(limit int) { // SetLimit: updates the rate limit dynamically (thread-safe)
	fw.mu.Lock()         // fw.mu.Lock(): acquire lock to protect limit field from concurrent writes
	defer fw.mu.Unlock() // defer fw.mu.Unlock(): ensure lock is released when function exits
	fw.limit = limit     // fw.limit = limit: update the rate limit value
}

func (fw *FixedWindow) Check(identity string, policy Policy) (Result, error) { // Check: main method to check if a request is allowed for the given identity
	if err := pol.Validate(policy.Limit, policy.Window); err != nil {
		return Result{}, err
	}

	fw.mu.Lock()         // fw.mu.Lock(): acquire lock to protect limit reads and storage operations
	defer fw.mu.Unlock() // defer fw.mu.Unlock(): ensure lock is released after check completes

	now := fw.clock.Now().Unix()                                // now: current Unix timestamp in seconds
	windowStart := now - (now % int64(policy.Window.Seconds())) // windowStart: start of the current time window (truncate to window boundary)

	key := identity // key: use identity string as the storage key (e.g., "create:alice")
	windowSeconds := int64(policy.Window.Seconds())

	if redisStorage, ok := fw.storage.(*rate.RedisStorage); ok {
		allowed, count, err := redisStorage.CheckAndSet(key, fw.limit, windowStart, windowSeconds)
		if err != nil {
			return Result{}, err
		}
		resetTime := time.Unix(windowStart+windowSeconds, 0)
		if !allowed {
			retryAfter := time.Until(resetTime)
			if retryAfter < 0 {
				retryAfter = 0
			}
			return Result{
				Allowed:    false,
				Limit:      fw.limit,
				Remaining:  maxRemaining(fw.limit, count),
				RetryAfter: retryAfter,
				ResetTime:  resetTime,
			}, nil
		}
		return Result{
			Allowed:    true,
			Limit:      fw.limit,
			Remaining:  maxRemaining(fw.limit, count),
			RetryAfter: 0,
			ResetTime:  resetTime,
		}, nil
	}

	record, exists, err := fw.storage.Get(key) // record, exists, err: retrieve existing record from storage for this identity
	if err != nil {
		return Result{}, err
	}

	if !exists || record.WindowStart != windowStart { // if no record exists OR window has rolled over to a new period
		record = rate.Record{ // record: create a fresh record for the new window
			WindowStart: windowStart, // WindowStart: set window start timestamp
			Count:       0,           // Count: reset count to zero for new window
		}
	}

	if record.Count >= fw.limit { // if request count has reached or exceeded the limit
		resetTime := time.Unix(windowStart+int64(policy.Window.Seconds()), 0) // resetTime: when the current window resets (next window start)
		retryAfter := time.Until(resetTime)                                   // retryAfter: duration until reset (for Retry-After header)
		if retryAfter < 0 {                                                   // if retryAfter is negative (clock skew or past reset), clamp to zero
			retryAfter = 0 // retryAfter = 0: no wait time
		}
		return Result{ // return: deny the request with rate limit info
			Allowed:    false,                                // Allowed: request denied
			Limit:      fw.limit,                             // Limit: the configured limit
			Remaining:  maxRemaining(fw.limit, record.Count), // Remaining: how many requests remain this window
			RetryAfter: retryAfter,                           // RetryAfter: seconds until window resets
			ResetTime:  resetTime,                            // ResetTime: when the window resets
		}, nil // nil: no error
	}

	record.Count++ // record.Count++: increment request count (allow the request)
	if err := fw.storage.Set(key, record); err != nil {
		return Result{}, err
	}

	resetTime := time.Unix(windowStart+int64(policy.Window.Seconds()), 0) // resetTime: calculate when the current window resets

	return Result{ // return: allow the request with rate limit info
		Allowed:    true,                    // Allowed: request permitted
		Limit:      fw.limit,                // Limit: the configured limit
		Remaining:  fw.limit - record.Count, // Remaining: remaining requests in current window
		RetryAfter: 0,                       // RetryAfter: no wait needed (request allowed)
		ResetTime:  resetTime,               // ResetTime: when the window resets
	}, nil // nil: no error
}

/*
================================================================================
ARCHITECTURAL / ENGINEERING DECISIONS
================================================================================

1. FIXED WINDOW ALGORITHM
   - Simplest rate limiting algorithm: divides time into fixed windows (e.g., 60 min).
   - Pros: O(1) time complexity for checks, no memory overhead for timestamps, easy to understand.
   - Cons: Burst at window boundaries (e.g., 50 requests at 10:59:59 and 50 at 11:00:01 = 100 in 2 seconds).
   - Used as the default algorithm in this implementation because: predictable, simple, and the spec says "50 per 60 min".

2. MUTEX LOCKING (sync.Mutex)
   - The FixedWindow struct uses a sync.Mutex to protect both the `limit` field and storage operations.
   - Decision: single mutex for both limit and storage — the critical section is short (map lookup + counter increment), so lock contention is minimal.
   - Alternative: separate mutexes for limit vs storage — rejected because it adds complexity with no measurable benefit for short critical sections.

3. STORAGE ABSTRACTION
   - Storage is injected via interface (rate.Storage), not hardcoded.
   - Decision: allows swapping in-memory storage for Redis without changing algorithm logic.
   - This is the Modular Monolith pattern — modules depend on interfaces, not concrete implementations.

4. KEY DESIGN: identity as storage key
   - The identity string (e.g., "create:alice") is the storage key directly.
   - Decision: avoids a separate mapping layer. The identity already uniquely identifies what we're rate limiting.

5. WINDOW START CALCULATION
   - windowStart = now - (now % windowSeconds): truncates timestamp to window boundary.
   - Example: now=3661, window=60 → windowStart=3600 (1 hour mark).
   - This is the standard approach for fixed window alignment and ensures consistent window boundaries across all instances.

6. RESET TIME CALCULATION
   - resetTime = windowStart + windowSeconds: the start of the next window.
   - RetryAfter is calculated as duration until resetTime, clamped to 0 (never negative).
   - Decision: Retry-After header tells the client exactly when to retry, reducing unnecessary retry storms.

7. THREAD SAFETY MODEL
   - The Check() method holds the mutex for its entire duration.
   - Decision: the operation is a simple Get + Compare + Set, which is very fast (microseconds). Holding the lock for the entire operation avoids race conditions without needing more complex patterns like CAS or optimistic locking.
*/
