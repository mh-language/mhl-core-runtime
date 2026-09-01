#!/usr/bin/env bash
# ITEM-12 (alvo) — approvers: no gate, avaliado contra o principal
source "$(dirname "${BASH_SOURCE[0]}")/../../_lib.sh"

WF="$(mktmp)"                     # serve dir: DocPipeline com approvers:["carol"]
sed 's/^        strategy: "per_step"$/&\n        approvers: ["carol"]/' \
  "$ROOT/docs-workflow.mh" > "$WF/docs-workflow.mh"
BADDIR="$(mktmp)"                 # approvers com tipo errado (p/ lint)
sed 's/^pipeline DocPipeline/pipeline BadAppr/; s/^        strategy: "per_step"$/&\n        approvers: 123/' \
  "$ROOT/docs-workflow.mh" > "$BADDIR/bad.mh"

# alvo 1: lint reconhece approvers:["carol"] e rejeita approvers:123
"$MHL" lint "$WF" > "$L/lint-ok.txt" 2>&1; LINT_OK_RC=$?
"$MHL" lint "$BADDIR" > "$L/lint-badtype.txt" 2>&1; LINT_BAD_RC=$?
log "lint approvers:[\"carol\"] rc=$LINT_OK_RC ; lint approvers:123 rc=$LINT_BAD_RC"
grep -qi 'unknown property\|propriedade' "$L/lint-ok.txt" && need "mhl lint não deve tratar 'approvers' como propriedade desconhecida"
[[ "$LINT_OK_RC" -eq 0 ]] || grep -qi 'approvers' "$L/lint-ok.txt" || need "mhl lint deve aceitar 'approvers:' (lista de strings) sem erro alheio"
[[ "$LINT_BAD_RC" -ne 0 ]] || need "mhl lint deve rejeitar 'approvers: 123' (tipo errado)"

STATE="$(mktmp)"; ADDR="127.0.0.1:8770"; BASE="http://$ADDR"; P=X-Mhl-Principal
PID=$(boot "$ADDR" "$BASE" "$L/mcp-server.log" "$STATE" --principal-header "$P") && track "$PID" || {
  log "FAIL: servidor não subiu (approvers: pode ter quebrado o parse do dir de workflows)"; tail -20 "$L/mcp-server.log" >>"$CLIENT_LOG"; echo FAIL; exit 1; }

ASID=$(initsid "$BASE" alice); CSID=$(initsid "$BASE" carol); BSID=$(initsid "$BASE" bob)

# alice (dona) inicia e a run para no gate
rpc "$BASE" "$ASID" '{"jsonrpc":"2.0","id":2,"method":"run/start","params":{"name":"DocPipeline","arguments":{"repo":"pub","approved":"no"}}}' "$L/alice-start.json" alice
RID=$(jget "$L/alice-start.json" result.runId)
for _ in $(seq 1 12); do
  rpc "$BASE" "$ASID" '{"jsonrpc":"2.0","id":3,"method":"run/status","params":{"runId":"'"$RID"'"}}' "$L/alice-status.json" alice
  [[ "$(jget "$L/alice-status.json" result.state)" == failed ]] && break; sleep 1
done
log "alice run/start -> $RID (parada no gate: $(jget "$L/alice-status.json" result.step))"

# regressão: bob (não-dono) -> -32602
rpc "$BASE" "$BSID" '{"jsonrpc":"2.0","id":4,"method":"run/status","params":{"runId":"'"$RID"'"}}' "$L/bob-status.json" bob
BOB=$(jget "$L/bob-status.json" error.code)
[[ "$BOB" == "-32602" ]] || need "REGRESSÃO: não-dono deve receber -32602 (recebeu '$BOB')"

# alvo 2: alice (dona, NÃO em approvers) run/resume -> forbidden, run segue parada
rpc "$BASE" "$ASID" '{"jsonrpc":"2.0","id":5,"method":"run/resume","params":{"runId":"'"$RID"'","arguments":{"approved":"yes"}}}' "$L/alice-resume.json" alice
A_RES_ERR=$(jget "$L/alice-resume.json" error.code)
sleep 1
rpc "$BASE" "$ASID" '{"jsonrpc":"2.0","id":6,"method":"run/status","params":{"runId":"'"$RID"'"}}' "$L/alice-status2.json" alice
A_STATE_AFTER=$(jget "$L/alice-status2.json" result.state)
log "alice (não-approver) run/resume -> err='$A_RES_ERR' ; run agora: '$A_STATE_AFTER'"
if [[ -z "$A_RES_ERR" || "$A_STATE_AFTER" == "completed" ]]; then
  need "run/resume por um principal FORA de approvers deve ser recusado (hoje: err='$A_RES_ERR', run='$A_STATE_AFTER')"
fi

# alvo 3: carol (em approvers) run/resume -> completed
if [[ -n "$A_RES_ERR" ]]; then
  rpc "$BASE" "$CSID" '{"jsonrpc":"2.0","id":7,"method":"run/resume","params":{"runId":"'"$RID"'","arguments":{"approved":"yes"}}}' "$L/carol-resume.json" carol
  C_DONE=""
  for _ in $(seq 1 15); do
    rpc "$BASE" "$CSID" '{"jsonrpc":"2.0","id":8,"method":"run/status","params":{"runId":"'"$RID"'"}}' "$L/carol-status.json" carol
    s=$(jget "$L/carol-status.json" result.state); [[ "$s" == completed || "$s" == failed ]] && { C_DONE="$s"; break; }; sleep 1
  done
  log "carol (approver) run/resume -> '$C_DONE'"
  # nota: carol não é a dona; se o modelo for "approver também precisa ser dono", ajustar o alvo.
  [[ "$C_DONE" == "completed" ]] || need "run/resume por um principal EM approvers deve completar a run (hoje: '$C_DONE')"
fi

verdict "approvers: reconhecido pelo lint e imposto no run/resume contra o principal"
