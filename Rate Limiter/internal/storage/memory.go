package storage // storage: in-memory storage implementation for rate limiter

import "sync" // sync: provides Mutex for thread-safe map access

// MemoryStorage is an in-memory implementation of the Storage interface. // MemoryStorage: stores rate records in a Go map, suitable for single-process use
type MemoryStorage struct { // MemoryStorage: thread-safe map-based storage
	mu      sync.Mutex // mu: protects storage map from concurrent access
	storage map[string]Record // storage: the actual data store (identity → Record)
}

// NewMemoryStorage creates a new MemoryStorage instance with an empty map. // NewMemoryStorage: constructor, initializes empty storage map
func NewMemoryStorage() *MemoryStorage { // NewMemoryStorage: returns pointer to initialized MemoryStorage
	return &MemoryStorage{ // return: new instance
		storage: make(map[string]Record), // storage: initialize empty map
	}
}

// Get retrieves a record by key from memory. // Get: thread-safe lookup; returns record, existence flag, and error (always nil for in-memory)
func (s *MemoryStorage) Get(key string) (Record, bool, error) { // Get: O(1) map lookup with mutex protection
	s.mu.Lock() // s.mu.Lock(): acquire lock for read access
	defer s.mu.Unlock() // defer s.mu.Unlock(): ensure lock released after read
	record, exists := s.storage[key] // record, exists: map lookup (Go idiom for map access)
	return record, exists, nil // return: record value, whether key was found, nil error
}

// Set persists a record to memory. // Set: thread-safe write; stores record for given key
func (s *MemoryStorage) Set(key string, record Record) error { // Set: O(1) map insert/update with mutex protection
	s.mu.Lock() // s.mu.Lock(): acquire lock for write access
	defer s.mu.Unlock() // defer s.mu.Unlock(): ensure lock released after write
	s.storage[key] = record // s.storage[key] = record: insert or update record in map
	return nil // nil: always succeeds (in-memory, no I/O failure modes)
}

// Delete removes a record from memory by key. // Delete: thread-safe removal; no-op if key doesn't exist
func (s *MemoryStorage) Delete(key string) error { // Delete: O(1) map deletion with mutex protection
	s.mu.Lock() // s.mu.Lock(): acquire lock for write access
	defer s.mu.Unlock() // defer s.mu.Unlock(): ensure lock released after write
	delete(s.storage, key) // delete(s.storage, key): remove key from map (safe if key missing)
	return nil // nil: always succeeds (in-memory, no I/O failure modes)
}

/*
================================================================================
ARCHITECTURAL / ENGINEERING DECISIONS
================================================================================

1. IN-MEMORY STORAGE
   - Simplest storage implementation; data lives only in process memory.
   - Decision: ideal for development, testing, and single-instance deployments.
   - Limitation: data is lost on process restart. Production should use RedisStorage.
   - For tests: each test creates its own MemoryStorage, so no cross-test contamination.

2. SYNC.MUTEX FOR CONCURRENCY
   - All methods lock the entire map for the duration of the operation.
   - Decision: Go maps are not safe for concurrent access. sync.Mutex is the simplest solution.
   - Alternative considered: sync.RWMutex (allows concurrent reads).
   - Why Mutex over RWMutex: in rate limiting, writes happen on every request (each Check() calls Set()).
     RWMutex would be beneficial only if reads dominate writes. In this case, both reads and writes
     happen in Check(), so Mutex is sufficient and avoids RWMutex starvation risk.

3. O(1) OPERATIONS
   - Get, Set, Delete are all O(1) map operations.
   - Decision: optimal for high-throughput rate limiting where latency matters.
   - No indexing, no iteration, no serialization overhead (unlike RedisStorage).

4. NO EXPIRY / TTL
   - MemoryStorage has no built-in expiry. Stale records accumulate indefinitely.
   - Decision: callers are responsible for cleanup (or use Delete when identity is no longer relevant).
   - Alternative: could use a background goroutine with time.AfterFunc for expiry, but adds complexity.
   - Why no expiry: the rate limiting algorithms themselves handle staleness (e.g., sliding window
     filters by cutoff). Extra cleanup is nice-to-have, not required.

5. RETURNING NIL ERROR
   - Set and Delete always return nil error.
   - Decision: simplifies callers — no error checking needed for in-memory operations.
   - Consistent interface with RedisStorage (which can fail). Implementations choose their own error model.

6. TEST USAGE
   - All integration tests (tests/integration/limiter_test.go) use NewMemoryStorage().
   - Decision: tests don't need persistence; fast in-memory storage is perfect.
   - Each test function creates its own MemoryStorage for isolation.
*/
