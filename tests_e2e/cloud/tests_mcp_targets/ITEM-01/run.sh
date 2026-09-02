#!/usr/bin/env bash
# ITEM-01 (alvo) — registro de run compartilhado + cancel distribuído
source "$(dirname "${BASH_SOURCE[0]}")/../_lib.sh"

STATE="$(mktmp)"
A="127.0.0.1:8761"; AB="http://$A"; B="127.0.0.1:8762"; BB="http://$B"
: > "$L/podA.log"; : > "$L/podB.log"
AP=$(boot "$A" "$AB" "$L/podA.log" "$STATE") && track "$AP" || { log "FAIL: Pod A não subiu"; echo FAIL; exit 1; }
BP=$(boot "$B" "$BB" "$L/podB.log" "$STATE") && track "$BP" || { log "FAIL: Pod B não subiu"; echo FAIL; exit 1; }
ASID=$(initsid "$AB"); BSID=$(initsid "$BB")
log "Pod A=$AP ($A) Pod B=$BP ($B) --state-dir compartilhado=$STATE"

# run/start SlowBuild no Pod A (executa ~9s: Compile 3s + Package 3s + Ship 1s(timeout))
rpc "$AB" "$ASID" '{"jsonrpc":"2.0","id":2,"method":"run/start","params":{"name":"SlowBuild","arguments":{"target":"x"}}}' "$L/A-start.json"
RID=$(jget "$L/A-start.json" result.runId)
[[ -n "$RID" ]] || { log "FAIL: sem runId"; cat "$L/A-start.json" >>"$CLIENT_LOG"; echo FAIL; exit 1; }
log "Pod A run/start SlowBuild -> $RID"

# ---- alvo 1: run/status no Pod B mostra working + step que avança ----
seen_steps=""
b_state_first=""
for i in 1 2 3 4 5; do
  sleep 1
  rpc "$BB" "$BSID" '{"jsonrpc":"2.0","id":3,"method":"run/status","params":{"runId":"'"$RID"'"}}' "$L/B-status-progress.json"
  st=$(jget "$L/B-status-progress.json" result.state)
  sp=$(jget "$L/B-status-progress.json" result.step)
  ec=$(jget "$L/B-status-progress.json" error.code)
  [[ -z "$b_state_first" ]] && b_state_first="${st:-err:$ec}"
  [[ -n "$sp" ]] && case " $seen_steps " in *" $sp "*) ;; *) seen_steps="$seen_steps $sp";; esac
  log "  B run/status #$i: state='$st' step='$sp' err='$ec'"
done
seen_steps="${seen_steps# }"
n_steps=$(echo "$seen_steps" | wc -w | tr -d ' ')
if [[ "$b_state_first" != "working" ]]; then
  need "run/status de uma run 'working' iniciada noutro pod deve devolver state=working (hoje: '$b_state_first')"
fi
if [[ "${n_steps:-0}" -lt 2 ]]; then
  need "run/status noutro pod deve mostrar o progresso live (step avançando); visto: [$seen_steps]"
fi

# ---- alvo 2: run/cancel no Pod B para a goroutine do Pod A ----
# reinicia a run p/ ter tempo de cancelar
rpc "$AB" "$ASID" '{"jsonrpc":"2.0","id":4,"method":"run/start","params":{"name":"SlowBuild","arguments":{"target":"y"}}}' "$L/A-start2.json"
RID2=$(jget "$L/A-start2.json" result.runId)
sleep 1
rpc "$BB" "$BSID" '{"jsonrpc":"2.0","id":5,"method":"run/cancel","params":{"runId":"'"$RID2"'"}}' "$L/B-cancel.json"
CC_ERR=$(jget "$L/B-cancel.json" error.code)
log "  B run/cancel: error='$CC_ERR' result.state='$(jget "$L/B-cancel.json" result.state)'"
final=""
for _ in $(seq 1 20); do
  rpc "$AB" "$ASID" '{"jsonrpc":"2.0","id":6,"method":"run/status","params":{"runId":"'"$RID2"'"}}' "$L/A-status-after-cancel.json"
  s=$(jget "$L/A-status-after-cancel.json" result.state); [[ "$s" == canceled || "$s" == completed || "$s" == failed ]] && { final="$s"; break; }; sleep 1
done
log "  A run/status pós-cancel-do-B: '$final'"
if [[ -n "$CC_ERR" ]]; then
  need "run/cancel noutro pod não pode devolver erro '$CC_ERR' — deve alcançar a run"
fi
if [[ "$final" != "canceled" ]]; then
  need "run/cancel do Pod B deve parar a goroutine do Pod A (state=canceled); hoje a run termina como '$final'"
fi

verdict "registro live entre pods + cancel distribuído funcionam"
