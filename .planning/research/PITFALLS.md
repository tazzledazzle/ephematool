# Pitfalls Research

**Domain:** Ephemeral preview-environment management platform (web dashboard + GitHub Actions provisioning + Kubernetes namespaces + OpenTelemetry)
**Researched:** 2026-10-11
**Confidence:** MEDIUM. Based on well-established platform behavior (GitHub Actions/webhooks, Kubernetes, OTel Collector, GitHub security guidance) from training knowledge. No live doc verification was done in this pass. Items marked [VERIFY] need a check against current official docs in the relevant phase.

## Critical Pitfalls

### Pitfall 1: Orphaned environments (leaked namespaces, PVCs, load balancers, DNS, cloud cost)

**What goes wrong:**
Environments outlive their purpose. The PR is closed, but the `closed` webhook was missed, the destroy workflow failed, or the namespace is stuck `Terminating`. Resources outside the namespace leak too: cloud LoadBalancers, PVs with `Retain` reclaim policy, DNS records, TLS certs, container images, and external databases or queues.

**Why it happens:**
Teardown is treated as the mirror image of create and assumed to succeed. Nothing independent checks that the world matches the inventory. Namespace deletion hangs on finalizers (CRDs whose controllers are gone, or unavailable aggregated APIs). Deleting the namespace is assumed to delete everything, but cluster-scoped objects and cloud resources are not namespaced.

**How to avoid:**
- Label every resource with `ephematool/env-id`, `owner`, `expires-at`, and `managed-by`, including cloud resources (tags). Do this at creation, from the one template layer.
- Run an independent reaper/sweeper that lists the cluster by label, not by the tool's database. Anything labeled but unknown to the DB, or expired, gets flagged (alert first, delete after a grace period).
- Make destroy idempotent and retried. Track `destroying` and `destroy_failed` as explicit states with alerting, never silently "destroyed".
- Surface stuck-`Terminating` namespaces and list the blocking finalizers and conditions. Never auto-strip finalizers without a human or an explicit policy.
- Prefer resources whose lifecycle is owned by the namespace (PVC `Delete` reclaim, in-cluster dependencies). Make cluster-scoped and cloud resources go through the same labeled path.
- Emit an "orphan count" and "estimated idle cost" metric. Cost visibility makes leaks visible.

**Warning signs:**
- Count of namespaces with the managed label exceeds count of non-destroyed DB rows.
- Namespaces `Terminating` for more than 10 minutes.
- Cloud bill or LB count creeping up while environment count is flat.
- Destroy workflow runs with failures nobody was paged for.

**Phase to address:**
Lifecycle/provisioning phase (labeling and idempotent destroy). Reconciler/sweeper in the same phase or immediately after. Cost metric in the observability phase.

---

### Pitfall 2: TTL races and extend/destroy ambiguity

**What goes wrong:**
A user extends an environment at the same moment the TTL job destroys it. Two replicas of the TTL controller both fire destroy. Clock skew or a timezone bug expires environments early. Extension is accepted after destroy started, so the UI shows "extended" for an environment that is gone. Environments with no TTL set (null, parse failure) live forever.

**Why it happens:**
TTL is implemented as a cron scan with read-then-write logic and no concurrency control. The expiry lives in one place (DB) while the effective expiry is elsewhere (annotation, workflow input).

**How to avoid:**
- Make `expires_at` a single source of truth, mirrored to a namespace annotation for the sweeper. Mandatory at creation; reject creation without a TTL and enforce a max TTL and max total extensions.
- Use compare-and-set transitions: `UPDATE ... SET state='expiring' WHERE id=? AND expires_at <= now() AND state='running'`. Extend uses the same row-version check, so exactly one of extend/expire wins.
- Use a single leader (DB advisory lock or Kubernetes Lease) for the TTL controller, and idempotent destroy so a double fire is harmless.
- Use server time only (UTC, DB `now()`), never client time. Add a short grace window and a "expiring soon" warning state.
- Treat a missing or unparseable expiry as expired-pending-review, not immortal.

