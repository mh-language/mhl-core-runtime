# Cenário 004: `env()` numa propriedade da extensão

**Objetivo:** Verificar que uma propriedade da declaração `extension` pode ser
`env("VAR")` — resolvida pelo avaliador de expressões / `auth.Resolve` — e que
uma variável não definida faz o run **falhar fechado** antes de a extensão ser
usada.

```gherkin
Dado um .mh com "extension store S { dir: env(\"STORE_PROBE_DIR\") }"
Quando STORE_PROBE_DIR está definida e aponta para um diretório
Então o run funciona e os arquivos aparecem lá
Quando STORE_PROBE_DIR não está definida
Então mhl run falha com um erro citando env("STORE_PROBE_DIR")
```

**Resultado Esperado:**
- Com `STORE_PROBE_DIR=<tmp>`: `S.put`/`S.get` funcionam; `<tmp>/k.json` existe.
- Sem `STORE_PROBE_DIR`: `mhl run` sai != 0 com mensagem mencionando `env("STORE_PROBE_DIR")`
  (credencial/variável não resolvida) — a extensão **não** é chamada.

**Resultado Real:**
- [ ] funcionou
- [ ] não funcionou

### Evidências:

- [ ] `logs/run-with-env.out`, `logs/run-no-env.out`

### Observações:
- Vale para o caminho da linguagem (`evalExtensionCall` → `resolveExtensionDeclaration`,
  que roda `ast.CredentialRefs` + `auth.Resolve` fail-closed) e para o caminho `serve`
  (`resolveStoreProps`).

**Executado em:** [Data e hora do teste]

## Como executar

```sh
./run.sh
```
