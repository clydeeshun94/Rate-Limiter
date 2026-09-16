package adjuster

import (
	"testing"
	"time"

	limiter "rate-limiter/internal/limiter"
)

func TestStrategy_Conservative(t *testing.T) {
	ml := &hysteresisLimiter{limit: 100}
	provider := &healthProvider{metrics: Metrics{ErrorRate: 0.01}}
	config := AdjusterConfig{
		MetricsToWatch: []string{"ErrorRate"},
		Strategy:       Conservative,
		Interval:       time.Hour,
		MinLimit:       10,
		MaxLimit:       200,
	}

	a, err := NewAdjuster([]limiter.LimitSetter{ml}, provider, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newLimit := a.CalculateNewLimit(100, 85)
	if newLimit != 110 {
		t.Fatalf("Conservative increase: expected 110 (10%%), got %d", newLimit)
	}

	newLimit = a.CalculateNewLimit(110, 20)
	if newLimit != 99 {
		t.Fatalf("Conservative decrease: expected 99 (10%% down), got %d", newLimit)
	}
}

func TestStrategy_Aggressive(t *testing.T) {
	ml := &hysteresisLimiter{limit: 100}
	provider := &healthProvider{metrics: Metrics{ErrorRate: 0.01}}
	config := AdjusterConfig{
		MetricsToWatch: []string{"ErrorRate"},
		Strategy:       Aggressive,
		Interval:       time.Hour,
		MinLimit:       10,
		MaxLimit:       200,
	}

	a, err := NewAdjuster([]limiter.LimitSetter{ml}, provider, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newLimit := a.CalculateNewLimit(100, 85)
	if newLimit != 120 {
		t.Fatalf("Aggressive increase: expected 120 (20%%), got %d", newLimit)
	}

	newLimit = a.CalculateNewLimit(100, 20)
	if newLimit != 50 {
		t.Fatalf("Aggressive decrease: expected 50 (50%% down), got %d", newLimit)
	}
}

func TestStrategy_Adaptive(t *testing.T) {
	ml := &hysteresisLimiter{limit: 100}
	provider := &healthProvider{metrics: Metrics{ErrorRate: 0.01}}
	config := AdjusterConfig{
		MetricsToWatch: []string{"ErrorRate"},
		Strategy:       Adaptive,
		Interval:       time.Hour,
		MinLimit:       10,
		MaxLimit:       200,
	}

	a, err := NewAdjuster([]limiter.LimitSetter{ml}, provider, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newLimit := a.CalculateNewLimit(100, 85)
	if newLimit != 115 {
		t.Fatalf("Adaptive increase: expected 115 (15%%), got %d", newLimit)
	}

	newLimit = a.CalculateNewLimit(100, 20)
	if newLimit != 70 {
		t.Fatalf("Adaptive decrease: expected 70 (30%% down), got %d", newLimit)
	}
}

func TestMaxChange_CapsAdjustment(t *testing.T) {
	ml := &hysteresisLimiter{limit: 100}
	provider := &healthProvider{metrics: Metrics{ErrorRate: 0.01}}
	config := AdjusterConfig{
		MetricsToWatch: []string{"ErrorRate"},
		Strategy:       Aggressive,
		MaxChange:      0.10,
		Interval:       time.Hour,
		MinLimit:       10,
		MaxLimit:       200,
	}

	a, err := NewAdjuster([]limiter.LimitSetter{ml}, provider, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newLimit := a.CalculateNewLimit(100, 85)
	if newLimit != 110 {
		t.Fatalf("MaxChange cap: expected 110 (10%% from 20%%), got %d", newLimit)
	}

	newLimit = a.CalculateNewLimit(100, 20)
	if newLimit != 90 {
		t.Fatalf("MaxChange cap decrease: expected 90 (10%% from 50%%), got %d", newLimit)
	}
}

func TestSmoothing_ReducesAdjustment(t *testing.T) {
	ml := &hysteresisLimiter{limit: 100}
	provider := &healthProvider{metrics: Metrics{ErrorRate: 0.01}}
	config := AdjusterConfig{
		MetricsToWatch: []string{"ErrorRate"},
		Strategy:       Conservative,
		Smoothing:      0.5,
		Interval:       time.Hour,
		MinLimit:       10,
		MaxLimit:       200,
	}

	a, err := NewAdjuster([]limiter.LimitSetter{ml}, provider, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newLimit := a.CalculateNewLimit(100, 85)
	if newLimit != 105 {
		t.Fatalf("Smoothing: expected 105 (10%% * 0.5 = 5%%), got %d", newLimit)
	}
}

func TestHealthHold_NoAdjustment(t *testing.T) {
	ml := &hysteresisLimiter{limit: 100}
	provider := &healthProvider{metrics: Metrics{ErrorRate: 0.5}}
	config := AdjusterConfig{
		MetricsToWatch: []string{"ErrorRate"},
		Strategy:       Conservative,
		Interval:       time.Hour,
		MinLimit:       10,
		MaxLimit:       200,
	}

	a, err := NewAdjuster([]limiter.LimitSetter{ml}, provider, config)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	newLimit := a.CalculateNewLimit(100, 50)
	if newLimit != 100 {
		t.Fatalf("Hold: expected 100 (no change), got %d", newLimit)
	}
}
