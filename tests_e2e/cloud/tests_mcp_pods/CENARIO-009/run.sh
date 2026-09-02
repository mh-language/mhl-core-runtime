#!/usr/bin/env bash
# CENARIO-009 — Drain gracioso no SIGTERM e checkpoint de shutdown
#
# --drain-timeout 20s: SIGTERM durante SlowBuild working ->
#   /readyz 503 ; /healthz 200 ; run/start -> -32000 draining ;
#   processo espera a run terminar antes de sair ; stderr linha "draining"
# restart no mesmo --state-dir -> run/status do runId como resumable
# --drain-timeout 0: SIGTERM cancela na hora (stderr cancelImmediately:true)
#
# Copia tests/cloud/mhl se preciso.

set -u

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MHL="$HERE/mhl"
[[ -x "$MHL" ]] || { cp "$HERE/../../mhl" "$MHL" 2>/dev/null && chmod +x "$MHL"; }
[[ -x "$MHL" ]] || { echo "FALHA: binário mhl não encontrado"; exit 1; }

WORKFLOWS="$HERE/../.."
ADDR="127.0.0.1:8728"
ADDR0="127.0.0.1:8729"
BASE="http://$ADDR"
TOKEN="cenario-009-$(date +%s)"
STATE="$(mktemp -d)"; STATE0="$(mktemp -d)"

L="$HERE/logs"; mkdir -p "$L"
SL="$L/mcp-server.log";        : > "$SL"
SL2="$L/mcp-server-restart.log"; : > "$SL2"
SL0="$L/mcp-server-drain0.log";  : > "$SL0"
CLIENT_LOG="$L/client.log";      : > "$CLIENT_LOG"

log() { echo "[$(date '+%H:%M:%S')] $*" | tee -a "$CLIENT_LOG"; }
cleanup() {
  for p in "${PID:-}" "${PID2:-}" "${PID0:-}"; do
    [[ -n "$p" ]] && kill -0 "$p" 2>/dev/null && { kill -KILL "$p" 2>/dev/null; wait "$p" 2>/dev/null; }
  done
  rm -rf "$STATE" "$STATE0"
}
trap cleanup EXIT

wait_ready() { # <pid> <base>
  local p="$1" b="$2"
  for i in $(seq 1 50); do
    kill -0 "$p" 2>/dev/null || return 1
    [[ "$(curl -s -o /dev/null -w '%{http_code}' "$b/healthz")" == "200" ]] && return 0
    sleep 0.2
  done
  return 1
}
rpc() { curl -s -o "$3" -w '%{http_code}' -X POST "$1/mcp" -H 'content-type: application/json' \
  -H "Authorization: Bearer $TOKEN" -H "Mcp-Session-Id: ${SID:-}" -d "$2"; }
jget() { python3 -c 'import json,sys;d=json.load(open(sys.argv[1]))
for p in sys.argv[2].split("."):
  d = d.get(p, {}) if isinstance(d, dict) else {}
print("" if d == {} else d)' "$1" "$2"; }

########################################################################
# Parte 1: --drain-timeout 20s
########################################################################
log "P1: iniciando servidor --drain-timeout 20s em $ADDR"
"$MHL" serve mcp --http --addr "$ADDR" --token "$TOKEN" \
  --drain-timeout 20s --state-dir "$STATE" \
  "$WORKFLOWS" >>"$SL" 2>&1 &
PID=$!
wait_ready "$PID" "$BASE" || { log "FALHA: servidor não ficou pronto"; echo "FAIL"; exit 1; }

curl -s -D "$L/init-headers.txt" -o /dev/null -X POST "$BASE/mcp" \
  -H 'content-type: application/json' -H "Authorization: Bearer $TOKEN" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{}}}'
SID=$(awk 'tolower($1)=="mcp-session-id:"{print $2}' "$L/init-headers.txt" | tr -d '\r')

rpc "$BASE" '{"jsonrpc":"2.0","id":2,"method":"run/start","params":{"name":"SlowBuild","arguments":{"target":"web"}}}' "$L/run-start.json" >/dev/null
RID=$(jget "$L/run-start.json" result.runId)
log "run/start SlowBuild -> runId=$RID state=$(jget "$L/run-start.json" result.state)"
[[ -n "$RID" ]] || { log "FALHA: sem runId"; cat "$L/run-start.json" | tee -a "$CLIENT_LOG"; echo "FAIL"; exit 1; }

sleep 1.5
log "enviando SIGTERM ao PID $PID"
T0=$(date +%s)
kill -TERM "$PID"

# janela de dreno — sondar imediatamente
sleep 0.3
RZ=$(curl -s -o "$L/readyz-draining.txt" -w '%{http_code}' "$BASE/readyz")
HZ=$(curl -s -o "$L/healthz-draining.txt" -w '%{http_code}' "$BASE/healthz")
DR_CODE=$(rpc "$BASE" '{"jsonrpc":"2.0","id":3,"method":"run/start","params":{"name":"SlowBuild","arguments":{"target":"rejeitada"}}}' "$L/run-start-during-drain.json")
DR_MSG=$(jget "$L/run-start-during-drain.json" error.message)
DR_CODEJ=$(jget "$L/run-start-during-drain.json" error.code)
log "durante o dreno: /readyz=$RZ /healthz=$HZ ; run/start HTTP=$DR_CODE code=$DR_CODEJ msg=\"$DR_MSG\""

wait "$PID"; PID_RC=$?
T1=$(date +%s); ELAPSED=$((T1 - T0))
PID=""
log "processo encerrou ${ELAPSED}s após o SIGTERM (rc=$PID_RC)"
grep '"msg":"draining"' "$SL" | tee -a "$CLIENT_LOG" || true

