package limiter

import (
	"sync"
	"time"

	rate "rate-limiter/internal/storage"
)

type SlidingWindowCounter struct {
	storage rate.Storage
	limit   int
	mu      sync.Mutex
}

func NewSlidingWindowCounterWithLimit(storage rate.Storage, limit int) *SlidingWindowCounter {
	return &SlidingWindowCounter{
		storage: storage,
		limit:   limit,
	}
}

func (sw *SlidingWindowCounter) SetLimit(limit int) {
	sw.mu.Lock()
	defer sw.mu.Unlock()
	sw.limit = limit
}

func (sw *SlidingWindowCounter) Check(identity string, policy Policy) (Result, error) {
	sw.mu.Lock()
	defer sw.mu.Unlock()

	now := time.Now()
	nowUnix := now.Unix()
	windowSeconds := int64(policy.Window.Seconds())
	windowStart := nowUnix - (nowUnix % windowSeconds)
	prevWindowStart := windowStart - windowSeconds

	key := identity
	record, exists := sw.storage.Get(key)

	var currentCount int
	if !exists {
		currentCount = 0
	} else if record.WindowStart == windowStart {
		currentCount = record.Count
	} else if record.WindowStart == prevWindowStart {
		ratio := float64(nowUnix-prevWindowStart) / float64(windowSeconds)
		currentCount = int(float64(record.Count) * (1.0 - ratio))
		if currentCount < 0 {
			currentCount = 0
		}
	} else {
		currentCount = 0
	}

	if currentCount >= sw.limit {
		resetTime := time.Unix(windowStart+windowSeconds, 0)
		retryAfter := time.Until(resetTime)
		if retryAfter < 0 {
			retryAfter = 0
		}
		return Result{
			Allowed:    false,
			Remaining:  sw.limit - currentCount,
			RetryAfter: retryAfter,
			ResetTime:  resetTime,
		}, nil
	}

	currentCount++
	sw.storage.Set(key, rate.Record{WindowStart: windowStart, Count: currentCount})

	resetTime := time.Unix(windowStart+windowSeconds, 0)
	return Result{
		Allowed:    true,
		Remaining:  sw.limit - currentCount,
		RetryAfter: 0,
		ResetTime:  resetTime,
	}, nil
}
