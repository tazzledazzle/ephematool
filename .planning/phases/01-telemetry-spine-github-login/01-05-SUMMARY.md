---
phase: 01-telemetry-spine-github-login
plan: 05
subsystem: ui
tags: [walking-skeleton, react, tanstack-router, tanstack-query, openapi-fetch, playwright, compose, nginx, otel-web]

requires:
  - phase: 01-telemetry-spine-github-login
    provides: "browser telemetry + tracedFetch (01-02), LGTM stack + worker (01-03), GitHub login, sessions, fake GitHub, /api/me (01-04), /api/status (01-01)"
provides:
  - "Typed openapi-fetch client with tracedFetch, ApiError(traceId), 401 -> /login?reason=expired"
  - "SPA routes /login, /denied and an authenticated shell at / (TanStack Router)"
  - "Login page, app shell with identity, role badge and Sign out, initial Telemetry status page"
  - "Compose web service (nginx + SPA) on 127.0.0.1:8080; /api/status requires a session"
  - "Playwright auth and cross-service trace specs; authenticated verify helpers; verify.d/dashboard.sh"
affects: [01-06, 01-07, 01-08]

tech-stack:
  added: []
  patterns:
    - "Page-load span context held in client.ts and cleared by pageLoadDone() when the first screen settles"
    - "Shell mounts the Outlet only after /api/me succeeds, so anonymous visits never fire a status 401"
    - "verify scripts authenticate through the fake GitHub with a curl cookie jar (ensure_session)"

key-files:
  created:
    - web/src/api/client.ts
    - web/src/api/client.test.ts
    - web/src/router.tsx
    - web/src/features/auth/useMe.ts
    - web/src/features/auth/LoginPage.tsx
    - web/src/features/auth/DeniedPage.tsx
    - web/src/features/status/StatusPage.tsx
    - web/src/components/AppShell.tsx
    - web/src/components/RoleBadge.tsx
    - web/src/components/PageHeading.tsx
    - web/e2e/helpers.ts
    - web/e2e/auth.spec.ts
    - web/e2e/telemetry.spec.ts
    - scripts/verify.d/dashboard.sh
  modified:
    - web/src/main.tsx
    - web/src/App.tsx
    - scripts/lib/verify-lib.sh
    - scripts/verify.d/up.sh
    - deploy/compose.yaml
    - deploy/nginx/default.conf
    - internal/httpapi/status.go
    - api/openapi.json
    - .planning/phases/01-telemetry-spine-github-login/SKELETON.md

key-decisions:
  - "Status page only mounts after /api/me succeeds, so an anonymous visit redirects to /login without the expired-session banner"
  - "401 from /api/me is not counted as ui.api_error (expected anonymous signal); every other non-2xx is"
  - "The 401-to-login redirect goes through an exported session.expire() seam so the unit test can spy on it (jsdom location is not stubbable)"

patterns-established:
  - "pageLoadDone() ends ui.page_load: StatusPage after first status result, LoginPage after /api/me settles, DeniedPage after config settles"
  - "Playwright must run with cwd=web or -c web/playwright.config.ts; bazel run //:e2e is the canonical runner"

requirements-completed: [AUTH-01, OBS-01]

duration: 45min
completed: 2026-10-12
---

# Phase 1 Plan 05: Walking skeleton end to end Summary

**An SRE runs `bazel run //:up_fake`, signs in through the dashboard with fake GitHub, lands on the Telemetry status page, and that page load is one Tempo trace across ephematool-dashboard, ephematool-api and ephematool-worker, with dashboard metrics in Prometheus and logs in Loki.**

## Performance

- **Tasks:** 3 (Task 2 as RED/GREEN)
- **Files:** 14 created, 9 modified

## Accomplishments

- Executable skeleton contract: 4 Playwright tests (3 auth, 1 trace) pass against the live stack; `bazel test //...` passes 15/15.
- Typed traced client: requests run inside the `ui.page_load` context, so the first `/api/status` carries a `traceparent` whose trace id resolves in Tempo to all three services.
- `GET /api/status` now declares the `session` security scheme: unauthenticated `curl http://127.0.0.1:8080/api/status` returns 401.
- `web` joins the one-command stack (nginx + SPA on 127.0.0.1:8080, same-origin front door for `/api` and `/otlp`).

## Evidence

