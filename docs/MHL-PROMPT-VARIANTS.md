# Variantes de `prompt` por harness e modelo

**Status:** implementado (Unreleased, depois da v1.15.0) · **Data:** 2026-11-22

---

## 1. O problema

Os fabricantes de LLM ampliam os modelos, mudam os harnesses (Claude Code, Codex, Copilot,
Antigravity...) e passam a recomendar outra forma de escrever prompts. Isso inclui tags XML,
formato de saída, nível de detalhe e instruções sobre ferramentas. Um `prompt` escrito para um
modelo envelhece, e o projeto precisa de versões do mesmo prompt para harnesses e modelos
diferentes.

O que se pede é isto: **declarar variantes do mesmo prompt sem mudar os lugares onde o prompt é
chamado.**

## 2. Contexto

O caso que motivou o pedido é o Dumont (`resources/workflows/shared/agents/`).

- **O modelo é escolhido na tela, a cada chamada.** `AgentConfig` lê um arquivo gravado pelo
  app, e cada agente monta `--model` com `CliArgs.pick(model, AgentConfig.claude_model())`.
- **O harness também é escolhido por chamada e por papel.** `Coder.execute_as(escolha, …)`
  pode usar um backend para quem implementa e outro para quem avalia, no mesmo run.
- **O prompt passa por tools antes de chegar ao `.run()`.** Ele é renderizado num step e
  entregue a `Writer.generate(prompt: string, …)` ou `Coder.call(…)`. Só ali o agente é
  escolhido. Antes, o texto ainda pode ser transformado: `json_prompt`,
  `AntigravityInput.line` ou um arquivo para o Devin.
- **O ajuste por harness hoje é manual.** `ReaderTools.texto(agent)` usa um `match` por backend
  e injeta o resultado no prompt como parâmetro.

Isso descarta dois desenhos óbvios:

- **Um alvo global por programa não serve**, porque implementador e avaliador diferem no mesmo
  run.
- **Uma variante escolhida pelo prompt** (um seletor declarado no próprio `prompt`) põe a decisão
  no lugar errado. Quem conhece o harness e o modelo é o **agente**.

## 3. Proposta

O prompt declara as variantes. **O agente escolhe** uma delas.

```mh
prompt Review(file: string) {
    default            from "review.md"
    for "claude/*"     from "review.claude.md"
    for "codex/gpt-5*" { "codex: review ${file}" }
}

export agent ClaudeCoder {
    input model: string = ""
    command: "claude"
    args: ["--model", CliArgs.pick(model, AgentConfig.claude_model()), ...]
    stdin: prompt.target("claude/" + CliArgs.pick(model, AgentConfig.claude_model()))
}

export agent CodexLike {
    input model: string = "codex/gpt-5.6"
    command: "codex"
    stdin: "${prompt.target(model)}"     // forma literal: um input ou uma string literal
}

// call sites inalterados
Writer.generate(prompt: Review(file: f), ...)
```

- **O corpo** do prompt pode ser uma lista de arms: um `default` obrigatório e quantos
  `for "<padrão>"` forem necessários. Cada arm tem corpo inline `{ ... }` ou `from "arquivo.md"`
  (com frontmatter, como um `prompt ... from`). Todos os arms compartilham os parâmetros.
- **Dentro do agente**, as expressões de `command`/`args`/`stdin`/`cwd`/`log`/`timeout`
  enxergam `prompt`, o prompt daquela chamada. `prompt.target(t)` devolve o arm que `t` escolhe.
- **O `"${prompt}"` continua sendo o texto `default`.** Nenhum agente existente muda de
  comportamento.

## 4. Seleção

| alvo (`t`)        | arms declarados                          | escolhido        |
| ----------------- | ---------------------------------------- | ---------------- |
| `claude/opus-5`   | `claude/*`, `claude/opus*`               | `claude/opus*`   |
| `claude/sonnet`   | `claude/*`, `claude/opus*`               | `claude/*`       |
| `cli/codex`       | `cli/codex`, `cli/*`                     | `cli/codex`      |
| `ollama/qwen`     | `claude/*`                               | `default`        |
| `""`              | qualquer                                 | `default`        |

- **Precedência:** um padrão exato vence. Senão vence o prefixo (terminado em `*`) mais longo.
  Senão, o `default`.
- **A ordem dos arms não importa.** Não há empate possível, porque um padrão repetido é erro.
  `"*"` sozinho é erro, porque é o papel do `default`.
- **O vocabulário do alvo é do projeto.** Usar `harness/modelo` (`claude/sonnet`,
  `codex/gpt-5.6`) é uma convenção, não uma regra.

## 5. Semântica

1. **O prompt renderizado continua sendo uma string**, o texto `default`. Por isso ele atravessa
   `var`, parâmetro de tool tipado `string`, retorno, campo de objeto e steps da mesma run sem
   mudança nenhuma.
