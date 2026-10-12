package status

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.opentelemetry.io/otel/metric/noop"
)

var fixedNow = time.Date(2026, 10, 12, 12, 0, 0, 0, time.UTC)

// fixture serves 200 for every HTTP target unless overridden.
type fixture struct {
	srv    *httptest.Server
	codes  map[string]int
	sleeps map[string]time.Duration
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{codes: map[string]int{}, sleeps: map[string]time.Duration{}}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if d := f.sleeps[r.URL.Path]; d > 0 {
			select {
			case <-time.After(d):
			case <-r.Context().Done():
				return
			}
		}
		if c := f.codes[r.URL.Path]; c != 0 {
			w.WriteHeader(c)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fixture) checks() Checks {
	fresh := fixedNow.Add(-5 * time.Second)
	return Checks{
		Postgres:    func(context.Context) error { return nil },
		Redis:       func(context.Context) error { return nil },
		HeartbeatAt: func(context.Context) (time.Time, error) { return fixedNow.Add(-3 * time.Second), nil },
		HTTP: map[string]string{
			"dashboard":      f.srv.URL + "/dashboard",
			"otel-collector": f.srv.URL + "/collector",
			"tempo":          f.srv.URL + "/tempo",
			"loki":           f.srv.URL + "/loki",
			"prometheus":     f.srv.URL + "/prometheus",
			"grafana":        f.srv.URL + "/grafana",
		},
		LastSignal: func(context.Context) (map[string]time.Time, error) {
			return map[string]time.Time{
				"ephematool-api":       fresh,
				"ephematool-worker":    fresh,
				"ephematool-dashboard": fresh,
			}, nil
		},
		Now: func() time.Time { return fixedNow },
	}
}

func byID(rs []Result) map[string]Result {
	m := make(map[string]Result, len(rs))
	for _, r := range rs {
		m[r.ID] = r
	}
	return m
}

func probeOnce(p *Prober) []Result {
	p.probeAll(context.Background())
	_, rs := p.Snapshot()
	return rs
}

func TestSnapshotOrder(t *testing.T) {
	f := newFixture(t)
	p := NewProber(f.checks(), 10*time.Second, 2*time.Second, noop.NewMeterProvider().Meter("t"))
	_, rs := p.Snapshot()
	require.Len(t, rs, 10)
	want := []string{"api", "worker", "dashboard", "postgres", "redis", "otel-collector", "tempo", "loki", "prometheus", "grafana"}
	for i, r := range rs {
		assert.Equal(t, want[i], r.ID)
		assert.Equal(t, "unknown", r.Status)
		assert.Equal(t, want[i], Services[i].ID)
	}
}

func TestHealthy(t *testing.T) {
	f := newFixture(t)
	p := NewProber(f.checks(), 10*time.Second, 2*time.Second, noop.NewMeterProvider().Meter("t"))
	rs := probeOnce(p)
	require.Len(t, rs, 10)
	for _, r := range rs {
		assert.Equal(t, "healthy", r.Status, r.ID)
		require.NotNil(t, r.LastSignalAt, r.ID)
	}
	m := byID(rs)
	assert.Equal(t, fixedNow, *m["loki"].LastSignalAt)
	assert.Equal(t, fixedNow, *m["postgres"].LastSignalAt)
	assert.Equal(t, fixedNow.Add(-5*time.Second), *m["api"].LastSignalAt)
}

func TestDownOnProbeFailure(t *testing.T) {
	f := newFixture(t)
	f.codes["/loki"] = http.StatusServiceUnavailable
	c := f.checks()
	c.Postgres = func(context.Context) error { return errors.New("boom: secret-host:5432") }
	p := NewProber(c, 10*time.Second, 2*time.Second, noop.NewMeterProvider().Meter("t"))
	m := byID(probeOnce(p))
	assert.Equal(t, "down", m["loki"].Status)
	assert.Contains(t, m["loki"].Detail, "503")
	assert.Equal(t, "down", m["postgres"].Status)
	// T-01-34: no raw error text leaks to the client.
	assert.NotContains(t, m["postgres"].Detail, "secret-host")
	assert.Equal(t, "healthy", m["tempo"].Status)
}

func TestDegradedStaleSignal(t *testing.T) {
	f := newFixture(t)
	c := f.checks()
	c.LastSignal = func(context.Context) (map[string]time.Time, error) {
		return map[string]time.Time{
			"ephematool-api":       fixedNow.Add(-2 * time.Second),
			"ephematool-worker":    fixedNow.Add(-2 * time.Second),
			"ephematool-dashboard": fixedNow.Add(-61 * time.Second),
		}, nil
	}
	p := NewProber(c, 10*time.Second, 2*time.Second, noop.NewMeterProvider().Meter("t"))
	m := byID(probeOnce(p))
	assert.Equal(t, "degraded", m["dashboard"].Status)
	assert.Equal(t, "healthy", m["api"].Status)
	assert.Equal(t, "healthy", m["worker"].Status)
}

func TestUnknownWhenPrometheusDown(t *testing.T) {
	f := newFixture(t)
	c := f.checks()
	c.LastSignal = func(context.Context) (map[string]time.Time, error) { return nil, errors.New("prometheus unreachable") }
	p := NewProber(c, 10*time.Second, 2*time.Second, noop.NewMeterProvider().Meter("t"))
	m := byID(probeOnce(p))
	for _, id := range []string{"api", "worker", "dashboard"} {
		assert.Equal(t, "unknown", m[id].Status, id)
	}
	// The prometheus row follows only its own ready probe.
	assert.Equal(t, "healthy", m["prometheus"].Status)

	f.codes["/prometheus"] = http.StatusServiceUnavailable
	m = byID(probeOnce(p))
	assert.Equal(t, "down", m["prometheus"].Status)
}

func TestWorkerHeartbeat(t *testing.T) {
	f := newFixture(t)
	c := f.checks()
	c.HeartbeatAt = func(context.Context) (time.Time, error) { return time.Time{}, errors.New("redis: nil") }
	p := NewProber(c, 10*time.Second, 2*time.Second, noop.NewMeterProvider().Meter("t"))
	assert.Equal(t, "down", byID(probeOnce(p))["worker"].Status)

	c.HeartbeatAt = func(context.Context) (time.Time, error) { return fixedNow.Add(-46 * time.Second), nil }
	p = NewProber(c, 10*time.Second, 2*time.Second, noop.NewMeterProvider().Meter("t"))
	assert.Equal(t, "down", byID(probeOnce(p))["worker"].Status)
}

func TestProbeTimeout(t *testing.T) {
	f := newFixture(t)
	f.sleeps["/tempo"] = 3 * time.Second
	p := NewProber(f.checks(), 10*time.Second, 2*time.Second, noop.NewMeterProvider().Meter("t"))

	done := make(chan struct{})
	start := time.Now()
	go func() {
		p.probeAll(context.Background())
		close(done)
	}()

	// Snapshot must not block while the probe is in flight.
	time.Sleep(100 * time.Millisecond)
	snapStart := time.Now()
	_, rs := p.Snapshot()
	assert.Less(t, time.Since(snapStart), 200*time.Millisecond)
	assert.Len(t, rs, 10)

	<-done
	elapsed := time.Since(start)
	assert.Less(t, elapsed, 2800*time.Millisecond)
	assert.GreaterOrEqual(t, elapsed, 1900*time.Millisecond)
	m := byID(func() []Result { _, r := p.Snapshot(); return r }())
	assert.Equal(t, "down", m["tempo"].Status)
	assert.Equal(t, "timeout", m["tempo"].Detail)
}

func TestProbeMetrics(t *testing.T) {
	f := newFixture(t)
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	p := NewProber(f.checks(), 10*time.Second, 2*time.Second, mp.Meter("test"))
	probeOnce(p)

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	seenUp := map[string]bool{}
	seenDur := false
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			switch m.Name {
			case "ephematool.service.up":
				g, ok := m.Data.(metricdata.Gauge[int64])
				require.True(t, ok)
				for _, dp := range g.DataPoints {
					require.Equal(t, 1, dp.Attributes.Len(), "only the service attribute")
					v, _ := dp.Attributes.Value("service")
					seenUp[v.AsString()] = true
					assert.Equal(t, int64(1), dp.Value)
				}
			case "ephematool.probe.duration":
				seenDur = true
			}
		}
	}
	assert.Len(t, seenUp, 10)
	assert.True(t, seenDur)
}
