#!/usr/bin/env bash
# No secret may land in stored telemetry (T-01-46, Security Domain V7).
# Runs a real login + status flow, downloads the resulting Tempo traces and the recent Loki
# logs, and fails if any of them contains a session cookie value, a cookie/authorization
# header attribute, a bearer or access token, or the fake GitHub client secret.
set -euo pipefail
. "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/lib/verify-lib.sh"

OUT="$VERIFY_TMP/redaction"
mkdir -p "$OUT"

# 1. Sign in and read the session cookie value from the jar (Netscape format, value in field 7).
ensure_session
jar="$VERIFY_TMP/cookies"
session_value="$(awk -F'\t' '$6 == "ephematool_session" {print $7; exit}' "$jar")"
[ -n "$session_value" ] || fail "could not read the ephematool_session value from the cookie jar"

# 2. Authenticated requests and their trace ids.
: > "$OUT/trace-ids"
for p in /api/status /api/me; do
  api_get "$p" > /dev/null || fail "GET $p failed"
  tid="$(cat "$VERIFY_TMP/last-trace-id")"
  [ -n "$tid" ] || fail "GET $p returned no X-Trace-Id"
  echo "$tid" >> "$OUT/trace-ids"
done

# A fresh login, replayed step by step so the callback response headers are visible.
cj="$VERIFY_TMP/redaction-cookies"
: > "$cj"
curl -sS -o /dev/null -D "$OUT/login-start" -c "$cj" -b "$cj" "$API_URL/api/auth/github/login" \
  || fail "login start request failed"
loc="$(header_value "$OUT/login-start" location)"
[ -n "$loc" ] || fail "no Location from /api/auth/github/login (use bazel run //:up_fake)"
curl -sS -o /dev/null -D "$OUT/login-authorize" "${loc}&login=octo-admin" \
  || fail "fake GitHub authorize request failed"
cb="$(header_value "$OUT/login-authorize" location | sed -E 's#^https?://[^/]+##')"
[ -n "$cb" ] || fail "fake GitHub returned no callback Location"
curl -sS -o /dev/null -D "$OUT/login-callback" -c "$cj" -b "$cj" "$API_URL$cb" \
  || fail "callback request failed"
cbtid="$(header_value "$OUT/login-callback" x-trace-id)"
[ -n "$cbtid" ] || fail "login callback returned no X-Trace-Id"
echo "$cbtid" >> "$OUT/trace-ids"
# The callback's own session value must not appear either.
cb_session="$(awk -F'\t' '$6 == "ephematool_session" {print $7; exit}' "$cj")"

# 3. Download every trace from Tempo (poll: the trace is flushed a few seconds after the request).
n=0
while read -r tid; do
  n=$((n + 1))
  got=""
  i=0
  while [ "$i" -le 40 ]; do
    if curl -fsS --max-time 5 "$TEMPO_URL/api/traces/$tid" -o "$OUT/trace-$n.json" 2>/dev/null \
      && grep -q 'ephematool-api' "$OUT/trace-$n.json"; then
      got=1
      break
    fi
    sleep 2
    i=$((i + 2))
  done
  [ -n "$got" ] || fail "trace $tid was not found in Tempo within 40s"
done < "$OUT/trace-ids"
pass "downloaded $n traces from Tempo"

# 3b. Span attributes that carry command text (Postgres db.query.text holds parameterized SQL
# with placeholders only, so it is allowed; Redis commands carry the values) (the Redis db.statement leak: scs session
# token plus the serialized session). Searched across all traces that started after the api
# container was created, so volumes holding older dev traces cannot fail the check.
since="$(date -u -d "$(docker inspect -f '{{.Created}}' ephematool-api-1 2>/dev/null)" +%s 2>/dev/null || true)"
if [ -z "$since" ]; then
  created="$(docker inspect -f '{{.Created}}' ephematool-api-1 2>/dev/null | sed -E 's/\.[0-9]+Z$/Z/')"
  since="$(date -j -u -f '%Y-%m-%dT%H:%M:%SZ' "$created" +%s 2>/dev/null || true)"
