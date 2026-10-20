# Phase 1b · Local stack: Collector, Tempo, Loki, Prometheus, Grafana, Compose, verify scripts

**Objective:** one command (`bazel run //:up`) starts the whole platform with telemetry flowing, and a set of shell checks proves it: a trace across API and worker, a related metric, correlated logs, a Grafana dashboard, no secrets in stored telemetry, and all ten services healthy.

**Design references:** `.planning/phases/01-telemetry-spine-github-login/01-CONTEXT.md` (D-09..D-13), `01-RESEARCH.md` (pitfalls 3, 4, 7, 8), `01-VALIDATION.md`.
**Requirements covered:** OBS-01.
**Success criteria covered:** SC2 (trace, metric and logs findable in Grafana), SC3 (fresh checkout, one command).
**Prerequisite:** Phase 1a acceptance checklist complete (Go binaries build and tests pass). The `web` image is built in Phase 1c; this phase brings the stack up **without** the `web` service (section 9 shows how).
**Produces (later phases rely on these exact names):**

| Artifact | Used later for |
|---|---|
| Collector pipelines `traces/platform`, `metrics/platform`, `logs/platform` | Phase 2 adds `*/environments` pipelines next to them; never share them |
| `bazel run //:up`, `//:up_fake`, `//:down`, `//:clean`, `//:logs` | every later phase's manual testing |
| `deploy/compose.e2e.yaml` | fake GitHub overlay for dev and Playwright |
| `scripts/verify.d/*.sh` + `scripts/lib/verify-lib.sh` | every later phase adds its own `verify.d` check |
| `tools/verify.sh` | the full local gate |

---

## 1. Directory structure after this phase

```
Dockerfile
.env.example
BUILD.bazel                         # extended
tools/{stack.sh,verify.sh}
deploy/
├── compose.yaml
├── compose.e2e.yaml
├── otel/collector.yaml
├── tempo/tempo.yaml
├── loki/loki.yaml
├── prometheus/prometheus.yml
└── grafana/
    ├── provisioning/{datasources/datasources.yaml,dashboards/dashboards.yaml}
    └── dashboards/ephematool-platform.json
scripts/
├── verify-telemetry.sh
├── lib/verify-lib.sh
└── verify.d/{backends,up,trace,metrics,logs,worker,status,dashboard,redaction,conventions,auth-denied,fresh}.sh
```

```bash
mkdir -p deploy/{otel,tempo,loki,prometheus} deploy/grafana/provisioning/{datasources,dashboards} deploy/grafana/dashboards
mkdir -p scripts/lib scripts/verify.d
```

---

## 2. Go container image

One multi-stage Dockerfile builds any of the three binaries (`CMD=api|worker|fakegithub`) into a distroless, non-root image. Version metadata is injected with `-ldflags -X` into `internal/buildinfo`.

`Dockerfile`

```dockerfile
FROM golang:1.26-alpine AS gobuild
ARG CMD=api
ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILT_AT=unknown
WORKDIR /src
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/ephematool/ephematool/internal/buildinfo.Version=${VERSION} -X github.com/ephematool/ephematool/internal/buildinfo.Commit=${COMMIT} -X github.com/ephematool/ephematool/internal/buildinfo.BuiltAt=${BUILT_AT}" \
    -o /out/app ./cmd/${CMD}

FROM gcr.io/distroless/static-debian12:nonroot AS go-runtime
COPY --from=gobuild /out/app /app
USER nonroot
ENTRYPOINT ["/app"]
CMD ["serve"]
```
---

## 3. OpenTelemetry Collector

Everything the platform emits goes **through the Collector**; the apps never talk to Tempo, Loki or Prometheus directly. That keeps backends swappable and gives one place to redact (T-01-46): the `attributes/redact` processor deletes `authorization`, `cookie` and `set-cookie` attributes from traces and logs before storage.

Notes on the file:

- Pipelines are **named** `traces/platform`, `metrics/platform`, `logs/platform`. Phase 2 will add `*/environments` pipelines for the preview environments' own telemetry, with their own limits. Do not reuse the platform pipelines for that (Pitfall 9).
- OTLP ports (4317/4318) are **not published** to the host. Only the health endpoint is (T-01-03). The browser reaches OTLP through nginx (Phase 1c).
- Collector self-metrics are exposed on `:8888` and scraped by Prometheus.

`deploy/otel/collector.yaml`

```yaml
receivers:
  otlp/platform:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
      http:
        endpoint: 0.0.0.0:4318

processors:
  memory_limiter:
    check_interval: 1s
    limit_mib: 400
    spike_limit_mib: 100
  resource/platform:
    attributes:
      - key: deployment.environment.name
        value: local
        action: upsert
  attributes/redact:
    actions:
      - key: http.request.header.authorization
        action: delete
      - key: http.request.header.cookie
        action: delete
      - key: http.response.header.set-cookie
        action: delete
  batch: {}

exporters:
  otlp/tempo:
    endpoint: tempo:4317
    tls:
      insecure: true
  otlphttp/loki:
    endpoint: http://loki:3100/otlp
  otlphttp/prometheus:
    endpoint: http://prometheus:9090/api/v1/otlp

extensions:
  health_check:
    endpoint: 0.0.0.0:13133

service:
  extensions: [health_check]
  telemetry:
    metrics:
      level: normal
      readers:
        - pull:
            exporter:
              prometheus:
                host: 0.0.0.0
                port: 8888
  pipelines:
    traces/platform:
      receivers: [otlp/platform]
      processors: [memory_limiter, resource/platform, attributes/redact, batch]
      exporters: [otlp/tempo]
    metrics/platform:
      receivers: [otlp/platform]
      processors: [memory_limiter, resource/platform, batch]
      exporters: [otlphttp/prometheus]
    logs/platform:
      receivers: [otlp/platform]
      processors: [memory_limiter, resource/platform, attributes/redact, batch]
      exporters: [otlphttp/loki]
    # Phase 2 reserves receivers otlp/environments and pipelines traces|metrics|logs/environments
    # with a separate memory_limiter and rate limits (PITFALLS 9). Do not share platform pipelines.
```
---

## 4. Backends

### 4.1 Tempo (traces)

`deploy/tempo/tempo.yaml`

```yaml
server:
  http_listen_port: 3200

distributor:
  receivers:
    otlp:
      protocols:
        grpc:
          endpoint: 0.0.0.0:4317

ingester:
  max_block_duration: 5m

compactor:
  compaction:
    block_retention: 168h

storage:
  trace:
    backend: local
    wal:
      path: /var/tempo/wal
    local:
      path: /var/tempo/blocks

# metrics_generator intentionally disabled (no metrics_generator block, no processors enabled)
```
### 4.2 Loki (logs)

`allow_structured_metadata: true` is required: OTLP log attributes (including `trace_id`) arrive as structured metadata.

`deploy/loki/loki.yaml`

```yaml
auth_enabled: false

server:
  http_listen_port: 3100

common:
  instance_addr: 127.0.0.1
  path_prefix: /loki
  replication_factor: 1
  ring:
    kvstore:
      store: inmemory
  storage:
    filesystem:
      chunks_directory: /loki/chunks
      rules_directory: /loki/rules

schema_config:
  configs:
    - from: "2024-01-01"
      store: tsdb
      object_store: filesystem
      schema: v13
      index:
        prefix: index_
        period: 24h

limits_config:
  allow_structured_metadata: true
  retention_period: 168h

analytics:
  reporting_enabled: false
```
### 4.3 Prometheus (metrics)

Prometheus runs with `--web.enable-otlp-receiver` (set in compose), so the Collector pushes metrics to `/api/v1/otlp`. `promote_resource_attributes` turns resource attributes into labels so queries can use `service_name`. `translation_strategy: UnderscoreEscapingWithSuffixes` is what produces names like `http_server_request_duration_seconds_count`; the status page's PromQL (`internal/telemetry/promql.go`) depends on it (Pitfall 7).

`deploy/prometheus/prometheus.yml`

```yaml
global:
  scrape_interval: 15s

otlp:
  promote_resource_attributes:
    - service.name
    - service.namespace
    - service.version
    - deployment.environment.name
  translation_strategy: UnderscoreEscapingWithSuffixes

storage:
  tsdb:
    out_of_order_time_window: 30m

scrape_configs:
  - job_name: otel-collector
    static_configs:
      - targets: ['otel-collector:8888']
  - job_name: tempo
    static_configs:
      - targets: ['tempo:3200']
  - job_name: loki
    static_configs:
      - targets: ['loki:3100']
  - job_name: grafana
    static_configs:
      - targets: ['grafana:3000']
```
### 4.4 Grafana provisioning

The three datasources are linked: a Tempo trace links to Loki logs for the same trace id (`tracesToLogsV2`), and a Loki log line with a `trace_id` links back to Tempo (`derivedFields`). That bidirectional link is the observable form of SC2.

`deploy/grafana/provisioning/datasources/datasources.yaml`

