package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sync"
	"testing"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/ephematool/ephematool/internal/config"
)

type itemInput struct {
	ID string `path:"id"`
}

type itemOutput struct {
	Body struct {
		ID string `json:"id"`
	}
}

// copyRequestMW simulates scs LoadAndSave, which hands the mux a request copy
// (r.WithContext) so otelhttp never sees Request.Pattern (Pitfall 2).
func copyRequestMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), struct{}{}, "x")))
	})
}

type harness struct {
	handler http.Handler
	spans   *tracetest.InMemoryExporter
	reader  *sdkmetric.ManualReader
}

func newHarness(t *testing.T, register func(api huma.API)) harness {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() {
		_ = tp.Shutdown(context.Background())
		_ = mp.Shutdown(context.Background())
	})

	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("test", "0"))
	api.UseMiddleware(RouteTagging)
	register(api)

	h := otelhttp.NewHandler(TraceIDHeader(copyRequestMW(mux)), "ephematool-api",
		otelhttp.WithTracerProvider(tp), otelhttp.WithMeterProvider(mp))
	return harness{handler: h, spans: exp, reader: reader}
}

func TestRouteTagging(t *testing.T) {
	h := newHarness(t, func(api huma.API) {
		huma.Get(api, "/api/items/{id}", func(ctx context.Context, in *itemInput) (*itemOutput, error) {
			out := &itemOutput{}
			out.Body.ID = in.ID
			return out, nil
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/api/items/42", nil)
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	spans := h.spans.GetSpans()
	require.NotEmpty(t, spans)
	var found bool
	for _, s := range spans {
		if s.Name == "GET /api/items/{id}" {
			found = true
			var route string
			for _, a := range s.Attributes {
				if a.Key == "http.route" {
					route = a.Value.AsString()
				}
			}
			assert.Equal(t, "/api/items/{id}", route)
		}
		assert.NotContains(t, s.Name, "/42")
	}
	require.True(t, found, "span named 'GET /api/items/{id}' not recorded; got %v", spanNames(spans))

	var rm metricdata.ResourceMetrics
	require.NoError(t, h.reader.Collect(context.Background(), &rm))
	var routes []string
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "http.server.request.duration" {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			require.True(t, ok)
			for _, dp := range hist.DataPoints {
				if v, ok := dp.Attributes.Value(attribute.Key("http.route")); ok {
					routes = append(routes, v.AsString())
				}
			}
		}
	}
	assert.Equal(t, []string{"/api/items/{id}"}, routes)
}

func TestTraceIDHeader(t *testing.T) {
	h := newHarness(t, func(api huma.API) {
		huma.Get(api, "/api/items/{id}", func(ctx context.Context, in *itemInput) (*itemOutput, error) {
			return &itemOutput{}, nil
		})
	})
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/items/1", nil))

	got := rec.Header().Get("X-Trace-Id")
	assert.Regexp(t, regexp.MustCompile(`^[0-9a-f]{32}$`), got)
	var server string
	for _, s := range h.spans.GetSpans() {
		if s.SpanKind == trace.SpanKindServer {
			server = s.SpanContext.TraceID().String()
		}
	}
	assert.Equal(t, server, got)
}

type fakeEnqueuer struct {
	mu    sync.Mutex
	calls []context.Context
}

func (f *fakeEnqueuer) EnqueuePing(ctx context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, ctx)
	return nil
}

func TestStatusEnqueuesPing(t *testing.T) {
	fe := &fakeEnqueuer{}
	deps := Deps{
		Config:   config.Config{GrafanaPublicURL: "http://localhost:3000", PingThrottle: 0},
		Enqueuer: fe,
	}
	h := newHarness(t, func(api huma.API) { registerStatus(api, deps) })

	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/status", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Contains(t, body, "generatedAt")
	assert.Equal(t, "http://localhost:3000", body["grafanaUrl"])
	assert.Contains(t, body, "services")
	build := body["build"].(map[string]any)
	api := build["api"].(map[string]any)
	assert.Contains(t, api, "version")

	require.Len(t, fe.calls, 1)
	var server trace.SpanContext
	for _, s := range h.spans.GetSpans() {
		if s.SpanKind == trace.SpanKindServer {
			server = s.SpanContext
		}
	}
	got := trace.SpanContextFromContext(fe.calls[0])
	require.True(t, got.IsValid())
	assert.Equal(t, server.TraceID(), got.TraceID())
	assert.Equal(t, server.SpanID(), got.SpanID(), "enqueue ctx must carry the server span so the producer span is its child")
}

func spanNames(spans tracetest.SpanStubs) []string {
	var n []string
	for _, s := range spans {
		n = append(n, s.Name)
	}
	return n
}
