# Cenário 002: As quatro operações do contrato `store`

**Objetivo:** Exercer `get` / `put` / `delete` / `list` de um `.mh`, incluindo os
casos de borda (get de chave ausente → `null`, delete de chave ausente = no-op,
`list(prefix)` filtra).

```gherkin
Dado a extensão store instalada
Quando um pipeline faz put de a/1 e b/1, get de ambos, get de ausente,
  delete de a/1, delete de ausente, list("") e list("a/")
Então os valores voltam corretos, a chave ausente é null, o delete ausente
  não falha, list("") reflete o estado e list("a/") filtra por prefixo
```

**Resultado Esperado:**
- `get("a/1")` → `10`, `get("b/1")` → `20`, `get("z/9")` → `null`.
- `delete("a/1")` remove; `delete("nao/existe")` retorna sem erro.
- `list("")` → `["b/1"]` (após o delete); `list("a/")` → `[]`; antes do delete `list("a/")` → `["a/1"]`.

**Resultado Real:**
- [ ] funcionou
- [ ] não funcionou

### Evidências:

- [ ] `logs/run.out`, `logs/probe.jsonl`, `logs/store-tree.txt`

### Observações:
- Contrato v1 do `store`: sem ordenação garantida, sem TTL, sem transação.

**Executado em:** [Data e hora do teste]

## Como executar

```sh
./run.sh
```
