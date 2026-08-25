# Runtime, segurança e diagnóstico

## Camadas do runtime

```text
CLI
 ├─ parser/lint: fonte → AST e diagnósticos
 ├─ interpreter: expressões, statements, prompts e chamadas
 ├─ runtime: steps, goto, checkpoints e loops
 └─ features: adapters, memória, tools, MCP, skills e traffic
```

No código Go, a direção de dependências é `lang → features → engine → cli` conforme a responsabilidade; o CLI é a camada de entrada que compõe todas as demais.

## Credenciais

Nunca grave tokens no fonte. Resolva valores via ambiente:

```mhl
mcp_server GitHub {
    transport: "http"
    url: "https://example.test/mcp"
    headers: {
        "Authorization": "Bearer " + env("GITHUB_TOKEN")
    }
}
```

`env("NOME")` retorna o valor da variável de ambiente, ou string vazia quando ela não está definida. A resolução acontece em runtime. Checkpoints redigem strings usando o resolvedor de auth antes de gravar variáveis; ainda assim, evite colocar segredos em arrays/objetos, logs, prompts ou resultados persistidos.

## Diagnósticos

Falhas de statements carregam arquivo, linha e coluna, por exemplo:

```text
main.mh:18:9: agent "Local" failed: ...
```

Erros dentro de `try` podem ser tratados com `catch (err)`. Para falhar deliberadamente com uma mensagem, use `fail("motivo")`; para terminar um pipeline com sucesso controlado, use `break "motivo"`.

Problemas comuns:

| Sintoma | Causa provável |
| --- | --- |
| `no pipeline declared` | O arquivo não contém `pipeline` ou o import não foi resolvido. |
| `requires a non-empty prompt` | A chamada `.run()` não recebeu `prompt` string não vazia. |
| `has no command` | Agente CLI não declarou `command`. |
| `engine ... is not supported yet` | Provider não é `cli/*` nem `ollama/*` no runtime atual. |
| `memory ... type ... not supported yet` | Backend, como `vector`, ainda não tem executor. |
| erro em `.mhl/state` | Caminho/TTL/permissão do checkpoint ou estado expirado. |
| `value is not callable` | A expressão chamada não é uma closure/lambda. |

## Limites de segurança operacional

- `while` e ciclos de `goto` têm limites para não deixar o processo preso indefinidamente;
- `cmd.exec` não deve receber entrada não confiável sem validação; prefira o formato argv a concatenar comandos;
- `fs.delete` é destrutivo e falha se o caminho não existir;
- `http.post` envia JSON e pode carregar dados sensíveis nos headers/body;
- `trace`, `log` e `log.*` podem expor prompts e respostas, portanto devem ser habilitados conscientemente.

## Testabilidade

Separe a lógica em tools, mantenha prompts externos e use `mhl test` para exercitar fluxos sem depender da infraestrutura real quando possível. Para alterações do runtime:

```bash
cd src/mhl-runtime
gofmt -w caminho/alterado.go
go test ./...
go vet ./...
```
