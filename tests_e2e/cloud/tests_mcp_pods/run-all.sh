#!/usr/bin/env bash
# run-all.sh — regressivo de todos os cenários de tests/cloud/tests_mcp_pods.
#
# Cada CENARIO-*/run.sh (e as variantes run-stateless.sh / run-wrong-tool.sh)
# sobe seu próprio `mhl serve mcp --http` numa porta dedicada, roda os passos,
# grava em CENARIO-*/logs/ e imprime PASS/FAIL (exit 0/1). Este script executa
# todos em sequência e consolida o resultado.
#
# Uso:
#   ./run-all.sh                       # todos os cenários de host (pula K8S-001)
#   RUN_K8S=1 ./run-all.sh             # inclui CENARIO-K8S-001 (precisa de cluster + docker)
#   ONLY="001 005 007" ./run-all.sh    # só esses códigos
#   SKIP="016 011" ./run-all.sh        # todos menos esses
#   VERBOSE=1 ./run-all.sh             # ecoa a saída de cada cenário
#   TIMEOUT=240 ./run-all.sh           # limite por cenário em segundos (default 300)
#   RESIGN=0 ./run-all.sh              # não re-assina os binários (ver nota macOS abaixo)
#
# Nota (macOS/arm64): um `mhl` recém-compilado ou recém-copiado leva SIGKILL do
# Gatekeeper até ser re-assinado ad-hoc. Por padrão este script roda
# `codesign --force --sign -` no binário de referência e nas cópias por cenário.

set -u

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REF_MHL="$HERE/../mhl"                 # tests/cloud/mhl — binário de referência
RUN_K8S="${RUN_K8S:-0}"
VERBOSE="${VERBOSE:-0}"
TIMEOUT="${TIMEOUT:-300}"
RESIGN="${RESIGN:-1}"
ONLY="${ONLY:-}"
SKIP="${SKIP:-}"

TS="$(date +%Y%m%d-%H%M%S)"
OUT="$HERE/logs-regression"
mkdir -p "$OUT"
RUN_LOG="$OUT/regression-$TS.log"
SUMMARY="$OUT/summary.txt"
: > "$SUMMARY"

c_grn=$'\033[32m'; c_red=$'\033[31m'; c_yel=$'\033[33m'; c_dim=$'\033[2m'; c_rst=$'\033[0m'
[[ -t 1 ]] || { c_grn=; c_red=; c_yel=; c_dim=; c_rst=; }

log()  { echo "$*" | tee -a "$RUN_LOG"; }

# ── binário de referência ────────────────────────────────────────────────
if [[ ! -x "$REF_MHL" ]]; then
  log "${c_red}FALHA${c_rst}: binário de referência não encontrado em $REF_MHL"
  log "        rode: make -C src/mhl-runtime build && cp src/mhl-runtime/dist/mhl tests/cloud/mhl"
  exit 2
fi
resign() { [[ "$RESIGN" == "1" ]] && command -v codesign >/dev/null && codesign --force --sign - "$1" >/dev/null 2>&1 || true; }
resign "$REF_MHL"
REF_VERSION="$("$REF_MHL" version 2>/dev/null || echo '??')"
log "regressivo tests_mcp_pods — $TS"
log "binário: $REF_MHL  ($REF_VERSION)"
log "opções: RUN_K8S=$RUN_K8S VERBOSE=$VERBOSE TIMEOUT=${TIMEOUT}s RESIGN=$RESIGN ONLY='${ONLY}' SKIP='${SKIP}'"
log ""

# ── lista de (codigo, dir, script, rótulo) ──────────────────────────────
# ordem: numérica; variantes logo após o run.sh do cenário; K8S por último.
ENTRIES=(
  "001|CENARIO-001|run.sh|conexao"
  "002|CENARIO-002|run.sh|listar-tools"
  "002|CENARIO-002|run-stateless.sh|stateless"
  "003|CENARIO-003|run.sh|call"
  "003|CENARIO-003|run-wrong-tool.sh|wrong-tool"
  "004|CENARIO-004|run.sh|probes"
  "005|CENARIO-005|run.sh|auth-guards"
  "006|CENARIO-006|run.sh|principal-isolation"
  "007|CENARIO-007|run.sh|async-lifecycle"
  "008|CENARIO-008|run.sh|concurrency-queue"
  "009|CENARIO-009|run.sh|drain-shutdown"
  "010|CENARIO-010|run.sh|step-timeout"
  "011|CENARIO-011|run.sh|state-dir-restart"
  "012|CENARIO-012|run.sh|metrics"
  "013|CENARIO-013|run.sh|run-logs"
  "014|CENARIO-014|run.sh|protocol-conformance"
  "015|CENARIO-015|run.sh|inputschema-validation"
  "016|CENARIO-016|run.sh|cancel-inflight"
  "K8S-001|CENARIO-K8S-001|run.sh|pod-lifecycle"
)

want() { # <codigo>
  local code="$1"
  if [[ -n "$ONLY" ]]; then case " $ONLY " in *" $code "*) ;; *) return 1;; esac; fi
  if [[ -n "$SKIP" ]]; then case " $SKIP " in *" $code "*) return 1;; esac; fi
  if [[ "$code" == "K8S-001" && "$RUN_K8S" != "1" ]]; then return 1; fi
  return 0
}

