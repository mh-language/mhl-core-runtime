#!/usr/bin/env bash
# CENARIO-019 — mhl-sql-postgres: implantar DDL com execScript (transação, tudo-ou-nada)
source "$(dirname "${BASH_SOURCE[0]}")/../_lib.sh"

sqlpg_ensure
sqlpg_project

# idempotência: limpa o que este cenário cria
sqlpg_psql "DROP TABLE IF EXISTS audit, b_table CASCADE" >/dev/null 2>&1 || true
: > "$L/schema.txt"

mk() { # <read_only> <corpo-do-step>
  { echo "extension sql Db {"
    echo "    dsn: env(\"SQL_PG_DSN\")"
    echo "    read_only: $1"
    echo "}"
    echo "pipeline P {"
    echo "    step s {"
    printf '%s\n' "$2"
    echo "    }"
    echo "}"
  } > main.mh
}

FAILS=()

########################################################################
# A — execScript aplica um script DDL multi-statement (read_only:false)
########################################################################
mk false '        var n = Db.execScript("
            CREATE TABLE audit (
                id bigserial PRIMARY KEY,
                at timestamptz NOT NULL DEFAULT now(),
                actor text NOT NULL,
                action text NOT NULL
            );
            CREATE INDEX audit_at_idx ON audit (at);
            INSERT INTO audit (actor, action) VALUES ('"'"'ana'"'"', '"'"'login'"'"'), ('"'"'bruno'"'"', '"'"'deploy'"'"');
        ")
        log(n)'
cp main.mh "$L/main-a.mh"
"$MHL" run main.mh > "$L/run-a.out" 2>&1; RC_A=$?
grep -vE '^step:' "$L/run-a.out" | tee -a "$CLIENT_LOG" >/dev/null
A_TABLE=$(sqlpg_psql "SELECT to_regclass('public.audit') IS NOT NULL")
A_INDEX=$(sqlpg_psql "SELECT count(*) FROM pg_indexes WHERE indexname = 'audit_at_idx'")
A_ROWS=$(sqlpg_psql "SELECT count(*) FROM audit" 2>/dev/null || echo "-")
echo "A: rc=$RC_A table=$A_TABLE index=$A_INDEX rows=$A_ROWS" >> "$L/schema.txt"
log "A: rc=$RC_A audit_existe=$A_TABLE indice=$A_INDEX linhas=$A_ROWS"
[[ "$RC_A" == "0" ]]        || FAILS+=("A: mhl run falhou (rc=$RC_A)")
[[ "$A_TABLE" == "t" ]]     || FAILS+=("A: tabela audit não foi criada")
[[ "$A_INDEX" == "1" ]]     || FAILS+=("A: índice audit_at_idx não existe")
[[ "$A_ROWS" == "2" ]]      || FAILS+=("A: audit tem $A_ROWS linhas (esperado 2)")

########################################################################
# B — script que falha no meio: rollback total
########################################################################
mk false '        var n = Db.execScript("
            CREATE TABLE b_table (id int PRIMARY KEY);
            INSERT INTO b_table (id, coluna_inexistente) VALUES (1, '"'"'x'"'"');
        ")
        log(n)'
cp main.mh "$L/main-b.mh"
"$MHL" run main.mh > "$L/run-b.out" 2>&1; RC_B=$?
grep -vE '^step:' "$L/run-b.out" | tee -a "$CLIENT_LOG" >/dev/null
B_TABLE=$(sqlpg_psql "SELECT to_regclass('public.b_table') IS NOT NULL")
echo "B: rc=$RC_B b_table=$B_TABLE" >> "$L/schema.txt"
log "B: rc=$RC_B b_table_existe=$B_TABLE (esperado f — rollback)"
[[ "$RC_B" != "0" ]]        || FAILS+=("B: mhl run devia falhar")
[[ "$B_TABLE" == "f" ]]     || FAILS+=("B: b_table foi criada — rollback não aconteceu")

########################################################################
# C — execScript bloqueado com read_only:true
########################################################################
mk true '        var n = Db.execScript("CREATE TABLE nope (id int)")
        log(n)'
cp main.mh "$L/main-c.mh"
"$MHL" run main.mh > "$L/run-c.out" 2>&1; RC_C=$?
grep -vE '^step:' "$L/run-c.out" | tee -a "$CLIENT_LOG" >/dev/null
C_TABLE=$(sqlpg_psql "SELECT to_regclass('public.nope') IS NOT NULL")
echo "C: rc=$RC_C nope=$C_TABLE" >> "$L/schema.txt"
log "C: rc=$RC_C (esperado != 0) nope_existe=$C_TABLE"
[[ "$RC_C" != "0" ]]                                  || FAILS+=("C: mhl run devia falhar com read_only:true")
grep -qi 'execScript is disabled' "$L/run-c.out"      || FAILS+=("C: erro não foi 'execScript is disabled'")
[[ "$C_TABLE" == "f" ]]                               || FAILS+=("C: tabela nope foi criada mesmo em read_only")

# limpa
sqlpg_psql "DROP TABLE IF EXISTS audit, b_table, nope CASCADE" >/dev/null 2>&1 || true

if [[ ${#FAILS[@]} -eq 0 ]]; then
  ok "execScript aplica DDL multi-statement (tabela+índice+2 linhas); falha no meio faz rollback total; bloqueado em read_only:true"
else
  for f in "${FAILS[@]}"; do log "  ! $f"; done
  die "veja as verificações acima"
fi
