#!/usr/bin/env bash
# CENARIO-002 — As quatro operações do contrato `store`
source "$(dirname "${BASH_SOURCE[0]}")/../_lib.sh"

ensure_env
new_project
STORE="$PROJ/store"; PROBE_LOG="$PROJ/probe.jsonl"; mkdir -p "$STORE"

cat > main.mh <<EOF
extension store S {
    dir: "$STORE"
    log: "$PROBE_LOG"
}

pipeline FourOps {
    step seed {
        S.put("a/1", 10)
        S.put("b/1", 20)
    }
    step gets {
        var a = S.get("a/1")
        var b = S.get("b/1")
        var z = S.get("z/9")
        log(a)
        log(b)
        log(z)
    }
    step list_before {
        var la = S.list("a/")
        log(la)
    }
    step deletes {
        S.delete("a/1")
        S.delete("nao/existe")
    }
    step list_after {
        var all = S.list("")
        var la2 = S.list("a/")
        log(all)
        log(la2)
    }
}
EOF

log "mhl run main.mh"
"$MHL" run main.mh > "$L/run.out" 2>&1 || { cat "$L/run.out" >> "$CLIENT_LOG"; die "mhl run falhou"; }
cat "$L/run.out" | tee -a "$CLIENT_LOG"
cp "$PROBE_LOG" "$L/probe.jsonl" 2>/dev/null || true
find "$STORE" -type f | sed "s|$STORE/||;s|\.json$||" | sort > "$L/store-tree.txt"

# saída esperada, em ordem: 10 / 20 / null / ["a/1"] / ["b/1"] / []
OUT=()
while IFS= read -r _line; do OUT+=("$_line"); done < <(grep -vE '^(session:|step:|executed )' "$L/run.out")
log "linhas de saída: ${OUT[*]}"

fails=()
[[ "${OUT[0]:-}" == "10" ]]        || fails+=("get a/1 != 10 (${OUT[0]:-})")
[[ "${OUT[1]:-}" == "20" ]]        || fails+=("get b/1 != 20 (${OUT[1]:-})")
[[ "${OUT[2]:-}" == "null" ]]      || fails+=("get z/9 != null (${OUT[2]:-})")
[[ "${OUT[3]:-}" == '["a/1"]' ]]   || fails+=("list('a/') antes != [\"a/1\"] (${OUT[3]:-})")
[[ "${OUT[4]:-}" == '["b/1"]' ]]   || fails+=("list('') depois != [\"b/1\"] (${OUT[4]:-})")
[[ "${OUT[5]:-}" == '[]' ]]        || fails+=("list('a/') depois != [] (${OUT[5]:-})")
grep -qx "b/1" "$L/store-tree.txt" && ! grep -qx "a/1" "$L/store-tree.txt" || fails+=("árvore do store inesperada: $(tr '\n' ' ' < "$L/store-tree.txt")")

if [[ ${#fails[@]} -eq 0 ]]; then
  ok "get/put/delete/list + bordas (miss→null, delete ausente no-op, list por prefixo)"
else
  for f in "${fails[@]}"; do log "  ! $f"; done
  die "veja as verificações acima"
fi
