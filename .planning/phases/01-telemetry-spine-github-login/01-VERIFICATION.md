---
phase: 01-telemetry-spine-github-login
verified: 2026-10-11T00:00:00Z
status: human_needed
score: 3/3 roadmap success criteria verified in code and automation; 1 manual-only item (real GitHub login) pending
overrides_applied: 0
human_verification:
  - test: "Real GitHub OAuth App login (VALIDATION manual-only, 01-08-SUMMARY 'Human verification pending')"
    expected: "Create OAuth App (callback http://localhost:8080/api/auth/github/callback, read:org), set GITHUB_CLIENT_ID/SECRET, ALLOWED_ORGS, ADMIN_LOGINS in .env, run bazel run //:up. Org member signs in and sees status page with login and role badge; admin login shows Admin, other member Viewer; non-member sees the Access denied page naming the org; closing and reopening the browser keeps the session; Sign out lands on /login."
    why_human: "No OAuth App credentials exist; the real GitHub org-membership API (including org third-party app approval for read:org) cannot be exercised against a fake."
  - test: "Open in Grafana from the status page after a real login and follow a page-load trace to its logs"
    expected: "Trace spans ephematool-dashboard, ephematool-api, ephematool-worker; correlated Loki logs reachable from the trace."
    why_human: "Visual UX confirmation in a real browser session; the automated Playwright grafana.spec.ts used the fake-GitHub stack."
---

# Phase 1: Telemetry Spine & GitHub Login Verification Report

**Phase Goal:** An SRE can log in with GitHub to a running dashboard, and the platform's own API, worker, and dashboard emit OpenTelemetry traces, metrics, and logs from first run.
**Status:** human_needed (all automated and code-level checks pass; only real-GitHub login remains)
**Re-verification:** No, initial verification

## Must-Haves

| # | Truth (ROADMAP SC unless noted) | Status | Evidence |
|---|---|---|---|
| SC1 | SRE signs in with GitHub, stays signed in across browser sessions, signs out | VERIFIED in code and fake-GitHub automation; real GitHub UNCERTAIN (human) | `internal/auth/handlers.go`: state in cookie compared with `subtle.ConstantTimeCompare`, PKCE S256 (`oauth2.GenerateVerifier`/`S256ChallengeOption`), org/team allowlist evaluated before any session, `UpsertUser`, `RenewToken` (fixation defense), role via `RoleFor` (D-02), `auth.denied` log + redirect to /denied (D-04). `internal/auth/session.go`: Redis-backed scs, `IdleTimeout` and `Lifetime` set explicitly (D-03, D-14), `Cookie.Persist=true`, HttpOnly, SameSite=Lax. `RequireSession` Huma middleware registered (handlers.go:216), status endpoint requires session. Logout, UserMenu and DeniedPage exist in `web/src`. `go test ./... -short` run by me: all packages ok. Playwright `auth.spec.ts` exists; `web/test-results/.last-run.json` status passed. |
| SC2 | After one page load + API call, trace, related metric, correlated logs for API, worker, dashboard findable in local Grafana | VERIFIED (automation passed; not re-run by me) | `internal/telemetry/telemetry.go` Setup is called in both `cmd/api/main.go:98` and `cmd/worker/main.go:38`; browser `initTelemetry()` runs before render (`web/src/main.tsx:11`) with `web/src/telemetry/{tracing,metrics,logs,pageLoad}.ts`; worker uses `TracingMiddleware` via `mux.Use` (`internal/queue/server.go:23`) for producer to consumer propagation; collector has separate `traces|metrics|logs/platform` pipelines with header redaction; Tempo/Loki/Prometheus/Grafana provisioned with a platform dashboard JSON; `scripts/verify.d/{trace,metrics,logs,dashboard,worker,redaction}.sh` and `web/e2e/{telemetry,grafana}.spec.ts` encode the checks. Gate `./tools/verify.sh` reported exit 0 (given). |
| SC3 | Fresh checkout starts whole stack with one command, telemetry flows with no extra config | VERIFIED (automation passed; not re-run by me) | `bazel run //:up` / `//:up_fake` (Bazel replaced make, intentional); `deploy/compose.yaml` runs otel-collector, tempo 2.10.8 (D-16), loki, prometheus, grafana, postgres, redis, api, web, worker. `scripts/verify.d/fresh.sh` clones to a temp dir and checks 10 services healthy; API starts without GitHub creds (`githubConfigured=false`). Caveat: WR-12 notes `fresh` reuses main stack volumes (see below). |
| D-05..D-08 | Go backend, Redis queue (asynq), Postgres, separate Vite React SPA with OpenAPI-typed client | VERIFIED | `go.mod`, `cmd/{api,worker}`, `internal/queue` (asynq), `internal/store` (sqlc/goose), `api/openapi.json`, `web/src/api/{client.ts,schema.d.ts}`. |
| D-09 | Prefixes defined once and generated | VERIFIED | `conventions/telemetry.json` to `internal/conventions/conventions.gen.go` and `web/src/gen/conventions.gen.ts`; `check_gen` drift test; genconventions has tests. |
| D-10 | Browser traces propagate into API trace | VERIFIED | `tracedFetch`/`startPageLoad` in `pageLoad.ts`, Vitest test, `telemetry.spec.ts`. |
| D-11/D-12/D-13 | LGTM + single collector; Compose; status page | VERIFIED | compose and collector configs; `internal/status/*`, `web/src/features/status/*`. |
| D-01..D-04, D-14, D-15 | Org allowlist, admin list, sliding 7-day/30-day sessions, denied page, OAuth App | VERIFIED in code | handlers.go, allowlist.go, session.go, DeniedPage.tsx. |

