# Agentes e execução de modelos

## Declaração

```mhl
agent ClaudeCLI {
    engine: "cli/claude-code"
    command: "claude"
    args: ["--non-interactive", "--output-format", "text"]
    timeout: 60s
    trace: true
    log: "./logs/claude.out"
}
```

O adapter CLI executa `command` com `args` e acrescenta o prompt ao final. Para controlar a posição, inclua o marcador literal `${prompt}` em `args`:

```mhl
args: ["--prompt", "${prompt}", "--non-interactive"]
```

O stdout não vazio do processo vira o resultado string de `.run()`. Falhas do processo carregam stderr para o diagnóstico.

`timeout` e `api_key` aparecem na especificação de configuração, mas o fluxo de execução atual do adapter CLI não aplica esses campos diretamente; controle o timeout no próprio comando/adaptador e injete credenciais com o ambiente.

## Ollama

```mhl
agent LocalModel {
    engine: "ollama/qwen2.5-coder"
    endpoint: "http://localhost:11434"
    temperature: 0.2
}
```

O modelo é a parte após `ollama/`. O runtime faz uma requisição `generate` sem streaming e usa `response` como resultado.

## Chamada

```mhl
var answer = LocalModel.run(
    prompt: Review(file: "main.go", code: source),
    schema: "{\"type\":\"object\"}"
)
```

`prompt` é obrigatório, deve ser string e não pode ser vazio. `schema` é opcional; no adapter CLI ele pode ser injetado em um argumento `${schema}` ou anexado ao final dos argumentos.

## Fallbacks

Fallbacks podem referenciar agentes declarados ou usar um bloco inline:

```mhl
agent Primary {
    engine: "cli/primary"
    command: "primary"
    fallback: [Backup, agent {
        engine: "cli/last-resort"
        command: "last-resort"
    }]
}

agent Backup {
    engine: "ollama/local"
    endpoint: "http://localhost:11434"
}
```

Cada agente tem sua própria política de retry, cache e limite. O fallback só é tentado após a tentativa (e retries) do agente anterior falhar.

## Retry, cache e limite

```mhl
agent Robust {
    engine: "cli/worker"
    command: "worker"
    retry: {
        max_attempts: 3
        delay: 2s
        retry_on: [500, 503, "timeout", "rate limit"]
    }
    cache: {
        ttl: 24h
        storage: "disk"
    }
    rate_limit: {
        requests_per_minute: 60
        concurrency: 4
        on_exceeded: "queue"
    }
}
```

O retry atual usa backoff exponencial (`delay`, `delay*2`, ...). `retry_on` procura códigos/textos na mensagem do erro; sem a lista, falhas são elegíveis a retry até o limite. O cache é por correspondência exata, com chave SHA-256 de engine, prompt e schema. `storage: "disk"` grava em `.mhl-cache/`; sem isso o cache vive somente no processo.

Limites de concorrência e requests/minute bloqueiam até haver capacidade. `on_exceeded` é armazenado, mas o runtime atual implementa o comportamento de fila/espera.

## Engines não suportados no CLI atual

Declarações com engines como `anthropic/...` ou `openai/...` podem aparecer na especificação, mas `runAgentAttempt` atualmente executa diretamente somente engines vazios/`cli/*` e `ollama/*`. Para outros providers, use um adapter/CLI configurado ou consulte o estado do runtime antes de adotar a configuração.
