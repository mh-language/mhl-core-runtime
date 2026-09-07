#!/usr/bin/env bash
# smoke.sh — end-to-end check for the mhl-sql-postgres extension.
#
#   make smoke          # builds bin/, brings up Postgres (seeded), runs this
#   ./smoke.sh          # assumes `make build` + `make up` already ran
#
# Override the runtime with:  MHL=/path/to/mhl ./smoke.sh
# EXTENSION_SOURCE can select a release archive URL (including #sha256=...).
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/../../.." && pwd)"

MHL="${MHL:-}"
if [[ -z "$MHL" ]]; then
  for cand in "$ROOT/src/mhl-runtime/dist/mhl" "$ROOT/tests/extensions/mhl" "$ROOT/tests/cloud/mhl"; do
    [[ -x "$cand" ]] && MHL="$cand" && break
  done
fi
[[ -x "$MHL" ]] || { echo "no mhl binary found — build it or set MHL=" >&2; exit 1; }
if [[ -z "${EXTENSION_SOURCE:-}" ]]; then
  [[ -x "$HERE/bin/mhl-sql-postgres" ]] || { echo "bin/mhl-sql-postgres missing — run: make build" >&2; exit 1; }
fi
EXTENSION_SOURCE="${EXTENSION_SOURCE:-$HERE}"

export SQL_PG_DSN="${SQL_PG_DSN:-postgres://mhl:mhl-secret-pw@localhost:5434/demo?sslmode=disable}"

PROJ="$(mktemp -d "${TMPDIR:-/tmp}/mhl-sql-pg-smoke.XXXXXX")"
trap 'rm -rf "$PROJ"' EXIT

cat > "$PROJ/main.mh" <<EOF
extension sql Db {
    dsn: env("SQL_PG_DSN")
    read_only: true
    log: "$PROJ/wire.jsonl"
}

pipeline Q {
    step rows {
        var acme = Db.query("SELECT name, org, score, tags FROM people WHERE org = \$1 ORDER BY name", "acme")
        log(acme)
    }
    step one {
        var top = Db.queryRow("SELECT name, score FROM people ORDER BY score DESC LIMIT 1")
        log(top)
    }
    step scalar {
        var n = Db.queryValue("SELECT count(*) FROM people WHERE active")
        log(n)
    }
}
EOF

cat > "$PROJ/write.mh" <<EOF
extension sql Db {
    dsn: env("SQL_PG_DSN")
    read_only: true
}
pipeline W {
    step nope {
        var r = Db.query("INSERT INTO people(name, org) VALUES ('mallory', 'x') RETURNING id")
        log(r)
    }
}
EOF

echo "==> project: $PROJ"
( cd "$PROJ" && "$MHL" extension install "$EXTENSION_SOURCE" )
( cd "$PROJ" && "$MHL" extension doctor )

diagnose() {
  echo "--- smoke diagnostics ---" >&2
  echo "runtime: $MHL" >&2
  echo "extension: $HERE" >&2
  echo "project: $PROJ" >&2
  echo "--- project files ---" >&2
  ls -la "$PROJ" >&2 || true
  for output in "$OUT" "${WOUT:-}"; do
    if [[ -n "$output" && -f "$output" ]]; then
      echo "--- $output ---" >&2
      cat "$output" >&2 || true
    fi
  done
  if [[ -f "$PROJ/wire.jsonl" ]]; then
    echo "--- $PROJ/wire.jsonl ---" >&2
    cat "$PROJ/wire.jsonl" >&2 || true
  fi
  if command -v psql >/dev/null 2>&1; then
    echo "--- PostgreSQL connectivity ---" >&2
    psql "$SQL_PG_DSN" -v ON_ERROR_STOP=1 -Atc \
      'SELECT current_database(), count(*) FROM people' >&2 || true
  else
    echo "psql not installed; PostgreSQL connectivity was not probed" >&2
  fi
}

OUT="$PROJ/run.out"
if ! ( cd "$PROJ" && "$MHL" run main.mh ) >"$OUT" 2>&1; then
  echo "--- mhl run main.mh failed ---" >&2
  diagnose
  exit 1
fi
echo "--- reads ---"; cat "$OUT"

WOUT="$PROJ/write.out"
set +e
( cd "$PROJ" && "$MHL" run write.mh ) >"$WOUT" 2>&1
wrc=$?
set -e
echo "--- write attempt (rc=$wrc) ---"; cat "$WOUT"

fails=()
grep -q '"name":"Ana"'                "$OUT" || fails+=("query did not return the seeded Ana row as an object")
grep -q '"score":91.5'                "$OUT" || fails+=("numeric score not rendered")
grep -q '\["lead","eu"\]'             "$OUT" || fails+=("jsonb tags not rendered as a nested array")
grep -qx '4'                          "$OUT" || fails+=("queryValue(count active) != 4")
[[ "$wrc" -ne 0 ]]                           || fails+=("write via query() unexpectedly succeeded under read_only")
grep -qiE 'read-only transaction|SQLSTATE 25006' "$WOUT" || fails+=("write rejection was not a read-only-transaction error")

if [[ ${#fails[@]} -eq 0 ]]; then
  echo "PASS — DQL round-trip (objects, numeric, jsonb, \$1); write rejected as read-only"
else
  printf 'FAIL:\n'; printf '  - %s\n' "${fails[@]}"
  diagnose
  exit 1
fi
