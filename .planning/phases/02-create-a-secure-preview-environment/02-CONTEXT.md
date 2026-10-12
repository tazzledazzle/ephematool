# Phase 2: Create a Secure Preview Environment - Context

**Gathered:** 2026-10-12
**Status:** Ready for planning

<domain>
## Phase Boundary

An SRE picks an allowed repo, a ref (branch, PR number or SHA) and an approved template in the dashboard, chooses a size preset, and clicks Create. A GitHub Actions run provisions an isolated, resource-limited namespace on a local kind cluster. The environment emits telemetry tagged with its id, owner and template. The whole creation is visible as one trace from the Create click through the Actions run to the environment's first healthy probe. Only creators and admins can create, every create (and every denial) is written to an append-only audit log, and previews are reachable only by signed-in, allowed platform users.

Requirements: ENV-01, ENV-02, OBS-02, OBS-03, GRD-01, GRD-04, AUTH-02, AUTH-03, plus **ENV-09** (new, added in this discussion: preview URLs require a signed-in, allowed platform user).

Out of this phase: destroy and the full inventory (Phase 3), the full health/drift aggregator (Phase 4), TTL enforcement and the reaper (Phase 5), per-environment telemetry views (Phase 6), concurrency quotas and secrets policy (Phase 7).

</domain>

<decisions>
## Implementation Decisions

### Cluster, runner and preview access
- **D-01:** Previews land in a **local kind cluster**, created and managed by a Bazel target alongside the Compose stack (no cloud cluster in Phase 2).
- **D-02:** A **self-hosted GitHub Actions runner** on the developer's machine, registered only to the platform's environments repo (D-14), deploys into kind using a **namespace-scoped ServiceAccount** credential. No inbound network exposure. **GitHub OIDC to a remote API server** (exact repo/environment `sub` pinning, `aud` check, as in the research) is the documented target for when a remote cluster exists; it is not built in Phase 2.
- **D-03:** Previews are reached through **ingress-nginx on kind** at `<env-id>.preview.localhost` (resolves to 127.0.0.1 in browsers, no hosts-file edits). Namespace names are ID-generated (`pv-<shortid>`), never branch-derived (research pitfall 6).
- **D-04 (new scope, ENV-09):** Preview URLs **require a signed-in, allowed platform user**, and every preview response carries `X-Robots-Tag: noindex`. Mechanism: the **platform session cookie stays host-only on the dashboard origin and is never sent to preview hosts**. Visiting a preview redirects through the platform, which (if the user is signed in and allowed) mints a **short-lived token bound to that one environment** and sets it as a cookie on the preview host; ingress-nginx validates it with `auth_request`. Preview code (untrusted branches) must never see the platform session.
- **D-05:** The ingress auth check **exempts one fixed health path** (for example `/__ephematool/healthz`) that the template's ingress serves; the worker's health probe (D-18) hits only that path, so no credential lives in the worker.

### Who can create, and ownership
- **D-06:** The **creator** role comes from membership in configured GitHub team(s) (`CREATOR_TEAMS`) or logins in `CREATOR_LOGINS`; `ADMIN_LOGINS` still wins. Roles are evaluated at login for UI hints, and the create endpoint **re-verifies creator/admin status against GitHub and the current config at request time**, so removal applies to the next create (closes the stale-role gap from the Phase 1 code review, WR-01, for this action). Works with the fake GitHub.
- **D-07:** Targetable repos come from an **explicit `ALLOWED_REPOS` allowlist** (`org/name`, comma-separated), checked server-side at create time and used to populate the form. Fail closed when empty.
- **D-08:** An environment's **owner is the creating user's GitHub login** (`ephematool.io/owner` label, `ephematool.env.owner` attribute). Team or repo ownership is added later if Phase 7 needs it.
- **D-09:** Roles in Phase 2: **viewer** reads; **creator** creates and (later) manages their own; **admin** can do everything and sees the full audit log. Destroy lands in Phase 3, so only the create gate and audit visibility are exercised now.

