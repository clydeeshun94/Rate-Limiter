package limiter

import (
	"sync"
	"time"

	rate "rate-limiter/internal/storage"
)

type SlidingWindowLog struct {
	storage rate.Storage
	limit   int
	mu      sync.Mutex
}

func NewSlidingWindowLogWithLimit(storage rate.Storage, limit int) *SlidingWindowLog {
	return &SlidingWindowLog{
		storage: storage,
		limit:   limit,
	}
}

func (swl *SlidingWindowLog) SetLimit(limit int) {
	swl.mu.Lock()
	defer swl.mu.Unlock()
	swl.limit = limit
}

func (swl *SlidingWindowLog) Check(identity string, policy Policy) (Result, error) {
	swl.mu.Lock()
	defer swl.mu.Unlock()

	now := time.Now().Unix()
	windowSeconds := int64(policy.Window.Seconds())
	cutoff := now - windowSeconds

	key := identity
	record, exists := swl.storage.Get(key)

	if !exists {
		record = rate.Record{WindowStart: now, Timestamps: []int64{}}
	}

	var valid []int64
	for _, ts := range record.Timestamps {
		if ts >= cutoff {
			valid = append(valid, ts)
		}
	}

	if len(valid) >= swl.limit {
		oldest := valid[0]
		retryAfter := time.Duration(oldest+windowSeconds-now) * time.Second
		if retryAfter < 0 {
			retryAfter = 0
		}
		return Result{
			Allowed:    false,
			Limit:      swl.limit,
			Remaining:  swl.limit - len(valid),
			RetryAfter: retryAfter,
			ResetTime:  time.Unix(oldest+windowSeconds, 0),
		}, nil
	}

	valid = append(valid, now)
	record.Timestamps = valid
	record.Count = len(valid)
	record.WindowStart = now
	swl.storage.Set(key, record)

	return Result{
		Allowed:    true,
		Limit:      swl.limit,
		Remaining:  swl.limit - len(valid),
		RetryAfter: 0,
		ResetTime:  time.Unix(now+windowSeconds, 0),
	}, nil
}
