package adjuster

import "time"

type BasicMetrics struct {
	ErrorRate    float64
	ResponseTime time.Duration
	CPUUsage     float64
	MemoryUsage  float64
	ActiveConns  int
}

func (m *BasicMetrics) GetMetrics() (Metrics, error) {
	return Metrics{
		ErrorRate:    m.ErrorRate,
		ResponseTime: m.ResponseTime,
		CPUUsage:     m.CPUUsage,
		MemoryUsage:  m.MemoryUsage,
		ActiveConns:  m.ActiveConns,
	}, nil
}

type SimpleMetricProvider struct {
	ErrorRate    *float64
	ResponseTime *time.Duration
	CPUUsage     *float64
	MemoryUsage  *float64
	ActiveConns  *int
}

func (p *SimpleMetricProvider) GetMetrics() (Metrics, error) {
	return Metrics{
		ErrorRate:    ptrFloat64(p.ErrorRate),
		ResponseTime: ptrDuration(p.ResponseTime),
		CPUUsage:     ptrFloat64(p.CPUUsage),
		MemoryUsage:  ptrFloat64(p.MemoryUsage),
		ActiveConns:  ptrInt(p.ActiveConns),
	}, nil
}

func ptrFloat64(p *float64) float64 {
	if p == nil {
		return 0
	}
	return *p
}

func ptrDuration(p *time.Duration) time.Duration {
	if p == nil {
		return 0
	}
	return *p
}

func ptrInt(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}
