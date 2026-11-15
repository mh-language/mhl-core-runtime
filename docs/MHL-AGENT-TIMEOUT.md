# Pedido de mudança no `mhl`: `timeout:` no bloco `agent`

**Para:** mantenedor do core da linguagem (`mhl`)
**De:** time do Dumont (host que embarca o `mhl` e declara todos os agentes de código como `agent`)
**Versão testada:** mhl v1.6.0 · **Data:** 2026-11-10 · **Status:** pedido aberto

---

## 1. O pedido

Pedimos um campo opcional `timeout:` no bloco `agent`. Quando o prazo vence, o `mhl` encerra o processo do agente e a chamada falha com um erro que o `retry` reconhece.

```mhl
export agent CopilotCoder {
    command: "bash"
    args: [ ... ]
    timeout: 30m          // novo: prazo por tentativa
    retry: {
        max_attempts: 2
        delay: 5s
        retry_on: [500, 503, "rate_limit"]
    }
}
```

Hoje um `agent` espera o processo indefinidamente. Se o CLI travar, a etapa do workflow trava junto, e nada no `.mh` consegue reagir: não chega erro ao `catch`, ao `retry` nem ao `finally`.

## 2. Contexto

Em 2026-11-10, uma chamada ao GitHub Copilot CLI ficou mais de 10 minutos sem responder, e não havia como o MHL encerrá-la.

No Dumont, todo agente é um CLI externo declarado como `agent`:

- **geradores** (só produzem JSON) chamam o CLI direto: `command: "claude"`, `command: "codex"`;
- **agentes de código e leitores** passam por um wrapper bash (`shared/agents/coder-in-dir.sh`), que entra no repositório e chama o CLI.

Essas chamadas duram de segundos a dezenas de minutos e dependem de rede.

### O incidente

O Copilot CLI 1.0.108 rodava como agente de código num spike de integração (detalhes em [COPILOT-CLI.md](COPILOT-CLI.md)):

| Momento | Evento |
| --- | --- |
| +0 s | início da sessão; o agente roda dois comandos com sucesso |
| +4 min 22 s | chamada ao modelo falha: `ETIMEDOUT`, depois de 188 s parada |
| +6 min 54 s | nova tentativa falha: `ECONNRESET`, depois de 141 s parada |
| +6 min 54 s em diante | terceira tentativa, sem nenhum evento |
| +11 min | SIGTERM manual; o CLI grava o resultado com `exitCode: 143` e sai |

Rodando dentro do `mhl`, essa etapa só destravaria quando alguém matasse o processo à mão. O registro está em [copilot-cli/fixtures/falha-rede-sigterm.jsonl](copilot-cli/fixtures/falha-rede-sigterm.jsonl).

Na mesma tarde houve uma segunda falha, já na inicialização: `Model catalog request timed out after 30000ms`, com exit 1. Essa o `retry` atual já cobre, porque o processo termina.

### Não é só o Copilot

Claude, Codex, Devin e Antigravity também dependem de rede, e nenhum garante um tempo máximo de execução. Vários agentes já declaram `retry_on: ["timeout", ...]`, mas isso só pega o timeout que o próprio CLI detecta e reporta. Quando o CLI fica parado sem reportar nada, não há o que pegar.

## 3. Proposta

`timeout:` recebe uma duração, no mesmo formato de `delay: 5s` e de `cmd.exec(..., timeout: 20m)`, e vale **por tentativa**. Sem o campo, o comportamento atual não muda.

Quando o prazo vence:

1. **SIGTERM no grupo de processos inteiro**, não só no PID direto. Nossos agentes de código rodam como `bash coder-in-dir.sh` → `copilot` → subprocessos (build, testes). Se só o bash morrer, o CLI e o build ficam órfãos.
2. **Prazo de graça, depois SIGKILL.** Os CLIs usam o SIGTERM para gravar o estado final: o Copilot emitiu o resultado com `exitCode: 143` antes de sair. Sugerimos 10 s de graça e, se for simples, um campo opcional `kill_grace:`.
3. **A chamada lança um erro.** `Agente.run(...)` deve lançar, e não devolver o que tiver, para que `catch`, `finally` e `retry` funcionem como em qualquer outra falha. Hoje o `finally` de `Coder.call` apaga os arquivos temporários do prompt; com o processo travado, ele nunca roda.
4. **Mensagem reconhecível.** O texto do erro traz `timeout`, o nome do agente e o prazo, seguidos do fim do stderr capturado. Exemplo: `agent CopilotCoder: timeout after 30m`.
5. **A saída parcial fica disponível.** Se a linguagem tiver erro estruturado, o stdout e o stderr recebidos até o encerramento vão junto com o erro. Para nós isso é valioso: o último evento do Copilot traz tokens e custo, e queremos registrar o consumo mesmo de uma chamada abortada.

