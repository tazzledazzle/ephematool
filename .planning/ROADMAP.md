# Roadmap: Ephematool

## Overview

Ephematool is built as vertical MVP slices, each leaving an SRE with a new end-to-end capability. Observability is the spine: the tool is instrumented and an SRE can log in first (Phase 1). Then a secure one-click environment that emits telemetry from birth and whose creation is traceable across GitHub Actions (Phase 2). Then an honest inventory with destroy and debugging (Phase 3). Then the central product claim, "live" verified by real health probes with drift flagged (Phase 4). Then guaranteed expiry (Phase 5), per-environment telemetry views (Phase 6), and quota and secrets guardrails (Phase 7).

## Phases

**Phase Numbering:**

- Integer phases (1, 2, 3): Planned milestone work
- Decimal phases (2.1, 2.2): Urgent insertions (marked with INSERTED)

- [x] **Phase 1: Telemetry Spine & GitHub Login** - SRE logs in with GitHub and the tool's own traces, metrics, and logs are visible from first run (completed 2026-10-12)
- [ ] **Phase 2: Create a Secure Preview Environment** - SRE creates a validated, isolated, telemetry-emitting environment with one action, traced end to end
- [ ] **Phase 3: Inventory, Lifecycle & Destroy** - SRE sees every environment with a correct lifecycle state, debugs failures, destroys safely, and spots orphans
- [ ] **Phase 4: Live Health & Drift Detection** - An environment shows as live only when lifecycle, cluster, and a real probe agree; disagreement is flagged
- [ ] **Phase 5: TTL, Extension & Auto-Teardown** - Every environment expires automatically unless explicitly extended, and PR close tears it down
- [ ] **Phase 6: Per-Environment Telemetry Views** - SRE views health and telemetry per environment and provisioning performance over time
- [ ] **Phase 7: Quota & Secrets Guardrails** - Creation is rejected with a clear reason at concurrency limits, and secrets are scoped by reference only

## Phase Details

### Phase 1: Telemetry Spine & GitHub Login

**Goal**: An SRE can log in with GitHub to a running dashboard, and the platform's own API, worker, and dashboard emit OpenTelemetry traces, metrics, and logs from first run
**Mode:** mvp
**Depends on**: Nothing (first phase)
**Requirements**: OBS-01, AUTH-01
**Success Criteria** (what must be TRUE):

  1. SRE can sign in with their GitHub account, stay signed in across browser sessions, and sign out from the dashboard
  2. After one dashboard page load and API call, the SRE can find the resulting trace, a related metric, and correlated logs for the API, worker, and dashboard in the local Grafana stack
  3. A fresh checkout starts the whole stack (API, worker, dashboard, collector, backends) with one command and telemetry flows without extra configuration

**Plans:** 9/8 plans complete
Plans:
**Wave 1**

- [x] 01-01-PLAN.md — Wave 0 backend telemetry spike: Compose + Collector + LGTM, Go API with one correlated trace/metric/log
- [x] 01-02-PLAN.md — SPA scaffold (UI-SPEC design system) + browser OTel; proves A9 page-load parenting

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 01-03-PLAN.md — Worker joins the trace via asynq headers; D-09 conventions generator
- [x] 01-04-PLAN.md — GitHub OAuth login backend: PKCE, org allowlist, roles, users table, sessions, fake GitHub

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 01-05-PLAN.md — Walking skeleton: sign in through the dashboard; one trace across dashboard, API, worker

**Wave 4** *(blocked on Wave 3 completion)*

- [x] 01-06-PLAN.md — Telemetry status page: 10-service prober, grouped table, trace-aware error states
- [x] 01-07-PLAN.md — Auth UX: sliding 7-day/30-day sessions, denied page, user menu with theme, expiry

**Wave 5** *(blocked on Wave 4 completion)*

- [x] 01-08-PLAN.md — Phase gate: Grafana dashboard + correlation, redaction, fresh-checkout, make verify, real login check

**UI hint**: yes

### Phase 2: Create a Secure Preview Environment

