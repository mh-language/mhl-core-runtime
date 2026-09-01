#!/usr/bin/env bash
# CENARIO-K8S-001 — Ciclo de vida do pod (probes, SIGTERM/drain, logs)
#
# Constroi a imagem mhl-serve:local, aplica tests/cloud/k8s/, e verifica:
#  - rollout / pod Ready / IP nos Endpoints do Service
#  - POST /mcp exige bearer ; /healthz /readyz /metrics livres
#  - initialize + tools/list via port-forward
#  - kubectl delete pod --grace-period=40 durante uma run SlowBuild:
#      IP sai dos Endpoints ; logs tem "draining" (timeout 30s) ;
#      o container so encerra apos a run terminar (>=~5s, <=~40s) ;
#      logs tem "run started" e "run completed"/"run failed" (runId+owner)
#  - Deployment recria o pod, availableReplicas=1, restartCount=0
#
# Pre-req: docker, kubectl, um cluster local. Vars: SKIP_BUILD=1, KEEP=1.

set -u

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "$HERE/../../../.." && pwd)"     # .../mhl.lang.nosync
K8S_DIR="$REPO_ROOT/tests/cloud/k8s"
NS="mhl-serve"
IMG="mhl-serve:local"
LPORT="8791"
BASE="http://127.0.0.1:$LPORT"

L="$HERE/logs"; mkdir -p "$L"
CLIENT_LOG="$L/client.log"; : > "$CLIENT_LOG"
log() { echo "[$(date '+%H:%M:%S')] $*" | tee -a "$CLIENT_LOG"; }
fail() { log "RESULTADO: NÃO FUNCIONOU — $*"; echo "FAIL"; exit 1; }

PF_PID=""
cleanup() {
  [[ -n "$PF_PID" ]] && kill "$PF_PID" 2>/dev/null
  if [[ "${KEEP:-0}" != "1" ]]; then
    log "teardown: kubectl delete namespace $NS"
    kubectl delete namespace "$NS" --wait=false >/dev/null 2>&1 || true
  else
    log "KEEP=1 — namespace $NS preservado"
  fi
}
trap cleanup EXIT

command -v kubectl >/dev/null || fail "kubectl não encontrado no PATH"
command -v docker  >/dev/null || fail "docker não encontrado no PATH"
kubectl cluster-info >/dev/null 2>&1 || fail "nenhum cluster kubernetes acessível (kubectl cluster-info)"
CTX="$(kubectl config current-context 2>/dev/null)"
log "contexto kubectl: $CTX"

jget() { python3 -c 'import json,sys
try: d=json.load(open(sys.argv[1]))
except Exception: print(""); sys.exit(0)
for p in sys.argv[2].split("."):
  d = d.get(p, {}) if isinstance(d, dict) else {}
print("" if d == {} else d)' "$1" "$2"; }

########################################################################
# 1. imagem
########################################################################
if [[ "${SKIP_BUILD:-0}" == "1" ]]; then
  log "SKIP_BUILD=1 — usando a imagem $IMG existente"
else
  log "docker build -f tests/cloud/Dockerfile -t $IMG . (a partir de $REPO_ROOT)"
  ( cd "$REPO_ROOT" && docker build -f tests/cloud/Dockerfile -t "$IMG" . ) >"$L/docker-build.log" 2>&1 \
    || { tail -30 "$L/docker-build.log" | tee -a "$CLIENT_LOG"; fail "docker build falhou (ver logs/docker-build.log)"; }
  log "imagem construída"
fi
if echo "$CTX" | grep -qi minikube; then
  log "contexto minikube — minikube image load $IMG"
  minikube image load "$IMG" >>"$L/client.log" 2>&1 || fail "minikube image load falhou"
fi

########################################################################
# 2. aplicar manifestos + secret com token aleatório
########################################################################
TOKEN="$(openssl rand -hex 24 2>/dev/null || head -c24 /dev/urandom | xxd -p | tr -d '\n')"
log "aplicando namespace/service/deployment e Secret (token aleatório)"
kubectl apply -f "$K8S_DIR/namespace.yaml" >>"$CLIENT_LOG" 2>&1 || fail "apply namespace"
kubectl -n "$NS" create secret generic mhl-serve-token \
  --from-literal=token="$TOKEN" --dry-run=client -o yaml | kubectl apply -f - >>"$CLIENT_LOG" 2>&1 \
  || fail "apply secret"
kubectl apply -f "$K8S_DIR/service.yaml" -f "$K8S_DIR/deployment.yaml" >>"$CLIENT_LOG" 2>&1 || fail "apply service/deployment"

log "aguardando rollout (até 150s)..."
kubectl -n "$NS" rollout status deploy/mhl-serve --timeout=150s >>"$CLIENT_LOG" 2>&1 \
  || { kubectl -n "$NS" get pods -o wide | tee -a "$CLIENT_LOG"; kubectl -n "$NS" describe deploy/mhl-serve | tail -40 | tee -a "$CLIENT_LOG"; fail "rollout não concluiu"; }

POD="$(kubectl -n "$NS" get pod -l app.kubernetes.io/name=mhl-serve -o jsonpath='{.items[0].metadata.name}')"
POD_IP="$(kubectl -n "$NS" get pod "$POD" -o jsonpath='{.status.podIP}')"
log "pod=$POD ip=$POD_IP"
kubectl -n "$NS" get pod "$POD" -o yaml > "$L/pod-initial.yaml"

# Endpoints contém o IP do pod (readiness gate funcionando)
EP_BEFORE="$(kubectl -n "$NS" get endpoints mhl-serve -o jsonpath='{.subsets[*].addresses[*].ip}')"
log "Endpoints antes: [$EP_BEFORE]"
echo "$EP_BEFORE" | grep -qw "$POD_IP" || fail "IP do pod ($POD_IP) não está nos Endpoints do Service"

########################################################################
# 3. port-forward + smoke MCP
########################################################################
kubectl -n "$NS" port-forward svc/mhl-serve "$LPORT:8711" >"$L/port-forward.log" 2>&1 &
PF_PID=$!
ok=""
for i in $(seq 1 40); do
  kill -0 "$PF_PID" 2>/dev/null || break
  [[ "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/healthz" 2>/dev/null)" == "200" ]] && { ok=1; break; }
  sleep 0.3
done
[[ -n "$ok" ]] || fail "port-forward/healthz não respondeu 200"

HZ=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/healthz")
RZ=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/readyz")
MZ=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/metrics")
NOAUTH=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/mcp" -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{}}}')
log "probes: /healthz=$HZ /readyz=$RZ /metrics=$MZ ; POST /mcp sem token=$NOAUTH"

curl -s -D "$L/init-headers.txt" -o "$L/initialize.json" -X POST "$BASE/mcp" \
  -H 'content-type: application/json' -H "Authorization: Bearer $TOKEN" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{}}}'
