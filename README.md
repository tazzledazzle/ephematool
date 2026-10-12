# Ephematool

## What This Is

A push-button web dashboard for SREs to create, configure, manage, and observe ephemeral preview environments (full app stacks on Kubernetes) provisioned via GitHub Actions. It tracks which environments are live and healthy, enforces guardrails on environment configuration to cut manual workflows, and ships with OpenTelemetry-based monitoring and observability from day one.

## Core Value

Every ephemeral environment is observable from the moment it exists: SREs can see, via OpenTelemetry, what is running, whether it is actually healthy, and when it will go away.

## Requirements

### Validated

(None yet — ship to validate)

### Active

- [ ] OpenTelemetry instrumentation from day one: traces, metrics, and logs for the tool itself and for each provisioned environment (observability-first v1)
- [ ] Observability views over environments: per-environment health, telemetry, and lifecycle in the dashboard
- [ ] Environment inventory showing which environments are live vs. not live
- [ ] "Live" status combines lifecycle state (provisioning / running / failed / destroyed, derived from GitHub Actions runs) with real health checks against the deployed app, and flags drift between the two
- [ ] Push-button creation of a preview environment (full app stack on Kubernetes) from the web dashboard, by triggering a GitHub Actions workflow
- [ ] Manage environments from the dashboard: view, configure, extend, and destroy
- [ ] Guardrail: TTL / auto-teardown, with every environment expiring unless explicitly extended
- [ ] Guardrail: size and cost limits (CPU/memory/replica caps, max concurrent environments per team or repo)
- [ ] Guardrail: validated configuration through schema-checked inputs and approved templates instead of free-form manual config
- [ ] Guardrail: access and secrets policy (who can create/destroy, scoped secrets, no production data in previews)

### Out of Scope

- Non-Kubernetes targets (Docker Compose hosts, raw VMs, static-site previews) — v1 targets Kubernetes only to keep the provisioning path and observability model focused
- CLI as a primary interface — the dashboard is the push-button surface; a CLI may come later
- Replacing GitHub Actions as the provisioning engine — the tool orchestrates and observes Actions workflows rather than reimplementing CI/CD

## Context

- Intended as part of regular SRE duties: the pain is manual, error-prone wiring of preview environments, no clear view of which environments are live, and environments lingering without anyone noticing.
- Environments are full app stacks (services plus their dependencies), typically per branch or PR, provisioned through GitHub Actions.
- The v1 bar is observability first: rich OpenTelemetry visibility over environments takes priority, while creation stays simple. Creation, management, and guardrails all still belong in the product, and the roadmap will sequence them around observability.
- Greenfield project in a portfolio-projects workspace; no existing code.

## Constraints

- **Platform**: Kubernetes as the environment runtime — decided up front; the dashboard and provisioning workflows target it
- **Provisioning**: GitHub Actions is the orchestration/provisioning mechanism — it is the stated premise of the tool
- **Observability**: OpenTelemetry from day one — telemetry cannot be retrofitted cleanly; it shapes the architecture
- **Interface**: Web dashboard as the primary push-button surface

## Key Decisions

| Decision | Rationale | Outcome |
|----------|-----------|---------|
| Environments are full app stacks | User wants realistic previews, not just static or web-only previews | — Pending |
| Kubernetes as the runtime target | User choice; namespace-per-environment fits ephemeral isolation | — Pending |
| Web dashboard as the push-button UI | Gives SREs a visual inventory plus a create action in one place | — Pending |
| Observability-first v1 | User's stated bar for v1 feeling real; OpenTelemetry from day one | — Pending |
| "Live" = lifecycle state AND health-verified, with drift flagged | A green workflow does not mean a healthy app; both signals needed | — Pending |
| All four guardrail types in scope (TTL, size/cost, validated config, access/secrets) | Core to reducing manual workflows | — Pending |

## Evolution

This document evolves at phase transitions and milestone boundaries.

**After each phase transition** (via `/gsd-transition`):
1. Requirements invalidated? → Move to Out of Scope with reason
2. Requirements validated? → Move to Validated with phase reference
3. New requirements emerged? → Add to Active
4. Decisions to log? → Add to Key Decisions
5. "What This Is" still accurate? → Update if drifted

**After each milestone** (via `/gsd-complete-milestone`):
1. Full review of all sections
2. Core Value check — still the right priority?
3. Audit Out of Scope — reasons still valid?
4. Update Context with current state

