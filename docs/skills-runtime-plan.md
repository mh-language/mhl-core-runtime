# Plano de adoção de skills no runtime MHL (v3)

Referência conceitual: [Agent Skills — Claude Platform Docs](https://platform.claude.com/docs/en/agents-and-tools/agent-skills/overview). O plano adapta a estrutura de `SKILL.md` ao harness MHL; não pressupõe que os agentes atuais tenham a leitura progressiva de arquivos descrita para Claude.

## Mudanças em relação à v2

A v2 corrigiu lacunas de viabilidade da v1 mas manteve a composição de `${skills}`/`${prompt}` como interpolação especial dentro de um elemento de `args` — o que exigia generalizar `injectPromptArg`/`checkAgentPlaceholders` para substituição de token embutido, um mecanismo novo e frágil (baseado em varredura de texto). Esta versão substitui essa abordagem por um hook explícito, `system_prompt`, e resolve dois pontos que ficaram em aberto na discussão da v2:

1. **Composição deixa de ser texto interpolado em `args` e passa a ser um hook explícito.** `system_prompt: (skills, prompt) -> ...` roda como uma expressão comum do corpo do agente — a mesma classe de coisa que `before`/`after` já são — e o texto que ele retorna é injetado no `${prompt}` existente (mecanismo de elemento inteiro, já implementado, inalterado) ou no campo `prompt` do Ollama. Isso elimina a necessidade de tocar em `injectPromptArg`/`checkAgentPlaceholders`.
2. **`skills` é uma lista de objetos, não um texto pré-montado.** Cada objeto expõe `name` (o identificador MHL da declaração `skill`, o mesmo que `nameof(...)` resolve), `frontmatter` (objeto genérico com as chaves do frontmatter) e `content` (corpo Markdown sem o frontmatter). Isso permite lógica condicional dentro do hook — inclusive comparação de identidade via `nameof`, que já existe no runtime hoje (`router.select: (prompt) -> nameof(Billing)`) e já é validada estaticamente pelo lint (`nameof: "X" is not a declared name`); o corpo de `system_prompt` herda essa validação de graça, pelo mesmo mecanismo genérico que já varre `before`/`after`/`select`.
3. **Frontmatter é um objeto genérico (`map[string]any`), reaproveitando acesso já existente no avaliador.** `frontmatter.name`/`frontmatter["name"]` não exigem nenhum trabalho novo no avaliador de expressões — objetos já são `map[string]any` e ambos os acessos (`.campo` e `["campo"]`) já são suportados genericamente hoje. Apenas `name`/`description` são chaves exigidas/validadas na skill; chaves extras passam por natural no objeto.
4. **`prompt ... from "arquivo"` ganha frontmatter opcional, no mesmo padrão.** Reaproveita o mesmo parser mínimo de frontmatter da skill. Arquivos de prompt existentes sem bloco de frontmatter continuam se comportando exatamente como hoje — isso é aditivo, não uma mudança de contrato. O acesso `PromptName.frontmatter` é um caso novo no avaliador, no molde do acesso a `enum` já existente (`resolveEnumAccess`: identificador + `.membro` sem chamada), não interpolação de texto.

O restante do documento já incorpora essas decisões.

## Objetivo e limite da primeira versão

Uma skill é um pacote de instruções reutilizáveis selecionado para uma chamada de agente. O runtime carrega seus arquivos, valida a seleção e envia o texto das instruções junto com o pedido ao modelo, através de um hook explícito no agente — nunca implicitamente. A primeira versão não tenta ativar skills automaticamente por interpretação do modelo, nem permite que o agente peça arquivos adicionais durante a geração: os agentes `cli/*` e `ollama/*` atuais fazem uma chamada única. Scripts de uma skill não são executados implicitamente; efeitos continuam sob `tool` e `extension`.

Skills são opcionais. Um agente sem `skills` e uma chamada sem `skills` mantêm o comportamento atual — nenhum hook de composição é chamado, e a requisição enviada é idêntica à de hoje. A lista declarada no agente é uma lista de permissões; a lista da chamada escolhe o subconjunto usado, preservando sua ordem. Nenhuma skill é carregada na chamada só por estar declarada no agente.

## Contrato de linguagem proposto

```mhl
skill CodeReview from "./skills/code-review/SKILL.md"
skill SecurityReview from "./skills/security-review/SKILL.md"

agent Reviewer {
    engine: "cli/claude"
    command: "claude"
    args: ["-p", "${prompt}"]
    skills: [CodeReview, SecurityReview]

    system_prompt: (skills, prompt) -> {
        var instructions = ""
        for (var s in skills) {
            instructions = instructions + "## " + s.frontmatter.name + "\n" + s.content + "\n\n"
        }
        return instructions + prompt
    }
}

pipeline ReviewPR {
    input diff: string
    step Review {
        var result = Reviewer.run(
            skills: [CodeReview, SecurityReview],
            prompt: "Revise este diff: ${diff}"
        )
        log(result)
    }
}
```

`args: ["-p", "${prompt}"]` não muda em relação ao agente sem skills — `${prompt}` continua sendo substituído pelo mecanismo de elemento inteiro que já existe (`injectPromptArg`). A diferença é só o valor que chega ali: se `system_prompt` está declarado e a chamada selecionou pelo menos uma skill, o texto injetado é o retorno do hook; caso contrário (sem skills selecionadas nessa chamada, mesmo que o agente declare `skills:`), o texto injetado é o `prompt:` resolvido puro, igual a hoje — `system_prompt` sequer é chamado.

`skill Name from "path"` aceita importação e exportação como as demais declarações nomeadas. O arquivo exige frontmatter com pelo menos `name` e `description`; o corpo Markdown depois do frontmatter é o conteúdo (`content`). `name` na declaração `skill` é sempre o identificador MHL (`CodeReview`) — o mesmo valor que `nameof(CodeReview)` resolve — nunca o nome do frontmatter, que fica isolado em `frontmatter.name` e serve só para identificar o pacote no texto que o hook decide compor. O runtime resolve o caminho relativo ao arquivo que declarou a skill, inclusive quando ela veio de um módulo importado.

`agent.skills` e o argumento `run(skills: ...)` contêm referências a declarações `skill`, sem strings dinâmicas na primeira versão. A chamada é inválida se selecionar uma skill não permitida pelo agente, repetir uma skill ou usar `skills:` num agente que não declarou a propriedade. Escolher `skills: []` equivale a não selecionar nenhuma.

Para `ollama/*`, que só tem um campo `prompt`, o mesmo texto que `system_prompt` produz (ou o `prompt:` puro, se não houver skills selecionadas) é enviado nesse campo — sem nenhuma composição própria do adaptador, porque a composição já aconteceu antes do dispatch por engine. `command:` continua sem reconhecer nenhum placeholder.

### O objeto skill e o objeto frontmatter

Cada item da lista `skills` recebida por `system_prompt`, na ordem selecionada pela chamada:

| Campo | Origem | Observação |
|---|---|---|
| `name` | identificador MHL da declaração `skill` | idêntico ao que `nameof(CodeReview)` resolve — permite `s.name == nameof(CodeReview)` |
| `frontmatter` | bloco de frontmatter do `SKILL.md` | objeto genérico (`map[string]any`); `name`/`description` são exigidos e validados no load, chaves extras passam por natural |
| `content` | corpo Markdown após o frontmatter | string; nunca inclui o bloco de frontmatter |

`frontmatter.name`/`frontmatter["name"]` não são mecanismo novo: objetos no avaliador já são `map[string]any`, e tanto acesso por campo (`.name`) quanto por chave dinâmica (`["name"]`) já são suportados genericamente hoje — a skill só passa a ser mais um produtor desse tipo de valor.

### Frontmatter em `prompt ... from "arquivo"` (escopo estendido)

O mesmo parser mínimo de frontmatter passa a ser reaproveitado por `prompt Name(...) from "./file.md"`, como extensão opcional e retrocompatível:

- um arquivo de prompt sem bloco `---\n...\n---` no início se comporta exatamente como hoje — `Body` é o arquivo inteiro, sem campos exigidos;
- um arquivo de prompt com bloco de frontmatter tem esse bloco removido do `Body` renderizado e disponibilizado via `PromptName.frontmatter` — acesso novo no avaliador, no molde do acesso a `enum` já existente (identificador seguido de `.membro`, sem parênteses de chamada), já que hoje `PromptName(...)` só é reconhecido como invocação;
- ao contrário da skill, `prompt ... from` **não exige nenhuma chave obrigatória** no frontmatter — é conveniência livre para o autor do prompt (versão, tags, o que fizer sentido), não uma dependência do fluxo de execução.

Isso é aditivo: nenhum `prompt ... from` existente muda de comportamento.

## Fluxo de execução

1. Parser e resolução de imports registram as declarações e carregam o Markdown/frontmatter da skill (e, quando presente, do prompt). O carregamento valida frontmatter exigido (skill), tamanho máximo e caminhos de arquivos referenciados, sem executar scripts.
2. `mhl lint` valida nomes, permissões, duplicatas, arquivo existente, frontmatter, a obrigatoriedade de `system_prompt` quando `agent.skills` não é vazio, e o uso de `skills:` na chamada.
3. Em `Agent.run`, o `before` continua sendo executado primeiro. O runtime resolve `prompt:` e `schema:`, seleciona as skills na ordem da chamada (montando os objetos `{name, frontmatter, content}`) e, só se pelo menos uma skill foi selecionada nessa chamada, invoca `system_prompt(skills, prompt)` — cujo retorno substitui o `promptText` usado daí em diante. Sem skills selecionadas, `promptText` segue puro e `system_prompt` não é chamado, preservando a requisição de hoje.
4. Cada tentativa do agente recebe o mesmo `promptText` já composto. Para CLI, a injeção em `${prompt}` usa o mecanismo de elemento inteiro existente, sem alteração. Para Ollama, o adaptador recebe o mesmo `promptText` já pronto. `after` continua recebendo a resposta final.
5. Cache e retries usam a requisição efetivamente enviada. A chave do cache inclui os nomes, a ordem e um hash do conteúdo bruto de cada arquivo de skill selecionado (frontmatter + corpo), além do prompt e dos parâmetros do agente. Uma edição em qualquer parte do `SKILL.md` — frontmatter incluso — não pode reutilizar uma resposta antiga.

### Allow-list do agente vs. seleção da chamada

`agent.skills` é a lista de permissão; `run(skills: [...])` escolhe o subconjunto usado, na ordem dada. Validação, estática (lint) e repetida em runtime (lint não é barreira de execução):

- toda skill em `run(skills: ...)` deve constar em `agent.skills` do agente chamado — senão, erro do tipo `agent %q: skill %q not permitted (declared skills: %s)`, no mesmo estilo de `agent %q fallback: agent %q is not declared`;
- repetir uma skill na chamada é erro;
- usar `skills:` numa chamada a um agente sem propriedade `skills` declarada é erro;
- `skills: []` é equivalente a omitir a propriedade.

### `system_prompt` obrigatório quando o agente permite skills

Diferente da v2 (que checava a presença textual de `${skills}` dentro de `args`), a regra agora é estrutural: **todo agente com `agent.skills` não vazio precisa declarar `system_prompt`** — erro de lint (`agent %q: declares skills but no system_prompt to receive them`) antes de qualquer execução. Isso vale para o agente primário e, pela mesma regra, para cada agente em `fallback: [...]` de um agente skill-capaz:

- `agent %q: fallback %q cannot receive skills — it does not declare system_prompt`.

Essa checagem é estática e conservadora — assume que qualquer chamada ao agente primário pode vir com skills selecionadas, sem rastrear call sites individuais, no mesmo espírito de `agent %q fallback: agent %q is not declared`.

### `system_prompt` como hook de dois parâmetros

`before`/`after` hoje são lambdas de zero parâmetros ("Both reject a parameter list", conforme `agent_hooks.go`/CLAUDE.md). `system_prompt` é um hook novo, de dois parâmetros nomeados (`skills`, `prompt`), avaliado como corpo de lambda comum — bloco de statements, não só expressão única, permitindo `for (var s in skills) { ... }` com o `for (var x in y) { ... }` já existente na linguagem. Como qualquer outro lambda do programa, seu corpo é varrido pela mesma checagem estática genérica que já valida `nameof(...)`, `match`, chamadas de agente/memória etc. dentro de `before`/`after`/`select` — nenhum código de lint novo é necessário para essa parte, só incluir `system_prompt` no conjunto de lambdas já percorrido.

## Etapas de implementação

0. **Refatoração pré-requisito — fonte única de propriedades de agente:** introduzir `ast.AgentBodyProperty`/`ast.AgentBodyProperties`/`ast.AgentBodyPropertyNames()` em `internal/lang/ast/agentconfig.go`, espelhando `ast.PipelineBodyProperties`. Trocar `lint.knownAgentProperties` (hoje mapa hand-copied) e `lsp.agentPropertyItems`/mensagem de erro em `checkAgentProperties` para derivar dessa lista, como `checkRouterProperties`/`routerPropertyItems` já fazem para `router`. Sem mudança de comportamento — remove uma fonte de duplicação antes de adicionar `skills`/`system_prompt` como propriedades novas.
1. **Parser mínimo de frontmatter (pacote compartilhado):** um leitor de bloco `---\nchave: valor\n---` flat — não YAML genérico, sem dependência nova. Aceita uma lista de chaves exigidas por chamador (skill: `name`, `description`; prompt: nenhuma) e devolve o restante como `map[string]any` livre. Usado tanto pelo carregador de skill quanto pela extensão de `loadPromptSource`.
2. **Modelo e parser:** adicionar `ast.Skill` em `internal/lang/ast` (mesmo formato de `ast.Prompt`: `Name`, `Source string` via `'from' @String`, mais os campos resolvidos — frontmatter e conteúdo), a alternativa de declaração em `program.go`, o argumento `skills:` de chamada, a propriedade `skills` em `ast.AgentBodyProperties` (passo 0), e o novo hook `system_prompt` em `agent_hooks.go` com lista de parâmetros `(skills, prompt)` — deviação explícita e documentada da regra "zero parâmetros" de `before`/`after`. Estender `ast.Prompt` com os mesmos campos de frontmatter resolvido. Testes de parsing/import/export.
3. **Leitura e validação:** estender `loadPromptSource`/o carregador de skill (`internal/engine/interpreter/imports.go` e seu par em `internal/lang/lint/imports.go`) para separar frontmatter de corpo usando o parser do passo 1, reaproveitando a resolução relativa de arquivo já existente. Resolver a origem de módulos importados antes de achatar declarações — isso também é o que faz `DefinitionDigest` (usado para invalidar checkpoints resumíveis) enxergar mudanças de conteúdo automaticamente, sem trabalho extra. Nova entrada `skill` em `mergeableDecl`/`findExport`. Rejeitar arquivo ausente, frontmatter inválido/incompleto (skill) ou fora do pacote, e conteúdo acima do limite — limite começa como constante no código (ex.: 64 KiB) com comentário explicando o valor; uma superfície de configuração só é adicionada se a constante se mostrar insuficiente na prática.
4. **Avaliador:** novo caso em `eval.go` para acesso `PromptName.frontmatter` (identificador + `.membro` sem chamada, no molde de `resolveEnumAccess` para `enum`); resolução de `SkillName` como valor dentro de `skills: [...]` (declaração e call site) produzindo o objeto `{name, frontmatter, content}`; binding dos parâmetros `skills`/`prompt` no corpo de `system_prompt`. `frontmatter.campo`/`frontmatter["campo"]` não precisam de código novo — reaproveitam o acesso a `map[string]any` já existente para qualquer objeto.
5. **Lint e editor:** validar `agent.skills`/`system_prompt` (via passo 0), `run(skills: ...)` contra a allow-list (ver "Allow-list do agente vs. seleção da chamada"), a obrigatoriedade estrutural de `system_prompt` em agente e fallback (ver seção correspondente), e incluir o corpo de `system_prompt` no mesmo percurso genérico de lambdas que já valida `nameof`/`match`/chamadas dentro de `before`/`after`/`select`. Adicionar completions de nomes de skill declarados dentro de `skills: [...]` e `run(skills: ...)` no LSP — trabalho novo, sem padrão equivalente hoje para completar nomes dentro de um array de referências. Exigir que seleção inválida falhe também em `mhl run`, pois lint não é barreira de execução.
6. **Execução:** em `runAgent`/`runAgentAttempt`, montar a lista de objetos skill selecionados; se não vazia, chamar `system_prompt(skills, prompt)` e usar o retorno como `promptText` dali em diante — senão, manter `promptText` puro (nenhuma chamada ao hook). O restante do fluxo (injeção em `args`, envio ao Ollama) usa esse `promptText` sem saber se veio de `system_prompt` ou não.
7. **Cache e observabilidade:** estender `agentCacheParams` (`internal/engine/interpreter/agent.go`) com nomes, ordem e hash do conteúdo bruto (frontmatter + corpo) de cada skill selecionada — `traffic.RequestKey` já serializa uma struct genérica, então isso é aditivo e não quebra entradas de cache antigas (deixam só de casar). Registrar apenas nomes e hashes das skills selecionadas em logs e checkpoints. Como não existe mecanismo geral de redação de conteúdo (a redação atual é por registro de credenciais, `auth.Redact`), a invariante "nunca escrever o `promptText` composto ou o conteúdo de uma skill em `ctx.out`, arquivo de `log:` ou `var`/checkpoint" é garantida por não introduzir nenhum caminho de código que o faça, e verificada por teste.
8. **Exemplos e verificação:** adicionar exemplos auto-verificáveis em `sample/features/skills` (incluindo um `system_prompt` com `for` sobre a lista de skills) e um `prompt ... from` com frontmatter em `sample/features/prompts` (ou equivalente), documentação da sintaxe em `docs/site/reference.html`, e testes de integração dos dois adaptadores com capturas da requisição/argv. Executar `gofmt`, `go test ./...`, `go vet ./...` e `make functional-test`.

## Critérios de aceitação

- A refatoração do passo 0 não muda o comportamento de nenhum agente existente.
- Um `prompt ... from "arquivo"` sem bloco de frontmatter se comporta exatamente como hoje.
- Uma chamada sem skills selecionadas produz a mesma requisição que hoje, mesmo que o agente declare `skills:` e `system_prompt` — o hook não é chamado.
- Um agente com `skills:` não vazio e sem `system_prompt` falha em `mhl lint`, antes de qualquer execução.
- Um fallback skill-incompatível (sem `system_prompt`) de um agente skill-capaz falha em `mhl lint`, antes de qualquer tentativa em runtime.
- Duas skills selecionadas chegam em `system_prompt` como uma lista, na ordem da chamada, cada uma com `name` (identificador MHL), `frontmatter` (objeto) e `content` (string).
- `s.name == nameof(CodeReview)` funciona, e um `nameof(Errado)` dentro de `system_prompt` falha em `mhl lint` com a mesma mensagem que já existe para `router.select`.
- `frontmatter.campo` e `frontmatter["campo"]` resolvem para o mesmo valor.
- Uma skill não permitida, ausente, duplicada ou inválida falha antes de chamar o modelo, tanto em `mhl lint` quanto em `mhl run`.
- Editar qualquer parte de uma skill (frontmatter ou corpo) invalida o cache, sem expor o texto em logs ou checkpoints (verificado por teste, não apenas por convenção).
- Retry e fallback recebem o mesmo `promptText` composto.
- Nenhum script incluído no pacote é executado pela ativação da skill.

## Evolução posterior

Depois de medir utilidade e custo de contexto da seleção explícita, avaliar seleção automática baseada nos metadados. Leitura progressiva de recursos durante a geração só deve ser oferecida quando um adaptador suportar uma conversa com chamadas de ferramenta ou leitura de arquivos no meio da execução. Essa capacidade deve ter permissões próprias e não alterar o significado da primeira versão. Um limite de tamanho configurável (em vez da constante fixa do passo 3) entra aqui, se a constante inicial se provar insuficiente. Um helper de composição (ex.: um método que já junta `skills` num único bloco de texto formatado) pode ser adicionado depois, se o padrão manual com `for` se mostrar repetitivo na prática — não é assumido de antemão.
