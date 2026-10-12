#!/usr/bin/env bash
# D-09 grep gate: the label/attribute prefixes must not be hand-written outside
# conventions/telemetry.json and the generated files. Test files are exempt
# because they assert the literal values.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
. "$ROOT/scripts/lib/verify-lib.sh"
cd "$ROOT"

dirs=()
for d in cmd internal web/src deploy; do [ -d "$d" ] && dirs+=("$d"); done

matches="$(grep -rnF --include='*.go' --include='*.ts' --include='*.tsx' --include='*.yaml' --include='*.json' \
  -e 'ephematool.io/' -e 'ephematool.env.' "${dirs[@]}" 2>/dev/null \
  | grep -v -e '\.gen\.' -e 'conventions/telemetry\.json' -e '_test\.go:' || true)"

[ -z "$matches" ] || fail "hand-written D-09 prefixes found (use internal/conventions or web/src/gen/conventions.gen.ts):
$matches"
pass "no hand-written D-09 prefixes outside generated files"