fi
[ -n "$since" ] || since=$(( $(date +%s) - 900 ))
for attr in 'span.db.statement =~ ".+"' 'span.db.query.text =~ ".+" && span.db.system.name = "redis"'; do
  curl -fsS --max-time 10 --get "$TEMPO_URL/api/search" \
    --data-urlencode "q={ $attr }" \
    --data-urlencode "start=$since" --data-urlencode "end=$(date +%s)" \
    --data-urlencode "limit=20" -o "$OUT/search.json" || fail "Tempo search for $attr failed"
  hits="$(jq -r '[.traces[]?.traceID] | length' "$OUT/search.json")"
  [ "$hits" = "0" ] || fail "$hits trace(s) match { $attr } (command text is recorded in spans)"
done
pass "no span records db.statement or db.query.text since the api container started"

# 4. Loki: everything the platform logged in the last 15 minutes. Wait until the logs of this
# flow are there, so the check cannot pass on a log stream that predates the flow.
first_tid="$(sed -n 1p "$OUT/trace-ids")"
i=0
while [ "$i" -le 40 ]; do
  now="$(date +%s)"
  curl -fsS --max-time 10 --get "$LOKI_URL/loki/api/v1/query_range" \
    --data-urlencode 'query={service_namespace="ephematool"}' \
    --data-urlencode "start=$(( (now - 900) ))000000000" \
    --data-urlencode "end=${now}000000000" \
    --data-urlencode "limit=5000" -o "$OUT/loki.json" 2>/dev/null || true
  if [ -s "$OUT/loki.json" ] && grep -q -F "$first_tid" "$OUT/loki.json"; then
    break
  fi
  sleep 2
  i=$((i + 2))
done
[ -s "$OUT/loki.json" ] || fail "no Loki response"
grep -q -F "$first_tid" "$OUT/loki.json" || fail "Loki has no log line for the status trace $first_tid within 40s"
lines="$(jq -r '[.data.result[]?.values | length] | add // 0' "$OUT/loki.json")"
[ "$lines" -ge 1 ] 2>/dev/null || fail "Loki returned no log lines"
pass "downloaded $lines log lines from Loki"

# 5. Forbidden content. grep -F over saved files; a non-empty match file is a failure.
: > "$OUT/matches"
scan() { # label, needle
  [ -n "$2" ] || return 0
  if grep -F -i -l -e "$2" "$OUT"/trace-*.json "$OUT/loki.json" > "$OUT/hit" 2>/dev/null; then
    echo "[$1] found in: $(tr '\n' ' ' < "$OUT/hit")" >> "$OUT/matches"
  fi
}
scan "session cookie value" "$session_value"
scan "callback session cookie value" "$cb_session"
scan "session cookie pair" "ephematool_session="
scan "oauth cookie pair" "ephematool_oauth="
scan "bearer token" "Bearer "
# Token-shaped uses only: the OAuth token endpoint's URL path (.../oauth/access_token) is a
# harmless span attribute and is not a secret.
scan "access_token query/form value" "access_token="
scan "access_token JSON key" '"access_token"'
scan "client secret" "e2e-secret"
scan "cookie header attribute" "http.request.header.cookie"
scan "set-cookie header attribute" "http.response.header.set-cookie"
scan "authorization header attribute" "http.request.header.authorization"

if [ -s "$OUT/matches" ]; then
  cat "$OUT/matches" >&2
  fail "secret material found in telemetry (see above)"
fi

# The scan is only meaningful if the grep sees real content.
grep -q -F "ephematool-api" "$OUT/trace-1.json" || fail "scan sanity check failed: trace content missing"
pass "no cookie, token, authorization header or client secret in $n traces and $lines log lines"
