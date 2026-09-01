# Cenário 012: `mhl-store-s3` sob concorrência

**Objetivo:** Verificar que o backend S3 aguenta chamadas concorrentes de um
grupo `parallel` sem perder objetos (dispatch goroutine-por-chamada +
`http.Client` reusado), **e** que ele herda a mesma limitação v1 do kind
`store`: read-modify-write concorrente da **mesma** chave perde updates (não há
CAS/lease).

```gherkin
# Parte A — fan-out
Dado "extension store S { ... , prefix: c012a/ }" apontando para o MinIO
Quando um grupo parallel faz 8 S.put em chaves distintas k/a..k/h
E um step seguinte faz S.list("k/")
Então list retorna as 8 chaves
E `mc ls` mostra exatamente 8 objetos .json sob c012a/k/
E mhl run sai 0 (nenhuma corrida de I/O de rede quebrou a extensão)

# Parte B — lost update
Dado a mesma extensão com prefix c012b/<modo>/
Quando 8 branches de um grupo parallel fazem  var v = S.get("n") ; sleep ; S.put("n", v+1)
Então o valor final é < 8 (updates perdidos — store v1 sem CAS)
E a versão sequencial do mesmo read-modify-write resulta em 8
```

**Resultado Esperado:**
- **A:** `S.list("k/")` = `["k/a","k/b","k/c","k/d","k/e","k/f","k/g","k/h"]`;
  `mc ls` conta 8 objetos; `mhl run` rc 0.
- **B:** paralelo → `n` final `< 8`; sequencial → `n` final `== 8`.

**Resultado Real:**
- [ ] funcionou
- [ ] não funcionou

### Evidências:
- [ ] `logs/run-a.out`, `logs/mc-a.txt` — fan-out + objetos no bucket
- [ ] `logs/run-b-par.out`, `logs/run-b-seq.out` — lost update vs sequencial
- [ ] `logs/wire-a.jsonl`, `logs/wire-b.jsonl` — traces da extensão

### Observações:
- `store-s3` não tem knob `latency_ms`; a janela do read-modify-write é
  alargada com `cmd.exec(["sh","-c","sleep 0.05"])` entre o `get` e o `put`,
  tornando o lost update determinístico.
- O `mcpserver` evita exatamente isto no caminho `serve` dando a cada run
  chaves `run/<id>/…` disjuntas (ver CENARIO-013). Mutação concorrente da
  **mesma** chave precisaria de CAS no protocolo do `store` (v2).
- PULA (SKIP) sem Docker.

**Executado em:** [Data e hora do teste]

## Como executar

```sh
./run.sh
```
