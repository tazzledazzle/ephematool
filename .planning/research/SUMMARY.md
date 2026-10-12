# Project Research Summary

**Project:** ephematool
**Domain:** Ephemeral preview-environment management platform (GitHub Actions provisioning, Kubernetes namespace-per-env, SRE dashboard, OTel-first)
**Researched:** 2026-10-11
**Confidence:** MEDIUM

## Executive Summary

ephematool is a control plane for short-lived Kubernetes preview environments. A dashboard and API record intent in Postgres. GitHub Actions does the provisioning through `workflow_dispatch` against approved Helm templates. Kubernetes namespaces are the runtime. A feedback path brings observed reality back into the control plane: workflow results, cluster state and health probes. Comparable tools (Argo CD ApplicationSet PR generator, Okteto, Uffizzi, Qovery, Release) share this skeleton. The differentiator here is the feedback path. "Live" means lifecycle AND cluster state AND a real probe agree, and disagreement shows up as drift. Every environment also ships telemetry from the moment it exists.

The recommended approach is a TypeScript modular monolith, ESM end to end, with two process roles (API and worker).
- **Backend:** Fastify 5, Postgres with Drizzle, and pg-boss for jobs and cron (no Redis). The dashboard is a React/Vite SPA.
- **GitHub access:** through a GitHub App.
- **Cluster writes:** the control plane writes nothing to the cluster. The workflow creates and stamps namespaces with labels, quota, LimitRange and NetworkPolicy, using OIDC-federated short-lived credentials. The control plane observes through a read-only, label-scoped informer.
- **Telemetry:** the OTel Collector gateway is the single OTLP ingress for the tool and for every environment. Tempo, Loki, Prometheus and Grafana are shared backends. The dashboard queries them through the API and deep-links into Grafana.
- **Out of scope for v1:** a telemetry store, a CRD/operator, and per-environment observability stacks.

The main risks concern the correctness of the feedback loop and the blast radius of the provisioning path: orphaned environments, TTL races, unreliable webhooks, reading a green workflow as a healthy app, fork-PR and OIDC privilege escalation, and OTel cardinality blowup from per-env labels. The roadmap follows from the mitigations.
- Telemetry conventions, including cardinality rules, come first.
- The state machine is level-triggered and reconciled from day one.
- Identity and namespace guardrails go into the base template before the first real deploy.
- A reaper/sweeper runs independently of the DB.
- Health is always a separate signal from workflow conclusion.

## Key Findings

### Recommended Stack
One language (TypeScript 5.9.x, pinned) in a pnpm monorepo, with shared Zod schemas and an OTel bootstrap package. The backend must be a long-running process (reaper, prober, K8s watch, webhook receiver), so no serverless and no Next.js-only backend. Details are in STACK.md.

**Core technologies:**
- Node.js 24 LTS, ESM-only: required by `@kubernetes/client-node` 2.x and OTel SDK 2.x.
- Fastify 5.12 with `@fastify/otel`: API and raw-body signature-verified webhook receiver. `@opentelemetry/instrumentation-fastify` is dead.
- PostgreSQL 17/18, Drizzle (pin exact versions, pre-1.0) and pg-boss 12: system of record, transactional enqueue, cron for the reaper and prober.
- Octokit via GitHub App (`@octokit/plugin-throttling`): dispatch, webhooks, installation tokens. No PATs.
- `@kubernetes/client-node` 2.0: read-only informers on labeled objects.
- OpenTelemetry JS SDK 2.x (`sdk-node` 0.223, api 1.9.1) with OTLP/HTTP to a contrib Collector. Upgrade OTel packages as a set.
- Tempo + Loki + Prometheus/Mimir + Grafana: shared OTLP-native backends, swappable.
- React 19, Vite, TanStack Router/Query/Table, Tailwind + shadcn/ui. Status via SSE or polling only.
- Zod 4 (native `z.toJSONSchema()`) plus Ajv: one schema source for forms, API and template validation.
- better-auth (GitHub OAuth) and `@casl/ability`, with a single `can(user, action, env)` choke point.
- Environment side: Helm per approved template. ResourceQuota, LimitRange and NetworkPolicy in every namespace. ValidatingAdmissionPolicy (CEL) as the in-cluster backstop. External Secrets Operator. GitHub OIDC to the cluster. Kubernetes 1.36 or later.

