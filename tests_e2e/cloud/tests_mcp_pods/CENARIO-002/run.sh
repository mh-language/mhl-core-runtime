#!/usr/bin/env bash
# CENARIO-002 — Teste de Listagem de Tools
#
# Dado que o servidor MCP está em execução
# Quando o cliente solicita a listagem de ferramentas disponíveis
# Então o servidor MCP deve retornar a lista completa de ferramentas
#
# Usa o binário `mhl` copiado para esta pasta.

set -u

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MHL="$HERE/mhl"
WORKFLOWS="$HERE/../.."          # tests/cloud (docs-workflow.mh / slow-build.mh)
ADDR="127.0.0.1:8712"
BASE="http://$ADDR"
TOKEN="cenario-002-$(date +%s)"
STATE="$(mktemp -d)"
SERVER_LOG="$HERE/logs/mcp-server.log"
CLIENT_LOG="$HERE/logs/client.log"
INIT_JSON="$HERE/logs/initialize-response.json"
LIST_JSON="$HERE/logs/tools-list-response.json"

mkdir -p "$HERE/logs"
: > "$SERVER_LOG"; : > "$CLIENT_LOG"; : > "$INIT_JSON"; : > "$LIST_JSON"

log() { echo "[$(date '+%H:%M:%S')] $*" | tee -a "$CLIENT_LOG"; }

cleanup() {
  if [[ -n "${SERVER_PID:-}" ]] && kill -0 "$SERVER_PID" 2>/dev/null; then
    kill -TERM "$SERVER_PID" 2>/dev/null
    wait "$SERVER_PID" 2>/dev/null
  fi
  rm -rf "$STATE"
}
trap cleanup EXIT

# ── Dado que o servidor MCP está em execução ─────────────────────────────────
log "iniciando: $MHL serve mcp --http --addr $ADDR (workflows: $(cd "$WORKFLOWS" && pwd))"
"$MHL" serve mcp --http \
  --addr "$ADDR" \
  --token "$TOKEN" \
  --state-dir "$STATE" \
  "$WORKFLOWS" >>"$SERVER_LOG" 2>&1 &
SERVER_PID=$!
log "servidor PID=$SERVER_PID"

ready=""
for i in $(seq 1 50); do
  kill -0 "$SERVER_PID" 2>/dev/null || { log "FALHA: processo do servidor terminou na inicialização"; break; }
  code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/healthz" 2>/dev/null)
  [[ "$code" == "200" ]] && { ready="yes"; log "healthz respondeu 200 (tentativa $i)"; break; }
  sleep 0.2
done
[[ "$ready" == "yes" ]] || { log "RESULTADO: NÃO FUNCIONOU — servidor não ficou pronto"; echo "FAIL"; exit 1; }

# handshake: initialize (obrigatório antes de tools/list; abre a sessão)
INIT_HDRS="$HERE/logs/initialize-headers.txt"
init_code=$(curl -s -D "$INIT_HDRS" -o "$INIT_JSON" -w '%{http_code}' \
  -X POST "$BASE/mcp" \
  -H 'content-type: application/json' \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"cenario-002-client","version":"1.0.0"}}}')
SID=$(awk 'tolower($1)=="mcp-session-id:"{print $2}' "$INIT_HDRS" | tr -d '\r')
log "POST /mcp initialize -> HTTP $init_code ; Mcp-Session-Id: ${SID:-<ausente>}"

# ── Quando o cliente solicita a listagem de ferramentas ─────────────────────
list_code=$(curl -s -o "$LIST_JSON" -w '%{http_code}' \
  -X POST "$BASE/mcp" \
  -H 'content-type: application/json' \
  -H "Authorization: Bearer $TOKEN" \
  -H "Mcp-Session-Id: $SID" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list"}')
log "POST /mcp tools/list -> HTTP $list_code"
log "corpo da resposta:"
python3 -m json.tool "$LIST_JSON" 2>/dev/null | tee -a "$CLIENT_LOG" || cat "$LIST_JSON" | tee -a "$CLIENT_LOG"
echo | tee -a "$CLIENT_LOG"

# ── Então o servidor deve retornar a lista completa de ferramentas ──────────
read -r TOOL_COUNT TOOL_NAMES WITH_DESC WITH_SCHEMA WF_COUNT CTL_COUNT <<EOF
$(python3 - "$LIST_JSON" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
tools = d.get("result", {}).get("tools", [])
names = ",".join(t.get("name", "?") for t in tools) or "-"
with_desc = sum(1 for t in tools if t.get("description"))
with_schema = sum(1 for t in tools if t.get("inputSchema"))
# The HTTP transport also lists the synthetic mhl_run_* control tools that
# bridge a tools/call to the run/* async family — they are not workflows.
ctl = sum(1 for t in tools if t.get("name", "").startswith("mhl_run_"))
wf = len(tools) - ctl
print(len(tools), names, with_desc, with_schema, wf, ctl)
PY
)
EOF
log "tools retornadas: count=$TOOL_COUNT (workflows=$WF_COUNT, control=$CTL_COUNT) names=[$TOOL_NAMES] com_descricao=$WITH_DESC com_inputSchema=$WITH_SCHEMA"

# servidor anuncia N tools no log de inicialização — comparar com o retorno
ANNOUNCED=$(sed -n 's/.*: \([0-9][0-9]*\) tool(s) from.*/\1/p' "$SERVER_LOG" | head -1)
log "servidor anunciou no startup: ${ANNOUNCED:-?} tool(s)"

ok="yes"
[[ "$init_code" == "200" ]]                         || { ok="no"; log "  ! initialize HTTP != 200"; }
[[ "$list_code" == "200" ]]                         || { ok="no"; log "  ! tools/list HTTP != 200"; }
grep -q '"result"' "$LIST_JSON"                     || { ok="no"; log "  ! resposta sem result"; }
[[ "${TOOL_COUNT:-0}" -ge 1 ]]                      || { ok="no"; log "  ! nenhuma tool retornada"; }
[[ "$WF_COUNT" == "${ANNOUNCED:-x}" ]]              || { ok="no"; log "  ! contagem de workflows difere do anúncio de startup"; }
[[ "${CTL_COUNT:-0}" -eq 6 ]]                       || { ok="no"; log "  ! esperados 6 control tools mhl_run_* (async), vistos ${CTL_COUNT:-0}"; }
echo "$TOOL_NAMES" | grep -q "DocPipeline"          || { ok="no"; log "  ! DocPipeline ausente"; }
echo "$TOOL_NAMES" | grep -q "SlowBuild"            || { ok="no"; log "  ! SlowBuild ausente"; }
echo "$TOOL_NAMES" | grep -q "mhl_run_start"        || { ok="no"; log "  ! mhl_run_start ausente"; }
[[ "${WITH_DESC:-0}" -eq "${TOOL_COUNT:-0}" ]]      || { ok="no"; log "  ! nem toda tool tem description"; }
[[ "${WITH_SCHEMA:-0}" -eq "${TOOL_COUNT:-0}" ]]    || { ok="no"; log "  ! nem toda tool tem inputSchema"; }

if [[ "$ok" == "yes" ]]; then
  log "RESULTADO: FUNCIONOU — $TOOL_COUNT tools listadas ($TOOL_NAMES), todas com description e inputSchema"
  echo "PASS"; exit 0
else
  log "RESULTADO: NÃO FUNCIONOU — veja as verificações acima"
  echo "FAIL"; exit 1
fi
