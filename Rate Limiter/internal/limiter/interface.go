package limiter

import (
	"time"

	rate "rate-limiter/internal/storage"
)

type Policy struct {
	Limit  int
	Window time.Duration
}

type Result struct {
	Allowed    bool
	Limit      int
	Remaining  int
	RetryAfter time.Duration
	ResetTime  time.Time
}

type Storage = rate.Storage

type RateLimiter interface {
	Check(identity string, policy Policy) (Result, error)
}

type LimitSetter interface {
	SetLimit(limit int)
}