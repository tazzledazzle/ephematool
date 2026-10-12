# Phase 2: Create a Secure Preview Environment - Pattern Map

**Mapped:** 2026-10-12
**Files analyzed:** 62 new/modified (grouped where one analog covers several)
**Analogs found:** 51 with a repo analog / 62; 11 have no analog (see "No Analog Found")

Module path: `github.com/ephematool/ephematool`. All analog paths are relative to the repo root `/Users/terenceschumacher/dev/october-portfolio-projects/ephematool`. Line numbers refer to files as they exist today.

## File Classification

### Go backend

| New/Modified File | Role | Data Flow | Closest Analog | Match |
|---|---|---|---|---|
| `internal/config/config.go` (modify) | config | request-response | itself (`config.go` struct + `Load`) | exact |
| `internal/store/migrations/00002_environments.sql`, `00003_audit_events.sql` | migration | CRUD | `internal/store/migrations/00001_users.sql` | role-match |
| `internal/store/queries/environments.sql`, `audit.sql` | sqlc queries | CRUD | `internal/store/queries/users.sql` | exact |
| `internal/store/{environments,audit}.sql.go`, `models.go` (generated) | generated | CRUD | `internal/store/users.sql.go` | exact |
| `internal/store/BUILD.bazel` (modify: srcs, `embedsrcs`) | build | n/a | itself | exact |
| `internal/store/store.go` (modify: owner-URL migrate, WR-11 locker) | service | batch | itself `Migrate` (l.49-56) | exact |
| `internal/auth/role.go` (modify: creator) + `RequireRole` helper | middleware/util | request-response | `internal/auth/role.go`, `middleware.go` | exact |
| `internal/auth/handlers.go` (modify: `MeBody.Role` enum, WR-10 `Clear`, WR-02 secure-by-default) | controller | request-response | itself l.179-183, l.207-260 | exact |
| `internal/auth/allowlist.go` (extend: creator-team check via App token) | service | request-response | itself (`MembershipClient`, `Allowlist.Evaluate`) | exact |
| `internal/httpapi/environments.go` (create/get/list/sizes/templates ops) | controller | CRUD + request-response | `internal/httpapi/status.go` (Huma op with `Security`) | role-match |
| `internal/httpapi/audit.go` (admin list, cursor) | controller | CRUD | `internal/httpapi/status.go` | role-match |
| `internal/httpapi/preview.go` (`POST /api/preview/ticket`, `GET /internal/preview/auth` public) | controller | request-response | `status.go` (Huma) + `auth.Register` plain `mux.Handle` + `RouteHandler` (OAuth routes) | role-match |
| `internal/httpapi/api.go` (modify Deps, register) | wiring | n/a | itself l.28-71 | exact |
| `internal/envs/` (create service, validate, state machine, field errors) | service | CRUD + event-driven | `internal/auth/allowlist.go` (interface-seam service) + `internal/status/prober.go` | role-match |
| `internal/envs/probe.go`, `poll.go`, `dispatch.go`, `sweep.go` (asynq handlers) | worker handlers | event-driven | `internal/queue/heartbeat.go`, `middleware.go` | role-match |
| `internal/queue/tasks.go`, `client.go`, `server.go` (modify: new types, enqueue, NewMux) | queue | pub-sub | itself | exact |
| `internal/ghapp/` (App JWT, token cache, go-github clients, GHCR HEAD) | service/client | request-response | `internal/auth/oauth.go` (`newHTTPClient`, `newGitHubClient`) | role-match |
| `internal/previewauth/` (ticket/cookie JWT, ForwardAuth handler) | service + middleware | request-response | `internal/auth/handlers.go` (cookie, `subtle`, `SafeNext`) + `next.go` | role-match |
| `internal/audit/` (insert + list) | service | CRUD | `internal/store` sqlc `Queries` wrapper in `auth/handlers.go` | partial |
| `internal/fakegithub/server.go` (extend) | test fake | request-response | itself (`New`, `caller`, `writeJSON`) | exact |
| `cmd/fakegithub/main.go` (modify: env knobs) | cmd | n/a | itself | exact |
| `cmd/worker/main.go` (modify: DB, GitHub App, scheduler) | cmd | n/a | itself | exact |
| `cmd/api/main.go` (modify: owner-URL migrate, deps) | cmd | n/a | itself `migrate()` l.64-77, `serve()` | exact |
| `cmd/sampleapp/main.go` | cmd (reference workload) | streaming (OTLP) | `cmd/worker/main.go` + `internal/telemetry/telemetry.go` | partial |
| `templates/embed.go` + `templates/web-app/**` | go:embed pkg + Helm chart | transform | `internal/store/store.go` `//go:embed migrations/*.sql` | partial |
| Go tests: `internal/{envs,previewauth,ghapp,audit}/*_test.go` | test | mixed | `internal/queue/middleware_test.go`, `internal/auth/session_test.go`, `internal/auth/oauth_integration_test.go` | exact |

### Bazel, deploy, scripts

| New/Modified File | Role | Data Flow | Closest Analog | Match |
|---|---|---|---|---|
| `BUILD.bazel` (modify: `test_integration`, new `sh_binary` kind targets, `_GEN_PACKAGES`) | build | n/a | itself l.31-37, l.66-98 | exact |
| `internal/*/BUILD.bazel` (new packages) | build | n/a | `internal/queue/BUILD.bazel`, `internal/httpapi/BUILD.bazel` | exact |
| `tools/kind.sh` (kind_up/down/verify/runner) | script | batch | `tools/stack.sh` | exact |
| `tools/verify.sh` (modify: optional kind step) | script | batch | itself | exact |
| `scripts/verify.d/env-trace.sh`, `env-telemetry.sh` (+ preview auth check) | verify subcommand | request-response | `scripts/verify.d/trace.sh`, `worker.sh`, `redaction.sh` | exact |
| `scripts/verify.d/redaction.sh` (modify: `ticket=`, `ephematool_preview`) | verify subcommand | transform | itself l.138-150 `scan` calls | exact |
| `deploy/compose.yaml` (modify: worker env, collector port 4320) | config | n/a | itself | exact |
| `deploy/compose.e2e.yaml` (modify: App/GHCR/fake env) | config | n/a | itself | exact |
| `deploy/compose.preview.yaml` (Traefik file provider + whoami) | config | request-response | `deploy/compose.e2e.yaml` (overlay shape) | role-match |
| `deploy/otel/collector.yaml` (modify: `*/environments` pipelines) | config | streaming | itself (`otlp/platform`, pipelines, reserved comment l.64-66) | exact |
| `deploy/nginx/default.conf` (modify: security headers IN-04) | config | request-response | itself | exact |
| `deploy/kind/*` (cluster.yaml, RBAC, VAPs, traefik values/middlewares, versions.env) | config | declarative | none | none |
| `environments/**` (workflow, provision.sh, baseline chart, sizes.yaml) | config/script | batch | none (`tools/stack.sh` for shell style only) | none |
| `.env.example` (modify) | config | n/a | itself | exact |

### Web

| New/Modified File | Role | Data Flow | Closest Analog | Match |
|---|---|---|---|---|
| `web/src/router.tsx` (modify: 6 routes) | config | n/a | itself | exact |
| `web/src/components/AppShell.tsx` (modify: `NAV_ITEMS`) | component | n/a | itself l.11, l.54-68 | exact |
| `web/src/components/RoleBadge.tsx` (modify: `creator`) | component | n/a | itself | exact |
| `web/src/features/auth/useMe.ts` | hook | request-response | itself | exact |
| `web/src/components/ui/{input,label,select,radio-group,switch,alert}.tsx` | shadcn primitives | n/a | `web/src/components/ui/badge.tsx`, `button.tsx` (CLI output style) | exact |
| `web/src/components/{StatusBadge,CopyTraceButton,ProgressSteps,DefinitionList,FormField,SchemaFields,SizePresetCards}.tsx` | components | n/a | `HealthBadge.tsx`, `StatusError.tsx` | role-match |
| `web/src/features/environments/{EnvironmentsPage,NewEnvironmentPage,EnvironmentDetailPage}.tsx` | pages | request-response / polling | `features/status/StatusPage.tsx` | role-match |
| `web/src/features/audit/AuditPage.tsx` | page | request-response | `StatusPage.tsx` + `ServiceTable.tsx` | role-match |
| `web/src/features/preview/{AuthorizePage,PreviewDeniedPage}.tsx` | pages | request-response | `features/auth/DeniedPage.tsx`, `LoginPage.tsx` | exact |
| `web/src/telemetry/pageLoad.ts` (modify `routeTemplate`) | util | n/a | itself l.11-22 | exact |
| `web/src/api/schema.d.ts` + `api/openapi.json` (generated) | generated | n/a | itself via `//:gen` | exact |
| `web/**/*.test.tsx` (Vitest) | test | n/a | `features/status/status.test.tsx`, `features/auth/auth.test.tsx` | exact |
| `web/e2e/environments.spec.ts`, `audit.spec.ts`, `preview.spec.ts` | e2e | request-response | `web/e2e/auth.spec.ts`, `helpers.ts` | exact |

---

## Pattern Assignments

### 1. Operations opt in to auth (WR-02) and role gating

**Analog:** `internal/auth/middleware.go` (whole file, 35 lines), `internal/auth/handlers.go` l.207-230, `internal/httpapi/status.go` l.57-62.

