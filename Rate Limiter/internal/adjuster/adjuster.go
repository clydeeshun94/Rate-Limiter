package adjuster // adjuster: automatic rate limit adjustment based on system metrics

import ( // import: standard library and internal imports
	"fmt" // fmt: formatting for error messages and warnings
	"sync" // sync: provides Mutex for thread-safe access
	"time" // time: durations and timestamps for scheduling

	limiter "rate-limiter/internal/limiter" // limiter: rate limiter interfaces (LimitSetter)
	"rate-limiter/pkg/logging" // logging: structured logging interface
)

// Metrics holds system-level measurements used by the Adjuster. // Metrics: input data for health score calculation
type Metrics struct { // Metrics: individual metric values from the system
	ResponseTime time.Duration // ResponseTime: average HTTP response time
	ErrorRate    float64 // ErrorRate: fraction of requests that returned errors (0.0 to 1.0)
	CPUUsage     float64 // CPUUsage: CPU utilization percentage (0.0 to 100.0)
	MemoryUsage  float64 // MemoryUsage: memory utilization percentage (0.0 to 100.0)
	ActiveConns  int // ActiveConns: number of currently active connections
}

// MetricProvider is the interface for fetching system metrics. // MetricProvider: abstraction for metrics sources (Prometheus, etc.)
type MetricProvider interface { // MetricProvider: implemented by metrics.Collector and external providers
	GetMetrics() (Metrics, error) // GetMetrics: fetches current system metrics snapshot
}

// HealthScore represents the overall system health and per-metric breakdown. // HealthScore: output of score calculation
type HealthScore struct { // HealthScore: overall score plus individual metric scores
	Overall  float64 // Overall: weighted average of all metric scores (0.0 to 100.0)
	ByMetric map[string]float64 // ByMetric: individual metric scores keyed by metric name
	Warnings []string // Warnings: human-readable warnings about metric disagreements
}

// Adjustment represents a single limit change decision. // Adjustment: records what changed and why
type Adjustment struct { // Adjustment: diff between old and new limits with justification
	Setter  limiter.LimitSetter // Setter: the rate limiter whose limit changed
	Old     int // Old: previous limit value
	New     int // New: new limit value
	Reason  string // Reason: explanation of why the adjustment was made
}

// AdjustmentStrategy controls how aggressively the Adjuster changes limits. // AdjustmentStrategy: behavior mode for limit changes
type AdjustmentStrategy int // AdjustmentStrategy: enumerated strategy type

const ( // const: strategy values (iota-based enum)
	Conservative AdjustmentStrategy = iota // Conservative: small adjustments (10% max change), slow changes
	Aggressive // Aggressive: larger adjustments (50% max change), fast changes
	Adaptive // Adaptive: moderate adjustments (30% max change), balanced approach
)

// AdjusterConfig holds configuration for the Adjuster. // AdjusterConfig: all tunable parameters for auto-scaling
type AdjusterConfig struct { // AdjusterConfig: configuration struct with defaults applied in NewAdjuster
	MetricsToWatch []string // MetricsToWatch: list of metric names to consider (e.g., "ErrorRate", "ResponseTime")
	Thresholds     map[string]float64 // Thresholds: per-metric thresholds for score calculation (e.g., ResponseTime: 500ms)
	Mode           string // Mode: "auto" or "manual" (only "auto" triggers adjustments)
	Interval       time.Duration // Interval: how often to check metrics and potentially adjust
	MinLimit       int // MinLimit: floor for limit values (never goes below)
	MaxLimit       int // MaxLimit: ceiling for limit values (never goes above)
	StableDuration time.Duration // StableDuration: minimum time between adjustments (prevents oscillation)
	Strategy       AdjustmentStrategy // Strategy: how aggressively to change limits
	MaxChange      float64 // MaxChange: maximum percentage change allowed per adjustment (per strategy)
	Smoothing      float64 // Smoothing: multiplier applied to adjustment amount (lower = smoother)
	Logger         logging.Logger // Logger: structured logger for adjustment events
}

// DefaultWeights defines the default weight for each metric in health score calculation. // DefaultWeights: relative importance of each metric (sums to 1.0)
var DefaultWeights = map[string]float64{ // DefaultWeights: error rate matters most, others less
	"ErrorRate":    0.4, // ErrorRate: 40% weight (most important — directly impacts users)
	"ResponseTime": 0.3, // ResponseTime: 30% weight (user experience)
	"CPUUsage":     0.1, // CPUUsage: 10% weight (infrastructure concern)
	"MemoryUsage":  0.1, // MemoryUsage: 10% weight (infrastructure concern)
	"ActiveConns":  0.1, // ActiveConns: 10% weight (load indicator)
}

