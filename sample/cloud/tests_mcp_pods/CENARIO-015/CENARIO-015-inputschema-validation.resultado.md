# CENÁRIO-015 — Resultado da Execução

> Registro gerado pelo agente `tester-mcp-server`.
> O arquivo original [`CENARIO-015-inputschema-validation.md`](CENARIO-015-inputschema-validation.md) não foi alterado.

## Cenário 015: Validação do `inputSchema` anunciado (campos obrigatórios / extras)

**Resultado Real:**
- [x] funcionou
- [ ] não funcionou

**Executado em:** 2026-08-31 15:10 -03:00 (reexecução após correção; 1ª execução às 13:49 falhou — ver histórico)

---

## Ambiente

| Item | Valor |
|---|---|
| Binário | `sample/cloud/tests/CENARIO-015/mhl` (cópia de `sample/cloud/mhl`, reconstruído do source atual) |
| Versão | `mhl v1.1.0-alpha-7-g418d7dc-dirty` |
| Servidor | `mhl serve mcp --http --addr 127.0.0.1:8736 --token <gerado> --state-dir <tmp> sample/cloud` |
| Modo | stateless (`params._meta`) |
| Script | [`run.sh`](run.sh) |

## `inputSchema` anunciado em `tools/list` para `DocPipeline` ([`logs/tools-list.json`](logs/tools-list.json))

```json
{
  "type": "object",
  "additionalProperties": false,
  "properties": { "approved": {"type":"string"}, "repo": {"type":"string"} },
  "required": ["approved", "repo"]
}
```

## Casos e resultados (todos OK)

| Caso | Esperado | Obtido |
|---|---|---|
| **1.** `tools/call DocPipeline {approved:"yes"}` (sem `repo`) | `-32602` citando o campo faltante | `-32602` · `invalid inputs for "DocPipeline": missing required input "repo" (declared: "approved", "repo")` |
| **2.** `tools/call DocPipeline {repo:"x", approved:"yes", campoExtra:123}` | `-32602` (`additionalProperties:false`) | `-32602` · `invalid inputs for "DocPipeline": undeclared input "campoExtra" (declared: "approved", "repo")` |
| **3.** `run/start DocPipeline {approved:"yes"}` (sem `repo`) | `-32602`, run não inicia | `-32602` · `missing required input "repo" ...` — **sem `runId`**, nenhuma run criada |

O log do servidor ([`logs/mcp-server.log`](logs/mcp-server.log)) **não** tem `session:` / `step:` /
`run started` para nenhum dos casos — a rejeição acontece na borda, antes de criar sessão, state dir ou run.

## Causa da correção

`internal/execsvc` ganhou um **admission check** no início de `execsvc.Run`
(`execsvc.go`, antes de resolver a sessão):

```go
// Admission check: enforce the pipeline's input contract (InputSchema) —
// required inputs present, no undeclared keys — before creating a session,
// state dir, or run. Strict always ... Skipped on resume: the checkpoint owns the inputs then.
if !req.Resume {
    if err := pipeline.ValidateInputs(req.Inputs); err != nil {
        return nil, err
    }
}
```

`runStart` e `callTool` no `internal/mcpserver` propagam esse erro como `-32602`.

## Histórico — 1ª execução (13:49) FALHOU

Antes da correção, os três casos **não** eram rejeitados:
- caso 1: a run executava e falhava no passo `Draft` (`undefined variable "repo"`), `isError:true`;
- caso 2: a run **completava** e `campoExtra:123` era injetado nas `vars`;
- caso 3: `run/start` retornava `runId`/`working` e a run falhava depois no `Draft`.

Ou seja, o `inputSchema` era só anunciado, não imposto — a lacuna prevista nas observações
do cenário. O admission check em `execsvc.Run` fechou essa lacuna.

## Evidências

- [x] [`logs/tools-list.json`](logs/tools-list.json) — `inputSchema` anunciado
- [x] [`logs/case1-tools-call-missing-repo.json`](logs/case1-tools-call-missing-repo.json) — `-32602 missing required input "repo"`
- [x] [`logs/case2-tools-call-extra-prop.json`](logs/case2-tools-call-extra-prop.json) — `-32602 undeclared input "campoExtra"`
- [x] [`logs/case3-run-start-missing-repo.json`](logs/case3-run-start-missing-repo.json) — `-32602`, sem `runId`
- [x] [`logs/mcp-server.log`](logs/mcp-server.log) — nenhum `step:` / `run started` (nada executado)
- [x] [`logs/client.log`](logs/client.log)

## Conclusão

**PASS.** O `inputSchema` publicado em `tools/list` (`required`, `additionalProperties:false`)
agora **é imposto** em `tools/call` e `run/start`: `required` ausente e propriedade não
declarada são rejeitados na borda com `-32602` e uma mensagem que cita o campo e o contrato,
sem criar sessão nem run.
