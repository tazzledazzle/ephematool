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
