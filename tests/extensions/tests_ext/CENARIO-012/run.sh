#!/usr/bin/env bash
# CENARIO-012 — mhl-store-s3 sob concorrência: fan-out sem perda + lost update (sem CAS)
source "$(dirname "${BASH_SOURCE[0]}")/../_lib.sh"

s3_ensure

decl() { # <prefix> <logfile>
  cat <<EOF
extension store S {
    bucket: env("S3_BUCKET")
    endpoint: env("S3_ENDPOINT")
    region: "us-east-1"
    access_key_id: env("AWS_ACCESS_KEY_ID")
    secret_access_key: env("AWS_SECRET_ACCESS_KEY")
    prefix: "$1"
    log: "$2"
}
EOF
}

########################################################################
# Parte A — parallel de 8 puts em chaves distintas + list  → 8 objetos
########################################################################
PFX_A="c012a/"; s3_wipe "$PFX_A"
s3_project a
A_LOG="$PROJ/wire.jsonl"
{
  decl "$PFX_A" "$A_LOG"
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
log "Parte A: parallel de 8 puts (store-s3 -> MinIO)"
"$MHL" run main.mh > "$L/run-a.out" 2>&1; RC_A=$?
cp "$A_LOG" "$L/wire-a.jsonl" 2>/dev/null || true
A_LIST=$(grep -vE '^(session:|step:|executed )' "$L/run-a.out" | tail -1)
s3_mc "mc ls --recursive local/$S3_BUCKET/${PFX_A}k/" > "$L/mc-a.txt" 2>&1 || true
A_OBJS=$(grep -c '\.json$' "$L/mc-a.txt" 2>/dev/null || echo 0)
log "Parte A: rc=$RC_A list=$A_LIST objetos_no_bucket=$A_OBJS"
grep -vE '^step:' "$L/run-a.out" | tee -a "$CLIENT_LOG" >/dev/null

########################################################################
# Parte B — read-modify-write concorrente na MESMA chave → lost update
########################################################################
gen_rmw() { # <mode: parallel|sequential>  (prefix c012b/<mode>/)
  decl "c012b/$1/" "$B_LOG"
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

s3_wipe "c012b/"
s3_project b
B_LOG="$PROJ/wire.jsonl"

gen_rmw parallel > main.mh;   cp main.mh "$L/main-b-par.mh"
log "Parte B: read-modify-write concorrente (parallel) na mesma chave"
"$MHL" run main.mh > "$L/run-b-par.out" 2>&1; RC_BP=$?
B_PAR=$(grep -vE '^(session:|step:|executed )' "$L/run-b-par.out" | tail -1)

gen_rmw sequential > main.mh; cp main.mh "$L/main-b-seq.mh"
log "Parte B: mesmo read-modify-write, sequencial (controle)"
"$MHL" run main.mh > "$L/run-b-seq.out" 2>&1; RC_BS=$?
B_SEQ=$(grep -vE '^(session:|step:|executed )' "$L/run-b-seq.out" | tail -1)
cp "$B_LOG" "$L/wire-b.jsonl" 2>/dev/null || true
log "Parte B: paralelo n=$B_PAR (rc=$RC_BP) ; sequencial n=$B_SEQ (rc=$RC_BS)"

########################################################################
fails=()
[[ "$RC_A" == "0" ]]                        || fails+=("A: mhl run falhou (rc=$RC_A)")
[[ "$A_LIST" == '["k/a","k/b","k/c","k/d","k/e","k/f","k/g","k/h"]' ]] \
                                           || fails+=("A: list != as 8 chaves ($A_LIST)")
[[ "${A_OBJS:-0}" == "8" ]]                 || fails+=("A: $A_OBJS objetos no bucket (esperado 8 — perda sob concorrência)")
[[ "$RC_BP" == "0" && "$RC_BS" == "0" ]]    || fails+=("B: mhl run falhou (par=$RC_BP seq=$RC_BS)")
[[ "$B_SEQ" == "8" ]]                       || fails+=("B: sequencial n=$B_SEQ != 8")
[[ "$B_PAR" =~ ^[0-9]+$ && "$B_PAR" -lt 8 ]] \
                                           || fails+=("B: paralelo n=$B_PAR não é < 8 (esperava-se lost update; store v1 sem CAS)")

if [[ ${#fails[@]} -eq 0 ]]; then
  ok "8 puts paralelos sem perda no S3 (mc ls conta 8); RMW concorrente na mesma chave perde updates (par=$B_PAR < seq=8) — store v1 sem CAS"
else
  for f in "${fails[@]}"; do log "  ! $f"; done
  die "veja as verificações acima"
fi
