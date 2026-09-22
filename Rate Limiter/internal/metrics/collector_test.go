package metrics

import (
	"fmt"
	"strings"
	"testing"
	"time"

	limiter "rate-limiter/internal/limiter"
	rate "rate-limiter/internal/storage"
)

func TestCollector_RecordAllowed(t *testing.T) {
	c := NewCollector()
	c.RecordAllowed("alice")
	c.RecordAllowed("alice")
	c.RecordAllowed("bob")

	text := c.ToPrometheusText()
	if !strings.Contains(text, fmt.Sprintf("rate_limiter_requests_allowed{identity_hash=\"%s\"} 2", identityHash("alice"))) {
		t.Fatal("expected alice allowed count = 2")
	}
	if !strings.Contains(text, fmt.Sprintf("rate_limiter_requests_allowed{identity_hash=\"%s\"} 1", identityHash("bob"))) {
		t.Fatal("expected bob allowed count = 1")
	}
}

func TestCollector_RecordDenied(t *testing.T) {
	c := NewCollector()
	c.RecordDenied("alice")
	c.RecordDenied("alice")
	c.RecordDenied("bob")

	text := c.ToPrometheusText()
	if !strings.Contains(text, fmt.Sprintf("rate_limiter_requests_denied{identity_hash=\"%s\"} 2", identityHash("alice"))) {
		t.Fatal("expected alice denied count = 2")
	}
}

func TestCollector_SetLimit(t *testing.T) {
	c := NewCollector()
	c.SetLimit("sliding_window_counter", 50)
	c.SetLimit("token_bucket", 20)

	text := c.ToPrometheusText()
	if !strings.Contains(text, "rate_limiter_current_limit{algorithm=\"sliding_window_counter\"} 50") {
		t.Fatal("expected sliding_window_counter limit = 50")
	}
	if !strings.Contains(text, "rate_limiter_current_limit{algorithm=\"token_bucket\"} 20") {
		t.Fatal("expected token_bucket limit = 20")
	}
}

func TestCollector_ToPrometheusText_HasAllSections(t *testing.T) {
	c := NewCollector()
	c.RecordAllowed("alice")
	c.RecordDenied("bob")
	c.SetLimit("fixed_window", 100)
	c.RecordDuration(5 * time.Millisecond)
	c.RecordDuration(10 * time.Millisecond)

	text := c.ToPrometheusText()

	checks := []string{
		"rate_limiter_requests_allowed",
		"rate_limiter_requests_denied",
		"rate_limiter_current_limit",
		"rate_limiter_checks_total",
		"rate_limiter_check_duration_seconds",
	}
	for _, check := range checks {
		if !strings.Contains(text, check) {
			t.Fatalf("expected metric section %s in output", check)
		}
	}
}

func TestWrap_CheckRecordsMetrics(t *testing.T) {
	storage := rate.NewMemoryStorage()
	inner := limiter.NewFixedWindowWithLimit(storage, 2)
	c := NewCollector()
	wrapped := Wrap(inner, c, "fixed_window")

	result, err := wrapped.Check("alice", limiter.Policy{Limit: 2, Window: 60 * time.Second})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Allowed {
		t.Fatal("expected allowed")
	}

	if c.allowedTotal[identityHash("alice")] != 1 {
		t.Fatalf("expected alice allowed = 1, got %d", c.allowedTotal[identityHash("alice")])
	}
}

func TestWrap_CheckRecordsDenied(t *testing.T) {
	storage := rate.NewMemoryStorage()
	inner := limiter.NewFixedWindowWithLimit(storage, 1)
	c := NewCollector()
	wrapped := Wrap(inner, c, "fixed_window")

	wrapped.Check("alice", limiter.Policy{Limit: 1, Window: 60 * time.Second})
	result, err := wrapped.Check("alice", limiter.Policy{Limit: 1, Window: 60 * time.Second})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Fatal("expected denied")
	}

	if c.deniedTotal[identityHash("alice")] != 1 {
		t.Fatalf("expected alice denied = 1, got %d", c.deniedTotal[identityHash("alice")])
	}
}

func TestWrap_SetLimitUpdatesCollector(t *testing.T) {
	storage := rate.NewMemoryStorage()
	inner := limiter.NewFixedWindowWithLimit(storage, 10)
	c := NewCollector()
	wrapped := Wrap(inner, c, "fixed_window")

	wrapped.SetLimit(50)

	if c.limits["fixed_window"] != 50 {
		t.Fatalf("expected limit = 50 in collector, got %d", c.limits["fixed_window"])
	}
}

func TestWrap_CheckDurationRecorded(t *testing.T) {
	storage := rate.NewMemoryStorage()
	inner := limiter.NewFixedWindowWithLimit(storage, 10)
	c := NewCollector()
	wrapped := Wrap(inner, c, "fixed_window")

	wrapped.Check("alice", limiter.Policy{Limit: 10, Window: 60 * time.Second})

	if c.totalChecks != 1 {
		t.Fatalf("expected 1 check, got %d", c.totalChecks)
	}
}
