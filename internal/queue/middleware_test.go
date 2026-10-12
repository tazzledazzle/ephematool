package queue

import (
	"context"
	"errors"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func setupTracing(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	prevTP := otel.GetTracerProvider()
	prevProp := otel.GetTextMapPropagator()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
	})
	return exp
}

func TestPropagation(t *testing.T) {
	exp := setupTracing(t)

	pctx, parent := otel.Tracer("test").Start(context.Background(), "api")
	headers := InjectHeaders(pctx)
	parent.End()
	task := asynq.NewTaskWithHeaders(TypePing, nil, headers)

	var handlerSpan trace.SpanContext
	h := TracingMiddleware(asynq.HandlerFunc(func(ctx context.Context, _ *asynq.Task) error {
		handlerSpan = trace.SpanContextFromContext(ctx)
		return nil
	}))
	require.NoError(t, h.ProcessTask(context.Background(), task))

	assert.Equal(t, parent.SpanContext().TraceID(), handlerSpan.TraceID())

	var found bool
	for _, s := range exp.GetSpans() {
		if s.Name != "telemetry:ping process" {
			continue
		}
		found = true
		assert.Equal(t, parent.SpanContext().SpanID(), s.Parent.SpanID())
		assert.Equal(t, trace.SpanKindConsumer, s.SpanKind)
	}
	assert.True(t, found, "consumer span not exported")
}

func TestNoHeadersStartsRoot(t *testing.T) {
	exp := setupTracing(t)
	task := asynq.NewTask(TypeHeartbeat, nil)
	h := TracingMiddleware(asynq.HandlerFunc(func(context.Context, *asynq.Task) error { return nil }))
	require.NotPanics(t, func() { require.NoError(t, h.ProcessTask(context.Background(), task)) })

	spans := exp.GetSpans()
	require.Len(t, spans, 1)
	assert.False(t, spans[0].Parent.IsValid())
}

func TestTaskMetrics(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	prev := otel.GetMeterProvider()
	otel.SetMeterProvider(mp)
	t.Cleanup(func() { otel.SetMeterProvider(prev) })
	setupTracing(t)

	ok := TracingMiddleware(asynq.HandlerFunc(func(context.Context, *asynq.Task) error { return nil }))
	bad := TracingMiddleware(asynq.HandlerFunc(func(context.Context, *asynq.Task) error { return errors.New("boom") }))
	require.NoError(t, ok.ProcessTask(context.Background(), asynq.NewTask(TypePing, nil)))
	require.Error(t, bad.ProcessTask(context.Background(), asynq.NewTask(TypePing, nil)))

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))

	counts := map[string]int64{}
	var durPoints int
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch d := m.Data.(type) {
			case metricdata.Sum[int64]:
				if m.Name != "ephematool.queue.task.processed" {
					continue
				}
				for _, dp := range d.DataPoints {
					assert.Equal(t, 2, dp.Attributes.Len(), "only task.type and outcome")
					tt, _ := dp.Attributes.Value("task.type")
					assert.Equal(t, TypePing, tt.AsString())
					o, _ := dp.Attributes.Value("outcome")
					counts[o.AsString()] += dp.Value
				}
			case metricdata.Histogram[float64]:
				if m.Name != "ephematool.queue.task.duration" {
					continue
				}
				for _, dp := range d.DataPoints {
					assert.Equal(t, 2, dp.Attributes.Len())
					durPoints += int(dp.Count)
				}
			}
		}
	}
	assert.Equal(t, map[string]int64{"success": 1, "error": 1}, counts)
	assert.Equal(t, 2, durPoints)
}
