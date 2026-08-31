#!/usr/bin/env bash
# CENARIO-006 — Isolamento de runs por principal (--principal-header)
#
# alice inicia uma run; bob nao a ve (run/list vazio) nem a acessa (run/status
# -> unknown runId); alice ve a propria. Subir com --principal-header sem
# --token falha.
#
# Requests em modo stateless (params._meta) + header X-Mhl-Principal.
# O script copia sample/cloud/mhl para esta pasta se necessário.

set -u

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MHL="$HERE/mhl"
[[ -x "$MHL" ]] || { cp "$HERE/../../mhl" "$MHL" 2>/dev/null && chmod +x "$MHL"; }
[[ -x "$MHL" ]] || { echo "FALHA: binário mhl não encontrado"; exit 1; }

WORKFLOWS="$HERE/../.."
ADDR="127.0.0.1:8724"
BASE="http://$ADDR"
TOKEN="cenario-006-$(date +%s)"
STATE="$(mktemp -d)"
PROTO="2026-07-28"
META='"_meta":{"io.modelcontextprotocol/protocolVersion":"'"$PROTO"'","io.modelcontextprotocol/clientCapabilities":{}}'
ALICE="alice@acme.com"
BOB="bob@acme.com"

L="$HERE/logs"; mkdir -p "$L"
SERVER_LOG="$L/mcp-server.log";     : > "$SERVER_LOG"
CLIENT_LOG="$L/client.log";         : > "$CLIENT_LOG"
NOTOKEN_LOG="$L/start-without-token.log"; : > "$NOTOKEN_LOG"

log() { echo "[$(date '+%H:%M:%S')] $*" | tee -a "$CLIENT_LOG"; }
save() { cp "$1" "$L/$2"; }

cleanup() {
  if [[ -n "${SERVER_PID:-}" ]] && kill -0 "$SERVER_PID" 2>/dev/null; then
    kill -TERM "$SERVER_PID" 2>/dev/null; wait "$SERVER_PID" 2>/dev/null
  fi
  rm -rf "$STATE"
}
trap cleanup EXIT

# mcp <principal> <json-body> <arquivo-saida>  -> imprime HTTP code
mcp() {
  curl -s -o "$3" -w '%{http_code}' -X POST "$BASE/mcp" \
    -H 'content-type: application/json' \
    -H "Authorization: Bearer $TOKEN" \
    -H "X-Mhl-Principal: $1" \
    -d "$2"
}

# ── Dado o servidor COM --principal-header e --token ───────────────────────
log "iniciando: serve mcp --http --principal-header X-Mhl-Principal --token <gerado>"
"$MHL" serve mcp --http --addr "$ADDR" --token "$TOKEN" \
  --principal-header X-Mhl-Principal --state-dir "$STATE" \
  "$WORKFLOWS" >>"$SERVER_LOG" 2>&1 &
SERVER_PID=$!