**Critical version notes:**
- Pin the `X-GitHub-Api-Version` header. In API `2026-03-10`, `workflow_dispatch` returns 200 with `workflow_run_id`. On `2022-11-28` it returns 204 unless `return_run_details: true`. Handle both.
- Stay on TypeScript 5.9. The registry's 7.x was not vetted.
- `@fastify/otel` 0.21 does not document OTel SDK 2.x compatibility. Smoke-test it in Phase 1.

### Expected Features
Details are in FEATURES.md.

**Must have (table stakes, all P1):**
- Create from branch/PR/ref via the dashboard, using schema-validated approved templates.
- Inventory with separate lifecycle states, URL, owner and expiry.
- Manual destroy, with verification that the namespace is actually gone.
- Mandatory TTL with capped extension and a reaper, plus PR-close auto-teardown.
- ResourceQuota/LimitRange and max concurrent environments per team/repo, checked atomically.
- AuthN/AuthZ roles, scoped secret references (never values), the no-prod-data rule, and an append-only audit log.
- Logs/status/events view for debugging failed previews.
- Default-deny NetworkPolicy isolation baseline.

**Should have (differentiators, mostly v1 because they are the core value):**
- Tool self-instrumentation, with a trace spanning click, dispatch, workflow, ready and first healthy probe.
- Per-env OTel injection with `ephemeral.env.*` resource attributes, plus a basic per-env health/telemetry view.
- "Live" = lifecycle AND cluster AND probe, with a first-class drift flag and reason.
- Basic expiry warning (PR comment).
- v1.x: orphan sweeper UI, cost estimates (requests x duration), richer telemetry dashboards, Slack notifications.

**Defer (v2+):** admission-policy product features (Kyverno/OPA), CLI, multi-cluster, non-K8s targets, Backstage plugin, GitOps/Argo backend, CRD/operator.

**Hold the line on these anti-features:** free-form YAML editor, auto-extending TTL on activity, production data cloning, a custom observability backend, rebuilding CI/CD, billing-accurate cost, and websockets everywhere.

### Architecture Approach
A modular monolith with API and worker roles (the worker is leader-elected). It follows a level-triggered desired-vs-observed model.
- Postgres holds intent. Three independent observers feed a pure Status Aggregator: Actions webhooks plus poll, the K8s informer, and the health prober. The aggregator is the only writer of derived `live`/`drift`.
- Commands flow down through dashboard, API, guardrails, Environment Service, outbox, GitHub dispatch, Actions and the cluster. The tool never touches the cluster directly.
- Labels (`env-id`, `owner`, `template`, `expires-at`, `managed-by`) are the contract between tool, workflow and cluster.
- The dashboard never queries telemetry backends directly. The API proxies with env-scoping so access policy applies to telemetry.

**Major components:**
1. Environment Service: aggregate and explicit state machine (requested, provisioning, running or failed, expiring, destroying, destroyed, plus `destroy_failed`), with an append-only event log and compare-and-set transitions.
2. GitHub Integration: App auth, dispatch with correlation, webhook ingress (HMAC, dedupe by delivery ID, persist then ack, async process).
3. Guardrail Policy Engine: in-process, pure and synchronous (schema, template, quota, TTL max, RBAC). Cluster quotas and admission policy are the backstop.
4. K8s Observer (read-only, label-scoped informers) and Health Prober (public-ingress probe with app-signature check, debounce, startup grace).
5. Status Aggregator: a pure, exhaustively testable function computing `live` and `drift`.
6. Reconciler: missed-webhook poll, TTL expiry, drift detection and orphan GC by label, grown in layers.
7. `telemetry/` package and Collector gateway: built first and imported by everything.
8. `templates/` and `workflows/`: versioned with the tool so schemas and reusable create/update/destroy workflows stay in lockstep.