SID=$(awk 'tolower($1)=="mcp-session-id:"{print $2}' "$L/init-headers.txt" | tr -d '\r')
SRVNAME=$(jget "$L/initialize.json" result.serverInfo.name)
curl -s -o "$L/tools-list.json" -X POST "$BASE/mcp" -H 'content-type: application/json' \
  -H "Authorization: Bearer $TOKEN" -H "Mcp-Session-Id: $SID" \
  -d '{"jsonrpc":"2.0","id":2,"method":"tools/list"}'
TOOLS=$(python3 -c 'import json,sys;print(",".join(t["name"] for t in json.load(open(sys.argv[1]))["result"]["tools"]))' "$L/tools-list.json" 2>/dev/null)
log "initialize serverInfo.name=$SRVNAME ; tools/list=[$TOOLS]"

########################################################################
# 4. drain: run SlowBuild + kubectl delete pod --grace-period=40
########################################################################
curl -s -o "$L/run-start.json" -X POST "$BASE/mcp" -H 'content-type: application/json' \
  -H "Authorization: Bearer $TOKEN" -H "Mcp-Session-Id: $SID" \
  -d '{"jsonrpc":"2.0","id":3,"method":"run/start","params":{"name":"SlowBuild","arguments":{"target":"k8s-drain"}}}'
RID=$(jget "$L/run-start.json" result.runId)
log "run/start SlowBuild -> runId=$RID state=$(jget "$L/run-start.json" result.state)"
[[ -n "$RID" ]] || { cat "$L/run-start.json" | tee -a "$CLIENT_LOG"; fail "run/start não retornou runId"; }

sleep 1.5
log "kubectl delete pod $POD --grace-period=40 (assíncrono)"
T0=$(date +%s)
kubectl -n "$NS" delete pod "$POD" --grace-period=40 --wait=false >>"$CLIENT_LOG" 2>&1

# captura contínua dos logs do pod que está terminando
( for i in $(seq 1 60); do
    kubectl -n "$NS" logs "$POD" > "$L/drained-pod.log" 2>/dev/null || break
    sleep 1
  done ) &
LOGCAP_PID=$!

# Endpoints devem perder o IP do pod em poucos segundos
EP_DROPPED=""
for i in $(seq 1 20); do
  ep="$(kubectl -n "$NS" get endpoints mhl-serve -o jsonpath='{.subsets[*].addresses[*].ip}' 2>/dev/null)"
  echo "[$i] endpoints=[$ep]" >> "$L/endpoints-during-drain.txt"
  echo "$ep" | grep -qw "$POD_IP" || { EP_DROPPED="$i"; break; }
  sleep 1
done
log "IP do pod saiu dos Endpoints na sondagem #${EP_DROPPED:-<não saiu em 20s>}"

