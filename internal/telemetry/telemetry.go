// Package telemetry bootstraps OpenTelemetry traces, metrics and logs once per binary.
package telemetry

import (
	"context"
	"errors"
	"log/slog"
	"os"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
)

// Config identifies the emitting service.
type Config struct {
	ServiceName string
	Version     string
	Env         string
}

// Setup registers global tracer, meter and logger providers plus the W3C
// propagator, routes the default slog logger to stdout and OTLP, and returns a
// shutdown function that flushes all three signals. Endpoints come from the
// standard OTEL_* environment variables.
func Setup(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	res, err := resource.Merge(resource.Default(), resource.NewSchemaless(
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceNamespace("ephematool"),
		semconv.ServiceVersion(cfg.Version),
		semconv.DeploymentEnvironmentName(cfg.Env),
	))
	if err != nil {
		return nil, err
	}

	te, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithResource(res), sdktrace.WithBatcher(te))

	me, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return nil, err
	}
	// PeriodicReader interval honours OTEL_METRIC_EXPORT_INTERVAL.
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(me)))

	le, err := otlploghttp.New(ctx)
	if err != nil {
		return nil, err
	}
	lp := sdklog.NewLoggerProvider(sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(le)))

	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	global.SetLoggerProvider(lp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{}))

	if err := runtime.Start(); err != nil {
		return nil, err
	}

	slog.SetDefault(slog.New(slog.NewMultiHandler(
		slog.NewJSONHandler(os.Stdout, nil),
		otelslog.NewHandler(cfg.ServiceName, otelslog.WithLoggerProvider(lp)),
	)))

	return func(ctx context.Context) error {
		return errors.Join(tp.Shutdown(ctx), mp.Shutdown(ctx), lp.Shutdown(ctx))
	}, nil
}
