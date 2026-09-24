package limiter

import (
	"testing"
	"time"

	rate "rate-limiter/internal/storage"
)

type fakeClock struct{ now time.Time }

func (f *fakeClock) Now() time.Time { return f.now }

// TestFixedWindow_ClockDeterministic verifies window boundary rollover without
// relying on real time or sleep. At t=1000, windowStart=960 (60s window); the
// 4th request fills the limit. Advancing past the window resets the bucket and
// yields a deterministic ResetTime.
func TestFixedWindow_ClockDeterministic(t *testing.T) {
	l := &FixedWindow{storage: rate.NewMemoryStorage(), limit: 3, clock: &fakeClock{now: time.Unix(1000, 0)}}
	policy := Policy{Limit: 3, Window: 60 * time.Second}

	for i := 0; i < 3; i++ {
		r, err := l.Check("alice", policy)
		if err != nil || !r.Allowed {
			t.Fatalf("req %d: want allowed, got %+v err=%v", i+1, r, err)
		}
	}
	if r, _ := l.Check("alice", policy); r.Allowed {
		t.Fatalf("want 4th denied, got allowed")
	}

	l.clock.(*fakeClock).now = l.clock.(*fakeClock).now.Add(61 * time.Second)
	r, err := l.Check("alice", policy)
	if err != nil || !r.Allowed {
		t.Fatalf("want allowed after window rollover, got %+v err=%v", r, err)
	}
	if want := time.Unix(1080, 0); !r.ResetTime.Equal(want) {
		t.Fatalf("reset time: want %v got %v", want, r.ResetTime)
	}
}

// TestTokenBucket_ClockDeterministic verifies refill behavior using a fake
// clock instead of sleeping, replacing the real-time TestTokenBucket_RefillsOverTime.
func TestTokenBucket_ClockDeterministic(t *testing.T) {
	l := &TokenBucket{storage: rate.NewMemoryStorage(), limit: 2, clock: &fakeClock{now: time.Unix(1000, 0)}}
	policy := Policy{Limit: 2, Window: 2 * time.Second}

	l.Check("bob", policy)
	l.Check("bob", policy)
	if r, _ := l.Check("bob", policy); r.Allowed {
		t.Fatalf("want bucket empty, got allowed")
	}

	l.clock.(*fakeClock).now = l.clock.(*fakeClock).now.Add(3 * time.Second)
	if r, err := l.Check("bob", policy); err != nil || !r.Allowed {
		t.Fatalf("want refill allowed, got %+v err=%v", r, err)
	}
}
