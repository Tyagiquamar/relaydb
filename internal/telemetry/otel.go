package telemetry

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// TracerProvider wraps OTel tracer provider setup.
type TracerProvider struct {
	provider *sdktrace.TracerProvider
	logger   *slog.Logger
}

// NewTracerProvider creates a tracer provider. If endpoint is empty, uses stdout exporter.
func NewTracerProvider(ctx context.Context, serviceName, endpoint string) (*TracerProvider, error) {
	logger := Logger()

	var exporter sdktrace.SpanExporter
	var err error

	if endpoint != "" {
		exporter, err = otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpoint(endpoint),
			otlptracegrpc.WithInsecure(),
		)
		if err != nil {
			return nil, err
		}
		logger.Info("OTLP exporter configured", "endpoint", endpoint)
	} else {
		exporter, err = stdouttrace.New(stdouttrace.WithPrettyPrint())
		if err != nil {
			return nil, err
		}
		logger.Info("stdout trace exporter configured (dev mode)")
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
			semconv.ServiceVersion("0.1.0"),
		),
	)
	if err != nil {
		return nil, err
	}

	// Parent-based sampling with ratio from env
	sampler := sdktrace.ParentBased(sdktrace.TraceIDRatioBased(0.1)) // 10% default

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	otel.SetTracerProvider(provider)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return &TracerProvider{
		provider: provider,
		logger:   logger,
	}, nil
}

// Shutdown flushes pending spans and shuts down the provider.
func (t *TracerProvider) Shutdown(ctx context.Context) error {
	return t.provider.Shutdown(ctx)
}

// Tracer returns a named tracer.
func (t *TracerProvider) Tracer(name string) trace.Tracer {
	return t.provider.Tracer(name)
}

// Span starts a new span with the given name.
func (t *TracerProvider) Span(ctx context.Context, name string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return t.provider.Tracer("relaydb").Start(ctx, name, opts...)
}
