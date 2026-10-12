#!/usr/bin/env bash
# A non-member sign-in must leave an auth.denied log line carrying the login in Loki (D-04).
set -euo pipefail
. "$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)/lib/verify-lib.sh"

# The fake login for octo-outsider ends in a /denied redirect and no session cookie,
# so ensure_session (which asserts a cookie) is not used; replay its steps instead.
jar="$VERIFY_TMP/denied-cookies"
: > "$jar"
curl -sS -o /dev/null -D "$VERIFY_TMP/denied-start" -c "$jar" -b "$jar" \
  "$API_URL/api/auth/github/login" || fail "login start request failed"
loc="$(header_value "$VERIFY_TMP/denied-start" location)"
[ -n "$loc" ] || fail "no Location from /api/auth/github/login (use bazel run //:up_fake)"
curl -sS -o /dev/null -D "$VERIFY_TMP/denied-authorize" "${loc}&login=octo-outsider" \
  || fail "fake GitHub authorize request failed"
cb="$(header_value "$VERIFY_TMP/denied-authorize" location)"
[ -n "$cb" ] || fail "fake GitHub returned no callback Location"
cb="$(printf '%s' "$cb" | sed -E 's#^https?://[^/]+##')"
curl -sS -o /dev/null -D "$VERIFY_TMP/denied-callback" -c "$jar" -b "$jar" "$API_URL$cb" \
  || fail "callback request failed"
dest="$(header_value "$VERIFY_TMP/denied-callback" location)"
case "$dest" in
  /denied*) ;;
  *) fail "expected redirect to /denied, got '${dest}'" ;;
esac

i=0
while [ "$i" -lt 30 ]; do
  n="$(loki_count '{service_name="ephematool-api"} |= "auth.denied" |= "octo-outsider"' 300 || echo 0)"
  if [ -n "$n" ] && [ "$n" -ge 1 ] 2>/dev/null; then
    pass "loki returned $n auth.denied line(s) for octo-outsider"
    exit 0
  fi
  sleep 2
  i=$((i + 2))
done
fail "no auth.denied log line for octo-outsider in Loki within 30s"
