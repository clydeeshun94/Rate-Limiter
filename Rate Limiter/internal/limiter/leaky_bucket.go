package limiter // limiter: rate limiting algorithm package

import ( // import: standard library imports
	"sync" // sync: provides Mutex for thread-safe access
	"time" // time: provides time-related functions for window calculations

	pol "rate-limiter/internal/policy"   // pol: policy validation package
	rate "rate-limiter/internal/storage" // rate: import storage package as rate for Record type
)

type LeakyBucket struct { // LeakyBucket: leaky bucket rate limiter implementation (processes requests at a constant rate)
	storage rate.Storage // storage: persistent storage for rate records
	limit   int          // limit: maximum water level (request queue capacity)
	mu      sync.Mutex   // mu: mutex for thread-safe access to limit field
	clock   Clock
}

func NewLeakyBucketWithLimit(storage rate.Storage, limit int) *LeakyBucket { // NewLeakyBucketWithLimit: constructor, creates a new LeakyBucket with given storage and limit
	return &LeakyBucket{ // return: return pointer to new LeakyBucket instance
		storage: storage, // storage: assign storage implementation
		limit:   limit,   // limit: assign max water level (queue capacity)
		clock:   RealClock{},
	}
}

func (lb *LeakyBucket) SetLimit(limit int) { // SetLimit: updates the rate limit dynamically (thread-safe)
	lb.mu.Lock()         // lb.mu.Lock(): acquire lock to protect limit field
	defer lb.mu.Unlock() // defer lb.mu.Unlock(): ensure lock is released when function exits
	lb.limit = limit     // lb.limit = limit: update the water capacity
}

func (lb *LeakyBucket) Check(identity string, policy Policy) (Result, error) { // Check: main method to check if a request is allowed for the given identity using leaky bucket algorithm
	if err := pol.Validate(policy.Limit, policy.Window); err != nil {
		return Result{}, err
	}

	lb.mu.Lock()         // lb.mu.Lock(): acquire lock to protect limit reads and storage operations
	defer lb.mu.Unlock() // defer lb.mu.Unlock(): ensure lock is released after check completes

	now := lb.clock.Now().Unix()                               // now: current Unix timestamp in seconds
	windowSeconds := int64(policy.Window.Seconds())            // windowSeconds: time window for leak rate calculation
	leakRate := float64(policy.Limit) / float64(windowSeconds) // leakRate: requests drained per second (constant outflow rate)

	key := identity // key: use identity string as the bucket key
	if atomicStorage, ok := lb.storage.(rate.AtomicCounterStorage); ok {
		allowed, remaining, err := atomicStorage.CheckAndSetLeakyBucket(key, lb.limit, windowSeconds)
		if err != nil {
			return Result{}, err
		}
		if !allowed {
			retryAfter := time.Duration(float64(time.Second) / leakRate)
			return Result{Allowed: false, Limit: lb.limit, Remaining: 0, RetryAfter: retryAfter, ResetTime: lb.clock.Now().Add(retryAfter)}, nil
		}
		return Result{Allowed: true, Limit: lb.limit, Remaining: remaining, RetryAfter: 0, ResetTime: lb.clock.Now().Add(time.Duration(windowSeconds) * time.Second)}, nil
	}
	record, exists, err := lb.storage.Get(key) // record, exists, err: retrieve existing record from storage for this identity
	if err != nil {
		return Result{}, err
	}

	var water float64                      // water: current water level
	if exists && record.WindowStart != 0 { // if record exists and has a valid timestamp
		elapsed := float64(now - record.WindowStart)     // elapsed: seconds since last recorded activity
		water = float64(record.Count) - leakRate*elapsed // water: drain based on elapsed time
		if water < 0 {                                   // if water drops below zero
			water = 0 // water = 0: clamp to zero (empty bucket)
		}
	} else { // if no record (new identity)
		water = 0 // water = 0: start with empty bucket
	}

	if water+1 > float64(lb.limit) { // if adding this request would exceed bucket capacity
		retryAfter := time.Duration(float64(time.Second) * (water + 1 - float64(lb.limit)) / leakRate) // retryAfter: time until enough water drains to fit one more request
		if retryAfter < 0 {                                                                            // if retryAfter is negative (overflow edge case), clamp to zero
			retryAfter = 0 // retryAfter = 0: no wait time
		}
		return Result{ // return: deny the request with rate limit info
			Allowed:    false,                          // Allowed: request denied (bucket overflow)
			Limit:      lb.limit,                       // Limit: the configured bucket capacity
			Remaining:  lb.limit - int(water),          // Remaining: space left in bucket
			RetryAfter: retryAfter,                     // RetryAfter: time until bucket drains enough space
			ResetTime:  lb.clock.Now().Add(retryAfter), // ResetTime: estimated time when request can be processed
		}, nil // nil: no error
	}

	newWater := water + 1 // newWater: add water for this request (enqueue)
	if err := lb.storage.Set(key, rate.Record{WindowStart: now, Count: int(newWater)}); err != nil {
		return Result{}, err
	}

	return Result{ // return: allow the request with rate limit info
		Allowed:    true,                                                           // Allowed: request permitted (enqueued)
		Limit:      lb.limit,                                                       // Limit: the configured bucket capacity
		Remaining:  lb.limit - int(newWater),                                       // Remaining: space left in bucket after adding request
		RetryAfter: 0,                                                              // RetryAfter: no wait needed (request allowed)
		ResetTime:  lb.clock.Now().Add(time.Duration(windowSeconds) * time.Second), // ResetTime: estimated window expiration
	}, nil // nil: no error
}

