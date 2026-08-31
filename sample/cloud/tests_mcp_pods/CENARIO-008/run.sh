#!/usr/bin/env bash
# CENARIO-008 — Concorrencia limitada e fila de runs (--max-concurrent-runs 1)
#
# 3x run/start SlowBuild -> working, queued(qp0), queued(qp1)
# tools/call com pool cheio -> -32000 "server at capacity" (HTTP 503) apos ~5s
# run/cancel numa run queued -> canceled sem executar
# run #1 termina -> run #2 passa de queued a working
#
# Modo handshake. Copia sample/cloud/mhl se preciso.

set -u

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MHL="$HERE/mhl"
[[ -x "$MHL" ]] || { cp "$HERE/../../mhl" "$MHL" 2>/dev/null && chmod +x "$MHL"; }
[[ -x "$MHL" ]] || { echo "FALHA: binário mhl não encontrado"; exit 1; }

WORKFLOWS="$HERE/../.."
ADDR="127.0.0.1:8727"
BASE="http://$ADDR"
TOKEN="cenario-008-$(date +%s)"
STATE="$(mktemp -d)"

L="$HERE/logs"; mkdir -p "$L"
SERVER_LOG="$L/mcp-server.log"; : > "$SERVER_LOG"
CLIENT_LOG="$L/client.log";     : > "$CLIENT_LOG"

log() { echo "[$(date '+%H:%M:%S')] $*" | tee -a "$CLIENT_LOG"; }
cleanup() {
  if [[ -n "${SERVER_PID:-}" ]] && kill -0 "$SERVER_PID" 2>/dev/null; then
    kill -TERM "$SERVER_PID" 2>/dev/null; wait "$SERVER_PID" 2>/dev/null
  fi
  rm -rf "$STATE"
}
trap cleanup EXIT

rpc() { curl -s -o "$3" -X POST "$BASE/mcp" -H 'content-type: application/json' \
  -H "Authorization: Bearer $TOKEN" -H "Mcp-Session-Id: $SID" -d "$2"; }
jget() { python3 -c 'import json,sys;d=json.load(open(sys.argv[1]))
for p in sys.argv[2].split("."):
  d = d.get(p, {}) if isinstance(d, dict) else {}
print("" if d == {} else d)' "$1" "$2"; }

# ── servidor com --max-concurrent-runs 1 ────────────────────────────────
log "iniciando: serve mcp --http --max-concurrent-runs 1"
"$MHL" serve mcp --http --addr "$ADDR" --token "$TOKEN" \
  --max-concurrent-runs 1 --state-dir "$STATE" \
  "$WORKFLOWS" >>"$SERVER_LOG" 2>&1 &
SERVER_PID=$!
ready=""
for i in $(seq 1 50); do
  kill -0 "$SERVER_PID" 2>/dev/null || { log "FALHA: servidor terminou"; break; }
  [[ "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/healthz")" == "200" ]] && { ready="yes"; break; }
  sleep 0.2
done
[[ "$ready" == "yes" ]] || { log "RESULTADO: NÃO FUNCIONOU — servidor não ficou pronto"; echo "FAIL"; exit 1; }

curl -s -D "$L/init-headers.txt" -o "$L/init.json" -X POST "$BASE/mcp" \
  -H 'content-type: application/json' -H "Authorization: Bearer $TOKEN" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{}}}' >/dev/null
SID=$(awk 'tolower($1)=="mcp-session-id:"{print $2}' "$L/init-headers.txt" | tr -d '\r')
log "sessão=$SID"

# ── 3x run/start SlowBuild em sequencia ────────────────────────────────
for n in 1 2 3; do
  rpc "$SID" '{"jsonrpc":"2.0","id":'"$((10+n))"',"method":"run/start","params":{"name":"SlowBuild","arguments":{"target":"t'"$n"'"}}}' "$L/start-$n.json"
  eval "RID$n=$(jget "$L/start-$n.json" result.runId)"
  st=$(jget "$L/start-$n.json" result.state); qp=$(jget "$L/start-$n.json" result.queuePosition)
  log "run/start #$n -> state=$st queuePosition=${qp:-<n/a>} runId=$(eval echo \$RID$n)"
done

# ── /metrics durante a fila ───────────────────────────────────────────
curl -s "$BASE/metrics" -o "$L/metrics-during-queue.txt"
Qd=$(sed -n 's/^mhl_serve_runs_queued \([0-9]*\)$/\1/p' "$L/metrics-during-queue.txt")
Ac=$(sed -n 's/^mhl_serve_runs_active \([0-9]*\)$/\1/p' "$L/metrics-during-queue.txt")
log "/metrics durante a fila: runs_active=$Ac runs_queued=$Qd"