ready=""
for i in $(seq 1 50); do
  kill -0 "$SERVER_PID" 2>/dev/null || { log "FALHA: servidor terminou (veja $SERVER_LOG)"; break; }
  [[ "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/healthz")" == "200" ]] && { ready="yes"; break; }
  sleep 0.2
done
[[ "$ready" == "yes" ]] || { log "RESULTADO: NÃO FUNCIONOU — servidor não ficou pronto"; echo "FAIL"; exit 1; }

# ── alice inicia uma run ─────────────────────────────────────────────────
START='{"jsonrpc":"2.0","id":1,"method":"run/start","params":{"name":"DocPipeline","arguments":{"repo":"iso-demo","approved":"no"},'"$META"'}}'
sc=$(mcp "$ALICE" "$START" "$L/alice-run-start.json")
RID=$(python3 -c 'import json,sys;print(json.load(open(sys.argv[1])).get("result",{}).get("runId",""))' "$L/alice-run-start.json")
log "alice run/start -> HTTP $sc ; runId=${RID:-<vazio>}"
[[ -n "$RID" ]] || { log "FALHA: alice não recebeu runId"; python3 -m json.tool "$L/alice-run-start.json" | tee -a "$CLIENT_LOG"; echo "FAIL"; exit 1; }

sleep 2   # Draft(1s) -> Review fail() ; run fica resumable

# alice confere a propria run
mcp "$ALICE" '{"jsonrpc":"2.0","id":2,"method":"run/status","params":{"runId":"'"$RID"'",'"$META"'}}' "$L/alice-run-status.json" >/dev/null
log "alice run/status:"; python3 -m json.tool "$L/alice-run-status.json" | tee -a "$CLIENT_LOG"

# ── bob nao ve nada ─────────────────────────────────────────────────────
mcp "$BOB" '{"jsonrpc":"2.0","id":3,"method":"run/list","params":{'"$META"'}}' "$L/bob-run-list.json" >/dev/null
log "bob run/list:"; python3 -m json.tool "$L/bob-run-list.json" | tee -a "$CLIENT_LOG"

mcp "$BOB" '{"jsonrpc":"2.0","id":4,"method":"run/status","params":{"runId":"'"$RID"'",'"$META"'}}' "$L/bob-run-status.json" >/dev/null
log "bob run/status (runId de alice):"; python3 -m json.tool "$L/bob-run-status.json" | tee -a "$CLIENT_LOG"

# ── alice ve a propria em run/list ──────────────────────────────────────
mcp "$ALICE" '{"jsonrpc":"2.0","id":5,"method":"run/list","params":{'"$META"'}}' "$L/alice-run-list.json" >/dev/null
log "alice run/list:"; python3 -m json.tool "$L/alice-run-list.json" | tee -a "$CLIENT_LOG"

# ── controle: --principal-header SEM --token ────────────────────────────
log "controle: iniciar servidor com --principal-header e SEM --token (deve falhar)"
"$MHL" serve mcp --http --addr "127.0.0.1:8725" --principal-header X-Mhl-Principal \
  "$WORKFLOWS" >>"$NOTOKEN_LOG" 2>&1 &
np=$!; sleep 1
NOTOKEN_RUNNING="no"; kill -0 "$np" 2>/dev/null && { NOTOKEN_RUNNING="yes"; kill -TERM "$np" 2>/dev/null; wait "$np" 2>/dev/null; }
log "processo sem --token ainda vivo? $NOTOKEN_RUNNING (esperado: no)"
grep -i "principal-header needs --token\|needs --token" "$NOTOKEN_LOG" | tee -a "$CLIENT_LOG" || true

# ── verdite ────────────────────────────────────────────────────────────
verdict=$(python3 - "$L/bob-run-list.json" "$L/bob-run-status.json" "$L/alice-run-list.json" "$RID" <<'PY'
import json, sys
bl, bs, al, rid = sys.argv[1:5]
bob_list = json.load(open(bl)).get("result", {}).get("runs", None)
bob_stat = json.load(open(bs))
alice_list = json.load(open(al)).get("result", {}).get("runs", [])
alice_ids = [r.get("runId") for r in alice_list]
fails = []
if bob_list != []:
    fails.append(f"bob run/list nao vazio: {bob_list!r}")
be = bob_stat.get("error", {})
if be.get("code") != -32602:
    fails.append(f"bob run/status sem erro -32602: {bob_stat!r}")
if "unknown runid" not in (be.get("message","").lower()):
    fails.append(f"bob run/status mensagem inesperada: {be.get('message')!r}")
if rid not in alice_ids:
    fails.append(f"alice run/list nao contem o runId ({rid}): {alice_ids!r}")
for f in fails: print("FAILCHECK " + f)
print("VERDICT " + ("FAIL" if fails else "PASS"))
PY
)
echo "$verdict" | while IFS= read -r l; do case "$l" in FAILCHECK\ *) log "  ! ${l#FAILCHECK }";; esac; done

ok="yes"
echo "$verdict" | grep -q "VERDICT PASS" || ok="no"
[[ "$NOTOKEN_RUNNING" == "no" ]] || { ok="no"; log "  ! servidor subiu com --principal-header sem --token"; }
grep -qi "needs --token" "$NOTOKEN_LOG" || { ok="no"; log "  ! stderr não explica a exigência de --token"; }

if [[ "$ok" == "yes" ]]; then
  log "RESULTADO: FUNCIONOU — isolamento por principal ok; --principal-header exige --token"
  echo "PASS"; exit 0
else
  log "RESULTADO: NÃO FUNCIONOU — veja as verificações acima"
  echo "FAIL"; exit 1
fi
