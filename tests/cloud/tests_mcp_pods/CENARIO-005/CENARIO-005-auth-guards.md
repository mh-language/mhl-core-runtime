# Cenário 005: Autenticação por bearer e guarda de Origin

**Objetivo:** Verificar o guard do endpoint de protocolo do pod: `POST /mcp` só
responde com o bearer correto (`--token` / `MHL_SERVE_TOKEN`), rejeita Origin
cross-site não-loopback, e o servidor avisa quando sobe sem token num bind não
local.

```gherkin
Dado que o servidor MCP está em execução com --token
Quando um cliente chama POST /mcp sem bearer ou com bearer errado
Então o servidor responde 401
E com o bearer correto responde 200
E uma requisição com Origin cross-site não-loopback recebe 403 "forbidden origin"
E subir o servidor em 0.0.0.0 sem --token imprime um aviso de endpoint não autenticado
```

**Resultado Esperado:**
- `POST /mcp` sem `Authorization` → `401`; com token errado → `401`; com token correto → `200`.
- `GET /healthz` e `GET /metrics` → `200` mesmo sem bearer (não são protegidos).
- `POST /mcp` com `Origin: http://evil.example` → `403` `forbidden origin`;
  `Origin: http://localhost` → aceito (loopback).
- Um segundo processo iniciado com `--addr 0.0.0.0:<porta>` e **sem** `--token`
  escreve em stderr: `warning: binding 0.0.0.0:... with no --token/... — the endpoint is unauthenticated`.

**Resultado Real:**
- [ ] funcionou
- [ ] não funcionou

### Evidências:

- [ ] Log do servidor MCP
- [ ] Matriz de códigos HTTP por cenário de credencial/Origin
- [ ] stderr do processo sem token com o aviso

### Observações:
- Referência de design: itens 03 e 10 (identidade / token único). Aqui só se testa o
  guard estático do pod, não JWT/JWKS.
- O guard de Origin (`originAllowed`) só bloqueia Origin **presente** e não-loopback;
  clientes sem navegador normalmente não enviam Origin.

**Executado em:** [Data e hora do teste]

## Como executar

```sh
./run.sh
```
