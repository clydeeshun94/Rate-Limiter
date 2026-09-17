package limiter

import (
	"sync"
	"time"

	rate "rate-limiter/internal/storage"
)

type LeakyBucket struct {
	storage  rate.Storage
	limit    int
	water    map[string]int
	mu       sync.Mutex
}

func NewLeakyBucketWithLimit(storage rate.Storage, limit int) *LeakyBucket {
	return &LeakyBucket{
		storage: storage,
		limit:   limit,
		water:   make(map[string]int),
	}
}

func (lb *LeakyBucket) SetLimit(limit int) {
	lb.mu.Lock()
	defer lb.mu.Unlock()
	lb.limit = limit
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
		lb.water[key] -= int(leakRate * elapsed)
		if lb.water[key] < 0 {
			lb.water[key] = 0
		}
	}

	if lb.water[key]+1 > lb.limit {
		retryAfter := time.Duration(float64(time.Second) * (float64(lb.water[key]+1-lb.limit) / leakRate))
		if retryAfter < 0 {
			retryAfter = 0
		}
		return Result{
			Allowed:    false,
			Limit:      lb.limit,
			Remaining:  lb.limit - lb.water[key],
			RetryAfter: retryAfter,
			ResetTime:  time.Now().Add(retryAfter),
		}, nil
	}

	lb.water[key]++
	lb.storage.Set(key, rate.Record{WindowStart: now, Count: lb.water[key]})

	return Result{
		Allowed:    true,
		Limit:      lb.limit,
		Remaining:  lb.limit - lb.water[key],
		RetryAfter: 0,
		ResetTime:  time.Now().Add(time.Duration(windowSeconds) * time.Second),
	}, nil
}
