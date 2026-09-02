#!/usr/bin/env bash
# CENARIO-011 — mhl-store-s3: carregar de um .mh e as 4 operações contra o MinIO
source "$(dirname "${BASH_SOURCE[0]}")/../_lib.sh"

s3_ensure
PFX="c011/"
s3_wipe "$PFX"
s3_project

PROBE="$PROJ/wire.jsonl"
cat > main.mh <<EOF
extension store S {
    bucket: env("S3_BUCKET")
    endpoint: env("S3_ENDPOINT")
    region: "us-east-1"
    access_key_id: env("AWS_ACCESS_KEY_ID")
    secret_access_key: env("AWS_SECRET_ACCESS_KEY")
    prefix: "$PFX"
    log: "$PROBE"
}

pipeline FourOps {
    step seed {
        S.put("run/demo/checkpoint/DocPipeline", "gate")
        S.put("session/sess-1", 7)
    }
    step reads {
        var a = S.get("run/demo/checkpoint/DocPipeline")
        var miss = S.get("nope/nada")
        log(a)
        log(miss)
    }
    step list_before {
        var lb = S.list("run/")
        log(lb)
    }
    step deletes {
        S.delete("run/demo/checkpoint/DocPipeline")
        S.delete("run/demo/checkpoint/DocPipeline")
    }
    step list_after {
        var la = S.list("run/")
        log(la)
    }
}
EOF
cp main.mh "$L/main.mh"

log "mhl extension doctor"
"$MHL" extension doctor > "$L/doctor.out" 2>&1 || { cat "$L/doctor.out" >> "$CLIENT_LOG"; die "extension doctor != 0"; }

log "mhl run main.mh"
"$MHL" run main.mh > "$L/run.out" 2>&1 || { cat "$L/run.out" >> "$CLIENT_LOG"; die "mhl run falhou"; }
cat "$L/run.out" | tee -a "$CLIENT_LOG"
cp "$PROBE" "$L/wire.jsonl" 2>/dev/null || true

# linhas não-diagnósticas; esperado: gate / null / ["run/demo/checkpoint/DocPipeline"] / []
OUT=()
while IFS= read -r _l; do OUT+=("$_l"); done < <(grep -vE '^(session:|step:|executed )' "$L/run.out")
log "linhas: ${OUT[*]}"

# objetos que restaram no bucket sob o prefixo, via mc (rede do compose).
# `mc ls local/<bucket>/<prefixo>/` imprime as chaves RELATIVAS ao prefixo.
s3_mc "mc ls --recursive local/$S3_BUCKET/$PFX" > "$L/mc-ls.txt" 2>&1 || true
awk '{print $NF}' "$L/mc-ls.txt" | grep '\.json$' | sort > "$L/objects.txt" || true
log "objetos no bucket sob $PFX:"; cat "$L/objects.txt" | tee -a "$CLIENT_LOG"

fails=()
[[ "${OUT[0]:-}" == "gate" ]]                                  || fails+=("get após put != gate (${OUT[0]:-})")
[[ "${OUT[1]:-}" == "null" ]]                                  || fails+=("get de chave ausente != null (${OUT[1]:-})")
[[ "${OUT[2]:-}" == '["run/demo/checkpoint/DocPipeline"]' ]]   || fails+=("list('run/') antes != a chave (${OUT[2]:-})")
[[ "${OUT[3]:-}" == '[]' ]]                                    || fails+=("list('run/') após delete != [] (${OUT[3]:-})")
grep -q "^session/sess-1.json$" "$L/objects.txt"               || fails+=("objeto session/sess-1.json não está no bucket")
! grep -q "^run/demo/checkpoint/DocPipeline.json$" "$L/objects.txt" \
                                                              || fails+=("objeto deletado ainda no bucket")
grep -q '"ev":"init"' "$L/wire.jsonl"                          || fails+=("wire trace sem init")
grep -q '"op":"put"'  "$L/wire.jsonl"                          || fails+=("wire trace sem put")

if [[ ${#fails[@]} -eq 0 ]]; then
  ok "store-s3 carregado de um .mh; get/put/delete/list round-trip verificado no MinIO (mc ls)"
else
  for f in "${fails[@]}"; do log "  ! $f"; done
  die "veja as verificações acima"
fi
