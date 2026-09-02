#!/usr/bin/env bash
# ITEM-04 (alvo) — tools/call longo promovido para run assíncrona
source "$(dirname "${BASH_SOURCE[0]}")/../../_lib.sh"

STATE="$(mktmp)"
ADDR="127.0.0.1:8767"; BASE="http://$ADDR"

# alvo 1: o servidor aceita uma flag de limiar (nome flexível)
FLAG=""; FLAG_OUT="$L/promote-flag.out"; : > "$FLAG_OUT"
for f in --tools-call-timeout --promote-after --tools-call-promote-after; do
  "$MHL" serve mcp --http --addr "$ADDR" --token "$TOKEN" --state-dir "$STATE" "$f" 5s "$WORKFLOWS" >>"$FLAG_OUT" 2>&1 &
  pid=$!; ok=""
  for _ in $(seq 1 20); do
    kill -0 "$pid" 2>/dev/null || break
    [[ "$("${CURL[@]}" -o /dev/null -w '%{http_code}' "$BASE/healthz" 2>/dev/null)" == "200" ]] && { ok=1; break; }
    sleep 0.2
  done
  if [[ -n "$ok" ]]; then FLAG="$f"; PID=$pid; track "$PID"; break; fi
  kill -KILL "$pid" 2>/dev/null; wait "$pid" 2>/dev/null
done

if [[ -z "$FLAG" ]]; then
  need "o servidor deve aceitar uma flag de limiar (--tools-call-timeout / --promote-after) OU promover automaticamente"
  # sobe sem flag p/ ao menos medir o comportamento atual (auto-promote embutido?)
  PID=$(boot "$ADDR" "$BASE" "$L/mcp-server.log" "$STATE") && track "$PID" || { log "FAIL: servidor não subiu"; echo FAIL; exit 1; }
  THRESH_S=999
else
  log "servidor aceitou a flag: $FLAG 5s"
  THRESH_S=5
fi

SID=$(initsid "$BASE")

# alvo 2: tools/call SlowBuild volta em ~limiar com um runId
t0=$(date +%s)
"${CURL[@]}" -o "$L/tools-call.json" -X POST "$BASE/mcp" -H 'content-type: application/json' \
  -H "Authorization: Bearer $TOKEN" -H "Mcp-Session-Id: $SID" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"SlowBuild","arguments":{"target":"x"}}}'
t1=$(date +%s); TC=$((t1-t0))
python3 -m json.tool "$L/tools-call.json" 2>/dev/null | tee -a "$CLIENT_LOG" >/dev/null
RID=$(python3 -c '
import json,sys,re
try: d=json.load(open(sys.argv[1]))
except Exception: print(""); sys.exit()
blob=json.dumps(d)
m=re.search(r"[0-9a-f]{16,64}", blob)
# procura por uma chave runId em qualquer nível
def find(o):
  if isinstance(o,dict):
    for k,v in o.items():
      if k.lower()=="runid" and isinstance(v,str): return v
      r=find(v)
      if r: return r
  if isinstance(o,list):
    for v in o:
      r=find(v)
      if r: return r
  return ""
print(find(d) or "")' "$L/tools-call.json")
log "tools/call SlowBuild: ${TC}s ; runId na resposta = '${RID:-<nenhum>}'"

[[ "$TC" -le $((THRESH_S + 3)) ]] || need "tools/call deve retornar em ~limiar (${THRESH_S}s), não pela duração inteira (levou ${TC}s)"
[[ -n "$RID" ]] || need "a resposta de tools/call promovido deve conter um runId recuperável (structuredContent/_meta/content)"

# alvo 3: run/status do runId promovido funciona
if [[ -n "$RID" ]]; then
  rpc "$BASE" "$SID" '{"jsonrpc":"2.0","id":3,"method":"run/status","params":{"runId":"'"$RID"'"}}' "$L/run-status.json"
  RS=$(jget "$L/run-status.json" result.state); RSE=$(jget "$L/run-status.json" error.code)
  log "run/status {$RID}: state='$RS' err='$RSE'"
  [[ -z "$RSE" ]] || need "run/status do runId promovido não deve dar erro ('$RSE')"
fi

verdict "tools/call longo é promovido: volta em ~limiar com runId, run/status funciona"
