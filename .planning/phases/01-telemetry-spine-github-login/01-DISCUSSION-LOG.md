# Phase 1: Telemetry Spine & GitHub Login - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-10-11
**Phase:** 1-Telemetry Spine & GitHub Login
**Areas discussed:** Who can sign in, Stack confirmation, Telemetry conventions, One-command dev stack

---

## Who can sign in

| Option | Description | Selected |
|--------|-------------|----------|
| GitHub org allowlist (Recommended) | Only members of configured GitHub org(s)/team(s) | ✓ |
| Any GitHub user | Simplest, but anyone could reach a tool that controls clusters | |
| Explicit user allowlist | GitHub usernames in config | |

**User's choice:** GitHub org allowlist

| Option | Description | Selected |
|--------|-------------|----------|
| Config-listed admins (Recommended) | Admin usernames in config; others viewer by default | ✓ |
| First user becomes admin | Zero config, risky on fresh deploy | |
| No roles in Phase 1 | All allowed members equal until Phase 2 | |

**User's choice:** Config-listed admins

| Option | Description | Selected |
|--------|-------------|----------|
| Sliding 7 days (Recommended) | Stays signed in while active; expires after a week idle | ✓ |
| Fixed 24 hours | Tighter, daily re-auth | |
| You decide | Leave to planner | |

**User's choice:** Sliding 7 days

| Option | Description | Selected |
|--------|-------------|----------|
| Clear denied page (Recommended) | Signed out, shown required org, denial logged | ✓ |
| Generic error | Minimal access denied | |

**User's choice:** Clear denied page

---

## Stack confirmation

| Option | Description | Selected |
|--------|-------------|----------|
| TypeScript monorepo (Recommended) | One language, shared schemas, OTel JS SDK 2.x | |
| Go backend + React frontend | Natural fit for k8s tooling, two languages | ✓ |
| Python backend + React frontend | FastAPI + OTel Python | |

**User's choice:** Go backend + React frontend

| Option | Description | Selected |
|--------|-------------|----------|
| Postgres + pg-boss (Recommended) | One datastore for state, queue, cron | |
| Postgres + Redis queue | Conventional queue, second stateful service | ✓ |
| SQLite for now | Simplest local dev, forces later migration | |

**User's choice:** Postgres + Redis queue

| Option | Description | Selected |
|--------|-------------|----------|
| Re-research the stack (Recommended) | Phase research re-derives Go/Redis picks; STACK.md marked superseded | ✓ |
| Lock Go + Redis, planner picks libraries | Record decisions, skip dedicated research | |

**User's choice:** Re-research the stack

| Option | Description | Selected |
|--------|-------------|----------|
| Separate SPA + OpenAPI-generated client (Recommended) | Go API publishes OpenAPI, SPA gets generated client | ✓ |
| Go serves embedded SPA build | Single binary | |
| You decide | Leave to planner | |

**User's choice:** Separate SPA + OpenAPI-generated client
**Notes:** The Go and Redis choices depart from the research recommendations in STACK.md.

---

## Telemetry conventions

| Option | Description | Selected |
|--------|-------------|----------|
| ephematool.io/* (Recommended) | Project-owned prefix; ephematool.env.* for OTel attributes | ✓ |
| ephemeral.* | Shorter, generic, higher clash risk | |
| app.kubernetes.io/* plus custom | Lean on standard labels | |

**User's choice:** ephematool.io/*

| Option | Description | Selected |
|--------|-------------|----------|
| Yes, traces with context propagation (Recommended) | Browser spans link to API traces | ✓ |
| Backend only for now | Add browser leg in Phase 2 | |

**User's choice:** Yes, traces with context propagation

| Option | Description | Selected |
|--------|-------------|----------|
| Grafana LGTM (Recommended) | Tempo, Loki, Prometheus, Grafana via one Collector | ✓ |
| Single all-in-one (grafana/otel-lgtm) | One container, less realistic | |
| Jaeger + Prometheus + Loki | More separate UIs | |

**User's choice:** Grafana LGTM

---

## One-command dev stack

| Option | Description | Selected |
|--------|-------------|----------|
| Docker Compose (Recommended) | Everything in compose; kind joins in Phase 2 | ✓ |
| kind/k3d cluster with Tilt | Closer to production, heavier | |
| Compose now, kind opt-in | Compose plus optional make cluster | |

**User's choice:** Docker Compose

| Option | Description | Selected |
|--------|-------------|----------|
| Telemetry status page (Recommended) | User, build info, per-service telemetry health, Grafana link | ✓ |
| Empty shell with nav | Placeholders only | |
| Just the user profile | Minimal | |

**User's choice:** Telemetry status page

---

## Claude's Discretion

- Go HTTP framework, project layout, migration tool, OpenAPI generation, Redis queue library
- Exact Collector pipeline layout
- Visual design of status, login and denied pages

## Deferred Ideas

None.
