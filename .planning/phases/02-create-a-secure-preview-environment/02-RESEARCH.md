# Phase 2: Create a Secure Preview Environment - Research

**Researched:** 2026-10-12
**Domain:** GitHub App + Actions dispatch, local kind cluster with a self-hosted runner, gateway-level preview auth, Helm template catalog, namespace guardrails, append-only audit, asynq state machine, OTel for environments
**Confidence:** HIGH on everything marked VERIFIED (most of the risky parts were proven in a throwaway kind cluster, a Postgres container and a collector container on this machine); MEDIUM on GitHub-side behavior that needs real credentials (UAT only)

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**Cluster, runner and preview access**
- **D-01:** Previews land in a **local kind cluster**, created and managed by a Bazel target alongside the Compose stack (no cloud cluster in Phase 2).
- **D-02:** A **self-hosted GitHub Actions runner** on the developer's machine, registered only to the platform's environments repo (D-14), deploys into kind using a **namespace-scoped ServiceAccount** credential. No inbound network exposure. **GitHub OIDC to a remote API server** (exact repo/environment `sub` pinning, `aud` check, as in the research) is the documented target for when a remote cluster exists; it is not built in Phase 2.
- **D-03:** Previews are reached through **ingress-nginx on kind** at `<env-id>.preview.localhost` (resolves to 127.0.0.1 in browsers, no hosts-file edits). Namespace names are ID-generated (`pv-<shortid>`), never branch-derived (research pitfall 6).
- **D-04 (new scope, ENV-09):** Preview URLs **require a signed-in, allowed platform user**, and every preview response carries `X-Robots-Tag: noindex`. Mechanism: the **platform session cookie stays host-only on the dashboard origin and is never sent to preview hosts**. Visiting a preview redirects through the platform, which (if the user is signed in and allowed) mints a **short-lived token bound to that one environment** and sets it as a cookie on the preview host; ingress-nginx validates it with `auth_request`. Preview code (untrusted branches) must never see the platform session.
- **D-05:** The ingress auth check **exempts one fixed health path** (for example `/__ephematool/healthz`) that the template's ingress serves; the worker's health probe (D-18) hits only that path, so no credential lives in the worker.

**Who can create, and ownership**
- **D-06:** The **creator** role comes from membership in configured GitHub team(s) (`CREATOR_TEAMS`) or logins in `CREATOR_LOGINS`; `ADMIN_LOGINS` still wins. Roles are evaluated at login for UI hints, and the create endpoint **re-verifies creator/admin status against GitHub and the current config at request time**, so removal applies to the next create (closes the stale-role gap from the Phase 1 code review, WR-01, for this action). Works with the fake GitHub.
- **D-07:** Targetable repos come from an **explicit `ALLOWED_REPOS` allowlist** (`org/name`, comma-separated), checked server-side at create time and used to populate the form. Fail closed when empty.
- **D-08:** An environment's **owner is the creating user's GitHub login** (`ephematool.io/owner` label, `ephematool.env.owner` attribute). Team or repo ownership is added later if Phase 7 needs it.
- **D-09:** Roles in Phase 2: **viewer** reads; **creator** creates and (later) manages their own; **admin** can do everything and sees the full audit log. Destroy lands in Phase 3, so only the create gate and audit visibility are exercised now.

**Templates and the create form**
- **D-10:** An approved template is a **Helm chart versioned in this repo** (`templates/<name>/` with a `values.schema.json`). The server loads the catalog and validates form inputs against each chart's schema (JSON Schema, in Go) before anything is dispatched; failures return **field-level errors**.
- **D-11:** The app image follows a convention: the platform resolves the chosen ref to a **commit SHA via the GitHub API** and deploys `ghcr.io/<org>/<repo>:<sha>` built by the target repo's own CI. If that tag does not exist, creation is rejected with a field-level error on the ref. The platform does not build images. If the package is private, kind needs an image pull secret.
- **D-12:** The form has: **repo** (from the allowlist), **ref** (type selector for branch / PR number / SHA, resolved server-side), **template** with its **schema-driven extra values** (for example replicas within a cap), and a **size preset (S / M / L)** that maps to fixed ResourceQuota and LimitRange values so users never type raw CPU or memory (GRD-01).
- **D-13:** Every environment is stamped with a default **`ephematool.io/expires-at`** label and DB column (creation time plus a configured default, for example 24h). It is **informational only in Phase 2** (no reaper); Phase 5 adds enforcement, extension and the reaper without a data migration.

**Dispatch, correlation and the first probe**
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

### Deferred Ideas (OUT OF SCOPE)
- GitHub **webhook receiver** (signature verification, delivery-ID dedupe) — when the platform has a public endpoint.
- **GitHub OIDC to a remote cluster** and a hosted-runner path — when a remote cluster exists.
- Pinning admins by numeric GitHub id (Phase 1 hardening item).
- Team or repo ownership attributes — Phase 7 if concurrency caps need them.
- Phase 1 review warnings not touched by Phase 2 (telemetry-ingest authentication, Grafana anonymous Admin, Loki retention) — a separate hardening pass.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| ENV-01 | Create a preview environment from a branch, PR or ref with one action | Create endpoint + asynq dispatch/poll/probe state machine; go-github v92 dispatch with `return_run_details`; runner + `provision.sh`; VERIFIED kind/Traefik/Helm path |
| ENV-02 | Creation accepts only schema-validated inputs against an approved template | `santhosh-tekuri/jsonschema/v6` (same library Helm uses) on the chart's `values.schema.json`; VERIFIED JSON-pointer field errors; Helm re-validates at install |
| ENV-09 | Preview URL reachable only by a signed-in, allowed platform user; session never exposed to preview code | Traefik ForwardAuth + signed ticket/cookie; VERIFIED end to end in Chromium and curl (cookie stripping, health exemption, session isolation, cookie-tossing behavior) |
| OBS-02 | One trace from Create click through the Actions run to first healthy probe | Traceparent stored on the env row, carried in asynq headers and as a workflow input; synthetic run/job/step spans from Actions API timestamps; probe span ends the trace |
| OBS-03 | Environment telemetry tagged with env id, owner, template | Chart injects `OTEL_*`; reserved `*/environments` collector pipelines (VERIFIED with `otelcol validate` and a live filter test); sample app for verification |
| GRD-01 | CPU/memory limits via ResourceQuota + LimitRange | VERIFIED manifests; quota rejection proven in kind; presets in one embedded `sizes.yaml` |
| GRD-04 | Default-deny NetworkPolicy baseline | VERIFIED kindnet (kind v0.33.0 default CNI) enforces NetworkPolicy; no Calico/Cilium needed |
| AUTH-02 | Actions gated by role (viewer, creator, admin) | Request-time role resolution via App installation token; WR-02 inversion (secure by default) lands here |
| AUTH-03 | Every create/denial recorded in append-only audit log | VERIFIED two-role grant model + trigger in Postgres 18.6; sqlc parses the migration |
</phase_requirements>

## Summary

The phase is feasible end to end on a developer laptop, and the highest-risk pieces were proven empirically here rather than assumed: a kind v0.33.0 cluster (Kubernetes 1.36.4) with Traefik v3.7.14 as the gateway, a scripted ForwardAuth flow exercised in curl and headless Chromium, namespace-scoped deployment with a short-lived ServiceAccount token under Helm 4.3.0, quota and NetworkPolicy enforcement, an append-only Postgres table under two roles, and the environment collector pipelines validated and exercised in the real `otelcol-contrib:0.162.0` image. The cluster was deleted afterward.

**Four locked decisions cannot be implemented exactly as written; each has a small compliant alternative that preserves the intent:** (1) **D-03 ingress-nginx is archived (read-only since March 2026, no further security fixes)**: use Traefik v3 behind the same `<env-id>.preview.localhost` hostnames. (2) **D-02 "namespace-scoped credential"** cannot create the namespace it lives in: use a two-credential model (a tightly scoped cluster-level *provisioner* that creates only `pv-*` namespaces and their guardrails, then a 15-minute namespace-scoped *deployer* token for Helm). (3) **D-04 "mint a token and set it as a cookie on the preview host"**: the dashboard origin cannot set a cookie for another host, so the mint step returns a one-time *ticket* URL and the preview host's own ForwardAuth handler exchanges it for the cookie (proven working). (4) **D-11/D-19 operational facts**: GitHub App installation tokens cannot read GHCR, so the image-exists check is anonymous for public packages and needs an optional PAT for private ones; and D-19 requires a separate migration (owner) database role, a new config value.

**Primary recommendation:** Build the phase around three platform-owned layers the untrusted PR code can never touch: (a) a Traefik entrypoint-level middleware chain (headers + ForwardAuth) so auth cannot be bypassed by a template, (b) a provisioner/deployer RBAC split with three small ValidatingAdmissionPolicies, and (c) a baseline (quota, LimitRange, default-deny NetworkPolicy, Pod Security labels) applied by the platform, not the template. Keep all provisioning logic in a script the workflow merely calls, so the same script drives an automated local kind verification and the manual real-Actions UAT.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Create form, schema-driven fields, progress, audit page | Browser / Client (React SPA) | API | UI-SPEC; server remains authoritative for all validation |
| Role resolution, request-time re-check, audit writes | API / Backend | Database | Authorization must be server-side; audit is DB-enforced |
| Ref/PR/SHA resolution, image-exists check, allowlist | API / Backend | GitHub (external) | Needs GitHub App credentials; inputs are untrusted |
| JSON Schema validation of template values | API / Backend | Helm (in runner) | API gives field errors early; Helm re-validates at install |
| Dispatch, run polling, probe, state transitions | Worker (asynq) | Database | Resumable, idempotent, off the request path |
| Provisioning (namespace, baseline, Helm install) | Self-hosted runner (script) | kind API server | Runs on the dev machine with a local credential; no inbound exposure |
| Namespace isolation (quota, limits, NetworkPolicy, PSA) | Kubernetes (kind) | Runner (applies) | Native, enforced by the API server and CNI |
| Preview auth gate and `X-Robots-Tag` | Gateway (Traefik entrypoint middleware) | API (`/internal/preview/auth`) | Gateway enforces for every router; API holds keys and decides |
| Ticket mint | API / Backend (needs platform session) | SPA (does the navigation) | Platform session must never leave the dashboard origin |
| Environment telemetry ingest | Collector (`*/environments` pipelines) | Tempo/Loki/Prometheus | Separate receiver, limiter and filters from platform telemetry |
| Trace stitching across Actions | Worker (synthetic spans) | Fake GitHub (test data) | Actions emits no spans without webhooks; timestamps come from the Runs/Jobs API |

## Standard Stack

