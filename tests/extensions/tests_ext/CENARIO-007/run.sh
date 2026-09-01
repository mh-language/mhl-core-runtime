#!/usr/bin/env bash
# CENARIO-007 — Concorrência: parallel de N put + list
source "$(dirname "${BASH_SOURCE[0]}")/../_lib.sh"

ensure_env

gen_mh() { # <store> <log> <serial-bool>
  cat <<EOF
extension store S {
    dir: "$1"
    log: "$2"
    latency_ms: 100
    serial: $3
}
pipeline Fan {
    parallel Puts {
        step a { S.put("k/a", 1) }
        step b { S.put("k/b", 2) }
        step c { S.put("k/c", 3) }
        step d { S.put("k/d", 4) }
        step e { S.put("k/e", 5) }
        step f { S.put("k/f", 6) }
        step g { S.put("k/g", 7) }
        step h { S.put("k/h", 8) }
    }
    step check {
        var ks = S.list("k/")
        log(ks)
    }
}
EOF
}

# overlap analysis over the put lines of a probe log
analyze() { # <probe.jsonl>  -> "count span_ms sum_ms overlapped"
  python3 - "$1" <<'PY'
import json, sys
puts = []
for l in open(sys.argv[1]):
    try:
        d = json.loads(l)
    except Exception:
        continue
    if d.get("ev") == "call" and d.get("op") == "put":
        puts.append((d["t"], d["dur_us"]))
if not puts:
    print("0 0 0 no"); sys.exit()
starts = [t - dur*1000 for t, dur in puts]      # t is END (logged after the op); start ~ t - dur
ends   = [t for t, _ in puts]
span_ms = (max(ends) - min(starts)) / 1e6
sum_ms  = sum(dur for _, dur in puts) / 1000
overlapped = "yes" if span_ms < 0.8 * sum_ms else "no"
print(f"{len(puts)} {span_ms:.1f} {sum_ms:.1f} {overlapped}")
PY
}

# ── concorrente (padrão) ────────────────────────────────────────────────
new_project concurrent
C_STORE="$PROJ/store"; C_LOG="$PROJ/probe.jsonl"; mkdir -p "$C_STORE"
gen_mh "$C_STORE" "$C_LOG" "false" > main.mh
log "run concorrente (serial:false, latency_ms:100)"
t0=$(date +%s.%N); "$MHL" run main.mh > "$L/run-concurrent.out" 2>&1; RC_C=$?; t1=$(date +%s.%N)
C_WALL=$(awk -v a="$t0" -v b="$t1" 'BEGIN{printf "%.2f", b-a}')
cp "$C_LOG" "$L/probe-concurrent.jsonl" 2>/dev/null || true
C_LIST=$(grep -vE '^(session:|step:|executed )' "$L/run-concurrent.out" | tail -1)
read -r C_N C_SPAN C_SUM C_OVL <<<"$(analyze "$L/probe-concurrent.jsonl")"
log "concorrente: wall=${C_WALL}s ; puts=$C_N span=${C_SPAN}ms sum=${C_SUM}ms overlapped=$C_OVL ; list=$C_LIST"

# ── serial (controle) ──────────────────────────────────────────────────
new_project serial
S_STORE="$PROJ/store"; S_LOG="$PROJ/probe.jsonl"; mkdir -p "$S_STORE"
gen_mh "$S_STORE" "$S_LOG" "true" > main.mh
log "run serial (serial:true, latency_ms:100)"
t0=$(date +%s.%N); "$MHL" run main.mh > "$L/run-serial.out" 2>&1; RC_S=$?; t1=$(date +%s.%N)
S_WALL=$(awk -v a="$t0" -v b="$t1" 'BEGIN{printf "%.2f", b-a}')
cp "$S_LOG" "$L/probe-serial.jsonl" 2>/dev/null || true
read -r S_N S_SPAN S_SUM S_OVL <<<"$(analyze "$L/probe-serial.jsonl")"
log "serial: wall=${S_WALL}s ; puts=$S_N span=${S_SPAN}ms sum=${S_SUM}ms overlapped=$S_OVL"

{ echo "concurrent wall ${C_WALL}s span ${C_SPAN}ms sum ${C_SUM}ms overlapped ${C_OVL}"
  echo "serial     wall ${S_WALL}s span ${S_SPAN}ms sum ${S_SUM}ms overlapped ${S_OVL}"; } > "$L/timing.txt"

fails=()
[[ "$RC_C" == "0" && "$RC_S" == "0" ]]           || fails+=("mhl run falhou (concorrente=$RC_C serial=$RC_S)")
[[ "$C_LIST" == '["k/a","k/b","k/c","k/d","k/e","k/f","k/g","k/h"]' ]] \
  || fails+=("list('k/') != as 8 chaves ($C_LIST)")
[[ "${C_N:-0}" == "8" ]]                         || fails+=("probe concorrente tem $C_N puts (esperado 8)")
[[ "$C_OVL" == "yes" ]]                          || fails+=("puts concorrentes NÃO se sobrepuseram (span ${C_SPAN}ms vs sum ${C_SUM}ms)")
[[ "$S_OVL" == "no" ]]                           || fails+=("controle serial se sobrepôs (não serializou)")
awk -v c="$C_WALL" -v s="$S_WALL" 'BEGIN{exit !(s > c + 0.3)}' \
  || fails+=("tempo serial ($S_WALL s) não é claramente maior que o concorrente ($C_WALL s)")
n=$(find "$C_STORE/k" -name '*.json' 2>/dev/null | wc -l | tr -d ' ')
[[ "$n" == "8" ]]                                || fails+=("$n arquivos sob dir/k (esperado 8)")

if [[ ${#fails[@]} -eq 0 ]]; then
  ok "8 puts paralelos concorrentes e sem perda; controle serial ~8× mais lento e sem sobreposição"
else
  for f in "${fails[@]}"; do log "  ! $f"; done
  die "veja as verificações acima"
fi