### Templates and the create form
- **D-10:** An approved template is a **Helm chart versioned in this repo** (`templates/<name>/` with a `values.schema.json`). The server loads the catalog and validates form inputs against each chart's schema (JSON Schema, in Go) before anything is dispatched; failures return **field-level errors**.
- **D-11:** The app image follows a convention: the platform resolves the chosen ref to a **commit SHA via the GitHub API** and deploys `ghcr.io/<org>/<repo>:<sha>` built by the target repo's own CI. If that tag does not exist, creation is rejected with a field-level error on the ref. The platform does not build images. If the package is private, kind needs an image pull secret.
- **D-12:** The form has: **repo** (from the allowlist), **ref** (type selector for branch / PR number / SHA, resolved server-side), **template** with its **schema-driven extra values** (for example replicas within a cap), and a **size preset (S / M / L)** that maps to fixed ResourceQuota and LimitRange values so users never type raw CPU or memory (GRD-01).
- **D-13:** Every environment is stamped with a default **`ephematool.io/expires-at`** label and DB column (creation time plus a configured default, for example 24h). It is **informational only in Phase 2** (no reaper); Phase 5 adds enforcement, extension and the reaper without a data migration.

### Dispatch, correlation and the first probe
- **D-14:** The provisioning workflow lives in a **dedicated platform repo** (for example `<org>/ephematool-environments`) as reusable create workflows. A **platform GitHub App** (separate from the Phase 1 login OAuth App, D-15 of Phase 1) dispatches `workflow_dispatch` there with validated inputs. Target repos get no write access to the cluster path.
- **D-15:** Correlation: pin the `X-GitHub-Api-Version` header and use the **`workflow_run_id` from the dispatch response when the API returns it**; **always also pass** `env_id`, `correlation_id` and `traceparent` as inputs and put `env_id`/`correlation_id` in `run-name`, so matching works either way. The phase researcher verifies the current API behavior.
- **D-16:** Run status comes from **polling** (ETags, backoff, jitter) in a worker job. The **webhook receiver is designed for but not built** until the platform has a public endpoint.
- **D-17:** **`cmd/fakegithub` is extended** with `workflow_dispatch`, run listing/status and a scripted run lifecycle (queued, in progress, success, failure), so the Go integration tests and the Playwright create flow run offline. The real Actions + kind path is a manual UAT item, like the real GitHub login in Phase 1.
- **D-18:** The **first healthy probe** is a minimal HTTP GET of the preview's exempt health path (D-05) through the ingress, with bounded retries. The first 2xx ends the trace and moves the environment to **`running`**. Phase 4 replaces it with the lifecycle + cluster + probe aggregator and drift. A green workflow alone never means running.
- **D-19:** The **audit log** is an `audit_events` table with **DB-enforced append-only** semantics (the app role gets INSERT and SELECT only, plus a trigger that raises on UPDATE/DELETE). Each event records actor (github_id, login), action, environment id, outcome and trace id. **Creates, denials (not a creator, repo not allowed) and validation rejections are all recorded.** Admins get a read-only audit page in the dashboard.

### Claude's Discretion
- The minimal environment record and state set in Phase 2 (for example requested, provisioning, running, failed); the full lifecycle and inventory come in Phase 3.
- kind and ingress-nginx versions and configuration, the Bazel target shape for the cluster, and size-preset values.
- Preview token format, lifetime and signing, as long as it is environment-bound, short-lived, and keeps the platform session off preview hosts (D-04).
- Polling cadence and timeouts, probe retry bounds, form layout within the approved design system (shadcn, light and dark) and the audit page layout.
- GitHub App permissions and install details, within least privilege.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Project scope
- `.planning/PROJECT.md` — core value and constraints
- `.planning/REQUIREMENTS.md` — ENV-01, ENV-02, OBS-02, OBS-03, GRD-01, GRD-04, AUTH-02, AUTH-03, and ENV-09 (added this phase)
- `.planning/ROADMAP.md` §Phase 2 — goal and success criteria
- `.planning/STATE.md` — blockers (cluster target, dispatch run ID, ownership model, preview access) resolved by this document

