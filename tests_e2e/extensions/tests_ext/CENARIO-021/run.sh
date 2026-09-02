#!/usr/bin/env bash
# CENARIO-021 — mhl-cache-redis: TTL e contador atômico (sem lost update)
source "$(dirname "${BASH_SOURCE[0]}")/../_lib.sh"

redis_ensure

########################################################################
# Parte A — TTL
########################################################################
redis_flush
redis_project a
A_LOG="$PROJ/wire.jsonl"
cat > main.mh <<EOF
extension cache C {
    url: env("REDIS_URL")
    key_prefix: "c021a:"
    ttl: "10s"
    log: "$A_LOG"
}
pipeline TTL {
    step defaults {
        C.set("a", 1)
        var ta = C.ttl("a")
        log(ta)
    }
    step explicit {
        C.set("b", 1, "4s")
        var tb = C.ttl("b")
        log(tb)
    }
    step refresh {
        C.expire("a", "30s")
        var ta2 = C.ttl("a")
        log(ta2)
    }
    step blink {
        C.set("blink", 1, "1s")
        cmd.exec(["sh", "-c", "sleep 1.6"])
        var still = C.has("blink")
        log(still)
    }
}
EOF
cp main.mh "$L/main-a.mh"
log "Parte A: TTL"
"$MHL" run main.mh > "$L/run-a.out" 2>&1 || { cat "$L/run-a.out" >> "$CLIENT_LOG"; die "Parte A: mhl run falhou"; }
cp "$A_LOG" "$L/wire.jsonl" 2>/dev/null || true
A=()
while IFS= read -r _l; do A+=("$_l"); done < <(grep -vE '^(session:|step:|executed )' "$L/run-a.out")
log "Parte A: ttl(a)=${A[0]:-} ttl(b)=${A[1]:-} ttl(a após expire)=${A[2]:-} has(blink)=${A[3]:-}"

########################################################################
# Parte B — contador: incr atômico vs read-modify-write
########################################################################
gen_par() { # <corpo-da-branch>   escreve um pipeline de 8 branches paralelas + leitura
  cat <<EOF
extension cache C {
    url: env("REDIS_URL")
    key_prefix: "c021b:"
    log: "$B_LOG"
}
pipeline Fan {
    step init {
        C.set("k", 0)
    }
    parallel Bump {
EOF
  for i in $(seq 1 8); do
    printf '        step s%s {\n%s\n        }\n' "$i" "$1"
  done
  cat <<EOF
    }
    step read {
        var v = C.get("k")
        log(v)
    }
}
EOF
}

redis_flush
redis_project b_incr
B_LOG="$PROJ/wire.jsonl"
gen_par '            C.incr("k")' > main.mh
cp main.mh "$L/main-b-incr.mh"
log "Parte B: parallel de 8 C.incr"
"$MHL" run main.mh > "$L/run-b-incr.out" 2>&1; RC_BI=$?
B_INCR=$(grep -vE '^(session:|step:|executed )' "$L/run-b-incr.out" | tail -1)

redis_flush
redis_project b_rmw
B_LOG="$PROJ/wire.jsonl"
gen_par '            var v = C.get("k")
            cmd.exec(["sh", "-c", "sleep 0.05"])
            C.set("k", v + 1)' > main.mh
cp main.mh "$L/main-b-rmw.mh"
log "Parte B: parallel de 8 read-modify-write (get+set) — controle"
"$MHL" run main.mh > "$L/run-b-rmw.out" 2>&1; RC_BR=$?
B_RMW=$(grep -vE '^(session:|step:|executed )' "$L/run-b-rmw.out" | tail -1)
log "Parte B: incr atômico -> k=$B_INCR (rc=$RC_BI) ; RMW -> k=$B_RMW (rc=$RC_BR)"

########################################################################
inrange() { [[ "$1" =~ ^-?[0-9]+$ ]] && (( $1 >= $2 && $1 <= $3 )); }
fails=()
inrange "${A[0]:-x}" 8 10   || fails+=("A: ttl(a) default ${A[0]:-} fora de [8,10]")
inrange "${A[1]:-x}" 2 4    || fails+=("A: ttl(b) explícito ${A[1]:-} fora de [2,4]")
inrange "${A[2]:-x}" 27 30  || fails+=("A: ttl(a) após expire ${A[2]:-} fora de [27,30]")
[[ "${A[3]:-}" == "false" ]]                    || fails+=("A: has(blink) após 1.6s != false (${A[3]:-})")
[[ "$RC_BI" == "0" && "$RC_BR" == "0" ]]        || fails+=("B: mhl run falhou (incr=$RC_BI rmw=$RC_BR)")
[[ "$B_INCR" == "8" ]]                          || fails+=("B: incr paralelo -> k=$B_INCR != 8 (perdeu incremento!)")
[[ "$B_RMW" =~ ^[0-9]+$ && "$B_RMW" -lt 8 ]]    || fails+=("B: RMW paralelo -> k=$B_RMW não é < 8 (esperava-se lost update)")

if [[ ${#fails[@]} -eq 0 ]]; then
  ok "TTL default/explícito/expire/expiração ok; incr paralelo = 8 (atômico, sem perda) vs RMW = $B_RMW (< 8)"
else
  for f in "${fails[@]}"; do log "  ! $f"; done
  die "veja as verificações acima"
fi
