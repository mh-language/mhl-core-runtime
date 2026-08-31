#!/usr/bin/env bash
# CENARIO-013 — run/logs e eventos de ciclo de vida estruturados
#
# run DocPipeline ate completed -> run/logs {runId} tem step: Draft/Review/Publish
# -> run/logs {runId, since:nextSince} vazio e nextSince estavel
# -> run/logs por outra sessao -> unknown runId
# -> stderr do servidor tem linhas JSON "run started"/"run completed" com runId+owner
#
# Modo handshake. Copia sample/cloud/mhl se preciso.

set -u

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MHL="$HERE/mhl"
[[ -x "$MHL" ]] || { cp "$HERE/../../mhl" "$MHL" 2>/dev/null && chmod +x "$MHL"; }
[[ -x "$MHL" ]] || { echo "FALHA: binário mhl não encontrado"; exit 1; }

WORKFLOWS="$HERE/../.."
ADDR="127.0.0.1:8734"
BASE="http://$ADDR"
TOKEN="cenario-013-$(date +%s)"
STATE="$(mktemp -d)"

L="$HERE/logs"; mkdir -p "$L"
SL="$L/mcp-server.log"; : > "$SL"
CLIENT_LOG="$L/client.log"; : > "$CLIENT_LOG"
log() { echo "[$(date '+%H:%M:%S')] $*" | tee -a "$CLIENT_LOG"; }
cleanup() { [[ -n "${PID:-}" ]] && kill -0 "$PID" 2>/dev/null && { kill -TERM "$PID" 2>/dev/null; wait "$PID" 2>/dev/null; }; rm -rf "$STATE"; }
trap cleanup EXIT

new_session() {
  curl -s -D "$L/init-$1-headers.txt" -o /dev/null -X POST "$BASE/mcp" \
    -H 'content-type: application/json' -H "Authorization: Bearer $TOKEN" \
    -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{}}}'
  awk 'tolower($1)=="mcp-session-id:"{print $2}' "$L/init-$1-headers.txt" | tr -d '\r'
}
rpc() { curl -s -o "$3" -X POST "$BASE/mcp" -H 'content-type: application/json' \
  -H "Authorization: Bearer $TOKEN" -H "Mcp-Session-Id: $1" -d "$2"; }
jget() { python3 -c 'import json,sys;d=json.load(open(sys.argv[1]))
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

SID_A=$(new_session A); SID_B=$(new_session B)
log "sessão A=$SID_A  B=$SID_B"

rpc "$SID_A" '{"jsonrpc":"2.0","id":2,"method":"run/start","params":{"name":"DocPipeline","arguments":{"repo":"logs-demo","approved":"yes"}}}' "$L/run-start.json"
RID=$(jget "$L/run-start.json" result.runId)
log "run/start -> runId=$RID"
[[ -n "$RID" ]] || { cat "$L/run-start.json" | tee -a "$CLIENT_LOG"; echo "FAIL"; exit 1; }

for i in $(seq 1 30); do
  rpc "$SID_A" '{"jsonrpc":"2.0","id":3,"method":"run/status","params":{"runId":"'"$RID"'"}}' "$L/run-status.json"
  s=$(jget "$L/run-status.json" result.state); log "  state=$s"
  [[ "$s" == "completed" || "$s" == "failed" ]] && break
  sleep 1
done

# ── run/logs primeira leitura ─────────────────────────────────────
rpc "$SID_A" '{"jsonrpc":"2.0","id":4,"method":"run/logs","params":{"runId":"'"$RID"'"}}' "$L/run-logs-1.json"
log "run/logs #1:"; python3 -m json.tool "$L/run-logs-1.json" | tee -a "$CLIENT_LOG"
TEXT1=$(jget "$L/run-logs-1.json" result.text)
NEXT1=$(jget "$L/run-logs-1.json" result.nextSince)

# ── run/logs releitura a partir de nextSince ─────────────────────
rpc "$SID_A" '{"jsonrpc":"2.0","id":5,"method":"run/logs","params":{"runId":"'"$RID"'","since":'"${NEXT1:-0}"'}}' "$L/run-logs-2.json"
log "run/logs #2 (since=$NEXT1):"; python3 -m json.tool "$L/run-logs-2.json" | tee -a "$CLIENT_LOG"
TEXT2=$(jget "$L/run-logs-2.json" result.text)
NEXT2=$(jget "$L/run-logs-2.json" result.nextSince)
ERR2=$(jget "$L/run-logs-2.json" error.code)

# ── run/logs por outra sessao ───────────────────────────────────
rpc "$SID_B" '{"jsonrpc":"2.0","id":6,"method":"run/logs","params":{"runId":"'"$RID"'"}}' "$L/run-logs-nonowner.json"
log "run/logs (não-dono):"; cat "$L/run-logs-nonowner.json" | tee -a "$CLIENT_LOG"
NONOWNER_ERR=$(jget "$L/run-logs-nonowner.json" error.code)

# ── linhas de ciclo de vida no stderr ──────────────────────────
grep -E '"msg":"run (started|completed|queued)"' "$SL" > "$L/lifecycle-events.jsonl" || true
log "eventos de ciclo de vida capturados:"; cat "$L/lifecycle-events.jsonl" | tee -a "$CLIENT_LOG"

# ── verdite ───────────────────────────────────────────────────
isint() { [[ "$1" =~ ^[0-9]+$ ]]; }
ok="yes"
echo "$TEXT1" | grep -q "step: Draft"   || { ok="no"; log "  ! run/logs #1 sem 'step: Draft'"; }
echo "$TEXT1" | grep -q "step: Review"  || { ok="no"; log "  ! run/logs #1 sem 'step: Review'"; }
echo "$TEXT1" | grep -q "step: Publish" || { ok="no"; log "  ! run/logs #1 sem 'step: Publish'"; }
isint "$NEXT1" && [[ "${NEXT1:-0}" -gt 0 ]] || { ok="no"; log "  ! nextSince #1 inválido: $NEXT1"; }
[[ -z "$TEXT2" ]]                       || { ok="no"; log "  ! run/logs #2 deveria vir vazio: '$TEXT2'"; }
[[ -z "$ERR2" ]]                        || { ok="no"; log "  ! run/logs #2 retornou erro $ERR2"; }
isint "$NEXT2" && [[ "$NEXT2" == "$NEXT1" ]] || { ok="no"; log "  ! nextSince #2 ($NEXT2) != #1 ($NEXT1)"; }
[[ "$NONOWNER_ERR" == "-32602" ]]      || { ok="no"; log "  ! run/logs não-dono não deu -32602 ($NONOWNER_ERR)"; }
grep -q '"msg":"run started"'  "$SL"   || { ok="no"; log "  ! stderr sem 'run started'"; }
grep -q '"msg":"run completed"' "$SL"  || { ok="no"; log "  ! stderr sem 'run completed'"; }
grep -E '"msg":"run started"' "$SL" | grep -q '"runId"' || { ok="no"; log "  ! 'run started' sem runId"; }
grep -E '"msg":"run started"' "$SL" | grep -q '"owner"' || { ok="no"; log "  ! 'run started' sem owner"; }

if [[ "$ok" == "yes" ]]; then
  log "RESULTADO: FUNCIONOU — run/logs cursored e owner-scoped; eventos de ciclo de vida em JSON"
  echo "PASS"; exit 0
else
  log "RESULTADO: NÃO FUNCIONOU — veja as verificações acima"
  echo "FAIL"; exit 1
fi
