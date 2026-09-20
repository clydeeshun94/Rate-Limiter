package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
)

// PrometheusCollector wraps the existing Collector with official Prometheus types.
// Counter: monotonically increasing (request counts)
// Gauge: value that goes up/down (limits, duration)
// Uses the standard Prometheus registry for interoperability.
type PrometheusCollector struct {
	allowedTotal  *prometheus.CounterVec
	deniedTotal   *prometheus.CounterVec
	currentLimit  *prometheus.GaugeVec
	checkDuration prometheus.Histogram
	checksTotal   prometheus.Counter
}

// NewPrometheusCollector creates a PrometheusCollector and registers all metrics
// with the provided registry (or prometheus.DefaultRegistry if nil).
func NewPrometheusCollector(registry prometheus.Registerer) *PrometheusCollector {
	c := &PrometheusCollector{
		allowedTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "rate_limiter_requests_allowed",
				Help: "Total allowed requests per identity",
			},
			[]string{"identity"},
		),
		deniedTotal: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: "rate_limiter_requests_denied",
				Help: "Total denied requests per identity",
			},
			[]string{"identity"},
		),
		currentLimit: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: "rate_limiter_current_limit",
				Help: "Current limit per algorithm",
			},
			[]string{"algorithm"},
		),
		checkDuration: prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Name:    "rate_limiter_check_duration_seconds",
				Help:    "Duration of rate limit checks in seconds",
				Buckets: []float64{.005, .01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10},
			},
		),
		checksTotal: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: "rate_limiter_checks_total",
				Help: "Total number of rate limit checks",
			},
		),
	}

	if registry != nil {
		registry.MustRegister(
			c.allowedTotal,
			c.deniedTotal,
			c.currentLimit,
			c.checkDuration,
			c.checksTotal,
		)
	}

	return c
}
