# Cenário 017: assinatura tipada — `workflow X(req: In): Out`

**Objetivo:** Verificar, pelo servidor MCP HTTP real, o contrato de um workflow com
assinatura tipada: o `inputSchema` vem dos campos do tipo de entrada, o
`outputSchema` do tipo de saída, o resultado é a projeção `output:` validada, um
`run/resume` com argumentos parciais é mesclado no `req` salvo no checkpoint, e
uma projeção que quebra o tipo falha a run sem devolver resultado.

```gherkin
Dado que o servidor MCP está em execução sobre CENARIO-017/workflow
Quando o cliente chama tools/list
Então Review anuncia inputSchema com required ["diff"] e outputSchema com title "ReviewOutput"
Quando o cliente chama tools/call Review com {diff, approved: "yes"}
Então structuredContent é exatamente {approved: true, summary: "d1@main"}
Quando o cliente chama run/start Review com {diff: "d2"}
Então a run para em state "paused" no passo Gate
Quando o cliente chama run/resume com só {approved: "yes", base: "dev"}
Então a run completa e run/status.vars é {approved: true, summary: "d2@main"}
Quando o cliente chama tools/call Broken
Então isError é true, sem structuredContent, e o texto cita "does not satisfy Status"
```

**Resultado Esperado:**
- `tools/list`: `Review.inputSchema.required == ["diff"]`, `additionalProperties: false`;
  `Review.outputSchema.title == "ReviewOutput"`.
- `tools/call Review`: `isError: false`, `structuredContent` = projeção exata.
- `run/resume` com argumentos parciais mantém `diff` do checkpoint. O `summary` é
  calculado no passo `Prepare`, antes da pausa, então continua com `base` = `main`.
- `tools/call Broken`: `isError: true`, sem `structuredContent`.

**Resultado Real:**
- [ ] funcionou
- [ ] não funcionou

### Evidências:

- [ ] `tools/list`, `tools/call`, `run/start`, `run/status`, `run/resume` (logs/)

### Observações:
- Usa um diretório de workflows próprio para não alterar o `tools/list` dos outros cenários.

**Executado em:** [Data e hora do teste]
