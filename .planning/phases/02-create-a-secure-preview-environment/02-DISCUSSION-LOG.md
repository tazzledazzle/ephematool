# Phase 2: Create a Secure Preview Environment - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-10-12
**Phase:** 2-Create a Secure Preview Environment
**Areas discussed:** Cluster & runner reachability, Who can create and what 'owner' means, Templates & the create form, Dispatch / correlation / first probe

---

## Cluster & runner reachability

| Option | Description | Selected |
|--------|-------------|----------|
| Local kind cluster (Recommended) | kind on the dev machine, managed by a Bazel target | ✓ |
| Existing remote cluster | A cluster you already run; needs provider and credentials | |
| Throwaway cloud cluster per session | Managed cluster created and torn down on demand | |

**User's choice:** Local kind cluster

| Option | Description | Selected |
|--------|-------------|----------|
| Self-hosted runner next to the cluster | Runner on the dev machine, namespace-scoped ServiceAccount token | |
| GitHub OIDC to a remote API server | Hosted runners exchange OIDC token for cluster access | ✓ (first answer) |
| Tunnel local kind to hosted runners | Expose kind API server via a tunnel | |

**User's choice (first):** GitHub OIDC to a remote API server
**Notes:** Conflicted with the local kind choice (a local API server is unreachable from hosted runners). Reconciliation question asked.

| Option | Description | Selected |
|--------|-------------|----------|
| kind + OIDC through a tunnel | Keep both via a public tunnel and API-server OIDC config | |
| kind + self-hosted runner | Keep local cluster, drop OIDC | |
| Remote cluster + OIDC | Keep OIDC, give up local cluster | |
| kind now, OIDC in a later phase | Self-hosted runner now; OIDC recorded as the target | ✓ |

**User's choice:** kind now, OIDC in a later phase

| Option | Description | Selected |
|--------|-------------|----------|
| ingress-nginx + <name>.localhost (Recommended) | Wildcard-style hostnames via *.localhost | ✓ |
| Path-based on one host | localhost:PORT/previews/<id>/ | |
| kubectl port-forward on demand | No ingress | |

**User's choice:** ingress-nginx + <name>.localhost

| Option | Description | Selected |
|--------|-------------|----------|
| Open on localhost, noindex header only | | |
| Require the dashboard login | Auth proxy in front of previews | ✓ |
| Neither for now | | |

**User's choice:** Require the dashboard login
**Notes:** Flagged as new scope (no Phase 2 requirement covers it) and as technically awkward (session cookie is host-only on localhost:8080).

| Option | Description | Selected |
|--------|-------------|----------|
| Own phase, right after Phase 2 | New requirement, decimal phase 2.1 | |
| Include in Phase 2 | Build forward-auth and cookie handoff now | ✓ |
| Defer to the remote-cluster phase | | |

**User's choice:** Include in Phase 2

| Option | Description | Selected |
|--------|-------------|----------|
| Separate preview token (Recommended) | Platform session stays host-only; env-bound short-lived token on the preview host | ✓ |
| Shared parent-domain session cookie | Previews receive the platform session (session-theft vector) | |
| Let research decide | | |

**User's choice:** Separate preview token

---

## Who can create, and what 'owner' means

| Option | Description | Selected |
|--------|-------------|----------|
| GitHub team + config list (Recommended) | CREATOR_TEAMS and CREATOR_LOGINS, ADMIN_LOGINS wins | ✓ |
| Config list of logins only | | |
| Any allowed org member can create | | |

**User's choice:** GitHub team + config list

| Option | Description | Selected |
|--------|-------------|----------|
| The creating user's GitHub login (Recommended) | | ✓ |
| The target repo (org/name) | | |
| Both: user as owner, repo as separate label | | |

**User's choice:** The creating user's GitHub login

| Option | Description | Selected |
|--------|-------------|----------|
| Explicit repo allowlist in config (Recommended) | ALLOWED_REPOS, fail closed | ✓ |
| Any repo the GitHub App is installed on | | |
| One fixed repo for Phase 2 | | |

**User's choice:** Explicit repo allowlist in config

