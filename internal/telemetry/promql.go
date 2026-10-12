package telemetry

// Prometheus names observed in the Wave 0 spike (Pitfall 7). Prometheus
// translates OTLP names with UnderscoreEscapingWithSuffixes, so dotted OTel
// names never appear here. Keep every dashboard and status query in this file.
const (
	// MetricHTTPServerDurationCount is the otelhttp server duration histogram count.
	MetricHTTPServerDurationCount = "http_server_request_duration_seconds_count"
	// MetricGoRoutines is emitted by the Go runtime instrumentation of every Go
	// service (api, worker) and carries the promoted service_* labels.
	MetricGoRoutines = "go_goroutine_count"

	LabelServiceName      = "service_name"
	LabelServiceNamespace = "service_namespace"
	LabelHTTPRoute        = "http_route"
)

// LastSignalQuery returns, per service_name, the unix time of the most recent
// sample of a Go service. Spike result for assumption A4: the original form
// max by (service_name)(timestamp(target_info{service_namespace="ephematool"}))
// is EMPTY, because Prometheus' target_info series carries only job and
// telemetry.sdk labels, not the promoted service_* attributes. The promoted
// attributes live on every metric series, so the query targets one metric that
// each Go service always emits. Do not use a __name__ regex: series from
// different metrics collapse to the same labelset and the query errors.
// Non-Go services (the browser dashboard) need their own selector in plan 01-06.
const LastSignalQuery = `max by (service_name) (timestamp(go_goroutine_count{service_namespace="ephematool"}))`
