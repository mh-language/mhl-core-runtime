#!/usr/bin/env bash
# CENARIO-010 — Cap de wall-clock por passo (step Ship timeout 1s sobre sleep 3)
#
# run/start SlowBuild -> poll -> failed em Ship, erro menciona timeout,
# reached tem Compile e Package -> run/resume -> Ship re-executa e falha de novo.
#
# Modo handshake, --state-dir. Copia sample/cloud/mhl se preciso.

set -u

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MHL="$HERE/mhl"
[[ -x "$MHL" ]] || { cp "$HERE/../../mhl" "$MHL" 2>/dev/null && chmod +x "$MHL"; }
[[ -x "$MHL" ]] || { echo "FALHA: binário mhl não encontrado"; exit 1; }

WORKFLOWS="$HERE/../.."
ADDR="127.0.0.1:8730"
BASE="http://$ADDR"
TOKEN="cenario-010-$(date +%s)"
STATE="$(mktemp -d)"

L="$HERE/logs"; mkdir -p "$L"
SL="$L/mcp-server.log"; : > "$SL"
CLIENT_LOG="$L/client.log"; : > "$CLIENT_LOG"

log() { echo "[$(date '+%H:%M:%S')] $*" | tee -a "$CLIENT_LOG"; }
cleanup() {
  if [[ -n "${PID:-}" ]] && kill -0 "$PID" 2>/dev/null; then kill -TERM "$PID" 2>/dev/null; wait "$PID" 2>/dev/null; fi
  rm -rf "$STATE"
}
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

poll_until_terminal() { # <outfile-prefix> -> deixa <prefix>-final.json
  local pref="$1" st
  for i in $(seq 1 40); do
    rpc '{"jsonrpc":"2.0","id":9,"method":"run/status","params":{"runId":"'"$RID"'"}}' "$L/${pref}-$i.json"
    st=$(jget "$L/${pref}-$i.json" result.state)
    log "  $pref[$i]: state=$st step=$(jget "$L/${pref}-$i.json" result.step)"
    case "$st" in failed|completed|canceled) cp "$L/${pref}-$i.json" "$L/${pref}-final.json"; return 0;; esac
    sleep 1
  done
  return 1
}

# ── tentativa 1 ──────────────────────────────────────────────────────
rpc '{"jsonrpc":"2.0","id":2,"method":"run/start","params":{"name":"SlowBuild","arguments":{"target":"timeout-demo"}}}' "$L/run-start.json"
RID=$(jget "$L/run-start.json" result.runId)
log "run/start -> runId=$RID state=$(jget "$L/run-start.json" result.state)"
[[ -n "$RID" ]] || { cat "$L/run-start.json" | tee -a "$CLIENT_LOG"; echo "FAIL"; exit 1; }

poll_until_terminal "status1" || { log "FALHA: run 1 não chegou a estado terminal"; echo "FAIL"; exit 1; }
log "run/status (tentativa 1):"; python3 -m json.tool "$L/status1-final.json" | tee -a "$CLIENT_LOG"

S1=$(jget "$L/status1-final.json" result.state)
STEP1=$(jget "$L/status1-final.json" result.step)
ERR1=$(jget "$L/status1-final.json" result.error)
REACHED1=$(jget "$L/status1-final.json" result.reached)

# ── run/resume -> tentativa 2 ───────────────────────────────────────
rpc '{"jsonrpc":"2.0","id":3,"method":"run/resume","params":{"runId":"'"$RID"'"}}' "$L/run-resume.json"
log "run/resume -> state=$(jget "$L/run-resume.json" result.state) $(jget "$L/run-resume.json" error.message)"
poll_until_terminal "status2" || { log "FALHA: run 2 não chegou a estado terminal"; echo "FAIL"; exit 1; }
log "run/status (tentativa 2):"; python3 -m json.tool "$L/status2-final.json" | tee -a "$CLIENT_LOG"
S2=$(jget "$L/status2-final.json" result.state)
STEP2=$(jget "$L/status2-final.json" result.step)
ERR2=$(jget "$L/status2-final.json" result.error)

# ── verdite ────────────────────────────────────────────────────────
ok="yes"
[[ "$S1" == "failed" ]]                 || { ok="no"; log "  ! tentativa 1 state=$S1 != failed"; }
[[ "$STEP1" == "Ship" ]]               || { ok="no"; log "  ! tentativa 1 step=$STEP1 != Ship"; }
echo "$ERR1" | grep -qi "timeout"      || { ok="no"; log "  ! erro 1 não menciona timeout: $ERR1"; }
echo "$REACHED1" | grep -q "Compile"   || { ok="no"; log "  ! reached 1 sem Compile: $REACHED1"; }
echo "$REACHED1" | grep -q "Package"   || { ok="no"; log "  ! reached 1 sem Package: $REACHED1"; }
[[ "$S2" == "failed" ]]                 || { ok="no"; log "  ! tentativa 2 state=$S2 != failed"; }
[[ "$STEP2" == "Ship" ]]               || { ok="no"; log "  ! tentativa 2 step=$STEP2 != Ship"; }
echo "$ERR2" | grep -qi "timeout"      || { ok="no"; log "  ! erro 2 não menciona timeout: $ERR2"; }

if [[ "$ok" == "yes" ]]; then
  log "RESULTADO: FUNCIONOU — Ship estoura o timeout, falha, e run/resume re-executa Ship com orçamento novo"
  echo "PASS"; exit 0
else
  log "RESULTADO: NÃO FUNCIONOU — veja as verificações acima"
  echo "FAIL"; exit 1
fi
