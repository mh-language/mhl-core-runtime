# Melhorias de linguagem sugeridas a partir de `workflows/`

> Feedback para quem mantém `src/mhl-runtime` (compilador/runtime/lint do mhl). Cada item nasceu de
> um comportamento real observado revisando a implementação em [`workflows/`](../workflows/) —
> um conjunto de `pipeline`/`workflow` em produção (Discovery, Delivery, Wiki, WorkItem) — não de
> preferência abstrata. Onde possível, o achado foi confirmado com um repro isolado rodado contra
> o build atual desta branch (`make build` em `src/mhl-runtime`), não só por leitura de código.
> Atualizar esta lista sempre que uma nova revisão de `workflows/` esbarrar em outra lacuna.

## 1. `router.select` não é um método consultável de fora — só `.delegate()` é público

[`workflows/shared/agents/agents.mh`](../workflows/shared/agents/agents.mh) precisa decidir, para
cada chamada, qual dos três backends (`Devin`/`Claude`/`Codex`) vai gerar o conteúdo — e precisa
saber a resposta **antes** de montar a chamada, porque cada backend espera o argumento `schema:`
num formato diferente (Claude quer o JSON inline; Codex quer um caminho de arquivo; Devin não tem
suporte nativo a schema). Isso é exatamente o problema que a declaração `router` (com seu hook
`select: (prompt) -> ...`) parece resolver — mas `select` não é um método consultável: ele só roda
internamente dentro do próprio `.delegate()`, e chamar `Router.select(...)`/`AgentRouter.select(...)`
de fora falha em runtime com `undefined variable`, mesmo sem nenhum import envolvido.

Como resultado, `router` foi descartado inteiramente pelo código real e substituído por um `tool`
comum + `match` manual, duplicado por operação (`agents.mh:179-190`, `246-276`):

```
tool AgentSelector {
    pick(prompt: string): string -> match env("SENPAI_AGENT", "codex") {
        "devin"  -> nameof(Devin)
        "claude" -> nameof(Claude)
        "codex"  -> nameof(Codex)
        _        -> nameof(Codex)
    }
}
```

seguido de dois blocos `match picked { nameof(Devin) -> ...; nameof(Claude) -> ...; nameof(Codex) -> ... }`
em `Writer.generate` — um para obter a resposta crua, outro para escolher o parser de uso certo —
cada um repetindo os três nomes de backend. Com um terceiro backend já em produção (o `router` só
foi desenhado, a julgar pela documentação, para o caso de dois candidatos), o padrão de duplicação
por `match` escala linearmente a cada novo backend, exatamente o tipo de crescimento que `router`
deveria absorver.

**Sugestão:** expor `Router.select(prompt: ...): string` como método público de fato (é uma função
pura, sem side-effect, já com assinatura declarada) — permitindo decidir a rota e usar essa decisão
para preparar argumentos auxiliares *antes* de chamar `.delegate()`. Alternativamente, se a
opacidade for proposital, deixar isso explícito na primeira menção de `select` na referência da
linguagem, já que a assinatura de "one-parameter lambda" hoje lê como convite a reuso externo.

## 2. Sem forma de copiar um objeto com poucos campos sobrescritos — reescrever todo campo à mão

