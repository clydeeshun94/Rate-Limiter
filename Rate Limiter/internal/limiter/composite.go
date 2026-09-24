package limiter

import (
	"context"
	"fmt"
)

type CompositeDimension struct {
	Limiter RateLimiter
	Policy  Policy
}

type CompositeLimiter struct {
	dimensions []CompositeDimension
	gate       chan int
}

func NewComposite(limiters ...RateLimiter) *CompositeLimiter {
	dimensions := make([]CompositeDimension, 0, len(limiters))
	for _, l := range limiters {
		dimensions = append(dimensions, CompositeDimension{Limiter: l})
	}
	return &CompositeLimiter{dimensions: dimensions, gate: make(chan int, 1)}
}

func NewCompositePolicy(dimensions ...CompositeDimension) *CompositeLimiter {
	return &CompositeLimiter{dimensions: append([]CompositeDimension(nil), dimensions...), gate: make(chan int, 1)}
}

// Check uses transactional reservation semantics when every child implements
// ReservingLimiter. Otherwise it uses conservative sequential consumption;
// earlier successful children are not rolled back after a later rejection.
func (c *CompositeLimiter) Check(identity string, policy Policy) (Result, error) {
	return c.CheckContext(context.Background(), identity, policy)
}
func (c *CompositeLimiter) CheckContext(ctx context.Context, identity string, policy Policy) (Result, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if len(c.dimensions) == 0 {
		return Result{}, fmt.Errorf("composite limiter has no dimensions")
	}
	allReserving := true
	for _, dimension := range c.dimensions {
		if dimension.Limiter == nil {
			return Result{}, fmt.Errorf("composite limiter contains nil dimension")
		}
		if _, ok := dimension.Limiter.(ReservingLimiter); !ok {
			allReserving = false
			break
		}
	}
	if allReserving {
		select {
		case c.gate <- 1:
			defer func() { <-c.gate }()
		case <-ctx.Done():
			return Result{}, ctx.Err()
		}
		return c.checkReserved(ctx, identity, policy)
	}
	return c.checkSequential(ctx, identity, policy)
}
func checkWithContext(ctx context.Context, l RateLimiter, id string, p Policy) (Result, error) {
	if x, ok := l.(ContextRateLimiter); ok {
		return x.CheckContext(ctx, id, p)
	}
	return l.Check(id, p)
}

type ReservingLimiter interface {
	RateLimiter
	// Reserve consumes capacity and returns a rollback function. The rollback
	// function must be called only when the composite transaction is aborted.
	Reserve(identity string, policy Policy) (Result, func(), error)
}

func (c *CompositeLimiter) checkReserved(ctx context.Context, identity string, policy Policy) (Result, error) {
	var rollbacks []func()
	var combined Result
	combined.Allowed = true
	combined.Remaining = -1
	for _, dimension := range c.dimensions {
		l := dimension.Limiter
		childPolicy := policy
		if dimension.Policy.Limit != 0 || dimension.Policy.Window != 0 || dimension.Policy.Cost != 0 || dimension.Policy.Resource != "" {
			childPolicy = dimension.Policy
		}
		select {
		case <-ctx.Done():
			for i := len(rollbacks) - 1; i >= 0; i-- {
				rollbacks[i]()
			}
			return Result{}, ctx.Err()
		default:
		}
		res, rollback, err := l.(ReservingLimiter).Reserve(identity, childPolicy)
		if err != nil {
			for i := len(rollbacks) - 1; i >= 0; i-- {
				rollbacks[i]()
			}
			return Result{}, err
		}
		if !res.Allowed {
			if rollback != nil {
				rollback()
			}
			for i := len(rollbacks) - 1; i >= 0; i-- {
				rollbacks[i]()
			}
			combined = mergeResult(combined, res)
			combined.Allowed = false
			return combined, nil
		}
		if rollback != nil {
			rollbacks = append(rollbacks, rollback)
		}
		combined = mergeResult(combined, res)
	}
	return combined, nil
}

func (c *CompositeLimiter) checkSequential(ctx context.Context, identity string, policy Policy) (Result, error) {
	var combined Result
	combined.Allowed = true
	combined.Remaining = -1
	for _, dimension := range c.dimensions {
		l := dimension.Limiter
		childPolicy := policy
		if dimension.Policy.Limit != 0 || dimension.Policy.Window != 0 || dimension.Policy.Cost != 0 || dimension.Policy.Resource != "" {
			childPolicy = dimension.Policy
		}
		select {
		case <-ctx.Done():
			return Result{}, ctx.Err()
		default:
		}
		res, err := checkWithContext(ctx, l, identity, childPolicy)
		if err != nil {
			return Result{}, err
		}
		combined = mergeResult(combined, res)
		if !res.Allowed {
			// Do not consume later dimensions after a rejection. Earlier
			// dimensions may already have been consumed; this is intentional
			// and is the documented fallback for non-reserving children.
			return combined, nil
		}
	}
	return combined, nil
}

func mergeResult(combined, res Result) Result {
	if combined.Remaining < 0 || res.Remaining < combined.Remaining {
		combined.Remaining = res.Remaining
	}
	if res.Limit < combined.Limit || combined.Limit == 0 {
		combined.Limit = res.Limit
	}
	if res.ResetTime.After(combined.ResetTime) {
		combined.ResetTime = res.ResetTime
	}
	if res.RetryAfter > combined.RetryAfter {
		combined.RetryAfter = res.RetryAfter
	}
	if !res.Allowed {
		combined.Allowed = false
	}
	return combined
}