**Goal**: An SRE can create a preview environment from a branch, PR, or ref with one dashboard action, and it comes up isolated, resource-limited, and emitting telemetry, with the whole creation visible as one trace
**Mode:** mvp
**Depends on**: Phase 1
**Requirements**: ENV-01, ENV-02, ENV-09, OBS-02, OBS-03, GRD-01, GRD-04, AUTH-02, AUTH-03
**Success Criteria** (what must be TRUE):

  1. SRE picks a repo and branch, PR, or ref and an approved template in the dashboard, clicks Create, and a Kubernetes environment is provisioned by a GitHub Actions run
  2. Invalid or unapproved inputs are rejected with field-level errors before any workflow is dispatched
  3. SRE can follow a single trace from the Create click through the GitHub Actions run to the environment's first healthy probe
  4. The new environment's namespace has CPU/memory limits and a default-deny network baseline, and its telemetry arrives tagged with environment id, owner, and template
  5. A viewer cannot create; a creator can; and each create is recorded in an append-only audit log showing who did it and when
  6. Opening a preview URL while signed out sends the SRE through the platform login, and only an allowed signed-in user reaches the preview; the preview app never receives the platform session cookie

**Plans**: TBD
**UI hint**: yes

### Phase 3: Inventory, Lifecycle & Destroy

**Goal**: An SRE can see all environments with a trustworthy lifecycle state, debug failed ones, destroy them with confirmation, and find leaked namespaces
**Mode:** mvp
**Depends on**: Phase 2
**Requirements**: STAT-01, STAT-02, STAT-05, STAT-06, ENV-03
**Success Criteria** (what must be TRUE):

  1. SRE can open an inventory listing every environment with repo/ref, owner, URL, expiry, and lifecycle state (requested, provisioning, running, failed, destroying, destroyed)
  2. After a missed webhook, an environment's state still converges to match its GitHub Actions run without manual action
  3. SRE can open GitHub Actions logs and Kubernetes events for a failed environment from its detail view
  4. SRE can destroy an environment, and the dashboard reports it destroyed only once the namespace is confirmed gone
  5. SRE can see labeled cluster namespaces unknown to the inventory, listed as orphans

**Plans**: TBD
**UI hint**: yes

### Phase 4: Live Health & Drift Detection

**Goal**: An SRE can trust the "live" label because an environment is live only when lifecycle state, cluster state, and a real health probe agree, and any disagreement is flagged with a reason
**Mode:** mvp
**Depends on**: Phase 3
**Requirements**: STAT-03, STAT-04
**Success Criteria** (what must be TRUE):

  1. An environment whose workflow succeeded but whose app is broken is shown as not live, not as healthy
  2. An environment is shown as live once lifecycle, cluster state, and the health probe all agree
  3. When signals disagree (for example namespace deleted out-of-band while the record says running), the inventory flags the environment as drifted and shows which signals conflict

**Plans**: TBD
**UI hint**: yes

### Phase 5: TTL, Extension & Auto-Teardown

**Goal**: No environment lingers: every one expires automatically unless an SRE explicitly extends it within a cap, and closing the PR removes it
**Mode:** mvp
**Depends on**: Phase 4
**Requirements**: ENV-04, ENV-05, ENV-06, ENV-07, ENV-08
**Success Criteria** (what must be TRUE):

  1. Every new environment has a mandatory expiry shown in the inventory, and it is destroyed automatically when the time passes
  2. SRE can extend an environment's TTL, and the extension is refused once the capped maximum lifetime would be exceeded
  3. After a platform restart mid-expiry, pending expirations still complete exactly once with no duplicate or missed teardown
  4. Closing or merging a PR destroys its environment without manual action
  5. The PR receives a comment before expiry showing the environment URL and expiry time

**Plans**: TBD
**UI hint**: yes

### Phase 6: Per-Environment Telemetry Views

**Goal**: An SRE can open any environment and see its health and telemetry, and see how long environments take to become ready over time
**Mode:** mvp
**Depends on**: Phase 5
**Requirements**: OBS-04, OBS-05
**Success Criteria** (what must be TRUE):

  1. SRE can open an environment's detail page and see error rate, latency, and recent traces for that environment only
  2. SRE can follow a deep link from the environment page into the matching Grafana view
  3. SRE can see time-to-ready and provisioning-duration trends across environments over a selectable time range

