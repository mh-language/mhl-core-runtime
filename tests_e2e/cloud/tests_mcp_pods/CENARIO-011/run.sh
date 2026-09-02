#!/usr/bin/env bash
# CENARIO-011 — Estado de run sobrevive a restart do processo (--state-dir)
#
# Com --state-dir D: run/start DocPipeline(approved=no) -> para no gate -> mata o
# processo -> novo processo no mesmo D -> run/status reconstroi (failed/resumable)
# -> run/resume(approved=yes) -> completed.
# Controle: mesmo fluxo SEM --state-dir -> unknown runId apos restart.
#
# Modo handshake. Copia tests/cloud/mhl se preciso.

set -u

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MHL="$HERE/mhl"
[[ -x "$MHL" ]] || { cp "$HERE/../../mhl" "$MHL" 2>/dev/null && chmod +x "$MHL"; }
[[ -x "$MHL" ]] || { echo "FALHA: binário mhl não encontrado"; exit 1; }

WORKFLOWS="$HERE/../.."
TOKEN="cenario-011-$(date +%s)"
STATE="$(mktemp -d)"

L="$HERE/logs"; mkdir -p "$L"
CLIENT_LOG="$L/client.log"; : > "$CLIENT_LOG"
log() { echo "[$(date '+%H:%M:%S')] $*" | tee -a "$CLIENT_LOG"; }

PIDS=()
cleanup() {
  for p in "${PIDS[@]:-}"; do [[ -n "$p" ]] && kill -0 "$p" 2>/dev/null && { kill -KILL "$p" 2>/dev/null; wait "$p" 2>/dev/null; }; done
  rm -rf "$STATE"
}
trap cleanup EXIT

jget() { python3 -c 'import json,sys;d=json.load(open(sys.argv[1]))
for p in sys.argv[2].split("."):
  d = d.get(p, {}) if isinstance(d, dict) else {}
print("" if d == {} else d)' "$1" "$2"; }

start_srv() { # <addr> <logfile> [extra args...]
  local addr="$1" lf="$2"; shift 2
  "$MHL" serve mcp --http --addr "$addr" --token "$TOKEN" "$@" "$WORKFLOWS" >>"$lf" 2>&1 &
  local pid=$!; PIDS+=("$pid"); echo "$pid"
}
wait_ready() { # <pid> <base>
  for i in $(seq 1 50); do
    kill -0 "$1" 2>/dev/null || return 1
    [[ "$(curl -s -o /dev/null -w '%{http_code}' "$2/healthz")" == "200" ]] && return 0
    sleep 0.2
  done; return 1
}
new_session() { # <base> <tag>
  curl -s -D "$L/init-$2-headers.txt" -o /dev/null -X POST "$1/mcp" \
    -H 'content-type: application/json' -H "Authorization: Bearer $TOKEN" \
    -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{}}}'
  awk 'tolower($1)=="mcp-session-id:"{print $2}' "$L/init-$2-headers.txt" | tr -d '\r'
}
rpc() { curl -s -o "$4" -X POST "$1/mcp" -H 'content-type: application/json' \
  -H "Authorization: Bearer $TOKEN" -H "Mcp-Session-Id: $2" -d "$3"; }

########################################################################
run_case() { # <nome> <base> <addr> <state-flag...>  -> exporta RESULT_*
  local name="$1" base="$2" addr="$3"; shift 3
  local lf1="$L/${name}-server1.log" lf2="$L/${name}-server2.log"
  : > "$lf1"; : > "$lf2"

  log "[$name] processo 1: serve mcp --http $* em $addr"
  local p1; p1=$(start_srv "$addr" "$lf1" "$@")
  wait_ready "$p1" "$base" || { log "[$name] FALHA: proc 1 não subiu"; RESULT_OK=no; return; }
  local sid1; sid1=$(new_session "$base" "${name}-1")

  rpc "$base" "$sid1" '{"jsonrpc":"2.0","id":2,"method":"run/start","params":{"name":"DocPipeline","arguments":{"repo":"billing","approved":"no"}}}' "$L/${name}-run-start.json"
  local rid; rid=$(jget "$L/${name}-run-start.json" result.runId)
  log "[$name] run/start -> runId=$rid state=$(jget "$L/${name}-run-start.json" result.state)"
  [[ -n "$rid" ]] || { RESULT_OK=no; log "[$name] FALHA: sem runId"; return; }
  sleep 2   # Draft(1s) -> Review fail()

  rpc "$base" "$sid1" '{"jsonrpc":"2.0","id":3,"method":"run/status","params":{"runId":"'"$rid"'"}}' "$L/${name}-status-before.json"
  log "[$name] run/status antes do restart: state=$(jget "$L/${name}-status-before.json" result.state) step=$(jget "$L/${name}-status-before.json" result.step)"

  log "[$name] matando proc 1 (PID $p1)"
  kill -KILL "$p1" 2>/dev/null; wait "$p1" 2>/dev/null

  log "[$name] processo 2: serve mcp --http $* em $addr (mesmo dir)"
  local p2; p2=$(start_srv "$addr" "$lf2" "$@")
  wait_ready "$p2" "$base" || { log "[$name] FALHA: proc 2 não subiu"; RESULT_OK=no; return; }
  local sid2; sid2=$(new_session "$base" "${name}-2")

  rpc "$base" "$sid2" '{"jsonrpc":"2.0","id":4,"method":"run/status","params":{"runId":"'"$rid"'"}}' "$L/${name}-status-after.json"
  log "[$name] run/status pós-restart:"; python3 -m json.tool "$L/${name}-status-after.json" | tee -a "$CLIENT_LOG"

  RESULT_STATE=$(jget "$L/${name}-status-after.json" result.state)
  RESULT_STEP=$(jget "$L/${name}-status-after.json" result.step)
  RESULT_RESUMABLE=$(jget "$L/${name}-status-after.json" result.resumable)
  RESULT_ERRCODE=$(jget "$L/${name}-status-after.json" error.code)
  RESULT_RID="$rid"; RESULT_BASE="$base"; RESULT_SID2="$sid2"; RESULT_P2="$p2"
  RESULT_OK=yes
}

