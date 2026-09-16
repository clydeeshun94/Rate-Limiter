package limiter

import (
	"sync"
	"time"

	rate "rate-limiter/internal/storage"
)

type SlidingWindowCounter struct {
	storage rate.Storage
	mu      sync.Mutex
}

func NewSlidingWindowCounter(storage rate.Storage) *SlidingWindowCounter {
	return &SlidingWindowCounter{
		storage: storage,
	}
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

	if currentCount >= policy.Limit {
		resetTime := time.Unix(windowStart+windowSeconds, 0)
		retryAfter := time.Until(resetTime)
		if retryAfter < 0 {
			retryAfter = 0
		}
		return Result{
			Allowed:    false,
			Remaining:  policy.Limit - currentCount,
			RetryAfter: retryAfter,
			ResetTime:  resetTime,
		}, nil
	}

	currentCount++
	sw.storage.Set(key, rate.Record{WindowStart: windowStart, Count: currentCount})

	resetTime := time.Unix(windowStart+windowSeconds, 0)
	return Result{
		Allowed:    true,
		Remaining:  policy.Limit - currentCount,
		RetryAfter: 0,
		ResetTime:  resetTime,
	}, nil
}
