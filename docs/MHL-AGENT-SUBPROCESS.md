# Pedido de mudança no `mhl`: `agent` sem wrapper de shell

**Para:** mantenedor do core da linguagem (`mhl`)
**De:** time do Dumont (host que embarca o `mhl` e declara todos os agentes de código como `agent`)
**Versão testada:** mhl v1.7.1 · **Data:** 2026-11-10 · **Status:** pedido aberto
**Pedido anterior:** [MHL-AGENT-TIMEOUT.md](MHL-AGENT-TIMEOUT.md) (`timeout:`, atendido na v1.7.1)

---

## 1. O pedido

Pedimos quatro recursos no bloco `agent`. Com eles, um agente que hoje precisa de um wrapper bash pode ser declarado só em `.mh`:

1. **`cwd:`**: o diretório de trabalho do processo, avaliado a cada chamada.
2. **Spread em `args`**: `...expr` insere uma lista de strings, que pode vir vazia.
3. **`input` no `agent`**: parâmetros por chamada (`Agente.run(prompt:, model: "x")`) que `args`, `cwd`, `stdin` e `log` podem ler.
4. **`log:` com expressão**: o caminho do log ao vivo calculado a cada chamada, como já acontece com `args`.

Um ajuste menor: numa saída com código diferente de zero, a mensagem de erro deve trazer o fim do stdout, como o timeout da v1.7.1 já faz.

E três correções: hoje `cwd:`, os argumentos extras de `run` e `log:` com expressão são ignorados **sem aviso** na execução (seção 3).

Exemplo do que queremos escrever:

```mhl
export agent CopilotCoder {
    input repo: string
    input model: string = ""
    input effort: string = ""
    input dirs: string[] = []
    input live: string = ""

    command: "copilot"
    cwd: repo
    args: [
        "--model", Model.or_default(model),
        ...Effort.flags(model, effort),        // [] com o modelo "auto"
        ...dirs.flat_map((d) -> ["--add-dir", d]),
        "--output-format", "json",
        "--sandbox"
    ]
    stdin: "${prompt}"
    log: live
    timeout: AgentConfig.coder_timeout()
}

var out = CopilotCoder.run(prompt: texto, repo: "/abs/repo", model: "gpt-5.6-luna", live: caminho)
```

## 2. Contexto: o que o wrapper faz hoje

Os 10 agentes de código e leitores, e 2 dos 5 geradores (12 dos 15 agentes de produção), rodam como `command: "bash"` + [`shared/agents/coder-in-dir.sh`](../resources/workflows/shared/agents/coder-in-dir.sh). O wrapper existe só para compensar o que falta no `agent`:

| O wrapper faz | Por quê |
|---|---|
| `cd <repo>` antes de chamar o CLI | não há `cwd:`; Devin, Claude e Antigravity não têm flag de CWD |
| Resolve `@PROMPT@`, `@EXPORT@`, `@USAGE@` e `@HERE@` como caminhos absolutos | o `${prompt}` chega relativo ao CWD do `mhl`, e o `cd` quebraria o caminho |
| `@STDIN@`: manda o arquivo de prompt pelo stdin | o prompt só chega como caminho, porque o `${schema}` está ocupado com o repositório |
| `<prompt>.model` e `<prompt>.effort`: troca o valor depois de `--model`, `--effort` e `--reasoning-effort` | `args` é fixo por declaração; não há parâmetro por chamada (o modelo é escolhido por feature) |
| `@ADDDIRS@`: um `--add-dir <d>` para cada pasta extra | `args` não aceita lista de tamanho variável |
| Tira `--reasoning-effort <v>` quando o valor é vazio ou o modelo é `auto` | `args` não omite elemento por condição; o Copilot recusa esforço com `auto` |
| `<prompt>.live`: `tee` do stdout para um arquivo que o app lê enquanto o agente trabalha | `log:` não aceita caminho por chamada |
| Codex: copia o motivo da falha, que vem num evento do stdout, para o stderr | numa falha, o erro do `agent` mostra só o stderr ("exit status 1") |

Para passar esses dados por chamada, o `Coder.call` grava até seis arquivos vizinhos do prompt (`.dirs`, `.model`, `.effort`, `.live`, `.export.json` e `.usage.json`) e os apaga num `finally`.