// state holds per-limiter runtime state inside the Adjuster. // state: internal tracking for one rate limiter
type state struct { // state: current and previous limit for one limiter
	setter    limiter.LimitSetter // setter: reference to the limiter to adjust
	limit     int // limit: current limit value
	prevLimit int // prevLimit: limit value before last adjustment (for rollback on health degradation)
}

// Adjuster automatically adjusts rate limits based on system metrics. // Adjuster: main auto-scaling controller
type Adjuster struct { // Adjuster: orchestrates metric collection, scoring, and limit adjustment
	states        []state // states: one state per managed rate limiter
	provider      MetricProvider // provider: source of system metrics
	config        AdjusterConfig // config: configuration (validated in NewAdjuster)
	running       bool // running: whether the adjuster is active
	mu            sync.Mutex // mu: protects running, states, and scheduling fields
	ticker        *time.Ticker // ticker: fires at Interval to trigger adjustments
	stop          chan bool // stop: signal channel to stop the run loop
	lastAdjustment time.Time // lastAdjustment: timestamp of most recent limit change
	lastHealth    float64 // lastHealth: previous health score (for degradation detection)
	logger        logging.Logger // logger: structured logger
}

// NewAdjuster creates a new Adjuster with validation and sensible defaults. // NewAdjuster: constructor, validates config, applies defaults
func NewAdjuster( // NewAdjuster: returns configured Adjuster or error if config is invalid
	limiters []limiter.LimitSetter, // limiters: rate limiters to auto-adjust
	provider MetricProvider, // provider: metrics source
	config AdjusterConfig, // config: tuning parameters
) (*Adjuster, error) { // returns: configured Adjuster or error
	if len(config.MetricsToWatch) == 0 { // validate: at least one metric must be configured
		return nil, fmt.Errorf("at least one metric must be configured") // error: nothing to monitor
	}
	hasRequired := false // hasRequired: flag for mandatory metrics
	for _, m := range config.MetricsToWatch { // m: each configured metric name
		if m == "ErrorRate" || m == "ResponseTime" { // ErrorRate and ResponseTime are mandatory
			hasRequired = true // mark as having required metrics
			break // exit loop early once found
		}
	}
	if !hasRequired { // ErrorRate or ResponseTime must be configured
		return nil, fmt.Errorf("at least one non-negotiable metric (ErrorRate or ResponseTime) must be configured") // error: mandatory metric missing
	}
	if config.StableDuration <= 0 { // if not set or non-positive
		config.StableDuration = 5 * time.Minute // default: 5 minutes between adjustments
	}
	if config.Interval <= 0 { // if not set or non-positive
		config.Interval = 30 * time.Second // default: check every 30 seconds
	}
	if config.MinLimit <= 0 { // must be positive (at least 1 request allowed)
		config.MinLimit = 1 // default: minimum 1 request per window
	}
	if config.MaxLimit <= config.MinLimit { // max must exceed min
		return nil, fmt.Errorf("MaxLimit must be greater than MinLimit") // error: invalid range
	}
	if config.Mode == "aggressive" && config.Strategy == 0 { // if mode is aggressive but strategy not set
		config.Strategy = Aggressive // auto-set strategy to Aggressive
	}
	if config.Strategy == 0 { // if strategy not explicitly set
		config.Strategy = Conservative // default to safest option
	}
	if config.MaxChange <= 0 { // if not set, apply strategy defaults
		switch config.Strategy { // Strategy determines default MaxChange
		case Aggressive: // Aggressive: allow up to 50% change
			config.MaxChange = 0.50
		case Adaptive: // Adaptive: allow up to 30% change
			config.MaxChange = 0.30
		default: // Conservative: allow up to 10% change
			config.MaxChange = 0.10
		}
	}
	if config.Logger == nil { // if no logger provided
		config.Logger = &logging.NoOpLogger{} // use NoOpLogger (silent)
	}
	if config.Smoothing <= 0 { // if not set or non-positive
		config.Smoothing = 1.0 // no smoothing (raw adjustment amount)
	}

	states := make([]state, len(limiters)) // states: initialize per-limiter state
	for i, l := range limiters { // i: index; l: each limiter
		states[i] = state{setter: l, limit: config.MaxLimit} // start each limiter at MaxLimit
	}

	return &Adjuster{ // return: configured Adjuster instance
		states:         states, // states: per-limiter state tracking
		provider:       provider, // provider: metrics source
		config:         config, // config: validated and defaulted config
		lastAdjustment: time.Time{}, // lastAdjustment: zero time (no adjustment yet)
		lastHealth:     0, // lastHealth: zero (no health score yet)
		stop:           make(chan bool), // stop: buffered channel for stop signal
		logger:         config.Logger, // logger: configured logger
	}, nil // nil: no error
}

