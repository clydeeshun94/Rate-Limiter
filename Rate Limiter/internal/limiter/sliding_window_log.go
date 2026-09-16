package limiter

import (
	"sync"
	"time"
)

type SlidingWindowLog struct {
	mu   sync.Mutex
	logs map[string][]int64
}

func NewSlidingWindowLog() *SlidingWindowLog {
	return &SlidingWindowLog{
		logs: make(map[string][]int64),
	}
}

func (swl *SlidingWindowLog) Check(identity string, policy Policy) (Result, error) {
	swl.mu.Lock()
	defer swl.mu.Unlock()

	now := time.Now().Unix()
	windowSeconds := int64(policy.Window.Seconds())
	cutoff := now - windowSeconds

	timestamps := swl.logs[identity]
	var valid []int64
	for _, ts := range timestamps {
		if ts >= cutoff {
			valid = append(valid, ts)
		}
	}
	swl.logs[identity] = valid

	if len(valid) >= policy.Limit {
		oldest := valid[0]
		retryAfter := time.Duration(oldest+windowSeconds-now) * time.Second
		if retryAfter < 0 {
			retryAfter = 0
		}
		return Result{
			Allowed:    false,
			Remaining:  policy.Limit - len(valid),
			RetryAfter: retryAfter,
			ResetTime:  time.Unix(oldest + windowSeconds, 0),
		}, nil
	}

	swl.logs[identity] = append(valid, now)

	return Result{
		Allowed:    true,
		Remaining:  policy.Limit - len(swl.logs[identity]),
		RetryAfter: 0,
		ResetTime:  time.Unix(now + windowSeconds, 0),
	}, nil
}
