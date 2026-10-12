---
phase: 01-telemetry-spine-github-login
reviewed: 2026-10-11T00:00:00Z
depth: standard
files_reviewed: 81
files_reviewed_list:
  - cmd/api/main.go
  - cmd/worker/main.go
  - cmd/fakegithub/main.go
  - internal/auth/allowlist.go
  - internal/auth/handlers.go
  - internal/auth/middleware.go
  - internal/auth/next.go
  - internal/auth/oauth.go
  - internal/auth/role.go
  - internal/auth/session.go
  - internal/auth/authtest/env.go
  - internal/config/config.go
  - internal/httpapi/api.go
  - internal/httpapi/middleware.go
  - internal/httpapi/status.go
  - internal/queue/client.go
  - internal/queue/heartbeat.go
  - internal/queue/middleware.go
  - internal/queue/server.go
  - internal/queue/tasks.go
  - internal/status/lastsignal.go
  - internal/status/model.go
  - internal/status/prober.go
  - internal/store/store.go
  - internal/store/migrations/00001_users.sql
  - internal/store/queries/users.sql
  - internal/telemetry/telemetry.go
  - internal/telemetry/promql.go
  - internal/fakegithub/server.go
  - internal/buildinfo/buildinfo.go
  - deploy/compose.yaml
  - deploy/compose.e2e.yaml
  - deploy/nginx/default.conf
  - deploy/otel/collector.yaml
  - deploy/tempo/tempo.yaml
  - deploy/loki/loki.yaml
  - deploy/prometheus/prometheus.yml
  - deploy/grafana/provisioning/datasources/datasources.yaml
  - deploy/grafana/provisioning/dashboards/dashboards.yaml
  - Dockerfile
  - .env.example
  - .gitignore
  - .bazelrc
  - MODULE.bazel
  - go.mod
  - scripts/verify-telemetry.sh
  - scripts/lib/verify-lib.sh
  - scripts/verify.d/*.sh
  - tools/stack.sh
  - tools/verify.sh
  - tools/genconventions/main.go
findings:
  critical: 0
  warning: 12
  info: 7
  total: 19
status: issues_found
---

# Phase 1: Code Review Report

**Reviewed:** 2026-10-11
**Depth:** standard
**Files Reviewed:** 81 (generated sqlc code and test files read only for context)
**Status:** issues_found

## Summary

The OAuth core is sound. State is compared in constant time against an HttpOnly, SameSite=Lax cookie scoped to `/api/auth/github`. PKCE S256 is used. `next` passes through `SafeNext` both when it is stored and when it is read back. The session token is renewed after the allowlist passes. The allowlist fails closed on 404, 403, pending state, transport error and an empty list. The redirect URI comes from `PUBLIC_BASE_URL`, not the Host header. I checked otelhttp v0.72.0 server attributes: it records `url.path` but not the query string, so the OAuth `code` and `state` do not reach spans. The Redis `db.statement` fix is in place on both the API and worker clients.

No blocker was found. The most serious issues are:

- Authorization is evaluated only at login and can go stale for up to 30 days.
- Huma routes are open unless someone remembers to tag them.
- `COOKIE_SECURE` defaults to off.
- Browser OTLP ingest is unauthenticated and shares a trust level with the platform pipelines.
- Anonymous Grafana Admin is a larger local attack surface than it needs to be.
- Several config values can panic or silently break the prober.

## Warnings

### WR-01: Org/team membership and admin role are never re-validated after login (up to 30 days stale)

**File:** `internal/auth/handlers.go:150-172`, `internal/auth/session.go:28-32`
**Issue:** The allowlist is evaluated once in the callback. The role and the org list are frozen in the session. The idle timeout is sliding and the absolute lifetime is 30 days.
- Scenario: an engineer is removed from the GitHub org, or removed from `ADMIN_LOGINS`. A session that is used at least weekly keeps working, including any admin role, until the 30-day cap. Nothing revokes it.
- Phase 2 will add environment-mutating, push-button actions on top of this.
- There is also no way to list or kill sessions for one user.

**Fix:** Pick one or both:
- Store `RecheckAt` in the session and re-run `Allowlist.Evaluate` on the next request after a short TTL. This needs the access token, so cache the membership decision server-side keyed by github_id instead of keeping the token.
- Maintain a Redis `user:{id}:session_epoch` key. Embed the epoch in the session and compare it in `RequireSession`. Bump it on admin demotion or revocation.
- At minimum, make `RequireSession` re-read the role from the DB or config instead of trusting the session copy.

### WR-02: RequireSession is opt-in per operation, so new Huma routes are public by default

**File:** `internal/auth/middleware.go:12-34`, `internal/httpapi/status.go:63`, `internal/auth/handlers.go:243`
**Issue:** `needsSession` returns false unless the operation sets `Security: session`. A Phase 2 handler that forgets `o.Security` is served unauthenticated with no error or test failure. There is also no role gate: `/api/me` is the only place a role is surfaced, and nothing enforces `admin`.
**Fix:** Invert the default. Require a session unless the operation sets an explicit marker, for example `Metadata["public"]=true`, and mark only `get-auth-config` and `post-auth-logout` public. Add a test that enumerates `api.OpenAPI().Paths` and fails if an operation has neither `Security` nor the public marker. Add a `RequireRole("admin")` helper before Phase 2 mutations land.

### WR-03: COOKIE_SECURE defaults to false with no guard against an https PUBLIC_BASE_URL

**File:** `internal/config/config.go:33`, `deploy/compose.yaml:119`, `internal/auth/session.go:36`, `internal/auth/handlers.go:79`
**Issue:** `COOKIE_SECURE` is opt-in. If `PUBLIC_BASE_URL=https://...` is deployed without the flag, both the session cookie and the oauth cookie are issued without `Secure`. They can then travel over plain HTTP, for example through a downgrade or mixed-content request. Nothing warns or fails at startup.
**Fix:** In `config.Load`, derive the default from the scheme and reject the inconsistent case:
```go
if u, _ := url.Parse(c.PublicBaseURL); u != nil && u.Scheme == "https" && !c.CookieSecure {
    return Config{}, errors.New("COOKIE_SECURE must be true when PUBLIC_BASE_URL is https")
}
```
Consider the `__Host-` cookie name prefix once Secure is on.

### WR-04: Unauthenticated browser OTLP ingest shares the platform pipelines; status signals and the pipeline can be spoofed or abused

**File:** `deploy/nginx/default.conf:35-44`, `deploy/otel/collector.yaml:1-8,54-66`, `internal/telemetry/promql.go:27,33`, `internal/status/prober.go:263`
**Issue:** `/otlp/v1/*` is open to anyone who can reach nginx. Rate limiting is per `$binary_remote_addr`, which is a single bucket behind a NAT or proxy. Everything lands on the same `otlp/platform` receiver and the same pipelines as the trusted Go services.
- Spoofing: a caller can send metrics with `service.name=ephematool-api` or `ephematool-worker`, `service.namespace=ephematool`, and metric `go_goroutine_count`. That keeps the status page's "telemetry signal" row healthy while the real service is silent. The `ephematool-dashboard` row can be spoofed trivially with `ephematool_ui_page_views_total`.
- Abuse: unbounded label cardinality goes straight into Prometheus. Arbitrary log bodies and attributes go into Loki as structured metadata. Gzip bodies of up to 1 MB expand inside the collector. The collector's `attributes/redact` list does nothing for this.

**Fix:**
- Give browser traffic its own receiver (a different port) and its own pipelines.
- On that pipeline, force identity with a `resource` processor, for example `service.name=ephematool-dashboard`.
- Add a `filter` processor that allow-lists metric names and drops unknown ones.
- Cap attributes with a `transform` processor.
- Keep the status LastSignal queries restricted to the trusted pipeline, for example by adding a pipeline-origin label.
- On nginx, add `limit_req_zone` keyed on a real client IP header and `gzip off` handling, and require a same-origin `Origin` or `Sec-Fetch-Site`.

### WR-05: Anonymous Grafana Admin is more privilege than needed and is reachable from any local browser tab

**File:** `deploy/compose.yaml:49-53`
**Issue:** Anonymous org role `Admin` plus the login form disabled means:
- Any page the developer visits can issue requests to `http://127.0.0.1:3000`, and DNS rebinding can read the responses.
- With Admin, a hostile page can create service accounts and datasources and install plugins. Datasource proxying can then reach every internal HTTP service on the compose network.
- The code comments call this dev-only, but nothing prevents `compose.yaml` being used outside a laptop. Only the port binding to 127.0.0.1 mitigates it.

**Fix:** Use `GF_AUTH_ANONYMOUS_ORG_ROLE: Viewer`. The provisioned dashboards need only view access. If Explore is needed, use Editor with `GF_SECURITY_ALLOW_EMBEDDING=false`, `GF_PLUGINS_PLUGIN_ADMIN_ENABLED=false` and `GF_SERVER_DOMAIN=localhost`.

### WR-06: Loki retention_period is ignored because the compactor retention is not enabled

**File:** `deploy/loki/loki.yaml:28-30`
**Issue:** In Loki, `limits_config.retention_period` takes effect only with `compactor.retention_enabled: true` (and a compactor working directory). Without it logs are never deleted. The logs include GitHub logins, ids and denied-login records, so the 7-day intent is not honoured.
**Fix:**
```yaml
compactor:
  working_directory: /loki/compactor
  retention_enabled: true
  delete_request_store: filesystem
```

### WR-07: nginx forwards `Host: $host` (no port), so Go CrossOriginProtection mis-compares Origin on the fallback path

**File:** `deploy/nginx/default.conf:28`
**Issue:** `$host` drops the port. When a request carries `Origin: http://localhost:8080` but no `Sec-Fetch-Site` (older Safari/WebViews, some automation), Go compares the Origin host `localhost:8080` against `r.Host` = `localhost`. It rejects the mutating request (for example logout) with 403 even though the request is same-origin.
**Fix:** Use `proxy_set_header Host $http_host;`. Optionally add `proxy_set_header X-Forwarded-Host $http_host;`.

### WR-08: Config values are not validated; zero or odd durations panic or silently break health

**File:** `internal/status/prober.go:79`, `internal/queue/server.go:36`, `internal/config/config.go:34-40`, `internal/queue/heartbeat.go:15`, `internal/status/model.go:63`
**Issue:**
- `PROBE_INTERVAL=0` (or negative) makes `time.NewTicker` panic at API start.
- `PROBE_TIMEOUT=0` makes every probe time out, so every row is Down.
- `HEARTBEAT_INTERVAL=0` gives `"@every 0s"`, so the scheduler registration fails.
- `HEARTBEAT_INTERVAL >= 45s` exceeds the 45 s heartbeat TTL, and `HeartbeatMaxAge` is hard-coded to match. The worker then flaps to Down while healthy.
- `SESSION_IDLE_TIMEOUT` or `SESSION_LIFETIME` of zero or negative gives a degenerate session policy: the cookie expires immediately or never.

**Fix:** Validate in `config.Load`: all durations positive, `HeartbeatInterval < HeartbeatTTL/2`, `SessionIdleTimeout <= SessionLifetime`, `ProbeTimeout < ProbeInterval`. Derive `HeartbeatTTL` and `HeartbeatMaxAge` from `HeartbeatInterval` (for example 3x) instead of using constants.

### WR-09: Allowlist silently swallows errors and malformed entries

**File:** `internal/auth/allowlist.go:44-58`
**Issue:**
- The transport or JSON error from `OrgMembership` and `TeamMembership` is dropped. Only an org 403 is logged. A GitHub outage, a rate limit, a bad token or a wrong `GITHUB_API_URL` produces a plain "denied" with no trace of the cause. The same applies to a team 403.
- A malformed `ALLOWED_TEAMS` entry (for example `sre` without an org) is skipped silently.
- If both allowlists are empty, every login is denied (fail closed, good) with no startup warning.

**Fix:** Log `err` and `status` at warn level (go-github errors carry no token) for any non-200/404 outcome. Validate `ALLOWED_TEAMS` entries in `config.Load`. Log a startup warning when GitHub is configured and both allowlists are empty.

### WR-10: Callback does not clear pre-existing session data and records no session-level denial

**File:** `internal/auth/handlers.go:164-172`
**Issue:** `RenewToken` keeps the old session payload. If a browser holds a session for user A and then completes OAuth as user B, the code overwrites the five keys. Any key that another feature adds later (a Phase 2 CSRF token or cached state) would survive across identities. This is not exploitable today.
**Fix:** Call `h.sm.Clear(ctx)` before `RenewToken`, or use `Destroy` followed by a fresh session, then `Put` the keys.

### WR-11: migrate-on-start has no cross-instance lock

**File:** `internal/store/store.go:49-57`, `cmd/api/main.go:128-132`, `internal/config/config.go:20`
**Issue:** `MIGRATE_ON_START` defaults to true. Two API replicas starting together (a rolling deploy in Phase 2, or the scaled e2e stack) both run `goose.UpContext`. Goose does not take a lock by default, so concurrent runs can race on the version table and fail one replica at boot. `goose.SetBaseFS` and `SetDialect` also mutate package globals.
**Fix:** Use goose's Provider API with `goose.WithSessionLocker(lock.NewPostgresSessionLocker())`, or take `pg_advisory_lock` around the migration. Default `MIGRATE_ON_START` to false outside local.

### WR-12: The `fresh` verification reuses the main stack's volumes, so it does not test a fresh start

**File:** `scripts/verify.d/fresh.sh:41-46`, `deploy/compose.yaml:1,179-184`, `tools/stack.sh:46-49`
**Issue:** The clone uses the same compose project name `ephematool`, and the main stack is stopped with `down` (no `-v`). The clone's `up_fake` attaches to the existing `postgres-data`, `loki-data`, `tempo-data` and other volumes. Migrations from an empty database, first-boot Grafana provisioning and the empty-Tempo path are therefore never exercised. The check can pass while a cold start is broken.
**Fix:** Run the clone with `COMPOSE_PROJECT_NAME=ephematool-fresh-$$`, which also isolates volumes, or run `tools/stack.sh clean` first. Document that this wipes the main stack's data.

## Info

### IN-01: Provider error text and unbounded `next` are written to logs and into a cookie

**File:** `internal/auth/handlers.go:99-100,109-111`
**Issue:** `reason = "provider_" + q.Get("error")` logs an attacker-controlled string of up to the URL length limit. `next` is unbounded after base64 encoding. A long `next` pushes the oauth cookie past 4 KB, the browser drops it, and the callback fails as `missing_oauth_cookie`. Both are self-inflicted at worst.
**Fix:** Allow-list provider errors (`access_denied`, `redirect_uri_mismatch`, ...) and map the rest to `provider_other`. In `SafeNext`, reject paths longer than about 512 bytes.

### IN-02: `/api/auth/config` and the Huma docs routes are public and disclose the allowlist

**File:** `internal/auth/handlers.go:240-249`, `internal/httpapi/api.go:57`
**Issue:** `GET /api/auth/config` exposes the allowed orgs and teams to anyone. `huma.DefaultConfig` also serves `/openapi.json` and `/docs` on the API's own port (8081 is published on localhost; nginx only proxies `/api/`). The allowlist is not secret, but it is recon data for targeting an org.
**Fix:** If the UI does not need the org names before login, return only `githubConfigured`. Set `cfg.DocsPath = ""` outside local.

### IN-03: HTTP server has only ReadHeaderTimeout

**File:** `cmd/api/main.go:150`
**Issue:** There is no `ReadTimeout`, `WriteTimeout`, `IdleTimeout` or `MaxHeaderBytes`. Slow-body clients can hold connections open indefinitely. nginx mitigates this only in the compose topology.
**Fix:** Set `ReadTimeout: 30s`, `WriteTimeout: 30s`, `IdleTimeout: 120s`.

### IN-04: nginx serves the SPA with no security headers

**File:** `deploy/nginx/default.conf:12-52`
**Issue:** There is no `Content-Security-Policy`, `X-Frame-Options` or `frame-ancestors`, `X-Content-Type-Options` or `Referrer-Policy`. This is a push-button operations console, so clickjacking is a real risk for Phase 2. nginx also resolves `api` and `otel-collector` once at startup. If the api container is recreated (`up --build`), nginx keeps the stale IP and returns 502 until restarted.
**Fix:** Add `add_header X-Frame-Options DENY always; add_header Content-Security-Policy "default-src 'self'; frame-ancestors 'none'" always; add_header X-Content-Type-Options nosniff always;`. Use a `resolver 127.0.0.11 valid=10s;` with variable `proxy_pass` for runtime re-resolution.

### IN-05: Collector hard-codes `deployment.environment.name=local`, overriding the app's APP_ENV

**File:** `deploy/otel/collector.yaml:14-18`
**Issue:** The apps set `deployment.environment.name` from `APP_ENV`. The `upsert` replaces it with `local` for every environment, so a staging or production deployment would be mislabeled.
**Fix:** Use `action: insert` so the SDK-provided value wins. Template the default from an env var.

### IN-06: Redis has no persistence or password; sessions and the queue die with the container

**File:** `deploy/compose.yaml:80-86`
**Issue:** There is no volume and no `--appendonly`, and no `requirepass`. The 30-day sessions (D-14) are lost whenever the redis container is recreated, for example on an image bump. Queued asynq tasks are lost too.
**Fix:** Add a `redis-data` volume with `--appendonly yes`. Set a password via env and update `REDIS_ADDR` consumers (`redis.Options{Password}`, `asynq.RedisClientOpt{Password}`) before Phase 2 exposes more.

### IN-07: Integration tests silently skip when Docker is unavailable

**File:** `internal/auth/authtest/env.go:127`
**Issue:** `t.Skipf("containers unavailable")` means the end-to-end OAuth, allowlist and session tests are reported as skipped, not failed. Under `--config=integration` in CI, a broken Docker socket would produce a green run with the whole auth suite skipped. `verify.sh` step 1 would not catch that.
**Fix:** Fail instead of skip when an env flag such as `REQUIRE_INTEGRATION=1` is set (set it in `--config=integration` and CI).

---

_Reviewed: 2026-10-11_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