// Start begins the periodic adjustment loop in a goroutine. // Start: runs adjuster in background, checks metrics at Interval
func (a *Adjuster) Start() { // Start: no return value; goroutine runs until Stop()
	a.mu.Lock() // a.mu.Lock(): acquire lock to safely set running flag
	a.running = true // a.running = true: mark adjuster as active
	a.mu.Unlock() // a.mu.Unlock(): release lock
	a.ticker = time.NewTicker(a.config.Interval) // ticker: create periodic timer
	go a.run() // go a.run(): start adjustment loop in background goroutine
}

// Stop halts the adjustment loop. // Stop: cleanly shuts down background goroutine
func (a *Adjuster) Stop() { // Stop: blocks until goroutine exits
	a.mu.Lock() // a.mu.Lock(): acquire lock for safe shutdown
	defer a.mu.Unlock() // defer a.mu.Unlock(): release lock when done
	if !a.running { // if already stopped
		return // return: nothing to do
	}
	a.running = false // a.running = false: mark as stopped
	a.ticker.Stop() // stop the ticker
	a.stop <- true // send stop signal to run() loop
}

// run is the main adjustment loop, executing in a goroutine. // run: selects on ticker and stop channels
func (a *Adjuster) run() { // run: infinite loop until stop signal received
	for { // infinite loop
		select { // wait for either ticker or stop
		case <-a.ticker.C: // a.ticker.C: interval elapsed
			a.adjust() // a.adjust(): perform one adjustment cycle
		case <-a.stop: // a.stop: stop signal received
			return // return: exit goroutine
		}
	}
}

// adjust performs one adjustment cycle: fetch metrics, score health, adjust limits. // adjust: core logic — metrics → scores → limit changes
func (a *Adjuster) adjust() { // adjust: called periodically by run()
	a.mu.Lock() // a.mu.Lock(): lock for checking lastAdjustment
	if time.Since(a.lastAdjustment) < a.config.StableDuration { // if too soon since last adjustment
		a.mu.Unlock() // a.mu.Unlock(): release lock and skip this cycle
		return // return: wait for next interval
	}
	a.mu.Unlock() // a.mu.Unlock(): release lock for metrics fetch (not holding during I/O)

	metrics, err := a.provider.GetMetrics() // metrics: fetch current system metrics
	if err != nil { // if metrics fetch failed
		return // return: skip this cycle (try again next interval)
	}

	scores := a.calculateScores(metrics) // scores: compute per-metric health scores
	warnings := a.checkWarnings(scores) // warnings: detect metric disagreements
	health := scores.Overall // health: weighted overall score (0-100)

	a.mu.Lock() // a.mu.Lock(): re-lock for adjustment decisions
	defer a.mu.Unlock() // defer a.mu.Unlock(): ensure lock released

	if health < a.lastHealth { // if system health degraded since last check
		for i := range a.states { // i: each limiter state
			a.states[i].setter.SetLimit(a.states[i].prevLimit) // rollback to previous limit (HYSTERESIS)
			a.states[i].limit = a.states[i].prevLimit // update tracked limit
		}
		a.lastAdjustment = time.Now().Add(a.config.StableDuration * 3) // extend cooldown 3x (prevent rapid flip-flop)
		a.lastHealth = health // update tracked health
		for _, w := range warnings { // w: each warning
			a.logger.Warn(w, nil) // log warnings
		}
		return // return: done — hysteresis mode (rollback complete)
	}

	for i := range a.states { // i: each limiter state
		oldLimit := a.states[i].limit // oldLimit: current limit for this limiter
		newLimit := a.CalculateNewLimit(float64(oldLimit), health) // newLimit: computed limit based on health
		if newLimit != oldLimit { // if limit actually changes
			a.states[i].prevLimit = oldLimit // save current as previous (for rollback on degradation)
			a.states[i].setter.SetLimit(newLimit) // apply new limit to limiter
			a.states[i].limit = newLimit // update tracked limit
		}
	}

	a.lastAdjustment = time.Now() // a.lastAdjustment: record time of this adjustment
	a.lastHealth = health // a.lastHealth: record health score

	if len(warnings) > 0 { // if any warnings were generated
		for _, w := range warnings { // w: each warning
			a.logger.Warn(w, nil) // log each warning
		}
	}
}

