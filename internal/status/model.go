// Package status probes the platform's own services and keeps a cached snapshot
// for /api/status. Probing is deliberately uninstrumented: it must not create
// a trace per probe (RESEARCH Pitfall 4).
package status

import "time"

// Health states reported to the UI.
const (
	Healthy  = "healthy"
	Degraded = "degraded"
	Down     = "down"
	Unknown  = "unknown"
)

// Service groups, matching the API enum.
const (
	GroupPlatform = "platform"
	GroupData     = "data"
	GroupPipeline = "pipeline"
	GroupBackends = "backends"
)

// Service is one fixed row of the status table.
type Service struct {
	ID, Name, Group string
}

// Services is the contract order of the status page.
var Services = []Service{
	{"api", "API", GroupPlatform},
	{"worker", "Worker", GroupPlatform},
	{"dashboard", "Dashboard", GroupPlatform},
	{"postgres", "Postgres", GroupData},
	{"redis", "Redis", GroupData},
	{"otel-collector", "OTel Collector", GroupPipeline},
	{"tempo", "Tempo", GroupBackends},
	{"loki", "Loki", GroupBackends},
	{"prometheus", "Prometheus", GroupBackends},
	{"grafana", "Grafana", GroupBackends},
}

// Result is the cached health of one service.
type Result struct {
	ID           string
	Status       string
	LastSignalAt *time.Time
	Detail       string
}

// Prometheus service_name values of the services that emit their own telemetry.
var signalServiceNames = map[string]string{
	"api":       "ephematool-api",
	"worker":    "ephematool-worker",
	"dashboard": "ephematool-dashboard",
}

const (
	// StaleSignal is how old a platform service's last telemetry may be before
	// the row turns degraded.
	StaleSignal = 60 * time.Second
	// HeartbeatMaxAge matches queue.HeartbeatTTL.
	HeartbeatMaxAge = 45 * time.Second
)