/*
================================================================================
ARCHITECTURAL / ENGINEERING DECISIONS
================================================================================

1. LEAKY BUCKET ALGORITHM
   - Models requests as water in a bucket: water leaks out at a constant rate, requests pour in.
   - How it works: water drains continuously at `leakRate` per second. Each request adds water. If bucket overflows, request is denied.
   - Pros: Smooths traffic to a constant rate (ideal for rate-limited APIs), simple model, O(1) time complexity.
   - Cons: No burst tolerance (unlike token bucket), water level in-memory only.
   - Best for: Strict rate limiting where consistent processing speed is required (e.g., payment APIs).

2. WATER LEVEL COMPUTATION
   - Water level computed from Redis record.Count minus elapsed-drain.
   - Decision: removes in-memory water map, ensuring distributed correctness.
   - Tradeoff: requires computing water level from elapsed time on every request, but consistent across instances.

3. NO INITIAL WATER (new identities)
   - Decision: new identities start with zero water (empty bucket).
   - Why: fair — no free burst like token bucket. Requests queue up and process at leak rate.
   - Contrast with TokenBucket: token bucket gives full bucket on first request; leaky bucket gives empty bucket.

4. RETRY-AFTER CALCULATION
   - retryAfter = (water + 1 - limit) / leakRate seconds (time until enough water drains).
   - Decision: calculates exact wait time until there is space for one more request in the bucket.
   - This is more accurate than fixed Retry-After because it accounts for current water level.

5. REDIS AS SOURCE OF TRUTH
   - Water level is computed from Redis record.Count + elapsed time drain.
   - Decision: removes in-memory water cache, ensuring distributed correctness.

6. CONSTANT RATE PROCESSING
   - Unlike token bucket, leaky bucket does not allow bursts even if capacity is available.
   - Decision: this is the defining characteristic of leaky bucket. The "water level" represents queue depth.
   - Tradeoff: fairness vs responsiveness. Leaky bucket is fairer but less responsive to legitimate bursts.

7. MUTEX LOCKING (sync.Mutex)
   - Single mutex protects limit field.
   - Decision: no in-memory state to protect. Storage operations are handled by storage layer.

8. DIFFERENCE FROM TOKEN BUCKET
   - Token bucket: allows bursts (up to limit), tokens refill over time. Good for burst-tolerant APIs.
   - Leaky bucket: no bursts, water drains at constant rate. Good for strict rate enforcement.
   - Choice depends on use case: burst tolerance (token bucket) vs smooth processing (leaky bucket).
*/
