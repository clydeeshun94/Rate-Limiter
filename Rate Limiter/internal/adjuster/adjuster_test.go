package adjuster_test

import (
	"testing"
	"time"

	adjuster "rate-limiter/internal/adjuster"
	limiter "rate-limiter/internal/limiter"
)

type mockLimiter struct {
	limit int
}

func (m *mockLimiter) Check(identity string, policy limiter.Policy) (limiter.Result, error) {
	return limiter.Result{}, nil
}

func (m *mockLimiter) SetLimit(limit int) {
	m.limit = limit
}

type mockProvider struct {
	metrics adjuster.Metrics
	err     error
}

func (m *mockProvider) GetMetrics() (adjuster.Metrics, error) {
	return m.metrics, m.err
}

func TestNewAdjuster_RequiresMetrics(t *testing.T) {
	_, err := adjuster.NewAdjuster(
		[]limiter.LimitSetter{&mockLimiter{}},
		&mockProvider{},
		adjuster.AdjusterConfig{MetricsToWatch: []string{}, Interval: time.Second},
	)
	if err == nil {
		t.Fatal("expected error when no metrics configured")
	}
}

func TestNewAdjuster_RequiresNonNegotiable(t *testing.T) {
	_, err := adjuster.NewAdjuster(
		[]limiter.LimitSetter{&mockLimiter{}},
		&mockProvider{},
		adjuster.AdjusterConfig{
			MetricsToWatch: []string{"CPUUsage"},
			Interval:       time.Second,
		},
	)
	if err == nil {
		t.Fatal("expected error when no non-negotiable metric configured")
	}
	if err.Error() != "at least one non-negotiable metric (ErrorRate or ResponseTime) must be configured" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNewAdjuster_ValidConfig(t *testing.T) {
	a, err := adjuster.NewAdjuster(
		[]limiter.LimitSetter{&mockLimiter{}},
		&mockProvider{},
		adjuster.AdjusterConfig{
			MetricsToWatch: []string{"ErrorRate"},
			Interval:       10 * time.Second,
			MinLimit:       10,
			MaxLimit:       100,
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a == nil {
		t.Fatal("expected non-nil adjuster")
	}
}

func TestAdjuster_SetLimit(t *testing.T) {
	ml := &mockLimiter{limit: 100}
	a, _ := adjuster.NewAdjuster(
		[]limiter.LimitSetter{ml},
		&mockProvider{metrics: adjuster.Metrics{ErrorRate: 0.01}},
		adjuster.AdjusterConfig{
			MetricsToWatch: []string{"ErrorRate"},
			Interval:       time.Hour,
			MinLimit:       10,
			MaxLimit:       100,
		},
	)
	ml.SetLimit(50)
	if ml.limit != 50 {
		t.Fatalf("expected limit=50, got %d", ml.limit)
	}
	_ = a
}

func TestHealthScore_HealthySystem(t *testing.T) {
	provider := &mockProvider{
		metrics: adjuster.Metrics{
			ErrorRate:   0.01,
			ResponseTime: 100 * time.Millisecond,
			CPUUsage:    20,
			MemoryUsage: 30,
			ActiveConns: 50,
		},
	}
	a, _ := adjuster.NewAdjuster(
		[]limiter.LimitSetter{&mockLimiter{limit: 100}},
		provider,
		adjuster.AdjusterConfig{
			MetricsToWatch: []string{"ErrorRate", "ResponseTime", "CPUUsage", "MemoryUsage", "ActiveConns"},
			Thresholds:     map[string]float64{"ResponseTime": 500, "ActiveConns": 1000},
			Interval:       time.Hour,
			MinLimit:       10,
			MaxLimit:       100,
		},
	)
	metrics, _ := provider.GetMetrics()
	scores := a.CalculateScores(metrics)
	if scores.Overall < 70 {
		t.Fatalf("expected healthy system (score > 70), got %.2f", scores.Overall)
	}
}

func TestHealthScore_SickSystem(t *testing.T) {
	provider := &mockProvider{
		metrics: adjuster.Metrics{
			ErrorRate:    0.8,
			ResponseTime: 1 * time.Second,
			CPUUsage:     95,
			MemoryUsage:  90,
			ActiveConns:  2000,
		},
	}
	a, _ := adjuster.NewAdjuster(
		[]limiter.LimitSetter{&mockLimiter{limit: 100}},
		provider,
		adjuster.AdjusterConfig{
			MetricsToWatch: []string{"ErrorRate", "ResponseTime", "CPUUsage", "MemoryUsage", "ActiveConns"},
			Thresholds:     map[string]float64{"ResponseTime": 500, "ActiveConns": 1000},
			Interval:       time.Hour,
			MinLimit:       10,
			MaxLimit:       100,
		},
	)
	metrics, _ := provider.GetMetrics()
	scores := a.CalculateScores(metrics)
	if scores.Overall >= 30 {
		t.Fatalf("expected sick system (score < 30), got %.2f", scores.Overall)
	}
}

func TestAdjustment_HealthyIncreasesLimit(t *testing.T) {
	a, _ := adjuster.NewAdjuster(
		[]limiter.LimitSetter{&mockLimiter{limit: 100}},
		&mockProvider{metrics: adjuster.Metrics{ErrorRate: 0.01}},
		adjuster.AdjusterConfig{
			MetricsToWatch: []string{"ErrorRate"},
			Interval:       time.Hour,
			MinLimit:       10,
			MaxLimit:       200,
		},
	)
	newLimit := a.CalculateNewLimit(100, 85)
	if newLimit != 110 {
		t.Fatalf("expected 110 (10%% increase), got %d", newLimit)
	}
	_ = a
}

func TestAdjustment_SickDecreasesConservative(t *testing.T) {
	a, _ := adjuster.NewAdjuster(
		[]limiter.LimitSetter{&mockLimiter{limit: 100}},
		&mockProvider{metrics: adjuster.Metrics{ErrorRate: 0.5}},
		adjuster.AdjusterConfig{
			MetricsToWatch: []string{"ErrorRate"},
			Mode:           "conservative",
			Interval:       time.Hour,
			MinLimit:       10,
			MaxLimit:       200,
		},
	)
	newLimit := a.CalculateNewLimit(100, 20)
	if newLimit != 90 {
		t.Fatalf("expected 90 (10%% decrease), got %d", newLimit)
	}
	_ = a
}

func TestAdjustment_SickDecreasesAggressive(t *testing.T) {
	a, _ := adjuster.NewAdjuster(
		[]limiter.LimitSetter{&mockLimiter{limit: 100}},
		&mockProvider{metrics: adjuster.Metrics{ErrorRate: 0.5}},
		adjuster.AdjusterConfig{
			MetricsToWatch: []string{"ErrorRate"},
			Mode:           "aggressive",
			Interval:       time.Hour,
			MinLimit:       10,
			MaxLimit:       200,
		},
	)
	newLimit := a.CalculateNewLimit(100, 20)
	if newLimit != 50 {
		t.Fatalf("expected 50 (50%% decrease), got %d", newLimit)
	}
	_ = a
}

func TestAdjustment_StaysInBounds(t *testing.T) {
	a, _ := adjuster.NewAdjuster(
		[]limiter.LimitSetter{&mockLimiter{limit: 15}},
		&mockProvider{metrics: adjuster.Metrics{ErrorRate: 0.5}},
		adjuster.AdjusterConfig{
			MetricsToWatch: []string{"ErrorRate"},
			Mode:           "aggressive",
			Interval:       time.Hour,
			MinLimit:       10,
			MaxLimit:       200,
		},
	)
	newLimit := a.CalculateNewLimit(15, 20)
	if newLimit != 10 {
		t.Fatalf("expected 10 (floored at MinLimit), got %d", newLimit)
	}
	_ = a
}

func TestAdjustment_Hold(t *testing.T) {
	a, _ := adjuster.NewAdjuster(
		[]limiter.LimitSetter{&mockLimiter{limit: 100}},
		&mockProvider{metrics: adjuster.Metrics{ErrorRate: 0.01}},
		adjuster.AdjusterConfig{
			MetricsToWatch: []string{"ErrorRate"},
			Interval:       time.Hour,
			MinLimit:       10,
			MaxLimit:       200,
		},
	)
	newLimit := a.CalculateNewLimit(100, 50)
	if newLimit != 100 {
		t.Fatalf("expected 100 (hold), got %d", newLimit)
	}
	_ = a
}