- Trace id from the passing telemetry spec: `56aec9b71fe541bbd6fe24666fba7aa4` (contains ephematool-dashboard, ephematool-api, ephematool-worker, asserted by the spec).
- Grafana Explore URL (stack must be up):
  `http://localhost:3000/explore?schemaVersion=1&orgId=1&panes=%7B%22a%22%3A%7B%22datasource%22%3A%22tempo%22%2C%22queries%22%3A%5B%7B%22refId%22%3A%22A%22%2C%22datasource%22%3A%7B%22type%22%3A%22tempo%22%2C%22uid%22%3A%22tempo%22%7D%2C%22queryType%22%3A%22traceql%22%2C%22query%22%3A%2256aec9b71fe541bbd6fe24666fba7aa4%22%7D%5D%2C%22range%22%3A%7B%22from%22%3A%22now-6h%22%2C%22to%22%3A%22now%22%7D%7D%7D`
- `scripts/verify-telemetry.sh up|trace|worker|logs|metrics|dashboard` all exit 0 with authenticated requests. Dashboard: `ephematool_ui_page_views_total{service_name="ephematool-dashboard"}` = 1, 5 dashboard log lines in Loki.
- `bazel run //:e2e`: `4 passed`.

## Task Commits

1. **Task 1: failing e2e specs and authenticated verify helpers (RED)** - `715d6c4` (test)
2. **Task 2 RED: failing client tests** - `110b38b` (test)
3. **Task 2 GREEN: SPA routes, traced client, login page, shell** - `4772659` (feat)
4. **Task 3: web service in the stack, session on /api/status, nginx fix** - `7eb3180` (feat)

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] nginx OTLP proxy returned 502 (no resolver)**
- **Found during:** Task 3, first `bazel run //:e2e` (telemetry spec saw no trace)
- **Issue:** `proxy_pass http://otel-collector:4318/$1;` uses a variable, so nginx needs a runtime `resolver`; every browser `POST /otlp/v1/*` failed with 502 and no dashboard telemetry arrived.
- **Fix:** Declared `upstream otel_collector_http { server otel-collector:4318; }` (resolved at startup) and proxied to it.
- **Files modified:** deploy/nginx/default.conf
- **Commit:** 7eb3180

**2. [Rule 3 - Blocking] verify `up` waited on /api/status, which is now 401**
- **Found during:** Task 1 (planned the change) / Task 3
- **Fix:** `scripts/verify.d/up.sh` waits on public `/api/auth/config` and asserts the anonymous `/api/status` is 401.
- **Commit:** 715d6c4

**3. [Rule 3 - Blocking] Playwright cwd under `npm --prefix web exec`**
- `npm --prefix web exec` runs in the repo root, so Playwright found no config and tried to load Vitest files. Used `-c web/playwright.config.ts` and `tsc -b web/tsconfig.json --noEmit` instead of the plan's `-p .` forms. `bazel run //:e2e` is unaffected. The trace-id hand-off file path uses `BUILD_WORKSPACE_DIRECTORY` so it lands in the checkout under `bazel run`.

**4. [Rule 2 - Missing] Small additions beyond the file list**
- `web/src/components/PageHeading.tsx` (focus-on-mount heading per UI-SPEC focus contract).
- GitHub mark is an inline SVG because lucide-react 1.55 ships no brand icons (no new dependency).
- Minimal status-error block (heading, trace id, "Retry Loading Status") and a shell account-load error with Retry; 01-06 owns the full error and empty states.
- 401 from `/api/me` is excluded from the `ui.api_error` metric and log (anonymous bootstrap is expected).

**Total deviations:** 4. No new dependencies; no Go imports changed, so no Gazelle or lockfile changes.

## Issues Encountered

- `/api/status` schema.d.ts did not change after adding `Security` (openapi-typescript does not emit it); only `api/openapi.json` changed and `bazel test //:check_gen` is green.

## Known Stubs

- `Services` list on the status page is intentionally not rendered; the 10-service table and empty/error states belong to plan 01-06. `DeniedPage` has no "Try a different account" button yet (plan 01-07). No placeholder data flows to the UI.

## Threat Flags

None. T-01-28 (status 401) verified by curl; T-01-29 (login rendered as React text), T-01-30 (`encodeURIComponent(next)`), T-01-32 (`propagateTraceHeaderCorsUrls: []` unchanged) and T-01-33 (`rel="noopener noreferrer"`) are implemented as specified.

## Next Phase Readiness

The skeleton is proven and the stack is stopped (`bazel run //:down`). SKELETON.md "Stack Touched in Phase 1" is ticked. Plan 01-06 can add the 10-service table on top of the existing `["status"]` query; 01-07 can add the user menu, theme controls and denied-page button.

## Self-Check: PASSED

All created files exist and commits 715d6c4, 110b38b, 4772659, 7eb3180 are in `git log`.
