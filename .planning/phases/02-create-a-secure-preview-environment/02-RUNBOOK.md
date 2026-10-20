# Phase 2 Runbook: run it by hand, locally

Sources: Phase 1 SUMMARY files (incl. `01-BAZEL-CONVERSION-SUMMARY.md`, `01-HUMAN-UAT.md`), `.env.example`, `tools/stack.sh`, `tools/verify.sh`, and the 17 Phase 2 PLAN files (`02-01` to `02-17`).

**Read this first**
- Phase 1 commands below are the ones its summaries record. They were not run in the session that wrote this file.
- Phase 2 is **planned, not built**. There are no `02-*-SUMMARY.md` files, and `environments/`, `deploy/kind/` and `tools/kind.sh` do not exist yet. Most Phase 2 tasks *write code*. A shell command cannot replace that. Part C gives you, per plan, the command that proves the plan is done. To have the code written, run `/gsd-execute-phase 2`, then use this runbook to verify and operate it.
- All commands run from the repo root. Everything is `bazel`, `docker`, `kind`, `helm` or `kubectl`. There is no Makefile and no `npm` build step.

---

## Part A. Prerequisites

```bash
docker info >/dev/null && echo docker ok     # Docker Desktop running; needed for compose, testcontainers, kind
bazel --version                              # Bazel 8.2.1 (.bazelversion; bazelisk pins it)
git rev-parse --short HEAD                   # stack.sh stamps this into the build
cp .env.example .env                         # first time only; all defaults work except GitHub values
```

Phase 2 also needs, installed by the repo itself (Part C, plan 02-13): kind v0.33.0 (per RESEARCH) and Helm 4.3.0 into `.tools/bin`. kubectl you install yourself (minor >= `KUBECTL_MIN_MINOR` in `deploy/kind/versions.env`). `gh` must be logged in (`gh auth status`) for the runner step.

## Part B. Phase 1 commands (baseline you build on)

| Purpose | Command |
|---|---|
| Start the stack (10 services) | `bazel run //:up` |
| Start with fake GitHub (no OAuth credentials needed; dev/e2e only) | `bazel run //:up_fake` |
| Follow logs | `bazel run //:logs` |
| Stop (keeps volumes) | `bazel run //:down` |
| Stop and delete volumes | `bazel run //:clean` |
| Regenerate sqlc, OpenAPI, TS types, conventions | `bazel run //:gen` |
| Drift check of generated code | `bazel test //:check_gen` |
| Regenerate Go BUILD files after import or file changes | `bazel run //:gazelle` |
| Unit tests, Vitest, typecheck, drift | `bazel test //...` |
| Docker-backed Go integration tests | `bazel test --config=integration //:test_integration` |
| Playwright e2e (needs the fake stack up) | `bazel run //:e2e` |
| All telemetry checks | `bazel run //:verify_telemetry` |
| One telemetry check | `bazel run //:verify_telemetry -- trace` (also: `backends conventions logs metrics up worker redaction`) |
| Fresh-clone check | `bazel run //:verify_fresh` |
| Full ordered gate | `./tools/verify.sh` |

`./tools/verify.sh` runs, in order: integration tests, `bazel test //...`, `up_fake`, `e2e`, `verify_telemetry`. Run it as a plain script. Never wrap it in `bazel run`, because nested bazel blocks on the output-base lock.

**Smoke test the baseline before starting Phase 2:**
```bash
bazel run //:up_fake
curl -s http://localhost:8080/api/status | head     # all 10 services Healthy, commit stamped
bazel run //:verify_telemetry
bazel run //:down
```
Dashboard http://localhost:8080, Grafana http://localhost:3000. Ports bind to 127.0.0.1 only.

**Phase 1 open items** (`01-HUMAN-UAT.md`, both still pending): real GitHub login with an OAuth App, and "Open in Grafana" trace-to-logs after a real login. For a real login set `GITHUB_CLIENT_ID`, `GITHUB_CLIENT_SECRET`, `ALLOWED_ORGS`, `ADMIN_LOGINS` in `.env`, then `bazel run //:down && bazel run //:up`.

