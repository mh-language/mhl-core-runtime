# Cenário 017: `mhl-sql-postgres` — consultas livres (DQL) de um `.mh`

**Objetivo:** Verificar que a extensão **oficial** `src/mhl-extensions/mhl-sql-postgres`
(kind `sql`) é carregada por um `.mh` e executa `SELECT` livres contra o
Postgres, devolvendo linhas como objetos JSON — com tipos mapeados
corretamente (`numeric`, `jsonb`, `timestamptz`), parâmetros `$1`, e os
métodos `query` / `queryRow` / `queryValue`.

Banco semeado por `src/mhl-extensions/mhl-sql-postgres/initdb/seed.sql` (tabela `people`, 5
linhas; 4 `active`).

```gherkin
Dado um Postgres semeado (docker compose de src/mhl-extensions/mhl-sql-postgres) e mhl-sql-postgres instalado
E um .mh declara "extension sql Db { dsn: env(...), read_only: true }"
Quando Db.query("SELECT name, org, score, tags FROM people WHERE org = $1 ORDER BY name", "acme")
Então devolve 3 objetos; o 1º é {name: "Ana", score: 91.5, tags: ["lead","eu"], ...}
Quando Db.queryRow("SELECT name FROM people ORDER BY score DESC LIMIT 1")
Então devolve {name: "Ana"}; e um SELECT sem linhas devolve null
Quando Db.queryValue("SELECT count(*) FROM people WHERE active")
Então devolve 4
E a tabela continua com 5 linhas (nada foi escrito)
E "mhl extension doctor" sai 0
```

**Resultado Esperado:**
- `query("... org = $1 ...", "acme")` → 3 linhas; `["lead","eu"]` no `tags` da Ana; `score` 91.5 (número).
- `queryRow(... LIMIT 1)` → objeto com `name = Ana`; `queryRow("SELECT 1 WHERE false")` → `null`.
- `queryValue("SELECT count(*) ... active")` → `4`.
- `psql -c "SELECT count(*) FROM people"` → `5` (inalterado).
- `mhl extension doctor` sai `0`.

**Resultado Real:**
- [ ] funcionou
- [ ] não funcionou

### Evidências:
- [ ] `logs/run.out` — saída de `mhl run`
- [ ] `logs/wire.jsonl` — trace (SQL head + nargs, sem valores)
- [ ] `logs/count.txt` — `SELECT count(*) FROM people` antes/depois
- [ ] `logs/doctor.out`

### Observações:
- Exige Docker (Postgres semeado); **PULA** (SKIP, não FAIL) sem ele.
- Kind `sql` — diferente de `store`; convive com `mhl-store-postgres`.
- Credenciais via `env()`, resolvidas host-side e redigidas; o wire trace grava
  só o começo do SQL e a **contagem** de args, nunca os valores.

**Executado em:** [Data e hora do teste]

## Como executar

```sh
./run.sh
```