**Plans**: TBD
**UI hint**: yes

### Phase 7: Quota & Secrets Guardrails

**Goal**: Creation is bounded by team and repo limits with clear explanations, and environments use scoped secrets by reference with values never exposed
**Mode:** mvp
**Depends on**: Phase 6
**Requirements**: GRD-02, GRD-03, GRD-05
**Success Criteria** (what must be TRUE):

  1. When a team or repo is at its maximum concurrent environments, a create attempt is rejected, even when two requests race
  2. A rejected request shows why, including remaining quota and TTL countdown of existing environments
  3. SRE can reference a scoped secret by name in an environment, and no secret value appears in the dashboard, API responses, or telemetry
  4. A template or request that tries to use production data or an out-of-scope secret is refused

**Plans**: TBD
**UI hint**: yes

## Progress

**Execution Order:**
Phases execute in numeric order: 1 -> 2 -> 3 -> 4 -> 5 -> 6 -> 7

| Phase | Plans Complete | Status | Completed |
|-------|----------------|--------|-----------|
| 1. Telemetry Spine & GitHub Login | 8/8 | Complete    | 2026-10-12 |
| 2. Create a Secure Preview Environment | 0/TBD | Not started | - |
| 3. Inventory, Lifecycle & Destroy | 0/TBD | Not started | - |
| 4. Live Health & Drift Detection | 0/TBD | Not started | - |
| 5. TTL, Extension & Auto-Teardown | 0/TBD | Not started | - |
| 6. Per-Environment Telemetry Views | 0/TBD | Not started | - |
| 7. Quota & Secrets Guardrails | 0/TBD | Not started | - |

## Backlog

Pending and unfinished work backfilled on 2026-10-12. Each item below is also a GitHub issue; existing phase sections above are unchanged. Issues carry a `<!-- backlog-id -->` marker so the backfill can be re-run safely.

**Suggested phase (suggestion only, no phase created):** a "Phase 1 hardening pass" between Phase 2 and Phase 3 could take the Phase 1 follow-ups plus the hardening list below. The migration lock and server timeouts are best done before Phase 2 rolling deploys.

### Hardening (tracked here, not as public issues)

Findings from `.planning/phases/01-telemetry-spine-github-login/01-REVIEW.md` and `01-REVIEW-WEB.md` and from `02-CONTEXT.md` that are deliberately not filed as public issues. Details stay in the review files.

- `01-REVIEW.md` WR-01 (`internal/auth/handlers.go`, `internal/auth/session.go`): re-validate org, team and admin role after login instead of trusting the session copy.
- `01-REVIEW.md` WR-02 (`internal/auth/middleware.go`, `internal/httpapi/status.go`): make session requirement the default for API operations, with explicit public markers, and add a role gate helper before Phase 2 mutations.
- `01-REVIEW.md` WR-03 (`internal/config/config.go`, `deploy/compose.yaml`): align cookie security settings with the configured public URL scheme.
- `01-REVIEW.md` WR-04 (`deploy/nginx/default.conf`, `deploy/otel/collector.yaml`, `internal/status/prober.go`): separate trust levels for browser telemetry ingest and platform telemetry pipelines.
- `01-REVIEW.md` WR-05 (`deploy/compose.yaml`): reduce the Grafana anonymous access role.
- `01-REVIEW.md` WR-10 (`internal/auth/handlers.go`): reset pre-existing session data during login.
- `01-REVIEW.md` IN-01 (`internal/auth/handlers.go`): bound and normalize provider error and `next` values that are logged or stored in cookies.
- `01-REVIEW.md` IN-02 (`internal/auth/handlers.go`, `internal/httpapi/api.go`): review what the public auth config and API docs routes disclose.
- `01-REVIEW.md` IN-04 (`deploy/nginx/default.conf`) and `01-REVIEW-WEB.md` IN-01 (`web/index.html`): add HTTP security headers and a content security policy.
- `01-REVIEW.md` IN-06 (`deploy/compose.yaml`): add authentication to Redis (persistence is tracked in a public issue).
- `01-REVIEW-WEB.md` WR-06 (`web/src/features/auth/DeniedPage.tsx`): validate the login value shown on the denied page.
- `01-REVIEW-WEB.md` WR-07 (`web/src/features/auth/LoginPage.tsx`, `web/src/router.tsx`): validate `next` on the client as defense in depth.
- `01-REVIEW-WEB.md` WR-08 (`web/src/features/status/StatusPage.tsx`): check the URL scheme of the Grafana link before rendering it.
- `02-CONTEXT.md` Deferred Ideas: pin admins by numeric GitHub id instead of login.

