#!/usr/bin/env bash
# ITEM-02 (alvo) — sessões MCP compartilhadas entre réplicas
source "$(dirname "${BASH_SOURCE[0]}")/../_lib.sh"

########################################################################
# Fase 1 — sessão compartilhada (sem --principal-header)
########################################################################
S1="$(mktmp)"
A="127.0.0.1:8763"; AB="http://$A"; B="127.0.0.1:8764"; BB="http://$B"
: > "$L/podA.log"; : > "$L/podB.log"
AP=$(boot "$A" "$AB" "$L/podA.log" "$S1") && track "$AP" || { log "FAIL: Pod A"; echo FAIL; exit 1; }
BP=$(boot "$B" "$BB" "$L/podB.log" "$S1") && track "$BP" || { log "FAIL: Pod B"; echo FAIL; exit 1; }
log "Fase 1: Pod A=$AP ($A) Pod B=$BP ($B) --state-dir=$S1"

# initialize no Pod A
SIDA=$(initsid "$AB")
log "sidA (cunhado no Pod A) = $SIDA"

# alvo 1: tools/list no Pod B com sidA -> 200
FC=$("${CURL[@]}" -o "$L/B-foreign-session.txt" -w '%{http_code}' -X POST "$BB/mcp" \
  -H 'content-type: application/json' -H "Authorization: Bearer $TOKEN" -H "Mcp-Session-Id: $SIDA" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}')
log "  tools/list no Pod B com sidA -> HTTP $FC"
[[ "$FC" == "200" ]] || need "o Pod B deve reconhecer o Mcp-Session-Id cunhado pelo Pod A (hoje: HTTP $FC)"

# alvo 2: run/start no A e run/status no B, MESMA sessão
rpc "$AB" "$SIDA" '{"jsonrpc":"2.0","id":3,"method":"run/start","params":{"name":"DocPipeline","arguments":{"repo":"r","approved":"no"}}}' "$L/A-start.json"
RID=$(jget "$L/A-start.json" result.runId)
for _ in $(seq 1 12); do
  rpc "$AB" "$SIDA" '{"jsonrpc":"2.0","id":4,"method":"run/status","params":{"runId":"'"$RID"'"}}' "$L/A-status.json"
  [[ "$(jget "$L/A-status.json" result.state)" == failed ]] && break; sleep 1
done
rpc "$BB" "$SIDA" '{"jsonrpc":"2.0","id":5,"method":"run/status","params":{"runId":"'"$RID"'"}}' "$L/B-crosspod-status.json"
B_STATE=$(jget "$L/B-crosspod-status.json" result.state)
B_ERR=$(jget "$L/B-crosspod-status.json" error.code)
log "  run/start no A + run/status no B (mesma sessão $SIDA): state='$B_STATE' err='$B_ERR'"
[[ -z "$B_ERR" && -n "$B_STATE" ]] || need "run/status no Pod B com a sessão do Pod A deve funcionar (hoje: err='$B_ERR')"

########################################################################
# Fase 2 — regressão: --principal-header isola entre pods (deve continuar MET)
########################################################################
S2="$(mktmp)"
A2="127.0.0.1:8765"; AB2="http://$A2"; B2="127.0.0.1:8766"; BB2="http://$B2"; P=X-Mhl-Principal
: > "$L/podA-p.log"; : > "$L/podB-p.log"
AP2=$(boot "$A2" "$AB2" "$L/podA-p.log" "$S2" --principal-header "$P") && track "$AP2" || { log "FAIL: Pod A/p2"; echo FAIL; exit 1; }
BP2=$(boot "$B2" "$BB2" "$L/podB-p.log" "$S2" --principal-header "$P") && track "$BP2" || { log "FAIL: Pod B/p2"; echo FAIL; exit 1; }
AS=$(initsid "$AB2" alice); BB_=$(initsid "$BB2" bob); BA=$(initsid "$BB2" alice)
rpc "$AB2" "$AS" '{"jsonrpc":"2.0","id":2,"method":"run/start","params":{"name":"DocPipeline","arguments":{"repo":"s","approved":"no"}}}' "$L/C-start.json" alice
CRID=$(jget "$L/C-start.json" result.runId)
for _ in $(seq 1 12); do
  rpc "$AB2" "$AS" '{"jsonrpc":"2.0","id":3,"method":"run/status","params":{"runId":"'"$CRID"'"}}' "$L/C-a.json" alice
  [[ "$(jget "$L/C-a.json" result.state)" == failed ]] && break; sleep 1
done
rpc "$BB2" "$BB_" '{"jsonrpc":"2.0","id":4,"method":"run/status","params":{"runId":"'"$CRID"'"}}' "$L/C-bob-status.json" bob
rpc "$BB2" "$BA" '{"jsonrpc":"2.0","id":5,"method":"run/status","params":{"runId":"'"$CRID"'"}}' "$L/C-alice-status.json" alice
C_BOB=$(jget "$L/C-bob-status.json" error.code)
C_ALICE=$(jget "$L/C-alice-status.json" result.state)
log "  --principal-header: bob no Pod B -> err='$C_BOB' ; alice no Pod B -> state='$C_ALICE'"
[[ "$C_BOB" == "-32602" ]] || need "REGRESSÃO: com --principal-header, bob no Pod B não deveria enxergar a run de alice (err='$C_BOB')"
[[ "$C_ALICE" == "failed" ]] || need "REGRESSÃO: com --principal-header, alice no Pod B deveria enxergar a própria run (state='$C_ALICE')"

verdict "sessão reconhecida nos dois pods; isolamento por principal preservado entre réplicas"
