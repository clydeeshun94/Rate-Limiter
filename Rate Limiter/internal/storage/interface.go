package storage // storage: persistent storage abstraction for rate limiter

// Record represents a single rate limiting data point stored per identity. // Record: stored data for each identity key
type Record struct { // Record: holds window position, request count, and timestamp history
	WindowStart int64 // WindowStart: Unix timestamp (seconds) marking the start of the current window
	Count       int   // Count: number of requests in the current window
	Timestamps  []int64 // Timestamps: individual request timestamps (used by SlidingWindowLog algorithm)
}

// Storage defines the interface for persistent rate limiting state. // Storage: abstraction for storing/retrieving rate limit records
type Storage interface { // Storage: interface implemented by MemoryStorage, RedisStorage, etc.
	Get(key string) (Record, bool, error) // Get: retrieve record by key; bool=existence, error=storage failure
	Set(key string, record Record) error // Set: persist record; returns error on failure
	Delete(key string) error // Delete: remove record by key; returns error on failure
}

/*
================================================================================
ARCHITECTURAL / ENGINEERING DECISIONS
================================================================================

1. STORAGE ABSTRACTION (interface)
   - Storage is an interface, not a concrete implementation.
   - Decision: allows swapping between in-memory (dev/test) and Redis (production) without changing algorithm code.
   - All 5 rate limiting algorithms use only this interface, never concrete types.

2. RECORD STRUCTURE
   - Record has 3 fields: WindowStart, Count, Timestamps.
   - WindowStart + Count: used by FixedWindow, SlidingWindowCounter, TokenBucket, LeakyBucket (O(1) algorithms).
   - Timestamps: used only by SlidingWindowLog (stores every request time).
   - Decision: a single Record type serves all algorithms, even if they don't use all fields.
   - Memory concern: Timestamps slice can grow large. SlidingWindowLog prunes expired entries on each Check().

3. KEY FORMAT
   - Keys are identity strings (e.g., "create:alice", "redirect:abc123").
   - Decision: key format is determined by callers, not storage. Storage is agnostic to key structure.
   - URL Shortener uses keys like "create:{user_id}" and "redirect:{short_code}".

4. DELETE OPERATION
   - Storage supports Delete for cleanup (e.g., removing expired identity data).
   - Decision: not all algorithms use Delete (most just overwrite via Set). Provided for flexibility.

5. THREAD SAFETY
   - The Storage interface does NOT specify thread safety.
   - MemoryStorage handles its own synchronization (sync.Mutex).
   - RedisStorage uses retry logic for transient failures.
   - Decision: each implementation handles concurrency independently.

6. ERROR HANDLING
    - Set and Delete return errors; Get returns (Record, bool, error).
    - Decision: Get now propagates storage errors so callers can distinguish "not found" from "storage failure".
    - MemoryStorage always returns nil error (in-memory, no I/O failure modes).
    - RedisStorage returns error on network failures or Redis errors.
    - Each caller decides fail-open vs fail-closed based on its needs.
*/
