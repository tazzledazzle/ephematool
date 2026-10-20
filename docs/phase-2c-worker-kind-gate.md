# Phase 2c · Worker, kind cluster and preview gate

**Objective:** make a created environment actually come up. The worker drives the forward-only state machine `requested → provisioning → running | failed` through the GitHub workflow and a health probe, and turns the GitHub Actions run into synthetic spans in the same trace as the create request. A local kind cluster with a fenced provisioner, namespace guardrails and a Traefik gateway gives previews somewhere real to run. A compose copy of the same gateway makes the preview access rules testable without a cluster.

**Design references:** `.planning/phases/02-create-a-secure-preview-environment/` `02-CONTEXT.md` (D-14, D-15, D-18, D-20, D-21, D-22, D-23, D-24, D-25, D-26), `02-RESEARCH.md` (Patterns 3, 8, 9; Pitfalls 3, 5, 9, 10, 11), `02-PATTERNS.md` (sections for the worker, kind and gate files), `02-VALIDATION.md` (ENV-09, GRD-01, GRD-04, OBS-02, OBS-03).
**Plans covered:** 02-11 (worker and trace), 02-13 (kind, provisioner, baseline, environments repo content), 02-15 (compose preview gate, redaction gate, dashboard headers).
**Requirements covered:** ENV-01, ENV-09, GRD-01, GRD-04, OBS-02, OBS-03.
**Prerequisite:** Phase 2b acceptance checklist complete (`phase-2b-create-api.md`, section 13). In particular `POST /api/environments` returns `202` and leaves the row `requested`, and the test fixture in `internal/envs/create_integration_test.go` has the `cfg config.Config` and `gh *ghapp.Client` fields that this phase's integration tests reuse.
**Produces (later phases rely on these exact names):**

| Artifact | Used later for |
|---|---|
| `envs.Worker`, `NextPollDelay`, the four `Reason*` strings and four `Status*` values | dashboard copy for failed environments (2d); inventory and lifecycle (Phase 3) |
| `queue.Client.EnqueueEnv`, `queue.NewDispatchServer`, `queue.RegisterSweep` | TTL teardown tasks (Phase 5) use the same client, server and scheduler |
| spans `github.actions.run`, `.job`, `.step`, `env.probe` | the per-environment trace panel (Phase 6) |
| `deploy/kind/*`, `tools/kind.sh`, `bazel run //:kind_up`, `//:kind_verify` | the runner and the manual UAT (2d) |
| `environments/` (workflow, `provision.sh`, `baseline/`) | published to the private environments repository (2d) |
| `deploy/compose.preview.yaml`, `scripts/verify.d/preview-gate.sh` | the Playwright spec for the access flow (2d) |
| `api_post` in `scripts/lib/verify-lib.sh` | every later verify script that needs a state-changing request |

> **How to read this document.** Every file is complete: copy it to the path shown. **replace** means take the whole file; **edit** shows the change. Commands are `bash` from the repository root. Where a file is marked "gazelle", run `bazel run //:gazelle` and let it write the `BUILD.bazel`. The document has three parts that you can check one at a time: **A** the worker (sections 2–8, checked on the fake stack), **B** the kind cluster (sections 9–10, checked with `kind_verify`), **C** the compose preview gate (section 11).

---

## 1. What this phase adds

```
internal/
├── envs/{worker.go,workerstore.go,pgworkerstore.go,dispatch.go,poll.go,probe.go,sweep.go,spans.go,register.go}
│       {worker_test.go,spans_test.go,worker_integration_test.go}
│       create_integration_test.go                    # already edited in 2b (cfg and gh fields)
├── queue/{client.go,server.go,enqueue_env_test.go}  # replace / new test
├── previewauth/forwardauth.go                       # 2b already strips the session cookie too
└── store/queries/environments.sql                   # edit: ReleaseDispatch
cmd/worker/main.go                                   # replace
deploy/
├── compose.e2e.yaml                                 # replace (worker block)
├── compose.preview.yaml, traefik/dynamic.yaml       # new: Traefik + whoami
├── nginx/default.conf                               # replace: anti-framing headers, Host $http_host
└── kind/{versions.env,cluster.yaml,traefik-values.yaml,traefik-middlewares.yaml,
          bootstrap-rbac.yaml,admission-policies.yaml}
environments/
├── .github/workflows/create-environment.yml
├── scripts/{provision.sh,provision_test.sh}
├── baseline/{Chart.yaml,values.yaml}
│         └── templates/{_helpers.tpl,resourcequota.yaml,limitrange.yaml,networkpolicies.yaml,serviceaccounts.yaml,rolebinding.yaml}
├── BUILD.bazel, README.md
tools/{kind.sh,stack.sh}                             # new / replace
scripts/
├── lib/verify-lib.sh                                # edit: api_post
└── verify.d/{env-trace.sh,preview-gate.sh,redaction.sh}
BUILD.bazel                                          # edit: kind_* targets, test_integration
```

```bash
mkdir -p deploy/kind deploy/traefik environments/{.github/workflows,scripts,baseline/templates} cmd/worker
```

---

# Part A · The worker

## 2. How the worker behaves

Four asynq tasks, all carrying only the environment id. Everything else is read from Postgres, so a task can be retried, duplicated or lost without changing the outcome.

```
requested ──ClaimDispatch (CAS)──▶ POST workflow_dispatch ──▶ provisioning ──▶ run concludes ──▶ probe ──2xx──▶ running
    │                                  │ ambiguous                │ failure / cancelled / deadline              │ window ends
    └──────────────────────────────────┴──────────────────────────┴──────────────▶ failed ◀────────────────────┘
```

| Task | Queue | What it does |
|---|---|---|
| `env:dispatch` | `dispatch`, concurrency 1 | claims the dispatch with a compare-and-set, POSTs the workflow, records the run id, moves to `provisioning`, schedules the first poll |
| `env:poll` | default | conditional `GET` of the run (ETag, so an unchanged run costs no rate limit); on completion lists jobs, emits spans, schedules the probe or fails the environment |
| `env:probe` | default | `GET http://<PREVIEW_GATEWAY_ADDR>/__ephematool/healthz` with the preview `Host`; a 2xx marks `running`; the window running out fails the environment |
| `env:sweep` | default, scheduled | finds active environments nobody is working on (a lost task, a restart) and re-enqueues them under a uniqueness window |

The rules that make this safe to run twice:

| Rule | Why |
|---|---|
| `ClaimDispatch` is a CAS on `dispatch_attempted_at`: exactly one delivery POSTs | at-least-once delivery must not start two workflows |
| After an **ambiguous** result (a `204` with no run id, or a transport error) the worker **never POSTs again**. It searches for the run by correlation id for 2 minutes, then fails with "Could not confirm the workflow dispatch" | a second POST would create a second environment |
| `ReleaseDispatch` runs **only** after an explicit rate-limit refusal | there the request was provably not performed, so retrying is safe |
| POSTs are serial (dispatch queue, concurrency 1) and spaced by `ENV_DISPATCH_SPACING` | GitHub's secondary rate limits punish bursts |
| Every state change is a forward-only CAS | a late duplicate cannot move an environment backwards |
| Spans are emitted only by the delivery that wins `MarkRunConcluded` | a retried poll must not duplicate the trace |
| A running environment requires a probe 2xx, not just a green run | "live" must not rely on workflow success alone (D-18) |
| Every pass touches the row; the sweeper re-enqueues only rows untouched for `staleAfter` | otherwise the sweeper plus self-re-enqueuing polls would multiply poll chains |

The failure reasons are user-facing copy, rendered verbatim by the dashboard: "Actions run failed", "Actions run was cancelled", "Timed out waiting for the Actions run (15m)" (the duration comes from `ENV_PROVISION_DEADLINE`), "The workflow dispatch was rejected", "Could not confirm the workflow dispatch", "Preview did not become healthy", "GitHub App not configured".

**What the trace can and cannot show.** GitHub Actions emits no spans. The worker rebuilds `github.actions.run`, `.job` and `.step` from the API's timestamps after the run completes, with `trace.WithTimestamp`, under the traceparent stored when the environment was created. They have one-second resolution, no step internals, and appear when the run ends, not live. An `env.probe` span records `env.probe.result` as `healthy` or `unhealthy`.

---

## 3. Queue and SQL additions (plan 02-11, task 1)

`EnqueueEnv` routes `env:dispatch` to the `dispatch` queue, retries a task at most 3 times, and sets a uniqueness window only when asked (the sweeper asks; continuations never do, and no `TaskID` is ever set, because a completed task id would block the next enqueue).

### 3.1 `internal/queue/client.go` (replace)

`internal/queue/client.go`

```go
package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/hibiken/asynq"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Client wraps an asynq producer. It owns its own Redis connection, which is
// deliberately not instrumented (Pitfall 4).
type Client struct {
	c *asynq.Client
}

// NewClient connects an asynq client to redisAddr.
func NewClient(redisAddr string) *Client {
	return &Client{c: asynq.NewClient(asynq.RedisClientOpt{Addr: redisAddr})}
}

// Close releases the Redis connection.
func (c *Client) Close() error { return c.c.Close() }

// EnqueuePing publishes a telemetry:ping task under a producer span and carries
// the W3C trace context in the task headers.
func (c *Client) EnqueuePing(ctx context.Context) error {
	ctx, span := otel.Tracer("ephematool/queue").Start(ctx, "telemetry:ping publish",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "asynq"),
			attribute.String("messaging.destination.name", "default"),
		))
	defer span.End()

	task := asynq.NewTaskWithHeaders(TypePing, nil, InjectHeaders(ctx))
	if _, err := c.c.EnqueueContext(ctx, task, asynq.MaxRetry(1)); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}

// EnqueueEnvDispatch publishes an env:dispatch task for envID on the dispatch queue. The W3C
// trace context rides in the task headers, so the worker's spans join the create request's trace.
func (c *Client) EnqueueEnvDispatch(ctx context.Context, envID string) error {
	ctx, span := otel.Tracer("ephematool/queue").Start(ctx, "env:dispatch publish",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "asynq"),
			attribute.String("messaging.destination.name", QueueDispatch),
		))
	defer span.End()

	payload, err := json.Marshal(EnvPayload{EnvID: envID})
	if err != nil {
		return err
	}
	task := asynq.NewTaskWithHeaders(TypeEnvDispatch, payload, InjectHeaders(ctx))
	if _, err := c.c.EnqueueContext(ctx, task, asynq.Queue(QueueDispatch), asynq.MaxRetry(3)); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}

// EnqueueEnv publishes a follow-up env:* task (poll, probe, sweep or a dispatch retry).
//
//   - processIn delays the task (poll backoff, probe cadence); 0 runs it now.
//   - uniqueFor > 0 drops a duplicate of the same type and env id for that long. Only the sweeper
//     passes it. Continuations pass 0 and never set a task id: asynq keeps a finished task's id
//     reserved, so a fixed id would block the next poll (Pitfall 12).
//
// The W3C trace context comes from ctx, so every task joins the environment's single trace.
// The payload is the bare env id: nothing else is ever carried on a task.
func (c *Client) EnqueueEnv(ctx context.Context, typ, envID string, processIn, uniqueFor time.Duration) error {
	q := "default"
	if typ == TypeEnvDispatch {
		q = QueueDispatch
	}
	ctx, span := otel.Tracer("ephematool/queue").Start(ctx, typ+" publish",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "asynq"),
			attribute.String("messaging.destination.name", q),
		))
	defer span.End()

	payload, err := json.Marshal(EnvPayload{EnvID: envID})
	if err != nil {
		return err
	}
	opts := []asynq.Option{asynq.Queue(q), asynq.MaxRetry(3)}
	if processIn > 0 {
		opts = append(opts, asynq.ProcessIn(processIn))
	}
	if uniqueFor > 0 {
		opts = append(opts, asynq.Unique(uniqueFor))
	}
	task := asynq.NewTaskWithHeaders(typ, payload, InjectHeaders(ctx))
	if _, err := c.c.EnqueueContext(ctx, task, opts...); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return fmt.Errorf("enqueue %s: %w", typ, err)
	}
	return nil
}
```
### 3.2 `internal/queue/server.go` (replace)

`NewDispatchServer` serves **only** the `dispatch` queue with concurrency 1: that is the whole serial-dispatch mechanism. `RegisterSweep` registers the periodic sweep with the scheduler.

`internal/queue/server.go`

```go
package queue

import (
	"time"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// NewServer builds the asynq consumer for the default queue (poll, probe, sweep, heartbeat).
// It uses a plain RedisClientOpt, so its polling traffic is never traced (Pitfall 4).
func NewServer(redisAddr string) *asynq.Server {
	return asynq.NewServer(asynq.RedisClientOpt{Addr: redisAddr}, asynq.Config{
		Concurrency: 4,
		Queues:      map[string]int{"default": 1},
	})
}

// NewDispatchServer builds the consumer for the dispatch queue ONLY, with concurrency 1: GitHub
// asks for workflow dispatches to be serial and about one second apart, and a dispatch is not
// idempotent. It must never share a server with the default queue.
func NewDispatchServer(redisAddr string) *asynq.Server {
	return asynq.NewServer(asynq.RedisClientOpt{Addr: redisAddr}, asynq.Config{
		Concurrency: 1,
		Queues:      map[string]int{QueueDispatch: 1},
	})
}

// NewMux registers the heartbeat and ping handlers wrapped in TracingMiddleware. rdb is the
// instrumented application client used by the heartbeat. The worker adds the env handlers.
func NewMux(rdb *redis.Client) *asynq.ServeMux {
	mux := asynq.NewServeMux()
	mux.Use(TracingMiddleware)
	mux.HandleFunc(TypePing, pingHandler)
	mux.HandleFunc(TypeHeartbeat, heartbeatHandler(rdb))
	return mux
}

// NewDispatchMux is an empty mux with the tracing middleware, for the dispatch server.
func NewDispatchMux() *asynq.ServeMux {
	mux := asynq.NewServeMux()
	mux.Use(TracingMiddleware)
	return mux
}

// NewScheduler registers the periodic heartbeat task.
func NewScheduler(redisAddr string, every time.Duration) (*asynq.Scheduler, error) {
	s := asynq.NewScheduler(asynq.RedisClientOpt{Addr: redisAddr}, nil)
	if _, err := s.Register("@every "+every.String(), asynq.NewTask(TypeHeartbeat, nil),
		asynq.Unique(uniqueFor(every)), asynq.MaxRetry(0)); err != nil {
		return nil, err
	}
	return s, nil
}

// RegisterSweep adds the periodic env:sweep task to s. MaxRetry(0): the next tick is the retry.
func RegisterSweep(s *asynq.Scheduler, every time.Duration) error {
	_, err := s.Register("@every "+every.String(), asynq.NewTask(TypeEnvSweep, nil),
		asynq.Unique(uniqueFor(every)), asynq.MaxRetry(0))
	return err
}

// uniqueFor is one second shorter than the period, floored at one second, so each tick's task is
// deduplicated against a slow previous one without ever blocking the next tick.
func uniqueFor(every time.Duration) time.Duration {
	if u := every - time.Second; u >= time.Second {
		return u
	}
	return time.Second
}
```
### 3.3 `internal/queue/enqueue_env_test.go` (new)

`internal/queue/enqueue_env_test.go`

```go
package queue

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
)

func TestEnqueueEnvRoutesAndDeduplicates(t *testing.T) {
	mr := miniredis.RunT(t)
	c := NewClient(mr.Addr())
	t.Cleanup(func() { _ = c.Close() })
	insp := asynq.NewInspector(asynq.RedisClientOpt{Addr: mr.Addr()})
	t.Cleanup(func() { _ = insp.Close() })
	ctx := context.Background()

	// Dispatch goes to its own queue; everything else to default.
	if err := c.EnqueueEnv(ctx, TypeEnvDispatch, "pv-aaaaaaaaaa", 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := c.EnqueueEnv(ctx, TypeEnvPoll, "pv-aaaaaaaaaa", 3*time.Second, 0); err != nil {
		t.Fatal(err)
	}
	d, err := insp.ListPendingTasks(QueueDispatch)
	if err != nil || len(d) != 1 || d[0].Type != TypeEnvDispatch {
		t.Fatalf("dispatch queue: %v %v", d, err)
	}
	var p EnvPayload
	if err := json.Unmarshal(d[0].Payload, &p); err != nil || p.EnvID != "pv-aaaaaaaaaa" {
		t.Fatalf("payload %q (%v): want only the env id", d[0].Payload, err)
	}
	sched, err := insp.ListScheduledTasks("default")
	if err != nil || len(sched) != 1 || sched[0].Type != TypeEnvPoll {
		t.Fatalf("a delayed poll should be scheduled on default: %v %v", sched, err)
	}

	// Unique: a second sweeper-style enqueue inside the window is refused; continuations
	// (uniqueFor 0) are never refused.
	if err := c.EnqueueEnv(ctx, TypeEnvProbe, "pv-bbbbbbbbbb", 0, 20*time.Second); err != nil {
		t.Fatal(err)
	}
	if err := c.EnqueueEnv(ctx, TypeEnvProbe, "pv-bbbbbbbbbb", 0, 20*time.Second); err == nil {
		t.Fatal("duplicate unique enqueue should be refused")
	}
	for i := 0; i < 2; i++ {
		if err := c.EnqueueEnv(ctx, TypeEnvPoll, "pv-cccccccccc", time.Second, 0); err != nil {
			t.Fatalf("continuation %d: %v", i, err)
		}
	}
}

func TestUniqueFor(t *testing.T) {
	if got := uniqueFor(30 * time.Second); got != 29*time.Second {
		t.Errorf("30s -> %v", got)
	}
	if got := uniqueFor(time.Second); got != time.Second {
		t.Errorf("1s -> %v (floor)", got)
	}
}
```
### 3.4 `internal/store/queries/environments.sql` (edit)

Append this query. It is the only way a claimed dispatch can be undone.

`internal/store/queries/environments.sql`

```sql
-- name: ReleaseDispatch :execrows
-- Undo a claim ONLY after GitHub explicitly refused the dispatch (rate limit), where the request
-- was not performed. Never after an ambiguous result: that path must search, never re-POST.
update environments
set dispatch_attempted_at = null, updated_at = now()
where id = $1 and status = 'requested' and dispatch_attempted_at is not null;
```
```bash
bazel run //:gen          # regenerates internal/store/*.sql.go (sqlc)
grep -n 'func (q \*Queries) ReleaseDispatch' internal/store/*.sql.go    # one match
```

> **Check the generated names.** `pgworkerstore.go` (below) reads the sqlc row types. sqlc spells initialisms its own way, so after `bazel run //:gen` open the generated `Environment` struct and make sure the fields the adapter uses (`RunHtmlUrl`, `RunEtag`, `Sha`) exist under exactly those names. If sqlc produced `RunHTMLURL` or similar, rename them in `pgworkerstore.go`; nothing else depends on them.

---

## 4. `internal/envs`: the worker (plan 02-11, task 1)

The package is split on purpose: **pure logic** (`worker.go`, `dispatch.go`, `poll.go`, `probe.go`, `sweep.go`) that only talks to interfaces, and **thin adapters** (`pgworkerstore.go` for sqlc, `spans.go` for OpenTelemetry, `register.go` for asynq). The unit tests run the logic against fakes, so the state machine is tested without Docker.

### 4.1 `worker.go`: types, interfaces, failure reasons, poll delay

`NextPollDelay` is `initial × 1.5^attempt`, capped at `ENV_POLL_MAX`, with ±20 % jitter so a restart does not make every environment poll in the same second.

`internal/envs/worker.go`

```go
package envs

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ephematool/ephematool/internal/config"
	"github.com/ephematool/ephematool/internal/ghapp"
	"github.com/ephematool/ephematool/internal/queue"
)

// Failure reasons. The dashboard renders these verbatim, so they are user-facing copy.
const (
	ReasonRunFailed           = "Actions run failed"
	ReasonRunCancelled        = "Actions run was cancelled"
	ReasonDispatchRejected    = "The workflow dispatch was rejected"
	ReasonDispatchUnconfirmed = "Could not confirm the workflow dispatch"
	ReasonNotHealthy          = "Preview did not become healthy"
	ReasonNoGitHub            = "GitHub App not configured"
)

// reasonTimeout is "Timed out waiting for the Actions run (15m)"; the duration comes from config.
func reasonTimeout(d time.Duration) string {
	s := d.String() // 15m0s -> 15m, 1h0m0s -> 1h
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return fmt.Sprintf("Timed out waiting for the Actions run (%s)", s)
}

// Environment statuses the worker moves between. Forward only: requested -> provisioning ->
// running, or failed from either of the first two.
const (
	StatusRequested    = "requested"
	StatusProvisioning = "provisioning"
	StatusRunning      = "running"
	StatusFailed       = "failed"
)

const (
	// dispatchConfirmWindow is how long the worker keeps searching for a run it cannot see after
	// an ambiguous dispatch. It never POSTs again; past the window the environment fails.
	dispatchConfirmWindow = 2 * time.Minute
	ambiguousRetryIn      = 3 * time.Second
	confirmRetryIn        = 5 * time.Second
)

// GitHub is what the worker needs from the platform's GitHub client. *ghapp.Client satisfies it.
type GitHub interface {
	Dispatch(ctx context.Context, in ghapp.DispatchInput) (ghapp.DispatchResult, error)
	FindRunByCorrelation(ctx context.Context, correlationID string, since time.Time) (ghapp.Run, bool, error)
	GetRun(ctx context.Context, runID int64, etag string) (run ghapp.Run, newETag string, notModified bool, err error)
	ListJobs(ctx context.Context, runID int64) ([]ghapp.Job, error)
}

// EnvEnqueuer schedules the next env:* task. processIn delays it; uniqueFor > 0 makes the
// enqueue idempotent for that long (used by the sweeper only: continuations never use it).
// *queue.Client satisfies it.
type EnvEnqueuer interface {
	EnqueueEnv(ctx context.Context, typ, envID string, processIn, uniqueFor time.Duration) error
}

// WorkerDeps are the collaborators of a Worker.
type WorkerDeps struct {
	Cfg     config.Config
	Store   WorkerStore
	GH      GitHub // nil when the GitHub App is not configured: environments then fail with ReasonNoGitHub
	Enqueue EnvEnqueuer
	HTTP    *http.Client     // probe client; nil builds a traced one with the configured timeout
	Now     func() time.Time // tests only
	Sleep   func(context.Context, time.Duration) error
	Rand    func() float64 // tests only: jitter source in [0,1)
}

// Worker owns the four env:* handlers. Every handler is idempotent: it reloads the row, does one
// compare-and-set transition, and returns nil when another delivery already won.
type Worker struct {
	cfg   config.Config
	st    WorkerStore
	gh    GitHub
	enqr  EnvEnqueuer
	hc    *http.Client
	now   func() time.Time
	sleep func(context.Context, time.Duration) error
	rnd   func() float64

	dmu          sync.Mutex // serialises dispatch POSTs and their spacing
	lastDispatch time.Time
}

// NewWorker builds a Worker, defaulting every optional dependency.
func NewWorker(d WorkerDeps) *Worker {
	w := &Worker{cfg: d.Cfg, st: d.Store, gh: d.GH, enqr: d.Enqueue, hc: d.HTTP, now: d.Now, sleep: d.Sleep, rnd: d.Rand}
	if w.now == nil {
		w.now = time.Now
	}
	if w.sleep == nil {
		w.sleep = func(ctx context.Context, dur time.Duration) error {
			t := time.NewTimer(dur)
			defer t.Stop()
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-t.C:
				return nil
			}
		}
	}
	if w.rnd == nil {
		w.rnd = rand.Float64
	}
	if w.hc == nil {
		w.hc = newProbeClient(d.Cfg.EnvProbeTimeout)
	}
	return w
}

// NextPollDelay is initial*1.5^attempt capped at max, then +/-20% jitter. rnd is in [0,1).
func NextPollDelay(attempt int, initial, max time.Duration, rnd func() float64) time.Duration {
	if attempt < 0 {
		attempt = 0
	}
	d := float64(initial) * math.Pow(1.5, float64(attempt))
	if d > float64(max) {
		d = float64(max)
	}
	return time.Duration(d * (0.8 + 0.4*rnd()))
}

// pollAttempt recovers "which poll is this" from the time since dispatch, so the task payload
// stays a bare env id. It inverts the sum of the un-jittered delays 3, 4.5, 6.75, ...
func pollAttempt(elapsed, initial time.Duration) int {
	if elapsed <= 0 || initial <= 0 {
		return 0
	}
	n := math.Log(1+elapsed.Seconds()/(2*initial.Seconds())) / math.Log(1.5)
	return int(n)
}

// ProbeHost is the Host header the probe sends: "<id>.<domain>[:<port>]".
func ProbeHost(cfg config.Config, envID string) string { return cfg.PreviewHost(envID) }

// load reloads the row. A missing row is not an error: the task is simply stale.
func (w *Worker) load(ctx context.Context, id string) (EnvRow, bool, error) {
	env, err := w.st.Get(ctx, id)
	if errors.Is(err, ErrNotFound) {
		return EnvRow{}, false, nil
	}
	return env, err == nil, err
}

// enq schedules the next task. Continuations pass uniqueFor == 0 and never a fixed task id:
// asynq keeps a completed task's id reserved, so a fixed id would block the next poll.
func (w *Worker) enq(ctx context.Context, typ, id string, processIn, uniqueFor time.Duration) error {
	if w.enqr == nil {
		return errors.New("envs: worker has no enqueuer")
	}
	return w.enqr.EnqueueEnv(ctx, typ, id, processIn, uniqueFor)
}

// fail is the only way the worker marks an environment failed. A lost CAS (someone else already
// moved the row) is not an error.
func (w *Worker) fail(ctx context.Context, env EnvRow, reason string) error {
	won, err := w.st.MarkFailed(ctx, env.ID, reason)
	if err != nil {
		return err
	}
	if won {
		slog.WarnContext(ctx, "env.failed", "env", env.ID, "reason", reason)
	}
	return nil
}

// touch records "a worker looked at this row just now". The sweeper only re-enqueues rows that
// have been quiet for staleAfter, which is what stops it from starting a second poll chain.
func (w *Worker) touch(ctx context.Context, env EnvRow) {
	if err := w.st.Touch(ctx, env.ID, env.RunETag); err != nil {
		slog.WarnContext(ctx, "env.touch_failed", "env", env.ID, "err", err)
	}
}

// staleAfter is how long a non-terminal row may be quiet before the sweeper acts.
func (w *Worker) staleAfter() time.Duration {
	d := 3 * w.cfg.EnvPollMax
	if s := 2 * w.cfg.EnvSweepInterval; s > d {
		d = s
	}
	return d
}

func derefTime(t *time.Time) time.Time {
	if t == nil {
		return time.Time{}
	}
	return *t
}

// Compile-time proof that the real queue client satisfies the worker's enqueuer.
var _ EnvEnqueuer = (*queue.Client)(nil)
```
### 4.2 `workerstore.go` and `pgworkerstore.go`

