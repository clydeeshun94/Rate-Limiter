package limiter

import (
	"errors"
	"strings"
	"time"
)

// Clock is injected into limiters that need deterministic time in tests.
type Clock interface{ Now() time.Time }
type RealClock struct{}

func (RealClock) Now() time.Time { return time.Now() }

type FailMode int

const (
	FailOpen FailMode = iota
	FailClosed
	FailLocal
)

var (
	ErrStorageUnavailable = errors.New("rate limiter storage unavailable")
	ErrInvalidIdentity    = errors.New("invalid identity")
)

// ValidateIdentity applies the invariant shared by all production key builders.
func ValidateIdentity(identity string, maxLen int) error {
	identity = strings.TrimSpace(identity)
	if identity == "" || maxLen <= 0 || len(identity) > maxLen || strings.IndexByte(identity, 0) >= 0 {
		return ErrInvalidIdentity
	}
	return nil
}

// WithCost returns a policy that consumes cost units. Algorithms that do not
// support cost-aware atomic operations reject costs other than one.
func WithCost(policy Policy, cost int) Policy { policy.Cost = cost; return policy }
