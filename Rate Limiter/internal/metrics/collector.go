package metrics // metrics: observability metrics collection for rate limiter

import ( // import: standard library imports
	"fmt" // fmt: formatting for Prometheus text output
	"sync" // sync: provides Mutex for thread-safe map access
	"time" // time: duration type for timing measurements
)

// Collector accumulates rate limiter metrics for observability and Prometheus export. // Collector: thread-safe metrics accumulator
type Collector struct { // Collector: tracks allowed/denied counts, limits, and timing per identity
	mu            sync.Mutex // mu: protects all fields from concurrent access
	allowedTotal  map[string]int64 // allowedTotal: cumulative allowed request counts per identity
	deniedTotal   map[string]int64 // deniedTotal: cumulative denied request counts per identity
	limits        map[string]int // limits: current limit per algorithm name
	totalChecks   int64 // totalChecks: total number of Check() calls (allowed + denied)
	totalDuration float64 // totalDuration: cumulative check duration in seconds
}

// NewCollector creates a new Collector with empty maps. // NewCollector: constructor, initializes all tracking maps
func NewCollector() *Collector { // NewCollector: returns pointer to initialized Collector
	return &Collector{ // return: new instance
		allowedTotal: make(map[string]int64), // allowedTotal: empty identity → count map
		deniedTotal:  make(map[string]int64), // deniedTotal: empty identity → count map
		limits:       make(map[string]int), // limits: empty algorithm → limit map
	}
}

// RecordAllowed increments the allowed request count for an identity. // RecordAllowed: thread-safe increment
func (c *Collector) RecordAllowed(identity string) { // RecordAllowed: also increments total check count
	c.mu.Lock() // c.mu.Lock(): acquire lock for safe map write
	defer c.mu.Unlock() // defer c.mu.Unlock(): release lock after update
	c.allowedTotal[identity]++ // increment allowed count for this identity
	c.totalChecks++ // increment total check counter
}

// RecordDenied increments the denied request count for an identity. // RecordDenied: thread-safe increment
func (c *Collector) RecordDenied(identity string) { // RecordDenied: also increments total check count
	c.mu.Lock() // c.mu.Lock(): acquire lock for safe map write
	defer c.mu.Unlock() // defer c.mu.Unlock(): release lock after update
	c.deniedTotal[identity]++ // increment denied count for this identity
	c.totalChecks++ // increment total check counter
}

// RecordDuration adds a check duration to the cumulative total. // RecordDuration: accumulates timing data
func (c *Collector) RecordDuration(d time.Duration) { // RecordDuration: converts to seconds before accumulating
	c.mu.Lock() // c.mu.Lock(): acquire lock for safe map write
	defer c.mu.Unlock() // defer c.mu.Unlock(): release lock after update
	c.totalDuration += d.Seconds() // add duration in seconds to total
}

// SetLimit records the current limit for a given algorithm. // SetLimit: tracks active limits per algorithm
func (c *Collector) SetLimit(algorithm string, limit int) { // SetLimit: updates limit for one algorithm
	c.mu.Lock() // c.mu.Lock(): acquire lock for safe map write
	defer c.mu.Unlock() // defer c.mu.Unlock(): release lock after update
	c.limits[algorithm] = limit // store limit for this algorithm name
}