`WorkerStore` is everything the worker needs from the database: each method is one CAS statement.

`internal/envs/workerstore.go`

```go
package envs

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned by WorkerStore.Get for an unknown environment id.
var ErrNotFound = errors.New("environment not found")

// EnvRow is the part of an environment the worker reads. It is plain Go (no sqlc or pgx types),
// so the handlers are unit-tested against an in-memory store.
type EnvRow struct {
	ID, Repo, SHA, Image, Template, ChartVersion, Size string
	OwnerLogin, Status, FailureReason                  string
	CorrelationID, Traceparent, TraceID                string
	RunConclusion, RunETag, RunHTMLURL                 string
	Values                                             []byte // JSON object
	RunID                                              int64
	CreatedAt, UpdatedAt, ExpiresAt                    time.Time
	DispatchAttemptedAt, DispatchedAt                  *time.Time
	RunConcludedAt, ProvisionDeadlineAt                *time.Time
}

// WorkerStore is every database operation the worker performs. Each mutation is a
// compare-and-set that reports whether THIS call changed the row.
type WorkerStore interface {
	Get(ctx context.Context, id string) (EnvRow, error)
	ListActive(ctx context.Context) ([]EnvRow, error)
	// ClaimDispatch sets dispatch_attempted_at once. Only the first caller gets true.
	ClaimDispatch(ctx context.Context, id string) (bool, error)
	// ReleaseDispatch undoes a claim after GitHub explicitly refused the request (rate limit),
	// so the next attempt may POST. Never call it after an ambiguous result.
	ReleaseDispatch(ctx context.Context, id string) error
	MarkProvisioning(ctx context.Context, id string, runID int64, htmlURL string, deadline time.Time) (bool, error)
	Touch(ctx context.Context, id, etag string) error
	MarkRunConcluded(ctx context.Context, id, conclusion string) (bool, error)
	MarkRunning(ctx context.Context, id string) (bool, error)
	MarkFailed(ctx context.Context, id, reason string) (bool, error)
}
```
`internal/envs/pgworkerstore.go`

```go
package envs

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/ephematool/ephematool/internal/store"
)

// pgWorkerStore is the production WorkerStore over the sqlc queries (runtime role: SELECT and
// UPDATE on environments, nothing else).
type pgWorkerStore struct{ q *store.Queries }

// NewPGWorkerStore returns the production WorkerStore.
func NewPGWorkerStore(q *store.Queries) WorkerStore { return pgWorkerStore{q: q} }

func ts(t pgtype.Timestamptz) *time.Time {
	if !t.Valid {
		return nil
	}
	v := t.Time
	return &v
}

func toRow(e store.Environment) EnvRow {
	return EnvRow{
		ID: e.ID, Repo: e.Repo, SHA: e.Sha, Image: e.Image, Template: e.Template,
		ChartVersion: e.ChartVersion, Size: e.Size, OwnerLogin: e.OwnerLogin, Status: e.Status,
		FailureReason: e.FailureReason, CorrelationID: e.CorrelationID, Traceparent: e.Traceparent,
		TraceID: e.TraceID, RunConclusion: e.RunConclusion, RunETag: e.RunEtag, RunHTMLURL: e.RunHtmlUrl,
		Values: e.Values, RunID: e.RunID.Int64,
		CreatedAt: e.CreatedAt.Time, UpdatedAt: e.UpdatedAt.Time, ExpiresAt: e.ExpiresAt.Time,
		DispatchAttemptedAt: ts(e.DispatchAttemptedAt), DispatchedAt: ts(e.DispatchedAt),
		RunConcludedAt: ts(e.RunConcludedAt), ProvisionDeadlineAt: ts(e.ProvisionDeadlineAt),
	}
}

func (s pgWorkerStore) Get(ctx context.Context, id string) (EnvRow, error) {
	e, err := s.q.GetEnvironment(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return EnvRow{}, ErrNotFound
	}
	if err != nil {
		return EnvRow{}, err
	}
	return toRow(e), nil
}

func (s pgWorkerStore) ListActive(ctx context.Context) ([]EnvRow, error) {
	rows, err := s.q.ListActiveEnvironments(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]EnvRow, 0, len(rows))
	for _, e := range rows {
		out = append(out, toRow(e))
	}
	return out, nil
}

func (s pgWorkerStore) ClaimDispatch(ctx context.Context, id string) (bool, error) {
	n, err := s.q.ClaimDispatch(ctx, id)
	return n == 1, err
}

func (s pgWorkerStore) ReleaseDispatch(ctx context.Context, id string) error {
	_, err := s.q.ReleaseDispatch(ctx, id)
	return err
}

func (s pgWorkerStore) MarkProvisioning(ctx context.Context, id string, runID int64, htmlURL string, deadline time.Time) (bool, error) {
	n, err := s.q.MarkProvisioning(ctx, store.MarkProvisioningParams{
		ID: id, RunID: pgtype.Int8{Int64: runID, Valid: true}, RunHtmlUrl: htmlURL,
		ProvisionDeadlineAt: pgtype.Timestamptz{Time: deadline, Valid: true},
	})
	return n == 1, err
}

func (s pgWorkerStore) Touch(ctx context.Context, id, etag string) error {
	return s.q.UpdateRunETag(ctx, store.UpdateRunETagParams{ID: id, RunEtag: etag})
}

func (s pgWorkerStore) MarkRunConcluded(ctx context.Context, id, conclusion string) (bool, error) {
	n, err := s.q.MarkRunConcluded(ctx, store.MarkRunConcludedParams{ID: id, RunConclusion: conclusion})
	return n == 1, err
}

func (s pgWorkerStore) MarkRunning(ctx context.Context, id string) (bool, error) {
	n, err := s.q.MarkRunning(ctx, id)
	return n == 1, err
}

func (s pgWorkerStore) MarkFailed(ctx context.Context, id, reason string) (bool, error) {
	n, err := s.q.MarkFailed(ctx, store.MarkFailedParams{ID: id, FailureReason: reason})
	return n == 1, err
}
```
### 4.3 `dispatch.go`

`internal/envs/dispatch.go`

```go
package envs

import (
	"context"
	"encoding/base64"
	"errors"
	"log/slog"

	"github.com/ephematool/ephematool/internal/ghapp"
	"github.com/ephematool/ephematool/internal/queue"
)

// DispatchInputFor maps an environment row to the workflow's 12 inputs. The values object is
// base64 of the exact JSON stored on the row, and expires_at is epoch seconds (label-safe).
func DispatchInputFor(env EnvRow) ghapp.DispatchInput {
	values := env.Values
	if len(values) == 0 {
		values = []byte("{}")
	}
	return ghapp.DispatchInput{
		EnvID: env.ID, CorrelationID: env.CorrelationID, Traceparent: env.Traceparent,
		Repo: env.Repo, SHA: env.SHA, Image: env.Image, Template: env.Template,
		ChartVersion: env.ChartVersion, Size: env.Size,
		ValuesB64: base64.StdEncoding.EncodeToString(values),
		Owner:     env.OwnerLogin, ExpiresAt: env.ExpiresAt.Unix(),
	}
}

// Dispatch handles env:dispatch. The workflow dispatch POST is not idempotent, so the rules are:
//
//   - ClaimDispatch is a compare-and-set: exactly one delivery ever POSTs.
//   - After an ambiguous result (HTTP 204 with no run id, or a transport error) the worker NEVER
//     POSTs again. It searches for the run by correlation id until dispatchConfirmWindow passes.
//   - The only time a claim is released is an explicit rate-limit refusal, where GitHub did not
//     perform the request.
func (w *Worker) Dispatch(ctx context.Context, envID string) error {
	env, ok, err := w.load(ctx, envID)
	if err != nil || !ok || env.Status != StatusRequested {
		return err
	}
	claimed, err := w.st.ClaimDispatch(ctx, env.ID)
	if err != nil {
		return err
	}
	if !claimed {
		return w.confirmDispatch(ctx, env)
	}
	if w.gh == nil {
		return w.fail(ctx, env, ReasonNoGitHub)
	}

	res, err := w.post(ctx, env)
	switch {
	case errors.Is(err, ghapp.ErrDispatchRejected):
		return w.fail(ctx, env, ReasonDispatchRejected)
	case err != nil:
		if d, limited := ghapp.RetryAfter(err); limited {
			// GitHub refused before doing anything, so it is safe to try again later.
			if rerr := w.st.ReleaseDispatch(ctx, env.ID); rerr != nil {
				return rerr
			}
			return w.enq(ctx, queue.TypeEnvDispatch, env.ID, d, 0)
		}
		slog.WarnContext(ctx, "env.dispatch.ambiguous", "env", env.ID, "err", err)
		return w.enq(ctx, queue.TypeEnvDispatch, env.ID, ambiguousRetryIn, 0)
	case res.Ambiguous:
		slog.WarnContext(ctx, "env.dispatch.ambiguous", "env", env.ID, "reason", "no run id in response")
		return w.enq(ctx, queue.TypeEnvDispatch, env.ID, ambiguousRetryIn, 0)
	}
	return w.provisioning(ctx, env, res.RunID, res.HTMLURL)
}

// post sends the dispatch, holding the lock so POSTs are serial and at least
// EnvDispatchSpacing apart (GitHub's secondary rate limits).
func (w *Worker) post(ctx context.Context, env EnvRow) (ghapp.DispatchResult, error) {
	w.dmu.Lock()
	defer w.dmu.Unlock()
	if !w.lastDispatch.IsZero() {
		if wait := w.cfg.EnvDispatchSpacing - w.now().Sub(w.lastDispatch); wait > 0 {
			if err := w.sleep(ctx, wait); err != nil {
				return ghapp.DispatchResult{}, err
			}
		}
	}
	res, err := w.gh.Dispatch(ctx, DispatchInputFor(env))
	w.lastDispatch = w.now()
	return res, err
}

// confirmDispatch runs when a dispatch was already attempted: find the run or give up.
func (w *Worker) confirmDispatch(ctx context.Context, env EnvRow) error {
	if w.gh == nil {
		return w.fail(ctx, env, ReasonNoGitHub)
	}
	attempted := derefTime(env.DispatchAttemptedAt)
	run, found, err := w.gh.FindRunByCorrelation(ctx, env.CorrelationID, attempted)
	if err != nil {
		if d, limited := ghapp.RetryAfter(err); limited {
			return w.enq(ctx, queue.TypeEnvDispatch, env.ID, d, 0)
		}
		return err // asynq retries with backoff
	}
	if found {
		return w.provisioning(ctx, env, run.ID, run.HTMLURL)
	}
	if w.now().Sub(attempted) > dispatchConfirmWindow {
		return w.fail(ctx, env, ReasonDispatchUnconfirmed)
	}
	w.touch(ctx, env)
	return w.enq(ctx, queue.TypeEnvDispatch, env.ID, confirmRetryIn, 0)
}

// provisioning records the run and starts polling. Losing the CAS means another delivery did it.
func (w *Worker) provisioning(ctx context.Context, env EnvRow, runID int64, htmlURL string) error {
	deadline := w.now().Add(w.cfg.EnvProvisionDeadline)
	won, err := w.st.MarkProvisioning(ctx, env.ID, runID, htmlURL, deadline)
	if err != nil || !won {
		return err
	}
	slog.InfoContext(ctx, "env.provisioning", "env", env.ID, "run_id", runID)
	return w.enq(ctx, queue.TypeEnvPoll, env.ID, w.cfg.EnvPollInitial, 0)
}
```
### 4.4 `poll.go`

`internal/envs/poll.go`

```go
package envs

import (
	"context"
	"log/slog"
	"time"

	"github.com/ephematool/ephematool/internal/ghapp"
	"github.com/ephematool/ephematool/internal/queue"
)

// Poll handles env:poll: follow the Actions run until it concludes. Polling is rescheduling, not
// retrying: every pass that has nothing new enqueues the next pass with backoff and jitter.
//
// Order matters. The conclusion is recorded first (a compare-and-set that exactly one delivery
// wins), then the synthetic spans are emitted by that winner only, then the next step starts.
func (w *Worker) Poll(ctx context.Context, envID string) error {
	env, ok, err := w.load(ctx, envID)
	if err != nil || !ok {
		return err
	}
	if env.Status != StatusProvisioning || env.RunConcludedAt != nil {
		return nil // stale task: already concluded or no longer provisioning
	}
	if env.ProvisionDeadlineAt != nil && w.now().After(*env.ProvisionDeadlineAt) {
		return w.fail(ctx, env, reasonTimeout(w.cfg.EnvProvisionDeadline))
	}
	if w.gh == nil {
		return w.fail(ctx, env, ReasonNoGitHub)
	}

	run, etag, notModified, err := w.gh.GetRun(ctx, env.RunID, env.RunETag)
	if err != nil {
		if d, limited := ghapp.RetryAfter(err); limited {
			return w.enq(ctx, queue.TypeEnvPoll, env.ID, d, 0)
		}
		return err // transient: asynq retries (MaxRetry 3); the sweeper recovers after that
	}
	// Remember the ETag and mark the row as freshly looked at (see Worker.touch).
	if err := w.st.Touch(ctx, env.ID, etag); err != nil {
		return err
	}
	if notModified || run.Status != "completed" {
		return w.enq(ctx, queue.TypeEnvPoll, env.ID, w.nextPoll(env), 0)
	}

	won, err := w.st.MarkRunConcluded(ctx, env.ID, run.Conclusion)
	if err != nil || !won {
		return err // lost the CAS: the winner emits the spans and continues
	}
	jobs, jerr := w.gh.ListJobs(ctx, run.ID)
	if jerr != nil {
		// Spans are best effort: a failed job listing must not change the environment's fate.
		slog.WarnContext(ctx, "env.spans.jobs_unavailable", "env", env.ID, "err", jerr)
	}
	emitRunSpans(env, run, jobs)

	switch run.Conclusion {
	case "success":
		return w.enq(ctx, queue.TypeEnvProbe, env.ID, 0, 0)
	case "cancelled":
		return w.fail(ctx, env, ReasonRunCancelled)
	default: // failure, timed_out, skipped, neutral, action_required, ...
		return w.fail(ctx, env, ReasonRunFailed)
	}
}

func (w *Worker) nextPoll(env EnvRow) (d time.Duration) {
	elapsed := time.Duration(0)
	if env.DispatchedAt != nil {
		elapsed = w.now().Sub(*env.DispatchedAt)
	}
	return NextPollDelay(pollAttempt(elapsed, w.cfg.EnvPollInitial), w.cfg.EnvPollInitial, w.cfg.EnvPollMax, w.rnd)
}
```
### 4.5 `probe.go`

The probe sends **no cookies** and does **not follow redirects**: a `302` to the dashboard means the gate is up but the app is not answering, which is not healthy.

`internal/envs/probe.go`

```go
package envs

import (
	"context"
	"io"
	"log/slog"
	"net/http"

	"github.com/ephematool/ephematool/internal/previewauth"
	"github.com/ephematool/ephematool/internal/queue"
)

// Probe handles env:probe. A green workflow only means Helm finished; the environment is
// "running" when the preview itself answers (D-18). The probe goes through the SAME gateway users
// use, to the one path the gate exempts from authentication, so it never carries a credential.
func (w *Worker) Probe(ctx context.Context, envID string) error {
	env, ok, err := w.load(ctx, envID)
	if err != nil || !ok {
		return err
	}
	if env.Status != StatusProvisioning || env.RunConclusion != "success" || env.RunConcludedAt == nil {
		return nil
	}
	if w.now().After(env.RunConcludedAt.Add(w.cfg.EnvProbeWindow)) {
		return w.fail(ctx, env, ReasonNotHealthy)
	}

	ctx, endSpan := startProbeSpan(ctx, env)
	healthy, status := w.probeOnce(ctx, env.ID)
	endSpan(healthy, status)

	if healthy {
		won, err := w.st.MarkRunning(ctx, env.ID)
		if err != nil {
			return err
		}
		if won {
			logRunning(ctx, env)
		}
		return nil
	}
	w.touch(ctx, env)
	return w.enq(ctx, queue.TypeEnvProbe, env.ID, w.cfg.EnvProbeInitial, 0)
}

// probeOnce does one GET of the exempt health path. The URL points at the gateway's address; the
// Host header names the environment, so no DNS is involved and the fake and kind take the same
// code path. Redirects are NOT followed: a 302 means the gate is asking for a login, which is
// not healthy. No cookies or tokens are sent.
func (w *Worker) probeOnce(ctx context.Context, envID string) (healthy bool, status int) {
	ctx, cancel := context.WithTimeout(ctx, w.cfg.EnvProbeTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://"+w.cfg.PreviewGatewayAddr+previewauth.HealthPath, nil)
	if err != nil {
		return false, 0
	}
	req.Host = ProbeHost(w.cfg, envID)
	resp, err := w.hc.Do(req)
	if err != nil {
		return false, 0
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	return resp.StatusCode >= 200 && resp.StatusCode < 300, resp.StatusCode
}

func logRunning(ctx context.Context, env EnvRow) {
	slog.InfoContext(ctx, "env.running", "env", env.ID)
}
```
### 4.6 `sweep.go`

`internal/envs/sweep.go`

```go
package envs

import (
	"context"
	"log/slog"
	"time"

	"github.com/ephematool/ephematool/internal/queue"
)

// Sweep handles env:sweep, scheduled every EnvSweepInterval. It makes the whole pipeline
// resumable after a worker restart, a Redis flush or a lost task: every non-terminal row that has
// been quiet for staleAfter gets exactly the task its state calls for, under its STORED
// traceparent so the resumed work stays in the original trace.
//
// "Quiet" matters. Poll and probe continuations re-enqueue themselves, so a sweeper that enqueued
// a poll for every provisioning row every 30 seconds would start a new self-perpetuating chain
// each time. Handlers touch the row on every pass; the sweeper leaves fresh rows alone.
func (w *Worker) Sweep(ctx context.Context) error {
	rows, err := w.st.ListActive(ctx)
	if err != nil {
		return err
	}
	unique := w.cfg.EnvSweepInterval - time.Second
	if unique < time.Second {
		unique = time.Second
	}
	stale := w.staleAfter()
	for _, env := range rows {
		if w.now().Sub(env.UpdatedAt) < stale {
			continue
		}
		var typ string
		switch {
		case env.Status == StatusRequested:
			typ = queue.TypeEnvDispatch
		case env.Status == StatusProvisioning && env.RunConcludedAt == nil:
			typ = queue.TypeEnvPoll
		case env.Status == StatusProvisioning && env.RunConclusion == "success":
			typ = queue.TypeEnvProbe
		case env.Status == StatusProvisioning:
			// Concluded but not successful, and the worker died before marking it failed.
			reason := ReasonRunFailed
			if env.RunConclusion == "cancelled" {
				reason = ReasonRunCancelled
			}
			if err := w.fail(ctx, env, reason); err != nil {
				slog.WarnContext(ctx, "env.sweep.fail_error", "env", env.ID, "err", err)
			}
			continue
		default:
			continue
		}
		tctx := traceContext(ctx, env.Traceparent)
		if err := w.enq(tctx, typ, env.ID, 0, unique); err != nil {
			// A duplicate-task error from the unique window is expected and harmless.
			slog.DebugContext(ctx, "env.sweep.enqueue", "env", env.ID, "type", typ, "err", err)
			continue
		}
		slog.InfoContext(ctx, "env.sweep.requeued", "env", env.ID, "type", typ)
	}
	return nil
}
```
### 4.7 `spans.go`: the only file that imports OpenTelemetry

`internal/envs/spans.go`

```go
package envs

import (
	"context"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/ephematool/ephematool/internal/conventions"
	"github.com/ephematool/ephematool/internal/ghapp"
)

// All OpenTelemetry use of the worker lives in this file.
//
// What the trace shows (OBS-02, D-26): the API request, the worker tasks, the probe, and run, job
// and step spans rebuilt from GitHub's API timestamps AFTER the run completes. What it cannot
// show: log lines or internals of steps, a live view while the run is going (the spans appear
// when the run ends), or anything finer than GitHub's one-second timestamps. GitHub Actions emits
// no spans of its own; these are synthetic and the README says so.

func tracer() trace.Tracer { return otel.Tracer("ephematool/envs") }

// traceContext rebuilds the original create-request trace context from the traceparent stored on
// the row, so resumed work (the sweeper, the synthetic spans) joins the same trace.
func traceContext(ctx context.Context, traceparent string) context.Context {
	if traceparent == "" {
		return ctx
	}
	return otel.GetTextMapPropagator().Extract(ctx, propagation.MapCarrier{"traceparent": traceparent})
}

func envAttrs(env EnvRow) []attribute.KeyValue {
	return []attribute.KeyValue{
		attribute.String(conventions.AttrEnvID, env.ID),
		attribute.String(conventions.AttrOwner, env.OwnerLogin),
		attribute.String(conventions.AttrTemplate, env.Template),
	}
}

// emitRunSpans writes github.actions.run > github.actions.job > github.actions.step with the
// timestamps GitHub reported. Spans carry run ids, conclusions and timings only: never workflow
// inputs or tokens (T-02-11-05).
func emitRunSpans(env EnvRow, run ghapp.Run, jobs []ghapp.Job) {
	ctx := traceContext(context.Background(), env.Traceparent)
	end := run.UpdatedAt
	if end.Before(run.CreatedAt) {
		end = run.CreatedAt
	}
	attrs := append(envAttrs(env),
		attribute.Int64("github.run.id", run.ID),
		attribute.String("github.run.conclusion", run.Conclusion),
		attribute.String("github.run.html_url", run.HTMLURL),
	)
	if !run.RunStartedAt.IsZero() && !run.CreatedAt.IsZero() {
		attrs = append(attrs, attribute.Int64("github.run.queue_ms", run.RunStartedAt.Sub(run.CreatedAt).Milliseconds()))
	}
	rctx, rs := tracer().Start(ctx, "github.actions.run",
		trace.WithTimestamp(run.CreatedAt), trace.WithAttributes(attrs...))
	setConclusion(rs, run.Conclusion)

	for _, j := range jobs {
		if j.StartedAt.IsZero() {
			continue
		}
		jctx, js := tracer().Start(rctx, "github.actions.job",
			trace.WithTimestamp(j.StartedAt),
			trace.WithAttributes(append(envAttrs(env), attribute.String("github.job.name", j.Name),
				attribute.String("github.job.conclusion", j.Conclusion))...))
		setConclusion(js, j.Conclusion)
		for _, s := range j.Steps {
			if s.StartedAt.IsZero() {
				continue
			}
			_, ss := tracer().Start(jctx, "github.actions.step",
				trace.WithTimestamp(s.StartedAt),
				trace.WithAttributes(attribute.String("github.step.name", s.Name),
					attribute.Int64("github.step.number", s.Number),
					attribute.String("github.step.conclusion", s.Conclusion)))
			setConclusion(ss, s.Conclusion)
			ss.End(trace.WithTimestamp(orElse(s.CompletedAt, s.StartedAt)))
		}
		js.End(trace.WithTimestamp(orElse(j.CompletedAt, j.StartedAt)))
	}
	rs.End(trace.WithTimestamp(end))
}

func orElse(t, fallback time.Time) time.Time {
	if t.IsZero() || t.Before(fallback) {
		return fallback
	}
	return t
}

func setConclusion(s trace.Span, conclusion string) {
	if conclusion != "" && conclusion != "success" && conclusion != "skipped" {
		s.SetStatus(codes.Error, conclusion)
	}
}

// startProbeSpan opens env.probe. The returned func ends it with the result.
func startProbeSpan(ctx context.Context, env EnvRow) (context.Context, func(healthy bool, status int)) {
	ctx, span := tracer().Start(ctx, "env.probe", trace.WithAttributes(envAttrs(env)...))
	return ctx, func(healthy bool, status int) {
		result := "unhealthy"
		if healthy {
			result = "healthy"
		}
		span.SetAttributes(attribute.String("env.probe.result", result), attribute.Int("http.response.status_code", status))
		if !healthy {
			span.SetStatus(codes.Error, "preview not healthy yet")
		}
		span.End()
	}
}

// newProbeClient is the traced HTTP client for the probe: redirects are returned, not followed.
func newProbeClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout:       timeout,
		Transport:     otelhttp.NewTransport(http.DefaultTransport),
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
}
```
### 4.8 `register.go`: asynq wiring

A payload that cannot be decoded is wrapped with `asynq.SkipRetry`: retrying garbage helps nobody.

`internal/envs/register.go`

```go
package envs

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hibiken/asynq"

	"github.com/ephematool/ephematool/internal/queue"
)

// RegisterWorker attaches poll, probe and sweep to the default server's mux.
func RegisterWorker(mux *asynq.ServeMux, w *Worker) {
	mux.HandleFunc(queue.TypeEnvPoll, envTask(w.Poll))
	mux.HandleFunc(queue.TypeEnvProbe, envTask(w.Probe))
	mux.HandleFunc(queue.TypeEnvSweep, func(ctx context.Context, _ *asynq.Task) error { return w.Sweep(ctx) })
}

// RegisterDispatch attaches env:dispatch to the dispatch server's mux (concurrency 1).
func RegisterDispatch(mux *asynq.ServeMux, w *Worker) {
	mux.HandleFunc(queue.TypeEnvDispatch, envTask(w.Dispatch))
}

// envTask decodes the bare {env_id} payload. A payload that does not decode can never succeed,
// so it is not retried.
func envTask(fn func(context.Context, string) error) func(context.Context, *asynq.Task) error {
	return func(ctx context.Context, t *asynq.Task) error {
		var p queue.EnvPayload
		if err := json.Unmarshal(t.Payload(), &p); err != nil || p.EnvID == "" {
			return fmt.Errorf("%w: bad %s payload", asynq.SkipRetry, t.Type())
		}
		return fn(ctx, p.EnvID)
	}
}
```
---

## 5. Unit tests (no Docker)

`worker_test.go` has 25 cases against in-memory fakes: the input mapping of the 12 workflow inputs, dispatch happening once, an ambiguous result (a `204` without a run id, or a transport error) resolved by correlation search and failing after the window, a rejected dispatch, no GitHub App, serial spacing, a stale dispatch task being a no-op, polling with ETag backoff, success, failure and cancellation, the deadline, a transient error being retried rather than failed, redelivery emitting spans once, the probe's status mapping, request shape and window, and the sweeper. `spans_test.go` uses OpenTelemetry's in-memory exporter to check the span timestamps and parentage under the stored trace, that a failed run's span is an error, that no span carries inputs or tokens, and the probe span's result.

`internal/envs/worker_test.go`