The mechanism today is **opt-in**: `RequireSession` only enforces when the operation's `Security` contains the `"session"` scheme.

```go
// internal/auth/middleware.go:12-34
func RequireSession(api huma.API, sm *scs.SessionManager) func(huma.Context, func(huma.Context)) {
	return func(ctx huma.Context, next func(huma.Context)) {
		op := ctx.Operation()
		if op == nil || !needsSession(op) {
			next(ctx)
			return
		}
		if sm == nil || sm.GetInt64(ctx.Context(), KeyUserID) == 0 {
			_ = huma.WriteErr(api, ctx, http.StatusUnauthorized, "authentication required")
			return
		}
		next(ctx)
	}
}
func needsSession(op *huma.Operation) bool {
	for _, req := range op.Security {
		if _, ok := req["session"]; ok { return true }
	}
	return false
}
```

```go
// internal/auth/handlers.go:207-216  (scheme registration + middleware install)
oa.Components.SecuritySchemes["session"] = &huma.SecurityScheme{Type: "apiKey", In: "cookie", Name: CookieName}
api.UseMiddleware(RequireSession(api, h.sm))
```

```go
// internal/httpapi/status.go:57-62  (how an operation opts in)
}, func(o *huma.Operation) {
	o.OperationID = "get-status"
	o.Summary = "Telemetry status snapshot"
	// RequireSession answers 401 without a signed-in user (T-01-28).
	o.Security = []map[string][]string{{"session": {}}}
})
```
Alternative registration form: `huma.Register(api, huma.Operation{OperationID: ..., Security: []map[string][]string{{"session": {}}}}, handler)` (handlers.go:221-223).

Public operations today simply omit `Security` (`get-auth-config`, `post-auth-logout`, handlers.go:244-248, 252-255).

**Phase 2 conventions the new files MUST follow:**
- Every new Huma op in `environments.go`, `audit.go`, `preview.go` sets `Security: {{"session": {}}}` and a distinct `OperationID` (kebab-case, `verb-noun`: `create-environment`, `get-environment`, `list-environments`, `list-audit-events`, `post-preview-ticket`, `list-templates`).
- Research (AUTH-02 row) requires inverting WR-02 to **secure by default**: add a test that walks `api.OpenAPI().Paths` and fails for any operation that has neither `Security` containing `session` nor an explicit public marker (extension `x-public: true` via `op.Extensions` is the natural form; the three existing public ops `get-auth-config`, `post-auth-logout` and the new ForwardAuth endpoint must be marked). Implement by changing `needsSession` to "unless public" and add the test in `internal/httpapi` modeled on `middleware_test.go` (its `harness` builds an API on a `ServeMux`, see l.41-72).
- Role gate: add `auth.RequireRole(api, sm, resolver, "creator")` as a per-operation `huma.Operation.Middlewares` entry (Huma v2 supports `Middlewares huma.Middlewares` on the operation) or call a `h.requireRole(ctx, role)` helper at the top of the handler returning `huma.Error403Forbidden`. It must resolve the role **now** (D-06): `RoleFor`-style config check first (`internal/auth/role.go:9-16`, case-insensitive `strings.EqualFold`, `ADMIN_LOGINS` wins), then `CREATOR_LOGINS`, then team membership via the App installation token. Do not read `KeyRole` from the session for the gate (that is the stale-role bug, WR-01); the session role is only the UI hint.
- Plain (non-Huma) routes, e.g. `/internal/preview/auth`, register on the `mux` and wrap with `RouteHandler` for bounded `http.route`:

```go
// internal/auth/handlers.go:218-219
mux.Handle("GET /api/auth/github/login", wrap("GET", "/api/auth/github/login", http.HandlerFunc(h.login)))
mux.Handle("GET /api/auth/github/callback", wrap("GET", "/api/auth/github/callback", http.HandlerFunc(h.callback)))
```
`wrap` is `httpapi.RouteHandler` (middleware.go:34-38) passed in to avoid an `httpapi <-> auth` import cycle; a new `previewauth.Register(mux, h, wrap)` must take the same `RouteWrapper` parameter (type at handlers.go:28). Route must **not** live under `/api/` (nginx proxies `/api/` to the API, `deploy/nginx/default.conf` `location /api/`); Traefik calls the API port 8081 directly via `host.docker.internal`.

**`MeBody.Role` enum** (handlers.go:182): `` `json:"role" enum:"admin,viewer"` `` becomes `enum:"admin,creator,viewer"`; then `bazel run //:gen` regenerates `api/openapi.json` and `web/src/api/schema.d.ts` (drift-checked by `//:check_gen`). `RoleBadge` prop type must follow (see Web section).

**Callback hardening hooks (WR-10, WR-03):** `callback` (handlers.go:118-175) calls `h.sm.RenewToken(ctx)` then `Put`s four keys. Add `h.sm.Clear(ctx)` before the `Put`s. WR-03: refuse non-localhost `PublicBaseURL` when `!cfg.CookieSecure` in `config.Load`/startup (config validation, see section 2).

---

### 2. `internal/config/config.go` (modify)

**Analog:** itself, `internal/config/config.go:14-52`.

Conventions: `caarlos0/env/v11` struct tags, `envDefault` for every dev default, lists use `envSeparator:","` and are normalized in `Load` with `cleanList` (l.62-78), durations are `time.Duration`. Add (names from CONTEXT/RESEARCH): `MigrateDatabaseURL` (`MIGRATE_DATABASE_URL`, fall back to `DatabaseURL` when empty), `CreatorTeams`, `CreatorLogins`, `AllowedRepos` (all lists, add to the `cleanList` block at l.65-67), `GitHubAppID`, `GitHubAppInstallationID`, `GitHubAppPrivateKeyFile` (file path, never the PEM in env), `EnvironmentsRepo` (`org/name`), `PreviewDomain` (`preview.localhost`), `PreviewPort` (8088), `PreviewGatewayAddr`, `PreviewTokenKeyFile`, `PreviewCookieTTL` (30m), `DefaultTTL` (24h), `ImageTagFormat` (D-23), `ProvisionDeadline` (15m), poll/probe cadences. Validate all durations > 0 (WR-08) in `Load` after `env.Parse`; return an error rather than clamping.

`config` has no tests today; add `config_test.go` with `t.Setenv` + `Load()` (pattern is plain `testing`, no helper library needed) and a `BUILD.bazel` `go_test` (see section 11). The worker currently calls `config.Load()` (cmd/worker/main.go:33) and uses only `RedisAddr`, so the same struct serves it; do not split it (RESEARCH Pitfall 11 asks for one validated struct).

---

### 3. Migrations, sqlc queries and generated code

**Analogs:** `internal/store/migrations/00001_users.sql`, `internal/store/queries/users.sql`, `sqlc.yaml`, `internal/store/BUILD.bazel`, `internal/store/store.go`.

**Migration style** (`00001_users.sql`, whole file): lower-case SQL, `-- +goose Up` / `-- +goose Down` markers, `create table`, explicit `timestamptz not null default now()`.
```sql
-- +goose Up
create table users (
    github_id     bigint primary key,
    ...
    first_login_at timestamptz not null default now(),
    last_login_at  timestamptz not null default now()
);
-- +goose Down
drop table users;
```
Files are numbered `0000N_name.sql`, goose sorts lexically. New: `00002_environments.sql`, `00003_audit_events.sql` (the audit DDL in 02-RESEARCH "Audit migration" is the verified body; keep its `-- +goose StatementBegin/StatementEnd` wrapping around the `DO $$` block and the `CREATE FUNCTION ... $f$` block, otherwise goose splits on `;`). Each migration needs a `-- +goose Down` (drop trigger, function, table; do not drop the role). Per-table `GRANT` statements go in the same migration as the table (research: no `ALTER DEFAULT PRIVILEGES`). `users` and `environments` need `GRANT SELECT, INSERT, UPDATE ... TO ephematool_app`; put the `users` grant in the new `00002` migration since `00001` predates the role. CHECK constraint on `environments.status` (`requested|provisioning|running|failed`) per Pattern 8.

**Queries style** (`queries/users.sql`): sqlc `-- name: Verb :one|:many|:exec`, positional `$1`, lower-case SQL, `returning *`.
```sql
-- name: UpsertUser :one
insert into users (github_id, login, avatar_url)
values ($1, $2, $3)
on conflict (github_id) do update
    set login = excluded.login, ...
returning *;
```
New files `queries/environments.sql` and `queries/audit.sql`. CAS transitions are `:execrows` (`update environments set status='provisioning', run_id=$2 where id=$1 and status='requested'`) so the handler can check rows-affected (Pattern 8). Audit cursor list is `:many` keyed on `(occurred_at, id) < ($1,$2)` to use `audit_events_occurred_idx`.