**Warning signs:**
- Environments with `expires_at IS NULL` or far-future values.
- Duplicate destroy workflow runs for the same env.
- Support complaints of "it vanished right after I extended".

**Phase to address:**
Guardrails phase (TTL). The state machine should be designed in the lifecycle phase so TTL does not require rework.

---

### Pitfall 3: Treating webhooks as reliable, ordered, exactly-once

**What goes wrong:**
The inventory drifts because a webhook was dropped (receiver down during deploy, response over the ~10s timeout), delivered twice, or delivered out of order (`completed` processed before `in_progress`; `closed` before `synchronize`). A late event overwrites a newer state.

**Why it happens:**
GitHub delivery is at-least-once with no ordering guarantee, and failed deliveries are not automatically redelivered [VERIFY current redelivery behavior]. Developers write handlers as "event -> set state".

**How to avoid:**
- Respond 2xx fast: verify HMAC signature (`X-Hub-Signature-256`, constant-time compare, against the raw body), persist the raw event keyed on `X-GitHub-Delivery`, enqueue, return. Process asynchronously.
- Dedupe on delivery ID. Make handlers idempotent and monotonic: compare `run_attempt`, `updated_at`, or run status rank before applying; never regress state.
- Do not trust the webhook payload as truth. Treat it as a hint to re-fetch authoritative state from the API (run status, PR state).
- Add a periodic reconciliation poll (see Pitfall 4) and use the "recent deliveries" redelivery API to backfill after outages.
- Webhook ingestion must have a dead-letter path and an alert on lag.

**Warning signs:**
- Environments stuck `provisioning` though the run finished.
- Handler code that does `env.state = payload.status` unconditionally.
- No stored delivery IDs; no ingestion-lag metric.

**Phase to address:**
GitHub integration phase (ingestion design). Reconciliation phase for the backstop.

---

### Pitfall 4: State reconciliation bugs (three sources of truth)

**What goes wrong:**
There are at least three truths: the tool's DB, GitHub (runs, PRs), and the cluster. They diverge. The dashboard says "running" for an environment whose namespace is gone; or "destroyed" while pods still burn money. A refresh can flap states, or the tool "heals" by recreating something that was intentionally destroyed.

**Why it happens:**
Event-driven updates only, with no desired-vs-observed loop. No defined precedence about which source wins for which field.

**How to avoid:**
- Model it as desired state (what the user/guardrails want) vs observed state (workflow + cluster + health), with a reconcile loop like a Kubernetes controller: level-triggered, idempotent, re-run on interval and on events.
- Define field ownership: lifecycle intent = DB; provisioning outcome = Actions run conclusion; existence/readiness = cluster API; "healthy" = probes.
- Use an explicit state machine with legal transitions (`requested -> provisioning -> running | failed -> expiring -> destroying -> destroyed`) plus a `drift` flag and reason, not a single status string.
- Keep an append-only event/transition log for debugging and audit.
- Never auto-recreate on drift in v1; flag it, and make remediation an explicit action.

**Warning signs:**
- Status fields overwritten from multiple code paths.
- "Refresh" button needed for correct data.
- Bugs that only reproduce after a controller restart.

**Phase to address:**
Core lifecycle/state-model phase (design up front). Reconciler built alongside inventory.

---

### Pitfall 5: "Workflow succeeded" equals "environment healthy"

**What goes wrong:**
The Actions run is green (helm install returned, `kubectl apply` succeeded) but pods are CrashLooping, ingress has no endpoints, a migration job failed, or DNS/cert is not ready. The dashboard shows live. This is the project's core value, so getting it wrong defeats the product.

**Why it happens:**
Using run conclusion as health. Shallow health checks (a single TCP or `/healthz` that returns 200 from a default backend or a generic ingress error page). Checking once at creation only.

