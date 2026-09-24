package limiter

import (
	"context"
	"errors"
	"testing"
	"time"
)

type compositeFake struct {
	allowed bool
	calls   int
}

func (f *compositeFake) Check(string, Policy) (Result, error) {
	f.calls++
	return Result{Allowed: f.allowed, Limit: 10, Remaining: 3, ResetTime: time.Now().Add(time.Minute)}, nil
}

type reservingFake struct {
	allowed   bool
	calls     int
	rollbacks int
}

func (f *reservingFake) Check(string, Policy) (Result, error) {
	return Result{Allowed: f.allowed, Limit: 10}, nil
}
func (f *reservingFake) Reserve(string, Policy) (Result, func(), error) {
	f.calls++
	return Result{Allowed: f.allowed, Limit: 10, Remaining: 2}, func() { f.rollbacks++ }, nil
}

func TestCompositeAcceptsPerDimensionPolicies(t *testing.T) {
	first := &compositeFake{allowed: true}
	second := &compositeFake{allowed: true}
	composite := NewCompositePolicy(
		CompositeDimension{Limiter: first, Policy: Policy{Limit: 10, Window: time.Minute}},
		CompositeDimension{Limiter: second, Policy: Policy{Limit: 100, Window: time.Hour}},
	)
	if _, err := composite.Check("id", Policy{Limit: 1, Window: time.Second}); err != nil {
		t.Fatal(err)
	}
}

func TestCompositeStopsSequentialChecksAfterDenial(t *testing.T) {
	first := &compositeFake{allowed: false}
	second := &compositeFake{allowed: true}
	res, err := NewComposite(first, second).Check("id", Policy{Limit: 10, Window: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if res.Allowed {
		t.Fatal("expected denial")
	}
	if second.calls != 0 {
		t.Fatalf("later dimension was checked %d times", second.calls)
	}
}

func TestCompositeRollsBackReservationsOnDenial(t *testing.T) {
	first := &reservingFake{allowed: true}
	second := &reservingFake{allowed: false}
	res, err := NewComposite(first, second).Check("id", Policy{Limit: 10, Window: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if res.Allowed {
		t.Fatal("expected denial")
	}
	if first.rollbacks != 1 {
		t.Fatalf("expected first reservation rollback, got %d", first.rollbacks)
	}
	if second.rollbacks != 1 {
		t.Fatalf("expected denied reservation rollback, got %d", second.rollbacks)
	}
}

func TestCompositeContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := NewComposite(&compositeFake{allowed: true}).CheckContext(ctx, "id", Policy{Limit: 1, Window: time.Minute})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context cancellation, got %v", err)
	}
}
