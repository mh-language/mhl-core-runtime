# Pedido de implementação no `mhl`: carregar workflows de um pack cifrado

**Para:** equipe da linguagem (mhl / Leibniz core runtime)
**De:** time do Dumont (host Tauri que embarca o `mhl`)
**Versão do formato:** `DMPK` v2 · **Status:** formato congelado e testado no host; falta o lado do motor

---

## 1. O pedido em um parágrafo

O Dumont é distribuído na máquina do cliente e vai passar a ser licenciado.
Os workflows `.mh` e os prompts `.prompt.md` são o produto, e hoje eles chegam
ao `mhl` como uma **árvore de arquivos em texto plano no disco do cliente** —
qualquer usuário lê a orquestração inteira. O host já resolveu metade do
problema: os workflows viajam no instalador dentro de um artefato cifrado
(AES-256-GCM) e são decifrados só em memória. O que falta é o `mhl` saber
**carregar o programa desse artefato em vez de um diretório**, recebendo a
chave do host no spawn. Enquanto isso não existe, o host decifra e grava uma
árvore em claro num diretório efêmero — ou seja, **o texto plano continua
existindo em disco durante a sessão**, que é exatamente o que queremos
eliminar.

O pedido concreto, no CLI:

```sh
# hoje (continua valendo para sempre, é o modo de desenvolvimento)
mhl serve mcp --http --addr 127.0.0.1:8711 ./workflows

# o que pedimos (modo empacotado)
MHL_SERVE_WORKFLOWS_KEY=<64 hex> \
  mhl serve mcp --http --addr 127.0.0.1:8711 \
    --workflows-pack /caminho/workflows.dmpk ./runtime-root
```

## 2. A parte que decide o desenho: o que pode viver só em RAM

Não é tudo. Vasculhamos a árvore e ela tem **dois grupos com destinos
diferentes**:

| Grupo | Arquivos | Tamanho | Quem consome | Destino |
| --- | --- | --- | --- | --- |
| **Programa e prompts** | 132 (`*.mh`, `*.prompt.md`) | 129 KiB | só o `mhl` | **só RAM** — nunca toca o disco |
| **Recursos por caminho** | 49 (`*.json`, `*.html`, `*.sh`) | ~2 MiB | **subprocessos**, por caminho | diretório efêmero `0700` do host |

O segundo grupo não tem como ser só RAM: são arquivos cujo **caminho** é
entregue a um processo externo, que vai abri-los por conta própria. Exemplos
reais na árvore:

```mhl
// shared/core/workflows_root.mh — monta um caminho para um SUBPROCESSO
WorkflowsRoot.resolve(relative) -> env("LEIBNIZ_WORKFLOWS_ROOT") + "/" + relative

// shared/agents/agents.mh:194 — um .sh que é EXECUTADO
path(): string -> WorkflowsRoot.resolve("shared/agents/coder-in-dir.sh")

// shared/agents/usage.mh:244 — JSON lido pelo CLI do agente
file(): string -> WorkflowsRoot.resolve("shared/agents/pricing/openai-codex.json")
```

Mais o contrato `SchemaFile = {content: string, path: string}`, em que o
**Codex recebe o caminho** do schema, não o conteúdo.

Daí a meta honesta deste trabalho — e é esta que pedimos que o `mhl`
viabilize:

> **Nenhum `.mh` e nenhum `.prompt.md` toca o disco do cliente, em momento
> algum.** Schemas, templates HTML e scripts `.sh` continuam materializados
> num diretório efêmero, porque um processo externo precisa abri-los. São
> estrutura e cola, não a orquestração nem os prompts.

### Divisão de responsabilidade que isso implica

O host continua dono da chave e do pack — ele já tem a maquinaria de
diretório efêmero. Então o `mhl` passa a receber **dois** caminhos, com
papéis distintos:

| Entrada | Conteúdo | Para quê |
| --- | --- | --- |
| `--workflows-pack <arquivo>` + chave no env | o pack **inteiro** (132 + 49 arquivos) | é daqui que o motor compila o programa e lê prompts — **em RAM** |
| `[dir]` posicional + `LEIBNIZ_WORKFLOWS_ROOT` | só o segundo grupo, já materializado pelo host | é o que `WorkflowsRoot.resolve()` e `SchemaFile.path` devem compor |