**How to avoid:**
- Separate `provisioned` (run conclusion) from `healthy` (continuous probes) in the model and UI. "Live" requires both, with drift flagged when they disagree in either direction (red run but healthy app, green run but unhealthy app).
- Health checks should combine: Kubernetes readiness (Deployments available, no restarts spike), and an external synthetic probe through the real ingress with an expected response signature (not just status 200; verify an app-specific header or body, to avoid hitting a default backend or wildcard DNS).
- Probe continuously with debounce (N consecutive failures) and a startup grace period so provisioning does not flap.
- Make the provisioning workflow wait on rollout (`kubectl rollout status`, `helm --wait`) with timeouts, but still do not trust it as the final word.
- Emit health as OTel metrics so it is visible and alertable.

**Warning signs:**
- Health defined as a field set once by the workflow callback.
- Probe hits the cluster-internal service but not the public URL users use.
- No tests with a deliberately broken-but-deployed app.

**Phase to address:**
Health/observability phase. Include a "green workflow, broken app" fixture in acceptance tests.

---

### Pitfall 6: Secrets exposure via fork PRs and `pull_request_target`

**What goes wrong:**
Previews built from PR code run with real secrets, or a "pwn request": a `pull_request_target` (or `workflow_run`) workflow checks out and builds/executes untrusted PR head code with write token and secrets, leaking cluster credentials or enabling cluster takeover. PR code in the preview namespace can also read the secrets mounted there.

**Why it happens:**
Fork PRs get no secrets under `pull_request`, so teams "fix" it by switching to `pull_request_target`, which runs in the base repo context with secrets while checking out attacker-controlled code. Templated workflow inputs (`${{ github.event.pull_request.title }}`, branch names) are script-injectable.

**How to avoid:**
- Default: do not create preview environments for fork PRs automatically. Require a maintainer approval/label gate (e.g., `safe-to-preview`) tied to a specific commit SHA, and re-approve on new pushes.
- Never check out or execute PR head code in a `pull_request_target`/`workflow_run` context that has secrets. Split into an unprivileged build job (no secrets, produces artifact/image) and a privileged deploy job that only consumes the artifact and trusted templates.
- Dispatch provisioning from the tool via `workflow_dispatch` with schema-validated inputs, and pass inputs through `env:` rather than interpolating into `run:` scripts. Validate branch/name against a strict regex (DNS-label safe).
- Pin third-party actions by commit SHA; set workflow `permissions:` to least privilege; use protected GitHub Environments for deploy jobs.
- Preview secrets are scoped, non-production, short-lived, and delivered per-namespace (external-secrets/sealed). Never copy production secrets or data into previews (this is a stated guardrail; enforce it in the template, not by convention).
- Mask secrets in logs, and make sure the tool's own logs/telemetry never record them (see Pitfall 9).

**Warning signs:**
- Any workflow with `pull_request_target` plus `actions/checkout` of `head.sha`/`head.ref`.
- `${{ github.event.* }}` inside `run:` blocks.
- Secrets in repo-level (not environment-scoped) scope used by preview jobs.

**Phase to address:**
Provisioning workflow phase (workflow design) and Access/secrets guardrail phase. Needs a security review before first real repo is connected.

---

### Pitfall 7: Over-privileged cluster access from Actions (long-lived kubeconfig, loose OIDC trust)

**What goes wrong:**
A cluster-admin kubeconfig stored as a GitHub secret; any workflow in the repo (including from PR branches) can use it. Or OIDC is set up but trust policy matches `repo:org/*`, so any repo/branch/PR can assume the deploy role.

**Why it happens:**
Kubeconfig-in-secret is the quickest path. OIDC trust conditions are fiddly and wildcards "just work".

**How to avoid:**
- Use GitHub OIDC federation to the cloud/cluster (short-lived tokens). Constrain the `sub` claim to exact repo plus a protected GitHub Environment (e.g., `repo:org/repo:environment:preview`) and validate `aud`. Avoid wildcard subs; avoid trusting `pull_request` subs for privileged roles.
- Grant namespace-scoped RBAC, not cluster-admin: the deploy identity can create namespaces only with the managed label/prefix and act only inside namespaces it created. Enforce with admission policy (ValidatingAdmissionPolicy or Kyverno/Gatekeeper): name prefix, required labels, quota present, no privileged pods, no hostPath/hostNetwork, allowed registries.
- Separate identities: the dashboard backend (reads cluster, triggers workflows) vs the workflow identity (writes). The dashboard needs read-mostly cluster access.
- Treat the dashboard's GitHub App/token like a credential with blast radius: use a GitHub App with minimal permissions (Actions: write, Pull requests: read, Metadata) rather than a personal PAT.

