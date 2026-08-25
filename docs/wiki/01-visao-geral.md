# Visão geral e primeiros passos

## O que é MHL?

MHL descreve um fluxo de trabalho de IA em um arquivo declarativo. Um programa combina:

- `agent`: processo CLI ou endpoint local de inferência;
- `prompt`: template de texto parametrizado;
- `tool`: funções reutilizáveis que encapsulam operações nativas;
- `memory`: armazenamento entre statements, steps ou execuções;
- `pipeline`: sequência de steps com controle de fluxo e recuperação;
- `skill` e `mcp_server`: contratos modulares e integrações declarativas.

O runtime Go transforma o fonte em uma AST e interpreta as expressões durante a execução. Não há compilação para um bytecode separado.

## Primeiro programa

```mhl
agent Local {
    engine: "cli"
    command: "echo"
    args: ["resposta local"]
}

pipeline Hello {
    input name: string

    step Greet {
        var message = "Olá, ${name}!"
        log(message)
        var answer = Local.run(prompt: message)
        log(answer)
    }
}
```

O agente recebe um prompt não vazio. A forma geral de chamada é `NomeDoAgente.run(prompt: expressão)`, e o valor retornado é uma `string`.

## Organização recomendada

```text
meu-projeto/
├── main.mh
├── modules/
│   ├── agents.mh
│   └── prompts/
│       └── review.prompt.md
├── .mhl/
│   └── state/          # checkpoints, quando habilitados
└── .mhl-cache/         # cache de agentes com storage: "disk"
```

Use módulos para separar configuração de agentes, templates e ferramentas. Caminhos de `import`, `use` e prompts externos são resolvidos relativos ao arquivo que os declara.

## Ciclo de execução

```text
fonte .mh
  ↓ lexer/parser
AST
  ↓ resolução de imports e avaliação
steps do pipeline
  ↓
agentes, memória, tools nativas e estado
```

Uma variável criada com `var` dentro de um step é local àquele step. Para compartilhar dados entre steps, use uma variável de pipeline (declarada no corpo do pipeline) ou uma memória. Para sobreviver a um novo processo com `--resume`, use memória persistente ou o estado do checkpoint.
