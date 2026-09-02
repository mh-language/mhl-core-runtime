# Cenário 020: `mhl-cache-redis` — carregar de um `.mh` e as operações de cache

**Objetivo:** Verificar que a extensão **oficial** `src/mhl-extensions/mhl-cache-redis`
(kind `cache`) é carregada por um `.mh` e que `get` / `set` / `has` / `delete`
/ `incr` / `incrBy` fazem round-trip real contra o Redis — confirmado com
`redis-cli`. Valores objeto passam por JSON; `incr`/`incrBy` são inteiros
nativos.

```gherkin
Dado um Redis local (docker compose de src/mhl-extensions/mhl-cache-redis) e a extensão instalada
E um .mh declara "extension cache C { url: env(...), key_prefix, ttl }"
Quando um step faz C.get("user:1")            → null (ausente)
E C.set("user:1", {name:"Ana", score:91})     e C.get("user:1") → o objeto
E C.has("user:1")                             → true
E C.incr("hits") e C.incrBy("hits", 4)        → 1, depois 5
E C.delete("user:1") e C.has("user:1")        → false
Então `redis-cli GET <prefix>user:1` some; `redis-cli GET <prefix>hits` == "5"
E a wire trace tem "op":"set" e "op":"incr" (sem valores)
E "mhl extension doctor" sai 0
```

**Resultado Esperado:**
- Linhas de saída: `null` / `{"name":"Ana","score":91}` / `true` / `1` / `5` / `false`.
- `redis-cli EXISTS <prefix>user:1` → `0`; `redis-cli GET <prefix>hits` → `"5"`.
- `logs/wire.jsonl`: `"ev":"init"`, `"op":"set"`, `"op":"incr"`.

**Resultado Real:**
- [ ] funcionou
- [ ] não funcionou

### Evidências:
- [ ] `logs/run.out`, `logs/wire.jsonl`
- [ ] `logs/redis-dump.txt` — chaves e valores no Redis ao fim
- [ ] `logs/doctor.out`

### Observações:
- Kind `cache` — diferente de `store`/`sql`; convive com as outras extensões.
- Exige Docker; **PULA** (SKIP, não FAIL) sem ele.
- `incr`/`incrBy` só em chaves com inteiro; a wire trace grava `op` + `key`,
  nunca o valor.

**Executado em:** [Data e hora do teste]

## Como executar

```sh
./run.sh
```
