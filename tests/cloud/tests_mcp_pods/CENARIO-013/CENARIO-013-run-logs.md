# Cenário 013: Logs por run (`run/logs`) e eventos de ciclo de vida estruturados

**Objetivo:** Verificar que a saída `step:` / `log()` de uma run é recuperável pela
API, com cursor por offset de bytes e escopo de dono, e que o stderr do processo
emite linhas JSON de ciclo de vida com `runId` e `owner`.

```gherkin
Dado que o servidor MCP está em execução
E o cliente executou uma run DocPipeline até completed
Quando o cliente chama run/logs { runId }
Então recebe { text, nextSince } com "step: Draft", "step: Review" e "step: Publish" no text
Quando repete com run/logs { runId, since: nextSince }
Então recebe text vazio e um nextSince estável (sem erro)
E run/logs { runId } de outra sessão devolve -32602 "unknown runId"
E o stderr do servidor tem linhas JSON "run started" e "run completed" com runId e owner
```

**Resultado Esperado:**
- `run/logs { runId }` → `{ runId, text, nextSince }`; `text` contém
  `step: Draft`, `step: Review`, `step: Publish`; `nextSince` inteiro > 0.
- `run/logs { runId, since: <nextSince> }` → `text` vazio, `nextSince` igual, sem `error`.
- `run/logs { runId }` numa sessão diferente → `-32602 unknown runId`.
- stderr do servidor: linhas JSON com `"msg":"run started"` e `"msg":"run completed"`,
  ambas com as chaves `runId` e `owner`.

**Resultado Real:**
- [ ] funcionou
- [ ] não funcionou

### Evidências:

- [ ] Respostas de `run/logs` (primeira leitura, releitura com `since`, leitura por não-dono)
- [ ] Linhas JSON de ciclo de vida no stderr do servidor

### Observações:
- Referência de design: itens 08 e 09.
- O `text` também vai para o stderr do processo (`kubectl logs`); `run/logs` é a cópia
  por run, num ring de 64 KiB (`dropped:true` quando o cursor cai na região descartada).

**Executado em:** [Data e hora do teste]

## Como executar

```sh
./run.sh
```
