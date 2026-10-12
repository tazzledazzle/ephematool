---
phase: 2
slug: create-a-secure-preview-environment
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-10-12
---

# Phase 2 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `testing` + testify (unit; `integration` tag with testcontainers for Postgres and Redis); Vitest 5 (UI units); Playwright (e2e); shell scripts for stack and kind checks |
| **Config file** | `.bazelrc` (`--config=integration`), `tools/bzl/go.bzl`, `web/playwright.config.ts`, `web/vitest.bazel.config.ts` |
| **Quick run command** | `bazel test //internal/... //cmd/...` (touched packages: `bazel test //internal/<pkg>/...`) |
| **Full suite command** | `./tools/verify.sh` (extended), then `bazel run //:kind_verify` |
| **Estimated runtime** | quick ~30s; full several minutes; kind verification a few minutes |

---

## Sampling Rate

- **After every task commit:** `bazel test //internal/<package>/...` for the touched package
- **After every plan wave:** `bazel test //...` plus `bazel test --config=integration //:test_integration` (new packages added: envs, previewauth, audit, ghapp, store)
- **Before `/gsd-verify-work`:** `./tools/verify.sh` green, then `bazel run //:kind_verify`, then the manual UAT list
- **Max feedback latency:** 30 seconds for the quick run

---

## Per-Task Verification Map

Rows are keyed by requirement and success criterion; the planner maps tasks to these. Every task carries its own `<automated>` verify in its PLAN.md.

| Behavior | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|----------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| Create → dispatch → poll → probe → running with a scripted fake run | ENV-01 | — | — | Go integration (Postgres, Redis, fakegithub, worker handlers) | `bazel test --config=integration //:test_integration` | ❌ W0 | ⬜ pending |
| Create flow in the browser including progress to Running | ENV-01 | — | — | Playwright on the `up_fake` overlay | `bazel run //:e2e` | ❌ W0 | ⬜ pending |
| Provisioning yields a running namespace on a real kind cluster (no Actions) | ENV-01 | — | Fenced credentials | script | `bazel run //:kind_verify` | ❌ W0 | ⬜ pending |
| Real Actions run on the self-hosted runner | ENV-01 | — | — | **Manual UAT** | checklist | n/a | ⬜ pending |
| Allowlist, ref, image, schema and size rejections produce field errors and zero dispatches | ENV-02 | SSRF / input injection | Reject before dispatch | Go unit + HTTP | `bazel test //internal/envs/...` | ❌ W0 | ⬜ pending |
| Form schema trimming and unsupported-construct rules | ENV-02 | — | — | Go table test + Vitest | `bazel test //internal/envs/... //web:test` | ❌ W0 | ⬜ pending |
| One trace id spans api, worker, synthetic run/job spans and the probe | OBS-02 | — | — | Script via Tempo API | `scripts/verify.d/env-trace.sh` | ❌ W0 | ⬜ pending |
| Sample app telemetry arrives tagged with id/owner/template; spoofed or id-less data dropped | OBS-03 | Telemetry spoofing | Collector filter drops | Script + `otelcol validate` | `scripts/verify.d/env-telemetry.sh` | ❌ W0 | ⬜ pending |
| Quota and LimitRange exist; extra replicas rejected | GRD-01 | — | Resource caps | `kind_verify` assertions | `bazel run //:kind_verify` | ❌ W0 | ⬜ pending |
| Default-deny blocks egress and ingress; allow rules work | GRD-04 | Cross-namespace access | Default deny | `kind_verify` (busybox exec) | `bazel run //:kind_verify` | ❌ W0 | ⬜ pending |
| viewer 403, creator 202, admin 202; removed creator denied on the next create; every operation has Security or an explicit public marker | AUTH-02 | Stale roles; unprotected routes | Request-time role re-check | Go unit + integration | `bazel test //internal/auth/... //internal/httpapi/...` | ❌ W0 | ⬜ pending |
| Creates, denials and rejections recorded; app role cannot UPDATE/DELETE/TRUNCATE; owner blocked by trigger | AUTH-03 | Audit tampering | Append-only | Go integration (two DB roles) | `bazel test --config=integration //:test_integration` | ❌ W0 | ⬜ pending |
| Signed-out preview → login → preview; session cookie never reaches the app; preview cookie stripped; health path exempt; `X-Robots-Tag`; replay, wrong-env, expired, forged-alg and open-redirect all denied | ENV-09 | Token forgery/replay; session theft; cookie tossing | Env-bound one-time ticket | Go httptest + compose overlay (Traefik file provider + whoami) with Playwright | `bazel test //internal/previewauth/...`; `bazel run //:e2e` | ❌ W0 | ⬜ pending |
| Same ENV-09 behavior through Traefik inside kind | ENV-09 | — | — | `kind_verify` + manual | `bazel run //:kind_verify` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] Extend `internal/fakegithub` with dispatch, run listing/status (ETag path), installation tokens, refs/PR/commit resolution, GHCR manifest checks, a deterministic control endpoint and a fake preview-health endpoint (surface and scripted lifecycle in `02-RESEARCH.md`)
- [ ] `deploy/compose.preview.yaml` overlay: Traefik (file provider, same middleware chain) plus whoami, so ENV-09 is automatable without kind
- [ ] `cmd/sampleapp` reference workload (emits OTLP, serves `/health`, echoes request headers for the cookie assertion) and a way to `kind load` it under the expected image name for offline verification
- [ ] Integration harness for two DB roles (owner migrates; `ephematool_app` connects)
- [ ] `scripts/verify.d/env-trace.sh`, `env-telemetry.sh`; `kind_verify`, `kind_up`, `kind_down`, `runner` targets in `BUILD.bazel`
- [ ] Add every new Go package to `//:test_integration`; run `bazel run //:gazelle`, `bazel mod tidy` after `go get`, and `bazel run //:gen` for sqlc/OpenAPI/TS types
- [ ] shadcn components via the official CLI; Vitest stubs for Radix select/radio under Bazel (Phase 1 pattern)

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| A real GitHub Actions run, scheduled onto the registered self-hosted runner, provisions an environment in kind | ENV-01, OBS-02 | Needs a real GitHub App, an environments repo and a registered runner | Follow the runner and App setup in the README; create an environment; confirm it reaches Running and the trace shows the run |
| Preview URLs resolve and gate correctly in Safari and Firefox | ENV-09 | `*.localhost` resolution differs by browser (macOS returns ::1); only Chromium and curl were tested | Open a preview URL signed out and signed in |
| Real GHCR tag check against a public package | ENV-01 | Needs a real public image | Create with a real ref whose image exists, and one whose image is missing |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 30s for the quick run
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
