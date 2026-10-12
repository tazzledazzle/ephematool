---
phase: 01-telemetry-spine-github-login
plan: 08
subsystem: observability
tags: [grafana, dashboards, redaction, fresh-checkout, docs, phase-gate, redis, otel]

requires:
  - phase: 01-telemetry-spine-github-login
    provides: "Grafana datasources and compose (01-01), status page and prober (01-06), auth experience and e2e specs (01-07)"
provides:
  - "Provisioned 'Ephematool platform' Grafana dashboard (7 panels on real metric names)"
  - "Automated Grafana log -> trace -> logs correlation spec (A5 UI half)"
  - "scripts/verify.d/redaction.sh: secrets-in-telemetry regression guard"
  - "scripts/verify.d/up.sh 10-service/120s check; scripts/verify.d/fresh.sh and //:verify_fresh (SC3)"
  - "README Phase 1 run docs, .env.example security comments"
affects: [phase-2]

key-files:
  created:
    - deploy/grafana/provisioning/dashboards/dashboards.yaml
    - deploy/grafana/dashboards/ephematool-platform.json
    - web/e2e/grafana.spec.ts
    - scripts/verify.d/redaction.sh
    - scripts/verify.d/fresh.sh
  modified:
    - scripts/verify.d/up.sh
    - scripts/verify-telemetry.sh
    - BUILD.bazel
    - README.md
    - .env.example
    - deploy/compose.e2e.yaml
    - cmd/api/main.go
    - cmd/worker/main.go

key-decisions:
  - "Redis spans never record command text (redisotel.WithDBStatement(false)); the redaction check guards it."
  - "Redaction scans the traces of its own flow (always fresh) plus a Tempo search limited to traces started after the api container's creation, so stale dev volumes cannot fail it and no clean step is needed."
  - "PING_THROTTLE=0 in the e2e overlay only (global throttle made parallel specs race)."
  - "up.sh tolerates the dashboard row being degraded with a 'telemetry signal' detail when no browser is open (01-06 carryover); Playwright asserts all 10 Healthy with a live browser."

requirements-completed: [OBS-01]
requirements-partial: [AUTH-01 - real GitHub login pending the human check below]

duration: multi-session
completed: 2026-10-12
---

# Phase 1 Plan 08: Phase gate Summary

The platform ships a provisioned Grafana dashboard with proven log/trace correlation, a redaction regression guard (which caught and drove the fix of a real Redis session-token leak), a one-command fresh-clone check, and a fully green `./tools/verify.sh`.

## Accomplishments

- Dashboard `ephematool-platform` (folder Ephematool, file provider, `disableDeletion: true`): request rate, p95 latency, 5xx error ratio, worker tasks, auth outcomes, service up state timeline, page views. Metric names confirmed in Prometheus before committing; the 5xx label is `http_response_status_code`.
- **A5 (UI half): PROVEN, no fallback applied (no FALLBACK-APPLIED).** `grafana.spec.ts`: Explore with the Loki query, expand a row, the `trace_id` derived field exposes a "Tempo" button, the trace view opens with `ephematool/ephematool-api`, and the "Logs for this trace" link (Grafana 13 wording; older builds say "Logs for this span") returns to a Loki query. Stable across repeated runs.
- `redaction`: real login + status + /api/me flow, downloads the 3 traces plus 15 minutes of Loki, and fails on the session cookie value (both sessions), `ephematool_session=`, `ephematool_oauth=`, `Bearer `, `access_token=`, `"access_token"`, `e2e-secret`, and cookie/set-cookie/authorization header attributes; also searches Tempo for `db.statement` and Redis `db.query.text` spans since the api container started.
- `up`: after backends readiness, `/api/status` must show 10 services healthy within 120s of script start.
- `fresh` (`bazel run //:verify_fresh`): refuses a dirty tree, clones with `file://`, stops the main stack via `tools/stack.sh down`, runs `bazel run //:up_fake` in the clone, runs `verify-telemetry up`, and always tears down the clone's stack and deletes the clone. `all` skips `fresh*`.
- README appended (existing 429 lines kept, file committed after a secret scan found only project prose): Run locally, GitHub sign-in setup (read:org, callback URL, org approval, env vars), Fake GitHub fixtures, Verification, Security notes (dev stack only). `.env.example` comments aligned (COOKIE_SECURE, org approval, fake mode).

## Verification (real output)

- `./tools/verify.sh`: exit 0, "verify: all steps passed". Steps: integration `//internal/auth:auth_test` PASSED; `bazel test //...` 16/16 pass (incl. `//web:test`, `//web:typecheck`, check_gen); `bazel run //:up_fake`; `bazel run //:e2e` 15 passed; `bazel run //:verify_telemetry` all subcommands PASS, including `no span records db.statement or db.query.text`, `no cookie, token, authorization header or client secret in 3 traces and 581 log lines`, and `all 10 services healthy in /api/status after 0s`.
- `bazel run //:verify_fresh`: exit 0. `up_fake returned after 19s`, `all 10 services healthy in /api/status after 20s`, total 40s. Afterwards 0 containers and no temp clone remained.
- `go mod tidy -diff`: empty (the otel/metric cosmetic change from 01-03 was already resolved), so no tidy commit was needed.
- Redaction regression proof: temporarily reverting the fix in cmd/api/main.go (`WithDBStatement(true)`) and rebuilding the api made `redaction` FAIL ("session cookie value found in trace-1.json trace-2.json", "callback session cookie value found in trace-3.json"). The revert was restored with `git checkout -- cmd/api/main.go` and was never committed; the check then passed again. Note the Tempo search step did not trip on the revert because the search index lags the by-id lookup, so the value scan is the primary guard and the search is defence in depth.