[`workflows/shared/artifacts/c4_diagram.mh:499-515`](../workflows/shared/artifacts/c4_diagram.mh#L499-L515)
precisa devolver cada diagrama de um lote com **um campo a mais** (`avisos_lote`, calculado à
parte) sem alterar o restante. Sem um operador de spread, o único jeito é reconstruir o objeto
inteiro, campo a campo, revalidando manualmente a opcionalidade de cada um com `?? default`:

```
out += [{
    titulo: d.titulo,
    diagram_type: d.diagram_type,
    descricao: d.descricao,
    descricao_fontes: d.descricao_fontes,
    escopo: d?.escopo ?? {nome: "", descricao: "", fontes: []},
    ambiente: d?.ambiente ?? "",
    elementos: d?.elementos ?? [],
    relacoes: d?.relacoes ?? [],
    diagrama_mermaid: d?.diagrama_mermaid ?? "",
    avisos_lote: avisos[i],
}]
```

Nove campos reescritos só para anexar um décimo — e o comentário no próprio arquivo já registra
isso como limite conhecido, não escolha de estilo: *"mhl nao tem spread nem atribuicao de campo,
dai a reconstrucao campo a campo"* (linha 482). O mesmo padrão se repete, em menor grau, em
`historia_spec.mh` e `plano_model.mh`. Note que isto é diferente de mutar um objeto existente
(`obj["campo"] = valor`, que já funciona e é usado em `auto_review.mh`'s `tag_items`) — o caso
aqui é produzir uma **cópia nova** preservando a maioria dos campos, sem tocar no objeto original.

**Sugestão:** em vez de spread estilo JS/TS (`{...obj, campo: valor}`), uma expressão `with` estilo
C# (`obj with { campo: valor }`) — sintaticamente mais explícita sobre a intenção ("cópia de `obj`
com estes campos sobrescritos", não "misture estes objetos"), e evita a ambiguidade de spread com
múltiplas fontes ou de spread dentro de um literal que também declara campos novos não presentes no
original. O exemplo acima viraria:

```
out += [d with {
    avisos_lote: avisos[i],
}]
```

sem tocar nos outros 9 campos, e sem precisar repetir `?? default` para cada um — a leitura dos
campos preservados vem do valor original de `d`, não de uma reconstrução literal. Continua sendo
uma cópia (não mutação): `d` original não é alterado, o mesmo comportamento que `{...obj, ...}`
teria, só com sintaxe diferente. Caso a linguagem prefira manter `{...}` como marcador universal de
"literal de objeto" (evitando uma palavra reservada nova, `with`, num operador infixo), a mesma
semântica também poderia ser expressa como método de valor — `d.with({avisos_lote: avisos[i]})` —
mantendo tudo dentro da sintaxe de chamada já existente, ao custo de ficar menos declarativo que a
forma infixa.

## 3. Sem forma nativa de iterar com índice

`for (var item in lista)` não expõe o índice da iteração atual. Quando o índice é necessário — o
caso mais comum encontrado é casar cada item de uma lista de saída com um valor pré-calculado no
mesmo índice de outra lista (`avisos[i]` acima) — o padrão que se formou no código real é declarar
um contador antes do laço e incrementá-lo manualmente dentro dele. Confirmado em **10 sites
diferentes**, todos com a forma exata `var i = 0` antes do `for` + `i = i + 1` no fim do corpo:

- [`shared/artifacts/c4_diagram.mh`](../workflows/shared/artifacts/c4_diagram.mh) — linhas 114, 166, 218, 499
- [`shared/artifacts/der_model.mh`](../workflows/shared/artifacts/der_model.mh) — linhas 75, 137
- [`shared/artifacts/plano_model.mh`](../workflows/shared/artifacts/plano_model.mh) — linha 67
- [`shared/artifacts/historia_spec.mh`](../workflows/shared/artifacts/historia_spec.mh) — linha 196
- [`shared/core/dir_clear.mh`](../workflows/shared/core/dir_clear.mh) — linha 37
- [`work_item/actions.mh`](../workflows/work_item/actions.mh) — linha 266

Dez sites reimplementando à mão a mesma contagem que o próprio `for` já mantém internamente é
sintoma de uma operação comum sem forma nativa — a mesma classe de lacuna que já levou o runtime a
ganhar `string.matches(...)` para classes de caractere.

**Sugestão:** `for (i, item in lista) { ... }` (segunda variável de binding ativa o modo indexado,
sem quebrar a forma de uma variável já existente) ou um método `.enumerate(): {index, value}[]` em
`array`.

## 4. `input` reatribuído não sobrevive a `goto` + resume — pegadinha de semântica não documentada, já causou loop infinito em produção

Registrado no próprio código de [`workflows/discovery/discovery.mh:72-81`](../workflows/discovery/discovery.mh#L72-L81):
uma versão anterior do `step Gate` tentava "consumir" o `input feedback` fazendo `feedback = ""`
depois de usá-lo, contando com a mesma persistência entre reentradas que uma `var` de pipeline já
tem (`pending_data`, por exemplo, sobrevive normalmente a um `goto` seguido de resume). Isso **não
funciona para `input`**: reatribuído dentro do corpo de um step, ele não persiste através de um
resume seguinte — volta a ser re-vinculado ao valor original da chamada/resume toda vez que o step
é reexecutado.

O comentário registra a consequência real: *"confirmado por spike isolado (LoopProbe) que a versao
anterior deste Gate ... entrava num laco infinito de verdade: um resume real bateu em producao,
cada volta era uma chamada de LLM nova."* A correção foi introduzir uma `var feedback_consumed`
paralela só para guardar o que `feedback = ""` deveria ter guardado.

A regra em si pode ser proposital — mas essa assimetria entre `input` e `var` quanto à persistência
através de `goto`+resume não aparece na documentação de referência, e só foi descoberta via
incidente real seguido de spike dedicado.

**Sugestão:** documentar explicitamente, na seção de `input` de `Docs-Specification.dc.html`
(idealmente ao lado da explicação de checkpoint/resume): *reatribuir um `input` dentro de um step
não persiste através de um resume seguinte — use `var` para qualquer estado que precise sobreviver
a um `goto` seguido de pausa/resume.* Um aviso de `mhl lint` quando um `input` é reatribuído dentro
de um step que também contém `pause()` seria ainda melhor — sinalizaria exatamente a classe de bug
que causou o incidente.

## 5. `mhl lint` não sinaliza identificador nunca declarado dentro do corpo de um hook

[`workflows/discovery/discovery.partial.hook.mh`](../workflows/discovery/discovery.partial.hook.mh)
tem um hook `step_end` (telemetria best-effort) cujo corpo referencia `eventType` — uma variável
que **não existe em escopo nenhum**: não é campo de `StepContext` (que só tem `pipeline, step,
index, total, error` — `internal/lang/types/types.go`), não é input/var do workflow, não é
parâmetro de nada. `mhl lint workflows/discovery/`, rodado contra este arquivo real, devolve `No
problems found.`

Reproduzido isolado, contra o build atual desta branch, para confirmar que é bug de runtime de
verdade, não algo resolvido por escopo que eu não tenha visto:

```
step_end: (ctx: StepContext) -> {
    try {
        var data = { seen: undeclared_name }
        log.info("built")
    } catch (e) {
        log.error("swallowed: ${e}")
    }
}
```

- `mhl lint` no arquivo isolado → `No problems found.`
- `mhl run --format json` → **`"ok": true`**, execução completa; o único rastro é uma linha
  `[ERROR] swallowed: undefined variable "undeclared_name"` dentro do campo `log` (texto livre,
  não uma falha estruturada).
- Removendo o `try`/`catch` do mesmo repro, o comportamento passa a ser o esperado: `mhl run` falha
  de verdade, com `"ok": false`, `"kind": "step_failed"` e a posição exata do erro.

O runtime está correto — uma exceção capturada por `catch` é, por definição, tratada. O gap é que
`mhl lint`, que já resolve formas de chamada bem mais sofisticadas em outros lugares (`tool`,
`memory`, `agent`, alvos de `goto`, placeholders de `args:`), não tenta resolver identificadores
livres contra o conjunto de nomes visíveis no escopo de um hook (parâmetros + campos do tipo do
parâmetro + inputs/vars do pipeline + declarações importadas). Um typo dentro de um hook
best-effort — o padrão mais comum para telemetria/observabilidade, que existe justamente para
nunca quebrar o fluxo principal — fica permanentemente invisível em produção: a telemetria deste
projeto nunca funcionou, e o único sintoma é um warning genérico indistinguível de falha de rede.

**Sugestão:** checagem de lint (mesmo que best-effort) para identificador referenciado em corpo de
hook/lambda que não corresponde a nenhum parâmetro, campo de tipo builtin
(`SessionContext`/`StepContext`/`FailureContext`), símbolo importado ou declaração visível no
escopo léxico.

## 6. Sem receita oficial para reduzir duplicação de step Generate/Commit entre `workflow`s irmãos

Quantificado comparando [`workflows/discovery/`](../workflows/discovery/) (fragmentado em
`partial workflow`) com [`workflows/delivery/delivery.mh`](../workflows/delivery/delivery.mh)
(monolítico): os pares `*Generate`/`*Commit` de `brief`, `requisitos`, `adr`, `der`, `diagramas`,
`historias` e `plano` existem **quase idênticos nos dois workflows**, só trocando `self.x` por `x`
e alguns literais — 13 sites de commit (`PageShell`+`ArtifactMeta.line`+`ArtifactData.write`+
`fs.write`) e 13 de geração (`Writer.generate`/`AutoReview.generate`) só entre os dois arquivos.

Isso **não é, no fundo, um limite de linguagem**: é a combinação de duas regras já corretas —
nenhuma chamada indireta (`self.` dentro de um `tool`, `after`, `router.delegate`) herda escopo de
quem a originou, só parâmetros e retorno explícitos atravessam a fronteira; e `goto`/`pause()`/
`complete()` só são válidos dentro do corpo textual de um `step`, nunca dentro de um `tool` chamado
por ele — razoável, já que o grafo estático de steps (usado por `mhl lint`, `--resume`/checkpoint e
pela lista `reached`) depende de todo `goto` ser conhecível sem rodar o programa.

Dado isso, o lado *Generate* de cada par já está no limite possível de fatoração (a chamada ao
modelo é feita por uma função compartilhada; só sobra copiar 3-4 valores de volta e um `goto`
fixo). Mas o lado *Commit* — montar `PageShell`, gravar `ArtifactData`/`fs.write`, montar o dict de
resultado — **não tem `goto` nenhum antes do último `goto Done`**, e por isso já seria extraível
hoje para um `tool` comum (`discovery_result = ArtifactCommit.finish(...); goto Done`) sem esperar
nenhuma mudança de linguagem. Isso não foi feito em nenhum dos dois workflows.

**Sugestão (só para os docs, não para o runtime):** um exemplo em `Docs-Reference.dc.html`, na
seção de composição de `workflow`, mostrando explicitamente esse padrão — "o lado sem controle de
fluxo de um par Generate/Commit pode virar um `tool` comum reusado entre `workflow`s irmãos; só o
`goto` final precisa ficar no step". Sem um exemplo oficial, times deixam de perceber a extração
possível e reinventam a duplicação — foi exatamente o que aconteceu aqui, em dois workflows
escritos pela mesma equipe, com meses de distância um do outro.
