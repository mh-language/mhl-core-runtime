#!/usr/bin/env bash
# CENARIO-014 — Conformidade de protocolo JSON-RPC / MCP
#
# metodo desconhecido, corpo malformado, MCP-Protocol-Version invalida,
# negociacao do initialize, DELETE de sessao, sessao desconhecida,
# notificacao sem id, ping (legado vs stateless).
#
# Copia sample/cloud/mhl se preciso.

set -u

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MHL="$HERE/mhl"
[[ -x "$MHL" ]] || { cp "$HERE/../../mhl" "$MHL" 2>/dev/null && chmod +x "$MHL"; }
[[ -x "$MHL" ]] || { echo "FALHA: binário mhl não encontrado"; exit 1; }

WORKFLOWS="$HERE/../.."
ADDR="127.0.0.1:8735"
BASE="http://$ADDR"
TOKEN="cenario-014-$(date +%s)"
STATE="$(mktemp -d)"
META='"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}'

L="$HERE/logs"; mkdir -p "$L"
SL="$L/mcp-server.log"; : > "$SL"
CLIENT_LOG="$L/client.log"; : > "$CLIENT_LOG"
MATRIX="$L/matrix.txt"; : > "$MATRIX"
log() { echo "[$(date '+%H:%M:%S')] $*" | tee -a "$CLIENT_LOG"; }
cleanup() { [[ -n "${PID:-}" ]] && kill -0 "$PID" 2>/dev/null && { kill -TERM "$PID" 2>/dev/null; wait "$PID" 2>/dev/null; }; rm -rf "$STATE"; }
trap cleanup EXIT
jget() { python3 -c 'import json,sys
try: d=json.load(open(sys.argv[1]))
except Exception: print(""); sys.exit(0)
for p in sys.argv[2].split("."):
  d = d.get(p, {}) if isinstance(d, dict) else {}
print("" if d == {} else d)' "$1" "$2"; }
mrow() { printf '%-48s | %s\n' "$1" "$2" | tee -a "$MATRIX" "$CLIENT_LOG" >/dev/null; }

log "iniciando servidor em $ADDR"
"$MHL" serve mcp --http --addr "$ADDR" --token "$TOKEN" --state-dir "$STATE" \
  "$WORKFLOWS" >>"$SL" 2>&1 &
PID=$!
for i in $(seq 1 50); do
  kill -0 "$PID" 2>/dev/null || { log "FALHA: servidor terminou"; echo "FAIL"; exit 1; }
  [[ "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/healthz")" == "200" ]] && break
  sleep 0.2
done

new_session() {
  curl -s -D "$L/init-$1-headers.txt" -o "$L/init-$1.json" -X POST "$BASE/mcp" \
    -H 'content-type: application/json' -H "Authorization: Bearer $TOKEN" \
    -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{}}}'
  awk 'tolower($1)=="mcp-session-id:"{print $2}' "$L/init-$1-headers.txt" | tr -d '\r'
}
SID=$(new_session main)
SID_DEL=$(new_session todelete)
log "sessão principal=$SID ; sessão p/ DELETE=$SID_DEL"

P() { # <nome> <curl-args...> : grava corpo em $BF, retorna HTTP em $CODE
  local name="$1"; shift
  BF="$L/$(echo "$name" | tr ' /' '__').json"
  CODE=$(curl -s -o "$BF" -w '%{http_code}' "$@")
}

# 1) metodo desconhecido
P "unknown-method" -X POST "$BASE/mcp" -H 'content-type: application/json' -H "Authorization: Bearer $TOKEN" -H "Mcp-Session-Id: $SID" \
  -d '{"jsonrpc":"2.0","id":10,"method":"foo/bar"}'
UM_CODE=$(jget "$BF" error.code); UM_MSG=$(jget "$BF" error.message)
mrow "foo/bar (id, sessão)" "HTTP $CODE  code=$UM_CODE  msg=$UM_MSG"

# 2) corpo malformado
P "malformed-json" -X POST "$BASE/mcp" -H 'content-type: application/json' -H "Authorization: Bearer $TOKEN" -H "Mcp-Session-Id: $SID" \
  -d 'isto nao e json'
MJ_HTTP=$CODE; MJ_CODE=$(jget "$BF" error.code)
mrow "corpo não-JSON" "HTTP $CODE  code=$MJ_CODE"

# 3) MCP-Protocol-Version invalida
P "bad-proto-header" -X POST "$BASE/mcp" -H 'content-type: application/json' -H "Authorization: Bearer $TOKEN" -H "Mcp-Session-Id: $SID" \
  -H "MCP-Protocol-Version: 9999-99-99" \
  -d '{"jsonrpc":"2.0","id":11,"method":"tools/list"}'
BP_HTTP=$CODE; BP_CODE=$(jget "$BF" error.code); BP_MSG=$(jget "$BF" error.message)
mrow "header MCP-Protocol-Version: 9999-99-99" "HTTP $CODE  code=$BP_CODE  msg=$BP_MSG"

# 4) initialize com versao bogus -> negocia
P "init-bogus-version" -X POST "$BASE/mcp" -H 'content-type: application/json' -H "Authorization: Bearer $TOKEN" \
  -d '{"jsonrpc":"2.0","id":12,"method":"initialize","params":{"protocolVersion":"1999-01-01","capabilities":{}}}'
IB_HTTP=$CODE; IB_PV=$(jget "$BF" result.protocolVersion)
mrow "initialize protocolVersion=1999-01-01" "HTTP $CODE  result.protocolVersion=$IB_PV"