| Option | Description | Selected |
|--------|-------------|----------|
| creator creates; admin also destroys others' and sees the full audit log (Recommended) | | ✓ |
| creator and admin equivalent in Phase 2 | | |

**User's choice:** creator creates; admin also destroys others' and sees the full audit log

| Option | Description | Selected |
|--------|-------------|----------|
| Re-check on every create (Recommended) | Create endpoint re-verifies against GitHub and config | ✓ |
| Shorten the session instead | | |
| Accept login-time roles for Phase 2 | | |

**User's choice:** Re-check on every create

---

## Templates & the create form

| Option | Description | Selected |
|--------|-------------|----------|
| Helm charts versioned in this repo (Recommended) | templates/<name>/ with values.schema.json | ✓ |
| Template lives in the target repo | | |
| Raw manifests + Kustomize overlays | | |

**User's choice:** Helm charts versioned in this repo

| Option | Description | Selected |
|--------|-------------|----------|
| Pre-built image from the target repo's CI (Recommended) | Platform deploys, never builds | ✓ |
| Workflow builds the image from the ref | | |
| Demo app only for Phase 2 | | |

**User's choice:** Pre-built image from the target repo's CI

| Option | Description | Selected |
|--------|-------------|----------|
| Convention: ghcr.io/<repo>:<sha> resolved from the ref (Recommended) | | ✓ |
| The creator pastes an image reference | | |
| Template declares the image from a repo variable | | |

**User's choice:** Convention ghcr.io/<repo>:<sha>

**Form fields selected (multi):** Repo (from allowlist); Ref: branch, PR number, or SHA; Template + its schema-driven values; Size preset (S / M / L)

| Option | Description | Selected |
|--------|-------------|----------|
| Stamp a default expires-at label now (Recommended) | Informational until Phase 5 | ✓ |
| No expiry data until Phase 5 | | |

**User's choice:** Stamp a default expires-at label now

---

## Dispatch, correlation & first probe

| Option | Description | Selected |
|--------|-------------|----------|
| A dedicated platform repo with reusable workflows (Recommended) | | ✓ |
| This repo's own .github/workflows | | |
| Workflow in each target repo | | |

**User's choice:** A dedicated platform repo with reusable workflows

| Option | Description | Selected |
|--------|-------------|----------|
| Use returned run ID if present, else correlation_id in run-name (Recommended) | | ✓ |
| correlation_id in run-name only | | |

**User's choice:** Use returned run ID if present, else correlation_id in run-name

| Option | Description | Selected |
|--------|-------------|----------|
| Polling now, webhooks when reachable (Recommended) | | ✓ |
| Webhooks via a tunnel | | |
| Both from day one | | |

**User's choice:** Polling now, webhooks when reachable

| Option | Description | Selected |
|--------|-------------|----------|
| Extend cmd/fakegithub with dispatch + runs (Recommended) | | ✓ |
| Mock the GitHub client in unit tests only | | |

**User's choice:** Extend cmd/fakegithub with dispatch + runs

| Option | Description | Selected |
|--------|-------------|----------|
| A minimal HTTP probe of the preview URL (Recommended) | | ✓ |
| Kubernetes readiness only | | |
| Both, probe decides | | |

**User's choice:** A minimal HTTP probe of the preview URL

| Option | Description | Selected |
|--------|-------------|----------|
| DB-enforced append-only + admin view (Recommended) | | ✓ |
| Append-only by convention + API only | | |
| Structured log lines only | | |

**User's choice:** DB-enforced append-only + admin view

| Option | Description | Selected |
|--------|-------------|----------|
| Probe a platform-injected health path that auth skips (Recommended) | | ✓ |
| Worker presents a service token | | |
| Probe the Service inside the cluster | | |

**User's choice:** Probe a platform-injected health path that auth skips

---

## Claude's Discretion

- Minimal environment record and state set; kind and ingress-nginx versions and Bazel target shape; size-preset values; preview token format; polling cadence and probe bounds; UI layout within the approved design system; GitHub App permissions within least privilege.

## Deferred Ideas

- Webhook receiver; GitHub OIDC to a remote cluster; numeric-id admin pinning; team/repo ownership attributes; untouched Phase 1 review warnings.