---

## Part C. Phase 2, in wave order

Per-plan gate commands are the `<automated>` verify lines in each PLAN. Run them after the plan's code exists. After any plan that adds or moves Go files, run `bazel run //:gazelle` first. After any plan that changes the API, run `bazel run //:gen`.

### Wave 1

**02-01. Package legitimacy gate (human approval, blocking)**
```bash
curl -fsSL https://get.helm.sh/helm-v4.3.0-darwin-arm64.tar.gz.sha256sum   # spot-check against versions.env
source deploy/kind/versions.env                                            # pins: kind, helm, traefik, whoami, busybox, runner, checkout
! grep -q "golang-jwt\|santhosh-tekuri" go.mod                             # proves nothing installed before approval
```
Approve only after reading the evidence table in the SUMMARY. Do not run `go get` before approving.

**02-02. Config contract and conventions**
```bash
bazel test //internal/config/... && docker compose -f deploy/compose.yaml -f deploy/compose.e2e.yaml config -q && bash -n tools/stack.sh
bazel test //tools/genconventions/... //:check_gen && bash scripts/verify.d/conventions.sh
bash -c 'set -a; . environments/conventions.gen.env; echo $EPHEMATOOL_ATTR_ENV_ID $EPHEMATOOL_LABEL_MANAGED'
```
Expected: `ephematool.env.id ephematool.io/managed`.

**02-03. Fake GitHub: App, repos, Actions, GHCR**
```bash
bazel run //:gazelle && bazel test //internal/fakegithub/... && bazel build //cmd/fakegithub
```
`FAKE_RUN_STEP` is the knob that scripts how far a fake run progresses.

### Wave 2

**02-04. Environments and append-only audit log (two DB roles)**
```bash
bazel run //:gazelle && bazel test //:check_gen //internal/store/... 
bazel test --config=integration //internal/store:store_test //internal/auth:auth_test
bazel test //internal/audit/... //cmd/... && bazel test --config=integration //:test_integration
docker compose -f deploy/compose.yaml config -q
```

**02-05. internal/ghapp (GitHub App client)**. Adds `github.com/golang-jwt/jwt/v5@v5.3.1`, only after 02-01 approval.
```bash
go get github.com/golang-jwt/jwt/v5@v5.3.1 && go mod tidy && bazel mod tidy && bazel run //:gazelle
bazel test //internal/ghapp/... //internal/fakegithub/...
```

**02-06. Creator role, request-time role resolution, secure-by-default routes**
```bash
bazel run //:gazelle && bazel test //internal/auth/... //internal/httpapi/...
# FAILED
bazel test --config=integration //internal/auth:auth_test
bazel run //:gen && bazel test //:check_gen //web:test //web:typecheck
```

### Wave 3

**02-07. Template catalog (web-app chart, sizes.yaml, schema validation)**. Adds `github.com/santhosh-tekuri/jsonschema/v6@v6.0.3`.
```bash
go get github.com/santhosh-tekuri/jsonschema/v6@v6.0.3 && go mod tidy && bazel mod tidy && bazel run //:gazelle
# FAILED
bazel build //templates && bash scripts/verify.d/conventions.sh
python3 -I -c "import json; json.load(open('templates/web-app/values.schema.json'))"
bazel test //internal/catalog/... && bazel build //...
```

**02-08. Preview access (ForwardAuth, tickets, cookies)**
```bash
bazel run //:gazelle && bazel test //internal/previewauth/...
# FAILED
bazel run //:gen && bazel test //internal/previewauth/... //internal/httpapi/... //:check_gen //web:typecheck
```

**02-09. Collector pipelines for environments and `cmd/sampleapp`**
```bash
docker compose -f deploy/compose.yaml config -q
docker run --rm --env-file environments/conventions.gen.env -v "$PWD/deploy/otel:/cfg:ro" \
  otel/opentelemetry-collector-contrib:0.162.0 validate --config=/cfg/collector.yaml
bash -n scripts/verify.d/env-telemetry.sh && bash scripts/verify.d/conventions.sh
# FAILED
bazel run //:gazelle && bazel test //cmd/sampleapp/... && bazel build //cmd/sampleapp
```

