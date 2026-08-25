# Skills, memória e ferramentas

## Skills

Uma skill declara descrição, escopo de ferramentas/servidores, contrato de entrada/saída e instruções de sistema:

```mhl
export skill CodeReview {
    description: "Revisa código procurando riscos."
    tools: [execution.read_file, execution.git_diff]
    mcp_servers: [Repository]

    input {
        target: string
        strict: boolean
    }

    output {
        findings: int
        report: string
    }

    system_instructions: """
    Seja objetivo e não exponha credenciais.
    """
}
```

O resolvedor de skills combina instruções do agente com as da skill e restringe o conjunto ativo de tools/MCP à declaração da skill (fail-closed quando a skill não é resolvida). A declaração e o resolver são parte do runtime; a forma de invocação de skill pelo fluxo MHL deve ser validada com a versão do CLI em uso.

## Memória

### KV efêmera

```mhl
memory session {
    type: "kv"
    store: "memory"
}

session.set("attempt", 1)
var attempt = session.get("attempt", 0)
```

Para `kv`, os métodos efetivamente suportados são `set(key, value)` e `get(key [, default])`. A store vive apenas durante uma execução de `mhl`.

### JSON persistente

```mhl
memory state {
    type: "json"
    path: "./.mhl/state.json"
}

state.set("last_status", "ok")
state.set({ "count": 3, "owner": "team-a" })
var status = state.get("last_status", "unknown")
state.remove("last_status")
```

O arquivo é carregado sob demanda e reescrito a cada alteração. Valores podem ser strings, números, bools, arrays e objetos.

### Logs append-only

```mhl
memory audit {
    type: "append_log"
    path: "./logs/audit.log"
}

audit.append("pipeline concluído")
```

`jsonl` usa `append(value)` e serializa cada valor como uma linha JSON. `vector` está descrito na especificação, mas não possui backend de execução no runtime atual.

Chaves de `get` podem navegar por valores estruturados com `::`, por exemplo `state.get("config::retries", 0)` ou `session.get("tags::0")`.

## Tools declaradas

```mhl
tool project {
    read(path: string) -> fs.read(path)
    write(path: string, content: string) -> fs.write(path, content)
    run_tests() -> cmd.exec(["go", "test", "./..."], timeout: 120s)
}
```

Métodos de tool recebem argumentos posicionais e executam em um ambiente próprio, contendo somente os parâmetros declarados. Dentro do corpo, `self.method(...)` chama outro método da mesma tool.

Também é possível usar um bloco:

```mhl
tool checks {
    verify(path: string) -> {
        var result = cmd.exec(["go", "test", path])
        if (result.exit_code == 0) {
            return true
        }
        return false
    }
}
```

## Namespaces nativos

Os namespaces reservados são `cmd`, `git`, `fs`, `http`, `json` e `log`. Eles são normalmente usados no corpo de uma tool, mas a avaliação reconhece essas operações como namespaces nativos.

| Namespace | Operações principais |
| --- | --- |
| `cmd` | `exec`, `exec_all` |
| `git` | `diff`, `add`, `commit`, `status`, `rev_parse`, `log` |
| `fs` | `read`, `exists`, `write`, `append`, `delete`, `list` |
| `http` | `post` |
| `json` | `parse`, `parse_lines`, `stringify` |
| `log` | `info`, `warn`, `error` |

`cmd.exec` aceita uma string de comando ou array de argv. Resultados de comando têm `stdout`, `stderr` e `exit_code`; um exit code diferente de zero é um resultado inspecionável, não necessariamente um erro de avaliação. `http.post` retorna `status` e `body`; respostas HTTP não-2xx também são retornadas para decisão do pipeline.
