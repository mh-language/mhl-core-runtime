# Cenário 003: Allow-list do lock e pin de hash

**Objetivo:** Verificar que uma extensão só carrega se estiver no
`.mhl/extensions.lock` **e** o hash do executável bater com o pin — o mecanismo
de confiança do mhl.

```gherkin
Dado store-probe instalado e fixado no lock
Quando o lock é esvaziado (extensão fora da allow-list)
Então a extensão não carrega e uma chamada S.put falha / avisa
Quando o lock é restaurado mas o binário vendido é adulterado
Então o mhl recusa a extensão por hash divergente e "mhl extension doctor" sai != 0
```

**Resultado Esperado:**
- Lock vazio (`{"extensions":{}}`): `mhl run` emite `warning: extension ... not loaded` / a
  chamada resulta em erro `no extension registered for kind "store"` (a extensão não entra no registry).
- Binário adulterado (1 byte a mais) com o lock original: `mhl run` / `mhl extension doctor`
  reportam divergência de `sha256`; `doctor` sai com código != 0.
- Lock + binário íntegros (controle): carrega normalmente.

**Resultado Real:**
- [ ] funcionou
- [ ] não funcionou

### Evidências:

- [ ] `logs/run-nolock.out`, `logs/run-tampered.out`, `logs/doctor-tampered.log`, `logs/run-ok.out`

### Observações:
- "An extension present on disk but absent from the lock does not load" — a lock é a
  allow-list explícita do projeto. Não há download automático.

**Executado em:** [Data e hora do teste]

## Como executar

```sh
./run.sh
```
