#!/usr/bin/env bash
set -euo pipefail
. "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/lib/verify-lib.sh"

api_get /api/status > /dev/null || fail "GET /api/status failed"
id="$(cat "$VERIFY_TMP/last-trace-id")"
[ -n "$id" ] || fail "no X-Trace-Id header on /api/status"

i=0
while [ "$i" -lt 30 ]; do
  n="$(loki_count "{service_name=\"ephematool-api\"} | trace_id=\"$id\"" 300 || echo 0)"
  if [ -n "$n" ] && [ "$n" -ge 1 ] 2>/dev/null; then
    pass "loki returned $n line(s) for trace $id"
    exit 0
  fi
  sleep 2
  i=$((i + 2))
done
fail "no loki lines for ephematool-api with trace_id=$id within 30s"
