#!/usr/bin/env bash
# Full local verification gate (replaces `make verify`). Run it directly from a shell:
#
#   ./tools/verify.sh
#
# Steps, in order (stops at the first failure):
#   1. Docker-backed Go integration tests   bazel test --config=integration //:test_integration
#   2. Unit tests, typecheck, codegen drift bazel test //...
#   3. Bring up the stack with fake GitHub  bazel run //:up_fake
#   4. Playwright e2e against the stack     bazel run //:e2e
#   5. Telemetry checks against the stack   bazel run //:verify_telemetry
#
# This is a plain script on purpose: it calls bazel sequentially. It must not be wrapped
# in a `bazel run` target, because a bazel invocation nested inside `bazel run` blocks on
# the output base lock.
set -euo pipefail

cd "$(dirname "${BASH_SOURCE[0]}")/.."

BAZEL="${BAZEL:-bazel}"

step() {
  echo
  echo "==> $*"
  "$@"
}

step "$BAZEL" test --config=integration //:test_integration
step "$BAZEL" test //...
step "$BAZEL" run //:up_fake
step "$BAZEL" run //:e2e
step "$BAZEL" run //:verify_telemetry
echo
echo "verify: all steps passed"
