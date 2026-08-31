# Cenário 010: Cap de wall-clock por passo (`step … timeout <dur>`)

**Objetivo:** Verificar que um passo que estoura sua cláusula `timeout` auto-termina
a run com um erro que menciona o timeout, e que `run/resume` re-executa esse passo
com um orçamento novo.

```gherkin
Dado que o servidor MCP está em execução
E o workflow SlowBuild tem o passo Ship com "timeout 1s" sobre um "sleep 3"
Quando o cliente inicia SlowBuild e acompanha por run/status
Então a run termina como failed no passo Ship, com erro mencionando timeout
E reached contém Compile e Package
Quando o cliente chama run/resume para o mesmo runId
Então o passo Ship é re-executado e volta a falhar por timeout (orçamento renovado)
```

**Resultado Esperado:**
- `run/status` → `state: "failed"`, `step: "Ship"`, `error` contém
  `exceeded its timeout` (ou `step timeout`), `reached` inclui `"Compile"` e `"Package"`.
- `run/resume { runId }` → `state: "working"`; nova sondagem → `state: "failed"` em
  `Ship` de novo, com a mesma classe de erro.

**Resultado Real:**
- [ ] funcionou
- [ ] não funcionou

### Evidências:

- [ ] Log do servidor MCP (`step: Compile`, `step: Package`, `step: Ship`)
- [ ] `run/status` mostrando o erro de timeout
- [ ] `run/resume` e a `run/status` seguinte

### Observações:
- Referência de design: item da tabela "Per-step wall-clock cap".
- `--state-dir` é usado para o checkpoint entre a falha e o `run/resume`.
- Para deixar a run **completar**, bastaria subir o `timeout` de `Ship` para `10s` no
  `sample/cloud/slow-build.mh` — o que este cenário **não** faz (não altera fonte).
- Teste sensível a tempo (~7 s por tentativa).

**Executado em:** [Data e hora do teste]

## Como executar

```sh
./run.sh
```
