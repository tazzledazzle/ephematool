#!/usr/bin/env bash
# Local compose stack orchestration behind the `bazel run` targets in //:BUILD.bazel:
#
#   bazel run //:up                 docker compose up --build --wait
#   bazel run //:up_fake            same, plus the fake-GitHub e2e overlay
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
COMPOSE_FAKE=("${COMPOSE[@]}" -f deploy/compose.e2e.yaml)

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