### Core (already in the repo; reuse)
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Go | 1.26 | Backend | go.mod [VERIFIED: go.mod] |
| `github.com/google/go-github/v92` | v92.0.0 (2026-09-14) | GitHub REST: dispatch, runs, jobs, refs, PRs, installation tokens | Already approved in Phase 1. Its `CreateWorkflowDispatchEventByFileName` returns `*WorkflowDispatchRunDetails` and the request struct has `ReturnRunDetails` [VERIFIED: module source, go list] |
| `github.com/hibiken/asynq` | v0.26.0 | Dispatch/poll/probe/sweep tasks | Has `Unique`, `ProcessIn`, `TaskID`, `Timeout`, `Retention`, `MaxRetry` [VERIFIED: module source] |
| `github.com/danielgtaylor/huma/v2` | v2.39.1 | API incl. 422 field errors | Existing |
| `github.com/pressly/goose/v3` | v3.29.0 | Migrations (use Provider + session locker for WR-11) | Existing |
| sqlc (go tool) 1.31.1 | | Typed queries | VERIFIED parses a migration with `DO $$`, `CREATE ROLE`, triggers, grants, goose `StatementBegin` markers |
| `testcontainers-go` postgres/redis 0.44.0 | | Integration tests incl. two-role audit test | Existing |

### New (each needs human approval per the project rule; see audit)
| Library | Version | Purpose | Why |
|---------|---------|---------|-----|
| `github.com/santhosh-tekuri/jsonschema/v6` | v6.0.3 (2026-08-06) | Validate form values against each chart's `values.schema.json`; field errors from JSON pointers | Apache-2.0, drafts 4/6/7/2019-09/2020-12, **Helm v4.3.0 itself depends on this exact version**, so server and `helm install` agree on semantics [VERIFIED: helm go.mod; Go module proxy] |
| `github.com/golang-jwt/jwt/v5` | v5.3.1 (2026-01-28) | Sign/verify preview tickets and cookies (HS256) and the GitHub App JWT (RS256) | MIT, 9k+ stars, parser options `WithValidMethods`, `WithAudience`, `WithIssuer`, `WithExpirationRequired`, `WithLeeway` exist [VERIFIED: module source]. Pinning the algorithm is what prevents alg-confusion |
| `golang.org/x/text` | v0.43.0 | Needed *directly* because `jsonschema` error text requires a non-nil `message.Printer` (nil panics, VERIFIED) | Already an indirect dependency, so not a new module |

**Rejected:** `github.com/bradleyfalzon/ghinstallation/v2` v2.19.0. It is maintained, but its go.mod requires `go-github/v88` and `golang-jwt/jwt/v4`, so it would drag a second go-github major and a second jwt major into the module [VERIFIED: its go.mod]. With jwt/v5 already approved, sign the 10-minute RS256 app JWT (iss, iat, exp) and call `Apps.CreateInstallationToken` from go-github v92; cache the token until 5 minutes before expiry.

### Infrastructure (pinned versions, all verified on 2026-10-12)
| Component | Pin | Notes |
|-----------|-----|-------|
| kind | v0.33.0 (2026-08-26). darwin-arm64 asset sha256 `0c8c7dbe5e23594a198b786c4bc13dacc101fa6196b0cb0b23a1ca44e61f4b4f` matches the GitHub release digest | The machine currently has kind v0.29.0: the Bazel target must version-check and fail with an install hint |
| kind node image | `kindest/node:v1.36.4@sha256:099e049362a1526b2db71494e1947aae99bd16290d7c895f2b7ea312e3cbfaed` | Tested. Kind v0.33.0 default is 1.37.0 (untested here). Must use the digest [CITED: kind v0.33.0 release notes] |
| Traefik | image `traefik:v3.7.14`, Helm chart `traefik/traefik` 41.7.1 (repo `https://traefik.github.io/charts`) | Replaces ingress-nginx (see Locked-decision deviations). MIT, 65k stars |
| Helm | 4.3.0 (runner) | Tested with a namespace-scoped token incl. `values.schema.json` enforcement. Helm 3.18.4 is what is installed locally and is near end of life; Helm 3 compatibility was not tested |
| kubectl | 1.36.x | Local client is 1.36.1 |
| Actions runner | `actions/runner` v2.338.0 (2026-10-06) | Host process, ephemeral (see pattern 5) |
| Gateway test upstream | `traefik/whoami` v1.12.0 (v1.11.0 was the tested one) | e2e/UAT only |
| OTel Collector contrib | 0.162.0 (already pinned in compose) | Config below validated against this exact image |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| Traefik | Envoy Gateway v1.9.2 (SecurityPolicy ext_authz), NGINX Gateway Fabric 2.7.2 | Gateway API is the long-term direction, but ext-auth wiring is per-route policy objects (more CRDs, heavier on a laptop). Traefik ForwardAuth plus entrypoint-level middlewares gave a global, template-proof gate with one small CRD and was proven working |
| Traefik | Keep ingress-nginx v1.15.1 (last release 2026-03-19, repo archived) | Functional on kind, but no further security fixes while it fronts untrusted PR code. Only acceptable if the user explicitly accepts the risk for local-only use |
| Cookie-based `X-Forwarded` trust | mTLS between gateway and API | Not worth it locally |
| Direct export from pods to the Compose collector | Collector DaemonSet in kind with `k8sattributes` | Better trust (identity from namespace labels, not client-asserted) but heavier; recommended for Phase 6 |

**Installation (new Go deps):**
```bash
go get github.com/santhosh-tekuri/jsonschema/v6@v6.0.3 github.com/golang-jwt/jwt/v5@v5.3.1
go mod tidy && bazel mod tidy && bazel run //:gazelle
```
No new npm packages: the UI-SPEC adds only official shadcn registry components (`input label select radio-group switch alert`), built on the already installed `radix-ui` 1.8.0.

**Version verification performed:** `go list -m` against the Go proxy for every Go module; `gh api .../releases/latest` for kind, Traefik, the chart, Helm, the runner, whoami, Gateway API, Envoy Gateway, NGF.

## Package Legitimacy Audit

slopcheck 0.6.1 was installed and run (`slopcheck scan --pkg go <module> --json`); it reports `OK` with an informational `NO_REPO` flag for all three modules, which is a limitation of the Go proxy metadata, so source repositories were verified separately through `gh api`.

| Package | Registry | Age | Activity | Source Repo | slopcheck | Disposition |
|---------|----------|-----|----------|-------------|-----------|-------------|
| `santhosh-tekuri/jsonschema/v6` | Go proxy | repo since 2017-05; v6.0.0 2024-06 | v6.0.3 2026-08-06, pushed 2026-09-21, 1.3k stars, Apache-2.0, not archived | github.com/santhosh-tekuri/jsonschema | [OK] | NEW dependency. Strong provenance (Helm uses it). Planner: `checkpoint:human-verify` before `go get` |
| `golang-jwt/jwt/v5` | Go proxy | repo since 2021-05 (v5 line) | v5.3.1 2026-01-28, pushed 2026-09-14, 9.2k stars, MIT | github.com/golang-jwt/jwt | [OK] | NEW dependency. Planner: `checkpoint:human-verify` |
| `bradleyfalzon/ghinstallation/v2` | Go proxy | older | v2.19.0 2026-06-11, Apache-2.0 | github.com/bradleyfalzon/ghinstallation | [OK] | **Not recommended** (duplicate majors of go-github and jwt) |
| `traefik` image / chart 41.7.1 | Docker Hub / traefik.github.io | repo since 2015-09, chart 2019-09 | v3.7.14 2026-10-06; chart 2026-10-09; MIT / Apache-2.0 | github.com/traefik/traefik, traefik-helm-chart | n/a (not a package manager ecosystem slopcheck covers) | NEW image/chart. Pin the image digest at plan time. Planner: `checkpoint:human-verify` |
| `kindest/node` v1.36.4 | Docker Hub | | pinned by digest from the kind release notes | kubernetes-sigs/kind | n/a | NEW (digest-pinned) |
| `kind` v0.33.0 binary | GitHub release | repo since 2018-09 | sha256 verified equal to the release asset digest | kubernetes-sigs/kind | n/a | NEW dev tool |
| `helm` v4.3.0 | get.helm.sh | repo since 2015-10 | the tarball I used had sha256 `d3870437e1e95b67f8edbde964156c84a26503f560821d40c542441658934fba`; **not compared to the published .sha256sum**, so the planner must pin the official checksum | helm/helm | n/a | NEW dev tool |
| `actions/runner` v2.338.0 | GitHub release | repo since 2019-04 | released 2026-10-06 | actions/runner | n/a | NEW dev tool |
| `traefik/whoami` v1.12.0 | Docker Hub | repo since 2015-09 | 2026-07-30, Apache-2.0 | traefik/whoami | n/a | e2e only, NEW |

