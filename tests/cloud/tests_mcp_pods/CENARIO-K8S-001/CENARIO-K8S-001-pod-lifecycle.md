# Cenário K8S-001: Ciclo de vida do pod (probes, SIGTERM/drain, logs)

**Objetivo:** Rodar `mhl serve mcp --http` como um pod real num cluster local
(Docker Desktop ou minikube) e verificar as garantias que só aparecem sob um
kubelet de verdade: as probes de liveness/readiness ligadas ao Service, o
`SIGTERM` do `kubectl delete pod` chegando ao `mhl` como PID 1, a interação
`terminationGracePeriodSeconds` × `--drain-timeout`, e as linhas JSON de ciclo
de vida em `kubectl logs`.

```gherkin
Dado que a imagem mhl-serve:local foi construída e os manifestos de tests/cloud/k8s foram aplicados
Quando o Deployment (1 réplica) termina o rollout
Então o pod fica Ready e seu IP entra nos Endpoints do Service
E POST /mcp exige bearer (401 sem), enquanto /healthz /readyz /metrics respondem sem auth
Quando uma run SlowBuild está working e o pod é removido com kubectl delete pod --grace-period=40
Então o IP do pod sai dos Endpoints em poucos segundos (readiness -> 503)
E kubectl logs do pod mostra a linha JSON "draining" com timeout 30s
E o container só encerra depois que a run chega a um estado terminal (entre ~5s e ~40s)
E kubectl logs tem linhas JSON "run started" e "run completed"/"run failed" com runId e owner
E o Deployment recria o pod e volta a 1 réplica disponível, sem restart de container (restartCount 0)
```

**Resultado Esperado:**
- `kubectl rollout status` conclui; pod `Ready`; `Endpoints/mhl-serve` contém o IP do pod.
- `POST /mcp` sem `Authorization` → `401`; com o token do Secret → `200` com `serverInfo`;
  `tools/list` → `DocPipeline` e `SlowBuild`. `/healthz` `/readyz` `/metrics` → `200` sem auth.
- Após `kubectl delete pod`: o IP do pod some de `Endpoints` em ≤ ~15 s.
- `kubectl logs <pod>` (o que está terminando): tem `{"...","msg":"draining","timeout":"30s",...}`
  e, depois dela, `{"...","msg":"run completed"|"run failed",...,"runId":...,"owner":...}`.
- Tempo entre o `delete` e o pod sumir: **≥ ~5 s** (esperou a run) e **≤ ~40 s** (não levou SIGKILL).
- Novo pod: `Running`, `restartCount == 0`; Deployment `availableReplicas == 1`.

**Resultado Real:**
- [ ] funcionou
- [ ] não funcionou

### Evidências:

- [ ] `kubectl get pod/endpoints/deploy` antes, durante e depois
- [ ] `kubectl logs` do pod drenado (linha `draining` + término da run)
- [ ] Respostas MCP via `port-forward` (initialize, tools/list, 401 sem token)
- [ ] Tempo medido entre `delete pod` e o pod sumir

### Observações:
- Pré-requisitos: `docker`, `kubectl`, um cluster local acessível. Contexto atual:
  `kubectl config current-context`. Em minikube o script faz `minikube image load`.
- O script constrói `mhl-serve:local` (pule com `SKIP_BUILD=1`), aplica os manifestos,
  cria o Secret com um token aleatório, e ao final apaga o namespace (`KEEP=1` preserva).
- Escopo: **um pod**. Não cobre gateway, multi-réplica, store compartilhado, HPA.
- A variante durável (`pvc.yaml` + `deployment-durable.yaml`) para "runId sobrevive a
  restart de pod" fica para um K8S-002; aqui o estado é `emptyDir`.

**Executado em:** [Data e hora do teste]

## Como executar

```sh
./run.sh            # constrói a imagem, aplica, testa, e faz teardown
KEEP=1 ./run.sh     # não apaga o namespace no fim
SKIP_BUILD=1 ./run.sh
```

Artefatos em `./logs/`.
