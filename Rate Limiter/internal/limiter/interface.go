package limiter

import (
	"context"
	"time"

	rate "rate-limiter/internal/storage"
)

type Policy struct {
	Limit    int
	Window   time.Duration
	Cost     int
	Resource string
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

// ContextRateLimiter is implemented by limiters whose storage operations can
// observe request cancellation and deadlines.
type ContextRateLimiter interface {
	CheckContext(ctx context.Context, identity string, policy Policy) (Result, error)
}

type LimitSetter interface {
	SetLimit(limit int)
}
