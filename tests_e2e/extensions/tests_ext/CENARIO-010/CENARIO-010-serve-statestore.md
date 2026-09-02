# Cenário 010: `mhl serve mcp --http` com `extension store` como StateStore

**Objetivo:** Verificar o caminho de produção do StateStore: uma declaração
`extension store S { dir }` no diretório servido faz `mhl serve mcp --http`
rotear **todo o estado durável** (`run/*` checkpoints, `run/*/owner`, sessões)
pela extensão em vez do `.mhl/state` em disco — inclusive sob `run/start`
concorrentes.

```gherkin
Dado um serve dir com docs-workflow.mh, slow-build.mh e
      "extension store S { dir: <state>, log: <probe.jsonl> }" (sem --state-dir)
Quando um cliente faz run/start DocPipeline (approved=no) e a run para no gate
Então a extensão recebe put de session/<sid> e run/<rid>/checkpoint/DocPipeline
Quando run/resume (approved=yes)
Então a run vai a completed
Quando o processo do servidor é morto e outro sobe no mesmo dir
Então run/status <rid'> de uma run parada reconstrói do store da extensão
Quando N run/start SlowBuild concorrentes rodam com --max-concurrent-runs 2
Então cada run tem chaves run/<id>/... disjuntas no store, sem corrupção
```

**Resultado Esperado:**
- Sem `--state-dir`: `probe.jsonl` mostra `put`/`get`/`list` com `key` começando em
  `session/` e `run/`; a árvore de `<state>` tem `session/<sid>.json` e
  `run/<rid>/checkpoint/DocPipeline.json`.
- `run/resume` → `state: "completed"`.
- Após restart (novo processo, mesmo dir): `run/status` de uma run parada →
  `state: "failed"`, `resumable: true` (reconstruída do store da extensão).
- Concorrência: 4× `run/start SlowBuild`, `--max-concurrent-runs 2` → todas chegam
  a um estado terminal; as chaves `run/<id>/…` no store são distintas por `id`.

**Resultado Real:**
- [ ] funcionou
- [ ] não funcionou

### Evidências:

- [ ] `logs/probe.jsonl` (chaves session/ e run/), `logs/state-tree.txt`
- [ ] respostas de run/start, run/status, run/resume, run/list
- [ ] `logs/state-tree-after-restart.txt`

### Observações:
- `--state-dir` passa a ser só o scratch do interpretador; o estado de `run/*` e
  `session/*` vive na extensão (`internal/cli/storext.go` → `extKV` → `KVStore`).
- Teste sensível a tempo (`SlowBuild` ~7 s cada).

**Executado em:** [Data e hora do teste]

## Como executar

```sh
./run.sh
```