---
# Architecture Research

**Domain:** Ephemeral preview-environment management platform (control plane + GitHub Actions provisioning + Kubernetes runtime + OpenTelemetry)
**Researched:** 2026-10-11
**Confidence:** MEDIUM. The overall shape comes from well-established patterns (Argo CD/Flux, Backstage, Qovery/Release/Uffizzi-style preview tools, the Kubernetes controller pattern, OTel Collector gateway deployments). I did not verify current GitHub API details or Collector component maturity against live docs in this pass. Those are flagged LOW below and need phase-level verification.

## Standard Architecture

Ephemeral-environment systems almost always have the same skeleton: a **control plane** that owns intent and policy, an **execution engine** that does the provisioning (here GitHub Actions), a **runtime** (here Kubernetes), and a **feedback path** that brings observed reality back into the control plane. The product's value comes from the feedback path (observed state, drift, TTL), so that is where the design effort goes.

### System Overview

```
┌──────────────────────────────────────────────────────────────────────┐
│                         PRESENTATION                                  │
│  ┌──────────────────────────┐                                         │
│  │ Web Dashboard (SPA)      │── deep links ──► Grafana (traces/logs)  │
│  └────────────┬─────────────┘                                         │
├───────────────┼──────────────────────────────────────────────────────┤
│               ▼          CONTROL PLANE (one deployable, modular)      │
│  ┌──────────────────────────────────────────────────────────────┐    │
│  │ API  (authn/z, REST, SSE to dashboard)                        │    │
│  └───┬─────────────┬───────────────┬────────────────┬───────────┘    │
│      ▼             ▼               ▼                ▼                 │
│ ┌─────────┐  ┌───────────┐  ┌─────────────┐  ┌────────────────┐      │
│ │Guardrail│  │Environment│  │ GitHub      │  │ Status         │      │
│ │ Policy  │  │ Service   │  │ Integration │  │ Aggregator     │      │
│ │ Engine  │  │ (lifecycle│  │ (App,       │  │ (live = life-  │      │
│ │ (schema,│  │  state    │  │  dispatch,  │  │  cycle+health, │      │
│ │ quota,  │  │  machine) │  │  webhooks)  │  │  drift)        │      │
│ │ RBAC)   │  └─────┬─────┘  └──────┬──────┘  └───────▲────────┘      │
│ └─────────┘        │               │                 │                │
│        BACKGROUND WORKERS (same binary, leader-elected)               │
│  ┌──────────────┐ ┌───────────────┐ ┌─────────────────┐              │
│  │ Reconciler   │ │ K8s Observer  │ │ Health Prober   │              │
│  │ (TTL, drift, │ │ (informers on │ │ (HTTP checks    │              │
│  │  orphan GC,  │ │  namespaces/  │ │  vs. env URLs)  │              │
│  │  poll Actions)│ │  deploys/pods)│ │                 │              │
│  └──────────────┘ └───────────────┘ └─────────────────┘              │
├──────────────────────────────────────────────────────────────────────┤
│                           STATE                                       │
│  ┌──────────────┐  ┌──────────────┐  ┌───────────────────────┐       │
│  │ Postgres     │  │ Job queue /  │  │ Audit log (table,     │       │
│  │ (envs, events│  │ outbox (in   │  │ append-only)          │       │
│  │  templates)  │  │ Postgres)    │  │                       │       │
│  └──────────────┘  └──────────────┘  └───────────────────────┘       │
└──────────────────────────────────────────────────────────────────────┘
        │ workflow_dispatch              ▲ webhooks (workflow_run,
        ▼                                │ workflow_job, deployment_status)
┌──────────────────────────────────────────────────────────────────────┐
│ GITHUB: Actions workflows (create / update / destroy), reusable       │
│ workflows + approved templates, GitHub App installation               │
└───────────────┬──────────────────────────────────────────────────────┘
                ▼ helm/kubectl/OIDC
┌──────────────────────────────────────────────────────────────────────┐
│ KUBERNETES CLUSTER                                                    │
│  ns: env-<id>  (labels: ephematool/env-id, owner, expires-at;         │
│                 ResourceQuota, LimitRange, NetworkPolicy)             │
│   ├ app services + deps    ├ OTel env config (injected)               │
│  ns: ephematool-system: control plane, OTel Collector gateway,        │
│                         (optional) OTel Operator                      │
└───────────────┬──────────────────────────────────────────────────────┘
                ▼ OTLP
┌──────────────────────────────────────────────────────────────────────┐
│ OBSERVABILITY: Collector gateway ► Tempo (traces) / Prometheus or     │
│ Mimir (metrics) / Loki (logs) ► Grafana                               │
└──────────────────────────────────────────────────────────────────────┘
```

