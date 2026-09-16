package metrics

import (
	"fmt"
	"sync"
	"time"
)

type Collector struct {
	mu            sync.Mutex
	allowedTotal  map[string]int64
	deniedTotal   map[string]int64
	limits        map[string]int
	totalChecks   int64
	totalDuration float64
}

func NewCollector() *Collector {
	return &Collector{
		allowedTotal: make(map[string]int64),
		deniedTotal:  make(map[string]int64),
		limits:       make(map[string]int),
	}
}

func (c *Collector) RecordAllowed(identity string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.allowedTotal[identity]++
	c.totalChecks++
}

func (c *Collector) RecordDenied(identity string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.deniedTotal[identity]++
	c.totalChecks++
}

func (c *Collector) RecordDuration(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.totalDuration += d.Seconds()
}

func (c *Collector) SetLimit(algorithm string, limit int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.limits[algorithm] = limit
}

func (c *Collector) ToPrometheusText() string {
	c.mu.Lock()
	defer c.mu.Unlock()

	var out string

	out += "# HELP rate_limiter_requests_allowed Total allowed requests per identity\n"
	out += "# TYPE rate_limiter_requests_allowed counter\n"
	for identity, count := range c.allowedTotal {
		out += fmt.Sprintf("rate_limiter_requests_allowed{identity=\"%s\"} %d\n", identity, count)
	}

	out += "# HELP rate_limiter_requests_denied Total denied requests per identity\n"
	out += "# TYPE rate_limiter_requests_denied counter\n"
	for identity, count := range c.deniedTotal {
		out += fmt.Sprintf("rate_limiter_requests_denied{identity=\"%s\"} %d\n", identity, count)
	}

	out += "# HELP rate_limiter_current_limit Current limit per algorithm\n"
	out += "# TYPE rate_limiter_current_limit gauge\n"
	for algorithm, limit := range c.limits {
		out += fmt.Sprintf("rate_limiter_current_limit{algorithm=\"%s\"} %d\n", algorithm, limit)
	}

	out += "# HELP rate_limiter_checks_total Total number of checks\n"
	out += "# TYPE rate_limiter_checks_total counter\n"
	out += fmt.Sprintf("rate_limiter_checks_total %d\n", c.totalChecks)

	if c.totalChecks > 0 {
		avg := c.totalDuration / float64(c.totalChecks)
		out += "# HELP rate_limiter_check_duration_seconds Average check duration in seconds\n"
		out += "# TYPE rate_limiter_check_duration_seconds gauge\n"
		out += fmt.Sprintf("rate_limiter_check_duration_seconds %f\n", avg)
	}

	return out
}
