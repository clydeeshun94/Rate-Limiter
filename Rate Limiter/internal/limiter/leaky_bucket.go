package limiter

import (
	"sync"
	"time"

	rate "rate-limiter/internal/storage"
)

type LeakyBucket struct {
	storage   rate.Storage
	mu        sync.Mutex
	water     int
	lastLeak  int64
}

func NewLeakyBucket(storage rate.Storage) *LeakyBucket {
	return &LeakyBucket{
		storage:  storage,
		water:    0,
		lastLeak: time.Now().Unix(),
	}
}

func (lb *LeakyBucket) Check(identity string, policy Policy) (Result, error) {
	lb.mu.Lock()
	defer lb.mu.Unlock()

	now := time.Now().Unix()
	windowSeconds := int64(policy.Window.Seconds())
	leakRate := float64(policy.Limit) / float64(windowSeconds)

	key := identity
	record, exists := lb.storage.Get(key)

	if exists {
		elapsed := float64(now - record.WindowStart)
		lb.water -= int(leakRate * elapsed)
		if lb.water < 0 {
			lb.water = 0
		}
	}

	if lb.water+1 > policy.Limit {
		retryAfter := time.Duration(float64(time.Second) * (float64(lb.water+1-policy.Limit) / leakRate))
		if retryAfter < 0 {
			retryAfter = 0
		}
		return Result{
			Allowed:    false,
			Remaining:  policy.Limit - lb.water,
			RetryAfter: retryAfter,
			ResetTime:  time.Now().Add(retryAfter),
		}, nil
	}

	lb.water++
	lb.storage.Set(key, rate.Record{WindowStart: now, Count: lb.water})

	return Result{
		Allowed:    true,
		Remaining:  policy.Limit - lb.water,
		RetryAfter: 0,
		ResetTime:  time.Now().Add(time.Duration(windowSeconds) * time.Second),
	}, nil
}
