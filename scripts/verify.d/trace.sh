#!/usr/bin/env bash
set -euo pipefail
. "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/lib/verify-lib.sh"

api_get /api/status > /dev/null || fail "GET /api/status failed"
id="$(cat "$VERIFY_TMP/last-trace-id")"
[ -n "$id" ] || fail "no X-Trace-Id header on /api/status"

services="$(tempo_services "$id" 45)"
echo "$services" | grep -qx 'ephematool-api' \
  || fail "trace $id has no ephematool-api spans (services: ${services:-none})"
pass "trace $id contains ephematool-api"
