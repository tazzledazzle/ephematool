# Feature Research

**Domain:** Ephemeral / preview environment management platform (Kubernetes, GitHub Actions-provisioned, observability-first)
**Researched:** 2026-10-11
**Confidence:** MEDIUM (landscape drawn from training knowledge of Argo CD ApplicationSet PR generator, Okteto, Uffizzi, Qovery, Release, Garden, Backstage plugins; no live re-verification of current vendor feature sets this session. Vendor feature details are LOW-MEDIUM; the table-stakes/differentiator split is a judgment call.)

## Feature Landscape

### Table Stakes (Users Expect These)

| Feature | Why Expected | Complexity | Notes |
|---------|--------------|------------|-------|
| Create environment from branch/PR/ref | Core purpose; every competitor does it (ApplicationSet PR generator, Okteto, Release, Qovery) | MEDIUM | Dashboard form triggers GitHub Actions `workflow_dispatch` with validated inputs. Needs run-ID correlation back to the environment record. |
| Environment inventory with status | Every platform has a list view | LOW | Name, repo/ref, owner, created, expires, status. Backed by DB, not scraped live. |
| Lifecycle states (provisioning/running/failed/destroyed) | Users need to know if creation worked | MEDIUM | Derive from GH Actions runs + webhooks; reconcile by polling (webhooks get lost). |
| Destroy environment (manual) | Cleanup is the whole point | MEDIUM | Trigger teardown workflow; verify namespace actually gone (not just workflow green). |
| Environment URL / access link | Previews exist to be viewed | LOW | Show ingress URL once health-verified. |
| Auto-teardown on PR close/merge | Standard in Argo CD PR generator, Release, Uffizzi | MEDIUM | GitHub webhook -> destroy; backstop for TTL. |
| Idle/TTL expiry with extension | Prevents lingering envs (stated pain) | MEDIUM | Default TTL on every env; extend action; expiry warnings. Reaper must be idempotent and survive tool restarts. |
| Logs/status/events view for an env | Debugging failed previews | MEDIUM | Link to/stream GH Actions logs and k8s events; OTel logs views. |
| Resource limits (CPU/mem/replicas) | ResourceQuota/LimitRange are expected hygiene | MEDIUM | Enforce via namespace ResourceQuota + validated template caps. |
| Max concurrent envs per team/repo | Cost control; common in hosted platforms | LOW-MEDIUM | Check at create time, atomically (avoid race). |
| Authn/authz (who can create/destroy) | SRE tool with cluster power | MEDIUM | GitHub OAuth/SSO + roles (viewer/creator/admin). |
| Secrets scoped per environment | Previews must not share prod secrets | MEDIUM-HIGH | Reference external secret store/ExternalSecrets; never display values; no prod data. |
| Template-based config | Okteto/Release/Garden all use manifests/templates | MEDIUM | Approved templates with JSON-schema-validated inputs. |
| Audit log of actions | Expected for any privileged tool | LOW-MEDIUM | Who created/extended/destroyed, when. |
| Namespace-per-env isolation | Standard k8s isolation | LOW-MEDIUM | NetworkPolicy default-deny baseline. |

### Differentiators (Competitive Advantage)

Align with Core Value: every env observable from the moment it exists.