### Prior phase decisions and results
- `.planning/phases/01-telemetry-spine-github-login/01-CONTEXT.md` — stack (Go, Postgres, Redis/asynq, OpenAPI-typed SPA), conventions prefixes, session policy, Docker Compose dev stack
- `.planning/phases/01-telemetry-spine-github-login/01-04-SUMMARY.md` — auth, sessions, allowlist, fake GitHub
- `.planning/phases/01-telemetry-spine-github-login/01-BAZEL-CONVERSION-SUMMARY.md` — Bazel rules, command mapping, caveats
- `.planning/phases/01-telemetry-spine-github-login/01-08-SUMMARY.md` — verify gate, Redis `db.statement` leak and fix
- `.planning/phases/01-telemetry-spine-github-login/01-REVIEW.md` and `01-REVIEW-WEB.md` — warnings relevant here: WR-01 (stale roles), WR-02 (routes opt in to auth), WR-03 (COOKIE_SECURE), WR-04 (unauthenticated telemetry ingest), WR-10 (clear session data on login)
- `.planning/phases/01-telemetry-spine-github-login/01-UI-SPEC.md` — design system (Tailwind 4, shadcn, light and dark) the create form and audit page must follow

### Research
- `.planning/research/PITFALLS.md` — pitfalls 6 (fork PRs, injection, ID-generated namespace names), 7 (OIDC and least privilege), 10 (rate limits and dispatch correlation), 11 (multi-tenant isolation)
- `.planning/research/ARCHITECTURE.md` — dispatch and correlation flow, label contract, environment state machine
- `.planning/research/SUMMARY.md` — phase 3 research flags (OIDC, VAP/MAP, CNI NetworkPolicy)
- `conventions/telemetry.json` — D-09 prefixes; generated constants in `internal/conventions` and `web/src/gen`

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/auth` (OAuth/PKCE, allowlist, role.go, scs sessions in Redis, `RequireSession`): extend with creator role, request-time role re-check, and the preview-token mint endpoint.
- `internal/queue` (asynq client, tracing middleware, traceparent in task headers): dispatch and polling jobs reuse the trace propagation.
- `internal/store` (pgx, goose migrations, sqlc): add environments and audit_events tables; sqlc output is committed and drift-checked.
- `internal/conventions` + `conventions/telemetry.json`: label and attribute keys (`envId`, `owner`, `template`, `expiresAt`, `managed`) used to stamp namespaces and telemetry.
- `cmd/fakegithub` / `internal/fakegithub`: extend with dispatch and runs (D-17).
- `deploy/compose*.yaml`, `deploy/otel/collector.yaml`: the platform pipelines have a reserved `*/environments` slot for per-environment telemetry (OBS-03).
- `web/`: shadcn components, `AppShell`, `UserMenu`, traced API client (`tracedFetch`), status page patterns.

### Established Patterns
- Bazel (bzlmod) owns build, test, codegen and stack orchestration; `bazel run //:gazelle` after Go import changes, `bazel mod tidy` after `go get`, new integration packages added to `//:test_integration`, `./tools/verify.sh` is the ordered gate (never `bazel` inside `bazel run`).
- Operations opt in to `RequireSession` (review WR-02): every new Phase 2 route must opt in and add a role gate.
- Tests: Go unit and Docker-backed integration tests, Vitest (Radix pieces need stubs under Bazel), Playwright against the fake-GitHub overlay.
- Redis spans must not record command text (`WithDBStatement(false)`); secrets and tokens never go in logs or span attributes (the redaction check enforces this).

### Integration Points
- A new environments-repo of workflows and the `templates/` Helm catalog live outside the Go code but are versioned with it.
- Compose gains the kind lifecycle and ingress; the preview-token endpoint connects the dashboard session to ingress-nginx `auth_request`.

</code_context>

<specifics>
## Specific Ideas

- No external reference designs were given. Choices followed the recommended options except two deliberate user calls: previews are protected by the platform login **inside Phase 2** (D-04), and local kind with a self-hosted runner now, OIDC later (D-02).
- Phase 2 proves the full chain with the fake GitHub in automation; the real Actions run on a self-hosted runner against kind is a manual UAT item.

</specifics>

<deferred>
## Deferred Ideas

- GitHub **webhook receiver** (signature verification, delivery-ID dedupe) — when the platform has a public endpoint.
- **GitHub OIDC to a remote cluster** and a hosted-runner path — when a remote cluster exists.
- Pinning admins by numeric GitHub id (Phase 1 hardening item).
- Team or repo ownership attributes — Phase 7 if concurrency caps need them.
- Phase 1 review warnings not touched by Phase 2 (telemetry-ingest authentication, Grafana anonymous Admin, Loki retention) — a separate hardening pass.

</deferred>

---

*Phase: 2-Create a Secure Preview Environment*
*Context gathered: 2026-10-12*
