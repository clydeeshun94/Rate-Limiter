package limiter

import (
	"context"
	"sync"
	"time"
)

type FailurePolicy struct {
	Mode             FailMode
	Local            RateLimiter
	MaxLocalFailures int
}
type ResilientLimiter struct {
	Primary   RateLimiter
	Config    FailurePolicy
	mu        sync.Mutex
	failures  int
	openUntil time.Time
	cooldown  time.Duration
}

func NewResilient(primary RateLimiter, cfg FailurePolicy) *ResilientLimiter {
	if cfg.MaxLocalFailures <= 0 {
		cfg.MaxLocalFailures = 3
	}
	return &ResilientLimiter{Primary: primary, Config: cfg, cooldown: time.Second}
}
func (r *ResilientLimiter) Check(identity string, p Policy) (Result, error) {
	return r.CheckContext(context.Background(), identity, p)
}
func (r *ResilientLimiter) CheckContext(ctx context.Context, identity string, p Policy) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if r.Primary == nil {
		return r.fallback(identity, p, ErrStorageUnavailable)
	}
	r.mu.Lock()
	circuitOpen := !r.openUntil.IsZero() && time.Now().Before(r.openUntil)
	r.mu.Unlock()
	if circuitOpen {
		return r.fallback(identity, p, ErrStorageUnavailable)
	}
	res, err := checkWithContext(ctx, r.Primary, identity, p)
	if err == nil {
		r.recordSuccess()
		return res, nil
	}
	r.recordFailure()
	return r.fallback(identity, p, err)
}
func (r *ResilientLimiter) fallback(id string, p Policy, err error) (Result, error) {
	switch r.Config.Mode {
	case FailOpen:
		return Result{Allowed: true, Limit: p.Limit, Remaining: p.Limit, ResetTime: time.Now().Add(p.Window)}, nil
	case FailLocal:
		if r.Config.Local != nil {
			res, e := r.Config.Local.Check(id, p)
			if e == nil {
				return res, nil
			}
		}
	}
	return Result{Allowed: false, Limit: p.Limit, Remaining: 0, RetryAfter: p.Window, ResetTime: time.Now().Add(p.Window)}, err
}
func (r *ResilientLimiter) recordSuccess() {
	r.mu.Lock()
	r.failures = 0
	r.openUntil = time.Time{}
	r.mu.Unlock()
}

func (r *ResilientLimiter) recordFailure() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.failures++
	if r.failures >= r.Config.MaxLocalFailures {
		r.openUntil = time.Now().Add(r.cooldown)
	}
}

// Degraded reports whether the circuit is currently open.
func (r *ResilientLimiter) Degraded() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return !r.openUntil.IsZero() && time.Now().Before(r.openUntil)
}