**Packages removed due to slopcheck [SLOP] verdict:** none
**Packages flagged [SUS]:** none. Everything is NEW relative to Phase 1 approvals, so each install gets a human-verify checkpoint per the project rule.
*Package names were confirmed from authoritative sources (Helm's own go.mod, GitHub repos, official release notes), not from search results alone.*

## Architecture Patterns

### System Architecture Diagram

```
 Browser (SRE)                                   Compose stack (host)                          kind cluster (host Docker)
 -------------                                   -------------------                           --------------------------
  Create click ──POST /api/environments────────▶ API (huma, scs session)                       
   (tracedFetch,                                   │ 1 role re-check (config + GitHub App token)
    traceparent)                                   │ 2 validate: allowlist, ref→SHA (App), image exists (GHCR),
                                                   │            template schema (jsonschema), size
                                                   │ 3 audit_events INSERT (success | denied | rejected)
                                                   │ 4 environments INSERT status=requested, traceparent, expires_at
                                                   │ 5 enqueue env:dispatch (asynq headers = traceparent)
  ◀── 202 {id} / 422 field errors / 403 ───────────┘
                                                 Worker (asynq)
                                                   env:dispatch ──POST dispatches (return_run_details)──▶ GitHub (environments repo)
                                                      CAS requested→provisioning, store run_id            │ workflow_dispatch
                                                   env:poll (ProcessIn 3s→15s, ETag) ◀─GET run/jobs───────┘
                                                      on completed: synthetic spans (run, jobs, steps)           ▲
                                                   env:probe ──GET gateway:8088 Host:<id>.preview.localhost      │ job picked up
                                                      /__ephematool/healthz  (2xx ⇒ CAS provisioning→running)    │
                                                   env:sweep @every 30s (re-enqueue stuck envs from DB)          │
                                                                                                          Self-hosted runner (host process, ephemeral)
                                                                                                           provision.sh:
                                                                                                           1 validate inputs (regex)
                                                                                                           2 provisioner kubeconfig (local file)
                                                                                                             ─▶ namespace pv-<id> + baseline
                                                                                                                (quota, limits, netpol, PSA, SA, binding)
                                                                                                           3 kubectl create token (15 min, deployer)
                                                                                                           4 helm upgrade --install templates/<name>
                                                                                                                                │
                                                                                                                                ▼
  Open preview ─▶ http://pv-x.preview.localhost:8088 ─────────────────────────────▶ Traefik (NodePort 30080 → host 8088)
                    │ entrypoint middleware chain: headers(X-Robots-Tag) → ForwardAuth ─▶ API /internal/preview/auth
                    │   no/invalid cookie → 302 → dashboard /preview/authorize?env&return        (decides, holds key)
                    │   SPA: POST /api/preview/ticket (session) ─▶ {url: …/__ephematool/callback?ticket=…}
                    │   callback hit on preview host ⇒ auth handler verifies ticket (single use), 302 + Set-Cookie
                    │   later requests: valid cookie ⇒ 200, auth strips the cookie before upstream
                    └─ health path /__ephematool/healthz: exempt (exact match) ───────────────▶ preview Service (NetworkPolicy allows only traefik ns)
  App in pv-x ── OTLP/HTTP ─▶ host.docker.internal:4320 ─▶ Collector otlp/environments ─▶ filter ─▶ Tempo | Prometheus | Loki
```

### Recommended Project Structure
```
templates/                         # D-10 catalog (Go package templates + charts)
├── embed.go                       # go:embed charts; Bazel embedsrcs via gazelle
└── web-app/{Chart.yaml,values.yaml,values.schema.json,templates/}
environments/                      # publishable content of the <org>/ephematool-environments repo
├── .github/workflows/create-environment.yml   # thin: validate inputs, call provision.sh
├── scripts/provision.sh           # ALL logic; also run by tools/kind verify (no Actions needed)
├── baseline/                      # platform-owned chart: namespace, quota, limitrange, netpol, SA, binding
└── sizes.yaml                     # S/M/L single source of truth (also go:embed'ed by the API)
deploy/kind/                       # cluster.yaml, bootstrap RBAC + VAPs, traefik-values.yaml, versions.env
deploy/compose.preview.yaml        # e2e overlay: Traefik (file provider) + whoami, no kind
internal/envs/                     # service: create, validate, state machine, probe, store glue
internal/ghapp/                    # App JWT, installation token cache, dispatch/runs/refs/PR/GHCR clients
internal/previewauth/              # ticket + cookie signing, ForwardAuth handler
internal/audit/                    # insert + list (cursor)
internal/fakegithub/               # extended (D-17)
cmd/sampleapp/                     # reference workload that emits OTLP + serves /health
tools/kind.sh                      # sh_binary wrappers: kind_up, kind_down, kind_verify, runner
```

### Pattern 1: Dispatch with run id, tolerant of both API shapes
**What:** go-github v92 pins `X-GitHub-Api-Version: 2022-11-28` by default and its service methods take no per-call version option. Under 2022-11-28 the response is 200 with `workflow_run_id` only when the body has `return_run_details: true`, otherwise 204. Under 2026-03-10 the parameter is removed and 200 is always returned [VERIFIED: GitHub's published OpenAPI descriptions for both versions; changelog 2026-02-19; breaking-changes page]. 2022-11-28 is supported until 2028-03-10, so the default is safe to pin.
**Example:**
```go
// Source: go-github v92 actions_workflows.go + GitHub OpenAPI (2022-11-28)
rd, resp, err := gh.Actions.CreateWorkflowDispatchEventByFileName(ctx, envsOwner, envsRepo, "create-environment.yml",
    github.CreateWorkflowDispatchEventRequest{
        Ref: "main",
        ReturnRunDetails: github.Ptr(true),
        Inputs: map[string]any{ // max 25 properties, 65,535 chars total
            "env_id": id, "correlation_id": corr, "traceparent": tp,
            "repo": repo, "sha": sha, "template": tmpl, "chart_version": ver, "size": size,
            "values_b64": b64JSON,
        },
    })
if err == nil && rd != nil && rd.WorkflowRunID != nil { /* store run id, status 200 */ }
// resp.StatusCode == 204 or rd == nil: ambiguous-run path, correlate via run-name (below)
```
**Correlation fallback:** `run-name: "create ${{ inputs.env_id }} ${{ inputs.correlation_id }}"`; list `GET /repos/{o}/{r}/actions/runs?event=workflow_dispatch&created=>=<dispatch_time-1m>` and match the correlation id. Never re-POST a dispatch after an ambiguous failure before running this search (non-idempotent).

### Pattern 2: Installation token without a new dependency
App JWT: RS256, `iss` = App ID, `iat` = now-60s, `exp` = now+9m, signed with `golang-jwt/jwt/v5` using a PEM read from `GITHUB_APP_PRIVATE_KEY_FILE` (a mounted file, never an env value, never logged). Then `Apps.CreateInstallationToken(ctx, installationID, nil)`; cache with expiry margin; a `RoundTripper` injects `Authorization: Bearer <token>`. Rate limit is 5,000/hour per installation (scales to 12,500) [CITED: docs.github.com rate limits]; 304 responses with a conditional request do not count against the primary limit when authorized [CITED: docs.github.com best practices].

**Minimum GitHub App permissions (least privilege)** [VERIFIED against the permissions-required table]:
| Permission | Level | Needed for |
|-----------|-------|-----------|
| Actions | read & write | dispatch (write), runs/jobs (read) on the environments repo |
| Contents | read | resolve branch / SHA on allowed repos |
| Pull requests | read | PR number → head SHA, fork detection |
| Metadata | read | mandatory |
| Organization Members | read | request-time team membership for `CREATOR_TEAMS` (D-06) |
No Administration (the platform never registers runners), no Packages (the App cannot use GHCR anyway), no Workflows. Install on the environments repo plus every allowed repo.

### Pattern 3: Preview auth via entrypoint-level ForwardAuth (VERIFIED)
**Why it works (proved in Chromium and curl):** Traefik applies `entryPoints.web.http.middlewares` to every router on that entrypoint before router middlewares, so a template cannot omit auth. In the Helm chart this is `ports.web.http.middlewares: [traefik-preview-guard@kubernetescrd]`. On a non-2xx auth response Traefik copies the auth server's status, **all headers (Set-Cookie, Location)** and body to the client, so the auth handler can answer the ticket callback with `302 + Set-Cookie` without any extra route [VERIFIED: Traefik v3.7.14 `forward.go` source and a live run]. `addAuthCookiesToResponse` applies only to the 2xx path and is not needed.
```yaml
# deploy/kind/traefik-middlewares.yaml (VERIFIED working)
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata: {name: preview-forward-auth, namespace: traefik}
spec:
  forwardAuth:
    address: http://host.docker.internal:8081/internal/preview/auth
    preserveLocationHeader: true
    maxResponseBodySize: 4096
    authRequestHeaders: [Cookie]        # plus X-Forwarded-* which Traefik always sets
    authResponseHeaders: [Cookie]       # auth returns the cookie header minus our cookie ⇒ upstream never sees it
---
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata: {name: preview-headers, namespace: traefik}
spec:
  headers:
    customResponseHeaders: {X-Robots-Tag: "noindex, nofollow", Referrer-Policy: "no-referrer"}
---
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata: {name: preview-guard, namespace: traefik}
spec:
  chain: {middlewares: [{name: preview-headers}, {name: preview-forward-auth}]}
```
**Auth endpoint logic** (`GET /internal/preview/auth`, marked public for RequireSession, NOT under `/api/` so the nginx front door never proxies it):
1. `env` = first label of `X-Forwarded-Host`, must match `^pv-[a-z0-9]{8,16}\.<PREVIEW_DOMAIN>(:<port>)?$` exactly; anything else, 403.
2. `X-Forwarded-Uri` parsed with `url.Parse`; reject `..`, encoded dots/slashes, `//`, backslash, control chars. If method is GET/HEAD and path **equals** `/__ephematool/healthz` exactly, 200 (D-05).
3. If path equals `/__ephematool/callback`: verify `ticket` (below); on success mark `jti` used, respond `302 Location: <ret from signed claim>` plus `Set-Cookie`; on failure 403.
4. Else iterate **all** cookies named `ephematool_preview` (a sibling preview can toss a same-named cookie, see Pitfall 4), accept if any verifies for this env; respond 200 with a `Cookie` response header containing every cookie except `ephematool_preview` (VERIFIED: the upstream received only `app=1`).
5. Otherwise `302 Location: <PUBLIC_BASE_URL>/preview/authorize?env=<id>&return=<url-encoded absolute preview URL>`.

**Token design (D-04 clarified):**
| Token | Claims | TTL | Enforcement |
|-------|--------|-----|-------------|
| Ticket (minted by `POST /api/preview/ticket`, session required, Sec-Fetch-Site guard via existing CrossOriginProtection) | `iss=ephematool`, `aud=<env-id>`, `sub=<github_id>`, `typ=ticket`, `jti` (128-bit random), `ret` (path, validated), `iat`, `exp=iat+60s` | 60 s | single use: Redis `SET preview:jti:<jti> NX EX 120`; `ret` is **inside** the signed claims so there is no `return` query parameter to tamper with |
| Cookie (`ephematool_preview`) | same `aud`, `typ=session`, `exp=iat+PREVIEW_COOKIE_TTL` (default 30 m, no sliding) | 30 m | HS256 only (`WithValidMethods`), `WithAudience(env)`, `WithIssuer`, `WithExpirationRequired`, 5 s leeway |
Cookie attributes: host-only (no Domain), `Path=/`, `HttpOnly`, `SameSite=Lax`, `Secure` iff `COOKIE_SECURE`. Key: `PREVIEW_TOKEN_KEY` (32+ random bytes, from a mounted file) with an optional previous key for rotation (`kid` header). `/api/preview/ticket` returns 404 for unknown envs and 403 for forbidden, with one body, so env ids cannot be enumerated (UI-SPEC item 7). Open redirect: the `ret` path must start with a single `/`, contain no `\`, CR/LF or `//`, and the SPA-supplied `return` URL host must equal exactly the env's own preview host (scheme, host, port).

### Pattern 4: Two-credential Kubernetes model (resolves D-02) (VERIFIED)
The tested manifests are in Code Examples. Summary of what was proven in kind v0.33.0 / K8s 1.36.4:
- `ephematool-provisioner` (SA in kube-system, long-lived token Secret exported to a chmod-600 kubeconfig on the runner host, **never** a GitHub secret). Its ClusterRole may only create/get/patch namespaces and create/update quota, limitrange, NetworkPolicy, ServiceAccount, RoleBinding, plus `bind` on the single ClusterRole `ephematool-deployer` and `serviceaccounts/token` create restricted by `resourceNames: [ephematool-deployer]`. Proven denials: cannot create namespace `evil`, cannot relabel `kube-system`, cannot create a RoleBinding in `kube-system`, cannot bind `admin` (escalation check), cannot mint a token for `coredns`, cannot list secrets cluster-wide.
- Two `ValidatingAdmissionPolicy` objects (GA since 1.30, matched on `request.userInfo.username`) pin the provisioner to namespaces named `pv-[a-z0-9]{8,16}` carrying `ephematool.io/managed=true`. A third optional policy should require every Ingress host in a managed namespace to equal `<namespace>.<PREVIEW_DOMAIN>` (defense in depth; not exercised).
- `ephematool-deployer` is minted per run with `kubectl create token ephematool-deployer -n pv-x --duration=15m` and used by Helm 4.3.0 with `--wait`. Proven: it can deploy, cannot touch NetworkPolicy/ResourceQuota/RBAC, cannot list namespaces or read `kube-system`. **Gap found and fixed during the spike:** the deployer needs `deployments/scale` and read-only `resourcequotas`/`limitranges`/`networkpolicies` for debugging and rollout.
- The real PR-controlled code is only the *container image*. Pods get no API credentials (`automountServiceAccountToken: false` in the chart and baseline default SA), PSA `enforce=baseline`, `warn/audit=restricted`, quota, and default-deny egress.

### Pattern 5: Self-hosted runner on a developer machine
- **Run it as a host process, ephemeral, not in Docker.** The kind API server is only published on `127.0.0.1:<random port>`; a containerized runner cannot reach it without joining the `kind` Docker network and using the internal address. A host process also has `kubectl`, `helm` and the provisioner kubeconfig directly. `./config.sh --url https://github.com/<org>/<env-repo> --token <reg-token> --ephemeral --labels ephematool-kind --unattended` in a loop re-registers per job [CITED: GitHub docs: self-hosted runner hardening, JIT runners]. A `tools/kind.sh runner` helper gets the one-hour registration token with the developer's own `gh api -X POST repos/<o>/<r>/actions/runners/registration-token`. **The platform App never gets Administration**; registration endpoints document only user/PAT auth, not App permissions [CITED: docs.github.com REST self-hosted runners].
- **GitHub's own guidance: self-hosted runners "should almost never be used for public repositories"** [CITED: docs.github.com secure-use]. Hard requirements: environments repo **private**, workflow triggers limited to `workflow_dispatch`, forks disabled, only admins may change `.github/workflows/**` (editing it is code execution on the developer's laptop), `permissions: contents: read`, runner labels unique (`runs-on: [self-hosted, ephematool-kind]`), no repo secrets in Phase 2.
- **Workflow input injection:** inputs reach the shell only through `env:` and are regex-validated in the first step (`env_id ^pv-[a-z0-9]{10}$`, `sha ^[0-9a-f]{40}$`, `repo ^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`, template and size against fixed lists). Never `${{ inputs.* }}` inside `run:` [CITED: docs.github.com secure-use].
- **Offline vs manual:** everything inside `provision.sh` plus the gateway can be exercised offline with `kind` and no GitHub (automated `kind_verify`); only "GitHub schedules the job onto the registered runner" needs real GitHub and is the manual UAT item.

### Pattern 6: Template catalog and schema validation (VERIFIED)
- Each chart ships `values.schema.json` (draft-07); Helm enforces it on `template`/`install`/`lint` (VERIFIED: `maximum: got 9, want 3` at `/replicas`). The Go server embeds the same charts (go:embed from a small `templates` package), compiles each schema once, and validates the user's values object.
- Field errors from `*jsonschema.ValidationError`: walk `Causes` to the leaves and use `InstanceLocation` ([]string, a JSON pointer split) mapped to UI field ids (`/replicas` → `values.replicas`). `ErrorKind.LocalizedString(printer)` needs `message.NewPrinter(language.English)`; passing nil panics (VERIFIED). Output observed: `/ingress/host => 'BAD' does not match pattern …`, `/ => additional properties 'extra' not allowed` (the last has no field and goes to the summary, as UI-SPEC requires).
- The form-schema trimming the UI-SPEC needs (supported constructs only; unsupported required property ⇒ template disabled) is a pure Go function over the parsed schema; unit test it table-driven.
- **Chart/server skew:** pass `chart_version` as a workflow input; `provision.sh` fails fast if the checked-out chart's `Chart.yaml` version differs. Image and presets are not user input.
- **Image pinning (recommended, cheap):** the GHCR existence check returns `Docker-Content-Digest`; deploy `ghcr.io/<repo>@sha256:…` so a re-pushed tag cannot change what runs.

### Pattern 7: Guardrails baseline (VERIFIED in kind)
Namespace `pv-<id>` labels from `conventions/telemetry.json`: `ephematool.io/managed=true`, `ephematool.io/env-id`, `ephematool.io/owner`, `ephematool.io/template`, `ephematool.io/expires-at`, plus `pod-security.kubernetes.io/enforce=baseline`, `warn=restricted`, `audit=restricted`. **Label values cannot contain `:`**, so store `expires-at` as Unix epoch seconds (the DB column is authoritative). Presets (UI-SPEC): S 0.5 vCPU/512 MiB, M 1/1 GiB, L 2/2 GiB as **limits**; requests are half. Quota also pins `pods`, `count/services`, `persistentvolumeclaims: "0"`, `services.nodeports: "0"`, `services.loadbalancers: "0"`. LimitRange supplies container defaults so quota does not reject pods without explicit resources (VERIFIED: pods got 50m/64Mi requests and 100m/128Mi limits; the sixth replica was rejected with `exceeded quota`). NetworkPolicies: default-deny (ingress and egress), allow DNS egress to kube-dns, allow ingress from namespace `traefik`, allow egress TCP 4320 (the environment OTLP port).
**CNI:** kind's default kindnet **does enforce NetworkPolicy** (via kube-network-policies, since kind v0.24) [CITED: kind v0.24.0 release notes; VERIFIED here: with only default-deny, a pod's DNS lookup and its egress to 1.1.1.1 both failed, and Traefik got 504 until the ingress allow rule was added]. No Calico or Cilium needed; cost zero.

