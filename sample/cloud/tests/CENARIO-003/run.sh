#!/usr/bin/env bash
# CENARIO-003 — Chamada de uma Tool usando o protocolo MCP stateless
#
# Dado que o servidor MCP está em execução
# E o cliente está autenticado e autorizado a usar a ferramenta
# Quando o cliente chama a ferramenta
# Então o servidor responde corretamente
#
# Stateless = sem `initialize`, sem header `Mcp-Session-Id`; cada request carrega
# params._meta (io.modelcontextprotocol/protocolVersion 2026-07-28 + clientCapabilities).
# Usa o binário `mhl` copiado para esta pasta.

set -u

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MHL="$HERE/mhl"
WORKFLOWS="$HERE/../.."          # sample/cloud (DocPipeline / SlowBuild)
ADDR="127.0.0.1:8714"
BASE="http://$ADDR"
TOKEN="cenario-003-$(date +%s)"
STATE="$(mktemp -d)"
PROTO="2026-07-28"

L="$HERE/logs"
SERVER_LOG="$L/mcp-server.log"
CLIENT_LOG="$L/client.log"
CALL_JSON="$L/tools-call-response.json"
CALL_HDRS="$L/tools-call-headers.txt"
NOAUTH_JSON="$L/tools-call-no-auth-response.json"
FAILGATE_JSON="$L/tools-call-review-gate-response.json"

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

# ── E o cliente está autenticado e autorizado: controle sem bearer -> 401 ───
noauth_code=$(curl -s -o "$NOAUTH_JSON" -w '%{http_code}' -X POST "$BASE/mcp" \
  -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"DocPipeline","arguments":{"repo":"x","approved":"yes"},"_meta":{"io.modelcontextprotocol/protocolVersion":"'"$PROTO"'","io.modelcontextprotocol/clientCapabilities":{}}}}')
log "controle (sem bearer) tools/call -> HTTP $noauth_code (esperado 401)"

# ── Quando o cliente chama a ferramenta (stateless, autenticado) ────────────
REPO="demo-api"
REQ='{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"DocPipeline","arguments":{"repo":"'"$REPO"'","approved":"yes"},"_meta":{"io.modelcontextprotocol/protocolVersion":"'"$PROTO"'","io.modelcontextprotocol/clientCapabilities":{}}}}'
log "request stateless (tools/call DocPipeline repo=$REPO approved=yes):"
echo "$REQ" | python3 -m json.tool | tee -a "$CLIENT_LOG"

call_code=$(curl -s -D "$CALL_HDRS" -o "$CALL_JSON" -w '%{http_code}' \
  -X POST "$BASE/mcp" \
  -H 'content-type: application/json' \
  -H "Authorization: Bearer $TOKEN" \
  -H "MCP-Protocol-Version: $PROTO" \
  -d "$REQ")
log "POST /mcp tools/call (stateless) -> HTTP $call_code"
SID_HDR=$(awk 'tolower($1)=="mcp-session-id:"{print $2}' "$CALL_HDRS" | tr -d '\r')
log "header Mcp-Session-Id na resposta: ${SID_HDR:-<ausente, como esperado no stateless>}"
log "corpo da resposta:"
python3 -m json.tool "$CALL_JSON" 2>/dev/null | tee -a "$CLIENT_LOG" || cat "$CALL_JSON" | tee -a "$CLIENT_LOG"
echo | tee -a "$CLIENT_LOG"

# ── controle: mesma tool com approved=no -> gate de revisão -> isError:true ──
curl -s -o "$FAILGATE_JSON" -X POST "$BASE/mcp" \
  -H 'content-type: application/json' -H "Authorization: Bearer $TOKEN" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"DocPipeline","arguments":{"repo":"'"$REPO"'","approved":"no"},"_meta":{"io.modelcontextprotocol/protocolVersion":"'"$PROTO"'","io.modelcontextprotocol/clientCapabilities":{}}}}' >/dev/null
gate_iserr=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1])).get("result",{}).get("isError"))' "$FAILGATE_JSON")
gate_txt=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1])).get("result",{}).get("content",[{}])[0].get("text",""))' "$FAILGATE_JSON")
log "controle (approved=no) -> isError=$gate_iserr texto=\"$gate_txt\""

# ── Então o servidor responde corretamente ─────────────────────────────────
# Python faz todas as asserções e imprime uma linha de resumo + PASS/FAIL.
verdict=$(python3 - "$CALL_JSON" "$REPO" "$call_code" "$SID_HDR" "$noauth_code" "$gate_iserr" <<'PY'
import json, sys
path, repo, call_code, sid_hdr, noauth_code, gate_iserr = sys.argv[1:7]
d = json.load(open(path))
res = d.get("result", {})
is_err   = res.get("isError")
rt       = res.get("resultType", "-")
si       = res.get("_meta", {}).get("io.modelcontextprotocol/serverInfo", {})
si       = f'{si.get("name","?")}/{si.get("version","?")}' if si else "-"
content  = res.get("content", [])
has_text = bool(content and content[0].get("type") == "text" and content[0].get("text"))
sc       = res.get("structuredContent", {}) or {}
pub, rev = sc.get("published", ""), sc.get("review", "")

fails = []
if call_code != "200":                                   fails.append(f"HTTP {call_code} != 200")
if "result" not in d:                                     fails.append("resposta sem result")
if is_err is not False:                                   fails.append(f"isError={is_err} != false")
if not has_text:                                          fails.append("sem bloco content[].text")
if pub != f"published docs for {repo} (reviewed)":        fails.append(f"published inesperado: {pub!r}")
if rev != "reviewed":                                     fails.append(f"review != reviewed: {rev!r}")
if rt != "complete":                                      fails.append(f"resultType={rt!r} != complete")
if not si.startswith("mhl/"):                             fails.append("_meta serverInfo ausente")
if sid_hdr:                                               fails.append(f"stateless emitiu Mcp-Session-Id: {sid_hdr}")
if noauth_code != "401":                                  fails.append(f"chamada sem bearer -> {noauth_code} != 401")
if gate_iserr != "True":                                  fails.append(f"approved=no -> isError={gate_iserr} != true")

print(f"SUMMARY isError={is_err} resultType={rt} serverInfo={si} content.text={'yes' if has_text else 'no'} published={pub!r} review={rev!r}")
if fails:
    for f in fails:
        print("FAILCHECK " + f)
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
  log "RESULTADO: FUNCIONOU — DocPipeline executada via stateless; published=\"published docs for ${REPO} (reviewed)\"; isError=false"
  echo "PASS"; exit 0
else
  log "RESULTADO: NÃO FUNCIONOU — veja as verificações acima"
  echo "FAIL"; exit 1
fi