########################################################################
# Parte 2: restart no mesmo --state-dir -> run/status do runId
########################################################################
log "P2: reiniciando servidor no mesmo --state-dir"
"$MHL" serve mcp --http --addr "$ADDR" --token "$TOKEN" --state-dir "$STATE" \
  "$WORKFLOWS" >>"$SL2" 2>&1 &
PID2=$!
wait_ready "$PID2" "$BASE" || { log "FALHA: restart não ficou pronto"; echo "FAIL"; exit 1; }
curl -s -D "$L/init2-headers.txt" -o /dev/null -X POST "$BASE/mcp" \
  -H 'content-type: application/json' -H "Authorization: Bearer $TOKEN" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{}}}'
SID=$(awk 'tolower($1)=="mcp-session-id:"{print $2}' "$L/init2-headers.txt" | tr -d '\r')
rpc "$BASE" '{"jsonrpc":"2.0","id":4,"method":"run/status","params":{"runId":"'"$RID"'"}}' "$L/run-status-after-restart.json" >/dev/null
log "run/status pós-restart:"; python3 -m json.tool "$L/run-status-after-restart.json" | tee -a "$CLIENT_LOG"
RS_STATE=$(jget "$L/run-status-after-restart.json" result.state)
RS_RESUMABLE=$(jget "$L/run-status-after-restart.json" result.resumable)
kill -TERM "$PID2" 2>/dev/null; wait "$PID2" 2>/dev/null; PID2=""

########################################################################
# Parte 3: --drain-timeout 0 -> cancelamento imediato
########################################################################
log "P3: servidor --drain-timeout 0 em $ADDR0"
"$MHL" serve mcp --http --addr "$ADDR0" --token "$TOKEN" \
  --drain-timeout 0 --state-dir "$STATE0" \
  "$WORKFLOWS" >>"$SL0" 2>&1 &
PID0=$!
wait_ready "$PID0" "http://$ADDR0" || { log "FALHA: servidor P3 não ficou pronto"; echo "FAIL"; exit 1; }
curl -s -D "$L/init0-headers.txt" -o /dev/null -X POST "http://$ADDR0/mcp" \
  -H 'content-type: application/json' -H "Authorization: Bearer $TOKEN" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{}}}'
SID0=$(awk 'tolower($1)=="mcp-session-id:"{print $2}' "$L/init0-headers.txt" | tr -d '\r')
curl -s -o "$L/p3-run-start.json" -X POST "http://$ADDR0/mcp" -H 'content-type: application/json' \
  -H "Authorization: Bearer $TOKEN" -H "Mcp-Session-Id: $SID0" \
  -d '{"jsonrpc":"2.0","id":2,"method":"run/start","params":{"name":"SlowBuild","arguments":{"target":"drain0"}}}' >/dev/null
sleep 1
D0=$(date +%s); kill -TERM "$PID0"; wait "$PID0" 2>/dev/null; D1=$(date +%s); PID0=""
D0_ELAPSED=$((D1 - D0))
log "P3: processo encerrou ${D0_ELAPSED}s após o SIGTERM"
grep '"cancelImmediately":true' "$SL0" | tee -a "$CLIENT_LOG" || true

########################################################################
# verdite
########################################################################
ok="yes"
[[ "$RZ" == "503" ]]                    || { ok="no"; log "  ! /readyz durante dreno != 503 ($RZ)"; }
[[ "$HZ" == "200" ]]                    || { ok="no"; log "  ! /healthz durante dreno != 200 ($HZ)"; }
[[ "$DR_CODE" == "503" ]]              || { ok="no"; log "  ! run/start durante dreno HTTP != 503 ($DR_CODE)"; }
[[ "$DR_CODEJ" == "-32000" ]]         || { ok="no"; log "  ! run/start durante dreno code != -32000 ($DR_CODEJ)"; }
echo "$DR_MSG" | grep -qi "draining"   || { ok="no"; log "  ! msg de dreno não menciona 'draining'"; }
[[ "$ELAPSED" -ge 3 ]]                 || { ok="no"; log "  ! processo saiu em ${ELAPSED}s (< 3s): não esperou a run"; }
grep -q '"msg":"draining"' "$SL"        || { ok="no"; log "  ! stderr sem linha JSON 'draining'"; }
grep -q '"timeout":"20s"' "$SL"         || { ok="no"; log "  ! linha 'draining' sem timeout 20s"; }
[[ "$RS_STATE" == "failed" || "$RS_STATE" == "canceled" ]] || { ok="no"; log "  ! run/status pós-restart state=$RS_STATE"; }
[[ "$RS_RESUMABLE" == "True" || "$RS_RESUMABLE" == "true" ]] || { ok="no"; log "  ! run/status pós-restart sem resumable:true"; }
grep -q '"cancelImmediately":true' "$SL0" || { ok="no"; log "  ! P3 stderr sem cancelImmediately:true"; }
[[ "$D0_ELAPSED" -le 8 ]]              || { ok="no"; log "  ! P3 processo demorou ${D0_ELAPSED}s (> 8s) com drain-timeout 0"; }

if [[ "$ok" == "yes" ]]; then
  log "RESULTADO: FUNCIONOU — dreno gracioso, /readyz 503, run/start bloqueado, checkpoint de shutdown, drain-timeout 0 imediato"
  echo "PASS"; exit 0
else
  log "RESULTADO: NÃO FUNCIONOU — veja as verificações acima"
  echo "FAIL"; exit 1
fi