// CalculateScores is the public wrapper for calculateScores. // CalculateScores: exported wrapper (use calculateScores internally)
func (a *Adjuster) CalculateScores(m Metrics) HealthScore { // CalculateScores: computes per-metric and overall health scores
	return a.calculateScores(m) // delegate to private method
}

// CalculateNewLimit is the public wrapper for calculateNewLimit. // CalculateNewLimit: exported wrapper (use calculateNewLimit internally)
func (a *Adjuster) CalculateNewLimit(current, health float64) int { // CalculateNewLimit: computes new limit based on current limit and health
	return a.calculateNewLimit(current, health) // delegate to private method
}

// calculateScores computes per-metric health scores and weighted overall score. // calculateScores: core scoring logic
func (a *Adjuster) calculateScores(m Metrics) HealthScore { // calculateScores: returns HealthScore with all metric scores
	scores := make(map[string]float64) // scores: per-metric score results

	if a.config.Thresholds == nil { // if thresholds not configured
		a.config.Thresholds = map[string]float64{ // apply defaults
			"ResponseTime": 500, // ResponseTime threshold: 500ms
			"ActiveConns":  1000, // ActiveConns threshold: 1000 connections
		}
	}

	// ErrorRate: binary score (0 or 100) based on whether rate is in acceptable range
	if _, ok := scores["ErrorRate"]; ok || contains(a.config.MetricsToWatch, "ErrorRate") { // ErrorRate: always included if in watchlist
		if m.ErrorRate < 0 { // clamp negative values
			m.ErrorRate = 0
		}
		if m.ErrorRate > 1 { // clamp above 100%
			m.ErrorRate = 1
		}
		scores["ErrorRate"] = (1 - m.ErrorRate) * 100 // ErrorRate score: 100 if no errors, 0 if all errors
	}

	// ResponseTime: linear interpolation between threshold and 2x threshold
	if contains(a.config.MetricsToWatch, "ResponseTime") { // ResponseTime: only if in watchlist
		threshold := a.config.Thresholds["ResponseTime"] // threshold: configured or default (500ms)
		if threshold <= 0 { // safety check
			threshold = 500 // default threshold
		}
		rt := float64(m.ResponseTime.Milliseconds()) // rt: response time in milliseconds
		var score float64 // score: computed score
		if rt <= threshold { // if within threshold
			score = 100 // perfect score
		} else if rt >= threshold*2 { // if double threshold
			score = 0 // zero score
		} else { // linear interpolation between threshold and 2x threshold
			score = 100 * (1 - (rt-threshold)/threshold) // score: linearly decreasing
		}
		scores["ResponseTime"] = score // store response time score
	}

	// CPUUsage: inverse (100% usage = 0 score)
	if contains(a.config.MetricsToWatch, "CPUUsage") { // CPUUsage: only if in watchlist
		cpu := m.CPUUsage // cpu: CPU percentage
		if cpu < 0 { // clamp negative
			cpu = 0
		}
		if cpu > 100 { // clamp above 100
			cpu = 100
		}
		scores["CPUUsage"] = 100 - cpu // CPUUsage score: 100 at idle, 0 at full usage
	}

	// MemoryUsage: inverse (100% usage = 0 score)
	if contains(a.config.MetricsToWatch, "MemoryUsage") { // MemoryUsage: only if in watchlist
		mem := m.MemoryUsage // mem: memory percentage
		if mem < 0 { // clamp negative
			mem = 0
		}
		if mem > 100 { // clamp above 100
			mem = 100
		}
		scores["MemoryUsage"] = 100 - mem // MemoryUsage score: 100 at idle, 0 at full usage
	}

	// ActiveConns: linear interpolation between threshold and 2x threshold (like ResponseTime)
	if contains(a.config.MetricsToWatch, "ActiveConns") { // ActiveConns: only if in watchlist
		threshold := a.config.Thresholds["ActiveConns"] // threshold: configured or default (1000)
		if threshold <= 0 { // safety check
			threshold = 1000 // default threshold
		}
		conns := float64(m.ActiveConns) // conns: active connection count
		var score float64 // score: computed score
		if conns <= threshold { // if within threshold
			score = 100 // perfect score
		} else if conns >= threshold*2 { // if double threshold
			score = 0 // zero score
		} else { // linear interpolation
			score = 100 * (1 - (conns-threshold)/threshold) // score: linearly decreasing
		}
		scores["ActiveConns"] = score // store connection count score
	}

	// Compute weighted overall score
	overall := 0.0 // overall: weighted average accumulator
	weightSum := 0.0 // weightSum: total weights (normalization)
	for metric, score := range scores { // metric: each metric name; score: its score
		weight := DefaultWeights[metric] // weight: importance factor for this metric
		overall += score * weight // accumulate weighted score
		weightSum += weight // accumulate weights
	}
	if weightSum > 0 { // avoid division by zero
		overall = overall / weightSum // normalize to weighted average
	}

	return HealthScore{ // return: complete health score
		Overall:  overall, // Overall: weighted average score (0-100)
		ByMetric: scores, // ByMetric: individual metric scores
	}
}

