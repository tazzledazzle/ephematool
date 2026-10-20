# Phase 0 · Bootstrap: toolchain, Bazel workspace, Go module

**Objective:** an empty but buildable monorepo: Bazel (bzlmod) + Go module + Gazelle + the `go_test` wrapper, with a dev-secrets-safe `.gitignore`. Nothing runs yet; every later phase adds packages to this skeleton.

**Design references:** `.planning/PROJECT.md` (constraints), `.planning/research/STACK.md`.
**Prerequisite:** a machine with Docker (with the Compose plugin), Git, `openssl`, and [bazelisk](https://github.com/bazelbuild/bazelisk) on `PATH` as `bazel`. You do **not** install Go or Node yourself for Bazel builds; Bazel downloads both. Install Go 1.26 only if you want to run `go` commands directly (`go mod tidy`, `go vet`).
**Produces (later phases rely on these exact names):**

| Artifact | Used later for |
|---|---|
| `//:gazelle` | regenerating every Go `BUILD.bazel` after you add files or imports |
| `//tools/bzl:go.bzl` (`go_test`) | every Go test target (adds `-test.short`, dropped under `--config=integration`) |
| `--config=integration` | Docker-backed (testcontainers) Go tests |
| module path `github.com/ephematool/ephematool` | every Go import |

---

## 1. Directory structure after this phase

```
ephematool/
├── .bazelignore
├── .bazelrc
├── .bazelversion
├── .dockerignore
├── .gitignore
├── BUILD.bazel
├── MODULE.bazel
├── go.mod
└── tools/
    └── bzl/
        ├── BUILD.bazel
        └── go.bzl
```

```bash
mkdir -p ephematool/tools/bzl && cd ephematool
git init
```

---

## 2. Check the toolchain

```bash
bazel --version          # bazelisk will download the version pinned in .bazelversion (8.2.1)
docker compose version   # needs the Compose v2 plugin
openssl version          # used later to generate dev-only secrets
git --version
```

Expected: all four print a version. If `docker compose` is "not a docker command", install Docker Desktop or the `docker-compose-plugin` package.

---

## 3. Bazel and Go module files

### 3.1 `.bazelversion`

```bash
printf '8.2.1\n' > .bazelversion
```

### 3.2 `.bazelrc`

`.bazelrc`

```text
# ---------------------------------------------------------------- defaults
common --enable_bzlmod
common --enable_workspace=false
# Aspect rules ship opt-out usage telemetry; opt out.
common --repo_env=DO_NOT_TRACK=1

build --incompatible_strict_action_env
# Go builds with cgo off, like the Dockerfile (CGO_ENABLED=0). This also makes sqlc use
# its pure-Go/wasm Postgres parser instead of compiling libpg_query from C.
common --@io_bazel_rules_go//go/config:pure
build --nolegacy_external_runfiles
build --verbose_failures
# Targets tagged `manual` (e2e, gazelle) are skipped by `//...` and only run when named.
test --test_output=errors

# Go tests get `-test.short` by default (like the old `go test ./... -short`); see
# tools/bzl/go.bzl. --config=integration drops it (like `go test -tags integration`).

# ---------------------------------------------------------------- --config=integration
# Equivalent of `go test -tags integration ./...`: builds Go with the `integration`
# build tag and runs tests outside the sandbox with the Docker socket reachable
# (testcontainers). Entry point: bazel test --config=integration //:test_integration
test:integration --@io_bazel_rules_go//go/config:tags=integration
test:integration --strategy=TestRunner=local
test:integration --test_env=DOCKER_HOST
test:integration --test_env=DOCKER_CONTEXT
test:integration --test_env=HOME
test:integration --test_env=TESTCONTAINERS_RYUK_DISABLED
test:integration --test_env=TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE
test:integration --test_env=TESTCONTAINERS_HOST_OVERRIDE
test:integration --test_timeout=900
test:integration --define=integration=true

# ---------------------------------------------------------------- --config=ci
build:ci --noshow_progress
build:ci --show_timestamps
build:ci --announce_rc
build:ci --color=no
build:ci --curses=no
build:ci --keep_going
test:ci --test_output=errors

# Machine-local overrides (git-ignored).
try-import %workspace%/user.bazelrc
```
Why each block matters:

- `--enable_workspace=false` makes Bazel use **only** `MODULE.bazel` (bzlmod); there is no `WORKSPACE`.
- `--@io_bazel_rules_go//go/config:pure` builds Go with cgo off, matching the container build (`CGO_ENABLED=0`). It also lets `sqlc` use its pure-Go parser instead of compiling C.
- `--config=integration` is how tests that need Docker (Postgres/Redis via testcontainers) are run. They need the host Docker socket, so they run **outside** the Bazel sandbox (`--strategy=TestRunner=local`).

### 3.3 `.bazelignore`

`.bazelignore`

```text
web/node_modules
web/dist
web/test-results
web/playwright-report
```
### 3.4 `go.mod`

This is the dependency set for the whole project (Phases 1 to 7). Copy it, then run `go mod tidy` once Phase 1 code exists so unused entries are pruned.

`go.mod`

```go
module github.com/ephematool/ephematool

go 1.26.0

require (
	github.com/alexedwards/scs/goredisstore v0.0.0-20251002162104-209de6e426de
	github.com/alexedwards/scs/v2 v2.9.0
	github.com/alicebob/miniredis/v2 v2.40.0
	github.com/caarlos0/env/v11 v11.4.1
	github.com/danielgtaylor/huma/v2 v2.39.1
	github.com/exaring/otelpgx v0.13.0
	github.com/google/go-github/v92 v92.0.0
	github.com/hibiken/asynq v0.26.0
	github.com/jackc/pgx/v5 v5.11.0
	github.com/pressly/goose/v3 v3.29.0
	github.com/redis/go-redis/extra/redisotel/v9 v9.23.0
	github.com/redis/go-redis/v9 v9.23.0
	github.com/stretchr/testify v1.12.1
	github.com/testcontainers/testcontainers-go/modules/postgres v0.44.0
	github.com/testcontainers/testcontainers-go/modules/redis v0.44.0
	go.opentelemetry.io/contrib/bridges/otelslog v0.21.0
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.72.0
	go.opentelemetry.io/contrib/instrumentation/runtime v0.72.0
	go.opentelemetry.io/otel v1.47.0
	go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp v0.23.0
	go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp v1.47.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp v1.47.0
	go.opentelemetry.io/otel/log v1.47.0
	go.opentelemetry.io/otel/metric v1.47.0
	go.opentelemetry.io/otel/sdk v1.47.0
	go.opentelemetry.io/otel/sdk/log v1.47.0
	go.opentelemetry.io/otel/sdk/metric v1.47.0
	go.opentelemetry.io/otel/trace v1.47.0
	golang.org/x/oauth2 v0.37.0
)

require (
	cel.dev/expr v0.25.2 // indirect
	dario.cat/mergo v1.0.2 // indirect
	filippo.io/edwards25519 v1.2.0 // indirect
	github.com/Azure/go-ansiterm v0.0.0-20261006220739-8c912ac31dd4 // indirect
	github.com/Microsoft/go-winio v0.6.3 // indirect
	github.com/antlr4-go/antlr/v4 v4.13.1 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/containerd/errdefs v1.0.0 // indirect
	github.com/containerd/errdefs/pkg v0.3.0 // indirect
	github.com/containerd/log v0.1.0 // indirect
	github.com/containerd/platforms v0.2.1 // indirect
	github.com/coreos/go-semver v0.3.1 // indirect
	github.com/cpuguy83/dockercfg v0.3.2 // indirect
	github.com/cubicdaiya/gonp v1.0.4 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/docker/go-connections v0.8.2 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/ebitengine/purego v0.10.1 // indirect
	github.com/fatih/structtag v1.2.0 // indirect
	github.com/felixge/httpsnoop v1.1.0 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-ole/go-ole v1.3.0 // indirect
	github.com/go-sql-driver/mysql v1.10.1 // indirect
	github.com/google/cel-go v0.28.0 // indirect
	github.com/google/go-querystring v1.2.0 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.30.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/jackc/puddle/v2 v2.2.3 // indirect
	github.com/jinzhu/inflection v1.0.0 // indirect
	github.com/klauspost/compress v1.20.1 // indirect
	github.com/lufia/plan9stats v0.0.0-20260330125221-c963978e514e // indirect
	github.com/magiconair/properties v1.8.10 // indirect
	github.com/mdelapenya/tlscert v0.2.0 // indirect
	github.com/mfridman/interpolate v0.0.2 // indirect
	github.com/moby/docker-image-spec v1.3.1 // indirect
	github.com/moby/go-archive v0.2.0 // indirect
	github.com/moby/moby/api v1.56.1 // indirect
	github.com/moby/moby/client v0.6.2 // indirect
	github.com/moby/patternmatcher v0.6.1 // indirect
	github.com/moby/sys/sequential v0.7.0 // indirect
	github.com/moby/sys/user v0.4.0 // indirect
	github.com/moby/sys/userns v0.1.0 // indirect
	github.com/moby/term v0.5.2 // indirect
	github.com/ncruces/go-sqlite3 v0.32.0 // indirect
	github.com/ncruces/julianday v1.0.0 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.1 // indirect
	github.com/pganalyze/pg_query_go/v6 v6.2.2 // indirect
	github.com/pingcap/errors v0.11.5-0.20250523034308-74f78ae071ee // indirect
	github.com/pingcap/failpoint v0.0.0-20240528011301-b51a646c7c86 // indirect
	github.com/pingcap/log v1.1.0 // indirect
	github.com/pingcap/tidb/pkg/parser v0.0.0-20260418072757-ce92298d1124 // indirect
	github.com/power-devops/perfstat v0.0.0-20240221224432-82ca36839d55 // indirect
	github.com/redis/go-redis/extra/rediscmd/v9 v9.23.0 // indirect
	github.com/riza-io/grpc-go v0.2.0 // indirect
	github.com/robfig/cron/v3 v3.0.1 // indirect
	github.com/sethvargo/go-retry v0.5.0 // indirect
	github.com/shirou/gopsutil/v4 v4.26.6 // indirect
	github.com/sirupsen/logrus v1.10.2 // indirect
	github.com/spf13/cast v1.10.0 // indirect
	github.com/spf13/cobra v1.10.2 // indirect
	github.com/spf13/pflag v1.0.10 // indirect
	github.com/sqlc-dev/doubleclick v1.0.0 // indirect
	github.com/sqlc-dev/sqlc v1.31.1 // indirect
	github.com/testcontainers/testcontainers-go v0.44.0 // indirect
	github.com/tetratelabs/wazero v1.11.0 // indirect
	github.com/tklauser/go-sysconf v0.4.0 // indirect
	github.com/tklauser/numcpus v0.12.0 // indirect
	github.com/wasilibs/go-pgquery v0.0.0-20250409022910-10ac41983c07 // indirect
	github.com/wasilibs/wazero-helpers v0.0.0-20240620070341-3dff1577cd52 // indirect
	github.com/yuin/gopher-lua v1.1.1 // indirect
	github.com/yusufpapurcu/wmi v1.2.4 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.47.0 // indirect
	go.opentelemetry.io/proto/otlp v1.11.0 // indirect
	go.uber.org/atomic v1.12.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.27.0 // indirect
	go.yaml.in/yaml/v3 v3.0.5 // indirect
	golang.org/x/crypto v0.58.0 // indirect
	golang.org/x/exp v0.0.0-20261011100332-faecfb24eb49 // indirect
	golang.org/x/net v0.61.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	golang.org/x/text v0.43.0 // indirect
	golang.org/x/time v0.14.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20261005182115-fad411399dd8 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20261005182115-fad411399dd8 // indirect
	google.golang.org/grpc v1.84.0 // indirect
	google.golang.org/protobuf v1.36.12 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
)

tool github.com/sqlc-dev/sqlc/cmd/sqlc
```
> The `tool` directive that makes `go tool sqlc` work is part of this file. Keep it, and keep the `go 1.26.0` line: rules_go reads the Go SDK version from it (`go_sdk.from_file`).

### 3.5 `MODULE.bazel` (Go half)

Create it with only the Go section now. The Node/web section is added in Phase 1c.

`MODULE.bazel`

```python
"""Ephematool build: Go (rules_go + Gazelle), the web SPA (rules_js), codegen and stack orchestration."""

module(name = "ephematool")

# Every module comes from the Bazel Central Registry at an exact version.
bazel_dep(name = "rules_go", version = "0.64.2", repo_name = "io_bazel_rules_go")
bazel_dep(name = "gazelle", version = "0.54.0", repo_name = "bazel_gazelle")
bazel_dep(name = "bazel_lib", version = "3.7.2")
bazel_dep(name = "bazel_skylib", version = "1.9.2")
bazel_dep(name = "rules_shell", version = "0.8.0")

# ---------------------------------------------------------------- Go
go_sdk = use_extension("@io_bazel_rules_go//go:extensions.bzl", "go_sdk")
go_sdk.from_file(go_mod = "//:go.mod")

go_deps = use_extension("@bazel_gazelle//:extensions.bzl", "go_deps")
go_deps.from_file(go_mod = "//:go.mod")

# These modules ship checked-in .pb.go files next to their .proto sources; use those
# instead of building protoc (and abseil) from source.
go_deps.gazelle_override(
    directives = ["gazelle:proto disable"],
    path = "github.com/hibiken/asynq",
)
go_deps.gazelle_override(
    directives = ["gazelle:proto disable"],
    path = "github.com/sqlc-dev/sqlc",
)
go_deps.gazelle_override(
    directives = ["gazelle:proto disable"],
    path = "github.com/pganalyze/pg_query_go/v6",
)

# sqlc (go.mod `tool` directive) -> tidb parser: Gazelle resolves the parser's own
# test_driver imports to a non-existent //pkg/parser/... path; pin them to the module root.
go_deps.gazelle_override(
    directives = [
        "gazelle:resolve go github.com/pingcap/tidb/pkg/parser/ast //ast",
        "gazelle:resolve go github.com/pingcap/tidb/pkg/parser/charset //charset",
        "gazelle:resolve go github.com/pingcap/tidb/pkg/parser/format //format",
        "gazelle:resolve go github.com/pingcap/tidb/pkg/parser/mysql //mysql",
        "gazelle:resolve go github.com/pingcap/tidb/pkg/parser/types //types",
    ],
    path = "github.com/pingcap/tidb/pkg/parser",
)

# Keep in sync with go.mod: `bazel mod tidy` rewrites this list.
use_repo(go_deps, "com_github_alexedwards_scs_goredisstore", "com_github_alexedwards_scs_v2", "com_github_alicebob_miniredis_v2", "com_github_caarlos0_env_v11", "com_github_danielgtaylor_huma_v2", "com_github_exaring_otelpgx", "com_github_google_go_github_v92", "com_github_hibiken_asynq", "com_github_jackc_pgx_v5", "com_github_pressly_goose_v3", "com_github_redis_go_redis_extra_redisotel_v9", "com_github_redis_go_redis_v9", "com_github_sqlc_dev_sqlc", "com_github_stretchr_testify", "com_github_testcontainers_testcontainers_go_modules_postgres", "com_github_testcontainers_testcontainers_go_modules_redis", "io_opentelemetry_go_contrib_bridges_otelslog", "io_opentelemetry_go_contrib_instrumentation_net_http_otelhttp", "io_opentelemetry_go_contrib_instrumentation_runtime", "io_opentelemetry_go_otel", "io_opentelemetry_go_otel_exporters_otlp_otlplog_otlploghttp", "io_opentelemetry_go_otel_exporters_otlp_otlpmetric_otlpmetrichttp", "io_opentelemetry_go_otel_exporters_otlp_otlptrace_otlptracehttp", "io_opentelemetry_go_otel_log", "io_opentelemetry_go_otel_metric", "io_opentelemetry_go_otel_sdk", "io_opentelemetry_go_otel_sdk_log", "io_opentelemetry_go_otel_sdk_metric", "io_opentelemetry_go_otel_trace", "org_golang_x_oauth2")
```
Two non-obvious pieces:

- The `gazelle_override(... "gazelle:proto disable" ...)` entries stop Bazel from trying to build `protoc` and abseil from source for modules that already ship generated `.pb.go` files.
- The `use_repo(...)` list must match `go.mod`. When you add or remove a dependency, run `bazel mod tidy` and it rewrites that list for you.

> **Phase 1c adds** two more `bazel_dep` lines (`aspect_rules_js`, `rules_nodejs`) and the `Node / web` section. They are left out here because `web/` does not exist yet and Bazel would fail to load them.

### 3.6 `tools/bzl/go.bzl` and `tools/bzl/BUILD.bazel`

`tools/bzl/go.bzl`

```python
"""go_test wrapper wired in via `# gazelle:map_kind` in the root BUILD.bazel.

Adds `-test.short` by default so `bazel test //...` matches the old `go test ./... -short`.
Under --config=integration (which sets --define=integration=true) the flag is dropped,
matching the old `go test -tags integration ./...`.
"""

load("@io_bazel_rules_go//go:def.bzl", _go_test = "go_test")

def go_test(name, args = [], **kwargs):
    _go_test(
        name = name,
        args = select({
            "//tools/bzl:integration": [],
            "//conditions:default": ["-test.short"],
        }) + args,
        **kwargs
    )
```
`tools/bzl/BUILD.bazel`

```python
exports_files(["go.bzl"])

# True under --config=integration (.bazelrc).
config_setting(
    name = "integration",
    define_values = {"integration": "true"},
    visibility = ["//visibility:public"],
)
```
### 3.7 Root `BUILD.bazel` (bootstrap version)

The final root file (shown in Phase 1b) also defines codegen and stack targets that need packages which do not exist yet. For now use this minimal version:

```python
load("@bazel_gazelle//:def.bzl", "gazelle")

# gazelle:prefix github.com/ephematool/ephematool
# gazelle:go_naming_convention import
# gazelle:proto disable_global
# gazelle:build_tags integration
# gazelle:map_kind go_test go_test //tools/bzl:go.bzl
# gazelle:exclude web
# gazelle:exclude deploy
# gazelle:exclude scripts
# gazelle:exclude .planning
# gazelle:exclude bazel-*

exports_files([
    "go.mod",
    "go.sum",
    "sqlc.yaml",
    ".bazelignore",
])

# bazel run //:gazelle  -- regenerate Go BUILD files after adding/removing imports or files.
gazelle(name = "gazelle")
```

The `map_kind` directive is the key line: Gazelle will emit `go_test` from `//tools/bzl:go.bzl`, so every generated test target gets the `-test.short` default for free.

### 3.8 `.gitignore` and `.dockerignore`

```text
.env
web/node_modules
web/dist
web/test-results
web/playwright-report

# Bazel
/bazel-*
user.bazelrc
```

`.dockerignore`

```text
.git
web/node_modules
.planning
.env
bazel-*
```
---

## 4. Build check

```bash
bazel build //tools/...
bazel run //:gazelle -- --help >/dev/null && echo "gazelle ok"
```

First run downloads Bazel, the Go SDK and modules; expect a few minutes. Expected: `Build completed successfully` and `gazelle ok`.

## 5. Phase 0 acceptance checklist

- [ ] `bazel --version` reports 8.x through bazelisk.
- [ ] `bazel build //tools/...` succeeds.
- [ ] `bazel run //:gazelle` runs without error.
- [ ] `docker compose version` works.
- [ ] `git status` shows no `bazel-*` symlinks as untracked (they are ignored).

## 6. Common failures

| Symptom | Cause | Fix |
|---|---|---|
| `module extension go_sdk ... go.mod` error | `go` directive missing or older than 1.26.0 | Keep `go 1.26.0` in `go.mod`. |
| `no such package '@@aspect_rules_js...'` | the Node `bazel_dep` lines or `npm` extension were added before `web/` exists | Leave them out until Phase 1c. |
| `The repository '@com_github_...' could not be resolved` | `use_repo` list is stale | `bazel mod tidy`. |
| Gazelle writes `go_test` from `@io_bazel_rules_go` | the `map_kind` directive is missing in the root `BUILD.bazel` | Add it exactly as shown, then re-run Gazelle. |
