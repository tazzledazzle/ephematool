# Roadmap: Ephematool

## Overview

Ephematool is built as vertical MVP slices, each leaving an SRE with a new end-to-end capability. Observability is the spine: the tool is instrumented and an SRE can log in first (Phase 1). Then a secure one-click environment that emits telemetry from birth and whose creation is traceable across GitHub Actions (Phase 2). Then an honest inventory with destroy and debugging (Phase 3). Then the central product claim, "live" verified by real health probes with drift flagged (Phase 4). Then guaranteed expiry (Phase 5), per-environment telemetry views (Phase 6), and quota and secrets guardrails (Phase 7).

## Phases

**Phase Numbering:**
- Integer phases (1, 2, 3): Planned milestone work
- Decimal phases (2.1, 2.2): Urgent insertions (marked with INSERTED)

- [ ] **Phase 1: Telemetry Spine & GitHub Login** - SRE logs in with GitHub and the tool's own traces, metrics, and logs are visible from first run
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
**Plans:** 8 plans

Plans:
- [ ] 01-01-PLAN.md — Wave 0 backend telemetry spike: Compose + Collector + LGTM, Go API with one correlated trace/metric/log
- [ ] 01-02-PLAN.md — SPA scaffold (UI-SPEC design system) + browser OTel; proves A9 page-load parenting
- [ ] 01-03-PLAN.md — Worker joins the trace via asynq headers; D-09 conventions generator
- [ ] 01-04-PLAN.md — GitHub OAuth login backend: PKCE, org allowlist, roles, users table, sessions, fake GitHub
- [ ] 01-05-PLAN.md — Walking skeleton: sign in through the dashboard; one trace across dashboard, API, worker
- [ ] 01-06-PLAN.md — Telemetry status page: 10-service prober, grouped table, trace-aware error states
- [ ] 01-07-PLAN.md — Auth UX: sliding 7-day/30-day sessions, denied page, user menu with theme, expiry
- [ ] 01-08-PLAN.md — Phase gate: Grafana dashboard + correlation, redaction, fresh-checkout, make verify, real login check
**UI hint**: yes

### Phase 2: Create a Secure Preview Environment
**Goal**: An SRE can create a preview environment from a branch, PR, or ref with one dashboard action, and it comes up isolated, resource-limited, and emitting telemetry, with the whole creation visible as one trace
**Mode:** mvp
**Depends on**: Phase 1
**Requirements**: ENV-01, ENV-02, OBS-02, OBS-03, GRD-01, GRD-04, AUTH-02, AUTH-03
**Success Criteria** (what must be TRUE):
  1. SRE picks a repo and branch, PR, or ref and an approved template in the dashboard, clicks Create, and a Kubernetes environment is provisioned by a GitHub Actions run
  2. Invalid or unapproved inputs are rejected with field-level errors before any workflow is dispatched
  3. SRE can follow a single trace from the Create click through the GitHub Actions run to the environment's first healthy probe
  4. The new environment's namespace has CPU/memory limits and a default-deny network baseline, and its telemetry arrives tagged with environment id, owner, and template
  5. A viewer cannot create; a creator can; and each create is recorded in an append-only audit log showing who did it and when
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
| 1. Telemetry Spine & GitHub Login | 0/8 | Not started | - |
| 2. Create a Secure Preview Environment | 0/TBD | Not started | - |
| 3. Inventory, Lifecycle & Destroy | 0/TBD | Not started | - |
| 4. Live Health & Drift Detection | 0/TBD | Not started | - |
| 5. TTL, Extension & Auto-Teardown | 0/TBD | Not started | - |
| 6. Per-Environment Telemetry Views | 0/TBD | Not started | - |
| 7. Quota & Secrets Guardrails | 0/TBD | Not started | - |
