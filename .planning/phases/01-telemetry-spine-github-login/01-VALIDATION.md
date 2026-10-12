---
phase: 1
slug: telemetry-spine-github-login
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-10-11
---

# Phase 1 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `testing` + testify (integration via testcontainers-go, build tag `integration`, miniredis for fast unit tests); Vitest (SPA unit); Playwright (e2e); bash + curl + jq (stack e2e) |
| **Config file** | none yet — Wave 0 creates `vitest.config.ts`, `playwright.config.ts`, `Makefile` targets |
| **Quick run command** | `go test ./... -short && npm --prefix web test -- --run` |
| **Full suite command** | `make verify` (integration tests + SPA unit tests + `docker compose up --build --wait` + `scripts/verify-telemetry.sh` + Playwright) |
| **Estimated runtime** | quick ~30 seconds; full several minutes |

---

## Sampling Rate

- **After every task commit:** Run `go test ./... -short && npm --prefix web test -- --run`
- **After every plan wave:** Run `go test -tags integration ./...` and `scripts/verify-telemetry.sh` against a running stack
- **Before `/gsd-verify-work`:** `make verify` must be green, plus a human check of a real GitHub login
- **Max feedback latency:** 30 seconds for the quick run

---

## Per-Task Verification Map

Task IDs are assigned by the planner; rows below are keyed by requirement and success criterion and are refined per task after planning.

| Behavior | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|----------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| OAuth login: state mismatch rejected, PKCE sent, allowed member gets session, denied member gets none and an `auth.denied` log, `next` open redirect rejected | AUTH-01 | OAuth CSRF, open redirect | Fail closed on any non-active membership | Go integration (fake GitHub) | `go test -tags integration ./internal/auth/... -run 'TestOAuth'` | ❌ W0 | ⬜ pending |
| Session persists across restart (cookie Max-Age, HttpOnly, SameSite=Lax), TTL slides, 7-day idle and 30-day absolute caps enforced, logout destroys session | AUTH-01 | Session fixation | `RenewToken` on login | Go unit (miniredis) | `go test ./internal/auth/... -run 'TestSession'` | ❌ W0 | ⬜ pending |
| Allowlist: org active/pending/404/403 distinct, team match, admin list case-insensitive | AUTH-01 | Org membership bypass | Deny on 403 and unknown states | Go unit | `go test ./internal/auth/... -run 'TestAllowlist\|TestRole'` | ❌ W0 | ⬜ pending |
| UI: unauthenticated redirects to /login, /denied shows orgs, sign out lands on /login, 401 mid-use redirects | AUTH-01 | — | — | Playwright (fake GitHub) | `npx playwright test e2e/auth.spec.ts` | ❌ W0 | ⬜ pending |
| Trace spanning `ephematool-api` and `ephematool-worker` retrievable from Tempo by trace ID | OBS-01 | — | — | Stack e2e | `scripts/verify-telemetry.sh trace` | ❌ W0 | ⬜ pending |
| Related request and worker metrics present in Prometheus | OBS-01 | — | — | Stack e2e | `scripts/verify-telemetry.sh metrics` | ❌ W0 | ⬜ pending |
| Correlated logs in Loki by trace ID for api and worker | OBS-01 | — | — | Stack e2e | `scripts/verify-telemetry.sh logs` | ❌ W0 | ⬜ pending |
| Browser spans share the API trace (`ephematool-dashboard` + `ephematool-api`) | OBS-01 | — | — | Playwright + script | `npx playwright test e2e/telemetry.spec.ts` then `scripts/verify-telemetry.sh dashboard <traceId>` | ❌ W0 | ⬜ pending |
| `http.route` is the route template, not the raw path | OBS-01 | Cardinality blowup | Bounded labels | Go unit | `go test ./internal/httpapi/... -run TestRouteTagging` | ❌ W0 | ⬜ pending |
| Cookie and Authorization never reach Tempo | OBS-01 | Secret leakage into telemetry | Collector redaction | Stack e2e | `scripts/verify-telemetry.sh redaction` | ❌ W0 | ⬜ pending |
| `/api/status` returns exactly 10 services in fixed groups; stopping Loki flips it to `down` within 2 probe cycles | OBS-01 (D-13) | Info disclosure | Requires a session | Stack e2e + Go unit | `scripts/verify-telemetry.sh status`; `go test ./internal/status/...` | ❌ W0 | ⬜ pending |
| Generated constants and OpenAPI spec are in sync with their sources | OBS-01 (D-07, D-09) | — | — | Generated-file check | `make check-gen` | ❌ W0 | ⬜ pending |
| Fresh checkout starts the whole stack with one command; all 10 services healthy within 120s | OBS-01 (SC3) | — | — | Stack e2e | `scripts/verify-telemetry.sh up` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `deploy/compose.yaml` plus Collector, Tempo, Loki, Prometheus and Grafana configs, with a smoke spike proving one trace, one metric and one log land and correlate (resolves research assumptions A4, A5, A6, A13 and confirms Prometheus metric names)
- [ ] `scripts/verify-telemetry.sh` skeleton with `up`, `trace`, `metrics`, `logs` subcommands
- [ ] `cmd/fakegithub` and `deploy/compose.e2e.yaml` (a fresh checkout has no OAuth App credentials)
- [ ] Go test scaffolding (`internal/auth` fake GitHub helper, miniredis fixtures), `vitest.config.ts`, `playwright.config.ts`
- [ ] `tools/genconventions` and `Makefile` targets (`up`, `gen`, `check-gen`, `test`, `verify`)
- [ ] Framework installs per RESEARCH.md Installation section

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Real GitHub login with a real org | AUTH-01 | Needs a real OAuth App and org membership; not available on a fresh checkout | Create the OAuth App, set the env vars, sign in as an org member and as a non-member; confirm the dashboard and the denied page. Org owners may need to approve the app for `read:org`. |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 30s for the quick run
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
