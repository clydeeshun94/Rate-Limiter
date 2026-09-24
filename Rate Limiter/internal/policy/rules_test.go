package policy

import (
	"testing"
	"time"
)

func TestResolverValidatesAndVersionsRules(t *testing.T) {
	r, err := NewResolver([]Rule{{Name: "search", Resource: "search", Identity: IdentityAuthenticated, Algorithm: "token_bucket", Limit: 10, Window: time.Minute}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Version() != 1 {
		t.Fatalf("version=%d", r.Version())
	}
	got, ok, version := r.Resolve("search", IdentityAuthenticated)
	if !ok || got.Limit != 10 || version != 1 {
		t.Fatalf("unexpected rule: %+v %v %d", got, ok, version)
	}
}

func TestResolverRejectsInvalidRule(t *testing.T) {
	if _, err := NewResolver([]Rule{{Name: "bad", Resource: "x", Algorithm: "fixed_window", Limit: 0, Window: time.Minute}}); err == nil {
		t.Fatal("expected invalid rule error")
	}
}
