# Manual da linguagem MHL

**MHL (Meta-Harness Language)** é uma DSL declarativa para descrever agentes de IA, prompts, ferramentas, memória e pipelines executáveis pelo CLI `mhl`.

Esta wiki documenta a linguagem a partir do parser e do runtime presentes em `src/mhl-runtime`. Os exemplos usam a extensão recomendada `.mh`; arquivos `.mhl` também aparecem na documentação histórica do projeto.

## Navegação

- [Visão geral e primeiros passos](01-visao-geral.md)
- [Instalação e CLI](02-instalacao-cli.md)
- [Sintaxe lexical e declarações](03-sintaxe.md)
- [Expressões e valores](04-expressoes.md)
- [Módulos, imports e prompts](05-modulos-prompts.md)
- [Agentes e execução de modelos](06-agentes.md)
- [Skills, memória e ferramentas](07-skills-memoria-tools.md)
- [Pipelines, controle de fluxo e checkpoints](08-pipelines.md)
- [MCP, resiliência e cache](09-mcp-resiliencia.md)
- [Runtime, segurança e diagnóstico](10-runtime-seguranca.md)
- [Referência rápida](11-referencia-rapida.md)

## Mapa mental

```text
programa .mh
├── declarações: agent, prompt, skill, memory, tool, mcp_server, pipeline
├── expressões: valores, operadores, chamadas, acesso e lambdas
└── execução: mhl run → parser → interpretador → adapters/tools → estado
```

## Estado da implementação

O parser aceita a sintaxe descrita nesta wiki. A execução efetiva depende da integração correspondente no interpretador. Em particular:

- agentes `cli/*` (ou sem `engine`) e `ollama/*` são os adapters executados diretamente pelo CLI atual;
- `cmd`, `git`, `fs`, `http`, `json` e `log` são namespaces nativos disponíveis em corpos de `tool`;
- `memory` suporta `kv` em memória, `json`, `append_log` e `jsonl`; `vector` permanece sem backend de execução;
- `mcp_server` e `skill` têm AST, resolução e componentes de runtime, mas o fluxo de alto nível do CLI deve ser conferido antes de depender de uma chamada MHL direta a MCP/skill;
- propriedades aceitas pela sintaxe podem ser ignoradas ou ter suporte parcial. As páginas indicam essas limitações quando relevantes.

As especificações de projeto em [`docs/language-specification.md`](../language-specification.md) e [`docs/language-design.md`](../language-design.md) continuam sendo referências de intenção e roadmap.
