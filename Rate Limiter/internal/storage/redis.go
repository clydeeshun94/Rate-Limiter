package storage // storage: Redis-backed storage implementation for rate limiter

import ( // import: standard library and Redis client imports
	"context" // context: for cancellation and timeouts on Redis operations
	"encoding/json" // json: serialize/deserialize Record structs to/from Redis values
	"time" // time: durations for TTL and retry delays

	"github.com/go-redis/redis/v8" // redis: official Go Redis client
)

const ( // const: package-level configuration for Redis operations
	maxRetries = 3 // maxRetries: maximum number of retry attempts for transient failures
	retryDelay = 100 * time.Millisecond // retryDelay: wait time between retry attempts
)

// RedisStorage is a Redis-backed implementation of the Storage interface. // RedisStorage: distributed storage for production deployments
type RedisStorage struct { // RedisStorage: holds Redis client, context, and default TTL
	client *redis.Client // client: Redis client connection
	ctx    context.Context // ctx: base context for all Redis operations
	ttl    time.Duration // ttl: time-to-live for stored keys (auto-expire stale records)
}

// NewRedisStorage creates a new RedisStorage instance. // NewRedisStorage: constructor, configures Redis client with connection parameters
func NewRedisStorage(addr string, password string, db int, ttl time.Duration) *RedisStorage { // NewRedisStorage: returns pointer to configured RedisStorage
	return &RedisStorage{ // return: new instance
		client: redis.NewClient(&redis.Options{ // client: configure Redis connection
			Addr:     addr, // Addr: Redis server address (host:port)
			Password: password, // Password: Redis auth password (empty if none)
			DB:       db, // DB: Redis database number (0-15)
		}),
		ctx: context.Background(), // ctx: background context (no cancellation)
		ttl: ttl, // ttl: default TTL for all keys
	}
}

// retry executes a function with automatic retry on transient failures. // retry: handles network blips and temporary Redis unavailability
func (r *RedisStorage) retry(fn func() error) error { // retry: returns first nil result or last error after maxRetries attempts
	var err error // err: stores the last error encountered
	for i := 0; i < maxRetries; i++ { // i: attempt counter (0 to maxRetries-1)
		err = fn() // err: execute the operation
		if err == nil { // if operation succeeded
			return nil // return nil: success, no retry needed
		}
		if i < maxRetries-1 { // if not the last attempt
			time.Sleep(retryDelay) // wait before retrying (avoids hammering Redis)
		}
	}
	return err // return: last error after all retries exhausted
}

// Get retrieves a record from Redis by key. // Get: deserializes JSON record from Redis; handles missing keys
func (r *RedisStorage) Get(key string) (Record, bool) { // Get: thread-safe Redis GET with retry and JSON deserialization
	var record Record // record: deserialized result
	var found bool // found: whether key exists in Redis
	err := r.retry(func() error { // err: execute GET with retry logic
		val, err := r.client.Get(r.ctx, key).Result() // val: raw string value from Redis
		if err == redis.Nil { // if key doesn't exist
			found = false // found = false: key not found
			return nil // return nil: success (key just doesn't exist)
		}
		if err != nil { // if other Redis error
			return err // return err: will trigger retry
		}
		var rec Record // rec: temporary variable for JSON unmarshal target
		if err := json.Unmarshal([]byte(val), &rec); err != nil { // if JSON parse fails
			return err // return err: will trigger retry (or fail permanently)
		}
		record = rec // record = rec: assign deserialized record
		found = true // found = true: key exists and was parsed
		return nil // return nil: success
	})
	if err != nil { // if retry exhausted or permanent failure
		return Record{}, false // return empty record, false: caller treats as "not found"
	}
	return record, found // return: deserialized record and existence flag
}

// Set persists a record to Redis with TTL. // Set: serializes Record to JSON and stores in Redis with auto-expiry
func (r *RedisStorage) Set(key string, record Record) error { // Set: JSON marshal + Redis SET with retry and TTL
	data, err := json.Marshal(record) // data: JSON-encoded record bytes
	if err != nil { // if JSON marshal fails (unlikely for simple struct)
		return err // return err: no point retrying serialization failure
	}
	return r.retry(func() error { // return: execute SET with retry
		return r.client.Set(r.ctx, key, data, r.ttl).Err() // Set key to JSON data with TTL; Err() returns error (not *Result)
	})
}

// Delete removes a record from Redis by key. // Delete: removes key from Redis with retry logic
func (r *RedisStorage) Delete(key string) error { // Delete: Redis DEL with retry
	return r.retry(func() error { // return: execute DEL with retry
		return r.client.Del(r.ctx, key).Err() // Del key; Err() returns error (not *Result)
	})
}

/*
================================================================================
ARCHITECTURAL / ENGINEERING DECISIONS
================================================================================

1. REDIS AS DISTRIBUTATED STORAGE
   - Redis provides shared state across multiple Rate Limiter instances.
   - Decision: production deployments run multiple process instances behind a load balancer.
     In-memory storage would not sync across instances. Redis provides a single source of truth.
   - Tradeoff: network latency per operation vs correctness in distributed setup.

2. JSON SERIALIZATION
   - Records are marshaled to JSON before storing in Redis, deserialized on read.
   - Decision: Redis stores strings/bytes. JSON is a portable, human-readable format.
   - Alternative: MessagePack (more compact but less debuggable).
   - Why JSON: Record struct is simple (int64, int, []int64). JSON overhead is negligible.

3. RETRY MECHANISM (maxRetries=3, retryDelay=100ms)
   - Transient failures (network timeout, connection drop) are retried up to 3 times.
   - Decision: 3 attempts with 100ms delay provides resilience without excessive latency.
   - Total worst-case retry time: ~200ms (100ms + 100ms for 2 retries).
   - Alternative: exponential backoff. Not needed for short-lived operations.

4. CONTEXT USAGE
   - Each Redis operation uses r.ctx (background context).
   - Decision: no request-scoped cancellation needed. Operations are fast (<10ms typically).
   - For HTTP server context propagation, callers could use context.WithTimeout.

5. TTL (TIME-TO-LIVE)
   - All Redis keys have a TTL set automatically on Set().
   - Decision: prevents unbounded memory growth in Redis. Stale identity data auto-expires.
   - TTL value is configurable via NewRedisStorage(..., ttl).

6. ERROR HANDLING ON GET FAILURE
   - If Get fails after retries, returns (Record{}, false) — treated as "not found".
   - Decision: fails safe. If Redis is unavailable, all requests are treated as new (currentCount=0).
   - This could allow brief bursts past the limit during Redis outage.
   - Alternative: fail closed (deny all on storage error). Chosen: fail open for availability.

7. ERR() VS RESULT()
   - Set uses .Err() (returns error), not .Result() (returns *Result and error).
   - Same for Del. Decision: simpler API — we only care about success/failure, not the Redis result.

8. THREAD SAFETY
   - Redis client is inherently thread-safe (handles connection pooling internally).
   - No additional synchronization needed in RedisStorage struct.
   - Note: the retry() function is stateless, safe for concurrent use.
*/
