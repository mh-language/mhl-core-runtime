# Cenário 014: `mhl-store-postgres` — carregar de um `.mh` e as 4 operações

**Objetivo:** Verificar que a extensão **oficial** `src/mhl-extensions/mhl-store-postgres`
(kind `store`, backend PostgreSQL) é carregada por um `.mh`, cria seu schema
(`auto_migrate`) e faz `get` / `put` / `delete` / `list` round-trip real contra
o banco — confirmado inspecionando a tabela com `psql`. Também verifica que
`put` é **upsert atômico** (`INSERT ... ON CONFLICT DO UPDATE`).

```gherkin
Dado um Postgres local (docker compose de src/mhl-extensions/mhl-store-postgres)
E mhl-store-postgres instalado no projeto (mhl extension install)
E um .mh declara "extension store S { dsn: env(...), table, log }"
Quando um step faz S.put("run/demo/checkpoint/DocPipeline","gate") e S.put("session/sess-1", 7)
E outro step reescreve a mesma chave: S.put("run/demo/checkpoint/DocPipeline","review")
E steps fazem get (hit + miss->null), list("run/"), delete (2x, idempotente), list("run/")
Então a tabela mhl_store foi criada (auto_migrate)
E get devolve "review" (o upsert sobrescreveu), o miss devolve null
E list("run/") antes = ["run/demo/checkpoint/DocPipeline"], depois do delete = []
E `psql` mostra a linha session/sess-1 e NÃO mostra a chave deletada
E a wire trace tem "ev":"init" e "op":"put"
E "mhl extension doctor" sai 0
```

**Resultado Esperado:**
- `mhl extension doctor` sai `0`.
- Linhas de saída, em ordem: `review` / `null` / `["run/demo/checkpoint/DocPipeline"]` / `[]`.
- `psql -c "SELECT key FROM mhl_store"` inclui `session/sess-1` e **não** inclui
  `run/demo/checkpoint/DocPipeline`.
- `logs/wire.jsonl`: `"ev":"init"` + linhas `"ev":"call"`.

**Resultado Real:**
- [ ] funcionou
- [ ] não funcionou

### Evidências:
- [ ] `logs/run.out` — saída de `mhl run`
- [ ] `logs/wire.jsonl` — trace da extensão
- [ ] `logs/rows.txt` — `SELECT key, value FROM mhl_store`
- [ ] `logs/doctor.out`

### Observações:
- Exige um Postgres alcançável: `pg_ensure` sobe via `docker compose up -d
  --wait` e **PULA** o cenário (SKIP, não FAIL) se o Docker não estiver
  disponível.
- Credenciais chegam por `env()` — resolvidas host-side pelo runtime e
  registradas para redação; o processo da extensão não herda ambiente.
- `auto_migrate: true` (default) roda `CREATE TABLE/INDEX IF NOT EXISTS` na 1ª
  chamada; ponha `false` quando o schema for gerido por fora.

**Executado em:** [Data e hora do teste]

## Como executar

```sh
./run.sh
```