## Deviations from Plan

**1. [Rule 1 - Bug, security] Redis spans leaked the session token and session blob into Tempo (T-01-46).**
- Found during: Task 2 redaction check (first run failed).
- Issue: redisotel's default `db.statement` recorded full commands such as `get/set scs:session:<token> <serialized session>`: the live session token plus user id, login, role and orgs, on every authenticated request. Not present in Loki.
- Fix (by the coordinator, commit 7d46dae): `redisotel.InstrumentTracing(rdb, redisotel.WithDBStatement(false))` in cmd/api/main.go and cmd/worker/main.go. After the rebuild, 36 new Redis spans had 0 `db.statement`; all session-key-bearing traces predate the fix. The redaction check is the regression guard.
- Residual: Tempo volumes of existing dev stacks still hold pre-fix traces with fake dev tokens; `bazel run //:clean` removes them. The redaction check ignores them by design.

**2. [Rule 1 - Bug] e2e telemetry.spec.ts failed reproducibly under `bazel run //:e2e` (14/15).**
- Root cause: `/api/status` enqueues the worker ping behind a global (not per-user) Redis throttle. With specs running in parallel, another page's status request consumed the window, so the page load under test produced an api+dashboard trace with no worker span (confirmed by fetching the trace: services were only api and dashboard). The assertion was correct and was not loosened.
- Fix: `PING_THROTTLE=0` in `deploy/compose.e2e.yaml` (default stays 10s in compose.yaml). 3 consecutive full runs 15/15, then green in the full gate. Commit 73f0084.

**3. [Rule 1 - Bug] redaction `access_token` pattern.** The plan's bare `access_token` match would trip on the harmless OAuth token endpoint URL path (`/login/oauth/access_token`) in an outbound HTTP span. Narrowed to `access_token=` and `"access_token"` (token-shaped uses). The same reasoning applies to `db.query.text`: Postgres spans hold parameterized SQL with `$1` placeholders, so only `db.statement` and Redis `db.query.text` are rejected.

**4. [Rule 1 - Bug] fresh.sh cleanup ran `bazel run //:down` from the wrong directory** (`$tmp` instead of `$tmp/repo`), leaving the clone's stack running (compose project name is `ephematool` for both). Fixed in the follow-up commit; verified 0 containers and no leftover clone after a run.

**5. Dashboard locator wording and Explore form.** Used the Grafana 13 `panes` URL form, and "Logs for this trace" (not "Logs for this span").

**6. Build hygiene.** No Go imports changed in this plan (the cmd/ fix reused the existing redisotel import), so no gazelle run was needed. `bazel mod tidy` was not required.

## Human verification pending

The only outstanding item: confirm a real GitHub OAuth App login (VALIDATION manual-only, AUTH-01). No OAuth App credentials exist on the build machine, so this was not run. Steps:

1. Create a GitHub OAuth App (https://github.com/settings/developers): Homepage `http://localhost:8080`, callback `http://localhost:8080/api/auth/github/callback`; the app requests the `read:org` scope.
2. If the org restricts third-party OAuth apps, an org owner approves the app (org Settings, Third-party access).
3. Copy `.env.example` to `.env` and set `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET`, `ALLOWED_ORGS=<your org>`, `ADMIN_LOGINS=<your login>`.
4. `bazel run //:down && bazel run //:up`.
5. Open http://localhost:8080, click "Continue with GitHub", authorize. Expect the Telemetry status page showing your login and the Admin badge, all 10 services Healthy.
6. Close and reopen the browser: still signed in.
7. Sign out from the user menu: lands on /login.
8. Sign in as an org MEMBER who is not in `ADMIN_LOGINS`: Viewer badge. Sign in as a NON-member: "Access denied" naming the org.
9. Click "Open in Grafana", find the page-load trace, follow it to its logs.

Result: PENDING (human).

## Commits

- c6734a7 feat(01-08): provision Ephematool platform dashboard and Grafana log/trace correlation e2e
- 7d46dae fix(01-08): stop recording Redis command text in spans (coordinator)
- 73f0084 fix(01-08): disable the status ping throttle in the e2e overlay
- 6891de5 test(01-08): add redaction verify subcommand for secrets in Tempo and Loki
- 87cc526 test(01-08): add 10-service up check within 120s and the fresh-clone verify_fresh target
- 2fd4448 docs(01-08): add Phase 1 run, GitHub sign-in, fake GitHub, verification and security docs
- fix(01-08): run the fresh-clone cleanup down from the clone checkout

## Known Stubs

None. The "Dashboard page views" panel is empty until a browser reports views (data source is wired).

## Threat Flags

None beyond the fixed Redis db.statement leak (T-01-46 mitigated and guarded). T-01-47 and T-01-48 are covered by the README Security notes and .env.example comments; T-01-49 and T-01-50 accepted as planned.

## Self-Check: PASSED
