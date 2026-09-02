# ITEM-11 (alvo): `run/*` anunciado e negociável

**Item do design:** 11 — *`run/*` is a non-standard extension* (aberto).
**Estado hoje:** `initialize` anuncia só `capabilities.tools`; nenhum campo
descreve `run/*`, e não há versão nem descoberta.

## Comportamento-alvo

```gherkin
Quando initialize
Então result.capabilities descreve a execução assíncrona com uma versão explícita
  (ex.: capabilities.experimental["mhl.run"] = {"version":"1", "methods":[...]}
   ou capabilities["run"] = {...})

Quando tools/list
Então cada tool que representa um pipeline longo traz uma anotação/`_meta`
  indicando que deve ser chamada via run/* (não bloqueante)

Quando um método run/* desconhecido é chamado
Então -32601 (já funciona — não regredir)
```

## Critério de aceite

1. A resposta de `initialize` tem uma chave de capability (em `experimental`,
   `run`, ou equivalente) que **nomeia** a família `run/*` **com uma versão**.
2. O conteúdo da capability lista (ou referencia) os métodos:
   `run/start`, `run/status`, `run/resume`, `run/cancel`, `run/list`, `run/logs`.
3. (opcional, ponto extra) `tools/list` sinaliza quais tools são "long-running".

## Como implementar (pistas)

- `internal/mcpserver/server.go`: no handler de `initialize`, incluir em
  `result.capabilities` algo como
  `"experimental": {"mhl.run": {"version": "1", "methods": ["run/start", ...]}}`.
- Manter a compat: clientes que ignoram `experimental` seguem funcionando.
- Acompanhar o mecanismo de progress-notification do MCP conforme estabiliza e
  migrar quando fizer sentido.

## Evidências (logs/)
- `initialize.json` (capabilities)
- `run-bogus.json` (regressão: -32601)

**Verificado por:** `./run.sh` — hoje **PENDING**.
