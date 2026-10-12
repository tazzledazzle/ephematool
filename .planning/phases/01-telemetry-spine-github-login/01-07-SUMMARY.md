---
phase: 01-telemetry-spine-github-login
plan: 07
subsystem: auth
tags: [auth, sessions, scs, sliding-expiry, react, shadcn, theme, playwright, miniredis]

requires:
  - phase: 01-telemetry-spine-github-login
    provides: "scs session manager and OAuth flow (01-04); login/status UI and e2e helpers (01-05); status page (01-06)"
provides:
  - "TestSession* regression suite (miniredis): cookie flags, sliding TTL, idle expiry, absolute lifetime, logout, config defaults"
  - "UserMenu (avatar trigger, Theme radio group, Sign out) and ThemeProvider (localStorage ephematool-theme)"
  - "DeniedPage with focused heading, Try a different account, orgs joined with or"
  - "Playwright auth specs: denied, cancelled, expired, browser restart, theme/sign-out, viewer badge"
  - "scripts/verify.d/auth-denied.sh (auth.denied event in Loki)"
affects: [01-08]

tech-stack:
  added: ["github.com/alicebob/miniredis/v2 v2.40.0 (approved; gopher-lua v1.1.1 transitive)"]
  patterns:
    - "Sliding sessions come from scs itself (IdleTimeout > 0); no touch code"
    - "Sign-out lives in AppShell and is passed to UserMenu as onSignOut, keeping UserMenu free of router/query context"
    - "Loki attributes are structured metadata: filter with | login=\"x\", not |= \"x\""

key-files:
  created:
    - internal/auth/session_test.go
    - web/src/components/UserMenu.tsx
    - web/src/lib/ThemeProvider.tsx
    - web/src/features/auth/auth.test.tsx
    - scripts/verify.d/auth-denied.sh
  modified:
    - web/e2e/auth.spec.ts
    - web/src/features/auth/DeniedPage.tsx
    - web/src/components/AppShell.tsx
    - web/src/components/ui/dropdown-menu.tsx
    - web/src/main.tsx
    - web/vitest.bazel.config.ts
    - internal/auth/session.go
    - internal/auth/handlers.go
    - internal/config/config.go
    - internal/auth/authtest/env.go
    - internal/auth/BUILD.bazel
    - go.mod
    - go.sum
    - MODULE.bazel
    - .env.example
    - deploy/compose.yaml

key-decisions:
  - "scs v2.9.0 Load marks any found session Modified while IdleTimeout > 0, so every authenticated request re-slides the Redis TTL and cookie. The explicit last_touch design in RESEARCH was wrong and was removed."
  - "T-01-41 (write amplification) is accepted: one commit per authenticated request, 15s poll per open tab is negligible at Phase 1 scale."
  - "Radix avatar/dropdown are stubbed in Vitest and React is deduped in vitest.bazel.config.ts to avoid the two-React-copies problem under Bazel."

requirements-completed: [AUTH-01]

duration: multi-session
completed: 2026-10-12
---

# Phase 1 Plan 07: Auth experience Summary

Sessions slide on every request via scs itself, the denied/login/user-menu/theme UX matches the UI-SPEC, and the whole flow is covered by Go session tests, Vitest and 10 Playwright auth specs.

## Accomplishments

- TestSession* suite proves persistent cookie (HttpOnly, Lax, Path=/), TTL re-slide after activity, idle expiry, absolute lifetime cap (D-14), logout destroying the Redis key, and the 168h/720h config defaults.
- UserMenu: avatar trigger labelled "Open user menu for {login}" with tooltip "Account menu", Theme radio group (Light, Dark, System with check icon, menu stays open), Sign out. ThemeProvider persists to `ephematool-theme` and applies without animation.
- DeniedPage: heading focused on mount, orgs/teams as mono code joined with " or ", "Try a different account" returns to /login. Login is rendered as React text (Vitest asserts `<b>` appears literally); unknown `reason` values render nothing; no `dangerouslySetInnerHTML` in web/src.
- `auth.denied` reaches Loki; `scripts/verify-telemetry.sh auth-denied` passes.

## Verification (real runs)

- `go test ./internal/auth/... -count=1` (no -short): pass, absolute-lifetime test ~4s. `bazel test //...`: 16/16 pass. `bazel test --config=integration //:test_integration`: pass.
- `bazel test //web:test` and `npm --prefix web test -- --run`: 33 tests pass under both. `//web:typecheck` and `npm --prefix web run build` pass.
- `bazel run //:up_fake`, then Playwright (`cd web && npx playwright test`): 13/13 pass (10 auth, status, telemetry). `scripts/verify-telemetry.sh all` passes. Stack stopped with `bazel run //:down`.

## Deviations from Plan

**1. [Rule 4 resolved with coordinator] scs already slides sessions.**
- Found during: Task 1 (probe test).
- Issue: RESEARCH Pitfall 1 said scs only commits when modified. In scs v2.9.0, `Load` sets status Modified whenever `IdleTimeout > 0`. A read-only request moved the miniredis TTL from 3.999s to 10s and issued a new Set-Cookie.
- Fix (coordinator-approved option 1): removed the touch logic, `TestSessionNoTouchWithinInterval` and its acceptance criterion; Go session tests became regression guards (no RED phase); T-01-41 changed to accepted; corrected 01-RESEARCH.md and SKELETON.md; removed `SessionTouchInterval`, `SESSION_TOUCH_INTERVAL` (.env.example, compose) and the `last_touch` session key. `internal/auth/middleware.go` is unchanged.
- Commit: 81fb929.

**2. [Rule 3 - Blocking] Vitest under Bazel.** React is deduped in `web/vitest.bazel.config.ts` (`resolve.dedupe`), and Radix avatar and dropdown-menu are mocked in auth.test.tsx (same approach as the 01-06 tooltip mock; the real menu is covered by Playwright). Inlining Radix/react-query into Vite was tried and breaks nested pnpm resolution. To avoid react-query's own React copy, sign-out moved to AppShell and is passed to UserMenu as `onSignOut`.

**3. [Rule 1 - Bug] auth-denied Loki query.** Log attributes are structured metadata, so `|= "octo-outsider"` matched nothing. Changed to `| login="octo-outsider"`. Commit: 51eaedb.

**4. Other adjustments.** The dropdown radio item indicator was changed from a circle to a check icon (UI-SPEC). Existing happy-path/sign-out e2e tests were updated to open the user menu first. `go mod tidy` was needed so miniredis is a direct dependency and `bazel mod tidy` emits its `use_repo`; `bazel mod tidy` also reordered one line in MODULE.bazel. Playwright must run from `web/` (`cd web && npx playwright test`); `npm --prefix web exec` runs it without the config and fails with an invalid URL.

## Commits

- 81fb929 docs(01-07): correct scs sliding-session assumption and remove unused touch config
- 7c16e0c test(01-07): add session regression tests, auth e2e specs and auth-denied verify
- 51eaedb fix(01-07): query auth.denied login via Loki structured metadata
- 283114a feat(01-07): add user menu with theme switch, denied page actions and ThemeProvider

## Known Stubs

None.

## Threat Flags

None.

## Self-Check: PASSED
