#!/usr/bin/env bash
# Shared helpers for scripts/verify.d/*.sh. Source this file; do not execute it.

# Through nginx (the same-origin front door), not the API port.
API_URL="${API_URL:-http://127.0.0.1:8080}"
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

# header_value FILE NAME -> value of the first matching response header
header_value() {
  tr -d '\r' < "$1" | awk -v n="$(echo "$2" | tr '[:upper:]' '[:lower:]'):" 'tolower($1)==n {print $2; exit}'
}

# ensure_session [LOGIN=${LOGIN:-octo-admin}]
# Signs in through the fake GitHub with curl and leaves ephematool_session in $VERIFY_TMP/cookies.
ensure_session() {
  local login="${1:-${LOGIN:-octo-admin}}" jar="$VERIFY_TMP/cookies" loc cb
  : > "$jar"
  curl -sS -o /dev/null -D "$VERIFY_TMP/oauth-start" -c "$jar" -b "$jar" \
    "$API_URL/api/auth/github/login" || fail "ensure_session: login start request failed"
  loc="$(header_value "$VERIFY_TMP/oauth-start" location)"
  [ -n "$loc" ] || fail "ensure_session: /api/auth/github/login returned no Location (is GitHub configured? use bazel run //:up_fake)"
  curl -sS -o /dev/null -D "$VERIFY_TMP/oauth-authorize" "${loc}&login=${login}" \
    || fail "ensure_session: fake GitHub authorize request failed"
  cb="$(header_value "$VERIFY_TMP/oauth-authorize" location)"
  [ -n "$cb" ] || fail "ensure_session: fake GitHub returned no callback Location for $login"
  # The callback URL carries PUBLIC_BASE_URL's origin (localhost); replay it against API_URL
  # so the cookie jar sees a single host.
  cb="$(printf '%s' "$cb" | sed -E 's#^https?://[^/]+##')"
  curl -sS -o /dev/null -c "$jar" -b "$jar" "$API_URL$cb" || fail "ensure_session: callback request failed"
  grep -q 'ephematool_session' "$jar" || fail "ensure_session: no ephematool_session cookie after callback for $login"
}

# api_get PATH -> body on stdout; headers in $VERIFY_TMP/last-headers; trace id in $VERIFY_TMP/last-trace-id
# Sends the session cookie jar; on a 401 it signs in once and retries.
api_get() {
  local path="$1" jar="$VERIFY_TMP/cookies" code attempt
  [ -f "$jar" ] || : > "$jar"
  for attempt in 1 2; do
    code="$(curl -sS -o "$VERIFY_TMP/last-body" -w '%{http_code}' -D "$VERIFY_TMP/last-headers" \
      -b "$jar" -c "$jar" "$API_URL$path")" || return 1
    if [ "$code" = "401" ] && [ "$attempt" = "1" ]; then
      ensure_session
      continue
    fi
    break
  done
  case "$code" in
    2??) ;;
    *) echo "GET $path -> HTTP $code" >&2; return 1 ;;
  esac
  header_value "$VERIFY_TMP/last-headers" x-trace-id > "$VERIFY_TMP/last-trace-id"
  cat "$VERIFY_TMP/last-body"
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
