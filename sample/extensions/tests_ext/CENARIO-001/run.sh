#!/usr/bin/env bash
# CENARIO-001 — Carregar uma extensão e chamá-la de um .mh
source "$(dirname "${BASH_SOURCE[0]}")/../_lib.sh"

ensure_env
new_project

STORE="$PROJ/store"; PROBE_LOG="$PROJ/probe.jsonl"
mkdir -p "$STORE"

cat > main.mh <<EOF
extension store S {
    dir: "$STORE"
    log: "$PROBE_LOG"
}

pipeline LoadAndCall {
    step write {
        S.put("greeting", "hello-ext")
    }
    step read {
        var v = S.get("greeting")
        log(v)
    }
    step miss {
        var m = S.get("nope")
        log(m)
    }
}
EOF

log "mhl run main.mh"
"$MHL" run main.mh > "$L/run.out" 2>&1 || { cat "$L/run.out" | tee -a "$CLIENT_LOG"; die "mhl run falhou"; }
cat "$L/run.out" | tee -a "$CLIENT_LOG"

"$MHL" extension doctor > "$L/extension-doctor.log" 2>&1
DOCTOR_RC=$?
log "mhl extension doctor -> rc=$DOCTOR_RC"; cat "$L/extension-doctor.log" | tee -a "$CLIENT_LOG"

cp "$PROBE_LOG" "$L/probe.jsonl" 2>/dev/null || true
find "$STORE" -type f | sed "s|$STORE/||" > "$L/store-tree.txt"
log "árvore do store:"; cat "$L/store-tree.txt" | tee -a "$CLIENT_LOG"
log "probe.jsonl:"; cat "$L/probe.jsonl" | tee -a "$CLIENT_LOG"

INITS=$(grep -c '"ev":"init"' "$L/probe.jsonl" || true)
CALLS=$(grep -c '"ev":"call"' "$L/probe.jsonl" || true)
GREETING_VAL=$(python3 -c 'import json;print(json.load(open("'"$STORE"'/greeting.json")))' 2>/dev/null || echo "<sem arquivo>")

fails=()
grep -qx "hello-ext" "$L/run.out"                 || fails+=("run.out sem a linha 'hello-ext' (S.get hit)")
grep -qx "null" "$L/run.out"                      || fails+=("run.out sem 'null' (S.get miss)")
[[ -f "$STORE/greeting.json" ]]                   || fails+=("$STORE/greeting.json não existe")
[[ "$GREETING_VAL" == "hello-ext" ]]              || fails+=("greeting.json != hello-ext ($GREETING_VAL)")
[[ "${INITS:-0}" == "1" ]]                        || fails+=("probe.jsonl tem $INITS 'init' (esperado 1)")
[[ "${CALLS:-0}" == "3" ]]                        || fails+=("probe.jsonl tem $CALLS 'call' (esperado 3)")
[[ "$DOCTOR_RC" == "0" ]]                         || fails+=("mhl extension doctor rc=$DOCTOR_RC != 0")
grep -q "dev.mhl.store-probe" "$L/extension-doctor.log" || fails+=("doctor não menciona a extensão")

if [[ ${#fails[@]} -eq 0 ]]; then
  ok "extensão carregada; put/get/miss OK; 1 init + 3 call; doctor OK"
else
  for f in "${fails[@]}"; do log "  ! $f"; done
  die "veja as verificações acima"
fi