```yaml
apiVersion: 1

datasources:
  - name: Prometheus
    type: prometheus
    uid: prometheus
    access: proxy
    url: http://prometheus:9090
    isDefault: true
    editable: false

  - name: Tempo
    type: tempo
    uid: tempo
    access: proxy
    url: http://tempo:3200
    editable: false
    jsonData:
      tracesToLogsV2:
        datasourceUid: loki
        filterByTraceID: true
        filterBySpanID: false
        tags:
          - key: service.name
            value: service_name
      tracesToMetrics:
        datasourceUid: prometheus
      serviceMap:
        datasourceUid: prometheus

  - name: Loki
    type: loki
    uid: loki
    access: proxy
    url: http://loki:3100
    editable: false
    jsonData:
      derivedFields:
        - name: trace_id
          matcherType: label
          matcherRegex: trace_id
          url: '$${__value.raw}'
          datasourceUid: tempo
```
`deploy/grafana/provisioning/dashboards/dashboards.yaml`

```yaml
apiVersion: 1

providers:
  - name: ephematool
    orgId: 1
    folder: Ephematool
    type: file
    disableDeletion: true
    allowUiUpdates: false
    updateIntervalSeconds: 30
    options:
      path: /var/lib/grafana/dashboards
```
`deploy/grafana/dashboards/ephematool-platform.json`

```json
{
  "uid": "ephematool-platform",
  "title": "Ephematool platform",
  "tags": ["ephematool"],
  "timezone": "browser",
  "schemaVersion": 39,
  "version": 1,
  "editable": true,
  "refresh": "10s",
  "time": { "from": "now-30m", "to": "now" },
  "annotations": { "list": [] },
  "templating": { "list": [] },
  "panels": [
    {
      "id": 1,
      "type": "timeseries",
      "title": "Request rate",
      "description": "API requests per second by route.",
      "datasource": { "type": "prometheus", "uid": "prometheus" },
      "gridPos": { "h": 8, "w": 12, "x": 0, "y": 0 },
      "fieldConfig": { "defaults": { "unit": "reqps" }, "overrides": [] },
      "targets": [
        {
          "refId": "A",
          "datasource": { "type": "prometheus", "uid": "prometheus" },
          "expr": "sum by (http_route) (rate(http_server_request_duration_seconds_count{service_name=\"ephematool-api\"}[1m]))",
          "legendFormat": "{{http_route}}"
        }
      ]
    },
    {
      "id": 2,
      "type": "timeseries",
      "title": "p95 latency",
      "description": "95th percentile API request duration by route.",
      "datasource": { "type": "prometheus", "uid": "prometheus" },
      "gridPos": { "h": 8, "w": 12, "x": 12, "y": 0 },
      "fieldConfig": { "defaults": { "unit": "s" }, "overrides": [] },
      "targets": [
        {
          "refId": "A",
          "datasource": { "type": "prometheus", "uid": "prometheus" },
          "expr": "histogram_quantile(0.95, sum by (le, http_route) (rate(http_server_request_duration_seconds_bucket{service_name=\"ephematool-api\"}[5m])))",
          "legendFormat": "{{http_route}}"
        }
      ]
    },
    {
      "id": 3,
      "type": "timeseries",
      "title": "Error ratio",
      "description": "Share of API responses with a 5xx status.",
      "datasource": { "type": "prometheus", "uid": "prometheus" },
      "gridPos": { "h": 8, "w": 12, "x": 0, "y": 8 },
      "fieldConfig": { "defaults": { "unit": "percentunit", "min": 0, "max": 1 }, "overrides": [] },
      "targets": [
        {
          "refId": "A",
          "datasource": { "type": "prometheus", "uid": "prometheus" },
          "expr": "sum(rate(http_server_request_duration_seconds_count{service_name=\"ephematool-api\",http_response_status_code=~\"5..\"}[5m])) / sum(rate(http_server_request_duration_seconds_count{service_name=\"ephematool-api\"}[5m]))",
          "legendFormat": "5xx ratio"
        }
      ]
    },
    {
      "id": 4,
      "type": "timeseries",
      "title": "Worker tasks",
      "description": "Processed worker tasks per second by type and outcome.",
      "datasource": { "type": "prometheus", "uid": "prometheus" },
      "gridPos": { "h": 8, "w": 12, "x": 12, "y": 8 },
      "fieldConfig": { "defaults": { "unit": "ops" }, "overrides": [] },
      "targets": [
        {
          "refId": "A",
          "datasource": { "type": "prometheus", "uid": "prometheus" },
          "expr": "sum by (task_type, outcome) (rate(ephematool_queue_task_processed_total[5m]))",
          "legendFormat": "{{task_type}} {{outcome}}"
        }
      ]
    },
    {
      "id": 5,
      "type": "barchart",
      "title": "Auth outcomes",
      "description": "Sign-in attempts in the last hour by outcome.",
      "datasource": { "type": "prometheus", "uid": "prometheus" },
      "gridPos": { "h": 8, "w": 12, "x": 0, "y": 16 },
      "fieldConfig": { "defaults": { "unit": "short" }, "overrides": [] },
      "options": { "xField": "outcome", "legend": { "showLegend": false } },
      "targets": [
        {
          "refId": "A",
          "datasource": { "type": "prometheus", "uid": "prometheus" },
          "expr": "sum by (outcome) (increase(ephematool_auth_logins_total[1h]))",
          "format": "table",
          "instant": true
        }
      ],
      "transformations": [
        { "id": "organize", "options": { "excludeByName": { "Time": true }, "renameByName": { "Value": "logins" } } }
      ]
    },
    {
      "id": 6,
      "type": "state-timeline",
      "title": "Service up",
      "description": "Prober result for each platform service (1 = up).",
      "datasource": { "type": "prometheus", "uid": "prometheus" },
      "gridPos": { "h": 8, "w": 12, "x": 12, "y": 16 },
      "fieldConfig": {
        "defaults": {
          "mappings": [
            { "type": "value", "options": { "0": { "text": "down", "color": "red", "index": 0 }, "1": { "text": "up", "color": "green", "index": 1 } } }
          ],
          "color": { "mode": "thresholds" },
          "thresholds": { "mode": "absolute", "steps": [{ "color": "red", "value": null }, { "color": "green", "value": 1 }] }
        },
        "overrides": []
      },
      "targets": [
        {
          "refId": "A",
          "datasource": { "type": "prometheus", "uid": "prometheus" },
          "expr": "ephematool_service_up",
          "legendFormat": "{{service}}"
        }
      ]
    },
    {
      "id": 7,
      "type": "timeseries",
      "title": "Dashboard page views",
      "description": "Browser page views reported by the dashboard SPA.",
      "datasource": { "type": "prometheus", "uid": "prometheus" },
      "gridPos": { "h": 8, "w": 24, "x": 0, "y": 24 },
      "fieldConfig": { "defaults": { "unit": "ops" }, "overrides": [] },
      "targets": [
        {
          "refId": "A",
          "datasource": { "type": "prometheus", "uid": "prometheus" },
          "expr": "sum(rate(ephematool_ui_page_views_total[5m]))",
          "legendFormat": "page views"
        }
      ]
    }
  ]
}
```
---

## 5. Environment file

Copy to `.env` (git-ignored). Every variable has a working default, so you only need a `.env` for a real GitHub login.

`.env.example`

```bash
# Copy to .env (git-ignored). Every variable has a working default for `bazel run //:up`;
# only the GitHub OAuth values are needed for a real login.

# --- Runtime ---
# Dev stack only: Grafana runs with anonymous Admin and every port is bound to 127.0.0.1.
# Deployment environment label stamped on telemetry.
APP_ENV=local
# API listen address inside the container.
HTTP_ADDR=:8081
# Public URL of the dashboard (OAuth callback host).
PUBLIC_BASE_URL=http://localhost:8080
# Public URL of Grafana, used for the status page deep link.
GRAFANA_PUBLIC_URL=http://localhost:3000

# --- Data stores (dev-only credentials; DB and Redis ports are not published) ---
DATABASE_URL=postgres://ephematool:ephematool@postgres:5432/ephematool?sslmode=disable
# Run goose migrations when the API starts.
MIGRATE_ON_START=true
REDIS_ADDR=redis:6379

# --- GitHub OAuth login ---
# Create a GitHub OAuth App at https://github.com/settings/developers
#   Homepage URL:  http://localhost:8080
#   Callback URL:  http://localhost:8080/api/auth/github/callback
# The app requests the read:org scope so private org membership can be checked (D-15).
# Org owners must approve the app (org Settings > Third-party access) if the org restricts
# third-party OAuth apps, otherwise members are denied.
# For a login without credentials use `bazel run //:up_fake` (fake GitHub, dev/e2e only).
GITHUB_CLIENT_ID=
GITHUB_CLIENT_SECRET=
# Overridable endpoints (the e2e fake GitHub overrides these).
GITHUB_AUTHORIZE_URL=https://github.com/login/oauth/authorize
GITHUB_TOKEN_URL=https://github.com/login/oauth/access_token
GITHUB_API_URL=https://api.github.com/
# Comma-separated org logins allowed to sign in.
ALLOWED_ORGS=
# Comma-separated org/team-slug entries allowed to sign in.
ALLOWED_TEAMS=
# Comma-separated GitHub logins that get the admin role; everyone else is a viewer.
ADMIN_LOGINS=

