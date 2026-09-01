#!/usr/bin/env bash
# CENARIO-002b — Teste de Listagem de Tools usando o protocolo MCP stateless
#
# Dado que o servidor MCP está em execução
# Quando o cliente solicita a listagem de ferramentas disponíveis
# E o protocolo informado é stateless
# Então o servidor MCP deve retornar a lista completa de ferramentas
#
# Stateless = sem `initialize`, sem header `Mcp-Session-Id`; cada request carrega
# params._meta com io.modelcontextprotocol/protocolVersion (2026-07-28) e
# io.modelcontextprotocol/clientCapabilities.
#
# Usa o binário `mhl` copiado para esta pasta.

set -u

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MHL="$HERE/mhl"
WORKFLOWS="$HERE/../.."          # tests/cloud
ADDR="127.0.0.1:8713"
BASE="http://$ADDR"
TOKEN="cenario-002b-$(date +%s)"
STATE="$(mktemp -d)"
PROTO="2026-07-28"

SERVER_LOG="$HERE/logs-stateless/mcp-server.log"
CLIENT_LOG="$HERE/logs-stateless/client.log"
LIST_JSON="$HERE/logs-stateless/tools-list-response.json"
LIST_HDRS="$HERE/logs-stateless/tools-list-headers.txt"
NOMETA_JSON="$HERE/logs-stateless/tools-list-no-meta-response.json"
BADVER_JSON="$HERE/logs-stateless/tools-list-bad-version-response.json"

mkdir -p "$HERE/logs-stateless"
: > "$SERVER_LOG"; : > "$CLIENT_LOG"

log() { echo "[$(date '+%H:%M:%S')] $*" | tee -a "$CLIENT_LOG"; }

cleanup() {
  if [[ -n "${SERVER_PID:-}" ]] && kill -0 "$SERVER_PID" 2>/dev/null; then
    kill -TERM "$SERVER_PID" 2>/dev/null; wait "$SERVER_PID" 2>/dev/null
  fi
  rm -rf "$STATE"
}
trap cleanup EXIT

# ── Dado que o servidor MCP está em execução ─────────────────────────────────
log "iniciando: $MHL serve mcp --http --addr $ADDR (workflows: $(cd "$WORKFLOWS" && pwd))"
"$MHL" serve mcp --http --addr "$ADDR" --token "$TOKEN" --state-dir "$STATE" \
  "$WORKFLOWS" >>"$SERVER_LOG" 2>&1 &
SERVER_PID=$!
log "servidor PID=$SERVER_PID"

ready=""
for i in $(seq 1 50); do
  kill -0 "$SERVER_PID" 2>/dev/null || { log "FALHA: processo do servidor terminou"; break; }
  [[ "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/healthz")" == "200" ]] && { ready="yes"; log "healthz 200 (tentativa $i)"; break; }
  sleep 0.2
done
[[ "$ready" == "yes" ]] || { log "RESULTADO: NÃO FUNCIONOU — servidor não ficou pronto"; echo "FAIL"; exit 1; }

# ── Quando o cliente solicita tools/list / E o protocolo informado é stateless ─
# SEM initialize, SEM Mcp-Session-Id. params._meta carrega os campos 2026-07-28.
REQ='{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"'"$PROTO"'","io.modelcontextprotocol/clientCapabilities":{}}}}'
log "request stateless:"
echo "$REQ" | python3 -m json.tool | tee -a "$CLIENT_LOG"

list_code=$(curl -s -D "$LIST_HDRS" -o "$LIST_JSON" -w '%{http_code}' \
  -X POST "$BASE/mcp" \
  -H 'content-type: application/json' \
  -H "Authorization: Bearer $TOKEN" \
  -H "MCP-Protocol-Version: $PROTO" \
  -d "$REQ")
log "POST /mcp tools/list (stateless) -> HTTP $list_code"
SID_HDR=$(awk 'tolower($1)=="mcp-session-id:"{print $2}' "$LIST_HDRS" | tr -d '\r')
log "header Mcp-Session-Id na resposta: ${SID_HDR:-<ausente, como esperado no stateless>}"
log "corpo da resposta:"
python3 -m json.tool "$LIST_JSON" 2>/dev/null | tee -a "$CLIENT_LOG" || cat "$LIST_JSON" | tee -a "$CLIENT_LOG"
echo | tee -a "$CLIENT_LOG"

# ── controles negativos ────────────────────────────────────────────────────
# (a) sem params._meta -> -32602
curl -s -o "$NOMETA_JSON" -X POST "$BASE/mcp" \
  -H 'content-type: application/json' -H "Authorization: Bearer $TOKEN" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' >/dev/null
nometa_code=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1])).get("error",{}).get("code","<sem erro>"))' "$NOMETA_JSON")
log "controle (a) tools/list sem _meta -> error.code=$nometa_code (esperado -32602)"

