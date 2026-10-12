#!/usr/bin/env bash
set -euo pipefail
. "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/lib/verify-lib.sh"

wait_http "$TEMPO_URL/ready" 120
pass "tempo ready"
wait_http "$LOKI_URL/ready" 120
pass "loki ready"
wait_http "$PROM_URL/-/ready" 120
pass "prometheus ready"
wait_http "$GRAFANA_URL/api/health" 120
pass "grafana ready"
wait_http "$COLLECTOR_HEALTH_URL" 120
pass "collector healthy"

curl -fsS "$GRAFANA_URL/api/datasources/uid/loki" | grep -q derivedFields \
  || fail "loki datasource missing derivedFields"
pass "loki datasource has derivedFields"
curl -fsS "$GRAFANA_URL/api/datasources/uid/tempo" | grep -q tracesToLogsV2 \
  || fail "tempo datasource missing tracesToLogsV2"
pass "tempo datasource has tracesToLogsV2"
