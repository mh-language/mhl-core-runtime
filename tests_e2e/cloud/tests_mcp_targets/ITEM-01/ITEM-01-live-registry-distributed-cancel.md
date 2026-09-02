# ITEM-01 (alvo): registro de run compartilhado + cancel distribuído

> **Escopo: distribuição.** Coordenar uma run que está executando numa réplica a
> partir de outra — ver o progresso e pedir cancelamento. Não é autorização: o
> `run/cancel` já é gateado por *posse* (o principal dono); o que falta é o
> sinal alcançar a goroutine no pod certo.

**Item do design:** 01 — *Async run state is node-local* (parcial).
**Estado hoje:** o estado durável (checkpoint + owner) já cruza entre pods via
`extension store`; o **registro live** (`h.runs`, a goroutine, o `cancel`) é por
pod.

## Comportamento-alvo

Com duas réplicas (`Pod A`, `Pod B`) compartilhando estado
(`--state-dir` comum ou `extension store`):

```gherkin
Dado run/start SlowBuild no Pod A (executando, ~9s)
Quando run/status {runId} no Pod B, repetidamente
Então o Pod B reporta state="working" e o `step` AVANÇA (Compile -> Package -> Ship)
  — progresso live, não só o último checkpoint commitado

Quando run/cancel {runId} no Pod B enquanto a run executa no Pod A
Então em no máximo um passo a run alcança state="canceled"
E ela NÃO chega a "completed"/"failed" por conta própria
  — o cancel do Pod B parou a goroutine do Pod A
```

## Critério de aceite (o que faz este cenário virar MET)

1. `run/status` de uma run `working` iniciada noutro pod devolve `working` **com
   o step corrente** (não `-32602`, não um estado congelado).
2. `run/cancel` noutro pod resulta em `state=canceled` na run, observado de
   qualquer pod, dentro de ~1 passo.
3. A run cancelada **não** transita para `completed`/`failed` sozinha.

## Como implementar (pistas)

- `internal/mcpserver/runs.go`: `h.runs` / `reconstructRun` — um `StateStore`
  (list/get/put/claim/watch) com o registro de runs, não só os checkpoints; ou
  um índice de runs no `extension store`.
- Cancel distribuído: o pod dono grava/lê um flag `cancelRequested` no store a
  cada *step boundary* (`interpreter.RunStep` já recebe o `ctx`); ou afinidade
  explícita de run no ingress.
- Progresso live: publicar `currentStep` no store por passo (barato) para o
  `run/status` de outro pod ler.

## Evidências (logs/)
- `podA.log`, `podB.log`
- `B-status-progress.json` (deve mostrar o step avançando)
- `B-cancel.json`, `A-status-after-cancel.json`

**Verificado por:** `./run.sh` — hoje **PENDING**.
