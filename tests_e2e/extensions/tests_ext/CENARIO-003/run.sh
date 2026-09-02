#!/usr/bin/env bash
# CENARIO-003 — Allow-list do lock e pin de hash
source "$(dirname "${BASH_SOURCE[0]}")/../_lib.sh"

ensure_env
new_project
STORE="$PROJ/store"; mkdir -p "$STORE"
LOCK="$PROJ/.mhl/extensions.lock"
BIN="$PROJ/.mhl/extensions/dev.mhl.store-probe/bin/store-probe"

cat > main.mh <<EOF
extension store S {
    dir: "$STORE"
}
pipeline P {
    step s {
        S.put("k", 1)
        var v = S.get("k")
        log(v)
    }
}
EOF

# ── controle: lock íntegro carrega ────────────────────────────────────────
cp "$LOCK" "$L/lock-original.json"
"$MHL" run main.mh > "$L/run-ok.out" 2>&1; OK_RC=$?
log "controle (lock íntegro) -> rc=$OK_RC"; grep -vE '^(session:|step:)' "$L/run-ok.out" | tee -a "$CLIENT_LOG"

# ── (a) lock vazio: extensão fora da allow-list ─────────────────────────
echo '{"extensions":{}}' > "$LOCK"
"$MHL" run main.mh > "$L/run-nolock.out" 2>&1; NOLOCK_RC=$?
log "lock vazio -> rc=$NOLOCK_RC"; cat "$L/run-nolock.out" | tee -a "$CLIENT_LOG"

# ── (b) lock restaurado + binário adulterado ───────────────────────────
cp "$L/lock-original.json" "$LOCK"
printf '\n// tampered\n' >> "$BIN"     # muda o sha256
command -v codesign >/dev/null && codesign --force --sign - "$BIN" 2>/dev/null || true
"$MHL" run main.mh > "$L/run-tampered.out" 2>&1; TAMPER_RC=$?
"$MHL" extension doctor > "$L/doctor-tampered.log" 2>&1; DOCTOR_RC=$?
log "binário adulterado -> run rc=$TAMPER_RC ; doctor rc=$DOCTOR_RC"
cat "$L/run-tampered.out" | tee -a "$CLIENT_LOG"
cat "$L/doctor-tampered.log" | tee -a "$CLIENT_LOG"

fails=()
# controle
grep -qx "1" "$L/run-ok.out"                          || fails+=("controle: S.get não retornou 1")
# (a) lock vazio: extensão não deve carregar -> a chamada não resolve o kind
grep -Eq 'not loaded|no extension registered for kind "store"|absent from the lock|not in the lock' "$L/run-nolock.out" \
  || fails+=("lock vazio: sem aviso/erro de extensão não carregada")
grep -qx "1" "$L/run-nolock.out" && fails+=("lock vazio: S.get ainda retornou 1 (extensão carregou mesmo fora do lock)")
# (b) hash drift
grep -Eqi 'sha256|hash|checksum|drift|mismatch|refus' "$L/doctor-tampered.log" \
  || fails+=("doctor não reporta divergência de hash no binário adulterado")
[[ "$DOCTOR_RC" != "0" ]]                              || fails+=("doctor saiu 0 com o binário adulterado")

if [[ ${#fails[@]} -eq 0 ]]; then
  ok "lock é allow-list (fora dele não carrega); hash divergente é recusado; doctor != 0"
else
  for f in "${fails[@]}"; do log "  ! $f"; done
  die "veja as verificações acima"
fi
