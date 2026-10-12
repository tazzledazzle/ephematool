# Requirements: Ephematool

**Defined:** 2026-10-11
**Core Value:** Every ephemeral environment is observable from the moment it exists: SREs can see, via OpenTelemetry, what is running, whether it is actually healthy, and when it will go away.

## v1 Requirements

Requirements for initial release. Each maps to roadmap phases.

### Observability

- [x] **OBS-01**: The platform emits OpenTelemetry traces, metrics, and logs for its own API, worker, and dashboard from first run
- [ ] **OBS-02**: An SRE can follow one trace from a dashboard "Create" click through the GitHub Actions run to the environment's first healthy probe
- [ ] **OBS-03**: Every environment ships OTel telemetry from creation, tagged with environment id, owner, and template attributes
- [ ] **OBS-04**: An SRE can view a per-environment health and telemetry panel (error rate, latency, recent traces) with a deep link into Grafana
- [ ] **OBS-05**: An SRE can see time-to-ready and provisioning-duration metrics for environments over time

### Inventory & Status

- [ ] **STAT-01**: An SRE can view an inventory of all environments showing repo/ref, owner, URL, expiry, and lifecycle state
- [ ] **STAT-02**: Each environment's lifecycle state (requested, provisioning, running, failed, destroying, destroyed) is derived from its GitHub Actions runs and kept correct even when webhooks are missed
- [ ] **STAT-03**: An environment is shown as "live" only when its lifecycle state, cluster state, and a real health probe all agree
- [ ] **STAT-04**: When lifecycle, cluster, and health signals disagree, the environment is flagged as drifted with the reason shown
- [ ] **STAT-05**: An SRE can open the GitHub Actions logs and Kubernetes events for an environment to debug a failed preview
- [ ] **STAT-06**: An SRE can see labeled cluster namespaces that the inventory does not know about (orphans)

### Environment Lifecycle

- [ ] **ENV-01**: An SRE can create a preview environment from a branch, PR, or ref in the dashboard with one action
- [ ] **ENV-02**: Environment creation accepts only schema-validated inputs against an approved template
- [ ] **ENV-03**: An SRE can destroy an environment, and the platform confirms the namespace is actually gone
- [ ] **ENV-04**: Every environment has a mandatory TTL after which it is automatically destroyed
- [ ] **ENV-05**: An SRE can extend an environment's TTL up to a capped maximum lifetime
- [ ] **ENV-06**: The expiry reaper is idempotent and resumes correctly after a platform restart
- [ ] **ENV-07**: An environment is destroyed automatically when its PR is closed or merged
- [ ] **ENV-08**: A PR comment warns of upcoming expiry with the environment URL and expiry time

### Guardrails & Access

- [ ] **GRD-01**: Every environment namespace has enforced CPU and memory limits through ResourceQuota and LimitRange
- [ ] **GRD-02**: Creation is rejected when the team or repo is at its maximum number of concurrent environments, checked atomically
- [ ] **GRD-03**: When a request is rejected, the dashboard explains why, including remaining quota and TTL countdown
- [ ] **GRD-04**: Every environment namespace gets a default-deny NetworkPolicy baseline
- [ ] **GRD-05**: Environments reference scoped external secrets by name; secret values are never displayed, and production data is never used
- [ ] **AUTH-01**: An SRE can log in with GitHub
- [ ] **AUTH-02**: Actions are gated by role (viewer, creator, admin)
- [ ] **AUTH-03**: Every create, extend, and destroy action is recorded in an append-only audit log showing who did it and when

## v2 Requirements

Deferred to future release. Tracked but not in current roadmap.

### Cost & Notifications

- **COST-01**: User can see estimated cost per environment (requests x duration)
- **NOTF-01**: User receives Slack notifications for expiry and failures

### Observability Depth

- **OBS-06**: User can view rich per-environment dashboards with SLO-style health scoring

### Enforcement

- **POL-01**: Guardrails are enforced by admission controller policy (Kyverno/OPA) even if the dashboard is bypassed

### Platform Reach

- **PLAT-01**: User can drive the platform from a CLI
- **PLAT-02**: User can target multiple clusters
- **PLAT-03**: User can use a Backstage plugin or GitOps (Argo CD) provisioning backend

## Out of Scope

Explicitly excluded. Documented to prevent scope creep.

| Feature | Reason |
|---------|--------|
| Reimplementing CI/CD or build pipelines | Tool orchestrates and observes GitHub Actions only; reimplementation is a huge surface |
| Free-form YAML/manifest editor | Defeats the validated-config guardrail |
| Non-Kubernetes targets (Compose, VMs, static previews) | Dilutes the provisioning and observability model |
| Dev-in-cluster inner loop (file sync, remote dev) | A different product (developer tooling vs SRE operations) |
| Production data cloning | Violates the no-prod-data rule and creates compliance risk; use synthetic fixtures |
| Custom observability backend/UI | Reinvents Grafana; export OTel and deep-link instead |
| Auto-extending TTL on activity | Reintroduces lingering environments; extension must be explicit and capped |
| Billing-accurate cost accounting | Needs cloud billing integrations; request-based estimates are deferred to v2 |
| Websockets for all views | Cost without value; polling or SSE for status only |

## Traceability

Which phases cover which requirements. Updated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| AUTH-01 | Phase 1 | Pending |
| OBS-01 | Phase 1 | Complete |
| AUTH-02 | Phase 2 | Pending |
| AUTH-03 | Phase 2 | Pending |
| ENV-01 | Phase 2 | Pending |
| ENV-02 | Phase 2 | Pending |
| GRD-01 | Phase 2 | Pending |
| GRD-04 | Phase 2 | Pending |
| OBS-02 | Phase 2 | Pending |
| OBS-03 | Phase 2 | Pending |
| ENV-03 | Phase 3 | Pending |
| STAT-01 | Phase 3 | Pending |
| STAT-02 | Phase 3 | Pending |
| STAT-05 | Phase 3 | Pending |
| STAT-06 | Phase 3 | Pending |
| STAT-03 | Phase 4 | Pending |
| STAT-04 | Phase 4 | Pending |
| ENV-04 | Phase 5 | Pending |
| ENV-05 | Phase 5 | Pending |
| ENV-06 | Phase 5 | Pending |
| ENV-07 | Phase 5 | Pending |
| ENV-08 | Phase 5 | Pending |
| OBS-04 | Phase 6 | Pending |
| OBS-05 | Phase 6 | Pending |
| GRD-02 | Phase 7 | Pending |
| GRD-03 | Phase 7 | Pending |
| GRD-05 | Phase 7 | Pending |

**Coverage:**
- v1 requirements: 27 total
- Mapped to phases: 27
- Unmapped: 0

---
*Requirements defined: 2026-10-11*
*Last updated: 2026-10-11 after initial definition*
