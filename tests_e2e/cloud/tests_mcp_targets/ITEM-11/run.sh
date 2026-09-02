#!/usr/bin/env bash
# ITEM-11 (alvo) — run/* anunciado e negociável
source "$(dirname "${BASH_SOURCE[0]}")/../_lib.sh"

STATE="$(mktmp)"; ADDR="127.0.0.1:8769"; BASE="http://$ADDR"
PID=$(boot "$ADDR" "$BASE" "$L/mcp-server.log" "$STATE") && track "$PID" || { log "FAIL: servidor não subiu"; echo FAIL; exit 1; }

"${CURL[@]}" -D "$L/init-hdr.txt" -o "$L/initialize.json" -X POST "$BASE/mcp" -H 'content-type: application/json' \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{}}}'
SID=$(awk 'tolower($1)=="mcp-session-id:"{print $2}' "$L/init-hdr.txt" | tr -d '\r')
python3 -m json.tool "$L/initialize.json" | tee -a "$CLIENT_LOG" >/dev/null

# alvo 1+2: existe uma capability que nomeia run/* com versão e cita os métodos?
ANALYSIS=$(python3 -c '
import json,sys,re
d=json.load(open(sys.argv[1]))
caps=d.get("result",{}).get("capabilities",{})
blob=json.dumps(caps).lower()
has_run_key = bool(re.search(r"\"(run|mhl\.run|mhl/run|async|asyncrun)\"", blob)) or ("run/start" in blob)
has_version = has_run_key and ("version" in blob)
methods=["run/start","run/status","run/resume","run/cancel","run/list","run/logs"]
cited=sum(1 for m in methods if m in blob)
print(f"{int(has_run_key)} {int(has_version)} {cited} {sorted(caps.keys())}")' "$L/initialize.json")
read -r HAS_KEY HAS_VER CITED CAPKEYS <<<"$ANALYSIS"
log "capabilities keys=$CAPKEYS ; nomeia run/*? $HAS_KEY ; com versão? $HAS_VER ; métodos citados=$CITED/6"

[[ "$HAS_KEY" == "1" ]] || need "initialize.capabilities deve ter uma chave que nomeia a família run/* (experimental/run/mhl.run)"
[[ "$HAS_VER" == "1" ]] || need "a capability de run/* deve trazer uma versão explícita"
[[ "${CITED:-0}" -ge 3 ]] || need "a capability deve listar/referenciar os métodos run/* (citados hoje: ${CITED:-0}/6)"

# ponto extra: tools/list sinaliza long-running
"${CURL[@]}" -o "$L/tools-list.json" -X POST "$BASE/mcp" -H 'content-type: application/json' \
  -H "Authorization: Bearer $TOKEN" -H "Mcp-Session-Id: $SID" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}'
LR=$(python3 -c 'import json,sys,re;print("yes" if re.search(r"long.running|async|run/start|blocking", json.dumps(json.load(open(sys.argv[1]))).lower()) else "no")' "$L/tools-list.json")
log "tools/list sinaliza long-running? $LR (ponto extra, não bloqueia MET)"

# regressão: método run/* inexistente -> -32601
"${CURL[@]}" -o "$L/run-bogus.json" -X POST "$BASE/mcp" -H 'content-type: application/json' \
  -H "Authorization: Bearer $TOKEN" -H "Mcp-Session-Id: $SID" \
  -d '{"jsonrpc":"2.0","id":3,"method":"run/frobnicate","params":{}}'
BOGUS=$(jget "$L/run-bogus.json" error.code)
[[ "$BOGUS" == "-32601" ]] || need "REGRESSÃO: run/<bogus> deve dar -32601 (deu '$BOGUS')"

verdict "run/* é anunciado em initialize com versão e lista de métodos"