# ── timeout portável ───────────────────────────────────────────────────
run_bounded() { # <segundos> <logfile> <cmd...>
  local secs="$1" lf="$2"; shift 2
  ( "$@" ) >"$lf" 2>&1 &
  local pid=$!
  ( sleep "$secs"; kill -TERM "$pid" 2>/dev/null; sleep 3; kill -KILL "$pid" 2>/dev/null ) &
  local watch=$!
  wait "$pid" 2>/dev/null; local rc=$?
  kill -TERM "$watch" 2>/dev/null; wait "$watch" 2>/dev/null
  return $rc
}

# ── execução ──────────────────────────────────────────────────────────
declare -a ROWS
total=0; passed=0; failed=0; skipped=0
overall_start=$(date +%s)

printf '%-14s %-20s %-8s %6s\n' "CÓDIGO" "VARIANTE" "STATUS" "TEMPO" | tee -a "$SUMMARY"
printf '%s\n' "--------------------------------------------------------------" | tee -a "$SUMMARY"

for entry in "${ENTRIES[@]}"; do
  IFS='|' read -r code dir script label <<<"$entry"
  scen_dir="$HERE/$dir"
  scen_script="$scen_dir/$script"

  if ! want "$code"; then
    if [[ "$code" == "K8S-001" && "$RUN_K8S" != "1" ]]; then
      ROWS+=("$code|$label|SKIP|—|não incluído (RUN_K8S!=1)")
      printf '%-14s %-20s %s%-8s%s %6s\n' "$code" "$label" "$c_yel" "SKIP" "$c_rst" "—" | tee -a "$SUMMARY"
      skipped=$((skipped+1))
    fi
    continue
  fi
  [[ -f "$scen_script" ]] || { ROWS+=("$code|$label|MISSING|—|$scen_script"); \
    printf '%-14s %-20s %s%-8s%s %6s\n' "$code" "$label" "$c_red" "MISSING" "$c_rst" "—" | tee -a "$SUMMARY"; \
    failed=$((failed+1)); total=$((total+1)); continue; }

  # binário fresco e assinado na pasta do cenário (o run.sh então pula seu cp)
  cp "$REF_MHL" "$scen_dir/mhl" 2>/dev/null && chmod +x "$scen_dir/mhl" && resign "$scen_dir/mhl"

  child_log="$OUT/${dir}-${label}.log"

  log "${c_dim}▶ $code ($label) — $dir/$script${c_rst}"
  t0=$(date +%s)
  run_bounded "$TIMEOUT" "$child_log" bash "$scen_script"
  rc=$?
  t1=$(date +%s); dur=$((t1-t0))
  total=$((total+1))

  # veredito: última linha PASS/FAIL do próprio cenário, senão o exit code
  verdict="$(grep -E '^(PASS|FAIL)$' "$child_log" | tail -1)"
  if [[ -z "$verdict" ]]; then
    if [[ $rc -eq 143 || $rc -eq 137 ]]; then verdict="TIMEOUT"; else verdict="FAIL"; fi
  fi

  case "$verdict" in
    PASS) passed=$((passed+1)); col="$c_grn" ;;
    *)    failed=$((failed+1)); col="$c_red" ;;
  esac
  ROWS+=("$code|$label|$verdict|${dur}s|$child_log")
  printf '%-14s %-20s %s%-8s%s %5ss\n' "$code" "$label" "$col" "$verdict" "$c_rst" "$dur" | tee -a "$SUMMARY"
  [[ "$VERBOSE" == "1" ]] && { echo "$c_dim"; sed 's/^/    /' "$child_log"; echo "$c_rst"; }

  # rede de segurança: mata qualquer mhl órfão desta pasta
  pkill -f "$scen_dir/mhl serve mcp" 2>/dev/null || true
done

overall_end=$(date +%s)
elapsed=$((overall_end - overall_start))
log ""
log "--------------------------------------------------------------"
log "total=$total  ${c_grn}PASS=$passed${c_rst}  ${c_red}FAIL=$failed${c_rst}  ${c_yel}SKIP=$skipped${c_rst}  (${elapsed}s)"
log "log completo: $RUN_LOG"
log "logs por cenário: $OUT/"

# ── REGRESSION.md ─────────────────────────────────────────────────────
{
  echo "# Regressivo — tests_mcp_pods"
  echo
  echo "**Execução:** $TS · binário \`$REF_VERSION\` · TIMEOUT ${TIMEOUT}s · RUN_K8S=$RUN_K8S"
  echo
  echo "| Código | Variante | Status | Tempo | Log |"
  echo "|---|---|---|---|---|"
  for r in "${ROWS[@]}"; do
    IFS='|' read -r c l s d lg <<<"$r"
    if [[ "$s" == "PASS" || "$s" == "FAIL" || "$s" == "TIMEOUT" ]]; then
      lg_rel="${lg#$HERE/}"
      echo "| $c | $l | $s | $d | [$lg_rel]($lg_rel) |"
    else
      echo "| $c | $l | $s | $d | ${lg:-—} |"
    fi
  done
  echo
  echo "**Resumo:** $passed PASS · $failed FAIL · $skipped SKIP de $total executados."
} > "$HERE/REGRESSION.md"
log "resumo: $HERE/REGRESSION.md"

[[ $failed -eq 0 ]] && exit 0 || exit 1
