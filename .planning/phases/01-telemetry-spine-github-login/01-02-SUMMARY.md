---
phase: 01-telemetry-spine-github-login
plan: 02
subsystem: ui
tags: [react, vite, tailwind, shadcn, otel-web, nginx, vitest, playwright, spike]

requires: []
provides:
  - web/ SPA scaffold (Vite 8.3, React 19.3, Tailwind 4, shadcn/Radix) with UI-SPEC tokens and pre-paint theme script
  - Browser OTel (traces, logs, metrics) via initTelemetry, exporting to same-origin /otlp/v1/*
  - startPageLoad and tracedFetch (A9 proven)
  - Vitest and Playwright configs
  - web/Dockerfile and deploy/nginx/default.conf (same-origin front door)
affects: [01-03, 01-05, 01-06, 01-07]

tech-stack:
  added: [vite 8.3.4, react 19.3.0, tailwindcss 4.3.3, vitest 5.0.3, "@playwright/test 1.64.0", opentelemetry-js web SDK 2.12.0 / 0.223.0, radix-ui 1.8.0, shadcn CLI 4.21.4]
  patterns:
    - Telemetry initialized before React renders
    - Lazy-delegating uiMetrics wrappers so importers can hold instruments before init
    - Route-template-only metric attributes
    - nginx as same-origin front door for SPA, /api and /otlp

key-files:
  created:
    - web/package.json
    - web/package-lock.json
    - web/vite.config.ts
    - web/vitest.config.ts
    - web/playwright.config.ts
    - web/index.html
    - web/src/index.css
    - web/src/lib/theme.ts
    - web/src/telemetry/index.ts
    - web/src/telemetry/tracing.ts
    - web/src/telemetry/pageLoad.ts
    - web/src/telemetry/logs.ts
    - web/src/telemetry/metrics.ts
    - web/src/telemetry/telemetry.test.ts
    - web/Dockerfile
    - deploy/nginx/default.conf
  modified: []

key-decisions:
  - "Register StackContextManager explicitly; A9 holds without Zone.js"
  - "uiMetrics are thin wrappers over late-bound instruments instead of proxy-meter instruments"
  - "web/Dockerfile.dockerignore added because web/.dockerignore is ignored when the build context is the repo root"

patterns-established:
  - "Metric attributes limited to route template and outcome"
  - "OTLP ingest from the browser goes only through nginx POST allowlist"

requirements-completed: [OBS-01]

duration: 25min
completed: 2026-10-11
---

# Phase 1 Plan 02: Dashboard SPA scaffold and browser OTel Summary

**Vite/React/Tailwind/shadcn dashboard with the UI-SPEC theme system, browser OpenTelemetry for traces, logs and metrics over same-origin /otlp, and an nginx front door; assumption A9 (page-load parenting without Zone.js) proven in Vitest.**

## Package legitimacy

Task 1 (blocking-human gate) was resolved before execution: user reply "approved" (2026-10-11) after provenance check (repository github.com/vitest-dev/vitest, first release 2021-12-03, 486 releases, 5.0.3 published 2026-09-30 by the Vitest maintainers). No npm install ran before approval.

Related package notes from this run:
- `npx shadcn@4.21.4 add ...` mis-resolved the `@/` alias (the root tsconfig had no `paths`) and auto-added an unrelated npm package named `cn` plus generated `import ... from "cn"` lines. The package was removed with `npm uninstall cn` and all imports were rewritten to `@/lib/utils`. The lockfile does not contain `cn`.
- `@opentelemetry/core` 2.12.0 (already a transitive dependency of the SDK, same release train) was added as a direct dependency for `W3CTraceContextPropagator`.

## Spike Results

| Assumption | Result | Evidence |
|---|---|---|
| A9 `ui.page_load` parents fetches via synchronous `context.with`, no Zone.js | PROVEN | `npm --prefix web test -- --run src/telemetry`: test "A9: ui.page_load is the parent of fetches without Zone.js > runs the fetch with the ui.page_load span active and injects its trace id" passes with an explicitly registered `StackContextManager`. The active span inside the stub fetch has the page span's spanId, and `propagation.inject` yields `traceparent` `00-<pageTraceId>-<16 hex>-01`. No ZoneContextManager fallback was needed. |

Not covered here: the browser to nginx to Collector to Tempo path with a real browser (Playwright). That check belongs to plan 01-05/01-07 once the Compose `web` service exists.

## Performance

- **Duration:** ~25 min
- **Tasks:** 3 (Task 1 resolved by prior human approval)
- **Files created:** ~35

## Accomplishments

- `npm --prefix web run build` yields a static SPA with the eight approved shadcn components and the light/dark variables from UI-SPEC (hex values exact); fonts limited to 12/14/20/28 px and weights 400/600; 2px focus ring with 2px offset; skeleton static under reduced motion; default button height 44px.
- Theme preference is applied before first paint by an inline `index.html` script; `readThemeChoice` accepts only light/dark/system (T-01-12).
- `initTelemetry()` runs before `ReactDOM.createRoot`, registering tracing, fetch and document-load instrumentation, a LoggerProvider and a MeterProvider (15s export) with `service.name=ephematool-dashboard`, `service.namespace=ephematool`; flushes on `pagehide`.
- nginx config passes `nginx -t`; web image builds with `docker build -f web/Dockerfile .`.

## Task Commits

1. **Task 1: Package legitimacy gate** - no commit (resolved by user approval)
2. **Task 2: Scaffold web/, design system, theme, runners, Dockerfile, nginx** - `72eaf87` (feat)
3. **Task 3: Browser OTel and A9 proof** - `77349a0` (feat)

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] shadcn CLI wrote components to `web/@/components/ui` and added a stray `cn` package**
- **Found during:** Task 2
- **Fix:** Moved the files into `src/components/ui`, uninstalled `cn`, rewrote imports to `@/lib/utils`, added `paths` to the root tsconfig.json for future CLI runs, installed radix-ui, class-variance-authority, clsx, tailwind-merge and tw-animate-css with exact pins.
- **Commit:** 72eaf87

**2. [Rule 2 - Missing critical] Dockerfile ignore file for repo-root build context**
- **Issue:** `web/.dockerignore` is not read when the context is the repo root, so host `node_modules` would be copied into the image.
- **Fix:** Added `web/Dockerfile.dockerignore` with the same content (BuildKit per-Dockerfile ignore). `web/.dockerignore` is kept per the plan.
- **Commit:** 72eaf87

**3. [Minor] Adaptations of generated shadcn code to UI-SPEC**
- `font-medium` replaced with `font-semibold` (two weights only), shadcn's 3px focus ring replaced by the global 2px/2px accent outline, default button height `h-11`.

**4. [Minor] Extra files and TDD shape**
- Added `tsconfig.app.json`, `tsconfig.node.json`, and `src/telemetry/resource.ts` (shared resource), not in the plan's file list.
- The theme test was verified RED (module missing) then GREEN, but test and implementation landed in one feat commit per task rather than separate test/feat commits. The telemetry test was likewise RED (modules missing) before implementation.
- The A9 test file has four test cases covering the five listed behaviors (active span and traceparent are asserted in one case).
- `main.tsx` uses `ReactDOM.createRoot` so the `createRoot` token first appears after `initTelemetry()`.

**Total deviations:** 1 blocking fix, 1 missing-critical addition, 2 minor. No scope change.

## Issues Encountered

- npm prints EBADENGINE warnings for jsdom 30 dependencies on local Node 25.9 (they require ^22.22.2 || ^24.15.0 || >=26); tests run fine, and the Docker build uses Node 24.
- Vite warned about `__dirname` in config; switched to `import.meta.dirname`.
- Untracked pre-existing `README.md` left untouched.

## Known Stubs

- `web/src/App.tsx` renders only a centered "Ephematool" heading; plan 01-05 replaces it with the router and screens.
- `npm run gen` points at `src/api/schema.d.ts` which does not exist yet; plan 01-03 produces it.

## Threat Flags

None beyond the plan's threat model. Mitigations in place: T-01-08 (POST-only `limit_except`, path regex allowlist, `client_max_body_size 1m`, `limit_req` 20r/s burst 20, 404 for other /otlp paths), T-01-09 (`Cookie` and `Authorization` stripped), T-01-11 (route/outcome attributes only), T-01-12 (theme validation, tested), T-01-SC (exact pins, committed lockfile, `npm ci` in Dockerfile).

## Self-Check: PASSED

- Files present: web/package.json, web/package-lock.json, web/src/telemetry/{index,tracing,pageLoad,logs,metrics}.ts, web/src/telemetry/telemetry.test.ts, web/Dockerfile, deploy/nginx/default.conf.
- Commits `72eaf87` and `77349a0` exist.
- `npm --prefix web test -- --run` (8 tests), `npm --prefix web run build`, `nginx -t` and `docker build -f web/Dockerfile .` all succeed; `web/src/gen` does not exist.
