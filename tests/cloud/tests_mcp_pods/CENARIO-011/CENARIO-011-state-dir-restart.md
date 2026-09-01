# Cenário 011: Estado de run sobrevive a restart do processo (`--state-dir`)

**Objetivo:** Verificar que, com `--state-dir`, um `runId` iniciado por um processo
pode ser consultado e retomado por outro processo apontado para o mesmo diretório;
e que sem `--state-dir` esse estado se perde no restart.

```gherkin
Dado que o servidor MCP roda com --state-dir apontando para um diretório D
E uma run DocPipeline foi iniciada com approved="no" e parou no gate Review
Quando o processo é morto e um novo processo sobe com o mesmo --state-dir D
Então run/status para aquele runId devolve state failed, step Review, resumable true
E run/resume com approved="yes" leva a run a completed
E o mesmo fluxo sem --state-dir devolve "unknown runId" após o restart
```

**Resultado Esperado:**
- Com `--state-dir D`: após o restart, `run/status { runId }` → `state: "failed"`,
  `step: "Review"`, `resumable: true` (run reconstruída do disco). `run/resume` →
  `completed`, `vars.published == "published docs for billing (reviewed)"`.
- Sem `--state-dir` (dir por processo): após o restart, `run/status { runId }` →
  `-32602 unknown runId`.

**Resultado Real:**
- [ ] funcionou
- [ ] não funcionou

### Evidências:

- [ ] Logs dos dois processos (antes e depois do restart) — caso com `--state-dir`
- [ ] `run/status` e `run/resume` pós-restart
- [ ] `run/status` pós-restart do caso de controle (sem `--state-dir`) com o erro

### Observações:
- Referência de design: item 01 (gap 1 — estado node-local). Aqui é o **mesmo nó**,
  apenas um restart de processo — o que um PVC no K8s cobriria.
- `DocPipeline` declara `checkpoint { strategy: "per_step" }`, condição do resume.
- Sem `--principal-header`, o owner é o hash da sessão; como ele não é persistido,
  o primeiro chamador após o restart reivindica o `runId` (comportamento histórico).

**Executado em:** [Data e hora do teste]

## Como executar

```sh
./run.sh
```