### Wave 4

**02-10. Create API (validation pipeline, `pv-<10 chars>` ids, audit on every outcome)**
```bash
# FAILED
bazel run //:gazelle && bazel test //internal/envs/... //internal/queue/...
bazel run //:gen && bazel test //internal/httpapi/... //internal/envs/... //:check_gen //web:typecheck
bazel test --config=integration //:test_integration
docker compose -f deploy/compose.yaml -f deploy/compose.e2e.yaml config -q
```

### Wave 5

**02-11. Worker: dispatch, poll, probe, sweep, plus the env-trace script**
```bash
bazel run //:gazelle && bazel test //cmd/... //internal/...
bazel test --config=integration //:test_integration
# no such file
bash -n scripts/verify.d/env-trace.sh && bash -n scripts/lib/verify-lib.sh && grep -q 'api_post()' scripts/lib/verify-lib.sh
```
Trace proof, with the stack up (`bazel run //:up_fake`): `bazel run //:verify_telemetry -- env-trace` (OBS-02).

**02-12. Dashboard: launch and progress pages**
```bash
bazel test //web:test //web:typecheck && bazel build //web:dist
```

**02-13. kind cluster, Traefik gateway, provisioner RBAC, environments repo content**
```bash
bash -n tools/kind.sh && bazel build //:kind_up //:kind_down //:kind_verify //:kind_tools
bash scripts/verify.d/conventions.sh && grep -q 'authResponseHeaders' deploy/kind/traefik-middlewares.yaml
bazel test //environments:provision_test && bash -n environments/scripts/provision.sh
bazel run //:kind_tools                  # checksum-verified kind + helm into .tools/bin
bazel run //:kind_up                     # needs 127.0.0.1:8088 free
kubectl --context kind-ephematool -n traefik get middleware preview-guard
stat -f %Lp deploy/.secrets/kind-provisioner.kubeconfig                         # expect 600
bazel run //:kind_verify                 # real-cluster assertions, no GitHub needed
curl -s -o /dev/null -w '%{http_code}\n' -H 'Host: pv-kverify001.preview.localhost:8088' http://127.0.0.1:8088/__ephematool/healthz
bazel run //:kind_down
```
Port 8088 is shared with the compose Traefik overlay (02-15). Run only one gateway at a time (`bazel run //:down` before `kind_up`).

### Wave 6

**02-14. Create form and Playwright create flow**
```bash
bazel test //web:test //web:typecheck && bazel build //web:dist
bazel run //:up_fake && bazel run //:e2e -- environments.spec.ts
```

**02-15. Traefik + whoami compose overlay (`deploy/compose.preview.yaml`)**
```bash
docker compose -f deploy/compose.yaml -f deploy/compose.e2e.yaml -f deploy/compose.preview.yaml config -q
bash -n tools/stack.sh && grep -q "frame-ancestors 'none'" deploy/nginx/default.conf
bash -n scripts/verify.d/preview-gate.sh && bash -n scripts/verify.d/redaction.sh
bazel run //:up_fake
bazel run //:verify_telemetry -- preview-gate
bazel run //:verify_telemetry -- redaction
```

### Wave 7

**02-16. Audit log page, preview interstitial, cross-origin specs**
```bash
bazel run //:gazelle && bazel run //:gen
bazel test //internal/httpapi/... //:check_gen //web:test //web:typecheck && bazel build //web:dist
bazel run //:e2e        # runs web/e2e/audit.spec.ts and web/e2e/preview.spec.ts too
```

### Wave 8

