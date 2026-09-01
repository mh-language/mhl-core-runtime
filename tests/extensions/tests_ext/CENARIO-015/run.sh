#!/usr/bin/env bash
# CENARIO-015 — mhl-store-postgres sob concorrência: fan-out, lost update, integridade do upsert
source "$(dirname "${BASH_SOURCE[0]}")/../_lib.sh"

pg_ensure

decl() { # <logfile>
  cat <<EOF
extension store S {
    dsn: env("PG_DSN")
    table: "mhl_store"
    log: "$1"
}
EOF
}

########################################################################
# Parte A — parallel de 8 puts em chaves distintas + list → 8 linhas
########################################################################
pg_wipe
pg_project a
A_LOG="$PROJ/wire.jsonl"
{
  decl "$A_LOG"
  echo "pipeline Fan {"
  echo "    parallel Puts {"
  for k in a b c d e f g h; do
    printf '        step %s {\n            S.put("k/%s", 1)\n        }\n' "$k" "$k"
  done
  echo "    }"
  echo "    step check {"
  echo "        var ks = S.list(\"k/\")"
  echo "        log(ks)"
  echo "    }"
  echo "}"
} > main.mh
cp main.mh "$L/main-a.mh"
log "Parte A: parallel de 8 puts (store-postgres)"
"$MHL" run main.mh > "$L/run-a.out" 2>&1; RC_A=$?
cp "$A_LOG" "$L/wire-a.jsonl" 2>/dev/null || true
A_LIST=$(grep -vE '^(session:|step:|executed )' "$L/run-a.out" | tail -1)
A_ROWS=$(pg_psql "SELECT count(*) FROM mhl_store WHERE key LIKE 'k/%'")
echo "$A_ROWS" > "$L/count-a.txt"
log "Parte A: rc=$RC_A list=$A_LIST linhas_na_tabela=$A_ROWS"

########################################################################
# Parte B — read-modify-write concorrente na MESMA chave → lost update
########################################################################
gen_rmw() { # <mode: parallel|sequential>
  decl "$B_LOG"
  echo "pipeline Rmw {"
  echo "    step init {"
  echo "        S.put(\"n\", 0)"
  echo "    }"
  if [[ "$1" == parallel ]]; then
    echo "    parallel Bump {"
    for i in $(seq 1 8); do
      printf '        step s%s {\n            var v = S.get("n")\n            cmd.exec(["sh", "-c", "sleep 0.05"])\n            S.put("n", v + 1)\n        }\n' "$i"
    done
    echo "    }"
  else
    echo "    step bump {"
    for i in $(seq 1 8); do
      printf '        var v%s = S.get("n")\n        S.put("n", v%s + 1)\n' "$i" "$i"
    done
    echo "    }"
  fi
  echo "    step read {"
  echo "        var f = S.get(\"n\")"
  echo "        log(f)"
  echo "    }"
  echo "}"
}

pg_wipe
pg_project b
B_LOG="$PROJ/wire.jsonl"
gen_rmw parallel > main.mh;   cp main.mh "$L/main-b-par.mh"
log "Parte B: read-modify-write concorrente (parallel) na mesma chave"
"$MHL" run main.mh > "$L/run-b-par.out" 2>&1; RC_BP=$?
B_PAR=$(grep -vE '^(session:|step:|executed )' "$L/run-b-par.out" | tail -1)

pg_wipe
gen_rmw sequential > main.mh; cp main.mh "$L/main-b-seq.mh"
log "Parte B: mesmo read-modify-write, sequencial (controle)"
"$MHL" run main.mh > "$L/run-b-seq.out" 2>&1; RC_BS=$?
B_SEQ=$(grep -vE '^(session:|step:|executed )' "$L/run-b-seq.out" | tail -1)
cp "$B_LOG" "$L/wire-b.jsonl" 2>/dev/null || true
log "Parte B: paralelo n=$B_PAR (rc=$RC_BP) ; sequencial n=$B_SEQ (rc=$RC_BS)"

########################################################################
# Parte C — 8 puts concorrentes na MESMA chave (sem get) → 1 linha íntegra
########################################################################
pg_wipe
pg_project c
C_LOG="$PROJ/wire.jsonl"
{
  decl "$C_LOG"
  echo "pipeline Blind {"
  echo "    parallel Puts {"
  for i in $(seq 1 8); do
    printf '        step s%s {\n            S.put("c", %s)\n        }\n' "$i" "$i"
  done
  echo "    }"
  echo "    step read {"
  echo "        var v = S.get(\"c\")"
  echo "        log(v)"
  echo "    }"
  echo "}"
} > main.mh
cp main.mh "$L/main-c.mh"
log "Parte C: 8 puts concorrentes na mesma chave (integridade do upsert)"
"$MHL" run main.mh > "$L/run-c.out" 2>&1; RC_C=$?
C_READ=$(grep -vE '^(session:|step:|executed )' "$L/run-c.out" | tail -1)
C_COUNT=$(pg_psql "SELECT count(*) FROM mhl_store WHERE key = 'c'")
C_VAL=$(pg_psql "SELECT value::text FROM mhl_store WHERE key = 'c'")
echo "count=$C_COUNT value=$C_VAL read=$C_READ" > "$L/row-c.txt"
log "Parte C: rc=$RC_C linhas('c')=$C_COUNT value=$C_VAL get=$C_READ"

########################################################################
fails=()
[[ "$RC_A" == "0" ]]                        || fails+=("A: mhl run falhou (rc=$RC_A)")
[[ "$A_LIST" == '["k/a","k/b","k/c","k/d","k/e","k/f","k/g","k/h"]' ]] \
                                           || fails+=("A: list != as 8 chaves ($A_LIST)")
[[ "${A_ROWS:-0}" == "8" ]]                 || fails+=("A: $A_ROWS linhas na tabela (esperado 8 — perda sob concorrência)")
[[ "$RC_BP" == "0" && "$RC_BS" == "0" ]]    || fails+=("B: mhl run falhou (par=$RC_BP seq=$RC_BS)")
[[ "$B_SEQ" == "8" ]]                       || fails+=("B: sequencial n=$B_SEQ != 8")
[[ "$B_PAR" =~ ^[0-9]+$ && "$B_PAR" -lt 8 ]] \
                                           || fails+=("B: paralelo n=$B_PAR não é < 8 (esperava-se lost update; contrato store v1 sem CAS)")
[[ "$RC_C" == "0" ]]                        || fails+=("C: mhl run falhou (rc=$RC_C)")
[[ "${C_COUNT:-0}" == "1" ]]               || fails+=("C: $C_COUNT linhas para a chave 'c' (esperado exatamente 1 — upsert)")
[[ "$C_VAL" =~ ^[1-8]$ ]]                  || fails+=("C: value da chave 'c' = '$C_VAL' fora de [1,8] (corrupção?)")

if [[ ${#fails[@]} -eq 0 ]]; then
  ok "A: 8 puts paralelos sem perda (8 linhas); B: RMW concorrente perde updates (par=$B_PAR < seq=8, limite do contrato v1); C: upsert concorrente = 1 linha íntegra (value=$C_VAL)"
else
  for f in "${fails[@]}"; do log "  ! $f"; done
  die "veja as verificações acima"
fi
