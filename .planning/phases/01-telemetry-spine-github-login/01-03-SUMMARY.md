---
phase: 01-telemetry-spine-github-login
plan: 03
subsystem: infra
tags: [go, asynq, redis, worker, otel, propagation, conventions]

requires:
  - phase: 01-telemetry-spine-github-login
    provides: "queue producer with InjectHeaders, telemetry.Setup, Compose stack, verify harness (01-01)"
provides:
  - cmd/worker binary emitting traces, metrics and logs as ephematool-worker
  - queue.TracingMiddleware (consumer span, task metrics) plus NewServer/NewMux/NewScheduler
  - Redis worker:heartbeat key (45s TTL) written every HEARTBEAT_INTERVAL
  - Compose worker service and verify-telemetry worker/conventions subcommands
  - D-09 conventions generator (JSON -> Go + TS) and grep gate
affects: [01-04, 01-06, phase-2]

tech-stack:
  added: []
  patterns:
    - Consumer-side trace extraction from asynq task Headers via propagation.MapCarrier
    - asynq server/scheduler use uninstrumented RedisClientOpt; only the app go-redis client is instrumented
    - Single JSON source for label/attribute prefixes with generated Go and TS constants

key-files:
  created:
    - cmd/worker/main.go
    - internal/queue/middleware.go
    - internal/queue/middleware_test.go
    - internal/queue/server.go
    - internal/queue/heartbeat.go
    - conventions/telemetry.json
    - tools/genconventions/main.go
    - tools/genconventions/main_test.go
    - internal/conventions/generate.go
    - internal/conventions/conventions.gen.go
    - internal/conventions/conventions_test.go
    - web/src/gen/conventions.gen.ts
    - scripts/verify.d/worker.sh
    - scripts/verify.d/conventions.sh
  modified:
    - deploy/compose.yaml

key-decisions:
  - "Task metric instruments are created inside TracingMiddleware (not package init) so they bind to the meter provider set by telemetry.Setup or a test"
  - "managed is a label-only key; it has no OTel attribute constant"
  - "Conventions grep gate exempts *_test.go because tests must assert the literal values"

patterns-established:
  - "Task metrics carry only task.type and outcome (asserted in TestTaskMetrics)"
  - "Heartbeat tasks start their own root trace; ping tasks continue the API trace"

requirements-completed: [OBS-01]

duration: 25min
completed: 2026-10-11
---

# Phase 1 Plan 03: Worker, trace propagation and D-09 conventions Summary

**One GET /api/status now yields a single Tempo trace across ephematool-api and ephematool-worker (asynq Headers carry traceparent), with trace-correlated worker logs in Loki, task counters in Prometheus, a Redis heartbeat, and D-09 prefixes generated once for Go and TS.**

## Performance

- **Duration:** ~25 min
- **Completed:** 2026-10-11
- **Tasks:** 3
- **Files created:** 14, modified: 1 (plus generated schema, see deviations)

## Accomplishments

- TracingMiddleware extracts context from task Headers, runs a Consumer span named "<type> process", and records `ephematool.queue.task.processed` and `.duration` with only `task.type` and `outcome`.
- Worker binary with graceful shutdown of the asynq server, scheduler and telemetry; Compose `worker` service with no published ports.
- `scripts/verify-telemetry.sh worker` passes against the live stack: both services in one trace, Loki worker lines with the trace_id, counter above zero, `worker:heartbeat` present. `trace` still passes.
- Conventions generator (stdlib only), generated Go and TS constants, `make check-gen` clean, `conventions` grep gate passes.

## Task Commits

1. **Task 1: Failing propagation tests and worker check (RED)** - `5a8c926` (test)
2. **Task 2: Worker, middleware, heartbeat, Compose service (GREEN)** - `52c07ce` (feat)
3. **Task 3 RED: conventions tests** - `17127b8` (test)
4. **Task 3 GREEN: generator, outputs, grep gate** - `7147cf9` (feat)
5. **Generated OpenAPI TS schema** - `87198db` (chore)

## Deviations from Plan

**1. [Rule 3 - Blocking] Generated `web/src/api/schema.d.ts` committed**
- **Found during:** Task 3 (`make check-gen` runs `npm run gen`, which 01-02 pointed at a file that did not exist yet)
- **Issue:** The file appeared untracked after check-gen; check-gen also guards `web/src/api`, so it must be tracked for the drift gate to mean anything.
- **Fix:** Committed it as-is (output of openapi-typescript from api/openapi.json).
- **Commit:** 87198db

**2. [Minor] Grep gate exempts `*_test.go`**
- Tests assert literal prefix values, so the gate excludes test files in addition to generated files and conventions/telemetry.json.

**3. [Minor] `internal/queue/tasks.go` not modified**
- Plan listed it, but no change was needed.

**4. [Minor] Task 3 was committed as test then feat; the generator test needed one whitespace fix**
- The initial assertion assumed single-space alignment; gofmt aligns constants, so the assertion became a regexp. Fixed before the GREEN commit.

Total: 1 blocking, 3 minor. No scope change; go.mod and go.sum untouched.

## Issues Encountered

None blocking. PING_THROTTLE can skip an enqueue, so `worker.sh` retries up to 3 times with an 11s sleep, as planned.

## Known Stubs

None. (Heartbeat is consumed by the status endpoint in a later plan.)

## Threat Flags

None. Mitigations in place: T-01-14 (no redisotel in server.go, grep clean), T-01-15 (TestTaskMetrics asserts two attributes), T-01-17 (check-gen plus conventions gate).

## Self-Check: PASSED

- Files verified present; commits 5a8c926, 52c07ce, 17127b8, 7147cf9, 87198db exist.
- `go test ./... -short`, `go vet ./...`, `make check-gen`, and `verify-telemetry.sh worker|trace|conventions` all pass; the stack was stopped afterwards.
