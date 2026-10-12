package status

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

// Checks are the probe functions. Every field is optional; a missing check
// leaves its row "unknown".
type Checks struct {
	Postgres    func(ctx context.Context) error
	Redis       func(ctx context.Context) error
	HeartbeatAt func(ctx context.Context) (time.Time, error)
	// HTTP maps a service id to a readiness URL taken from server config only.
	HTTP map[string]string
	// LastSignal returns the newest telemetry timestamp per Prometheus service_name.
	LastSignal func(ctx context.Context) (map[string]time.Time, error)
	Now        func() time.Time
}

// Prober runs the checks on an interval and caches the results.
type Prober struct {
	c        Checks
	interval time.Duration
	timeout  time.Duration
	client   *http.Client // plain client: no otelhttp, so probes create no spans

	up       metric.Int64Gauge
	duration metric.Float64Histogram

	mu          sync.RWMutex
	generatedAt time.Time
	results     []Result
}

// NewProber builds a prober. meter may be a no-op meter.
func NewProber(c Checks, interval, timeout time.Duration, meter metric.Meter) *Prober {
	if c.Now == nil {
		c.Now = time.Now
	}
	p := &Prober{
		c:        c,
		interval: interval,
		timeout:  timeout,
		client:   &http.Client{Timeout: timeout},
	}
	p.up, _ = meter.Int64Gauge("ephematool.service.up",
		metric.WithDescription("1 when the last probe of the service succeeded"))
	p.duration, _ = meter.Float64Histogram("ephematool.probe.duration",
		metric.WithUnit("s"), metric.WithDescription("Probe duration per service"))
	p.results = make([]Result, len(Services))
	for i, s := range Services {
		p.results[i] = Result{ID: s.ID, Status: Unknown}
	}
	return p
}

// Snapshot returns the cached results in Services order. It never blocks on a probe.
func (p *Prober) Snapshot() (generatedAt time.Time, results []Result) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]Result, len(p.results))
	copy(out, p.results)
	return p.generatedAt, out
}

// Run probes immediately and then every interval until ctx ends.
func (p *Prober) Run(ctx context.Context) {
	p.probeAll(ctx)
	t := time.NewTicker(p.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.probeAll(ctx)
		}
	}
}

type probeOut struct {
	err      error
	duration time.Duration
}

// probeAll runs every probe concurrently, then publishes one snapshot.
func (p *Prober) probeAll(ctx context.Context) {
	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		probed  = map[string]probeOut{}
		signals map[string]time.Time
		sigErr  error
		hbAt    time.Time
		hbErr   error
	)
	run := func(id string, fn func(context.Context) error) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, p.timeout)
			defer cancel()
			start := time.Now()
			err := fn(cctx)
			mu.Lock()
			probed[id] = probeOut{err: err, duration: time.Since(start)}
			mu.Unlock()
		}()
	}

	for id, url := range p.c.HTTP {
		run(id, func(c context.Context) error { return p.httpProbe(c, url) })
	}
	if p.c.Postgres != nil {
		run("postgres", p.c.Postgres)
	}
	if p.c.Redis != nil {
		run("redis", p.c.Redis)
	}
	if p.c.HeartbeatAt != nil {
		run("worker", func(c context.Context) error {
			hbAt, hbErr = p.c.HeartbeatAt(c)
			return hbErr
		})
	}
	if p.c.LastSignal != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cctx, cancel := context.WithTimeout(ctx, p.timeout)
			defer cancel()
			signals, sigErr = p.c.LastSignal(cctx)
		}()
	}
	wg.Wait()

	now := p.c.Now()
	results := make([]Result, len(Services))
	for i, s := range Services {
		results[i] = p.evaluate(s, now, probed, hbAt, hbErr, signals, sigErr)
	}

	p.mu.Lock()
	prev := p.results
	p.results = results
	p.generatedAt = now.UTC()
	p.mu.Unlock()

	for i, r := range results {
		id := r.ID
		up := int64(0)
		if r.Status == Healthy || r.Status == Degraded {
			up = 1
		}
		attrs := metric.WithAttributes(attribute.String("service", id))
		p.up.Record(ctx, up, attrs)
		if o, ok := probed[id]; ok {
			p.duration.Record(ctx, o.duration.Seconds(), attrs)
		}
		logTransition(ctx, id, prev[i].Status, r.Status)
	}
}

func logTransition(ctx context.Context, id, from, to string) {
	if from == to || from == Unknown && to == Healthy {
		return
	}
	switch to {
	case Down, Degraded:
		slog.WarnContext(ctx, "status.transition", "service", id, "from", from, "to", to)
	default:
		slog.InfoContext(ctx, "status.transition", "service", id, "from", from, "to", to)
	}
}

func (p *Prober) httpProbe(ctx context.Context, url string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return httpStatusError(resp.StatusCode)
	}
	return nil
}

type httpStatusError int

func (e httpStatusError) Error() string { return fmt.Sprintf("HTTP %d", int(e)) }

// detailFor maps a probe error to a short, client-safe string (T-01-34).
func detailFor(err error) string {
	var hs httpStatusError
	var ne net.Error
	switch {
	case errors.As(err, &hs):
		return hs.Error()
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return "timeout"
	default:
		return "unreachable"
	}
}

func (p *Prober) evaluate(s Service, now time.Time, probed map[string]probeOut,
	hbAt time.Time, hbErr error, signals map[string]time.Time, sigErr error) Result {
	r := Result{ID: s.ID}

	// Probe outcome: the api row is healthy because this code is running.
	var probeErr error
	probeRan := s.ID == "api"
	if o, ok := probed[s.ID]; ok {
		probeRan = true
		probeErr = o.err
	}
	if !probeRan {
		r.Status = Unknown
		return r
	}
	if s.ID == "worker" && probeErr == nil && now.Sub(hbAt) > HeartbeatMaxAge {
		probeErr = errors.New("stale heartbeat")
		r.Detail = "stale heartbeat"
	}
	if probeErr != nil {
		r.Status = Down
		if r.Detail == "" {
			r.Detail = detailFor(probeErr)
		}
		if s.ID == "worker" && hbErr != nil {
			r.Detail = "no heartbeat"
		}
		return r
	}

	if s.Group != GroupPlatform {
		t := now
		r.LastSignalAt = &t
		r.Status = Healthy
		return r
	}

	// Platform rows also need recent telemetry from Prometheus.
	if p.c.LastSignal == nil || sigErr != nil {
		r.Status = Unknown
		r.Detail = "telemetry query unavailable"
		return r
	}
	ts, ok := signals[signalServiceNames[s.ID]]
	if !ok {
		r.Status = Degraded
		r.Detail = "no telemetry signal"
		return r
	}
	r.LastSignalAt = &ts
	if now.Sub(ts) > StaleSignal {
		r.Status = Degraded
		r.Detail = "stale telemetry signal"
		return r
	}
	r.Status = Healthy
	return r
}