### Backlog issues

| # | Title | Labels | Milestone |
|---|-------|--------|-----------|
| [#1](https://github.com/tazzledazzle/ephematool/issues/1) | Verify sign-in with a real GitHub OAuth App and org | `phase-1`, `type:uat`, `area:auth`, `source:uat` | Phase 1 follow-ups |
| [#2](https://github.com/tazzledazzle/ephematool/issues/2) | Verify Open in Grafana and trace-to-logs correlation after a real login | `phase-1`, `type:uat`, `area:telemetry`, `source:uat` | Phase 1 follow-ups |
| [#3](https://github.com/tazzledazzle/ephematool/issues/3) | Validate duration and interval config values at startup | `phase-1`, `type:hardening`, `area:ops`, `source:code-review` | Phase 1 follow-ups |
| [#4](https://github.com/tazzledazzle/ephematool/issues/4) | Serialize migrate-on-start across concurrent API replicas | `phase-1`, `type:hardening`, `area:deploy`, `source:code-review` | Phase 1 follow-ups |
| [#5](https://github.com/tazzledazzle/ephematool/issues/5) | Forward the Host header with its port in the nginx proxy | `phase-1`, `type:tech-debt`, `area:deploy`, `source:code-review` | Phase 1 follow-ups |
| [#6](https://github.com/tazzledazzle/ephematool/issues/6) | Enable Loki compactor retention so retention_period takes effect | `phase-1`, `type:tech-debt`, `area:telemetry`, `source:code-review` | Phase 1 follow-ups |
| [#7](https://github.com/tazzledazzle/ephematool/issues/7) | Make the fresh-checkout verification use isolated compose volumes | `phase-1`, `type:test-gap`, `area:build`, `source:code-review` | Phase 1 follow-ups |
| [#8](https://github.com/tazzledazzle/ephematool/issues/8) | Log allowlist lookup failures and validate ALLOWED_TEAMS entries | `phase-1`, `type:tech-debt`, `area:auth`, `source:code-review` | Phase 1 follow-ups |
| [#9](https://github.com/tazzledazzle/ephematool/issues/9) | Set HTTP server read, write and idle timeouts | `phase-1`, `type:hardening`, `area:ops`, `source:code-review` | Phase 1 follow-ups |
| [#10](https://github.com/tazzledazzle/ephematool/issues/10) | Re-resolve nginx upstreams when containers are recreated | `phase-1`, `type:tech-debt`, `area:deploy`, `source:code-review` | Phase 1 follow-ups |
| [#11](https://github.com/tazzledazzle/ephematool/issues/11) | Tidy collector config: deprecated exporter aliases and environment label override | `phase-1`, `type:tech-debt`, `area:telemetry`, `source:code-review` | Phase 1 follow-ups |
| [#12](https://github.com/tazzledazzle/ephematool/issues/12) | Persist Redis data so sessions and queued tasks survive container recreation | `phase-1`, `type:tech-debt`, `area:deploy`, `source:code-review` | Phase 1 follow-ups |
| [#13](https://github.com/tazzledazzle/ephematool/issues/13) | Fail integration tests instead of skipping when Docker is unavailable | `phase-1`, `type:test-gap`, `area:build`, `source:code-review` | Phase 1 follow-ups |
| [#14](https://github.com/tazzledazzle/ephematool/issues/14) | Set explicit bucket boundaries for the page-load duration histogram | `phase-1`, `type:tech-debt`, `area:web`, `area:telemetry`, `source:code-review` | Phase 1 follow-ups |
| [#15](https://github.com/tazzledazzle/ephematool/issues/15) | End the page-load span when /api/me fails with a non-401 error | `phase-1`, `type:tech-debt`, `area:web`, `area:telemetry`, `source:code-review` | Phase 1 follow-ups |
| [#16](https://github.com/tazzledazzle/ephematool/issues/16) | Stop the stale-data counter from spamming screen readers | `phase-1`, `type:tech-debt`, `area:web`, `accessibility`, `source:code-review` | Phase 1 follow-ups |
| [#17](https://github.com/tazzledazzle/ephematool/issues/17) | Handle unknown service statuses and groups, and add a router error boundary | `phase-1`, `type:tech-debt`, `area:web`, `source:code-review` | Phase 1 follow-ups |
| [#18](https://github.com/tazzledazzle/ephematool/issues/18) | Guard against a full-page redirect loop on repeated 401 responses | `phase-1`, `type:tech-debt`, `area:web`, `area:auth`, `source:code-review` | Phase 1 follow-ups |
| [#19](https://github.com/tazzledazzle/ephematool/issues/19) | Fix web tests that cannot fail or leak state | `phase-1`, `type:test-gap`, `area:web`, `source:code-review` | Phase 1 follow-ups |
| [#20](https://github.com/tazzledazzle/ephematool/issues/20) | Add unit tests for AppShell, StatusPage and page-load span behaviour | `phase-1`, `type:test-gap`, `area:web`, `source:code-review` | Phase 1 follow-ups |
| [#21](https://github.com/tazzledazzle/ephematool/issues/21) | Fix transition-suppression style leaking in hidden tabs | `phase-1`, `type:tech-debt`, `area:web`, `source:code-review` | Phase 1 follow-ups |
| [#22](https://github.com/tazzledazzle/ephematool/issues/22) | Keep a single web lockfile and treat BUILT_AT=unknown as unset | `phase-1`, `type:tech-debt`, `area:build`, `area:web`, `source:code-review` | Phase 1 follow-ups |
| [#23](https://github.com/tazzledazzle/ephematool/issues/23) | Run the web container as a non-root user | `phase-1`, `type:hardening`, `area:deploy`, `source:code-review` | Phase 1 follow-ups |
| [#24](https://github.com/tazzledazzle/ephematool/issues/24) | Run real Radix tooltip and dropdown components in Bazel Vitest | `phase-1`, `type:test-gap`, `area:build`, `area:web`, `source:code-review` | Phase 1 follow-ups |
| [#25](https://github.com/tazzledazzle/ephematool/issues/25) | Build compose images with rules_oci for hermetic, cached images | `phase-1`, `type:tech-debt`, `area:build`, `source:deferred` | Phase 1 follow-ups |
| [#26](https://github.com/tazzledazzle/ephematool/issues/26) | Stamp Bazel-built Go binaries with commit and build time | `phase-1`, `type:tech-debt`, `area:build`, `source:deferred` | Phase 1 follow-ups |
| [#27](https://github.com/tazzledazzle/ephematool/issues/27) | Audit the go-querystring transitive dependency | `phase-1`, `type:tech-debt`, `area:build`, `source:research` | Phase 1 follow-ups |
| [#28](https://github.com/tazzledazzle/ephematool/issues/28) | Make the worker ping throttle deterministic instead of a global Redis throttle | `phase-1`, `type:tech-debt`, `area:ops`, `source:code-review` | Phase 1 follow-ups |
| [#29](https://github.com/tazzledazzle/ephematool/issues/29) | Update the stale technology stack block in CLAUDE.md | `phase-1`, `type:docs`, `source:code-review` | Phase 1 follow-ups |
| [#30](https://github.com/tazzledazzle/ephematool/issues/30) | Add a GitHub webhook receiver with signature verification and delivery dedupe | `backlog`, `type:feature`, `area:ops`, `source:deferred` | Backlog |
| [#31](https://github.com/tazzledazzle/ephematool/issues/31) | Support GitHub OIDC to a remote cluster and a hosted-runner path | `backlog`, `type:feature`, `area:deploy`, `source:deferred` | Backlog |
| [#32](https://github.com/tazzledazzle/ephematool/issues/32) | Support private container images when validating environment image tags | `backlog`, `type:feature`, `area:deploy`, `source:deferred` | Backlog |
| [#33](https://github.com/tazzledazzle/ephematool/issues/33) | Add team or repo ownership attributes to environments | `phase-7`, `type:feature`, `area:ops`, `source:deferred` | Phase 7: Quota & Secrets Guardrails |
| [#34](https://github.com/tazzledazzle/ephematool/issues/34) | Verify environment creation with a real GitHub App, environments repo and self-hosted runner | `phase-2`, `type:uat`, `area:deploy`, `source:research` | Phase 2: Create a Secure Preview Environment |
| [#35](https://github.com/tazzledazzle/ephematool/issues/35) | Verify preview URL access in Safari, Firefox and Linux Docker | `phase-2`, `type:test-gap`, `area:deploy`, `source:research` | Phase 2: Create a Secure Preview Environment |
| [#36](https://github.com/tazzledazzle/ephematool/issues/36) | COST-01: Show estimated cost per environment | `backlog`, `type:feature`, `source:deferred` | Backlog |
| [#37](https://github.com/tazzledazzle/ephematool/issues/37) | NOTF-01: Send Slack notifications for expiry and failures | `backlog`, `type:feature`, `source:deferred` | Backlog |
| [#38](https://github.com/tazzledazzle/ephematool/issues/38) | OBS-06: Rich per-environment dashboards with SLO-style health scoring | `backlog`, `type:feature`, `area:telemetry`, `source:deferred` | Backlog |
| [#39](https://github.com/tazzledazzle/ephematool/issues/39) | POL-01: Enforce guardrails with admission controller policy | `backlog`, `type:feature`, `area:deploy`, `source:deferred` | Backlog |
| [#40](https://github.com/tazzledazzle/ephematool/issues/40) | PLAT-01: Drive the platform from a CLI | `backlog`, `type:feature`, `source:deferred` | Backlog |
| [#41](https://github.com/tazzledazzle/ephematool/issues/41) | PLAT-02: Target multiple clusters | `backlog`, `type:feature`, `area:deploy`, `source:deferred` | Backlog |
| [#42](https://github.com/tazzledazzle/ephematool/issues/42) | PLAT-03: Backstage plugin or GitOps (Argo CD) provisioning backend | `backlog`, `type:feature`, `area:deploy`, `source:deferred` | Backlog |
| [#43](https://github.com/tazzledazzle/ephematool/issues/43) | Epic: Phase 2 - Create a Secure Preview Environment | `epic`, `phase-2` | Phase 2: Create a Secure Preview Environment |
| [#44](https://github.com/tazzledazzle/ephematool/issues/44) | Epic: Phase 3 - Inventory, Lifecycle & Destroy | `epic`, `phase-3` | Phase 3: Inventory, Lifecycle & Destroy |
| [#45](https://github.com/tazzledazzle/ephematool/issues/45) | Epic: Phase 4 - Live Health & Drift Detection | `epic`, `phase-4` | Phase 4: Live Health & Drift Detection |
| [#46](https://github.com/tazzledazzle/ephematool/issues/46) | Epic: Phase 5 - TTL, Extension & Auto-Teardown | `epic`, `phase-5` | Phase 5: TTL, Extension & Auto-Teardown |
| [#47](https://github.com/tazzledazzle/ephematool/issues/47) | Epic: Phase 6 - Per-Environment Telemetry Views | `epic`, `phase-6` | Phase 6: Per-Environment Telemetry Views |
| [#48](https://github.com/tazzledazzle/ephematool/issues/48) | Epic: Phase 7 - Quota & Secrets Guardrails | `epic`, `phase-7` | Phase 7: Quota & Secrets Guardrails |
