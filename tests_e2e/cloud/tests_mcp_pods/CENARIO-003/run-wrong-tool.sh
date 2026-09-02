#!/usr/bin/env bash
# CENARIO-003b — Chamada de uma Tool INCORRETA usando o protocolo MCP stateless
#
# Dado que o servidor MCP está em execução
# E o cliente está autenticado e autorizado a usar a ferramenta
# Quando o cliente chama uma ferramenta incorreta
# Então o servidor responde com um erro indicando que a ferramenta não foi encontrada
#
# Stateless = sem `initialize`, sem header `Mcp-Session-Id`; cada request carrega
# params._meta (io.modelcontextprotocol/protocolVersion 2026-07-28 + clientCapabilities).
# Usa o binário `mhl` copiado para esta pasta.

set -u

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MHL="$HERE/mhl"
WORKFLOWS="$HERE/../.."          # tests/cloud (DocPipeline / SlowBuild)
ADDR="127.0.0.1:8715"
BASE="http://$ADDR"
TOKEN="cenario-003b-$(date +%s)"
STATE="$(mktemp -d)"
PROTO="2026-07-28"
BADNAME="NaoExisteTool"

L="$HERE/logs-wrong-tool"
SERVER_LOG="$L/mcp-server.log"
CLIENT_LOG="$L/client.log"
BAD_JSON="$L/tools-call-wrong-name-response.json"
BAD_HDRS="$L/tools-call-wrong-name-headers.txt"
CASE_JSON="$L/tools-call-wrong-case-response.json"
OK_JSON="$L/tools-call-valid-name-response.json"

mkdir -p "$L"
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

meta='"_meta":{"io.modelcontextprotocol/protocolVersion":"'"$PROTO"'","io.modelcontextprotocol/clientCapabilities":{}}'

# ── Quando o cliente chama uma ferramenta incorreta (autenticado, stateless) ─
REQ='{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"'"$BADNAME"'","arguments":{"repo":"demo-api","approved":"yes"},'"$meta"'}}'
log "request stateless (tools/call name=$BADNAME — inexistente):"
echo "$REQ" | python3 -m json.tool | tee -a "$CLIENT_LOG"

bad_code=$(curl -s -D "$BAD_HDRS" -o "$BAD_JSON" -w '%{http_code}' \
  -X POST "$BASE/mcp" \
  -H 'content-type: application/json' \
  -H "Authorization: Bearer $TOKEN" \
  -H "MCP-Protocol-Version: $PROTO" \
  -d "$REQ")
log "POST /mcp tools/call (nome inexistente) -> HTTP $bad_code"
log "corpo da resposta:"
python3 -m json.tool "$BAD_JSON" 2>/dev/null | tee -a "$CLIENT_LOG" || cat "$BAD_JSON" | tee -a "$CLIENT_LOG"
echo | tee -a "$CLIENT_LOG"

# ── controle: nome válido com caixa errada também deve ser "não encontrada" ──
curl -s -o "$CASE_JSON" -X POST "$BASE/mcp" \
  -H 'content-type: application/json' -H "Authorization: Bearer $TOKEN" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"docpipeline","arguments":{"repo":"x","approved":"yes"},'"$meta"'}}' >/dev/null
case_code=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1])).get("error",{}).get("code","<sem erro>"))' "$CASE_JSON")
log "controle (nome 'docpipeline', caixa errada) -> error.code=$case_code"

# ── controle: nome VÁLIDO ainda funciona (prova que é o nome, não o transporte) ─
curl -s -o "$OK_JSON" -X POST "$BASE/mcp" \
  -H 'content-type: application/json' -H "Authorization: Bearer $TOKEN" \
  -d '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"DocPipeline","arguments":{"repo":"demo-api","approved":"yes"},'"$meta"'}}' >/dev/null
ok_iserr=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1])).get("result",{}).get("isError"))' "$OK_JSON")
ok_haserr=$(python3 -c 'import json,sys;print("error" in json.load(open(sys.argv[1])))' "$OK_JSON")
log "controle (nome válido 'DocPipeline') -> tem error? $ok_haserr ; result.isError=$ok_iserr"

# ── Então o servidor responde com erro "ferramenta não encontrada" ──────────
verdict=$(python3 - "$BAD_JSON" "$bad_code" "$BADNAME" "$case_code" "$ok_haserr" "$ok_iserr" <<'PY'
import json, sys
path, http_code, badname, case_code, ok_haserr, ok_iserr = sys.argv[1:7]
d = json.load(open(path))
err = d.get("error")
code = err.get("code") if err else None
msg  = (err.get("message") if err else "") or ""

fails = []
if http_code != "200":                       fails.append(f"HTTP {http_code} != 200 (erro JSON-RPC vem no corpo)")
if "result" in d:                            fails.append("resposta trouxe result (não deveria)")
if err is None:                              fails.append("resposta sem objeto error")
if code != -32602:                           fails.append(f"error.code={code} != -32602")
low = msg.lower()
if not ("unknown tool" in low or "not found" in low or "não encontr" in low):
    fails.append(f"mensagem não indica tool inexistente: {msg!r}")
if badname not in msg:                        fails.append(f"mensagem não cita o nome chamado ({badname!r}): {msg!r}")
if case_code != "-32602":                     fails.append(f"controle caixa-errada: code={case_code} != -32602")
if ok_haserr != "False":                      fails.append("controle nome-válido veio com error")
if ok_iserr != "False":                       fails.append(f"controle nome-válido: isError={ok_iserr} != false")

print(f"SUMMARY http={http_code} error.code={code} error.message={msg!r}")
if fails:
    for f in fails: print("FAILCHECK " + f)
    print("VERDICT FAIL")
else:
    print("VERDICT PASS")
PY
)

echo "$verdict" | while IFS= read -r line; do
  case "$line" in
    SUMMARY\ *)   log "resposta: ${line#SUMMARY }" ;;
    FAILCHECK\ *) log "  ! ${line#FAILCHECK }" ;;
  esac
done

if echo "$verdict" | grep -q "VERDICT PASS"; then
  log "RESULTADO: FUNCIONOU — servidor rejeitou a tool inexistente com JSON-RPC -32602 \"unknown tool\""
  echo "PASS"; exit 0
else
  log "RESULTADO: NÃO FUNCIONOU — veja as verificações acima"
  echo "FAIL"; exit 1
fi
