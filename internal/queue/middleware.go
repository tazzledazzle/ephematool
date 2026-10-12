package queue

import (
	"context"
	"time"

	"github.com/hibiken/asynq"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// TracingMiddleware extracts the W3C trace context from the task headers,
// runs the handler under a consumer span, and records task metrics. Metric
// attributes are limited to task.type and outcome (no task IDs).
func TracingMiddleware(next asynq.Handler) asynq.Handler {
	meter := otel.Meter("ephematool/queue")
	processed, _ := meter.Int64Counter("ephematool.queue.task.processed",
		metric.WithDescription("Tasks processed by the worker"))
	duration, _ := meter.Float64Histogram("ephematool.queue.task.duration",
		metric.WithUnit("s"), metric.WithDescription("Task processing duration"))
	tracer := otel.Tracer("ephematool/queue")

	return asynq.HandlerFunc(func(ctx context.Context, t *asynq.Task) error {
		ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier(t.Headers()))
		ctx, span := tracer.Start(ctx, t.Type()+" process",
			trace.WithSpanKind(trace.SpanKindConsumer),
			trace.WithAttributes(
				attribute.String("messaging.system", "asynq"),
				attribute.String("messaging.operation.type", "process"),
				attribute.String("messaging.destination.name", "default"),
			))
		defer span.End()

		start := time.Now()
		err := next.ProcessTask(ctx, t)

		outcome := "success"
		if err != nil {
			outcome = "error"
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		attrs := metric.WithAttributes(
			attribute.String("task.type", t.Type()),
			attribute.String("outcome", outcome),
		)
		processed.Add(ctx, 1, attrs)
		duration.Record(ctx, time.Since(start).Seconds(), attrs)
		return err
	})
}