O custo disso:
- **Windows:** todo agente de código depende do Git Bash.
- **Duas linguagens:** cada CLI novo mexe no bash e no `.mh`, como aconteceu com o Copilot nesta semana.
- **Protocolo de arquivos:** um protocolo de arquivos vizinhos que só existe para atravessar a fronteira entre o MHL e o processo.

## 3. O que a v1.7.1 faz hoje

Testado com agentes mínimos (`mhl test` e `mhl lint`):

| Teste | Resultado |
|---|---|
| `cwd: "/tmp"` num `agent` | o lint acusa `unknown property "cwd"`; **a execução ignora o campo** e roda no CWD do `mhl` |
| `args: Tool.lista()` | `agent "X" args must be an array of strings` |
| `args: ["x", ...Tool.lista()]` | erro de parse em `..` |
| `X.run(prompt: "p", model: "m")` com `"${model}"` nos args | **`model:` é aceito e ignorado**; o `"${model}"` passa literal |
| `log: "c1.log"` (literal) | ✅ grava o stdout no arquivo **ao vivo**, linha a linha |
| `log: Tool.caminho()` | **não grava nada nem avisa**; o lint também não acusa |
| `before: (p) -> ...` | `before takes no parameters`; não serve para montar os args |
| `os.cwd()` | ✅ existe, e com ele os caminhos absolutos podem ser montados no próprio MHL |

As três linhas em negrito são falhas silenciosas, e corrigi-las vale mesmo sem o resto do pedido. Um erro de configuração vira um agente rodando no lugar errado ou com o modelo errado.

## 4. Proposta

### 4.1 `cwd:`

- Recebe uma expressão de string, avaliada a cada chamada (como `args`). Um caminho relativo é resolvido contra o CWD do `mhl`.
- O diretório precisa existir. Se não existir, a chamada falha antes de iniciar o processo, com `agent X: cwd not found: <dir>`.
- Sem o campo, nada muda.
- **Desejável:** `Agente.run(..., cwd:)` por chamada, como o `timeout:` da v1.7.1. Com `input` (4.3) isso deixa de ser necessário.

### 4.2 Spread em `args`

- `...expr` dentro do array de `args` insere os elementos de `expr`, que precisa ser `string[]`.
  - `[]` não insere nada.
  - `null` é erro, para não esconder uma expressão que deu errado.
- **Alternativa equivalente:** aceitar `args: <expressão que devolve string[]>`. O spread nos atende melhor, porque mantém a parte fixa legível.
- **Precisa funcionar com `if` como expressão:** `...(if (e.is_empty()) [] else ["--reasoning-effort", e])`.

### 4.3 `input` no `agent`

- Mesma sintaxe e semântica do `input` de `workflow`: `input nome: tipo = padrão`.
- `Agente.run(prompt:, schema:, nome: valor)` preenche os inputs. Um input sem padrão e sem valor é erro de chamada.
- Os inputs são visíveis nas expressões de `args`, `cwd`, `stdin`, `log` e `timeout`.
- **Argumento desconhecido em `run` vira erro**, na execução e no `lint`. Hoje ele é ignorado.
- `prompt` e `schema` continuam reservados. `${prompt}` e `${schema}` seguem funcionando como hoje.
- **`fallback`:** o `run` repassa os mesmos inputs ao agente de fallback. Se o fallback não declarar um input recebido, é erro de declaração no `lint`.

### 4.4 `log:` com expressão

- **Caminho:** a expressão é avaliada a cada chamada, como `args`. Um caminho vazio (`""`) desliga o log nessa chamada, que é o caso das chamadas fora do Desenvolvimento.
- **Truncar ou acrescentar:** o arquivo é truncado no início de cada **tentativa**, para o app não misturar uma tentativa com a anterior. Se preferirem acrescentar, pedimos um separador ou um campo `log_mode: "truncate" | "append"`.
- **Pasta inexistente:** a pasta do arquivo é criada se faltar. Hoje o wrapper faz `mkdir -p`.
- **Gravação:** linha a linha, como o `log:` literal já faz.

### 4.5 Fim do stdout no erro

