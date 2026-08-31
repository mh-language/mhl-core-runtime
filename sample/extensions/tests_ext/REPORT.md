# Relatório — Cenários da capacidade de extensões do mhl (`tests_ext`)

Suíte que exercita `extension <kind> <Name> { ... }` na linguagem mhl, com foco
no **StateStore** (kind `store`) e em **concorrência**. Sujeitos de teste:
[`../store-probe/`](../store-probe/) — um `store` instrumentado (superset de
[`../store-fs/`](../store-fs/)) — nos cenários 001–010; e a extensão **oficial**
[`../../../src/mhl-store-s3/`](../../../src/mhl-store-s3/) — `store` sobre Amazon
S3 / MinIO — nos cenários 011–013.

| Item | Valor |
|---|---|
| Binário | `sample/extensions/mhl` (cópia de `sample/cloud/mhl`, `v1.1.0-alpha-7-g418d7dc`) |
| Extensões | `dev.mhl.store-probe` 0.1.0 · `dev.mhl.store-s3` 0.1.0 (`store` — get/put/delete/list) |
| Data | 2026-08-31 |
| Executados | 13 |
| Aprovados | 13 |
| Reprovados | 0 |

## Cenários

| Código | Descrição | Status | Cenário | Script |
|---|---|---|---|---|
| CENARIO-001 | Carregar a extensão de um `.mh` e chamar `put`/`get` (hit + miss→null); `mhl extension doctor`. | ✅ | [md](CENARIO-001/CENARIO-001-load-and-call.md) | [run.sh](CENARIO-001/run.sh) |
| CENARIO-002 | As 4 operações do contrato `store` (get miss→null, delete ausente no-op, `list(prefix)`). | ✅ | [md](CENARIO-002/CENARIO-002-four-ops.md) | [run.sh](CENARIO-002/run.sh) |
| CENARIO-003 | Allow-list do lock: fora do lock não carrega; drift de hash → recusa; `doctor` != 0. | ✅ | [md](CENARIO-003/CENARIO-003-lock-allowlist.md) | [run.sh](CENARIO-003/run.sh) |
| CENARIO-004 | `env()` em propriedade da extensão (`dir: env(...)`) — resolve; unset → falha fechada. | ✅ | [md](CENARIO-004/CENARIO-004-env-props.md) | [run.sh](CENARIO-004/run.sh) |
| CENARIO-005 | Ciclo de vida do processo: 1 `init`, N `call`, `shutdown`; um processo reusado. | ✅ | [md](CENARIO-005/CENARIO-005-process-lifecycle.md) | [run.sh](CENARIO-005/run.sh) |
| CENARIO-006 | Crash no meio de um run: erro diagnóstico (extensão + exit status + stderr), sem deadlock, seq e parallel. `maxRestarts` é guarda de boot-loop (não alcançável de um `.mh` comum — ver observações do `.md`). | ✅ | [md](CENARIO-006/CENARIO-006-crash-restart.md) | [run.sh](CENARIO-006/run.sh) |
| CENARIO-007 | **Concorrência**: `parallel` de 8 `put` + `list` → 8 chaves, sem perda; janelas sobrepostas (`overlapped=yes`); controle `serial` ~3× mais lento (`overlapped=no`). | ✅ | [md](CENARIO-007/CENARIO-007-parallel-puts.md) | [run.sh](CENARIO-007/run.sh) |
| CENARIO-008 | **Concorrência**: read-modify-write concorrente → **lost update** (`store` v1 sem CAS/lease; final < N); sequencial = N. Limitação documentada. | ✅ | [md](CENARIO-008/CENARIO-008-lost-update.md) | [run.sh](CENARIO-008/run.sh) |
| CENARIO-009 | **Concorrência**: 3 declarações do mesmo kind compartilham **um** processo (1 `init`); 12 puts interleaved sem perda. | ✅ | [md](CENARIO-009/CENARIO-009-shared-process.md) | [run.sh](CENARIO-009/run.sh) |
| CENARIO-010 | `mhl serve mcp --http` com `extension store`: `run/*`+`session/*` na extensão; `run/resume`; **restart-reclaim** do store; múltiplas runs com chaves `run/<id>` disjuntas. | ✅ | [md](CENARIO-010/CENARIO-010-serve-statestore.md) | [run.sh](CENARIO-010/run.sh) |
| CENARIO-011 | **store-s3**: carregar a extensão oficial de um `.mh`; get/put/delete/list round-trip contra o MinIO, verificado com `mc ls` no bucket; `extension doctor`. | ✅ | [md](CENARIO-011/CENARIO-011-store-s3-minio.md) | [run.sh](CENARIO-011/run.sh) |
| CENARIO-012 | **store-s3 / Concorrência**: `parallel` de 8 puts → 8 objetos no bucket, sem perda; read-modify-write concorrente da mesma chave → **lost update** (par < 8, seq = 8). | ✅ | [md](CENARIO-012/CENARIO-012-store-s3-concurrency.md) | [run.sh](CENARIO-012/run.sh) |
| CENARIO-013 | **store-s3 / `serve`**: `mhl serve mcp --http` com estado durável num **bucket S3**; `run/resume`; reclaim pós-restart lendo do bucket; `run/start` concorrentes com chaves `run/<id>` disjuntas. | ✅ | [md](CENARIO-013/CENARIO-013-store-s3-serve.md) | [run.sh](CENARIO-013/run.sh) |