```go
package envs

import (
	"context"
	"encoding/base64"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ephematool/ephematool/internal/config"
	"github.com/ephematool/ephematool/internal/ghapp"
	"github.com/ephematool/ephematool/internal/queue"
)

// ---- fakes -----------------------------------------------------------------------------------

// memStore implements WorkerStore with the same compare-and-set rules as the SQL.
type memStore struct {
	mu   sync.Mutex
	rows map[string]*EnvRow
	now  func() time.Time
}

func newMemStore(now func() time.Time) *memStore {
	return &memStore{rows: map[string]*EnvRow{}, now: now}
}

func (m *memStore) put(e EnvRow) { m.mu.Lock(); defer m.mu.Unlock(); c := e; m.rows[e.ID] = &c }

func (m *memStore) Get(_ context.Context, id string) (EnvRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.rows[id]; ok {
		return *r, nil
	}
	return EnvRow{}, ErrNotFound
}

func (m *memStore) ListActive(context.Context) ([]EnvRow, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []EnvRow
	for _, r := range m.rows {
		if r.Status == StatusRequested || r.Status == StatusProvisioning {
			out = append(out, *r)
		}
	}
	return out, nil
}

func (m *memStore) ClaimDispatch(_ context.Context, id string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.rows[id]
	if r == nil || r.Status != StatusRequested || r.DispatchAttemptedAt != nil {
		return false, nil
	}
	t := m.now()
	r.DispatchAttemptedAt, r.UpdatedAt = &t, t
	return true, nil
}

func (m *memStore) ReleaseDispatch(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r := m.rows[id]; r != nil && r.Status == StatusRequested {
		r.DispatchAttemptedAt = nil
	}
	return nil
}

func (m *memStore) MarkProvisioning(_ context.Context, id string, runID int64, url string, deadline time.Time) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.rows[id]
	if r == nil || r.Status != StatusRequested {
		return false, nil
	}
	t := m.now()
	r.Status, r.RunID, r.RunHTMLURL, r.ProvisionDeadlineAt, r.DispatchedAt, r.UpdatedAt = StatusProvisioning, runID, url, &deadline, &t, t
	return true, nil
}

func (m *memStore) Touch(_ context.Context, id, etag string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r := m.rows[id]; r != nil {
		r.RunETag, r.UpdatedAt = etag, m.now()
	}
	return nil
}

func (m *memStore) MarkRunConcluded(_ context.Context, id, c string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.rows[id]
	if r == nil || r.RunConcludedAt != nil {
		return false, nil
	}
	t := m.now()
	r.RunConclusion, r.RunConcludedAt, r.UpdatedAt = c, &t, t
	return true, nil
}

func (m *memStore) MarkRunning(_ context.Context, id string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.rows[id]
	if r == nil || r.Status != StatusProvisioning {
		return false, nil
	}
	r.Status, r.UpdatedAt = StatusRunning, m.now()
	return true, nil
}

func (m *memStore) MarkFailed(_ context.Context, id, reason string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r := m.rows[id]
	if r == nil || (r.Status != StatusRequested && r.Status != StatusProvisioning) {
		return false, nil
	}
	r.Status, r.FailureReason, r.UpdatedAt = StatusFailed, reason, m.now()
	return true, nil
}

type enqCall struct {
	typ, id           string
	processIn, unique time.Duration
}

type recEnqueuer struct {
	mu    sync.Mutex
	calls []enqCall
	err   error
}

func (r *recEnqueuer) EnqueueEnv(_ context.Context, typ, id string, in, uniq time.Duration) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, enqCall{typ, id, in, uniq})
	return r.err
}

func (r *recEnqueuer) last() enqCall {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.calls) == 0 {
		return enqCall{}
	}
	return r.calls[len(r.calls)-1]
}

func (r *recEnqueuer) count() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.calls) }

type fakeGH struct {
	dispatchRes   ghapp.DispatchResult
	dispatchErr   error
	dispatches    []ghapp.DispatchInput
	found         bool
	foundRun      ghapp.Run
	findErr       error
	findCalls     int
	getRuns       []ghapp.Run // popped one per GetRun; the last one repeats
	getNotMod     bool
	getErr        error
	jobs          []ghapp.Job
	listJobsCalls int
}

func (f *fakeGH) Dispatch(_ context.Context, in ghapp.DispatchInput) (ghapp.DispatchResult, error) {
	f.dispatches = append(f.dispatches, in)
	return f.dispatchRes, f.dispatchErr
}

func (f *fakeGH) FindRunByCorrelation(context.Context, string, time.Time) (ghapp.Run, bool, error) {
	f.findCalls++
	return f.foundRun, f.found, f.findErr
}

func (f *fakeGH) GetRun(context.Context, int64, string) (ghapp.Run, string, bool, error) {
	if f.getErr != nil {
		return ghapp.Run{}, "", false, f.getErr
	}
	if f.getNotMod {
		return ghapp.Run{}, "etag-1", true, nil
	}
	r := f.getRuns[0]
	if len(f.getRuns) > 1 {
		f.getRuns = f.getRuns[1:]
	}
	return r, "etag-2", false, nil
}

func (f *fakeGH) ListJobs(context.Context, int64) ([]ghapp.Job, error) {
	f.listJobsCalls++
	return f.jobs, nil
}

// ---- harness ---------------------------------------------------------------------------------

var t0 = time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)

type harness struct {
	t     *testing.T
	clock time.Time
	st    *memStore
	gh    *fakeGH
	enq   *recEnqueuer
	w     *Worker
	cfg   config.Config
	slept []time.Duration
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{t: t, clock: t0, gh: &fakeGH{}, enq: &recEnqueuer{}}
	h.st = newMemStore(func() time.Time { return h.clock })
	h.cfg = config.Config{
		EnvProvisionDeadline: 15 * time.Minute, EnvPollInitial: 3 * time.Second, EnvPollMax: 15 * time.Second,
		EnvProbeInitial: 5 * time.Second, EnvProbeWindow: 60 * time.Second, EnvProbeTimeout: 2 * time.Second,
		EnvSweepInterval: 30 * time.Second, EnvDispatchSpacing: time.Second,
		PreviewDomain: "preview.localhost", PreviewPort: 8088, PreviewScheme: "http",
	}
	h.build()
	return h
}

func (h *harness) build() {
	h.w = NewWorker(WorkerDeps{
		Cfg: h.cfg, Store: h.st, GH: h.gh, Enqueue: h.enq,
		HTTP: &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		Now:  func() time.Time { return h.clock },
		Sleep: func(_ context.Context, d time.Duration) error {
			h.slept = append(h.slept, d)
			h.clock = h.clock.Add(d)
			return nil
		},
		Rand: func() float64 { return 0.5 },
	})
}

func (h *harness) seed(id string, mut func(*EnvRow)) {
	e := EnvRow{
		ID: id, Repo: "ephematool-e2e/demo-app", SHA: strings.Repeat("a", 40), Image: "ghcr.io/ephematool-e2e/demo-app@sha256:" + strings.Repeat("b", 64),
		Template: "web-app", ChartVersion: "0.1.0", Size: "S", OwnerLogin: "octo-admin", Status: StatusRequested,
		CorrelationID: "cr-0123456789abcdef", Traceparent: "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01",
		Values: []byte(`{"replicas":2}`), ExpiresAt: time.Unix(1792000000, 0).UTC(), CreatedAt: h.clock, UpdatedAt: h.clock,
	}
	if mut != nil {
		mut(&e)
	}
	h.st.put(e)
}

func (h *harness) row(id string) EnvRow {
	r, err := h.st.Get(context.Background(), id)
	if err != nil {
		h.t.Fatal(err)
	}
	return r
}

const id1 = "pv-aaaaaaaaaa"

// ---- pure functions --------------------------------------------------------------------------

func TestNextPollDelay(t *testing.T) {
	mid := func() float64 { return 0.5 }
	if got := NextPollDelay(0, 3*time.Second, 15*time.Second, mid); got != 3*time.Second {
		t.Errorf("attempt 0 = %v, want 3s", got)
	}
	if got := NextPollDelay(1, 3*time.Second, 15*time.Second, mid); got != 4500*time.Millisecond {
		t.Errorf("attempt 1 = %v, want 4.5s", got)
	}
	if got := NextPollDelay(30, 3*time.Second, 15*time.Second, mid); got != 15*time.Second {
		t.Errorf("capped = %v, want 15s", got)
	}
	lo := NextPollDelay(30, 3*time.Second, 15*time.Second, func() float64 { return 0 })
	hi := NextPollDelay(30, 3*time.Second, 15*time.Second, func() float64 { return 0.999999 })
	if lo != 12*time.Second || hi > 18*time.Second || hi < 17*time.Second {
		t.Errorf("jitter bounds lo=%v hi=%v, want 12s and <= 18s (15s +/- 20%%)", lo, hi)
	}
}

func TestPollAttemptInvertsTheBackoff(t *testing.T) {
	// Cumulative un-jittered delays with initial 3s: 3, 7.5, 14.25, ...
	for _, c := range []struct {
		elapsed time.Duration
		want    int
	}{{0, 0}, {3 * time.Second, 1}, {7500 * time.Millisecond, 2}, {14250 * time.Millisecond, 3}} {
		if got := pollAttempt(c.elapsed, 3*time.Second); got != c.want {
			t.Errorf("pollAttempt(%v) = %d, want %d", c.elapsed, got, c.want)
		}
	}
}

func TestReasonTimeoutUsesTheConfiguredDuration(t *testing.T) {
	for d, want := range map[time.Duration]string{
		15 * time.Minute: "Timed out waiting for the Actions run (15m)",
		2 * time.Minute:  "Timed out waiting for the Actions run (2m)",
		time.Hour:        "Timed out waiting for the Actions run (1h)",
		90 * time.Second: "Timed out waiting for the Actions run (1m30s)",
	} {
		if got := reasonTimeout(d); got != want {
			t.Errorf("%v: %q, want %q", d, got, want)
		}
	}
}

func TestDispatchInputMapping(t *testing.T) {
	in := DispatchInputFor(EnvRow{ID: id1, Values: []byte(`{"replicas":2}`), ExpiresAt: time.Unix(1792000000, 0)})
	raw, err := base64.StdEncoding.DecodeString(in.ValuesB64)
	if err != nil || string(raw) != `{"replicas":2}` {
		t.Fatalf("values_b64 decodes to %q (%v)", raw, err)
	}
	if in.ExpiresAt != 1792000000 {
		t.Errorf("expires_at = %d, want epoch seconds", in.ExpiresAt)
	}
	if empty := DispatchInputFor(EnvRow{}); empty.ValuesB64 != base64.StdEncoding.EncodeToString([]byte("{}")) {
		t.Errorf("empty values must encode {}, got %q", empty.ValuesB64)
	}
}

// ---- dispatch --------------------------------------------------------------------------------

func TestDispatchSuccessMovesToProvisioningAndStartsPolling(t *testing.T) {
	h := newHarness(t)
	h.seed(id1, nil)
	h.gh.dispatchRes = ghapp.DispatchResult{RunID: 9000001, HTMLURL: "http://x/runs/9000001"}
	if err := h.w.Dispatch(t.Context(), id1); err != nil {
		t.Fatal(err)
	}
	r := h.row(id1)
	if r.Status != StatusProvisioning || r.RunID != 9000001 || r.ProvisionDeadlineAt == nil {
		t.Fatalf("row = %+v", r)
	}
	if got := r.ProvisionDeadlineAt.Sub(h.clock); got != 15*time.Minute {
		t.Errorf("deadline = now+%v, want 15m", got)
	}
	if c := h.enq.last(); c.typ != queue.TypeEnvPoll || c.processIn != 3*time.Second || c.unique != 0 {
		t.Errorf("next task = %+v, want a delayed poll with no unique window", c)
	}
	if len(h.gh.dispatches) != 1 {
		t.Errorf("dispatch POSTs = %d, want 1", len(h.gh.dispatches))
	}
}

func TestDispatchIsOnlyEverPostedOnce(t *testing.T) {
	h := newHarness(t)
	h.seed(id1, nil)
	h.gh.dispatchRes = ghapp.DispatchResult{Ambiguous: true} // HTTP 204, no run id
	for i := 0; i < 3; i++ {
		if err := h.w.Dispatch(t.Context(), id1); err != nil {
			t.Fatal(err)
		}
	}
	if len(h.gh.dispatches) != 1 {
		t.Fatalf("POSTs = %d, want exactly 1 even across redeliveries", len(h.gh.dispatches))
	}
	if h.gh.findCalls != 2 {
		t.Errorf("correlation searches = %d, want 2 (the second and third delivery)", h.gh.findCalls)
	}
}

func TestAmbiguousThenFoundByCorrelation(t *testing.T) {
	h := newHarness(t)
	h.seed(id1, nil)
	h.gh.dispatchRes = ghapp.DispatchResult{Ambiguous: true}
	_ = h.w.Dispatch(t.Context(), id1)
	if c := h.enq.last(); c.typ != queue.TypeEnvDispatch || c.processIn != 3*time.Second {
		t.Fatalf("after an ambiguous POST the next task = %+v, want dispatch in 3s", c)
	}
	h.gh.found, h.gh.foundRun = true, ghapp.Run{ID: 9000002, HTMLURL: "http://x/runs/9000002"}
	_ = h.w.Dispatch(t.Context(), id1)
	if r := h.row(id1); r.Status != StatusProvisioning || r.RunID != 9000002 {
		t.Fatalf("row = %+v, want provisioning with the found run", r)
	}
	if len(h.gh.dispatches) != 1 {
		t.Errorf("POSTs = %d, want 1", len(h.gh.dispatches))
	}
}

func TestTransportErrorIsTreatedAsAmbiguous(t *testing.T) {
	h := newHarness(t)
	h.seed(id1, nil)
	h.gh.dispatchErr = errors.New("connection reset by peer")
	if err := h.w.Dispatch(t.Context(), id1); err != nil {
		t.Fatalf("an ambiguous POST must not error (a retry would re-POST): %v", err)
	}
	if h.row(id1).Status != StatusRequested || h.enq.last().typ != queue.TypeEnvDispatch {
		t.Fatalf("want still requested with a dispatch follow-up, got %+v / %+v", h.row(id1), h.enq.last())
	}
}

func TestUnconfirmedDispatchFailsAfterTheWindow(t *testing.T) {
	h := newHarness(t)
	h.seed(id1, nil)
	h.gh.dispatchRes = ghapp.DispatchResult{Ambiguous: true}
	_ = h.w.Dispatch(t.Context(), id1)
	h.clock = h.clock.Add(dispatchConfirmWindow + time.Second)
	_ = h.w.Dispatch(t.Context(), id1)
	if r := h.row(id1); r.Status != StatusFailed || r.FailureReason != ReasonDispatchUnconfirmed {
		t.Fatalf("row = %+v", r)
	}
}

func TestRejectedDispatchFails(t *testing.T) {
	h := newHarness(t)
	h.seed(id1, nil)
	h.gh.dispatchErr = ghapp.ErrDispatchRejected
	_ = h.w.Dispatch(t.Context(), id1)
	if r := h.row(id1); r.Status != StatusFailed || r.FailureReason != ReasonDispatchRejected {
		t.Fatalf("row = %+v", r)
	}
}

func TestNoGitHubAppFailsWithAReason(t *testing.T) {
	h := newHarness(t)
	h.gh = nil
	h.w = NewWorker(WorkerDeps{Cfg: h.cfg, Store: h.st, GH: nil, Enqueue: h.enq, Now: func() time.Time { return h.clock }})
	h.seed(id1, nil)
	_ = h.w.Dispatch(t.Context(), id1)
	if r := h.row(id1); r.Status != StatusFailed || r.FailureReason != ReasonNoGitHub {
		t.Fatalf("row = %+v", r)
	}
}

func TestDispatchesAreSpacedOutAndSerial(t *testing.T) {
	h := newHarness(t)
	h.seed(id1, nil)
	h.seed("pv-bbbbbbbbbb", nil)
	h.gh.dispatchRes = ghapp.DispatchResult{RunID: 1}
	_ = h.w.Dispatch(t.Context(), id1)
	_ = h.w.Dispatch(t.Context(), "pv-bbbbbbbbbb") // immediately after: must wait for the spacing
	if len(h.slept) != 1 || h.slept[0] != time.Second {
		t.Fatalf("sleeps = %v, want one 1s wait before the second POST", h.slept)
	}
}

func TestStaleDispatchTaskIsANoop(t *testing.T) {
	h := newHarness(t)
	h.seed(id1, func(e *EnvRow) { e.Status = StatusRunning })
	if err := h.w.Dispatch(t.Context(), id1); err != nil || len(h.gh.dispatches) != 0 {
		t.Fatalf("err=%v posts=%d", err, len(h.gh.dispatches))
	}
	if err := h.w.Dispatch(t.Context(), "pv-zzzzzzzzzz"); err != nil {
		t.Fatalf("unknown env must be a quiet no-op: %v", err)
	}
}

// ---- poll ------------------------------------------------------------------------------------

func provisioningRow(e *EnvRow) {
	d := t0
	dl := t0.Add(15 * time.Minute)
	e.Status, e.RunID, e.DispatchedAt, e.ProvisionDeadlineAt = StatusProvisioning, 9000001, &d, &dl
}

func TestPollNotModifiedReschedulesWithBackoff(t *testing.T) {
	h := newHarness(t)
	h.seed(id1, provisioningRow)
	h.gh.getNotMod = true
	if err := h.w.Poll(t.Context(), id1); err != nil {
		t.Fatal(err)
	}
	c := h.enq.last()
	if c.typ != queue.TypeEnvPoll || c.unique != 0 || c.processIn != 3*time.Second {
		t.Fatalf("next = %+v, want poll in 3s, no unique", c)
	}
	h.clock = h.clock.Add(7500 * time.Millisecond)
	_ = h.w.Poll(t.Context(), id1)
	if c := h.enq.last(); c.processIn != 6750*time.Millisecond {
		t.Errorf("third poll delay = %v, want 6.75s (attempt 2)", c.processIn)
	}
}

func TestPollSuccessConcludesAndStartsProbe(t *testing.T) {
	h := newHarness(t)
	h.seed(id1, provisioningRow)
	h.gh.getRuns = []ghapp.Run{{ID: 9000001, Status: "completed", Conclusion: "success", CreatedAt: t0, RunStartedAt: t0.Add(5 * time.Second), UpdatedAt: t0.Add(time.Minute)}}
	if err := h.w.Poll(t.Context(), id1); err != nil {
		t.Fatal(err)
	}
	r := h.row(id1)
	if r.RunConclusion != "success" || r.RunConcludedAt == nil || r.Status != StatusProvisioning {
		t.Fatalf("row = %+v: success concludes the run but the env is NOT running yet", r)
	}
	if c := h.enq.last(); c.typ != queue.TypeEnvProbe {
		t.Errorf("next = %+v, want probe", c)
	}
	if h.gh.listJobsCalls != 1 {
		t.Errorf("ListJobs calls = %d, want 1", h.gh.listJobsCalls)
	}
}

func TestPollFailureAndCancellationReasons(t *testing.T) {
	for _, c := range []struct{ conclusion, reason string }{
		{"failure", ReasonRunFailed}, {"cancelled", ReasonRunCancelled}, {"timed_out", ReasonRunFailed}, {"skipped", ReasonRunFailed},
	} {
		h := newHarness(t)
		h.seed(id1, provisioningRow)
		h.gh.getRuns = []ghapp.Run{{ID: 9000001, Status: "completed", Conclusion: c.conclusion, CreatedAt: t0, UpdatedAt: t0}}
		_ = h.w.Poll(t.Context(), id1)
		if r := h.row(id1); r.Status != StatusFailed || r.FailureReason != c.reason {
			t.Errorf("%s: row = %+v, want failed %q", c.conclusion, r, c.reason)
		}
	}
}

func TestPollPastTheDeadlineTimesOut(t *testing.T) {
	h := newHarness(t)
	h.seed(id1, provisioningRow)
	h.clock = t0.Add(16 * time.Minute)
	_ = h.w.Poll(t.Context(), id1)
	if r := h.row(id1); r.Status != StatusFailed || r.FailureReason != "Timed out waiting for the Actions run (15m)" {
		t.Fatalf("row = %+v", r)
	}
}

func TestPollTransientErrorIsRetriedNotFailed(t *testing.T) {
	h := newHarness(t)
	h.seed(id1, provisioningRow)
	h.gh.getErr = errors.New("boom") // not a rate limit: bubbles up so asynq retries
	if err := h.w.Poll(t.Context(), id1); err == nil {
		t.Fatal("a transient error must be returned for asynq to retry")
	}
	if h.row(id1).Status != StatusProvisioning {
		t.Fatal("a transient error must not fail the environment")
	}
}

func TestPollRedeliveryEmitsSpansOnce(t *testing.T) {
	h := newHarness(t)
	h.seed(id1, provisioningRow)
	h.gh.getRuns = []ghapp.Run{{ID: 9000001, Status: "completed", Conclusion: "success", CreatedAt: t0, UpdatedAt: t0.Add(time.Minute)}}
	_ = h.w.Poll(t.Context(), id1)
	_ = h.w.Poll(t.Context(), id1) // the same completion delivered twice
	if h.gh.listJobsCalls != 1 {
		t.Fatalf("ListJobs called %d times: the CAS winner alone emits spans", h.gh.listJobsCalls)
	}
	probes := 0
	for _, c := range h.enq.calls {
		if c.typ == queue.TypeEnvProbe {
			probes++
		}
	}
	if probes != 1 {
		t.Errorf("probe tasks = %d, want 1", probes)
	}
}

// ---- probe -----------------------------------------------------------------------------------

func concludedRow(e *EnvRow) {
	provisioningRow(e)
	c := t0
	e.RunConclusion, e.RunConcludedAt = "success", &c
}

type gateway struct {
	*httptest.Server
	mu   sync.Mutex
	last *http.Request
	code int
}

func newGateway(code int) *gateway {
	g := &gateway{code: code}
	g.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g.mu.Lock()
		g.last = r.Clone(r.Context())
		c := g.code
		g.mu.Unlock()
		if c == http.StatusFound {
			w.Header().Set("Location", "http://localhost:8080/preview/authorize")
		}
		w.WriteHeader(c)
	}))
	return g
}

func TestProbeStatusMapping(t *testing.T) {
	for _, c := range []struct {
		code    int
		running bool
	}{{200, true}, {204, true}, {503, false}, {302, false}, {404, false}} {
		gw := newGateway(c.code)
		h := newHarness(t)
		h.cfg.PreviewGatewayAddr = strings.TrimPrefix(gw.URL, "http://")
		h.build()
		h.seed(id1, concludedRow)
		if err := h.w.Probe(t.Context(), id1); err != nil {
			t.Fatal(err)
		}
		got := h.row(id1).Status == StatusRunning
		if got != c.running {
			t.Errorf("gateway %d: running=%v, want %v", c.code, got, c.running)
		}
		if !c.running {
			if n := h.enq.last(); n.typ != queue.TypeEnvProbe || n.processIn != 5*time.Second {
				t.Errorf("gateway %d: next = %+v, want probe in 5s", c.code, n)
			}
		}
		gw.Close()
	}
}

func TestProbeRequestShape(t *testing.T) {
	gw := newGateway(200)
	defer gw.Close()
	h := newHarness(t)
	h.cfg.PreviewGatewayAddr = strings.TrimPrefix(gw.URL, "http://")
	h.build()
	h.seed(id1, concludedRow)
	_ = h.w.Probe(t.Context(), id1)
	r := gw.last
	if r.Host != "pv-aaaaaaaaaa.preview.localhost:8088" {
		t.Errorf("Host = %q", r.Host)
	}
	if r.URL.Path != "/__ephematool/healthz" || r.Method != http.MethodGet {
		t.Errorf("request = %s %s", r.Method, r.URL.Path)
	}
	if r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
		t.Errorf("the probe must carry no credentials: %v", r.Header)
	}
}

func TestProbeWindowExhaustedFails(t *testing.T) {
	gw := newGateway(503)
	defer gw.Close()
	h := newHarness(t)
	h.cfg.PreviewGatewayAddr = strings.TrimPrefix(gw.URL, "http://")
	h.build()
	h.seed(id1, concludedRow)
	h.clock = t0.Add(61 * time.Second)
	_ = h.w.Probe(t.Context(), id1)
	if r := h.row(id1); r.Status != StatusFailed || r.FailureReason != ReasonNotHealthy {
		t.Fatalf("row = %+v", r)
	}
}

func TestProbeIgnoresRowsThatAreNotReady(t *testing.T) {
	h := newHarness(t)
	h.seed(id1, provisioningRow) // run not concluded
	if err := h.w.Probe(t.Context(), id1); err != nil || h.enq.count() != 0 {
		t.Fatalf("err=%v enq=%d: a probe before the run concludes must do nothing", err, h.enq.count())
	}
}

// ---- sweep -----------------------------------------------------------------------------------

func TestSweepEnqueuesTheRightTaskForStaleRowsOnly(t *testing.T) {
	h := newHarness(t)
	h.seed("pv-reqqqqqqqq", nil)                                                                                                // requested
	h.seed("pv-pollllllll", provisioningRow)                                                                                    // provisioning, run not concluded
	h.seed("pv-probbbbbbb", concludedRow)                                                                                       // concluded success
	h.seed("pv-fresssssss", func(e *EnvRow) { provisioningRow(e); e.UpdatedAt = t0.Add(59 * time.Second) })                     // looked at recently
	h.seed("pv-deadddddd", func(e *EnvRow) { provisioningRow(e); e.RunConclusion = "failure"; c := t0; e.RunConcludedAt = &c }) // worker died before failing it
	h.seed("pv-donnnnnnn", func(e *EnvRow) { e.Status = StatusRunning })
	h.clock = t0.Add(time.Minute) // staleAfter = max(3*15s, 2*30s) = 60s

	if err := h.w.Sweep(t.Context()); err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, c := range h.enq.calls {
		got[c.id] = c.typ
		if c.unique != 29*time.Second {
			t.Errorf("%s unique = %v, want 29s", c.id, c.unique)
		}
	}
	want := map[string]string{"pv-reqqqqqqqq": queue.TypeEnvDispatch, "pv-pollllllll": queue.TypeEnvPoll, "pv-probbbbbbb": queue.TypeEnvProbe}
	if len(got) != len(want) {
		t.Fatalf("enqueued %v, want %v", got, want)
	}
	for id, typ := range want {
		if got[id] != typ {
			t.Errorf("%s -> %q, want %q", id, got[id], typ)
		}
	}
	if r := h.row("pv-deadddddd"); r.Status != StatusFailed || r.FailureReason != ReasonRunFailed {
		t.Errorf("a concluded-but-failed row must be failed by the sweeper: %+v", r)
	}
}

func TestSweepSurvivesAnEnqueueError(t *testing.T) {
	h := newHarness(t)
	h.seed("pv-reqqqqqqqq", nil)
	h.clock = t0.Add(2 * time.Minute)
	h.enq.err = errors.New("task already exists") // the unique window refusing a duplicate
	if err := h.w.Sweep(t.Context()); err != nil {
		t.Fatalf("a duplicate-task refusal is expected and harmless: %v", err)
	}
}
```
`internal/envs/spans_test.go`