- Quando o processo sai com código diferente de zero, a mensagem inclui o fim do stdout, como a v1.7.1 já faz no timeout: `partial stdout: …` com os últimos ~2 KB.
- **O `retry_on` continua casando só com o que casa hoje** (stderr e código de saída), nunca com o stdout. Assim o JSONL do agente não provoca nova tentativa por acaso. Foi o que verificamos no timeout da v1.7.1, e queremos o mesmo aqui.

## 5. Alternativas consideradas

| Alternativa | Por que não basta |
|---|---|
| Manter o wrapper | É o que temos hoje. Funciona, mas mantém o Git Bash obrigatório no Windows, duas linguagens por agente e o protocolo de arquivos vizinhos. |
| Um `agent` por combinação (com e sem esforço, com N pastas extras) | Explode combinatoriamente: a quantidade de pastas extras é variável. |
| Flag de CWD de cada CLI | Só Codex (`--cd`) e Copilot (`-C`) têm; Devin, Claude e Antigravity não têm. Resolveria 2 de 5. |
| `cmd.exec` no lugar de `agent` | Perde `retry`, `timeout`/`kill_grace`, `stdin:` e `log:`; seriam 15 agentes reescritos sobre um primitivo mais fraco. |

## 6. Critérios de aceite

Todos testáveis com `mhl test` e agentes falsos (`command: "bash"` ou `pwd`):

1. `cwd: dir` faz `pwd` imprimir `dir`; um `cwd` inexistente falha antes de iniciar o processo.
2. `cwd:` lendo um `input` muda de diretório entre duas chamadas do mesmo agente.
3. `["a", ...lista, "b"]` com `lista = ["x", "y"]` produz `a x y b`; com `[]`, `a b`; com `null`, erro.
4. Spread de `if (c) [] else ["--f", v]` omite ou inclui o par conforme `c`.
5. `run(prompt:, model: "m")` com `input model: string = ""` faz `args` receberem `m`; sem o argumento, recebem `""`.
6. `run(prompt:, modelo: "m")`, com um nome não declarado, é erro em `mhl test` e em `mhl lint`.
7. `log: caminho_input` grava o stdout ao vivo no arquivo daquela chamada; com `""`, não grava nada.
8. Com `retry`, o log da segunda tentativa não contém as linhas da primeira, ou as separa, conforme 4.4.
9. Uma saída com código 1 que imprimiu no stdout traz `partial stdout:` no erro, e `retry_on` não casa com o texto do stdout.
10. As três falhas silenciosas da seção 3 viram erro ou aviso no `lint` e na execução.
11. No Windows, 1, 2 e 7 passam sem bash no PATH.
12. Um `agent` sem nenhum dos campos novos se comporta exatamente como na v1.7.1.

## 7. Compatibilidade

- **Campos novos são opcionais.** O único comportamento que muda é o argumento desconhecido em `run`, que deixa de ser ignorado. Isso conserta erros, mas pode quebrar um `.mh` que hoje passa argumentos a mais sem perceber. Se preferirem, isso pode ser um aviso numa versão e erro na seguinte.
- **Ferramentas:** o `mhl lint` e o `mhl lsp` (vscode-mhl) precisam conhecer `cwd`, `input`, o spread e `log` com expressão.

## 8. Do lado do Dumont

Quando a versão sair:

1. **Os 12 agentes que passam pelo wrapper passam a declarar `command:` com o próprio CLI,** como `command: "copilot"`, em vez de `command: "bash"` mais o wrapper. Ficam com:
   - `cwd:` vindo de um `input repo`;
   - `stdin:` com o texto do prompt;
   - os modelos, o esforço e as pastas extras como `input`;
   - `log:` com o caminho ao vivo.
2. **O `Coder.call` deixa de gravar os arquivos vizinhos.** Só o export do Devin e o consumo do Copilot continuam em arquivo, porque os próprios CLIs gravam neles. Os caminhos absolutos vêm de `os.cwd()`.
3. **O que o wrapper fazia passa para o parser:**
   - o motivo da falha do Codex passa a vir de `partial stdout`;
   - o filtro do Claude ao vivo (`stream-json`, de onde só o `result` interessa) passa para o `extract_claude`.
4. **O `coder-in-dir.sh` é removido.** Com ele sai a exigência de Git Bash no Windows para os agentes de código.
