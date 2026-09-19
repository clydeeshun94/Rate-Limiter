package limiter // limiter: rate limiting algorithm package

import ( // import: standard library imports
	"sync" // sync: provides Mutex for thread-safe access
	"time" // time: provides time-related functions for window calculations

	rate "rate-limiter/internal/storage" // rate: import storage package as rate for Record type
)

type LeakyBucket struct { // LeakyBucket: leaky bucket rate limiter implementation (processes requests at a constant rate)
	storage  rate.Storage // storage: persistent storage for rate records
	limit    int          // limit: maximum water level (request queue capacity)
	water    map[string]int // water: per-identity current water level (in-memory cache)
	mu       sync.Mutex  // mu: mutex for thread-safe access to water map and limit field
}

func NewLeakyBucketWithLimit(storage rate.Storage, limit int) *LeakyBucket { // NewLeakyBucketWithLimit: constructor, creates a new LeakyBucket with given storage and limit
	return &LeakyBucket{ // return: return pointer to new LeakyBucket instance
		storage: storage, // storage: assign storage implementation
		limit:   limit,   // limit: assign max water level (queue capacity)
		water:   make(map[string]int), // water: initialize empty water map per identity
	}
}

func (lb *LeakyBucket) SetLimit(limit int) { // SetLimit: updates the rate limit dynamically (thread-safe)
	lb.mu.Lock() // lb.mu.Lock(): acquire lock to protect limit and water fields
	defer lb.mu.Unlock() // defer lb.mu.Unlock(): ensure lock is released when function exits
	lb.limit = limit // lb.limit = limit: update the water capacity
}

func (lb *LeakyBucket) Check(identity string, policy Policy) (Result, error) { // Check: main method to check if a request is allowed for the given identity using leaky bucket algorithm
	lb.mu.Lock() // lb.mu.Lock(): acquire lock to protect water reads/writes and storage operations
	defer lb.mu.Unlock() // defer lb.mu.Unlock(): ensure lock is released after check completes

	now := time.Now().Unix() // now: current Unix timestamp in seconds
	windowSeconds := int64(policy.Window.Seconds()) // windowSeconds: time window for leak rate calculation
	leakRate := float64(policy.Limit) / float64(windowSeconds) // leakRate: requests drained per second (constant outflow rate)

	key := identity // key: use identity string as the bucket key
	record, exists := lb.storage.Get(key) // record, exists: retrieve existing record from storage for this identity

	if exists { // if a record exists for this identity
		elapsed := float64(now - record.WindowStart) // elapsed: seconds since last recorded activity
		lb.water[key] -= int(leakRate * elapsed) // lb.water[key]: drain water based on elapsed time (leak out)
		if lb.water[key] < 0 { // if water drops below zero (all leaked)
			lb.water[key] = 0 // lb.water[key] = 0: clamp to zero (empty bucket)
		}
	}

	if lb.water[key]+1 > lb.limit { // if adding this request would exceed bucket capacity
		retryAfter := time.Duration(float64(time.Second) * (float64(lb.water[key]+1-lb.limit) / leakRate)) // retryAfter: time until enough water drains to fit one more request
		if retryAfter < 0 { // if retryAfter is negative (overflow edge case), clamp to zero
			retryAfter = 0 // retryAfter = 0: no wait time
		}
		return Result{ // return: deny the request with rate limit info
			Allowed:    false, // Allowed: request denied (bucket overflow)
			Limit:      lb.limit, // Limit: the configured bucket capacity
			Remaining:  lb.limit - lb.water[key], // Remaining: space left in bucket
			RetryAfter: retryAfter, // RetryAfter: time until bucket drains enough space
			ResetTime:  time.Now().Add(retryAfter), // ResetTime: estimated time when request can be processed
		}, nil // nil: no error
	}

	lb.water[key]++ // lb.water[key]++: add water for this request (enqueue)
	lb.storage.Set(key, rate.Record{WindowStart: now, Count: lb.water[key]}) // lb.storage.Set: persist water level to storage

	return Result{ // return: allow the request with rate limit info
		Allowed:    true, // Allowed: request permitted (enqueued)
		Limit:      lb.limit, // Limit: the configured bucket capacity
		Remaining:  lb.limit - lb.water[key], // Remaining: space left in bucket after adding request
		RetryAfter: 0, // RetryAfter: no wait needed (request allowed)
		ResetTime:  time.Now().Add(time.Duration(windowSeconds) * time.Second), // ResetTime: estimated window expiration
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

2. WATER LEVEL IN-MEMORY (map[string]int)
   - Water level tracked in-memory (lb.water map), persistent state in storage.
   - Decision: map provides O(1) water level access; storage provides durability.
   - Risk: process restart loses water levels. Mitigated by storage recovery on next Check().
   - Note: the storage only stores Count and WindowStart — the "water" is a derived value computed from elapsed time.

3. LEAK RATE CALCULATION (leakRate = limit / windowSeconds)
   - Leak rate derived from policy.Limit divided by window duration.
   - Decision: leak rate is proportional to the configured limit, ensuring that higher limits have proportionally faster draining.
   - This ensures the bucket can handle the configured load over the window period.

4. NO INITIAL WATER (new identities)
   - Decision: new identities start with zero water (empty bucket).
   - Why: fair — no free burst like token bucket. Requests queue up and process at leak rate.
   - Contrast with TokenBucket: token bucket gives full bucket on first request; leaky bucket gives empty bucket.

5. RETRY-AFTER CALCULATION
   - retryAfter = (water + 1 - limit) / leakRate seconds (time until enough water drains).
   - Decision: calculates exact wait time until there is space for one more request in the bucket.
   - This is more accurate than fixed Retry-After because it accounts for current water level.

6. CONSTANT RATE PROCESSING
   - Unlike token bucket, leaky bucket does not allow bursts even if capacity is available.
   - Decision: this is the defining characteristic of leaky bucket. The "water level" represents queue depth.
   - Tradeoff: fairness vs responsiveness. Leaky bucket is fairer but less responsive to legitimate bursts.

7. MUTEX LOCKING (sync.Mutex)
   - Single mutex protects water map and limit field.
   - Decision: water level updates (read-modify-write) require synchronization.
   - Storage operations covered by same lock to prevent inconsistent reads.

8. DIFFERENCE FROM TOKEN BUCKET
   - Token bucket: allows bursts (up to limit), tokens refill over time. Good for burst-tolerant APIs.
   - Leaky bucket: no bursts, water drains at constant rate. Good for strict rate enforcement.
   - Choice depends on use case: burst tolerance (token bucket) vs smooth processing (leaky bucket).
*/