```go
package envs

import (
	"context"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"

	"github.com/ephematool/ephematool/internal/conventions"
	"github.com/ephematool/ephematool/internal/ghapp"
)

// useExporter installs an in-memory tracer provider and the W3C propagator for one test.
func useExporter(t *testing.T) *tracetest.InMemoryExporter {
	t.Helper()
	exp := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
	prevTP, prevProp := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() {
		otel.SetTracerProvider(prevTP)
		otel.SetTextMapPropagator(prevProp)
		_ = tp.Shutdown(context.Background())
	})
	return exp
}

func attr(s tracetest.SpanStub, key string) (attribute.Value, bool) {
	for _, kv := range s.Attributes {
		if string(kv.Key) == key {
			return kv.Value, true
		}
	}
	return attribute.Value{}, false
}

func TestRunSpansUseGitHubTimestampsAndTheStoredTrace(t *testing.T) {
	exp := useExporter(t)
	created := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	run := ghapp.Run{ID: 9000001, Conclusion: "success", HTMLURL: "http://x/runs/9000001",
		CreatedAt: created, RunStartedAt: created.Add(5 * time.Second), UpdatedAt: created.Add(time.Minute)}
	jobs := []ghapp.Job{{Name: "provision", Conclusion: "success", StartedAt: created.Add(5 * time.Second), CompletedAt: created.Add(50 * time.Second),
		Steps: []ghapp.Step{
			{Name: "Validate inputs", Number: 1, Conclusion: "success", StartedAt: created.Add(5 * time.Second), CompletedAt: created.Add(6 * time.Second)},
			{Name: "Provision namespace", Number: 2, Conclusion: "success", StartedAt: created.Add(6 * time.Second), CompletedAt: created.Add(9 * time.Second)},
			{Name: "Helm install", Number: 3, Conclusion: "success", StartedAt: created.Add(9 * time.Second), CompletedAt: created.Add(50 * time.Second)},
		}}}
	env := EnvRow{ID: "pv-aaaaaaaaaa", OwnerLogin: "octo-admin", Template: "web-app",
		Traceparent: "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"}

	emitRunSpans(env, run, jobs)

	spans := exp.GetSpans()
	if len(spans) != 5 {
		t.Fatalf("spans = %d, want 1 run + 1 job + 3 steps", len(spans))
	}
	byName := map[string][]tracetest.SpanStub{}
	for _, s := range spans {
		byName[s.Name] = append(byName[s.Name], s)
		if s.SpanContext.TraceID().String() != "0af7651916cd43dd8448eb211c80319c" {
			t.Errorf("%s is in trace %s, want the stored trace id", s.Name, s.SpanContext.TraceID())
		}
	}
	runSpan := byName["github.actions.run"][0]
	if !runSpan.StartTime.Equal(created) || !runSpan.EndTime.Equal(created.Add(time.Minute)) {
		t.Errorf("run span %v..%v, want 10:00:00..10:01:00", runSpan.StartTime, runSpan.EndTime)
	}
	if v, ok := attr(runSpan, "github.run.queue_ms"); !ok || v.AsInt64() != 5000 {
		t.Errorf("queue_ms = %v (%v), want 5000", v, ok)
	}
	for _, key := range []string{conventions.AttrEnvID, conventions.AttrOwner, conventions.AttrTemplate, "github.run.id", "github.run.html_url"} {
		if _, ok := attr(runSpan, key); !ok {
			t.Errorf("run span lacks %s", key)
		}
	}
	job := byName["github.actions.job"][0]
	if job.Parent.SpanID() != runSpan.SpanContext.SpanID() {
		t.Error("job is not a child of the run span")
	}
	if len(byName["github.actions.step"]) != 3 {
		t.Fatalf("steps = %d", len(byName["github.actions.step"]))
	}
	for _, s := range byName["github.actions.step"] {
		if s.Parent.SpanID() != job.SpanContext.SpanID() {
			t.Errorf("step %v is not a child of the job span", s.Name)
		}
	}
}

func TestFailedRunSpanIsAnError(t *testing.T) {
	exp := useExporter(t)
	now := time.Now()
	emitRunSpans(EnvRow{ID: "pv-aaaaaaaaaa", Traceparent: "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"},
		ghapp.Run{ID: 1, Conclusion: "failure", CreatedAt: now, UpdatedAt: now}, nil)
	spans := exp.GetSpans()
	if len(spans) != 1 || spans[0].Status.Code.String() != "Error" {
		t.Fatalf("spans = %+v, want one span with status Error", spans)
	}
}

func TestSpansNeverCarryInputsOrTokens(t *testing.T) {
	exp := useExporter(t)
	now := time.Now()
	emitRunSpans(EnvRow{ID: "pv-aaaaaaaaaa", Image: "ghcr.io/x@sha256:secret", Values: []byte(`{"app":{"debug":true}}`),
		Traceparent: "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01"},
		ghapp.Run{ID: 1, Conclusion: "success", CreatedAt: now, UpdatedAt: now}, nil)
	for _, s := range exp.GetSpans() {
		for _, kv := range s.Attributes {
			if v := kv.Value.Emit(); v == "ghcr.io/x@sha256:secret" || v == `{"app":{"debug":true}}` {
				t.Errorf("attribute %s leaks a workflow input", kv.Key)
			}
		}
	}
}

func TestSweepEnqueuesUnderTheStoredTrace(t *testing.T) {
	useExporter(t)
	ctx := traceContext(context.Background(), "00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01")
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() || sc.TraceID().String() != "0af7651916cd43dd8448eb211c80319c" {
		t.Fatalf("traceContext = %v, want the stored trace id", sc)
	}
	if got := traceContext(context.Background(), ""); trace.SpanContextFromContext(got).IsValid() {
		t.Error("an empty traceparent must not invent a trace")
	}
}

func TestProbeSpanRecordsTheResult(t *testing.T) {
	exp := useExporter(t)
	_, end := startProbeSpan(context.Background(), EnvRow{ID: "pv-aaaaaaaaaa"})
	end(true, 200)
	s := exp.GetSpans()
	if len(s) != 1 || s[0].Name != "env.probe" {
		t.Fatalf("spans = %+v", s)
	}
	if v, _ := attr(s[0], "env.probe.result"); v.AsString() != "healthy" {
		t.Errorf("result = %v, want healthy", v)
	}
}
```
```bash
bazel run //:gazelle
bazel test //internal/envs/... //internal/queue/...
```

---

## 6. `cmd/worker/main.go` and the e2e overlay (plan 02-11, task 2)