2. **A run guarda as renderizações.** Para cada prompt com variantes renderizado, ela guarda
   declaração e argumentos numa tabela, indexada pelo texto. `texto.target(t)` consulta essa
   tabela. Uma string que não está lá devolve a si mesma.
3. **Qualquer operação de string gera uma string comum**, o texto `default` sem variantes. Isso
   vale para `+`, interpolação, `.trim()` e `fs.write`. No Dumont, a saída é levar a
   transformação para dentro do agente. Exemplo:
   `stdin: AntigravityInput.line(prompt.target("antigravity/" + …))`.
4. **Prompts aninhados seguem o mesmo alvo.** Em `Wrap(inner: Review(...))`, o
   `.target("cli/codex")` resolve o `Review` interno com o mesmo alvo.
5. **Cada perna de fallback** avalia o próprio `stdin`/`args` e escolhe a própria variante.
6. **No router**, `select` e `decider` veem o texto `default`. O agente escolhido decide depois.
7. **Com skills**, o `system_prompt` compõe uma string nova, sem variantes.
8. **Cache:** a chave inclui o texto escolhido. **Trace:** com `trace: true`, o agente imprime
   `prompt Review → for "claude/*" (target "claude/sonnet")`.
9. **Ambiguidade:** dois renders com o mesmo texto `default` podem divergir num arm, porque o
   `default` ignora um parâmetro que o arm usa. Nesse caso o `.target` **falha**, em vez de
   escolher um deles. A regra prática é manter todos os parâmetros no `default`.
10. **Checkpoint:** a tabela não é persistida. Depois de um resume, um prompt renderizado antes
    da pausa escolhe o `default`.

## 6. Por que o valor é string, e não um tipo novo

O plano inicial era um tipo de valor próprio, que se comportaria como string. Um levantamento no
código mudou o plano. O interpretador tem 63 `.(string)` e o `features` tem mais 22, além de
`switch`es com `case string` e do `types.Check`. Cada ponto esquecido viraria um erro "must be a
string, got prompt".

A tabela por run dá a mesma semântica visível sem tocar em nenhum desses pontos. O custo são os
dois limites acima: a ambiguidade de texto (detectada e reportada) e o resume (documentado).

## 7. Alternativas descartadas

- **Argumento no call site** (`Review(..., target: x)`): viola o requisito de não mudar os call
  sites.
- **Seletor declarado no prompt** (`prompt X(...) by <expr>`): põe no prompt uma decisão que é do
  agente.
- **Alvo global** (bloco no programa ou flag `--prompt-target`): não distingue dois papéis no
  mesmo run.
- **Escopo `with target (…) { … }`**: obriga a envolver cada lugar que renderiza o prompt.
- **Primeiro arm que casar, ou `path.Match`**: torna a escolha dependente da ordem e mais difícil
  de prever.

## 8. Critérios de aceite

- [x] O mesmo call site entrega variantes diferentes a agentes diferentes (`prompt_variant_selected_by_agent_target.mh`).
- [x] O alvo vem de um input escolhido por chamada (`prompt_variant_target_from_input.mh`).
- [x] A variante sobrevive a um tool tipado `string` e entre steps (`prompt_variant_survives_tool_string_param.mh`, `internal/cli/prompt_variants_test.go`).
- [x] Uma operação de string gera o `default` (`prompt_variant_string_ops_use_default.mh`).
- [x] O fallback escolhe a própria variante (`prompt_variant_fallback_picks_own_variant.mh`).
- [x] Os arms carregam de Markdown, com frontmatter (`prompt_variant_loaded_from_markdown.mh`).
- [x] `mhl lint` checa os placeholders de cada arm, os arquivos dos arms e o argumento de `${prompt.target(X)}`.
- [x] Um programa sem variantes mantém o `DefinitionDigest` (`TestDefinitionDigestUnchangedByPromptVariants`).

## 9. Compatibilidade

- **Sintaxe antiga:** `prompt X() { expr }` e `prompt X() from "p"` continuam iguais.
  `default` só é palavra-chave como arm, então `{ default }` ainda é um identificador.
- **Digest:** `Prompt.Variants` é `digest:"omitzero"`, e o `default` vai para `Body`/`Source`.
  Um corpo só com `default` gera o mesmo hash da forma simples.
- **Agentes:** o `${prompt}` não muda. A única mudança visível é que `prompt` agora é um nome
  ligado nas expressões de propriedade do agente. Um agente sem inputs que lia uma variável
  `prompt` do chamador passa a ver o prompt da chamada, que normalmente é o mesmo texto.
- **Correção junto:** `mhl lint` deixou de reportar como faltando um parâmetro de prompt que tem
  default.
