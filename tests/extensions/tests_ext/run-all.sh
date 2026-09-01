#!/usr/bin/env bash
# run-all.sh — regressivo de todos os cenários de tests/extensions/tests_ext.
#
# Cada CENARIO-*/run.sh monta seu próprio projeto scratch (mktemp), instala
# store-probe nele, roda os passos, grava em CENARIO-*/logs/ e imprime PASS/FAIL.
# Este script executa todos em sequência e consolida.
#
#   ./run-all.sh                 # todos
#   ONLY="001 007" ./run-all.sh  # só esses
#   SKIP="010" ./run-all.sh
#   VERBOSE=1 ./run-all.sh
#   TIMEOUT=180 ./run-all.sh     # limite por cenário (default 180s)
#
# Cenários S3 (011-013) e Postgres (014-016) sobem um MinIO / Postgres via
# `docker compose` (src/mhl-extensions/mhl-store-s3/, src/mhl-extensions/mhl-store-postgres/) e se auto-PULAM
# (SKIP, não FAIL) quando o Docker não está disponível. Ao final os bancos são
# derrubados (`down -v`); passe S3_KEEP=1 / PG_KEEP=1 para mantê-los de pé.

set -u
HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT="$(cd "$HERE/.." && pwd)"
ONLY="${ONLY:-}"; SKIP="${SKIP:-}"; VERBOSE="${VERBOSE:-0}"; TIMEOUT="${TIMEOUT:-180}"

TS="$(date +%Y%m%d-%H%M%S)"
OUT="$HERE/logs-regression"; mkdir -p "$OUT"
RUN_LOG="$OUT/regression-$TS.log"; : > "$RUN_LOG"
log() { echo "$*" | tee -a "$RUN_LOG"; }

c_grn=$'\033[32m'; c_red=$'\033[31m'; c_yel=$'\033[33m'; c_dim=$'\033[2m'; c_rst=$'\033[0m'
[[ -t 1 ]] || { c_grn=; c_red=; c_yel=; c_dim=; c_rst=; }

# pré-flight: binário mhl e store-probe
[[ -x "$ROOT/mhl" ]] || { log "${c_red}FALHA${c_rst}: $ROOT/mhl ausente — rode: cp tests/cloud/mhl tests/extensions/mhl"; exit 2; }
command -v go >/dev/null || { log "${c_red}FALHA${c_rst}: go não encontrado (necessário para store-probe)"; exit 2; }
command -v codesign >/dev/null && codesign --force --sign - "$ROOT/mhl" 2>/dev/null || true
( cd "$ROOT/store-probe" && go build -o bin/store-probe . ) || { log "${c_red}FALHA${c_rst}: build de store-probe"; exit 2; }
command -v codesign >/dev/null && codesign --force --sign - "$ROOT/store-probe/bin/store-probe" 2>/dev/null || true

log "regressivo tests_ext — $TS"
log "binário: $ROOT/mhl  ($("$ROOT/mhl" version 2>/dev/null))"
log "opções: VERBOSE=$VERBOSE TIMEOUT=${TIMEOUT}s ONLY='$ONLY' SKIP='$SKIP'"
log ""

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

want() { local n="$1"
  [[ -n "$ONLY" ]] && { case " $ONLY " in *" $n "*) ;; *) return 1;; esac; }
  [[ -n "$SKIP" ]] && { case " $SKIP " in *" $n "*) return 1;; esac; }
  return 0
}

declare -a ROWS
total=0; passed=0; failed=0; skipped=0
t_all0=$(date +%s)
printf '%-12s %-8s %6s\n' "CÓDIGO" "STATUS" "TEMPO" | tee -a "$RUN_LOG"
printf '%s\n' "------------------------------------" | tee -a "$RUN_LOG"

