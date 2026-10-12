---
phase: 01-telemetry-spine-github-login
reviewed: 2026-10-11T00:00:00Z
depth: standard
files_reviewed: 48
files_reviewed_list:
  - web/index.html
  - web/Dockerfile
  - web/package.json
  - web/vitest.config.ts
  - web/vitest.bazel.config.ts
  - web/playwright.config.ts
  - web/e2e/auth.spec.ts
  - web/e2e/grafana.spec.ts
  - web/e2e/helpers.ts
  - web/e2e/status.spec.ts
  - web/e2e/telemetry.spec.ts
  - web/src/App.tsx
  - web/src/main.tsx
  - web/src/router.tsx
  - web/src/buildinfo.ts
  - web/src/api/client.ts
  - web/src/api/client.test.ts
  - web/src/telemetry/*.ts
  - web/src/features/auth/*
  - web/src/features/status/*
  - web/src/components/{AppShell,UserMenu,PageHeading,RoleBadge}.tsx
  - web/src/lib/*
findings:
  critical: 0
  warning: 9
  info: 4
  total: 13
status: issues_found
---

# Phase 1: Web Code Review Report

**Depth:** standard. No XSS sinks were found: there is no dangerouslySetInnerHTML, and `login` is rendered as React text. `next` is sanitized server-side by `SafeNext` (internal/auth/handlers.go:99, covered by Go tests for `//evil`, `https://evil` and `/\evil`). No session or token material reaches localStorage, telemetry attributes or URLs. Only the theme choice is stored. The nginx OTLP path is allowlisted, size-capped, rate-limited and has Cookie/Authorization stripped. The findings below are correctness, a11y, robustness and test-quality problems.

## Warnings

### WR-01: Page-load duration histogram uses default bucket boundaries, so every observation falls in one bucket
**File:** `web/src/telemetry/metrics.ts:24-27`
**Issue:** `ephematool.ui.page_load.duration` records seconds (`pageLoad.ts:37`), but the SDK's default explicit bucket boundaries are `[0,5,10,25,50,75,100,...]`. Scenario: a 0.4s and a 4.9s page load both land in `(0,5]`, so p50/p95 in Grafana are meaningless.
**Fix:** Pass `advice: { explicitBucketBoundaries: [0.1,0.25,0.5,1,2,5,10] }` to `createHistogram`.

### WR-02: Page-load span is never ended by the real path when `/api/me` fails with non-401 (or on any unexpected render), so duration is skewed to the 10s backstop
**File:** `web/src/components/AppShell.tsx:79-93`, `web/src/api/client.ts:19-23`, `web/src/telemetry/pageLoad.ts:51`
**Issue:** `pageLoadDone()` is called only from LoginPage, DeniedPage and StatusPage. If `/api/me` returns 500, AppShell shows "Couldn't load your account" and `<Outlet/>` never mounts, so nobody calls `pageLoadDone()`. The span, the `page_views` increment and the duration metric then fire only at the 10s timer. This records a false 10s page load for every failed load, and any Retry request is still parented to the stale page-load trace. The span's `route` attribute is also taken from `location.pathname` at bootstrap, so a visit to `/` that redirects to `/login` is attributed to `/`.
**Fix:** In AppShell, call `pageLoadDone()` in an effect when `me.isError`. Optionally record the final route template at end time.

### WR-03: Per-second `role="status"` text update causes continuous screen-reader announcements
**File:** `web/src/features/status/StatusPage.tsx:70-74` (with `useNow` at 16-23)
**Issue:** When a poll fails and cached data exists, `<span role="status">Updated {staleFor}s ago</span>` is mounted. `now` ticks every second, so the live region's text changes every second. Scenario: the API goes down, and a screen-reader user hears "Updated 41s ago", "Updated 42s ago" and so on indefinitely. Also, with data present and the poll failing, no error message, trace ID or Retry control is shown (StatusError renders only when `!status.data`), so the only signal is this counter.
**Fix:** Remove `role="status"` from the ticking text (or make it `aria-live="off"`). Announce a single static "Status refresh failed" message once, and show a Retry control in the stale state.

### WR-04: HealthBadge and ServiceTable crash or silently drop rows on API values outside the compile-time enums; no error boundary exists
**File:** `web/src/features/status/HealthBadge.tsx:17`, `web/src/features/status/ServiceTable.tsx:55-57`, `web/src/router.tsx`
**Issue:** `STATES[status]` is undefined for any status string the API adds, such as "starting". The destructure throws during render, and with no error boundary on the router the entire app unmounts to a blank page. Services whose `group` is not one of the four known values are filtered out silently. If all are unknown, the table renders only the header.
**Fix:** `const s = STATES[status] ?? STATES.unknown;`. Render a trailing "Other" group for unmatched services. Add a `defaultErrorComponent` or error boundary to `createRouter` or the root route.

### WR-05: Potential redirect loop if `/api/me` succeeds but `/api/status` returns 401
**File:** `web/src/api/client.ts:82-84`, `web/src/features/auth/LoginPage.tsx:26`
**Issue:** Any 401 other than on `/api/me` does a full-page navigation to `/login?reason=expired`. LoginPage immediately `Navigate`s to `/` if `/api/me` succeeds. If a session passes `/me` but is rejected by `/status` (a revoked role, a middleware inconsistency or a cookie race), then `/` loads, `/status` returns 401, `/login?reason=expired` loads, `/me` succeeds, and the cycle repeats as a full-reload loop with no backoff. The e2e "expired session" test only covers the case where both fail.
**Fix:** Guard `session.expire` with a sessionStorage counter or timestamp, so a second expiry within a few seconds shows an error screen instead of redirecting. Alternatively, on a 401 from another endpoint, invalidate `["me"]` and let the route guard decide.

### WR-06: DeniedPage renders an attacker-supplied, unvalidated `login` inside trusted-looking copy
**File:** `web/src/features/auth/DeniedPage.tsx:34`
**Issue:** The page is public and the text is escaped, so there is no script injection. But `/denied?login=<anything up to URL length>` lets a third party craft a link showing "Your GitHub account (<arbitrary 2KB text, e.g. 'contact admin@evil to get access'>) is not a member of ..." on the real origin, which is content spoofing. `r` is also used as a React key, and an org that appears in both `allowedOrgs` and `allowedTeams` gives a duplicate-key warning.
**Fix:** Show `login` only when it matches `/^[A-Za-z0-9](?:[A-Za-z0-9-]{0,38})$/`, else "unknown". Use `key={`${r}-${i}`}`.

### WR-07: `next` is forwarded unvalidated on the client (defense in depth only)
**File:** `web/src/features/auth/LoginPage.tsx:30`, `web/src/router.tsx:15`
**Issue:** The server's `SafeNext` blocks open redirects today. The SPA forwards any string, so safety rests on a single control that lives in another service. Nothing in the SPA currently sets `next` (AppShell's `<Navigate to="/login">` drops it, so deep links are lost after login).
**Fix:** Sanitize in `validateSearch`: `next` must start with `/` and not with `//` or `/\`. Have AppShell pass `search: { next: location.pathname + location.search }`.

### WR-08: `grafanaUrl` from the API is placed in an `href` with no scheme check
**File:** `web/src/features/status/StatusPage.tsx:54`
**Issue:** If the API config is ever user-influenced, or the response is tampered with, `javascript:` or `data:` URLs would be activated on click. `rel=noopener` does not mitigate this. The risk is low (server-controlled) but the check is trivial.
**Fix:** `/^https?:\/\//.test(url)` before rendering the link, else render the button disabled.

### WR-09: Tests that cannot fail or leak state
**File:** `web/src/telemetry/telemetry.test.ts:39-43`, `web/src/api/client.test.ts:21-25,15-19`, `web/src/features/auth/auth.test.tsx:19-21`
**Issue:**
- (a) The test "falls back to the active context" only asserts that fetch was called and the promise resolved. It would pass if `tracedFetch` ignored context entirely.
- (b) `mockFetch` assigns `window.fetch` directly, and `vi.restoreAllMocks()` does not restore it, so the mocks leak into later tests (including telemetry tests if files share an environment).
- (c) `useMe` is hard-mocked to `{isSuccess:false,isError:true}`, so the LoginPage "already signed in then Navigate" branch and `pageLoadDone` effect are untested.
- (d) Nothing unit-tests AppShell (401 Navigate, sign-out failure alert, 5xx retry), StatusPage (poll, stale indicator, `pageLoadDone` call), the 10s auto-end, or `pageLoadDone` ending the span. These are the highest-risk behaviors in the phase.
**Fix:** Assert `trace.getActiveSpan()` is undefined or equals a known span inside the fallback fetch. Save and restore `window.fetch` in `afterEach`. Add tests with fake timers for auto-end and for `pageLoadDone` span end, plus AppShell/StatusPage tests with a mocked `api`.

## Info

### IN-01: No CSP or security headers in nginx, and the inline theme script would conflict with a strict CSP
**File:** `web/index.html:7-19`, `deploy/nginx/default.conf` (no `add_header`)
**Issue:** The inline bootstrap script is safe in itself (it reads only a validated localStorage value). Adding `script-src 'self'` later would block it unless a hash or nonce is used. There are also no X-Frame-Options, `frame-ancestors`, nosniff or Referrer-Policy headers.
**Fix:** Add `Content-Security-Policy: default-src 'self'; script-src 'self' 'sha256-<hash of inline script>'; img-src 'self' https://avatars.githubusercontent.com; ...`, plus `frame-ancestors 'none'`.

### IN-02: ThemeProvider transition-suppression style can leak when the tab is hidden
**File:** `web/src/lib/ThemeProvider.tsx:16`
**Issue:** `requestAnimationFrame` does not fire in background tabs. A system-theme change while hidden leaves a `<style>` with `transition:none!important` until the tab is shown. Multiple changes stack several style elements.
**Fix:** Use `setTimeout(…, 0)` or double rAF, and remove any existing override element before adding one.

### IN-03: Dockerfile and lockfile hygiene
**File:** `web/Dockerfile:5-6`, `web/` (both `package-lock.json` and `pnpm-lock.yaml`)
**Issue:** Two lockfiles can drift: the image builds with npm while Bazel presumably uses pnpm. The runtime stage has no `USER` (it relies on the conf listening on 8080), and `BUILT_AT=unknown` is truthy, so `vite.config.ts` never falls back to the real build time.
**Fix:** Keep one lockfile as the source of truth (or add a CI drift check). Use `nginxinc/nginx-unprivileged` or an explicit `USER nginx`. Treat "unknown" as unset in vite.config.

### IN-04: `PageHeading` `role` prop is a misleading name
**File:** `web/src/components/PageHeading.tsx:4,11`
**Issue:** `role` ("display" | "heading") is a size variant, not an ARIA role. It collides conceptually with the HTML `role` attribute and could confuse a future a11y audit.
**Fix:** Rename it to `size`.
