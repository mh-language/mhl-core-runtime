#!/usr/bin/env bash
# CENARIO-015 — Validacao do inputSchema anunciado
#
# tools/call / run/start de DocPipeline sem "repo" (required) e com campo extra
# (additionalProperties:false). Registra se o servidor rejeita nos parametros
# ou se a run avanca mesmo assim (lacuna).
#
# Modo stateless (params._meta). Copia sample/cloud/mhl se preciso.

set -u

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MHL="$HERE/mhl"
[[ -x "$MHL" ]] || { cp "$HERE/../../mhl" "$MHL" 2>/dev/null && chmod +x "$MHL"; }
[[ -x "$MHL" ]] || { echo "FALHA: binário mhl não encontrado"; exit 1; }

WORKFLOWS="$HERE/../.."
ADDR="127.0.0.1:8736"
BASE="http://$ADDR"
TOKEN="cenario-015-$(date +%s)"
STATE="$(mktemp -d)"
META='"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}'

L="$HERE/logs"; mkdir -p "$L"
SL="$L/mcp-server.log"; : > "$SL"
CLIENT_LOG="$L/client.log"; : > "$CLIENT_LOG"
log() { echo "[$(date '+%H:%M:%S')] $*" | tee -a "$CLIENT_LOG"; }
cleanup() { [[ -n "${PID:-}" ]] && kill -0 "$PID" 2>/dev/null && { kill -TERM "$PID" 2>/dev/null; wait "$PID" 2>/dev/null; }; rm -rf "$STATE"; }
trap cleanup EXIT
jget() { python3 -c 'import json,sys
try: d=json.load(open(sys.argv[1]))
except Exception: print(""); sys.exit(0)
for p in sys.argv[2].split("."):
  d = d.get(p, {}) if isinstance(d, dict) else {}
print("" if d == {} else d)' "$1" "$2"; }

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

# ── inputSchema anunciado ──────────────────────────────────────────
post '{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{'"$META"'}}' "$L/tools-list.json" >/dev/null
log "inputSchema de DocPipeline:"
python3 -c 'import json,sys
for t in json.load(open(sys.argv[1]))["result"]["tools"]:
  if t["name"]=="DocPipeline": print(json.dumps(t["inputSchema"], indent=2))' "$L/tools-list.json" | tee -a "$CLIENT_LOG"

# ── caso 1: tools/call sem "repo" ─────────────────────────────────
C1=$(post '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"DocPipeline","arguments":{"approved":"yes"},'"$META"'}}' "$L/case1-tools-call-missing-repo.json")
log "caso 1 (tools/call sem repo) -> HTTP $C1"; python3 -m json.tool "$L/case1-tools-call-missing-repo.json" | tee -a "$CLIENT_LOG"
C1_ERR=$(jget "$L/case1-tools-call-missing-repo.json" error.code)
C1_ISERR=$(jget "$L/case1-tools-call-missing-repo.json" result.isError)
C1_TEXT=$(jget "$L/case1-tools-call-missing-repo.json" result.content)

# ── caso 2: tools/call com campo extra ───────────────────────────
C2=$(post '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"DocPipeline","arguments":{"repo":"x","approved":"yes","campoExtra":123},'"$META"'}}' "$L/case2-tools-call-extra-prop.json")
log "caso 2 (tools/call campo extra) -> HTTP $C2"; python3 -m json.tool "$L/case2-tools-call-extra-prop.json" | tee -a "$CLIENT_LOG"
C2_ERR=$(jget "$L/case2-tools-call-extra-prop.json" error.code)
C2_ISERR=$(jget "$L/case2-tools-call-extra-prop.json" result.isError)

# ── caso 3: run/start sem "repo" ─────────────────────────────────
C3=$(post '{"jsonrpc":"2.0","id":4,"method":"run/start","params":{"name":"DocPipeline","arguments":{"approved":"yes"},'"$META"'}}' "$L/case3-run-start-missing-repo.json")
log "caso 3 (run/start sem repo) -> HTTP $C3"; python3 -m json.tool "$L/case3-run-start-missing-repo.json" | tee -a "$CLIENT_LOG"
C3_ERR=$(jget "$L/case3-run-start-missing-repo.json" error.code)
RID3=$(jget "$L/case3-run-start-missing-repo.json" result.runId)
if [[ -n "$RID3" ]]; then
  sleep 3
  post '{"jsonrpc":"2.0","id":5,"method":"run/status","params":{"runId":"'"$RID3"'",'"$META"'}}' "$L/case3-run-status.json" >/dev/null
  log "caso 3 run/status:"; python3 -m json.tool "$L/case3-run-status.json" | tee -a "$CLIENT_LOG"
fi

# ── veredito ────────────────────────────────────────────────────
# Contrato pretendido: os 3 casos rejeitam NOS PARAMETROS (error.code=-32602),
# a run NAO chega a iniciar no caso 3.
ok="yes"
[[ "$C1_ERR" == "-32602" ]] || { ok="no"; log "  ! caso 1: sem erro -32602 de parâmetros (error.code=$C1_ERR isError=$C1_ISERR)"; }
[[ "$C2_ERR" == "-32602" ]] || { ok="no"; log "  ! caso 2: sem erro -32602 de parâmetros (error.code=$C2_ERR isError=$C2_ISERR)"; }
[[ "$C3_ERR" == "-32602" ]] || { ok="no"; log "  ! caso 3: sem erro -32602 de parâmetros (error.code=$C3_ERR runId=$RID3)"; }

if [[ "$ok" == "yes" ]]; then
  log "RESULTADO: FUNCIONOU — o servidor faz cumprir o inputSchema anunciado (rejeita nos parâmetros)"
  echo "PASS"; exit 0
else
  log "RESULTADO: NÃO FUNCIONOU — o inputSchema anunciado NÃO é imposto em tools/call / run/start (ver corpos acima)"
  log "  (comportamento atual esperado conforme observações do cenário — registrar como lacuna)"
  echo "FAIL"; exit 1
fi
