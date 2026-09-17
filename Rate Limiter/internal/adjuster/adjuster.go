package adjuster

import (
	"fmt"
	"sync"
	"time"

	limiter "rate-limiter/internal/limiter"
	"rate-limiter/pkg/logging"
)

type Metrics struct {
	ResponseTime time.Duration
	ErrorRate    float64
	CPUUsage     float64
	MemoryUsage  float64
	ActiveConns  int
}

type MetricProvider interface {
	GetMetrics() (Metrics, error)
}

type HealthScore struct {
	Overall  float64
	ByMetric map[string]float64
	Warnings []string
}

type Adjustment struct {
	Setter  limiter.LimitSetter
	Old     int
	New     int
	Reason  string
}

type AdjustmentStrategy int

const (
	Conservative AdjustmentStrategy = iota
	Aggressive
	Adaptive
)

type AdjusterConfig struct {
	MetricsToWatch []string
	Thresholds     map[string]float64
	Mode           string
	Interval       time.Duration
	MinLimit       int
	MaxLimit       int
	StableDuration time.Duration
	Strategy       AdjustmentStrategy
	MaxChange      float64
	Smoothing      float64
	Logger         logging.Logger
}

var DefaultWeights = map[string]float64{
	"ErrorRate":    0.4,
	"ResponseTime": 0.3,
	"CPUUsage":     0.1,
	"MemoryUsage":  0.1,
	"ActiveConns":  0.1,
}

type state struct {
	setter    limiter.LimitSetter
	limit     int
	prevLimit int
}

type Adjuster struct {
	states        []state
	provider      MetricProvider
	config        AdjusterConfig
	running       bool
	mu            sync.Mutex
	ticker        *time.Ticker
	stop          chan bool
	lastAdjustment time.Time
	lastHealth    float64
	logger        logging.Logger
}

func NewAdjuster(
	limiters []limiter.LimitSetter,
	provider MetricProvider,
	config AdjusterConfig,
) (*Adjuster, error) {
	if len(config.MetricsToWatch) == 0 {
		return nil, fmt.Errorf("at least one metric must be configured")
	}
	hasRequired := false
	for _, m := range config.MetricsToWatch {
		if m == "ErrorRate" || m == "ResponseTime" {
			hasRequired = true
			break
		}
	}
	if !hasRequired {
		return nil, fmt.Errorf("at least one non-negotiable metric (ErrorRate or ResponseTime) must be configured")
	}
	if config.StableDuration <= 0 {
		config.StableDuration = 5 * time.Minute
	}
	if config.Interval <= 0 {
		config.Interval = 30 * time.Second
	}
	if config.MinLimit <= 0 {
		config.MinLimit = 1
	}
	if config.MaxLimit <= config.MinLimit {
		return nil, fmt.Errorf("MaxLimit must be greater than MinLimit")
	}
	if config.Mode == "aggressive" && config.Strategy == 0 {
		config.Strategy = Aggressive
	}
	if config.Strategy == 0 {
		config.Strategy = Conservative
	}
	if config.MaxChange <= 0 {
		switch config.Strategy {
		case Aggressive:
			config.MaxChange = 0.50
		case Adaptive:
			config.MaxChange = 0.30
		default:
			config.MaxChange = 0.10
		}
	}
	if config.Logger == nil {
		config.Logger = &logging.NoOpLogger{}
	}
	if config.Smoothing <= 0 {
		config.Smoothing = 1.0
	}

	states := make([]state, len(limiters))
	for i, l := range limiters {
		states[i] = state{setter: l, limit: config.MaxLimit}
	}

	return &Adjuster{
		states:         states,
		provider:       provider,
		config:         config,
		lastAdjustment: time.Time{},
		lastHealth:     0,
		stop:           make(chan bool),
		logger:         config.Logger,
	}, nil
}

func (a *Adjuster) Start() {
	a.mu.Lock()
	a.running = true
	a.mu.Unlock()
	a.ticker = time.NewTicker(a.config.Interval)
	go a.run()
}

func (a *Adjuster) Stop() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.running {
		return
	}
	a.running = false
	a.ticker.Stop()
	a.stop <- true
}

func (a *Adjuster) run() {
	for {
		select {
		case <-a.ticker.C:
			a.adjust()
		case <-a.stop:
			return
		}
	}
}

