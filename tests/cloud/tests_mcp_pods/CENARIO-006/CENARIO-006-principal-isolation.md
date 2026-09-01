# Cenário 006: Isolamento de runs por principal

**Objetivo:** Verificar que, com `--principal-header`, o pod isola runs por
identidade verificada: um principal não vê nem consegue tocar as runs de outro,
e o servidor recusa `--principal-header` sem `--token`.

```gherkin
Dado que o servidor MCP está em execução com --principal-header X-Mhl-Principal e --token
E o principal "alice@acme.com" iniciou uma run
Quando o principal "bob@acme.com" pede run/list
Então recebe uma lista vazia
E run/status para o runId de alice devolve erro "unknown runId"
E alice consegue ver o próprio runId em run/list
E iniciar o servidor com --principal-header mas sem --token falha na largada
```

**Resultado Esperado:**
- `alice` faz `run/start` → recebe `runId`.
- `bob` (`X-Mhl-Principal: bob@acme.com`) `run/list` → `{"runs":[]}`.
- `bob` `run/status {runId de alice}` → erro JSON-RPC `-32602` `unknown runId "..."`.
- `alice` `run/list` → contém o `runId`.
- Processo com `--principal-header X-Mhl-Principal` e **sem** `--token` → sai com
  erro: `--principal-header needs --token / MHL_SERVE_TOKEN: ...`.

**Resultado Real:**
- [ ] funcionou
- [ ] não funcionou

### Evidências:

- [ ] Log do servidor MCP
- [ ] `run/start` de alice, `run/list` e `run/status` de bob, `run/list` de alice
- [ ] Saída/stderr do processo iniciado sem `--token`

### Observações:
- Referência de design: item 03 (gap 3 — identidade). Aqui simula-se a identidade que
  o API Gateway injetaria, via header confiável + segredo compartilhado.
- Requests em modo stateless (`params._meta`), com `X-Mhl-Principal` em cada chamada.
- O workflow `DocPipeline` roda com `approved: "no"` para parar no gate e deixar a run
  "resumable" enquanto os checks acontecem.
- `context.principal` chegando ao workflow exige um bloco `context:` no `.mh` — não há
  nos workflows de `tests/cloud`, então não é verificado aqui.

**Executado em:** [Data e hora do teste]

## Como executar

```sh
./run.sh
```
