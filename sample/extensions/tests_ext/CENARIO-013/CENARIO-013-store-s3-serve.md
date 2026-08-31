# Cenário 013: `mhl serve mcp --http` com `extension store` apontando para S3

**Objetivo:** Verificar que `mhl serve mcp --http` roteia **todo o estado
durável** (sessões + checkpoints de `run/*` + owner) para a extensão oficial
`mhl-store-s3` quando o diretório de workflows declara `extension store S { ...
bucket/endpoint ... }` — e que `run/resume`, o reclaim pós-restart e várias
`run/start` concorrentes funcionam com o estado vivendo num bucket S3.

```gherkin
Dado um MinIO local e um dir de workflows com DocPipeline + `extension store S` -> S3
Quando o servidor sobe e um run/start (approved:no) para no gate
Então há um objeto run/<id>/checkpoint/DocPipeline.json no bucket (via mc ls)
E há um objeto session/<sid>.json no bucket
E a wire trace da extensão registra chaves "session/..." e "run/<id>/..."

Quando run/resume {approved:yes}
Então o run chega a "completed"

Dado um segundo run/start (approved:no) parado, o servidor é reiniciado no mesmo bucket
Quando run/status é consultado após o restart
Então o run é reconstruído do S3: state=failed, resumable=true

Quando dois run/start concorrentes (approved:no) são disparados
Então cada um tem sua própria chave run/<id>/checkpoint/DocPipeline.json no bucket
E run/list da sessão devolve >= 2 runs
```

**Resultado Esperado:**
- Objetos no bucket sob `c013/`: pelo menos um `session/<sid>` e, por run
  parado, um `run/<id>/checkpoint/DocPipeline`.
- `run/resume` → `completed`.
- `run/status` pós-restart do run #2 → `state=failed`, `resumable=true`.
- `run/list` da sessão ≥ 2; ≥ 3 run ids distintos com checkpoint vivo no bucket
  (run #2 parado + as 2 concorrentes).

**Resultado Real:**
- [ ] funcionou
- [ ] não funcionou

### Evidências:
- [ ] `logs/run-start.json`, `logs/status1*.json`, `logs/run-resume.json`
- [ ] `logs/status-after-restart.json` — reclaim do S3
- [ ] `logs/state-keys*.txt` — chaves lógicas derivadas de `mc ls`
- [ ] `logs/probe.jsonl` — wire trace da extensão (chaves session/ e run/<id>/)
- [ ] `logs/mcp-server.log`

### Observações:
- É o análogo do CENARIO-010 (que usa `store-probe` + FS local); aqui o
  backend é a extensão oficial `src/mhl-store-s3` e o estado vive num bucket S3
  (MinIO). PULA (SKIP) sem Docker.
- Runs concluídas removem o checkpoint (`terminal == "completed"` →
  `cps.Remove`); por isso a Parte B usa `approved:no` (para no gate) para que
  as chaves `run/<id>/checkpoint` persistam no bucket na hora da verificação.
- O `mcpserver` dá a cada run chaves `run/<id>/…` disjuntas — é assim que ele
  evita o lost update que o CENARIO-012/B demonstra para a **mesma** chave.

**Executado em:** [Data e hora do teste]

## Como executar

```sh
./run.sh
```