func (a *Adjuster) adjust() {
	a.mu.Lock()
	if time.Since(a.lastAdjustment) < a.config.StableDuration {
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()

	metrics, err := a.provider.GetMetrics()
	if err != nil {
		return
	}

	scores := a.calculateScores(metrics)
	warnings := a.checkWarnings(scores)
	health := scores.Overall

	a.mu.Lock()
	defer a.mu.Unlock()

	if health < a.lastHealth {
		for i := range a.states {
			a.states[i].setter.SetLimit(a.states[i].prevLimit)
			a.states[i].limit = a.states[i].prevLimit
		}
		a.lastAdjustment = time.Now().Add(a.config.StableDuration * 3)
		a.lastHealth = health
		for _, w := range warnings {
			a.logger.Warn(w, nil)
		}
		return
	}

	for i := range a.states {
		oldLimit := a.states[i].limit
		newLimit := a.CalculateNewLimit(float64(oldLimit), health)
		if newLimit != oldLimit {
			a.states[i].prevLimit = oldLimit
			a.states[i].setter.SetLimit(newLimit)
			a.states[i].limit = newLimit
		}
	}

	a.lastAdjustment = time.Now()
	a.lastHealth = health

	if len(warnings) > 0 {
		for _, w := range warnings {
			a.logger.Warn(w, nil)
		}
	}
}

func (a *Adjuster) CalculateScores(m Metrics) HealthScore {
	return a.calculateScores(m)
}

func (a *Adjuster) CalculateNewLimit(current, health float64) int {
	return a.calculateNewLimit(current, health)
}

func (a *Adjuster) calculateScores(m Metrics) HealthScore {
	scores := make(map[string]float64)

	if a.config.Thresholds == nil {
		a.config.Thresholds = map[string]float64{
			"ResponseTime": 500,
			"ActiveConns":  1000,
		}
	}

	if _, ok := scores["ErrorRate"]; ok || contains(a.config.MetricsToWatch, "ErrorRate") {
		if m.ErrorRate < 0 {
			m.ErrorRate = 0
		}
		if m.ErrorRate > 1 {
			m.ErrorRate = 1
		}
		scores["ErrorRate"] = (1 - m.ErrorRate) * 100
	}

	if contains(a.config.MetricsToWatch, "ResponseTime") {
		threshold := a.config.Thresholds["ResponseTime"]
		if threshold <= 0 {
			threshold = 500
		}
		rt := float64(m.ResponseTime.Milliseconds())
		var score float64
		if rt <= threshold {
			score = 100
		} else if rt >= threshold*2 {
			score = 0
		} else {
			score = 100 * (1 - (rt-threshold)/threshold)
		}
		scores["ResponseTime"] = score
	}

	if contains(a.config.MetricsToWatch, "CPUUsage") {
		cpu := m.CPUUsage
		if cpu < 0 {
			cpu = 0
		}
		if cpu > 100 {
			cpu = 100
		}
		scores["CPUUsage"] = 100 - cpu
	}

	if contains(a.config.MetricsToWatch, "MemoryUsage") {
		mem := m.MemoryUsage
		if mem < 0 {
			mem = 0
		}
		if mem > 100 {
			mem = 100
		}
		scores["MemoryUsage"] = 100 - mem
	}

	if contains(a.config.MetricsToWatch, "ActiveConns") {
		threshold := a.config.Thresholds["ActiveConns"]
		if threshold <= 0 {
			threshold = 1000
		}
		conns := float64(m.ActiveConns)
		var score float64
		if conns <= threshold {
			score = 100
		} else if conns >= threshold*2 {
			score = 0
		} else {
			score = 100 * (1 - (conns-threshold)/threshold)
		}
		scores["ActiveConns"] = score
	}

	overall := 0.0
	weightSum := 0.0
	for metric, score := range scores {
		weight := DefaultWeights[metric]
		overall += score * weight
		weightSum += weight
	}
	if weightSum > 0 {
		overall = overall / weightSum
	}

	return HealthScore{
		Overall:  overall,
		ByMetric: scores,
	}
}

func (a *Adjuster) checkWarnings(scores HealthScore) []string {
	var warnings []string
	metrics := make([]string, 0, len(scores.ByMetric))
	for m := range scores.ByMetric {
		metrics = append(metrics, m)
	}
	for i := 0; i < len(metrics); i++ {
		for j := i + 1; j < len(metrics); j++ {
			diff := scores.ByMetric[metrics[i]] - scores.ByMetric[metrics[j]]
			if diff < 0 {
				diff = -diff
			}
			if diff > 40 {
				warnings = append(warnings, fmt.Sprintf(
					"Metric disagreement: %s (%.0f) vs %s (%.0f) — difference of %.0f points. Consider adjusting weights or investigating.",
					metrics[i], scores.ByMetric[metrics[i]],
					metrics[j], scores.ByMetric[metrics[j]],
					diff,
				))
			}
		}
	}
	return warnings
}

func (a *Adjuster) calculateNewLimit(current, health float64) int {
	limit := int(current)

	var baseRate float64
	if health > 70 {
		baseRate = a.getIncreaseRate()
	} else if health < 30 {
		baseRate = a.getDecreaseRate()
	} else {
		return limit
	}

	adjustment := float64(limit) * baseRate * a.config.Smoothing

	maxChange := float64(limit) * a.config.MaxChange
	if adjustment > maxChange {
		adjustment = maxChange
	}
	if adjustment < -maxChange {
		adjustment = -maxChange
	}

	newLimit := limit + int(adjustment)
	if newLimit > a.config.MaxLimit {
		newLimit = a.config.MaxLimit
	}
	if newLimit < a.config.MinLimit {
		newLimit = a.config.MinLimit
	}
	return newLimit
}

func (a *Adjuster) getIncreaseRate() float64 {
	switch a.config.Strategy {
	case Aggressive:
		return 0.20
	case Adaptive:
		return 0.15
	default:
		return 0.10
	}
}

func (a *Adjuster) getDecreaseRate() float64 {
	switch a.config.Strategy {
	case Aggressive:
		return -0.50
	case Adaptive:
		return -0.30
	default:
		return -0.10
	}
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