// ToPrometheusText returns metrics in Prometheus text exposition format. // ToPrometheusText: scrapable format for Prometheus
func (c *Collector) ToPrometheusText() string { // ToPrometheusText: builds complete Prometheus text output
	c.mu.Lock() // c.mu.Lock(): acquire lock for consistent read
	defer c.mu.Unlock() // defer c.mu.Unlock(): release lock after build

	var out string // out: accumulating Prometheus output text

	// Allowed requests counter per identity
	out += "# HELP rate_limiter_requests_allowed Total allowed requests per identity\n" // HELP: metric description
	out += "# TYPE rate_limiter_requests_allowed counter\n" // TYPE: counter (monotonically increasing)
	for identity, count := range c.allowedTotal { // identity: each tracked identity; count: allowed count
		out += fmt.Sprintf("rate_limiter_requests_allowed{identity=\"%s\"} %d\n", identity, count) // format as Prometheus time series
	}

	// Denied requests counter per identity
	out += "# HELP rate_limiter_requests_denied Total denied requests per identity\n" // HELP: metric description
	out += "# TYPE rate_limiter_requests_denied counter\n" // TYPE: counter
	for identity, count := range c.deniedTotal { // identity: each tracked identity; count: denied count
		out += fmt.Sprintf("rate_limiter_requests_denied{identity=\"%s\"} %d\n", identity, count) // format as Prometheus time series
	}

	// Current limit per algorithm (gauge — can go up or down)
	out += "# HELP rate_limiter_current_limit Current limit per algorithm\n" // HELP: metric description
	out += "# TYPE rate_limiter_current_limit gauge\n" // TYPE: gauge (can increase or decrease)
	for algorithm, limit := range c.limits { // algorithm: limiter name; limit: current limit value
		out += fmt.Sprintf("rate_limiter_current_limit{algorithm=\"%s\"} %d\n", algorithm, limit) // format as Prometheus gauge
	}

	// Total check count (counter)
	out += "# HELP rate_limiter_checks_total Total number of checks\n" // HELP: metric description
	out += "# TYPE rate_limiter_checks_total counter\n" // TYPE: counter
	out += fmt.Sprintf("rate_limiter_checks_total %d\n", c.totalChecks) // total checks across all identities

	// Average check duration (gauge — only present if checks > 0)
	if c.totalChecks > 0 { // avoid division by zero; only report if data exists
		avg := c.totalDuration / float64(c.totalChecks) // avg: mean check duration in seconds
		out += "# HELP rate_limiter_check_duration_seconds Average check duration in seconds\n" // HELP: metric description
		out += "# TYPE rate_limiter_check_duration_seconds gauge\n" // TYPE: gauge
		out += fmt.Sprintf("rate_limiter_check_duration_seconds %f\n", avg) // average duration in seconds
	}

	return out // return: complete Prometheus text exposition format
}

/*
================================================================================
ARCHITECTURAL / ENGINEERING DECISIONS
================================================================================

1. PROMETHEUS TEXT EXPOSITION FORMAT
   - Output is in Prometheus text format (# HELP, # TYPE, then time series).
   - Decision: Prometheus is the standard monitoring system. This format is scrapable by Prometheus.
   - Can be exposed via HTTP endpoint (e.g., GET /metrics) for Prometheus to scrape.

2. PER-IDENTITY METRICS
   - allowedTotal and deniedTotal track counts per identity string.
   - Decision: identity comes from rate limiter Check() calls ("create:alice", "user:123").
   - Cardinality concern: high-cardinality identities could create many series.
   - Mitigation: only active identities are tracked (no pre-registration).

3. ALGORITHM-LEVEL LIMIT TRACKING
   - limits map tracks current limit per algorithm name.
   - Decision: useful for monitoring which algorithms are being auto-scaled.
   - Algorithm names: "fixed_window", "sliding_window_counter", "token_bucket", "leaky_bucket", "sliding_window_log".

4. THREAD SAFETY (sync.Mutex)
   - All methods lock the entire Collector for the operation duration.
   - Decision: metrics updates are very fast (O(1) map operations). Lock contention is minimal.
   - Alternative: sharded locks for high-throughput. Not needed for typical load.

5. COUNTER VS GAUGE TYPES
   - allowedTotal, deniedTotal, totalChecks: counter (monotonically increasing).
   - limits, totalDuration/avg: gauge (can go up or down).
   - Decision: Prometheus semantic correctness. Counters can only increase; gauges can fluctuate.

6. NO RESET / CLEAR METHOD
   - Collector has no Reset() method.
   - Decision: Prometheus scrapes are cumulative (counters keep growing).
   - If reset needed, create a new Collector instance.

7. OPTIONAL AVERAGE DURATION
   - Average duration only emitted when totalChecks > 0.
   - Decision: avoids emitting a gauge with 0 checks (meaningless value).
   - Prometheus client libraries handle this with NaN, but explicit skip is cleaner.

8. STRUCTURE FOR EXTENSIBILITY
   - Easy to add new metric types (just add new map field + new export section).
   - Decision: follow OpenTelemetry conventions for future migration.
*/
