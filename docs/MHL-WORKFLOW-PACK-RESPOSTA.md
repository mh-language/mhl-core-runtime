# Resposta: workflows pack cifrado no `mhl`

**Para:** time do Dumont
**De:** equipe da linguagem (mhl)
**Referência:** `MHL-WORKFLOW-PACK-SPEC.md` (formato `DMPK` v2)
**Disponível a partir de:** `mhl <versão>`

---

## 1. Resumo

O pedido está implementado, com uma mudança de desenho em relação à proposta de
vocês: **o `mhl` passa a ser dono do formato nas duas pontas.** É ele que gera o
pack (`mhl pack`) e é ele que o lê (`mhl serve mcp --workflows-pack`). O host
não precisa mais cifrar, decifrar nem gravar `.mh`/`.prompt.md` em lugar nenhum.
O que sobra para o host é guardar a chave e repassá-la no spawn.

O formato continua sendo o `DMPK` v2 do spec, byte a byte. O vetor da §5 passa
como teste unitário no `mhl`. Por isso o `pack_format.rs` de vocês deixa de ser
necessário (ver §5 abaixo).

## 2. Gerar o pack no build: `mhl pack`

```sh
# chave nova: o stdout recebe só a chave, o resumo vai para o stderr
KEY=$(mhl pack ./workflows -o workflows.dmpk --new-key)

# chave existente (ex.: segredo do CI)
MHL_SERVE_WORKFLOWS_KEY=<64 hex> mhl pack ./workflows -o workflows.dmpk
# packed 181 file(s), 12 workflow(s) (...) into workflows.dmpk
```

Regras do comando:

- **A chave vem só pelo ambiente** (`MHL_SERVE_WORKFLOWS_KEY`, 64 caracteres
  hex), nunca por flag. `--new-key` junto com a variável já definida é erro.
- **O que entra:** todos os arquivos não ocultos da árvore, ordenados por
  caminho. Qualquer entrada que comece com `.` (`.mhl/`, `.git`, `.DS_Store`)
  fica de fora, e um symlink é erro (melhor que um arquivo faltando em
  silêncio).
- **O pack é verificado antes de ser gravado:** o comando reabre o pack e
  carrega os workflows dele exatamente como o `serve` faria. Import quebrado,
  prompt faltando, erro de sintaxe ou árvore sem workflow fazem o build falhar
  ali mesmo, com código de saída diferente de zero e nenhum arquivo escrito.
- A gravação é atômica, com permissão `0600`.

No `build.rs`, isso vira uma chamada só:

```rust
let status = std::process::Command::new(mhl_bin)
    .args(["pack", "workflows", "-o", out_dir.join("workflows.dmpk").to_str().unwrap()])
    .env("MHL_SERVE_WORKFLOWS_KEY", &key_hex) // do segredo de build, nunca de um arquivo versionado
    .status()?;
assert!(status.success(), "mhl pack falhou");
```

Usem para o `mhl pack` o mesmo binário `mhl` que vai no instalador, para que
gerador e leitor sejam sempre da mesma versão.

## 3. Ler o pack em runtime: `mhl serve mcp --workflows-pack`

```sh
MHL_SERVE_WORKFLOWS_KEY=<64 hex> \
  mhl serve mcp --http --addr 127.0.0.1:8711 \
    --workflows-pack /caminho/workflows.dmpk /caminho/runtime-root
```

| Entrada | Papel |
| --- | --- |
| `--workflows-pack <arquivo>` (ou `MHL_SERVE_WORKFLOWS_PACK`) | fonte do programa: `.mh`, `prompt … from`, `skill … from`, `schema … from`. Lido só em memória |
| `MHL_SERVE_WORKFLOWS_KEY` | a chave, 64 hex. Obrigatória com pack; definida sem pack é erro |
| `[dir]` posicional | **não é mais lido como fonte.** É só a raiz onde se compõem os caminhos entregues a subprocessos |

No spawn (`src-tauri/src/mhl/client.rs`), a chave vai pelo env do processo
filho, nunca por `arg`:

```rust
Command::new(mhl_bin)
    .args(["serve", "mcp", "--http", "--addr", "127.0.0.1:8711",
           "--workflows-pack", pack_path, runtime_root])
    .env("MHL_SERVE_WORKFLOWS_KEY", &key_hex)
    .spawn()?;
```

**`--workflows-pack` exige `--http`.** O modo stdio recusa o pack com erro
claro. Se precisarem dele em stdio, nos avisem.

### O diretório posicional (`runtime-root`)

Só precisa conter os arquivos que **um processo externo abre por caminho**:
os scripts `.sh`, o JSON de preços, os templates HTML e os schemas entregues
por caminho ao Codex. Esses arquivos precisam estar **no mesmo layout relativo
do pack**. O `path` de um `SchemaFile` passa a ser
`<runtime-root>/<caminho no pack>`, e o `content` vem do pack.

Isso responde a pergunta 6 do spec: o `mhl` não lê `LEIBNIZ_WORKFLOWS_ROOT`.
Definam essa variável com o mesmo valor do diretório posicional, e
`WorkflowsRoot.resolve()` e `SchemaFile.path` passam a apontar para o mesmo
lugar.

Como esses arquivos não são sigilosos ("estrutura e cola", nas palavras do
spec), vocês podem distribuí-los em claro no instalador e apontar o
`runtime-root` direto para essa pasta. Não precisam mais decifrar nada para
materializá-los. Gerem os dois a partir da mesma árvore no mesmo build, para
o conteúdo de um schema no pack e a cópia em disco nunca divergirem.

