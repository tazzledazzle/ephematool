<!-- GSD:project-start source:PROJECT.md -->
## Project

**Ephematool**

A push-button web dashboard for SREs to create, configure, manage, and observe ephemeral preview environments (full app stacks on Kubernetes) provisioned via GitHub Actions. It tracks which environments are live and healthy, enforces guardrails on environment configuration to cut manual workflows, and ships with OpenTelemetry-based monitoring and observability from day one.

**Core Value:** Every ephemeral environment is observable from the moment it exists: SREs can see, via OpenTelemetry, what is running, whether it is actually healthy, and when it will go away.

### Constraints

- **Platform**: Kubernetes as the environment runtime — decided up front; the dashboard and provisioning workflows target it
- **Provisioning**: GitHub Actions is the orchestration/provisioning mechanism — it is the stated premise of the tool
- **Observability**: OpenTelemetry from day one — telemetry cannot be retrofitted cleanly; it shapes the architecture
- **Interface**: Web dashboard as the primary push-button surface
<!-- GSD:project-end -->

<!-- GSD:stack-start source:research/STACK.md -->
## Technology Stack

## Recommended Stack
### Core Technologies
| Technology | Version | Purpose | Why Recommended | Confidence |
|------------|---------|---------|-----------------|------------|
| Node.js | 24 LTS (registry shows v25 as Current; pin 24 for production) | Runtime | OTel JS SDK 2.x needs ^18.19 or >=20.6; `@kubernetes/client-node` 2.x needs >=20.9. 24 LTS satisfies both and is the safe long-lived choice. | MEDIUM (LTS designation from training knowledge) |
| TypeScript | 5.9.x (pin; see note) | Language | The registry's latest is 7.0.2, which I could not vet for ecosystem compatibility (Drizzle, Zod, OTel types). Start on the last 5.x and upgrade deliberately. | LOW on exact pin |
| Fastify | 5.12.5 | Backend API, webhook receiver | Fast, schema-first, strong plugin model, and OTel support via `@fastify/otel`. Hono 4.13 is the alternative, but Fastify's lifecycle hooks suit the signature-verified raw-body GitHub webhooks. | HIGH |
| React | 19.3.0 | Dashboard UI | Standard. | HIGH |
| Vite + TanStack Router | Vite 8.x, `@tanstack/react-router` 1.170.x | Dashboard SPA build and routing | The dashboard is an authenticated internal tool with no SEO need. A separate SPA against the Fastify API avoids running two server runtimes. Next.js 16.4 is a fine alternative if the team prefers it, but its server layer would duplicate the backend. | MEDIUM |
| PostgreSQL | 17 or 18 | System of record (environments, state transitions, audit, quotas, templates) | Environment state, TTL deadlines, quotas and audit need transactions and constraints. Postgres also runs the job queue (pg-boss), so there is no Redis. | HIGH |
| Drizzle ORM + drizzle-kit | `drizzle-orm` 0.45.4, `drizzle-kit` 0.31.11 | Typed SQL and migrations | Thin and SQL-transparent. Works with `pg` and `@opentelemetry/instrumentation-pg`. Pre-1.0, so pin exact versions. | MEDIUM |
| pg-boss | 12.37.1 | Job queue and scheduler (TTL reaper, health probes, teardown retries, cron) | Postgres-backed with transactional enqueue (state change and job in one commit), retries, cron and singleton jobs. Removes Redis from the footprint. | MEDIUM |
| Octokit | `octokit` 5.0.5 (or `@octokit/app` 16.1.4 plus `@octokit/webhooks` 14.2.0 and `@octokit/auth-app` 8.3.1), `@octokit/plugin-throttling` 11.0.5 | GitHub API: dispatch workflows, read runs, verify webhooks | Official SDK. Use a **GitHub App** (not a PAT) for installation tokens, fine-grained permissions (Actions rw, Contents r, Metadata r) and webhooks. | HIGH |
| `@kubernetes/client-node` | 2.0.0 (ESM-only) | Read namespace, Deployment and Pod state; watch/informers; apply ResourceQuota on teardown or extend | Official client. v2 is ESM-only, so make the backend ESM from day one. | HIGH on version, MEDIUM on v2 migration friction |
| OpenTelemetry JS | `@opentelemetry/api` 1.9.1; `sdk-node`, `exporter-*-otlp-http`, `sdk-logs` 0.223.0; `sdk-trace-base` 2.12.0; `semantic-conventions` 1.43.0 | Tracing, metrics and logs for the tool itself | SDK 2.x line is stable (released Feb 2025). Use `NodeSDK` plus OTLP/HTTP export to a Collector. Initialize via `--import ./otel.mjs` before any other module. | HIGH |
| `@fastify/otel` | 0.21.2 | Fastify route and hook spans | `@opentelemetry/instrumentation-fastify` is no longer maintained and was removed from auto-instrumentations; `@fastify/otel` replaces it. Pre-1.0. Its README does not state SDK 2.x compatibility, so verify in the first phase. | MEDIUM |
| `@opentelemetry/auto-instrumentations-node` | 0.81.0 | HTTP, undici, pg and pino instrumentation | Auto-spans for outbound GitHub and K8s calls plus DB. Enable selectively via `OTEL_NODE_ENABLED_INSTRUMENTATIONS` to limit overhead. | HIGH |
| OpenTelemetry Collector (contrib) | Current release (verify at deploy time) | Telemetry gateway for the tool and for every preview environment | One OTLP ingress. Per-env resource attributes (`deployment.environment.name`, `ephemeral.env.id`, `k8s.namespace.name`, owner, expiry) are stamped via the `k8sattributes` and `resource` processors. Deploy it as a DaemonSet or gateway, with workloads sending to a fixed in-cluster endpoint injected by the template. | MEDIUM |
| Grafana Tempo + Loki + Prometheus (or Mimir) + Grafana | Current stable | Traces, logs and metrics backends | Open, OTLP-native, and the de facto self-hosted default. The dashboard queries Prometheus HTTP API and Tempo and Loki APIs for per-env views and deep-links to Grafana for exploration. Do not build a telemetry store. | MEDIUM (LOW on exact versions; verify with Helm chart versions) |
| Kubernetes | 1.36 minimum (1.36.5 and 1.37.1 current as of Sep 2026) | Runtime | ValidatingAdmissionPolicy has been GA since 1.30, and MutatingAdmissionPolicy is GA in 1.36. These give in-cluster guardrails with no webhook server. | HIGH |
### Supporting Libraries
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| Zod | 4.6.6 | Single source of truth for env-config schema, API input, and form validation. `z.toJSONSchema()` emits JSON Schema natively. | Everywhere. Keep the template-input schema in a shared package. |
| Ajv + ajv-formats | 8.20.0 / 3.0.1 | Validate user config against **versioned JSON Schema published per template** | When schemas are authored as data (stored templates) rather than code. Zod covers the in-code case. |
| `yaml` | 2.9.1 | Parse and emit environment and template YAML | Template and values rendering. |
| `pg` | 8.23.1 | Postgres driver (used by Drizzle) | Always. |
| pino | 10.4.0 | Structured logging | Pair with `@opentelemetry/instrumentation-pino` (0.69.0) for trace-id correlation and log bridging to OTLP. |
| `@opentelemetry/host-metrics` | 0.39.0 | Process metrics | Optional; basic runtime health of the tool. |
| better-auth | 1.7.7 | Auth: GitHub OAuth login, sessions, org/team mapping | Dashboard login. Alternative is `arctic` plus hand-rolled sessions. Do **not** use next-auth 4.x (legacy line, 4.24.15) in a Fastify app. |
| `@casl/ability` | 7.0.1 | RBAC and per-team permission checks (create/destroy/extend by team and repo) | Backend enforcement. Keep a single `can(user, action, env)` choke point. |
| TanStack Query | 5.104.2 | Server state, polling and refetch for live status | Dashboard data layer. Poll or SSE for status. |
| TanStack Table | 9.2.8 | Environment inventory grid | Inventory, filtering and sorting. v9 is recent; pin and read the migration notes. |
| Tailwind CSS + shadcn/ui | 4.3.3 / CLI 4.21.4 | UI components and styling | Fast, accessible dashboard UI. |
| Recharts | 3.10.1 | Small sparklines and health charts | In-dashboard summaries. Use Grafana panels (embedded or linked) for deep telemetry. |
| `@opentelemetry/semantic-conventions` | 1.43.0 | Attribute names | Import stable constants, and define custom `ephemeral.*` attributes in one module. |
### Development Tools
| Tool | Purpose | Notes |
|------|---------|-------|
| Vitest 5.0.3 | Unit and integration tests | Use Testcontainers for Postgres. |
| Playwright 1.64.0 | Dashboard E2E | Cover the create, extend and destroy flows. |
| kind (or k3d) | Local and CI Kubernetes | Run reconciler and health-check tests against a real API server. Pin node image to K8s 1.36. |
| pnpm workspaces | Monorepo (`apps/api`, `apps/web`, `packages/schema`, `packages/otel`) | Shared Zod schema and OTel bootstrap packages. |
| Biome or ESLint + Prettier | Lint and format | Either is fine; pick one. |
| `kubeconform` / `kubectl --dry-run=server` | Validate rendered manifests in CI | Guardrail test layer. |
### Environment-Side (what the GitHub Actions workflow provisions)
| Piece | Choice | Why |
|-------|--------|-----|
| Packaging | Helm 3/4 chart per approved template (or Kustomize overlays) | Approved templates and schema-validated inputs map directly to `values.schema.json`. Helm is where the "validated config instead of free-form" guardrail lives. Prefer one packaging tool; choose Helm. |
| Namespace guardrails | `ResourceQuota` + `LimitRange` + `NetworkPolicy` stamped into every namespace, labeled `ephemeral.io/env-id`, `owner`, `expires-at` | Native, enforced by the API server, and satisfy the CPU/memory/replica caps. |
| Policy | ValidatingAdmissionPolicy (CEL) to deny privileged pods, un-quota'd namespaces, and disallowed images/registries; MutatingAdmissionPolicy to inject default labels | In-tree, no webhook server to run. Kyverno is the alternative if policy authors need generate/verify-images features. |
| Secrets | External Secrets Operator (or CSI driver) sourcing from the team's vault, with per-team scoped paths; GitHub OIDC to cloud/vault for the workflow itself | Avoids long-lived secrets in Actions. "No production data" is enforced by only allowing a preview-scoped secret store path. |
| Cluster auth from Actions | GitHub OIDC federation to the cluster (cloud IAM or an OIDC-trusting API server) | No kubeconfig secrets stored in GitHub. |
| Health checks | Probe from the control plane (HTTP against env ingress URL plus K8s readiness aggregation) | "Live" must not rely on workflow success alone. Also emit as OTel metrics (`ephemeral.env.healthy`). |
## Installation
# Backend (ESM; "type": "module")
# Dashboard
# Dev
## Alternatives Considered
| Recommended | Alternative | When to Use Alternative |
|-------------|-------------|-------------------------|
| Fastify 5 | Hono 4.13 | If you want edge-portable code. Not needed here. |
| Vite SPA | Next.js 16.4 | If you want SSR, one deployable, and server actions, and accept running business logic in it. Keep reconcilers out of it either way. |
| pg-boss | BullMQ 6.3 (Redis) | If you already operate Redis or need very high job throughput. Not the case here. |
| Custom control plane (Postgres + reconcilers) | A Kubernetes CRD + operator (kubebuilder/Go, or Operator SDK) | If the tool grows into a platform where environments are K8s-native resources with GitOps. Heavier for v1 and a second language. Revisit after v1. |
| GitHub Actions dispatch | Argo CD ApplicationSet PR generator / Argo Workflows | Out of scope: the project's premise is GitHub Actions as the provisioning engine. |
| Tempo/Loki/Prometheus/Grafana | SigNoz, ClickHouse-based stacks, or a commercial backend (Datadog, Honeycomb, Grafana Cloud) | If you want one datastore and a built-in UI, or do not want to operate backends. OTLP keeps this swappable, which is the point of OTel-first. |
| Drizzle | Prisma / Kysely | Prisma if the team prefers its DX and tolerates heavier tooling. Kysely for pure query-builder style. |
| Helm | Kustomize / raw manifests | Kustomize if templates are overlays on one base. Helm wins here for `values.schema.json`. |
| better-auth | Auth.js v5, Arctic, Keycloak/Dex | Dex or Keycloak if the org requires SSO beyond GitHub. |
| ValidatingAdmissionPolicy | Kyverno / OPA Gatekeeper | Kyverno if you need generate policies or image signature verification. |
## What NOT to Use
| Avoid | Why | Use Instead |
|-------|-----|-------------|
| `@opentelemetry/instrumentation-fastify` | Unmaintained and dropped from auto-instrumentations | `@fastify/otel` |
| OTel JS 1.x packages / `new Resource()` / `OTEL_PROPAGATORS` on the tracer provider | 1.x is on a one-year support window since Feb 2025; `Resource` replaced by `resourceFromAttributes` | SDK 2.x (`NodeSDK`) |
| PAT or long-lived deploy token for GitHub access | Over-scoped, not rotated, not attributable | GitHub App with installation tokens |
| Polling-only run tracking | Rate limits and latency | Webhooks (`workflow_run`, `workflow_job`) as primary, with polling reconciliation as a safety net |
| Matching dispatched runs by guessing (timestamp or inputs) | Racy. This was the old workaround because dispatch returned 204 with no ID. | Use the dispatch response run ID (see compatibility note), plus a correlation `env_id` input echoed as the run-name for older API versions |
| Storing the schedule/TTL only in K8s annotations or GitHub | Hard to query, extend and audit | Postgres `expires_at` is authoritative; annotate the namespace as a secondary safety net for orphan sweeps |
| Webhook-triggered teardown as the only TTL mechanism | Missed events leave environments lingering, which is the core pain | Periodic reaper job plus label-based orphan sweeper that finds namespaces not in the DB |
| Redis solely for a queue | Extra stateful component to operate | pg-boss |
| Storing user secrets in the app DB | Violates the secrets guardrail | External Secrets Operator referencing a vault; the app stores only references |
| Building a custom telemetry store or tracing UI | Large effort, off the core value | Query Prometheus and Tempo, deep-link into Grafana |
| `next-auth` 4.x | Legacy line | better-auth or Arctic |
| `cluster-admin` for the tool's service account | Blast radius | Namespaced Role/ClusterRole limited to namespaces labeled `ephemeral.io/managed=true`; only the workflow's OIDC identity creates namespaces |
## Stack Patterns by Variant
- Next.js 16 for UI and BFF plus a separate worker process running pg-boss and reconcilers.
- Because the reaper and prober must not live inside request-serving processes.
- Have the workflow commit rendered manifests or an ArgoCD Application per env.
- Because it adds drift detection, but it also adds a second control plane, so defer past v1.
- Keep the Collector, change only the exporter.
- Because OTLP-first makes the backend swappable.
## Version Compatibility
| Package A | Compatible With | Notes |
|-----------|-----------------|-------|
| `@opentelemetry/sdk-node@0.223.0` | `@opentelemetry/api@>=1.3.0 <1.10.0` (peer) | Keep api at 1.9.1. Unstable (0.2xx) and stable (2.x) packages are versioned together; upgrade as a set. |
| `@opentelemetry/auto-instrumentations-node@0.81.0` | OTel SDK 2.x | Confirm peer ranges against `sdk-node` at install time. |
| `@kubernetes/client-node@2.0.0` | Node >=20.9, ESM only | Backend must be ESM; no `require()`. |
| `@fastify/otel@0.21.2` | Fastify 5, OTel 2.x (not documented in README) | Verify with a smoke test in phase 1. |
| GitHub REST API version `2026-03-10` | `workflow_dispatch` always returns 200 with `workflow_run_id`, `run_url`, `html_url`; on `2022-11-28` it returns 204 unless `return_run_details: true` | Clients that treat only 204 as success will misread this. Pin the `X-GitHub-Api-Version` header explicitly and handle both. |
| Zod 4 | `z.toJSONSchema()` native | Do not add `zod-to-json-schema` (3.x-era helper). |
| Kubernetes 1.36+ | VAP (GA 1.30), MAP (GA 1.36) | If targeting managed clusters on older minors, MAP may be unavailable. Check provider version. |
## Sources
- npm registry (`npm view`, run 2026-10-11): all package versions and engine/peer ranges above. HIGH.
- [OTel JS SDK 2.0 announcement](https://opentelemetry.io/blog/2025/otel-js-sdk-2-0/) and [upgrade guide](https://github.com/open-telemetry/opentelemetry-js/blob/main/doc/upgrade-to-2.x.md): 2.x stable, Node and TS minimums, `Resource` migration. HIGH.
- [Elastic EDOT Node release notes](https://www.elastic.co/docs/release-notes/edot/sdks/node): `instrumentation-fastify` removed in favor of `@fastify/otel`. MEDIUM.
- [`@fastify/otel` repo](https://github.com/fastify/otel): usage; compatibility not stated. MEDIUM.
- [GitHub changelog: workflow dispatch returns run IDs (2026-02-19)](https://github.blog/changelog/2026-02-19-workflow-dispatch-api-now-returns-run-ids/) and [opaque-dev issue #148](https://github.com/opaque-dev/opaque/issues/148) for API 2026-03-10 behavior. MEDIUM (second-hand for the 2026-03-10 details; confirm against the REST docs).
- [Kubernetes releases](https://kubernetes.io/releases/) and [Mutating Admission Policy docs](https://kubernetes.io/docs/reference/access-authn-authz/mutating-admission-policy/): supported minors, VAP/MAP status. MEDIUM-HIGH.
- Collector, Tempo/Loki/Grafana chart versions, External Secrets, Helm 4, Node 24 LTS status, TypeScript 5.9 as last 5.x: training knowledge, not verified. LOW.
<!-- GSD:stack-end -->

<!-- GSD:conventions-start source:CONVENTIONS.md -->
## Conventions

Conventions not yet established. Will populate as patterns emerge during development.
<!-- GSD:conventions-end -->

<!-- GSD:architecture-start source:ARCHITECTURE.md -->
## Architecture

Architecture not yet mapped. Follow existing patterns found in the codebase.
<!-- GSD:architecture-end -->

<!-- GSD:skills-start source:skills/ -->
## Project Skills

No project skills found. Add skills to any of: `.claude/skills/`, `.agents/skills/`, `.cursor/skills/`, `.github/skills/`, or `.codex/skills/` with a `SKILL.md` index file.
<!-- GSD:skills-end -->

<!-- GSD:workflow-start source:GSD defaults -->
## GSD Workflow Enforcement

Before using Edit, Write, or other file-changing tools, start work through a GSD command so planning artifacts and execution context stay in sync.

Use these entry points:
- `/gsd-quick` for small fixes, doc updates, and ad-hoc tasks
- `/gsd-debug` for investigation and bug fixing
- `/gsd-execute-phase` for planned phase work

Do not make direct repo edits outside a GSD workflow unless the user explicitly asks to bypass it.
<!-- GSD:workflow-end -->



<!-- GSD:profile-start -->
## Developer Profile

> Profile not yet configured. Run `/gsd-profile-user` to generate your developer profile.
> This section is managed by `generate-claude-profile` -- do not edit manually.
<!-- GSD:profile-end -->