Ou seja: `--workflows-pack` substitui o diretório **como fonte do programa**,
e o diretório posicional deixa de ser fonte e passa a ser apenas a **raiz de
caminhos para subprocessos**. Os dois modos coexistem para sempre:

- **sem** `--workflows-pack` → tudo vem do diretório (modo de hoje, dev);
- **com** `--workflows-pack` → programa e prompts vêm do pack; caminhos
  para subprocesso vêm do diretório.

Se vocês preferirem que o próprio `mhl` materialize o segundo grupo a partir
do pack, também serve — digam, porque aí o host para de fazer isso e o
contrato fica com uma entrada só. Nossa proposta é a de cima só porque é a
que pede menos mudança de vocês.

## 3. Contrato do CLI

Seguindo a convenção de `serve mcp` (flag + env var equivalente):

| Flag | Env var | Obrigatório | Valor |
| --- | --- | --- | --- |
| `--workflows-pack <path>` | `MHL_SERVE_WORKFLOWS_PACK` | não | caminho do `.dmpk` |
| — | `MHL_SERVE_WORKFLOWS_KEY` | sim, se houver pack | chave AES-256 em **64 caracteres hex** |

**A chave nunca vem por flag.** `argv` é legível por qualquer processo da
máquina (`ps`); o env de um processo, só pelo mesmo usuário. É o degrau
simples e já muito melhor que disco. O degrau seguinte, quando quiserem:
receber a chave pelo **handshake do JSON-RPC de loopback** ou por um **fd
herdado** (`--workflows-key-fd <n>`), onde ela não aparece nem em `ps -E`.
O host implementa o que vocês expuserem.

### Higiene da chave dentro do motor (não negociável)

O `mhl` **spawna CLIs de agente** (`claude`, `codex`, `devin`, `agy`), e em Go
`os/exec` herda o env do processo por padrão. Sem cuidado, a chave do produto
vaza para dentro de um CLI de terceiro, que pode logá-la.

```go
raw := os.Getenv("MHL_SERVE_WORKFLOWS_KEY")
os.Unsetenv("MHL_SERVE_WORKFLOWS_KEY") // antes de qualquer spawn
key, err := hex.DecodeString(strings.TrimSpace(raw))
```

Depois de abrir o pack, zerem o `[]byte` da chave (`for i := range key { key[i] = 0 }`).
Em Go isso é best-effort — strings são imutáveis e o GC pode ter deixado
cópias —, então prefiram `[]byte` desde a leitura do env.

## 4. Formato `DMPK` v2

Fonte única, com testes: [`src-tauri/src/pack_format.rs`](../src-tauri/src/pack_format.rs)
no repo do Dumont (mesmo arquivo compilado no `build.rs`, que produz o pack, e
na lib, que o lê). Inteiros **little-endian**, sem alinhamento nem padding
além do indicado.

```
cabeçalho (12 bytes):
  magic    : "DMPK"            4 bytes
  version  : u8 = 2
  _pad     : [u8; 3]           zero (o produtor escreve zero; o leitor ignora)
  count    : u32               número de entradas

entrada i (0-based), count vezes:
  nonce    : [u8; 12]
  ct_len   : u32
  ct       : [u8; ct_len]      AES-256-GCM: ciphertext || tag (16 bytes)
```

Nada depois da última entrada: bytes sobrando são **erro**, não lixo tolerado.

**Texto em claro de cada entrada:**

```
  path_len : u16               bytes do caminho
  path     : [u8; path_len]    UTF-8, relativo à raiz, '/' como separador
  content  : [u8; resto]       o arquivo, sem transformação
```

### Regras de cifragem

- **AES-256-GCM.** Chave 32 bytes, nonce 12, tag 16 — os defaults de
  `cipher.NewGCM`, nada customizado.
- **O caminho é cifrado junto do conteúdo**, não fica no cabeçalho. Nomes como
  `development/prompts/avaliacao.prompt.md` descrevem a orquestração tanto
  quanto o conteúdo; deixá-los legíveis entregaria o mapa do produto a um
  `strings` no executável. (A v1 errava nisso: usava o caminho como AAD, em
  claro. Por isso v2.)
