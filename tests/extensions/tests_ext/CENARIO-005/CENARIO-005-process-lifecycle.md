# Cenário 005: Ciclo de vida do processo da extensão

**Objetivo:** Verificar que o processo da extensão é **iniciado uma vez** (na
primeira chamada), **reusado** por todos os steps do run, e **encerrado** ao
fim — nunca um processo por chamada.

```gherkin
Dado um .mh com "extension store S { log }" e vários steps que chamam S
Quando o pipeline roda
Então probe.jsonl tem exatamente 1 "ev":"init", N "ev":"call" (um por chamada),
  e 1 "ev":"shutdown"
E todos os "call" vêm do mesmo pid (um único processo)
```

**Resultado Esperado:**
- `probe.jsonl`: 1× `init`, 7× `call`, 1× `shutdown` (`via:"notify"` ou `via:"eof"`).
- Um único `pid` (registrado na linha `init`).

**Resultado Real:**
- [ ] funcionou
- [ ] não funcionou

### Evidências:

- [ ] `logs/probe.jsonl`

### Observações:
- O shutdown gracioso é **best-effort**: o host envia a notificação `shutdown` e
  fecha o stdin; se a extensão não sair em poucos µs ele manda `SIGKILL`. A
  `store-probe` grava a linha `shutdown` de forma síncrona (append) para não perdê-la.

**Executado em:** [Data e hora do teste]

## Como executar

```sh
./run.sh
```
