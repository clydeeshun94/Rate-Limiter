package limiter

import (
	"sync"
	"time"

	rate "rate-limiter/internal/storage"
)

type FixedWindow struct {
	storage rate.Storage
	mu      sync.Mutex
}

func NewFixedWindow(storage rate.Storage) *FixedWindow {
	return &FixedWindow{
		storage: storage,
	}
}

func (fw *FixedWindow) Check(identity string, policy Policy) (Result, error) {
	fw.mu.Lock()
	defer fw.mu.Unlock()

	now := time.Now().Unix()
	windowStart := now - (now % int64(policy.Window.Seconds()))

	key := identity
	record, exists := fw.storage.Get(key)

	if !exists || record.WindowStart != windowStart {
		record = rate.Record{
			WindowStart: windowStart,
			Count:       0,
		}
	}

	if record.Count >= policy.Limit {
		resetTime := time.Unix(windowStart+int64(policy.Window.Seconds()), 0)
		retryAfter := time.Until(resetTime)
		if retryAfter < 0 {
			retryAfter = 0
		}
		return Result{
			Allowed:    false,
			Remaining:  record.Count,
			RetryAfter: retryAfter,
			ResetTime:  resetTime,
		}, nil
	}

	record.Count++
	fw.storage.Set(key, record)

	resetTime := time.Unix(windowStart+int64(policy.Window.Seconds()), 0)

	return Result{
		Allowed:    true,
		Remaining:  policy.Limit - record.Count,
		RetryAfter: 0,
		ResetTime:  resetTime,
	}, nil
}