Regressivo: `./run-all.sh` → [REGRESSION.md](REGRESSION.md) (2026-08-31: **13 PASS / 0 FAIL**; 011–013 PULAM sem Docker).

## Achados

1. **`store-fs` (o `store` de referência) não funciona pelo caminho da linguagem.**
   `S.put("k","v")` num `.mh` envia args **posicionais**; `store-fs` só lê `named_args`
   (foi feito para o caminho `mhl serve`). `store-probe` trata os dois.
2. **`extension store` v1 não tem CAS/lease** — read-modify-write concorrente na mesma
   chave perde updates (CENARIO-008). O `mcpserver` contorna usando chaves `run/<id>/…`
   disjuntas por run (confirmado no CENARIO-010); mutação concorrente da **mesma** chave
   precisaria de CAS no protocolo (Phase 4, per `store-fs/README`).
3. **`maxRestarts` / "keeps exiting" quase não é alcançável de um `.mh`**: o respawn só
   ocorre numa nova chamada que encontra o processo morto, e a primeira chamada com
   falha já aborta o run. É uma guarda de boot-loop, não um mecanismo de retry.
4. **Uma extensão = um processo = uma config**: N declarações `extension store` do mesmo
   kind compartilham o processo e a `store-probe` pina `dir`/`log` da **primeira** chamada
   (CENARIO-009). O caminho `serve` recusa > 1 declaração `extension store`.
5. **Shutdown gracioso é best-effort** — o host notifica `shutdown` e pode `SIGKILL` quase
   em seguida; a extensão precisa gravar de forma síncrona (CENARIO-005).
6. **A extensão oficial `mhl-store-s3` cobre o caminho `serve` de ponta a ponta**
   (CENARIO-013): declarar `extension store S { bucket, endpoint, ... }` no dir de
   workflows faz `mhl serve mcp --http` gravar sessões e checkpoints de `run/*` como
   objetos S3 (`<prefix><key>.json`), com `run/resume` e reclaim pós-restart lendo do
   bucket. Herda a limitação do Achado #2 (sem CAS): CENARIO-012/B mostra lost update
   na **mesma** chave; o `mcpserver` contorna com chaves `run/<id>/…` disjuntas por run.
7. **`env()` em propriedade de extensão vale para credenciais** (CENARIO-011/013):
   `access_key_id`/`secret_access_key`/`bucket`/`endpoint` vêm de `env(...)`, resolvidos
   host-side pelo runtime (e registrados para redação); o processo da extensão não herda
   ambiente — `permissions.secrets` fica vazio.

## Observações

- Nenhum código-fonte do runtime foi alterado. `store-probe` é uma extensão de
  amostra nova (não é o runtime nem o servidor MCP).
- Cada `run.sh` compila `store-probe` sob demanda, monta um projeto scratch
  (`mktemp -d`), faz `mhl extension install`, e limpa ao fim.
- macOS: binários recém-compilados/copiados são re-assinados ad-hoc
  (`codesign --force --sign -`) para não levar `SIGKILL` do Gatekeeper.
