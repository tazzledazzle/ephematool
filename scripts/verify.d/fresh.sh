#!/usr/bin/env bash
# SC3 / D-12: a fresh clone starts the whole stack with one command and all 10 services report
# healthy within 120s. Not part of `all`: it tears down the main stack and builds a second one.
#
#   bazel run //:verify_fresh
#
# Runs inside `bazel run //:verify_fresh`, so it must not call bazel against the main
# workspace (output-base lock). The clone has its own output base, so bazel inside it is fine.
set -euo pipefail

ROOT="${BUILD_WORKSPACE_DIRECTORY:-$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)}"
cd "$ROOT"

fail() { echo "FAIL: $*" >&2; exit 1; }
pass() { echo "PASS: $*"; }

# The clone contains committed files only, so uncommitted work would not be tested.
if [ -n "$(git status --porcelain)" ]; then
  git status --short >&2
  fail "working tree is not clean: commit or remove these changes first (the clone only sees committed files)"
fi

tmp="$(mktemp -d "${TMPDIR:-/tmp}/ephematool-fresh.XXXXXX")"
case "$tmp" in
  */ephematool-fresh.*) ;;
  *) fail "unexpected temp dir: $tmp" ;;
esac

cleanup() {
  status=$?
  (cd "$tmp/repo" && bazel run //:down && bazel shutdown) >/dev/null 2>&1 || true
  rm -rf "$tmp"
  exit "$status"
}
trap cleanup EXIT

git clone --quiet --depth 1 "file://$ROOT" "$tmp/repo"
[ ! -e "$tmp/repo/.env" ] || fail "clone unexpectedly contains .env"

# Free the host ports: stop the main stack. Called directly (not via bazel run //:down).
"$ROOT/tools/stack.sh" down

started=$SECONDS
echo "==> bazel run //:up_fake (fresh clone at $tmp/repo)"
(cd "$tmp/repo" && bazel run //:up_fake)
echo "up_fake returned after $((SECONDS - started))s"

# All 10 services healthy within 120s of the stack coming up.
BUILD_WORKSPACE_DIRECTORY="$tmp/repo" "$tmp/repo/scripts/verify-telemetry.sh" up
pass "fresh clone: one command started the stack and all 10 services are healthy (total $((SECONDS - started))s)"
