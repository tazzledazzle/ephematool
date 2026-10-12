#!/usr/bin/env bash
# Dispatcher: scripts/verify-telemetry.sh <subcommand|all>
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
DIR="$ROOT/scripts/verify.d"

available() {
  for f in "$DIR"/*.sh; do
    basename "$f" .sh
  done
}

if [ $# -lt 1 ]; then
  echo "usage: $0 <subcommand|all>" >&2
  echo "available: $(available | tr '\n' ' ')all" >&2
  exit 2
fi

sub="$1"
shift

if [ "$sub" = "all" ]; then
  for f in "$DIR"/*.sh; do
    # fresh builds a second stack from a clone (bazel run //:verify_fresh); never part of `all`.
    case "$(basename "$f")" in fresh*) continue ;; esac
    echo "== $(basename "$f" .sh)"
    bash "$f" "$@"
  done
  exit 0
fi

if [ ! -f "$DIR/$sub.sh" ]; then
  echo "unknown subcommand: $sub" >&2
  echo "available: $(available | tr '\n' ' ')all" >&2
  exit 2
fi

exec bash "$DIR/$sub.sh" "$@"