**How generation + drift check work (do not hand-edit generated files):**
- `sqlc.yaml` (repo root): `engine: postgresql`, `schema: internal/store/migrations`, `queries: internal/store/queries`, `sql_package: pgx/v5`, `out: internal/store`. sqlc parses the **migrations** as the schema, so `DO $$`/`CREATE ROLE` migrations must parse (research VERIFIED that they do).
- Bazel: `internal/store/BUILD.bazel` `genrule(name="sqlc_gen")` globs `queries/*.sql` and `migrations/*.sql` automatically; outputs are `_gen/db.go`, `_gen/models.go`, `_gen/<query>.go` (computed from `glob(["queries/*.sql"])`, l.33). `write_source_files(name="gen")` copies them over `internal/store/*.go`, and its generated `gen_tests` target is what `//:check_gen` runs (root `BUILD.bazel:44-55`, `_GEN_PACKAGES` already lists `//internal/store`).
- **What a new query file requires by hand:** add `environments.sql.go` and `audit.sql.go` to `go_library.srcs` in `internal/store/BUILD.bazel` (l.5-11; srcs are explicit, not globbed) and add every new migration path to `embedsrcs` (l.12, currently only `migrations/00001_users.sql`). Gazelle does not manage `embedsrcs` for this package reliably; list them explicitly. Then `bazel run //:gen`.
- go:embed in code: `//go:embed migrations/*.sql` (store.go:18) picks the files up at runtime.

**Two-role connection (D-24):** `store.Migrate(ctx, pool)` (store.go:49-57) uses `goose.SetBaseFS` + `goose.UpContext` on a pool opened with `cfg.DatabaseURL`. Phase 2 changes: `cmd/api` `migrate()` (main.go:64-77) and the `MigrateOnStart` branch in `serve()` (main.go:112-116) must open a **separate pool with `cfg.MigrateDatabaseURL`**, migrate, close it, then open the runtime pool with the app-role `DatabaseURL`. Replace `goose.UpContext` with `goose.NewProvider(goose.DialectPostgres, db, fsys, goose.WithSessionLocker(...))` (WR-11) in `Migrate`. Existing helpers to reuse: `store.Open` (traced, pings), `store.OpenUntraced`.

**Integration test harness for two roles** (Wave 0): extend `internal/auth/authtest/env.go` pattern (see section 12): migrate with the owner URL, then build the `store.Queries` from a pool connecting as `ephematool_app`. The existing `NewEnv` runs `truncate users` (env.go:~137) as the owner; keep truncation on the owner pool, but `audit_events` cannot be truncated (trigger), so tests must use unique actors/ids instead of truncating it.

---

### 4. asynq tasks, tracing middleware, and trace propagation

**Analogs:** `internal/queue/tasks.go`, `client.go`, `server.go`, `middleware.go`, `heartbeat.go`.

**Task type constants + header injection** (`tasks.go:11-23`):
```go
const (
	TypePing      = "telemetry:ping"
	TypeHeartbeat = "telemetry:heartbeat"
)
func InjectHeaders(ctx context.Context) map[string]string {
	h := map[string]string{}
	otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(h))
	return h
}
```
New types follow the `noun:verb` naming: `TypeEnvDispatch = "env:dispatch"`, `TypeEnvPoll = "env:poll"`, `TypeEnvProbe = "env:probe"`, `TypeEnvSweep = "env:sweep"`.

**Producer** (`client.go:30-47`): producer span `"<type> publish"` with `SpanKindProducer`, attributes `messaging.system=asynq`, `messaging.destination.name=default`, `NewTaskWithHeaders(Type, payload, InjectHeaders(ctx))`, `EnqueueContext(ctx, task, asynq.MaxRetry(n))`, `span.RecordError` + `span.SetStatus(codes.Error, ...)` on failure. Copy this body for `EnqueueEnvDispatch(ctx, envID)`. Payload carries **only the environment id** (never tokens, never traceparent: the header carries it). The client uses its own un-instrumented Redis connection by design ("deliberately not instrumented (Pitfall 4)"); keep it that way.

**Re-injecting a stored trace (sweeper / poll continuation):** `InjectHeaders(ctx)` injects from `ctx`. For tasks created without a live request (sweep, self-rescheduling poll), parse the env row's stored `traceparent` into a context first: `ctx = otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier{"traceparent": row.Traceparent})`, then call the same `Enqueue*`. Poll continuations use a fresh `TaskID` plus `asynq.ProcessIn(d)`; sweeper uses `asynq.Unique(25*time.Second)` (see `NewScheduler`, server.go:30-44, which computes `uniq := every - time.Second` and passes `asynq.Unique(uniq), asynq.MaxRetry(0)`).

**Consumer** (`middleware.go:19-55`): `TracingMiddleware` extracts the context from `t.Headers()`, starts a `SpanKindConsumer` span named `t.Type()+" process"`, records `ephematool.queue.task.processed` / `.duration` with **only** `task.type` and `outcome` attributes ("no task IDs"). New handlers need no tracing code of their own for the consumer span; they `tracer.Start` child spans inside (`validate`, `github.dispatch`, `probe`). Do not add env ids to the *queue* metrics (cardinality). Env id/owner/template go on **spans** via constants, see section 13.

**Registration** (`server.go:19-27`):
```go
func NewMux(rdb *redis.Client) *asynq.ServeMux {
	mux := asynq.NewServeMux()
	mux.Use(TracingMiddleware)
	mux.HandleFunc(TypePing, pingHandler)
	mux.HandleFunc(TypeHeartbeat, heartbeatHandler(rdb))
	return mux
}
```
`NewMux` currently takes only `rdb`. The env handlers need DB + GitHub clients, and `internal/envs` will import `internal/queue` (to enqueue), so `queue` must NOT import `envs` (cycle). Change the signature to accept handler registrations, e.g. `NewMux(rdb *redis.Client, register func(*asynq.ServeMux))`, or have `cmd/worker/main.go` build the mux (`asynq.NewServeMux()`, `mux.Use(queue.TracingMiddleware)`, then `envs.Register(mux, deps)`), keeping `queue.NewMux` for ping/heartbeat. Dispatch queue must use concurrency 1 / >=1 s spacing for GitHub POSTs (Pattern 8): add a second queue in `NewServer`'s `Queues` map (currently `{"default": 1}`, Concurrency 4) rather than changing the default queue.

**Handler shape** (`heartbeat.go:21-36`): `func(ctx context.Context, t *asynq.Task) error`, log with `slog.InfoContext(ctx, "...")` so the OTLP log bridge adds trace_id; return nil for "someone else won" CAS outcomes, `asynq.SkipRetry` wrapped errors for permanent failures.

**Worker wiring** (`cmd/worker/main.go:33-79`): copy the existing order: `config.Load`, `telemetry.Setup`, Redis client with `redisotel.InstrumentTracing(rdb, redisotel.WithDBStatement(false))` + `InstrumentMetrics`, then queue server/scheduler. Add `store.Open(ctx, cfg.DatabaseURL)` (app role) + `store.New(pool)`, the ghapp client, and `sched.Register("@every 30s", asynq.NewTask(TypeEnvSweep, nil), asynq.Unique(25*time.Second), asynq.MaxRetry(0))`. Add `extra_hosts: ["host.docker.internal:host-gateway"]` plus `DATABASE_URL`, App settings, `PREVIEW_GATEWAY_ADDR` to the worker service in `deploy/compose.yaml` (currently worker env l.~154-165 has only OTEL, APP_ENV, REDIS_ADDR, HEARTBEAT_INTERVAL and `depends_on` redis+collector; add `postgres: condition: service_healthy`). `cmd/worker/BUILD.bazel` deps list must gain `//internal/store`, `//internal/envs`, `//internal/ghapp` (run gazelle).

---

### 5. GitHub clients (`internal/ghapp`) and the fake GitHub extension

#### `internal/ghapp/`
**Analog:** `internal/auth/oauth.go` l.28-47.
```go
func newHTTPClient() *http.Client {
	return &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
		Timeout:   15 * time.Second,
	}
}
func newGitHubClient(hc *http.Client, apiURL, tok string) (*github.Client, error) {
	if !strings.HasSuffix(apiURL, "/") { apiURL += "/" }
	return github.NewClient(github.WithHTTPClient(hc), github.WithAuthToken(tok), github.WithURLs(&apiURL, nil))
}
```
Copy: `otelhttp.NewTransport` for client spans (this is what makes "GitHub client spans" appear in the OBS-02 trace), trailing-slash normalization of `cfg.GitHubAPIURL` (so the fake works), `github.WithURLs`. For App auth the token is not static: use a `RoundTripper` that injects `Authorization: Bearer <installation token>` from a cache (research Pattern 2), signing the RS256 app JWT with `golang-jwt/jwt/v5`. Keep the token only in the transport (existing comment: "token is held only by the returned client's transport and is never stored").
- `statusOf(resp, err)` (allowlist.go:76-86) is the established way to get an HTTP status from go-github errors (`*github.ErrorResponse`); reuse (export or copy) for ref/PR lookups and 404 vs 403 mapping.
- **Interface seam** for testability: `auth.MembershipClient` (allowlist.go:25-28) is a 2-method interface with a `GitHubMembershipClient` go-github impl. Define `envs` dependencies the same way (`RefResolver`, `ImageChecker`, `Dispatcher`, `RunReader`, `TeamChecker`) so `internal/envs` unit tests use hand-written fakes and the integration tests use real clients pointed at `fakegithub`.
- `GITHUB_API_URL` already exists for the fake (`deploy/compose.e2e.yaml` sets `GITHUB_API_URL=http://fakegithub:9999/`). GHCR needs its own base URL config (`GHCR_BASE_URL`, default `https://ghcr.io`) so the fake can stand in for `GET /token` + `HEAD /v2/...`.
- Never log or span-attribute tokens, PEM, or private key path contents (redaction gate, section 10).

