---
phase: 01-telemetry-spine-github-login
plan: BAZEL-CONVERSION
subsystem: build
tags: [bazel, bzlmod, rules_go, gazelle, rules_js, codegen, build-system]
requires: [01-01, 01-02, 01-03, 01-04]
provides: [bazel-build, bazel-codegen, bazel-stack-orchestration, tools/verify.sh]
affects: [01-05, 01-06, 01-07, 01-08]
key-files:
  created: [MODULE.bazel, MODULE.bazel.lock, .bazelversion, .bazelrc, .bazelignore, BUILD.bazel, tools/bzl/go.bzl, tools/stack.sh, tools/verify.sh, web/BUILD.bazel, web/pnpm-lock.yaml, web/pnpm-workspace.yaml, web/vitest.bazel.config.ts, api/BUILD.bazel, conventions/BUILD.bazel]
  removed: [Makefile]
decisions:
  - Bazel 8.2.1 (bzlmod) handles every build, test, codegen and stack-orchestration step. The Makefile is gone, with no shim.
  - Go builds pure (cgo off), the same as the Dockerfile. sqlc comes from the go.mod tool directive and uses its wasm Postgres parser, so no binary download is needed.
  - The full ordered gate is ./tools/verify.sh, a plain script. //:verify does not exist, because a nested bazel call inside bazel run would block on the output-base lock.
  - Compose images are still built from the Dockerfiles. rules_oci is deferred.
completed: 2026-10-12
---

# Phase 1: Makefile to Bazel conversion (user-directed) Summary

Bazel 8.2.1 with bzlmod now builds and tests the Go module and the Vite SPA, regenerates and drift-checks all committed codegen, and drives the compose stack. The Makefile has been removed, and plans 01-05..01-08 now use Bazel commands.

## Command mapping

| Old (`make`) | Now | Notes |
|---|---|---|
| `make up` | `bazel run //:up` | `docker compose -f deploy/compose.yaml up --build --wait` |
| `make up-fake` | `bazel run //:up_fake` | adds `deploy/compose.e2e.yaml` |
| `make down` | `bazel run //:down` | |
| `make clean` | `bazel run //:clean` | `down -v` |
| `make logs` | `bazel run //:logs` | |
| `make gen` | `bazel run //:gen` | sqlc, OpenAPI, openapi-typescript, genconventions |
| `make check-gen` | `bazel test //:check_gen` | write_source_files diff tests |
| `make test` | `bazel test //...` | Go unit tests (`-test.short`), Vitest, tsc typecheck, check_gen. Skips `manual` targets |
| `make test-integration` | `bazel test --config=integration //:test_integration` | `integration` build tag, Docker socket, no `-short` |
| `make e2e` | `bazel run //:e2e` | Playwright (`//web:e2e`) |
| `make verify-telemetry` | `bazel run //:verify_telemetry` | to run one subcommand: `bazel run //:verify_telemetry -- trace` |
| `make verify` | `./tools/verify.sh` | integration, unit, up_fake, e2e, verify_telemetry, run in that order |
| `make verify-fresh` (01-08 adds it) | `bazel run //:verify_fresh` (01-08 adds it) | described in 01-08 Task 2 |
| `npm --prefix web run build` | `bazel build //web:dist` + `bazel test //web:typecheck` | npm commands still work unchanged |
| (new) | `bazel run //:gazelle` | regenerate Go BUILD files after import or file changes |

The `bazel run` stack targets resolve the repo from `BUILD_WORKSPACE_DIRECTORY` and stamp `GIT_COMMIT` (`git rev-parse --short HEAD`) and `BUILT_AT` (UTC) into the compose environment. `/api/status` reported `commit: 19f5c81` after `bazel run //:up_fake`.

## Dependencies added

All Bazel modules come from the Bazel Central Registry at exact versions. Each was checked with an HTTP 200 on `bazel-central-registry/modules/<name>/<version>/MODULE.bazel`.

| Module | Version | Purpose |
|---|---|---|
| rules_go | 0.64.2 | Go rules and SDK |
| gazelle | 0.54.0 | BUILD generation, `go_deps` from go.mod |
| aspect_rules_js | 3.5.1 | npm deps, Vite, Vitest, tsc, Playwright, openapi-typescript |
| rules_nodejs | 6.7.6 | Node toolchain |
| bazel_lib | 3.7.2 | `write_source_files`. bazel_lib is the renamed successor of aspect_bazel_lib (rules_js 3.x depends on it, and aspect_bazel_lib 2.22.5 still comes in transitively) |
| bazel_skylib | 1.9.2 | common Starlark |
| rules_shell | 0.8.0 | `sh_binary` for the stack wrappers |

