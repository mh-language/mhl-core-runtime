#!/usr/bin/env bash
# CENARIO-001 — Teste de Conexão com o Servidor MCP
#
# Dado que o servidor MCP está em execução
# Quando o cliente tenta se conectar ao servidor MCP
# Então a conexão deve ser estabelecida com sucesso
#
# Usa o binário `mhl` copiado para esta pasta (../../mhl -> ./mhl).

set -u

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MHL="$HERE/mhl"
WORKFLOWS="$HERE/../.."          # sample/cloud (contém docs-workflow.mh / slow-build.mh)
ADDR="127.0.0.1:8711"
BASE="http://$ADDR"
TOKEN="cenario-001-$(date +%s)"
STATE="$(mktemp -d)"
SERVER_LOG="$HERE/logs/mcp-server.log"
CLIENT_LOG="$HERE/logs/client.log"
RESULT_JSON="$HERE/logs/initialize-response.json"

mkdir -p "$HERE/logs"
: > "$SERVER_LOG"; : > "$CLIENT_LOG"; : > "$RESULT_JSON"

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

# aguarda readiness via probe não-autenticado
ready=""
for i in $(seq 1 50); do
  if ! kill -0 "$SERVER_PID" 2>/dev/null; then
    log "FALHA: o processo do servidor terminou durante a inicialização"
    break
  fi
  code=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/healthz" 2>/dev/null)
  if [[ "$code" == "200" ]]; then ready="yes"; log "healthz respondeu 200 (tentativa $i)"; break; fi
  sleep 0.2
done

if [[ "$ready" != "yes" ]]; then
  log "RESULTADO: NÃO FUNCIONOU — servidor não ficou pronto"
  echo "FAIL"
  exit 1
fi

# ── Quando o cliente tenta se conectar ao servidor MCP ──────────────────────
# Handshake MCP: POST /mcp  method=initialize  (com o bearer gateway<->mhl)
HDRS="$HERE/logs/initialize-headers.txt"
http_code=$(curl -s -D "$HDRS" -o "$RESULT_JSON" -w '%{http_code}' \
  -X POST "$BASE/mcp" \
  -H 'content-type: application/json' \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"cenario-001-client","version":"1.0.0"}}}')

SID=$(awk 'tolower($1)=="mcp-session-id:"{print $2}' "$HDRS" | tr -d '\r')

log "POST /mcp initialize -> HTTP $http_code"
log "Mcp-Session-Id: ${SID:-<ausente>}"
log "corpo da resposta:"
cat "$RESULT_JSON" | tee -a "$CLIENT_LOG"
echo | tee -a "$CLIENT_LOG"

# controle negativo: sem bearer deve dar 401 (prova que o guard está ativo)
neg_code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/mcp" \
  -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{}}}')
log "controle negativo (sem token) -> HTTP $neg_code (esperado 401)"

# ── Então a conexão deve ser estabelecida com sucesso ──────────────────────
ok="yes"
[[ "$http_code" == "200" ]]                          || { ok="no"; log "  ! HTTP != 200"; }
grep -q '"result"' "$RESULT_JSON"                    || { ok="no"; log "  ! resposta sem campo result"; }
grep -q '"protocolVersion"' "$RESULT_JSON"           || { ok="no"; log "  ! resposta sem protocolVersion"; }
grep -q '"serverInfo"' "$RESULT_JSON"                || { ok="no"; log "  ! resposta sem serverInfo"; }
[[ -n "$SID" ]]                                      || { ok="no"; log "  ! sem Mcp-Session-Id"; }
[[ "$neg_code" == "401" ]]                           || { ok="no"; log "  ! controle negativo != 401"; }

if [[ "$ok" == "yes" ]]; then
  log "RESULTADO: FUNCIONOU — conexão MCP estabelecida (initialize OK, sessão $SID)"
  echo "PASS"
  exit 0
else
  log "RESULTADO: NÃO FUNCIONOU — veja as verificações acima"
  echo "FAIL"
  exit 1
fi
