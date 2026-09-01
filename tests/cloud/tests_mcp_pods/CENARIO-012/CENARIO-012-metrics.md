# Cenário 012: Métricas Prometheus em `/metrics`

**Objetivo:** Verificar que o pod expõe contadores e gauges Prometheus que refletem
a atividade de runs e de `tools/call`, sem autenticação.

```gherkin
Dado que o servidor MCP está em execução
E o cliente executou uma run que completou, uma run que foi cancelada,
  um tools/call bem-sucedido e um tools/call com erro
Quando o cliente faz GET /metrics sem bearer
Então recebe 200 com texto Prometheus
E mhl_serve_runs_total{outcome="completed"} >= 1
E mhl_serve_runs_total{outcome="canceled"} >= 1
E mhl_serve_run_duration_seconds_count >= 2
E mhl_serve_tool_calls_total{outcome="ok"} >= 1 e {outcome="error"} >= 1
E as gauges runs_active, runs_queued e sessions_active estão presentes
```

**Resultado Esperado:** `GET /metrics` → `200`, `Content-Type: text/plain; version=0.0.4`,
corpo com as séries `mhl_serve_runs_total`, `mhl_serve_run_duration_seconds_{sum,count}`,
`mhl_serve_tool_calls_total{outcome="ok"|"error"}`, `mhl_serve_runs_active`,
`mhl_serve_runs_queued`, `mhl_serve_sessions_active`, com os valores acima.

**Resultado Real:**
- [ ] funcionou
- [ ] não funcionou

### Evidências:

- [ ] Log do servidor MCP
- [ ] Corpo de `/metrics` (antes e depois da atividade)
- [ ] Respostas das runs / tools/call que geraram os números

### Observações:
- Referência de design: item 08 ("Unstructured diagnostics, no metrics").
- `ObserveRun` só conta ao término da run; o script faz polling até os estados terminais.
- `run_duration_seconds` exclui o tempo em fila.

**Executado em:** [Data e hora do teste]

## Como executar

```sh
./run.sh
```
