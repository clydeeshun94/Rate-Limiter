package tracing

import (
	"context"
	"fmt"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// Tracer wraps the OpenTelemetry tracer for the rate-limiter service.
// Sampling rate is read from OTEL_TRACES_SAMPLING (default: 1.0).
// Collector endpoint is configured via OTEL_EXPORTER_OTLP_ENDPOINT.
type Tracer struct {
	tracer     oteltrace.Tracer
	shutdownFn func()
}

// New creates a Tracer with the OpenTelemetry SDK provider.
func New(serviceName string) (*Tracer, error) {
	samplerRate := 1.0
	if s := os.Getenv("OTEL_TRACES_SAMPLING"); s != "" {
		fmt.Sscanf(s, "%f", &samplerRate)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.TraceIDRatioBased(samplerRate)),
	)

	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.TraceContext{})

	return &Tracer{
		tracer:     provider.Tracer(serviceName),
		shutdownFn: func() { provider.Shutdown(context.Background()) },
	}, nil
}

// Tracer returns the OpenTelemetry tracer for creating spans.
func (t *Tracer) Tracer() oteltrace.Tracer {
	return t.tracer
}

// Shutdown stops the tracing provider and flushes pending spans.
func (t *Tracer) Shutdown() {
	t.shutdownFn()
}
