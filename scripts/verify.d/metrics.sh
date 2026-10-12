#!/usr/bin/env bash
set -euo pipefail
. "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/lib/verify-lib.sh"

api_get /api/status > /dev/null || fail "GET /api/status failed"

q='sum(http_server_request_duration_seconds_count{service_name="ephematool-api",http_route="/api/status"})'
i=0
while [ "$i" -lt 30 ]; do
  v="$(prom_value "$q")"
  if [ -n "$v" ] && awk -v v="$v" 'BEGIN{exit !(v > 0)}'; then
    pass "http_server_request_duration_seconds_count = $v"
    exit 0
  fi
  sleep 2
  i=$((i + 2))
done
fail "no http_server_request_duration_seconds_count for /api/status within 30s"