// checkWarnings detects large disagreements between metric scores. // checkWarnings: identifies concerning metric imbalances
func (a *Adjuster) checkWarnings(scores HealthScore) []string { // checkWarnings: returns human-readable warning strings
	var warnings []string // warnings: collected warning messages
	metrics := make([]string, 0, len(scores.ByMetric)) // metrics: list of metric names
	for m := range scores.ByMetric { // m: each metric name
		metrics = append(metrics, m) // collect metric names
	}
	for i := 0; i < len(metrics); i++ { // i: first metric index
		for j := i + 1; j < len(metrics); j++ { // j: second metric index (pairwise comparison)
			diff := scores.ByMetric[metrics[i]] - scores.ByMetric[metrics[j]] // diff: score difference
			if diff < 0 { // absolute value
				diff = -diff
			}
			if diff > 40 { // if difference exceeds 40 points
				warnings = append(warnings, fmt.Sprintf( // add warning message
					"Metric disagreement: %s (%.0f) vs %s (%.0f) — difference of %.0f points. Consider adjusting weights or investigating.",
					metrics[i], scores.ByMetric[metrics[i]], // metrics[i]: first metric name and score
					metrics[j], scores.ByMetric[metrics[j]], // metrics[j]: second metric name and score
					diff, // diff: absolute difference
				))
			}
		}
	}
	return warnings // return: all warnings generated
}

// calculateNewLimit computes the adjusted limit based on health and strategy. // calculateNewLimit: core adjustment formula
func (a *Adjuster) calculateNewLimit(current, health float64) int { // calculateNewLimit: returns new limit (clamped to Min/Max)
	limit := int(current) // limit: current limit (integer)

	var baseRate float64 // baseRate: percentage change per cycle
	if health > 70 { // healthy: increase limit (system can handle more)
		baseRate = a.getIncreaseRate() // getIncreaseRate: strategy-dependent increase percentage
	} else if health < 30 { // unhealthy: decrease limit (system is struggling)
		baseRate = a.getDecreaseRate() // getDecreaseRate: strategy-dependent decrease percentage
	} else { // moderate health (30-70): no change needed
		return limit // return current limit unchanged
	}

	adjustment := float64(limit) * baseRate * a.config.Smoothing // adjustment: raw change amount

	maxChange := float64(limit) * a.config.MaxChange // maxChange: maximum allowed change
	if adjustment > maxChange { // cap at maximum increase
		adjustment = maxChange
	}
	if adjustment < -maxChange { // cap at maximum decrease
		adjustment = -maxChange
	}

	newLimit := limit + int(adjustment) // newLimit: apply adjustment
	if newLimit > a.config.MaxLimit { // clamp to maximum
		newLimit = a.config.MaxLimit
	}
	if newLimit < a.config.MinLimit { // clamp to minimum
		newLimit = a.config.MinLimit
	}
	return newLimit // return: clamped new limit
}

// getIncreaseRate returns the increase percentage based on strategy. // getIncreaseRate: strategy-driven increase rate
func (a *Adjuster) getIncreaseRate() float64 { // getIncreaseRate: returns 0.10-0.20 depending on strategy
	switch a.config.Strategy { // Strategy determines increase rate
	case Aggressive: // Aggressive: 20% increase per cycle
		return 0.20
	case Adaptive: // Adaptive: 15% increase per cycle
		return 0.15
	default: // Conservative: 10% increase per cycle
		return 0.10
	}
}

