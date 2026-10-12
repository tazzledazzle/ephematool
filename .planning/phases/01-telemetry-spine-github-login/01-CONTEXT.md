# Phase 1: Telemetry Spine & GitHub Login - Context

**Gathered:** 2026-10-11
**Status:** Ready for planning

<domain>
## Phase Boundary

An SRE signs in with GitHub to a running dashboard. The platform's own API, worker, and dashboard emit OpenTelemetry traces, metrics, and logs that can be found in a local Grafana stack. A fresh checkout starts the whole stack with one command.

Requirements: OBS-01, AUTH-01. Creating environments, roles beyond a minimal admin/viewer split, and any environment features belong to later phases.

</domain>

<decisions>
## Implementation Decisions

### Who can sign in
- **D-01:** Sign-in is restricted to members of configured GitHub org(s)/team(s). The allowlist is plain config.
- **D-02:** Admins are listed by GitHub username in config. Every other allowed org member is a viewer by default. Full viewer/creator/admin role management arrives in Phase 2 (AUTH-02).
- **D-03:** Sessions are sliding 7 days: the user stays signed in across browser sessions while active and expires after 7 days idle. Sign-out is available from the dashboard.
- **D-04:** A GitHub user who authenticates but is not in an allowed org is signed out and shown a clear denied page naming the required org membership. The denial is recorded as a log event.

### Stack (overrides research recommendations)
- **D-05:** Backend language is **Go**, not TypeScript. This supersedes the TypeScript-specific parts of `.planning/research/STACK.md` (Fastify, Drizzle, pg-boss, better-auth, shared Zod schemas, `@kubernetes/client-node`, OTel JS SDK).
- **D-06:** System of record is **Postgres**. The job queue and scheduling use **Redis** (not pg-boss). Redis is a second stateful service the platform must run and observe.
- **D-07:** The dashboard is a **separate React SPA** (Vite). The Go API publishes an OpenAPI spec and the SPA uses a generated typed client. The shared-Zod-schema idea from research no longer applies.
- **D-08:** Because STACK.md is now stale for backend choices, the phase research pass must **re-derive the Go and Redis specifics** (HTTP router, OTel Go SDK and instrumentation, GitHub OAuth and org-membership check library, session handling, Redis queue library, Postgres driver and migration tool, OpenAPI tooling). Treat STACK.md as superseded where it conflicts with D-05 to D-07.

### Telemetry conventions
- **D-09:** Attribute and label prefix is **`ephematool.io/*`** for Kubernetes labels (for example `ephematool.io/env-id`) and **`ephematool.env.*`** for OpenTelemetry attributes. This resolves the label-prefix blocker logged in STATE.md. Encode it once in a single shared module or constants file and reference it everywhere.
- **D-10:** The React dashboard emits its own browser traces with context propagation, so a page load appears as one trace linked to the API trace. This sets up the click-to-Actions-run trace required in Phase 2 (OBS-02).
- **D-11:** Local backends are **Grafana LGTM**: Tempo (traces), Loki (logs), Prometheus (metrics), and Grafana, fed by one OTel Collector gateway.

### One-command dev stack
- **D-12:** One command runs everything with **Docker Compose**: API, worker, SPA, Postgres, Redis, OTel Collector, and the LGTM backends. A kind cluster is not part of Phase 1 and joins in Phase 2 when provisioning needs one.
- **D-13:** After login the dashboard shows a **telemetry status page**: the signed-in user, build info, per-service telemetry health, and a Grafana deep link. This gives the empty shell a purpose and proves the spine works.

### Claude's Discretion
- Go HTTP framework, project layout, Postgres migration tool, OpenAPI generation approach, and Redis queue library, to be chosen during research and planning (see D-08).
- Exact Collector pipeline layout, subject to a separate pipeline for the tool's own telemetry per the research pitfalls.
- Visual design of the status page and login/denied pages.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Project scope
- `.planning/PROJECT.md` — core value, constraints, and key decisions
- `.planning/REQUIREMENTS.md` — OBS-01 and AUTH-01 definitions
- `.planning/ROADMAP.md` §Phase 1 — goal and success criteria

### Research (read with the D-05 to D-08 caveat)
- `.planning/research/SUMMARY.md` — roadmap implications and gaps
- `.planning/research/STACK.md` — SUPERSEDED for backend language, queue, schema sharing, and K8s client; still useful for OTel Collector, LGTM, and Kubernetes guidance
- `.planning/research/ARCHITECTURE.md` — component boundaries, OTel gateway design, telemetry package idea
- `.planning/research/PITFALLS.md` — OTel cardinality (pitfall 8) and Collector sizing (pitfall 9) apply to Phase 1 conventions

</canonical_refs>

<code_context>
## Existing Code Insights

Greenfield repository: no existing code, patterns, or reusable assets. No codebase map exists.

### Integration Points
- Phase 1 establishes the monorepo layout, the shared attribute and label constants (D-09), the Collector gateway, and the auth session that Phase 2 builds on.

</code_context>

<specifics>
## Specific Ideas

No specific references beyond the success criteria in ROADMAP.md. The user chose the recommended option for sign-in, session, label prefix, browser telemetry, LGTM, Compose, and the status page. The two deliberate departures from the research are Go for the backend (D-05) and Redis for the queue (D-06).

</specifics>

<deferred>
## Deferred Ideas

None — discussion stayed within phase scope.

</deferred>

---

*Phase: 1-Telemetry Spine & GitHub Login*
*Context gathered: 2026-10-11*
