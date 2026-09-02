# Cenário 008: Concorrência — read-modify-write concorrente (limitação do `store` v1)

**Objetivo:** Demonstrar, de forma inequívoca, que o contrato `store` v1
(get/put/delete/list, sem compare-and-swap nem lease) **não** garante um
incremento correto quando várias branches `parallel` fazem
`get → +1 → put` no mesmo contador — e que a mesma sequência **sequencial**
produz o valor certo.

```gherkin
Dado "extension store S { latency_ms: 40, log }" e counter=0
Quando 8 branches de um grupo parallel fazem cada uma:
      var c = S.get("counter")  ;  S.put("counter", c + 1)
Então S.get("counter") final é < 8 (lost update — todas leram o mesmo 0)
Quando os mesmos 8 incrementos são feitos em steps sequenciais
Então S.get("counter") final é exatamente 8
```

**Resultado Esperado:**
- Ramo concorrente: `counter` final **< 8** (com `latency_ms` forçando todas a
  lerem `0` antes de qualquer `put`, o valor final costuma ser `1`).
- Ramo sequencial: `counter` final **== 8**.

**Resultado Real:**
- [ ] funcionou
- [ ] não funcionou

### Evidências:

- [ ] `logs/run-parallel.out`, `logs/run-serial.out`, `logs/probe-parallel.jsonl`

### Observações:
- Do `store-fs/README`: *"It never assumes ordering, TTL, or transactions in 3a;
  CAS / lease come with distributed run execution (Phase 4)."* Este cenário fixa
  essa limitação como comportamento observado, não como bug.
- **Implicação para o StateStore:** múltiplos pods/goroutines mutando a mesma
  chave (`run/<id>/checkpoint/...`) precisam de CAS/lease no protocolo — ou de
  chaves disjuntas por run (o que o `mcpserver` já faz hoje).

**Executado em:** [Data e hora do teste]

## Como executar

```sh
./run.sh
```
