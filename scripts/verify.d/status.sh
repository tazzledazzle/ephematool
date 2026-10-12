#!/usr/bin/env bash
# 10-service /api/status check, then stop/start loki to prove Down is reflected within 2 probe cycles.
set -euo pipefail
D="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "$D/../lib/verify-lib.sh"

ROOT="$(cd "$D/../.." && pwd)"
COMPOSE=(docker compose -f "$ROOT/deploy/compose.yaml")
[ -f "$ROOT/deploy/compose.e2e.yaml" ] && COMPOSE=(docker compose -f "$ROOT/deploy/compose.yaml" -f "$ROOT/deploy/compose.e2e.yaml")

WANT_IDS='["api","worker","dashboard","postgres","redis","otel-collector","tempo","loki","prometheus","grafana"]'

# Always bring loki back, even when an assertion fails (the stack must not stay broken).
restore_loki() { "${COMPOSE[@]}" start loki >/dev/null 2>&1 || true; }
trap restore_loki EXIT

status_of() { # status_of ID -> status string ("" if the call failed)
  api_get /api/status 2>/dev/null | jq -r --arg id "$1" '.services[] | select(.id==$id) | .status' 2>/dev/null || true
}

# wait_status ID WANT TIMEOUT
wait_status() {
  local id="$1" want="$2" timeout="$3" i=0 got=""
  while [ "$i" -lt "$timeout" ]; do
    got="$(status_of "$id")"
    [ "$got" = "$want" ] && return 0
    sleep 1
    i=$((i + 1))
  done
  fail "$id did not reach '$want' within ${timeout}s (last: '${got}')"
}

# 1. All 10 services, exact order, all healthy (poll up to 120s from stack start).
# The dashboard row's telemetry comes from a browser, which only exports while a page is open.
# This script has no browser, so "dashboard" may legitimately be degraded with a telemetry-signal
# detail; the Playwright status spec asserts all 10 rows Healthy with a live browser.
ALL_OK='(.services | length == 10) and ([.services[] | select(.id != "dashboard") | .status] | all(. == "healthy"))
  and ([.services[] | select(.id == "dashboard") | (.status == "healthy" or (.status == "degraded" and (.detail | test("telemetry signal"))))] | all)'
ensure_session
deadline=$((SECONDS + 120))
body=""
while :; do
  body="$(api_get /api/status 2>/dev/null || true)"
  if [ -n "$body" ] && echo "$body" | jq -e "$ALL_OK" >/dev/null 2>&1; then
    break
  fi
  [ "$SECONDS" -lt "$deadline" ] || {
    echo "$body" | jq -c '.services[]? | {id, status, detail}' >&2 || true
    fail "/api/status did not report 10 services (dashboard may lack a browser signal, the rest healthy) within 120s"
  }
  sleep 2
done
echo "$body" | jq -e --argjson want "$WANT_IDS" '[.services[].id] == $want' >/dev/null \
  || fail "service ids are not in the contract order"
pass "/api/status reports 10 services in order, all healthy (dashboard healthy or awaiting a browser signal)"

# 2. Stop loki: Down within 25s (2 probe cycles + margin).
"${COMPOSE[@]}" stop loki >/dev/null
wait_status loki down 25
pass "loki reported down within 25s of stopping it"

# 3. Start loki: healthy again within 60s.
"${COMPOSE[@]}" start loki >/dev/null
wait_status loki healthy 60
pass "loki healthy again after restart"
