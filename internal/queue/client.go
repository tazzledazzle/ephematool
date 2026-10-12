package queue

import (
	"context"

	"github.com/hibiken/asynq"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Client wraps an asynq producer. It owns its own Redis connection, which is
// deliberately not instrumented (Pitfall 4).
type Client struct {
	c *asynq.Client
}

// NewClient connects an asynq client to redisAddr.
func NewClient(redisAddr string) *Client {
	return &Client{c: asynq.NewClient(asynq.RedisClientOpt{Addr: redisAddr})}
}

// Close releases the Redis connection.
func (c *Client) Close() error { return c.c.Close() }

// EnqueuePing publishes a telemetry:ping task under a producer span and carries
// the W3C trace context in the task headers.
func (c *Client) EnqueuePing(ctx context.Context) error {
	ctx, span := otel.Tracer("ephematool/queue").Start(ctx, "telemetry:ping publish",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "asynq"),
			attribute.String("messaging.destination.name", "default"),
		))
	defer span.End()

	task := asynq.NewTaskWithHeaders(TypePing, nil, InjectHeaders(ctx))
	if _, err := c.c.EnqueueContext(ctx, task, asynq.MaxRetry(1)); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}
