# Cenário 001: Carregar uma extensão e chamá-la de um `.mh`

**Objetivo:** Verificar que a linguagem mhl carrega uma extensão externa `store`
instalada num projeto (`.mhl/extensions.lock` + `.mhl/extensions/<id>/`) e que
um `step` consegue chamar seus métodos e usar o valor retornado.

```gherkin
Dado que store-probe foi instalado no projeto (mhl extension install)
E um .mh declara "extension store S { dir, log }"
Quando um step chama S.put("k","v") e outro chama S.get("k")
Então S.get retorna "v", S.get de uma chave ausente retorna null
E um arquivo JSON aparece sob dir, e probe.jsonl registra init + 3 call
E "mhl extension doctor" reporta a extensão como OK
```

**Resultado Esperado:**
- `mhl extension install` fixa o hash no lock; `mhl extension doctor` sai `0`.
- `S.get("greeting")` → `hello-ext`; `S.get("nope")` → `null`.
- `<dir>/greeting.json` existe com o valor.
- `probe.jsonl`: 1 linha `"ev":"init"`, 3 linhas `"ev":"call"` (put, get, get).

**Resultado Real:**
- [ ] funcionou
- [ ] não funcionou

### Evidências:

- [ ] `logs/run.out` — saída de `mhl run`
- [ ] `logs/probe.jsonl` — log da extensão
- [ ] `logs/store-tree.txt` — árvore de `dir`
- [ ] `logs/extension-doctor.log`

### Observações:
- O caminho da linguagem (`S.put("k","v")`) envia args **posicionais**; a
  `store-probe` resolve chave/valor de posicional ou nomeado.

**Executado em:** [Data e hora do teste]

## Como executar

```sh
./run.sh
```
