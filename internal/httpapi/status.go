package httpapi

import (
	"context"
	"log/slog"
	"time"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ephematool/ephematool/internal/buildinfo"
)

// ServiceStatus is one row of the telemetry status page. The prober in plan
// 01-06 fills the list.
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
		return &StatusResponse{Body: StatusBody{
			GeneratedAt: time.Now().UTC(),
			GrafanaURL:  deps.Config.GrafanaPublicURL,
			Build:       BuildSection{API: buildinfo.Info()},
			Services:    []ServiceStatus{},
		}}, nil
	}, func(o *huma.Operation) {
		o.OperationID = "get-status"
		o.Summary = "Telemetry status snapshot"
	})
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