### Pattern 8: Environment state machine on asynq
States (text + CHECK constraint so Phase 3 can add `destroying`/`destroyed` with a one-line migration): `requested → provisioning → running`, and `failed` from either of the first two. Forward-only compare-and-set updates, e.g. `UPDATE environments SET status='provisioning', run_id=$2 … WHERE id=$1 AND status='requested'`; rows-affected 0 means someone else won, so the handler returns nil.
| Task | Trigger | Behavior |
|------|---------|----------|
| `env:dispatch` | enqueued by create (after the row commits) and by the sweeper | CAS-claim `dispatch_attempted_at` (NULL → now); POST dispatch; on 200 store `run_id`, `run_html_url`, CAS `requested→provisioning`; on ambiguous result run the correlation search before any retry; queue with concurrency 1 and ≥1 s spacing (GitHub secondary limits: POSTs serial, 1 s apart) |
| `env:poll` | `ProcessIn(3s)` then backoff to 15 s + jitter; re-enqueues itself with a fresh TaskID | `GET runs/{id}` with `If-None-Match` (ETag stored in the row); 304 ⇒ reschedule; `completed/success` ⇒ emit synthetic spans, enqueue `env:probe`; other conclusions or `provision_deadline_at` exceeded ⇒ CAS to `failed` with reason ("Actions run failed", "Timed out waiting for the Actions run (15m)") |
| `env:probe` | after run success | GET `http://<PREVIEW_GATEWAY_ADDR>/__ephematool/healthz` with `Host: <id>.preview.localhost:<port>` (no DNS dependence; same code path in fake and kind), bounded (e.g. 12 tries × 5 s, per-request timeout 2 s). First 2xx ⇒ CAS `provisioning→running`, set `running_at`, end trace; exhausted ⇒ `failed` ("Preview did not become healthy") |
| `env:sweep` | scheduler `@every 30s` | selects non-terminal rows (stale `requested` without dispatch, `provisioning` without a pending poll) and enqueues with `asynq.Unique(25s)` keyed on type+env id; makes everything resumable after a worker restart or Redis flush |
Retries: `MaxRetry(3)` with exponential delay only for transient GitHub/DB errors; polling is rescheduling, not retrying. On `*github.RateLimitError` / `*github.AbuseRateLimitError` reschedule at the reset / `Retry-After` instead of failing. The traceparent is stored on the row and re-injected from the DB by the sweeper, so a restart keeps the same trace. The worker currently has no DB or GitHub config; it needs `DATABASE_URL` (app role), the App settings, and `extra_hosts: host.docker.internal:host-gateway` in compose.

### Pattern 9: Trace (OBS-02), honestly scoped
Platform spans (real): browser fetch → `POST /api/environments` → `validate` → GitHub client spans (otelhttp transport) → asynq publish → `env:dispatch` → `env:poll` (per attempt) → `env:probe`. Actions has no native span emission, and `githubreceiver`/webhooks need a public endpoint (deferred). So the poller emits **synthetic spans retroactively** from API timestamps once a run completes: `github.actions.run` (created_at → updated_at, with queue time = `run_started_at - created_at` as an attribute), one child per job (`started_at`/`completed_at`), one per step; created with `trace.WithTimestamp` under the stored traceparent. `traceparent` is also passed as a workflow input so a future step-level emitter (otel-cli exec honors `TRACEPARENT`, but is "experimental" with a single maintainer) or webhook receiver can attach real spans. **What the trace can show:** request, validation, dispatch, queue latency, run/job/step durations (1 s resolution), poll cadence, probe. **What it cannot:** log lines or internals of steps, and it appears as the run completes, not live. State this in the UI copy/README to avoid overclaiming.