#### `internal/fakegithub/server.go` (extend)
**Analog:** itself. Everything is stdlib `net/http` `ServeMux` (Go 1.22 method+path patterns) registered in `New()`:
```go
// server.go:80-84
s.mux.HandleFunc("GET /login/oauth/authorize", s.authorize)
s.mux.HandleFunc("POST /login/oauth/access_token", s.accessToken)
s.mux.HandleFunc("GET /user", s.user)
s.mux.HandleFunc("GET /user/memberships/orgs/{org}", s.orgMembership)
s.mux.HandleFunc("GET /orgs/{org}/teams/{slug}/memberships/{login}", s.teamMembership)
```
Conventions to keep: path values via `r.PathValue("org")`, JSON via `writeJSON(w, status, v)` (l.150-154), 404 via `notFound(w)`, shared state behind `s.mu sync.Mutex`, canonical users in `Fixtures()` (l.43-52) with a `Fixture` struct (l.28-41). Bearer resolution is `caller()` (l.186-196), which **only** accepts `fake-token-<login>`; the existing `teamMembership` handler calls `s.caller` and then looks up `r.PathValue("login")` independent of the caller identity. Phase 2 must make `caller` (or a sibling `appOrUser`) also accept the fake installation token minted by the new `POST /app/installations/{id}/access_tokens` (check the RS256 JWT `iss`/`exp` structurally only, no signature check, no committed key, per RESEARCH). Add new fixtures to `Fixtures()`: a creator-team member and a "removed between two creates" mutable fixture (new control endpoint `POST /__fake/...` guarded the same way `__fake` run control is). New routes (RESEARCH "Fake GitHub surface"): `POST /repos/{o}/{r}/actions/workflows/{file}/dispatches`, `GET /repos/{o}/{r}/actions/runs`, `GET .../runs/{id}` (real `ETag`/`If-None-Match` 304), `GET .../runs/{id}/jobs`, `GET /repos/{o}/{r}/git/ref/heads/{b}`, `GET /repos/{o}/{r}/commits/{ref}`, `GET /repos/{o}/{r}/pulls/{n}`, `GET /token`, `HEAD /v2/{org}/{repo}/manifests/{tag}`, `POST /__fake/runs/{id}/advance`. Branches containing `/` need `{b...}` wildcard (`"GET /repos/{o}/{r}/git/ref/heads/{b...}"`).
- Package doc says "stdlib-only ... test-only: it ships in tests and in deploy/compose.e2e.yaml, never in the base stack (T-01-26)". Keep it stdlib-only; no go-github import in the fake.
- BUILD: `internal/fakegithub/BUILD.bazel` is `go_library srcs=["server.go"]` with **no deps** and `go_test` embedding it. If the fake is split into several files (`actions.go`, `registry.go`), list each in `srcs` (gazelle will do it). `cmd/fakegithub/main.go` reads `FAKEGITHUB_ADDR`; add knobs the same way (`FAKE_RUN_STEP`, `os.Getenv` + default) and document them in `deploy/compose.e2e.yaml`.
- Test style: `server_test.go` uses `httptest.NewServer(New())`, a `nonFollowing()` client, helper funcs taking `t *testing.T` and `t.Helper()`, plain `t.Fatalf` (no testify in this package; check `go_test` deps is empty, so adding testify here means a BUILD dep). The `membership()` helper (l.74-90) authenticates with `Authorization: Bearer fake-token-<login>`: follow it for new endpoint tests.
- Playwright preview-health stand-in (`GET /preview-health` on the gateway address) belongs in the fake but is **only reachable** if compose routes the worker's `PREVIEW_GATEWAY_ADDR` to `fakegithub:9999` in `compose.e2e.yaml`.

---

### 6. Preview auth (`internal/previewauth`)

**Analogs:** `internal/auth/handlers.go` (cookie helpers l.55-70, state compare l.100-110), `internal/auth/next.go` (`SafeNext` redirect sanitizer), `internal/auth/session.go` (cookie flags).

