---
phase: 01-telemetry-spine-github-login
plan: 04
subsystem: auth
tags: [go, oauth2, pkce, github, go-github, scs, sessions, postgres, pgx, goose, sqlc, fakegithub]

requires:
  - phase: 01-telemetry-spine-github-login
    provides: "config.Config, httpapi.New/RouteHandler/TraceIDHeader, Compose stack (01-01)"
provides:
  - fakegithub library, binary and deploy/compose.e2e.yaml overlay (fixture users, PKCE-verifying token endpoint)
  - internal/auth: allowlist, roles, SafeNext, OAuth+PKCE handlers, scs Redis sessions, RequireSession
  - internal/store: pgx pool with otelpgx, embedded goose migrations, sqlc users queries
  - /api/me, /api/auth/config, /api/auth/logout and plain /api/auth/github/{login,callback}
  - authtest integration environment (testcontainers Postgres 18 + Redis 8 + fake GitHub)
affects: [01-05, 01-06, 01-07, phase-2]

tech-stack:
  added: [oauth2, go-github/v92, scs/v2, scs/goredisstore, pgx/v5, otelpgx, goose/v3, sqlc (tool), testcontainers-go]
  patterns:
    - Fail-closed membership evaluation behind a MembershipClient interface
    - Plain OAuth routes tagged via an injected RouteWrapper (avoids httpapi<->auth import cycle)
    - Handler chain CrossOriginProtection -> otelhttp -> TraceIDHeader -> LoadAndSave -> mux
    - Integration env shares containers per test process and resets Postgres/Redis per test

key-files:
  created:
    - internal/fakegithub/server.go
    - internal/fakegithub/server_test.go
    - cmd/fakegithub/main.go
    - deploy/compose.e2e.yaml
    - internal/auth/authtest/env.go
    - internal/auth/oauth_integration_test.go
    - internal/auth/allowlist.go
    - internal/auth/allowlist_test.go
    - internal/auth/role.go
    - internal/auth/next.go
    - internal/auth/next_test.go
    - internal/auth/oauth.go
    - internal/auth/session.go
    - internal/auth/handlers.go
    - internal/auth/middleware.go
    - internal/store/store.go
    - internal/store/generate.go
    - internal/store/migrations/00001_users.sql
    - internal/store/queries/users.sql
    - internal/store/db.go
    - internal/store/models.go
    - internal/store/users.sql.go
    - sqlc.yaml
  modified:
    - internal/httpapi/api.go
    - cmd/api/main.go
    - api/openapi.json
    - web/src/api/schema.d.ts
    - go.mod
    - go.sum

key-decisions:
  - "auth.Register takes a RouteWrapper parameter; httpapi passes RouteHandler (import cycle avoidance, orchestrator-approved)"
  - "oauth cookie value is state|verifier|base64url(next) so arbitrary next paths survive cookie sanitization"
  - "httpapi.New builds auth.Handlers itself when Deps.Auth is nil, so openapi generation and existing httpapi tests need no sessions"
  - "GET /api/me reads the persisted user row (login, avatar) and the role/orgs from the session"

patterns-established:
  - "Access token is a local variable in the callback only; grep gate over slog/SetAttributes passes"
  - "Denied logins redirect to /denied?reason=org_membership&login=... and log auth.denied (D-04)"

requirements-completed: [AUTH-01]

duration: 45min
completed: 2026-10-11
---

# Phase 1 Plan 04: GitHub OAuth login, sessions and users store Summary

**GitHub OAuth (state + PKCE S256, read:org) with a fail-closed org/team allowlist, Redis-backed scs sessions, the first Postgres users table, and a fake GitHub for credential-free tests and e2e.**

## Performance

- **Tasks:** 3 (all autonomous, TDD RED then GREEN)
- **Verification:** `go test -tags integration ./internal/auth/...` passes (allowed member, viewer, team member, denied x3, state mismatch, PKCE, open redirect, cancelled, not configured, logout); `go test ./... -short` passes; `make check-gen` clean; e2e stack up with `/api/auth/config` githubConfigured=true and `/api/me` 401 without a cookie. Stack stopped afterwards.

## Accomplishments

- Fake GitHub with fixtures octo-admin/viewer/teamonly/pending/outsider/forbidden, S256 verifier check, `LastAuthorize()`; Compose overlay on 127.0.0.1:9999.
- Allowlist denies on pending, 404, 403 (also logs `auth.org_check_forbidden`), errors and empty config; admin role by case-insensitive `ADMIN_LOGINS`.
- Session cookie `ephematool_session` HttpOnly, SameSite=Lax, persistent, idle 168h and absolute 720h set explicitly; `RenewToken` before identity is stored.
- API starts without GitHub credentials; login then redirects to `/login?reason=failed`.

## Task Commits

1. Task 1 (RED): `0f15a05` test(01-04): fake GitHub, e2e overlay, failing OAuth integration tests
2. Task 2: `b715190` feat(01-04): allowlist, roles, safe redirect, users store
3. Task 3 (GREEN): `87c730e` feat(01-04): OAuth + PKCE login, sessions, /api/me, logout

## Deviations from Plan

### Auto-fixed / orchestrator-approved

**1. [Design - approved] auth.Register takes a RouteWrapper instead of calling httpapi.RouteHandler**
- **Issue:** httpapi imports auth, so auth importing httpapi is a cycle.
- **Fix:** `auth.Register(api, mux, h, wrap RouteWrapper)`; httpapi passes `RouteHandler`.
- **Commit:** 87c730e

**2. [Rule 3 - Blocking] Missing go.sum entry for go-querystring**
- **Found during:** Task 2
- **Issue:** go-github/v92 transitively requires `github.com/google/go-querystring v1.2.0`, which was absent from go.mod/go.sum.
- **Fix:** `go get github.com/google/go-github/v92/github@v92.0.0` (the command Go suggested; fetches only the approved module's own requirement). Not separately audited in 01-RESEARCH.md; flagging for visibility.
- **Commit:** b715190

**3. [Rule 1 - Test bug] TestOAuthPKCESent never reached the fake**
- **Issue:** The test only called the login endpoint, so the fake had no recorded authorize query.
- **Fix:** Drive `Authorize` as well before reading `LastAuthorize()`.
- **Commit:** 87c730e

**4. [Env] go.mod/go.sum tidy removed miniredis**
- `go mod tidy` (run after code imported the modules) dropped `github.com/alicebob/miniredis/v2 v2.40.0` because nothing imports it yet. Plan 01-07 (session expiry tests) will need `go get github.com/alicebob/miniredis/v2@v2.40.0` again; that version was previously approved.

**5. [Detail] compose.e2e.yaml api environment uses list syntax** so the acceptance greps (`ALLOWED_ORGS=ephematool-e2e`) match; Compose merges it with the base map form.

## Known Stubs

None. Sliding renewal (`last_touch` touch) is intentionally deferred to plan 01-07 as stated in the plan.

## Threat Flags

None beyond the plan's threat model. The oauth state cookie is scoped to `/api/auth/github`.

## Self-Check: PASSED

All created files exist and commits 0f15a05, b715190, 87c730e are in history.