The worker opens the **app-role** pool (not the migrator role: D-24), builds the GitHub App client only when it is configured (a typed-nil pointer inside the `GitHub` interface would defeat the worker's `== nil` check), runs two asynq servers (default queues, and the serial `dispatch` queue) with separate muxes, and a scheduler for the sweep.

### 6.1 `cmd/worker/main.go` (replace)

`cmd/worker/main.go`

```go
// Command worker consumes asynq tasks and emits its own telemetry. It runs two servers: the
// default one (heartbeat, env:poll, env:probe, env:sweep) and a dispatch server with concurrency
// one, because workflow dispatches must be serial and spaced.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"

	"github.com/ephematool/ephematool/internal/buildinfo"
	"github.com/ephematool/ephematool/internal/config"
	"github.com/ephematool/ephematool/internal/envs"
	"github.com/ephematool/ephematool/internal/ghapp"
	"github.com/ephematool/ephematool/internal/queue"
	"github.com/ephematool/ephematool/internal/store"
	"github.com/ephematool/ephematool/internal/telemetry"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	shutdownTelemetry, err := telemetry.Setup(ctx, telemetry.Config{
		ServiceName: "ephematool-worker",
		Version:     buildinfo.Version,
		Env:         cfg.AppEnv,
	})
	if err != nil {
		return err
	}
	defer func() {
		fctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = shutdownTelemetry(fctx)
	}()

	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	// Same rule as the API: never record Redis command text in spans.
	if err := redisotel.InstrumentTracing(rdb, redisotel.WithDBStatement(false)); err != nil {
		return err
	}
	if err := redisotel.InstrumentMetrics(rdb); err != nil {
		return err
	}
	defer rdb.Close()

	// The worker connects as the restricted runtime role (D-24): it can read and update
	// environments, append audit rows, and nothing else. It never migrates.
	pool, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("database: %w", err)
	}
	defer pool.Close()

	// GitHub is optional. Without the App the worker still runs, and any environment that
	// needs GitHub fails with "GitHub App not configured" instead of retrying forever. The
	// typed-nil trap: only assign the interface when the client exists.
	var gh envs.GitHub
	if c, err := ghapp.New(cfg); err == nil {
		gh = c
	} else if errors.Is(err, ghapp.ErrNotConfigured) {
		slog.WarnContext(ctx, "ghapp.not_configured", "effect", "environments will fail until the GitHub App is set up")
	} else {
		return err
	}

	producer := queue.NewClient(cfg.RedisAddr)
	defer producer.Close()

	w := envs.NewWorker(envs.WorkerDeps{
		Cfg: cfg, Store: envs.NewPGWorkerStore(store.New(pool)), GH: gh, Enqueue: producer,
	})

	// Default server: heartbeat, poll, probe, sweep.
	srv := queue.NewServer(cfg.RedisAddr)
	mux := queue.NewMux(rdb)
	envs.RegisterWorker(mux, w)

	// Dispatch server: concurrency one, its own queue.
	dsrv := queue.NewDispatchServer(cfg.RedisAddr)
	dmux := queue.NewDispatchMux()
	envs.RegisterDispatch(dmux, w)

	sched, err := queue.NewScheduler(cfg.RedisAddr, cfg.HeartbeatInterval)
	if err != nil {
		return err
	}
	if err := queue.RegisterSweep(sched, cfg.EnvSweepInterval); err != nil {
		return err
	}
	if err := sched.Start(); err != nil {
		return err
	}
	if err := srv.Start(mux); err != nil {
		return err
	}
	if err := dsrv.Start(dmux); err != nil {
		return err
	}
	slog.InfoContext(ctx, "worker started", "version", buildinfo.Version)

	<-ctx.Done()
	slog.InfoContext(context.Background(), "worker shutting down")
	dsrv.Shutdown()
	srv.Shutdown()
	sched.Shutdown()
	return nil
}
```
### 6.2 `deploy/compose.e2e.yaml` (replace)

The worker block is new: it talks to the fake GitHub and, for now, to the fake's own health endpoint as the preview gateway. Section 11 swaps that for the real Traefik gate. `FAKE_RUN_STEP: 1s` makes the fake's runs finish in a few seconds.

`deploy/compose.e2e.yaml`

```yaml
# Overlay for dev/e2e login with the fake GitHub. NOT for real deployments (T-01-26).
services:
  fakegithub:
    build:
      context: ..
      dockerfile: Dockerfile
      target: go-runtime
      args:
        CMD: fakegithub
    # The fakegithub binary has no subcommands; ignore the image default CMD.
    command: []
    environment:
      FAKEGITHUB_ADDR: ":9999"
      # Dispatched runs advance by themselves: queued -> in_progress -> completed, one step per
      # second, so a create finishes in seconds without a test driving the fake by hand.
      FAKE_RUN_STEP: 1s
    ports:
      - "127.0.0.1:9999:9999"

  api:
    environment:
      - GITHUB_CLIENT_ID=e2e-client
      - GITHUB_CLIENT_SECRET=e2e-secret
      # Browser-facing authorize URL; server-facing token and API URLs.
      - GITHUB_AUTHORIZE_URL=http://localhost:9999/login/oauth/authorize
      - GITHUB_TOKEN_URL=http://fakegithub:9999/login/oauth/access_token
      - GITHUB_API_URL=http://fakegithub:9999/
      - ALLOWED_ORGS=ephematool-e2e
      - ALLOWED_TEAMS=partner-org/sre
      - ADMIN_LOGINS=Octo-Admin
      # The ping throttle is global, so parallel specs would steal each other's window and a page
      # load would get no worker span. Disabled for e2e only (the default stays 10s).
      - PING_THROTTLE=0
      # ---- Phase 2 (DEV/E2E ONLY): the platform acts as a GitHub App against the fake ----
      # The fake checks the App JWT's shape, not its signature, so the dev key from
      # tools/stack.sh (ensure_secrets) is enough; no real credential is ever involved.
      - ALLOWED_REPOS=ephematool-e2e/demo-app
      - CREATOR_TEAMS=ephematool-e2e/creators
      - GITHUB_APP_ID=12345
      - GITHUB_APP_INSTALLATION_ID=777
      - GITHUB_APP_PRIVATE_KEY_FILE=/run/ephematool-secrets/github-app-dev.pem
      - GHCR_BASE_URL=http://fakegithub:9999
      - ENVIRONMENTS_REPO=ephematool-e2e/ephematool-environments
    depends_on:
      fakegithub:
        condition: service_started

  # The worker drives environments to running or failed. Everything here is DEV/E2E ONLY.
  worker:
    environment:
      - GITHUB_API_URL=http://fakegithub:9999/
      - GITHUB_APP_ID=12345
      - GITHUB_APP_INSTALLATION_ID=777
      - GITHUB_APP_PRIVATE_KEY_FILE=/run/ephematool-secrets/github-app-dev.pem
      - ENVIRONMENTS_REPO=ephematool-e2e/ephematool-environments
      # Stand-in preview gateway: the fake answers /__ephematool/healthz with 200 once the run
      # for the Host's environment succeeded. Section 6 of the doc swaps this for the real
      # Traefik gateway (traefik:8088).
      - PREVIEW_GATEWAY_ADDR=fakegithub:9999
      - ENV_PROVISION_DEADLINE=2m
    depends_on:
      fakegithub:
        condition: service_started
```
### 6.3 Root `BUILD.bazel` (edit)

```python
test_suite(
    name = "test_integration",
    tests = [
        "//internal/auth:auth_test",
        "//internal/store:store_test",
        "//internal/audit:audit_test",
        "//internal/envs:envs_test",        # already there since 2b; now includes worker_integration_test.go
        "//internal/httpapi:httpapi_test",
    ],
)
```

---

## 7. Integration tests: real Postgres, real Redis, fake GitHub

`worker_integration_test.go` runs the worker's handlers against real Postgres, real Redis and the fake GitHub. The test calls `Dispatch`, `Poll`, `Probe` and `Sweep` directly and records what each would enqueue (`taskLog`), so each step is deterministic; only the idempotency test uses the real queue client. The fake maps the branch name to a scenario (`fail-x`, `cancel-x`, `stall-x`, `no-runid-x`). Highlights:

| Test | What it proves |
|---|---|
| HappyPath | requested → provisioning → (green run alone is **not** running) → healthy probe → running with `running_at` set; the fake recorded **exactly 1** dispatch carrying all 12 workflow inputs, including the env id and a traceparent |
| FailureScenarios | branches `fail-x` and `cancel-x` end `failed` with "Actions run failed" and "Actions run was cancelled" |
| StalledRunTimesOut | with a 1 s deadline a run that never finishes fails with the timeout reason |
| AmbiguousDispatch | branch `no-runid-x` (a `204` without a run id) leaves the row `requested`; the next delivery finds the run by correlation id and the fake saw **exactly 1** POST |
| UnhealthyPreview | a gateway address nobody listens on leads to "Preview did not become healthy" once the probe window passes |
| SweepRequeues, SweepIsIdempotentInsideTheUniqueWindow | the sweeper revives a stale `requested` row, and two sweeps inside the window enqueue one task (real Redis, asynq Inspector) |

`internal/envs/worker_integration_test.go`

```go
//go:build integration

package envs_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hibiken/asynq"

	"github.com/ephematool/ephematool/internal/envs"
	"github.com/ephematool/ephematool/internal/queue"
)

// taskLog records the follow-up tasks a handler schedules, so a test can run the chain by hand:
// the handlers are driven directly, the way asynq would call them, with no Redis timing in the way.
type taskLog struct {
	mu    sync.Mutex
	tasks []struct{ Typ, ID string }
}

func (l *taskLog) EnqueueEnv(_ context.Context, typ, id string, _, _ time.Duration) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.tasks = append(l.tasks, struct{ Typ, ID string }{typ, id})
	return nil
}

func (l *taskLog) has(typ string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, t := range l.tasks {
		if t.Typ == typ {
			return true
		}
	}
	return false
}

// workerFixture is the create fixture plus a Worker over the SAME database and the fake GitHub.
// The fake itself plays the preview gateway (its /__ephematool/healthz answers 200 once the run
// for the Host's environment succeeded), so the probe takes the production code path.
type workerFixture struct {
	*fixture
	w    *envs.Worker
	log  *taskLog
	addr string // gateway host:port
}

func newWorkerFixture(t *testing.T, mut func(*workerFixture)) *workerFixture {
	t.Helper()
	f := newFixture(t)
	wf := &workerFixture{fixture: f, log: &taskLog{}}
	cfg := f.cfg
	cfg.EnvProvisionDeadline = 2 * time.Minute
	cfg.EnvPollInitial, cfg.EnvPollMax = time.Second, 2*time.Second
	cfg.EnvProbeInitial, cfg.EnvProbeWindow, cfg.EnvProbeTimeout = time.Second, 2*time.Second, time.Second
	cfg.EnvSweepInterval, cfg.EnvDispatchSpacing = 30*time.Second, 0
	cfg.PreviewDomain, cfg.PreviewPort, cfg.PreviewScheme = "preview.localhost", 8088, "http"
	cfg.PreviewGatewayAddr = strings.TrimPrefix(strings.TrimSuffix(cfg.GitHubAPIURL, "/"), "http://") // the fake's host:port
	wf.addr, wf.cfg = cfg.PreviewGatewayAddr, cfg
	if mut != nil {
		mut(wf)
	}
	wf.w = envs.NewWorker(envs.WorkerDeps{
		Cfg: wf.cfg, Store: envs.NewPGWorkerStore(f.env.Queries), GH: f.gh, Enqueue: wf.log,
	})
	return wf
}

// createOn creates an environment for octo-creator on the given branch and returns its id.
func (wf *workerFixture) createOn(branch string) string {
	wf.t.Helper()
	req := goodReq()
	req.Ref = branch
	id, err := wf.create("octo-creator", 1007, req)
	if err != nil {
		wf.t.Fatalf("create on %s: %v", branch, err)
	}
	return id
}

func (wf *workerFixture) status(id string) (status, reason string) {
	wf.t.Helper()
	if err := wf.env.Owner.QueryRow(context.Background(),
		`select status, failure_reason from environments where id=$1`, id).Scan(&status, &reason); err != nil {
		wf.t.Fatal(err)
	}
	return
}

func (wf *workerFixture) runID(id string) int64 {
	wf.t.Helper()
	var run int64
	if err := wf.env.Owner.QueryRow(context.Background(),
		`select coalesce(run_id, 0) from environments where id=$1`, id).Scan(&run); err != nil {
		wf.t.Fatal(err)
	}
	return run
}

func (wf *workerFixture) mustDo(fn func(context.Context, string) error, id string) {
	wf.t.Helper()
	if err := fn(context.Background(), id); err != nil {
		wf.t.Fatal(err)
	}
}

// advance moves the fake run through queued -> in_progress -> completed (two steps).
func (wf *workerFixture) advance(id string, steps int) {
	wf.t.Helper()
	run := wf.runID(id)
	for i := 0; i < steps; i++ {
		if !wf.fake.AdvanceRun(run) {
			wf.t.Fatalf("fake has no run %d", run)
		}
	}
}

func TestWorkerHappyPath(t *testing.T) {
	wf := newWorkerFixture(t, nil)
	id := wf.createOn("main")
	wf.mustDo(wf.w.Dispatch, id)
	if s, _ := wf.status(id); s != "provisioning" {
		t.Fatalf("after dispatch: %s", s)
	}
	if !wf.log.has(queue.TypeEnvPoll) {
		t.Fatal("dispatch should schedule a poll")
	}

	wf.mustDo(wf.w.Poll, id) // run still queued: nothing changes
	if s, _ := wf.status(id); s != "provisioning" {
		t.Fatalf("queued run: %s", s)
	}
	wf.advance(id, 2) // in_progress, then success
	wf.mustDo(wf.w.Poll, id)
	if !wf.log.has(queue.TypeEnvProbe) {
		t.Fatal("a successful run should schedule a probe")
	}
	if s, _ := wf.status(id); s != "provisioning" {
		t.Fatalf("a green workflow alone must not mean running, got %s", s)
	}
	wf.mustDo(wf.w.Probe, id)
	if s, _ := wf.status(id); s != "running" {
		t.Fatalf("after a healthy probe: %s", s)
	}

	var runningAt *time.Time
	if err := wf.env.Owner.QueryRow(context.Background(), `select running_at from environments where id=$1`, id).Scan(&runningAt); err != nil || runningAt == nil {
		t.Fatalf("running_at not set: %v", err)
	}
	d := wf.fake.Dispatches()
	if len(d) != 1 {
		t.Fatalf("dispatches = %d, want exactly 1", len(d))
	}
	if len(d[0].Inputs) != 12 {
		t.Errorf("workflow inputs = %d, want 12: %v", len(d[0].Inputs), d[0].Inputs)
	}
	if d[0].Inputs["env_id"] != id || d[0].Inputs["traceparent"] == "" {
		t.Errorf("inputs lack the env id or traceparent: %v", d[0].Inputs)
	}
}

func TestWorkerFailureScenarios(t *testing.T) {
	for _, c := range []struct{ branch, reason string }{
		{"fail-x", envs.ReasonRunFailed},
		{"cancel-x", envs.ReasonRunCancelled},
	} {
		t.Run(c.branch, func(t *testing.T) {
			wf := newWorkerFixture(t, nil)
			id := wf.createOn(c.branch)
			wf.mustDo(wf.w.Dispatch, id)
			wf.advance(id, 2)
			wf.mustDo(wf.w.Poll, id)
			if s, reason := wf.status(id); s != "failed" || reason != c.reason {
				t.Fatalf("status=%s reason=%q, want failed %q", s, reason, c.reason)
			}
		})
	}
}

func TestWorkerStalledRunTimesOut(t *testing.T) {
	wf := newWorkerFixture(t, func(wf *workerFixture) { wf.cfg.EnvProvisionDeadline = time.Second })
	id := wf.createOn("stall-x")
	wf.mustDo(wf.w.Dispatch, id)
	wf.advance(id, 5) // a stalled run stays in progress forever
	time.Sleep(1500 * time.Millisecond)
	wf.mustDo(wf.w.Poll, id)
	if s, reason := wf.status(id); s != "failed" || reason != "Timed out waiting for the Actions run (1s)" {
		t.Fatalf("status=%s reason=%q", s, reason)
	}
}

func TestWorkerAmbiguousDispatchIsFoundByCorrelation(t *testing.T) {
	wf := newWorkerFixture(t, nil)
	id := wf.createOn("no-runid-x") // the fake answers 204 with no run id
	wf.mustDo(wf.w.Dispatch, id)
	if s, _ := wf.status(id); s != "requested" {
		t.Fatalf("after an ambiguous dispatch: %s, want still requested", s)
	}
	wf.mustDo(wf.w.Dispatch, id) // the follow-up delivery searches by correlation id
	if s, _ := wf.status(id); s != "provisioning" {
		t.Fatalf("after the correlation search: %s", s)
	}
	if n := len(wf.fake.Dispatches()); n != 1 {
		t.Fatalf("the fake saw %d POSTs, want exactly 1", n)
	}
}

func TestWorkerUnhealthyPreviewFails(t *testing.T) {
	// The gateway always answers 503 for this environment because the probe asks for a host the
	// fake does not know: point the gateway at a closed port instead.
	wf := newWorkerFixture(t, func(wf *workerFixture) { wf.cfg.PreviewGatewayAddr = "127.0.0.1:1" })
	id := wf.createOn("main")
	wf.mustDo(wf.w.Dispatch, id)
	wf.advance(id, 2)
	wf.mustDo(wf.w.Poll, id)
	time.Sleep(2500 * time.Millisecond) // past the 2s probe window
	wf.mustDo(wf.w.Probe, id)
	if s, reason := wf.status(id); s != "failed" || reason != envs.ReasonNotHealthy {
		t.Fatalf("status=%s reason=%q", s, reason)
	}
}

func TestWorkerSweepRequeuesLostWorkOnce(t *testing.T) {
	wf := newWorkerFixture(t, nil)
	id := wf.createOn("main") // the create-time task was "lost": nothing consumes recEnq
	// Age the row past the sweeper's staleness threshold (the DB clock is authoritative).
	if _, err := wf.env.Owner.Exec(context.Background(),
		`update environments set updated_at = now() - interval '5 minutes' where id=$1`, id); err != nil {
		t.Fatal(err)
	}
	wf.mustDo(func(ctx context.Context, _ string) error { return wf.w.Sweep(ctx) }, id)
	if !wf.log.has(queue.TypeEnvDispatch) {
		t.Fatal("the sweeper should re-enqueue a dispatch for a stale requested row")
	}
}

// With the REAL queue client and Redis: two sweeps inside the unique window enqueue one task.
func TestWorkerSweepIsIdempotentInsideTheUniqueWindow(t *testing.T) {
	wf := newWorkerFixture(t, nil)
	id := wf.createOn("main")
	if _, err := wf.env.Owner.Exec(context.Background(),
		`update environments set updated_at = now() - interval '5 minutes' where id=$1`, id); err != nil {
		t.Fatal(err)
	}
	producer := queue.NewClient(wf.env.RedisAddr)
	t.Cleanup(func() { _ = producer.Close() })
	w := envs.NewWorker(envs.WorkerDeps{
		Cfg: wf.cfg, Store: envs.NewPGWorkerStore(wf.env.Queries), GH: wf.gh, Enqueue: producer,
	})
	for i := 0; i < 2; i++ {
		if err := w.Sweep(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	insp := asynq.NewInspector(asynq.RedisClientOpt{Addr: wf.env.RedisAddr})
	t.Cleanup(func() { _ = insp.Close() })
	pending, err := insp.ListPendingTasks(queue.QueueDispatch)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, p := range pending {
		if strings.Contains(string(p.Payload), id) {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("pending dispatch tasks for %s = %d, want 1 (the second sweep was inside the unique window)", id, n)
	}
}
```
```bash
bazel run //:gazelle
bazel test --config=integration //internal/envs:envs_test
bazel test --config=integration //:test_integration
```

---

## 8. One trace for the whole creation (OBS-02) on the fake stack

### 8.1 `scripts/lib/verify-lib.sh` (edit: add `api_post`)

Add this function after `api_get`. It prints the HTTP status code and leaves the body, headers and trace id in `$VERIFY_TMP`. It sends an `Origin` equal to `API_URL`, which Go's cross-origin protection compares with `Host`; that is why nginx must forward the port (section 11.4).

`scripts/lib/verify-lib.sh`

```bash
# api_post PATH JSON -> prints the HTTP status code; body in $VERIFY_TMP/last-body,
# headers in $VERIFY_TMP/last-headers, trace id in $VERIFY_TMP/last-trace-id.
# Sends the session cookie jar and an Origin equal to API_URL, which Go's CrossOriginProtection
# compares with the Host header (so nginx must forward the port: see deploy/nginx/default.conf).
api_post() {
  local path="$1" json="$2" jar="$VERIFY_TMP/cookies" code
  [ -f "$jar" ] || : > "$jar"
  code="$(curl -sS -o "$VERIFY_TMP/last-body" -w '%{http_code}' -D "$VERIFY_TMP/last-headers" \
    -b "$jar" -c "$jar" -X POST -H "Origin: $API_URL" -H 'Content-Type: application/json' \
    -d "$json" "$API_URL$path")" || return 1
  header_value "$VERIFY_TMP/last-headers" x-trace-id > "$VERIFY_TMP/last-trace-id"
  echo "$code"
}

```
### 8.2 `scripts/verify.d/env-trace.sh` (new)

`scripts/verify.d/env-trace.sh`

```bash
#!/usr/bin/env bash
# OBS-02: creating an environment is ONE trace: the API request, the worker tasks, synthetic
# Actions run/job/step spans and the health probe. Runs on the fake stack: bazel run //:up_fake
set -euo pipefail
. "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/lib/verify-lib.sh"

ensure_session octo-admin

# 1. Create. octo-admin passes the creator check; the fake knows demo-app and its `main` branch.
code="$(api_post /api/environments \
  '{"repo":"ephematool-e2e/demo-app","refType":"branch","ref":"main","template":"web-app","size":"S","values":{}}')"
[ "$code" = "202" ] || fail "POST /api/environments returned HTTP $code: $(cat "$VERIFY_TMP/last-body")"
id="$(jq -r '.id' "$VERIFY_TMP/last-body")"
[ -n "$id" ] && [ "$id" != "null" ] || fail "create returned no environment id"
pass "created $id (202)"

# 2. Wait for running. With FAKE_RUN_STEP=1s this takes a few seconds; a failed environment
# reports its reason, which is far more useful than a timeout.
status=""
i=0
while [ "$i" -lt 90 ]; do
  api_get "/api/environments/$id" > "$VERIFY_TMP/env.json" || fail "GET /api/environments/$id failed"
  status="$(jq -r '.status' "$VERIFY_TMP/env.json")"
  [ "$status" = "running" ] && break
  [ "$status" = "failed" ] && fail "environment failed: $(jq -r '.failureReason' "$VERIFY_TMP/env.json")"
  sleep 1
  i=$((i + 1))
done
[ "$status" = "running" ] || fail "environment $id is still '$status' after 90s (is the worker running? bazel run //:logs)"
pass "$id is running after ${i}s"

# 3. One trace id, owned by the create request, must contain both services.
trace_id="$(jq -r '.traceId' "$VERIFY_TMP/env.json")"
[ -n "$trace_id" ] && [ "$trace_id" != "null" ] || fail "the environment has no traceId"
services="$(tempo_services "$trace_id" 45)"
echo "$services" | grep -qx 'ephematool-api'    || fail "trace $trace_id has no ephematool-api spans (services: ${services:-none})"
echo "$services" | grep -qx 'ephematool-worker' || fail "trace $trace_id has no ephematool-worker spans (services: ${services:-none})"
pass "trace $trace_id spans ephematool-api and ephematool-worker"

# 4. The synthetic and probe spans arrive a moment after the workers' batch flush: poll for them.
want="github.actions.run github.actions.job github.actions.step env.probe"
i=0
while [ "$i" -le 40 ]; do
  curl -fsS --max-time 5 "$TEMPO_URL/api/traces/$trace_id" -o "$VERIFY_TMP/trace.json" 2>/dev/null || true
  names="$(jq -r '[.batches[]?, .resourceSpans[]?] | .[] | .scopeSpans[]?.spans[]?.name' "$VERIFY_TMP/trace.json" 2>/dev/null | sort -u || true)"
  missing=""
  for n in $want; do echo "$names" | grep -qx "$n" || missing="$missing $n"; done
  [ -z "$missing" ] && break
  sleep 2
  i=$((i + 2))
done
[ -z "$missing" ] || fail "trace $trace_id lacks span(s):$missing (has: $(echo "$names" | tr '\n' ' '))"
pass "trace $trace_id contains $want"
```
```bash
bazel run //:up_fake
bazel run //:verify_telemetry -- env-trace
# PASS: created pv-... (202)
# PASS: pv-... is running after 6s
# PASS: trace ... spans ephematool-api and ephematool-worker
# PASS: trace ... contains github.actions.run github.actions.job github.actions.step env.probe
bazel run //:down
```

Open Grafana (`http://localhost:3000`, Explore → Tempo) and paste the trace id from the `PASS` line to see the API request, the worker tasks, the synthetic Actions spans and the probe on one timeline.

---

# Part B · The kind cluster

## 9. A cluster that runs untrusted previews safely (plan 02-13)

Previews run code from pull requests, so the cluster is built on the assumption that a preview is hostile. Five controls, each verified by `kind_verify` (section 10):

| Control | Mechanism | Verified by |
|---|---|---|
| Nothing but the gate is reachable | Traefik is the only entry; its **entrypoint** middleware chain (noindex headers + ForwardAuth) applies to every router | `kind_verify` step 7 |
| The provisioner is fenced | its ClusterRole can only create `pv-` namespaces carrying the managed label; `bind` and `serviceaccounts/token` are limited with `resourceNames: [ephematool-deployer]`; three ValidatingAdmissionPolicies deny the rest | step 6 |
| The deploying credential is per run | `provision.sh` mints a 15-minute token for `ephematool-deployer` in that one namespace; it cannot change quota, network policy or RBAC | step 6 |
| Resource caps are real | `ResourceQuota` plus `LimitRange` stamped from the size preset (GRD-01) | step 4 |
| Default-deny network | `NetworkPolicy` allows DNS and the gateway in, nothing else (GRD-04); kind's default CNI enforces it | step 5 |

Every pinned version (kind, Helm, node image, Traefik image and chart digest, helper images) lives in one file. `tools/kind.sh tools` downloads kind and Helm and **refuses to install a binary whose SHA-256 differs** from this file.

### 9.1 `deploy/kind/versions.env`

`deploy/kind/versions.env`

```bash
# Pinned versions, image digests and official checksums for Phase 2 (plan 02-01).
# Values taken from official sources on 2026-10-12. Source URL in the comment above each key.

# kubernetes-sigs/kind v0.33.0 release asset digests (gh api repos/kubernetes-sigs/kind/releases/tags/v0.33.0)
KIND_VERSION=v0.33.0
KIND_SHA256_DARWIN_ARM64=0c8c7dbe5e23594a198b786c4bc13dacc101fa6196b0cb0b23a1ca44e61f4b4f
KIND_SHA256_DARWIN_AMD64=5a99f26f57246dc9319dd294803313197a0f34d33c525b3ea8b655db5916ece0
KIND_SHA256_LINUX_AMD64=aee6151561422756b764a4ae28e7f44cda5af5a9eead3cc9985112b1de8d8e0d
# kind v0.33.0 release notes; digest confirmed via docker buildx imagetools inspect
KIND_NODE_IMAGE=kindest/node:v1.36.4@sha256:099e049362a1526b2db71494e1947aae99bd16290d7c895f2b7ea312e3cbfaed

# https://get.helm.sh/helm-v4.3.0-<os>-<arch>.tar.gz.sha256sum (published values)
HELM_VERSION=v4.3.0
HELM_SHA256_DARWIN_ARM64=d3870437e1e95b67f8edbde964156c84a26503f560821d40c542441658934fba
HELM_SHA256_DARWIN_AMD64=347a784877e0e20eac865e8d1c36a80f6bb0861d6f29abd34defb6570ef95d92
HELM_SHA256_LINUX_AMD64=86584a54def73570558f66f5111cc53dfed56689637ae32c1201205d494f54fb

# Minimum kubectl minor you install yourself
KUBECTL_MIN_MINOR=36

# Docker Hub multi-arch index digests (docker buildx imagetools inspect)
TRAEFIK_IMAGE=traefik:v3.7.14@sha256:575fa15b135078fe5e50aa847987d96dbddd7b093c172429618404df73f3fa7c
# https://traefik.github.io/charts/index.yaml, entry traefik 41.7.1
TRAEFIK_CHART_REPO=https://traefik.github.io/charts
TRAEFIK_CHART_VERSION=41.7.1
TRAEFIK_CHART_DIGEST=8566d98f9636c38ab59098978520a37160fa1e9f0a13af3dde4218529fa21d73
WHOAMI_IMAGE=traefik/whoami:v1.12.0@sha256:c4717a8d1f0134a7444e24f881160e033991f23027c6c5a9a3f8fd22e70d1d44
# busybox:stable (BusyBox v1.36.1) index digest
BUSYBOX_IMAGE=busybox:stable@sha256:73aaf090f3d85aa34ee199857f03fa3a95c8ede2ffd4cc2cdb5b94e566b11662

# actions/runner v2.338.0 release body (gh api repos/actions/runner/releases/tags/v2.338.0)
RUNNER_VERSION=2.338.0
RUNNER_SHA256_OSX_ARM64=df4cebda25c86a886ed204e49fee63f5c2e7cec5f447b5c98440a826bbdf9df2
RUNNER_SHA256_OSX_X64=dea7a58796ce215fc424a8b27c0fdd9b813fa5fc03d109a4a1d38131d6bfa1a3
RUNNER_SHA256_LINUX_X64=af4b794c1bc41d73d40535e3fe092a39f9679cd8d965954c2aca25a05ca41d32

# actions/checkout v7.0.1, dereferenced to a commit (gh api repos/actions/checkout/git/ref/tags/v7.0.1, type=commit)
CHECKOUT_ACTION=actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1

# Go modules and tools (installed later, plans 02-05 and 02-07)
GO_JSONSCHEMA=github.com/santhosh-tekuri/jsonschema/v6@v6.0.3
GO_JWT=github.com/golang-jwt/jwt/v5@v5.3.1
# Approved in Phase 1, reused
SHADCN_CLI=shadcn@4.21.4
```
### 9.2 `cluster.yaml` and `traefik-values.yaml`

The cluster maps NodePort `30080` to `127.0.0.1:${PREVIEW_PORT}` (loopback only). `__KIND_NODE_IMAGE__` and `__PREVIEW_PORT__` are substituted by `kind.sh`.

`deploy/kind/cluster.yaml`

```yaml
# kind cluster for local previews. tools/kind.sh substitutes the two placeholders below from
# deploy/kind/versions.env and the environment, then runs `kind create cluster --config`.
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
name: ephematool
nodes:
  - role: control-plane
    image: __KIND_NODE_IMAGE__
    extraPortMappings:
      # Traefik's NodePort (30080, see traefik-values.yaml) becomes 127.0.0.1:<PREVIEW_PORT> on
      # the host. Bound to IPv4 loopback only: it is never reachable from the network, and macOS
      # resolves *.localhost to ::1 first, so a client may pause briefly before falling back to
      # IPv4 (Pitfall 2).
      - containerPort: 30080
        hostPort: __PREVIEW_PORT__
        listenAddress: "127.0.0.1"
        protocol: TCP
```
The Traefik values put the middleware chain on the **entrypoint** (`ports.web.http.middlewares`), turn the access log off (a ticket is a query-string secret) and turn the dashboard and unused providers off.

`deploy/kind/traefik-values.yaml`

```yaml
# Values for the Traefik Helm chart (version pinned by TRAEFIK_CHART_VERSION in versions.env).
# tools/kind.sh adds --set image.tag=<tag@digest> from TRAEFIK_IMAGE, so the image is pinned.
#
# The one setting that matters: ports.web.http.middlewares attaches the preview-guard chain to the
# ENTRYPOINT. Traefik runs entrypoint middlewares before any router's own, for every router
# on that entrypoint, so a template author cannot forget or remove authentication (D-20).
ports:
  web:
    nodePort: 30080
    http:
      middlewares:
        - traefik-preview-guard@kubernetescrd
  websecure:
    expose:
      default: false
service:
  type: NodePort
providers:
  kubernetesCRD:
    enabled: true
    # A Middleware may only be referenced from its own namespace. The guard is entrypoint-level,
    # so previews never reference it and cannot reference anyone else's.
    allowCrossNamespace: false
  kubernetesIngress:
    enabled: true
    publishedService:
      enabled: false
ingressRoute:
  dashboard:
    enabled: false
logs:
  general:
    level: INFO
  access:
    # Off on purpose: preview tickets travel in query strings and must never reach a log.
    enabled: false
```
### 9.3 `traefik-middlewares.yaml`

Three `Middleware` objects in the `traefik` namespace, identical in effect to `deploy/traefik/dynamic.yaml` (section 11). `__API_AUTH_ADDR__` is where a kind node reaches the API on the host.

`deploy/kind/traefik-middlewares.yaml`

```yaml
# The preview gate: three Middleware objects in namespace "traefik" (kubernetes CRD provider).
# tools/kind.sh substitutes __API_AUTH_ADDR__ (default host.docker.internal:8081, the API's
# published port as seen from a kind node on Docker Desktop).
#
# The same chain exists for compose in deploy/traefik/dynamic.yaml. Keep the two identical.
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: preview-headers
  namespace: traefik
spec:
  headers:
    customResponseHeaders:
      X-Robots-Tag: "noindex, nofollow"
      Referrer-Policy: "no-referrer"
---
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: preview-forward-auth
  namespace: traefik
spec:
  forwardAuth:
    address: http://__API_AUTH_ADDR__/internal/preview/auth
    # A non-2xx answer (the 302 with Set-Cookie, the 403) is copied to the browser as is,
    # including Location and Set-Cookie; this keeps the Location the auth handler chose.
    preserveLocationHeader: true
    maxResponseBodySize: 4096
    # Traefik always adds X-Forwarded-Host/-Uri/-Method; the handler also needs the cookies.
    authRequestHeaders: [Cookie]
    # The handler answers 200 with a Cookie header that no longer contains our own cookie.
    # Copying it onto the request is what stops the preview app from ever seeing it.
    authResponseHeaders: [Cookie]
---
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: preview-guard
  namespace: traefik
spec:
  chain:
    middlewares:
      - name: preview-headers
      - name: preview-forward-auth
```
### 9.4 `bootstrap-rbac.yaml`: the provisioner and the deployer role

`deploy/kind/bootstrap-rbac.yaml`

```yaml
# Two Kubernetes credentials with different power (D-21, resolving D-02):
#
#   provisioner  long-lived; can create ONLY pv-<id> namespaces and their guardrails. Its token is
#                exported to a chmod-600 kubeconfig on the runner host. It is never a GitHub secret.
#   deployer     minted PER RUN, 15 minutes, scoped to one namespace; can deploy the app but cannot
#                touch quota, network policy, RBAC or namespaces.
#
# RBAC says what each may do; the admission policies in admission-policies.yaml say WHERE
# the provisioner may do it, so even a bug in the verbs cannot reach another namespace.
apiVersion: v1
kind: ServiceAccount
metadata:
  name: ephematool-provisioner
  namespace: kube-system
---
# Since Kubernetes 1.24 a ServiceAccount has no token Secret by default; this one is created on
# purpose so the runner host has a credential that does not expire every hour.
apiVersion: v1
kind: Secret
metadata:
  name: ephematool-provisioner-token
  namespace: kube-system
  annotations:
    kubernetes.io/service-account.name: ephematool-provisioner
type: kubernetes.io/service-account-token
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: ephematool-provisioner
rules:
  - apiGroups: [""]
    resources: [namespaces]
    verbs: [create, get, list, patch]
  - apiGroups: [""]
    resources: [resourcequotas, limitranges, serviceaccounts]
    verbs: [create, get, update, patch]
  - apiGroups: [networking.k8s.io]
    resources: [networkpolicies]
    verbs: [create, get, update, patch]
  - apiGroups: [rbac.authorization.k8s.io]
    resources: [rolebindings]
    verbs: [create, get, update, patch]
  # Creating a RoleBinding to a role you do not hold needs the "bind" verb on that role. Restricted
  # to ONE ClusterRole, so the provisioner can never bind admin or cluster-admin.
  - apiGroups: [rbac.authorization.k8s.io]
    resources: [clusterroles]
    verbs: [bind]
    resourceNames: [ephematool-deployer]
  # Mint a token for the deployer ServiceAccount only (resourceNames names the ServiceAccount).
  - apiGroups: [""]
    resources: [serviceaccounts/token]
    verbs: [create]
    resourceNames: [ephematool-deployer]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: ephematool-provisioner
subjects:
  - kind: ServiceAccount
    name: ephematool-provisioner
    namespace: kube-system
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: ephematool-provisioner
---
# What the per-run deployer token may do inside ITS namespace (bound there by a RoleBinding that
# the baseline chart creates). Note the absences: no rbac.authorization.k8s.io, no namespaces, and
# only READ access to resourcequotas, limitranges and networkpolicies.
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: ephematool-deployer
rules:
  - apiGroups: [""]
    resources: [configmaps, services, pods, pods/log, secrets, events, persistentvolumeclaims]
    verbs: [get, list, watch, create, update, patch, delete]
  - apiGroups: [apps]
    resources: [deployments, deployments/scale, statefulsets, replicasets]
    verbs: [get, list, watch, create, update, patch, delete]
  - apiGroups: [batch]
    resources: [jobs, cronjobs]
    verbs: [get, list, watch, create, update, patch, delete]
  - apiGroups: [networking.k8s.io]
    resources: [ingresses]
    verbs: [get, list, watch, create, update, patch, delete]
  - apiGroups: [traefik.io]
    resources: [middlewares]
    verbs: [get, list, watch, create, update, patch, delete]
  # Read-only: useful for debugging a failed rollout, useless for weakening a guardrail.
  - apiGroups: [""]
    resources: [resourcequotas, limitranges]
    verbs: [get, list, watch]
  - apiGroups: [networking.k8s.io]
    resources: [networkpolicies]
    verbs: [get, list, watch]
```
### 9.5 `admission-policies.yaml`: three ValidatingAdmissionPolicies

They apply to **everyone**, including cluster admins, inside managed namespaces: the provisioner may only create `pv-<id>` namespaces that carry the managed label, may only write into managed `pv-` namespaces, and an Ingress may only name its own host.

`deploy/kind/admission-policies.yaml`

```yaml
# ValidatingAdmissionPolicies (CEL, GA since Kubernetes 1.30): no webhook server to run.
# tools/kind.sh substitutes the two placeholders before applying:
#   __LABEL_MANAGED__   the "managed" label key from environments/conventions.gen.env
#   __PREVIEW_DOMAIN__  PREVIEW_DOMAIN (default preview.localhost)
#
# Policies 1 and 2 apply to the PROVISIONER only: they pin it to pv-<id> namespaces that carry
# the managed label. Policy 3 applies to EVERYONE: an Ingress in a managed namespace may only
# route its own host, so a template cannot claim another preview's name.
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicy
metadata:
  name: ephematool-provisioner-namespaces
spec:
  failurePolicy: Fail
  matchConstraints:
    resourceRules:
      - apiGroups: [""]
        apiVersions: ["v1"]
        operations: ["CREATE", "UPDATE"]
        resources: ["namespaces"]
  matchConditions:
    - name: is-provisioner
      expression: "request.userInfo.username == 'system:serviceaccount:kube-system:ephematool-provisioner'"
  validations:
    - expression: "object.metadata.name.matches('^pv-[a-z0-9]{8,16}$')"
      message: "the provisioner may only manage namespaces named pv-<id>"
    - expression: "has(object.metadata.labels) && '__LABEL_MANAGED__' in object.metadata.labels && object.metadata.labels['__LABEL_MANAGED__'] == 'true'"
      message: "a namespace created by the provisioner must carry the managed label"
---
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicyBinding
metadata:
  name: ephematool-provisioner-namespaces
spec:
  policyName: ephematool-provisioner-namespaces
  validationActions: [Deny]
---
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicy
metadata:
  name: ephematool-provisioner-objects
spec:
  failurePolicy: Fail
  matchConstraints:
    resourceRules:
      - apiGroups: [""]
        apiVersions: ["v1"]
        operations: ["CREATE", "UPDATE"]
        resources: ["resourcequotas", "limitranges", "serviceaccounts"]
      - apiGroups: ["networking.k8s.io"]
        apiVersions: ["v1"]
        operations: ["CREATE", "UPDATE"]
        resources: ["networkpolicies"]
      - apiGroups: ["rbac.authorization.k8s.io"]
        apiVersions: ["v1"]
        operations: ["CREATE", "UPDATE"]
        resources: ["rolebindings"]
  matchConditions:
    - name: is-provisioner
      expression: "request.userInfo.username == 'system:serviceaccount:kube-system:ephematool-provisioner'"
  validations:
    - expression: "namespaceObject.metadata.name.startsWith('pv-')"
      message: "the provisioner may only write into pv-<id> namespaces"
    - expression: "has(namespaceObject.metadata.labels) && '__LABEL_MANAGED__' in namespaceObject.metadata.labels && namespaceObject.metadata.labels['__LABEL_MANAGED__'] == 'true'"
      message: "the provisioner may only write into namespaces that carry the managed label"
---
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicyBinding
metadata:
  name: ephematool-provisioner-objects
spec:
  policyName: ephematool-provisioner-objects
  validationActions: [Deny]
---
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicy
metadata:
  name: ephematool-ingress-host
spec:
  failurePolicy: Fail
  matchConstraints:
    namespaceSelector:
      matchLabels:
        __LABEL_MANAGED__: "true"
    resourceRules:
      - apiGroups: ["networking.k8s.io"]
        apiVersions: ["v1"]
        operations: ["CREATE", "UPDATE"]
        resources: ["ingresses"]
  validations:
    - expression: "has(object.spec.rules) && size(object.spec.rules) > 0 && object.spec.rules.all(r, has(r.host) && r.host == namespaceObject.metadata.name + '.__PREVIEW_DOMAIN__')"
      message: "every Ingress rule must name this environment's own host (<namespace>.<preview domain>)"
    - expression: "!has(object.spec.defaultBackend)"
      message: "an Ingress may not set a default backend"
---
apiVersion: admissionregistration.k8s.io/v1
kind: ValidatingAdmissionPolicyBinding
metadata:
  name: ephematool-ingress-host
spec:
  policyName: ephematool-ingress-host
  validationActions: [Deny]
```
### 9.6 `tools/kind.sh` (new)

Commands: `tools` installs pinned, checksum-verified `kind` and `helm` into `.tools/bin`; `up` creates the cluster, installs Traefik (digest-checked chart, `--wait --timeout 6m` because the first image pull is slow), applies the middleware, RBAC and policies, and writes the provisioner kubeconfig (`chmod 600`, never printed); `down` deletes the cluster; `verify` runs the nine checks in section 10.

On Linux Docker, `host.docker.internal` is often not reachable from kind nodes. Set `KIND_API_ADDR` to an address the nodes can reach, for example the Docker bridge `172.17.0.1:8081`, before `kind_up`.

`tools/kind.sh`

```bash
#!/usr/bin/env bash
# Local Kubernetes for previews, behind the `bazel run` targets in //:BUILD.bazel:
#
#   bazel run //:kind_tools     install pinned, checksum-verified kind and helm into .tools/bin
#   bazel run //:kind_up        create the cluster, Traefik gateway, guardrails and credentials
#   bazel run //:kind_down      delete the cluster
#   bazel run //:kind_verify    prove the guardrails and the gateway on a real cluster (no GitHub)
#
# Versions, image digests and checksums come from deploy/kind/versions.env. Never call bazel
# from here: a nested bazel inside `bazel run` blocks on the output base lock.
set -euo pipefail

if [ -n "${BUILD_WORKSPACE_DIRECTORY:-}" ]; then
  ROOT="$BUILD_WORKSPACE_DIRECTORY"
else
  ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fi
cd "$ROOT"

# shellcheck disable=SC1091
. deploy/kind/versions.env
# The label and attribute keys come from the generated file; nothing here is hand-written.
# shellcheck disable=SC1091
. environments/conventions.gen.env

CLUSTER=ephematool
CTX="kind-$CLUSTER"
PREVIEW_PORT="${PREVIEW_PORT:-8088}"
PREVIEW_DOMAIN="${PREVIEW_DOMAIN:-preview.localhost}"
# How a kind node reaches the API on the host. host.docker.internal works on Docker Desktop;
# on Linux Docker use the bridge address and publish the API on it, e.g. KIND_API_ADDR=172.17.0.1:8081.
KIND_API_ADDR="${KIND_API_ADDR:-host.docker.internal:8081}"
TOOLS="$ROOT/.tools/bin"
SECRETS="$ROOT/deploy/.secrets"
PROVISIONER_KC="$SECRETS/kind-provisioner.kubeconfig"
export PATH="$TOOLS:$PATH"

log() { echo "kind: $*"; }
die() {
  echo "kind: $*" >&2
  exit 1
}

# ------------------------------------------------------------------------------------ tools
platform() {
  local os arch
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$(uname -m)" in
    x86_64 | amd64) arch=amd64 ;;
    arm64 | aarch64) arch=arm64 ;;
    *) die "unsupported CPU $(uname -m)" ;;
  esac
  echo "$os $arch"
}

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then sha256sum "$1" | awk '{print $1}'; else shasum -a 256 "$1" | awk '{print $1}'; fi
}

# download_verified URL DEST EXPECTED_SHA256: nothing is made executable until the checksum matches.
download_verified() {
  local url="$1" dest="$2" want="$3" tmp got
  tmp="$(mktemp)"
  curl -fsSL --retry 3 -o "$tmp" "$url" || die "download failed: $url"
  got="$(sha256_of "$tmp")"
  if [ "$got" != "$want" ]; then
    rm -f "$tmp"
    die "checksum mismatch for $url (got $got, want $want); refusing to install"
  fi
  mv "$tmp" "$dest"
}

cmd_tools() {
  local os arch var want
  read -r os arch < <(platform)
  mkdir -p "$TOOLS"

  var="KIND_SHA256_$(echo "${os}_${arch}" | tr '[:lower:]' '[:upper:]')"
  want="${!var:-}"
  [ -n "$want" ] || die "no pinned kind checksum for $os/$arch in versions.env"
  if ! "$TOOLS/kind" version 2>/dev/null | grep -q "$KIND_VERSION"; then
    log "installing kind $KIND_VERSION"
    download_verified "https://github.com/kubernetes-sigs/kind/releases/download/$KIND_VERSION/kind-$os-$arch" "$TOOLS/kind" "$want"
    chmod +x "$TOOLS/kind"
  fi

  var="HELM_SHA256_$(echo "${os}_${arch}" | tr '[:lower:]' '[:upper:]')"
  want="${!var:-}"
  [ -n "$want" ] || die "no pinned helm checksum for $os/$arch in versions.env"
  if ! "$TOOLS/helm" version --short 2>/dev/null | grep -q "^$HELM_VERSION"; then
    log "installing helm $HELM_VERSION"
    local tgz extract
    tgz="$(mktemp)"
    extract="$(mktemp -d)"
    download_verified "https://get.helm.sh/helm-$HELM_VERSION-$os-$arch.tar.gz" "$tgz" "$want"
    tar -xzf "$tgz" -C "$extract" "$os-$arch/helm"
    mv "$extract/$os-$arch/helm" "$TOOLS/helm"
    chmod +x "$TOOLS/helm"
    rm -rf "$tgz" "$extract"
  fi

  log "kind and helm are installed in .tools/bin. kubectl is yours to install (1.$KUBECTL_MIN_MINOR or newer)."
  check_tools
}

need() { command -v "$1" >/dev/null 2>&1 || die "$1 not found. Run: bazel run //:kind_tools"; }

check_tools() {
  need docker
  need kind
  need helm
  need kubectl
  kind version 2>/dev/null | grep -q "$KIND_VERSION" ||
    die "kind $KIND_VERSION is required (found: $(kind version 2>&1 | head -1)). Run: bazel run //:kind_tools"
  [[ "$(helm version --short 2>/dev/null)" == "$HELM_VERSION"* ]] ||
    die "helm $HELM_VERSION is required (found: $(helm version --short 2>&1 | head -1)). Run: bazel run //:kind_tools"
  local minor
  minor="$(kubectl version --client -o json 2>/dev/null | sed -n 's/.*"minor": *"\([0-9]*\).*/\1/p' | head -1)"
  [ "${minor:-0}" -ge "$KUBECTL_MIN_MINOR" ] ||
    die "kubectl 1.$KUBECTL_MIN_MINOR or newer is required (found minor ${minor:-none}). Install it from https://kubernetes.io/docs/tasks/tools/"
}

# ------------------------------------------------------------------------------------ cluster
port_in_use() { (exec 3<>"/dev/tcp/127.0.0.1/$1") 2>/dev/null; }

# render FILE: print the manifest with the placeholders substituted.
render() {
  sed \
    -e "s|__KIND_NODE_IMAGE__|$KIND_NODE_IMAGE|g" \
    -e "s|__PREVIEW_PORT__|$PREVIEW_PORT|g" \
    -e "s|__API_AUTH_ADDR__|$KIND_API_ADDR|g" \
    -e "s|__LABEL_MANAGED__|$EPHEMATOOL_LABEL_MANAGED|g" \
    -e "s|__PREVIEW_DOMAIN__|$PREVIEW_DOMAIN|g" \
    "$1"
}

K() { kubectl --context "$CTX" "$@"; }

install_traefik() {
  local dir tag tgz
  dir="$(mktemp -d)"
  tgz="$dir/traefik-$TRAEFIK_CHART_VERSION.tgz"
  helm pull traefik --repo "$TRAEFIK_CHART_REPO" --version "$TRAEFIK_CHART_VERSION" -d "$dir" >/dev/null
  [ "$(sha256_of "$tgz")" = "$TRAEFIK_CHART_DIGEST" ] || {
    rm -rf "$dir"
    die "the Traefik chart does not match TRAEFIK_CHART_DIGEST; refusing to install"
  }
  tag="${TRAEFIK_IMAGE#traefik:}" # v3.7.14@sha256:...: the chart builds repository:tag
  log "installing Traefik (first run pulls the image; allow several minutes)"
  # --wait with a generous timeout: the first image pull on a fresh node is slow (Pitfall 9).
  helm upgrade --install traefik "$tgz" --kube-context "$CTX" -n traefik --create-namespace \
    -f deploy/kind/traefik-values.yaml --set-string "image.tag=$tag" --wait --timeout 6m
  rm -rf "$dir"
}

write_provisioner_kubeconfig() {
  local tok="" ca server i
  for i in $(seq 1 30); do
    tok="$(K -n kube-system get secret ephematool-provisioner-token -o jsonpath='{.data.token}' 2>/dev/null || true)"
    [ -n "$tok" ] && break
    sleep 1
  done
  [ -n "$tok" ] || die "the provisioner token Secret was never populated"
  ca="$(K -n kube-system get secret ephematool-provisioner-token -o jsonpath='{.data.ca\.crt}')"
  server="$(K config view --raw --minify -o jsonpath='{.clusters[0].cluster.server}')"
  mkdir -p "$SECRETS"
  chmod 700 "$SECRETS"
  (
    umask 077 # the file is created 600; the token is never printed
    cat >"$PROVISIONER_KC" <<YAML
apiVersion: v1
kind: Config
clusters:
  - name: preview
    cluster: {server: "$server", certificate-authority-data: "$ca"}
users:
  - name: provisioner
    user: {token: "$(printf '%s' "$tok" | base64 --decode)"}
contexts:
  - name: provisioner
    context: {cluster: preview, user: provisioner}
current-context: provisioner
YAML
  )
  chmod 600 "$PROVISIONER_KC"
}

cmd_up() {
  check_tools
  docker info >/dev/null 2>&1 || die "Docker is not running"
  if ! kind get clusters 2>/dev/null | grep -qx "$CLUSTER"; then
    if port_in_use "$PREVIEW_PORT"; then
      die "127.0.0.1:$PREVIEW_PORT is in use (the compose preview gateway from bazel run //:up_fake uses it too; stop it with bazel run //:down)"
    fi
    local cfg
    cfg="$(mktemp)"
    render deploy/kind/cluster.yaml >"$cfg"
    log "creating cluster $CLUSTER (Kubernetes from $KIND_NODE_IMAGE)"
    kind create cluster --config "$cfg" --wait 120s
    rm -f "$cfg"
  fi
  K cluster-info >/dev/null || die "cannot reach the $CTX cluster"

  install_traefik
  log "applying the preview gate, RBAC and admission policies"
  render deploy/kind/traefik-middlewares.yaml | K apply -f -
  K apply -f deploy/kind/bootstrap-rbac.yaml
  render deploy/kind/admission-policies.yaml | K apply -f -
  write_provisioner_kubeconfig
  log "ready: previews are served at http://<id>.$PREVIEW_DOMAIN:$PREVIEW_PORT"
  log "provisioner credential: deploy/.secrets/kind-provisioner.kubeconfig (chmod 600)"
}

cmd_down() {
  need kind
  kind delete cluster --name "$CLUSTER"
  rm -f "$PROVISIONER_KC"
}

# ------------------------------------------------------------------------------------ verify
PASSED=0
pass() {
  PASSED=$((PASSED + 1))
  echo "PASS: $*"
}
fail() {
  echo "FAIL: $*" >&2
  exit 1
}
# wait_until SECONDS CMD...: poll a command until it succeeds.
wait_until() {
  local secs="$1" i=0
  shift
  until "$@" >/dev/null 2>&1; do
    i=$((i + 1))
    [ "$i" -ge "$secs" ] && return 1
    sleep 1
  done
}

cmd_verify() {
  check_tools
  kind get clusters 2>/dev/null | grep -qx "$CLUSTER" || die "no kind cluster. Run: bazel run //:kind_up"
  [ -f "$PROVISIONER_KC" ] || die "no provisioner kubeconfig. Run: bazel run //:kind_up"
  curl -fsS --max-time 3 -o /dev/null http://127.0.0.1:8081/api/auth/config ||
    die "the API is not reachable on 127.0.0.1:8081 (the kind gateway asks it who may enter). Run: bazel run //:up"

  local ENV=pv-kverify001 SHA40 IMG VER TMP
  SHA40="$(printf 'c%.0s' {1..40})"
  IMG="ghcr.io/ephematool-e2e/demo-app:$SHA40"
  VER="$(awk '/^version:/ {gsub(/["\047]/, "", $2); print $2; exit}' templates/web-app/Chart.yaml)"
  TMP="$(mktemp -d)"
  trap 'rm -rf "$TMP"' RETURN
  local P=(kubectl --kubeconfig "$PROVISIONER_KC")

  # ---- 1. the sample app image, loaded into the cluster (no registry involved)
  log "building the sample app image"
  docker build -q --build-arg CMD=sampleapp --target go-runtime -t "$IMG" . >/dev/null || fail "docker build of the sample app failed"
  kind load docker-image "$IMG" --name "$CLUSTER" >/dev/null || fail "kind load docker-image failed"
  docker pull -q "$BUSYBOX_IMAGE" >/dev/null && kind load docker-image "$BUSYBOX_IMAGE" --name "$CLUSTER" >/dev/null || true
  pass "image $IMG loaded into the cluster"

  # ---- 2. provision exactly as the workflow does
  K delete namespace "$ENV" --ignore-not-found --wait=true >/dev/null
  log "provisioning $ENV (helm --wait; this is the slow step)"
  (
    export ENV_ID="$ENV" CORRELATION_ID=cr-0123456789abcdef REPO=ephematool-e2e/demo-app SHA="$SHA40" IMAGE="$IMG"
    export TEMPLATE=web-app CHART_VERSION="$VER" SIZE=S OWNER=octo-admin EXPIRES_AT="$(($(date +%s) + 86400))"
    export PROVISIONER_KUBECONFIG="$PROVISIONER_KC" TEMPLATES_DIR="$ROOT/templates" PREVIEW_DOMAIN
    VALUES_B64="$(printf '{"replicas":1,"healthPath":"/health"}' | base64 | tr -d '\n')"
    export VALUES_B64
    TRACEPARENT="00-$(head -c16 /dev/urandom | od -An -tx1 | tr -d ' \n')-$(head -c8 /dev/urandom | od -An -tx1 | tr -d ' \n')-01"
    export TRACEPARENT
    export OTEL_ENDPOINT="${OTEL_ENDPOINT:-http://host.docker.internal:4320}"
    environments/scripts/provision.sh
  ) || fail "provision.sh failed"
  pass "provision.sh deployed $ENV"

  # ---- 3. labels and Pod Security Admission
  lbl() { K get namespace "$ENV" -o "jsonpath={.metadata.labels.${1//./\\.}}"; }
  [ "$(lbl "$EPHEMATOOL_LABEL_MANAGED")" = true ] || fail "managed label missing"
  [ "$(lbl "$EPHEMATOOL_LABEL_ENV_ID")" = "$ENV" ] || fail "env-id label wrong"
  [ "$(lbl "$EPHEMATOOL_LABEL_OWNER")" = octo-admin ] || fail "owner label wrong"
  [ "$(lbl "$EPHEMATOOL_LABEL_TEMPLATE")" = web-app ] || fail "template label wrong"
  [[ "$(lbl "$EPHEMATOOL_LABEL_EXPIRES_AT")" =~ ^[0-9]{10}$ ]] || fail "expires-at label is not epoch seconds"
  [ "$(lbl pod-security.kubernetes.io/enforce)" = baseline ] || fail "Pod Security enforce label missing"
  pass "namespace carries the five platform labels and Pod Security baseline"

  # ---- 4. GRD-01: the quota is real
  [ "$(K -n "$ENV" get resourcequota ephematool-quota -o 'jsonpath={.spec.hard.limits\.cpu}')" = 500m ] || fail "quota limits.cpu is not the S preset"
  K -n "$ENV" get limitrange ephematool-limits >/dev/null || fail "LimitRange missing"
  K -n "$ENV" scale deployment/web-app --replicas=6 >/dev/null
  quota_hit() { K -n "$ENV" get events -o jsonpath='{.items[*].message}' | grep -q 'exceeded quota'; }
  wait_until 60 quota_hit || fail "scaling to 6 replicas never produced an 'exceeded quota' event"
  K -n "$ENV" scale deployment/web-app --replicas=1 >/dev/null
  pass "GRD-01: the sixth replica was rejected with 'exceeded quota'"

  # ---- 5. GRD-04: default-deny network policy
  probe() { # probe NAMESPACE NAME SHELL_COMMAND: run a one-shot busybox pod, return the command's status
    K -n "$1" run "$2" --image="$BUSYBOX_IMAGE" --image-pull-policy=IfNotPresent --restart=Never --rm -i \
      --pod-running-timeout=90s --command -- sh -c "$3" >/dev/null 2>&1
  }
  probe "$ENV" np-dns 'nslookup kubernetes.default.svc.cluster.local' || fail "DNS lookup failed inside the namespace (allow-dns-egress)"
  ! probe "$ENV" np-egress 'wget -T 3 -q -O /dev/null http://1.1.1.1' || fail "a pod reached the internet (default-deny egress is not working)"
  ! probe default np-cross "wget -T 3 -q -O /dev/null http://web-app.$ENV.svc.cluster.local" || fail "a pod in another namespace reached the preview Service"
  pass "GRD-04: DNS works; internet and cross-namespace traffic are denied"

  # ---- 6. fenced credentials
  ! "${P[@]}" create namespace evil >/dev/null 2>&1 || fail "the provisioner created namespace 'evil'"
  ! "${P[@]}" -n kube-system get secrets >/dev/null 2>&1 || fail "the provisioner read kube-system secrets"
  pass "the provisioner cannot create other namespaces or read kube-system"
  local tok server
  tok="$("${P[@]}" create token ephematool-deployer -n "$ENV" --duration=10m)"
  server="$("${P[@]}" config view --raw --minify -o jsonpath='{.clusters[0].cluster.server}')"
  "${P[@]}" config view --raw --minify -o jsonpath='{.clusters[0].cluster.certificate-authority-data}' | base64 --decode >"$TMP/ca.crt"
  local D=(kubectl --server "$server" --certificate-authority "$TMP/ca.crt" --token "$tok" -n "$ENV")
  "${D[@]}" get pods >/dev/null || fail "the deployer token cannot list pods in its own namespace"
  ! "${D[@]}" patch networkpolicy default-deny-all --type=merge -p '{"spec":{"podSelector":{"matchLabels":{"x":"y"}}}}' >/dev/null 2>&1 ||
    fail "the deployer changed a NetworkPolicy"
  ! "${D[@]}" patch resourcequota ephematool-quota --type=merge -p '{"spec":{"hard":{"pods":"500"}}}' >/dev/null 2>&1 ||
    fail "the deployer changed the ResourceQuota"
  ! "${D[@]}" create rolebinding evil --clusterrole=ephematool-deployer --serviceaccount="$ENV:ephematool-deployer" >/dev/null 2>&1 ||
    fail "the deployer created a RoleBinding"
  unset tok
  pass "the deployer cannot change quota, network policy or RBAC"
  bad_ingress="$(K -n "$ENV" create ingress bad --class=traefik --rule="other.$PREVIEW_DOMAIN/*=web-app:80" 2>&1 || true)"
  grep -q "own host" <<<"$bad_ingress" || fail "an Ingress for another preview's host was not rejected: $bad_ingress"
  pass "an Ingress for another host is rejected by the admission policy"

  # ---- 7. ENV-09: the gate, through the real Traefik
  GW="http://127.0.0.1:$PREVIEW_PORT"
  hostH="Host: $ENV.$PREVIEW_DOMAIN:$PREVIEW_PORT"
  healthy() { [ "$(curl -s -o /dev/null -D "$TMP/h" -w '%{http_code}' -H "$hostH" "$GW/__ephematool/healthz")" = 200 ]; }
  wait_until 90 healthy || fail "the health path never answered 200 through the gateway"
  tr -d '\r' <"$TMP/h" | grep -qi '^x-robots-tag: noindex, nofollow' || fail "the preview response lacks X-Robots-Tag: noindex"
  code="$(curl -s -o /dev/null -D "$TMP/r" -w '%{http_code}' -H "$hostH" "$GW/")"
  [ "$code" = 302 ] || fail "GET / without a cookie returned $code, want 302"
  tr -d '\r' <"$TMP/r" | grep -i '^location:' | grep -q "http://localhost:8080/preview/authorize?env=$ENV" ||
    fail "the redirect does not point at the dashboard authorize page"
  pass "ENV-09: health is open and tagged noindex; everything else redirects to the dashboard"

  # ---- 8. OBS-03: the app's telemetry arrives tagged
  tagged() {
    curl -fsS --get "http://127.0.0.1:3200/api/search" \
      --data-urlencode "tags=$EPHEMATOOL_ATTR_ENV_ID=$ENV $EPHEMATOOL_ATTR_OWNER=octo-admin $EPHEMATOOL_ATTR_TEMPLATE=web-app" |
      grep -q '"traceID"'
  }
  wait_until 90 tagged || fail "no trace tagged with $ENV reached Tempo within 90s (collector on 4320 up? app NetworkPolicy ok?)"
  pass "OBS-03: Tempo holds a trace tagged with the environment id, owner and template"

  # ---- 9. cleanup
  if [ "${KEEP:-}" = 1 ]; then
    log "KEEP=1: leaving $ENV in place"
  else
    K delete namespace "$ENV" --wait=false >/dev/null
  fi
  log "kind_verify: $PASSED checks passed"
}

# ------------------------------------------------------------------------------------ main
if [ $# -lt 1 ]; then
  echo "usage: $0 <tools|up|down|verify>" >&2
  exit 2
fi
cmd="$1"
shift
case "$cmd" in
  tools) cmd_tools "$@" ;;
  up) cmd_up "$@" ;;
  down) cmd_down "$@" ;;
  verify) cmd_verify "$@" ;;
  *)
    echo "unknown command: $cmd" >&2
    echo "usage: $0 <tools|up|down|verify>" >&2
    exit 2
    ;;
esac
```
### 9.7 Root `BUILD.bazel` (edit: four more targets)

```python
# Local kind cluster for previews on real Kubernetes (tools/kind.sh).
#   bazel run //:kind_tools   install pinned kind and helm into .tools/bin
#   bazel run //:kind_up      create the cluster, gateway, guardrails and credentials
#   bazel run //:kind_down    delete the cluster
#   bazel run //:kind_verify  prove the guardrails and the gate on the real cluster
[
    sh_binary(
        name = "kind_" + cmd,
        srcs = ["tools/kind.sh"],
        args = [cmd],
        tags = [
            "local",
            "manual",
            "requires-docker",
        ],
    )
    for cmd in [
        "tools",
        "up",
        "down",
        "verify",
    ]
]
```

```bash
echo '.tools' >> .bazelignore      # .gitignore already lists .tools/ and deploy/.secrets/
chmod +x tools/kind.sh
bash -n tools/kind.sh && shellcheck tools/kind.sh   # shellcheck optional
```

---

## 10. The environments repository content and the end-to-end cluster check

The workflow GitHub runs, the script it runs, and the namespace baseline. **The workflow is thin on purpose (D-14):** it maps its twelve inputs to environment variables and calls `provision.sh`; all logic lives in a script you can run and test without GitHub. `kind_verify` runs this same script.

### 10.1 `environments/.github/workflows/create-environment.yml`

Inputs reach the shell only through `env:`, never through `${{ inputs.* }}` inside `run:` (script injection). Permissions are `contents: read`, there is one concurrency group per environment, and the third-party action is pinned to a commit sha.

`environments/.github/workflows/create-environment.yml`

```yaml
# Create one preview environment. The platform dispatches this workflow; nothing else may.
#
# HARDENING REQUIREMENTS for the repository that holds this file (it runs on a developer machine):
#   1. The repository is PRIVATE. Self-hosted runners must almost never serve public repos.
#   2. Only `workflow_dispatch` triggers it. No push, pull_request or pull_request_target.
#   3. Forks are disabled, and only admins may change .github/workflows/** (editing this file is
#      code execution on the runner's machine).
#   4. The runner is EPHEMERAL (one job, then it deregisters) and carries the label below.
#   5. No repository secrets. The provisioner kubeconfig lives on the runner host
#      (PROVISIONER_KUBECONFIG, set in the runner's .env file), never in GitHub.
#
# Inputs reach the shell ONLY through `env:` and are validated by scripts/provision.sh before any
# cluster call. Never write ${{ inputs.* }} inside a `run:` block: GitHub substitutes it as text
# before the shell parses it, which is command injection.
name: create-environment

# Expressions are allowed in run-name (not in run:). The platform finds its run by this title.
run-name: create ${{ inputs.env_id }} ${{ inputs.correlation_id }}

on:
  workflow_dispatch:
    inputs:
      env_id: {description: "Environment id (pv-xxxxxxxxxx)", required: true, type: string}
      correlation_id: {description: "Platform correlation id", required: true, type: string}
      traceparent: {description: "W3C trace context of the create request", required: true, type: string}
      repo: {description: "Source repository (org/name)", required: true, type: string}
      sha: {description: "Commit being previewed (40 hex)", required: true, type: string}
      image: {description: "Digest-pinned image reference", required: true, type: string}
      template: {description: "Approved template name", required: true, type: string}
      chart_version: {description: "Template chart version the platform validated against", required: true, type: string}
      size: {description: "Size preset (S, M or L)", required: true, type: string}
      values_b64: {description: "Template values, base64 of a JSON object", required: true, type: string}
      owner: {description: "GitHub login of the creator", required: true, type: string}
      expires_at: {description: "Expiry, Unix epoch seconds", required: true, type: string}

permissions:
  contents: read

concurrency:
  group: create-${{ inputs.env_id }}
  cancel-in-progress: false

jobs:
  provision:
    runs-on: [self-hosted, ephematool-kind]
    timeout-minutes: 15
    steps:
      # Pinned to a commit, not a tag. The tag is in the comment so a human can read it.
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
        with:
          persist-credentials: false
      - name: Provision
        run: ./scripts/provision.sh
        env:
          ENV_ID: ${{ inputs.env_id }}
          CORRELATION_ID: ${{ inputs.correlation_id }}
          TRACEPARENT: ${{ inputs.traceparent }}
          REPO: ${{ inputs.repo }}
          SHA: ${{ inputs.sha }}
          IMAGE: ${{ inputs.image }}
          TEMPLATE: ${{ inputs.template }}
          CHART_VERSION: ${{ inputs.chart_version }}
          SIZE: ${{ inputs.size }}
          VALUES_B64: ${{ inputs.values_b64 }}
          OWNER: ${{ inputs.owner }}
          EXPIRES_AT: ${{ inputs.expires_at }}
```
### 10.2 `environments/scripts/provision.sh`

Every input is validated with a strict regex **before** any cluster call (exit `2`), so a hostile input fails without touching anything. `PROVISION_DRY_RUN=1` renders the baseline with `helm template` and stops. Platform values go in with `--set-string` **after** `-f`, so they always win over the user's values file.

`environments/scripts/provision.sh`

```bash
#!/usr/bin/env bash
# provision.sh: creates one preview environment. It is the ONLY provisioning logic: the GitHub
# workflow and `kind_verify` both run this exact script, so what is tested is what ships.
#
# Inputs arrive as environment variables (the workflow maps its inputs to env: and nothing else).
# Every one is validated against a strict pattern BEFORE any cluster call, so a hostile input
# fails with exit code 2 and touches nothing. Exit codes: 2 invalid input, 1 cluster failure.
#
#   ENV_ID CORRELATION_ID TRACEPARENT REPO SHA IMAGE TEMPLATE CHART_VERSION SIZE VALUES_B64
#   OWNER EXPIRES_AT                       (set by the workflow from the dispatch inputs)
#   PROVISIONER_KUBECONFIG                 (set on the runner host; never a GitHub secret)
#   PREVIEW_DOMAIN   default preview.localhost
#   TEMPLATES_DIR    default <repo>/templates, or ../templates in the monorepo
#   SIZES_FILE       default $TEMPLATES_DIR/sizes.yaml
#   OTEL_ENDPOINT    default http://host.docker.internal:4320 (the environments OTLP receiver)
#   PROVISION_DRY_RUN=1   validate, render the baseline with `helm template`, and stop
#
# Secrets (the provisioner's kubeconfig, the minted deployer token) are never echoed.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)" # the environments root: baseline/ and scripts/ live here

invalid() {
  echo "provision: invalid input: $*" >&2
  exit 2
}

# require NAME REGEX: the variable must be set and match the pattern in full.
require() {
  local name="$1" re="$2" val="${!1-}"
  [[ -n "$val" ]] || invalid "$name is required"
  [[ "$val" =~ $re ]] || invalid "$name does not match $re"
}

# ---------------------------------------------------------------------------------- validate
RE_ENV_ID='^pv-[a-z0-9]{10}$'
RE_CORR='^cr-[0-9a-f]{16}$'
RE_SHA='^[0-9a-f]{40}$'
RE_REPO='^[A-Za-z0-9-]+/[A-Za-z0-9_.-]+$'
RE_TEMPLATE='^[a-z0-9-]+$'
RE_SIZE='^(S|M|L)$'
RE_IMAGE='^ghcr\.io/[a-z0-9._/-]+(@sha256:[0-9a-f]{64}|:[A-Za-z0-9._-]{1,128})$'
RE_OWNER='^[A-Za-z0-9-]{1,39}$'
RE_EXPIRES='^[0-9]{10}$'
RE_TRACEPARENT='^00-[0-9a-f]{32}-[0-9a-f]{16}-[0-9a-f]{2}$'
RE_B64='^[A-Za-z0-9+/=]*$'

require ENV_ID "$RE_ENV_ID"
require CORRELATION_ID "$RE_CORR"
require REPO "$RE_REPO"
require SHA "$RE_SHA"
require IMAGE "$RE_IMAGE"
require TEMPLATE "$RE_TEMPLATE"
require SIZE "$RE_SIZE"
require OWNER "$RE_OWNER"
require EXPIRES_AT "$RE_EXPIRES"
[[ -z "${TRACEPARENT:-}" ]] || require TRACEPARENT "$RE_TRACEPARENT"
[[ -n "${CHART_VERSION:-}" ]] || invalid "CHART_VERSION is required"

PREVIEW_DOMAIN="${PREVIEW_DOMAIN:-preview.localhost}"
[[ "$PREVIEW_DOMAIN" =~ ^[a-z0-9]([a-z0-9.-]*[a-z0-9])?$ ]] || invalid "PREVIEW_DOMAIN is not a lowercase domain"
OTEL_ENDPOINT="${OTEL_ENDPOINT:-http://host.docker.internal:4320}"
[[ "$OTEL_ENDPOINT" =~ ^https?://[A-Za-z0-9.:-]+$ ]] || invalid "OTEL_ENDPOINT must be http(s)://host[:port]"

if [[ -z "${TEMPLATES_DIR:-}" ]]; then
  if [[ -d "$ROOT/templates" ]]; then TEMPLATES_DIR="$ROOT/templates"; else TEMPLATES_DIR="$ROOT/../templates"; fi
fi
SIZES_FILE="${SIZES_FILE:-$TEMPLATES_DIR/sizes.yaml}"
CHART_DIR="$TEMPLATES_DIR/$TEMPLATE"
[[ -f "$CHART_DIR/Chart.yaml" ]] || invalid "TEMPLATE $TEMPLATE is not an approved template"
[[ -f "$SIZES_FILE" ]] || invalid "sizes file not found"

# Chart/server skew: the API validated values against ITS copy of the chart. If the checked-out
# chart differs, refuse before anything is created.
chart_version="$(awk '/^version:/ {gsub(/["\047]/, "", $2); print $2; exit}' "$CHART_DIR/Chart.yaml")"
[[ "$chart_version" == "$CHART_VERSION" ]] || invalid "CHART_VERSION $CHART_VERSION does not match the checked-out chart ($chart_version)"

# VALUES_B64 is untrusted JSON: bounded, decodable and an object.
require VALUES_B64 "$RE_B64"
[[ ${#VALUES_B64} -le 24000 ]] || invalid "VALUES_B64 is too large"
WORK="$(mktemp -d)"
chmod 700 "$WORK"
trap 'rm -rf "$WORK"' EXIT # removes the values file and, later, the deployer kubeconfig
VALUES_FILE="$WORK/values.json"
printf '%s' "$VALUES_B64" | base64 --decode >"$VALUES_FILE" 2>/dev/null || invalid "VALUES_B64 is not valid base64"
chmod 600 "$VALUES_FILE"
[[ "$(wc -c <"$VALUES_FILE")" -le 16384 ]] || invalid "values exceed 16 KiB"
is_json_object() {
  if command -v jq >/dev/null 2>&1; then
    jq -e 'type == "object"' "$1" >/dev/null 2>&1
  else
    python3 -c 'import json,sys; sys.exit(0 if isinstance(json.load(open(sys.argv[1])), dict) else 1)' "$1" 2>/dev/null
  fi
}
is_json_object "$VALUES_FILE" || invalid "values must be a JSON object"

# ---------------------------------------------------------------------------------- render
# The label keys come from the generated conventions file, never from this script.
CONV="$ROOT/conventions.gen.env"
[[ -f "$CONV" ]] || CONV="$ROOT/environments/conventions.gen.env"
[[ -f "$CONV" ]] || invalid "conventions.gen.env not found"
# shellcheck disable=SC1090
. "$CONV"

BASELINE=(helm template baseline "$ROOT/baseline" -n "$ENV_ID" -f "$SIZES_FILE"
  --set-string "size=$SIZE" --set-string "envId=$ENV_ID" --set-string "owner=$OWNER"
  --set-string "template=$TEMPLATE" --set-string "expiresAt=$EXPIRES_AT"
  --set-string "labelKeys.managed=$EPHEMATOOL_LABEL_MANAGED" --set-string "labelKeys.envId=$EPHEMATOOL_LABEL_ENV_ID"
  --set-string "labelKeys.owner=$EPHEMATOOL_LABEL_OWNER" --set-string "labelKeys.template=$EPHEMATOOL_LABEL_TEMPLATE"
  --set-string "labelKeys.expiresAt=$EPHEMATOOL_LABEL_EXPIRES_AT")

if [[ "${PROVISION_DRY_RUN:-}" == "1" ]]; then
  "${BASELINE[@]}" # prints the rendered quota, limits, policies and bindings; nothing is applied
  echo "provision: dry run complete for $ENV_ID" >&2
  exit 0
fi

# ---------------------------------------------------------------------------------- provision
[[ -n "${PROVISIONER_KUBECONFIG:-}" && -f "$PROVISIONER_KUBECONFIG" ]] || {
  echo "provision: PROVISIONER_KUBECONFIG is not set or not a file" >&2
  exit 1
}
KP=(kubectl --kubeconfig "$PROVISIONER_KUBECONFIG")

echo "provision: $ENV_ID namespace and guardrails"
# Labels (epoch seconds: a label value cannot contain ':'; the database holds the real deadline)
# and Pod Security Admission: baseline is enforced, restricted is reported.
"${KP[@]}" apply -f - <<YAML
apiVersion: v1
kind: Namespace
metadata:
  name: $ENV_ID
  labels:
    $EPHEMATOOL_LABEL_MANAGED: "true"
    $EPHEMATOOL_LABEL_ENV_ID: "$ENV_ID"
    $EPHEMATOOL_LABEL_OWNER: "$OWNER"
    $EPHEMATOOL_LABEL_TEMPLATE: "$TEMPLATE"
    $EPHEMATOOL_LABEL_EXPIRES_AT: "$EXPIRES_AT"
    pod-security.kubernetes.io/enforce: baseline
    pod-security.kubernetes.io/warn: restricted
    pod-security.kubernetes.io/audit: restricted
YAML
# No Helm release for the baseline: the provisioner applies the rendered objects directly, so
# nothing the deployer can read or delete describes the guardrails.
"${BASELINE[@]}" | "${KP[@]}" apply -f -

echo "provision: minting a 15-minute deployer token"
TOKEN="$("${KP[@]}" create token ephematool-deployer -n "$ENV_ID" --duration=15m)"
SERVER="$("${KP[@]}" config view --raw --minify -o jsonpath='{.clusters[0].cluster.server}')"
CA="$("${KP[@]}" config view --raw --minify -o jsonpath='{.clusters[0].cluster.certificate-authority-data}')"
DEPLOYER_KC="$WORK/deployer.kubeconfig"
(
  umask 077
  cat >"$DEPLOYER_KC" <<YAML
apiVersion: v1
kind: Config
clusters:
  - name: preview
    cluster: {server: "$SERVER", certificate-authority-data: "$CA"}
users:
  - name: deployer
    user: {token: "$TOKEN"}
contexts:
  - name: deployer
    context: {cluster: preview, user: deployer, namespace: "$ENV_ID"}
current-context: deployer
YAML
)
unset TOKEN

# Platform values go in with --set-string AFTER -f, so they always win over the user's file.
# Commas separate --set pairs, so the attribute list escapes its own.
ATTRS="$EPHEMATOOL_ATTR_ENV_ID=$ENV_ID,$EPHEMATOOL_ATTR_OWNER=$OWNER,$EPHEMATOOL_ATTR_TEMPLATE=$TEMPLATE,k8s.namespace.name=$ENV_ID"
echo "provision: installing $TEMPLATE $CHART_VERSION"
helm upgrade --install app "$CHART_DIR" --kubeconfig "$DEPLOYER_KC" -n "$ENV_ID" \
  -f "$VALUES_FILE" \
  --set-string "image=$IMAGE" \
  --set-string "platform.envId=$ENV_ID" \
  --set-string "platform.host=$ENV_ID.$PREVIEW_DOMAIN" \
  --set-string "platform.otelEndpoint=$OTEL_ENDPOINT" \
  --set-string "platform.otelResourceAttributes=${ATTRS//,/\\,}" \
  --wait --timeout 5m
echo "provision: $ENV_ID is deployed"
```
### 10.3 `environments/scripts/provision_test.sh`

Twenty-two hostile or malformed inputs must each exit `2` before `kubectl` or `helm` runs (they are stubs that fail the test if called). One extra case renders the baseline and needs a real `helm`; it skips without one.

`environments/scripts/provision_test.sh`

```bash
#!/usr/bin/env bash
# Tests for provision.sh that need no cluster. Two things are proven:
#   1. every hostile or malformed input is rejected with exit code 2 BEFORE kubectl or helm runs
#      (the stub binaries put first on PATH record any call and fail the test);
#   2. a valid input renders the baseline with the size preset's numbers and applies nothing
#      (needs a real `helm`; skipped with a message when none is installed).
# Run it directly (bash environments/scripts/provision_test.sh) or as bazel test //environments:provision_test.
set -uo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROVISION="$HERE/provision.sh"
ENVROOT="$(cd "$HERE/.." && pwd)"
if [[ -d "$ENVROOT/../templates" ]]; then TEMPLATES="$ENVROOT/../templates"; else TEMPLATES="$ENVROOT/templates"; fi
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

# A real helm, if one is installed (found BEFORE the stubs are put on PATH).
REAL_HELM="$(command -v helm 2>/dev/null || true)"
[[ -n "$REAL_HELM" ]] || { [[ -x "$ENVROOT/../.tools/bin/helm" ]] && REAL_HELM="$ENVROOT/../.tools/bin/helm"; }

# Stubs: any call records itself and fails loudly.
STUBS="$TMP/stubs"
mkdir -p "$STUBS"
for tool in kubectl helm; do
  printf '#!/bin/sh\necho "%s $*" >> "%s/called"\nexit 99\n' "$tool" "$TMP" >"$STUBS/$tool"
  chmod +x "$STUBS/$tool"
done

failures=0
fail() { echo "FAIL: $*" >&2; failures=$((failures + 1)); }
pass() { echo "PASS: $*"; }

CHART_VERSION="$(awk '/^version:/ {gsub(/["\047]/, "", $2); print $2; exit}' "$TEMPLATES/web-app/Chart.yaml")"
VALUES_OK="$(printf '{"replicas":1}' | base64 | tr -d '\n')"

# valid_env prints the variable assignments of a fully valid input set, one per line.
valid_env() {
  cat <<EOV
ENV_ID=pv-abcdefghij
CORRELATION_ID=cr-0123456789abcdef
TRACEPARENT=00-0af7651916cd43dd8448eb211c80319c-b7ad6b7169203331-01
REPO=ephematool-e2e/demo-app
SHA=$(printf 'a%.0s' {1..40})
IMAGE=ghcr.io/ephematool-e2e/demo-app@sha256:$(printf 'b%.0s' {1..64})
TEMPLATE=web-app
CHART_VERSION=$CHART_VERSION
SIZE=S
VALUES_B64=$VALUES_OK
OWNER=octo-admin
EXPIRES_AT=1792000000
TEMPLATES_DIR=$TEMPLATES
EOV
}

# run_with NAME=VALUE...: run provision.sh with the valid set overridden. Sets $rc and $out.
run_with() {
  local -a over=("$@") args=()
  local line k skip
  while IFS= read -r line; do
    k="${line%%=*}"
    skip=""
    for o in "${over[@]}"; do [[ "${o%%=*}" == "$k" ]] && skip=1; done
    [[ -n "$skip" ]] || args+=("$line")
  done < <(valid_env)
  args+=("${over[@]}")
  rm -f "$TMP/called"
  out="$(env -i PATH="$STUBS:/usr/bin:/bin:/usr/local/bin" HOME="$TMP" "${args[@]}" bash "$PROVISION" 2>&1)"
  rc=$?
}

# reject LABEL NAME=VALUE...: must exit 2 and never call kubectl or helm.
reject() {
  local label="$1"
  shift
  run_with "$@"
  if [[ "$rc" != 2 ]]; then
    fail "$label: exit $rc, want 2 ($out)"
  elif [[ -f "$TMP/called" ]]; then
    fail "$label: called $(cat "$TMP/called")"
  else
    pass "$label rejected"
  fi
}

reject "ENV_ID uppercase" 'ENV_ID=pv-ABC'
reject "ENV_ID word" 'ENV_ID=evil'
reject "ENV_ID substitution" 'ENV_ID=pv-abc$(id)'
reject "ENV_ID too long" 'ENV_ID=pv-abcdefghijk'
reject "SHA 39 chars" "SHA=$(printf 'a%.0s' {1..39})"
reject "SHA uppercase" "SHA=$(printf 'A%.0s' {1..40})"
reject "REPO injection" 'REPO=a/b;rm -rf'
reject "TEMPLATE traversal" 'TEMPLATE=../x'
reject "TEMPLATE unknown" 'TEMPLATE=nope'
reject "SIZE XL" 'SIZE=XL'
reject "IMAGE other registry" 'IMAGE=docker.io/x:y'
reject "IMAGE spaces" 'IMAGE=ghcr.io/x/y:z --set a=b'
reject "OWNER injection" 'OWNER=octo;id'
reject "EXPIRES_AT scientific" 'EXPIRES_AT=1e9'
reject "CHART_VERSION skew" 'CHART_VERSION=9.9.9'
reject "CORRELATION_ID" 'CORRELATION_ID=nope'
reject "TRACEPARENT" 'TRACEPARENT=not-a-traceparent'
reject "VALUES_B64 not base64" 'VALUES_B64=!!!'
reject "VALUES_B64 array" "VALUES_B64=$(printf '[1,2]' | base64 | tr -d '\n')"
reject "VALUES_B64 scalar" "VALUES_B64=$(printf '"x"' | base64 | tr -d '\n')"
big="$(python3 -c 'import base64,json; print(base64.b64encode(json.dumps({"a":"x"*20000}).encode()).decode())')"
reject "VALUES_B64 over 16 KiB" "VALUES_B64=$big"
reject "missing ENV_ID" 'ENV_ID='

# A valid set must get past validation: with PROVISION_DRY_RUN and a failing helm stub it
# reaches the helm call (exit 99 from the stub), proving every check passed.
run_with PROVISION_DRY_RUN=1
if [[ "$rc" == 2 ]]; then fail "valid input was rejected: $out"; else pass "valid input passes validation (reached helm, rc=$rc)"; fi

# The dry run proper, with a real helm.
if [[ -n "$REAL_HELM" ]]; then
  helmdir="$(dirname "$REAL_HELM")"
  rm -f "$TMP/called"
  mapfile -t vars < <(valid_env)
  out="$(env -i PATH="$helmdir:/usr/bin:/bin" HOME="$TMP" "${vars[@]}" PROVISION_DRY_RUN=1 bash "$PROVISION" 2>&1)"
  rc=$?
  if [[ "$rc" != 0 ]]; then
    fail "dry run exited $rc: $out"
  else
    grep -q 'requests.cpu: "250m"' <<<"$out" && grep -q 'limits.memory: "512Mi"' <<<"$out" \
      && grep -q 'persistentvolumeclaims: "0"' <<<"$out" && pass "dry run renders the size S quota" \
      || fail "dry run output lacks the size S quota values"
    ! grep -q 'kubectl apply' <<<"$out" || fail "dry run mentions kubectl apply"
  fi
else
  echo "SKIP: dry-run render (no helm installed; run bazel run //:kind_tools)"
fi

[[ "$failures" == 0 ]] || { echo "$failures failure(s)" >&2; exit 1; }
echo "all provision tests passed"
```
### 10.4 The baseline chart

It renders the namespace's `ResourceQuota`, `LimitRange`, four `NetworkPolicy` objects, the service accounts and the deployer's `RoleBinding`, from the size preset in `templates/sizes.yaml`. The provisioner applies the rendered objects directly instead of installing a release, so nothing the deployer can read describes the guardrails.

`environments/baseline/Chart.yaml`

```yaml
apiVersion: v2
name: baseline
description: Platform-owned namespace guardrails (quota, limits, network policy, service accounts, RBAC). Applied by the provisioner, never by the template's deployer.
type: application
version: 0.1.0
```
`environments/baseline/values.yaml`

```yaml
# All of these are set by scripts/provision.sh. The defaults exist only so `helm template` fails
# loudly (required/fail) instead of rendering something half-empty.
envId: ""
owner: ""
template: ""
expiresAt: ""
size: S

# Label keys arrive as VALUES (--set-string labelKeys.managed=...), taken from the generated
# conventions file, so no key is hand-written in this chart.
labelKeys:
  managed: ""
  envId: ""
  owner: ""
  template: ""
  expiresAt: ""

# The size presets: provision.sh passes templates/sizes.yaml with -f, which fills `sizes:`.
sizes: {}
```
`environments/baseline/templates/_helpers.tpl`

```text
{{- define "baseline.size" -}}
{{- required (printf "unknown size %q (sizes.yaml defines S, M and L)" .Values.size) (index .Values.sizes .Values.size) | toJson -}}
{{- end -}}

{{/* Labels every object carries. Keys come from values, never from this file. */}}
{{- define "baseline.labels" -}}
{{ required "labelKeys.managed is set by provision.sh" .Values.labelKeys.managed | quote }}: "true"
{{ required "labelKeys.envId is set by provision.sh" .Values.labelKeys.envId | quote }}: {{ required "envId is set by provision.sh" .Values.envId | quote }}
{{- end -}}
```
`environments/baseline/templates/resourcequota.yaml`

```yaml
{{- /* The hard ceiling for the whole namespace (GRD-01). persistentvolumeclaims, nodeports and
       load balancers are pinned to zero: previews get no storage and no external addresses. */ -}}
{{- $s := include "baseline.size" . | fromJson }}
apiVersion: v1
kind: ResourceQuota
metadata:
  name: ephematool-quota
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "baseline.labels" . | nindent 4 }}
spec:
  hard:
    requests.cpu: {{ $s.requests.cpu | quote }}
    requests.memory: {{ $s.requests.memory | quote }}
    limits.cpu: {{ $s.limits.cpu | quote }}
    limits.memory: {{ $s.limits.memory | quote }}
    pods: {{ $s.pods | quote }}
    count/services: {{ $s.services | quote }}
    persistentvolumeclaims: "0"
    services.nodeports: "0"
    services.loadbalancers: "0"
```
`environments/baseline/templates/limitrange.yaml`

```yaml
{{- /* Gives every container the preset's defaults so a pod without a resources block is
       admitted (the quota would otherwise reject it), and caps any one container at the whole
       preset. The chart's Deployment deliberately has no resources block. */ -}}
{{- $s := include "baseline.size" . | fromJson }}
apiVersion: v1
kind: LimitRange
metadata:
  name: ephematool-limits
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "baseline.labels" . | nindent 4 }}
spec:
  limits:
    - type: Container
      default:
        cpu: {{ $s.containerDefault.cpu | quote }}
        memory: {{ $s.containerDefault.memory | quote }}
      defaultRequest:
        cpu: {{ $s.containerDefaultRequest.cpu | quote }}
        memory: {{ $s.containerDefaultRequest.memory | quote }}
      max:
        cpu: {{ $s.limits.cpu | quote }}
        memory: {{ $s.limits.memory | quote }}
```
`environments/baseline/templates/networkpolicies.yaml`

```yaml
{{- /* Default deny in both directions, then four narrow allowances. kind's default CNI enforces
       NetworkPolicy, so this is real isolation even locally. */ -}}
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: default-deny-all
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "baseline.labels" . | nindent 4 }}
spec:
  podSelector: {}
  policyTypes: [Ingress, Egress]
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-dns-egress
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "baseline.labels" . | nindent 4 }}
spec:
  podSelector: {}
  policyTypes: [Egress]
  egress:
    - to:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: kube-system
          podSelector:
            matchLabels:
              k8s-app: kube-dns
      ports:
        - {protocol: UDP, port: 53}
        - {protocol: TCP, port: 53}
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-ingress-from-gateway
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "baseline.labels" . | nindent 4 }}
spec:
  podSelector: {}
  policyTypes: [Ingress]
  ingress:
    - from:
        - namespaceSelector:
            matchLabels:
              kubernetes.io/metadata.name: traefik
---
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: allow-otlp-egress
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "baseline.labels" . | nindent 4 }}
spec:
  podSelector: {}
  policyTypes: [Egress]
  egress:
    # The environments OTLP receiver (collector port 4320). Not "any port": the app may send
    # telemetry and nothing else leaves the namespace.
    - to:
        - ipBlock:
            cidr: 0.0.0.0/0
      ports:
        - {protocol: TCP, port: 4320}
```
`environments/baseline/templates/serviceaccounts.yaml`

```yaml
{{- /* The deployer account exists so the provisioner can mint a short-lived token for it. Neither
       account mounts a token into pods: the app never talks to the Kubernetes API. */ -}}
apiVersion: v1
kind: ServiceAccount
metadata:
  name: ephematool-deployer
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "baseline.labels" . | nindent 4 }}
automountServiceAccountToken: false
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: default
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "baseline.labels" . | nindent 4 }}
automountServiceAccountToken: false
```
`environments/baseline/templates/rolebinding.yaml`

```yaml
{{- /* Binds the cluster-wide deployer role to the deployer account in THIS namespace only. The
       provisioner may create this binding because of its "bind" permission on exactly that role. */ -}}
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: ephematool-deployer
  namespace: {{ .Release.Namespace }}
  labels:
    {{- include "baseline.labels" . | nindent 4 }}
subjects:
  - kind: ServiceAccount
    name: ephematool-deployer
    namespace: {{ .Release.Namespace }}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: ephematool-deployer
```
### 10.5 `environments/BUILD.bazel` and `environments/README.md`

`environments/BUILD.bazel`

```python
load("@bazel_lib//lib:write_source_files.bzl", "write_source_files")
load("@rules_shell//shell:sh_test.bzl", "sh_test")

# conventions.gen.env is generated from conventions/telemetry.json and committed. Shell,
# YAML and Helm consumers read the label/attribute keys from it instead of hand-writing them
# (the D-09 grep gate would reject a hand-written copy). //:check_gen fails when it is stale.
write_source_files(
    name = "gen",
    files = {"conventions.gen.env": "//internal/conventions:_gen/conventions.gen.env"},
    suggested_update_target = "//:gen",
    visibility = ["//:__subpackages__"],
)

exports_files(["conventions.gen.env"])

# Input-validation tests for provision.sh: no cluster needed. Each hostile input must exit 2
# before kubectl or helm runs. The render check inside needs a real helm and skips without one.
sh_test(
    name = "provision_test",
    size = "small",
    srcs = ["scripts/provision_test.sh"],
    data = [
        "conventions.gen.env",
        "scripts/provision.sh",
        "//templates:charts",
    ] + glob(["baseline/**"]),
)
```
`environments/README.md`

```markdown
# environments/

Everything the **private environments repository** needs, kept next to the platform code so the two
change together. It is published, together with `templates/`, to a PRIVATE repository named
`<org>/ephematool-environments`; GitHub does not run anything from this directory in the monorepo.

| Path | What it is |
|---|---|
| `.github/workflows/create-environment.yml` | the thin workflow the platform dispatches (12 string inputs) |
| `scripts/provision.sh` | all provisioning logic; also run by `bazel run //:kind_verify` |
| `scripts/provision_test.sh` | no-cluster tests: hostile inputs exit 2 before any cluster call |
| `baseline/` | platform-owned namespace guardrails: quota, limits, network policy, RBAC |
| `conventions.gen.env` | generated label and attribute keys (never hand-edit) |

The repository must also contain `templates/` (the chart directory the API embeds).

## What this trace shows, and what it does not

GitHub Actions emits no spans. The worker rebuilds `github.actions.run`, `.job` and `.step` spans from
the API timestamps **after** the run completes: durations at one-second resolution, no step
internals, and they appear when the run ends, not live.

## Hardening checklist (every item is required; the runner executes code on a developer machine)

- [ ] The repository is **private**.
- [ ] The workflow triggers on `workflow_dispatch` only.
- [ ] Forks are disabled; only admins can change `.github/workflows/**`.
- [ ] The runner is **ephemeral** (`--ephemeral`), labelled `ephematool-kind`, and runs as a host process.
- [ ] No repository secrets exist. `PROVISIONER_KUBECONFIG` lives in the runner's `.env` on the host.
- [ ] The provisioner kubeconfig is `chmod 600` and git-ignored (`deploy/.secrets/`).
```
```bash
bazel test //environments:provision_test
bazel run //:gazelle
```

### 10.6 Bring the cluster up and prove it

`kind_verify` needs the platform stack on the host (it asks the API who may enter, and checks that the app's telemetry reaches Tempo). Use `bazel run //:up`, **not** `up_fake`: the fake stack's Traefik uses the same port 8088 as kind's.

```bash
bazel run //:kind_tools          # kind and helm into .tools/bin (kubectl and Docker are yours)
bazel run //:up                  # API, collector, Tempo on the host
bazel run //:kind_up             # several minutes the first time: node image and Traefik image pull
bazel run //:kind_verify
```

`kind_verify` prints one `PASS` per check (ten in all) and stops at the first failure:

1. the sample app image is built and loaded into the cluster (no registry);
2. `provision.sh` deploys it exactly as the workflow would;
3. the namespace carries the five platform labels and Pod Security `baseline`;
4. **GRD-01:** a sixth replica is rejected with `exceeded quota`;
5. **GRD-04:** DNS works, the internet is unreachable, and another namespace cannot reach the Service;
6. the provisioner cannot create other namespaces or read `kube-system`; the deployer cannot change quota, network policy or RBAC; an Ingress for another host is rejected;
7. **ENV-09:** health answers `200` with `X-Robots-Tag: noindex`, everything else redirects to the dashboard authorize page;
8. **OBS-03:** Tempo holds a trace tagged with the environment id, owner and template;
9. cleanup (`KEEP=1 bazel run //:kind_verify` leaves the namespace for you to inspect).

```bash
bazel run //:kind_down           # when finished; also stops using port 8088
```

> **What is not covered here.** Registering a self-hosted runner and publishing `environments/` to the private repository, so that a click in the dashboard really runs the workflow on your machine, is done in Phase 2d (plan 02-17) together with the manual acceptance test. Until then the two halves are proven separately: the fake GitHub proves dispatch, polling and spans (Part A); `kind_verify` proves provisioning and the guardrails (this part).

---

# Part C · The compose preview gate

## 11. The same gate without a cluster (plan 02-15)

`bazel run //:up_fake` now brings up a real Traefik with the **same entrypoint middleware chain** as kind, in front of `traefik/whoami`, which echoes every request header it receives. That makes the access rules (ENV-09) testable with `curl`, and lets the worker's probe cross the gate's health exemption exactly as it does on kind.

> Both gateways must keep the same chain. If you change `deploy/traefik/dynamic.yaml` change `deploy/kind/traefik-middlewares.yaml` too, and re-run both checks.

### 11.1 `deploy/compose.preview.yaml` and `deploy/traefik/dynamic.yaml`

The images are the digests from `deploy/kind/versions.env`. The gateway binds `127.0.0.1:8088` (loopback), has the access log **off**, and `authResponseHeaders: [Cookie]` makes Traefik delete the request's `Cookie` header and replace it with what ForwardAuth returns, so the app never sees the preview or session cookie.

`deploy/compose.preview.yaml`

```yaml
# Overlay for dev/e2e preview gating with Traefik. NOT for real deployments.
#
# A real Traefik gateway with the SAME entrypoint middleware chain the kind cluster uses
# (headers + ForwardAuth), in front of traefik/whoami as an echo upstream. It makes the
# preview gate (ENV-09) testable with `bazel run //:up_fake`, no cluster needed.
#
# Both gateways, this one and deploy/kind/traefik-values.yaml + traefik-middlewares.yaml, must keep
# the same chain. If you change one, change the other and re-run both gate checks.
#
# Images are digest-pinned in deploy/kind/versions.env; the values below must match it
# (scripts/verify.d/conventions.sh does not check images, so the compose-config test in
# the Phase 2c doc does).
services:
  traefik:
    image: traefik:v3.7.14@sha256:575fa15b135078fe5e50aa847987d96dbddd7b093c172429618404df73f3fa7c
    command:
      - --entrypoints.web.address=:8088
      # The chain is on the ENTRYPOINT, not on a router: a router added later cannot forget it.
      - --entrypoints.web.http.middlewares=preview-guard@file
      - --providers.file.filename=/etc/traefik/dynamic.yaml
      - --api.dashboard=false
      # A ticket travels in a query string: never write request lines to a log.
      - --accesslog=false
      - --ping=true
    volumes:
      - ./traefik/dynamic.yaml:/etc/traefik/dynamic.yaml:ro
    ports:
      # Loopback only. kind's gateway uses the same port, so the two cannot run together.
      - "127.0.0.1:${PREVIEW_PORT:-8088}:8088"
    depends_on:
      - api
      - whoami

  # Echoes every request header it receives, which is how the gate check proves the app
  # never sees the preview or session cookie. No published ports: only Traefik reaches it.
  whoami:
    image: traefik/whoami:v1.12.0@sha256:c4717a8d1f0134a7444e24f881160e033991f23027c6c5a9a3f8fd22e70d1d44
```
`deploy/traefik/dynamic.yaml`

```yaml
# Dynamic configuration for the compose preview gateway. Keep it identical in effect to
# deploy/kind/traefik-middlewares.yaml (D-20, D-22, D-25).
http:
  middlewares:
    preview-headers:
      headers:
        customResponseHeaders:
          X-Robots-Tag: "noindex, nofollow" # previews are never indexed
          Referrer-Policy: "no-referrer"    # a ticket URL must not leak through Referer
    preview-forward-auth:
      forwardAuth:
        address: "http://api:8081/internal/preview/auth"
        preserveLocationHeader: true # the 302 to the dashboard keeps its Location
        maxResponseBodySize: 4096
        authRequestHeaders: [Cookie] # the auth handler reads the preview cookie, nothing else
        # Traefik DELETES the request's Cookie header and copies the auth response's, so the
        # app sees only what the handler returns (the cookies minus the preview/session ones).
        authResponseHeaders: [Cookie]
    preview-guard:
      chain:
        middlewares: [preview-headers, preview-forward-auth]

  routers:
    preview:
      # One catch-all router for every preview host. The chain above is on the entrypoint.
      rule: "HostRegexp(`^pv-[a-z0-9]{10}\\.preview\\.localhost$`)"
      service: whoami
  services:
    whoami:
      loadBalancer:
        servers:
          - url: "http://whoami:80"
```
### 11.2 `tools/stack.sh` (replace)

`COMPOSE_FAKE` gains the preview overlay, so `up_fake`, `down` and `clean` all include it.

`tools/stack.sh`

```bash
#!/usr/bin/env bash
# Local compose stack orchestration behind the `bazel run` targets in //:BUILD.bazel:
#
#   bazel run //:up                 docker compose up --build --wait
#   bazel run //:up_fake            same, plus the fake-GitHub e2e overlay and the Traefik preview gate
#   bazel run //:down               stop the stack (both overlays)
#   bazel run //:clean              stop the stack and delete its volumes
#   bazel run //:logs               follow the stack logs
#   bazel run //:verify_telemetry   scripts/verify-telemetry.sh all
#   bazel run //:verify_telemetry -- trace   (one subcommand)
#
# Runs in the workspace root (BUILD_WORKSPACE_DIRECTORY, set by `bazel run`) and stamps
# GIT_COMMIT and BUILT_AT into the compose environment for the image build args.
# Never call bazel from here: a nested bazel inside `bazel run` blocks on the output base lock.
set -euo pipefail

if [ -n "${BUILD_WORKSPACE_DIRECTORY:-}" ]; then
  ROOT="$BUILD_WORKSPACE_DIRECTORY"
else
  ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fi
cd "$ROOT"

GIT_COMMIT="${GIT_COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)}"
BUILT_AT="${BUILT_AT:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"
export GIT_COMMIT BUILT_AT

# Dev-only secrets live in deploy/.secrets (git-ignored). Existing files are never
# overwritten and key material is never printed.
ensure_secrets() {
  local dir=deploy/.secrets
  mkdir -p "$dir"
  chmod 700 "$dir"
  if [ ! -f "$dir/preview-token.key" ]; then
    (umask 077 && openssl rand -hex 32 >"$dir/preview-token.key")
  fi
  if [ ! -f "$dir/github-app-dev.pem" ]; then
    (umask 077 && openssl genrsa -out "$dir/github-app-dev.pem" 2048 2>/dev/null)
  fi
}


COMPOSE=(docker compose -f deploy/compose.yaml)
# The fake overlay adds fakegithub and the worker; the preview overlay adds Traefik + whoami,
# the same gate the kind cluster runs, so the preview gate is testable without a cluster.
COMPOSE_FAKE=("${COMPOSE[@]}" -f deploy/compose.e2e.yaml -f deploy/compose.preview.yaml)

if [ $# -lt 1 ]; then
  echo "usage: $0 <up|up-fake|down|clean|logs|verify-telemetry> [args...]" >&2
  exit 2
fi

cmd="$1"
shift

case "$cmd" in
  up)
    ensure_secrets
    exec "${COMPOSE[@]}" up --build --wait "$@"
    ;;
  up-fake)
    ensure_secrets
    exec "${COMPOSE_FAKE[@]}" up --build --wait "$@"
    ;;
  down)
    "${COMPOSE_FAKE[@]}" down "$@" || true
    exec "${COMPOSE[@]}" down "$@"
    ;;
  clean)
    "${COMPOSE_FAKE[@]}" down -v "$@" || true
    exec "${COMPOSE[@]}" down -v "$@"
    ;;
  logs)
    exec "${COMPOSE[@]}" logs -f "$@"
    ;;
  verify-telemetry)
    if [ $# -eq 0 ]; then
      set -- all
    fi
    exec scripts/verify-telemetry.sh "$@"
    ;;
  *)
    echo "unknown command: $cmd" >&2
    exit 2
    ;;
