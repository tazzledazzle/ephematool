#!/usr/bin/env bash
# Dashboard telemetry: the browser trace id captured by the Playwright telemetry spec
# resolves in Tempo to dashboard + api spans, and dashboard metrics and logs are queryable.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
. "$ROOT/scripts/lib/verify-lib.sh"

id="${1:-}"
if [ -z "$id" ] && [ -f "$ROOT/web/test-results/dashboard-trace-id.txt" ]; then
  id="$(tr -d '[:space:]' < "$ROOT/web/test-results/dashboard-trace-id.txt")"
fi
[ -n "$id" ] || fail "no trace id: pass one as the first argument, or run bazel run //:e2e first (writes web/test-results/dashboard-trace-id.txt)"

services="$(tempo_services "$id" 45)"
for svc in ephematool-dashboard ephematool-api; do
  echo "$services" | grep -qx "$svc" \
    || fail "trace $id has no $svc spans (services: ${services:-none})"
done
pass "trace $id contains ephematool-dashboard and ephematool-api"

# Browser metrics export every 15s, so poll.
v=""
for _ in $(seq 1 23); do
  v="$(prom_value 'sum(ephematool_ui_page_views_total{service_name="ephematool-dashboard"})')"
  if [ -n "$v" ] && awk -v v="$v" 'BEGIN{exit !(v > 0)}'; then break; fi
  sleep 2
done
[ -n "$v" ] && awk -v v="$v" 'BEGIN{exit !(v > 0)}' \
  || fail "ephematool_ui_page_views_total{service_name=\"ephematool-dashboard\"} not > 0 (got '${v:-empty}')"
pass "dashboard page view counter = $v"

n=0
for _ in $(seq 1 23); do
  n="$(loki_count '{service_name="ephematool-dashboard"}')"
  [ "${n:-0}" -ge 1 ] && break
  sleep 2
done
[ "${n:-0}" -ge 1 ] || fail "no Loki logs for service_name=ephematool-dashboard"
pass "Loki has $n dashboard log line(s)"
