# Cenário 016: `mhl serve mcp --http` com `extension store` apontando para PostgreSQL

**Objetivo:** Verificar que `mhl serve mcp --http` roteia **todo o estado
durável** (sessões + checkpoints de `run/*` + owner) para a extensão oficial
`mhl-store-postgres` quando o diretório de workflows declara `extension store S
{ dsn ... }` — e que `run/resume`, o reclaim pós-restart e várias `run/start`
concorrentes funcionam com o estado vivendo numa tabela do Postgres.

```gherkin
Dado um Postgres local e um dir de workflows com DocPipeline + `extension store S` -> Postgres
Quando o servidor sobe e um run/start (approved:no) para no gate
Então há uma linha run/<id>/checkpoint/DocPipeline na tabela (via psql)
E há uma linha session/<sid>
E a wire trace da extensão registra chaves "session/..." e "run/<id>/..."

Quando run/resume {approved:yes}
Então o run chega a "completed"

Dado um segundo run/start (approved:no) parado, o servidor é reiniciado no mesmo banco
Quando run/status é consultado após o restart
Então o run é reconstruído do Postgres: state=failed, resumable=true

Quando dois run/start concorrentes (approved:no) são disparados
Então cada um tem sua própria chave run/<id>/checkpoint/DocPipeline na tabela
E run/list da sessão devolve >= 2 runs
```

**Resultado Esperado:**
- Linhas na tabela sob o prefixo do serve: ao menos um `session/<sid>` e, por
  run parado, um `run/<id>/checkpoint/DocPipeline`.
- `run/resume` → `completed`.
- `run/status` pós-restart do run #2 → `state=failed`, `resumable=true`.
- `run/list` da sessão ≥ 2; ≥ 3 run ids distintos com checkpoint vivo na tabela.

**Resultado Real:**
- [ ] funcionou
- [ ] não funcionou

### Evidências:
- [ ] `logs/run-start.json`, `logs/status1*.json`, `logs/run-resume.json`
- [ ] `logs/status-after-restart.json` — reclaim do Postgres
- [ ] `logs/state-keys*.txt` — `SELECT key FROM mhl_store`
- [ ] `logs/probe.jsonl` — wire trace da extensão
- [ ] `logs/mcp-server.log`

### Observações:
- É o análogo do CENARIO-013 (que usa `mhl-store-s3`) e do CENARIO-010 (que usa
  `store-probe` + FS local); aqui o backend é `src/mhl-extensions/mhl-store-postgres` e o
  estado vive numa tabela. PULA (SKIP) sem Docker.
- Runs concluídas removem o checkpoint (`terminal == "completed"` →
  `cps.Remove`); por isso a Parte B usa `approved:no` (para no gate) para que
  as chaves `run/<id>/checkpoint` persistam na hora da verificação.
- O `mcpserver` dá a cada run chaves `run/<id>/…` disjuntas — é assim que ele
  evita o lost update que o CENARIO-015/B demonstra para a **mesma** chave.

**Executado em:** [Data e hora do teste]

## Como executar

```sh
./run.sh
```
