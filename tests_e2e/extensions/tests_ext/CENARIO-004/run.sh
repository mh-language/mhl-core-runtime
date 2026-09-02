#!/usr/bin/env bash
# CENARIO-004 — env() numa propriedade da extensão
source "$(dirname "${BASH_SOURCE[0]}")/../_lib.sh"

ensure_env
new_project
STORE="$PROJ/store"; mkdir -p "$STORE"

cat > main.mh <<'EOF'
extension store S {
    dir: env("STORE_PROBE_DIR")
}
pipeline P {
    step s {
        S.put("k", 42)
        var v = S.get("k")
        log(v)
    }
}
EOF

# ── com a variável definida ──────────────────────────────────────────────
log "run com STORE_PROBE_DIR=$STORE"
STORE_PROBE_DIR="$STORE" "$MHL" run main.mh > "$L/run-with-env.out" 2>&1; WITH_RC=$?
cat "$L/run-with-env.out" | tee -a "$CLIENT_LOG"

# ── sem a variável ──────────────────────────────────────────────────────
log "run sem STORE_PROBE_DIR (deve falhar fechado)"
env -u STORE_PROBE_DIR "$MHL" run main.mh > "$L/run-no-env.out" 2>&1; NO_RC=$?
cat "$L/run-no-env.out" | tee -a "$CLIENT_LOG"

fails=()
[[ "$WITH_RC" == "0" ]]                              || fails+=("run com env definida saiu $WITH_RC != 0")
grep -qx "42" "$L/run-with-env.out"                  || fails+=("S.get não retornou 42 com a env definida")
[[ -f "$STORE/k.json" ]]                             || fails+=("$STORE/k.json não foi criado")
[[ "$NO_RC" != "0" ]]                                || fails+=("run sem a env saiu 0 (deveria falhar)")
grep -qi 'STORE_PROBE_DIR' "$L/run-no-env.out"       || fails+=("erro sem a env não menciona STORE_PROBE_DIR")
grep -qx "42" "$L/run-no-env.out" && fails+=("S.get retornou 42 mesmo sem a env (não falhou fechado)")

if [[ ${#fails[@]} -eq 0 ]]; then
  ok "env() na prop resolve quando definida; run falha fechado quando ausente"
else
  for f in "${fails[@]}"; do log "  ! $f"; done
  die "veja as verificações acima"
fi
