#!/usr/bin/env bash
# CENARIO-014 — mhl-store-postgres: carregar de um .mh e as 4 operações
source "$(dirname "${BASH_SOURCE[0]}")/../_lib.sh"

pg_ensure
pg_wipe
pg_project

PROBE="$PROJ/wire.jsonl"
cat > main.mh <<EOF
extension store S {
    dsn: env("PG_DSN")
    table: "mhl_store"
    log: "$PROBE"
}

pipeline FourOps {
    step seed {
        S.put("run/demo/checkpoint/DocPipeline", "gate")
        S.put("session/sess-1", 7)
    }
    step overwrite {
        S.put("run/demo/checkpoint/DocPipeline", "review")
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

OUT=()
while IFS= read -r _l; do OUT+=("$_l"); done < <(grep -vE '^(session:|step:|executed )' "$L/run.out")
log "linhas: ${OUT[*]}"

pg_psql "SELECT key || ' => ' || value::text FROM mhl_store ORDER BY key" > "$L/rows.txt" 2>&1 || true
pg_psql "SELECT key FROM mhl_store ORDER BY key" > "$L/keys.txt" 2>&1 || true
log "linhas na tabela mhl_store:"; cat "$L/rows.txt" | tee -a "$CLIENT_LOG"
TABLE_EXISTS=$(pg_psql "SELECT to_regclass('public.mhl_store') IS NOT NULL")

fails=()
[[ "$TABLE_EXISTS" == "t" ]]                                   || fails+=("auto_migrate não criou a tabela mhl_store")
[[ "${OUT[0]:-}" == "review" ]]                                || fails+=("get após upsert != review (${OUT[0]:-})")
[[ "${OUT[1]:-}" == "null" ]]                                  || fails+=("get de chave ausente != null (${OUT[1]:-})")
[[ "${OUT[2]:-}" == '["run/demo/checkpoint/DocPipeline"]' ]]   || fails+=("list('run/') antes != a chave (${OUT[2]:-})")
[[ "${OUT[3]:-}" == '[]' ]]                                    || fails+=("list('run/') após delete != [] (${OUT[3]:-})")
grep -qx "session/sess-1" "$L/keys.txt"                        || fails+=("linha session/sess-1 não está na tabela")
! grep -qx "run/demo/checkpoint/DocPipeline" "$L/keys.txt"     || fails+=("linha deletada ainda na tabela")
grep -q '"ev":"init"' "$L/wire.jsonl"                          || fails+=("wire trace sem init")
grep -q '"op":"put"'  "$L/wire.jsonl"                          || fails+=("wire trace sem put")

if [[ ${#fails[@]} -eq 0 ]]; then
  ok "store-postgres carregado de um .mh; auto_migrate + get/put/delete/list + upsert verificados no Postgres (psql)"
else
  for f in "${fails[@]}"; do log "  ! $f"; done
  die "veja as verificações acima"
fi