**Warning signs:**
- `KUBECONFIG` or `KUBE_CONFIG_DATA` secrets.
- IAM/trust policies with `StringLike ... repo:org/*`.
- Deploy role can touch `kube-system` or other namespaces.

**Phase to address:**
Provisioning phase (identity design before first deploy). Guardrails phase enforces admission policy.

---

### Pitfall 8: OpenTelemetry cardinality blowup from per-environment labels

**What goes wrong:**
Every environment is a new `env_id`/namespace/pod-name/commit-SHA label value. Short-lived series are created, then abandoned, so the metrics backend's active-series count and cost grow without bound (high churn, not just high cardinality). Prometheus-style stores slow or OOM; vendor bills spike. Traces and logs have the same problem with indexed labels (e.g., Loki labels).

**Why it happens:**
"Observe each environment" is taken literally as "put the environment ID on everything." Pod names, PR numbers, and SHAs are unbounded. Metric attributes added in SDK instrumentation have no limits.

**How to avoid:**
- Separate dimensions by purpose. For aggregate/fleet metrics, use bounded labels only (team, repo, template, state). For per-environment drill-down, use traces/logs, exemplars, or a per-env metric set with a retention/cleanup policy, and drop `env_id` from long-retained aggregates.
- Put environment identity in OTel Resource attributes (`deployment.environment.name`, custom `ephematool.env.id`) and decide per backend which become metric labels. With Prometheus exporters/remote-write, resource attributes become `target_info` rather than labels by default, which is the safer shape [VERIFY against current exporter config].
- Drop or hash unbounded attributes in the Collector (`attributes`/`transform` processors; `filter`), and enforce SDK/view attribute limits (metric views with attribute allow-lists).
- Plan series lifecycle: when an environment is destroyed, stop emitting (staleness markers) and delete or let per-env series age out quickly. Use short retention for per-env data.
- Set a series budget and alert on active-series growth rate. Load-test by creating and destroying 100 environments and measuring series count afterward.

**Warning signs:**
- Active series count grows monotonically with environment churn rather than with current environment count.
- Pod name or SHA appears as a metric label.
- Dashboard queries get slower the more historical environments exist.

**Phase to address:**
Observability foundation phase (telemetry schema/attribute conventions, decided before instrumenting). Cannot be cheaply retrofitted, matching the project's own constraint.

---

### Pitfall 9: OTel Collector mis-sizing and misconfiguration

**What goes wrong:**
Collector OOM-kills and loses telemetry exactly when many environments spin up (a burst of new pods, new targets). Single Collector becomes a bottleneck or single point of failure. The tool's own telemetry is lost during incidents. Tail sampling gives broken traces after horizontal scaling.

**Why it happens:**
Default config copied from examples; no `memory_limiter`; `batch` before `memory_limiter`; one Collector for both agent and gateway roles; no queue/retry limits.

**How to avoid:**
- Pipeline order: `memory_limiter` first, then enrichment (`k8sattributes`, resource), then `batch` last. Set the container memory limit and the limiter limits consistently (limiter below limit).
- Use a two-tier topology: per-node agent (DaemonSet) or sidecar for collection, a gateway Deployment (HPA) for processing/export. Use sending queues with bounded size and persistent storage if loss is unacceptable.
- Tail sampling requires all spans of a trace on the same Collector instance: use the `loadbalancingexporter` keyed by trace ID in front of the sampling tier. Otherwise keep head sampling for v1.
- Monitor the Collector itself (its own internal metrics: refused spans, queue size, dropped), and send the tool's own telemetry on a separate path/pipeline from tenant-environment telemetry so a noisy preview cannot starve it.
- Apply per-environment ingest limits (rate limit/ quota) so a chatty preview app cannot DOS the pipeline.
- `k8sattributes` needs RBAC and can add load; scope its watch to labeled namespaces.