### Pattern 10: Collector `*/environments` pipelines (VERIFIED)
Validated with `otelcol-contrib:0.162.0 validate` (a deliberately broken OTTL expression correctly failed) and a live test: of three spans sent (valid env id; no env id; `service.namespace=ephematool` spoof) only the valid one reached the exporter.
```yaml
receivers:
  otlp/environments:
    protocols:
      http: {endpoint: 0.0.0.0:4320, max_request_body_size: 1048576}   # no gRPC: smaller surface
processors:
  memory_limiter/environments: {check_interval: 1s, limit_mib: 300, spike_limit_mib: 50}
  filter/environments:
    error_mode: ignore
    traces:  {span:       ['resource.attributes["ephematool.env.id"] == nil', 'resource.attributes["service.namespace"] == "ephematool"']}
    metrics: {metric:     ['resource.attributes["ephematool.env.id"] == nil', 'resource.attributes["service.namespace"] == "ephematool"']}
    logs:    {log_record: ['resource.attributes["ephematool.env.id"] == nil', 'resource.attributes["service.namespace"] == "ephematool"']}
  resource/environments: {attributes: [{key: deployment.environment.name, value: preview, action: upsert}, {key: ephematool.source, value: environment, action: upsert}]}
  batch/environments: {send_batch_size: 512, send_batch_max_size: 1024, timeout: 5s}
service:
  pipelines:   # traces|metrics|logs / environments: [memory_limiter/environments, filter/environments, resource/environments, (attributes/redact), batch/environments] → the same exporters
```
Notes: (1) `memory_limiter` instances measure the **whole collector process**, so give the environments limiter a **lower** threshold (300 MiB) than the platform one (400 MiB) so environment traffic is shed first; that is how "separate limiter" protects the platform. (2) **There is no built-in rate limiter in collector-contrib** (open feature requests only), so the PITFALLS "rate limits" item becomes: request body cap, bounded batch, bounded sending queue per exporter, lower memory threshold, and (if needed) a Phase 6 nginx `limit_req` in front. Say so explicitly in the plan. (3) Prefer separate named exporter instances (e.g. `otlp/tempo-env`) so a slow environment export queue cannot block platform exports. (4) Prometheus `promote_resource_attributes` does **not** list the `ephematool.env.*` keys, so env identity stays in `target_info`, not on series labels, which satisfies PITFALLS 8. Keep it that way. (5) Resource attributes are **client-asserted**: a malicious preview can claim another env id inside the environments pipelines; accepted residual risk for Phase 2, fixed in Phase 6 by stamping identity from namespace labels with a collector in kind. (6) Compose publishes `127.0.0.1:4320:4320` only (never through nginx). On Docker Desktop for Mac a pod reaches a service bound to host 127.0.0.1 via `host.docker.internal` (192.168.65.254) [VERIFIED]; on Linux Docker this needs `0.0.0.0` or the bridge IP [ASSUMED].
The chart sets `OTEL_EXPORTER_OTLP_ENDPOINT=http://host.docker.internal:4320`, `OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf`, `OTEL_SERVICE_NAME` default, and `OTEL_RESOURCE_ATTRIBUTES=ephematool.env.id=…,ephematool.env.owner=…,ephematool.env.template=…,k8s.namespace.name=…` (conventions keys, generated constants).

### Pattern 11: Audit table (VERIFIED on Postgres 18.6-alpine)
Two roles: the existing compose user stays the **owner/migrator** (`MIGRATE_DATABASE_URL`); a new `ephematool_app` login role (dev-only password in the migration, `ALTER ROLE` out of band elsewhere) is the runtime `DATABASE_URL`. The migration (idempotent role creation, `uuid` PK so no sequence grants are needed, `REVOKE ALL FROM PUBLIC`, `GRANT SELECT, INSERT` only, row trigger for UPDATE/DELETE, statement trigger for TRUNCATE). Results: as the app role, UPDATE/DELETE/TRUNCATE all fail with `permission denied`; as the owner, DELETE fails with the trigger's `audit_events is append-only`; the owner *can* `ALTER TABLE … DISABLE TRIGGER` (so the guarantee is "no application path", not "DBA-proof"; state it). Grant per table explicitly in each migration; do **not** use `ALTER DEFAULT PRIVILEGES`, which would hand UPDATE on `audit_events` to the app role. `users` and `environments` need `SELECT, INSERT, UPDATE` for the app role; the goose version table stays owner-only. Run migrations through goose's Provider with a Postgres session locker (WR-11) using the owner URL.

### Anti-Patterns to Avoid
- **Auth as a per-Ingress annotation or inside the template chart:** a template or typo can drop it. Use the entrypoint-level chain.
- **Stripping nothing before upstream:** without `authResponseHeaders: [Cookie]` the preview app receives its own `ephematool_preview` token.
- **Believing a green workflow means running** (D-18) or re-POSTing a dispatch after a timeout.
- **A cluster-admin kubeconfig as a GitHub secret** (pitfall 7), or giving the runner the long-lived provisioner token inside a public repo's reach.
- **Putting env id on Prometheus series** (pitfall 8).
- **Trusting the session role at create time** (WR-01).
- **`ALTER DEFAULT PRIVILEGES` for the app role** (defeats D-19).
- **`return` as an unsigned query parameter** (open redirect). Sign it in the ticket.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| JSON Schema validation and error locations | Custom validator | `santhosh-tekuri/jsonschema/v6` | Same engine as Helm; JSON-pointer instance locations |
| JWT signing/verification | Custom HMAC token parser | `golang-jwt/jwt/v5` with pinned alg | alg-confusion, exp/aud edge cases |
| GitHub App installation flow | Own HTTP client | go-github `Apps.CreateInstallationToken` + jwt/v5 | Avoids ghinstallation's duplicate majors |
| Run correlation by timestamp guessing | "latest run" heuristics | `workflow_run_id` + `run-name` correlation id | Racy otherwise |
| Namespace guardrails | Per-template YAML | Platform baseline (quota, LimitRange, NetworkPolicy, PSA) | Templates must not weaken them |
| Gateway auth | Custom reverse proxy | Traefik ForwardAuth | Proven here; global enforcement |
| Image existence | `docker pull` in the API | OCI manifest `HEAD` | Cheap; returns the digest |
| Append-only guarantee | App-level checks | DB grants + trigger | Survives app bugs |
| Rate limiting for env telemetry | Custom collector processor | Size caps + lower memory threshold (+ edge `limit_req` later) | No contrib component exists |
| Job scheduling/dedupe | Goroutine tickers | asynq `Unique`, `ProcessIn`, scheduler | Already in the stack, restart-safe |

**Key insight:** every control that matters for untrusted PR code (auth gate, quotas, network policy, RBAC scope, admission policy) must be enforced by something the template and the app cannot configure. Anything a chart can omit will eventually be omitted.

## Runtime State Inventory

Not a rename/refactor/migration phase. Greenfield additions only. One existing-state caveat: existing Phase 1 Postgres volumes already contain `users` owned by the compose user, so the new `ephematool_app` role and its grants must be created by a **migration** (not a first-boot init script) or existing developers' volumes will never get them.

## Common Pitfalls

### Pitfall 1: ingress-nginx is archived
**What goes wrong:** Following D-03 literally installs a controller with no future security fixes in front of untrusted code. **Avoid:** Traefik per this document; get the user's explicit sign-off on the D-03 deviation. **Warning sign:** any task naming `ingress-nginx`/`auth-url` annotations.

### Pitfall 2: `*.localhost` resolves to `::1` on macOS
**What goes wrong:** macOS's system resolver returns `::1` for `pv-x.preview.localhost` (VERIFIED), while the kind port mapping listens on `127.0.0.1` only. Chrome and curl resolve `*.localhost` themselves to loopback and worked (VERIFIED in headless Chromium and curl); Safari/Firefox/system-resolver clients rely on connect fallback to IPv4. **Avoid:** keep `listenAddress: 127.0.0.1`, expect a small first-connect delay in some clients, and put Safari/Firefox on the UAT checklist [ASSUMED for those two browsers]. The worker never relies on DNS (Host header override).

### Pitfall 3: The preview URL has a port
**What goes wrong:** UI-SPEC shows `{env-id}.preview.localhost`, but port 8080 (web), 8081 (api), 3000, 3100, 3200, 9090, 9999, 13133 are taken and 80 is privileged on Linux. Recommended host port **8088** (`PREVIEW_PORT`). The displayed URL, the `X-Forwarded-Host` regex, `ret` validation and the probe `Host` header all must include the port. Cookies ignore ports, which matters for Pitfall 4.

### Pitfall 4: Sibling-preview cookie tossing (VERIFIED in Chromium)
**What goes wrong:** From a preview page, `document.cookie = "x=1; Domain=localhost"` was **rejected** (so the platform session cookie on `localhost` cannot be overwritten from a preview, good), but `Domain=preview.localhost` was **accepted**, which makes a cookie visible to every sibling preview. A malicious preview could toss a bad `ephematool_preview` cookie to break or confuse a sibling's auth. **Avoid:** the auth handler iterates *all* same-named cookies; the Cookie-stripping removes all of them; cookies are HMAC-signed and audience-bound so they cannot be forged, only made noisy; production must use a registrable domain for previews that is separate from the dashboard's (and `__Host-` cookie prefix when `Secure` is on). Firefox/Safari behavior for `Domain=localhost` was not tested [ASSUMED]; add to UAT.

### Pitfall 5: The host-only platform cookie plus ports
Cookies are not port-isolated: the platform session cookie on `localhost` is also sent to Grafana (`localhost:3000`) and any other localhost service. Already true in Phase 1; it is **not** sent to `*.preview.localhost` (VERIFIED: the dashboard stub's session value never appeared in the app's request echo). Do not weaken by ever setting `Domain=` on the session cookie.

