package limiter // limiter: rate limiting algorithm package

import ( // import: standard library imports
	"sync" // sync: provides Mutex for thread-safe access
	"time" // time: provides time-related functions for window calculations

	rate "rate-limiter/internal/storage" // rate: import storage package as rate for Record type
	pol "rate-limiter/internal/policy" // pol: policy validation package
)

type TokenBucket struct { // TokenBucket: token bucket rate limiter implementation
	storage rate.Storage // storage: persistent storage for rate records
	limit   int          // limit: maximum tokens (burst capacity) allowed
	tokens  map[string]int // tokens: per-identity token count (in-memory cache)
	mu      sync.Mutex  // mu: mutex for thread-safe access to tokens map and limit field
}

func NewTokenBucketWithLimit(storage rate.Storage, limit int) *TokenBucket { // NewTokenBucketWithLimit: constructor, creates a new TokenBucket with given storage and limit
	return &TokenBucket{ // return: return pointer to new TokenBucket instance
		storage: storage, // storage: assign storage implementation
		limit:   limit,   // limit: assign max tokens per window
		tokens:  make(map[string]int), // tokens: initialize empty token map per identity
	}
}

func (tb *TokenBucket) SetLimit(limit int) { // SetLimit: updates the rate limit dynamically (thread-safe)
	tb.mu.Lock() // tb.mu.Lock(): acquire lock to protect limit and tokens fields
	defer tb.mu.Unlock() // defer tb.mu.Unlock(): ensure lock is released when function exits
	tb.limit = limit // tb.limit = limit: update the token capacity
}

func (tb *TokenBucket) Check(identity string, policy Policy) (Result, error) { // Check: main method to check if a request is allowed for the given identity using token bucket algorithm
	if err := pol.Validate(policy.Limit, policy.Window); err != nil {
		return Result{}, err
	}

	tb.mu.Lock() // tb.mu.Lock(): acquire lock to protect token reads/writes and storage operations
	defer tb.mu.Unlock() // defer tb.mu.Unlock(): ensure lock is released after check completes

	now := time.Now().Unix() // now: current Unix timestamp in seconds
	windowSeconds := int64(policy.Window.Seconds()) // windowSeconds: token refill period in seconds
	refillRate := float64(tb.limit) / float64(windowSeconds) // refillRate: tokens added per second (linear refill)

	key := identity // key: use identity string as the token bucket key
	record, exists, err := tb.storage.Get(key) // record, exists, err: retrieve existing record from storage for this identity
	if err != nil {
		return Result{}, err
	}

	if exists && record.WindowStart != 0 { // if record exists and has a valid timestamp (not a new record)
		elapsed := float64(now - record.WindowStart) // elapsed: seconds since last recorded activity
		tb.tokens[key] += int(refillRate * elapsed) // tb.tokens[key]: add tokens based on elapsed time (linear refill)
		if tb.tokens[key] > tb.limit { // if refilled tokens exceed capacity
			tb.tokens[key] = tb.limit // tb.tokens[key] = tb.limit: cap at maximum (burst limit)
		}
	} else if !exists { // if no record exists for this identity (first request)
		tb.tokens[key] = tb.limit // tb.tokens[key] = tb.limit: start with full bucket (initial capacity)
	}

	tb.tokens[key]-- // tb.tokens[key]--: consume one token for this request
	if tb.tokens[key] < 0 { // if no tokens available (request exceeds capacity)
		tb.tokens[key] = 0 // tb.tokens[key] = 0: clamp to zero
		retryAfter := time.Duration((1.0 / refillRate) * float64(time.Second)) // retryAfter: time to wait for one token to refill (1/refillRate seconds)
		resetTime := time.Now().Add(retryAfter) // resetTime: when the next token will be available
		return Result{ // return: deny the request with rate limit info
			Allowed:    false, // Allowed: request denied (no tokens)
			Limit:      tb.limit, // Limit: the configured token capacity
			Remaining:  0, // Remaining: no tokens left
			RetryAfter: retryAfter, // RetryAfter: time until next token available
			ResetTime:  resetTime, // ResetTime: estimated time of next token refill
		}, nil // nil: no error
	}

	tb.storage.Set(key, rate.Record{WindowStart: now, Count: tb.tokens[key]}) // tb.storage.Set: persist remaining token count to storage

	return Result{ // return: allow the request with rate limit info
		Allowed:    true, // Allowed: request permitted (token consumed)
		Limit:      tb.limit, // Limit: the configured token capacity
		Remaining:  tb.tokens[key], // Remaining: tokens left in bucket after consumption
		RetryAfter: 0, // RetryAfter: no wait needed (request allowed)
		ResetTime:  time.Now().Add(time.Duration(windowSeconds) * time.Second), // ResetTime: estimated window expiration
	}, nil // nil: no error
}

/*
================================================================================
ARCHITECTURAL / ENGINEERING DECISIONS
================================================================================

1. TOKEN BUCKET ALGORITHM
   - Token bucket allows bursts up to `limit` (full bucket), then refills at a steady rate.
   - How it works: tokens are added to the bucket at `refillRate` per second. Each request consumes one token. If bucket is empty, request is denied.
   - Pros: Allows controlled bursts (up to full bucket), simple math, O(1) time complexity.
   - Cons: Bucket state stored in-memory (map), not persistent across restarts (relies on storage Set/Get).
   - Best for: APIs where occasional bursts are acceptable (e.g., login endpoints).

2. INITIAL BUCKET STATE (full on first request)
   - Decision: new identities start with a FULL bucket (tb.tokens[key] = tb.limit).
   - Why: allows a burst of `limit` requests immediately. Prevents unfair penalty for new users.
   - Alternative considered: start empty (slow-start). Rejected — too restrictive for most APIs.

3. LINEAR REFILL RATE
   - Refill calculated as refillRate = limit / windowSeconds (tokens per second).
   - Decision: linear refill is the standard token bucket behavior. No exponential or complex scheduling.
   - refillRate is computed once per Check() call. Cached computation not needed due to O(1) cost.

4. IN-MEMORY TOKEN CACHE (map[string]int)
   - Tokens are tracked in-memory (tb.tokens map), while persistent state is in storage.
   - Decision: the map provides fast token balance lookups without hitting storage on every request.
   - Storage is still used for persistence (Set after consumption) and for cross-instance sync.
   - Risk: if the process restarts, in-memory tokens are lost. Recovery: storage provides last known state on next Check().

5. RETRY-AFTER CALCULATION
   - retryAfter = 1.0 / refillRate seconds (time to refill exactly 1 token).
   - Decision: simplest and most conservative retry estimate. Actual refill is continuous; this gives a safe minimum wait.

6. MUTEX LOCKING (sync.Mutex)
   - Single mutex protects both tokens map and limit field.
   - Decision: token consumption is read-modify-write on the map, requiring synchronization.
   - The mutex also covers storage Get/Set to prevent race conditions between concurrent requests.

7. DIFFERENCE FROM LEAKY BUCKET
   - Token bucket allows bursts (up to limit). Requests consume tokens; tokens refill over time.
   - Leaky bucket processes at a fixed rate regardless of burst size.
   - Token bucket is preferred for APIs needing burst tolerance; leaky bucket for strict pacing.
*/
