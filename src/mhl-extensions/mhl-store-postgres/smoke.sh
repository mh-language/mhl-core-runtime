#!/usr/bin/env bash
# smoke.sh — end-to-end check for the mhl-store-postgres extension.
#
#   make smoke          # builds bin/, brings up Postgres, runs this
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
[[ -x "$MHL" ]] || { echo "no mhl binary found — build it (cd src/mhl-runtime && make build) or set MHL=" >&2; exit 1; }
if [[ -z "${EXTENSION_SOURCE:-}" ]]; then
  [[ -x "$HERE/bin/mhl-store-postgres" ]] || { echo "bin/mhl-store-postgres missing — run: make build" >&2; exit 1; }
fi
EXTENSION_SOURCE="${EXTENSION_SOURCE:-$HERE}"

export PG_DSN="${PG_DSN:-postgres://mhl:mhl-secret-pw@localhost:5433/mhl_state?sslmode=disable}"

PROJ="$(mktemp -d "${TMPDIR:-/tmp}/mhl-store-pg-smoke.XXXXXX")"
PROBE="$PROJ/wire.jsonl"
trap 'rm -rf "$PROJ"' EXIT

cat > "$PROJ/main.mh" <<EOF
extension store S {
    dsn: env("PG_DSN")
    table: "mhl_store"
    log: "$PROBE"
}

pipeline PgRoundTrip {
    step seed {
        S.put("run/demo/checkpoint/DocPipeline", "gate")
        S.put("session/sess-1", 7)
    }
    step reads {
        var a = S.get("run/demo/checkpoint/DocPipeline")
        var miss = S.get("nope/nothing")
        log(a)
        log(miss)
    }
    step listing {
        var before = S.list("run/")
        log(before)
    }
    step cleanup {
        S.delete("run/demo/checkpoint/DocPipeline")
        S.delete("run/demo/checkpoint/DocPipeline")
        var after = S.list("run/")
        log(after)
    }
    step cas {
        var acq1 = S.put_if_absent("run/lk/lock", "A")
        var acq2 = S.put_if_absent("run/lk/lock", "B")
        var swapOK  = S.compare_and_swap("run/lk/lock", "\"A\"", "B")
        var swapBad = S.compare_and_swap("run/lk/lock", "\"A\"", "C")
        S.delete("run/lk/lock")
        log("cas acq1=\${acq1} acq2=\${acq2} swapOK=\${swapOK} swapBad=\${swapBad}")
    }
}
EOF

echo "==> project: $PROJ"
echo "==> mhl:     $MHL"
( cd "$PROJ" && "$MHL" extension install "$EXTENSION_SOURCE" )
( cd "$PROJ" && "$MHL" extension doctor )

OUT="$PROJ/run.out"
if ! ( cd "$PROJ" && "$MHL" run main.mh ) >"$OUT" 2>&1; then
  echo "--- mhl run failed ---"; cat "$OUT"
  echo "--- $PROBE ---"; cat "$PROBE" 2>/dev/null || true
  exit 1
fi

echo "--- run output ---"; cat "$OUT"
echo "--- wire trace ($PROBE) ---"; cat "$PROBE" 2>/dev/null || true

fails=()
grep -q 'gate'                            "$OUT" || fails+=("get after put did not return the stored value")
grep -qx 'null'                           "$OUT" || fails+=("get of an absent key did not print null")
grep -q 'run/demo/checkpoint/DocPipeline' "$OUT" || fails+=("list(\"run/\") before delete did not contain the key")
grep -qx '\[\]'                           "$OUT" || fails+=("list(\"run/\") after delete was not empty")
grep -q '"ev":"init"'                     "$PROBE" || fails+=("extension never initialised")
grep -q '"op":"put"'                      "$PROBE" || fails+=("no put recorded in the wire trace")
grep -q 'cas acq1=true acq2=false swapOK=true swapBad=false' "$OUT" \
  || fails+=("put_if_absent / compare_and_swap did not behave: $(grep '^cas ' "$OUT" || true)")

if [[ ${#fails[@]} -eq 0 ]]; then
  echo "PASS — get/put/delete/list + cas round-trip through PostgreSQL"
else
  printf 'FAIL:\n'; printf '  - %s\n' "${fails[@]}"
  exit 1
fi
