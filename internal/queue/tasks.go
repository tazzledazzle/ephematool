// Package queue holds asynq task types and trace-context propagation.
package queue

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// Task type names.
const (
	TypePing      = "telemetry:ping"
	TypeHeartbeat = "telemetry:heartbeat"
)

// InjectHeaders serialises the W3C trace context from ctx into a header map
// suitable for asynq.NewTaskWithHeaders.
func InjectHeaders(ctx context.Context) map[string]string {
	h := map[string]string{}
	otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(h))
	return h
}
