#!/usr/bin/env bash
# CENARIO-016 — run/cancel aborta uma chamada em andamento
#
# run/start SlowBuild -> ~1s (dentro do sleep 3 do Compile) -> run/cancel
# -> state canceled imediato ; a run nunca entra em Package
# (nem run/logs nem o stderr mostram "step: Package" nos segundos seguintes).
#
# Modo handshake. Copia sample/cloud/mhl se preciso.

set -u

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MHL="$HERE/mhl"
[[ -x "$MHL" ]] || { cp "$HERE/../../mhl" "$MHL" 2>/dev/null && chmod +x "$MHL"; }
[[ -x "$MHL" ]] || { echo "FALHA: binário mhl não encontrado"; exit 1; }

WORKFLOWS="$HERE/../.."
ADDR="127.0.0.1:8737"
BASE="http://$ADDR"
TOKEN="cenario-016-$(date +%s)"
STATE="$(mktemp -d)"

L="$HERE/logs"; mkdir -p "$L"
SL="$L/mcp-server.log"; : > "$SL"
CLIENT_LOG="$L/client.log"; : > "$CLIENT_LOG"
log() { echo "[$(date '+%H:%M:%S')] $*" | tee -a "$CLIENT_LOG"; }
cleanup() { [[ -n "${PID:-}" ]] && kill -0 "$PID" 2>/dev/null && { kill -TERM "$PID" 2>/dev/null; wait "$PID" 2>/dev/null; }; rm -rf "$STATE"; }
trap cleanup EXIT

rpc() { curl -s -o "$2" -X POST "$BASE/mcp" -H 'content-type: application/json' \
  -H "Authorization: Bearer $TOKEN" -H "Mcp-Session-Id: $SID" -d "$1"; }
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

curl -s -D "$L/init-headers.txt" -o /dev/null -X POST "$BASE/mcp" \
  -H 'content-type: application/json' -H "Authorization: Bearer $TOKEN" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{}}}'
SID=$(awk 'tolower($1)=="mcp-session-id:"{print $2}' "$L/init-headers.txt" | tr -d '\r')

rpc '{"jsonrpc":"2.0","id":2,"method":"run/start","params":{"name":"SlowBuild","arguments":{"target":"cancel-inflight"}}}' "$L/run-start.json"
RID=$(jget "$L/run-start.json" result.runId)
T_START=$(date +%s.%N)
log "run/start SlowBuild -> runId=$RID state=$(jget "$L/run-start.json" result.state)"
[[ -n "$RID" ]] || { cat "$L/run-start.json" | tee -a "$CLIENT_LOG"; echo "FAIL"; exit 1; }

sleep 1   # dentro do sleep 3 do Compile

log "run/cancel..."
rpc '{"jsonrpc":"2.0","id":3,"method":"run/cancel","params":{"runId":"'"$RID"'"}}' "$L/run-cancel.json"
T_CANCEL=$(date +%s.%N)
CANCEL_STATE=$(jget "$L/run-cancel.json" result.state)
log "run/cancel -> state=$CANCEL_STATE (t+$(awk -v a=$T_START -v b=$T_CANCEL 'BEGIN{printf "%.2f", b-a}')s do start)"

rpc '{"jsonrpc":"2.0","id":4,"method":"run/status","params":{"runId":"'"$RID"'"}}' "$L/run-status-immediate.json"
IMM_STATE=$(jget "$L/run-status-immediate.json" result.state)
IMM_REACHED=$(jget "$L/run-status-immediate.json" result.reached)
log "run/status imediato -> state=$IMM_STATE reached=$IMM_REACHED"

# observar 4s: a run NAO deve avancar para Package
log "observando 4s se a run avança para Package..."
sleep 4
rpc '{"jsonrpc":"2.0","id":5,"method":"run/logs","params":{"runId":"'"$RID"'"}}' "$L/run-logs.json"
rpc '{"jsonrpc":"2.0","id":6,"method":"run/status","params":{"runId":"'"$RID"'"}}' "$L/run-status-after.json"
LOGS_TEXT=$(jget "$L/run-logs.json" result.text)
AFTER_STATE=$(jget "$L/run-status-after.json" result.state)
AFTER_REACHED=$(jget "$L/run-status-after.json" result.reached)
log "run/logs (t+~5s):"; python3 -m json.tool "$L/run-logs.json" | tee -a "$CLIENT_LOG"
log "run/status (t+~5s) -> state=$AFTER_STATE reached=$AFTER_REACHED"
log "----- stderr do servidor -----"; grep -E 'step: |"msg":"run ' "$SL" | tee -a "$CLIENT_LOG" || true

# ── verdite ────────────────────────────────────────────────────
ok="yes"
[[ "$CANCEL_STATE" == "canceled" ]]              || { ok="no"; log "  ! run/cancel não retornou state=canceled ($CANCEL_STATE)"; }
[[ "$IMM_STATE" == "canceled" ]]                 || { ok="no"; log "  ! run/status imediato != canceled ($IMM_STATE)"; }
echo "$LOGS_TEXT" | grep -q "step: Compile"      || { ok="no"; log "  ! run/logs sem 'step: Compile' (a run nem começou?)"; }
echo "$LOGS_TEXT" | grep -q "step: Package"      && { ok="no"; log "  ! run/logs MOSTRA 'step: Package' — o cancel não abortou o Compile"; }
grep -q "step: Compile" "$SL"                    || { ok="no"; log "  ! stderr sem 'step: Compile'"; }
grep -q "step: Package" "$SL"                    && { ok="no"; log "  ! stderr MOSTRA 'step: Package' — a run seguiu após o cancel"; }
echo "$AFTER_REACHED" | grep -q "Package"        && { ok="no"; log "  ! reached contém Package após o cancel"; }

if [[ "$ok" == "yes" ]]; then
  log "RESULTADO: FUNCIONOU — run/cancel abortou o subprocesso do Compile; a run não avançou para Package"
  echo "PASS"; exit 0
else
  log "RESULTADO: NÃO FUNCIONOU — veja as verificações acima"
  echo "FAIL"; exit 1
fi