# 5) DELETE com sessao valida
DEL_OK=$(curl -s -o /dev/null -w '%{http_code}' -X DELETE "$BASE/mcp" -H "Authorization: Bearer $TOKEN" -H "Mcp-Session-Id: $SID_DEL")
mrow "DELETE /mcp (sessão válida)" "HTTP $DEL_OK"

# 6) DELETE com sessao inexistente
DEL_BAD=$(curl -s -o /dev/null -w '%{http_code}' -X DELETE "$BASE/mcp" -H "Authorization: Bearer $TOKEN" -H "Mcp-Session-Id: sessao-que-nao-existe")
mrow "DELETE /mcp (sessão inexistente)" "HTTP $DEL_BAD"

# 7) POST com sessao inexistente
POST_BAD=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/mcp" -H 'content-type: application/json' -H "Authorization: Bearer $TOKEN" -H "Mcp-Session-Id: sessao-que-nao-existe" \
  -d '{"jsonrpc":"2.0","id":13,"method":"tools/list"}')
mrow "POST /mcp (Mcp-Session-Id inexistente)" "HTTP $POST_BAD"

# 8) notificacao sem id
NOTIF=$(curl -s -o "$L/notif-body.txt" -w '%{http_code}' -X POST "$BASE/mcp" -H 'content-type: application/json' -H "Authorization: Bearer $TOKEN" -H "Mcp-Session-Id: $SID" \
  -d '{"jsonrpc":"2.0","method":"notifications/initialized"}')
NOTIF_LEN=$(wc -c < "$L/notif-body.txt" | tr -d ' ')
mrow "notificação notifications/initialized" "HTTP $NOTIF  corpo_bytes=$NOTIF_LEN"

# 9) ping em sessao legada
P "ping-legacy" -X POST "$BASE/mcp" -H 'content-type: application/json' -H "Authorization: Bearer $TOKEN" -H "Mcp-Session-Id: $SID" \
  -d '{"jsonrpc":"2.0","id":14,"method":"ping"}'
PING_L_HTTP=$CODE; PING_L_HASRESULT=$(python3 -c 'import json,sys;print("result" in json.load(open(sys.argv[1])))' "$BF" 2>/dev/null)
PING_L_ERR=$(jget "$BF" error.code)
mrow "ping (sessão legada)" "HTTP $CODE  hasResult=$PING_L_HASRESULT  err=$PING_L_ERR"

# 10) ping em modo stateless
P "ping-stateless" -X POST "$BASE/mcp" -H 'content-type: application/json' -H "Authorization: Bearer $TOKEN" \
  -d '{"jsonrpc":"2.0","id":15,"method":"ping","params":{'"$META"'}}'
PING_S_ERR=$(jget "$BF" error.code); PING_S_MSG=$(jget "$BF" error.message)
mrow "ping (stateless)" "HTTP $CODE  code=$PING_S_ERR  msg=$PING_S_MSG"

log "----- matriz -----"; cat "$MATRIX" | tee -a "$CLIENT_LOG"

# ── verdite ─────────────────────────────────────────────────────
ok="yes"
[[ "$UM_CODE" == "-32601" ]]                     || { ok="no"; log "  ! foo/bar code != -32601 ($UM_CODE)"; }
echo "$UM_MSG" | grep -q "foo/bar"               || { ok="no"; log "  ! msg de foo/bar não cita o método"; }
[[ "$MJ_HTTP" == "400" && "$MJ_CODE" == "-32700" ]] || { ok="no"; log "  ! corpo malformado != 400/-32700 ($MJ_HTTP/$MJ_CODE)"; }
[[ "$BP_HTTP" == "400" && "$BP_CODE" == "-32602" ]] || { ok="no"; log "  ! header versão inválida != 400/-32602 ($BP_HTTP/$BP_CODE)"; }
echo "$BP_MSG" | grep -qi "unsupported MCP-Protocol-Version" || { ok="no"; log "  ! msg do header versão inválida inesperada: $BP_MSG"; }
[[ "$IB_HTTP" == "200" && "$IB_PV" == "2025-06-18" ]] || { ok="no"; log "  ! initialize bogus não negociou p/ 2025-06-18 ($IB_HTTP/$IB_PV)"; }
[[ "$DEL_OK" == "204" ]]                          || { ok="no"; log "  ! DELETE sessão válida != 204 ($DEL_OK)"; }
[[ "$DEL_BAD" == "404" ]]                         || { ok="no"; log "  ! DELETE sessão inexistente != 404 ($DEL_BAD)"; }
[[ "$POST_BAD" == "404" ]]                        || { ok="no"; log "  ! POST sessão inexistente != 404 ($POST_BAD)"; }
[[ "$NOTIF" == "202" && "${NOTIF_LEN:-0}" -le 1 ]] || { ok="no"; log "  ! notificação != 202/corpo-vazio ($NOTIF/${NOTIF_LEN}b)"; }
[[ "$PING_L_HTTP" == "200" && "$PING_L_HASRESULT" == "True" ]] || { ok="no"; log "  ! ping legado sem result ($PING_L_HTTP/$PING_L_HASRESULT/err=$PING_L_ERR)"; }
[[ "$PING_S_ERR" == "-32601" ]]                   || { ok="no"; log "  ! ping stateless != -32601 ($PING_S_ERR)"; }

if [[ "$ok" == "yes" ]]; then
  log "RESULTADO: FUNCIONOU — todas as bordas de protocolo respondem conforme o contrato"
  echo "PASS"; exit 0
else
  log "RESULTADO: NÃO FUNCIONOU — veja as verificações acima"
  echo "FAIL"; exit 1
fi