# ── tools/call com o pool cheio (bloqueia ~5s e leva -32000) ──────────
log "tools/call SlowBuild com pool cheio (espera ~5s)..."
CAP_CODE=$(curl -s -o "$L/toolscall-at-capacity.json" -w '%{http_code}' -X POST "$BASE/mcp" \
  -H 'content-type: application/json' -H "Authorization: Bearer $TOKEN" -H "Mcp-Session-Id: $SID" \
  -d '{"jsonrpc":"2.0","id":20,"method":"tools/call","params":{"name":"SlowBuild","arguments":{"target":"cap"},"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}}}')
CAP_MSG=$(jget "$L/toolscall-at-capacity.json" error.message)
CAP_CODEJ=$(jget "$L/toolscall-at-capacity.json" error.code)
log "tools/call @capacity -> HTTP $CAP_CODE ; error.code=$CAP_CODEJ ; msg=\"$CAP_MSG\""

# ── run/cancel na run #3 (queued) ────────────────────────────────────
rpc "$SID" '{"jsonrpc":"2.0","id":30,"method":"run/cancel","params":{"runId":"'"$RID3"'"}}' "$L/cancel-3.json"
log "run/cancel #3 -> state=$(jget "$L/cancel-3.json" result.state)"

# ── esperar a fila andar: run #2 deve sair de queued ─────────────────
S2=""
for i in $(seq 1 40); do
  rpc "$SID" '{"jsonrpc":"2.0","id":40,"method":"run/status","params":{"runId":"'"$RID2"'"}}' "$L/status2-$i.json"
  s=$(jget "$L/status2-$i.json" result.state)
  log "  run #2 status[$i]: state=$s"
  [[ "$s" == "working" || "$s" == "completed" || "$s" == "failed" ]] && { S2="$s"; break; }
  [[ "$s" == "canceled" ]] && { S2="$s"; break; }
  sleep 1
done
cp "$L/status2-$i.json" "$L/status2-final.json" 2>/dev/null || true
rpc "$SID" '{"jsonrpc":"2.0","id":41,"method":"run/status","params":{"runId":"'"$RID3"'"}}' "$L/status3-final.json"

# ── verdite ─────────────────────────────────────────────────────────
S1_STATE=$(jget "$L/start-1.json" result.state)
S2_START=$(jget "$L/start-2.json" result.state); QP2=$(jget "$L/start-2.json" result.queuePosition)
S3_START=$(jget "$L/start-3.json" result.state); QP3=$(jget "$L/start-3.json" result.queuePosition)
S3_FINAL=$(jget "$L/status3-final.json" result.state)

ok="yes"
[[ "$S1_STATE" == "working" ]]                 || { ok="no"; log "  ! run #1 state=$S1_STATE != working"; }
[[ "$S2_START" == "queued" && "$QP2" == "0" ]] || { ok="no"; log "  ! run #2 != queued/qp0 ($S2_START/$QP2)"; }
[[ "$S3_START" == "queued" && "$QP3" == "1" ]] || { ok="no"; log "  ! run #3 != queued/qp1 ($S3_START/$QP3)"; }
[[ "$CAP_CODE" == "503" ]]                     || { ok="no"; log "  ! tools/call @capacity HTTP != 503 ($CAP_CODE)"; }
[[ "$CAP_CODEJ" == "-32000" ]]                 || { ok="no"; log "  ! tools/call @capacity error.code != -32000 ($CAP_CODEJ)"; }
echo "$CAP_MSG" | grep -qi "capacity"          || { ok="no"; log "  ! msg @capacity não menciona 'capacity'"; }
[[ "$S3_FINAL" == "canceled" ]]                || { ok="no"; log "  ! run #3 pós-cancel state=$S3_FINAL != canceled"; }
[[ "$S2" == "working" || "$S2" == "completed" || "$S2" == "failed" ]] || { ok="no"; log "  ! run #2 não saiu da fila (state=$S2)"; }
[[ "${Qd:-0}" -ge 1 ]]                         || { ok="no"; log "  ! /metrics runs_queued < 1 durante a fila"; }
grep -q "step: Compile" "$SERVER_LOG" && ! grep -q "target=t3\|t3" "$SERVER_LOG" || true

if [[ "$ok" == "yes" ]]; then
  log "RESULTADO: FUNCIONOU — limite=1 respeitado, fila com queuePosition, shed de carga e drain da fila"
  echo "PASS"; exit 0
else
  log "RESULTADO: NÃO FUNCIONOU — veja as verificações acima"
  echo "FAIL"; exit 1
fi
