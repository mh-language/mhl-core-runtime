# tests_ext — cenários da capacidade de extensões do mhl

Mesma estrutura de `tests/cloud/tests_mcp_pods/`: um diretório por cenário, cada
um com um `.md` de especificação e um `run.sh` autocontido que grava em
`./logs/` e imprime `PASS`/`FAIL`. `run-all.sh` roda o regressivo; `REPORT.md`
consolida.

**O que se testa:** a capacidade da linguagem mhl de carregar e usar **extensões
externas** (`extension <kind> <Name> { ... }`), com foco no `StateStore` (kind
`store`) — o backend de estado durável que `mhl serve mcp --http` usa. Inclui
**cenários de concorrência**: um grupo `parallel` disparando chamadas à
extensão, o gargalo serial-vs-concorrente, um processo compartilhado por várias
declarações, restart em crash no meio da concorrência, e o caminho `serve` com
`run/start` concorrentes backed pela extensão. Os cenários **011+** repetem o
essencial contra a extensão **oficial** `src/mhl-extensions/mhl-store-s3` (backend Amazon S3)
usando um MinIO local.

## Sujeitos de teste

- `../store-probe/` — um `store` instrumentado (superset de `../store-fs`): mesmo
  contrato (get/put/delete/list, um JSON por chave sob `dir`), com knobs lidos
  das **propriedades da declaração**: `log` (uma linha JSON por mensagem),
  `latency_ms` (sleep por chamada), `crash_after` (exit 1 após N chamadas),
  `serial` (uma chamada por vez vs. goroutine por chamada). Cenários **001–010**.
- `../../../src/mhl-extensions/mhl-store-s3/` — a extensão **oficial** `store` sobre S3 (SigV4
  sem dependências), com `docker-compose.yml` de MinIO. Cenários **011–013**.
- `../../../src/mhl-extensions/mhl-store-postgres/` — a extensão **oficial** `store` sobre
  PostgreSQL (`pgx/v5`; `put` = upsert atômico), com `docker-compose.yml` de
  Postgres 16. Cenários **014–016**.
- `../../../src/mhl-extensions/mhl-sql-postgres/` — a extensão **oficial** de kind `sql`:
  consultas livres (DQL) e, com `read_only:false`, DML/DDL (`exec` /
  `execScript` transacional) contra PostgreSQL. Postgres semeado
  (`initdb/seed.sql`). Cenários **017–019**.
- `../../../src/mhl-extensions/mhl-cache-redis/` — a extensão **oficial** de kind `cache`:
  cache TTL-first sobre Redis (cliente RESP2 sem dependências). Cenários
  **020–021**.

## Pré-requisitos

- `go` (para compilar `store-probe` / `mhl-store-s3` / `mhl-store-postgres` sob demanda em `bin/`);
- `tests/extensions/mhl` — cópia do binário de referência
  (`cp tests/cloud/mhl tests/extensions/mhl`). No macOS os scripts re-assinam
  ad-hoc (`codesign --force --sign -`) binário recém-compilado/copiado.
- **cenários 011+**: Docker (MinIO de `src/mhl-extensions/mhl-store-s3/`, Postgres de
  `src/mhl-extensions/mhl-store-postgres/`). Sem Docker, esses cenários **PULAM** (SKIP, não FAIL).

Cada `run.sh` monta um projeto scratch (`mktemp -d`), faz `mhl extension install`
da extensão nele (cria `.mhl/extensions.lock` + `.mhl/extensions/<id>/`),
escreve o `.mh`, roda, e valida saída + estado do store + wire trace. Os
cenários S3 conferem os objetos direto no bucket com `mc`.

## Cenários

| # | Foco |
|---|---|
| 001 | Carregar a extensão de um `.mh` e chamar `put`/`get` (hit + miss→null); `mhl extension doctor` |
| 002 | As 4 operações do contrato `store` (get miss, delete ausente, list com prefixo) |
| 003 | Allow-list do lock: fora do lock não carrega; drift de hash → recusa; `doctor` non-zero |
| 004 | `env()` em propriedade da extensão (`dir: env(...)`) — resolve; unset → falha fechada |
| 005 | Ciclo de vida do processo: 1 `init`, N `call`, `shutdown`; um processo reusado entre steps |
| 006 | `crash_after` + `maxRestarts`: respawn 3× e o run falha ("keeps exiting"); chamadas em voo recebem erro |
| 007 | **Concorrência**: `parallel` de N `put` + `list` → N chaves, sem perda; janelas de execução sobrepostas |
| 008 | **Concorrência**: read-modify-write concorrente → lost update (o `store` v1 não tem CAS/lease); sequencial = N |
| 009 | **Concorrência**: N declarações do mesmo kind compartilham **um** processo; chamadas interleaved |
| 010 | Caminho `mhl serve mcp --http` com `extension store`: `run/*` checkpoints/owner/sessions na extensão; restart-reclaim; `run/start` concorrentes |
| 011 | **store-s3**: carregar a extensão oficial de um `.mh` e as 4 operações contra o MinIO — verificado com `mc ls` no bucket |
| 012 | **store-s3 / Concorrência**: `parallel` de 8 puts sem perda no bucket; read-modify-write concorrente da mesma chave → lost update (sem CAS) |
| 013 | **store-s3 / `serve`**: `mhl serve mcp --http` com estado durável num bucket S3 — resume, reclaim pós-restart do bucket, runs concorrentes com chaves disjuntas |
| 014 | **store-postgres**: carregar de um `.mh`, `auto_migrate` + as 4 operações + upsert (`ON CONFLICT`) contra o Postgres — verificado com `psql` |
| 015 | **store-postgres / Concorrência**: 8 puts paralelos sem perda; RMW concorrente da mesma chave → lost update (limite do contrato v1); 8 puts cegos na mesma chave → 1 linha íntegra (upsert atômico) |
| 016 | **store-postgres / `serve`**: `mhl serve mcp --http` com estado durável numa tabela — resume, reclaim pós-restart da tabela, runs concorrentes com chaves disjuntas |
| 017 | **sql-postgres**: carregar a extensão de kind `sql` de um `.mh`; `query`/`queryRow`/`queryValue` + tipos (`numeric`, `jsonb`) + parâmetro `$1`; nada escrito — verificado com `psql` |
| 018 | **sql-postgres / read-only**: `INSERT` via `query` recusado pelo Postgres (25006); `exec` desabilitado; `;`-stacking recusado; `max_rows` atua; `read_only:false` permite `exec` |
| 019 | **sql-postgres / DDL**: `execScript` aplica um script DDL multi-statement numa transação (tabela+índice+linhas); falha no meio → rollback total; bloqueado em `read_only:true` |
| 020 | **cache-redis**: kind `cache` de um `.mh`; `get`/`set`/`has`/`delete` + objeto JSON + `incr`/`incrBy` — verificado com `redis-cli` |
| 021 | **cache-redis / TTL + atômico**: `ttl` default/explícito/`expire`/expiração; `parallel` de 8 `incr` → **8** (atômico, sem perda) vs read-modify-write → `< 8` |

Cenários 011–013 (MinIO), 014–016 (Postgres), 017–019 (Postgres semeado) e
020–021 (Redis) sobem um serviço via `docker compose` e **PULAM** sem Docker.
`run-all.sh` derruba tudo ao final (`S3_KEEP=1` / `PG_KEEP=1` / `REDIS_KEEP=1` mantêm).