# (b) _meta com protocolVersion não suportada -> -32022
curl -s -o "$BADVER_JSON" -X POST "$BASE/mcp" \
  -H 'content-type: application/json' -H "Authorization: Bearer $TOKEN" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/list","params":{"_meta":{"io.modelcontextprotocol/protocolVersion":"1999-01-01","io.modelcontextprotocol/clientCapabilities":{}}}}' >/dev/null
badver_code=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1])).get("error",{}).get("code","<sem erro>"))' "$BADVER_JSON")
log "controle (b) tools/list com protocolVersion inválida -> error.code=$badver_code (esperado -32022)"

# ── Então o servidor deve retornar a lista completa de ferramentas ──────────
read -r COUNT NAMES WITH_DESC WITH_SCHEMA RESULT_TYPE SRV_INFO <<EOF
$(python3 - "$LIST_JSON" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
res = d.get("result", {})
tools = res.get("tools", [])
names = ",".join(t.get("name","?") for t in tools) or "-"
wd = sum(1 for t in tools if t.get("description"))
ws = sum(1 for t in tools if t.get("inputSchema"))
rt = res.get("resultType", "-")
si = res.get("_meta", {}).get("io.modelcontextprotocol/serverInfo", {})
si = f'{si.get("name","?")}/{si.get("version","?")}' if si else "-"
print(len(tools), names, wd, ws, rt, si)
PY
)
EOF
log "stateless tools/list: count=$COUNT names=[$NAMES] com_descricao=$WITH_DESC com_inputSchema=$WITH_SCHEMA resultType=$RESULT_TYPE serverInfo=$SRV_INFO"

ANNOUNCED=$(sed -n 's/.*: \([0-9][0-9]*\) tool(s) from.*/\1/p' "$SERVER_LOG" | head -1)
log "servidor anunciou no startup: ${ANNOUNCED:-?} tool(s)"

ok="yes"
[[ "$list_code" == "200" ]]                      || { ok="no"; log "  ! HTTP != 200"; }
grep -q '"result"' "$LIST_JSON"                  || { ok="no"; log "  ! resposta sem result"; }
[[ "${COUNT:-0}" -ge 1 ]]                        || { ok="no"; log "  ! nenhuma tool retornada"; }
[[ "$COUNT" == "${ANNOUNCED:-x}" ]]              || { ok="no"; log "  ! contagem difere do startup"; }
echo "$NAMES" | grep -q "DocPipeline"            || { ok="no"; log "  ! DocPipeline ausente"; }
echo "$NAMES" | grep -q "SlowBuild"              || { ok="no"; log "  ! SlowBuild ausente"; }
[[ "${WITH_DESC:-0}" -eq "${COUNT:-0}" ]]        || { ok="no"; log "  ! nem toda tool tem description"; }
[[ "${WITH_SCHEMA:-0}" -eq "${COUNT:-0}" ]]      || { ok="no"; log "  ! nem toda tool tem inputSchema"; }
[[ "$RESULT_TYPE" == "complete" ]]               || { ok="no"; log "  ! resultType != complete (decoração stateless)"; }
[[ "$SRV_INFO" == mhl/* ]]                       || { ok="no"; log "  ! _meta serverInfo ausente"; }
[[ -z "$SID_HDR" ]]                              || { ok="no"; log "  ! stateless não deveria emitir Mcp-Session-Id"; }
[[ "$nometa_code" == "-32602" ]]                 || { ok="no"; log "  ! sem _meta deveria dar -32602"; }
[[ "$badver_code" == "-32022" ]]                 || { ok="no"; log "  ! versão inválida deveria dar -32022"; }

if [[ "$ok" == "yes" ]]; then
  log "RESULTADO: FUNCIONOU — $COUNT tools via stateless ($NAMES); resultType=complete; serverInfo=$SRV_INFO; sem sessão"
  echo "PASS"; exit 0
else
  log "RESULTADO: NÃO FUNCIONOU — veja as verificações acima"
  echo "FAIL"; exit 1
fi