Toolchains downloaded by these rules (standard toolchain fetches, not extra binaries): Go SDK 1.26.0 (taken from `go.mod`), Node 24.20.0 (Node 24 LTS, the same major as `web/Dockerfile`), and pnpm 10.34.6 (the rules_js default, used only to derive `web/pnpm-lock.yaml`). Go and npm packages still come from go.mod/go.sum and web/package-lock.json. No new Go or npm packages were added.

## What was built

- **Go:** Gazelle generates the BUILD files (`# gazelle:build_tags integration` keeps the integration files in `go_test` srcs, and rules_go filters them out at compile time). There is a `go_binary` for cmd/api, cmd/worker, cmd/fakegithub and tools/genconventions, and a `go_test` for every tested package. `tools/bzl/go.bzl` (wired in with `gazelle:map_kind`) adds `-test.short` unless `--config=integration` is set.
- **Integration:** `--config=integration` sets the rules_go `integration` tag and `--define=integration=true` (which drops `-short`), runs `TestRunner` locally (unsandboxed), and passes through `DOCKER_HOST`, `HOME` and the `TESTCONTAINERS_*` variables. `//:test_integration` lists `//internal/auth:auth_test`.
- **Codegen:** each generator writes to `_gen/` and `write_source_files` copies the result into the committed paths. sqlc output names come from a glob of `internal/store/queries/*.sql`. schema.d.ts is generated from the freshly generated openapi.json, the same chain `make gen` used.
- **SPA:** `npm_translate_lock` reads `web/package-lock.json` and derives `web/pnpm-lock.yaml`, which is committed. The resolved versions match exactly: 288 packages, 0 mismatches. `//web:dist` (vite build), `//web:typecheck` (`tsc -b --noEmit`), `//web:test` (vitest run) and `//web:e2e` (Playwright; tags manual, local, no-sandbox, requires-docker, requires-network, external).
- **Orchestration:** `tools/stack.sh` sits behind `//:up`, `//:up_fake`, `//:down`, `//:clean`, `//:logs` and `//:verify_telemetry`. `//:e2e` is an alias to `//web:e2e`. `tools/verify.sh` runs the ordered gate.
- `.gitignore` now covers `/bazel-*` and `user.bazelrc`, and `.dockerignore` covers `bazel-*`. `.bazelrc` has defaults plus `--config=integration` and `--config=ci`, and `try-import user.bazelrc`.

## Verification (real output)

- `bazel build //...`: `Build completed successfully` (manual targets skipped).
- `bazel test //... --nocache_test_results`: `Executed 15 out of 15 tests: 15 tests pass.` The Go tests are auth, conventions, fakegithub, httpapi, queue and genconventions, the same six packages that `go test ./... -short` reports `ok`. `//web:test` reports `Test Files 2 passed (2) / Tests 8 passed (8)`. `//web:typecheck` and the 7 gen diff tests also pass.
- `bazel run //:gen` on a clean tree: 7 files copied, `git status` clean. `bazel test //:check_gen`: 7/7 pass.
  - Drift 1: appended a newline to the committed `api/openapi.json`. `//api:gen_test FAILED` with "api:openapi.json is out of date... run bazel run //:gen". Reverted.
  - Drift 2: added a query to `internal/store/queries/users.sql` without regenerating. `//internal/store:gen_2_test FAILED`. Reverted, and check_gen is green again.
- `bazel test --config=integration //:test_integration`: `//internal/auth:auth_test PASSED`. With `-test.v` the output shows TestOAuthAllowedMember, ViewerRole, TeamMember, DeniedNonMember, StateMismatch, PKCESent, OpenRedirect, NextPreserved, Cancelled and NotConfigured all passing against testcontainers Postgres and Redis.
- `bazel run //:up_fake`: all 10 containers `Healthy`, ports bound to 127.0.0.1. `bazel run //:verify_telemetry -- trace` printed `PASS: trace ... contains ephematool-api`, and `bazel run //:verify_telemetry` (all) passed backends, conventions, logs, metrics, trace, up and worker. `bazel run //:down` removed everything (0 containers left).
- `git grep -n -E '\bmake (up|down|gen|check-gen|test|verify|up-fake|e2e)'` over 01-0[5-8]-PLAN.md, 01-VALIDATION.md, SKELETON.md, CLAUDE.md and scripts: no matches.
- `go build ./...`, `go vet ./...` and `go vet -tags integration ./...` all pass. The Makefile is gone.

## Deviations and notes