```go
// handlers.go:62-67  cookie construction pattern (HttpOnly, SameSite Lax, Secure from config)
return &http.Cookie{
	Name: oauthCookie, Value: value, Path: "/api/auth/github", MaxAge: maxAge,
	HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: h.cfg.CookieSecure,
}
```
```go
// handlers.go:105  constant-time comparison idiom
subtle.ConstantTimeCompare([]byte(parts[0]), []byte(q.Get("state"))) != 1
```
```go
// session.go:21-33  session cookie is host-only: no Domain set. NEVER set Domain on it (Pitfall 5).
sm.Cookie.Name = CookieName; sm.Cookie.HttpOnly = true
sm.Cookie.SameSite = http.SameSiteLaxMode; sm.Cookie.Secure = cfg.CookieSecure; sm.Cookie.Path = "/"
```
- Preview cookie `ephematool_preview`: same flags, **no `Domain`**, `Path=/`, `MaxAge` from `PREVIEW_COOKIE_TTL`; named constant like `oauthCookie` (handlers.go:23). Use `__Host-` prefix only when `CookieSecure`.
- Redirect target validation: study `internal/auth/next.go` + `next_test.go` (the existing table-driven open-redirect tests) and build `previewauth.SafeReturn` the same way (single leading `/`, no `\`, CR/LF, `//`), test table-driven.
- JWT parse options exactly as RESEARCH "Preview token verification": `WithValidMethods([]string{"HS256"})`, `WithAudience(envID)`, `WithIssuer("ephematool")`, `WithExpirationRequired()`, `WithLeeway(5*time.Second)`; single-use ticket via Redis `SetNX("preview:jti:"+jti, 120s)` using the same `*redis.Client` the API already holds (`Deps.Redis`, api.go:30).
- Ticket mint op `POST /api/preview/ticket` is a Huma op with `Security: session`; CSRF/clickjacking: it gets `http.NewCrossOriginProtection()` for free (api.go:70 wraps the whole handler), so tests must send `Origin` (see `authtest.Env.Post`, env.go:~212, which sets `Origin` to the server URL).
- 404-and-403 return **one body** (UI-SPEC item 7): implement with a single `huma.Error403Forbidden("access denied")` for both unknown env and forbidden, never `404`.
- Tests: pure `httptest` unit tests (no containers) with `miniredis` for the jti store, as `internal/auth/session_test.go` does (imports `github.com/alicebob/miniredis/v2`; the `go_test` deps in `internal/auth/BUILD.bazel` show the exact bazel labels `@com_github_alicebob_miniredis_v2//:miniredis`). Cover: forged alg (`none`, RS256), wrong env audience, expired, replayed jti, open-redirect `ret`, sibling cookie tossing (two same-named cookies), cookie stripped on 200 (response `Cookie` header).

---

### 7. Telemetry attributes use the generated conventions constants

**Analog:** `internal/conventions/conventions.gen.go` (generated), `conventions/telemetry.json`, `scripts/verify.d/conventions.sh`.

```go
// internal/conventions/conventions.gen.go
const (
	LabelEnvID     = "ephematool.io/env-id"
	LabelExpiresAt = "ephematool.io/expires-at"
	LabelManaged   = "ephematool.io/managed"
	LabelOwner     = "ephematool.io/owner"
	LabelTemplate  = "ephematool.io/template"
)
const (
	AttrEnvID     = "ephematool.env.id"
	AttrExpiresAt = "ephematool.env.expires-at"
	AttrOwner     = "ephematool.env.owner"
	AttrTemplate  = "ephematool.env.template"   // managed is label-only (no Attr)
)
```
Usage (nothing in Go uses these yet; this phase is the first consumer): `import "github.com/ephematool/ephematool/internal/conventions"` and `span.SetAttributes(attribute.String(conventions.AttrEnvID, env.ID), attribute.String(conventions.AttrOwner, env.Owner), attribute.String(conventions.AttrTemplate, env.Template))` on the create, dispatch, poll, probe and synthetic spans. TS side: `import { ATTRS, LABELS } from "@/gen/conventions.gen"` (`web/src/gen/conventions.gen.ts`).

**Enforcement (a trap for new files):** `scripts/verify.d/conventions.sh` greps `cmd internal web/src deploy` for the literals `ephematool.io/` and `ephematool.env.` in `*.go *.ts *.tsx *.yaml *.json`, excluding `.gen.`, `conventions/telemetry.json` and `_test.go`. Consequences for Phase 2:
- `deploy/kind/*.yaml` (VAP CEL expressions use `labels['ephematool.io/managed']`) and `deploy/otel/collector.yaml` (`resource.attributes["ephematool.env.id"]` in the filter OTTL) **will fail the gate** if hand-written. Planner must choose one: (a) extend the generator `tools/genconventions/main.go` to also emit a YAML/env snippet and `include` it, (b) have `provision.sh` / a `kind_up` step substitute from `versions.env` generated from the JSON, or (c) extend the gate's exclusion list for specific deploy files with a documented reason. Option (b)/(c) are cheapest; do not silently weaken the gate.
- `templates/`, `environments/` and `tools/` are **not** scanned (only `cmd internal web/src deploy`), but the Helm chart's `OTEL_RESOURCE_ATTRIBUTES` and namespace labels should still receive these keys from the platform (values generated from the Go constants), not be hand-typed in the chart.
- `sizes.yaml`/chart tests under `internal/` or `templates/` that assert literals must be `_test.go`.
- Resource-vs-span: the collector filter keys on the **resource** attribute `ephematool.env.id` (RESEARCH Pattern 10), so the sampleapp (`cmd/sampleapp`) must set it via `OTEL_RESOURCE_ATTRIBUTES` (as `internal/telemetry/telemetry.go:Setup` already merges `resource.Default()` which honors that env var; see its `resource.Merge(resource.Default(), ...)` at l.~37). Do not put env id on Prometheus metric labels (Pitfall 8; `queue` metrics comment shows the rule).

---

### 8. Huma request/response conventions for new ops

**Analog:** `internal/httpapi/status.go` and `internal/auth/handlers.go`.
- Output wrapper: `type XResponse struct{ Body XBody }` (status.go:33-35 `StatusResponse`; handlers.go:186 `MeResponse`). Body fields have explicit `json` tags and `enum:"a,b"` tags (status.go:17-18) so the OpenAPI + generated TS types carry the unions (`"requested"|"provisioning"|"running"|"failed"`, outcome `"success"|"denied"|"rejected"`).
- Empty list responses: initialize to `[]` not `nil` (`nonNil`, handlers.go:195-200, and `services := []ServiceStatus{}` in status.go:~66) so JSON is `[]`.
- Timestamps `time.Time` / `*time.Time` with `omitempty` (status.go:22).
- Registration shape used by new files: `func registerEnvironments(api huma.API, deps Deps)` called from `New` next to `registerStatus(api, deps)` (api.go:63), using either `huma.Get/Post(api, path, handler, func(o *huma.Operation){...})` (status.go:51-62) or `huma.Register`.
- Input structs for path/query: `type itemInput struct{ ID string \`path:"id"\` }` (middleware_test.go:26-28). Request bodies: `struct{ Body CreateEnvironmentRequest }`.
- **Field-level 422 contract** (UI-SPEC): `errors: [{field, code, message}]`. Huma's default validation errors have a different shape (`errors[].location/message/value`). Define a custom error type implementing `huma.StatusError` (`GetStatus() int`, `Error() string`) with `Errors []FieldError{Field, Code, Message}` and return it as `error` from the handler (status 422), plus a flag for "audited". Use the same type for the 403 guardrail response (extra `creatorTeams []string`). Do not rely on Huma's built-in 422 for schema errors because the code vocabulary (`ref_not_found`, `image_not_found`, `schema_violation`, ...) is fixed by the UI-SPEC table.
- `X-Trace-Id` is attached to every response by `TraceIDHeader` (middleware.go:~43-51); the UI reads it in `ApiError.fromResponse`. Nothing to add per op.
- Every op already gets `RouteTagging` (api.go:58 `api.UseMiddleware(RouteTagging)`), so spans are named `METHOD /path` with bounded `http.route`; keep paths templated (`/api/environments/{id}`) so cardinality stays bounded.
- OpenAPI regeneration: `cmd/api` `openapi` subcommand builds `httpapi.New(httpapi.Deps{})` with **all deps nil** (main.go:49-53). New `register*` functions must tolerate nil `deps.Queries`, `deps.Redis`, etc. at registration time (only dereference inside handlers), or `//:check_gen`/`bazel run //:gen` will panic.
- `GET /api/environments/{id}` handler order from RESEARCH "Create handler order": role -> allowlist -> ref -> image -> schema -> tx(insert env + audit success) -> enqueue; **zero dispatches on any 403/422 path** is an explicit test assertion (use the `Enqueuer` seam: `Deps.Enqueuer` interface at api.go:23-26 lets tests inject a recording fake; extend the interface with `EnqueueEnvDispatch` rather than adding a second field, and update `maybeEnqueuePing`'s nil handling pattern in status.go:~87+).
- Audit insert happens even when the validation fails, so use a **separate** statement/tx from the env insert on denial paths; success path inserts env + audit in one tx (`pgx.Tx` via `pool.Begin`, `queries.WithTx(tx)` which sqlc generates in `db.go`).

---

### 9. Bazel wiring for new Go packages and `//:test_integration`

**Analogs:** `internal/queue/BUILD.bazel`, `internal/httpapi/BUILD.bazel`, `internal/auth/BUILD.bazel`, root `BUILD.bazel`, `tools/bzl/go.bzl`, `.bazelrc`.

Every new package gets a `BUILD.bazel` shaped like:
```python
load("@io_bazel_rules_go//go:def.bzl", "go_library")
load("//tools/bzl:go.bzl", "go_test")          # NOT rules_go's go_test (adds -test.short)

go_library(
    name = "envs",
    srcs = [...],
    importpath = "github.com/ephematool/ephematool/internal/envs",
    visibility = ["//:__subpackages__"],
    deps = [ "//internal/store", "@com_github_hibiken_asynq//:asynq", ... ],
)
go_test(name = "envs_test", srcs = [...], embed = [":envs"], deps = [...])
```
- **Do not hand-write deps.** Run `bazel run //:gazelle` after adding files/imports. The root `BUILD.bazel` carries the directives that make it work: `# gazelle:prefix github.com/ephematool/ephematool`, `# gazelle:go_naming_convention import`, `# gazelle:build_tags integration`, `# gazelle:map_kind go_test go_test //tools/bzl:go.bzl` (so gazelle emits the repo's wrapped `go_test`), and `# gazelle:exclude web|deploy|scripts|.planning|bazel-*`.
- **New external modules** (`jwt/v5`, `jsonschema/v6`, `golang.org/x/text`): `go get ...` then `go mod tidy && bazel mod tidy && bazel run //:gazelle` (README/CLAUDE "Established Patterns"). External labels follow `@com_github_<org>_<repo>[_vN]//:<pkg>` e.g. `@com_github_golang_jwt_jwt_v5//:jwt`, `@com_github_santhosh_tekuri_jsonschema_v6//:jsonschema` (verify with `bazel query` after tidy; existing examples: `@com_github_google_go_github_v92//github`, `@com_github_redis_go_redis_v9//:go-redis`).
- Each new module requires the project's human-verify checkpoint before `go get` (RESEARCH audit).
- **Integration tests** are build-tagged `//go:build integration` (first line of the file, e.g. `internal/auth/oauth_integration_test.go:1`, `authtest/env.go:1`). The `go_test` for a package whose `*_integration_test.go` needs containers goes into the suite:
```python
# BUILD.bazel (root) lines 31-37
test_suite(
    name = "test_integration",
    tests = [
        "//internal/auth:auth_test",
        # Phase 2: add //internal/envs:envs_test, //internal/previewauth:previewauth_test,
        # //internal/audit:audit_test, //internal/ghapp:ghapp_test, //internal/store:store_test
    ],
)
```
  A package with only unit tests needs no entry (they run in `bazel test //...` which `tools/verify.sh` step 2 already runs). Under `--config=integration` (`.bazelrc:25-36`) the tag is set, `-test.short` is dropped (`go.bzl:11-17`), tests run `local` with Docker env passed through; `t.Skipf("containers unavailable...")` is the convention when Docker is absent (`authtest/env.go:~118`). Unit tests that must run in the short/sandbox mode should not need Docker.
- `go_test(embed = [":pkg"])` (white-box) is the convention (`queue_test`, `httpapi_test`, `auth_test`); black-box helpers live in a sub-package like `internal/auth/authtest` with its own `BUILD.bazel` (copy for an `internal/envs/envstest` package if the harness is shared).
- New `cmd/sampleapp`: copy `cmd/worker/BUILD.bazel` (`go_library "sampleapp_lib"` with `visibility = ["//visibility:private"]` + `go_binary` embedding it). The Dockerfile builds any `./cmd/${CMD}` generically (`Dockerfile:12`, `ARG CMD`), so the sampleapp image is `docker build --build-arg CMD=sampleapp --target go-runtime`; `kind load docker-image` under the expected `ghcr.io/<org>/<repo>:<sha>` name for offline verification.
- **go:embed for `templates/`**: `templates/embed.go` with `//go:embed all:web-app` (or a glob), and in `templates/BUILD.bazel` `embedsrcs = glob(["web-app/**"])` (see `internal/store/BUILD.bazel:12` for the explicit `embedsrcs` pattern; chart `templates/` subdir contains files whose names start with `_` or `.`, so use `all:` or list them). `sizes.yaml` shared by the API and the environments repo: put it in `templates/` (or a small `sizes` package) and `exports_files` it, rather than embedding across package boundaries (go:embed cannot reference `../`).
- **Codegen packages**: new generated outputs belong to an existing `write_source_files` (store, api, web). Do not add a new `_GEN_PACKAGES` entry unless a new generator is introduced.
- `tools/bzl/BUILD.bazel` defines the `//tools/bzl:integration` config_setting; nothing to add.

---

### 10. `tools/kind.sh`, root sh_binary targets, `verify.d` subcommands

#### Root `BUILD.bazel` + `tools/kind.sh`
**Analog:** `tools/stack.sh` and `BUILD.bazel:66-98`.
```python
[
    sh_binary(
        name = name,
        srcs = ["tools/stack.sh"],
        args = [cmd],
        tags = ["local", "manual", "requires-docker"],
    )
    for name, cmd in [("up", "up"), ("up_fake", "up-fake"), ("down", "down"), ...]
]
```
Add the identical comprehension for `tools/kind.sh`: `kind_up`, `kind_down`, `kind_verify`, `runner` (args `up|down|verify|runner`), tags `local`, `manual`, `requires-docker`. `load("@rules_shell//shell:sh_binary.bzl", "sh_binary")` is already at the top of the root file. `tools/kind.sh` must copy `stack.sh` l.14-26 verbatim: `set -euo pipefail`, `ROOT` from `BUILD_WORKSPACE_DIRECTORY` (set by `bazel run`) else relative path, `cd "$ROOT"`, `case "$cmd" in ... *) echo "unknown command" >&2; exit 2`, usage line when `$# -lt 1`. **Never call `bazel` inside** (documented in both `stack.sh` and `verify.sh` headers: nested bazel deadlocks on the output-base lock). Version-check kind (`kind version` must equal the pin in `deploy/kind/versions.env`; machine has v0.29.0) and print the install hint on mismatch. `tools/verify.sh` is a plain script, not a bazel target; add the kind step as **optional** (`[ -n "${VERIFY_KIND:-}" ] && step "$BAZEL" run //:kind_verify`) using its `step()` helper (verify.sh:22-26).

#### verify.d subcommands
**Analogs:** `scripts/verify-telemetry.sh` (dispatcher), `scripts/lib/verify-lib.sh`, `scripts/verify.d/trace.sh`, `worker.sh`, `redaction.sh`.
- **Registration is implicit by file presence**: the dispatcher does `for f in "$DIR"/*.sh` and `all` runs every file alphabetically except names starting `fresh` (verify-telemetry.sh:19-25). Dropping `scripts/verify.d/env-trace.sh` and `env-telemetry.sh` registers `bazel run //:verify_telemetry -- env-trace` and includes them in `all`. Consequence: **`all` runs against the plain `up_fake` stack**, so these scripts must either (a) work with only fakegithub + worker + a scripted run (env-trace: create env via API as `octo-admin`, advance the fake run, poll `GET /api/environments/{id}`, then `tempo_services` on the trace id and assert `ephematool-api` + `ephematool-worker` plus the synthetic `github.actions.run` span), or (b) `exit 0` with a clear `echo SKIP: ...` when a prerequisite (kind/sampleapp) is absent. Prefix with `kind-` and `continue` in the dispatcher (like the `fresh*` skip at l.22) for any script that needs the real kind cluster.
- Script skeleton (copy `trace.sh` l.1-4):
```bash
#!/usr/bin/env bash
set -euo pipefail
. "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/lib/verify-lib.sh"
...
pass "..."    # prints PASS: ..., fail "..." prints FAIL: ... to stderr and exits 1
```
- Helpers available from `verify-lib.sh`: `API_URL` (the nginx front door :8080), `ensure_session [login]` (drives fake GitHub login with curl, l.~46-62), `api_get PATH` (sets `$VERIFY_TMP/last-trace-id`, retries once after a 401), `tempo_services TRACE_ID [timeout]`, `loki_count`, `prom_value`, `wait_http`, `header_value FILE NAME`. There is **no `api_post` helper**; add one to `verify-lib.sh` (copy `api_get`, add `-X POST -H 'Origin: $API_URL' -H 'Content-Type: application/json' -d`) because CrossOriginProtection requires an `Origin` matching the host (nginx passes Origin through unchanged, per the comment in default.conf).
- Poll-with-deadline idiom (trace.sh l.23-31, worker.sh): `for _ in $(seq 1 15); do ...; [ cond ] && break; sleep 2; done; [ cond ] || fail "..."`.
- For other personas use `ensure_session octo-viewer` / `LOGIN=octo-viewer`; viewer 403 and removed-creator denial assertions belong in Go tests, not here.
- **Redaction extension** (`redaction.sh:138-150`): the `scan "<label>" "<needle>"` list; add `scan "preview ticket query" "ticket="`, `scan "preview cookie pair" "ephematool_preview="`, `scan "app private key" "BEGIN RSA PRIVATE KEY"`, and a scan for the actual ticket value minted during the flow. Mint a ticket inside the script so the value-scan is meaningful ("The scan is only meaningful if the grep sees real content" sanity check at the end).
- `scripts/verify.d/conventions.sh` also scans new `deploy/` YAML, see section 7.

---

### 11. Collector `*/environments` pipelines

**Analog:** `deploy/otel/collector.yaml` (whole file) and compose `otel-collector` service.

Existing naming scheme to continue: receivers/processors/pipelines suffixed with the slot: `otlp/platform`, `resource/platform`, `traces/platform`; shared processors without suffix (`memory_limiter`, `attributes/redact`, `batch`). The file already reserves the slot:
```yaml
# collector.yaml:64-66
# Phase 2 reserves receivers otlp/environments and pipelines traces|metrics|logs/environments
# with a separate memory_limiter and rate limits (PITFALLS 9). Do not share platform pipelines.
```
Replace with the verified config in RESEARCH Pattern 10 (HTTP-only receiver on `0.0.0.0:4320`, `max_request_body_size`, `memory_limiter/environments` with **lower** `limit_mib: 300` than the platform's 400, `filter/environments` OTTL, `resource/environments`, `batch/environments`, reuse `attributes/redact`, exporters: add `otlp/tempo-env`, `otlphttp/loki-env`, `otlphttp/prometheus-env` as separate named instances with bounded `sending_queue`). Compose change: publish `127.0.0.1:4320:4320` on `otel-collector` (existing publish is `127.0.0.1:13133:13133` only, with the comment "OTLP ports are not published (T-01-03)"; update that comment to state 4320 is the single environments ingest and is never routed through nginx). Add the `otelcol validate` test (`docker run --rm -v ... otel/opentelemetry-collector-contrib:0.162.0 validate --config=...`) as a `scripts/verify.d` or `go_test` with `requires-docker`.

---

### 12. Compose overlays

**Analog:** `deploy/compose.e2e.yaml` (overlay pattern), `deploy/compose.yaml`, `tools/stack.sh` `COMPOSE_FAKE=("${COMPOSE[@]}" -f deploy/compose.e2e.yaml)`.

- Overlays are plain additional `-f` files layered by `stack.sh` (`COMPOSE`, `COMPOSE_FAKE` arrays, l.28-29); `down`/`clean` bring both down. A third overlay `deploy/compose.preview.yaml` needs: a new `COMPOSE_PREVIEW=...` array in `stack.sh`, a new `up-preview` case, and inclusion in `down`/`clean` (`"${COMPOSE_FAKE[@]}" down ... || true` pattern, l.45-52). The comment header style: `# Overlay for dev/e2e ... NOT for real deployments (T-01-26).`
- The e2e overlay shows the env-override style for the api service (`GITHUB_API_URL=http://fakegithub:9999/`, `GITHUB_AUTHORIZE_URL=http://localhost:9999/...` browser-facing vs server-facing) — mirror for `GHCR_BASE_URL`, `GITHUB_APP_*`, `ALLOWED_REPOS`, `CREATOR_LOGINS`, `PREVIEW_*`, `MIGRATE_DATABASE_URL`. `ADMIN_LOGINS=Octo-Admin` (mixed case on purpose; `RoleFor` is case-insensitive) is the existing e2e admin; keep fixtures consistent.
- New Postgres role: compose `postgres` service (compose.yaml:~70-82) keeps `POSTGRES_USER=ephematool` as the **owner**; api gets `MIGRATE_DATABASE_URL=postgres://ephematool:ephematool@postgres:5432/...` and `DATABASE_URL=postgres://ephematool_app:ephematool_app@postgres:5432/...` (dev-only creds, matching the "DEV-ONLY credentials" comments).
- Compose variable style in `compose.yaml` api block: `NAME: ${NAME:-default}` for every setting (l.~100-131). Add new vars the same way and mirror them in `.env.example` with a comment block per group (see `.env.example` "--- GitHub OAuth login ---" style).
- Worker needs `extra_hosts: - "host.docker.internal:host-gateway"` (RESEARCH Pitfall 11).
- Traefik/whoami for `compose.preview.yaml`: no analog in repo for Traefik; follow compose service style (image pinned by tag, `127.0.0.1:` port publishes, `depends_on`).

---

### 13. Web: routes, shell, role badge, pages

#### Router
**Analog:** `web/src/router.tsx` (whole file).
```tsx
const str = (v: unknown): string | undefined => (typeof v === "string" ? v : undefined);
const loginRoute = createRoute({
  getParentRoute: () => rootRoute, path: "/login",
  validateSearch: (s: Record<string, unknown>): { reason?: string; next?: string } => ({ reason: str(s.reason), next: str(s.next) }),
  component: LoginPage,
});
// Pathless layout route: everything inside needs a signed-in user.
const shellRoute = createRoute({ getParentRoute: () => rootRoute, id: "shell", component: AppShell });
const routeTree = rootRoute.addChildren([loginRoute, deniedRoute, shellRoute.addChildren([statusRoute])]);
```
Add: under `shellRoute` -> `/environments`, `/environments/new`, `/environments/$envId`, `/admin/audit` (search params `outcome|action|actor` via `validateSearch` with `str`); under `rootRoute` (outside shell, same as `loginRoute`/`deniedRoute`) -> `/preview/authorize` (search `env`, `return`) and `/preview/denied` (search `env`). Signed-out interstitial: redirect to `/login?next=<encoded authorize URL>`; `LoginPage` already carries `next` through to `/api/auth/github/login?next=` (see `SafeNext` server side). Public interstitial pages must not depend on the shell's `useMe` (copy `DeniedPage`: "Public page: it must not depend on a session").

#### `routeTemplate()` in telemetry
`web/src/telemetry/pageLoad.ts:11-22` is a `switch (window.location.pathname)` with a bounded set (`"/"`, `"/login"`, `"/denied"`, `default: "unknown"`) — "Route templates only (bounded cardinality); never the raw pathname". New routes with params (`/environments/<id>`) would all collapse to `"unknown"` or, worse, leak ids if someone adds a default passthrough. Extend with prefix matching: `/environments` -> `"/environments"`, `/environments/new` -> `"/environments/new"`, `pathname.startsWith("/environments/")` -> `"/environments/$envId"`, `/admin/audit`, `/preview/authorize`, `/preview/denied`. Add a test in `web/src/telemetry/telemetry.test.ts`.

#### Shell nav + RoleBadge
**Analog:** `web/src/components/AppShell.tsx:11` and `:54-68`, `RoleBadge.tsx`.
```tsx
const NAV_ITEMS = [{ to: "/", label: "Telemetry" }] as const;
...
<Link to={item.to} className="inline-flex h-14 items-center border-b-2 border-transparent text-sm font-semibold outline-none focus-visible:ring-2 focus-visible:ring-ring aria-[current=page]:border-primary aria-[current=page]:text-primary"
  activeOptions={{ exact: true }}>
```
Change to a typed array with `exact?: boolean` and `adminOnly?: boolean` (UI-SPEC screen 1), pass `activeOptions={{ exact: item.exact ?? false }}`, filter with `me.data?.role === "admin"`. The comment "Rendered from an array so later phases add items without touching the markup" is the design intent; keep markup unchanged. `RoleBadge` (`role: "admin" | "viewer"` -> add `"creator"`; derive the type from the generated schema `components["schemas"]["MeBody"]["role"]` instead of re-declaring).

#### Data fetching
**Analog:** `features/status/StatusPage.tsx:28-44` and `useMe.ts`.
```tsx
const status = useQuery({
  queryKey: ["status"], refetchInterval: 15000, retry: false,
  queryFn: async () => {
    const { data, response } = await api.GET("/api/status");
    if (!response.ok || !data) throw ApiError.fromResponse(response);
    return data;
  },
});
useEffect(() => { if (status.isSuccess || status.isError) pageLoadDone(); }, [status.isSuccess, status.isError]);
```
Rules: always `api.GET/POST` from `@/api/client` (typed via generated `paths`, traced via `tracedFetch`), `retry: false`, throw `ApiError.fromResponse(response)` so `traceId` is available, call `pageLoadDone()` once the first render data/error is in. Detail-page polling (UI-SPEC: 3s while Requested/Provisioning): `refetchInterval: (q) => isTerminal(q.state.data?.status) ? false : 3000`. Mutations (create): `useMutation` or a direct `api.POST` in the submit handler; a 422 response body is typed as the error shape in `schema.d.ts` after `//:gen`, read it from `error` returned by openapi-fetch (`const { data, error, response } = await api.POST(...)`). `handleResponse` middleware (client.ts:57-82) already redirects to `/login?reason=expired` on any 401 except `/api/me`; so 401 on the preview mint call navigates to login automatically — the authorize page must pass its own `next`.

#### Error component / copy trace button
**Analog:** `features/status/StatusError.tsx` (copy logic l.11-24, tooltip markup l.35-52). UI-SPEC says extract `CopyTraceButton` into `web/src/components/CopyTraceButton.tsx` with identical behavior (2s "Trace ID copied", `aria-label="Copy trace ID"`, sr-only `aria-live="polite"` region, `navigator.clipboard.writeText` in try/catch) and have `StatusError` consume it, with `status.test.tsx` kept green (it checks the copy behavior).

#### Badges
**Analog:** `features/status/HealthBadge.tsx` — `STATES: Record<Health, { label; Icon: LucideIcon; tone }>` table, `Badge variant="secondary" className={cn("text-xs font-semibold", tone)} data-health={status}` with `<Icon aria-hidden="true" />`. Copy exactly for `StatusBadge` (env state and audit outcome as two maps, `data-status` attr for tests). Tone classes only `text-foreground | text-destructive | text-muted-foreground` (no accent, no `text-warning`; UI-SPEC). `loader-2` spin uses `motion-safe:animate-spin`.

#### Pages
- Create-form / detail / launch: layout pattern from `StatusPage.tsx` (outer `flex flex-col gap-6`, header row `flex items-center justify-between gap-4` with `<PageHeading>`, `Card role="region" aria-label=...`, `CardHeader/CardTitle className="text-xl leading-[1.2] font-semibold"`, `Skeleton` blocks sized to final content, `Button asChild` + `<a target="_blank" rel="noopener noreferrer"><ExternalLink aria-hidden="true" />`).
- Interstitial/denied: **exact analog `features/auth/DeniedPage.tsx`**: `<main className="min-h-screen bg-background px-4 pt-16"><div className="mx-auto flex w-full max-w-[400px] flex-col gap-4 rounded-xl border bg-card p-8 text-card-foreground"><PageHeading role="display">...` and `Button variant="secondary" className="min-h-11 w-full"`. UI-SPEC uses `outline` variants for the denied buttons; follow the UI-SPEC. Search params via `useSearch({ strict: false }) as { env?: string }`; values from the URL are rendered as React text only, never as `href` (comment in DeniedPage). The SPA does `window.location.replace(url)` only to the URL returned by the mint endpoint, never to the `return` query value.
- `PageHeading` (components/PageHeading.tsx) takes focus on mount; use it for every new route heading and `role="display"` for the auth-style pages.
- Env ids/SHAs/trace ids in mono: `<code className="font-mono">`.
- 44px control height: the Phase 1 `Button` already provides `min-h-11` usage sites; for new `Input/Select/RadioGroup/Switch` pass `className="h-11"` / `min-h-11`.

#### shadcn primitives
`web/components.json`: style `new-york`, `tsx`, aliases `@/components/ui`, `iconLibrary: lucide`, `baseColor: neutral`. Add with the official CLI (`npx shadcn@latest add input label select radio-group switch alert` from `web/`), which writes into `web/src/components/ui/` in the same style as existing `badge.tsx` (`import { Slot } from "radix-ui"`, `cva`, `data-slot` attributes, `cn` from `@/lib/utils`). Radix is the umbrella `radix-ui` package (single dependency), so no new npm packages. After adding, `web/package-lock.json` and `pnpm-lock.yaml` may be untouched; if `radix-ui` already ships these, verify with `bazel test //web:typecheck`. The `web/BUILD.bazel` `srcs` glob picks up `src/**/*.tsx` automatically.

#### Vitest under Bazel (Radix stubs)
**Analogs:** `features/status/status.test.tsx:12-25`, `features/auth/auth.test.tsx:9-100`, `web/vitest.bazel.config.ts`.
Radix components load a second React copy under Bazel's symlinked `node_modules` ("Invalid hook call"), so every unit test that renders Radix-backed primitives must `vi.mock` them with minimal passthroughs. Tooltip stub (copy verbatim):
```tsx
vi.mock("@/components/ui/tooltip", () => ({
  TooltipProvider: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipContent: ({ children }: { children: React.ReactNode }) => <span data-testid="tooltip">{children}</span>,
}));
```
Avatar stub (auth.test.tsx l.40-44) and a context-based always-open dropdown stub (l.50-100) are the models for stubbing stateful Radix pieces. New stubs required in the Phase 2 tests: `@/components/ui/select` (render a native `<select>` or a list of `<option>`-like buttons using a `GroupCtx` like the dropdown stub), `@/components/ui/radio-group` (context + `<input type="radio">`), `@/components/ui/switch` (`<input type="checkbox" role="switch">`), `@/components/ui/avatar`, `@/components/ui/tooltip`. `alert` and `label` are plain elements (Label wraps `@radix-ui/react-label`, stub if "Invalid hook call" appears). The real components are exercised in Playwright, as the comments state. Router mocking pattern: `vi.mock("@tanstack/react-router", () => ({ useSearch: () => nav.search, useNavigate: () => nav.navigate, Navigate: () => null }))` with `vi.hoisted` state (auth.test.tsx l.10-17). API mocking: `vi.mock("@/api/client", async () => { const actual = await vi.importActual(...); return { ...actual, pageLoadDone: vi.fn(), api: { GET: vi.fn(...), POST: vi.fn(...) } }; })` (l.24-35). Always `afterEach(cleanup)` and `vi.restoreAllMocks()`. Test files named `*.test.tsx` next to the component; `web/BUILD.bazel` globs `src/**/*.test.ts(x)` automatically for `//web:test` and `typecheck`.
Pure functions (trimmed-form-schema builder, `relativeTime`, short ID formatting) get plain `.test.ts` without DOM (see `relativeTime` tests, status.test.tsx:40-52).

#### Playwright e2e
**Analog:** `web/e2e/auth.spec.ts`, `helpers.ts`, `playwright.config.ts` (`baseURL: http://localhost:8080`, chromium only, `retries: 0`, runs against `bazel run //:up_fake`). Use `loginAs(page, "octo-admin")` from `helpers.ts`; add `loginAs`-style helpers for creator/viewer fixtures. Queries by role/name: `page.getByRole("button", { name: ... })`, `expect(heading).toBeFocused()` for PageHeading focus. The preview flow crosses origins (`pv-x.preview.localhost:8088`), so the preview spec needs `compose.preview.yaml` up as well and must not reuse the same `loginAs` page cookie jar assumptions: assert the session cookie is absent from requests to the preview origin via `page.on("request")`.

---

### 14. `internal/envs` service and tests (no exact analog; closest patterns)

- Dependency seams as interfaces: `auth.MembershipClient`/`Allowlist` (allowlist.go:25-28) and `httpapi.Enqueuer` (api.go:23-26) are the repo's established "small interface + real impl + test fake" style.
- Fail-closed evaluation style to copy for allowlist/creator checks: `Allowlist.Evaluate` (allowlist.go:45-70): loop, accumulate `GrantedBy`, deny on every error/pending/403/empty list, log denial with `slog.WarnContext(ctx, "auth.org_check_forbidden", ...)`. Reuse the structured log message naming (`dotted.snake`: `auth.login`, `auth.denied`, `status.served`); new events: `env.create.denied`, `env.create.rejected`, `env.dispatch.ambiguous`. Integration tests can then assert logs with `authtest.LogCapture.Find(msg)` (env.go:~58-85; a `slog.Handler` capturing records).
- State machine CAS = sqlc `:execrows` (section 3). Handlers return `nil` when 0 rows affected.
- Metrics: copy the `ephematool/auth` counter pattern (`handlers.go:44-46`, `h.count`): `otel.Meter("ephematool/envs").Int64Counter("ephematool.envs.created", ...)` with attributes limited to `outcome` (bounded). Never env id on metrics.
- Integration test harness: extend/copy `internal/auth/authtest/env.go` (singleton testcontainers via `sync.Once` `startContainers`, postgres `postgres:18.6-alpine`, redis `redis:8.8-alpine`, `NewEnv(t, mutate ...func(*config.Config))`, cookie-jar client that does not follow redirects, `slog` capture, `fakegithub.New()` on `httptest.NewServer`). For Phase 2 either extend `authtest.Env` (add worker handlers + `fakegithub` run control + owner/app two-role pools) or create `internal/envs/envstest` reusing `shared`; if you extend `authtest`, the `authtest` BUILD deps grow and `//internal/auth:auth_test` stays the integration target. `ephematool_app` role: tests that assert UPDATE/DELETE/TRUNCATE denial need a pool built from `ephematool_app` credentials (`pgx` conn string with the app user).
- Schema validation field-error mapping: use the verified snippet in RESEARCH "Field errors from the schema" (`message.NewPrinter(language.English)` non-nil, walk `Causes`, `InstanceLocation` -> `values.<dotted>`); table-driven unit tests, no containers.

---

## Shared Patterns

### Tracing: HTTP inbound, outbound, DB, Redis
- Inbound: `otelhttp.NewHandler(TraceIDHeader(inner), "ephematool-api")` in `httpapi.New` (api.go:69). Outbound: `otelhttp.NewTransport(http.DefaultTransport)` (oauth.go:~37). Postgres: `store.Open` installs `otelpgx.NewTracer()` (store.go:~32). Redis: `redisotel.InstrumentTracing(rdb, redisotel.WithDBStatement(false))` is **mandatory** on every Redis client that touches session/jti data (cmd/api/main.go:100-107; comment: "scs session keys embed the session token"). Preview `jti` keys and tickets would leak via `db.statement` otherwise; any new Redis client must copy this.
- asynq's own Redis connection is deliberately uninstrumented (Pitfall 4).
- Creating child spans: `otel.Tracer("ephematool/<pkg>").Start(ctx, "name", trace.WithSpanKind(...), trace.WithAttributes(...))`, `defer span.End()`, `span.RecordError(err); span.SetStatus(codes.Error, err.Error())` (client.go:30-45).
- Synthetic spans (D-26): `tracer.Start(ctx, "github.actions.run", trace.WithTimestamp(createdAt))` and `span.End(trace.WithTimestamp(updatedAt))` under a context built from the row's stored `traceparent`.

### Errors and logging
- `slog.InfoContext/WarnContext(ctx, "dotted.event", "key", value)` everywhere (ctx carries trace correlation via the otelslog bridge, telemetry.go imports `otelslog`). Never log tokens/cookies/tickets/PEM.
- HTTP errors from Huma handlers: `huma.Error403Forbidden(...)`, `huma.Error500InternalServerError("could not end session")` (handlers.go:262). 401 is written by the middleware, not by handlers.
- Redirect-style plain handlers fail with a redirect + log, not a body (`h.fail`, handlers.go:56-60).

### Secrets and cookies
- Secrets from mounted files (`*_FILE` env pointing to a path), never env values or logs. `.gitignore` the provisioner kubeconfig path.
- Cookie flags: HttpOnly, SameSite Lax, `Secure: cfg.CookieSecure`, no `Domain` (handlers.go:62-67, session.go:21-33).

### Testing conventions
- Go: stdlib `testing` + testify `assert`/`require` in `queue`, `httpapi`, `conventions`; plain `t.Fatalf` in `fakegithub`/`auth`. Unit tests are always short-mode safe; Docker tests carry `//go:build integration` and skip when containers are unavailable.
- Web: Vitest + Testing Library, Radix stubs (section 13), Playwright for real Radix behavior.
- Shell verification: `pass`/`fail` helpers, polling loops with deadlines.

### Codegen drift
- `bazel run //:gen` regenerates: `api/openapi.json` (from `cmd/api openapi`), `web/src/api/schema.d.ts` (openapi-typescript), sqlc output, conventions Go+TS. `bazel test //:check_gen` (inside `bazel test //...`) fails on drift. After touching any Huma operation, store query/migration, or `conventions/telemetry.json`, run `//:gen` and commit all outputs.
- After Go import changes: `bazel run //:gazelle`; after `go get`: `bazel mod tidy`.

---

## No Analog Found

| File | Role | Data Flow | Reason / Closest Guidance |
|---|---|---|---|
| `deploy/kind/cluster.yaml`, bootstrap RBAC, VAPs, `traefik-values.yaml`, `traefik-middlewares.yaml`, `versions.env` | config | declarative | No Kubernetes manifests exist in the repo. Use RESEARCH "Namespace baseline", "Provisioner/deployer RBAC", Pattern 3 middlewares (verified in spike). Conventions gate applies to `deploy/` (section 7). |
| `environments/.github/workflows/create-environment.yml`, `environments/scripts/provision.sh`, `environments/baseline/**`, `environments/sizes.yaml` | workflow/script/Helm | batch | No GitHub Actions workflows or Helm charts in repo (`.github` absent). Use RESEARCH Pattern 5 (input regex validation via `env:`, never `${{ inputs.* }}` in `run:`). Shell style: `tools/stack.sh` (`set -euo pipefail`). |
| `templates/web-app/**` (Chart.yaml, values.yaml, values.schema.json, templates) | Helm chart | transform | No charts yet. Schema draft-07, server compiles with `santhosh-tekuri/jsonschema/v6`. |
| `deploy/compose.preview.yaml` Traefik service | config | request-response | Overlay mechanism has an analog (`compose.e2e.yaml`), Traefik file-provider content does not. |
| `internal/previewauth` JWT signing | service | request-response | No JWT code in repo; `golang-jwt/jwt/v5` is a NEW dependency (needs human-verify checkpoint). Cookie/redirect hygiene analogs exist (section 6). |
| `internal/ghapp` App JWT + installation token cache | service | request-response | No GitHub App code (Phase 1 is OAuth only). Transport/URL normalization analog exists (oauth.go). |
| `internal/envs` jsonschema validator + form-schema trimmer | service | transform | No schema validation anywhere; `santhosh-tekuri/jsonschema/v6` NEW dependency. |
| `cmd/sampleapp` | cmd | streaming | Closest: `cmd/worker/main.go` (telemetry setup + graceful shutdown) and `internal/telemetry.Setup`; it adds an HTTP server (`/health`, header echo) with `ReadHeaderTimeout` like `cmd/api/main.go:134`. |
| Two-role audit DB integration test | test | CRUD | `authtest` provides containers but only a single owner role. |
| `store.Migrate` with goose Provider + session locker (WR-11) | service | batch | Existing `Migrate` uses legacy global `goose.UpContext`; new Provider API has no in-repo example. |
| Dispatch-queue concurrency/spacing | queue | pub-sub | Only a single `default` queue exists (server.go:17-18). |

## Metadata

**Analog search scope:** `internal/**`, `cmd/**`, `web/src/**`, `web/e2e/**`, `deploy/**`, `scripts/**`, `tools/**`, root `BUILD.bazel`, `.bazelrc`, `sqlc.yaml`, `Dockerfile`, `.env.example`
**Files scanned:** ~110 (all non-generated, non-vendored source files in those trees were read or grepped)
**Pattern extraction date:** 2026-10-12
**Notes for the planner:** (1) `conventions.sh` will fail on hand-written `ephematool.io/` / `ephematool.env.` in new `deploy/` YAML; decide the substitution approach up front. (2) OpenAPI generation constructs `httpapi.New(httpapi.Deps{})` with nil deps; new register functions must be nil-safe at registration. (3) `queue.NewMux(rdb)` signature must change without creating a `queue -> envs` import cycle. (4) `//:test_integration` currently lists only `//internal/auth:auth_test`; every new package with `integration`-tagged tests must be added. (5) The audit table cannot be truncated by tests (trigger); isolate by unique actors.
