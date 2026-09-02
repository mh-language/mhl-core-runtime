# ITEM-04 (alvo): `tools/call` longo é promovido para run assíncrona

> **Fora do escopo do runtime.** Não é item de distribuição nem de isolamento —
> é ergonomia de gateway. Atrás do API Gateway o cliente **deve** usar
> `run/start` → `run/status` → `run/resume` (o `docs-workflow.mh` documenta
> isso, e a capability `experimental["mhl.run"]` do ITEM-11 anuncia que a
> família existe). Este `.md` fica como registro; o `run.sh` mostra o gap.

**Item do design:** 04 — *`tools/call` blocks for the whole run* (parcial).
**Estado hoje:** `callTool` segura a requisição HTTP por `execsvc.Run` inteiro.
Atrás do API Gateway (~29s de integração) e do ALB (~60s idle), uma run real
estoura. A superfície `run/*` existe, mas um cliente MCP padrão só conhece
`tools/call`.

## Comportamento-alvo

```gherkin
Dado `mhl serve mcp --http --tools-call-timeout 5s`
Quando tools/call SlowBuild (a run leva ~9s)
Então em ~5s a resposta volta com um resultado que aponta um runId
  (ex.: result.structuredContent.runId, ou _meta.io.modelcontextprotocol/... , ou
   um content text "run promoted: <runId>")
E run/status {runId} depois funciona e a run segue executando
E o initialize/`capabilities` anuncia esse comportamento (ver ITEM-11)
```

Alternativa aceitável ao flag: promoção automática acima de um limiar embutido
(documentado), desde que o `runId` seja recuperável da resposta.

## Critério de aceite

1. O servidor **aceita** uma flag de limiar (`--tools-call-timeout` /
   `--promote-after` — nome à escolha) **ou** promove automaticamente.
2. `tools/call` de uma run que excede o limiar retorna em ~limiar (não pela
   duração inteira), e a resposta **contém um runId** recuperável.
3. `run/status {runId}` funciona e a run continua/termina normalmente.

## Como implementar (pistas)

- `internal/cli/serve.go`: nova flag `--tools-call-timeout <dur>`.
- `internal/mcpserver/server.go` `callTool`: se a run não terminar dentro do
  limiar, registrar como run assíncrona (`h.runs`), devolver um result MCP que
  carrega o `runId` (em `structuredContent` e/ou `_meta`), e deixar a goroutine
  seguir — exatamente como `run/start`.
- Anunciar em `initialize` (ITEM-11).

## Evidências (logs/)
- `promote-flag.out` (o servidor aceitou a flag?)
- `tools-call.json` + tempo (deve ter runId e voltar em ~limiar)
- `run-status.json` (a run promovida segue viva)

**Verificado por:** `./run.sh` — hoje **PENDING**.