// getDecreaseRate returns the decrease percentage based on strategy. // getDecreaseRate: strategy-driven decrease rate
func (a *Adjuster) getDecreaseRate() float64 { // getDecreaseRate: returns -0.10 to -0.50 depending on strategy
	switch a.config.Strategy { // Strategy determines decrease rate
	case Aggressive: // Aggressive: 50% decrease per cycle (fast response to problems)
		return -0.50
	case Adaptive: // Adaptive: 30% decrease per cycle
		return -0.30
	default: // Conservative: 10% decrease per cycle (gentle response)
		return -0.10
	}
}

// contains checks if a string exists in a slice. // contains: utility function (not a method)
func contains(slice []string, item string) bool { // contains: linear search, returns true if found
	for _, s := range slice { // s: each element
		if s == item { // match found
			return true // return true
		}
	}
	return false // no match found, return false
}

/*
================================================================================
ARCHITECTURAL / ENGINEERING DECISIONS
================================================================================

1. HYSTERESIS (ROLLBACK ON HEALTH DEGRADATION)
   - When health score drops below the previous check's score, all limits are rolled back to prevLimit.
   - Additionally, lastAdjustment is pushed forward by StableDuration * 3 (3x cooldown).
   - Decision: prevents oscillation when health hovers near the adjustment threshold.
   - Example: limit increased at T=0, health drops at T=30s → rollback and wait until T=30s + 15min (3 × 5min) before next adjustment.
   - Why: without this, the system could flip-flop between increase/decrease every cycle.

2. HYGIEA STRATEGY (SLOW AND STABLE)
   - Conservative default (10% max change, 5min minimum interval, 30s check interval).
   - Decision: rate limit changes should be gradual to avoid user impact.
   - Aggressive mode available for systems that need faster adaptation.
   - StableDuration prevents rapid changes; 5 minutes is substantial for rate limiting.

3. REQUIRED METRICS (ErrorRate and ResponseTime)
   - These two metrics are mandatory; at least one must be configured.
   - Decision: ErrorRate is the most important user-facing metric (failures). ResponseTime is the most important performance metric.
   - Other metrics (CPU, Memory, ActiveConns) are optional infrastructure indicators.

4. WEIGHTED HEALTH SCORE
   - Default weights: ErrorRate=0.4, ResponseTime=0.3, CPU=0.1, Memory=0.1, Conns=0.1.
   - Decision: errors affect users most directly, then response time. Infrastructure metrics are secondary.
   - Users notice errors and latency before noticing CPU/memory.

5. SEPARATE LOCKING STRATEGY
   - adjust() releases the lock during provider.GetMetrics() (I/O operation).
   - Decision: GetMetrics may take 100ms+ (HTTP call to Prometheus). Holding the lock would block Start/Stop.
   - Lock is re-acquired for all state mutations and scoring.

6. HEALTH THRESHOLDS (>70 healthy, <30 unhealthy, 30-70 stable)
   - 3-tier system: increase (health>70), decrease (health<30), hold (30-70).
   - Decision: wide "no change" zone prevents unnecessary adjustments when system is in flux.
   - Thresholds are not configurable (would add complexity); defaults are well-tested.

7. EXTENDED COOLDOWN ON ROLLBACK (StableDuration * 3)
   - After a rollback, the adjuster waits 3× longer before the next adjustment.
   - Decision: rollback means system is unstable. Aggressive re-adjustment could worsen the situation.
   - This is the key hysteresis mechanism that prevents oscillation.

8. STOP CHANNEL VS CONTEXT
   - Uses chan bool for stop signal instead of context.Context.
   - Decision: simpler for a single goroutine with a single stop condition. Context would be better for multiple goroutines.

9. PRELOAD STRATEGY (start at MaxLimit)
   - New adjuster starts each limiter at config.MaxLimit.
   - Decision: avoids under-provisioning on startup (users prefer slightly fast initial response).
   - Adjustments will decrease limits if system metrics indicate strain.

10. ADJUSTMENT LOGGING
    - Warnings are logged via configured Logger (NoOpLogger by default).
    - Decision: callers must configure a real logger (e.g., SimpleLogger) to see adjustment decisions.
    - No logging of normal adjustments — only warnings are logged to avoid log noise.
*/