for dir in "$HERE"/CENARIO-*/; do
  [[ -f "$dir/run.sh" ]] || continue
  code="$(basename "$dir" | sed 's/^CENARIO-//')"
  want "$code" || continue
  child_log="$OUT/CENARIO-${code}.log"
  log "${c_dim}▶ $code${c_rst}"
  t0=$(date +%s)
  run_bounded "$TIMEOUT" "$child_log" bash "$dir/run.sh"
  rc=$?
  t1=$(date +%s); dur=$((t1-t0)); total=$((total+1))
  verdict="$(grep -E '^(PASS|FAIL|SKIP)$' "$child_log" | tail -1)"
  [[ -z "$verdict" ]] && { [[ $rc -eq 143 || $rc -eq 137 ]] && verdict="TIMEOUT" || verdict="FAIL"; }
  case "$verdict" in
    PASS) passed=$((passed+1)); col="$c_grn";;
    SKIP) skipped=$((skipped+1)); col="$c_yel";;
    *)    failed=$((failed+1)); col="$c_red";;
  esac
  ROWS+=("$code|$verdict|${dur}s|$child_log")
  printf '%-12s %s%-8s%s %5ss\n' "$code" "$col" "$verdict" "$c_rst" "$dur" | tee -a "$RUN_LOG"
  [[ "$VERBOSE" == "1" ]] && { echo "$c_dim"; sed 's/^/    /' "$child_log"; echo "$c_rst"; }
done

t_all1=$(date +%s)
log ""
log "------------------------------------"
log "total=$total  ${c_grn}PASS=$passed${c_rst}  ${c_red}FAIL=$failed${c_rst}  ${c_yel}SKIP=$skipped${c_rst}  ($((t_all1-t_all0))s)"
log "log: $RUN_LOG   por cenário: $OUT/"

# derruba os bancos dos cenários de extensão (a menos que *_KEEP=1)
if command -v docker >/dev/null 2>&1; then
  S3_COMPOSE="$ROOT/../../src/mhl-extensions/mhl-store-s3/docker-compose.yml"
  if [[ "${S3_KEEP:-0}" != "1" && -f "$S3_COMPOSE" ]]; then
    docker compose -f "$S3_COMPOSE" down -v >/dev/null 2>&1 || true
    log "MinIO (src/mhl-extensions/mhl-store-s3) derrubado — S3_KEEP=1 para manter"
  fi
  PG_COMPOSE="$ROOT/../../src/mhl-extensions/mhl-store-postgres/docker-compose.yml"
  if [[ "${PG_KEEP:-0}" != "1" && -f "$PG_COMPOSE" ]]; then
    docker compose -f "$PG_COMPOSE" down -v >/dev/null 2>&1 || true
    log "Postgres (src/mhl-extensions/mhl-store-postgres) derrubado — PG_KEEP=1 para manter"
  fi
  SQLPG_COMPOSE="$ROOT/../../src/mhl-extensions/mhl-sql-postgres/docker-compose.yml"
  if [[ "${PG_KEEP:-0}" != "1" && -f "$SQLPG_COMPOSE" ]]; then
    docker compose -f "$SQLPG_COMPOSE" down -v >/dev/null 2>&1 || true
    log "Postgres (src/mhl-extensions/mhl-sql-postgres) derrubado — PG_KEEP=1 para manter"
  fi
  REDIS_COMPOSE="$ROOT/../../src/mhl-extensions/mhl-cache-redis/docker-compose.yml"
  if [[ "${REDIS_KEEP:-0}" != "1" && -f "$REDIS_COMPOSE" ]]; then
    docker compose -f "$REDIS_COMPOSE" down -v >/dev/null 2>&1 || true
    log "Redis (src/mhl-extensions/mhl-cache-redis) derrubado — REDIS_KEEP=1 para manter"
  fi
fi

{
  echo "# Regressivo — tests_ext"
  echo
  echo "**Execução:** $TS · binário \`$("$ROOT/mhl" version 2>/dev/null)\` · TIMEOUT ${TIMEOUT}s"
  echo
  echo "| Código | Status | Tempo | Log |"
  echo "|---|---|---|---|"
  for r in "${ROWS[@]}"; do
    IFS='|' read -r c s d lg <<<"$r"
    echo "| $c | $s | $d | [${lg#$HERE/}](${lg#$HERE/}) |"
  done
  echo
  echo "**Resumo:** $passed PASS · $failed FAIL · $skipped SKIP de $total."
} > "$HERE/REGRESSION.md"
log "resumo: $HERE/REGRESSION.md"

[[ $failed -eq 0 ]] && exit 0 || exit 1