# espera o pod sumir de fato
kubectl -n "$NS" wait --for=delete "pod/$POD" --timeout=60s >>"$CLIENT_LOG" 2>&1
T1=$(date +%s)
DRAIN_ELAPSED=$((T1 - T0))
kill "$LOGCAP_PID" 2>/dev/null
log "pod removido ${DRAIN_ELAPSED}s após o delete"
log "----- logs do pod drenado (fim) -----"; tail -25 "$L/drained-pod.log" | tee -a "$CLIENT_LOG" 2>/dev/null || true

########################################################################
# 5. novo pod / recuperação do Deployment
########################################################################
kubectl -n "$NS" rollout status deploy/mhl-serve --timeout=120s >>"$CLIENT_LOG" 2>&1 || true
NEWPOD="$(kubectl -n "$NS" get pod -l app.kubernetes.io/name=mhl-serve -o jsonpath='{.items[0].metadata.name}')"
NEW_PHASE="$(kubectl -n "$NS" get pod "$NEWPOD" -o jsonpath='{.status.phase}')"
NEW_RESTARTS="$(kubectl -n "$NS" get pod "$NEWPOD" -o jsonpath='{.status.containerStatuses[0].restartCount}')"
AVAIL="$(kubectl -n "$NS" get deploy/mhl-serve -o jsonpath='{.status.availableReplicas}')"
log "novo pod=$NEWPOD phase=$NEW_PHASE restartCount=$NEW_RESTARTS ; deploy availableReplicas=$AVAIL"
kubectl -n "$NS" get pods -o wide > "$L/pods-after.txt" 2>&1

########################################################################
# verdite
########################################################################
DRAINING_LINE=$(grep '"msg":"draining"' "$L/drained-pod.log" 2>/dev/null | head -1)
RUN_STARTED=$(grep '"msg":"run started"' "$L/drained-pod.log" 2>/dev/null | head -1)
RUN_DONE=$(grep -E '"msg":"run (completed|failed)"' "$L/drained-pod.log" 2>/dev/null | head -1)

ok="yes"
[[ "$HZ" == "200" && "$RZ" == "200" && "$MZ" == "200" ]] || { ok="no"; log "  ! probes/metrics sem auth != 200 ($HZ/$RZ/$MZ)"; }
[[ "$NOAUTH" == "401" ]]              || { ok="no"; log "  ! POST /mcp sem token != 401 ($NOAUTH)"; }
[[ "$SRVNAME" == "mhl" ]]            || { ok="no"; log "  ! initialize serverInfo.name != mhl ($SRVNAME)"; }
echo "$TOOLS" | grep -q "DocPipeline" || { ok="no"; log "  ! tools/list sem DocPipeline"; }
echo "$TOOLS" | grep -q "SlowBuild"   || { ok="no"; log "  ! tools/list sem SlowBuild"; }
[[ -n "$EP_DROPPED" ]]               || { ok="no"; log "  ! IP do pod não saiu dos Endpoints durante o drain"; }
[[ -n "$DRAINING_LINE" ]]            || { ok="no"; log "  ! logs do pod sem linha JSON 'draining'"; }
echo "$DRAINING_LINE" | grep -q '"timeout":"30s"' || { ok="no"; log "  ! linha 'draining' sem timeout 30s"; }
[[ -n "$RUN_STARTED" ]]              || { ok="no"; log "  ! logs sem 'run started'"; }
[[ -n "$RUN_DONE" ]]                 || { ok="no"; log "  ! logs sem 'run completed'/'run failed' (a run não terminou no drain?)"; }
echo "$RUN_STARTED$RUN_DONE" | grep -q '"runId"' || { ok="no"; log "  ! eventos de ciclo de vida sem runId"; }
echo "$RUN_STARTED$RUN_DONE" | grep -q '"owner"' || { ok="no"; log "  ! eventos de ciclo de vida sem owner"; }
[[ "$DRAIN_ELAPSED" -ge 4 ]]         || { ok="no"; log "  ! pod sumiu em ${DRAIN_ELAPSED}s (<4s): não esperou a run"; }
[[ "$DRAIN_ELAPSED" -le 40 ]]        || { ok="no"; log "  ! pod levou ${DRAIN_ELAPSED}s (>40s): provável SIGKILL, SIGTERM não drenou"; }
[[ "$NEW_PHASE" == "Running" ]]      || { ok="no"; log "  ! novo pod phase=$NEW_PHASE != Running"; }
[[ "${NEW_RESTARTS:-x}" == "0" ]]    || { ok="no"; log "  ! novo pod restartCount=$NEW_RESTARTS != 0"; }
[[ "${AVAIL:-0}" == "1" ]]           || { ok="no"; log "  ! deploy availableReplicas=$AVAIL != 1"; }

if [[ "$ok" == "yes" ]]; then
  log "RESULTADO: FUNCIONOU — probes ligadas ao Service, SIGTERM drenou como PID 1, logs de ciclo de vida, pod recriado limpo"
  echo "PASS"; exit 0
else
  log "RESULTADO: NÃO FUNCIONOU — veja as verificações acima"
  echo "FAIL"; exit 1
fi
