package integration_test

import (
	"testing"
	"time"

	adjuster "rate-limiter/internal/adjuster"
	limiter "rate-limiter/internal/limiter"
	rate "rate-limiter/internal/storage"
)

type mockProvider struct {
	metrics adjuster.Metrics
	err     error
}

func (p *mockProvider) GetMetrics() (adjuster.Metrics, error) {
	return p.metrics, p.err
}

func TestAdjuster_WithAlgorithmAndStorage(t *testing.T) {
	storage := rate.NewMemoryStorage()
	ml := limiter.NewFixedWindowWithLimit(storage, 10)
	provider := &mockProvider{metrics: adjuster.Metrics{ErrorRate: 0.01}}
	config := adjuster.AdjusterConfig{
		MetricsToWatch: []string{"ErrorRate"},
		Interval:       time.Hour,
		MinLimit:       1,
		MaxLimit:       100,
	}

	_, err := adjuster.NewAdjuster([]limiter.LimitSetter{ml}, provider, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ml.Check("alice", limiter.Policy{Limit: 10, Window: 60 * time.Second})
	_, exists := storage.Get("alice")
	if !exists {
		t.Fatal("expected storage to have record for alice after check")
	}
}

func TestAdjuster_MultipleAlgorithmsSharingOneAdjuster(t *testing.T) {
	storage := rate.NewMemoryStorage()
	l1 := limiter.NewFixedWindowWithLimit(storage, 10)
	l2 := limiter.NewSlidingWindowCounterWithLimit(storage, 10)
	provider := &mockProvider{metrics: adjuster.Metrics{ErrorRate: 0.01}}
	config := adjuster.AdjusterConfig{
		MetricsToWatch: []string{"ErrorRate"},
		Interval:       time.Hour,
		MinLimit:       1,
		MaxLimit:       100,
	}

	a, err := adjuster.NewAdjuster([]limiter.LimitSetter{l1, l2}, provider, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if a == nil {
		t.Fatal("expected non-nil adjuster")
	}

	if len(a.CalculateScores(adjuster.Metrics{ErrorRate: 0.5}).ByMetric) == 0 {
		t.Fatal("expected non-empty scores")
	}

	newLimit := a.CalculateNewLimit(50, 85)
	if newLimit != 55 {
		t.Fatalf("expected 55 (10%% increase), got %d", newLimit)
	}
}

func TestAdjuster_AdjustsMultipleLimiters(t *testing.T) {
	storage := rate.NewMemoryStorage()
	l1 := limiter.NewFixedWindowWithLimit(storage, 10)
	l2 := limiter.NewSlidingWindowCounterWithLimit(storage, 10)
	provider := &mockProvider{metrics: adjuster.Metrics{ErrorRate: 0.5}}
	config := adjuster.AdjusterConfig{
		MetricsToWatch: []string{"ErrorRate"},
		Interval:       time.Hour,
		MinLimit:       1,
		MaxLimit:       200,
	}

	a, err := adjuster.NewAdjuster([]limiter.LimitSetter{l1, l2}, provider, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newLimit := a.CalculateNewLimit(100, 20)
	if newLimit != 90 {
		t.Fatalf("expected 90 (10%% decrease), got %d", newLimit)
	}
}

func TestAdjuster_WithSlidingWindowLog(t *testing.T) {
	storage := rate.NewMemoryStorage()
	l := limiter.NewSlidingWindowLogWithLimit(storage, 10)
	provider := &mockProvider{metrics: adjuster.Metrics{ErrorRate: 0.01}}
	config := adjuster.AdjusterConfig{
		MetricsToWatch: []string{"ErrorRate"},
		Interval:       time.Hour,
		MinLimit:       1,
		MaxLimit:       100,
	}

	a, err := adjuster.NewAdjuster([]limiter.LimitSetter{l}, provider, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	l.Check("frank", limiter.Policy{Limit: 10, Window: 60 * time.Second})
	record, exists := storage.Get("frank")
	if !exists {
		t.Fatal("expected storage to have record for frank")
	}
	if len(record.Timestamps) != 1 {
		t.Fatalf("expected 1 timestamp, got %d", len(record.Timestamps))
	}

	newLimit := a.CalculateNewLimit(50, 85)
	if newLimit != 55 {
		t.Fatalf("expected 55, got %d", newLimit)
	}
}
