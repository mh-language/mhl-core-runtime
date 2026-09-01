# Cenário 007: Concorrência — `parallel` de N `put` + `list`

**Objetivo:** Verificar que um grupo `parallel` do mhl dispara N chamadas à
extensão que rodam **de fato concorrentes** (o host multiplexa por id; a
`store-probe` atende com uma goroutine por chamada sob um mutex de storage) e
que nenhuma escrita se perde.

```gherkin
Dado "extension store S { latency_ms: 100, log }"
E um pipeline com "parallel Fan { step a..h { S.put(\"k/<x>\", n) } }" (8 branches)
E um step final S.list("k/")
Quando o pipeline roda
Então list("k/") devolve as 8 chaves e os 8 arquivos existem
E as janelas [t, t+dur_us] das 8 chamadas put no probe.jsonl se sobrepõem
  (executaram concorrentes, não em série)
E, num controle com "serial: true", o tempo de parede ~8×latência em vez de ~1×
```

**Resultado Esperado:**
- `S.list("k/")` → 8 chaves; 8 `*.json` sob `dir`.
- No `probe.jsonl` concorrente: `max(t) - min(t)` entre os 8 `put` << `8 × 100 ms`
  e cada `dur_us` ≈ 100 000 — ou seja, sobrepostas.
- Controle `serial: true`: `sum(dur_us)` ≈ `max(end) - min(start)` (uma de cada vez).

**Resultado Real:**
- [ ] funcionou
- [ ] não funcionou

### Evidências:

- [ ] `logs/probe-concurrent.jsonl`, `logs/probe-serial.jsonl`, `logs/timing.txt`

### Observações:
- O host (`internal/extension/external/process.go`) sempre multiplexa por id; a
  serialização (ou não) é escolha da extensão. `store-fs` é serial; `store-probe`
  é concorrente por padrão.

**Executado em:** [Data e hora do teste]

## Como executar

```sh
./run.sh
```
