# Cenário 018: `mhl-sql-postgres` — "DQL only" e guardas

**Objetivo:** Verificar as três camadas que garantem que a extensão só faz
consulta de dados, e as guardas de tamanho:

- **A — escrita via `query`** (`read_only: true`): `INSERT/UPDATE/DELETE` são
  recusados **pelo Postgres** (`default_transaction_read_only=on`,
  `SQLSTATE 25006`).
- **B — `exec` desabilitado** com `read_only: true`: erro claro da extensão.
- **C — `;`-stacking**: dois statements num texto → recusado (protocolo estendido).
- **D — `max_rows`**: `SELECT *` numa tabela maior que `max_rows` → erro pedindo `LIMIT`.
- **E — controle**: com `read_only: false`, `Db.exec("INSERT …")` grava (e limpamos).

```gherkin
Dado o Postgres semeado (5 linhas em people) e mhl-sql-postgres instalado
Quando um .mh com read_only:true faz Db.query("INSERT INTO people …")
Então mhl run falha com "read-only transaction" / SQLSTATE 25006
E people continua com 5 linhas

Quando o mesmo faz Db.exec("INSERT …")
Então mhl run falha com "exec is disabled (read_only: true)"

Quando faz Db.query("SELECT 1; DROP TABLE people")
Então mhl run falha (múltiplos comandos não são aceitos)

Quando read_only:true, max_rows:2 e Db.query("SELECT * FROM people")
Então mhl run falha com "result exceeded max_rows=2"

Quando read_only:false e Db.exec("INSERT INTO people(name,org) VALUES ('tmp','tmp')")
Então retorna 1; depois Db.exec("DELETE … WHERE org='tmp'") limpa
```

**Resultado Esperado:**
- A: `mhl run` != 0; `run-a.out` contém `read-only transaction` ou `25006`;
  `SELECT count(*) FROM people` continua `5`.
- B: `mhl run` != 0; `run-b.out` contém `exec is disabled`.
- C: `mhl run` != 0 (múltiplos comandos recusados).
- D: `mhl run` != 0; `run-d.out` contém `max_rows=2`.
- E: `mhl run` == 0; `run-e.out` mostra `1`; contagem volta a `5`.

**Resultado Real:**
- [ ] funcionou
- [ ] não funcionou

### Evidências:
- [ ] `logs/run-a.out` … `logs/run-e.out`
- [ ] `logs/counts.txt` — `SELECT count(*) FROM people` em cada etapa

### Observações:
- As guardas são independentes: mesmo um superusuário não escreve numa
  transação `read only`; o parsing de SQL **não** é usado como defesa.
- PULA (SKIP) sem Docker.

**Executado em:** [Data e hora do teste]

## Como executar

```sh
./run.sh
```
