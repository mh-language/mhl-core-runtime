# MCP, resiliência e cache

## Servidores MCP

```mhl
mcp_server LocalTools {
    transport: "stdio"
    command: "npx"
    args: ["-y", "meu-mcp-server"]
}

mcp_server RemoteTools {
    transport: "http"
    url: "https://example.test/mcp"
    headers: {
        "Authorization": "Bearer " + env("MCP_TOKEN")
    }
}
```

O cliente MCP possui dois transportes stateless:

- `stdio`: inicia um processo para cada chamada, envia uma linha JSON-RPC e lê a resposta;
- `http`: envia um POST JSON-RPC independente, com headers configurados.

Não há handshake ou sessão persistida entre chamadas. Respostas JSON-RPC expõem erros tipados e o metadado `_meta.ttlMs` para que um cache possa respeitar o TTL menor informado pelo servidor.

No CLI atual, a declaração `mcp_server` é parte da AST/infraestrutura MCP, mas não existe uma operação MHL genérica documentada como `Server.call(...)`. Use a integração que estiver efetivamente conectada à sua versão do runtime.

## Retry

```mhl
retry: {
    max_attempts: 4
    delay: 1s
    retry_on: ["timeout", 429, 503]
}
```

`max_attempts` inclui a primeira chamada. O delay dobra a cada nova tentativa. A lista `retry_on` compara o texto do erro e códigos numéricos convertidos para texto. A propriedade `backoff` é aceita pela configuração, mas o runtime atual sempre aplica backoff exponencial.

## Cache

```mhl
cache: {
    ttl: 1h
    storage: "disk"
    strategy: "exact"
}
```

O cache de agente é opcional e exato: engine, prompt e schema formam a chave SHA-256. Com `storage: "disk"`, os arquivos JSON ficam em `.mhl-cache/`; sem essa opção, o cache é somente memória do processo. A implementação atual usa correspondência exata mesmo que outra `strategy` seja declarada.

## Rate limiting

```mhl
rate_limit: {
    requests_per_minute: 30
    concurrency: 2
    on_exceeded: "queue"
}
```

`concurrency` usa um semáforo e `requests_per_minute` uma janela temporal. As chamadas esperam por capacidade; `on_exceeded` não altera esse comportamento atualmente.

## Processos e timeouts

O adapter CLI executa subprocessos com gerenciamento de grupo de processos no pacote de tools. `cmd.exec` aceita `timeout`; Ollama e HTTP usam timeouts próprios do adapter. Sempre configure limites para operações externas e trate `exit_code`, `status` e erros de transporte explicitamente.