| Feature | Value Proposition | Complexity | Notes |
|---------|-------------------|------------|-------|
| Auto-injected OTel (collector/SDK config, resource attributes `env.id`, `env.team`, `env.ttl`) per env | Telemetry from second zero; most competitors only link out to logs | HIGH | Per-env OTel Collector or shared gateway with env attributes; core architecture decision. |
| Tool self-instrumentation (traces/metrics/logs of the platform) | Credible observability-first story; traces create->workflow->ready | MEDIUM | Trace spanning dashboard click -> GH run -> deploy -> first healthy check. |
| "Live" = lifecycle AND real health check, with drift flag | Green workflow != healthy app; rare in competitors | HIGH | Synthetic probes + k8s readiness + OTel signals compared with lifecycle state; drift categories (workflow says running but unhealthy, resources exist but record says destroyed, orphaned namespaces). |
| Drift/orphan detection (cluster vs inventory) | Finds lingering envs nobody tracks | MEDIUM-HIGH | Periodic reconcile of labeled namespaces vs DB. |
| Time-to-ready and provisioning-duration metrics | Quantify env quality over time | MEDIUM | Falls out of self-instrumentation. |
| Per-env health/telemetry dashboard (RED metrics, error rate, recent traces) | One place to judge a preview | HIGH | Needs queryable backend (Prometheus/Tempo/Loki or similar) and embedded views/deep links. |
| Cost visibility per env (estimated from requests x duration) | Makes size/cost guardrails tangible | MEDIUM | Estimate from requests, not billing integration. |
| Guardrail policy surfaced in UI ("why was this rejected") | Reduces manual back-and-forth | MEDIUM | Inline validation errors, remaining quota, TTL countdown. |
| Expiry warnings (Slack/GitHub PR comment) | Prevents surprise teardown | LOW-MEDIUM | PR comment with URL and expiry is a known pattern (Uffizzi, Release). |
| Policy-as-code for admission (Kyverno/OPA-style) as enforcement backstop | Guardrails enforced even if the dashboard is bypassed | HIGH | Defer; v1 can rely on ResourceQuota + validation at create. |

### Anti-Features (Commonly Requested, Often Problematic)

| Feature | Why Requested | Why Problematic | Alternative |
|---------|---------------|-----------------|-------------|
| Reimplementing CI/CD / build pipelines | "Do everything in one tool" | Out of scope; huge surface | Orchestrate and observe GH Actions only |
| Free-form YAML/manifest editor | Flexibility | Defeats validated-config guardrail | Schema-checked inputs on approved templates |
| Non-k8s targets (Compose, VMs, static previews) | Breadth | Dilutes provisioning and observability model | k8s only in v1 |
| Primary CLI | Developer preference | Splits effort; dashboard is the surface | Defer; expose API first |
| Dev-in-cluster inner loop (file sync, remote dev like Okteto/Garden) | Developer appeal | Different product (dev tools vs SRE ops) | Stay preview/ops-focused |
| Production data cloning / snapshot copy | Realistic previews | Violates "no prod data in previews"; compliance risk | Seeded/synthetic fixtures |
| Building a full observability backend/UI | Self-contained | Reinventing Grafana | OTel export + embed/deep-link to existing backends; minimal summary views |
| Auto-extend TTL on activity by default | Convenience | Reintroduces lingering envs | Explicit extension, capped max lifetime |
| Multi-cluster / multi-cloud scheduling | Scale | Complexity premature | Single cluster in v1 |
| Billing-accurate cost accounting | Finance asks | Needs cloud billing integrations | Request-based estimates |
| Real-time everything (websockets for all views) | Slick feel | Cost without value | Polling/SSE for status only |

## Feature Dependencies

```
Push-button create
    └──requires──> Validated config (templates + schema)
    └──requires──> AuthN/AuthZ
    └──requires──> Environment record + GH run correlation
                       └──requires──> Webhook + reconcile polling

Lifecycle state ──requires──> Environment record + GH run correlation
Health checks ──requires──> Env URL/endpoints + Lifecycle state
"Live" + drift ──requires──> Lifecycle state AND Health checks
Orphan detection ──requires──> Namespace labeling convention + inventory

TTL auto-teardown ──requires──> Destroy workflow + reaper job + inventory
Expiry warnings ──enhances──> TTL
Auto-teardown on PR close ──requires──> GitHub webhook ingestion

Size/cost limits ──requires──> Templates (caps) + env/team ownership model
Cost visibility ──enhances──> Size/cost limits

Secrets policy ──requires──> AuthZ + secret store integration
Per-env OTel ──requires──> Provisioning template injection + collector pipeline
Per-env observability views ──requires──> Per-env OTel + telemetry backend
Tool self-instrumentation ──precedes──> everything (day-one, cannot retrofit)

Free-form config editor ──conflicts──> Validated config
Auto-extend TTL ──conflicts──> TTL guardrail
```

### Dependency Notes

- **OTel foundation precedes all:** the tool emits traces/metrics/logs and templates inject env telemetry before create/manage features accrue, so later features are born instrumented.
- **Environment record + run correlation is the keystone:** lifecycle, live status, TTL, and destroy all hang off it.
- **Live+drift requires both signals:** lifecycle derivation and health probes must exist separately before drift can be computed.
- **TTL requires destroy:** reaper reuses the destroy path; build destroy first.
- **Validated config underpins guardrails:** size caps and secrets rules are expressed in templates/schema.

