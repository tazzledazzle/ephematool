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
*Last updated: 2026-10-11 after initialization*
