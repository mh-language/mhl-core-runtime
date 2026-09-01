#!/usr/bin/env bash
# smoke.sh — end-to-end check for the mhl-store-s3 extension against local MinIO.
#
#   make smoke          # builds bin/, brings up MinIO, runs this
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
[[ -x "$MHL" ]] || { echo "no mhl binary found — build it (cd src/mhl-runtime && make build) or set MHL=" >&2; exit 1; }

[[ -x "$HERE/bin/mhl-store-s3" ]] || { echo "bin/mhl-store-s3 missing — run: make build" >&2; exit 1; }

export AWS_ACCESS_KEY_ID="${AWS_ACCESS_KEY_ID:-mhl}"
export AWS_SECRET_ACCESS_KEY="${AWS_SECRET_ACCESS_KEY:-mhl-secret-key}"
export S3_BUCKET="${S3_BUCKET:-mhl-state}"
export S3_ENDPOINT="${S3_ENDPOINT:-http://localhost:9000}"

PROJ="$(mktemp -d "${TMPDIR:-/tmp}/mhl-store-s3-smoke.XXXXXX")"
PROBE="$PROJ/store-s3.jsonl"
trap 'rm -rf "$PROJ"' EXIT

cat > "$PROJ/main.mh" <<EOF
extension store S {
    bucket: env("S3_BUCKET")
    endpoint: env("S3_ENDPOINT")
    region: "us-east-1"
    access_key_id: env("AWS_ACCESS_KEY_ID")
    secret_access_key: env("AWS_SECRET_ACCESS_KEY")
    prefix: "smoke/"
    log: "$PROBE"
}

pipeline S3RoundTrip {
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
grep -q 'gate'                                  "$OUT" || fails+=("get after put did not return the stored value")
grep -qx 'null'                                 "$OUT" || fails+=("get of an absent key did not print null")
grep -q 'run/demo/checkpoint/DocPipeline'       "$OUT" || fails+=("list(\"run/\") before delete did not contain the key")
grep -qx '\[\]'                                 "$OUT" || fails+=("list(\"run/\") after delete was not empty")
grep -q '"ev":"init"'                           "$PROBE" || fails+=("extension never initialised")
grep -q '"op":"put"'                            "$PROBE" || fails+=("no put recorded in the wire trace")

if [[ ${#fails[@]} -eq 0 ]]; then
  echo "PASS — get/put/delete/list round-trip through S3 (MinIO)"
else
  printf 'FAIL:\n'; printf '  - %s\n' "${fails[@]}"
  exit 1
fi
