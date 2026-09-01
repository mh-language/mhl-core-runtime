#!/usr/bin/env bash
# CENARIO-007 — Ciclo de vida assincrono run/*
#
# run/start (approved=no) -> poll run/status ate failed/resumable no gate
# -> run/resume (approved=yes) -> completed com vars.published
# -> run/list mostra so as runs do caller ; outra sessao -> unknown runId
# -> run/cancel numa run SlowBuild working -> canceled
#
# Modo handshake (initialize -> Mcp-Session-Id). Copia tests/cloud/mhl se preciso.

set -u

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MHL="$HERE/mhl"
[[ -x "$MHL" ]] || { cp "$HERE/../../mhl" "$MHL" 2>/dev/null && chmod +x "$MHL"; }
[[ -x "$MHL" ]] || { echo "FALHA: binário mhl não encontrado"; exit 1; }

WORKFLOWS="$HERE/../.."
ADDR="127.0.0.1:8726"
BASE="http://$ADDR"
TOKEN="cenario-007-$(date +%s)"
STATE="$(mktemp -d)"

L="$HERE/logs"; mkdir -p "$L"
SERVER_LOG="$L/mcp-server.log"; : > "$SERVER_LOG"
CLIENT_LOG="$L/client.log";     : > "$CLIENT_LOG"
POLL_LOG="$L/run-status-poll.log"; : > "$POLL_LOG"

log() { echo "[$(date '+%H:%M:%S')] $*" | tee -a "$CLIENT_LOG"; }

cleanup() {
  if [[ -n "${SERVER_PID:-}" ]] && kill -0 "$SERVER_PID" 2>/dev/null; then
    kill -TERM "$SERVER_PID" 2>/dev/null; wait "$SERVER_PID" 2>/dev/null
  fi
  rm -rf "$STATE"
}
trap cleanup EXIT

