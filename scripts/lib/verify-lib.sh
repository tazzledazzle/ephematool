#!/usr/bin/env bash
# Shared helpers for scripts/verify.d/*.sh. Source this file; do not execute it.

API_URL="${API_URL:-http://127.0.0.1:8081}"
TEMPO_URL="${TEMPO_URL:-http://127.0.0.1:3200}"
PROM_URL="${PROM_URL:-http://127.0.0.1:9090}"
LOKI_URL="${LOKI_URL:-http://127.0.0.1:3100}"
GRAFANA_URL="${GRAFANA_URL:-http://127.0.0.1:3000}"
COLLECTOR_HEALTH_URL="${COLLECTOR_HEALTH_URL:-http://127.0.0.1:13133/}"
VERIFY_TMP="${VERIFY_TMP:-$(mktemp -d)}"

pass() { echo "PASS: $*"; }
fail() { echo "FAIL: $*" >&2; exit 1; }

# wait_http URL TIMEOUT_SECONDS
wait_http() {
  local url="$1" timeout="${2:-60}" i=0
  while [ "$i" -lt "$timeout" ]; do
    if curl -fsS -o /dev/null --max-time 3 "$url" 2>/dev/null; then
      return 0
    fi
    sleep 1
    i=$((i + 1))
  done
  fail "timed out after ${timeout}s waiting for $url"
}

# api_get PATH -> body on stdout; headers in $VERIFY_TMP/last-headers; trace id in $VERIFY_TMP/last-trace-id
api_get() {
  local path="$1"
  curl -fsS -D "$VERIFY_TMP/last-headers" "$API_URL$path" || return 1
  tr -d '\r' < "$VERIFY_TMP/last-headers" \
    | awk 'tolower($1)=="x-trace-id:" {print $2}' > "$VERIFY_TMP/last-trace-id"
}

# tempo_services TRACE_ID [TIMEOUT] -> sorted unique service.name values
tempo_services() {
  local id="$1" timeout="${2:-30}" waited=0 out=""
  while [ "$waited" -le "$timeout" ]; do
    if curl -fsS --max-time 5 "$TEMPO_URL/api/traces/$id" -o "$VERIFY_TMP/trace.json" 2>/dev/null; then
      out="$(jq -r '
        ([.batches[]?, .resourceSpans[]?]
          | .[]
          | .resource.attributes[]?
          | select(.key == "service.name")
          | .value.stringValue) // empty' "$VERIFY_TMP/trace.json" 2>/dev/null | sort -u)"
      if [ -n "$out" ]; then
        echo "$out"
        return 0
      fi
    fi
    sleep 2
    waited=$((waited + 2))
  done
  return 0
}

# prom_value PROMQL -> first sample value or empty
prom_value() {
  curl -fsS --max-time 5 --get "$PROM_URL/api/v1/query" --data-urlencode "query=$1" 2>/dev/null \
    | jq -r '.data.result[0].value[1] // empty'
}

# loki_count LOGQL [SINCE_SECONDS] -> number of log lines
loki_count() {
  local q="$1" since="${2:-900}" now start
  now="$(date +%s)"
  start=$((now - since))
  curl -fsS --max-time 10 --get "$LOKI_URL/loki/api/v1/query_range" \
    --data-urlencode "query=$q" \
    --data-urlencode "start=${start}000000000" \
    --data-urlencode "end=${now}000000000" \
    --data-urlencode "limit=1000" 2>/dev/null \
    | jq -r '[.data.result[]?.values | length] | add // 0'
}