- **AAD = o índice da entrada**, `uint32` little-endian. Prende cada entrada à
  sua posição: trocar duas de lugar, remover uma do meio ou colar uma entrada
  de outro pack **invalida a tag**.
- **Ordem é parte do artefato** (as entradas saem ordenadas por caminho).
- **Falha é fatal.** Uma tag inválida aborta o pack inteiro. Não existe "abrir
  o que der" nem "servir os workflows que deram".
- O pack revela **quantas entradas há e o tamanho de cada uma**. Esconder isso
  pediria padding e não vale o custo.

### Validação de entrada

O pack é produzido por nós, mas um pack adulterado não pode virar um bug
explorável. Recusem, com erro claro:

- `magic` diferente, `version != 2`;
- `count` acima de um teto (usamos 100.000) ou `ct_len` acima de um teto por
  entrada;
- qualquer truncamento, ou bytes sobrando no fim;
- caminho vazio, absoluto, com `..` ou `.`, com `\`, com NUL, ou não-UTF-8 —
  `path.Clean(p) == p && !path.IsAbs(p)` cobre quase tudo;
- **caminho repetido** (sinal de adulteração: um mapa aceitaria o último em
  silêncio).

### Esboço do leitor em Go

```go
const (
	magic       = "DMPK"
	version     = 2
	nonceLen    = 12
	maxEntries  = 100_000
	maxEntryLen = 16 << 20
)

type File struct {
	Path    string
	Content []byte
}

