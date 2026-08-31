# `mhl serve mcp --http` in a single Kubernetes pod

A minimal, **single-replica** deployment for a local cluster (Docker Desktop or
minikube). Scope is deliberately one pod: it exercises the Kubernetes-shaped
concerns the host `run.sh` scripts cannot — a real kubelet driving the
liveness/readiness probes, `SIGTERM` from `kubectl delete pod` reaching `mhl`
as PID 1, `terminationGracePeriodSeconds` vs `--drain-timeout`, config from an
env + `Secret`, `kubectl logs` carrying the JSON lifecycle lines.

It does **not** cover the infra around the pod (API Gateway, multi-replica,
shared store, HPA, PDB) — see `../mhl-eks-design.html`. More than one replica is
unsafe until a shared store lands (design gaps 1–2).

## Files

| File | Purpose |
|---|---|
| `namespace.yaml` | `mhl-serve` namespace |
| `secret.yaml` | `mhl-serve-token` — the gateway↔mhl shared bearer (`MHL_SERVE_TOKEN`); placeholder, override it |
| `service.yaml` | ClusterIP `mhl-serve:8711` |
| `deployment.yaml` | 1 replica, probes, `terminationGracePeriodSeconds: 40`, `--drain-timeout 30s`, `--max-concurrent-runs 4`, **state on an `emptyDir`** |
| `pvc.yaml` + `deployment-durable.yaml` | variant with `/state` on a PVC so a `runId` survives a pod restart |
| `kustomization.yaml` | applies namespace + secret + service + `deployment.yaml` |

`mhl` runs as `nonroot` (uid 65532) with `readOnlyRootFilesystem`; `/state` and
`/tmp` are the only writable mounts. The image base is `alpine` (not distroless):
the sample workflows shell out via `cmd.exec(["sh","-c",...])`, so `/bin/sh` must
be present.

## 1. Build the image

From the **repo root** (the build stage needs `src/mhl-runtime/`):

```sh
docker build -f sample/cloud/Dockerfile -t mhl-serve:local .
```

The multi-stage build cross-compiles for the daemon's architecture, so this is
`linux/arm64` on Apple Silicon with no host Go toolchain needed. (`make -C
src/mhl-runtime linux-arm64` is available if you want a raw binary to
`kubectl cp`.)

### Make the image visible to the cluster

- **Docker Desktop Kubernetes** — shares the Docker daemon; nothing to do.
- **minikube** — load it in:
  ```sh
  minikube image load mhl-serve:local
  ```
- Any cluster — `imagePullPolicy: IfNotPresent` + the tag `mhl-serve:local`
  (no registry prefix) means it never tries to pull.

## 2. Set the token, then apply

```sh
kubectl apply -f sample/cloud/k8s/namespace.yaml
kubectl -n mhl-serve create secret generic mhl-serve-token \
  --from-literal=token="$(openssl rand -hex 24)" \
  --dry-run=client -o yaml | kubectl apply -f -

kubectl apply -k sample/cloud/k8s          # service + deployment (skips secret.yaml if you kustomize-exclude it)
# or, plainly:
kubectl apply -f sample/cloud/k8s/service.yaml -f sample/cloud/k8s/deployment.yaml

kubectl -n mhl-serve rollout status deploy/mhl-serve --timeout=120s
```

> `kubectl apply -k` will also apply `secret.yaml` (the placeholder). Running the
> `create secret ... | apply` line afterwards overwrites it with a real value.

## 3. Reach it

```sh
kubectl -n mhl-serve port-forward svc/mhl-serve 8711:8711 &
BASE=http://127.0.0.1:8711
TOKEN=$(kubectl -n mhl-serve get secret mhl-serve-token -o jsonpath='{.data.token}' | base64 -d)

curl -s -o /dev/null -w '%{http_code}\n' $BASE/healthz       # 200
curl -s -o /dev/null -w '%{http_code}\n' $BASE/readyz        # 200
curl -s $BASE/metrics | head                                  # Prometheus text, no auth

curl -s -X POST $BASE/mcp -H 'content-type: application/json' -H "Authorization: Bearer $TOKEN" \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{}}}'
```

The host `run.sh` scenarios in `../tests/CENARIO-0*/` can be pointed at this
`BASE`/`TOKEN` instead of spawning their own server — but only the
Kubernetes-shaped ones gain anything (004 probes, 009 drain, 011 restart).
`../tests/CENARIO-K8S-001/` is the k8s-native version.

## 4. Drain behaviour (what the pod adds)

```sh
# start a genuinely-working run, then evict the pod
POD=$(kubectl -n mhl-serve get pod -l app.kubernetes.io/name=mhl-serve -o jsonpath='{.items[0].metadata.name}')
# ... run/start SlowBuild via $BASE ...
kubectl -n mhl-serve delete pod "$POD" --grace-period=40 &

kubectl -n mhl-serve get endpoints mhl-serve -w        # the pod IP drops immediately (readiness -> 503)
kubectl -n mhl-serve logs "$POD" | grep '"msg":"draining"'   # timeout":"30s"
kubectl -n mhl-serve logs "$POD" | grep '"msg":"run '        # the run finishes before the container exits
```

## 5. Durable variant (optional)

```sh
kubectl delete -n mhl-serve deploy/mhl-serve
kubectl apply -f sample/cloud/k8s/pvc.yaml -f sample/cloud/k8s/deployment-durable.yaml
kubectl -n mhl-serve rollout status deploy/mhl-serve --timeout=120s
# now a runId parked at a gate survives `kubectl delete pod` and can be run/resume'd
```

## Teardown

```sh
kubectl delete namespace mhl-serve
```
