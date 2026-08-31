#!/usr/bin/env bash
# CENARIO-013 — mhl serve mcp --http com `extension store S` -> S3 (MinIO)
# Análogo do CENARIO-010, mas o backend de estado é a extensão oficial mhl-store-s3.
source "$(dirname "${BASH_SOURCE[0]}")/../_lib.sh"

s3_ensure
PFX="c013/"
s3_wipe "$PFX"
s3_project

PROBE_LOG="$PROJ/probe.jsonl"
CLOUD="$(cd "$ROOT/../cloud" && pwd)"
cp "$CLOUD/docs-workflow.mh" "$CLOUD/slow-build.mh" "$PROJ/" \
  || die "faltam docs-workflow.mh/slow-build.mh em $CLOUD"
cat > "$PROJ/store.mh" <<EOF
extension store S {
    bucket: env("S3_BUCKET")
    endpoint: env("S3_ENDPOINT")
    region: "us-east-1"
    access_key_id: env("AWS_ACCESS_KEY_ID")
    secret_access_key: env("AWS_SECRET_ACCESS_KEY")
    prefix: "$PFX"
    log: "$PROBE_LOG"
}
EOF

ADDR="127.0.0.1:8791"; BASE="http://$ADDR"; TOKEN="cenario-ext-013-$(date +%s)"
SL="$L/mcp-server.log"; : > "$SL"
CURL=(curl -s --max-time 10)

start_server() { # extra args... -> SRV = pid do mhl (child direto)
  local cwd="$PWD"; cd "$L"
  "$MHL" serve mcp --http --addr "$ADDR" --token "$TOKEN" "$@" "$PROJ" >>"$SL" 2>&1 &
  SRV=$!
  cd "$cwd"
  _atexit_extra='for _x in "${SRV:-}" "${SRV2:-}"; do [ -n "$_x" ] && kill -KILL "$_x" 2>/dev/null; done'
}
stop_server() { # <pid>
  [[ -z "${1:-}" ]] && return 0
  kill -TERM "$1" 2>/dev/null
  for _ in $(seq 1 24); do kill -0 "$1" 2>/dev/null || break; sleep 0.25; done
  kill -KILL "$1" 2>/dev/null; wait "$1" 2>/dev/null
  for _ in $(seq 1 16); do "${CURL[@]}" -o /dev/null "$BASE/healthz" 2>/dev/null && sleep 0.25 || break; done
}
wait_ready() {
  for _ in $(seq 1 80); do
    kill -0 "$1" 2>/dev/null || return 1
    [[ "$("${CURL[@]}" -o /dev/null -w '%{http_code}' "$BASE/healthz" 2>/dev/null)" == "200" ]] && return 0
    sleep 0.25
  done; return 1
}
initsid() { # <hdrfile>
  "${CURL[@]}" -D "$1" -o /dev/null -X POST "$BASE/mcp" -H 'content-type: application/json' \
    -H "Authorization: Bearer $TOKEN" \
    -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{}}}'
  awk 'tolower($1)=="mcp-session-id:"{print $2}' "$1" | tr -d '\r'
}
rpc() { "${CURL[@]}" -o "$3" -X POST "$BASE/mcp" -H 'content-type: application/json' \
  -H "Authorization: Bearer $TOKEN" -H "Mcp-Session-Id: ${1}" -d "$2"; }

# chaves lógicas no bucket = objetos sob <prefixo> (mc já os imprime relativos
# ao prefixo), apenas sem o sufixo .json
state_keys() { # <outfile>
  s3_mc "mc ls --recursive local/$S3_BUCKET/$PFX" 2>/dev/null \
    | awk '{print $NF}' | grep '\.json$' | sed 's|\.json$||' | sort > "$1"
}

########################################################################
# Parte A — estado durável no S3 + resume + restart-reclaim
########################################################################
start_server --max-concurrent-runs 2
wait_ready "$SRV" || { tail -20 "$SL" >> "$CLIENT_LOG"; die "servidor 1 não ficou pronto"; }
SID=$(initsid "$L/init-headers.txt"); log "sessão=$SID"