**Warning signs:**
- Collector restarts (OOMKilled) correlated with environment creation.
- `otelcol_exporter_send_failed_*` or `refused_*` metrics non-zero.
- Fragmented traces after scaling the sampling tier.

**Phase to address:**
Observability foundation phase (topology) and a load-test in a hardening phase.

---

### Pitfall 10: GitHub API rate limits and workflow-dispatch semantics

**What goes wrong:**
Polling run status per environment from the dashboard exhausts the API quota (primary limit on the order of 5,000 requests/hour for a token, with separate secondary limits on bursts and concurrent requests [VERIFY current numbers]). Bursts of create requests trigger secondary rate limiting (403/429). Also: `workflow_dispatch` historically returns 204 with no run ID, so correlating "my click" to a specific run is racy and can attach the wrong run to an environment [VERIFY: GitHub may have changed this].

**Why it happens:**
N environments x poll interval = linear API cost. Correlation by "latest run on branch" breaks with concurrent dispatches.

**How to avoid:**
- Prefer webhooks plus conditional requests (ETag/`If-None-Match`, which do not count against the limit when 304) plus reconciliation at low frequency with jitter. Cache aggressively; one poll per repo (list runs) instead of per env.
- Correlate runs deterministically: pass a unique `correlation_id` (env ID) as a dispatch input and put it in the run name (`run-name:`), then find the run by that. Do not rely on timestamps.
- Honor `Retry-After` / `x-ratelimit-reset`, and use exponential backoff with jitter. Serialize dispatches through a queue with a concurrency cap.
- Use a GitHub App installation token (higher, per-installation limits that scale with installation size) rather than one PAT.
- Use workflow `concurrency` groups per environment to avoid parallel create/update/destroy on the same env. Note that `cancel-in-progress` on destroy or create can cancel a half-done operation and leave a partial state; and queued runs beyond one pending get dropped. Do not cancel destroy runs.
- Surface rate-limit remaining as a metric.

**Warning signs:**
- 403 with "secondary rate limit" messages; dashboard slows as env count grows.
- Environment linked to another environment's run.

**Phase to address:**
GitHub integration phase.

---

### Pitfall 11: Multi-tenant isolation gaps between preview namespaces