**Windows:** como não existe SIGTERM, o equivalente é encerrar a árvore de processos (Job Object ou `taskkill /T`). Distribuímos o `mhl-windows-amd64.exe`.

### Desejável, não essencial

- **Prazo por chamada:** `Agente.run(prompt:, schema:, timeout: 45m)`, como já existe em `cmd.exec`. Hoje uma duração não pode ser argumento de método em `.mh`; o `dev_tools.mh` contorna isso com um `if` que escolhe entre dois `cmd.exec`.
- **Prazo vindo de uma expressão:** `timeout: AgentConfig.coder_timeout()`, como já fazemos em `args`, para o usuário poder configurar o prazo no app. Uma string como `"30m"` ou um número de segundos serviria.

## 4. Interação com `retry`

- **O prazo vale por tentativa.** Com `max_attempts: 2`, o pior caso é `2 × timeout + delay`. Se vocês preferirem um teto global (`total_timeout:`), também nos atende, desde que o prazo por tentativa continue existindo.
- **Repetir ou não depende só de `retry_on`.** Como a mensagem contém `timeout`, quem já declara `retry_on: ["timeout"]` passa a repetir automaticamente. É o que queremos para os geradores, que são curtos e idempotentes. Os agentes de código não declaram `"timeout"` e não devem repetir: uma segunda implementação em cima de uma primeira pela metade é pior que uma falha clara.
- **Código de saída.** Um processo encerrado pelo timeout é sempre erro de timeout, mesmo que o CLI saia com 0 no tratamento do SIGTERM. Fora do timeout, nada muda.

## 5. Alternativas consideradas

| Alternativa | Por que não basta |
| --- | --- |
| Vigia no `coder-in-dir.sh` (bash manda SIGTERM depois de N minutos) | Só protege quem passa pelo wrapper; os geradores chamam `claude` e `codex` direto. O macOS não traz `timeout`, e no Windows o wrapper depende do Git Bash. |
| Trocar `agent` por `cmd.exec(..., timeout:)` | Perde `retry`, `stdin:`, a substituição de `${prompt}`/`${schema}` e a semântica de agente. Seriam 14 agentes reescritos à mão. |
| Usar o timeout de cada CLI | O Copilot não tem timeout por execução, e os outros têm flags diferentes (ou nenhuma). Cada um ganharia um tratamento próprio, e o caso de "parado sem reportar nada" continuaria descoberto. |

A primeira alternativa é a que vamos usar enquanto o `timeout:` não existir, só nos agentes de código.

## 6. Critérios de aceite

Todos podem ser testados com `mhl test`, com agentes falsos como os que já usamos (`command: "bash"` + script):

1. Um agente com `sleep 5` e `timeout: 1s` falha em cerca de 1 s mais a graça, com mensagem contendo `timeout` e o nome do agente.
2. O agente roda um bash que inicia `sleep 100 &` e espera. Depois do timeout, nenhum `sleep` sobra (o grupo de processos foi encerrado).
3. Um script que trata SIGTERM, imprime uma linha e sai com 143: a linha aparece na saída parcial do erro, e o script sai antes do SIGKILL.
4. Um script que ignora SIGTERM morre por SIGKILL ao fim da graça.
5. Com `retry_on: ["timeout"]` e `max_attempts: 2`, o agente roda duas vezes; sem `"timeout"`, roda uma vez.
6. O `finally` em volta de `Agente.run(...)` executa depois do timeout.
7. Um agente sem `timeout:` se comporta exatamente como hoje.
8. No Windows, os casos 1 e 2 passam com uma árvore de processos equivalente.

## 7. Compatibilidade

- **Campo opcional e retrocompatível.** Nenhum `.mh` existente muda de comportamento.
- **Ferramentas:** o `mhl lint` e o `mhl lsp` (vscode-mhl) precisam aceitar o campo e validar a duração.
- **Do lado do Dumont:** quando a versão com `timeout:` sair, adicionamos o campo aos agentes (geradores com cerca de 10 min, agentes de código com cerca de 45 min, ainda a calibrar) e removemos o vigia do wrapper.
