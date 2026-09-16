package adjuster

import (
	"testing"
	"time"

	limiter "rate-limiter/internal/limiter"
)

type hysteresisLimiter struct {
	limit int
}

func (m *hysteresisLimiter) Check(identity string, policy limiter.Policy) (limiter.Result, error) {
	return limiter.Result{}, nil
}

func (m *hysteresisLimiter) SetLimit(limit int) {
	m.limit = limit
}

type healthProvider struct {
	metrics Metrics
}

func (p *healthProvider) GetMetrics() (Metrics, error) {
	return p.metrics, nil
}

func TestHysteresis_BlocksRapidAdjustment(t *testing.T) {
	ml := &hysteresisLimiter{limit: 200}
	provider := &healthProvider{metrics: Metrics{ErrorRate: 0.9}}
	config := AdjusterConfig{
		MetricsToWatch: []string{"ErrorRate"},
		StableDuration: 1 * time.Minute,
		Interval:       time.Hour,
		MinLimit:       10,
		MaxLimit:       200,
	}

	a, err := NewAdjuster([]limiter.LimitSetter{ml}, provider, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	a.mu.Lock()
	a.lastAdjustment = time.Now().Add(-2 * time.Minute)
	a.lastHealth = 0
	a.mu.Unlock()

	a.adjust()
	firstLimit := ml.limit

	a.adjust()
	secondLimit := ml.limit

	if firstLimit == 200 {
		t.Fatalf("expected first adjustment to decrease limit, got %d", firstLimit)
	}
	if secondLimit != firstLimit {
		t.Fatalf("hysteresis should block second adjustment: first=%d, second=%d", firstLimit, secondLimit)
	}
}

func TestHysteresis_AllowsAfterCooldown(t *testing.T) {
	ml := &hysteresisLimiter{limit: 200}
	provider := &healthProvider{metrics: Metrics{ErrorRate: 0.9}}
	config := AdjusterConfig{
		MetricsToWatch: []string{"ErrorRate"},
		StableDuration: 100 * time.Millisecond,
		Interval:       time.Hour,
		MinLimit:       10,
		MaxLimit:       200,
	}

	a, err := NewAdjuster([]limiter.LimitSetter{ml}, provider, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	a.mu.Lock()
	a.lastAdjustment = time.Now().Add(-2 * time.Minute)
	a.lastHealth = 0
	a.mu.Unlock()

	a.adjust()
	firstLimit := ml.limit

	a.mu.Lock()
	a.lastAdjustment = time.Now().Add(-2 * time.Minute)
	a.mu.Unlock()

	a.adjust()
	secondLimit := ml.limit

	if firstLimit == 200 {
		t.Fatalf("expected first adjustment, got limit=%d", firstLimit)
	}
	if secondLimit == firstLimit {
		t.Fatalf("expected second adjustment after cooldown, got same limit %d", secondLimit)
	}
}

func TestCircuitBreaker_RevertsOnWorseHealth(t *testing.T) {
	ml := &hysteresisLimiter{limit: 200}
	provider := &healthProvider{metrics: Metrics{ErrorRate: 0.9}}
	config := AdjusterConfig{
		MetricsToWatch: []string{"ErrorRate"},
		StableDuration: 5 * time.Minute,
		Interval:       time.Hour,
		MinLimit:       10,
		MaxLimit:       200,
	}

	a, err := NewAdjuster([]limiter.LimitSetter{ml}, provider, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	a.mu.Lock()
	a.lastHealth = 0
	a.mu.Unlock()

	a.adjust()
	limitAfterFirst := ml.limit

	a.mu.Lock()
	a.lastAdjustment = time.Now().Add(-10 * time.Minute)
	a.mu.Unlock()

	provider.metrics = Metrics{ErrorRate: 1.0}
	a.adjust()
	limitAfterSecond := ml.limit

	if limitAfterSecond != 200 {
		t.Fatalf("circuit breaker should revert to 200. First=%d, Second=%d", limitAfterFirst, limitAfterSecond)
	}
}

func TestCircuitBreaker_ExtendsCooldown(t *testing.T) {
	ml := &hysteresisLimiter{limit: 200}
	provider := &healthProvider{metrics: Metrics{ErrorRate: 0.9}}
	config := AdjusterConfig{
		MetricsToWatch: []string{"ErrorRate"},
		StableDuration: 5 * time.Minute,
		Interval:       time.Hour,
		MinLimit:       10,
		MaxLimit:       200,
	}

	a, err := NewAdjuster([]limiter.LimitSetter{ml}, provider, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	a.mu.Lock()
	a.lastHealth = 0
	a.mu.Unlock()

	a.adjust()

	a.mu.Lock()
	firstCooldown := a.lastAdjustment
	a.mu.Unlock()

	a.mu.Lock()
	a.lastAdjustment = time.Now().Add(-10 * time.Minute)
	a.mu.Unlock()

	provider.metrics = Metrics{ErrorRate: 1.0}
	a.adjust()

	a.mu.Lock()
	secondCooldown := a.lastAdjustment
	a.mu.Unlock()

	expectedMin := firstCooldown.Add(5 * time.Minute)
	if !secondCooldown.After(expectedMin) {
		t.Fatalf("expected extended cooldown (3x stable). First=%v, Second=%v, expected after %v", firstCooldown, secondCooldown, expectedMin)
	}
}

func TestCircuitBreaker_HoldsOnStableHealth(t *testing.T) {
	ml := &hysteresisLimiter{limit: 200}
	provider := &healthProvider{metrics: Metrics{ErrorRate: 0.5}}
	config := AdjusterConfig{
		MetricsToWatch: []string{"ErrorRate"},
		StableDuration: 5 * time.Minute,
		Interval:       time.Hour,
		MinLimit:       10,
		MaxLimit:       200,
	}

	a, err := NewAdjuster([]limiter.LimitSetter{ml}, provider, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	a.mu.Lock()
	a.lastHealth = 50
	a.mu.Unlock()

	a.adjust()
	limitAfterFirst := ml.limit

	a.mu.Lock()
	a.lastHealth = 50
	a.mu.Unlock()

	a.adjust()
	limitAfterSecond := ml.limit

	if limitAfterFirst != limitAfterSecond {
		t.Fatalf("expected stable limit on stable health. First=%d, Second=%d", limitAfterFirst, limitAfterSecond)
	}
}

func TestCircuitBreaker_RevertRestoresPreviousLimit(t *testing.T) {
	ml := &hysteresisLimiter{limit: 200}
	provider := &healthProvider{metrics: Metrics{ErrorRate: 0.9}}
	config := AdjusterConfig{
		MetricsToWatch: []string{"ErrorRate"},
		StableDuration: 5 * time.Minute,
		Interval:       time.Hour,
		MinLimit:       10,
		MaxLimit:       200,
	}

	a, err := NewAdjuster([]limiter.LimitSetter{ml}, provider, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	a.mu.Lock()
	a.lastHealth = 0
	a.mu.Unlock()

	a.adjust()
	limitAfterDecrease := ml.limit

	a.mu.Lock()
	a.lastAdjustment = time.Now().Add(-10 * time.Minute)
	a.mu.Unlock()

	provider.metrics = Metrics{ErrorRate: 1.0}
	a.adjust()
	limitAfterRevert := ml.limit

	if limitAfterRevert != 200 {
		t.Fatalf("expected revert to original 200, got %d", limitAfterRevert)
	}
	_ = limitAfterDecrease
}
