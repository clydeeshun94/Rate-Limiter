package limiter

import (
	"sync"
	"time"

	rate "rate-limiter/internal/storage"
)

type TokenBucket struct {
	storage    rate.Storage
	mu         sync.Mutex
	tokens     int
	lastRefill int64
}

func NewTokenBucket(storage rate.Storage) *TokenBucket {
	return &TokenBucket{
		storage:    storage,
		tokens:     0,
		lastRefill: time.Now().Unix(),
	}
}

func (tb *TokenBucket) Check(identity string, policy Policy) (Result, error) {
	tb.mu.Lock()
	defer tb.mu.Unlock()

	now := time.Now().Unix()
	windowSeconds := int64(policy.Window.Seconds())
	refillRate := float64(policy.Limit) / float64(windowSeconds)

	key := identity
	record, exists := tb.storage.Get(key)

	if exists && record.WindowStart != 0 {
		elapsed := float64(now - record.WindowStart)
		tb.tokens += int(refillRate * elapsed)
		if tb.tokens > policy.Limit {
			tb.tokens = policy.Limit
		}
	} else if !exists {
		tb.tokens = policy.Limit
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