init_session() { # -> imprime Mcp-Session-Id
  local h="$L/init-$1-headers.txt"
  curl -s -D "$h" -o "$L/init-$1.json" -X POST "$BASE/mcp" \
    -H 'content-type: application/json' -H "Authorization: Bearer $TOKEN" \
    -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"c7-'"$1"'","version":"1"}}}' >/dev/null
  awk 'tolower($1)=="mcp-session-id:"{print $2}' "$h" | tr -d '\r'
}
rpc() { # <sid> <json> <outfile>
  curl -s -o "$3" -X POST "$BASE/mcp" -H 'content-type: application/json' \
    -H "Authorization: Bearer $TOKEN" -H "Mcp-Session-Id: $1" -d "$2"
}
jget() { python3 -c 'import json,sys;d=json.load(open(sys.argv[1]))
k=sys.argv[2].split(".")
for p in k:
  d=d.get(p,{}) if isinstance(d,dict) else {}
print(d if d!={} else "")' "$1" "$2"; }

# ── servidor ─────────────────────────────────────────────────────────────
log "iniciando servidor em $ADDR"
"$MHL" serve mcp --http --addr "$ADDR" --token "$TOKEN" --state-dir "$STATE" \
  "$WORKFLOWS" >>"$SERVER_LOG" 2>&1 &
SERVER_PID=$!
ready=""
for i in $(seq 1 50); do
  kill -0 "$SERVER_PID" 2>/dev/null || { log "FALHA: servidor terminou"; break; }
  [[ "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/healthz")" == "200" ]] && { ready="yes"; break; }
  sleep 0.2
done
[[ "$ready" == "yes" ]] || { log "RESULTADO: NÃO FUNCIONOU — servidor não ficou pronto"; echo "FAIL"; exit 1; }

SID_A=$(init_session A); SID_B=$(init_session B)
log "sessão A=$SID_A  sessão B=$SID_B"

# ── run/start DocPipeline approved=no ────────────────────────────────────
rpc "$SID_A" '{"jsonrpc":"2.0","id":2,"method":"run/start","params":{"name":"DocPipeline","arguments":{"repo":"async-demo","approved":"no"}}}' "$L/run-start.json"
RID=$(jget "$L/run-start.json" result.runId)
log "run/start -> runId=$RID state=$(jget "$L/run-start.json" result.state)"
[[ -n "$RID" ]] || { log "FALHA: sem runId"; python3 -m json.tool "$L/run-start.json" | tee -a "$CLIENT_LOG"; echo "FAIL"; exit 1; }

# ── poll ate parar no gate ──────────────────────────────────────────────
STATE1=""
for i in $(seq 1 40); do
  rpc "$SID_A" '{"jsonrpc":"2.0","id":3,"method":"run/status","params":{"runId":"'"$RID"'"}}' "$L/status-$i.json"
  st=$(jget "$L/status-$i.json" result.state)
  echo "[$i] $(cat "$L/status-$i.json")" >> "$POLL_LOG"
  log "  run/status[$i]: state=$st step=$(jget "$L/status-$i.json" result.step)"
  [[ "$st" == "failed" || "$st" == "completed" || "$st" == "canceled" ]] && { STATE1="$st"; cp "$L/status-$i.json" "$L/status-parked.json"; break; }
  sleep 1
done
log "estado ao parar: ${STATE1:-<timeout>}"

# ── run/resume approved=yes ─────────────────────────────────────────────
rpc "$SID_A" '{"jsonrpc":"2.0","id":4,"method":"run/resume","params":{"runId":"'"$RID"'","arguments":{"approved":"yes"}}}' "$L/run-resume.json"
log "run/resume -> state=$(jget "$L/run-resume.json" result.state) $(jget "$L/run-resume.json" error.message)"

STATE2=""
for i in $(seq 1 40); do
  rpc "$SID_A" '{"jsonrpc":"2.0","id":5,"method":"run/status","params":{"runId":"'"$RID"'"}}' "$L/status-after-$i.json"
  st=$(jget "$L/status-after-$i.json" result.state)
  log "  run/status(pós-resume)[$i]: state=$st"
  [[ "$st" == "completed" || "$st" == "failed" || "$st" == "canceled" ]] && { STATE2="$st"; cp "$L/status-after-$i.json" "$L/status-final.json"; break; }
  sleep 1
done
log "run/status final:"; python3 -m json.tool "$L/status-final.json" 2>/dev/null | tee -a "$CLIENT_LOG"

# ── run/list A e B ─────────────────────────────────────────────────────
rpc "$SID_A" '{"jsonrpc":"2.0","id":6,"method":"run/list"}' "$L/run-list-A.json"
rpc "$SID_B" '{"jsonrpc":"2.0","id":6,"method":"run/list"}' "$L/run-list-B.json"
rpc "$SID_B" '{"jsonrpc":"2.0","id":7,"method":"run/status","params":{"runId":"'"$RID"'"}}' "$L/run-status-B.json"
log "run/list A: $(cat "$L/run-list-A.json")"
log "run/list B: $(cat "$L/run-list-B.json")"
log "run/status B (runId de A): $(cat "$L/run-status-B.json")"

# ── run/cancel numa SlowBuild working ─────────────────────────────────
rpc "$SID_A" '{"jsonrpc":"2.0","id":8,"method":"run/start","params":{"name":"SlowBuild","arguments":{"target":"c7"}}}' "$L/slow-start.json"
RID2=$(jget "$L/slow-start.json" result.runId)
log "SlowBuild run/start -> runId=$RID2 state=$(jget "$L/slow-start.json" result.state)"
sleep 1
rpc "$SID_A" '{"jsonrpc":"2.0","id":9,"method":"run/cancel","params":{"runId":"'"$RID2"'"}}' "$L/slow-cancel.json"
log "run/cancel -> state=$(jget "$L/slow-cancel.json" result.state)"
sleep 1
rpc "$SID_A" '{"jsonrpc":"2.0","id":10,"method":"run/status","params":{"runId":"'"$RID2"'"}}' "$L/slow-status.json"
log "SlowBuild run/status pós-cancel -> state=$(jget "$L/slow-status.json" result.state)"

# ── verdite ──────────────────────────────────────────────────────────
verdict=$(python3 - "$L/status-parked.json" "$L/status-final.json" "$L/run-list-A.json" "$L/run-list-B.json" "$L/run-status-B.json" "$L/slow-status.json" "$RID" <<'PY'
import json, sys
parked, final, la, lb, sb, slow, rid = sys.argv[1:8]
P = json.load(open(parked)).get("result", {})
F = json.load(open(final)).get("result", {})
LA = json.load(open(la)).get("result", {}).get("runs", [])
LB = json.load(open(lb)).get("result", {}).get("runs", [])
SB = json.load(open(sb))
SLOW = json.load(open(slow)).get("result", {})
fails = []
if P.get("state") != "failed":          fails.append(f"parou em state={P.get('state')!r}, esperado failed")
if P.get("step") != "Review":           fails.append(f"step no gate={P.get('step')!r}, esperado Review")
if P.get("resumable") is not True:       fails.append("parked sem resumable:true")
if P.get("reached") != ["Draft","Review"]: fails.append(f"reached no gate={P.get('reached')!r}")
if F.get("state") != "completed":        fails.append(f"final state={F.get('state')!r}, esperado completed")
if F.get("reached") != ["Draft","Review","Publish"]: fails.append(f"reached final={F.get('reached')!r}")
if F.get("vars",{}).get("published") != "published docs for async-demo (reviewed)":
    fails.append(f"vars.published={F.get('vars',{}).get('published')!r}")
if rid not in [r.get('runId') for r in LA]: fails.append("run/list A nao contem o runId")
if LB != []:                            fails.append(f"run/list B nao vazio: {LB!r}")
if SB.get("error",{}).get("code") != -32602: fails.append(f"run/status B sem -32602: {SB!r}")
if SLOW.get("state") != "canceled":     fails.append(f"SlowBuild pos-cancel state={SLOW.get('state')!r}, esperado canceled")
for f in fails: print("FAILCHECK " + f)
print("VERDICT " + ("FAIL" if fails else "PASS"))
PY
)
echo "$verdict" | while IFS= read -r l; do case "$l" in FAILCHECK\ *) log "  ! ${l#FAILCHECK }";; esac; done

if echo "$verdict" | grep -q "VERDICT PASS"; then
  log "RESULTADO: FUNCIONOU — start/status/resume/list/cancel no ciclo completo"
  echo "PASS"; exit 0
else
  log "RESULTADO: NÃO FUNCIONOU — veja as verificações acima"
  echo "FAIL"; exit 1
fi
