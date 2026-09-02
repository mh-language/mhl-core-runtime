# Cenário 004: Probes de saúde (liveness / readiness)

**Objetivo:** Verificar que o pod expõe `GET /healthz` e `GET /readyz` e que esses
endpoints (mais `GET /metrics`) respondem **sem** autenticação, como um kubelet os
consome.

```gherkin
Dado que o servidor MCP está em execução
Quando um probe faz GET /healthz e GET /readyz sem enviar bearer token
Então /healthz responde 200 "ok" e /readyz responde 200 "ready"
E GET /metrics também responde 200 sem bearer
E POST /mcp sem bearer continua respondendo 401
```

**Resultado Esperado:** `/healthz` → `200` `ok`; `/readyz` → `200` `ready`;
`/metrics` → `200` (texto Prometheus); os três não exigem `Authorization`. O
endpoint de protocolo `POST /mcp` continua protegido (`401` sem token). A
transição de `/readyz` para `503` durante o drain é coberta pelo CENÁRIO-009.

**Resultado Real:**
- [ ] funcionou
- [ ] não funcionou

### Evidências:

- [ ] Log do servidor MCP mostrando o endpoint no ar
- [ ] Códigos HTTP e corpo de `/healthz`, `/readyz`, `/metrics` (com e sem bearer)

### Observações:
- Referência de design: item 06 ("No health or readiness endpoints") — já implementado.
- `run.sh` sobe o servidor com `--token`; nenhum dos probes envia o token.

**Executado em:** [Data e hora do teste]

## Como executar

```sh
./run.sh
```

Artefatos em `./logs/`. O script copia `tests/cloud/mhl` para esta pasta se não existir.