**02-17. Runner, publish, README, phase gate**
```bash
bazel build //:runner //:environments_publish
RUNNER_DRY_RUN=1 bash tools/kind.sh runner <org>/<environments-repo>        # prints config.sh --ephemeral --labels ephematool-kind, token masked
./tools/verify.sh                                                          # must end: "verify: all steps passed"
VERIFY_KIND=1 ./tools/verify.sh                                            # adds the kind steps
git ls-files deploy/.secrets                                               # must print nothing
grep -rn 'internal/fakegithub' cmd/api cmd/worker                          # must print nothing
grep -rn 'dangerouslySetInnerHTML' web/src                                 # must print nothing
grep -rn 'ingress-nginx' deploy environments tools templates               # must print nothing
```

---

## Part D. The real path (real GitHub, real cluster)

Needs a GitHub org you control. The README section written in 02-17 is the authoritative version.

1. **GitHub App** (separate from the Phase 1 OAuth App). Permissions exactly: Actions read/write, Contents read, Pull requests read, Metadata read, Organization members read. No Administration, Packages or Workflows. Install it on the environments repo and every `ALLOWED_REPOS` repo. Save the private key under `deploy/.secrets/` (git-ignored).
2. **`.env`:** `GITHUB_APP_ID`, `GITHUB_APP_INSTALLATION_ID`, `GITHUB_APP_PRIVATE_KEY_FILE`, `ENVIRONMENTS_REPO=<org>/<name>`, `ALLOWED_REPOS`, `CREATOR_LOGINS`/`CREATOR_TEAMS`, `ADMIN_LOGINS`, `PREVIEW_DOMAIN=preview.localhost`, `PREVIEW_PORT=8088`, `PREVIEW_TOKEN_KEY_FILE`. Generate the token key with `openssl rand -hex 32 > deploy/.secrets/<name>`.
3. **Environments repo:** private, `workflow_dispatch` only, forks disabled, admin-only changes to `.github/workflows`, no secrets. Then:
   ```bash
   bazel run //:environments_publish -- <checkout-dir>   # copies environments/ and templates/; review, then commit and push yourself
   ```
4. **Cluster:** `bazel run //:kind_tools && bazel run //:kind_up`
5. **Runner (host process, ephemeral; leave it running in its own terminal):** `bazel run //:runner -- <org>/<environments-repo>`. It uses your own `gh` login for the registration token and keeps the provisioner kubeconfig on your machine.
6. **App stack** (not `up_fake`, and not alongside the compose Traefik overlay): `bazel run //:down && bazel run //:up`
7. **Create an environment** in the dashboard http://localhost:8080. The image must exist as a **public** GHCR package tagged `ghcr.io/<org>/<repo>:<full 40-char sha>` (tag format is `IMAGE_TAG_FORMAT`). Expect Requested, Provisioning, Running, then open `http://pv-xxxxxxxxxx.preview.localhost:8088`.
8. **Inspect:**
   ```bash
   kubectl --context kind-ephematool get ns -l ephematool.io/managed=true
   kubectl --context kind-ephematool -n <pv-id> get deploy,pods,resourcequota,networkpolicy
   ```
   The Actions run is traced through Tempo in Grafana (http://localhost:3000). The pipeline span is `github.actions.run`.
9. **Teardown:** `bazel run //:kind_down && bazel run //:down` (add `//:clean` to drop volumes).

Manual UAT items (from 02-17, saved to `02-HUMAN-UAT.md`): real App + repo + runner create reaching Running with a trace; Safari/Firefox preview access signed out and in, including the `Domain=localhost` cookie check; real GHCR image exists vs missing; the ticket and cookie flow through the kind gateway in Chromium; `kind_verify` on your machine.

## Part E. Troubleshooting

- **Bazel hangs:** a `bazel` call nested inside `bazel run` holds the output-base lock. Only the stack wrappers belong in `bazel run`.
- **Port 8088 busy:** the compose Traefik overlay (`up_fake`) and the kind gateway cannot coexist. Run `bazel run //:down` before `kind_up`.
- **Generated files drift:** `bazel run //:gen`, then re-run `bazel test //:check_gen`.
- **Missing Go BUILD target after adding a file or import:** `bazel run //:gazelle`.
- **Unrelated exited kind containers:** 02-13 says not to touch them. Only `kind delete cluster --name ephematool` is yours.
