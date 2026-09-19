package adjuster // adjuster: automatic rate limit adjustment based on system metrics

import "time" // time: duration type for pointer helpers

// BasicMetrics is a concrete Metrics implementation that stores all fields directly. // BasicMetrics: direct field storage
type BasicMetrics struct { // BasicMetrics: holds all metric values as direct fields
	ErrorRate    float64 // ErrorRate: fraction of requests that returned errors (0.0 to 1.0)
	ResponseTime time.Duration // ResponseTime: average HTTP response time
	CPUUsage     float64 // CPUUsage: CPU utilization percentage (0.0 to 100.0)
	MemoryUsage  float64 // MemoryUsage: memory utilization percentage (0.0 to 100.0)
	ActiveConns  int // ActiveConns: number of currently active connections
}

// GetMetrics returns BasicMetrics as Metrics (identity conversion). // GetMetrics: implements MetricProvider
func (m *BasicMetrics) GetMetrics() (Metrics, error) { // GetMetrics: returns same data wrapped in Metrics struct
	return Metrics{ // return: convert BasicMetrics fields to Metrics fields
		ErrorRate:    m.ErrorRate, // ErrorRate: direct copy
		ResponseTime: m.ResponseTime, // ResponseTime: direct copy
		CPUUsage:     m.CPUUsage, // CPUUsage: direct copy
		MemoryUsage:  m.MemoryUsage, // MemoryUsage: direct copy
		ActiveConns:  m.ActiveConns, // ActiveConns: direct copy
	}, nil // nil: always succeeds (no external I/O)
}

// SimpleMetricProvider is a MetricProvider that reads from individual pointer fields. // SimpleMetricProvider: pointer-based metric source
type SimpleMetricProvider struct { // SimpleMetricProvider: each metric is a pointer (nil = zero value)
	ErrorRate    *float64 // ErrorRate: pointer to error rate (nil → 0)
	ResponseTime *time.Duration // ResponseTime: pointer to response time (nil → 0)
	CPUUsage     *float64 // CPUUsage: pointer to CPU usage (nil → 0)
	MemoryUsage  *float64 // MemoryUsage: pointer to memory usage (nil → 0)
	ActiveConns  *int // ActiveConns: pointer to active connections (nil → 0)
}

// GetMetrics reads from pointer fields, defaulting to zero for nil pointers. // GetMetrics: dereferences pointers with nil safety
func (p *SimpleMetricProvider) GetMetrics() (Metrics, error) { // GetMetrics: converts pointer fields to Metrics
	return Metrics{ // return: construct Metrics from pointer fields
		ErrorRate:    ptrFloat64(p.ErrorRate), // ErrorRate: nil-safe dereference
		ResponseTime: ptrDuration(p.ResponseTime), // ResponseTime: nil-safe dereference
		CPUUsage:     ptrFloat64(p.CPUUsage), // CPUUsage: nil-safe dereference
		MemoryUsage:  ptrFloat64(p.MemoryUsage), // MemoryUsage: nil-safe dereference
		ActiveConns:  ptrInt(p.ActiveConns), // ActiveConns: nil-safe dereference
	}, nil // nil: always succeeds
}

// ptrFloat64 dereferences a *float64, returning 0 if nil. // ptrFloat64: nil-safe float64 dereference
func ptrFloat64(p *float64) float64 { // ptrFloat64: returns *p or 0
	if p == nil { // if pointer is nil
		return 0 // return zero value
	}
	return *p // return dereferenced value
}

// ptrDuration dereferences a *time.Duration, returning 0 if nil. // ptrDuration: nil-safe Duration dereference
func ptrDuration(p *time.Duration) time.Duration { // ptrDuration: returns *p or 0
	if p == nil { // if pointer is nil
		return 0 // return zero value
	}
	return *p // return dereferenced value
}

// ptrInt dereferences a *int, returning 0 if nil. // ptrInt: nil-safe int dereference
func ptrInt(p *int) int { // ptrInt: returns *p or 0
	if p == nil { // if pointer is nil
		return 0 // return zero value
	}
	return *p // return dereferenced value
}

/*
================================================================================
ARCHITECTURAL / ENGINEERING DECISIONS
================================================================================

1. TWO METRICS PROVIDERS
   - BasicMetrics: stores values directly (no pointers). Used when metrics are already computed.
   - SimpleMetricProvider: stores values as pointers (can be nil). Used when some metrics may be unavailable.
   - Decision: both implement MetricProvider, giving flexibility in how metrics are sourced.

2. NIL-SAFE POINTER HELPERS (ptrFloat64, ptrDuration, ptrInt)
   - SimpleMetricProvider fields are pointers (can be nil).
   - Helper functions return 0 for nil pointers.
   - Decision: avoids nil pointer dereference panics. Callers can omit optional metrics.
   - ErrorRate and ResponseTime are mandatory (validated in NewAdjuster), but CPU/Memory/Conns are optional.

3. METRICS STRUCTURE
   - BasicMetrics maps directly to Metrics struct fields (identity conversion in GetMetrics).
   - Decision: simple adapter pattern. BasicMetrics is a concrete implementation; Metrics is the interface type.

4. POINTER VS VALUE RECEIVERS
   - Both GetMetrics use pointer receivers ((m *BasicMetrics), (p *SimpleMetricProvider)).
   - Decision: consistent with MetricProvider interface signature. Also avoids copying struct values.

5. NO EXTERNAL DEPENDENCIES
   - SimpleMetricProvider uses only standard library (time, pointers).
   - Decision: no external packages needed for basic metrics. External providers (Prometheus) use MetricProvider interface.

6. ERROR RETURN
   - Both GetMetrics return nil error.
   - Decision: these providers don't fail (no I/O, no network). Error return satisfies MetricProvider interface.
   - Future providers (Prometheus, statsd) would return actual errors.
*/
