package httpapi

import (
	"context"
	"log/slog"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ephematool/ephematool/internal/buildinfo"
	"github.com/ephematool/ephematool/internal/status"
)

// ServiceStatus is one row of the telemetry status page, filled from the
// prober's cached snapshot.
type ServiceStatus struct {
	ID           string     `json:"id"`
	Name         string     `json:"name"`
	Group        string     `json:"group" enum:"platform,data,pipeline,backends"`
	Status       string     `json:"status" enum:"healthy,degraded,down,unknown"`
	LastSignalAt *time.Time `json:"lastSignalAt,omitempty"`
	Detail       string     `json:"detail"`
}

// BuildSection groups per-component build metadata.
type BuildSection struct {
	API buildinfo.BuildInfo `json:"api"`
}

// StatusBody is the /api/status payload.
type StatusBody struct {
	GeneratedAt time.Time       `json:"generatedAt"`
	GrafanaURL  string          `json:"grafanaUrl"`
	Build       BuildSection    `json:"build"`
	Services    []ServiceStatus `json:"services"`
}

// StatusResponse is the Huma output wrapper.
type StatusResponse struct {
	Body StatusBody
}

const pingThrottleKey = "telemetry:ping:throttle"

func registerStatus(api huma.API, deps Deps) {
	huma.Get(api, "/api/status", func(ctx context.Context, _ *struct{}) (*StatusResponse, error) {
		enqueued := maybeEnqueuePing(ctx, deps)
		slog.InfoContext(ctx, "status.served", "ping_enqueued", enqueued)
		generatedAt, services := time.Now().UTC(), []ServiceStatus{}
		if deps.Status != nil {
			generatedAt, services = snapshotServices(deps.Status)
		}
		return &StatusResponse{Body: StatusBody{
			GeneratedAt: generatedAt,
			GrafanaURL:  deps.Config.GrafanaPublicURL,
			Build:       BuildSection{API: buildinfo.Info()},
			Services:    services,
		}}, nil
	}, func(o *huma.Operation) {
		o.OperationID = "get-status"
		o.Summary = "Telemetry status snapshot"
		// RequireSession answers 401 without a signed-in user (T-01-28).
		o.Security = []map[string][]string{{"session": {}}}
	})
}

// snapshotServices maps the cached prober snapshot to the API model. Before the
// first probe completes the generation time is zero; report "now" instead.
func snapshotServices(p *status.Prober) (time.Time, []ServiceStatus) {
	at, results := p.Snapshot()
	if at.IsZero() {
		at = time.Now().UTC()
	}
	out := make([]ServiceStatus, 0, len(results))
	for i, r := range results {
		out = append(out, ServiceStatus{
			ID:           r.ID,
			Name:         status.Services[i].Name,
			Group:        status.Services[i].Group,
			Status:       r.Status,
			LastSignalAt: r.LastSignalAt,
			Detail:       r.Detail,
		})
	}
	return at, out
}

// maybeEnqueuePing enqueues the ping task unless throttled. Failures are logged
// and never fail the request.
func maybeEnqueuePing(ctx context.Context, deps Deps) bool {
	enq := deps.enqueuer()
	if enq == nil {
		return false
	}
	if ttl := deps.Config.PingThrottle; ttl > 0 && deps.Redis != nil {
		ok, err := deps.Redis.SetNX(ctx, pingThrottleKey, 1, ttl).Result()
		if err != nil {
			slog.WarnContext(ctx, "status.ping_throttle_failed", "error", err)
			return false
		}
		if !ok {
			return false
		}
	}
	if err := enq.EnqueuePing(ctx); err != nil {
		slog.WarnContext(ctx, "status.ping_enqueue_failed", "error", err)
		return false
	}
	return true
}