esac
```
### 11.3 `deploy/compose.e2e.yaml` (edit: the worker probes through the gate)

In the `worker:` block, replace the stand-in gateway and add the dependency:

```yaml
      # The probe goes through the real Traefik gate (deploy/compose.preview.yaml), so it crosses
      # the health exemption exactly as it does on kind. Go tests still use the fake's
      # /__ephematool/healthz endpoint instead.
      - PREVIEW_GATEWAY_ADDR=traefik:8088
      - ENV_PROVISION_DEADLINE=2m
    depends_on:
      fakegithub:
        condition: service_started
      traefik:
        condition: service_started
```

### 11.4 `deploy/nginx/default.conf` (replace): anti-framing headers and the `Host` fix

Two changes. The dashboard now sends `X-Frame-Options: DENY`, `frame-ancestors 'none'`, `nosniff` and `Referrer-Policy: same-origin` (no `script-src` or `style-src`: a strict policy would break Radix's inline styles). And `/api/` forwards `Host $http_host` instead of `$host`: `$host` drops the port, so Go's cross-origin protection saw `Origin: http://localhost:8080` against `Host: localhost` and answered `403` to every state-changing request from `:8080`.

`deploy/nginx/default.conf`

```nginx
# Same-origin front door for the dashboard SPA, the API, and browser OTLP ingest.
# Included inside the http context by the official nginx image.

limit_req_zone $binary_remote_addr zone=otlp:10m rate=20r/s;

# A named upstream is resolved at startup. A bare host in a proxy_pass that uses a
# variable ($1 below) would need a runtime `resolver` and fail with 502 otherwise.
upstream otel_collector_http {
    server otel-collector:4318;
}

server {
    listen 8080;
    server_name _;

    root /usr/share/nginx/html;
    index index.html;

    # The console must not be framed (clickjacking) or sniffed. No script-src or style-src:
    # a strict policy would break Radix's inline styles. add_header is inherited by a location
    # only if that location sets none of its own; none of them does, so no repeats are needed.
    add_header X-Frame-Options DENY always;
    add_header Content-Security-Policy "frame-ancestors 'none'; base-uri 'self'; object-src 'none'" always;
    add_header X-Content-Type-Options nosniff always;
    add_header Referrer-Policy same-origin always;

    location = /healthz {
        default_type text/plain;
        return 200 "ok";
    }

    # API: Origin and Sec-Fetch-Site pass through unchanged (proxy_pass forwards
    # all client headers by default) so Go's CrossOriginProtection works.
    location /api/ {
        proxy_pass http://api:8081;
        # $http_host keeps the port (Go's CrossOriginProtection compares Origin with Host;
        # $host drops the port, so every state-changing request from :8080 would get a 403).
        proxy_set_header Host $http_host;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    }

    # Browser OTLP ingest: POST-only allowlist of three signal paths, size cap,
    # rate limit, and credentials stripped before reaching the Collector.
    location ~ ^/otlp/(v1/(traces|logs|metrics))$ {
        limit_except POST {
            deny all;
        }
        client_max_body_size 1m;
        limit_req zone=otlp burst=20 nodelay;
        proxy_set_header Cookie "";
        proxy_set_header Authorization "";
        proxy_pass http://otel_collector_http/$1;
    }

    # Any other /otlp path is not served.
    location /otlp/ {
        return 404;
    }

    location / {
        try_files $uri /index.html;
    }
}
```
### 11.5 `scripts/verify.d/preview-gate.sh` (new)

