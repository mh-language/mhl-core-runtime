# Cenário 009: Concorrência — várias declarações, um processo

**Objetivo:** Verificar que N declarações `extension store` do mesmo kind
compartilham **um único** processo de extensão (iniciado uma vez, na primeira
chamada), e que chamadas concorrentes de declarações diferentes são
multiplexadas sem corrupção.

```gherkin
Dado três declarações "extension store A/B/C { log: <o mesmo arquivo> }"
E um grupo parallel que chama A.put, B.put e C.put ao mesmo tempo (×4 cada)
Quando o pipeline roda
Então probe.jsonl tem exatamente 1 "ev":"init" (um processo)
E as linhas "ev":"call" têm "decl":"A" | "B" | "C" intercaladas
E as 12 escritas estão todas presentes
```

**Resultado Esperado:**
- 1× `"ev":"init"` no `probe.jsonl` compartilhado (um processo para as 3 declarações).
- 12× `"ev":"call"` com `decl` ∈ {A,B,C}, ordem entrelaçada.
- `S.list("")` de qualquer declaração enxerga as 12 chaves.

**Resultado Real:**
- [ ] funcionou
- [ ] não funcionou

### Evidências:

- [ ] `logs/probe.jsonl`, `logs/run.out`

### Observações:
- "One process is shared by every declaration of the kinds this extension serves."
- **Consequência da `store-probe`:** ela pina `dir` (e `log`) via `sync.Once` na
  **primeira** chamada — então as props de `dir` das declarações B/C são ignoradas
  e tudo cai no `dir` da que foi chamada primeiro. O caminho `serve` nunca tem
  mais de uma declaração `extension store` (o host recusa >1). Aqui o cenário
  fixa a semântica de processo/config compartilhados.

**Executado em:** [Data e hora do teste]

## Como executar

```sh
./run.sh
```
