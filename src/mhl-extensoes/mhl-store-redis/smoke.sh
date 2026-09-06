#!/usr/bin/env bash
# smoke.sh — end-to-end check for the mhl-store-redis extension.
#
#   make smoke          # builds bin/, brings up Redis, runs this
#   ./smoke.sh          # assumes `make build` + `make up` already ran
#
# Override the runtime with:  MHL=/path/to/mhl ./smoke.sh
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
[[ -x "$HERE/bin/mhl-store-redis" ]] || { echo "bin/mhl-store-redis missing — run: make build" >&2; exit 1; }

export REDIS_URL="${REDIS_URL:-redis://localhost:6381/0}"

PROJ="$(mktemp -d "${TMPDIR:-/tmp}/mhl-store-redis-smoke.XXXXXX")"
PROBE="$PROJ/wire.jsonl"
trap 'rm -rf "$PROJ"' EXIT

cat > "$PROJ/main.mh" <<EOF
extension store S {
    url: env("REDIS_URL")
    key_prefix: "smoke/"
    log: "$PROBE"
}

pipeline StoreRoundTrip {
    step seed {
        S.put("run/demo/checkpoint/DocPipeline", "gate")
        S.put("session/sess-1", 7)
    }
    step reads {
        log(S.get("run/demo/checkpoint/DocPipeline"))
        log(S.get("nope/nothing"))
    }
    step listing {
        log(S.list("run/"))
    }
    step cleanup {
        S.delete("run/demo/checkpoint/DocPipeline")
        S.delete("run/demo/checkpoint/DocPipeline")
        log(S.list("run/"))
    }
    step cas {
        var acq1 = S.put_if_absent("run/lk/lock", "A")
        var acq2 = S.put_if_absent("run/lk/lock", "B")
        var swapOK  = S.compare_and_swap("run/lk/lock", "\"A\"", "B")
        var swapBad = S.compare_and_swap("run/lk/lock", "\"A\"", "C")
        S.delete("run/lk/lock")
        S.delete("session/sess-1")
        log("cas acq1=\${acq1} acq2=\${acq2} swapOK=\${swapOK} swapBad=\${swapBad}")
    }
}
EOF

echo "==> project: $PROJ"
echo "==> mhl:     $MHL"
( cd "$PROJ" && "$MHL" extension install "$HERE" )
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
  echo "PASS — get/put/delete/list + cas round-trip through Redis"
else
  printf 'FAIL:\n'; printf '  - %s\n' "${fails[@]}"
  exit 1
fi