# --- Sessions ---
# DEV DEFAULT false so the cookie works over plain HTTP on localhost. MUST be true for any
# non-localhost deployment (TLS required).
COOKIE_SECURE=false
# Sliding idle expiry (7 days).
SESSION_IDLE_TIMEOUT=168h
# Absolute session cap (30 days).
SESSION_LIFETIME=720h

# --- Telemetry / status ---
# Minimum interval between worker ping enqueues from /api/status.
PING_THROTTLE=10s
# Worker heartbeat period.
HEARTBEAT_INTERVAL=15s
# Service prober period and per-probe timeout.
PROBE_INTERVAL=10s
PROBE_TIMEOUT=2s
PROMETHEUS_URL=http://prometheus:9090
# COLLECTOR_HEALTH_URL, TEMPO_READY_URL, LOKI_READY_URL, PROMETHEUS_READY_URL,
# GRAFANA_HEALTH_URL, WEB_HEALTH_URL default to the in-network service addresses.
```
---

## 6. Compose

Design points:

- **Everything is bound to `127.0.0.1`.** The stack runs Grafana with anonymous Admin (dev only, `T-01-02`), so nothing may listen on a public interface. Postgres and Redis publish no ports at all.
- `depends_on` with `condition: service_healthy` makes the API wait for Postgres and Redis healthchecks instead of crash-looping.
- The worker overrides the image `CMD` with `command: []` because the worker binary has no subcommands (the API image default is `serve`).
- `OTEL_METRIC_EXPORT_INTERVAL=5000` makes metrics appear in Grafana within seconds instead of a minute, which keeps the verify scripts fast.

`deploy/compose.yaml`

`deploy/compose.yaml`

```yaml
name: ephematool