### Component Responsibilities

| Component | Responsibility | Typical Implementation |
|-----------|----------------|------------------------|
| Dashboard | Inventory, create form (schema-driven), extend/destroy, per-env health/telemetry views | SPA (React or similar) talking to the API; SSE/WebSocket or short polling for live status |
| API | AuthN (GitHub OAuth via the App), AuthZ (RBAC by team/repo), request validation, command endpoints (create/extend/destroy) and query endpoints | REST/JSON, OpenAPI-described; thin handlers delegating to services |
| Guardrail Policy Engine | Validates create/update requests *before* any workflow is dispatched: JSON-Schema input check, approved-template check, size caps, per-team/repo concurrency quota, TTL max, who may create/destroy, secret scope rules | In-process library module (JSON Schema + plain code). OPA/Kyverno is overkill for v1; use Kubernetes ResourceQuota/LimitRange as the *enforcement backstop* in the cluster |
| Environment Service | Owns the environment aggregate and its lifecycle state machine (requested → provisioning → running → failed → destroying → destroyed); writes events | Domain module over Postgres; every transition is an append-only event row plus a current-state row |
| GitHub Integration | Dispatches workflows, receives and verifies webhooks, maps runs to environments, holds installation tokens | GitHub App (not PAT). Octokit-style client; webhook endpoint verifies `X-Hub-Signature-256`, dedupes on `X-GitHub-Delivery`, persists then acks fast |
| K8s Observer | Watches namespaces/Deployments/Pods carrying the `ephematool/env-id` label and reports observed state | client-go / client-node informer (list+watch with resync), read-only RBAC |
| Health Prober | Real end-to-end checks against each environment's URL(s) from the control plane's vantage point | Scheduled worker, per-env check config from the template (path, expected status, timeout); emits OTel metrics and result rows |
| Status Aggregator | Computes the "live" status: lifecycle state (Actions) AND observed state (K8s) AND probe result; flags drift | Pure function over the three inputs, recomputed on any input change; stored as derived columns plus a drift reason |
| Reconciler | Level-triggered loop: expire TTLs, detect drift (DB says running, cluster has nothing; cluster has labeled namespace DB doesn't know), garbage-collect orphans, poll Actions for runs whose webhooks were missed | Periodic worker with leader election (lease) if more than one replica |
| State store | Source of truth for intent, history, templates, audit | Postgres; queue/outbox in Postgres (e.g. a `SKIP LOCKED` jobs table) to avoid a broker in v1 |
| OTel Collector (gateway) | Single OTLP ingress for the tool and all environments; enriches, filters, batches, routes to backends | Collector deployment in `ephematool-system`; contrib distribution for k8sattributes, resource, filter, batch, memory_limiter |
| Telemetry backends | Store and query traces, metrics, logs | Grafana LGTM-style OSS: Tempo + Prometheus (or Mimir) + Loki + Grafana |
| Env OTel injection | Make every provisioned stack emit telemetry with consistent identity without app teams doing wiring | Done by the *provisioning workflow/template* (see Pattern 4), not by the control plane at runtime |

## Recommended Project Structure

Stack language is not yet decided (STACK.md decides). Structure is language-neutral; names map to packages/modules in a **modular monolith**.

```
ephematool/
├── apps/
│   ├── api/                  # HTTP server, auth, routing, SSE; composition root
│   ├── worker/               # entrypoint for reconciler, observer, prober (same codebase as api, separate process/flag)
│   └── dashboard/            # SPA
├── packages/ (or internal/)
│   ├── environments/         # aggregate, state machine, events, repository
│   ├── guardrails/           # schema validation, quotas, RBAC rules, TTL policy
│   ├── github/               # App auth, dispatcher, webhook handler, run<->env correlation
│   ├── kube/                 # informers, label conventions, namespace/quota helpers
│   ├── health/               # prober, check definitions
│   ├── status/               # lifecycle x observed x probe -> live/drift
│   ├── reconciler/           # TTL, drift, orphan GC, missed-webhook poll
│   ├── telemetry/            # OTel SDK bootstrap, shared metric/span helpers, semantic attribute names
│   └── store/                # migrations, queue/outbox, audit log
├── templates/                # approved environment templates + their JSON Schemas + health-check defs
├── workflows/                # reusable GH Actions workflows (create/update/destroy) the tool dispatches
├── deploy/
│   ├── helm/                 # control plane chart
│   ├── otel/                 # collector config, (optional) operator + Instrumentation CRs
│   └── observability/        # Tempo/Prometheus/Loki/Grafana dev stack, dashboards-as-code
└── docs/
```

### Structure Rationale

- **One backend codebase, two process roles (api, worker):** keeps deploy simple while letting background loops scale or restart independently of request handling.
- **`telemetry/` as its own package from commit one:** the project constraint says OTel cannot be retrofitted. Every other package imports its helpers, so spans/metrics are present from the first feature.
- **`templates/` and `workflows/` in the repo:** approved templates plus reusable workflows are the guardrail's "approved path". Versioning them with the tool means schema and workflow stay in lockstep.
- **`status/` isolated and pure:** the live/drift logic is the product's core claim; keeping it a side-effect-free function makes it exhaustively testable.

## Architectural Patterns

### Pattern 1: Desired state in the DB, observed state from the cluster, reconcile the gap

**What:** The DB records intent (environment requested, TTL expiry). Observers record what is actually true (workflow conclusion, namespace/pod state, probe result). A level-triggered reconciler compares them periodically and on events, rather than reacting once to edge events.
**When to use:** Always here. Webhooks get lost, workflows are cancelled mid-flight, namespaces are deleted by hand; edge-only designs rot.
**Trade-offs:** Slightly more code than "webhook updates row". Pays back in drift detection, which is a stated requirement.

```typescript
// status/derive.ts: pure and exhaustive
type Lifecycle = 'provisioning' | 'running' | 'failed' | 'destroying' | 'destroyed';
type Observed  = { nsExists: boolean; workloadsReady: boolean | null };
type Probe     = { ok: boolean; at: Date } | null;

export function deriveLive(l: Lifecycle, o: Observed, p: Probe) {
  if (l === 'running' && (!o.nsExists || p?.ok === false))
    return { live: false, drift: 'lifecycle_says_running_but_unhealthy' };
  if (l === 'destroyed' && o.nsExists)
    return { live: false, drift: 'destroyed_but_namespace_remains' };
  return { live: l === 'running' && p?.ok === true, drift: null };
}
```

### Pattern 2: Dispatch-and-correlate for GitHub Actions

**What:** `workflow_dispatch` is fire-and-forget: historically it returned 204 with no run ID. Generate an `env_id` and `correlation_id` in the control plane, pass them as workflow inputs, put the correlation ID in the workflow's `run-name`, then match incoming `workflow_run` webhooks (or a list-runs poll) back to the environment. (LOW confidence: GitHub may have changed the dispatch API to return a run ID recently; verify in phase research and use it if present, but keep correlation as the fallback.)
**When to use:** Every create/update/destroy.
**Trade-offs:** Needs idempotent webhook handling and a poll fallback; avoids brittle "find the latest run" races.

```yaml
# workflows/create-env.yml
on:
  workflow_dispatch:
    inputs: { env_id: {required: true}, correlation_id: {required: true},
              template: {required: true}, params_json: {required: true},
              traceparent: {required: false}, expires_at: {required: true} }
run-name: "create-env ${{ inputs.env_id }} ${{ inputs.correlation_id }}"
```

### Pattern 3: Labels as the contract between tool, workflow, and cluster

**What:** Every namespace the workflow creates is stamped with `ephematool/env-id`, `ephematool/owner`, `ephematool/template`, `ephematool/expires-at`, plus `ResourceQuota`/`LimitRange`. The K8s Observer and the orphan GC find environments solely by label; the workflow is the only writer, the tool is read-only against the cluster (except possibly a break-glass delete).
**When to use:** v1.
**Trade-offs:** Convention-based rather than typed. Preferred over a CRD/operator in v1 (see Anti-Patterns). Revisit a CRD if multi-cluster or in-cluster self-service becomes a goal.

### Pattern 4: Telemetry identity injected by the template, collected by one gateway

**What:** Each approved template includes an OTel config fragment: `OTEL_EXPORTER_OTLP_ENDPOINT` pointing at the in-cluster Collector gateway, `OTEL_SERVICE_NAME`, and `OTEL_RESOURCE_ATTRIBUTES` containing `ephemeral.env.id`, `ephemeral.env.owner`, `ephemeral.env.template`, `vcs.ref` / PR number. Auto-instrumentation is optional per-service via the OTel Operator `Instrumentation` CR (annotation-based injection) created in the env namespace by the workflow. The Collector additionally applies `k8sattributes` so telemetry from apps that ignore the env vars is still tagged by namespace-derived labels.
**When to use:** Every environment; this is what delivers the core value ("observable from the moment it exists").
**Trade-offs:** Env-id as a resource attribute on metrics creates series churn (high-cardinality, short-lived). Mitigate with short retention for env-scoped metrics and by aggregating with namespace as the primary dimension. Gateway is a shared dependency: apply `memory_limiter`, queueing, and per-namespace rate limits so one noisy env cannot starve others.

### Pattern 5: The tool eats its own telemetry (and tells the CI story)

**What:** The control plane uses the OTel SDK (traces, metrics, logs via OTLP to the same Collector). Domain spans: `env.create` (root, spans dispatch → webhook → ready → first healthy probe), `guardrail.evaluate`, `reconcile.tick`, `probe.check`. Propagate `traceparent` into the workflow input so the CI run joins the same trace. Domain metrics: `ephematool.environments` (gauge by state/team), `ephematool.env.time_to_healthy`, `ephematool.probe.duration`, `ephematool.reconcile.drift_detected`, `ephematool.webhook.lag`, `ephematool.env.ttl_remaining` (or expiry timestamp). The Collector's GitHub receiver (contrib, workflow_job webhooks to traces) is a candidate for CI spans; (LOW confidence on its maturity: verify, alternatives are the otel-cicd action or emitting spans from the tool upon webhook receipt).
**When to use:** From the first phase.
**Trade-offs:** Slight instrumentation tax per feature; this is the cost the project has explicitly accepted.

## Data Flow

### Create Flow (request)

```
SRE clicks "Create"
    ↓
Dashboard → API → AuthZ → Guardrail Policy (schema, template, quota, TTL)
                              │ reject → 4xx with field-level reasons
                              ▼
                 Environment Service: insert env(state=requested, expires_at), event, outbox
                              ▼
                 GitHub Integration: workflow_dispatch(inputs incl. env_id, correlation_id, traceparent)
                              ▼
                 GitHub Actions: create ns+quota+labels, helm install stack, inject OTel config
                              ▼ webhooks (workflow_run/job, deployment_status)
                 GitHub Integration → verify+dedupe → persist → Environment Service transition
                              ▼
        K8s Observer (workloads ready) + Health Prober (app answers) → Status Aggregator → live=true
                              ▼
                 SSE → Dashboard updates
```

### Teardown Flow

```
TTL elapses (Reconciler)  OR  user clicks Destroy  OR  PR closed webhook
    ↓
Environment Service: state=destroying → dispatch destroy workflow (same correlation scheme)
    ↓
Workflow deletes namespace/resources → workflow_run completed
    ↓
Reconciler verifies namespace gone (K8s Observer) → state=destroyed
    ↓ if namespace still present after grace period: drift flag + retry dispatch + alert
```

### Telemetry Flow

```
Env pods (OTLP, env-id resource attrs) ──┐
Control plane SDK (traces/metrics/logs) ─┼─► Collector gateway ─► k8sattributes/resource/filter/batch
Collector k8s_cluster/kubeletstats recvr ┘                    ├─► Tempo
                                                                ├─► Prometheus/Mimir
                                                                └─► Loki
Dashboard: summary tiles/sparklines query Prometheus/Loki/Tempo APIs via the API (server-side, scoped by env-id);
           "open in Grafana" deep links for full exploration.
```

### State Management

```
Postgres (envs, env_events, templates, probe_results, audit)
    ↓ (API reads; SSE pushes change notifications via LISTEN/NOTIFY or in-process bus)
Dashboard store (query cache) ←→ commands (create/extend/destroy) → API
```

### Key Data Flows

1. **Command path (dashboard → GitHub → cluster):** policy-checked intent flows down; nothing touches the cluster directly from the tool.
2. **Observation path (cluster/GitHub/prober → control plane):** three independent signals converge in the Status Aggregator; no signal is trusted alone.
3. **Telemetry path (everything → Collector → backends):** one OTLP ingress, one place to enforce identity attributes and limits.
4. **Dashboard telemetry views:** dashboard never queries backends directly; the API proxies with env-scoping so access policy applies to telemetry too.

## Scaling Considerations

| Scale | Architecture Adjustments |
|-------|--------------------------|
| Tens of envs, one team (v1 reality) | Single API replica + one worker, single cluster, in-cluster Collector, single-node-ish LGTM. Postgres jobs table instead of a broker |
| Hundreds of envs, many teams | 2+ API replicas, leader-elected worker, informer caches with label selectors, GitHub rate-limit budgeting (webhooks primary, polling minimized), Collector horizontally scaled with load-balancing exporter if tail sampling is added |
| Multiple clusters | Introduce per-cluster agent (observer + prober) reporting to the control plane, or cluster registry with scoped credentials; consider a CRD at that point |

### Scaling Priorities

1. **First bottleneck:** GitHub API rate limits and webhook gaps. Fix: webhook-first with GitHub App installation tokens, conditional requests, bounded reconcile polling.
2. **Second bottleneck:** metric series churn from short-lived env-id labels. Fix: short retention for env-scoped series, limit label promotion, drop unneeded attributes in the Collector.

## Anti-Patterns

### Anti-Pattern 1: Trusting the workflow conclusion as "healthy"

**What people do:** Mark an environment live when the Actions run is green.
**Why it's wrong:** Green means helm returned; pods may crash-loop, ingress may 502. This is exactly the drift the project exists to catch.
**Do this instead:** Lifecycle and health are separate fields; `live` is derived from both; disagreement is surfaced as a first-class drift flag.

### Anti-Pattern 2: Webhook-only state updates

**What people do:** Update env state solely in webhook handlers.
**Why it's wrong:** Lost/duplicated/out-of-order deliveries leave environments stuck in "provisioning" forever.
**Do this instead:** Idempotent handlers keyed on delivery ID and run ID, monotonic transitions, plus reconciler polling for stale non-terminal states.

### Anti-Pattern 3: Building an operator/CRD in v1

**What people do:** Model each environment as a CRD with a controller that provisions it.
**Why it's wrong:** It competes with the stated premise (GitHub Actions is the provisioning engine) and doubles the surface area (CRD versioning, RBAC, controller lifecycle) before the core value is proven.
**Do this instead:** DB as intent store, labels as the cluster contract, read-only informer. Keep the Environment Service behind an interface so a CRD-backed implementation can be added later.

### Anti-Pattern 4: Control plane holding broad cluster-admin and long-lived secrets

**What people do:** Give the dashboard backend a cluster-admin kubeconfig and store PATs.
**Why it's wrong:** It is the highest-value target in the platform and conflicts with the access/secrets guardrail.
**Do this instead:** GitHub App with minimal permissions; Actions authenticates to the cluster via OIDC with a scoped role; the control plane's own K8s access is read-only on labeled objects; secrets are scoped per environment and never stored in the tool's DB.

### Anti-Pattern 5: Per-environment observability stacks

**What people do:** Deploy Prometheus/Loki/Grafana inside every preview namespace.
**Why it's wrong:** Heavy, slow to provision, destroys telemetry at teardown (exactly when you want the post-mortem), and wrecks cost guardrails.
**Do this instead:** Shared backends and one Collector gateway; environments only ship OTLP. Retain telemetry for a short period after the environment is destroyed.

### Anti-Pattern 6: Retrofitting telemetry

**What people do:** Add tracing after the features exist.
**Why it's wrong:** Context propagation (dashboard → API → GitHub → workflow → cluster) and attribute naming conventions are cross-cutting; changing them later touches everything.
**Do this instead:** Build `telemetry/` and the Collector pipeline first; make "emits spans and metrics" part of every feature's definition of done.

## Integration Points

### External Services

| Service | Integration Pattern | Notes |
|---------|---------------------|-------|
| GitHub (App) | OAuth login, installation tokens, `workflow_dispatch`, webhooks: `workflow_run`, `workflow_job`, `deployment_status`, `pull_request` (closed triggers teardown) | Webhook ingress needs a public URL (tunnel for dev). Verify signatures; dedupe delivery IDs; ack in <10s then process async. Dispatch only works for workflows on a ref where the file exists with `workflow_dispatch` trigger |
| GitHub Actions to Kubernetes | OIDC-federated short-lived credentials to the cluster | Workflow, not the tool, owns writes to the cluster |
| Kubernetes API | Read-only list/watch on Namespaces, Deployments, Pods, Ingress by label selector | Use informers with resync; do not poll `kubectl`-style |
| Environment URLs | HTTP probes from the control plane | Probe from inside the cluster for v1; ingress/DNS failures are a distinct failure class worth distinguishing (result includes DNS/connect/TLS/status phases) |
| OTel Collector / backends | OTLP gRPC/HTTP | Collector config as code; backends swappable behind OTLP |

### Internal Boundaries

| Boundary | Communication | Notes |
|----------|---------------|-------|
| Dashboard ↔ API | REST + SSE | API is the only thing the browser talks to (including telemetry queries) |
| API ↔ Guardrails | In-process call | Guardrails are pure and synchronous; reused by the reconciler for extension requests |
| Environment Service ↔ GitHub Integration | Outbox/job queue in Postgres | Decouples user request from dispatch and retries; dispatch is idempotent via correlation_id |
| Observers/Prober → Status Aggregator | Write observation rows, emit change event | Aggregator is the only writer of derived `live`/`drift` |
| Control plane ↔ Collector | OTLP | Same path as environments; no special case |

## Suggested Build Order (roadmap implications)

Dependencies drive the order; observability-first means telemetry is in the foundation, not at the end.

1. **Foundation: skeleton + telemetry spine.** Repo, Postgres + migrations, API/worker skeleton, `telemetry/` package, Collector gateway, minimal LGTM dev stack, a "tool emits traces/metrics/logs" smoke test. (Everything else depends on it; satisfies "OTel from day one".)
2. **Environment aggregate + GitHub integration.** GitHub App, auth, `workflow_dispatch` with correlation, webhook ingress (verify/dedupe/persist), lifecycle state machine, reusable create/destroy workflows against a kind/test cluster. Trace propagation into workflows.
3. **Cluster observation + inventory.** K8s Observer, label contract, inventory UI with lifecycle state. Reconciler v0 (poll for missed webhooks).
4. **Health prober + Status Aggregator.** Live/not-live, drift flags, probe metrics. This delivers the central product claim.
5. **Per-environment OTel injection.** Template OTel fragment, resource attributes, optional Operator auto-instrumentation, `k8sattributes`; dashboard per-env telemetry panels and Grafana deep links. (Could start earlier in thin form alongside 2, since it lives in the workflow templates; full dashboard views come after 4.)
6. **Guardrails.** TTL + teardown reconciler first (highest user pain, reuses destroy flow), then validated config/templates with schemas, then size/cost quotas (policy + ResourceQuota backstop), then access/secrets policy. Schema/template validation is naturally added at the create path in phase 2 in minimal form and hardened here.
7. **Management actions + polish.** Extend/configure/destroy UI flows, audit log views, notifications for expiring/drifting envs.

Ordering notes:
- Guardrails split: *minimal* validation (schema + a hard TTL default) should land with creation (phase 2), otherwise early environments leak. Full guardrail suite comes after observation exists, because quota and TTL enforcement need reliable observed state.
- Reconciler grows in layers (missed-webhook poll → drift → TTL → orphan GC); don't wait to build it all at once.
- Anything touching multi-cluster, CRD/operator, or non-K8s targets is explicitly outside v1.

## Research Flags

- **GitHub dispatch/run correlation (phase 2):** verify whether `workflow_dispatch` now returns a run ID, webhook event payload fields, and rate-limit behavior. LOW confidence here.
- **Collector GitHub receiver / CI tracing (phase 2 or 5):** verify current stability of contrib `githubreceiver` and the otel-cicd-action; decide trace-per-workflow strategy.
- **OTel Operator auto-instrumentation scope (phase 5):** language coverage and per-namespace behavior; may be limited to a subset of app runtimes. Confirm against app stacks used.
- **Metric cardinality/retention strategy (phase 5):** needs a concrete design once backend (Prometheus vs Mimir) is chosen in STACK.md.
- **Standard patterns, low need for research:** Postgres state/outbox, informer-based observer, TTL reconciler, JSON-Schema validation.

## Sources

- Kubernetes controller/informer pattern and level-triggered reconciliation: kubernetes.io docs (not re-verified in this pass; MEDIUM)
- OpenTelemetry Collector gateway deployment pattern, k8sattributes processor, OTel Operator `Instrumentation` CRD: opentelemetry.io docs (not re-verified in this pass; MEDIUM)
- GitHub Apps, webhooks (`workflow_run`, `workflow_job`, `deployment_status`), `workflow_dispatch`: docs.github.com (not re-verified in this pass; MEDIUM, dispatch run-ID behavior LOW)
- Prior art for shape: Argo CD ApplicationSet PR generator, Backstage, Qovery/Uffizzi-style preview environment platforms (training knowledge; LOW-MEDIUM)

---
*Architecture research for: ephemeral preview-environment management platform*
*Researched: 2026-10-11*


---

# Running Ephematool (Phase 1: telemetry spine and GitHub login)

## Run locally

Prerequisites: Docker 27+ with Compose v2, Bazel (via Bazelisk; the version is pinned in `.bazelversion`), Go 1.26, Node 24.

```
bazel run //:up_fake   # whole stack with a fake GitHub (no credentials needed)
bazel run //:up        # whole stack with real GitHub sign-in (see below)
bazel run //:down      # stop the stack (bazel run //:clean also deletes volumes)
```

- Dashboard: http://localhost:8080
- Grafana: http://localhost:3000 (provisioned "Ephematool platform" dashboard in the Ephematool folder; Explore links logs to traces and back)

All 10 services (API, worker, dashboard, Postgres, Redis, OTel Collector, Tempo, Loki, Prometheus, Grafana) start together and report on the dashboard's Telemetry status page.

## GitHub sign-in setup

1. Create an OAuth App at https://github.com/settings/developers with:
   - Homepage URL `http://localhost:8080`
   - Authorization callback URL `http://localhost:8080/api/auth/github/callback`
   - The app requests the `read:org` scope so private org membership can be checked.
2. Copy `.env.example` to `.env` (git-ignored) and set `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET`, `ALLOWED_ORGS`, `ALLOWED_TEAMS` and `ADMIN_LOGINS`. Members of an allowed org or team can sign in; logins in `ADMIN_LOGINS` get the Admin role and everyone else is a Viewer.
3. If the org restricts third-party OAuth apps, an org owner must approve the OAuth App under the org's Settings, Third-party access (OAuth app policy). Until then the org is not visible to the sign-in check and members are denied.
4. `bazel run //:down && bazel run //:up`, open http://localhost:8080 and choose "Continue with GitHub".

## Fake GitHub (dev and e2e)

`bazel run //:up_fake` adds `deploy/compose.e2e.yaml`, which points the API at a fake GitHub server on http://localhost:9999. Its sign-in page lists fixture logins: `octo-admin` (org member, Admin), `octo-viewer` (org member, Viewer), `octo-teamonly` (member of the allowed team only), `octo-pending` (pending org membership, denied), `octo-outsider` (non-member, denied) and `octo-forbidden` (org lookup forbidden, denied). Fake mode is for development and tests only and must never be used for a real deployment.

## Verification

```
./tools/verify.sh                          # integration tests, unit tests, stack up, Playwright, all telemetry checks
bazel run //:verify_fresh                  # fresh clone starts the stack with one command, all 10 services healthy
bazel run //:verify_telemetry              # every telemetry check against a running stack
bazel run //:verify_telemetry -- redaction # one subcommand (scripts/verify-telemetry.sh <subcommand>)
```

Subcommands: backends, conventions, dashboard, logs, metrics, redaction, status, trace, up, worker, auth-denied (and fresh via `//:verify_fresh`). After changing Go imports run `bazel run //:gazelle`; after `go get` run `bazel mod tidy`.

## Security notes (dev stack only)

- Grafana runs with anonymous Admin access and no login form. This is for the local stack only; never expose it beyond localhost.
- Every published port is bound to 127.0.0.1.
- `COOKIE_SECURE=false` by default so the session cookie works over plain HTTP on localhost. Set `COOKIE_SECURE=true` for any non-localhost deployment (TLS required).
- Postgres and Redis use dev-only credentials and are not published to the host.
- Telemetry must not carry secrets: `bazel run //:verify_telemetry -- redaction` checks Tempo and Loki for cookie values, tokens and header attributes after a login. Redis command text is deliberately not recorded in spans.
- `.env` holds the GitHub client secret and is git-ignored. Never commit it.