### Pitfall 6: Private GHCR images
GitHub App installation tokens do not work for ghcr.io pulls, and the Packages REST docs list only OAuth and classic-PAT auth [CITED: community discussions #171423, #74634; docs.github.com packages]. **Verified behavior:** anonymous token + `HEAD /v2/<org>/<repo>/manifests/<tag>` returns 200 for an existing public tag, 404 for a missing tag, and 403 for a nonexistent or private repository (the two are indistinguishable). **Plan:** public packages work with no secret (Phase 2 default and what UAT should use); private packages need an optional `GHCR_READ_TOKEN_FILE` (classic PAT `read:packages`) for the check and a pull secret in the namespace; document it as unsupported-by-default. Map anonymous 403/404 to `image_not_found`.

### Pitfall 7: Branch names containing `/` and URL building
Resolve with `GET /repos/{o}/{r}/git/ref/heads/{branch}` or commits API using go-github, and unit-test branch names like `feature/x`, `release/1.2`, names with `%`, unicode and a leading `-`. Reject by regex before any HTTP call: branch `^[A-Za-z0-9._/-]{1,200}$`, no `..`, no leading `/` or `-`; PR `^[0-9]{1,9}$`; SHA `^[0-9a-f]{7,40}$` (expand a short SHA through the API to 40 chars before use).

### Pitfall 8: Fork PRs and deleted forks
`GET /pulls/{n}`: `head.repo.full_name != base.repo.full_name` or `head.repo == null` ⇒ `pr_not_eligible` (UI copy exists). The head SHA is what gets deployed; its image is the target repo's own CI output, so a fork PR would otherwise let outsiders run code in the cluster.

### Pitfall 9: Helm `--wait` timeout on first install
During the spike, `helm upgrade --install traefik --wait --timeout 240s` returned `context deadline exceeded` although the pod was Running at 4 minutes. Likely the slow first image pull on the new node, not verified. Give bootstrap installs ≥ 5 minutes and pre-pull/`kind load` where possible.

### Pitfall 10: Label values and time
`ephematool.io/expires-at` cannot be RFC3339 (no `:` in label values). Use epoch seconds; parse in Phase 5.

### Pitfall 11: The worker needs more than it has
It currently has only Redis. Add Postgres (app role), GitHub App config, gateway address, and `extra_hosts`. Without a shared config struct split, the API image config and worker diverge; extend `internal/config` and validate (WR-08) for all new durations.

### Pitfall 12: asynq `Unique` semantics
`Unique(ttl)` dedupes on type+payload+queue until the ttl expires or the task completes; a self-re-enqueued poll must not use a fixed `TaskID` (completed tasks keep the id reserved under `Retention`). Use the sweeper's `Unique` and give poll continuations fresh ids.

### Pitfall 13: Collector `memory_limiter` is process-wide
See Pattern 10. A separate limiter with a *higher* threshold protects nothing.

## Code Examples

### Namespace baseline (VERIFIED applying as the provisioner; abbreviated)
```yaml
# Source: spike on kind v0.33.0 / K8s 1.36.4
apiVersion: v1
kind: Namespace
metadata:
  name: pv-abc12345
  labels:
    ephematool.io/managed: "true"
    ephematool.io/env-id: "pv-abc12345"
    ephematool.io/owner: "octo-admin"
    ephematool.io/template: "web-app"
    ephematool.io/expires-at: "1791849600"          # epoch seconds, not RFC3339
    pod-security.kubernetes.io/enforce: baseline
    pod-security.kubernetes.io/warn: restricted
    pod-security.kubernetes.io/audit: restricted
---
apiVersion: v1
kind: ResourceQuota
metadata: {name: ephematool-quota, namespace: pv-abc12345}
spec:
  hard: {requests.cpu: 250m, requests.memory: 256Mi, limits.cpu: 500m, limits.memory: 512Mi, pods: "10",
         count/services: "5", persistentvolumeclaims: "0", services.nodeports: "0", services.loadbalancers: "0"}   # preset S
---
apiVersion: v1
kind: LimitRange
metadata: {name: ephematool-limits, namespace: pv-abc12345}
spec:
  limits: [{type: Container, default: {cpu: 100m, memory: 128Mi}, defaultRequest: {cpu: 50m, memory: 64Mi}, max: {cpu: 500m, memory: 512Mi}}]
---
# NetworkPolicies: default-deny-all (podSelector {}, policyTypes [Ingress, Egress]);
# allow-dns-egress (to kube-system podSelector k8s-app=kube-dns, UDP/TCP 53);
# allow-ingress-from-gateway (namespaceSelector kubernetes.io/metadata.name=traefik);
# allow-otlp-egress (ipBlock 0.0.0.0/0, TCP 4320).
# ServiceAccount ephematool-deployer + RoleBinding to ClusterRole ephematool-deployer (namespaced binding).
```

### Provisioner/deployer RBAC (VERIFIED)
```yaml
# ClusterRole ephematool-provisioner (bound to SA kube-system/ephematool-provisioner)
rules:
- {apiGroups: [""], resources: [namespaces], verbs: [create, get, list, patch]}
- {apiGroups: [""], resources: [resourcequotas, limitranges, serviceaccounts], verbs: [create, get, update, patch]}
- {apiGroups: [networking.k8s.io], resources: [networkpolicies], verbs: [create, get, update, patch]}
- {apiGroups: [rbac.authorization.k8s.io], resources: [rolebindings], verbs: [create, get, update, patch]}
- {apiGroups: [rbac.authorization.k8s.io], resources: [clusterroles], verbs: [bind], resourceNames: [ephematool-deployer]}
- {apiGroups: [""], resources: [serviceaccounts/token], verbs: [create], resourceNames: [ephematool-deployer]}
# ClusterRole ephematool-deployer: CRUD on configmaps, services, pods(+log), PVCs, secrets (Helm release storage), events,
# apps deployments/statefulsets/replicasets (+ deployments/scale), batch jobs/cronjobs, networking ingresses,
# traefik.io middlewares; read-only on resourcequotas, limitranges, networkpolicies. No rbac, no namespaces.
# VAP (CEL) bound with validationActions [Deny], matched on request.userInfo.username == the provisioner SA:
#   namespaces:  object.metadata.name.matches('^pv-[a-z0-9]{8,16}$') && labels['ephematool.io/managed']=='true'
#   quota/limitrange/SA/netpol/rolebinding: namespaceObject.metadata.name.startsWith('pv-') && namespaceObject labels managed=='true'
```
Provisioner kubeconfig: built from the SA token Secret (`kubernetes.io/service-account-token`) plus the cluster CA, written chmod 600 to a git-ignored path that the runner's `.env` points to (`PROVISIONER_KUBECONFIG`). The deployer kubeconfig is built inside the job from the 15-minute token and discarded.

### Create handler order (secure by default)
```go
// 1 resolve role NOW (admin: config; creator: CREATOR_LOGINS or GitHub team via App token), deny+audit if not creator/admin
// 2 allowlist (exact, case-insensitive, fail closed when empty)      -> audit denied/repo_not_allowed (422 repo_not_allowed)
// 3 regex-validate ref input, resolve ref->40-char SHA (App), fork/PR checks   -> audit rejected (ref_not_found|ref_invalid|pr_not_eligible)
// 4 GHCR manifest HEAD for ghcr.io/<repo>:<sha> (digest recorded)  -> audit rejected/image_not_found
// 5 jsonschema.Validate(values) + enum template + size preset      -> audit rejected/schema_violation (collect ALL field errors)
// 6 tx: INSERT environments(status=requested, traceparent, expires_at=now+DEFAULT_TTL) + INSERT audit(success)
// 7 enqueue env:dispatch; on enqueue failure the sweeper picks it up; return 202 {id}
// No GitHub dispatch is made on any 403/422 path (assert in tests).
```

### Audit migration (VERIFIED; omit-by-design: no UPDATE/DELETE/TRUNCATE grant)
```sql
-- +goose Up
-- +goose StatementBegin
DO $$ BEGIN
  IF NOT EXISTS (SELECT FROM pg_roles WHERE rolname = 'ephematool_app') THEN
    CREATE ROLE ephematool_app LOGIN PASSWORD 'ephematool_app';  -- DEV-ONLY; ALTER ROLE out of band elsewhere
  END IF;
END $$;
-- +goose StatementEnd
CREATE TABLE audit_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  occurred_at timestamptz NOT NULL DEFAULT now(),
  actor_github_id bigint NOT NULL, actor_login text NOT NULL,
  action text NOT NULL, environment_id text,
  outcome text NOT NULL CHECK (outcome IN ('success','denied','rejected')),
  reason text NOT NULL DEFAULT '', trace_id text NOT NULL DEFAULT ''
);
CREATE INDEX audit_events_occurred_idx ON audit_events (occurred_at DESC, id DESC);   -- cursor paging
-- +goose StatementBegin
CREATE FUNCTION audit_events_immutable() RETURNS trigger LANGUAGE plpgsql AS $f$
BEGIN RAISE EXCEPTION 'audit_events is append-only' USING ERRCODE = 'restrict_violation'; END $f$;
-- +goose StatementEnd
CREATE TRIGGER audit_events_no_mutation BEFORE UPDATE OR DELETE ON audit_events FOR EACH ROW EXECUTE FUNCTION audit_events_immutable();
CREATE TRIGGER audit_events_no_truncate BEFORE TRUNCATE ON audit_events FOR EACH STATEMENT EXECUTE FUNCTION audit_events_immutable();
REVOKE ALL ON audit_events FROM PUBLIC;
GRANT USAGE ON SCHEMA public TO ephematool_app;
GRANT SELECT, INSERT ON audit_events TO ephematool_app;
```
The audit `reason` strings map to UI-SPEC reasons (Not a creator, Repository not allowed, Ref not found, Image not found, Invalid template options); store a stable code, not copy. Never store tokens or the `traceparent` header value beyond the trace id.

### Field errors from the schema (VERIFIED)
```go
// Source: jsonschema/v6 v6.0.3 (spike)
p := message.NewPrinter(language.English) // nil panics
var walk func(e *jsonschema.ValidationError)
walk = func(e *jsonschema.ValidationError) {
    if len(e.Causes) == 0 {
        out = append(out, FieldError{Field: "values" + pointerToDots(e.InstanceLocation), Code: "schema_violation", Message: e.ErrorKind.LocalizedString(p)})
        return
    }
    for _, c := range e.Causes { walk(c) }
}
```

### Preview token verification (pattern; jwt/v5 option names VERIFIED, code not run)
```go
claims := &TicketClaims{}
_, err := jwt.ParseWithClaims(tok, claims, keyFunc,
    jwt.WithValidMethods([]string{"HS256"}), jwt.WithAudience(envID), jwt.WithIssuer("ephematool"),
    jwt.WithExpirationRequired(), jwt.WithLeeway(5*time.Second))
// then: typ == "ticket", Redis SetNX("preview:jti:"+claims.ID, 120s) must be true
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `workflow_dispatch` returns 204, correlate by guessing | `return_run_details: true` (2022-11-28) or always-200 (2026-03-10) | 2026-02-19 / 2026-03-12 | Use the run id; keep run-name as the fallback |
| ingress-nginx | Gateway API / maintained controllers (Traefik, Envoy Gateway, NGF) | retired 2025-11-11, final releases 2026-03 | D-03 deviation |
| Kind needs Calico for NetworkPolicy | kindnet + kube-network-policies enforces it | kind v0.24 (2024) | GRD-04 costs nothing |
| OPA/webhook admission | ValidatingAdmissionPolicy (CEL), GA 1.30 | 2024 | Provisioner scope without a webhook server |
| Helm 3 | Helm 4.x (3.x near end of life) | Helm 4 GA 2025-11 [ASSUMED date] | Pin 4.3.0 on the runner |

**Deprecated/outdated:**
- `ghinstallation` for this module graph (mismatched go-github/jwt majors).
- CLAUDE.md's STACK section (Node/Fastify/pg-boss) describes the superseded research stack; the repo is Go + asynq + Postgres + Redis. Follow the repo and the Phase 1 research, not that section.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | Target repos' CI tags images with the **full 40-char SHA** (D-11 says `:<sha>`; common tooling emits `sha-<7>`) | Pattern 6 / Pitfall 6 | Every create is rejected `image_not_found`. Make the tag template a config value and confirm with the user |
| A2 | On Linux Docker, pods cannot reach a host service bound to 127.0.0.1 via `host-gateway` | Pattern 10 | Extra bind config; macOS (the user's OS) verified OK |
| A3 | Safari/Firefox connect fine to `*.preview.localhost` via IPv4 fallback and reject `Domain=localhost` cookies | Pitfalls 2, 4 | UAT finding; Chromium verified |
| A4 | Helm 3.18 also works with the chart and token flow | Standard stack | Pin 4.3.0 and fail the version check otherwise |
| A5 | The first-install Helm timeout was image-pull latency | Pitfall 9 | Only affects bootstrap timing |
| A6 | Helm 4 GA date and Helm 3 end-of-life timing | State of the art | Cosmetic |
| A7 | The Traefik chart accepts `ports.web.http.middlewares` as used in the spike and other hardening values (`ingressRoute.dashboard.enabled=false`, `ports.websecure.expose.default=false`) | Pattern 3 | `ports.web.http.middlewares` and `ports.web.nodePort` were exercised; dashboard/api hardening keys were not. Run `helm template` and check at plan time |
| A8 | GitHub applies `X-GitHub-Api-Version: 2022-11-28` + `return_run_details` identically to the published OpenAPI | Pattern 1 | The OpenAPI descriptions (primary source) agree; real-credential behavior is UAT |
| A9 | Requests without `X-Forwarded-*` trust (Traefik overwrites them from the real connection) are sufficient for the auth endpoint | Pattern 3 | The endpoint only makes allow/deny decisions that still require a valid HMAC token, so spoofing gains nothing; revisit if exposed publicly |

## Open Questions (all RESOLVED; one needs user confirmation)

1. **Does the API return a run id from dispatch?** (RESOLVED) Yes. 2022-11-28 with `return_run_details: true` returns 200 + `workflow_run_id`, `run_url`, `html_url`; 2026-03-10 always returns 200 and removes the parameter; both documented in GitHub's OpenAPI. go-github v92 supports the request field and response type and defaults to 2022-11-28. Handle 204 via run-name correlation.
2. **Is ingress-nginx still viable (D-03)?** (RESOLVED, **deviation needs user confirmation**) No: repository archived, retirement announced 2025-11-11 with no fixes after March 2026. Use Traefik v3.7.14 at the same hostnames. If the user insists on ingress-nginx, the `auth-url`/`auth-signin` design works but needs a separate callback Ingress per preview host and carries unpatched risk.
3. **Can a namespace-scoped credential do the whole deploy (D-02)?** (RESOLVED) No, the namespace must be created by something broader. Provisioner (VAP-fenced, local file credential) + 15-minute deployer token; OIDC remains the documented remote-cluster target.
4. **Can the mint step set the preview-host cookie directly (D-04)?** (RESOLVED) No (cross-origin). Ticket in the redirect URL, exchanged by the ForwardAuth handler on the preview host; proven working, including cookie stripping and health exemption.
5. **Does kindnet enforce NetworkPolicy?** (RESOLVED) Yes, verified on kind v0.33.0.
6. **Which JSON Schema library?** (RESOLVED) `santhosh-tekuri/jsonschema/v6` v6.0.3, NEW dependency needing approval.
7. **How does the platform verify team membership at request time without a stored user token?** (RESOLVED) GitHub App installation token with Organization Members: read; no caching beyond the request. The fake's team-membership endpoint must accept the installation token.
8. **Can the GHCR tag check use the App?** (RESOLVED) No. Anonymous OCI check for public packages; optional PAT for private; otherwise reject as `image_not_found`.
9. **Can an Actions run's internals appear as real spans?** (RESOLVED) Not without webhooks/`githubreceiver` or step-level emitters. Synthetic spans from run/job/step timestamps, `traceparent` passed as input for later enhancement.
10. **Collector "rate limits" (PITFALLS 9)?** (RESOLVED) No built-in rate limiter in contrib; use body caps, bounded batches/queues, a lower memory threshold, and defer true rate limiting.
11. **Where does `templates/` live relative to the environments repo (D-10 vs D-14)?** (RESOLVED) Source of truth in this repo; API embeds it; the environments repo content (`environments/`) is published with the same chart versions; `chart_version` input guards against skew.
12. **What is the preview URL given port conflicts?** (RESOLVED) `http://<env-id>.preview.localhost:8088`, port configurable; the UI should render the API-supplied URL rather than constructing it.
13. **Self-hosted runner in Docker or on the host?** (RESOLVED) Host process, ephemeral; Docker would need the kind network and an internal kubeconfig.
14. **Who may read an environment (GET)?** (RESOLVED) Any signed-in allowed user (viewer reads, D-09). The "no access" wording in the UI-SPEC then only applies to nonexistent ids; keep the preview ticket denial identical for both.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Docker Desktop | kind, compose, testcontainers | yes | Engine 27.0.3 | none (blocking) |
| kind | D-01 cluster | yes, but old | v0.29.0 (pin v0.33.0) | Version-check script prints install hint; v0.29.0 also supports NetworkPolicy |
| kubectl | provisioner/verify | yes | 1.36.1 | none |
| helm | runner, verify, chart tests | yes, old | 3.18.4 locally (4.3.0 tested) | Use pinned 4.3.0 from a checksum-verified download |
| bazel / go / node / gh | build, API, UI, runner registration | yes | /opt/homebrew/bin | none |
| Playwright Chromium | e2e | yes | chromium 1228/1243/1248 cached, playwright 1.64.0 | none |
| slopcheck | audit | yes (installed this session) | 0.6.1 | mark ASSUMED |
| Real GitHub App, environments repo, registered runner | manual UAT | not available to research | n/a | UAT item (fake covers automation) |
| ghcr.io reachability | image check | yes | anonymous token and HEAD verified | n/a |

**Missing dependencies with no fallback:** none for automated work. The GitHub App creation, environments repo, and runner registration are developer-performed UAT setup steps.
**Port plan:** 8088 preview gateway, 4320 environment OTLP (127.0.0.1 only), existing 8080/8081/3000/3100/3200/9090/9999/13133 untouched.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go `testing` + testify 1.12.1 (unit; integration tag with testcontainers); Vitest 5 (UI units); Playwright 1.64.0 (e2e) |
| Config file | `.bazelrc` (`--config=integration`), `tools/bzl/go.bzl` (adds `-test.short`), `web/playwright.config.ts` |
| Quick run command | `bazel test //internal/... //cmd/...` (unit, short) |
| Full suite command | `./tools/verify.sh` (extend with kind verification as an optional step) |

### Phase Requirements → Test Map
| Req / SC | Behavior | Test Type | Automated Command | File Exists? |
|----------|----------|-----------|-------------------|-------------|
| ENV-01 / SC1 | Create → dispatch → poll → probe → running with scripted fake run | Go integration (PG+Redis+fakegithub+worker handlers) | `bazel test --config=integration //:test_integration` | Wave 0 |
| ENV-01 / SC1 | Create flow in the browser incl. progress to Running | Playwright on `up_fake` overlay | `bazel run //:e2e` | Wave 0 |
| ENV-01 / SC1 | Provisioning produces a running namespace on real kind (no Actions) | Script | `tools/kind.sh verify` (`bazel run //:kind_verify`) | Wave 0 |
| ENV-01 | Real Actions run on the self-hosted runner | **Manual UAT** | checklist | n/a |
| ENV-02 / SC2 | Allowlist, ref, image, schema, size rejections produce field errors and **zero dispatches** | Go unit + HTTP | `bazel test //internal/envs/...` | Wave 0 |
| ENV-02 | Trimmed form schema, unsupported-construct rules | Go unit (table) + Vitest | same / `bazel test //web:...` | Wave 0 |
| OBS-02 / SC3 | One trace id spans api, worker, synthetic run/job spans and probe | Script via Tempo API | `scripts/verify.d/env-trace.sh` | Wave 0 |
| OBS-03 / SC4 | Sample app telemetry arrives with id/owner/template; spoofed or id-less data dropped | Script (Tempo search / Prometheus `target_info`) + collector config test | `scripts/verify.d/env-telemetry.sh`, `otelcol validate` | Wave 0 |
| GRD-01 / SC4 | Quota and LimitRange exist; extra replicas rejected | `kind_verify` assertions | `bazel run //:kind_verify` | Wave 0 |
| GRD-04 / SC4 | Default-deny blocks egress/ingress; allow rules work | `kind_verify` (busybox exec) | same | Wave 0 |
| AUTH-02 / SC5 | viewer 403, creator 202, admin 202; removed creator denied on the next create; **every operation either has Security or an explicit public marker** | Go unit/integration | `bazel test //internal/auth/... //internal/httpapi/...` | Wave 0 |
| AUTH-03 / SC5 | Creates, denials, rejections recorded; app role cannot UPDATE/DELETE/TRUNCATE; owner blocked by trigger | Go integration (two roles) | `bazel test --config=integration //:test_integration` | Wave 0 |
| ENV-09 / SC6 | Signed-out preview → login → preview; session cookie never reaches the app; cookie stripped; health exempt; `X-Robots-Tag`; replay, wrong-env, expired, forged-alg, open-redirect all denied | Go httptest (token logic) + compose e2e overlay (real Traefik file-provider + whoami) with Playwright | `bazel test //internal/previewauth/...`, `bazel run //:e2e` | Wave 0 |
| ENV-09 | Same through Traefik inside kind | `kind_verify` + manual | `bazel run //:kind_verify` | Wave 0 |

### Sampling Rate
- **Per task commit:** `bazel test //internal/<package>/...` for the touched package.
- **Per wave merge:** `bazel test //...` plus `bazel test --config=integration //:test_integration` (add the new packages: envs, previewauth, audit, ghapp, store).
- **Phase gate:** `./tools/verify.sh` green (extended), then `bazel run //:kind_verify`, then the manual UAT list.

### Wave 0 Gaps
- [ ] Extend `internal/fakegithub` (minimal surface below) with a deterministic control endpoint and a fake preview-health endpoint.
- [ ] `deploy/compose.preview.yaml` overlay: Traefik (file provider, same middleware chain) + whoami echo, so ENV-09 is automatable without kind.
- [ ] `cmd/sampleapp` reference workload (emits OTLP, serves `/health`, echoes request headers for the cookie assertion) and a way to `kind load` it under the expected `ghcr.io/<org>/<repo>:<sha>` name for offline verification.
- [ ] Integration test harness for two DB roles (owner migrates; `ephematool_app` connects).
- [ ] `scripts/verify.d/env-trace.sh`, `env-telemetry.sh`; add `kind_verify`, `kind_up`, `kind_down`, `runner` targets in `//:BUILD.bazel` (sh_binary pattern from `tools/stack.sh`).
- [ ] Add every new Go package to `//:test_integration`; run `bazel run //:gazelle` and `bazel mod tidy` after `go get`; `bazel run //:gen` for sqlc/OpenAPI/TS types.
- [ ] shadcn components via the official CLI; Vitest stubs for Radix select/radio under Bazel (Phase 1 pattern).

### Fake GitHub surface (D-17)
Routes: `POST /app/installations/{id}/access_tokens` (accept a structurally valid RS256 JWT, check `iss`/`exp`, no signature check so no private key is committed; return a short-lived fake token), `GET /orgs/{org}/teams/{slug}/memberships/{login}` (accept installation tokens; existing user-token path stays), `GET /repos/{o}/{r}/git/ref/heads/{b}` and `/commits/{ref}`, `GET /repos/{o}/{r}/pulls/{n}` (fixtures for same-repo, fork, deleted-fork, closed), `POST /repos/{o}/{r}/actions/workflows/{file}/dispatches` (200 with run details when `return_run_details` or version 2026-03-10, else 204, honoring the 25-input and size limits and recording inputs), `GET /repos/{o}/{r}/actions/runs` (filters `event`, `created`; run-name echoes `env_id correlation_id`), `GET .../runs/{id}` with a real `ETag`/`If-None-Match` 304 path, `GET .../runs/{id}/jobs`, `GET /token` + `HEAD /v2/{org}/{repo}/manifests/{tag}` (200 with `Docker-Content-Digest`, 404, 403 scenarios), plus an e2e-only `GET /preview-health` stand-in on the gateway address that returns 200 only after the scripted run succeeds. **Scripted lifecycle:** a run is a small state machine (queued → in_progress → completed:{success|failure|cancelled}). Two drivers: deterministic `POST /__fake/runs/{id}/advance` for Go tests, and an optional auto-advance interval (e.g. `FAKE_RUN_STEP=2s`) for Playwright demos; the scenario (success, fail, stall, ambiguous-204) is chosen by a magic ref such as `fail-*`, `stall-*`, `no-runid-*`. Fixtures also need creator-team members and a user removed between two creates (for the stale-role test).

## Security Domain

### Applicable ASVS Categories (Level 1, block on high)
| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | yes | Existing GitHub OAuth for users; App JWT (RS256, 9 min) for the platform; preview tickets |
| V3 Session Management | yes | Existing scs session stays host-only; separate short-lived env-bound preview cookie (HttpOnly, SameSite=Lax, Secure when `COOKIE_SECURE`); WR-03 guard and WR-10 `Clear()` land here |
| V4 Access Control | yes | `RequireRole` helper, secure-by-default route registration (WR-02), request-time role resolution (WR-01), per-env audience |
| V5 Input Validation | yes | Regexes for ref/repo/env id, allowlist, jsonschema, enums, in-workflow re-validation |
| V6 Cryptography | yes | `golang-jwt/jwt/v5` pinned HS256, 32+ byte key from a mounted file, rotation via `kid`, `crypto/rand` for ids/jti, no hand-rolled MAC |
| V7 Errors/Logging | yes | Append-only audit; no tokens, tickets, cookies, or PEM in logs/spans (extend the Phase 1 redaction check to `ticket=` and `ephematool_preview`) |
| V8 Data Protection | partial | Private key and PAT as files, not env; Traefik access logs off or query-redacted |
| V9 Communications | partial | TLS to GitHub; http on localhost only; production requires https + Secure |
| V10/V12/V14 Config, files | yes | Pod Security, no SA tokens in pods, digest-pinned images, pinned tool versions with checksums |

### Known Threat Patterns
| Pattern | STRIDE | Severity | Standard Mitigation |
|---------|--------|----------|---------------------|
| Compromised/forked-PR code runs on the self-hosted runner | Elevation | **High** | Private environments repo, `workflow_dispatch` only, no fork triggers, workflow-file edit restricted, ephemeral runner, no secrets, runner host holds only the fenced provisioner credential |
| Workflow input injection into shell | Tampering | **High** | Inputs via `env:` only, regex-validated first step, server-side validation before dispatch, fixed template/size lists |
| Preview ticket forgery / replay | Spoofing | **High** | HMAC HS256 pinned, `aud`=env, `jti` single-use in Redis, 60 s TTL, `ret` inside the signed claims |
| Open redirect via `return` | Spoofing | High | Host must equal the env's own preview host; path-only `ret` in signed claim; reject `//`, `\`, CR/LF |
| Platform session reaching preview code | Info disclosure | **High** | Host-only cookie (proved not sent to `*.preview.localhost`), `Domain=localhost` toss rejected in Chromium, preview cookie stripped before upstream, never set `Domain` on the session cookie |
| Cross-preview cookie tossing | Tampering | Medium | Check all same-named cookies, strip all, prod uses a separate registrable domain and `__Host-` prefix |
| Auth bypass by a template/chart | Elevation | **High** | Entrypoint-level middleware chain; exact-path health exemption with method and traversal checks |
| SSRF/injection through repo or ref | Tampering | High | Repo from allowlist only; ref type-specific regex; go-github builds paths; no user-supplied URLs; GHCR host fixed |
| Namespace-name injection | Tampering | High | Server-generated `pv-` ids, regex in workflow, VAP on the provisioner, never branch-derived |
| Cross-namespace access / privilege escalation | Elevation | **High** | Provisioner ClusterRole minimal with `bind`/token `resourceNames`, VAPs, per-run 15 min deployer token, deployer Role without RBAC/NetworkPolicy/Quota write |
| PR-supplied code in previews (egress, lateral movement, resource abuse) | Elevation/DoS | **High** | Default-deny NetworkPolicy, quota/LimitRange, PSA baseline, `automountServiceAccountToken: false`, image digest pinning, only the app image is PR-controlled |
| Audit tampering | Repudiation | High | Two roles, no UPDATE/DELETE/TRUNCATE grant, triggers (owner can still disable: documented) |
| Stale role after demotion | Elevation | High | Request-time re-check for create and audit endpoints |
| Environment telemetry spoofing/flooding | Spoofing/DoS | Medium | Separate receiver/limiter/filters, drop platform-identity and id-less data, body cap; identity is client-asserted (Phase 6 fix) |
| Dispatch rate limiting/ban | DoS | Medium | Serial queue, ≥1 s between POSTs, honor `retry-after`, ETag polling |
| Clickjacking of Create/mint | Tampering | Medium | Add IN-04 security headers (`frame-ancestors 'none'`, nosniff, referrer) to nginx; mint is POST with CrossOriginProtection |
| Secrets in logs/spans | Info disclosure | High | Extend redaction verify to tickets/cookies; Traefik access logs off |

## Locked-decision deviations to confirm (summary for the planner)
| Decision | Issue | Smallest compliant alternative |
|----------|-------|-------------------------------|
| D-03 ingress-nginx | Archived, unsupported | Traefik v3.7.14, same hostnames, same `auth_request`-style semantics via ForwardAuth. **Needs user confirmation** |
| D-02 namespace-scoped credential | Cannot create its own namespace | Fenced provisioner + per-run namespaced deployer token |
| D-04 mint sets preview-host cookie | Cross-origin cookie impossible | Ticket redirect handled by the preview host's own auth hook (same security properties) |
| D-05 "template's ingress serves" the health path | Exemption is enforced by the auth hook, not by an annotation | The chart still routes the path to the app (via `replacePath`), the exemption is exact-path in the auth handler |
| D-11 private GHCR | App tokens cannot read GHCR | Anonymous check (public) + optional PAT |
| D-19 app role INSERT/SELECT only | Needs a second DB role | `MIGRATE_DATABASE_URL` (owner) vs `DATABASE_URL` (`ephematool_app`) |
| UI-SPEC preview URL | Port required | Show `…preview.localhost:8088` from the API-supplied URL |
| OBS-02 | Actions emits no spans | Synthetic spans from run/job/step timestamps, stated honestly |

## Sources

### Primary (HIGH confidence)
- GitHub REST OpenAPI descriptions (`github/rest-api-description`), versions 2022-11-28 and 2026-03-10: dispatch request/response, `return_run_details`, 25-input limit, `enabledForGitHubApps: true`.
- docs.github.com: create-a-workflow-dispatch-event; API versions and breaking changes (2026-03-10); permissions required for GitHub Apps; best practices (ETag/304, polling, secondary limits); rate limits for the REST API; self-hosted runner REST (JIT, registration token); secure-use (self-hosted hardening, script injection); events that trigger workflows (25 inputs, 65,535 chars).
- GitHub changelog 2026-02-19 (run ids) and 2026-03-12 (API version 2026-03-10).
- go-github v92.0.0 module source (dispatch, installation token, API version handling); asynq v0.26.0 source; golang-jwt v5.3.1 parser options; santhosh-tekuri/jsonschema v6.0.3; Helm v4.3.0 go.mod.
- Traefik v3.7.14 `pkg/middlewares/auth/forward.go` and ForwardAuth / entrypoint docs (doc.traefik.io).
- Kubernetes blog 2025-11-11 (ingress-nginx retirement); `gh api` repo metadata (`kubernetes/ingress-nginx` archived, last push 2026-03-23).
- kind v0.24.0 and v0.33.0 release notes (kube-network-policies; node image digests).
- Empirical spike on this machine (2026-10-12): kind v0.33.0 + K8s 1.36.4, Traefik chart 41.7.1, headless Chromium (playwright 1.64.0), curl, Helm 4.3.0, Postgres 18.6-alpine, `otel/opentelemetry-collector-contrib:0.162.0`, ghcr.io anonymous registry calls. Throwaway cluster and containers were deleted.

### Secondary (MEDIUM confidence)
- GitHub community discussions #171423, #74634, #34084 (App tokens vs GHCR); corroborated by docs listing only PAT and GITHUB_TOKEN.
- opentelemetry-collector-contrib issues #6908, #35204, core #6725 (no built-in rate limiting).
- otel-cli README (experimental, TRACEPARENT support).

### Tertiary (LOW confidence, flagged in the Assumptions Log)
- Firefox/Safari behavior for `*.localhost` and `Domain=localhost` cookies; Linux Docker `host-gateway` reachability to loopback-bound ports; Helm 3/4 lifecycle dates.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH. Versions from registries and releases; new Go deps have strong provenance but need human approval.
- Architecture: HIGH for gateway auth, RBAC split, guardrails, audit, collector; MEDIUM for GitHub-side flow (needs real credentials).
- Pitfalls: HIGH where marked VERIFIED; LOW for the other browsers and Linux.

**Research date:** 2026-10-12
**Valid until:** 2026-11-11 for pinned versions (Traefik, kind, runner move weekly); the architecture conclusions are stable beyond that.
