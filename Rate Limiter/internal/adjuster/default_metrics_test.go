package adjuster

import (
	"testing"
	"time"
)

func TestBasicMetrics_GetMetrics(t *testing.T) {
	m := &BasicMetrics{
		ErrorRate:    0.1,
		ResponseTime: 200 * time.Millisecond,
		CPUUsage:     50,
		MemoryUsage:  60,
		ActiveConns:  100,
	}

	metrics, err := m.GetMetrics()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if metrics.ErrorRate != 0.1 {
		t.Fatalf("ErrorRate: expected 0.1, got %f", metrics.ErrorRate)
	}
	if metrics.ResponseTime != 200*time.Millisecond {
		t.Fatalf("ResponseTime: expected 200ms, got %v", metrics.ResponseTime)
	}
	if metrics.CPUUsage != 50 {
		t.Fatalf("CPUUsage: expected 50, got %f", metrics.CPUUsage)
	}
	if metrics.MemoryUsage != 60 {
		t.Fatalf("MemoryUsage: expected 60, got %f", metrics.MemoryUsage)
	}
	if metrics.ActiveConns != 100 {
		t.Fatalf("ActiveConns: expected 100, got %d", metrics.ActiveConns)
	}
}

func TestBasicMetrics_GetMetrics_ZeroValues(t *testing.T) {
	m := &BasicMetrics{}

	metrics, err := m.GetMetrics()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if metrics.ErrorRate != 0 {
		t.Fatalf("ErrorRate: expected 0, got %f", metrics.ErrorRate)
	}
	if metrics.ResponseTime != 0 {
		t.Fatalf("ResponseTime: expected 0, got %v", metrics.ResponseTime)
	}
	if metrics.CPUUsage != 0 {
		t.Fatalf("CPUUsage: expected 0, got %f", metrics.CPUUsage)
	}
	if metrics.MemoryUsage != 0 {
		t.Fatalf("MemoryUsage: expected 0, got %f", metrics.MemoryUsage)
	}
	if metrics.ActiveConns != 0 {
		t.Fatalf("ActiveConns: expected 0, got %d", metrics.ActiveConns)
	}
}

func TestSimpleMetricProvider_GetMetrics(t *testing.T) {
	errRate := 0.2
	respTime := 300 * time.Millisecond
	cpu := 70.0
	mem := 80.0
	conns := 200

	p := &SimpleMetricProvider{
		ErrorRate:    &errRate,
		ResponseTime: &respTime,
		CPUUsage:     &cpu,
		MemoryUsage:  &mem,
		ActiveConns:  &conns,
	}

	metrics, err := p.GetMetrics()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if metrics.ErrorRate != 0.2 {
		t.Fatalf("ErrorRate: expected 0.2, got %f", metrics.ErrorRate)
	}
	if metrics.ResponseTime != 300*time.Millisecond {
		t.Fatalf("ResponseTime: expected 300ms, got %v", metrics.ResponseTime)
	}
	if metrics.CPUUsage != 70 {
		t.Fatalf("CPUUsage: expected 70, got %f", metrics.CPUUsage)
	}
	if metrics.MemoryUsage != 80 {
		t.Fatalf("MemoryUsage: expected 80, got %f", metrics.MemoryUsage)
	}
	if metrics.ActiveConns != 200 {
		t.Fatalf("ActiveConns: expected 200, got %d", metrics.ActiveConns)
	}
}

func TestSimpleMetricProvider_GetMetrics_NilPointers(t *testing.T) {
	p := &SimpleMetricProvider{}

	metrics, err := p.GetMetrics()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if metrics.ErrorRate != 0 {
		t.Fatalf("ErrorRate: expected 0, got %f", metrics.ErrorRate)
	}
	if metrics.ResponseTime != 0 {
		t.Fatalf("ResponseTime: expected 0, got %v", metrics.ResponseTime)
	}
	if metrics.CPUUsage != 0 {
		t.Fatalf("CPUUsage: expected 0, got %f", metrics.CPUUsage)
	}
	if metrics.MemoryUsage != 0 {
		t.Fatalf("MemoryUsage: expected 0, got %f", metrics.MemoryUsage)
	}
	if metrics.ActiveConns != 0 {
		t.Fatalf("ActiveConns: expected 0, got %d", metrics.ActiveConns)
	}
}

func TestSimpleMetricProvider_GetMetrics_PartialPointers(t *testing.T) {
	errRate := 0.5
	p := &SimpleMetricProvider{
		ErrorRate: &errRate,
	}

	metrics, err := p.GetMetrics()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if metrics.ErrorRate != 0.5 {
		t.Fatalf("ErrorRate: expected 0.5, got %f", metrics.ErrorRate)
	}
	if metrics.ResponseTime != 0 {
		t.Fatalf("ResponseTime: expected 0 (nil pointer), got %v", metrics.ResponseTime)
	}
	if metrics.CPUUsage != 0 {
		t.Fatalf("CPUUsage: expected 0 (nil pointer), got %f", metrics.CPUUsage)
	}
	if metrics.MemoryUsage != 0 {
		t.Fatalf("MemoryUsage: expected 0 (nil pointer), got %f", metrics.MemoryUsage)
	}
	if metrics.ActiveConns != 0 {
		t.Fatalf("ActiveConns: expected 0 (nil pointer), got %d", metrics.ActiveConns)
	}
}

func TestSimpleMetricProvider_LiveUpdate(t *testing.T) {
	errRate := 0.1
	p := &SimpleMetricProvider{
		ErrorRate: &errRate,
	}

	metrics, _ := p.GetMetrics()
	if metrics.ErrorRate != 0.1 {
		t.Fatalf("expected 0.1, got %f", metrics.ErrorRate)
	}

	errRate = 0.9
	metrics, _ = p.GetMetrics()
	if metrics.ErrorRate != 0.9 {
		t.Fatalf("expected 0.9 after update, got %f", metrics.ErrorRate)
	}
}
