# Cenário 006: Crash da extensão no meio de um run

**Objetivo:** Verificar que, quando o processo da extensão morre no meio de um
run, o host **falha o run com um erro diagnóstico** (nomeia a extensão, o
`exit status` e anexa o tail do stderr da extensão) e a execução **termina sem
travar** — tanto para uma chamada sequencial quanto dentro de um grupo
`parallel`.

```gherkin
Dado "extension store S { crash_after: 0, log }" (a extensão sai com exit(1) na 1ª chamada)
Quando um step faz S.put(...)
Então mhl run falha com "S.put: extension process is not running: exit status 1"
E o erro traz o tail do stderr da extensão ("crash_after=0 reached — exiting 1")
E probe.jsonl registra "ev":"init" e "ev":"crash"
E o comando retorna em poucos segundos (guarda de 60 s não atua)

Dado "extension store S { crash_after: 3, log }" e um grupo parallel de 12 S.put
Quando a extensão morre no meio do grupo
Então o grupo falha de imediato com "extension process is not running"
E a execução retorna rápido (as branches em voo recebem erro, não travam)
```

**Resultado Esperado:**
- Sequencial: `mhl run` sai != 0; `run-seq.out` contém `extension process is not running`
  e `crash_after=0 reached`; `probe-seq.jsonl` tem `"ev":"init"` e `"ev":"crash"`;
  elapsed < ~10 s.
- Parallel: `mhl run` sai != 0; `run-par.out` contém `extension process is not running`;
  elapsed < ~15 s.

**Resultado Real:**
- [ ] funcionou
- [ ] não funcionou

### Evidências:

- [ ] `logs/run-seq.out`, `logs/probe-seq.jsonl`, `logs/run-par.out`, `logs/probe-par.jsonl`

### Observações:
- `maxRestarts = 3` (em `internal/extension/external`) é uma **guarda de
  boot-loop**: o respawn só acontece numa **nova** chamada que encontra o
  processo morto; não há retry da chamada que falhou, e um step que erra aborta
  o run. Por isso a mensagem "keeps exiting (restarted 3 times)" praticamente não
  é alcançável a partir de um `.mh` comum — a primeira chamada com falha já
  encerra o run. Este cenário fixa o que é observável: o erro é claro e não trava.

**Executado em:** [Data e hora do teste]

## Como executar

```sh
./run.sh
```
