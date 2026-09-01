# Cenário 019: `mhl-sql-postgres` — implantar DDL (`execScript`)

**Objetivo:** Verificar o método `execScript` — um script DDL/DML de **vários
statements** rodando numa **única transação** (tudo ou nada) — e que ele, como
`exec`, só funciona com `read_only: false`.

```gherkin
Dado o Postgres semeado e mhl-sql-postgres instalado

# A — aplica
Quando read_only:false e Db.execScript("CREATE TABLE audit (...); CREATE INDEX ...; INSERT INTO audit ...")
Então mhl run sai 0
E `psql` mostra a tabela audit, o índice audit_at_idx e 2 linhas

# B — rollback
Quando um execScript cria uma tabela e depois falha num statement inválido
Então mhl run falha
E `psql` mostra que a tabela do passo B NÃO foi criada (rollback total)

# C — bloqueado em read_only
Quando read_only:true e Db.execScript("CREATE TABLE x (...)")
Então mhl run falha com "execScript is disabled (read_only: true)"
```

**Resultado Esperado:**
- **A:** `mhl run` == 0; `SELECT to_regclass('audit')` não é nulo; índice
  `audit_at_idx` existe; `SELECT count(*) FROM audit` == 2.
- **B:** `mhl run` != 0; `SELECT to_regclass('b_table')` **é** nulo (nada aplicado).
- **C:** `mhl run` != 0; saída contém `execScript is disabled`.

**Resultado Real:**
- [ ] funcionou
- [ ] não funcionou

### Evidências:
- [ ] `logs/run-a.out`, `logs/run-b.out`, `logs/run-c.out`
- [ ] `logs/schema.txt` — objetos no schema após cada passo

### Observações:
- `execScript` não aceita parâmetros `$1` (é nível de script). Para um DDL único
  parametrizado, ou statements que não rodam em transação (`CREATE INDEX
  CONCURRENTLY`, `VACUUM`), use `exec`.
- O cenário derruba `audit`/`b_table` no início para ser idempotente.
- PULA (SKIP) sem Docker.

**Executado em:** [Data e hora do teste]

## Como executar

```sh
./run.sh
```