## MVP Definition

### Launch With (v1)

- [ ] Tool self-instrumentation with OTel (traces/metrics/logs) — core value, cannot retrofit
- [ ] Environment record, inventory, GH Actions run correlation, lifecycle states — keystone
- [ ] Per-env OTel injection via template + basic per-env health/telemetry view — core value
- [ ] Health checks and "live vs not live" with drift flag — stated v1 bar
- [ ] Push-button create from dashboard with schema-validated approved templates — creation stays simple
- [ ] Manual destroy, TTL default + extend + reaper, PR-close teardown — lingering-env pain
- [ ] Size limits (ResourceQuota, caps) and max concurrent per team/repo — guardrail
- [ ] AuthN/AuthZ roles, scoped secrets references, no-prod-data rule, audit log — guardrail
- [ ] Expiry warning (PR comment or notification), basic

### Add After Validation (v1.x)

- [ ] Orphan/namespace drift reconcile — once inventory trusted
- [ ] Cost estimates per env — after limits proven
- [ ] Deeper telemetry views, SLO-ish health scoring
- [ ] Slack notifications

### Future Consideration (v2+)

- [ ] Admission-controller policy enforcement (Kyverno/OPA)
- [ ] CLI, multi-cluster, non-k8s targets
- [ ] Backstage plugin/integration, GitOps (Argo CD) provisioning backend

## Feature Prioritization Matrix

| Feature | User Value | Implementation Cost | Priority |
|---------|------------|---------------------|----------|
| Tool OTel self-instrumentation | HIGH | MEDIUM | P1 |
| Env record + lifecycle derivation | HIGH | MEDIUM | P1 |
| Inventory (live vs not live) | HIGH | LOW | P1 |
| Health checks + drift flag | HIGH | HIGH | P1 |
| Per-env OTel injection | HIGH | HIGH | P1 |
| Push-button create + validated templates | HIGH | MEDIUM | P1 |
| Destroy + TTL reaper + extend | HIGH | MEDIUM | P1 |
| Size limits / concurrency caps | HIGH | MEDIUM | P1 |
| AuthZ + secrets policy + audit | HIGH | MEDIUM-HIGH | P1 |
| PR-close auto-teardown | MEDIUM | MEDIUM | P1 |
| Expiry warnings | MEDIUM | LOW | P2 |
| Orphan detection | MEDIUM | MEDIUM-HIGH | P2 |
| Cost estimates | MEDIUM | MEDIUM | P2 |
| Rich per-env telemetry dashboards | MEDIUM | HIGH | P2 |
| Admission policy backstop | MEDIUM | HIGH | P3 |

## Competitor Feature Analysis

| Feature | Argo CD ApplicationSet | Okteto / Garden | Uffizzi / Release / Qovery | Our Approach |
|---------|------------------------|-----------------|----------------------------|--------------|
| Trigger | PR generator polls/webhooks GitHub, templates an Application per PR | CLI/CI-driven preview, manifests | GitHub app, PR-driven, UI | Dashboard button + PR-close webhook |
| Teardown | App removed when PR closes | Sleep/destroy policies | TTL/idle sleep, PR close | TTL mandatory + PR close + manual |
| Config | Templated manifests | okteto.yml / garden.yml | Compose/Helm + UI vars | Approved templates + JSON schema |
| Observability | Sync/health status only | Logs, basic | Logs/status, links | OTel-native, health-verified live status |
| Guardrails | Via k8s quotas/RBAC (DIY) | Namespace quotas, some | Plan limits, team quotas | Four first-class guardrails in UI |
| Drift | Argo sync drift vs git | Little | Little | Lifecycle vs health vs cluster drift |

## Sources

- Argo CD ApplicationSet Pull Request generator docs (training knowledge; not re-fetched)
- Okteto, Uffizzi, Qovery, Release, Garden product docs (training knowledge; not re-fetched)
- Backstage plugin ecosystem (training knowledge)
- OpenTelemetry docs (training knowledge)
- Recommendation: validate competitor specifics via Context7/official docs in phase-specific research before committing to detail.

---
*Feature research for: ephemeral preview environment management*
*Researched: 2026-10-11*