rpc "$SID" '{"jsonrpc":"2.0","id":2,"method":"run/start","params":{"name":"DocPipeline","arguments":{"repo":"ext-demo","approved":"no"}}}' "$L/run-start.json"
RID=$(jget "$L/run-start.json" result.runId)
log "run/start #1 -> runId=$RID"
[[ -n "$RID" ]] || { cat "$L/run-start.json" >> "$CLIENT_LOG"; die "sem runId"; }
for _ in $(seq 1 15); do
  rpc "$SID" '{"jsonrpc":"2.0","id":3,"method":"run/status","params":{"runId":"'"$RID"'"}}' "$L/status1.json"
  [[ "$(jget "$L/status1.json" result.state)" == "failed" ]] && break; sleep 1
done
log "run #1 parada: state=$(jget "$L/status1.json" result.state) step=$(jget "$L/status1.json" result.step)"

state_keys "$L/state-keys.txt"
log "chaves no store (S3):"; cat "$L/state-keys.txt" | tee -a "$CLIENT_LOG"
KEYS_LOGGED=$(grep -o '"key":"[^"]*"' "$PROBE_LOG" | sort -u | tr '\n' ' ')
log "keys no probe.jsonl: $KEYS_LOGGED"

rpc "$SID" '{"jsonrpc":"2.0","id":4,"method":"run/resume","params":{"runId":"'"$RID"'","arguments":{"approved":"yes"}}}' "$L/run-resume.json"
RESUME_ST=""
for _ in $(seq 1 20); do
  rpc "$SID" '{"jsonrpc":"2.0","id":5,"method":"run/status","params":{"runId":"'"$RID"'"}}' "$L/status1b.json"
  s=$(jget "$L/status1b.json" result.state); [[ "$s" == completed || "$s" == failed ]] && { RESUME_ST="$s"; break; }; sleep 1
done
log "run #1 pós-resume: $RESUME_ST"

# run #2 fica parada para o teste de restart
rpc "$SID" '{"jsonrpc":"2.0","id":6,"method":"run/start","params":{"name":"DocPipeline","arguments":{"repo":"reclaim","approved":"no"}}}' "$L/run-start-2.json"
RID2=$(jget "$L/run-start-2.json" result.runId)
for _ in $(seq 1 15); do
  rpc "$SID" '{"jsonrpc":"2.0","id":6,"method":"run/status","params":{"runId":"'"$RID2"'"}}' "$L/status2.json"
  [[ "$(jget "$L/status2.json" result.state)" == "failed" ]] && break; sleep 1
done
log "run #2 antes do restart: state=$(jget "$L/status2.json" result.state) resumable=$(jget "$L/status2.json" result.resumable)"

stop_server "$SRV"; SRV=""
log "servidor 1 encerrado — subindo servidor 2 no mesmo bucket"
start_server --max-concurrent-runs 2
SRV2="$SRV"; SRV=""
wait_ready "$SRV2" || { tail -20 "$SL" >> "$CLIENT_LOG"; die "servidor 2 não ficou pronto (ver mcp-server.log)"; }
SID=$(initsid "$L/init2-headers.txt")
rpc "$SID" '{"jsonrpc":"2.0","id":7,"method":"run/status","params":{"runId":"'"$RID2"'"}}' "$L/status-after-restart.json"
log "run/status pós-restart (#2):"; python3 -m json.tool "$L/status-after-restart.json" | tee -a "$CLIENT_LOG"
RECLAIM_STATE=$(jget "$L/status-after-restart.json" result.state)
RECLAIM_RESUMABLE=$(jget "$L/status-after-restart.json" result.resumable)

