package analytics

import (
	"time"

	"rate-limiter/internal/adjuster"
	"rate-limiter/internal/metrics"
)

// CollectorProvider implements adjuster.MetricProvider by reading from the metrics.Collector.
// It translates collector data into the Metrics struct the Adjuster expects:
//   - ErrorRate: denied requests / total requests
//   - ResponseTime: average check duration from collector
//
// This allows the Adjuster to make decisions based on actual rate limiter
// traffic patterns rather than external system metrics.
type CollectorProvider struct {
	collector *metrics.Collector
}

// NewCollectorProvider creates a provider backed by the given collector.
func NewCollectorProvider(collector *metrics.Collector) *CollectorProvider {
	return &CollectorProvider{collector: collector}
}

// GetMetrics returns current metrics derived from collector data.
// Called periodically by the Adjuster's adjustment loop.
func (p *CollectorProvider) GetMetrics() (adjuster.Metrics, error) {
	snap := p.collector.Snapshot()

	allowedRaw, _ := snap["allowed_total"].(map[string]int64)
	deniedRaw, _ := snap["denied_total"].(map[string]int64)
	totalChecks, _ := snap["total_checks"].(int64)
	avgDuration, _ := snap["avg_duration_s"].(float64)

	var allowedCount, deniedCount int64
	for _, v := range allowedRaw {
		allowedCount += v
	}
	for _, v := range deniedRaw {
		deniedCount += v
	}

	var errorRate float64
	if totalChecks > 0 {
		errorRate = float64(deniedCount) / float64(totalChecks)
	}

	return adjuster.Metrics{
		ErrorRate:    errorRate,
		ResponseTime: time.Duration(avgDuration * float64(time.Second)),
	}, nil
}

/*
================================================================================
ENGINEERING / ARCHITECTURAL DECISIONS
================================================================================

1. SEPARATE PROVIDER (not Collector)
   - The collector does NOT implement adjuster.MetricProvider.
   - Decision: create a dedicated provider rather than modifying collector.
   - Keeps collector focused on metrics collection, not system metrics export.
   - Avoids modifying a file with line-by-line comments.

2. ERROR RATE DERIVATION
   - ErrorRate = denied / total checks.
   - Decision: "errors" in a rate limiter context are denied requests.
   - This gives the Adjuster visibility into protection intensity.
   - When many requests are denied, ErrorRate rises → Adjuster may increase limits.

3. RESPONSE TIME DERIVATION
   - ResponseTime = average check duration from collector.
   - Decision: check latency is the only available performance metric.
   - In production, replace with actual HTTP response time from upstream.

4. NO SYSTEM METRICS
   - CPUUsage, MemoryUsage, ActiveConns are not collected.
   - Decision: keep the provider self-contained (depends only on collector).
   - Can be extended later with actual system metrics (Prometheus, etc.).

5. WHY ANALYTICS PACKAGE EXISTS
   - Separates the consumer-facing analytics layer from internal metrics.
   - The /analytics endpoint is public-facing; collector internals should not leak.
   - Provides a clean boundary between monitoring and implementation.
*/
