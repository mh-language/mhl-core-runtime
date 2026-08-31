#!/usr/bin/env bash
# CENARIO-008 — read-modify-write concorrente (lost update; store v1 sem CAS)
source "$(dirname "${BASH_SOURCE[0]}")/../_lib.sh"

ensure_env

INCR_PARALLEL='
    parallel Bump {
        step b1 { var c = S.get("counter") S.put("counter", c + 1) }
        step b2 { var c = S.get("counter") S.put("counter", c + 1) }
        step b3 { var c = S.get("counter") S.put("counter", c + 1) }
        step b4 { var c = S.get("counter") S.put("counter", c + 1) }
        step b5 { var c = S.get("counter") S.put("counter", c + 1) }
        step b6 { var c = S.get("counter") S.put("counter", c + 1) }
        step b7 { var c = S.get("counter") S.put("counter", c + 1) }
        step b8 { var c = S.get("counter") S.put("counter", c + 1) }
    }'

# NOTE: mhl exige uma instrução por linha; geramos as branches com quebras.
gen_mh() { # <store> <log> <mode: parallel|serial>
  local store="$1" logp="$2" mode="$3"
  cat <<EOF
extension store S {
    dir: "$store"
    log: "$logp"
    latency_ms: 40
}
pipeline Counter {
    step seed {
        S.put("counter", 0)
    }
EOF
  if [[ "$mode" == "parallel" ]]; then
    echo "    parallel Bump {"
    for i in $(seq 1 8); do
      printf '        step b%02d {\n            var c = S.get("counter")\n            S.put("counter", c + 1)\n        }\n' "$i"
    done
    echo "    }"
  else
    for i in $(seq 1 8); do
      printf '    step b%02d {\n        var c = S.get("counter")\n        S.put("counter", c + 1)\n    }\n' "$i"
    done
  fi
  cat <<EOF
    step final {
        var v = S.get("counter")
        log(v)
    }
}
EOF
}

# ── ramo concorrente ───────────────────────────────────────────────────
new_project parallel
P_STORE="$PROJ/store"; mkdir -p "$P_STORE"
gen_mh "$P_STORE" "$PROJ/probe.jsonl" parallel > main.mh
cp main.mh "$L/main-parallel.mh"
"$MHL" run main.mh > "$L/run-parallel.out" 2>&1 || { cat "$L/run-parallel.out" >> "$CLIENT_LOG"; die "run paralelo falhou"; }
cp "$PROJ/probe.jsonl" "$L/probe-parallel.jsonl" 2>/dev/null || true
P_FINAL=$(grep -vE '^(session:|step:|executed )' "$L/run-parallel.out" | tail -1)
log "ramo concorrente: counter final = $P_FINAL (esperado < 8 — lost update)"

# ── ramo sequencial ───────────────────────────────────────────────────
new_project serial
S_STORE="$PROJ/store"; mkdir -p "$S_STORE"
gen_mh "$S_STORE" "$PROJ/probe.jsonl" serial > main.mh
cp main.mh "$L/main-serial.mh"
"$MHL" run main.mh > "$L/run-serial.out" 2>&1 || { cat "$L/run-serial.out" >> "$CLIENT_LOG"; die "run sequencial falhou"; }
S_FINAL=$(grep -vE '^(session:|step:|executed )' "$L/run-serial.out" | tail -1)
log "ramo sequencial: counter final = $S_FINAL (esperado == 8)"

fails=()
[[ "$S_FINAL" == "8" ]]                    || fails+=("sequencial: counter=$S_FINAL != 8")
[[ "$P_FINAL" =~ ^[0-9]+$ && "$P_FINAL" -lt 8 ]] \
  || fails+=("concorrente: counter=$P_FINAL não é < 8 (esperava-se lost update)")

if [[ ${#fails[@]} -eq 0 ]]; then
  ok "concorrente perde updates (counter=$P_FINAL < 8); sequencial correto (=8) — store v1 sem CAS/lease"
else
  for f in "${fails[@]}"; do log "  ! $f"; done
  die "veja as verificações acima"
fi
