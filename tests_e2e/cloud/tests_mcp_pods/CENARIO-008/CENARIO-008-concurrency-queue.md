# Cenário 008: Concorrência limitada e fila de runs

**Objetivo:** Verificar que `--max-concurrent-runs` limita execuções simultâneas no
pod, que runs excedentes entram em `queued`, que `run/cancel`
descarta uma run enfileirada, e que um `tools/call` síncrono com o pool cheio
sofre shed de carga.

```gherkin
Dado que o servidor MCP está em execução com --max-concurrent-runs 1
Quando o cliente dispara três run/start de SlowBuild em sequência
Então a primeira fica working e as outras ficam queued
E um tools/call síncrono nesse momento responde -32000 "server at capacity"
E run/cancel numa run queued a leva a canceled sem nunca executar
E conforme a run em execução termina, a próxima da fila passa a working
```

**Resultado Esperado:**
- `run/start` #1 → `state: "working"`.
- `run/start` #2 → `state: "queued"` (sem `queuePosition`: o campo foi removido;
  a profundidade da fila está em `/metrics`).
- `run/start` #3 → `state: "queued"`.
- `tools/call` (SlowBuild) com o pool cheio → após ~5 s, erro JSON-RPC `-32000`
  `server at capacity — retry, or use run/start` (HTTP 503).
- `run/cancel` na run #3 (queued) → `state: "canceled"`; ela nunca gera `step:`.
- Ao terminar a run #1, a run #2 transita `queued` → `working`.
- `/metrics` mostra `mhl_serve_runs_queued >= 1` enquanto há fila.

**Resultado Real:**
- [ ] funcionou
- [ ] não funcionou

### Evidências:

- [ ] Log do servidor MCP
- [ ] Respostas dos três run/start (com `state`)
- [ ] Resposta do tools/call em capacidade máxima
- [ ] `/metrics` durante a fila
- [ ] run/status da run enfileirada cancelada e da que subiu da fila

### Observações:
- Referência de design: item 05 ("No server-level concurrency limit or queue").
- `SlowBuild` leva ~7–9 s; o passo `Ship` tem `timeout 1s` sobre `sleep 3` e
  auto-termina — o importante aqui é que o **slot libera** e a fila anda.
- Teste sensível a tempo: os `run/start` são sequenciais para garantir a ordem da fila.

**Executado em:** [Data e hora do teste]

## Como executar

```sh
./run.sh
```
