# CENÁRIO-K8S-001 — Resultado da Execução

> Registro gerado pelo agente `tester-mcp-server`.
> O arquivo original [`CENARIO-K8S-001-pod-lifecycle.md`](CENARIO-K8S-001-pod-lifecycle.md) não foi alterado.

## Resultado

**Resultado Real:**
- [x] **funcionou** (após 2 correções — ver histórico abaixo)
- [ ] não funcionou

**Executado em:** 2026-08-31 11:48 -03:00 (execução final, `PASS`)
**Cluster:** `docker-desktop` (Kubernetes v1.34.1, nó único).

---

## Verificações da execução final (todas OK)

Timeline do `kubectl logs` do pod drenado ([`logs/drained-pod.log`](logs/drained-pod.log)):

```
14:48:21.315  run started        tool=SlowBuild
              step: Compile      (cmd.exec ["sh","-c","sleep 3; ..."])
14:48:23.690  draining           timeout=30s  working=1
              step: Package      ← a run CONTINUOU após o SIGTERM
              step: Ship
14:48:28.322  run failed         durationMs=7007  steps=3   ← terminou sozinha (timeout do Ship)
[11:48:28]    pod removido 6s após o delete                 ← o processo ESPEROU a run
```

| Verificação | Esperado | Observado |
|---|---|---|
| Rollout / pod `Ready` | ✔ | `deployment "mhl-serve" successfully rolled out` |
| IP do pod nos Endpoints do Service (readiness gate) | ✔ | `Endpoints antes: [10.1.0.12]` |
| SIGTERM → IP sai dos Endpoints | ≤ ~15 s | 1ª sondagem (`endpoints=[]`) — readiness virou 503 |
| SIGTERM chega ao `mhl` como **PID 1** | ✔ | linha JSON `"msg":"draining","timeout":"30s"` |
| **Run em andamento sobrevive ao dreno** | termina sozinha em ≤ 30 s | `step: Package` → `step: Ship` → `run failed` (7007 ms), **não** `run canceled` |
| **Processo espera a run antes de encerrar** | delete→sumiço ≥ ~5 s e ≤ ~40 s | **6 s** |
| `/healthz` `/readyz` `/metrics` sem bearer | 200 | 200 / 200 / 200 |
| `POST /mcp` sem bearer | 401 | 401 |
| `initialize` + `tools/list` via port-forward | `mhl`; `DocPipeline`,`SlowBuild` | ✔ |
| Deployment recria o pod | `Running`, `restartCount 0`, `availableReplicas 1` | ✔ (`mhl-serve-…-r2zxw`) |
| Linhas JSON de ciclo de vida com `runId` + `owner` | ✔ | `run started` / `run failed` |

`run.sh` → `PASS` (EXIT=0).

---

## Histórico — 2 achados corrigidos entre a 1ª e a execução final

### Achado 1 — imagem distroless não roda os workflows *(corrigido: base → alpine)*

1ª execução: `SlowBuild` morreu em 1 ms no `Compile` (`"run failed"..."durationMs":1`).
**Causa:** `SlowBuild` (todos os passos) e `DocPipeline.Draft` fazem
`cmd.exec(["sh","-c", ...])`; `gcr.io/distroless/static` **não tem `/bin/sh`**.
**Correção** (em `sample/cloud/Dockerfile` — empacotamento, não fonte do
runtime/módulo/servidor): base → `alpine:3.21`, mantendo `nonroot` (uid 65532) e
`readOnlyRootFilesystem` (escritas em `/state` e `/tmp`). Imagem ~26 MB.

### Achado 2 — `--drain-timeout` não segurava a run no SIGTERM *(corrigido no `internal/mcpserver`)*

2ª e 3ª execuções: `run canceled` ~0,8 ms depois de `draining` (`durationMs` ≈ 2,4 s,
ainda no `sleep 3` do `Compile`), sem o warning `drain deadline reached`.
**Causa:** `internal/mcpserver/http.go` criava `runsCtx` como filho do contexto de
sinal (`context.WithCancel(ctx)`), então o SIGTERM cancelava a run por propagação de
contexto antes de o dreno esperar — `--drain-timeout` virava no-op.
**Correção aplicada** (`http.go:223`):

```go
runsCtx, runsCancel := context.WithCancel(context.WithoutCancel(ctx))
```

`WithoutCancel` mantém os valores de `ctx` mas descarta a propagação do cancelamento,
deixando `runsCancel` (chamado só por `drain()`) como o único gatilho. Na execução
final, a run seguiu `Compile → Package → Ship` depois do SIGTERM e terminou no seu
próprio `timeout` do `Ship`; o processo só encerrou aí (6 s após o `delete`).

> O CENÁRIO-009 (host) exercita o mesmo caminho sem Kubernetes — vale re-rodá-lo
> para confirmar que o `PASS` também se sustenta lá.

## Evidências

- [x] [`logs/drained-pod.log`](logs/drained-pod.log) — `run started` → `draining` → `Package`/`Ship` → `run failed` (7007 ms)
- [x] [`logs/client.log`](logs/client.log) — trilha completa (probes, Endpoints, run/start, delete, 6 s, recriação)
- [x] [`logs/endpoints-during-drain.txt`](logs/endpoints-during-drain.txt) — `endpoints=[]` durante o dreno
- [x] [`logs/pods-after.txt`](logs/pods-after.txt) — novo pod `Running` / `RESTARTS 0`
- [x] [`logs/initialize.json`](logs/initialize.json), [`logs/tools-list.json`](logs/tools-list.json), [`logs/run-start.json`](logs/run-start.json)
- [x] [`logs/docker-build.log`](logs/docker-build.log), [`logs/environment-diagnostics.txt`](logs/environment-diagnostics.txt) (tentativa inicial com minikube)

## Reproduzir

```sh
kubectl config use-context docker-desktop
cd sample/cloud/tests/CENARIO-K8S-001 && ./run.sh          # reconstrói a imagem a partir do source atual
# ou SKIP_BUILD=1 ./run.sh para reaproveitar mhl-serve:local
```
