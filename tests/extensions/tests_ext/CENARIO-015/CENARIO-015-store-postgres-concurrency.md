# Cenário 015: `mhl-store-postgres` sob concorrência

**Objetivo:** Verificar o comportamento concorrente do backend PostgreSQL:

- **A — fan-out:** um grupo `parallel` de 8 `put` em chaves distintas → 8 linhas,
  sem perda.
- **B — lost update:** 8 branches fazendo `get` + `put` na **mesma** chave →
  valor final `< 8`. É uma limitação **do contrato `store` v1** (não do
  Postgres): `get` e `put` são duas chamadas; sem um método CAS/atômico, a
  janela entre elas corre. A versão sequencial resulta em 8.
- **C — integridade do upsert:** 8 branches fazendo apenas `put` (sem `get`) na
  mesma chave → **exatamente 1 linha**, com um dos valores escritos, nunca
  corrompida. `INSERT ... ON CONFLICT DO UPDATE` é atômico por linha.

```gherkin
# A
Quando parallel { 8x S.put("k/<i>", 1) } e depois S.list("k/")
Então list devolve as 8 chaves e a tabela tem 8 linhas sob k/

# B
Quando parallel { 8x [ var v = S.get("n") ; sleep ; S.put("n", v+1) ] }
Então S.get("n") < 8            # updates perdidos (contrato v1 sem CAS)
E a mesma lógica sequencial resulta em 8

# C
Quando parallel { S.put("c", 1) ... S.put("c", 8) }
Então existe exatamente 1 linha "c", com value entre 1 e 8
```

**Resultado Esperado:**
- **A:** `S.list("k/")` = 8 chaves; `SELECT count(*) ... LIKE 'k/%'` = 8; rc 0.
- **B:** paralelo → `n` final `< 8`; sequencial → `n` final `== 8`.
- **C:** `SELECT count(*) WHERE key='c'` = 1; valor ∈ [1,8].

**Resultado Real:**
- [ ] funcionou
- [ ] não funcionou

### Evidências:
- [ ] `logs/run-a.out`, `logs/count-a.txt`
- [ ] `logs/run-b-par.out`, `logs/run-b-seq.out`
- [ ] `logs/run-c.out`, `logs/row-c.txt`
- [ ] `logs/wire-*.jsonl`

### Observações:
- Paralelo ao CENARIO-012 (S3), para comparação direta. A diferença: no
  Postgres, o **B** poderia ser resolvido com um `store` v2 (método CAS, ou um
  `UPDATE ... SET value = value + 1`); no S3 não há primitiva equivalente.
- O `mcpserver` evita o B no caminho `serve` dando a cada run chaves
  `run/<id>/…` disjuntas (ver CENARIO-016).
- PULA (SKIP) sem Docker.

**Executado em:** [Data e hora do teste]

## Como executar

```sh
./run.sh
```