### Critical Pitfalls
Eleven are documented in PITFALLS.md. These five shape the roadmap most.
1. **Orphaned environments and cost leaks.** Label everything at creation. Run an independent label-based sweeper (alert, then delete after a grace period). Make destroy idempotent and retried, with an explicit `destroy_failed` state. Surface stuck-`Terminating` namespaces. Soft-delete rows.
2. **TTL races and extend/destroy ambiguity.** Make `expires_at` mandatory in Postgres, mirrored to a namespace annotation. Use compare-and-set transitions and a single leader (advisory lock or Lease). Use server time. Treat a null or unparseable expiry as expired-pending-review, never immortal.
3. **Unreliable webhooks and three sources of truth.** Verify HMAC on the raw body and dedupe by delivery ID. Make handlers monotonic and idempotent, and re-fetch truth from the API. Add a reconcile poll with ETag and jitter, and redelivery backfill. Define field ownership. Never auto-recreate on drift in v1.
4. **Green workflow is not a healthy app.** Keep `provisioned` and `healthy` separate. Probe the public ingress and verify an app-specific signature, not just a 200. Add a "green workflow, broken app" acceptance fixture.
5. **Privilege and isolation failures.**
   - Fork PRs: no auto-previews with secrets, and no `pull_request_target` that checks out head code.
   - Workflow inputs: pass via `env:` and validate names.
   - OIDC: pin `sub` to the exact repo plus a protected Environment, with namespace-scoped RBAC and a VAP backstop.
   - Namespace names: ID-generated (`pv-<shortid>`), never branch-derived.
   - Base template: quota, LimitRange, default-deny NetworkPolicy and PSA.

Two more matter. OTel cardinality blowup from per-env labels: keep env identity in Resource attributes and `target_info`, bound metric labels, use short retention for env-scoped series, and test with 100 create/destroy cycles. Collector sizing: `memory_limiter` first, `batch` last, a separate pipeline for the tool's own telemetry, and per-env rate limits.

## Implications for Roadmap

Seven phases, following the architecture's build order. One change: the destroy path, labeling and a minimal default TTL move into the provisioning phase, because early environments leak otherwise and the reaper reuses destroy.

### Phase 1: Foundation and Telemetry Spine
**Rationale:** Telemetry cannot be retrofitted, and everything else depends on the repo, DB and Collector.
**Delivers:** pnpm monorepo (api, worker, web, schema, otel). Postgres, migrations and pg-boss. API/worker skeleton. `telemetry/` package with the shared `ephemeral.*` attribute and label module and cardinality rules. Collector gateway with a separate pipeline for the tool's telemetry. A minimal Tempo/Loki/Prometheus/Grafana dev stack. A smoke test that the tool emits traces, metrics and logs, plus the `@fastify/otel` and SDK 2.x compatibility check.
**Addresses:** Tool self-instrumentation.
**Avoids:** Pitfalls 8 and 9, Anti-Pattern 6.

### Phase 2: Environment Model and GitHub Integration
**Rationale:** The environment record plus run correlation is the keystone for lifecycle, live status, TTL and destroy. The state machine must be designed here so TTL needs no rework.
**Delivers:**
- GitHub App, GitHub OAuth login and basic RBAC.
- State machine with event log and CAS transitions.
- `workflow_dispatch` with correlation (`env_id`, `correlation_id`, `traceparent`, run-name, per-env concurrency group, never cancel destroy).
- Webhook ingress (verify, dedupe, persist, async).
- Reconciler v0 (poll for missed webhooks).
- Audit log.
**Uses:** Fastify, Octokit and throttling, Drizzle, pg-boss, better-auth, CASL.
**Avoids:** Pitfalls 3, 4 and 10.

