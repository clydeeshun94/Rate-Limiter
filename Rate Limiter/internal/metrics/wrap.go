package metrics

import (
	"time"

	limiter "rate-limiter/internal/limiter"
)

type WrappedLimiter struct {
	limiter   limiter.RateLimiter
	collector *Collector
	algorithm string
}

func Wrap(limiter limiter.RateLimiter, collector *Collector, algorithm string) *WrappedLimiter {
	return &WrappedLimiter{
		limiter:   limiter,
		collector: collector,
		algorithm: algorithm,
	}
}

func (w *WrappedLimiter) Check(identity string, policy limiter.Policy) (limiter.Result, error) {
	start := time.Now()
	result, err := w.limiter.Check(identity, policy)
	duration := time.Since(start)

	w.collector.RecordDuration(duration)
	if result.Allowed {
		w.collector.RecordAllowed(identity)
	} else {
		w.collector.RecordDenied(identity)
	}

	return result, err
}

func (w *WrappedLimiter) SetLimit(limit int) {
	if setter, ok := w.limiter.(limiter.LimitSetter); ok {
		setter.SetLimit(limit)
	}
	w.collector.SetLimit(w.algorithm, limit)
}
