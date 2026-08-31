# Cenário 016: `run/cancel` aborta uma chamada em andamento

**Objetivo:** Verificar que cancelar uma run enquanto um passo está bloqueado numa
chamada externa (aqui um `cmd.exec` com `sleep`) interrompe esse subprocesso na
hora — a run não continua executando os passos seguintes.

```gherkin
Dado que o servidor MCP está em execução
E uma run SlowBuild está no passo Compile (um "sleep 3" dentro de cmd.exec)
Quando o cliente chama run/cancel ~1s após o início
Então run/status passa a canceled imediatamente
E a run nunca entra no passo Package (o subprocesso do Compile foi abortado)
E o stderr do servidor mostra "step: Compile" mas não "step: Package"
```

**Resultado Esperado:**
- `run/cancel` → `state: "canceled"` na resposta imediata.
- Nos ~4 s seguintes, nem `run/logs { runId }` nem o stderr do servidor mostram
  `step: Package` — ou seja, o `sleep` do `Compile` foi cortado, não aguardado.
- `reached` permanece `["Compile"]`.

**Resultado Real:**
- [ ] funcionou
- [ ] não funcionou

### Evidências:

- [ ] `run/start`, `run/cancel` e `run/status` (com timestamps)
- [ ] `run/logs { runId }` alguns segundos após o cancel
- [ ] stderr do servidor (`step: Compile` presente, `step: Package` ausente)

### Observações:
- Referência de design: item 01, direção ("a run-level cancel also aborts a blocking
  call already in flight — an agent subprocess / cmd/git/http native op").
- `SlowBuild`: `Compile` = `cmd.exec(["sh","-c","sleep 3; echo ..."])`; sem o abort,
  a run seguiria para `Package` (+3 s) e `Ship`.
- Teste sensível a tempo.

**Executado em:** [Data e hora do teste]

## Como executar

```sh
./run.sh
```
