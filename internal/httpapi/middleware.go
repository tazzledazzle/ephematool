package httpapi

import (
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	semconv "go.opentelemetry.io/otel/semconv/v1.40.0"
	"go.opentelemetry.io/otel/trace"
)

// tagRoute names the active server span after the route template and adds the
// bounded http.route attribute to both the span and the otelhttp metrics
// labeler. Raw request paths must never be used (cardinality, Pitfall 2).
func tagRoute(r *http.Request, method, route string) *http.Request {
	ctx := r.Context()
	span := trace.SpanFromContext(ctx)
	span.SetName(method + " " + route)
	span.SetAttributes(semconv.HTTPRoute(route))
	if l, ok := otelhttp.LabelerFromContext(ctx); ok {
		l.Add(semconv.HTTPRoute(route))
	}
	return r
}

// RouteTagging is a Huma middleware that tags the server span and metrics with
// the operation's route template.
func RouteTagging(ctx huma.Context, next func(huma.Context)) {
	if op := ctx.Operation(); op != nil {
		c := ctx.Context()
		span := trace.SpanFromContext(c)
		span.SetName(op.Method + " " + op.Path)
		span.SetAttributes(semconv.HTTPRoute(op.Path))
		if l, ok := otelhttp.LabelerFromContext(c); ok {
			l.Add(semconv.HTTPRoute(op.Path))
		}
	}
	next(ctx)
}

// RouteHandler tags non-Huma handlers (for example the OAuth routes) with their
// route template.
func RouteHandler(method, pattern string, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, tagRoute(r, method, pattern))
	})
}

// TraceIDHeader exposes the active trace id as X-Trace-Id so the UI and
// operators can look the request up in Tempo.
func TraceIDHeader(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if sc := trace.SpanContextFromContext(r.Context()); sc.IsValid() {
			w.Header().Set("X-Trace-Id", sc.TraceID().String())
		}
		next.ServeHTTP(w, r)
	})
}
