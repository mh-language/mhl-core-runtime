# tests_ext — cenários da capacidade de extensões do mhl

Mesma estrutura de `sample/cloud/tests_mcp_pods/`: um diretório por cenário, cada
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
essencial contra a extensão **oficial** `src/mhl-store-s3` (backend Amazon S3)
usando um MinIO local.

## Sujeitos de teste

- `../store-probe/` — um `store` instrumentado (superset de `../store-fs`): mesmo
  contrato (get/put/delete/list, um JSON por chave sob `dir`), com knobs lidos
  das **propriedades da declaração**: `log` (uma linha JSON por mensagem),
  `latency_ms` (sleep por chamada), `crash_after` (exit 1 após N chamadas),
  `serial` (uma chamada por vez vs. goroutine por chamada). Cenários **001–010**.
- `../../../src/mhl-store-s3/` — a extensão **oficial** `store` sobre S3 (SigV4
  sem dependências), com `docker-compose.yml` de MinIO. Cenários **011–013**.

## Pré-requisitos

- `go` (para compilar `store-probe` / `mhl-store-s3` sob demanda em `bin/`);
- `sample/extensions/mhl` — cópia do binário de referência
  (`cp sample/cloud/mhl sample/extensions/mhl`). No macOS os scripts re-assinam
  ad-hoc (`codesign --force --sign -`) binário recém-compilado/copiado.
- **cenários 011+**: Docker (para o MinIO de `src/mhl-store-s3/docker-compose.yml`).
  Sem Docker, esses cenários **PULAM** (SKIP, não FAIL).

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

Cenários 011–013 sobem um MinIO (`docker compose` de `src/mhl-store-s3/`) e
**PULAM** sem Docker. `run-all.sh` derruba o MinIO ao final (`S3_KEEP=1` mantém).
