# Sintaxe lexical e declarações

## Comentários e whitespace

Comentários de linha começam com `//`. Espaços, tabs e quebras de linha são ignorados fora de strings.

```mhl
// comentário
agent Local { command: "echo" }
```

Identificadores começam com letra ou `_` e continuam com letras, números ou `_`. Palavras reservadas são reconhecidas pelo contexto: `agent`, `prompt`, `skill`, `memory`, `tool`, `pipeline`, `loop`, `step`, `input`, `var`, `if`, `else`, `while`, `for`, `in`, `try`, `catch`, `finally`, `return`, `break`, `goto`, `import`, `use`, `from`, `export` e `test`.

## Strings

Strings comuns usam aspas duplas e aceitam escapes reconhecidos pelo lexer. Strings multilinha usam `"""`:

```mhl
prompt Review(file: string) {
    """
    Revise o arquivo ${file}.
    Retorne uma lista objetiva de problemas.
    """
}
```

O conteúdo da string é interpolado quando avaliado. Em um prompt externo, escreva `\${PLACEHOLDER}` para preservar literalmente `${PLACEHOLDER}` sem torná-lo uma interpolação MHL.

## Durações

Literais de duração são usados em configurações:

```mhl
timeout: 45s
delay: 2s
ttl: 24h
checkpoint: { ttl: 7d }
```

Os leitores de configuração do runtime usam `s`, `m`, `h` e `d`. Durações são valores de configuração e não podem ser usadas como valores de expressão comuns.

## Declarações de topo

```mhl
import "./modules/agents.mh" as agents
use { ReviewPrompt as Review } from "./modules/prompts.mh"

export agent Local { command: "echo" }
prompt P(name: string) { "Olá ${name}" }
skill ReviewSkill { description: "Revisão" }
memory session { type: "kv", store: "memory" }
tool files { read(path: string) -> fs.read(path) }
mcp_server Build { transport: "stdio", command: "build-mcp" }
pipeline Main { step Run { log("ok") } }
```

`export` marca a declaração para consumo por outro módulo. A gramática permite `export` antes de uma declaração de topo; `import` e `use` são as formas de composição.

## Separadores

Corpos de declarações usam `{ ... }`. Arrays usam `[ ... ]`. Objetos podem usar vírgulas e, em blocos multilinha de configuração, a vírgula entre campos pode ser omitida:

```mhl
agent Local {
    command: "echo"
    args: ["ok"]
}
```

Parâmetros e argumentos são separados por vírgulas. Chamadas de prompts exigem argumentos nomeados; métodos de `tool`, memória e agentes usam a convenção própria descrita nas páginas correspondentes.
