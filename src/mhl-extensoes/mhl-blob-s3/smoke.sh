#!/usr/bin/env bash
# smoke.sh — end-to-end check for the mhl-blob-s3 extension against local MinIO.
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
[[ -x "$HERE/bin/mhl-blob-s3" ]] || { echo "bin/mhl-blob-s3 missing — run: make build" >&2; exit 1; }

export AWS_ACCESS_KEY_ID="${AWS_ACCESS_KEY_ID:-mhl}"
export AWS_SECRET_ACCESS_KEY="${AWS_SECRET_ACCESS_KEY:-mhl-secret-key}"
export S3_BUCKET="${S3_BUCKET:-mhl-blob}"
export S3_ENDPOINT="${S3_ENDPOINT:-http://localhost:9000}"

PROJ="$(mktemp -d "${TMPDIR:-/tmp}/mhl-blob-s3-smoke.XXXXXX")"
PROBE="$PROJ/blob-s3.jsonl"
trap 'rm -rf "$PROJ"' EXIT

cat > "$PROJ/main.mh" <<EOF
extension blob Files {
    provider: "s3"
    bucket: env("S3_BUCKET")
    endpoint: env("S3_ENDPOINT")
    region: "us-east-1"
    access_key_id: env("AWS_ACCESS_KEY_ID")
    secret_access_key: env("AWS_SECRET_ACCESS_KEY")
    prefix: "smoke/"
    log: "$PROBE"
}

pipeline BlobRoundTrip {
    step seed {
        Files.put("reports/q3.csv", "a,b\n1,2\n", "text/csv")
        Files.put("reports/notes.txt", "hi", "text/plain")
    }
    step reads {
        log(Files.get("reports/q3.csv"))
        log(Files.get("nope/missing"))
        log(Files.exists("reports/q3.csv"))
        var m = Files.head("reports/q3.csv")
        log("size=\${m.size} etag_len=\${m.etag.size()}")
    }
    step listing {
        var l = Files.list("reports/", "")
        log("keys=\${l.keys.size()}")
        var folders = Files.list("", "/")
        log("folders=\${folders.common_prefixes}")
    }
    step copy_and_clean {
        Files.copy("reports/q3.csv", "archive/q3.csv")
        log(Files.exists("archive/q3.csv"))
        Files.delete("reports/q3.csv")
        Files.delete("reports/q3.csv")
        Files.delete("reports/notes.txt")
        Files.delete("archive/q3.csv")
        log(Files.list("reports/", "").keys)
    }
    step presign {
        Files.put("dl/file.bin", "payload", "application/octet-stream")
        var url = Files.presign_get("dl/file.bin", "10m")
        Files.delete("dl/file.bin")
        log("presigned=\${url.starts_with(\"$S3_ENDPOINT\")} sig=\${url.contains(\"X-Amz-Signature=\")}")
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
grep -qF 'a,b'                     "$OUT" || fails+=("get after put did not return the body")
grep -qx 'null'                    "$OUT" || fails+=("get of an absent key did not print null")
grep -q  'size=8 etag_len='        "$OUT" || fails+=("head did not report size 8")
grep -q  'keys=2'                  "$OUT" || fails+=("list(reports/) did not see 2 keys")
grep -q  'folders=\["reports/"\]'  "$OUT" || fails+=("list \"\" delimiter \"/\" missing the reports/ common prefix: $(grep '^folders' "$OUT" || true)")
grep -qx 'true'                    "$OUT" || fails+=("exists()/copy target not confirmed")
grep -qx '\[\]'                    "$OUT" || fails+=("list(reports/) after cleanup not empty")
grep -q  'presigned=true sig=true' "$OUT" || fails+=("presign_get URL malformed: $(grep '^presigned' "$OUT" || true)")
grep -q  '"op":"copy"'             "$PROBE" || fails+=("no copy recorded in the wire trace")

if [[ ${#fails[@]} -eq 0 ]]; then
  echo "PASS — put/get/head/exists/list(+delimiter)/copy/delete/presign through S3 (MinIO)"
else
  printf 'FAIL:\n'; printf '  - %s\n' "${fails[@]}"
  exit 1
fi