**Score:** 3/3 success criteria achieved in code; real-GitHub confirmation outstanding.

## Required Artifacts (spot-checked, not SUMMARY-trusted)

All plan-declared key artifacts exist and are substantive and wired: `internal/telemetry/telemetry.go`, `internal/httpapi/{middleware,status}.go`, `internal/queue/middleware.go`, `internal/auth/*`, `internal/store/migrations/00001_users.sql`, `internal/fakegithub/server.go`, `deploy/{compose.yaml,compose.e2e.yaml,otel/collector.yaml,nginx/default.conf,grafana/dashboards/ephematool-platform.json}`, `web/src/telemetry/*`, `web/src/features/{auth,status}/*`, `scripts/verify.d/*.sh`. No TBD/FIXME/XXX debt markers in phase source (only a mktemp `XXXXXX` template in fresh.sh, a false positive).

## Requirement Coverage

| Requirement | Plans claiming | REQUIREMENTS.md | Status |
|---|---|---|---|
| OBS-01 | 01-01, 01-02, 01-03, 01-05, 01-06, 01-08 | Checked, mapped Phase 1 | SATISFIED (automated evidence) |
| AUTH-01 | 01-04, 01-05, 01-07, 01-08 | Checked, mapped Phase 1 | SATISFIED against fake GitHub; real-GitHub confirmation pending (human) |

No orphaned requirements: REQUIREMENTS.md maps only OBS-01 and AUTH-01 to Phase 1, and both appear in plan frontmatter.

## Review Warnings Weighed Against Success Criteria

No warning in 01-REVIEW.md (12) or 01-REVIEW-WEB.md (9) invalidates a success criterion. Items worth the user's attention when triaging:

- Backend WR-01 (membership/role not re-validated for up to 30 days): a hardening gap for D-01/D-02 over time, does not break SC1.
- Backend WR-02 (RequireSession opt-in per operation): currently applied; risk is future routes. Phase 2 concern.
- Backend WR-03 (COOKIE_SECURE defaults false): fine for the local-only Phase 1 stack, must be addressed before any TLS deployment.
- Backend WR-04 (unauthenticated browser OTLP ingest shares platform pipelines): a design tension with the Phase 2 note about separate pipelines; not a SC failure.
- Backend WR-12 (`fresh` check reuses volumes, so it does not fully prove a clean start): weakens the automated evidence for SC3 slightly. The Compose file has no required config, so the claim is plausible, but a truly clean-volume run is only partially evidenced. Recommend a manual `docker compose down -v && bazel run //:up` on a clean clone as a cheap confirmation.
- Web WR-02/WR-05/WR-04 (page-load span skew, possible redirect loop, no error boundary): edge cases, not SC failures.

## Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|---|---|---|---|
| Go unit tests | `go test ./... -short` | all packages ok | PASS |
| Full gate | `./tools/verify.sh` | exit 0 (reported by orchestrator; not re-run, needs Docker/Bazel stack) | PASS (reported) |
| Playwright last run | `web/test-results/.last-run.json` | status passed | PASS |

## Gaps Summary

No gaps. The status is human_needed solely because the real GitHub OAuth login (the literal wording of AUTH-01 and SC1) can only be exercised by a human with an OAuth App; the fake-GitHub path proves the flow logic, PKCE/state handling, allowlist, session behavior, and denial handling.

---

_Verified: 2026-10-11_
_Verifier: Claude (gsd-verifier)_
