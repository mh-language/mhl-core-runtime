# Cenário 021: `mhl-cache-redis` — TTL e contador atômico

**Objetivo:** Verificar as duas propriedades que definem um cache:

- **A — TTL:** o `ttl` da declaração é o default de `set`; um `set(k, v, "5s")`
  sobrescreve; `ttl(k)` reporta o restante; `expire(k, …)` renova; uma chave
  com TTL curto **some** depois do prazo.
- **B — contador atômico:** um grupo `parallel` de 8 `incr("counter")` resulta
  em **exatamente 8** — `INCR` é atômico no servidor, **sem lost update**. É a
  primitiva que o contrato `store` v1 não tem (ver CENARIO-008/012B/015B).

```gherkin
# A
Dado "extension cache C { ttl: '10s' }"
Quando C.set("a", 1) e C.ttl("a")            → ~10
E C.set("b", 1, "4s") e C.ttl("b")           → ~4
E C.expire("a", "30s") e C.ttl("a")          → ~30
E C.set("blink", 1, "1s") ; espera 1.6s ; C.has("blink")  → false

# B
Quando parallel { 8x C.incr("counter") } e depois C.get("counter")
Então o valor é 8 (nenhum incremento perdido)
E a versão do mesmo teste com read-modify-write (get+set) perde updates (< 8)
```

**Resultado Esperado:**
- **A:** `ttl("a")` ∈ [8,10]; `ttl("b")` ∈ [2,4]; após `expire` `ttl("a")` ∈ [28,30];
  `has("blink")` == `false` após 1.6 s.
- **B:** `incr` paralelo → `counter` == `8`; controle `get`+`set` paralelo → `< 8`.

**Resultado Real:**
- [ ] funcionou
- [ ] não funcionou

### Evidências:
- [ ] `logs/run-a.out` — TTLs
- [ ] `logs/run-b-incr.out`, `logs/run-b-rmw.out` — atômico vs lost update
- [ ] `logs/wire.jsonl`

### Observações:
- `set` usa `PX` (expiry em ms); `expire` é de segundo (arredonda pra cima).
- A Parte B é o contraponto direto dos cenários de `store`: aqui a primitiva
  atômica existe.
- PULA (SKIP) sem Docker.

**Executado em:** [Data e hora do teste]

## Como executar

```sh
./run.sh
```