**What goes wrong:**
Namespace-per-environment is treated as an isolation boundary. A preview pod can reach other namespaces (including other teams' previews and internal/prod-adjacent services) over the pod network, exhaust node resources, read cluster metadata, or hit the cloud metadata endpoint. One runaway preview starves the others (noisy neighbor). Quotas are missing, so the "size/cost guardrails" exist only in the UI.

**Why it happens:**
Namespaces give naming and RBAC scoping, not network or resource isolation by default. Guardrails are enforced at the form level only.

**How to avoid:**
- Apply on every namespace at creation: default-deny `NetworkPolicy` (plus explicit allow to ingress, DNS, and the telemetry Collector), `ResourceQuota`, `LimitRange`, restricted Pod Security Admission level, a dedicated low-privilege ServiceAccount with `automountServiceAccountToken: false` by default, and no access to cloud metadata (block 169.254.169.254 or use IMDSv2 hop limit/workload identity).
- Enforce guardrails server-side: UI validation is advisory. The same limits must be enforced by admission policy and quotas so a hand-run workflow cannot bypass them.
- Schedule previews on a dedicated node pool (taints) or a dedicated cluster separate from production. Strongly prefer a separate cluster/account for previews; shared with prod is a critical risk.
- Remember NetworkPolicy needs a CNI that enforces it [VERIFY on the chosen cluster].
- Tenant-aware authorization in the tool: every API query is scoped by team/repo; test for IDOR (guessing another env's ID).

**Warning signs:**
- A `curl` from one preview namespace to another succeeds.
- Namespaces without a ResourceQuota object.
- Guardrail logic exists only in the front end or only in the tool's API.

**Phase to address:**
Guardrails phase (size/cost, validated config) with admission/quota baked into the base template in the provisioning phase.

---

## Technical Debt Patterns

| Shortcut | Immediate Benefit | Long-term Cost | When Acceptable |
|----------|-------------------|----------------|-----------------|
| Status as a single string field overwritten by handlers | Fast to build | Races, flapping, unexplainable states (Pitfalls 3, 4) | Never |
| Poll GitHub per environment from the UI | Simple, no webhook infra | Rate-limit exhaustion, slow UI | Spike/prototype only |
| Kubeconfig as GitHub secret | Works in 10 minutes | Cluster-wide credential exposure | Local dev cluster only, never shared |
| Free-form values.yaml overrides | Flexibility, fewer templates | Defeats the validated-config guardrail; unbounded resource asks | Never in v1; use schema-checked inputs |
| `env_id`/pod name as a metric label everywhere | Easy per-env dashboards | Series churn and cost blowup | Only on a short-retention per-env store |
| Skip the reaper; rely on destroy workflow | Less code | Permanent leak of failed destroys | Never |
| Single Collector for everything | One thing to run | OOM and data loss during bursts, tool blind during incidents | Local dev only |
| Hard-delete environment rows on destroy | Tidy DB | No audit trail, cannot debug orphans or cost | Never; soft-delete with retention |

## Integration Gotchas

| Integration | Common Mistake | Correct Approach |
|-------------|----------------|------------------|
| GitHub webhooks | Trusting order/uniqueness; processing inline; not verifying HMAC on raw body | Verify signature, store by delivery ID, enqueue, re-fetch truth, idempotent monotonic handlers |
| GitHub Actions dispatch | Correlating by "latest run" | Correlation ID input plus `run-name`; per-env `concurrency` group; never cancel destroy |
| GitHub auth | Personal PAT in the backend | GitHub App with minimal permissions, installation tokens |
| GitHub OIDC to cluster | Wildcard `sub`; reuse role across repos | Exact repo + Environment `sub`, `aud` check, namespace-scoped RBAC |
| Kubernetes API | Using list-all with per-request polling; no watch | Informers/watch with resync; label selectors |
| Kubernetes namespace delete | Assuming it completes | Watch for Terminating; surface finalizers; retry; sweeper |
| OTel Collector | Missing `memory_limiter`; `batch` first | `memory_limiter` first, `batch` last, bounded queues |
| Prometheus-style backend | Resource attrs promoted to labels | Keep identity in `target_info`/resource; allow-list labels |
| Health probes | Probing in-cluster service only, or accepting any 200 | Probe the public ingress path and verify an app signature |
| Ingress/DNS/TLS | Wildcard DNS plus cert issuance rate limits (e.g., Let's Encrypt per-domain limits) | Wildcard cert or pre-issued cert; track cert readiness as part of health [VERIFY limits] |

## Performance Traps

| Trap | Symptoms | Prevention | When It Breaks |
|------|----------|------------|----------------|
| Per-env API polling | 403s, slow dashboard | Webhooks + ETag + one poll per repo | Tens of concurrent envs per token |
| Metric series churn | Backend memory growth, slow queries | Bounded labels, short per-env retention | A few hundred create/destroy cycles per day |
| Dashboard live-query of cluster per page load | Slow inventory, API server load | Cache via informers into DB/in-memory store | ~50+ envs with several viewers |
| Reconcile loop doing a full scan every few seconds | API server throttling | Event-triggered + jittered resync, shard by env | ~100s of envs |
| Collector as single replica | Drops during bursts | Agent/gateway tiers, HPA, queues | Burst creation of many envs |
| Log label explosion | Log store index bloat | Keep env ID as structured field, not index label | Same as metric churn |

## Security Mistakes

| Mistake | Risk | Prevention |
|---------|------|------------|
| Auto-previews for fork PRs with secrets | Secret exfiltration, cluster takeover | Approval gate by SHA; no secrets for untrusted code; split build/deploy |
| `pull_request_target` checking out PR head | Pwn request | Never run head code with privileges |
| Script injection via PR title/branch in workflow `run:` | RCE in runner | Pass via `env:`; validate names with strict regex |
| Unpinned third-party actions | Supply-chain compromise | Pin to SHA; restrict allowed actions |
| Production data or secrets in previews | Data breach via a weakly protected environment | Synthetic seed data; separate secret store; template-level deny |
| Preview URLs publicly reachable and indexed | Leaks unreleased features/data | Auth in front of ingress (SSO/proxy), `noindex`, short TTL |
| Dashboard actions lack authz/audit | Anyone can destroy/extend anything | Per-team RBAC, audit log of every create/extend/destroy with actor |
| Secrets logged in OTel logs/span attributes | Credentials in telemetry backend | Redaction processors in the Collector, no env var dump, allow-list attributes |
| Dashboard backend with cluster-admin | Compromise of tool = cluster owned | Least-privilege, read-mostly, separate write identity |
| Webhook endpoint without signature verification | Forged events manipulate inventory | HMAC verify, constant-time compare |
| Namespace name derived from unvalidated branch | Collision, invalid names, targeting others' namespaces | Generated ID-based names (e.g., `pv-<shortid>`), never raw branch names |

## UX Pitfalls

| Pitfall | User Impact | Better Approach |
|---------|-------------|-----------------|
| Single "status" badge hides drift | SREs trust a green that is wrong | Show lifecycle and health separately, with a distinct drift indicator and reason |
| TTL expiry with no warning | Lost work, loss of trust | Expiring-soon notice (Slack/PR comment), one-click extend with a cap |
| Destroy with no confirmation or preview of what goes | Accidental deletions | Confirm with env name; show dependents and owner |
| "Destroyed" shown while teardown is failing | Hidden leaks | Distinct `destroy_failed` state, prominent in inventory |
| Stale data shown as fresh | Decisions on old state | Show last-observed timestamp per source; staleness badge |
| Config form allows invalid combos until submit fails in CI | Slow feedback, wasted runs | Validate against the same JSON Schema client- and server-side before dispatch |
| No link between a PR and its environment | Developers cannot find URL | Post URL/status as a PR comment/deployment status |
| Cost/limit denials with no explanation | Frustration, workaround via manual wiring | Explain which quota was hit and who can raise it |

## "Looks Done But Isn't" Checklist

- [ ] **Teardown:** Often missing cluster-scoped and cloud resources (LB, DNS, PV, certs) -- verify by diffing labeled resources after destroy, in both cluster and cloud account.
- [ ] **TTL:** Often missing race handling and null-expiry rejection -- verify with a concurrent extend/expire test and a create with no TTL.
- [ ] **Health:** Often just the workflow result -- verify with a deployed-but-broken app showing as unhealthy and drift-flagged.
- [ ] **Webhooks:** Often missing dedupe/ordering/backfill -- verify by replaying duplicate and reversed events and by downtime then redelivery.
- [ ] **Reconciler:** Often only handles creation -- verify by deleting a namespace out-of-band and by orphaning a namespace out-of-band.
- [ ] **Guardrails:** Often UI-only -- verify by bypassing the UI (direct workflow dispatch, direct kubectl with the workflow identity) and confirming rejection.
- [ ] **Fork PR safety:** Often untested -- verify a fork PR cannot read secrets or deploy without approval.
- [ ] **OIDC trust:** Often too broad -- verify that a different repo/branch cannot assume the role.
- [ ] **Isolation:** Often namespaces only -- verify cross-namespace connectivity is denied and quotas exist on every namespace.
- [ ] **Telemetry cost:** Often unmeasured -- verify active series after 100 create/destroy cycles returns near baseline.
- [ ] **Collector:** Often untested under burst -- verify no drops/OOM while creating many envs at once.
- [ ] **Tool self-observability:** Often forgotten -- verify the tool's own traces/metrics show webhook lag, reconcile duration, dispatch errors.

## Recovery Strategies

| Pitfall | Recovery Cost | Recovery Steps |
|---------|---------------|----------------|
| Orphaned namespaces/cloud resources | MEDIUM | Sweep by label/tag; compare with DB; delete after review; inspect stuck finalizers; fix root cause of failed destroy |
| TTL race destroyed an extended env | LOW | Recreate from stored config (keep config in DB); add CAS transition |
| Missed/duplicated webhooks, drifted inventory | LOW | Run full reconcile; use redelivery API; backfill from API |
| Leaked secret via fork PR | HIGH | Rotate all exposed secrets and tokens, audit runs, restrict workflow triggers, review cluster audit logs |
| Over-broad OIDC trust or kubeconfig | HIGH | Tighten trust policy, rotate credentials, audit cluster access; may need to rebuild trust boundaries |
| Cardinality blowup | MEDIUM-HIGH | Drop attributes in Collector, purge/shorten retention for offending series, redeploy instrumentation with views; may need backend reset |
| Collector OOM/loss | LOW-MEDIUM | Add limiter, scale gateway, add queues; data lost is unrecoverable |
| Rate-limit lockout | LOW | Back off, switch to webhooks/ETag, move to App token |
| Cross-tenant access discovered | HIGH | Apply NetworkPolicies/quotas retroactively, audit access, consider cluster separation |

## Pitfall-to-Phase Mapping

Phase names are indicative; the roadmap should map them to its actual phase list.

| Pitfall | Prevention Phase | Verification |
|---------|------------------|--------------|
| 1 Orphans/cost leaks | Lifecycle + provisioning (labels, idempotent destroy); reconciler/sweeper | Out-of-band orphan is detected and alerted; labeled-resource diff is empty after destroy |
| 2 TTL races | State model design, then Guardrails (TTL) | Concurrent extend/expire test passes; null TTL rejected |
| 3 Webhook reliability/order | GitHub integration | Duplicate/out-of-order replay yields correct final state |
| 4 Reconciliation drift | Core state model + reconciler | Out-of-band deletes/changes reflected and flagged within one resync interval |
| 5 Workflow-green vs app-health | Health/observability | Broken-but-deployed fixture shows drift |
| 6 Fork PR / pull_request_target secrets | Provisioning workflow design; Access/secrets guardrail | Fork PR cannot access secrets or auto-deploy; security review sign-off |
| 7 Over-privileged OIDC/kubeconfig | Provisioning (identity) + Guardrails (admission) | Wrong repo/branch cannot assume role; identity cannot touch other namespaces |
| 8 OTel cardinality | Observability foundation (schema before instrumenting) | Series count stable across 100 create/destroy cycles |
| 9 Collector sizing | Observability foundation; hardening load test | No drops/OOM under burst; Collector self-metrics clean |
| 10 GitHub rate limits/dispatch correlation | GitHub integration | Rate-limit headroom metric; concurrent dispatches map to the correct env |
| 11 Multi-tenant isolation | Provisioning base template + Guardrails | Cross-namespace traffic denied; every namespace has quota/limits/PSA |

## Sources

- GitHub Docs: Security hardening for GitHub Actions; Keeping your GitHub Actions and workflows secure (preventing pwn requests); OpenID Connect in cloud providers; Webhooks best practices and handling deliveries (`X-GitHub-Delivery`, `X-Hub-Signature-256`); Rate limits for the REST API; Workflow concurrency; `workflow_dispatch` REST endpoint. (From training knowledge; not re-fetched this session. [VERIFY] specific numbers and dispatch response behavior.)
- Kubernetes Docs: Namespaces, Resource Quotas, Network Policies, Pod Security Admission, ValidatingAdmissionPolicy, namespace termination and finalizers.
- OpenTelemetry Docs: Collector configuration and scaling, `memory_limiter`/`batch` ordering, tail sampling processor and load-balancing exporter, metrics SDK views and cardinality limits, Prometheus compatibility (`target_info`).
- Community knowledge: preview-environment leak post-mortems and "pwn request" write-ups (e.g., GitHub Security Lab), Prometheus high-cardinality/churn guidance.
- Confidence: MEDIUM overall (training knowledge, not live-verified). HIGH for design-level pitfalls (idempotency, state model, least privilege); MEDIUM for numeric limits and recent API behavior.

---
*Pitfalls research for: ephemeral preview-environment management platform*
*Researched: 2026-10-11*
