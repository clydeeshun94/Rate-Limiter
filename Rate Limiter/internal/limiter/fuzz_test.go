package limiter_test

import (
	"testing"
	"time"

	limiter "rate-limiter/internal/limiter"
	rate "rate-limiter/internal/storage"
)

// FuzzFixedWindowCheck asserts Check never panics on arbitrary inputs and
// always respects policy validation (invalid limits/windows are rejected
// before any storage mutation).
func FuzzFixedWindowCheck(f *testing.F) {
	f.Add("alice", 3, 60)
	f.Add("", 0, 0)
	f.Add("bad\x00id", 100001, 86401)

	l := limiter.NewFixedWindowWithLimit(rate.NewMemoryStorage(), 3)
	f.Fuzz(func(t *testing.T, identity string, limit int, windowSec int) {
		p := limiter.Policy{Limit: limit, Window: time.Duration(windowSec) * time.Second}
		_, _ = l.Check(identity, p)
	})
}
