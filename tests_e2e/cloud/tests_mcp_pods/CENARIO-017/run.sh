#!/usr/bin/env bash
# CENARIO-017 — assinatura tipada (workflow X(req: In): Out) sobre MCP HTTP.
#
# tools/list (inputSchema/outputSchema), tools/call (structuredContent = projeção
# validada), run/start → pause → run/resume parcial (merge no req do checkpoint),
# e contrato quebrado (isError sem structuredContent).
#
# Modo stateless (params._meta). Workflows próprios em CENARIO-017/workflow.

set -u

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MHL="$HERE/mhl"
[[ -x "$MHL" ]] || { cp "$HERE/../../mhl" "$MHL" 2>/dev/null && chmod +x "$MHL"; }
[[ -x "$MHL" ]] || { echo "FALHA: binário mhl não encontrado"; exit 1; }

WORKFLOWS="$HERE/workflow"
ADDR="127.0.0.1:8738"
BASE="http://$ADDR"
TOKEN="cenario-017-$(date +%s)"
STATE="$(mktemp -d)"
META='"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}'

L="$HERE/logs"; mkdir -p "$L"
SL="$L/mcp-server.log"; : > "$SL"
CLIENT_LOG="$L/client.log"; : > "$CLIENT_LOG"
log() { echo "[$(date '+%H:%M:%S')] $*" | tee -a "$CLIENT_LOG"; }
cleanup() { [[ -n "${PID:-}" ]] && kill -0 "$PID" 2>/dev/null && { kill -TERM "$PID" 2>/dev/null; wait "$PID" 2>/dev/null; }; rm -rf "$STATE"; }
trap cleanup EXIT

log "iniciando servidor em $ADDR"
"$MHL" serve mcp --http --addr "$ADDR" --token "$TOKEN" --state-dir "$STATE" \
  "$WORKFLOWS" >>"$SL" 2>&1 &
PID=$!
for i in $(seq 1 50); do
  kill -0 "$PID" 2>/dev/null || { log "FALHA: servidor terminou"; echo "FAIL"; exit 1; }
  [[ "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/healthz")" == "200" ]] && break
  sleep 0.2
done

post() { curl -s -o "$2" -w '%{http_code}' -X POST "$BASE/mcp" \
  -H 'content-type: application/json' -H "Authorization: Bearer $TOKEN" -d "$1"; }

# check <descrição> <arquivo> <expressão python sobre d> — registra e acumula o veredito.
ok="yes"
check() {
  if python3 -c 'import json,sys
d=json.load(open(sys.argv[1]))
sys.exit(0 if eval(sys.argv[2]) else 1)' "$2" "$3" 2>>"$CLIENT_LOG"; then
    log "  ok  $1"
  else
    ok="no"; log "  !!  $1"; python3 -m json.tool "$2" | tee -a "$CLIENT_LOG"
  fi
}

# ── tools/list ─────────────────────────────────────────────────────
post '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{'"$META"'}}' "$L/tools-list.json" >/dev/null
log "tools/list"
REVIEW='[t for t in d["result"]["tools"] if t["name"]=="Review"][0]'
check "inputSchema.required == [diff]" "$L/tools-list.json" "$REVIEW[\"inputSchema\"][\"required\"] == [\"diff\"]"
check "inputSchema fechado" "$L/tools-list.json" "$REVIEW[\"inputSchema\"][\"additionalProperties\"] is False"
check "outputSchema.title == ReviewOutput" "$L/tools-list.json" "$REVIEW[\"outputSchema\"][\"title\"] == \"ReviewOutput\""

# ── tools/call síncrono ────────────────────────────────────────────
post '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"Review","arguments":{"diff":"d1","approved":"yes"},'"$META"'}}' "$L/tools-call.json" >/dev/null
log "tools/call Review"
check "isError false" "$L/tools-call.json" 'd["result"]["isError"] is False'
check "structuredContent = projeção" "$L/tools-call.json" 'd["result"]["structuredContent"] == {"approved": True, "summary": "d1@main"}'

# ── run/start → pause → run/resume parcial ─────────────────────────
post '{"jsonrpc":"2.0","id":3,"method":"run/start","params":{"name":"Review","arguments":{"diff":"d2"},'"$META"'}}' "$L/run-start.json" >/dev/null
RID=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["result"]["runId"])' "$L/run-start.json")
log "run/start → $RID"
poll() {
  for i in $(seq 1 100); do
    post '{"jsonrpc":"2.0","id":4,"method":"run/status","params":{"runId":"'"$RID"'",'"$META"'}}' "$L/$1" >/dev/null
    st=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["result"]["state"])' "$L/$1")
    case "$st" in completed|failed|canceled|paused) return;; esac
    sleep 0.1
  done
}
poll run-status-paused.json
check "pausada em Gate" "$L/run-status-paused.json" 'd["result"]["state"] == "paused" and d["result"]["step"] == "Gate"'

post '{"jsonrpc":"2.0","id":5,"method":"run/resume","params":{"runId":"'"$RID"'","arguments":{"approved":"yes","base":"dev"},'"$META"'}}' "$L/run-resume.json" >/dev/null
poll run-status-final.json
check "completou após resume" "$L/run-status-final.json" 'd["result"]["state"] == "completed"'
check "vars = projeção (diff veio do checkpoint)" "$L/run-status-final.json" 'd["result"]["vars"] == {"approved": True, "summary": "d2@main"}'

# ── contrato quebrado ──────────────────────────────────────────────
post '{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"Broken","arguments":{},'"$META"'}}' "$L/tools-call-broken.json" >/dev/null
log "tools/call Broken"
check "isError true" "$L/tools-call-broken.json" 'd["result"]["isError"] is True'
check "sem structuredContent" "$L/tools-call-broken.json" '"structuredContent" not in d["result"]'
check "texto cita o contrato" "$L/tools-call-broken.json" '"does not satisfy Status" in d["result"]["content"][0]["text"]'

# ── veredito ───────────────────────────────────────────────────────
if [[ "$ok" == "yes" ]]; then
  log "RESULTADO: FUNCIONOU — assinatura tipada respeitada ponta a ponta"
  echo "PASS"; exit 0
else
  log "RESULTADO: NÃO FUNCIONOU — ver checks marcados com !! acima"
  echo "FAIL"; exit 1
fi