// Open devolve os arquivos na ordem do pack. Nada é escrito em disco.
func Open(raw, key []byte) ([]File, error) {
	if len(key) != 32 {
		return nil, errors.New("pack: chave deve ter 32 bytes")
	}
	if len(raw) < 12 || string(raw[:4]) != magic {
		return nil, errors.New("pack: magic invalido")
	}
	if raw[4] != version {
		return nil, fmt.Errorf("pack: versao %d nao suportada", raw[4])
	}
	count := int(binary.LittleEndian.Uint32(raw[8:12]))
	if count > maxEntries {
		return nil, fmt.Errorf("pack: %d entradas excede o teto", count)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	out := make([]File, 0, count)
	seen := make(map[string]struct{}, count)
	aad := make([]byte, 4)
	p := 12
	for i := 0; i < count; i++ {
		if p+nonceLen+4 > len(raw) {
			return nil, errors.New("pack: truncado")
		}
		nonce := raw[p : p+nonceLen]
		p += nonceLen
		ctLen := int(binary.LittleEndian.Uint32(raw[p : p+4]))
		p += 4
		if ctLen > maxEntryLen || p+ctLen > len(raw) {
			return nil, errors.New("pack: truncado")
		}
		binary.LittleEndian.PutUint32(aad, uint32(i))
		plain, err := aead.Open(nil, nonce, raw[p:p+ctLen], aad)
		if err != nil {
			return nil, fmt.Errorf("pack: entrada %d nao autentica", i) // sem %w: não vaze detalhe de cripto
		}
		p += ctLen
		if len(plain) < 2 {
			return nil, fmt.Errorf("pack: entrada %d sem cabecalho", i)
		}
		n := int(binary.LittleEndian.Uint16(plain[:2]))
		if 2+n > len(plain) {
			return nil, fmt.Errorf("pack: entrada %d com caminho truncado", i)
		}
		name := string(plain[2 : 2+n])
		if err := validPath(name); err != nil {
			return nil, err
		}
		if _, dup := seen[name]; dup {
			return nil, fmt.Errorf("pack: caminho repetido: %s", name)
		}
		seen[name] = struct{}{}
		out = append(out, File{Path: name, Content: plain[2+n:]})
	}
	if p != len(raw) {
		return nil, errors.New("pack: bytes sobrando depois da ultima entrada")
	}
	return out, nil
}
```

Daí para um `fs.FS` é um passo — e é o que sugerimos como alvo interno, porque
deixa o loader testável com `fstest.MapFS` sem nenhum pack envolvido.

## 5. Vetor de interoperabilidade

Este pack é **golden test** no nosso lado
([`pack_format.rs`, `mod interop_vector`](../src-tauri/src/pack_format.rs)):
se mudar, a `VERSION` sobe e este documento muda junto. O leitor de vocês tem
de abrir exatamente estes bytes.

```
chave   = 000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f
nonce 0 = 000102030405060708090a0b
nonce 1 = 0c0d0e0f1011121314151617
tamanho = 151 bytes
```

```
444d504b0200000002000000000102030405060708090a0b340000005102a174b78e9d72f9
24faa4c6860a06dcbff3519d5532144f0897ee7b056fc5216bd3f6588d72e2218516c3915
0cd1721084ab60c0d0e0f10111213141516173700000086fe1ab01e049937184b2fb0ffff
55c834648880820e15dae9cd9790908ead9e5c85054891b12ca5dbec022b119fc25e7f257
e3d1553aa
```

Abrir tem de devolver, **nesta ordem**:

| # | caminho | conteúdo |
| --- | --- | --- |
| 0 | `work_item/work_item.mh` | `workflow {}\n` |
| 1 | `shared/agents/agents.prompt.md` | `prompt\n` |

(O hex é contínuo — as quebras de linha acima são só de apresentação.)

## 6. O que muda dentro do motor

Não temos acesso ao código do `mhl`, então isto é uma lista de **suspeitas a
confirmar**, não um diff. O pedido arquitetural é um só: **o loader de
workflows precisa parar de falar com o sistema de arquivos direto e passar a
falar com uma abstração** (`fs.FS` é o encaixe natural em Go). Tudo que hoje
faz `os.ReadFile` / `os.Open` / `filepath.Walk` / `os.Stat` relativo à raiz
dos workflows entra na conta:

- descoberta das workflows (`filepath.Walk` da árvore → iterar as entradas do pack);
- resolução de `import` / `include` entre `.mh`;
- `export schema X from "schemas/y.json"` — o **conteúdo** vem do pack; o
  **`path`** do `SchemaFile` tem de ser composto a partir de
  `LEIBNIZ_WORKFLOWS_ROOT` (o diretório materializado), não de onde o
  programa foi carregado;
- carregamento de prompt (`.prompt.md`) — este é o mais sensível: tem de vir
  do pack e **nunca** ser escrito em lugar nenhum;
- qualquer cache de compilação em disco: se existir, ou desliga no modo pack,
  ou passa a ser cifrado com a mesma chave.

### Regras não negociáveis no modo pack

1. **Nada de `.mh` ou `.prompt.md` em disco** — nem em `--state-dir`, nem em
   `os.TempDir()`, nem em checkpoint de sessão, nem em cache.
2. **Mensagem de erro e log não carregam conteúdo.** Um erro de compilação
   pode citar nome de workflow, número de linha e o trecho mínimo — mas um
   `panic` com o prompt inteiro no stack, ou um log em nível debug despejando
   o template, anula o trabalho. Hoje o `serve mcp --http` loga
   `12 tool(s) from <dir>`: no modo pack, logar o caminho do `.dmpk` está ok.
3. **Falha é total.** Pack inválido, chave errada ou ausente → sair com erro
   claro, sem servir nada.
4. **Dev não muda.** Sem `--workflows-pack`, o comportamento de hoje é
   idêntico, bit a bit.

## 7. Dois vazamentos que o pack não resolve (e que achamos no caminho)

Levamos isto a vocês porque o conserto do segundo é do lado da linguagem.

### 7.1 `prompt_log.jsonl` — o pior deles, e é real

`Writer.generate` (`shared/agents/agents.mh:391`) faz:

```mhl
PromptLog.append({ artifact, backend: picked, prompt, response: raw, ... })
```

`prompt` é o **prompt final renderizado**. Ele cai em texto plano em
`<appdata>/dumont/data/projects/<id>/prompt_log.jsonl`, e fica lá. Na máquina
em que escrevemos isto: **16,6 MB, 124 entradas, com os templates inteiros
legíveis**. Ou seja: na primeira execução de um projeto, o cliente ganha em
disco, em claro e para sempre, exatamente aquilo que o pack cifrou.

Isso é uma decisão nossa de workflow (auditoria/depuração), e vamos tratá-la
aqui — mas as opções dependem do que a linguagem oferece:

- gravar só um `sha256` do prompt no lugar do texto (perde depuração);
- manter o texto **só quando um flag de dev estiver ligado**;
- `memory` com **conteúdo cifrado em repouso**, com a mesma chave — é esta que
  queríamos discutir com vocês: existe (ou faz sentido existir) um
  `type: "jsonl"` com cifragem, ou um `path_guard`-equivalente para
  confidencialidade?

### 7.2 O prompt vai para um CLI de terceiro

Por construção, o prompt renderizado é entregue a `claude`/`codex`/`devin`,
que o mandam para a nuvem e frequentemente gravam a própria sessão em disco.
Nada que façamos no pack muda isso. Não é problema de vocês; está aqui só
para ninguém prometer "o prompt nunca sai da memória" — ele sai, por desenho.

## 8. Critérios de aceite

O Dumont considera o trabalho pronto quando:

1. `mhl serve mcp --http --workflows-pack p.dmpk ./root` com a chave no env
   expõe **a mesma lista de tools, com os mesmos schemas**, que o modo
   diretório expõe hoje (são 12 tools na árvore atual).
2. Com o serve rodando, **nenhum arquivo aberto ou criado pelo processo
   contém texto de `.mh`/`.prompt.md`** — `lsof -p <pid>`, mais um `grep`
   recursivo no `--state-dir` e no `TMPDIR` por uma frase conhecida de prompt.
3. Os processos filhos (CLIs de agente) **não têm a chave no env**:
   `ps -E -p <pid-do-filho>` (macOS) ou `tr '\0' '\n' < /proc/<pid>/environ`
   (Linux) não mostram `MHL_SERVE_WORKFLOWS_KEY`.
4. O **vetor da §5** passa como teste unitário no repo do `mhl`.
5. Pack com **1 byte alterado** → erro claro, nada servido. Chave errada,
   chave ausente, pack truncado → idem.
6. Modo diretório **inalterado**: a suíte atual de vocês passa sem tocar em
   nada.
7. Uma run de ponta a ponta no Dumont (Discovery → Desenvolvimento) funciona
   igual, contra o pack.

## 9. Fora de escopo

- `mhl run`, `test`, `lint`, `lsp` — são ferramentas de desenvolvimento e
  rodam na árvore em claro. Só `serve mcp` precisa do modo pack.
- Gerar o pack: quem produz é o `build.rs` do Dumont. Se quiserem um
  `mhl pack` para testar, é bem-vindo, mas não é requisito.
- Rotação de chave, licenciamento e o vínculo da chave com a licença: é a
  Fase 3 do nosso plano, do lado do host e do servidor. Para o motor, a chave
  é só 32 bytes que chegam no spawn.

## 10. Perguntas para vocês

1. O loader já passa por alguma abstração de FS, ou lê o disco direto? Isso
   dimensiona o trabalho inteiro.
2. Existe cache de compilação em disco (`.mhl/` ou similar) com conteúdo de
   workflow dentro?
3. Preferem receber a chave por **env var** (nossa proposta, mais simples) ou
   já partir para **fd herdado / handshake JSON-RPC**? O host implementa o que
   vocês expuserem.
4. Quem materializa os 49 arquivos "por caminho" (§2): o host (nossa
   proposta) ou o `mhl`?
5. `memory` com conteúdo cifrado em repouso (§7.1) — existe, ou faz sentido
   propor?
6. O `SchemaFile.path` pode ser composto a partir de
   `LEIBNIZ_WORKFLOWS_ROOT`, independente de onde o programa foi carregado?

---

**Contexto do lado do host**, se ajudar:
[`docs/WORKFLOW-PACK-FORMAT.md`](WORKFLOW-PACK-FORMAT.md) (o que o Dumont já
faz, incluindo o paliativo atual e o plano de chave),
[`src-tauri/src/workflow_pack.rs`](../src-tauri/src/workflow_pack.rs) (abertura
do pack e diretório efêmero),
[`src-tauri/src/mhl/client.rs`](../src-tauri/src/mhl/client.rs) (o spawn do
`mhl`, onde a flag e a chave vão entrar).
