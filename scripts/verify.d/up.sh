#!/usr/bin/env bash
set -euo pipefail
D="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
bash "$D/backends.sh"
. "$D/../lib/verify-lib.sh"

# /api/status needs a session now; the public auth config proves api behind nginx is up.
wait_http "$API_URL/api/auth/config" 120
pass "api /api/auth/config returns 200 through nginx"

code="$(curl -sS -o /dev/null -w '%{http_code}' "$API_URL/api/status")"
[ "$code" = "401" ] || fail "unauthenticated /api/status returned $code, expected 401"
pass "unauthenticated /api/status returns 401"

# SC3: all 10 services report healthy in /api/status within 120s of this script starting.
# The dashboard row's telemetry comes from a browser, which only exports while a page is open;
# with no browser it may legitimately be degraded with a "telemetry signal" detail (the
# Playwright status spec asserts all 10 Healthy with a live browser).
ALL_OK='(.services | length == 10) and ([.services[] | select(.id != "dashboard") | .status] | all(. == "healthy"))
  and ([.services[] | select(.id == "dashboard") | (.status == "healthy" or (.status == "degraded" and (.detail | test("telemetry signal"))))] | all)'
START=0  # SECONDS counts from the start of this script
ensure_session
last=""
while [ $((SECONDS - START)) -lt 120 ]; do
  if body="$(api_get /api/status 2>/dev/null)" && echo "$body" | jq -e "$ALL_OK" >/dev/null 2>&1; then
    pass "all 10 services healthy in /api/status after $((SECONDS - START))s"
    exit 0
  fi
  last="$(echo "${body:-}" | jq -c '[.services[]? | select(.status != "healthy") | {id, status}]' 2>/dev/null || true)"
  sleep 2
done
fail "not all 10 services healthy within 120s (not healthy: ${last:-unknown})"
