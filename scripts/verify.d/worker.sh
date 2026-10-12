#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
. "$ROOT/scripts/lib/verify-lib.sh"

# PING_THROTTLE (default 10s) may skip the enqueue, so retry a few times.
id=""
services=""
for attempt in 1 2 3; do
  api_get /api/status > /dev/null || fail "GET /api/status failed"
  id="$(cat "$VERIFY_TMP/last-trace-id")"
  [ -n "$id" ] || fail "no X-Trace-Id header on /api/status"
  services="$(tempo_services "$id" 30)"
  if echo "$services" | grep -qx 'ephematool-worker'; then
    break
  fi
  [ "$attempt" -lt 3 ] && sleep 11
done
echo "$services" | grep -qx 'ephematool-api' \
  || fail "trace $id has no ephematool-api spans (services: ${services:-none})"
echo "$services" | grep -qx 'ephematool-worker' \
  || fail "trace $id has no ephematool-worker spans (services: ${services:-none})"
pass "trace $id spans ephematool-api and ephematool-worker"

n=0
for _ in $(seq 1 15); do
  n="$(loki_count "{service_name=\"ephematool-worker\"} | trace_id=\"$id\"")"
  [ "${n:-0}" -ge 1 ] && break
  sleep 2
done
[ "${n:-0}" -ge 1 ] || fail "no Loki worker logs with trace_id=$id"
pass "Loki has $n worker log line(s) for trace $id"

v=""
for _ in $(seq 1 15); do
  v="$(prom_value 'sum(ephematool_queue_task_processed_total{service_name="ephematool-worker"})')"
  [ -n "$v" ] && awk -v v="$v" 'BEGIN{exit !(v>0)}' && break
  sleep 2
done
[ -n "$v" ] && awk -v v="$v" 'BEGIN{exit !(v>0)}' \
  || fail "ephematool_queue_task_processed_total not > 0 (got '${v:-empty}')"
pass "worker task counter = $v"

exists="$(docker compose -f "$ROOT/deploy/compose.yaml" exec -T redis redis-cli EXISTS worker:heartbeat | tr -d '\r\n ')"
[ "$exists" = "1" ] || fail "redis key worker:heartbeat missing (EXISTS=$exists)"
pass "redis key worker:heartbeat exists"
