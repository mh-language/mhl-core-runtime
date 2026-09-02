#!/usr/bin/env bash
# CENARIO-012 — Metricas Prometheus em /metrics
#
# gera: 1 run completed, 1 run canceled, 1 tools/call ok, 1 tools/call erro
# depois GET /metrics (sem bearer) e confere as series.
#
# Modo handshake. Copia tests/cloud/mhl se preciso.

set -u

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MHL="$HERE/mhl"
[[ -x "$MHL" ]] || { cp "$HERE/../../mhl" "$MHL" 2>/dev/null && chmod +x "$MHL"; }
[[ -x "$MHL" ]] || { echo "FALHA: binário mhl não encontrado"; exit 1; }

WORKFLOWS="$HERE/../.."
ADDR="127.0.0.1:8733"
BASE="http://$ADDR"
TOKEN="cenario-012-$(date +%s)"
STATE="$(mktemp -d)"
META='"_meta":{"io.modelcontextprotocol/protocolVersion":"2026-07-28","io.modelcontextprotocol/clientCapabilities":{}}'

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
metric() { sed -n "s|^$1 \\([0-9.][0-9.]*\\)\$|\\1|p" "$2" | head -1; }

log "iniciando servidor em $ADDR"
"$MHL" serve mcp --http --addr "$ADDR" --token "$TOKEN" --state-dir "$STATE" \
  "$WORKFLOWS" >>"$SL" 2>&1 &
PID=$!
for i in $(seq 1 50); do
  kill -0 "$PID" 2>/dev/null || { log "FALHA: servidor terminou"; echo "FAIL"; exit 1; }
  [[ "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/healthz")" == "200" ]] && break
  sleep 0.2
done

curl -s "$BASE/metrics" -o "$L/metrics-before.txt"
log "/metrics antes da atividade salvo em metrics-before.txt"

curl -s -D "$L/init-headers.txt" -o /dev/null -X POST "$BASE/mcp" \
  -H 'content-type: application/json' -H "Authorization: Bearer $TOKEN" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{}}}'
SID=$(awk 'tolower($1)=="mcp-session-id:"{print $2}' "$L/init-headers.txt" | tr -d '\r')

# ── 1) run que completa (DocPipeline approved=yes) ──────────────────
rpc '{"jsonrpc":"2.0","id":2,"method":"run/start","params":{"name":"DocPipeline","arguments":{"repo":"metrics-demo","approved":"yes"}}}' "$L/run-completed-start.json"
RID_OK=$(jget "$L/run-completed-start.json" result.runId)
for i in $(seq 1 30); do
  rpc '{"jsonrpc":"2.0","id":3,"method":"run/status","params":{"runId":"'"$RID_OK"'"}}' "$L/run-completed-status.json"
  s=$(jget "$L/run-completed-status.json" result.state); log "  run completed? state=$s"
  [[ "$s" == "completed" || "$s" == "failed" ]] && break
  sleep 1
done

# ── 2) run cancelada (SlowBuild + run/cancel) ──────────────────────
rpc '{"jsonrpc":"2.0","id":4,"method":"run/start","params":{"name":"SlowBuild","arguments":{"target":"metrics-cancel"}}}' "$L/run-canceled-start.json"
RID_CX=$(jget "$L/run-canceled-start.json" result.runId)
sleep 1
rpc '{"jsonrpc":"2.0","id":5,"method":"run/cancel","params":{"runId":"'"$RID_CX"'"}}' "$L/run-canceled-cancel.json"
for i in $(seq 1 20); do
  rpc '{"jsonrpc":"2.0","id":6,"method":"run/status","params":{"runId":"'"$RID_CX"'"}}' "$L/run-canceled-status.json"
  s=$(jget "$L/run-canceled-status.json" result.state); log "  run canceled? state=$s"
  [[ "$s" == "canceled" || "$s" == "failed" || "$s" == "completed" ]] && break
  sleep 1
done

# ── 3) tools/call ok ──────────────────────────────────────────────
curl -s -o "$L/toolscall-ok.json" -X POST "$BASE/mcp" -H 'content-type: application/json' \
  -H "Authorization: Bearer $TOKEN" -H "Mcp-Session-Id: $SID" \
  -d '{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"DocPipeline","arguments":{"repo":"tc-ok","approved":"yes"},'"$META"'}}'
log "tools/call ok -> isError=$(jget "$L/toolscall-ok.json" result.isError)"

# ── 4) tools/call erro (tool inexistente) ────────────────────────
curl -s -o "$L/toolscall-err.json" -X POST "$BASE/mcp" -H 'content-type: application/json' \
  -H "Authorization: Bearer $TOKEN" -H "Mcp-Session-Id: $SID" \
  -d '{"jsonrpc":"2.0","id":8,"method":"tools/call","params":{"name":"NaoExiste","arguments":{},'"$META"'}}'
log "tools/call erro -> error.code=$(jget "$L/toolscall-err.json" error.code)"

# ── /metrics depois, sem bearer ─────────────────────────────────
MZ_CODE=$(curl -s -o "$L/metrics-after.txt" -w '%{http_code}' "$BASE/metrics")
MZ_CT=$(curl -s -o /dev/null -D - "$BASE/metrics" | awk 'tolower($1)=="content-type:"{ $1=""; print }' | tr -d '\r' | sed 's/^ *//')
log "GET /metrics (sem bearer) -> HTTP $MZ_CODE ; Content-Type:$MZ_CT"
log "----- /metrics (depois) -----"; cat "$L/metrics-after.txt" | tee -a "$CLIENT_LOG"

M="$L/metrics-after.txt"
RC=$(metric 'mhl_serve_runs_total{outcome="completed"}' "$M")
RX=$(metric 'mhl_serve_runs_total{outcome="canceled"}' "$M")
DCOUNT=$(metric 'mhl_serve_run_duration_seconds_count' "$M")
TOK=$(metric 'mhl_serve_tool_calls_total{outcome="ok"}' "$M")
TERR=$(metric 'mhl_serve_tool_calls_total{outcome="error"}' "$M")

ge() { awk -v a="${1:-0}" -v b="$2" 'BEGIN{exit !(a+0>=b+0)}'; }

ok="yes"
[[ "$MZ_CODE" == "200" ]]           || { ok="no"; log "  ! /metrics sem bearer != 200"; }
echo "$MZ_CT" | grep -qi "text/plain" || { ok="no"; log "  ! Content-Type de /metrics não é text/plain"; }
ge "$RC" 1     || { ok="no"; log "  ! runs_total{completed}=$RC < 1"; }
ge "$RX" 1     || { ok="no"; log "  ! runs_total{canceled}=$RX < 1"; }
ge "$DCOUNT" 2 || { ok="no"; log "  ! run_duration_seconds_count=$DCOUNT < 2"; }
ge "$TOK" 1    || { ok="no"; log "  ! tool_calls_total{ok}=$TOK < 1"; }
ge "$TERR" 1   || { ok="no"; log "  ! tool_calls_total{error}=$TERR < 1"; }
for g in mhl_serve_runs_active mhl_serve_runs_queued mhl_serve_sessions_active; do
  grep -q "^$g " "$M" || { ok="no"; log "  ! gauge $g ausente"; }
done

if [[ "$ok" == "yes" ]]; then
  log "RESULTADO: FUNCIONOU — contadores e gauges refletem a atividade; /metrics livre de auth"
  echo "PASS"; exit 0
else
  log "RESULTADO: NÃO FUNCIONOU — veja as verificações acima"
  echo "FAIL"; exit 1
fi