## 4. Garantias e erros

- **Nenhum `.mh` nem `.prompt.md` toca o disco.** Nada vai para `--state-dir`,
  `TMPDIR` ou cache. O `mhl` não tem cache de compilação em disco (pergunta 2
  do spec).
- **Falha é total.** Nos casos abaixo o processo sai com erro antes de servir
  qualquer coisa:

  ```
  chave errada / 1 byte alterado → pack: entry N does not authenticate (wrong key or tampered pack)
  sem chave                      → --workflows-pack needs its key in MHL_SERVE_WORKFLOWS_KEY
  chave malformada               → MHL_SERVE_WORKFLOWS_KEY: want 64 hex characters
  pack truncado / bytes sobrando → pack: truncated at entry N / pack: N trailing bytes …
  ```

  As mensagens nunca contêm a chave nem texto do programa. O log de startup
  mostra o caminho do `.dmpk`, não o conteúdo.
- **A chave sai do ambiente antes de qualquer coisa rodar.** Nenhum CLI de
  agente nem `cmd.exec` a herda, e `env("MHL_SERVE_WORKFLOWS_KEY")` dentro de
  um workflow devolve vazio.
- **O recurso MCP `mhl://workflow/<name>/source` foi removido**, em todos os
  modos. Nenhum cliente MCP consegue mais ler o `.mh` pelo servidor. O
  manifesto `mhl://workflow/<name>` continua existindo.

### Limite conhecido

`ps -E` (macOS) ou `/proc/<pid>/environ` (Linux) **no próprio processo
`mhl`** ainda mostram a chave para o mesmo usuário, porque apagar a variável
não reescreve o bloco de ambiente original do processo. Os processos filhos
estão limpos, que é o critério 3 do spec. O próximo degrau é a chave por fd
herdado (`--workflows-key-fd <n>`), que já está no nosso radar. Avisem se
isso for prioridade para a Fase 3 de vocês.

## 5. O que muda do lado de vocês

1. **`build.rs`:** trocar o produtor Rust por `mhl pack` (§2).
2. **`workflow_pack.rs`:** remover a decifragem e a árvore efêmera dos
   `.mh`/`.prompt.md`, ou seja, o paliativo atual. O host só precisa do
   caminho do pack e da chave.
3. **`pack_format.rs`:** pode sair. O formato agora é responsabilidade do
   `mhl`, e o golden test da §5 vive no nosso repo. Se quiserem manter o
   leitor Rust como verificação de interoperabilidade, abram nele um pack
   gerado pelo `mhl pack`. Isso nós não testamos, e seria a única checagem
   cruzada que falta.
4. **`mhl/client.rs`:** adicionar `--workflows-pack` e a env `MHL_SERVE_WORKFLOWS_KEY`
   no spawn (§3).
5. **`runtime-root`:** só os arquivos abertos por caminho, no layout do pack,
   e `LEIBNIZ_WORKFLOWS_ROOT` com o mesmo valor do diretório posicional.

## 6. Recomendações para os workflows

- **Mandem o prompt pela entrada padrão.** Hoje, por padrão, o prompt
  renderizado vai como argumento na linha de comando do CLI do agente, e
  `argv` é legível por qualquer processo da máquina (`ps`), não só pelo mesmo
  usuário. Declarem nos agentes:

  ```mhl
  agent Writer {
      command: "codex"
      args: ["exec", "-"]
      stdin: "${prompt}"
  }
  ```

  Com `stdin:` reivindicando `${prompt}`, o `mhl` não acrescenta o prompt ao
  `argv`. Ajustem `args` conforme cada CLI lê da entrada padrão.
- **Não leiam `.mh`/`.prompt.md` com `fs.read`.** No modo pack esses arquivos
  não existem em disco, e a chamada falha.
- **`prompt_log.jsonl` (§7.1 do spec):** continua sendo decisão do workflow.
  O `mhl` não tem `memory` cifrada em repouso (pergunta 5 do spec). Até lá,
  recomendamos gravar só o `sha256` do prompt, ou o texto apenas com um flag
  de dev ligado.
- Checkpoints em `--state-dir` gravam as variáveis do workflow em claro. Não
  guardem o prompt renderizado numa variável que sobreviva ao passo.

## 7. Como validar (critérios da §8 do spec)

| # | Critério | Como verificar |
| --- | --- | --- |
| 1 | mesma lista de tools e schemas | `tools/list` no modo diretório e no modo pack devem dar JSON idêntico (é um teste automatizado no `mhl`) |
| 2 | nenhum arquivo com texto de `.mh`/prompt | `lsof -p <pid>` e um `grep -r` por uma frase conhecida de prompt em `runtime-root`, `--state-dir` e `TMPDIR` |
| 3 | filhos sem a chave | `ps -E -p <pid-do-filho>` / `/proc/<pid>/environ` |
| 4 | vetor da §5 | teste unitário no `mhl` (`internal/pack`) |
| 5 | 1 byte alterado, chave errada, ausente, truncado | erro e nada servido (§4 acima) |
| 6 | modo diretório inalterado | suíte do `mhl` passa sem mudanças |
| 7 | Discovery → Desenvolvimento contra o pack | do lado de vocês |

Os critérios de 1 a 6 nós já rodamos contra o binário real. O 7 depende de
vocês. Qualquer divergência, nos mandem o erro exato do `mhl`: ele foi feito
para dizer o que está errado sem vazar o que está cifrado.
