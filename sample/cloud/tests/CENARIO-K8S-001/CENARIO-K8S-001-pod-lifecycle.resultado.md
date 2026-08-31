# CENÁRIO-K8S-001 — Resultado da Execução

> Registro gerado pelo agente `tester-mcp-server`.
> O arquivo original [`CENARIO-K8S-001-pod-lifecycle.md`](CENARIO-K8S-001-pod-lifecycle.md) não foi alterado.

## Resultado

**Resultado Real:**
- [ ] funcionou
- [x] **não funcionou** — o pod sobe e as probes/sinais funcionam, mas o **drain gracioso não segura a run em andamento** (achado no servidor MCP)

**Executado em:** 2026-08-31 10:45 -03:00
**Cluster:** `docker-desktop` (Kubernetes v1.34.1, nó único) — o `minikube` continua quebrado; o usuário habilitou o Kubernetes do Docker Desktop.

---

## Achado 1 — imagem distroless não roda os workflows (corrigido durante o teste)

Primeira execução: `run/start SlowBuild` retornou `state: working`, mas o log mostrou
`"msg":"run failed"..."durationMs":1,"steps":1` — a run morreu em 1 ms no passo `Compile`.

**Causa:** `SlowBuild` (todos os passos) e `DocPipeline.Draft` fazem
`cmd.exec(["sh","-c", ...])`. A base `gcr.io/distroless/static` **não tem `/bin/sh`**,
então o `exec` falha na hora.

**Correção aplicada** (em `sample/cloud/Dockerfile` — arquivo de empacotamento, não
fonte do runtime/módulo/servidor): base trocada para `alpine:3.21` (tem `/bin/sh`),
mantendo usuário não-root (uid 65532), `readOnlyRootFilesystem` compatível (escritas em
`/state` e `/tmp`). Imagem final **~26 MB**. `k8s/README.md` atualizado.

Depois disso os workflows executam dentro do pod (log: `step: Compile`).

## Achado 2 — `--drain-timeout` não segura a run no SIGTERM (NÃO corrigível aqui)

Segunda execução, linha do tempo do `kubectl logs` do pod drenado
([`logs/drained-pod.log`](logs/drained-pod.log)):

```
13:45:49.282  run started        tool=SlowBuild
13:45:49.xxx  step: Compile      (cmd.exec ["sh","-c","sleep 3; ..."])
13:45:51.674  draining           timeout=30s  working=1
13:45:51.675  run canceled       durationMs=2393  steps=1      ← 0,8 ms depois do "draining"
```

A run foi **cancelada imediatamente** ao começar o dreno — não nos 30 s de
`MHL_SERVE_DRAIN_TIMEOUT`. Não há o warning `drain deadline reached — cancelling runs`,
então não foi o deadline: a run já estava sendo derrubada por **propagação de contexto**.

**Causa (código):** `internal/mcpserver/http.go:223`

```go
runsCtx, runsCancel := context.WithCancel(ctx)   // ctx = contexto de sinal (SIGINT/SIGTERM)
```

`runsCtx` é **filho** do contexto de sinal. Quando o SIGTERM chega, `ctx` é cancelado e
o cancelamento **propaga** para `runsCtx` → para o contexto de cada run
(`context.WithCancel(h.runsCtx)` em `runs.go`) → a run é abortada na hora. O laço de
espera de `drain()` (`for time.Now().Before(deadline) { if workingRuns()==0 break; sleep 200ms }`)
e o próprio `--drain-timeout` ficam sem efeito prático.

O comentário do código (http.go:163-167) descreve a intenção **oposta**:
> "runsCtx is a child of baseCtx that only the drain path cancels: async runs
> descend from it, so --drain-timeout can let them finish past the signal."

**Correção provável (fora do escopo deste teste):** derivar `runsCtx` de um pai que
não herda o cancelamento do sinal, p.ex. `context.WithCancel(context.WithoutCancel(ctx))`
(Go 1.21+), mantendo `runsCancel` como o único gatilho.

**Impacto:** rotação de pod (deploy, HPA scale-in, node drain) mata builds de
documentação em andamento — exatamente o risco do item 07 do design
(`mhl-eks-design.html`), que se supunha mitigado por `--drain-timeout`. Só sobrevive
o workflow que declara `checkpoint { strategy: "per_step" }` (ex. `DocPipeline`);
`SlowBuild` (só `checkpoint { enabled: true }`) é retomável do último passo, mas a
run corrente é perdida.

**Cross-check:** o CENÁRIO-009 (host), se executado neste build, falha nas mesmas
asserções ("o processo espera a run terminar", "`run completed`/`run failed` após a
linha `draining`"). É o mesmo defeito, visível sem Kubernetes.

---

## O que PASSOU (plumbing do pod)

| Verificação | Observado |
|---|---|
| Rollout / pod `Ready` | ✔ `deployment "mhl-serve" successfully rolled out` |
| IP do pod nos Endpoints do Service (readiness gate) | ✔ `Endpoints antes: [10.1.0.8]` |
| SIGTERM → IP sai dos Endpoints | ✔ na 1ª sondagem (≤ 1 s) — readiness virou 503 |
| SIGTERM chega ao `mhl` como **PID 1** | ✔ linha JSON `"msg":"draining","timeout":"30s"` emitida |
| `/healthz` `/readyz` `/metrics` sem bearer | ✔ 200 / 200 / 200 |
| `POST /mcp` sem bearer | ✔ 401 |
| `initialize` + `tools/list` via port-forward | ✔ `serverInfo.name=mhl` ; tools `[DocPipeline, SlowBuild]` |
| Deployment recria o pod | ✔ novo pod `Running`, `restartCount=0`, `availableReplicas=1` |
| Linhas JSON de ciclo de vida (`run started` / `run canceled`) com `runId` + `owner` | ✔ |

## O que FALHOU

| Verificação | Esperado | Observado |
|---|---|---|
| Run em andamento sobrevive ao início do dreno | termina sozinha em ≤ 30 s (`run completed`/`failed`) | `run canceled` em ~1 ms |
| Processo espera a run antes de encerrar | delete→sumiço ≥ ~5 s | pod sumiu em **2 s** |

## Evidências

- [x] [`logs/drained-pod.log`](logs/drained-pod.log) — timeline `run started` → `draining` → `run canceled`
- [x] [`logs/client.log`](logs/client.log) — trilha completa (probes, Endpoints, run/start, delete, recriação)
- [x] [`logs/endpoints-during-drain.txt`](logs/endpoints-during-drain.txt), [`logs/pods-after.txt`](logs/pods-after.txt), [`logs/pod-initial.yaml`](logs/pod-initial.yaml)
- [x] [`logs/initialize.json`](logs/initialize.json), [`logs/tools-list.json`](logs/tools-list.json), [`logs/run-start.json`](logs/run-start.json)
- [x] [`logs/docker-build.log`](logs/docker-build.log), [`logs/environment-diagnostics.txt`](logs/environment-diagnostics.txt) (tentativa com minikube)

## Reproduzir

```sh
kubectl config use-context docker-desktop
cd sample/cloud/tests/CENARIO-K8S-001 && SKIP_BUILD=1 ./run.sh   # a imagem alpine já está construída
```

O `FAIL` é o resultado correto enquanto o Achado 2 não for corrigido no
`internal/mcpserver`.
