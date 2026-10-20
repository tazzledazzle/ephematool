package audit

import (
	"context"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// traceIDFrom is the trace-id rule Record uses, tested without a database.
// (Record itself is covered by the integration test.)
func TestTraceIDComesFromSpan(t *testing.T) {
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(tracetest.NewInMemoryExporter()))
	ctx, span := tp.Tracer("t").Start(context.Background(), "op")
	defer span.End()
	want := span.SpanContext().TraceID().String()
	if got := traceIDFrom(ctx, ""); got != want {
		t.Fatalf("traceIDFrom = %q, want %q", got, want)
	}
	if got := traceIDFrom(ctx, "explicit"); got != "explicit" {
		t.Fatalf("an explicit trace id must win, got %q", got)
	}
	if got := traceIDFrom(context.Background(), ""); got != "" {
		t.Fatalf("no span, want empty, got %q", got)
	}
}
