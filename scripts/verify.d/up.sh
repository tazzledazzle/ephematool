#!/usr/bin/env bash
set -euo pipefail
D="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
bash "$D/backends.sh"
. "$D/../lib/verify-lib.sh"

wait_http "$API_URL/api/status" 120
pass "api /api/status returns 200"
