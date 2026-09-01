#!/usr/bin/env bash
# run-all.sh — roda os cenários-ALVO de tests_mcp_targets.
#
# Cada ITEM-*/run.sh descreve o comportamento desejado de um item em aberto do
# mhl-eks-design.html e imprime MET (implementado) ou PENDING (falta fazer).
# Um PENDING NÃO é falha de build — é o item de trabalho. Só FAIL (script
# quebrado) faz o run-all sair != 0.
#
#   ./run-all.sh
#   ONLY="01 12" ./run-all.sh
#   TIMEOUT=180 ./run-all.sh
set -u
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ONLY="${ONLY:-}"; TIMEOUT="${TIMEOUT:-180}"
TS="$(date +%Y%m%d-%H%M%S)"
OUT="$HERE/logs-regression"; mkdir -p "$OUT"; RUN_LOG="$OUT/run-$TS.log"; : > "$RUN_LOG"
log() { echo "$*" | tee -a "$RUN_LOG"; }
c_grn=$'\033[32m'; c_red=$'\033[31m'; c_yel=$'\033[33m'; c_rst=$'\033[0m'; [[ -t 1 ]] || { c_grn=; c_red=; c_yel=; c_rst=; }
command -v codesign >/dev/null && codesign --force --sign - "$HERE/../mhl" 2>/dev/null || true

run_bounded() { local secs="$1" lf="$2"; shift 2
  ( "$@" ) >"$lf" 2>&1 & local pid=$!
  ( sleep "$secs"; kill -TERM "$pid" 2>/dev/null; sleep 3; kill -KILL "$pid" 2>/dev/null ) & local w=$!
  wait "$pid" 2>/dev/null; local rc=$?; kill -TERM "$w" 2>/dev/null; wait "$w" 2>/dev/null; return $rc
}

declare -a ROWS; met=0; pending=0; broke=0; t0=$(date +%s)
log "cenários-alvo tests_mcp_targets — $TS"
log "binário: $("$HERE/../mhl" version 2>/dev/null)"
log ""
printf '%-10s %-10s %6s  %s\n' "ITEM" "STATUS" "TEMPO" "detalhe" | tee -a "$RUN_LOG"
printf '%s\n' "------------------------------------------------------------" | tee -a "$RUN_LOG"

for dir in "$HERE"/ITEM-*/; do
  [[ -f "$dir/run.sh" ]] || continue
  code="$(basename "$dir" | sed 's/^ITEM-//')"
  [[ -n "$ONLY" ]] && { case " $ONLY " in *" $code "*) ;; *) continue;; esac; }
  clog="$OUT/ITEM-$code.log"
  s=$(date +%s); run_bounded "$TIMEOUT" "$clog" bash "$dir/run.sh"; rc=$?
  e=$(date +%s); d=$((e-s))
  v="$(grep -E '^(MET|PENDING|FAIL)$' "$clog" | tail -1)"
  [[ -z "$v" ]] && { [[ $rc -eq 143 || $rc -eq 137 ]] && v=FAIL || v=FAIL; }
  npend=$(grep -c '  PENDING:' "$clog" 2>/dev/null || echo 0)
  case "$v" in
    MET)     met=$((met+1)); col=$c_grn; det="—";;
    PENDING) pending=$((pending+1)); col=$c_yel; det="$npend item(ns) a implementar";;
    *)       broke=$((broke+1)); col=$c_red; det="script quebrou (ver log)";;
  esac
  ROWS+=("$code|$v|${d}s|$det|$clog")
  printf '%-10s %s%-10s%s %5ss  %s\n' "$code" "$col" "$v" "$c_rst" "$d" "$det" | tee -a "$RUN_LOG"
done
t1=$(date +%s)
log ""
log "------------------------------------------------------------"
log "${c_grn}MET=$met${c_rst}  ${c_yel}PENDING=$pending${c_rst}  ${c_red}FAIL=$broke${c_rst}  ($((t1-t0))s)"

{
  echo "# Cenários-alvo — tests_mcp_targets"
  echo
  echo "**Execução:** $TS · binário \`$("$HERE/../mhl" version 2>/dev/null)\`"
  echo
  echo "Cada linha é o **critério de aceite** de uma correção do"
  echo "[\`../mhl-eks-design.html\`](../mhl-eks-design.html). \`PENDING\` = trabalho a fazer;"
  echo "\`MET\` = correção landou e verificada."
  echo
  echo "| Item (design) | Status | Tempo | Detalhe | Spec |"
  echo "|---|---|---|---|---|"
  for r in "${ROWS[@]}"; do
    IFS='|' read -r c s d det lg <<<"$r"
    md=$(ls "$HERE/ITEM-$c/"*.md 2>/dev/null | head -1); md="${md#$HERE/}"
    echo "| $c | $s | $d | $det | [spec]($md) · [log](${lg#$HERE/}) |"
  done
  echo
  echo "**Resumo:** $met MET · $pending PENDING · $broke FAIL."
} > "$HERE/REGRESSION.md"
log "resumo: $HERE/REGRESSION.md"

# só um script quebrado (FAIL) faz o run-all sair != 0
[[ $broke -eq 0 ]] && exit 0 || exit 1
