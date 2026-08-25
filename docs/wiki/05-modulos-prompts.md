# Módulos, imports e prompts

## Importar um módulo inteiro

```mhl
import "./agents.mh" as agents

pipeline Main {
    step Run {
        var result = agents.Local.run(prompt: "Execute a tarefa")
        log(result)
    }
}
```

O alias é um namespace local. O caminho é relativo ao arquivo que contém o `import`.

## Import seletivo

```mhl
use { ReviewPrompt, LocalAgent as Agent } from "./modules/review.mh"

pipeline Main {
    step Review {
        log(Agent.run(prompt: ReviewPrompt(file: "main.go")))
    }
}
```

Cada item é `Nome` ou `Nome as alias`. O alias somente muda o nome local; o símbolo original continua sendo o exportado pelo módulo.

## Prompts inline

```mhl
prompt Review(file: string, code: string) {
    """
    Você é um revisor de código.
    Arquivo: ${file}

    ${code}
    """
}
```

Prompts são chamados como expressões. Os argumentos devem ser nomeados e os valores avaliados devem ser strings:

```mhl
var source = fs.read("main.go")
var prompt_text = Review(file: "main.go", code: source)
var answer = Agent.run(prompt: prompt_text)
```

Uma chamada de prompt pode ser composta com outra expressão ou outro prompt. A declaração também pode carregar Markdown externo:

```mhl
prompt Deploy(target: string) from "./prompts/deploy.prompt.md"
```

O conteúdo é carregado relativamente ao arquivo `.mh`, tratado como string multilinha e interpolado normalmente.

## Boas práticas

- mantenha credenciais fora de prompts e fontes;
- use nomes de parâmetros explícitos;
- deixe exemplos de shell com `\${...}` quando a variável for do shell, não da MHL;
- prefira prompts externos quando houver Markdown, blocos de código ou instruções longas;
- componha prompts antes de chamar o agente para tornar o pipeline testável.
