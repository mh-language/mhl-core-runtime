#!/usr/bin/env bash
# CENARIO-017 — mhl-sql-postgres: consultas livres (DQL) de um .mh
source "$(dirname "${BASH_SOURCE[0]}")/../_lib.sh"

sqlpg_ensure
sqlpg_project

BEFORE=$(sqlpg_psql "SELECT count(*) FROM people")
PROBE="$PROJ/wire.jsonl"
cat > main.mh <<EOF
extension sql Db {
    dsn: env("SQL_PG_DSN")
    read_only: true
    log: "$PROBE"
}

pipeline DQL {
    step rows {
        var acme = Db.query("SELECT name, org, score, tags FROM people WHERE org = \$1 ORDER BY name", "acme")
        log(acme)
    }
    step row_hit {
        var top = Db.queryRow("SELECT name FROM people ORDER BY score DESC LIMIT 1")
        log(top)
    }
    step row_miss {
        var none = Db.queryRow("SELECT 1 AS x WHERE false")
        log(none)
    }
    step scalar {
        var n = Db.queryValue("SELECT count(*) FROM people WHERE active")
        log(n)
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
AFTER=$(sqlpg_psql "SELECT count(*) FROM people")
echo "antes=$BEFORE depois=$AFTER" > "$L/count.txt"
log "linhas: rows=${OUT[0]:-} | row_hit=${OUT[1]:-} | row_miss=${OUT[2]:-} | scalar=${OUT[3]:-}"
log "people count antes=$BEFORE depois=$AFTER"

fails=()
[[ "${OUT[0]:-}" == '[{"name":"Ana","org":"acme","score":91.5,"tags":["lead","eu"]},'* ]] \
                                                  || fails+=("query() não devolveu objetos com os tipos esperados: ${OUT[0]:-}")
python3 -c 'import json,sys; a=json.loads(sys.argv[1]); sys.exit(0 if len(a)==3 and a[0]["name"]=="Ana" and a[0]["tags"]==["lead","eu"] and a[0]["score"]==91.5 else 1)' "${OUT[0]:-[]}" \
                                                  || fails+=("query() linhas/tipos inesperados")
[[ "${OUT[1]:-}" == '{"name":"Ana"}' ]]           || fails+=("queryRow(hit) != {\"name\":\"Ana\"} (${OUT[1]:-})")
[[ "${OUT[2]:-}" == "null" ]]                      || fails+=("queryRow(miss) != null (${OUT[2]:-})")
[[ "${OUT[3]:-}" == "4" ]]                         || fails+=("queryValue(count active) != 4 (${OUT[3]:-})")
[[ "$BEFORE" == "5" && "$AFTER" == "5" ]]          || fails+=("contagem de people mudou ($BEFORE -> $AFTER)")
grep -q '"op":"query"' "$L/wire.jsonl"             || fails+=("wire trace sem op query")
grep -q '"sql_head":"SELECT' "$L/wire.jsonl"       || fails+=("wire trace sem sql_head")
grep -q '"acme"' "$L/wire.jsonl" && fails+=("wire trace vazou um valor de argumento") || true

if [[ ${#fails[@]} -eq 0 ]]; then
  ok "sql-postgres carregado de um .mh; query/queryRow/queryValue + tipos (numeric/jsonb) + \$1 verificados; nada escrito"
else
  for f in "${fails[@]}"; do log "  ! $f"; done
  die "veja as verificações acima"
fi