Seven checks through the real gateway, using `-H "Host: <id>.preview.localhost:8088"` against `127.0.0.1:8088`: (1) health `200` and `X-Robots-Tag: noindex`; (2) `/` without a cookie redirects to the authorize page; (3) a forged ticket gets `403`; (4) a real ticket sets a host-only `HttpOnly` cookie and redirects to the requested path; (5) the same ticket again gets `403`; (6) the upstream sees `app=1` and **neither** `ephematool_preview` **nor** `ephematool_session`; (7) the cookie does not open a different environment.

`scripts/verify.d/preview-gate.sh`

```bash
#!/usr/bin/env bash
# ENV-09: the preview gate, exercised through a REAL Traefik with the production middleware
# chain (headers + ForwardAuth) in front of an echo upstream. Runs on: bazel run //:up_fake
#
# Checks: health is open and tagged noindex; everything else redirects to the dashboard; a forged
# ticket and a replayed ticket are refused; a valid ticket sets a host-only HttpOnly cookie; the
# upstream never sees the preview or session cookie; a cookie does not open another environment.
set -euo pipefail
. "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/lib/verify-lib.sh"

PORT="${PREVIEW_PORT:-8088}"
GW="http://127.0.0.1:$PORT"
T="$VERIFY_TMP/preview-gate"
mkdir -p "$T"

ensure_session octo-admin

# ---- setup: create an environment and wait for it (the same loop as env-trace.sh)
code="$(api_post /api/environments \
  '{"repo":"ephematool-e2e/demo-app","refType":"branch","ref":"main","template":"web-app","size":"S","values":{}}')"
[ "$code" = "202" ] || fail "POST /api/environments returned HTTP $code: $(cat "$VERIFY_TMP/last-body")"
id="$(jq -r '.id' "$VERIFY_TMP/last-body")"
[ -n "$id" ] && [ "$id" != "null" ] || fail "create returned no environment id"
i=0
status=""
while [ "$i" -lt 90 ]; do
  api_get "/api/environments/$id" > "$T/env.json" || fail "GET /api/environments/$id failed"
  status="$(jq -r '.status' "$T/env.json")"
  [ "$status" = "running" ] && break
  [ "$status" = "failed" ] && fail "environment failed: $(jq -r '.failureReason' "$T/env.json")"
  sleep 1
  i=$((i + 1))
done
[ "$status" = "running" ] || fail "environment $id is still '$status' after 90s"
pass "environment $id is running"

HOST="$id.preview.localhost:$PORT"
OTHER="pv-zzzzzzzzzz.preview.localhost:$PORT" # a well-formed host of a DIFFERENT environment

# gw HOST PATH [COOKIE]: one request through the gateway, never following redirects.
# Prints the status code; headers land in $T/h and the body in $T/b.
gw() {
  local host="$1" path="$2" cookie="${3:-}" args=()
  [ -n "$cookie" ] && args=(-H "Cookie: $cookie")
  # ${args[@]+...}: an empty array must not trip `set -u` on bash 3.2 (macOS).
  curl -sS --max-time 10 -o "$T/b" -D "$T/h" -w '%{http_code}' -H "Host: $host" \
    ${args[@]+"${args[@]}"} "$GW$path"
}
hdr() { tr -d '\r' < "$T/h" | grep -i "^$1:" | head -1 | sed -E 's/^[^:]+: *//'; }

# ---- 1. health is exempt, tagged noindex, and needs no credential
code="$(gw "$HOST" /__ephematool/healthz)"
[ "$code" = "200" ] || fail "(1) GET /__ephematool/healthz returned $code, want 200 (is Traefik up? docker compose ps)"
hdr x-robots-tag | grep -qi noindex || fail "(1) the health response lacks X-Robots-Tag: noindex"
pass "(1) health answers 200 without credentials and carries X-Robots-Tag: noindex"

# ---- 2. anything else, without a cookie, goes to the dashboard
code="$(gw "$HOST" /)"
[ "$code" = "302" ] || fail "(2) GET / without a cookie returned $code, want 302"
hdr location | grep -q "^http://localhost:8080/preview/authorize?env=$id" \
  || fail "(2) redirect Location is '$(hdr location)', want http://localhost:8080/preview/authorize?env=$id..."
pass "(2) GET / without a cookie redirects to the dashboard authorize page"

# ---- 3. a forged ticket is refused
code="$(gw "$HOST" '/__ephematool/callback?ticket=forged')"
[ "$code" = "403" ] || fail "(3) a forged ticket returned $code, want 403"
pass "(3) a forged ticket is refused (403)"

# ---- 4. a real ticket sets a host-only, HttpOnly cookie and lands on the requested path
code="$(api_post /api/preview/ticket "{\"envId\":\"$id\",\"return\":\"http://$HOST/hello\"}")"
[ "$code" = "200" ] || fail "(4) POST /api/preview/ticket returned $code: $(cat "$VERIFY_TMP/last-body")"
ticket_url="$(jq -r '.url' "$VERIFY_TMP/last-body")"
case "$ticket_url" in "http://$HOST/__ephematool/callback?ticket="*) ;; *) fail "(4) unexpected ticket URL shape" ;; esac
callback_path="${ticket_url#http://$HOST}"
code="$(gw "$HOST" "$callback_path")"
[ "$code" = "302" ] || fail "(4) the ticket callback returned $code, want 302"
[ "$(hdr location)" = "/hello" ] || fail "(4) callback Location is '$(hdr location)', want /hello"
setc="$(tr -d '\r' < "$T/h" | grep -i '^set-cookie: ephematool_preview=' | head -1 || true)"
[ -n "$setc" ] || fail "(4) the callback set no ephematool_preview cookie"
grep -qi 'httponly' <<< "$setc" || fail "(4) the preview cookie is not HttpOnly"
! grep -qi 'domain=' <<< "$setc" || fail "(4) the preview cookie has a Domain attribute (must be host-only)"
cookie_value="$(sed -E 's/^[^=]+=([^;]*);.*$/\1/' <<< "$setc")"
[ -n "$cookie_value" ] || fail "(4) could not read the preview cookie value"
pass "(4) a valid ticket sets a host-only HttpOnly cookie and redirects to /hello"

# ---- 5. the same ticket again is refused (single use)
code="$(gw "$HOST" "$callback_path")"
[ "$code" = "403" ] || fail "(5) replaying the ticket returned $code, want 403"
pass "(5) a replayed ticket is refused (403)"

# ---- 6. the upstream never sees the preview cookie or the dashboard session cookie
# Real browsers never send the host-only session cookie to a preview host (the Playwright spec
# asserts that); here a client sends one anyway, and the gate must still strip it.
code="$(gw "$HOST" /hello "ephematool_preview=$cookie_value; app=1; ephematool_session=leak")"
[ "$code" = "200" ] || fail "(6) GET /hello with a valid cookie returned $code, want 200"
hdr x-robots-tag | grep -qi noindex || fail "(6) the preview response lacks X-Robots-Tag: noindex"
echoed="$(tr -d '\r' < "$T/b" | grep -i '^cookie:' || true)"
grep -q 'app=1' <<< "$echoed" || fail "(6) the app cookie did not reach the upstream (echo: '${echoed:-none}')"
! grep -qi 'ephematool_preview' <<< "$echoed" || fail "(6) the upstream received the preview cookie"
! grep -qi 'ephematool_session' <<< "$echoed" || fail "(6) the upstream received the dashboard session cookie"
pass "(6) the upstream receives app=1 and neither ephematool_preview nor ephematool_session"

# ---- 7. a cookie is bound to its environment
code="$(gw "$OTHER" /hello "ephematool_preview=$cookie_value")"
[ "$code" = "302" ] || fail "(7) the cookie against another environment's host returned $code, want 302"
pass "(7) the cookie does not open a different environment"
```
### 11.6 `scripts/verify.d/redaction.sh` (edit: preview secrets)

