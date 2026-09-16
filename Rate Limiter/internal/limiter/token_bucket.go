package limiter

import (
	"sync"
	"time"

	rate "rate-limiter/internal/storage"
)

type TokenBucket struct {
	storage    rate.Storage
	limit      int
	tokens     int
	lastRefill int64
	mu         sync.Mutex
}

func NewTokenBucketWithLimit(storage rate.Storage, limit int) *TokenBucket {
	return &TokenBucket{
		storage: storage,
		limit:   limit,
		tokens:  limit,
	}
}

func (tb *TokenBucket) SetLimit(limit int) {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	tb.limit = limit
}

func (tb *TokenBucket) Check(identity string, policy Policy) (Result, error) {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now().Unix()
	windowSeconds := int64(policy.Window.Seconds())
	refillRate := float64(tb.limit) / float64(windowSeconds)

	key := identity
	record, exists := tb.storage.Get(key)

	if exists && record.WindowStart != 0 {
		elapsed := float64(now - record.WindowStart)
		tb.tokens += int(refillRate * elapsed)
		if tb.tokens > tb.limit {
			tb.tokens = tb.limit
		}
	} else if !exists {
		tb.tokens = tb.limit
	}

	tb.tokens--
	if tb.tokens < 0 {
		tb.tokens = 0
		retryAfter := time.Duration((1.0 / refillRate) * float64(time.Second))
		resetTime := time.Now().Add(retryAfter)
		return Result{
			Allowed:    false,
			Remaining:  0,
			RetryAfter: retryAfter,
			ResetTime:  resetTime,
		}, nil
	}

	tb.storage.Set(key, rate.Record{WindowStart: now, Count: tb.tokens})

	return Result{
		Allowed:    true,
		Remaining:  tb.tokens,
		RetryAfter: 0,
		ResetTime:  time.Now().Add(time.Duration(windowSeconds) * time.Second),
	}, nil
}