services:
  otel-collector:
    image: otel/opentelemetry-collector-contrib:0.162.0
    command: ["--config=/etc/otelcol/config.yaml"]
    volumes:
      - ./otel/collector.yaml:/etc/otelcol/config.yaml:ro
    ports:
      # health_check extension only; OTLP ports are not published (T-01-03)
      - "127.0.0.1:13133:13133"
    depends_on:
      - tempo
      - loki
      - prometheus

  tempo:
    image: grafana/tempo:2.10.8
    command: ["-config.file=/etc/tempo/tempo.yaml"]
    volumes:
      - ./tempo/tempo.yaml:/etc/tempo/tempo.yaml:ro
      - tempo-data:/var/tempo
    ports:
      - "127.0.0.1:3200:3200"

  loki:
    image: grafana/loki:3.7.8
    command: ["-config.file=/etc/loki/loki.yaml"]
    volumes:
      - ./loki/loki.yaml:/etc/loki/loki.yaml:ro
      - loki-data:/loki
    ports:
      - "127.0.0.1:3100:3100"

  prometheus:
    image: prom/prometheus:v3.15.0
    command:
      - --config.file=/etc/prometheus/prometheus.yml
      - --web.enable-otlp-receiver
      - --storage.tsdb.retention.time=7d
    volumes:
      - ./prometheus/prometheus.yml:/etc/prometheus/prometheus.yml:ro
      - prometheus-data:/prometheus
    ports:
      - "127.0.0.1:9090:9090"

  grafana:
    image: grafana/grafana:13.2.3
    environment:
      # DEV-ONLY: anonymous Admin access. Never ship this outside the local stack (A10, T-01-02).
      GF_AUTH_ANONYMOUS_ENABLED: "true"
      GF_AUTH_ANONYMOUS_ORG_ROLE: Admin
      GF_AUTH_DISABLE_LOGIN_FORM: "true"
    volumes:
      - ./grafana/provisioning:/etc/grafana/provisioning:ro
      - ./grafana/dashboards:/var/lib/grafana/dashboards:ro
      - grafana-data:/var/lib/grafana
    ports:
      - "127.0.0.1:3000:3000"
    depends_on:
      - prometheus
      - tempo
      - loki

  postgres:
    image: postgres:18.6-alpine
    environment:
      # DEV-ONLY credentials; the port is not published (T-01-07)
      POSTGRES_USER: ephematool
      POSTGRES_PASSWORD: ephematool
      POSTGRES_DB: ephematool
    volumes:
      - postgres-data:/var/lib/postgresql
    healthcheck:
      test: ["CMD-SHELL", "pg_isready -U ephematool"]
      interval: 5s
      timeout: 3s
      retries: 20

  redis:
    image: redis:8.8-alpine
    healthcheck:
      test: ["CMD", "redis-cli", "ping"]
      interval: 5s
      timeout: 3s
      retries: 20

  api:
    build:
      context: ..
      dockerfile: Dockerfile
      target: go-runtime
      args:
        CMD: api
        VERSION: ${VERSION:-dev}
        COMMIT: ${GIT_COMMIT:-unknown}
        BUILT_AT: ${BUILT_AT:-unknown}
    environment:
      OTEL_SERVICE_NAME: ephematool-api
      OTEL_EXPORTER_OTLP_ENDPOINT: http://otel-collector:4318
      OTEL_EXPORTER_OTLP_PROTOCOL: http/protobuf
      OTEL_METRIC_EXPORT_INTERVAL: "5000"
      APP_ENV: ${APP_ENV:-local}
      HTTP_ADDR: ${HTTP_ADDR:-:8081}
      # Browser-facing origin: the nginx front door (also the OAuth callback base).
      PUBLIC_BASE_URL: ${PUBLIC_BASE_URL:-http://localhost:8080}
      GRAFANA_PUBLIC_URL: ${GRAFANA_PUBLIC_URL:-http://localhost:3000}
      DATABASE_URL: ${DATABASE_URL:-postgres://ephematool:ephematool@postgres:5432/ephematool?sslmode=disable}
      MIGRATE_ON_START: ${MIGRATE_ON_START:-true}
      REDIS_ADDR: ${REDIS_ADDR:-redis:6379}
      GITHUB_CLIENT_ID: ${GITHUB_CLIENT_ID:-}
      GITHUB_CLIENT_SECRET: ${GITHUB_CLIENT_SECRET:-}
      GITHUB_AUTHORIZE_URL: ${GITHUB_AUTHORIZE_URL:-https://github.com/login/oauth/authorize}
      GITHUB_TOKEN_URL: ${GITHUB_TOKEN_URL:-https://github.com/login/oauth/access_token}
      GITHUB_API_URL: ${GITHUB_API_URL:-https://api.github.com/}
      ALLOWED_ORGS: ${ALLOWED_ORGS:-}
      ALLOWED_TEAMS: ${ALLOWED_TEAMS:-}
      ADMIN_LOGINS: ${ADMIN_LOGINS:-}
      COOKIE_SECURE: ${COOKIE_SECURE:-false}
      SESSION_IDLE_TIMEOUT: ${SESSION_IDLE_TIMEOUT:-168h}
      SESSION_LIFETIME: ${SESSION_LIFETIME:-720h}
      PING_THROTTLE: ${PING_THROTTLE:-10s}
      HEARTBEAT_INTERVAL: ${HEARTBEAT_INTERVAL:-15s}
      PROBE_INTERVAL: ${PROBE_INTERVAL:-10s}
      PROBE_TIMEOUT: ${PROBE_TIMEOUT:-2s}
      PROMETHEUS_URL: ${PROMETHEUS_URL:-http://prometheus:9090}
    ports:
      - "127.0.0.1:8081:8081"
    depends_on:
      postgres:
        condition: service_healthy
      redis:
        condition: service_healthy
      otel-collector:
        condition: service_started

  web:
    build:
      context: ..
      dockerfile: web/Dockerfile
      args:
        VERSION: ${VERSION:-dev}
        COMMIT: ${GIT_COMMIT:-unknown}
        BUILT_AT: ${BUILT_AT:-unknown}
    ports:
      - "127.0.0.1:8080:8080"
    depends_on:
      api:
        condition: service_started
      otel-collector:
        condition: service_started

  worker:
    build:
      context: ..
      dockerfile: Dockerfile
      target: go-runtime
      args:
        CMD: worker
        VERSION: ${VERSION:-dev}
        COMMIT: ${GIT_COMMIT:-unknown}
        BUILT_AT: ${BUILT_AT:-unknown}
    # The worker binary has no subcommands; ignore the image default CMD.
    command: []
    environment:
      OTEL_SERVICE_NAME: ephematool-worker
      OTEL_EXPORTER_OTLP_ENDPOINT: http://otel-collector:4318
      OTEL_EXPORTER_OTLP_PROTOCOL: http/protobuf
      OTEL_METRIC_EXPORT_INTERVAL: "5000"
      APP_ENV: ${APP_ENV:-local}
      REDIS_ADDR: ${REDIS_ADDR:-redis:6379}
      HEARTBEAT_INTERVAL: ${HEARTBEAT_INTERVAL:-15s}
      GITHUB_API_URL: ${GITHUB_API_URL:-https://api.github.com/}
    depends_on:
      redis:
        condition: service_healthy
      otel-collector:
        condition: service_started

volumes:
```
The fake GitHub overlay is used for development and for Playwright. It points the API's OAuth URLs at the fake and turns the ping throttle off (the throttle is global, so parallel e2e specs would starve each other). It is a separate file so it can never ship by accident.

`deploy/compose.e2e.yaml`

```yaml
# Overlay for dev/e2e login with the fake GitHub. NOT for real deployments (T-01-26).
services:
  fakegithub:
    build:
      context: ..
      dockerfile: Dockerfile
      target: go-runtime
      args:
        CMD: fakegithub
    # The fakegithub binary has no subcommands; ignore the image default CMD.
    command: []
    environment:
      FAKEGITHUB_ADDR: ":9999"
    ports:
      - "127.0.0.1:9999:9999"

  api:
    environment:
      - GITHUB_CLIENT_ID=e2e-client
      - GITHUB_CLIENT_SECRET=e2e-secret
      # Browser-facing authorize URL; server-facing token and API URLs.
      - GITHUB_AUTHORIZE_URL=http://localhost:9999/login/oauth/authorize
      - GITHUB_TOKEN_URL=http://fakegithub:9999/login/oauth/access_token
      - GITHUB_API_URL=http://fakegithub:9999/
      - ALLOWED_ORGS=ephematool-e2e
      - ALLOWED_TEAMS=partner-org/sre
      - ADMIN_LOGINS=Octo-Admin
      # The ping throttle is global, so parallel specs would steal each other's window and a page
      # load would get no worker span. Disabled for e2e only (the default stays 10s).
      - PING_THROTTLE=0
    depends_on:
      fakegithub:
        condition: service_started
```
---

## 7. Root `BUILD.bazel`, `tools/stack.sh`

`bazel run //:up` is a thin wrapper over `docker compose`. It must not call `bazel` itself: a nested Bazel inside `bazel run` blocks on the output-base lock, which is why the full gate (`tools/verify.sh`) is a plain script, not a target.

Replace the bootstrap root `BUILD.bazel` with this version. The `//web` entries are added in Phase 1c.

`BUILD.bazel`

```python
load("@bazel_gazelle//:def.bzl", "gazelle")
load("@bazel_lib//lib:write_source_files.bzl", "write_source_files")
load("@rules_shell//shell:sh_binary.bzl", "sh_binary")

# gazelle:prefix github.com/ephematool/ephematool
# gazelle:go_naming_convention import
# gazelle:proto disable_global
# gazelle:build_tags integration
# gazelle:map_kind go_test go_test //tools/bzl:go.bzl
# gazelle:exclude web
# gazelle:exclude deploy
# gazelle:exclude scripts
# gazelle:exclude .planning
# gazelle:exclude bazel-*

exports_files([
    "go.mod",
    "go.sum",
    "sqlc.yaml",
    ".bazelignore",
])

# bazel run //:gazelle  -- regenerate Go BUILD files after adding/removing imports or files.
gazelle(name = "gazelle")

# Docker-backed Go integration tests (`//go:build integration`, testcontainers).
#   bazel test --config=integration //:test_integration
# --config=integration builds with the `integration` tag, drops -test.short and runs the
# tests outside the sandbox with the Docker socket reachable. Add new packages that
# contain integration-tagged tests here.
test_suite(
    name = "test_integration",
    tests = [
        "//internal/auth:auth_test",
    ],
)

# ---------------------------------------------------------------- codegen
# bazel run //:gen        -- regenerate every committed generated file in place
# bazel test //:check_gen -- fail if any committed generated file is stale (drift check)
_GEN_PACKAGES = [
    "//api",
    "//internal/conventions",
    "//internal/store",
]

write_source_files(
    name = "gen",
    additional_update_targets = ["%s:gen" % p for p in _GEN_PACKAGES],
)

test_suite(
    name = "check_gen",
    tests = ["%s:gen_tests" % p for p in _GEN_PACKAGES],
)

# ---------------------------------------------------------------- local stack
# `bazel run` wrappers around docker compose and scripts/ (see tools/stack.sh). They run in
# the workspace root and stamp GIT_COMMIT / BUILT_AT. Images are still built from the
# Dockerfiles by compose. The ordered full gate is ./tools/verify.sh (not a bazel target:
# bazel must not be invoked from inside `bazel run`).
[
    sh_binary(
        name = name,
        srcs = ["tools/stack.sh"],
        args = [cmd],
        tags = [
            "local",
            "manual",
            "requires-docker",
        ],
    )
    for name, cmd in [
        ("up", "up"),
        ("up_fake", "up-fake"),
        ("down", "down"),
        ("clean", "clean"),
        ("logs", "logs"),
        ("verify_telemetry", "verify-telemetry"),
    ]
]

# SC3: fresh clone, one command, all 10 services healthy. bazel run //:verify_fresh
sh_binary(
    name = "verify_fresh",
    srcs = ["tools/stack.sh"],
    args = [
        "verify-telemetry",
        "fresh",
    ],
    tags = [
        "local",
        "manual",
        "requires-docker",
    ],
)

```
`tools/stack.sh`

```bash
#!/usr/bin/env bash
# Local compose stack orchestration behind the `bazel run` targets in //:BUILD.bazel:
#
#   bazel run //:up                 docker compose up --build --wait
#   bazel run //:up_fake            same, plus the fake-GitHub e2e overlay
#   bazel run //:down               stop the stack (both overlays)
#   bazel run //:clean              stop the stack and delete its volumes
#   bazel run //:logs               follow the stack logs
#   bazel run //:verify_telemetry   scripts/verify-telemetry.sh all
#   bazel run //:verify_telemetry -- trace   (one subcommand)
#
# Runs in the workspace root (BUILD_WORKSPACE_DIRECTORY, set by `bazel run`) and stamps
# GIT_COMMIT and BUILT_AT into the compose environment for the image build args.
# Never call bazel from here: a nested bazel inside `bazel run` blocks on the output base lock.
set -euo pipefail

if [ -n "${BUILD_WORKSPACE_DIRECTORY:-}" ]; then
  ROOT="$BUILD_WORKSPACE_DIRECTORY"
else
  ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fi
cd "$ROOT"

GIT_COMMIT="${GIT_COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)}"
BUILT_AT="${BUILT_AT:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
export GIT_COMMIT BUILT_AT



COMPOSE=(docker compose -f deploy/compose.yaml)
COMPOSE_FAKE=("${COMPOSE[@]}" -f deploy/compose.e2e.yaml)

if [ $# -lt 1 ]; then
  echo "usage: $0 <up|up-fake|down|clean|logs|verify-telemetry> [args...]" >&2
  exit 2
fi

cmd="$1"
shift

case "$cmd" in
  up)
    exec "${COMPOSE[@]}" up --build --wait "$@"
    ;;
  up-fake)
    exec "${COMPOSE_FAKE[@]}" up --build --wait "$@"
    ;;
  down)
    "${COMPOSE_FAKE[@]}" down "$@" || true
    exec "${COMPOSE[@]}" down "$@"
    ;;
  clean)
    "${COMPOSE_FAKE[@]}" down -v "$@" || true
    exec "${COMPOSE[@]}" down -v "$@"
    ;;
  logs)
    exec "${COMPOSE[@]}" logs -f "$@"
    ;;
  verify-telemetry)
    if [ $# -eq 0 ]; then
      set -- all
    fi
    exec scripts/verify-telemetry.sh "$@"
    ;;
  *)
    echo "unknown command: $cmd" >&2
    exit 2
    ;;
esac
```
Make it executable:

```bash
chmod +x tools/stack.sh tools/verify.sh scripts/verify-telemetry.sh scripts/verify.d/*.sh
```

---

## 8. Verify scripts

These are the executable form of the success criteria. `scripts/verify-telemetry.sh all` runs every check in `scripts/verify.d/` in alphabetical order (except `fresh`). Each check prints `PASS:` lines and exits non-zero on the first failure.

`scripts/verify-telemetry.sh`

```bash
#!/usr/bin/env bash
# Dispatcher: scripts/verify-telemetry.sh <subcommand|all>
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIR="$ROOT/scripts/verify.d"

available() {
  for f in "$DIR"/*.sh; do
    basename "$f" .sh
  done
}

if [ $# -lt 1 ]; then
  echo "usage: $0 <subcommand|all>" >&2
  echo "available: $(available | tr '\n' ' ')all" >&2
  exit 2
fi

sub="$1"
shift

if [ "$sub" = "all" ]; then
  for f in "$DIR"/*.sh; do
    # fresh builds a second stack from a clone (bazel run //:verify_fresh); never part of `all`.
    case "$(basename "$f")" in fresh*) continue ;; esac
    echo "== $(basename "$f" .sh)"
    bash "$f" "$@"
  done
  exit 0
fi

if [ ! -f "$DIR/$sub.sh" ]; then
  echo "unknown subcommand: $sub" >&2
  echo "available: $(available | tr '\n' ' ')all" >&2
  exit 2
fi

exec bash "$DIR/$sub.sh" "$@"
```
`scripts/lib/verify-lib.sh`

```bash
#!/usr/bin/env bash
# Shared helpers for scripts/verify.d/*.sh. Source this file; do not execute it.

# Through nginx (the same-origin front door), not the API port.
API_URL="${API_URL:-http://127.0.0.1:8080}"
TEMPO_URL="${TEMPO_URL:-http://127.0.0.1:3200}"
PROM_URL="${PROM_URL:-http://127.0.0.1:9090}"
LOKI_URL="${LOKI_URL:-http://127.0.0.1:3100}"
GRAFANA_URL="${GRAFANA_URL:-http://127.0.0.1:3000}"
COLLECTOR_HEALTH_URL="${COLLECTOR_HEALTH_URL:-http://127.0.0.1:13133/}"
VERIFY_TMP="${VERIFY_TMP:-$(mktemp -d)}"

pass() { echo "PASS: $*"; }
fail() { echo "FAIL: $*" >&2; exit 1; }

# wait_http URL TIMEOUT_SECONDS
wait_http() {
  local url="$1" timeout="${2:-60}" i=0
  while [ "$i" -lt "$timeout" ]; do
    if curl -fsS -o /dev/null --max-time 3 "$url" 2>/dev/null; then
      return 0
    fi
    sleep 1
    i=$((i + 1))
  done
  fail "timed out after ${timeout}s waiting for $url"
}

# header_value FILE NAME -> value of the first matching response header
header_value() {
  tr -d '\r' < "$1" | awk -v n="$(echo "$2" | tr '[:upper:]' '[:lower:]'):" 'tolower($1)==n {print $2; exit}'
}

# ensure_session [LOGIN=${LOGIN:-octo-admin}]
# Signs in through the fake GitHub with curl and leaves ephematool_session in $VERIFY_TMP/cookies.
ensure_session() {
  local login="${1:-${LOGIN:-octo-admin}}" jar="$VERIFY_TMP/cookies" loc cb
  : > "$jar"
  curl -sS -o /dev/null -D "$VERIFY_TMP/oauth-start" -c "$jar" -b "$jar" \
    "$API_URL/api/auth/github/login" || fail "ensure_session: login start request failed"
  loc="$(header_value "$VERIFY_TMP/oauth-start" location)"
  [ -n "$loc" ] || fail "ensure_session: /api/auth/github/login returned no Location (is GitHub configured? use bazel run //:up_fake)"
  curl -sS -o /dev/null -D "$VERIFY_TMP/oauth-authorize" "${loc}&login=${login}" \
    || fail "ensure_session: fake GitHub authorize request failed"
  cb="$(header_value "$VERIFY_TMP/oauth-authorize" location)"
  [ -n "$cb" ] || fail "ensure_session: fake GitHub returned no callback Location for $login"
  # The callback URL carries PUBLIC_BASE_URL's origin (localhost); replay it against API_URL
  # so the cookie jar sees a single host.
  cb="$(printf '%s' "$cb" | sed -E 's#^https?://[^/]+##')"
  curl -sS -o /dev/null -c "$jar" -b "$jar" "$API_URL$cb" || fail "ensure_session: callback request failed"
  grep -q 'ephematool_session' "$jar" || fail "ensure_session: no ephematool_session cookie after callback for $login"
}

# api_get PATH -> body on stdout; headers in $VERIFY_TMP/last-headers; trace id in $VERIFY_TMP/last-trace-id
# Sends the session cookie jar; on a 401 it signs in once and retries.
api_get() {
  local path="$1" jar="$VERIFY_TMP/cookies" code attempt
  [ -f "$jar" ] || : > "$jar"
  for attempt in 1 2; do
    code="$(curl -sS -o "$VERIFY_TMP/last-body" -w '%{http_code}' -D "$VERIFY_TMP/last-headers" \
      -b "$jar" -c "$jar" "$API_URL$path")" || return 1
    if [ "$code" = "401" ] && [ "$attempt" = "1" ]; then
      ensure_session
      continue
    fi
    break
  done
  case "$code" in
    2??) ;;
    *) echo "GET $path -> HTTP $code" >&2; return 1 ;;
  esac
  header_value "$VERIFY_TMP/last-headers" x-trace-id > "$VERIFY_TMP/last-trace-id"
  cat "$VERIFY_TMP/last-body"
}

# tempo_services TRACE_ID [TIMEOUT] -> sorted unique service.name values
tempo_services() {
  local id="$1" timeout="${2:-30}" waited=0 out=""
  while [ "$waited" -le "$timeout" ]; do
    if curl -fsS --max-time 5 "$TEMPO_URL/api/traces/$id" -o "$VERIFY_TMP/trace.json" 2>/dev/null; then
      out="$(jq -r '
        ([.batches[]?, .resourceSpans[]?]
          | .[]
          | .resource.attributes[]?
          | select(.key == "service.name")
          | .value.stringValue) // empty' "$VERIFY_TMP/trace.json" 2>/dev/null | sort -u)"
      if [ -n "$out" ]; then
        echo "$out"
        return 0
      fi
    fi
    sleep 2
    waited=$((waited + 2))
  done
  return 0
}

# prom_value PROMQL -> first sample value or empty
prom_value() {
  curl -fsS --max-time 5 --get "$PROM_URL/api/v1/query" --data-urlencode "query=$1" 2>/dev/null \
    | jq -r '.data.result[0].value[1] // empty'
}

# loki_count LOGQL [SINCE_SECONDS] -> number of log lines
loki_count() {
  local q="$1" since="${2:-900}" now start
  now="$(date +%s)"
  start=$((now - since))
  curl -fsS --max-time 10 --get "$LOKI_URL/loki/api/v1/query_range" \
    --data-urlencode "query=$q" \
    --data-urlencode "start=${start}000000000" \
    --data-urlencode "end=${now}000000000" \
    --data-urlencode "limit=1000" 2>/dev/null \
    | jq -r '[.data.result[]?.values | length] | add // 0'
}
```
Individual checks (each is small; read them, they document the behaviour):

`scripts/verify.d/backends.sh`

```bash
#!/usr/bin/env bash
set -euo pipefail
. "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/lib/verify-lib.sh"

wait_http "$TEMPO_URL/ready" 120
pass "tempo ready"
wait_http "$LOKI_URL/ready" 120
pass "loki ready"
wait_http "$PROM_URL/-/ready" 120
pass "prometheus ready"
wait_http "$GRAFANA_URL/api/health" 120
pass "grafana ready"
wait_http "$COLLECTOR_HEALTH_URL" 120
pass "collector healthy"

curl -fsS "$GRAFANA_URL/api/datasources/uid/loki" | grep -q derivedFields \
  || fail "loki datasource missing derivedFields"
pass "loki datasource has derivedFields"
curl -fsS "$GRAFANA_URL/api/datasources/uid/tempo" | grep -q tracesToLogsV2 \
  || fail "tempo datasource missing tracesToLogsV2"
pass "tempo datasource has tracesToLogsV2"
```
`scripts/verify.d/up.sh`

```bash
#!/usr/bin/env bash
set -euo pipefail
D="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
bash "$D/backends.sh"
. "$D/../lib/verify-lib.sh"

# /api/status needs a session now; the public auth config proves api behind nginx is up.
wait_http "$API_URL/api/auth/config" 120
pass "api /api/auth/config returns 200 through nginx"

code="$(curl -sS -o /dev/null -w '%{http_code}' "$API_URL/api/status")"
[ "$code" = "401" ] || fail "unauthenticated /api/status returned $code, expected 401"
pass "unauthenticated /api/status returns 401"

# SC3: all 10 services report healthy in /api/status within 120s of this script starting.
# The dashboard row's telemetry comes from a browser, which only exports while a page is open;
# with no browser it may legitimately be degraded with a "telemetry signal" detail (the
# Playwright status spec asserts all 10 Healthy with a live browser).
ALL_OK='(.services | length == 10) and ([.services[] | select(.id != "dashboard") | .status] | all(. == "healthy"))
  and ([.services[] | select(.id == "dashboard") | (.status == "healthy" or (.status == "degraded" and (.detail | test("telemetry signal"))))] | all)'
START=0  # SECONDS counts from the start of this script
ensure_session
last=""
while [ $((SECONDS - START)) -lt 120 ]; do
  if body="$(api_get /api/status 2>/dev/null)" && echo "$body" | jq -e "$ALL_OK" >/dev/null 2>&1; then
    pass "all 10 services healthy in /api/status after $((SECONDS - START))s"
    exit 0
  fi
  last="$(echo "${body:-}" | jq -c '[.services[]? | select(.status != "healthy") | {id, status}]' 2>/dev/null || true)"
  sleep 2
done
fail "not all 10 services healthy within 120s (not healthy: ${last:-unknown})"
```
`scripts/verify.d/trace.sh`

```bash
#!/usr/bin/env bash
set -euo pipefail
. "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/lib/verify-lib.sh"

api_get /api/status > /dev/null || fail "GET /api/status failed"
id="$(cat "$VERIFY_TMP/last-trace-id")"
[ -n "$id" ] || fail "no X-Trace-Id header on /api/status"

services="$(tempo_services "$id" 45)"
echo "$services" | grep -qx 'ephematool-api' \
  || fail "trace $id has no ephematool-api spans (services: ${services:-none})"
pass "trace $id contains ephematool-api"
```
`scripts/verify.d/metrics.sh`

```bash
#!/usr/bin/env bash
set -euo pipefail
. "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/lib/verify-lib.sh"

api_get /api/status > /dev/null || fail "GET /api/status failed"

q='sum(http_server_request_duration_seconds_count{service_name="ephematool-api",http_route="/api/status"})'
i=0
while [ "$i" -lt 30 ]; do
  v="$(prom_value "$q")"
  if [ -n "$v" ] && awk -v v="$v" 'BEGIN{exit !(v > 0)}'; then
    pass "http_server_request_duration_seconds_count = $v"
    exit 0
  fi
  sleep 2
  i=$((i + 2))
done
fail "no http_server_request_duration_seconds_count for /api/status within 30s"
```
`scripts/verify.d/logs.sh`

```bash
#!/usr/bin/env bash
set -euo pipefail
. "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/lib/verify-lib.sh"

api_get /api/status > /dev/null || fail "GET /api/status failed"
id="$(cat "$VERIFY_TMP/last-trace-id")"
[ -n "$id" ] || fail "no X-Trace-Id header on /api/status"

i=0
while [ "$i" -lt 30 ]; do
  n="$(loki_count "{service_name=\"ephematool-api\"} | trace_id=\"$id\"" 300 || echo 0)"
  if [ -n "$n" ] && [ "$n" -ge 1 ] 2>/dev/null; then
    pass "loki returned $n line(s) for trace $id"
    exit 0
  fi
  sleep 2
  i=$((i + 2))
done
fail "no loki lines for ephematool-api with trace_id=$id within 30s"
```
`scripts/verify.d/worker.sh`

```bash
#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
. "$ROOT/scripts/lib/verify-lib.sh"

# PING_THROTTLE (default 10s) may skip the enqueue, so retry a few times.
id=""
services=""
for attempt in 1 2 3; do
  api_get /api/status > /dev/null || fail "GET /api/status failed"
  id="$(cat "$VERIFY_TMP/last-trace-id")"
  [ -n "$id" ] || fail "no X-Trace-Id header on /api/status"
  services="$(tempo_services "$id" 30)"
  if echo "$services" | grep -qx 'ephematool-worker'; then
    break
  fi
  [ "$attempt" -lt 3 ] && sleep 11
done
echo "$services" | grep -qx 'ephematool-api' \
  || fail "trace $id has no ephematool-api spans (services: ${services:-none})"
echo "$services" | grep -qx 'ephematool-worker' \
  || fail "trace $id has no ephematool-worker spans (services: ${services:-none})"
pass "trace $id spans ephematool-api and ephematool-worker"

n=0
for _ in $(seq 1 15); do
  n="$(loki_count "{service_name=\"ephematool-worker\"} | trace_id=\"$id\"")"
  [ "${n:-0}" -ge 1 ] && break
  sleep 2
done
[ "${n:-0}" -ge 1 ] || fail "no Loki worker logs with trace_id=$id"
pass "Loki has $n worker log line(s) for trace $id"

v=""
for _ in $(seq 1 15); do
  v="$(prom_value 'sum(ephematool_queue_task_processed_total{service_name="ephematool-worker"})')"
  [ -n "$v" ] && awk -v v="$v" 'BEGIN{exit !(v>0)}' && break
  sleep 2
done
[ -n "$v" ] && awk -v v="$v" 'BEGIN{exit !(v>0)}' \
  || fail "ephematool_queue_task_processed_total not > 0 (got '${v:-empty}')"
pass "worker task counter = $v"

exists="$(docker compose -f "$ROOT/deploy/compose.yaml" exec -T redis redis-cli EXISTS worker:heartbeat | tr -d '\r\n ')"
[ "$exists" = "1" ] || fail "redis key worker:heartbeat missing (EXISTS=$exists)"
pass "redis key worker:heartbeat exists"
```
`scripts/verify.d/status.sh`

```bash
#!/usr/bin/env bash
# 10-service /api/status check, then stop/start loki to prove Down is reflected within 2 probe cycles.
set -euo pipefail
D="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "$D/../lib/verify-lib.sh"

ROOT="$(cd "$D/../.." && pwd)"
COMPOSE=(docker compose -f "$ROOT/deploy/compose.yaml")
[ -f "$ROOT/deploy/compose.e2e.yaml" ] && COMPOSE=(docker compose -f "$ROOT/deploy/compose.yaml" -f "$ROOT/deploy/compose.e2e.yaml")

WANT_IDS='["api","worker","dashboard","postgres","redis","otel-collector","tempo","loki","prometheus","grafana"]'

# Always bring loki back, even when an assertion fails (the stack must not stay broken).
restore_loki() { "${COMPOSE[@]}" start loki >/dev/null 2>&1 || true; }
trap restore_loki EXIT

status_of() { # status_of ID -> status string ("" if the call failed)
  api_get /api/status 2>/dev/null | jq -r --arg id "$1" '.services[] | select(.id==$id) | .status' 2>/dev/null || true
}

# wait_status ID WANT TIMEOUT
wait_status() {
  local id="$1" want="$2" timeout="$3" i=0 got=""
  while [ "$i" -lt "$timeout" ]; do
    got="$(status_of "$id")"
    [ "$got" = "$want" ] && return 0
    sleep 1
    i=$((i + 1))
  done
  fail "$id did not reach '$want' within ${timeout}s (last: '${got}')"
}

# 1. All 10 services, exact order, all healthy (poll up to 120s from stack start).
# The dashboard row's telemetry comes from a browser, which only exports while a page is open.
# This script has no browser, so "dashboard" may legitimately be degraded with a telemetry-signal
# detail; the Playwright status spec asserts all 10 rows Healthy with a live browser.
ALL_OK='(.services | length == 10) and ([.services[] | select(.id != "dashboard") | .status] | all(. == "healthy"))
  and ([.services[] | select(.id == "dashboard") | (.status == "healthy" or (.status == "degraded" and (.detail | test("telemetry signal"))))] | all)'
ensure_session
deadline=$((SECONDS + 120))
body=""
while :; do
  body="$(api_get /api/status 2>/dev/null || true)"
  if [ -n "$body" ] && echo "$body" | jq -e "$ALL_OK" >/dev/null 2>&1; then
    break
  fi
  [ "$SECONDS" -lt "$deadline" ] || {
    echo "$body" | jq -c '.services[]? | {id, status, detail}' >&2 || true
    fail "/api/status did not report 10 services (dashboard may lack a browser signal, the rest healthy) within 120s"
  }
  sleep 2
done
echo "$body" | jq -e --argjson want "$WANT_IDS" '[.services[].id] == $want' >/dev/null \
  || fail "service ids are not in the contract order"
pass "/api/status reports 10 services in order, all healthy (dashboard healthy or awaiting a browser signal)"

# 2. Stop loki: Down within 25s (2 probe cycles + margin).
"${COMPOSE[@]}" stop loki >/dev/null
wait_status loki down 25
pass "loki reported down within 25s of stopping it"

# 3. Start loki: healthy again within 60s.
"${COMPOSE[@]}" start loki >/dev/null
wait_status loki healthy 60
pass "loki healthy again after restart"
```
`scripts/verify.d/dashboard.sh`

```bash
#!/usr/bin/env bash
# Dashboard telemetry: the browser trace id captured by the Playwright telemetry spec
# resolves in Tempo to dashboard + api spans, and dashboard metrics and logs are queryable.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
. "$ROOT/scripts/lib/verify-lib.sh"

id="${1:-}"
if [ -z "$id" ] && [ -f "$ROOT/web/test-results/dashboard-trace-id.txt" ]; then
  id="$(tr -d '[:space:]' < "$ROOT/web/test-results/dashboard-trace-id.txt")"
fi
[ -n "$id" ] || fail "no trace id: pass one as the first argument, or run bazel run //:e2e first (writes web/test-results/dashboard-trace-id.txt)"

services="$(tempo_services "$id" 45)"
for svc in ephematool-dashboard ephematool-api; do
  echo "$services" | grep -qx "$svc" \
    || fail "trace $id has no $svc spans (services: ${services:-none})"
done
pass "trace $id contains ephematool-dashboard and ephematool-api"

# Browser metrics export every 15s, so poll.
v=""
for _ in $(seq 1 23); do
  v="$(prom_value 'sum(ephematool_ui_page_views_total{service_name="ephematool-dashboard"})')"
  if [ -n "$v" ] && awk -v v="$v" 'BEGIN{exit !(v > 0)}'; then break; fi
  sleep 2
done
[ -n "$v" ] && awk -v v="$v" 'BEGIN{exit !(v > 0)}' \
  || fail "ephematool_ui_page_views_total{service_name=\"ephematool-dashboard\"} not > 0 (got '${v:-empty}')"
pass "dashboard page view counter = $v"

n=0
for _ in $(seq 1 23); do
  n="$(loki_count '{service_name="ephematool-dashboard"}')"
  [ "${n:-0}" -ge 1 ] && break
  sleep 2
done
[ "${n:-0}" -ge 1 ] || fail "no Loki logs for service_name=ephematool-dashboard"
pass "Loki has $n dashboard log line(s)"
```
`scripts/verify.d/conventions.sh`

```bash
#!/usr/bin/env bash
# D-09 grep gate: the label/attribute prefixes must not be hand-written outside
# conventions/telemetry.json and the generated files. Test files are exempt
# because they assert the literal values.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
. "$ROOT/scripts/lib/verify-lib.sh"
cd "$ROOT"

dirs=()
for d in cmd internal web/src deploy; do [ -d "$d" ] && dirs+=("$d"); done

matches="$(grep -rnF --include='*.go' --include='*.ts' --include='*.tsx' --include='*.yaml' --include='*.json' \
  -e 'ephematool.io/' -e 'ephematool.env.' "${dirs[@]}" 2>/dev/null \
  | grep -v -e '\.gen\.' -e 'conventions/telemetry\.json' -e '_test\.go:' || true)"

[ -z "$matches" ] || fail "hand-written D-09 prefixes found (use internal/conventions or web/src/gen/conventions.gen.ts):
$matches"
pass "no hand-written D-09 prefixes outside generated files"
```
`scripts/verify.d/auth-denied.sh`

```bash
#!/usr/bin/env bash
# A non-member sign-in must leave an auth.denied log line carrying the login in Loki (D-04).
set -euo pipefail
. "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/lib/verify-lib.sh"

# The fake login for octo-outsider ends in a /denied redirect and no session cookie,
# so ensure_session (which asserts a cookie) is not used; replay its steps instead.
jar="$VERIFY_TMP/denied-cookies"
: > "$jar"
curl -sS -o /dev/null -D "$VERIFY_TMP/denied-start" -c "$jar" -b "$jar" \
  "$API_URL/api/auth/github/login" || fail "login start request failed"
loc="$(header_value "$VERIFY_TMP/denied-start" location)"
[ -n "$loc" ] || fail "no Location from /api/auth/github/login (use bazel run //:up_fake)"
curl -sS -o /dev/null -D "$VERIFY_TMP/denied-authorize" "${loc}&login=octo-outsider" \
  || fail "fake GitHub authorize request failed"
cb="$(header_value "$VERIFY_TMP/denied-authorize" location)"
[ -n "$cb" ] || fail "fake GitHub returned no callback Location"
cb="$(printf '%s' "$cb" | sed -E 's#^https?://[^/]+##')"
curl -sS -o /dev/null -D "$VERIFY_TMP/denied-callback" -c "$jar" -b "$jar" "$API_URL$cb" \
  || fail "callback request failed"
dest="$(header_value "$VERIFY_TMP/denied-callback" location)"
case "$dest" in
  /denied*) ;;
  *) fail "expected redirect to /denied, got '${dest}'" ;;
esac

i=0
while [ "$i" -lt 30 ]; do
  n="$(loki_count '{service_name="ephematool-api"} |= "auth.denied" | login="octo-outsider"' 300 || echo 0)"
  if [ -n "$n" ] && [ "$n" -ge 1 ] 2>/dev/null; then
    pass "loki returned $n auth.denied line(s) for octo-outsider"
    exit 0
  fi
  sleep 2
  i=$((i + 2))
done
fail "no auth.denied log line for octo-outsider in Loki within 30s"
```
`scripts/verify.d/redaction.sh`

```bash
#!/usr/bin/env bash
# No secret may land in stored telemetry (T-01-46, Security Domain V7).
# Runs a real login + status flow, downloads the resulting Tempo traces and the recent Loki
# logs, and fails if any of them contains a session cookie value, a cookie/authorization
# header attribute, a bearer or access token, or the fake GitHub client secret.
set -euo pipefail
. "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/lib/verify-lib.sh"

OUT="$VERIFY_TMP/redaction"
mkdir -p "$OUT"

# 1. Sign in and read the session cookie value from the jar (Netscape format, value in field 7).
ensure_session
jar="$VERIFY_TMP/cookies"
session_value="$(awk -F'\t' '$6 == "ephematool_session" {print $7; exit}' "$jar")"
[ -n "$session_value" ] || fail "could not read the ephematool_session value from the cookie jar"

# 2. Authenticated requests and their trace ids.
: > "$OUT/trace-ids"
for p in /api/status /api/me; do
  api_get "$p" > /dev/null || fail "GET $p failed"
  tid="$(cat "$VERIFY_TMP/last-trace-id")"
  [ -n "$tid" ] || fail "GET $p returned no X-Trace-Id"
  echo "$tid" >> "$OUT/trace-ids"
done

# A fresh login, replayed step by step so the callback response headers are visible.
cj="$VERIFY_TMP/redaction-cookies"
: > "$cj"
curl -sS -o /dev/null -D "$OUT/login-start" -c "$cj" -b "$cj" "$API_URL/api/auth/github/login" \
  || fail "login start request failed"
loc="$(header_value "$OUT/login-start" location)"
[ -n "$loc" ] || fail "no Location from /api/auth/github/login (use bazel run //:up_fake)"
curl -sS -o /dev/null -D "$OUT/login-authorize" "${loc}&login=octo-admin" \
  || fail "fake GitHub authorize request failed"
cb="$(header_value "$OUT/login-authorize" location | sed -E 's#^https?://[^/]+##')"
[ -n "$cb" ] || fail "fake GitHub returned no callback Location"
curl -sS -o /dev/null -D "$OUT/login-callback" -c "$cj" -b "$cj" "$API_URL$cb" \
  || fail "callback request failed"
cbtid="$(header_value "$OUT/login-callback" x-trace-id)"
[ -n "$cbtid" ] || fail "login callback returned no X-Trace-Id"
echo "$cbtid" >> "$OUT/trace-ids"
# The callback's own session value must not appear either.
cb_session="$(awk -F'\t' '$6 == "ephematool_session" {print $7; exit}' "$cj")"

# 3. Download every trace from Tempo (poll: the trace is flushed a few seconds after the request).
n=0
while read -r tid; do
  n=$((n + 1))
  got=""
  i=0
  while [ "$i" -le 40 ]; do
    if curl -fsS --max-time 5 "$TEMPO_URL/api/traces/$tid" -o "$OUT/trace-$n.json" 2>/dev/null \
      && grep -q 'ephematool-api' "$OUT/trace-$n.json"; then
      got=1
      break
    fi
    sleep 2
    i=$((i + 2))
  done
  [ -n "$got" ] || fail "trace $tid was not found in Tempo within 40s"
done < "$OUT/trace-ids"
pass "downloaded $n traces from Tempo"

# 3b. Span attributes that carry command text (Postgres db.query.text holds parameterized SQL
# with placeholders only, so it is allowed; Redis commands carry the values) (the Redis db.statement leak: scs session
# token plus the serialized session). Searched across all traces that started after the api
# container was created, so volumes holding older dev traces cannot fail the check.
since="$(date -u -d "$(docker inspect -f '{{.Created}}' ephematool-api-1 2>/dev/null)" +%s 2>/dev/null || true)"
if [ -z "$since" ]; then
  created="$(docker inspect -f '{{.Created}}' ephematool-api-1 2>/dev/null | sed -E 's/\.[0-9]+Z$/Z/')"
  since="$(date -j -u -f '%Y-%m-%dT%H:%M:%SZ' "$created" +%s 2>/dev/null || true)"
fi
[ -n "$since" ] || since=$(( $(date +%s) - 900 ))
for attr in 'span.db.statement =~ ".+"' 'span.db.query.text =~ ".+" && span.db.system.name = "redis"'; do
  curl -fsS --max-time 10 --get "$TEMPO_URL/api/search" \
    --data-urlencode "q={ $attr }" \
    --data-urlencode "start=$since" --data-urlencode "end=$(date +%s)" \
    --data-urlencode "limit=20" -o "$OUT/search.json" || fail "Tempo search for $attr failed"
  hits="$(jq -r '[.traces[]?.traceID] | length' "$OUT/search.json")"
  [ "$hits" = "0" ] || fail "$hits trace(s) match { $attr } (command text is recorded in spans)"
done
pass "no span records db.statement or db.query.text since the api container started"

# 4. Loki: everything the platform logged in the last 15 minutes. Wait until the logs of this
# flow are there, so the check cannot pass on a log stream that predates the flow.
first_tid="$(sed -n 1p "$OUT/trace-ids")"
i=0
while [ "$i" -le 40 ]; do
  now="$(date +%s)"
  curl -fsS --max-time 10 --get "$LOKI_URL/loki/api/v1/query_range" \
    --data-urlencode 'query={service_namespace="ephematool"}' \
    --data-urlencode "start=$(( (now - 900) ))000000000" \
    --data-urlencode "end=${now}000000000" \
    --data-urlencode "limit=5000" -o "$OUT/loki.json" 2>/dev/null || true
  if [ -s "$OUT/loki.json" ] && grep -q -F "$first_tid" "$OUT/loki.json"; then
    break
  fi
  sleep 2
  i=$((i + 2))
done
[ -s "$OUT/loki.json" ] || fail "no Loki response"
grep -q -F "$first_tid" "$OUT/loki.json" || fail "Loki has no log line for the status trace $first_tid within 40s"
lines="$(jq -r '[.data.result[]?.values | length] | add // 0' "$OUT/loki.json")"
[ "$lines" -ge 1 ] 2>/dev/null || fail "Loki returned no log lines"
pass "downloaded $lines log lines from Loki"

# 5. Forbidden content. grep -F over saved files; a non-empty match file is a failure.
: > "$OUT/matches"
scan() { # label, needle
  [ -n "$2" ] || return 0
  if grep -F -i -l -e "$2" "$OUT"/trace-*.json "$OUT/loki.json" > "$OUT/hit" 2>/dev/null; then
    echo "[$1] found in: $(tr '\n' ' ' < "$OUT/hit")" >> "$OUT/matches"
  fi
}
scan "session cookie value" "$session_value"
scan "callback session cookie value" "$cb_session"
scan "session cookie pair" "ephematool_session="
scan "oauth cookie pair" "ephematool_oauth="
scan "bearer token" "Bearer "
# Token-shaped uses only: the OAuth token endpoint's URL path (.../oauth/access_token) is a
# harmless span attribute and is not a secret.
scan "access_token query/form value" "access_token="
scan "access_token JSON key" '"access_token"'
scan "client secret" "e2e-secret"
scan "cookie header attribute" "http.request.header.cookie"
scan "set-cookie header attribute" "http.response.header.set-cookie"
scan "authorization header attribute" "http.request.header.authorization"

if [ -s "$OUT/matches" ]; then
  cat "$OUT/matches" >&2
  fail "secret material found in telemetry (see above)"
fi

# The scan is only meaningful if the grep sees real content.
grep -q -F "ephematool-api" "$OUT/trace-1.json" || fail "scan sanity check failed: trace content missing"
pass "no cookie, token, authorization header or client secret in $n traces and $lines log lines"
```
`scripts/verify.d/fresh.sh`

```bash
#!/usr/bin/env bash
# SC3 / D-12: a fresh clone starts the whole stack with one command and all 10 services report
# healthy within 120s. Not part of `all`: it tears down the main stack and builds a second one.
#
#   bazel run //:verify_fresh
#
# Runs inside `bazel run //:verify_fresh`, so it must not call bazel against the main
# workspace (output-base lock). The clone has its own output base, so bazel inside it is fine.
set -euo pipefail

ROOT="${BUILD_WORKSPACE_DIRECTORY:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
cd "$ROOT"

fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "PASS: $*"; }

# The clone contains committed files only, so uncommitted work would not be tested.
if [ -n "$(git status --porcelain)" ]; then
  git status --short >&2
  fail "working tree is not clean: commit or remove these changes first (the clone only sees committed files)"
fi

tmp="$(mktemp -d "${TMPDIR:-/tmp}/ephematool-fresh.XXXXXX")"
case "$tmp" in
  */ephematool-fresh.*) ;;
  *) fail "unexpected temp dir: $tmp" ;;
esac

cleanup() {
  status=$?
  (cd "$tmp/repo" && bazel run //:down && bazel shutdown) >/dev/null 2>&1 || true
  rm -rf "$tmp"
  exit "$status"
}
trap cleanup EXIT

git clone --quiet --depth 1 "file://$ROOT" "$tmp/repo"
[ ! -e "$tmp/repo/.env" ] || fail "clone unexpectedly contains .env"

# Free the host ports: stop the main stack. Called directly (not via bazel run //:down).
"$ROOT/tools/stack.sh" down

started=$SECONDS
echo "==> bazel run //:up_fake (fresh clone at $tmp/repo)"
(cd "$tmp/repo" && bazel run //:up_fake)
echo "up_fake returned after $((SECONDS - started))s"

# All 10 services healthy within 120s of the stack coming up.
BUILD_WORKSPACE_DIRECTORY="$tmp/repo" "$tmp/repo/scripts/verify-telemetry.sh" up
pass "fresh clone: one command started the stack and all 10 services are healthy (total $((SECONDS - started))s)"
```
And the ordered local gate:

`tools/verify.sh`

```bash
#!/usr/bin/env bash
# Full local verification gate (replaces `make verify`). Run it directly from a shell:
#
#   ./tools/verify.sh
#
# Steps, in order (stops at the first failure):
#   1. Docker-backed Go integration tests   bazel test --config=integration //:test_integration
#   2. Unit tests, typecheck, codegen drift bazel test //...
#   3. Bring up the stack with fake GitHub  bazel run //:up_fake
#   4. Playwright e2e against the stack     bazel run //:e2e
#   5. Telemetry checks against the stack   bazel run //:verify_telemetry
#
# This is a plain script on purpose: it calls bazel sequentially. It must not be wrapped
# in a `bazel run` target, because a bazel invocation nested inside `bazel run` blocks on
# the output base lock.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

BAZEL="${BAZEL:-bazel}"

step() {
  echo
  echo "==> $*"
  "$@"
}

step "$BAZEL" test --config=integration //:test_integration
step "$BAZEL" test //...
step "$BAZEL" run //:up_fake
step "$BAZEL" run //:e2e
step "$BAZEL" run //:verify_telemetry
echo
echo "verify: all steps passed"
```
---

## 9. Bring it up (without the web image)

The `web` service needs Phase 1c. To bring up and check everything else now:

```bash
docker compose -f deploy/compose.yaml -f deploy/compose.e2e.yaml up -d --build \
  otel-collector tempo loki prometheus grafana postgres redis api worker fakegithub
```

Then:

```bash
curl -s http://127.0.0.1:8081/api/auth/config        # public config, direct to the API port
bash scripts/verify.d/backends.sh                      # Tempo, Loki, Prometheus, Grafana, Collector are ready
```

Open Grafana at http://localhost:3000 (anonymous admin) and check **Connections → Data sources**: three datasources, each "Test" green.

After Phase 1c, the one-command form works:

```bash
bazel run //:up_fake
bazel run //:verify_telemetry
```

## 10. Phase 1b acceptance checklist

- [ ] `docker compose ... up -d` (section 9) leaves every container running or healthy.
- [ ] `scripts/verify.d/backends.sh` passes.
- [ ] A request to `/api/auth/config` shows up as a trace in Grafana Explore → Tempo (service `ephematool-api`), with matching log lines in Loki (`{service_name="ephematool-api"}`), and a request-rate point on the Ephematool dashboard.
- [ ] The worker heartbeat appears in Loki logs every ~15 s (`worker heartbeat`).
- [ ] Nothing is reachable from another machine: `ss -ltn | grep -E ':(3000|3100|3200|9090|8081) '` shows only `127.0.0.1`.
- [ ] After Phase 1c: `bazel run //:verify_telemetry` prints PASS for every check.

## 11. Common failures

| Symptom | Cause | Fix |
|---|---|---|
| Collector exits: `unknown type: "otlp/platform"` | wrong Collector image (core instead of contrib) | use `otel/opentelemetry-collector-contrib`. |
| Tempo shows traces but Loki has no logs | `allow_structured_metadata` missing | restore it in `loki.yaml`, recreate the loki container. |
| Prometheus has no `http_server_request_duration_seconds_count` | OTLP receiver flag or translation strategy missing | check `--web.enable-otlp-receiver` and `translation_strategy`. |
| Metric names differ from the dashboard queries | Prometheus version changed the escaping | confirm names with `curl :9090/api/v1/label/__name__/values` and update `promql.go` and the dashboard together. |
| API restarts with `connection refused` to Postgres | started before the DB was healthy | the compose `depends_on: service_healthy` entries are missing. |
| Worker never writes a heartbeat | `HEARTBEAT_INTERVAL` ≥ 22.5 s | config validation rejects it; use 15 s. |