Two additions. First, mint a real ticket before the traces are collected, so the scan has something to find (a scan for a string that never existed proves nothing). Insert after the loop over `/api/status` and `/api/me`, before "A fresh login":

`scripts/verify.d/redaction.sh`

```bash
# A preview ticket minted here, so the scan below sees a real value (a scan for a string that
# never existed proves nothing). The environment need not be running for a ticket to be minted.
code="$(api_post /api/environments \
  '{"repo":"ephematool-e2e/demo-app","refType":"branch","ref":"main","template":"web-app","size":"S","values":{}}')"
[ "$code" = "202" ] || fail "creating an environment to mint a ticket returned HTTP $code"
env_id="$(jq -r '.id' "$VERIFY_TMP/last-body")"
code="$(api_post /api/preview/ticket "{\"envId\":\"$env_id\",\"return\":\"http://$env_id.preview.localhost:${PREVIEW_PORT:-8088}/\"}")"
[ "$code" = "200" ] || fail "minting a preview ticket returned HTTP $code: $(cat "$VERIFY_TMP/last-body")"
ticket_value="$(jq -r '.url | sub("^.*ticket="; "")' "$VERIFY_TMP/last-body")"
[ -n "$ticket_value" ] && [ "$ticket_value" != "null" ] || fail "could not read the minted ticket value"
# The mint request's own trace is scanned too: that is where a ticket would leak first.
tid="$(cat "$VERIFY_TMP/last-trace-id")"
[ -n "$tid" ] || fail "the ticket mint returned no X-Trace-Id"
echo "$tid" >> "$OUT/trace-ids"
```
Second, add these scans after the existing `authorization header attribute` scan:

`scripts/verify.d/redaction.sh`

```bash
# Phase 2 secrets: tickets, preview cookies (the __Host- form contains the same string), keys.
scan "ticket query value" "ticket="
scan "minted ticket value" "$ticket_value"
scan "preview cookie pair" "ephematool_preview="
scan "RSA private key" "BEGIN RSA PRIVATE KEY"
scan "private key" "BEGIN PRIVATE KEY"
```
### 11.7 Run the gate

```bash
docker compose -f deploy/compose.yaml -f deploy/compose.e2e.yaml -f deploy/compose.preview.yaml config -q   # exits 0
bazel run //:up_fake
curl -sI http://localhost:8080/ | grep -i -e x-frame-options -e content-security-policy     # DENY, frame-ancestors 'none'
bazel run //:verify_telemetry -- preview-gate
bazel run //:verify_telemetry -- redaction
bazel run //:verify_telemetry -- env-trace                                                  # the probe now crosses Traefik
bazel run //:verify_telemetry                                                               # the whole gate, in order
bazel run //:down
```

---

## 12. Verification status of this document's code

| Part | Status |
|---|---|
| `internal/envs` worker logic (`worker.go`, `dispatch.go`, `poll.go`, `probe.go`, `sweep.go`) and `worker_test.go` (25 cases) | Compiled and run with `go test -race` against **stand-ins** for OpenTelemetry, the GitHub App client types, the queue and the preview auth package. The state machine, the never-re-POST rule, release-after-rate-limit, the poll delay bounds and the sweeper are proven; the real adapters are not. |
| `spans.go`, `spans_test.go`, `register.go`, `pgworkerstore.go`, `worker_integration_test.go` | Syntax-checked with `gofmt` only (OpenTelemetry SDK, asynq, pgx, sqlc types). The sqlc field names are the uncertain part: see the note in section 3.4. |
| `internal/queue` additions and test, `cmd/worker/main.go` | `gofmt` only (asynq, miniredis, pgx). |
| `internal/previewauth` cookie stripping | Compiled and run with `go test -race` with a stand-in for `golang-jwt/jwt/v5` and a one-constant stand-in for `auth.CookieName`. |
| `deploy/compose.e2e.yaml`, `compose.preview.yaml`, the three-file stack | Validated with `docker compose config`. Traefik itself was **not run** (no Docker daemon in the authoring environment). |
| `deploy/traefik/dynamic.yaml`, `deploy/kind/*.yaml`, workflow | Parsed as YAML. The Traefik chart values (`image.tag` with a digest, `ports.web.http.middlewares`, `logs.access.enabled`, `ingressRoute.dashboard.enabled`, `providers.*`) and the `HostRegexp` matching behaviour with a port in the `Host` header follow the research spike and were **not verified against the chart**: run `helm template` on the pinned chart with `traefik-values.yaml` and compare the rendered arguments with the middleware section of section 9 before relying on them. |
| `environments/baseline/` and `templates/web-app/` | Rendered with a small `text/template` stand-in for Helm: nine baseline objects and the quota values come out right. **Helm itself was never run.** |
| `provision.sh`, `provision_test.sh` | `bash -n` and `shellcheck`-clean. The input-validation cases (22 reject cases, one accept case) ran against stubs; the dry-run render case ran only against a Helm shim. |
| `tools/kind.sh`, `preview-gate.sh`, `redaction.sh`, `env-trace.sh`, `stack.sh` | `bash -n` and `shellcheck`-clean; the `jq` extraction and the cookie and ticket parsing were run on sample data. **No cluster, no gateway and no stack was run**, so every `PASS` line promised above is unobserved. |
| `scripts/verify.d/conventions.sh` scan | Run over the staged tree: no hand-written prefixes. |
| Collector validation, `kind`, Traefik, all integration tests | **Not run.** |

---

## 13. Acceptance checklist

- [ ] `bazel run //:gen` produced `ReleaseDispatch`; the field names used by `pgworkerstore.go` exist in the generated `Environment` struct.
- [ ] `bazel test //internal/envs/... //internal/queue/... //internal/previewauth/...` passes.
- [ ] `bazel test --config=integration //:test_integration` passes, including the happy path (exactly 1 recorded dispatch carrying 12 inputs) and the ambiguous-dispatch case (still exactly 1).
- [ ] `grep -n 'EnqueueEnv' internal/envs/*.go | grep -v _test` shows only interface and call sites; `grep -rn 'TaskID' internal/queue/*.go | grep -v _test` prints nothing.
- [ ] `bazel run //:verify_telemetry -- env-trace` prints four `PASS` lines on `up_fake`, and the trace in Grafana shows the API, worker, `github.actions.*` and `env.probe` spans.
- [ ] `bazel test //environments:provision_test` passes.
- [ ] `helm template baseline environments/baseline -n pv-aaaaaaaaaa -f templates/sizes.yaml --set-string size=S …` renders nine objects (`provision.sh` with `PROVISION_DRY_RUN=1` does this for you), and the quota's `limits.cpu` is `500m` for size `S`.
- [ ] `grep -n 'resourceNames' deploy/kind/bootstrap-rbac.yaml` shows `ephematool-deployer` twice (`bind` and `serviceaccounts/token`).
- [ ] `bazel run //:kind_verify` ends with `kind_verify: 10 checks passed`.
- [ ] `docker compose -f deploy/compose.yaml -f deploy/compose.e2e.yaml -f deploy/compose.preview.yaml config -q` exits 0 and `grep -n 'accesslog=false' deploy/compose.preview.yaml` matches.
- [ ] `bazel run //:verify_telemetry -- preview-gate` prints seven `PASS` lines; `-- redaction` passes with the new scans.
- [ ] `curl -sI http://localhost:8080/ | grep -i x-frame-options` prints `DENY`.
- [ ] `bash scripts/verify.d/conventions.sh` passes (the gate now scans `deploy`, `environments`, `templates` and `tools`).
- [ ] `grep -rn 'spike-bb\|ingress-nginx' tools deploy environments` prints nothing.

---

## 14. Common failures

| Symptom | Likely cause | Fix |
|---|---|---|
| a created environment stays `requested` forever | the worker is not running, or has no GitHub App (it then fails with "GitHub App not configured") | `bazel run //:logs`; on `up_fake` the worker block in `compose.e2e.yaml` must be present |
| `failed: Could not confirm the workflow dispatch` | the fake (or GitHub) answered `204` without a run id and the correlation search found nothing in 2 minutes | check the workflow's `run-name` carries the correlation id; the worker never re-POSTs by design |
| two environments dispatched in the same second hit a rate limit | `ENV_DISPATCH_SPACING` too small, or the `dispatch` queue has concurrency above 1 | keep `NewDispatchServer` at concurrency 1 and spacing at 1s or more |
| poll tasks multiply after a restart | a continuation was enqueued with a `TaskID` or without touching the row | continuations never set `TaskID`; every pass calls `Touch` (see `staleAfter`) |
| `env-trace.sh` fails on `env.probe` only | the probe span is emitted after the batch flush | the script already polls for 40 s; check the worker log for `probe` errors and that Tempo is up |
| `env-trace.sh` fails with `403` on create | nginx forwarded `Host $host` (no port) so the cross-origin check failed | use the section 11.4 `default.conf` (`Host $http_host`) |
| `pgworkerstore.go` does not compile on `RunHtmlUrl` | sqlc named the field differently | rename to the generated name (section 3.4) |
| `kind_up`: "127.0.0.1:8088 is in use" | the compose preview gateway from `up_fake` is running | `bazel run //:down`, then `kind_up` |
| `kind_up` times out waiting for Traefik | first image pull on a fresh node is slow, or the chart values do not match the chart | re-run (it is idempotent); then `kubectl -n traefik get pods` and `helm template` the chart with the values |
| `kind_up`: "the Traefik chart does not match TRAEFIK_CHART_DIGEST" | the chart was republished or the digest is stale | do not bypass: re-derive the digest from the chart you reviewed and update `versions.env` |
| `kind_verify` step 7 hangs on health | the ForwardAuth address is not reachable from the node (`host.docker.internal` on Linux) | set `KIND_API_ADDR` (for example `172.17.0.1:8081`) and `kind_up` again |
| `kind_verify` step 5: the internet is reachable | the CNI does not enforce NetworkPolicy | kind's default CNI does; a custom `cluster.yaml` must not disable it |
| `Forbidden` creating the namespace in `provision.sh` | the namespace name or labels do not satisfy the admission policy | the name must be `pv-<8-16 chars>` and carry the managed label; both come from the generated conventions file |
| `provision.sh` exits `2` | an input failed its regex, by design | read the message: it names the variable and the pattern |
| preview-gate check 6 shows `ephematool_session` upstream | `forwardauth.go` lacks the session-cookie strip, or Traefik's `authResponseHeaders` lacks `Cookie` | section 4.4 of 2b and section 11.1 |
| preview-gate check 4 fails on the ticket URL shape | the API's `PREVIEW_PORT` differs from the published port | keep both at `8088` on the fake stack |
| `redaction.sh` says "scan sanity check failed" | no trace content was downloaded | the stack is not up, or Tempo has not received the batch yet; re-run |
