#!/usr/bin/env bash
# CENARIO-018 — mhl-sql-postgres: "DQL only" (read-only) e guardas
source "$(dirname "${BASH_SOURCE[0]}")/../_lib.sh"

sqlpg_ensure
sqlpg_project

C0=$(sqlpg_psql "SELECT count(*) FROM people")
: > "$L/counts.txt"; echo "inicial=$C0" >> "$L/counts.txt"

# gera um main.mh com uma declaração + um único step
mk() { # <read_only> <max_rows-ou-vazio> <corpo-do-step>
  { echo "extension sql Db {"
    echo "    dsn: env(\"SQL_PG_DSN\")"
    echo "    read_only: $1"
    [[ -n "$2" ]] && echo "    max_rows: $2"
    echo "}"
    echo "pipeline P {"
    echo "    step s {"
    printf '%s\n' "$3"
    echo "    }"
    echo "}"
  } > main.mh
}

run_expect_fail() { # <tag> <grep-regex>
  local tag="$1" re="$2" rc
  cp main.mh "$L/main-$tag.mh"
  "$MHL" run main.mh > "$L/run-$tag.out" 2>&1; rc=$?
  grep -vE '^step:' "$L/run-$tag.out" | tee -a "$CLIENT_LOG" >/dev/null
  local cnt; cnt=$(sqlpg_psql "SELECT count(*) FROM people")
  echo "$tag: rc=$rc count=$cnt" >> "$L/counts.txt"
  log "$tag: rc=$rc people=$cnt"
  [[ "$rc" -ne 0 ]] || { FAILS+=("$tag: mhl run devia falhar (rc=0)"); return; }
  grep -qiE "$re" "$L/run-$tag.out" || FAILS+=("$tag: erro não casou /$re/")
}

FAILS=()

# A — INSERT via query() sob read_only:true
mk true "" '        var r = Db.query("INSERT INTO people(name, org) VALUES ('"'"'mallory'"'"', '"'"'x'"'"') RETURNING id")
        log(r)'
run_expect_fail a 'read-only transaction|SQLSTATE 25006'

# B — exec() sob read_only:true
mk true "" '        var r = Db.exec("INSERT INTO people(name, org) VALUES ('"'"'m'"'"', '"'"'x'"'"')")
        log(r)'
run_expect_fail b 'exec is disabled'

# C — dois statements num texto só
mk true "" '        var r = Db.query("SELECT 1; DROP TABLE people")
        log(r)'
run_expect_fail c 'multiple commands|cannot insert multiple|prepared statement|syntax'

# D — max_rows
mk true 2 '        var r = Db.query("SELECT * FROM people")
        log(r)'
run_expect_fail d 'max_rows=2'

# E — controle: read_only:false permite exec, e limpamos
mk false "" '        var ins = Db.exec("INSERT INTO people(name, org) VALUES ('"'"'tmp'"'"', '"'"'tmp'"'"')")
        log(ins)
        var del = Db.exec("DELETE FROM people WHERE org = '"'"'tmp'"'"'")
        log(del)'
cp main.mh "$L/main-e.mh"
"$MHL" run main.mh > "$L/run-e.out" 2>&1; RC_E=$?
grep -vE '^step:' "$L/run-e.out" | tee -a "$CLIENT_LOG" >/dev/null
CE=$(sqlpg_psql "SELECT count(*) FROM people")
echo "e: rc=$RC_E count=$CE" >> "$L/counts.txt"
log "e (controle read_only:false): rc=$RC_E people=$CE"
E_LINES=$(grep -vE '^(session:|step:|executed )' "$L/run-e.out")
[[ "$RC_E" == "0" ]]                       || FAILS+=("e: exec com read_only:false falhou (rc=$RC_E)")
grep -qx "1" <<<"$E_LINES"                 || FAILS+=("e: exec INSERT não retornou 1")
[[ "$CE" == "$C0" ]]                       || FAILS+=("e: contagem não voltou ao inicial ($C0 -> $CE)")

# nenhuma escrita deve ter passado nas partes A–D
CD=$(sqlpg_psql "SELECT count(*) FROM people")
[[ "$CD" == "$C0" ]]                       || FAILS+=("A-D: people mudou de $C0 para $CD (alguma escrita passou!)")

if [[ ${#FAILS[@]} -eq 0 ]]; then
  ok "read_only recusa INSERT via query (25006) e desabilita exec; ;-stacking recusado; max_rows atua; read_only:false permite exec — tabela intacta"
else
  for f in "${FAILS[@]}"; do log "  ! $f"; done
  die "veja as verificações acima"
fi
