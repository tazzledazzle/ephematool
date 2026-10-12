---
phase: 01-telemetry-spine-github-login
plan: 01
subsystem: infra
tags: [otel, collector, tempo, loki, prometheus, grafana, go, huma, compose, spike, asynq]

requires: []
provides:
  - One-command Compose stack (collector, tempo, loki, prometheus, grafana, postgres, redis, api)
  - Go module with config, buildinfo, telemetry bootstrap, httpapi, queue producer
  - GET /api/status with X-Trace-Id header and route-template span/metric tagging
  - scripts/verify-telemetry.sh harness (backends, up, trace, metrics, logs)
  - Spike-verified Prometheus names and last-signal query in internal/telemetry/promql.go
affects: [01-02, 01-03, 01-04, 01-05, 01-06, 01-07, 01-08]

tech-stack:
  added: [huma v2.39.1, otel-go 1.47.0, otelhttp 0.72.0, otelslog 0.21.0, go-redis 9.23.0, redisotel 9.23.0, asynq 0.26.0, caarlos0/env v11.4.1, testify 1.12.1]
  patterns:
    - Single telemetry.Setup bootstrap shared by binaries
    - RouteTagging Huma middleware sets span name, http.route and the otelhttp Labeler
    - asynq trace context carried in task Headers via propagation.MapCarrier
    - Named /platform Collector pipelines (environments pipelines reserved for Phase 2)

key-files:
  created:
    - deploy/compose.yaml
    - deploy/otel/collector.yaml
    - deploy/tempo/tempo.yaml
    - deploy/loki/loki.yaml
    - deploy/prometheus/prometheus.yml
    - deploy/grafana/provisioning/datasources/datasources.yaml
    - Dockerfile
    - Makefile
    - scripts/verify-telemetry.sh
    - scripts/lib/verify-lib.sh
    - scripts/verify.d/backends.sh
    - scripts/verify.d/up.sh
    - scripts/verify.d/trace.sh
    - scripts/verify.d/metrics.sh
    - scripts/verify.d/logs.sh
    - cmd/api/main.go
    - internal/config/config.go
    - internal/telemetry/telemetry.go
    - internal/telemetry/promql.go
    - internal/httpapi/api.go
    - internal/httpapi/middleware.go
    - internal/httpapi/middleware_test.go
    - internal/httpapi/status.go
    - internal/queue/client.go
    - internal/queue/tasks.go
    - api/openapi.json
  modified: []

key-decisions:
  - "Last-signal query uses go_goroutine_count (promoted service_* labels) because Prometheus target_info lacks service_name"
  - "Deps keeps Queue *queue.Client and adds an optional Enqueuer override so tests can fake the producer"
  - "openapi subcommand pretty-prints JSON so the committed spec is reviewable"

patterns-established:
  - "Every log call uses the *Context slog form (grep gate clean)"
  - "asynq Redis connection is never instrumented (grep gate clean)"
  - "All published ports bind to 127.0.0.1"

requirements-completed: [OBS-01]

duration: 40min
completed: 2026-10-11
---

# Phase 1 Plan 01: Telemetry spike and Go API skeleton Summary

**One `make up` brings up Collector + Tempo/Loki/Prometheus/Grafana + Postgres/Redis + a Go Huma API; one `GET /api/status` yields a Tempo trace (server and producer spans), a Prometheus duration metric, and a trace-correlated Loki log line.**

## Package legitimacy

Task 1 (blocking-human gate) was resolved before execution: user reply "approved" (2026-10-11) after a provenance check covering go-github/v92, otelhttp, otelslog, goose/v3, go-redis/v9, redisotel/v9, otelpgx and miniredis/v2. All eight modules resolve to their canonical repos (google/go-github, open-telemetry/opentelemetry-go-contrib, pressly/goose, redis/go-redis, exaring/otelpgx, alicebob/miniredis); none are archived; all have multi-year release histories; go-github/v92 shows one tag only because it is a new major-version module path. No `go get` ran before approval. This plan installed only otelhttp, otelslog and go-redis/redisotel from that set.

## Spike Results

All four assumptions resolved against a live `make up` stack.

| Assumption | Result | Evidence command |
|---|---|---|
| A4 last-signal query | FALLBACK-APPLIED | `curl -G $PROM/api/v1/query --data-urlencode 'query=max by (service_name)(timestamp(target_info{service_namespace="ephematool"}))'` returned `[]`. Prometheus `target_info` carries only `job`, `deployment_environment_name`, `service_version`, `telemetry_sdk_*`, not `service_name`/`service_namespace`. Fallback `max by (service_name)(timestamp(go_goroutine_count{service_namespace="ephematool"}))` returns `{service_name="ephematool-api"}`. A `__name__` regex form errors ("vector cannot contain metrics with the same labelset"). Stored as `LastSignalQuery`. Non-Go services (browser dashboard) need their own selector in plan 01-06. |
| A5 data plane | PROVEN | `curl -G -H 'X-Loki-Response-Encoding-Flags: categorize-labels' $LOKI/loki/api/v1/query_range --data-urlencode 'query={service_name="ephematool-api"}'` shows `trace_id` and `span_id` under `structuredMetadata`. `scripts/verify-telemetry.sh logs` passes with `\| trace_id="<id>"`. Grafana `GET /api/datasources/uid/loki` contains `derivedFields` (backends.sh). The UI click-through half is deferred to plan 01-08. |
| A6 Tempo 2.10.8 single binary | PROVEN | `curl http://127.0.0.1:3200/ready` returns `ready`; `scripts/verify-telemetry.sh trace` retrieves the trace by `X-Trace-Id` from `/api/traces/<id>` with `ephematool-api` spans. |
| A13 Collector self-metrics `readers` syntax | PROVEN | Collector 0.162.0 starts with `service.telemetry.metrics.readers[].pull.exporter.prometheus`; `up{job="otel-collector"} == 1` in Prometheus. |