1. **[Rule 3] Two go_deps gazelle_overrides.** `gazelle:proto disable` for asynq, sqlc and pg_query_go, because their checked-in `.pb.go` files would otherwise pull in protoc and abseil built from C++ source, which also fails on this Xcode SDK. Plus `gazelle:resolve` fixes for the tidb parser's `test_driver` package (a sqlc dependency).
2. **[Rule 3] Pure Go (`--@io_bazel_rules_go//go/config:pure`).** This matches the Dockerfile's `CGO_ENABLED=0`, and it is what lets sqlc build from go.mod without compiling libpg_query C (sqlc switches to `wasilibs/go-pgquery`). The generated sqlc output is byte-identical.
3. **[Rule 3] rules_js lock handling.** rules_js 3.5.1 only accepts pnpm locks, so `npm_package_lock` + `update_pnpm_lock = True` derive `web/pnpm-lock.yaml` (the documented default of True is not applied under bzlmod, so the attribute is set explicitly). `web/pnpm-workspace.yaml` declares `allowBuilds: {fsevents: false}`, which rules_js requires, and `.aspect/rules/external_repository_action_cache/` is committed. npm is still the package manager.
4. **[Rule 3] Vite and Vitest symlink handling.** Rolldown canonicalizes paths natively and escapes the sandbox, so `//web:dist` runs with `no-sandbox`. Vitest uses `web/vitest.bazel.config.ts`, which is the base config plus `resolve.preserveSymlinks: true`. npm runs still use `vitest.config.ts`. The new file is added to the `tsconfig.node.json` include, and `npm --prefix web exec tsc -b` and `npm test` still pass (8 tests).
5. **`//:verify` is not provided.** `./tools/verify.sh` is the entry point, because nesting bazel inside `bazel run` deadlocks. In 01-08, `fresh.sh` therefore calls `tools/stack.sh down` directly in the main checkout, and runs `bazel run //:up_fake` only inside the temp clone, which has its own output base.
6. **Plan edits beyond command spellings:** 01-08's file lists change `Makefile` to `BUILD.bazel, tools/verify.sh`. `verify-fresh` becomes `//:verify_fresh`, and the README acceptance string becomes "bazel run //:up_fake". Each of 01-05..01-08 gets a short Bazel build-hygiene note (run gazelle after Go import changes, run `bazel mod tidy` after `go get`, list new integration packages in `//:test_integration`, commit the derived pnpm-lock.yaml). 01-07 gets the miniredis note, and go.mod, go.sum, MODULE.bazel and internal/auth/BUILD.bazel are added to its file lists. Acceptance intent is unchanged. VALIDATION and SKELETON use the new spellings, and SKELETON gains a "Build system" row. CLAUDE.md had no make commands, so it is unchanged. The historical 01-01..01-04 SUMMARYs are untouched. `.env.example` now says `bazel run //:up`.

## Known issues and follow-ups

- **Playwright browsers:** Playwright 1.64.0 needs `chromium_headless_shell-1248`. The host cache only has 1243, so any spec fails at browser launch, both under Bazel and with plain `npm exec playwright test` (confirmed with a throwaway spec, since deleted). Today there are no e2e specs, so `bazel run //:e2e` prints "No tests found", the same as `make e2e` did. Before 01-05 runs, install the browser with `npm --prefix web exec -- playwright install chromium`. This is a host browser download, so it was not done here.
- **Follow-up (rules_oci):** compose images are still built from the Dockerfiles. Building the Go and nginx images with rules_oci (plus `oci_load` into compose) would make images hermetic and reuse the Bazel cache. This was deliberately left out of this change.
- **Follow-up:** Go binaries built by Bazel are not stamped with `x_defs`. The image builds still stamp them through the Dockerfile build args. If Bazel-built binaries ever ship, add a `--workspace_status_command` and `x_defs`.
- `.planning/STATE.md` had an uncommitted decision line written by the orchestrator before this run started. I left it for the orchestrator to commit.

## Commits

- eb79906 build: add Bazel (bzlmod) with rules_go and Gazelle-generated Go BUILD files
- fd2b461 build: add //:test_integration suite for Docker-backed Go integration tests
- 1850c9d build: express Go codegen as Bazel actions with //:gen and //:check_gen
- 19f5c81 build: build and test the web SPA with rules_js
- 08bd1de build: add bazel run targets for the compose stack and tools/verify.sh
- 60e0f02 build: remove the Makefile in favour of Bazel targets
- 062b710 docs(01): switch plans 01-05..01-08 from make targets to Bazel commands
- b6a607d docs(01): use Bazel commands in VALIDATION and SKELETON

## Self-Check: PASSED
