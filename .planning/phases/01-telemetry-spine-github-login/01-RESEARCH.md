# Phase 1: Telemetry Spine & GitHub Login - Research

**Researched:** 2026-10-11
**Domain:** Go API + worker with OpenTelemetry (traces/metrics/logs), GitHub OAuth login with org allowlist, React/Vite SPA with browser OTel, Docker Compose + Collector + Grafana LGTM
**Confidence:** MEDIUM-HIGH (versions verified against registries today; config snippets for Tempo/Loki/Prometheus/Grafana are from documentation plus training knowledge and must be proven by a Wave 0 smoke spike)

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions
- **D-01:** Sign-in is restricted to members of configured GitHub org(s)/team(s). The allowlist is plain config.
- **D-02:** Admins are listed by GitHub username in config. Every other allowed org member is a viewer by default. Full viewer/creator/admin role management arrives in Phase 2 (AUTH-02).
- **D-03:** Sessions are sliding 7 days: the user stays signed in across browser sessions while active and expires after 7 days idle. Sign-out is available from the dashboard.
- **D-04:** A GitHub user who authenticates but is not in an allowed org is signed out and shown a clear denied page naming the required org membership. The denial is recorded as a log event.
- **D-05:** Backend language is **Go**, not TypeScript. This supersedes the TypeScript-specific parts of `.planning/research/STACK.md` (Fastify, Drizzle, pg-boss, better-auth, shared Zod schemas, `@kubernetes/client-node`, OTel JS SDK).
- **D-06:** System of record is **Postgres**. The job queue and scheduling use **Redis** (not pg-boss). Redis is a second stateful service the platform must run and observe.
- **D-07:** The dashboard is a **separate React SPA** (Vite). The Go API publishes an OpenAPI spec and the SPA uses a generated typed client. The shared-Zod-schema idea from research no longer applies.
- **D-08:** Because STACK.md is now stale for backend choices, the phase research pass must re-derive the Go and Redis specifics (HTTP router, OTel Go SDK and instrumentation, GitHub OAuth and org-membership check library, session handling, Redis queue library, Postgres driver and migration tool, OpenAPI tooling). Treat STACK.md as superseded where it conflicts with D-05 to D-07.
- **D-09:** Attribute and label prefix is **`ephematool.io/*`** for Kubernetes labels (for example `ephematool.io/env-id`) and **`ephematool.env.*`** for OpenTelemetry attributes. Encode it once in a single shared module or constants file and reference it everywhere.
- **D-10:** The React dashboard emits its own browser traces with context propagation, so a page load appears as one trace linked to the API trace. This sets up the click-to-Actions-run trace required in Phase 2 (OBS-02).
- **D-11:** Local backends are **Grafana LGTM**: Tempo (traces), Loki (logs), Prometheus (metrics), and Grafana, fed by one OTel Collector gateway.
- **D-12:** One command runs everything with **Docker Compose**: API, worker, SPA, Postgres, Redis, OTel Collector, and the LGTM backends. A kind cluster is not part of Phase 1.
- **D-13:** After login the dashboard shows a **telemetry status page**: the signed-in user, build info, per-service telemetry health, and a Grafana deep link.

### Claude's Discretion
- Go HTTP framework, project layout, Postgres migration tool, OpenAPI generation approach, and Redis queue library, to be chosen during research and planning (see D-08).
- Exact Collector pipeline layout, subject to a separate pipeline for the tool's own telemetry per the research pitfalls.
- Visual design of the status page and login/denied pages (already fixed by the approved 01-UI-SPEC.md).

### Deferred Ideas (OUT OF SCOPE)
None. Discussion stayed within phase scope. (Kind cluster, environment creation, roles beyond admin/viewer, per-environment telemetry are Phase 2+.)
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| OBS-01 | The platform emits OpenTelemetry traces, metrics, and logs for its own API, worker, and dashboard from first run | Go OTel SDK bootstrap package (`internal/telemetry`), otelhttp/otelpgx/redisotel, otelslog bridge, asynq trace propagation via task Headers, browser OTel (traces + logs + metrics) through nginx `/otlp` proxy, Collector `platform` pipelines, Tempo/Loki/Prometheus/Grafana provisioning, verification script that finds trace + metric + correlated logs |
| AUTH-01 | An SRE can log in with GitHub | OAuth App + `golang.org/x/oauth2` (PKCE, state), org/team membership via `GET /user/memberships/orgs/{org}`, scs sessions in Redis with sliding idle timeout and absolute lifetime, denied page flow, sign-out, `/api/me` |
</phase_requirements>

## Project Constraints (from CLAUDE.md)

- Work must flow through GSD commands (`/gsd-execute-phase`, `/gsd-quick`, `/gsd-debug`); no ad-hoc repo edits outside a GSD workflow.
- Constraints: Kubernetes is the future runtime, GitHub Actions the provisioning mechanism, OpenTelemetry from day one, web dashboard as the primary surface.
- The "Technology Stack" block in CLAUDE.md (Fastify, Drizzle, pg-boss, better-auth, Zod, Node backend) is **superseded** by D-05..D-07 for backend choices. The frontend rows (React, Vite, Tailwind 4, shadcn, TanStack Query) still apply. The planner should not re-introduce the TypeScript backend stack.
- Conventions and Architecture sections in CLAUDE.md are empty: this phase establishes them (monorepo layout, constants module, telemetry package).

## Summary

Phase 1 is a greenfield spine: a Go modular monolith (two binaries, `api` and `worker`) plus a Vite React SPA, all emitting OTLP to one Collector that fans out to Tempo, Loki, and Prometheus, with Grafana provisioned for trace-to-log-to-metric navigation. GitHub OAuth login is gated by an org/team allowlist, and sessions live in Redis with sliding 7-day expiry. Everything starts with one `docker compose up --build --wait`.

The recommended stack is verified current on 2026-10-11: Go 1.26 (local toolchain is 1.26.2), **Huma v2.39.1 on the stdlib `net/http` mux** (code-first, emits OpenAPI 3.1; no chi needed), OTel Go 1.47.0 / contrib 0.72.0, pgx 5.11.0 + goose 3.29.0 + sqlc, go-redis 9.23.0, **asynq 0.26.0** (Redis queue; tasks now carry `Headers`, which makes W3C context propagation trivial), `golang.org/x/oauth2` with PKCE, `scs/v2` sessions on `goredisstore`. The SPA uses React 19.3, Vite 8.3, openapi-typescript 7.13 + openapi-fetch 0.17, TanStack Query 5, and OTel JS web SDK 2.12 / instrumentation 0.223. Backend images: Collector contrib 0.162.0, Tempo 2.10.8 (3.1.0 exists; see Open Questions), Loki 3.7.8, Prometheus v3.15.0, Grafana 13.2.3, Postgres 18, Redis 8.

The things most likely to hurt are non-obvious integration traps, all captured below: (1) scs only extends a session when it is modified, so "sliding 7 days" needs an explicit touch, and scs defaults to a 24h absolute lifetime that must be overridden; (2) otelhttp loses `http.route` when a middleware that copies the request sits between it and the mux; (3) a browser page load only becomes one trace with the API trace if the initial fetches run inside an explicit parent span context; (4) asynq's own Redis polling would flood Tempo if instrumented; (5) a fresh checkout has no GitHub OAuth App credentials, so automated login verification needs a fake GitHub; (6) Prometheus OTLP metric names are suffix-translated (`_total`, `_seconds`) so dashboards and status queries must use translated names.

**Primary recommendation:** Build Wave 0 as a thin vertical slice (Compose + Collector + three backends + API emitting one trace/metric/log + a verification script that finds all three in Tempo/Prometheus/Loki), then layer auth, worker, SPA, and status page on top, with the verification script growing alongside.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| GitHub OAuth redirect, code exchange, org/team check | API / Backend | — | Secrets and tokens must never reach the browser; token used once then discarded |
| Session storage and sliding expiry | API / Backend (Redis) | — | HttpOnly cookie holds opaque token only; state in Redis |
| Route guarding (redirect to /login, /denied) | Browser / Client (SPA router) | API (401 is the authority) | UX redirect in SPA; enforcement is API-side 401 |
| OpenAPI spec | API / Backend | CI drift check | Code-first from Huma; SPA types generated from the committed spec |
| Browser telemetry (page load, fetch spans, UI logs/metrics) | Browser / Client | Static server (nginx proxy to Collector) | SDK runs in browser; nginx same-origin proxy avoids CORS and keeps Collector off the public surface |
| Trace propagation SPA -> API -> worker | Browser, API, Worker | Redis (task headers) | traceparent header; asynq task Headers carry context to the worker |
| Telemetry gateway (receive, protect, route) | OTel Collector | — | One OTLP ingress; `memory_limiter` first, `batch` last |
| Telemetry storage and query | Tempo / Loki / Prometheus | Grafana (UI) | Backends are swappable behind OTLP; no custom store |
| Service health aggregation (10 services) | API / Backend (background prober, cached) | Prometheus (last-signal query) | API already trusted by the SPA; one cached `/api/status` response; no fan-out per request |
| Worker liveness | Worker (heartbeat key in Redis) | API reads | Worker has no HTTP surface needed |
| Static SPA hosting + reverse proxy | CDN / Static (nginx container) | — | Same origin as `/api` and `/otlp` so cookies and traceparent need no CORS |
| System of record | Database (Postgres) | — | Phase 1 stores users; schema/migrations set the pattern for Phase 2 |
| Queue / scheduling | Redis (asynq) | Worker | D-06 |