Observed Prometheus names and labels (Pitfall 7): `http_server_request_duration_seconds_{count,sum,bucket}` with labels `service_name`, `service_namespace`, `service_version`, `deployment_environment_name`, `http_route` (value `/api/status`, template only), `http_request_method`, `http_response_status_code`, `server_port`; Go runtime `go_goroutine_count`, `go_memory_used_bytes`; Redis pool `db_client_connections_*`; `job` is `ephematool/ephematool-api`. Trace for one request: `GET /api/status` (SERVER), `telemetry:ping publish` (PRODUCER), and a redis `set` (CLIENT) span.

## Performance

- **Duration:** ~40 min
- **Started:** 2026-10-12T02:45Z
- **Completed:** 2026-10-12T03:00Z
- **Tasks:** 3 (Task 1 resolved by prior human approval)
- **Files created:** 36

## Accomplishments

- Compose stack, Collector (redaction, memory limiter, platform pipelines), Tempo, Loki, Prometheus and provisioned Grafana datasources come up with one command, all ports on 127.0.0.1.
- Go API with route-accurate spans and metrics (TestRouteTagging asserts `GET /api/items/{id}` and no raw path labels), X-Trace-Id header, throttled asynq ping producer, committed OpenAPI 3.1 spec (`make check-gen` clean).
- Verification harness: `backends`, `up`, `trace`, `metrics`, `logs` all pass against the running stack; `trace` failed before the API existed (RED).

## Task Commits

1. **Task 1: Package legitimacy gate** - no commit (resolved by user approval)
2. **Task 2: Failing smoke harness, stack and configs (RED)** - `9b8a263` (test)
3. **Task 3: Go API, telemetry, status, spike GREEN** - `53268b7` (feat)

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] `go.opentelemetry.io/otel/sdk/log@v0.23.0` does not exist**
- **Found during:** Task 3 dependency install
- **Issue:** The research pin v0.23.0 is not a published version; sdk/log is at v1.47.0 (otlploghttp v0.23.0 does exist).
- **Fix:** Used `sdk/log@v1.47.0`, same release train as the rest of OTel Go 1.47.0.
- **Files modified:** go.mod, go.sum
- **Commit:** 53268b7

**2. [Rule 1 - Spec drift] A4 query returned empty**
- Covered under Spike Results (FALLBACK-APPLIED); handled by the plan's written fallback.

**3. [Minor] OpenAPI output is pretty-printed**
- The `openapi` subcommand indents JSON so the committed spec matches the `"openapi": "3.1` acceptance text and diffs well. `go run ./cmd/api openapi | diff - api/openapi.json` is empty.

**4. [Minor] Deps.Enqueuer**
- Added an optional `Enqueuer` field on `httpapi.Deps` (keeps `Queue *queue.Client` as specified) so TestStatusEnqueuesPing can fake the producer.

**Total deviations:** 1 blocking fix, 3 minor adjustments. No scope change.

## Issues Encountered

- The Collector logs deprecation warnings for the `otlp`, `otlphttp` exporter aliases (prefer `otlp_grpc`, `otlp_http`). The plan and acceptance criteria name `otlphttp/loki`, so the aliases were kept; they work in 0.162.0. Worth a follow-up rename.
- Docker daemon reported Engine 27.0.3 locally (research already noted this); no impact.
- Untracked pre-existing `README.md` was left untouched.

## Known Stubs

- `services` in `/api/status` is intentionally `[]`; the prober in plan 01-06 fills it.
- `GET /api/status` is unauthenticated for now; plan 01-04 adds the session layer.

## Threat Flags

None beyond the plan's threat model. Mitigations in place: T-01-01 `attributes/redact`, T-01-02 Grafana on 127.0.0.1 with DEV-ONLY comment, T-01-03 only health port 13133 published, T-01-04 `memory_limiter` first and `batch` last, T-01-05 TestRouteTagging, T-01-06 no redisotel on asynq (grep gate), T-01-SC exact pins.

## Self-Check: PASSED

- Files verified present (compose.yaml, collector.yaml, verify-telemetry.sh, promql.go, middleware.go, openapi.json).
- Commits `9b8a263` and `53268b7` exist in git log.
- Acceptance gates: httpapi tests pass, `go vet` clean, trace/metrics/logs verify scripts exit 0, no non-Context slog calls, no redisotel in internal/queue, `make check-gen` clean.
