# Cenário 009: Drain gracioso no SIGTERM e checkpoint de shutdown

**Objetivo:** Verificar que, ao receber `SIGTERM`, o pod para de aceitar trabalho
novo mas deixa as runs em andamento terminarem dentro de `--drain-timeout`, que
`/readyz` sinaliza `503` durante o dreno, e que uma run interrompida deixa um
ponto de retomada mesmo sem `checkpoint { strategy: "per_step" }`.

```gherkin
Dado que o servidor MCP está em execução com --drain-timeout 20s e --state-dir
E há uma run SlowBuild em andamento
Quando o processo recebe SIGTERM
Então GET /readyz passa a responder 503 e GET /healthz continua 200
E run/start passa a responder -32000 "server is draining — not accepting new runs"
E o processo só encerra depois que a run em andamento chega a um estado terminal
E o stderr registra uma linha JSON "draining" com o timeout configurado
E um servidor reiniciado no mesmo --state-dir consegue run/status desse runId como resumable
E com --drain-timeout 0 o SIGTERM cancela a run imediatamente
```

**Resultado Esperado:**
- Durante o dreno: `/readyz` → `503` `draining`; `/healthz` → `200` `ok`.
- `run/start` durante o dreno → HTTP `503`, erro `-32000` `server is draining — not accepting new runs`.
- `wait` no processo retorna só depois de a run SlowBuild terminar (não um corte em ~5 s).
- stderr do servidor tem `{"...","msg":"draining","timeout":"20s",...}`.
- Após restart no mesmo `--state-dir`: `run/status { runId }` → `state: "failed"`, `resumable: true`
  (checkpoint de shutdown, apesar de SlowBuild não declarar `strategy: "per_step"`).
- Segundo servidor com `--drain-timeout 0`: stderr `"cancelImmediately":true`, processo sai em ~5 s.

**Resultado Real:**
- [ ] funcionou
- [ ] não funcionou

### Evidências:

- [ ] Log/stderr do servidor (linha `draining`, `cancelImmediately`)
- [ ] `/readyz` e `/healthz` durante o dreno
- [ ] Resposta de `run/start` durante o dreno
- [ ] Tempo entre SIGTERM e encerramento do processo
- [ ] `run/status` do runId após o restart

### Observações:
- Referência de design: item 07 ("Shutdown cancels in-flight runs after 5s").
- `SlowBuild` fica `working` ~6–7 s (Compile 3 s + Package 3 s), tempo suficiente para
  observar a janela de dreno depois do SIGTERM.
- Teste sensível a tempo.

**Executado em:** [Data e hora do teste]

## Como executar

```sh
./run.sh
```