## Standard Stack

### Core (Go)

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Go | 1.26 (local 1.26.2, `golang:1.26-alpine` image exists) | Language | `slog.NewMultiHandler` (verified via `go doc`), `http.CrossOriginProtection`, `go.mod` `tool` directives [VERIFIED: local toolchain] |
| `github.com/danielgtaylor/huma/v2` | v2.39.1 (2026-07-29) | HTTP API framework, typed handlers, OpenAPI 3.1 generation | Code-first spec, `humago` adapter over stdlib mux, middleware gets `ctx.Operation().Path` (route template) [VERIFIED: go proxy + module source inspected] |
| `net/http` ServeMux (Go 1.22+ patterns) | stdlib | Router | Avoids chi dependency; `humago.New(mux, cfg)` [VERIFIED: module source] |
| `go.opentelemetry.io/otel` + `sdk` + `sdk/metric` + `sdk/log` | v1.47.0 (log SDK v0.23.0) | OTel API/SDK | Traces and metrics stable; Logs signal is **beta** (breaking changes possible between minors) so pin exactly [VERIFIED: go proxy; beta status CITED: uptrace/dash0 guides] |
| `.../otel/exporters/otlp/otlptrace/otlptracehttp` | v1.47.0 | Trace export to Collector | OTLP/HTTP; one port, easy to proxy [VERIFIED: go proxy] |
| `.../otlpmetric/otlpmetrichttp` | v1.47.0 | Metric export | [VERIFIED: go proxy] |
| `.../otlplog/otlploghttp` | v0.23.0 | Log export | [VERIFIED: go proxy] |
| `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` | v0.72.0 | Server and client HTTP spans/metrics | Standard. Uses `Request.Pattern` for route (with a known caveat, Pitfall 2) [VERIFIED: go proxy; CITED: opentelemetry-go-contrib#9859] |
| `go.opentelemetry.io/contrib/bridges/otelslog` | v0.21.0 | slog -> OTel logs with automatic trace_id/span_id | Official bridge; requires `*Context` log calls [VERIFIED: go proxy] |
| `go.opentelemetry.io/contrib/instrumentation/runtime` | v0.72.0 | Go runtime metrics | Cheap process health metrics [VERIFIED: go proxy] |
| `github.com/jackc/pgx/v5` | v5.11.0 | Postgres driver + pool | Standard for Go [VERIFIED: go proxy] |
| `github.com/exaring/otelpgx` | v0.13.0 | pgx tracing (+ pool metrics) | Tracer on `pgxpool.Config.ConnConfig.Tracer` [VERIFIED: go proxy; usage ASSUMED from training] |
| `github.com/pressly/goose/v3` | v3.29.0 | SQL migrations (embedded FS) | Simple, supports `embed`, runs from API `migrate` subcommand [VERIFIED: go proxy] |
| `github.com/sqlc-dev/sqlc` | v1.31.1 | Typed query codegen (pinned via `go.mod` `tool`) | Keeps SQL as SQL; no ORM [VERIFIED: go proxy] |
| `github.com/redis/go-redis/v9` | v9.23.0 | Redis client (sessions, status keys, app use) | [VERIFIED: go proxy] |
| `github.com/redis/go-redis/extra/redisotel/v9` | v9.23.0 | Redis tracing + metrics | `redisotel.InstrumentTracing(rdb)` on the **app** client only (Pitfall 4) [VERIFIED: go proxy] |
| `github.com/hibiken/asynq` | v0.26.0 (2026-02-03) | Redis job queue + scheduler | Retries, scheduler, unique tasks; tasks have `Headers` (verified in source) for context propagation. No official OTel contrib: write a ~40-line middleware [VERIFIED: module source] |
| `golang.org/x/oauth2` (+ `/github`) | v0.37.0 | OAuth2 authorization code + PKCE | `oauth2.GenerateVerifier`, `S256ChallengeOption` [VERIFIED: go proxy; PKCE API ASSUMED from training] |
| `github.com/google/go-github/v92` | v92.0.0 | GitHub REST (user, org membership, team membership) | Needed again in Phase 2 (Actions dispatch). Wrap its `http.Client` with `otelhttp.NewTransport` [VERIFIED: go proxy] |
| `github.com/alexedwards/scs/v2` + `scs/goredisstore` | v2.9.0 / pseudo-version 20251002 | Server-side sessions with idle timeout + lifetime | Do not hand-roll session handling; Redis TTL gives native expiry. goredisstore pins go-redis v9.0.2 minimum (MVS will lift to 9.23.0) [VERIFIED: module source] |
| `github.com/caarlos0/env/v11` | v11.4.1 | Env-var config struct | Small, no config-file complexity (12-factor via Compose) [VERIFIED: go proxy] |

### Core (Frontend)

| Library | Version | Purpose | Notes |
|---------|---------|---------|-------|
| react / react-dom | 19.3.0 | UI | [VERIFIED: npm registry] |
| vite | 8.3.4 | Build/dev | [VERIFIED: npm registry] |
| @tanstack/react-router | 1.170.42 | Routing (matches CLAUDE.md/UI-SPEC "route change moves focus to Heading") | react-router 8.4.0 is the alternative; pick TanStack Router per STACK.md frontend rows [VERIFIED: npm registry] |
| @tanstack/react-query | 5.104.2 | Server state, 15s polling | UI-SPEC locks polling and "no silent retries" (`retry: false` for status) [VERIFIED: npm registry] |
| openapi-typescript | 7.13.0 | Generate TS types from `api/openapi.json` | Supports OpenAPI 3.1 [VERIFIED: npm registry] |
| openapi-fetch | 0.17.0 | Typed client, accepts custom `fetch` (used for trace-context wrapper) | [VERIFIED: npm registry] |
| tailwindcss | 4.3.3 | Styling (UI-SPEC) | [VERIFIED: npm registry] |
| shadcn (CLI) | 4.21.4 | Component scaffolding (button, card, badge, avatar, dropdown-menu, skeleton, table, tooltip) | [VERIFIED: npm registry] |
| lucide-react | 1.55.0 | Icons (UI-SPEC) | [VERIFIED: npm registry] |
| @opentelemetry/api | 1.9.1 | Browser OTel API | [VERIFIED: npm registry] |
| @opentelemetry/sdk-trace-web | 2.12.0 | WebTracerProvider | SDK 2.x: pass `spanProcessors` in the constructor; `addSpanProcessor` is gone [CITED: opentelemetry.io browser getting-started] |
| @opentelemetry/sdk-trace-base | 2.12.0 | BatchSpanProcessor | Peer of sdk-trace-web [ASSUMED version, same release train] |
| @opentelemetry/exporter-trace-otlp-http | 0.223.0 | Trace export | [VERIFIED: npm registry] |
| @opentelemetry/sdk-logs + exporter-logs-otlp-http | 0.223.0 | Browser logs (beta) | Needed so "dashboard emits traces, metrics, **and logs**" holds [VERIFIED: npm registry] |
| @opentelemetry/sdk-metrics + exporter-metrics-otlp-http | 2.12.0 / 0.223.0 | Browser metrics | One counter + one histogram is enough [VERIFIED: npm registry] |
| @opentelemetry/instrumentation-fetch | 0.223.0 | Fetch spans + `traceparent` injection | Same-origin requests are propagated by default [VERIFIED: npm registry] |
| @opentelemetry/instrumentation-document-load | 0.68.0 | Page-load spans | [VERIFIED: npm registry] |
| @opentelemetry/instrumentation, resources | 0.223.0 / 2.12.0 | `registerInstrumentations`, `resourceFromAttributes` | [VERIFIED: npm registry] |
| web-vitals | 6.2.3 | Optional: LCP/INP as OTel histograms | Optional [VERIFIED: npm registry] |

### Backend images (Compose)

| Image | Pin | Notes |
|-------|-----|-------|
| `otel/opentelemetry-collector-contrib` | `0.162.0` | Latest release tag 2026-10 [VERIFIED: GitHub releases API + Docker Hub] |
| `grafana/tempo` | `2.10.8` | Stable monolithic config is well known; 3.1.0 (2026-09-29) also exists, see Open Question 1 [VERIFIED: Docker Hub + GitHub releases] |
| `grafana/loki` | `3.7.8` | [VERIFIED: Docker Hub + GitHub releases] |
| `prom/prometheus` | `v3.15.0` | Docker Hub shows v3.15.0 as highest; v3.13.4 also current patch of an older minor. Re-check at implementation [VERIFIED: Docker Hub tags] |
| `grafana/grafana` | `13.2.3` | [VERIFIED: Docker Hub] |
| `postgres` | `18.6-alpine` | [VERIFIED: Docker Hub] |
| `redis` | `8.8-alpine` (confirm alpine tag exists) | `8.8.3` verified; alpine variant tag ASSUMED [VERIFIED: Docker Hub for 8.8.3] |
| `golang` (build) | `1.26-alpine` | [VERIFIED: Docker Hub] |
| `node` (build) | `24-alpine` | [VERIFIED: Docker Hub] |
| `nginx` (serve) | `1.31-alpine` | 1.31.2 verified; alpine tag ASSUMED [VERIFIED: Docker Hub for 1.31.2] |

`grafana/otel-lgtm` 0.36.0 exists as an all-in-one image but is **rejected**: D-11 requires a real Collector gateway and observing Tempo/Loki/Prometheus/Grafana as separate services in the status page.

### Supporting

| Library | Version | Use |
|---------|---------|-----|
| `github.com/testcontainers/testcontainers-go` | v0.44.0 | Go integration tests against real Postgres/Redis (Docker is available locally) |
| `github.com/alicebob/miniredis/v2` | v2.40.0 | Fast unit tests for session/status keys (not for asynq Lua) |
| `github.com/stretchr/testify` | v1.12.1 | Assertions |
| vitest | 5.0.3 | SPA unit tests |
| @playwright/test | 1.64.0 | SPA e2e + browser trace assertion |

### Alternatives Considered

| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Huma code-first | oapi-codegen v2.8.0 spec-first | Spec-first gives a hand-authored contract but Huma's typed handlers + generated 3.1 spec removes drift by construction. oapi-codegen has weaker 3.1 support (Huma docs downgrade to 3.0 for it). Not needed because the SPA generator (openapi-typescript) handles 3.1 |
| stdlib mux | chi v5.3.2 | chi adds a dependency without helping otelhttp route naming; stdlib is enough |
| asynq (Redis) | River v0.49.0 (Postgres) | River has official otelriver but is Postgres-backed; D-06 locks Redis |
| scs sessions | Postgres sessions / hand-rolled | Redis TTL is native sliding storage; scs handles cookie, renewal, fixation |
| OAuth App | GitHub App user-to-server token | GitHub App needs installation on the org plus "Members: read" org permission and its tokens expire. Phase 2 needs a GitHub App anyway, so consolidating is a Phase 2 decision |
| go-github | hand-rolled 3 GETs | Phase 2 needs go-github for Actions; use it now |
| Tempo 2.10.8 | Tempo 3.1.0 monolith | See Open Question 1 |

**Installation:**
```bash
# Go (module root); tools pinned via go.mod tool directives
go mod init github.com/<owner>/ephematool
go get github.com/danielgtaylor/huma/v2@v2.39.1 \
  go.opentelemetry.io/otel@v1.47.0 go.opentelemetry.io/otel/sdk@v1.47.0 \
  go.opentelemetry.io/otel/sdk/metric@v1.47.0 go.opentelemetry.io/otel/sdk/log@v0.23.0 \
  go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp@v1.47.0 \
  go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp@v1.47.0 \
  go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp@v0.23.0 \
  go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp@v0.72.0 \
  go.opentelemetry.io/contrib/bridges/otelslog@v0.21.0 \
  go.opentelemetry.io/contrib/instrumentation/runtime@v0.72.0 \
  github.com/jackc/pgx/v5@v5.11.0 github.com/exaring/otelpgx@v0.13.0 \
  github.com/pressly/goose/v3@v3.29.0 github.com/redis/go-redis/v9@v9.23.0 \
  github.com/redis/go-redis/extra/redisotel/v9@v9.23.0 github.com/hibiken/asynq@v0.26.0 \
  golang.org/x/oauth2@v0.37.0 github.com/google/go-github/v92@v92.0.0 \
  github.com/alexedwards/scs/v2@v2.9.0 github.com/alexedwards/scs/goredisstore@latest \
  github.com/caarlos0/env/v11@v11.4.1
go get -tool github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1

# Frontend (web/)
npm create vite@latest web -- --template react-ts
npm i react@19.3.0 react-dom@19.3.0 @tanstack/react-query@5.104.2 @tanstack/react-router@1.170.42 \
  openapi-fetch@0.17.0 lucide-react tailwindcss@4.3.3 \
  @opentelemetry/api@1.9.1 @opentelemetry/sdk-trace-web@2.12.0 @opentelemetry/sdk-trace-base@2.12.0 \
  @opentelemetry/sdk-logs@0.223.0 @opentelemetry/sdk-metrics@2.12.0 @opentelemetry/resources@2.12.0 \
  @opentelemetry/exporter-trace-otlp-http@0.223.0 @opentelemetry/exporter-logs-otlp-http@0.223.0 \
  @opentelemetry/exporter-metrics-otlp-http@0.223.0 @opentelemetry/instrumentation@0.223.0 \
  @opentelemetry/instrumentation-fetch@0.223.0 @opentelemetry/instrumentation-document-load@0.68.0
npm i -D openapi-typescript@7.13.0 vitest@5.0.3 @playwright/test@1.64.0
npx shadcn@4.21.4 init   # Wave 0 per UI-SPEC
```

**Version verification:** Go modules checked with `go list -m -json <mod>@latest` and npm with `npm view <pkg> version` on 2026-10-11; images checked on Docker Hub tags API. Note `@opentelemetry/*` JS packages have two version trains (2.12.0 stable, 0.223.0 experimental): upgrade as a set. Go `otel` is 1.47.0 but `otel/log` SDK and `otlploghttp` are 0.x.

## Package Legitimacy Audit

slopcheck was installed locally and run (`slopcheck scan --pkg go|npm <name>`).

| Package | Registry | Age | Source Repo | slopcheck | Disposition |
|---------|----------|-----|-------------|-----------|-------------|
| huma/v2, oauth2, pgx/v5, asynq, scs/v2, scs/goredisstore, env/v11, testcontainers-go, sqlc | Go proxy | years | github.com | [OK] | Approved |
| go-github/v92 | Go proxy | years | github.com/google/go-github | [SUS] | Approved: flagged only because slopcheck found no linked source repo for the module path. Repo is the canonical Google project. Planner: add a one-line human-verify note on the first `go get` |
| otelhttp, otelslog | Go proxy | years | github.com/open-telemetry | [SUS] | Approved on same basis (official OpenTelemetry contrib repo) |
| goose/v3, go-redis/v9, redisotel/v9, otelpgx, miniredis/v2 | Go proxy | years | github.com/pressly/goose, redis/go-redis, exaring/otelpgx, alicebob/miniredis | [SLOP] (false positive) | **Not removed.** slopcheck derives "age" from the latest module version timestamp (e.g. goose v3.29.0 was published today, 2026-10-11; go-redis v9.23.0 on 2026-10-05), reporting "created 0-6 days ago". The unversioned module paths (`github.com/pressly/goose`, `github.com/redis/go-redis`, `github.com/alicebob/miniredis`) are [OK], and all are the canonical, long-lived projects. Planner: gate first install of these five behind a `checkpoint:human-verify` that confirms the import path against the project README |
| @opentelemetry/* (web, exporters, api), openapi-typescript, openapi-fetch, @tanstack/*, vite, shadcn, lucide-react, @playwright/test, web-vitals | npm | years | github.com | [OK] | Approved; `npm view` shows no `postinstall` on the OTel and openapi packages checked |
| vitest | npm | years | github.com/vitest-dev/vitest | [SUS] | Approved: flag is recency of latest release (5.0.3). Add human-verify note |

**Packages removed due to [SLOP]:** none (five [SLOP] results are documented false positives, see above).
**Packages flagged [SUS]:** go-github/v92, otelhttp, otelslog, vitest (planner inserts a lightweight verify note, not a blocker).

## Architecture Patterns

### System Architecture Diagram

```
 Browser (SPA, OTel web SDK)
   |  GET /, /assets/*            |  /api/* (cookie + traceparent)   |  POST /otlp/v1/{traces,logs,metrics}
   v                              v                                  v
 +--------------------- nginx "web" (same origin :8080) ---------------------+
 |  static SPA        reverse proxy /api -> api:8081      /otlp -> collector:4318 (rate-limited, path-allowlisted)
 +---------------------------------------------------------------------------+
        |                          |                                  |
        |                          v                                  |
        |            +-------- api (Go) ---------+                    |
        |            | otelhttp -> session -> huma|                    |
        |            |  /api/auth/github/login ---+--> github.com OAuth (browser redirect)
        |            |  /api/auth/github/callback-+--> api.github.com (user, org/team membership; token discarded)
        |            |  /api/me /api/status /api/auth/config /api/auth/logout
        |            +--+------+------+-----------+
        |               |      |      |  enqueue "telemetry:ping" (traceparent in task Headers)
        |            Postgres  Redis--+---------------------------+
        |           (users)  (sessions,       asynq queue         |
        |                    heartbeat keys)                      v
        |                                           +---- worker (Go) ----+
        |                                           | asynq server+scheduler|
        |                                           | heartbeat -> Redis key|
        |                                           +----------+-----------+
        |                                                      |
 api, worker, browser --OTLP/HTTP--> +------------- OTel Collector (gateway) -------------+
                                     | receivers: otlp/platform (4317/4318)               |
                                     | processors: memory_limiter -> resource -> attributes(redact) -> batch
                                     | traces/platform  -> Tempo   (otlp grpc 4317)       |
                                     | logs/platform    -> Loki    (otlphttp /otlp)       |
                                     | metrics/platform -> Prometheus (otlphttp /api/v1/otlp) |
                                     | extensions: health_check :13133 ; self metrics :8888 |
                                     +-----------------------------------------------------+
                                        |            |               |
                                      Tempo         Loki        Prometheus (scrapes collector:8888, tempo, loki, grafana)
                                        \            |              /
                                         +---- Grafana (provisioned datasources + correlations) ----+

 api prober goroutine (every 10s, cached): Postgres ping, Redis PING, worker heartbeat key, web /healthz,
   collector :13133, tempo :3200/ready, loki :3100/ready, prometheus :9090/-/ready, grafana :3000/api/health
   + Prometheus instant query for "last signal" of api/worker/dashboard  ==> GET /api/status (10 rows)
```

### Recommended Project Structure

```
ephematool/
├── go.mod / go.sum                  # one module, two binaries
├── cmd/
│   ├── api/main.go                  # serve | migrate | openapi subcommands
│   ├── worker/main.go
│   └── fakegithub/main.go           # test-only fake GitHub OAuth+REST (e2e)
├── internal/
│   ├── config/                      # env-driven config struct
│   ├── conventions/                 # GENERATED constants (D-09), do not edit
│   ├── telemetry/                   # Setup(ctx, serviceName) -> shutdown; slog handler; metrics helpers
│   ├── httpapi/                     # huma registration, middleware (route tagging, trace-id header)
│   ├── auth/                        # oauth flow, org check, session manager, role resolution
│   ├── status/                      # prober, Prometheus last-signal client, cache
│   ├── queue/                       # asynq client/server wiring, trace propagation middleware, task types
│   ├── store/                       # pgx pool, goose migrations (embed), sqlc generated queries
│   └── buildinfo/                   # version/commit/time via -ldflags
├── conventions/telemetry.json       # single source of truth for D-09 prefixes/keys
├── tools/genconventions/main.go     # emits internal/conventions + web/src/conventions.gen.ts
├── api/openapi.json                 # committed, generated by `api openapi`
├── migrations/                      # goose SQL files
├── web/                             # Vite React SPA (src/telemetry/, src/api/, src/routes/)
├── deploy/
│   ├── compose.yaml  compose.e2e.yaml
│   ├── otel/collector.yaml
│   ├── tempo/tempo.yaml  loki/loki.yaml  prometheus/prometheus.yml
│   ├── grafana/provisioning/{datasources,dashboards}/ + dashboards/*.json
│   └── nginx/default.conf
├── scripts/verify-telemetry.sh      # the Nyquist e2e check
├── Dockerfile                       # multi-target: api, worker (Go), web (node build -> nginx)
├── Makefile                         # up, down, gen, test, verify
└── .env.example
```

### Pattern 1: Single telemetry bootstrap, shared by api and worker
**What:** `internal/telemetry.Setup(ctx, Config{ServiceName, Version})` builds Resource (`service.name`, `service.namespace=ephematool`, `service.version`, `deployment.environment.name`), registers tracer/meter/logger providers, W3C TraceContext + Baggage propagator, runtime metrics, and returns a shutdown func. The default `slog` logger becomes `slog.NewMultiHandler(stdoutJSON, otelslog.NewHandler(name))` so `docker logs` works **and** logs reach Loki. All log calls must be `slog.InfoContext(ctx, ...)`; add a `sloglint`-style lint rule or a code-review checklist item.
**Example:**
```go
// Source: otelslog / OTel Go getting started (pattern); structure ASSUMED from training, verify in Wave 0
res, _ := resource.Merge(resource.Default(), resource.NewSchemaless(
    semconv.ServiceName(cfg.ServiceName), semconv.ServiceNamespace("ephematool"),
    semconv.ServiceVersion(cfg.Version), semconv.DeploymentEnvironmentName(cfg.Env)))
te, _ := otlptracehttp.New(ctx)           // endpoint from OTEL_EXPORTER_OTLP_ENDPOINT=http://otel-collector:4318
tp := sdktrace.NewTracerProvider(sdktrace.WithResource(res), sdktrace.WithBatcher(te))
lp := sdklog.NewLoggerProvider(sdklog.WithResource(res), sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp)))
otel.SetTracerProvider(tp)
otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
global.SetLoggerProvider(lp)
slog.SetDefault(slog.New(slog.NewMultiHandler(
    slog.NewJSONHandler(os.Stdout, nil), otelslog.NewHandler(cfg.ServiceName, otelslog.WithLoggerProvider(lp)))))
```
Configure the exporter through the standard `OTEL_*` env vars in Compose (`OTEL_EXPORTER_OTLP_ENDPOINT`, `OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf`, `OTEL_SERVICE_NAME`) so Phase 2 containers and K8s reuse the same convention.

### Pattern 2: Route-accurate spans and metrics regardless of middleware order
**What:** Stack is `otelhttp.NewHandler(sessionMW(mux))`. Session middleware (scs) does `r.WithContext`, which hides `Request.Pattern` from otelhttp (issue #9859). Fix: a Huma middleware (and a 4-line wrapper for the non-Huma auth routes) that reads `ctx.Operation().Path`, calls `trace.SpanFromContext(ctx).SetName(method + " " + path)`, sets `semconv.HTTPRoute(path)`, and adds the same attribute through `otelhttp.GetLabeler(ctx).Add(...)` so server metrics carry a bounded `http.route`. Also write `X-Trace-Id` (and let `traceparent` response header through) from the same middleware, as UI-SPEC requires the trace ID on error states. Never use raw URL paths as span names or metric labels.

### Pattern 3: Trace context through asynq
**What:** asynq 0.26 tasks have `Headers map[string]string`. Producer: `otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(h))` then `asynq.NewTaskWithHeaders(type, payload, h)`. Consumer middleware: `Extract` from `t.Headers()`, start a `consumer` span (child of producer span, which yields one trace API -> worker), record `ephematool.queue.task.duration` histogram and `...processed` counter with `task.type` and `outcome` labels only. Heartbeats created by the scheduler start their own root trace.
**Worker responsibilities in Phase 1 (decided):**
1. `telemetry:heartbeat` periodic task (scheduler every 15s, `asynq.Unique`) writes `worker:heartbeat` = unix time in Redis with 45s TTL and emits a log line, metric, and span. This is worker liveness.
2. `telemetry:ping` task enqueued by `GET /api/status` (throttled with `SET NX EX 10`) carrying traceparent. Handler logs "ping handled" and increments a counter. This makes API -> worker appear in the same trace as the dashboard's status call, satisfying success criterion 2 for all three components.
Tasks are no-ops by design; Phase 2 adds the reaper/provisioning tasks on the same wiring.

### Pattern 4: Browser page load as one trace
**What:** Initialize OTel before React renders. Create a root span `ui.page_load` at bootstrap and keep its `Context`. Wrap the generated client's `fetch` so **initial** requests (`/api/me`, first `/api/status`) execute under `context.with(trace.setSpan(context.active(), pageSpan), () => fetch(...))`; end the span when the first status data renders (or after 10s). Later polls get their own short root span per poll (each poll = one trace, as UI-SPEC wants a re-issued request to be separately traced). Use `StackContextManager` (the default for `WebTracerProvider` if no manager is registered... verify in Wave 0) so synchronous `context.with` works; avoid Zone.js. Keep `DocumentLoadInstrumentation` for resource timing, but note it creates a **separate** `documentLoad` trace; link it to `ui.page_load` or accept that two traces exist (document load vs app bootstrap). The success criterion only requires the dashboard span and API span in one trace.
**Export path:** SDK exporters use relative URLs `/otlp/v1/traces`, `/otlp/v1/logs`, `/otlp/v1/metrics`; nginx rewrites to the Collector. Same origin = no CORS, no preflight, `traceparent` propagated to `/api` by default.

### Pattern 5: Status endpoint for 10 services
**What:** `GET /api/status` returns a cached snapshot (never fans out per request):
```json
{ "generatedAt": "...", "grafanaUrl": "http://localhost:3000",
  "build": { "api": {"version":"...","commit":"...","builtAt":"..."}, "dashboard": {...} },
  "services": [ {"id":"api","group":"platform","status":"healthy|degraded|down|unknown","lastSignalAt":"...|null","detail":"..."} ] }
```
Dashboard build info: the SPA embeds its own version/commit/time at build time (Vite `define`) and the API returns only its own; the UI-SPEC "for API and dashboard in two columns" is satisfied client-side for the dashboard column. (If the planner prefers server-provided, pass `DASHBOARD_BUILD_*` env to the API instead.)
**Probe matrix (10s interval, 2s timeout, results cached in memory, one goroutine, no spans on probe HTTP calls to avoid 1 trace/second noise; probe results are exported as metrics `ephematool.service.up` (gauge) and `ephematool.probe.duration` with `service` label, bounded to 10 values):**

| Service | Health probe | Last-signal source |
|---------|--------------|--------------------|
| api | self (always ok if handler runs) | Prometheus `timestamp()` of own series |
| worker | Redis key `worker:heartbeat` present (TTL 45s) | heartbeat value |
| dashboard | `GET http://web/healthz` (nginx `location = /healthz {return 200;}`) | Prometheus series from browser metrics (`service_name="ephematool-dashboard"`) |
| postgres | `pool.Ping` | last successful probe |
| redis | `PING` | last successful probe |
| otel-collector | `GET :13133/` (health_check extension) | last successful probe |
| tempo | `GET :3200/ready` | last successful probe |
| loki | `GET :3100/ready` | last successful probe |
| prometheus | `GET :9090/-/ready` | last successful probe |
| grafana | `GET :3000/api/health` | last successful probe |

Status mapping: probe failed -> `down`; probe ok but a platform service (api/worker/dashboard) has no telemetry signal for >60s -> `degraded` (this is the "telemetry health" dimension); never probed or Prometheus unavailable for the signal query -> `unknown`; else `healthy`. Last-signal query (one call): `max by (service_name) (timestamp(target_info{service_namespace="ephematool"}))`, or if `target_info` lacks `service_name`, set `otlp.promote_resource_attributes` in `prometheus.yml` (service.name, service.namespace, service.version, deployment.environment.name) and query a promoted series. Prometheus down degrades only those three rows to `unknown`. The prober uses a plain `http.Client` (no otelhttp) deliberately.

### Pattern 6: Conventions as one source of truth (D-09)
`conventions/telemetry.json` holds `{ "k8sLabelPrefix": "ephematool.io/", "otelAttrPrefix": "ephematool.env.", "keys": {"envId":"env-id", ...} }`. `tools/genconventions` generates `internal/conventions/conventions.gen.go` and `web/src/conventions.gen.ts`; CI/`make gen` fails on diff. Phase 1 uses it for `ephematool.env.*` placeholder attributes (e.g. resource attr `ephematool.env.name=local`) and exposes constants Phase 2 consumes. Keep OTel metric names under `ephematool.*` (non-env) and **never** put `env.id` on metric labels (PITFALLS 8).

### Pattern 7: Auth flow (all server-side)
1. `GET /api/auth/github/login?next=/` (plain handler, not in OpenAPI): validate `next` is a same-origin relative path (reject `//`, scheme, backslash); generate `state` (32 random bytes) and PKCE verifier; store both plus `next` in a short-lived (10 min) HttpOnly cookie (`SameSite=Lax`, scoped to `/api/auth/github`), redirect to GitHub with `scope=read:org`, `code_challenge`, `S256`.
2. `GET /api/auth/github/callback`: constant-time compare `state`; on `error=access_denied` redirect `/login?reason=failed`; exchange code with verifier; call `/user`; evaluate allowlist (see below); **discard the token immediately** (never stored, never logged).
3. Allowed -> upsert `users` row (github_id PK, login, avatar_url, last_login_at), `sessions.RenewToken(ctx)` (fixation), put `user_id, login, avatar, role, orgs, last_touch` in the session, redirect to `next`. Denied -> `slog.WarnContext("auth.denied", login, github_id, reason, required_orgs)`, counter `ephematool.auth.logins{outcome=denied}`, no session, redirect `/denied?reason=org_membership&login=<login>`.
4. `GET /api/me` (Huma, 401 when no session) -> `{login, avatarUrl, role, orgs}`; `GET /api/auth/config` (public) -> `{allowedOrgs, allowedTeams}` for the denied page; `POST /api/auth/logout` destroys session, clears cookie.
**Allowlist logic:** for each configured org: `GET /user/memberships/orgs/{org}` (go-github `Organizations.GetOrgMembership(ctx, "", org)`), require `state == "active"`. For each configured `org/team-slug`: `GET /orgs/{org}/teams/{slug}/memberships/{login}` with `state == "active"`. Allowed if any rule matches. `404` -> not a member. `403` -> log `auth.org_check_forbidden` distinctly (usually org OAuth-app restrictions; the org owner must approve the app). Missing `read:org` yields false negatives for private membership. `org_membership` page copy lists `allowedOrgs/Teams` from config.
**Role:** `admin` if `strings.EqualFold(login, any ADMIN_LOGINS)`, else `viewer`. Evaluated at login and stored in session (so a config change takes effect at next login; acceptable for Phase 1, documented).
**Sliding session (scs):**
```go
sm := scs.New()
sm.Store = goredisstore.New(rdb)
sm.IdleTimeout = 7 * 24 * time.Hour
sm.Lifetime    = 30 * 24 * time.Hour   // absolute cap; scs default is 24h and would silently kill 7-day sessions. Value is an assumption (A2)
sm.Cookie.Name = "ephematool_session"; sm.Cookie.HttpOnly = true; sm.Cookie.SameSite = http.SameSiteLaxMode
sm.Cookie.Secure = cfg.CookieSecure; sm.Cookie.Persist = true   // survives browser restart (default true, verified)
```
scs commits (and therefore re-slides the Redis TTL and cookie `Max-Age`) **only when the session is modified** (verified in `commitAndWriteSessionCookie`). In an auth middleware do `if time.Since(lastTouch) > 5*time.Minute { sm.Put(ctx,"last_touch",now) }`. The SPA's 15s status poll then keeps an open tab alive, which matches "active".
**CSRF:** `http.NewCrossOriginProtection()` (stdlib, Go 1.25+) wrapped around the mux for non-safe methods plus `SameSite=Lax`; logout is POST. State-changing GETs are forbidden by convention.

### Anti-Patterns to Avoid
- **Instrumenting asynq's internal Redis client with redisotel:** asynq polls Redis continuously (BRPOP/Lua); that becomes a constant stream of orphan root traces. Instrument only the app-owned go-redis client (sessions, throttle, heartbeat reads). Let asynq create its own connection (default `asynq.RedisClientOpt`).
- **`slog.Info(...)` without context:** produces logs without `trace_id`/`span_id` and breaks Loki-to-Tempo correlation.
- **Raw `r.URL.Path` as span name/metric label:** unbounded cardinality (PITFALLS 8). Use the route template.
- **Putting `ephematool.env.id` or user login on metrics:** per-env/user identity belongs on traces/logs only.
- **Exposing the Collector's OTLP port to the browser directly:** needs CORS and opens an unauthenticated write path from anywhere. Use the nginx same-origin proxy with rate limits.
- **Fan-out health probing inside the request handler:** the 10 probes with 2s timeouts would make `/api/status` slow when a backend is down. Cache from the background prober.
- **Storing the GitHub access token:** only needed once at login for the membership check.
- **One shared Collector pipeline for platform and (future) environment telemetry:** keep named `/platform` pipelines now and reserve `/environments` receivers/pipelines for Phase 2 (PITFALLS 9).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Session cookies, renewal, expiry | Custom token table + cookie code | `scs/v2` + `goredisstore` | Fixation (`RenewToken`), cookie flags, idle/absolute timeouts, Redis TTL are subtle |
| OAuth2 code flow + PKCE | Manual HTTP token exchange | `golang.org/x/oauth2` (`GenerateVerifier`, `S256ChallengeOption`, `VerifierOption`) | Edge cases and error parsing |
| GitHub API calls | Hand-written REST structs | `go-github/v92` | Reused in Phase 2 |
| HTTP/DB/Redis/queue tracing | Manual span code everywhere | otelhttp, otelpgx, redisotel; custom only for asynq (no official middleware) | Semantic conventions |
| Log <-> trace correlation | Custom log wrappers adding trace_id | `otelslog` bridge | Automatic IDs and OTLP export |
| OpenAPI spec | Hand-written YAML | Huma generated 3.1 | Drift-free |
| TS API types | Hand-written fetch types | `openapi-typescript` + `openapi-fetch` | Typed client (D-07) |
| Job retries/scheduling | Redis lists + cron goroutines | asynq (Server, Scheduler, `Unique`) | Retry/backoff/dedupe |
| Migrations | Ad hoc SQL on startup | goose (embedded) run as `api migrate` | Ordering, version table |
| Dashboards of backends | Custom UI for traces | Grafana + deep link | Project out-of-scope rule |
| Cross-origin POST protection | Custom token scheme | `http.CrossOriginProtection` + SameSite=Lax | Stdlib |

**Key insight:** Phase 1 is almost entirely integration. Every piece has a mature library; the risk is wiring (context propagation, middleware order, config of five Grafana-family services), not algorithms.

## Common Pitfalls

### Pitfall 1: Sliding sessions that never slide (and die after 24h)
**What goes wrong:** Users get logged out after 24h, or sessions expire exactly 7 days after login regardless of activity.
**Why:** scs `Lifetime` defaults to 24h (absolute); scs only commits when the session is Modified, so reads do not extend the TTL.
**Avoid:** Set `Lifetime` explicitly and touch the session at most every 5 minutes; test with a controllable clock/short timeouts (`IdleTimeout=2s`).
**Warning signs:** Redis `TTL session:*` not increasing across requests.

### Pitfall 2: otelhttp loses `http.route` behind middleware
**What:** Span names are `GET` only and metrics lack route.
**Why:** otelhttp reads `Request.Pattern` from the original request after the handler; middleware that calls `next.ServeHTTP(w, r.WithContext(...))` (scs does) breaks it (opentelemetry-go-contrib#9859).
**Avoid:** Pattern 2 (set name, `http.route`, and Labeler attribute from Huma `ctx.Operation().Path`). Assert in tests that span name contains the route template.

### Pitfall 3: Browser spans don't join the API trace
**What:** `/api/me` shows as a root trace in Tempo with no browser parent.
**Why:** `instrumentation-fetch` uses the *active* context; React/TanStack Query fires fetches in async callbacks with no active span unless a context manager and explicit parent are used. DocumentLoad produces its own trace.
**Avoid:** Pattern 4 (explicit `context.with` around fetch). Verify via Playwright that the `traceparent` request header's trace ID resolves in Tempo to spans from `ephematool-dashboard` and `ephematool-api`.

### Pitfall 4: Telemetry flood from queue internals and probes
**What:** Tempo fills with Redis polling spans or one trace per health probe.
**Avoid:** Don't instrument asynq's client; probe with an un-instrumented client; export probe results as metrics.

### Pitfall 5: Fresh checkout cannot log in, so "one command" appears broken
**What:** `docker compose up` works but "Continue with GitHub" 500s because no OAuth App exists.
**Avoid:** `.env.example` documents creating an OAuth App (callback `http://localhost:8080/api/auth/github/callback`), the API starts fine without credentials, `/api/auth/config` reports `githubConfigured:false`, and `/api/auth/github/login` redirects to `/login?reason=failed` with a log line naming the missing variable. Compose must not hard-fail on unset OAuth vars (use `${GITHUB_CLIENT_ID:-}`). Automated tests use `cmd/fakegithub` (Pattern 7 endpoints, base URLs overridable via `GITHUB_OAUTH_BASE_URL`, `GITHUB_API_BASE_URL`).

### Pitfall 6: GitHub org third-party-app restrictions return 403/404
**What:** A genuine org member is denied because the org restricts OAuth app access.
**Why:** Unapproved OAuth apps are limited to public org data; membership may look absent (CITED: GitHub docs on OAuth app access restrictions via search results; MEDIUM).
**Avoid:** Request `read:org`; the denied page and docs tell owners to approve the app; log 403 distinctly; README section "Org setup".

### Pitfall 7: Prometheus renames OTLP metrics
**What:** Queries for `http.server.request.duration` return nothing.
**Why:** Default `translation_strategy: UnderscoreEscapingWithSuffixes` yields `http_server_request_duration_seconds_{bucket,count,sum}`; counters get `_total`; dots become underscores [CITED: prometheus.io/docs/guides/opentelemetry].
**Avoid:** Define dashboard and status queries from names observed in a Wave 0 spike; keep names in one Go/TS constants location.

### Pitfall 8: Prometheus/Loki/Tempo ingest config
- Prometheus needs `--web.enable-otlp-receiver` (off by default) [CITED]; nothing is promoted to labels unless `otlp.promote_resource_attributes` is set [CITED]; add `storage.tsdb.out_of_order_time_window: 30m` because multiple exporters hit it [CITED].
- Loki default index labels are a fixed resource-attribute list (service.name, service.namespace, deployment.environment.name, ...); `trace_id` lands in structured metadata, so query it with `| trace_id="..."`, not a stream selector [CITED: grafana.com Loki OTLP docs; trace_id specifically is an inference, verify in Wave 0].
- Collector 0.162 must use `otlphttp` to Loki (`endpoint: http://loki:3100/otlp`); the old `loki` exporter is gone.

### Pitfall 9: Compose startup ordering and healthchecks
Distroless images (collector, tempo, loki, grafana variants) have no shell/curl, so container `healthcheck` blocks cannot be used. Use `depends_on: condition: service_started` for them, `service_healthy` only for postgres (`pg_isready`) and redis (`redis-cli ping`), and let OTLP exporters retry. `docker compose up --wait` then waits only on those with healthchecks. `scripts/verify-telemetry.sh` must poll each backend readiness URL from the host before asserting.

### Pitfall 10: GitHub username as admin identity
GitHub logins can be renamed and later reclaimed. D-02 mandates usernames; mitigate by storing `github_id` in `users`, logging both on every login, and noting numeric-ID pinning as a Phase 2 hardening item.

### Pitfall 11: Cookie `Secure` on plain-HTTP localhost
`Secure` cookies are accepted on `http://localhost` by Chromium and Firefox but not reliably by Safari. Make `COOKIE_SECURE` configurable; Compose default `false`, documented production default `true`. Do not use the `__Host-` prefix in dev.

### Pitfall 12: Logs signal churn
Go `sdk/log` and browser `sdk-logs` are beta. Pin exact versions, upgrade OTel Go/JS as a set, and keep the bootstrap in one package so breakage is localized.

## Code Examples

### Huma on stdlib mux with OpenAPI export subcommand
```go
// Source: huma.rocks (humago adapter, OpenAPI export); API shapes verified in module source v2.39.1
mux := http.NewServeMux()
api := humago.New(mux, huma.DefaultConfig("Ephematool API", buildinfo.Version))
api.UseMiddleware(routeTagging)                 // Pattern 2
huma.Get(api, "/api/me", meHandler)
// `ephematool-api openapi > api/openapi.json` (subcommand builds the API without serving)
b, _ := api.OpenAPI().MarshalJSON()
```
SPA: `openapi-typescript api/openapi.json -o web/src/api/schema.d.ts`; `make gen` + CI diff check keeps the committed spec honest.

### Goose migration, embedded
```go
// Source: goose docs (embed pattern) - ASSUMED structure, verify in Wave 0
//go:embed *.sql
var fs embed.FS
goose.SetBaseFS(fs); _ = goose.SetDialect("postgres"); err := goose.Up(db, ".")   // db from pgx stdlib
```
Migration `00001_users.sql`: `users(github_id bigint primary key, login text not null, avatar_url text, first_login_at timestamptz default now(), last_login_at timestamptz not null)`.

### Collector config (core of `deploy/otel/collector.yaml`)
```yaml
# Source: Collector docs + Loki/Prometheus OTLP docs cited above; assembled config ASSUMED, prove in Wave 0
receivers:
  otlp/platform:
    protocols: { grpc: { endpoint: 0.0.0.0:4317 }, http: { endpoint: 0.0.0.0:4318 } }
processors:
  memory_limiter: { check_interval: 1s, limit_mib: 400, spike_limit_mib: 100 }
  resource/platform: { attributes: [ { key: deployment.environment.name, value: local, action: upsert } ] }
  attributes/redact: { actions: [ {key: http.request.header.authorization, action: delete}, {key: http.request.header.cookie, action: delete}, {key: http.response.header.set-cookie, action: delete} ] }
  batch: {}
exporters:
  otlp/tempo: { endpoint: tempo:4317, tls: { insecure: true } }
  otlphttp/loki: { endpoint: http://loki:3100/otlp }
  otlphttp/prometheus: { endpoint: http://prometheus:9090/api/v1/otlp }
extensions: { health_check: { endpoint: 0.0.0.0:13133 } }
service:
  extensions: [health_check]
  telemetry: { metrics: { level: normal, readers: [ { pull: { exporter: { prometheus: { host: 0.0.0.0, port: 8888 } } } } ] } }
  pipelines:
    traces/platform:  { receivers: [otlp/platform], processors: [memory_limiter, resource/platform, attributes/redact, batch], exporters: [otlp/tempo] }
    metrics/platform: { receivers: [otlp/platform], processors: [memory_limiter, resource/platform, batch], exporters: [otlphttp/prometheus] }
    logs/platform:    { receivers: [otlp/platform], processors: [memory_limiter, resource/platform, attributes/redact, batch], exporters: [otlphttp/loki] }
# Phase 2 adds receivers otlp/environments + pipelines */environments with separate limiter and rate limits
```
Collector self-telemetry configuration syntax changes between versions (`service.telemetry.metrics.readers` is the current form, older `address:` is deprecated): verify against 0.162 docs in Wave 0.

### Prometheus
```yaml
# prometheus.yml
global: { scrape_interval: 15s }
otlp: { promote_resource_attributes: [service.name, service.namespace, service.version, deployment.environment.name], translation_strategy: UnderscoreEscapingWithSuffixes }
storage: { tsdb: { out_of_order_time_window: 30m } }
scrape_configs:
  - { job_name: otel-collector, static_configs: [ { targets: ['otel-collector:8888'] } ] }
  - { job_name: tempo, static_configs: [ { targets: ['tempo:3200'] } ] }
  - { job_name: loki, static_configs: [ { targets: ['loki:3100'] } ] }
  - { job_name: grafana, static_configs: [ { targets: ['grafana:3000'] } ] }
# command flags: --web.enable-otlp-receiver --storage.tsdb.retention.time=7d
```

### Grafana provisioning (correlation, the heart of success criterion 2)
Datasources `prometheus` (default), `tempo` (`jsonData.tracesToLogsV2: {datasourceUid: loki, filterByTraceID: true, filterBySpanID: false, tags: [{key: service.name, value: service_name}]}`, `tracesToMetrics.datasourceUid: prometheus`, `serviceMap.datasourceUid: prometheus`), `loki` (`jsonData.derivedFields: [{name: trace_id, matcherType: label, matcherRegex: trace_id, url: '$${__value.raw}', datasourceUid: tempo}]`). Anonymous Admin access via `GF_AUTH_ANONYMOUS_ENABLED=true`, `GF_AUTH_ANONYMOUS_ORG_ROLE=Admin`, `GF_AUTH_DISABLE_LOGIN_FORM=true` for the local stack only (flag clearly as dev-only). Provision one dashboard "Ephematool platform" (request rate, p95 latency, error ratio from `http_server_request_duration_seconds_*`, worker task counters, auth outcomes). `derivedFields` with `matcherType: label` is MEDIUM confidence (Grafana 10.3+ feature, not re-verified against 13.x).

### nginx (same-origin front door)
```nginx
# deploy/nginx/default.conf
location /api/ { proxy_pass http://api:8081; proxy_set_header Host $host; proxy_set_header X-Forwarded-Proto $scheme; }
location = /healthz { return 200 "ok"; }
location ~ ^/otlp/(v1/(traces|logs|metrics))$ { limit_except POST { deny all; } client_max_body_size 1m; limit_req zone=otlp burst=20 nodelay; proxy_pass http://otel-collector:4318/$1; }
location / { try_files $uri /index.html; }
```
(`limit_req_zone` goes in `http {}`; do not forward `Cookie` to the Collector: `proxy_set_header Cookie "";`.)

### SPA trace-aware fetch (Pattern 4)
```ts
// Source: OTel JS API (context/trace) - structure ASSUMED, prove with Playwright check
import { context, trace } from '@opentelemetry/api';
export function pageLoadFetch(pageCtx: Context): typeof fetch {
  return (input, init) => context.with(pageCtx, () => fetch(input, init));   // instrumentation-fetch patches global fetch
}
// openapi-fetch: createClient<paths>({ baseUrl: '/', fetch: pageLoadFetch(pageCtx) })
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Collector `loki` exporter | `otlphttp` to Loki `/otlp` | Loki 3.x | Native OTLP, structured metadata |
| `remote_write` for metrics | Prometheus OTLP receiver `/api/v1/otlp` | Prometheus 3.x | Flag required |
| JS `addSpanProcessor`, `Resource` class | `spanProcessors` ctor option, `resourceFromAttributes` | OTel JS SDK 2.0 | Browser setup differs from older tutorials |
| `otelhttp.WithRouteTag` | `Request.Pattern` | contrib ~0.5x-0.6x | Removed; see Pitfall 2 |
| Third-party CSRF middleware | `http.CrossOriginProtection` | Go 1.25 | Stdlib |
| Hand-rolled multi-handler for slog | `slog.NewMultiHandler` | Go 1.26 | Verified locally |
| OAuth without PKCE | PKCE supported for OAuth Apps | GitHub, 2025-07-14 | Use S256 [CITED: github.blog changelog] |
| Tempo 2.x ingesters/Kafka-less | Tempo 3.x new architecture; monolith (`-target=all`) needs no Kafka | Tempo 3.0 | Config migration needed if on 3.x [CITED: grafana.com Tempo docs via search] |

**Deprecated/outdated:** STACK.md's Fastify/Drizzle/pg-boss/better-auth/OTel-JS-server rows (D-05); `@opentelemetry/instrumentation-xml-http-request` unneeded because the client uses `fetch`.

## Runtime State Inventory

Not applicable: greenfield phase (no rename/refactor/migration). Omitted by design.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Sessions have an absolute `Lifetime` cap of 30 days in addition to the 7-day idle timeout | Pattern 7 | CONTEXT only specifies idle expiry; user may want no absolute cap (set Lifetime large) or shorter. Needs confirmation |
| A2 | OAuth App (not GitHub App) is used for login in Phase 1, scope `read:org` | Standard Stack, Pattern 7 | If org policy forbids OAuth Apps, switch to GitHub App user tokens (needs Members: read, install on org) |
| A3 | Admin role is evaluated at login only (config change effective at next login) | Pattern 7 | Admin revocation delayed up to session life; acceptable for Phase 1 |
| A4 | `max by (service_name)(timestamp(target_info))` yields per-service last-signal; fallback is promoted-attribute series | Pattern 5 | Last-signal column wrong; Wave 0 spike decides |
| A5 | Grafana Loki derived field with `matcherType: label` works for structured-metadata `trace_id` on Grafana 13.2 | Code Examples | Logs-to-trace link missing; fallback: Explore query `| trace_id="..."` documented |
| A6 | Tempo 2.10.8 chosen for a known-good monolithic config instead of 3.1.0 | Standard Stack | 2.x ages out; migrate with `tempo-cli migrate config` later |
| A7 | `redis:8.8-alpine`, `nginx:1.31-alpine` tags exist | Standard Stack | Trivial tag fix |
| A8 | go-redis custom client option for asynq not needed (asynq creates its own connection) | Pattern 3 | None |
| A9 | `StackContextManager` default in `WebTracerProvider.register()` makes synchronous `context.with` work without Zone.js | Pattern 4 | Browser spans may not parent; fallback: register `StackContextManager` explicitly (exported by sdk-trace-web) |
| A10 | Grafana anonymous Admin is acceptable for the local dev stack | Code Examples | Security review may object; keep Compose-only and documented |
| A11 | Dashboard build info is embedded in the SPA at build time rather than served by the API | Pattern 5 | Planner may prefer server-provided |
| A12 | Browser logs/metrics are limited to a small set (page_load, API error log lines; page_views counter and a vitals histogram) | Standard Stack | OBS-01 interpretation of "dashboard emits logs/metrics" could demand more |
| A13 | Collector self-metrics syntax under `service.telemetry.metrics.readers` is valid for 0.162 | Code Examples | Config fails to start; check Collector docs in Wave 0 |

## Open Questions

1. **Tempo 2.10.8 or 3.1.0?**
   - Known: 3.1.0 released 2026-09-29; monolithic `-target=all` runs without Kafka (docs conflict slightly with the quick-start page); config differs from 2.x (`live_store`, no `ingester`).
   - Unclear: whether a 3.x single-binary Compose is stable enough for a local dev stack.
   - Recommendation: start on 2.10.8 for the Wave 0 spike; adopt 3.1.0 only if the spike can run it with a config produced by `tempo-cli migrate config --mode=monolithic`.
2. **Absolute session cap and revalidation of org membership** (A1/A3): Should a session older than N days force a fresh GitHub login so org removal takes effect? Recommendation: 30-day absolute cap, confirm with user.
3. **Dashboard liveness definition**: nginx `/healthz` plus browser-metric recency. If nobody has the page open, the dashboard has no recent signal, but the viewer of the status page is by definition the one providing it. Acceptable; revisit if a headless uptime check is wanted.
4. **Public hostname for Grafana deep link** is `GRAFANA_PUBLIC_URL` (default `http://localhost:3000`); document for non-local use.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Docker Engine | Compose stack, testcontainers | ✓ | 27.0.3 | — |
| Docker Compose | One-command stack (`up --wait`) | ✓ | v5.3.0 | — |
| Go | Build/tests/codegen | ✓ | 1.26.2 | Build inside `golang:1.26-alpine` |
| Node.js | SPA build/tests | ✓ | v25.9.0 (Docker build uses `node:24-alpine`) | Build in container |
| ctx7 CLI | Docs lookup (research only) | ✗ | — | WebSearch/WebFetch used |
| slopcheck | Package audit (research only) | ✓ | installed | — |
| GitHub OAuth App credentials | Real login (manual UAT only) | ✗ (user must create) | — | `cmd/fakegithub` for automation |
| Outbound internet to Docker Hub / GitHub | Image pulls, real OAuth | assumed ✓ | — | — |

**Missing with no fallback:** none blocking. **Missing with fallback:** GitHub OAuth credentials (fake GitHub for CI; real login is a human-verify item).

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Go | `testing` + testify 1.12.1; integration via testcontainers-go 0.44 (build tag `integration`); miniredis for fast session/status unit tests |
| SPA | Vitest 5.0.3 (unit), Playwright 1.64.0 (e2e) |
| Stack e2e | `scripts/verify-telemetry.sh` (bash + curl + jq) against `docker compose -f deploy/compose.yaml -f deploy/compose.e2e.yaml up --build --wait` |
| Config files | none yet: Wave 0 creates `vitest.config.ts`, `playwright.config.ts`, `Makefile` targets |
| Quick run | `go test ./... -short` and `npm --prefix web test -- --run` |
| Full suite | `make verify` = `go test -tags integration ./...` + `npm --prefix web test -- --run` + compose up + `scripts/verify-telemetry.sh` + Playwright |

### Phase Requirements -> Test Map

| Req / Success criterion | Behavior | Test Type | Automated Command | File Exists? |
|---|---|---|---|---|
| AUTH-01 / SC1a | Login flow: state mismatch rejected, PKCE sent, allowed member gets session, denied member gets no session + `auth.denied` log, `next` open-redirect rejected | Go integration with `httptest` fake GitHub | `go test -tags integration ./internal/auth/... -run 'TestOAuth'` | ❌ Wave 0 |
| AUTH-01 / SC1b | Session persists across "browser restart": cookie has `Max-Age`/`Expires`, HttpOnly, SameSite=Lax; Redis TTL slides on touch; expires after idle timeout; absolute cap enforced; logout destroys session | Go unit (miniredis, short timeouts) | `go test ./internal/auth/... -run 'TestSession'` | ❌ Wave 0 |
| AUTH-01 / SC1c | Allowlist logic: org active, org pending, 404, 403 distinct, team match, admin list case-insensitive | Go unit with stubbed GitHub | `go test ./internal/auth/... -run 'TestAllowlist\|TestRole'` | ❌ Wave 0 |
| AUTH-01 / SC1d | UI: unauth redirects to /login, /denied renders orgs and focuses heading, user menu sign out lands on /login, 401 mid-use redirects | Playwright against compose.e2e (fake GitHub) | `npx playwright test e2e/auth.spec.ts` | ❌ Wave 0 |
| AUTH-01 | Real GitHub round trip with a real org | Manual (human-verify, end-of-phase) | — | n/a: needs real OAuth App |
| OBS-01 / SC2a | After login + `/api/me` + `/api/status`, a trace with spans from `ephematool-api` AND `ephematool-worker` is retrievable by trace ID from Tempo | Stack e2e script | `scripts/verify-telemetry.sh trace` (reads `X-Trace-Id`, polls `GET :3200/api/traces/<id>`, asserts distinct `service.name` set) | ❌ Wave 0 |
| OBS-01 / SC2b | Related metric exists: `http_server_request_duration_seconds_count{service_name="ephematool-api",http_route="/api/status"}` > 0 and worker task counter > 0 | Stack e2e script | `scripts/verify-telemetry.sh metrics` (Prometheus `/api/v1/query`) | ❌ Wave 0 |
| OBS-01 / SC2c | Correlated logs: Loki `query_range` for `{service_name="ephematool-api"} \| trace_id="<id>"` returns >=1 line; same for worker | Stack e2e script | `scripts/verify-telemetry.sh logs` | ❌ Wave 0 |
| OBS-01 / SC2d | Dashboard emits telemetry in the same trace: Playwright captures the `traceparent` of the first `/api/status` request; Tempo returns spans from `ephematool-dashboard` and `ephematool-api` for it; browser metrics/logs appear in Prometheus/Loki (`service_name="ephematool-dashboard"`) | Playwright + script | `npx playwright test e2e/telemetry.spec.ts` then `scripts/verify-telemetry.sh dashboard <traceId>` | ❌ Wave 0 |
| OBS-01 | `http.route` is the template, not raw path (cardinality guard) | Go unit with in-memory span exporter + metric reader | `go test ./internal/httpapi/... -run TestRouteTagging` | ❌ Wave 0 |
| OBS-01 | Redaction: Cookie/Authorization never reach Tempo | Stack e2e script (grep trace JSON) | `scripts/verify-telemetry.sh redaction` | ❌ Wave 0 |
| D-13 / UI | `/api/status` returns exactly 10 services in fixed groups; stopping a backend (`docker compose stop loki`) flips it to `down` within 2 probe cycles | Stack e2e script + Go unit (prober with fake targets) | `scripts/verify-telemetry.sh status` ; `go test ./internal/status/...` | ❌ Wave 0 |
| D-09 | Generated constants in sync with `conventions/telemetry.json` | Go test / `make gen && git diff --exit-code` | `make check-gen` | ❌ Wave 0 |
| D-07 | `api/openapi.json` equals freshly generated spec; TS types generated | `make check-gen` | same | ❌ Wave 0 |
| SC3 | Fresh checkout starts everything with one command | Stack e2e: clean clone dir, `docker compose up --build --wait`, all 10 services `healthy` in `/api/status` within 120s | `scripts/verify-telemetry.sh up` | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go test ./... -short` and `npm --prefix web test -- --run` (under 30s)
- **Per wave merge:** `go test -tags integration ./...` + `scripts/verify-telemetry.sh` against a running stack
- **Phase gate:** full `make verify` green before `/gsd-verify-work`, plus human-verify of real GitHub login

### Wave 0 Gaps
- [ ] `deploy/compose.yaml`, collector/tempo/loki/prometheus/grafana configs, with a smoke spike proving one trace + one metric + one log land and correlate (this resolves A4, A5, A6, A13 and confirms metric names for Pitfall 7)
- [ ] `scripts/verify-telemetry.sh` (skeleton with `up`, `trace`, `metrics`, `logs` subcommands)
- [ ] `cmd/fakegithub` and `deploy/compose.e2e.yaml`
- [ ] Go test scaffolding (`internal/auth` fake GitHub helper, miniredis fixtures), `vitest.config.ts`, `playwright.config.ts`
- [ ] `tools/genconventions`, `Makefile` (`up`, `gen`, `check-gen`, `test`, `verify`)
- [ ] Framework install: see Installation above

## Security Domain

`security_enforcement` is enabled (ASVS level 1, block on high).

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | yes | GitHub OAuth code flow with `state` + PKCE S256; no password handling; deny by org allowlist |
| V3 Session Management | yes | scs server-side sessions, opaque token, `HttpOnly`, `SameSite=Lax`, `Secure` in prod, `RenewToken` on login, destroy on logout, idle + absolute timeouts |
| V4 Access Control | yes (minimal) | Every `/api/*` except `auth/*` and `healthz` requires session (Huma middleware returns 401); role claim stored server-side; viewer vs admin only displayed in Phase 1, enforced from Phase 2 |
| V5 Input Validation | yes | Huma validates request schemas; `next` redirect parameter allowlisted to relative paths; `login` query param on `/denied` rendered as text only (React escapes) |
| V6 Cryptography | yes | `crypto/rand` for state/verifier; no custom crypto; GitHub client secret from env only |
| V7 Logging/Errors | yes | Never log tokens, codes, cookies, client secret; Collector `attributes/redact` deletes sensitive HTTP header attributes; auth denial logged with login + github_id |
| V9/V14 Config & Comms | yes | Secrets via env (`.env` git-ignored, `.env.example` committed); pinned image tags; nginx rate-limits public OTLP ingest |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| OAuth login CSRF / code injection | Spoofing | `state` cookie compare (constant time) + PKCE verifier |
| Open redirect via `next` | Tampering | Accept only `/`-prefixed single-slash paths, reject `//`, `\`, schemes |
| Session fixation | Spoofing | `RenewToken` after authentication |
| Cross-site POST (logout CSRF) | Tampering | `SameSite=Lax` + `http.CrossOriginProtection` |
| Org membership bypass via missing scope / 403 handling | Elevation | Fail closed: any non-`active` result or non-200/404 error denies and logs |
| Stale access after org removal | Elevation | Absolute session lifetime (A1); documented limit |
| Token/secret leakage into telemetry | Info disclosure | Do not store token; no HTTP header attributes; Collector redact processor; review log fields |
| Unauthenticated browser OTLP ingest abuse (DoS / telemetry poisoning) | DoS / Tampering | nginx path allowlist, POST-only, body size cap, `limit_req`; Collector `memory_limiter`; browser spans clearly tagged `service.name=ephematool-dashboard` |
| Open Grafana (anonymous Admin) | Elevation | Local compose only; bind published ports to `127.0.0.1`; flagged dev-only |
| Status endpoint information disclosure | Info disclosure | Require session; returns health only, no internal hostnames beyond service ids and configured public Grafana URL |
| Compose default creds (Postgres) | Spoofing | Dev-only credentials in `.env.example`; DB port not published to host |

## Sources

### Primary (HIGH confidence)
- Go module proxy (`go list -m -json <mod>@latest`, 2026-10-11): all Go versions in the stack tables
- npm registry (`npm view`, 2026-10-11): all JS versions
- Docker Hub tags API and GitHub releases API (2026-10-11): Collector 0.162.0, Tempo 2.10.8 / 3.1.0, Loki 3.7.8, Prometheus v3.15.0, Grafana 13.2.3, Postgres 18.6, Redis 8.8.3, golang 1.26.x, node 24, nginx 1.31.2
- Local source inspection: `scs/v2@v2.9.0` (`session.go` commit-only-when-modified, `Persist` default true, IdleTimeout semantics), `asynq@v0.26.0` (`Task.Headers`, `NewTaskWithHeaders`, go-redis v9), `huma/v2@v2.39.1` (`adapters/humago`, `UseMiddleware`, `OpenAPI().MarshalJSON/YAML/DowngradeYAML`)
- Local toolchain: `go doc log/slog.NewMultiHandler` (Go 1.26)
- https://prometheus.io/docs/guides/opentelemetry/ (OTLP receiver flag, promote_resource_attributes, translation_strategy, out-of-order window)
- https://grafana.com/docs/loki/latest/send-data/otel/ (otlphttp `/otlp`, default index labels, structured metadata)
- https://docs.github.com/en/rest/orgs/members (`GET /user/memberships/orgs/{org}`, `active|pending`, 404 for no affiliation)
- https://github.blog/changelog/2025-07-14-pkce-support-for-oauth-and-github-app-authentication/ (PKCE S256 supported)

### Secondary (MEDIUM confidence)
- https://github.com/open-telemetry/opentelemetry-go-contrib/issues/9859 and #6193 (otelhttp `Request.Pattern` behavior and bug), via search summary
- https://opentelemetry.io/docs/languages/js/getting-started/browser/ (WebTracerProvider + `spanProcessors`; page lacks fetch/OTLP detail)
- https://huma.rocks/features/cli/ and /tutorial/client-sdks/ (OpenAPI export pattern), via search summary
- https://grafana.com/docs/tempo/latest/set-up-for-tracing/setup-tempo/migrate-to-3/ and deployment-modes (monolith without Kafka), via search summary
- Search summaries on `read:org` and org OAuth-app access restrictions (GitHub community/issues)
- riverqueue.com/docs/open-telemetry and pkg.go.dev otelriver (alternative considered)
- Uptrace/Dash0 guides on otelslog (Logs beta status, `*Context` requirement)

### Tertiary (LOW confidence)
- Training knowledge for Tempo 2.x / Loki / Grafana provisioning YAML shapes, otelpgx tracer wiring, goose embed usage, StackContextManager default, Collector `service.telemetry` reader syntax: all flagged in the Assumptions Log and to be proven by the Wave 0 spike

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH for versions (registry-verified); MEDIUM for fit (Huma/asynq/scs verified in source, rest from training)
- Architecture: MEDIUM-HIGH (patterns well established; browser single-trace and Prometheus last-signal query need proving)
- Pitfalls: HIGH for scs/otelhttp/asynq (source and issue verified), MEDIUM for Grafana/Loki correlation details

**Research date:** 2026-10-11
**Valid until:** 2026-11-10 for application libraries; 7 days for image pins and OTel Logs (beta) versions
