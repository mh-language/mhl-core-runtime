#!/usr/bin/env bash
# CENARIO-006 — Crash da extensão no meio de um run
source "$(dirname "${BASH_SOURCE[0]}")/../_lib.sh"

ensure_env

run_bounded_mh() { # <projdir> <outfile> <guard-secs>  -> rc
  ( cd "$1" && "$MHL" run main.mh ) > "$2" 2>&1 &
  local pid=$!
  ( sleep "${3:-60}"; kill -KILL "$pid" 2>/dev/null ) & local w=$!
  wait "$pid"; local rc=$?
  kill "$w" 2>/dev/null; wait "$w" 2>/dev/null
  return $rc
}

########################################################################
# Parte A — sequencial: crash na 1ª chamada
########################################################################
new_project seq
A_STORE="$PROJ/store"; A_LOG="$PROJ/probe.jsonl"; mkdir -p "$A_STORE"
cat > main.mh <<EOF
extension store S {
    dir: "$A_STORE"
    log: "$A_LOG"
    crash_after: 0
}
pipeline Seq {
    step s1 {
        S.put("k/1", 1)
    }
    step s2 {
        S.put("k/2", 2)
    }
}
EOF
log "Parte A: sequencial, crash_after=0 (guarda 60s)"
T0=$(date +%s); run_bounded_mh "$PROJ" "$L/run-seq.out" 60; A_RC=$?; T1=$(date +%s); A_EL=$((T1-T0))
cp "$A_LOG" "$L/probe-seq.jsonl" 2>/dev/null || true
log "  rc=$A_RC elapsed=${A_EL}s"
grep -vE '^step:' "$L/run-seq.out" | tee -a "$CLIENT_LOG"
log "  probe-seq.jsonl:"; cat "$L/probe-seq.jsonl" | tee -a "$CLIENT_LOG"

########################################################################
# Parte B — crash dentro de um grupo parallel
########################################################################
new_project par
B_STORE="$PROJ/store"; B_LOG="$PROJ/probe.jsonl"; mkdir -p "$B_STORE"
{
  echo "extension store S {"
  echo "    dir: \"$B_STORE\""
  echo "    log: \"$B_LOG\""
  echo "    crash_after: 3"
  echo "}"
  echo "pipeline Par {"
  echo "    parallel Fan {"
  for i in $(seq -w 1 12); do
    printf '        step p%s {\n            S.put("k/%s", %s)\n        }\n' "$i" "$i" "$((10#$i))"
  done
  echo "    }"
  echo "    step after {"
  echo "        log(\"after\")"
  echo "    }"
  echo "}"
} > main.mh
cp main.mh "$L/main-par.mh"
log "Parte B: parallel de 12, crash_after=3 (guarda 60s)"
T0=$(date +%s); run_bounded_mh "$PROJ" "$L/run-par.out" 60; B_RC=$?; T1=$(date +%s); B_EL=$((T1-T0))
cp "$B_LOG" "$L/probe-par.jsonl" 2>/dev/null || true
log "  rc=$B_RC elapsed=${B_EL}s"
grep -vE '^step:' "$L/run-par.out" | tee -a "$CLIENT_LOG"

########################################################################
# verdite
########################################################################
fails=()
# Parte A
[[ "$A_RC" != "0" && "$A_RC" != "137" ]]                       || fails+=("A: mhl run não falhou (rc=$A_RC)")
grep -q 'extension process is not running' "$L/run-seq.out"    || fails+=("A: sem 'extension process is not running'")
grep -q 'crash_after=0 reached' "$L/run-seq.out"               || fails+=("A: stderr da extensão não anexado ao erro")
grep -q '"ev":"init"'  "$L/probe-seq.jsonl"                    || fails+=("A: probe sem 'init'")
grep -q '"ev":"crash"' "$L/probe-seq.jsonl"                    || fails+=("A: probe sem 'crash'")
[[ "$A_EL" -lt 15 ]]                                           || fails+=("A: demorou ${A_EL}s (possível deadlock)")
# Parte B
[[ "$B_RC" != "0" && "$B_RC" != "137" ]]                       || fails+=("B: mhl run não falhou (rc=$B_RC)")
grep -q 'extension process is not running' "$L/run-par.out"    || fails+=("B: sem 'extension process is not running'")
[[ "$B_EL" -lt 20 ]]                                           || fails+=("B: demorou ${B_EL}s (possível deadlock)")

if [[ ${#fails[@]} -eq 0 ]]; then
  ok "crash surfaceado com diagnóstico (extensão + exit status + stderr); run termina sem travar, seq e parallel"
else
  for f in "${fails[@]}"; do log "  ! $f"; done
  die "veja as verificações acima"
fi