### Phase 3: Secure Provisioning Workflows and Base Template
**Rationale:** Identity and isolation must be designed before the first real deploy, because recovery from a leak or over-broad trust is HIGH cost. Destroy must exist before TTL.
**Delivers:**
- Reusable create/update/destroy workflows and an approved Helm template with `values.schema.json`.
- OIDC to the cluster (exact `sub`, `aud` check, namespace-scoped RBAC).
- Base namespace stamp: labels, ResourceQuota, LimitRange, default-deny NetworkPolicy, PSA, no SA token. Plus a VAP backstop and ID-generated namespace names.
- Fork-PR approval gate, injection-safe inputs, pinned actions and least-privilege permissions.
- A kind/k3d CI cluster.
- Idempotent destroy with `destroy_failed`.
- Minimal create-time schema validation, a hard default TTL, and the thin OTel env fragment.
**Addresses:** Create and destroy, validated templates, size limits, isolation.
**Avoids:** Pitfalls 1 (partly), 6, 7 and 11.

### Phase 4: Cluster Observation, Inventory and Orphan Sweeper
**Rationale:** Guardrails and drift need reliable observed state. The sweeper is the independent check on teardown.
**Delivers:** K8s Observer (label-scoped, read-only), inventory UI with lifecycle state, URL and last-observed timestamps, label-based orphan detection, stuck-`Terminating` surfacing, and a logs/events view.
**Avoids:** Pitfalls 1 and 4.

### Phase 5: Health Probing, Status Aggregator and Drift
**Rationale:** This is the central product claim, and it needs both lifecycle and observed signals first.
**Delivers:** Health Prober (public ingress, app signature, debounce, startup grace, DNS/connect/TLS/status phases), the pure Status Aggregator with drift reasons, probe metrics, and the "green workflow, broken app" and out-of-band-deletion fixtures.
**Avoids:** Pitfall 5, Anti-Pattern 1.

### Phase 6: Per-Environment Telemetry and Dashboard Views
**Rationale:** Dashboard views need Phase 5 health data and a settled backend and cardinality strategy.
**Delivers:** Full template OTel fragment plus Collector `k8sattributes`, optional Operator auto-instrumentation, per-env ingest rate limits, API-proxied env-scoped panels, Grafana deep links, short retention for env-scoped series and post-destroy retention. Verification: a 100-cycle series test and a Collector burst test.
**Avoids:** Pitfalls 8 and 9 (verification), Anti-Pattern 5.

### Phase 7: Guardrail Hardening and Management Actions
**Rationale:** Hardens the minimal versions once observed state is trustworthy.
**Delivers:**
- Full TTL: capped extend and max extensions, CAS extend vs expire, leader lock, expiring-soon state and PR-comment warning.
- PR-close teardown and atomic per-team/repo concurrency quotas.
- Full secrets policy: External Secrets, scoped paths, redaction in telemetry.
- RBAC refinement and IDOR tests.
- Extend/destroy UX with confirm-by-name and denial reasons.
- Audit views.
- A hardening review against the "Looks Done But Isn't" checklist (fork-PR, OIDC, isolation).
**Avoids:** Pitfalls 2, 6, 7 and 11 (verification).

### Phase Ordering Rationale
- Dependencies drive the order: telemetry, then the environment keystone, then provisioning, then observation, then health and drift, then telemetry views, then hardening.
- Guardrails are deliberately split. Minimal ones land in Phases 2 and 3. Full enforcement lands in Phase 7, once observation is reliable.
- Destroy and labeling precede TTL, because the reaper reuses destroy and the sweeper needs the label contract.
- The reconciler grows in layers across Phases 2, 4 and 7.
- Security-sensitive design (OIDC trust, fork-PR handling, isolation) is front-loaded to Phase 3, ahead of the first real repo connection.

### Research Flags
Needs deeper research (`/gsd-plan-phase --research-phase <N>`):
- **Phase 2:** GitHub dispatch run-ID behavior across API versions, webhook payload fields, rate-limit numbers, and the Collector `githubreceiver` and otel-cicd options.
- **Phase 3:** OIDC trust for the chosen cluster or cloud, VAP/MAP availability on the target K8s minor, CNI NetworkPolicy enforcement, fork-PR gating, and a security review. This phase has the highest blast radius.
- **Phase 6:** OTel Operator language coverage, Prometheus/Mimir attribute-to-label behavior (`target_info`), and cardinality and retention design.

