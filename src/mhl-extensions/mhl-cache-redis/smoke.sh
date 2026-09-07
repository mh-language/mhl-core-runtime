#!/usr/bin/env bash
# smoke.sh — end-to-end check for the mhl-cache-redis extension.
#
#   make smoke          # builds bin/, brings up Redis, runs this
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
  [[ -x "$HERE/bin/mhl-cache-redis" ]] || { echo "bin/mhl-cache-redis missing — run: make build" >&2; exit 1; }
fi
EXTENSION_SOURCE="${EXTENSION_SOURCE:-$HERE}"

export REDIS_URL="${REDIS_URL:-redis://localhost:6380/0}"

PROJ="$(mktemp -d "${TMPDIR:-/tmp}/mhl-cache-redis-smoke.XXXXXX")"
trap 'rm -rf "$PROJ"' EXIT

cat > "$PROJ/main.mh" <<EOF
extension cache C {
    url: env("REDIS_URL")
    key_prefix: "smoke:$(basename "$PROJ"):"
    ttl: "1h"
    log: "$PROJ/wire.jsonl"
}

pipeline Cache {
    step miss {
        var a = C.get("user:42")
        log("miss=\${a}")
    }
    step put {
        C.set("user:42", { "name": "Ana", "score": 91 })
        var b = C.get("user:42")
        log("value=\${b}")
        var present = C.has("user:42")
        log("present=\${present}")
    }
    step counter {
        var n1 = C.incr("calls")
        var n2 = C.incrBy("calls", 4)
        log("incr=\${n1}")
        log("incrBy=\${n2}")
    }
    step expiry {
        C.set("short", "bye", "2s")
        var t = C.ttl("short")
        log("ttl=\${t}")
    }
    step gone {
        C.delete("user:42")
        var after = C.has("user:42")
        log("after=\${after}")
        C.delete("calls")
        C.delete("short")
    }
}
EOF

echo "==> project: $PROJ"
( cd "$PROJ" && "$MHL" extension install "$EXTENSION_SOURCE" )
( cd "$PROJ" && "$MHL" extension doctor )

OUT="$PROJ/run.out"
if ! ( cd "$PROJ" && "$MHL" run main.mh ) >"$OUT" 2>&1; then
  echo "--- mhl run failed ---"; cat "$OUT"; exit 1
fi
echo "--- run output ---"; cat "$OUT"
echo "--- wire trace ---"; cat "$PROJ/wire.jsonl" 2>/dev/null || true

# Match labelled results so runtime diagnostics do not shift positional checks.
fails=()
grep -qx 'miss=null' "$OUT"                         || fails+=("get miss != null")
grep -qx 'value={"name":"Ana","score":91}' "$OUT" || fails+=("get after set != the object")
grep -qx 'present=true' "$OUT"                      || fails+=("has() != true")
grep -qx 'incr=1' "$OUT"                            || fails+=("incr() != 1")
grep -qx 'incrBy=5' "$OUT"                          || fails+=("incrBy(4) != 5")
grep -qxE 'ttl=(1|2)' "$OUT"                        || fails+=("ttl(short) not ~2s")
grep -qx 'after=false' "$OUT"                       || fails+=("has() after delete != false")
grep -q '"op":"incr"' "$PROJ/wire.jsonl"              || fails+=("wire trace missing incr")

if [[ ${#fails[@]} -eq 0 ]]; then
  echo "PASS — get/set/has/delete + JSON value + incr/incrBy + ttl through Redis"
else
  printf 'FAIL:\n'; printf '  - %s\n' "${fails[@]}"
  exit 1
fi