########################################################################
# Parte B — duas runs concorrentes (approved=no) → chaves run/<id> disjuntas
########################################################################
rpc "$SID" '{"jsonrpc":"2.0","id":10,"method":"run/start","params":{"name":"DocPipeline","arguments":{"repo":"cc1","approved":"no"}}}' "$L/ccstart-1.json" & p1=$!
rpc "$SID" '{"jsonrpc":"2.0","id":11,"method":"run/start","params":{"name":"DocPipeline","arguments":{"repo":"cc2","approved":"no"}}}' "$L/ccstart-2.json" & p2=$!
wait "$p1" "$p2"
CR1=$(jget "$L/ccstart-1.json" result.runId); CR2=$(jget "$L/ccstart-2.json" result.runId)
log "run/start concorrentes: CR1=$CR1 CR2=$CR2"
for _ in $(seq 1 20); do
  s1=$(rpc "$SID" '{"jsonrpc":"2.0","id":12,"method":"run/status","params":{"runId":"'"$CR1"'"}}' "$L/cc1.json"; jget "$L/cc1.json" result.state)
  s2=$(rpc "$SID" '{"jsonrpc":"2.0","id":13,"method":"run/status","params":{"runId":"'"$CR2"'"}}' "$L/cc2.json"; jget "$L/cc2.json" result.state)
  [[ "$s1" == "failed" && "$s2" == "failed" ]] && break; sleep 1
done
rpc "$SID" '{"jsonrpc":"2.0","id":14,"method":"run/list"}' "$L/run-list.json"
n=$(python3 -c 'import json,sys;print(len(json.load(open(sys.argv[1])).get("result",{}).get("runs",[])))' "$L/run-list.json" 2>/dev/null || echo 0)
log "run/list desta sessão: $n run(s) ; CR1=$s1 CR2=$s2"
cp "$PROBE_LOG" "$L/probe.jsonl" 2>/dev/null || true

state_keys "$L/state-keys-final.txt"
log "store no fim (S3):"; cat "$L/state-keys-final.txt" | tee -a "$CLIENT_LOG"
RUN_IDS_IN_STORE=$(grep -Eo '^run/[a-f0-9]{16,}/' "$L/state-keys-final.txt" | sed 's|^run/||;s|/$||' | sort -u)
NDISTINCT=$(printf '%s\n' "$RUN_IDS_IN_STORE" | grep -c . || echo 0)
log "run ids distintos no store: $NDISTINCT"
printf '%s\n' "$RUN_IDS_IN_STORE" | tee -a "$CLIENT_LOG"

stop_server "$SRV2"; SRV2=""

########################################################################
# veredito
########################################################################
fails=()
grep -q '"key":"session/' "$L/probe.jsonl"                 || fails+=("probe.jsonl sem chave session/*")
grep -Eq '"key":"run/[a-f0-9]+/' "$L/probe.jsonl"          || fails+=("probe.jsonl sem chave run/<id>/*")
grep -q "^session/" "$L/state-keys.txt"                    || fails+=("store (S3) sem session/")
grep -Eq "^run/${RID}/" "$L/state-keys.txt"                || fails+=("store (S3) sem run/$RID/ (checkpoint per_step)")
[[ "$RESUME_ST" == "completed" ]]                          || fails+=("run/resume não completou (state=$RESUME_ST)")
[[ "$RECLAIM_STATE" == "failed" ]]                         || fails+=("run #2 pós-restart state=$RECLAIM_STATE != failed")
[[ "$RECLAIM_RESUMABLE" == "True" || "$RECLAIM_RESUMABLE" == "true" ]] || fails+=("run #2 pós-restart sem resumable:true")
[[ "${n:-0}" -ge 2 ]]                                      || fails+=("run/list retornou $n run(s) (< 2)")
[[ "${NDISTINCT:-0}" -ge 3 ]]                              || fails+=("apenas $NDISTINCT run ids distintos no store (esperado >= 3)")

if [[ ${#fails[@]} -eq 0 ]]; then
  ok "estado durável de mhl serve no S3 (session/* + run/<id>/*); resume ok; reclaim pós-restart do bucket; runs concorrentes com chaves disjuntas"
else
  for f in "${fails[@]}"; do log "  ! $f"; done
  die "veja as verificações acima"
fi