Standard patterns (skip research-phase):
- **Phase 1:** well-documented Postgres, Fastify and OTel SDK 2.x setup, aside from the `@fastify/otel` smoke test.
- **Phase 4:** informer observer and label-selector patterns.
- **Phase 5:** pure-function aggregator and standard probing. The work is mostly fixture rigor.
- **Phase 7:** CAS TTL, JSON-Schema validation and RBAC. Revisit only for notification specifics.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | MEDIUM-HIGH | npm versions verified against the registry on 2026-10-11. Collector and backend chart versions, Helm 4, External Secrets, Node 24 LTS status and the TS 5.9 pin rest on training knowledge (LOW). |
| Features | MEDIUM | The table-stakes and differentiator split is a sound judgment call. Competitor specifics were not re-fetched. |
| Architecture | MEDIUM | The patterns are well established. GitHub dispatch details, Collector GitHub receiver maturity and Operator scope are unverified. |
| Pitfalls | MEDIUM | Design-level pitfalls are HIGH confidence. Numeric limits, redelivery behavior and recent API behavior need [VERIFY]. |

**Overall confidence:** MEDIUM

### Gaps to Address
- **GitHub dispatch run-ID behavior:** STACK.md says API `2026-03-10` returns run IDs (second-hand), while ARCHITECTURE.md and PITFALLS.md treat it as unknown. Resolve in Phase 2 research against the REST docs. Use the returned ID when present, and keep the `correlation_id` plus `run-name` fallback regardless.
- **Naming inconsistency:** STACK.md uses `ephemeral.io/env-id` and `ephemeral.*`, while ARCHITECTURE.md and PITFALLS.md use `ephematool/env-id`. Pick one prefix in Phase 1 and encode it once in the shared module.
- **Cluster and cloud target is unspecified:** OIDC setup, CNI NetworkPolicy support, ingress/DNS/TLS strategy (wildcard cert to avoid issuance limits), VAP/MAP availability and the separate-cluster-from-prod decision all depend on it. Decide before Phase 3.
- **CI cluster fidelity:** confirm kind/k3d pinned to K8s 1.36 is representative for NetworkPolicy and VAP tests.
- **Prometheus vs Mimir and the cardinality design:** needed before Phase 6. Fix attribute conventions in Phase 1 regardless.
- **Per-env URL access control:** SSO or proxy in front of previews, and `noindex` (from PITFALLS.md), is not in any feature list. Decide whether it is v1.
- **Package compatibility:** `@fastify/otel` with SDK 2.x, TanStack Table v9 and Drizzle pre-1.0 need early smoke tests and exact pins.
- **Team and repo ownership model:** concurrency quotas and RBAC depend on how teams map to GitHub org/team/repo. Define it in Phase 2.
- **Competitor specifics:** validate against official docs before any competitor claim reaches user-facing material.

## Sources

### Primary (HIGH confidence)
- npm registry (`npm view`, 2026-10-11): package versions and engine/peer ranges.
- OTel JS SDK 2.0 announcement and upgrade guide: 2.x stability, Node/TS minimums, `Resource` migration.

### Secondary (MEDIUM confidence)
- GitHub changelog 2026-02-19 and related issue on `workflow_dispatch` run IDs: second-hand for API `2026-03-10`.
- Kubernetes releases and Mutating Admission Policy docs: supported minors, VAP/MAP status.
- Elastic EDOT Node release notes and the `@fastify/otel` repo.
- Kubernetes controller/informer pattern, OTel Collector gateway and k8sattributes, OTel Operator `Instrumentation` CRD, GitHub Apps and webhooks docs (training knowledge, not re-verified).
- GitHub security hardening, OIDC, webhook best practices and rate-limit docs (training knowledge, [VERIFY] numbers).

### Tertiary (LOW confidence)
- Argo CD ApplicationSet, Okteto, Uffizzi, Qovery, Release, Garden and Backstage (training knowledge).
- Collector/Tempo/Loki/Grafana chart versions, External Secrets, Helm 4, Node 24 LTS status, TS 5.9 as last 5.x: unverified.

---
*Research completed: 2026-10-11*
*Ready for roadmap: yes*
