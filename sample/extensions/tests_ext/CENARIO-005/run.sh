#!/usr/bin/env bash
# CENARIO-005 — Ciclo de vida do processo da extensão
source "$(dirname "${BASH_SOURCE[0]}")/../_lib.sh"

ensure_env
new_project
STORE="$PROJ/store"; PROBE_LOG="$PROJ/probe.jsonl"; mkdir -p "$STORE"

cat > main.mh <<EOF
extension store S {
    dir: "$STORE"
    log: "$PROBE_LOG"
}
pipeline Lifecycle {
    step one {
        S.put("a", 1)
    }
    step two {
        S.put("b", 2)
    }
    step three {
        var x = S.get("a")
        log(x)
    }
    step four {
        var y = S.get("b")
        log(y)
    }
    step five {
        S.delete("a")
    }
    step six {
        var ks = S.list("")
        log(ks)
    }
    step seven {
        var z = S.get("a")
        log(z)
    }
}
EOF

log "mhl run main.mh"
"$MHL" run main.mh > "$L/run.out" 2>&1 || { cat "$L/run.out" >> "$CLIENT_LOG"; die "mhl run falhou"; }
cp "$PROBE_LOG" "$L/probe.jsonl" 2>/dev/null || true
log "probe.jsonl:"; cat "$L/probe.jsonl" | tee -a "$CLIENT_LOG"

INITS=$(grep -c '"ev":"init"' "$L/probe.jsonl" || true)
CALLS=$(grep -c '"ev":"call"' "$L/probe.jsonl" || true)
SHUTS=$(grep -c '"ev":"shutdown"' "$L/probe.jsonl" || true)
PIDS=$(grep '"ev":"init"' "$L/probe.jsonl" | python3 -c 'import sys,json;print(len({json.loads(l)["pid"] for l in sys.stdin}))')

fails=()
[[ "${INITS:-0}" == "1" ]]  || fails+=("init count = $INITS (esperado 1)")
[[ "${CALLS:-0}" == "7" ]]  || fails+=("call count = $CALLS (esperado 7)")
[[ "${SHUTS:-0}" -ge 1 ]]   || fails+=("shutdown não registrado")
[[ "${PIDS:-0}" == "1" ]]   || fails+=("mais de um pid nos init ($PIDS) — não é um processo único")

if [[ ${#fails[@]} -eq 0 ]]; then
  ok "1 init, 7 call, $SHUTS shutdown, 1 pid — um processo iniciado na 1ª chamada e reusado"
else
  for f in "${fails[@]}"; do log "  ! $f"; done
  die "veja as verificações acima"
fi