########################################################################
# Caso A: COM --state-dir
########################################################################
run_case "with-state-dir" "http://127.0.0.1:8731" "127.0.0.1:8731" --state-dir "$STATE"
A_STATE="$RESULT_STATE"; A_STEP="$RESULT_STEP"; A_RESUMABLE="$RESULT_RESUMABLE"
A_RID="$RESULT_RID"; A_BASE="$RESULT_BASE"; A_SID2="$RESULT_SID2"; A_P2="$RESULT_P2"; A_OK="$RESULT_OK"

A_RESUME_STATE=""
if [[ "$A_OK" == "yes" && ( "$A_STATE" == "failed" || "$A_STATE" == "canceled" ) ]]; then
  rpc "$A_BASE" "$A_SID2" '{"jsonrpc":"2.0","id":5,"method":"run/resume","params":{"runId":"'"$A_RID"'","arguments":{"approved":"yes"}}}' "$L/with-state-dir-resume.json"
  log "[with-state-dir] run/resume -> state=$(jget "$L/with-state-dir-resume.json" result.state) $(jget "$L/with-state-dir-resume.json" error.message)"
  for i in $(seq 1 30); do
    rpc "$A_BASE" "$A_SID2" '{"jsonrpc":"2.0","id":6,"method":"run/status","params":{"runId":"'"$A_RID"'"}}' "$L/with-state-dir-status-final.json"
    s=$(jget "$L/with-state-dir-status-final.json" result.state)
    [[ "$s" == "completed" || "$s" == "failed" ]] && { A_RESUME_STATE="$s"; break; }
    sleep 1
  done
  log "[with-state-dir] run/status final:"; python3 -m json.tool "$L/with-state-dir-status-final.json" | tee -a "$CLIENT_LOG"
fi
A_PUBLISHED=$(jget "$L/with-state-dir-status-final.json" result.vars.published 2>/dev/null || true)
kill -KILL "$A_P2" 2>/dev/null; wait "$A_P2" 2>/dev/null

########################################################################
# Caso B (controle): SEM --state-dir
########################################################################
run_case "no-state-dir" "http://127.0.0.1:8732" "127.0.0.1:8732"
B_ERRCODE="$RESULT_ERRCODE"; B_STATE="$RESULT_STATE"; B_OK="$RESULT_OK"; B_P2="$RESULT_P2"
kill -KILL "$B_P2" 2>/dev/null; wait "$B_P2" 2>/dev/null

########################################################################
# verdite
########################################################################
ok="yes"
[[ "$A_OK" == "yes" ]]                         || { ok="no"; log "  ! caso with-state-dir não completou o fluxo"; }
[[ "$A_STATE" == "failed" ]]                   || { ok="no"; log "  ! [A] run/status pós-restart state=$A_STATE != failed"; }
[[ "$A_STEP" == "Review" ]]                    || { ok="no"; log "  ! [A] step pós-restart=$A_STEP != Review"; }
[[ "$A_RESUMABLE" == "True" || "$A_RESUMABLE" == "true" ]] || { ok="no"; log "  ! [A] pós-restart sem resumable:true"; }
[[ "$A_RESUME_STATE" == "completed" ]]         || { ok="no"; log "  ! [A] run/resume não completou (state=$A_RESUME_STATE)"; }
[[ "$A_PUBLISHED" == "published docs for billing (reviewed)" ]] || { ok="no"; log "  ! [A] vars.published inesperado: $A_PUBLISHED"; }
[[ "$B_ERRCODE" == "-32602" ]]                 || { ok="no"; log "  ! [B] sem --state-dir: run/status pós-restart não deu -32602 (state=$B_STATE code=$B_ERRCODE)"; }

if [[ "$ok" == "yes" ]]; then
  log "RESULTADO: FUNCIONOU — com --state-dir o runId sobrevive ao restart e retoma; sem, some (unknown runId)"
  echo "PASS"; exit 0
else
  log "RESULTADO: NÃO FUNCIONOU — veja as verificações acima"
  echo "FAIL"; exit 1
fi
