# Plano de desenvolvimento: extensões MCP e A2A

Status: proposta

## 1. Objetivo

Remover MCP e A2A do núcleo específico da linguagem, transformando-os nas
primeiras extensões do runtime MHL, sem exigir compilação dos arquivos `.mh` e
sem degradar a performance das chamadas remotas.

O resultado esperado é que o núcleo conheça apenas o conceito de extensão,
declaração, método, chamada e valor. Protocolos, transportes, polling,
negociação de versão e normalização de respostas ficam sob responsabilidade das
extensões.

## 2. Diagnóstico atual

Os clientes estão razoavelmente isolados em:

- `src/mhl-runtime/internal/features/mcp`
- `src/mhl-runtime/internal/features/a2a`

Entretanto, o acoplamento ainda é alto:

- o AST possui nós específicos `MCPServer` e `A2AAgent`;
- o parser trata `mcp_server` e `a2a_agent` como construções nativas;
- o interpretador faz dispatch explícito para os dois protocolos em `eval.go`;
- o lint e o LSP mantêm listas próprias de propriedades e métodos;
- os pacotes estão sob `internal/`, sem contrato consumível por terceiros;
- registries e clients são recriados durante chamadas;
- chamadas MCP podem executar `tools/list` repetidamente para resolver
  `x-mcp-header`.

A baseline atual foi validada com:

```text
go test ./...
go vet ./...
```

## 3. Princípios arquiteturais

1. O `.mh` continua interpretado. Extensões não dependem de compilação do
   programa MHL.
2. O núcleo não deve conhecer nomes de protocolos ou métodos específicos.
3. A sintaxe atual deve continuar funcionando durante a migração.
4. Nenhum processo externo deve ser iniciado por chamada.
5. Extensões externas devem ser processos persistentes, multiplexados e com
   handshake de versão.
6. Segredos devem ser resolvidos pelo host ou por um serviço controlado, nunca
   expostos em checkpoints, logs ou mensagens de erro.
7. A API pública da extensão deve usar DTOs estáveis, não tipos internos do AST
   ou estruturas do interpretador.

## 4. Arquitetura-alvo

```text
arquivo .mh
    |
    v
parser do núcleo -> declaração genérica de extensão
    |
    v
ExtensionRegistry
    |
    +--> extensão MCP in-process ou host externo persistente
    |
    +--> extensão A2A in-process ou host externo persistente
    |
    +--> extensões futuras
```

O núcleo será responsável por avaliar argumentos, controlar escopo, cancelar
operações, aplicar limites e encaminhar a chamada. A extensão será responsável
por validar propriedades, declarar métodos e executar a capacidade externa.

## 5. Contrato inicial

Criar um pacote público, preferencialmente `pkg/extension`, contendo conceitos
estáveis como:

```go
type Extension interface {
    ID() string
    Version() string
    Declarations() []DeclarationSpec
    Validate(Declaration) []Diagnostic
    Bind(Declaration, HostContext) (Instance, error)
}

type Instance interface {
    Methods() []MethodSpec
    Call(context.Context, CallRequest) (Value, error)
}
```

O contrato deve incluir também:

- `Declaration`: tipo, nome, propriedades e posição no arquivo;
- `MethodSpec`: nome, assinatura, documentação e parâmetros;
- `CallRequest`: declaração, método, argumentos e contexto de execução;
- `Value`: valores compatíveis com o runtime MHL;
- `Diagnostic`: erro, warning, posição e código estável;
- `HostContext`: credenciais, HTTP, subprocessos, logs, métricas,
  cancelamento e limites.

O contrato não deve expor `*ast.Program`, `evalCtx` ou qualquer tipo privado do
interpretador.

## 6. Plano por fases

### Fase 0 — congelar comportamento e medir baseline

Tarefas:

- registrar as assinaturas atuais de MCP e A2A;
- manter os testes E2E existentes como testes de compatibilidade;
- criar benchmarks de chamada quente e fria;
- medir tempo de parsing, lookup, IPC, HTTP, polling e serialização;
- definir os limites de performance após a medição.

Entregáveis:

- documento de decisões arquiteturais;
- benchmarks reproduzíveis;
- matriz de compatibilidade da sintaxe atual.

Critério de aceite: todos os testes atuais continuam passando antes da
extração.

### Fase 1 — criar o Extension Registry no núcleo

Tarefas:

- criar `pkg/extension`;
- criar `ExtensionRegistry` e registro de extensões;
- criar uma declaração genérica de extensão no AST;
- permitir validação e descoberta de métodos sem conhecer MCP/A2A;
- criar o `HostContext` com `context.Context`, redaction e limites.

Decisão de sintaxe recomendada:

```mhl
extension mcp GitHub { ... }
extension a2a Translator { ... }
```

Durante a migração, `mcp_server` e `a2a_agent` devem ser aliases compatíveis
que produzem a mesma declaração genérica.

Critério de aceite: o núcleo consegue registrar uma extensão de teste e
executar um método fictício sem importar os pacotes MCP ou A2A.

### Fase 2 — migrar MCP e A2A para extensões in-process

Tarefas:

- mover clients, registries, normalização e validação para adaptadores de
  extensão;
- preservar as implementações de transporte existentes;
- remover `mcp_ops.go` e `a2a_ops.go` como pontos de dispatch específicos;
- fazer o interpretador consultar apenas o `ExtensionRegistry`;
- centralizar a conversão de erros e valores.

O primeiro modo de execução deve ser in-process. Ele elimina IPC e facilita a
validação do contrato antes de introduzir processos externos.

Critério de aceite: os programas atuais de MCP/A2A produzem os mesmos valores,
erros e mensagens observáveis.

### Fase 3 — migrar lint e LSP

Tarefas:

- obter propriedades a partir de `DeclarationSpec`;
- obter métodos e assinaturas a partir de `MethodSpec`;
- permitir diagnósticos fornecidos pela extensão;
- remover listas duplicadas de métodos MCP/A2A do LSP;
- manter lint sem chamadas de rede por padrão.

Critério de aceite: adicionar uma nova extensão não exige editar o parser, o
interpretador, o lint e o LSP em arquivos separados.

### Fase 4 — host externo persistente

Tarefas:

- definir handshake de versão da API;
- criar manifesto de extensão;
- iniciar o processo somente quando a extensão for usada;
- manter uma instância viva durante a execução do pipeline;
- usar um canal multiplexado com IDs de requisição;
- suportar cancelamento, timeout, encerramento e reinício controlado;
- registrar stderr separadamente e aplicar redaction antes de exibir erros.

Fluxo esperado:

```text
primeira chamada -> inicia extensão -> handshake
chamadas seguintes -> reutiliza processo e canal
fim da execução -> shutdown gracioso
```

O protocolo pode começar com JSON-RPC delimitado por newline. MessagePack ou
outro formato binário só deve ser introduzido se os benchmarks mostrarem que a
serialização é um gargalo.

Critério de aceite: nenhuma chamada de método inicia um novo processo.

### Fase 5 — empacotamento, permissões e compatibilidade

Tarefas:

- definir diretório de instalação e resolução de versões;
- adicionar manifesto com `id`, versão, versão da API, executável e
  capacidades;
- adicionar `mhl extension list` e `mhl extension doctor`;
- exigir declaração explícita de extensões permitidas no projeto;
- documentar permissões de rede, subprocesso, filesystem e secrets;
- manter aliases antigos por pelo menos uma versão major;
- emitir warning de depreciação antes de remover a sintaxe antiga.

Não implementar download ou execução automática de extensões sem uma política
de confiança definida.

### Fase 6 — experiência do desenvolvedor externo

O desenvolvedor externo não deve alterar o parser, o interpretador ou recompilar
o runtime MHL. Ele deve criar um executável de extensão compatível com o
protocolo do host.

Estrutura mínima recomendada:

```text
mhl-ext-crm/
├── extension.json
├── src/
└── bin/mhl-ext-crm
```

Exemplo de manifesto:

```json
{
  "id": "com.acme.crm",
  "api_version": "1",
  "kinds": ["crm"],
  "executable": "bin/mhl-ext-crm",
  "permissions": {
    "network": ["api.acme.com"],
    "secrets": true
  }
}
```

O uso no MHL será baseado em uma declaração genérica, resolvida pela extensão
instalada:

```mhl
extension crm Customer {
    endpoint: env("CRM_URL")
}

pipeline sync {
    step load {
        var customer = Customer.lookup("123")
        log(customer)
    }
}
```

Durante o handshake, a extensão informa suas declarações, métodos, assinaturas,
documentação e regras de validação. Esses metadados devem ser usados tanto pelo
runtime quanto pelo lint e pelo LSP:

```json
{
  "id": "com.acme.crm",
  "methods": [
    {
      "name": "lookup",
      "parameters": ["id"],
      "signature": "lookup(id: string) -> object",
      "documentation": "Busca um cliente pelo identificador."
    }
  ]
}
```

Uma chamada será encaminhada pelo host com request ID, permitindo
multiplexação e concorrência:

```json
{
  "id": 42,
  "method": "call",
  "params": {
    "declaration": "Customer",
    "operation": "lookup",
    "arguments": ["123"]
  }
}
```

O processo deve permanecer vivo durante a execução do pipeline. A sequência
esperada é:

```text
primeira chamada -> inicia processo -> handshake
chamadas seguintes -> reutiliza processo e canal
fim da execução -> shutdown gracioso
```

O SDK externo deve fornecer, no mínimo:

- `mhl extension init` para gerar um projeto;
- `mhl extension dev` para executar a extensão localmente;
- `mhl extension test` para testar o protocolo com um host fake;
- `mhl extension package` para gerar o artefato distribuível;
- helpers de handshake, request IDs, cancelamento e conversão de valores;
- emissão de logs exclusivamente em stderr;
- geração e validação do manifesto.

### Fase 6.1 — credenciais para extensões externas

Extensões não devem receber automaticamente todo o ambiente do processo. O
host deve oferecer uma operação controlada para resolução de segredos:

```json
{
  "id": 7,
  "method": "secret.resolve",
  "params": {
    "reference": "env(CRM_TOKEN)"
  }
}
```

O acesso deve ser autorizado pelo manifesto, auditável, redigido em logs,
excluído de checkpoints e limitado ao tempo da chamada.

### Fase 6.2 — instalação e versionamento

O usuário deve instalar uma extensão sem recompilar o MHL:

```text
mhl extension install com.acme.crm
```

O projeto deve poder fixar a extensão em `.mhl/extensions.lock`:

```json
{
  "extensions": {
    "com.acme.crm": {
      "version": "1.2.0",
      "sha256": "..."
    }
  }
}
```

Durante a execução, o host verifica o manifesto, a versão da API, o hash e as
permissões antes de iniciar o processo. Download e execução automáticos só
podem ser habilitados depois de definida uma política de confiança.

### Fase 6.3 — extensão externa versus in-process

O modelo externo é o mecanismo padrão para terceiros:

```text
MHL runtime <-> processo da extensão
```

Ele não exige recompilação do runtime, aceita implementações em Go, Rust, Node,
Python ou outras linguagens e oferece isolamento operacional.

O modelo in-process deve ser reservado para extensões oficiais ou embutidas na
distribuição do runtime. Ele possui menor overhead, mas normalmente exige uma
nova distribuição do binário MHL.

Critério de aceite: um desenvolvedor externo consegue criar, testar, empacotar
e executar uma extensão nova sem editar o código do parser ou do interpretador.

## 7. Estratégia de performance

### Dentro do runtime

- construir o registry uma vez por execução;
- reutilizar instances e clients;
- compartilhar `http.Transport` para connection pooling;
- cachear `tools/list` por configuração e TTL;
- cachear Agent Cards quando permitido;
- não serializar valores mais de uma vez na mesma fronteira;
- usar limites de concorrência por extensão;
- substituir `time.Sleep` em polling por timers canceláveis via context.

### No host externo

- uma extensão persistente por execução ou por configuração compatível;
- múltiplas chamadas simultâneas pelo mesmo processo;
- request IDs para multiplexação;
- evitar handshake repetido;
- manter conexões HTTP reutilizáveis dentro da extensão;
- usar JSON inicialmente, trocando o codec apenas com evidência de gargalo.

Metas a validar nos benchmarks:

- overhead do dispatch in-process próximo ao custo do lookup local;
- overhead p95 do host externo aquecido inferior a uma chamada de rede local;
- zero criação de processo no caminho quente;
- nenhuma regressão mensurável em chamadas MCP/A2A remotas após o warm-up.

## 8. Segurança

O `HostContext` deve controlar capacidades, em vez de entregar acesso irrestrito
ao sistema operacional. Extensões devem declarar se precisam de:

- rede;
- subprocessos;
- filesystem;
- credenciais;
- persistência;
- acesso a logs ou métricas.

Credenciais devem ser resolvidas sob demanda, registradas no mecanismo de
redaction e excluídas de valores persistidos. Falhas de resolução devem ser
fail-closed, mantendo o comportamento atual.

`plugin.Open` do Go não é recomendado como mecanismo externo por causa do
acoplamento de versão, plataforma e build. O host por processo fornece melhor
isolamento e permite extensões em Go, Node, Python, Rust ou outras linguagens.

## 9. Testes obrigatórios

- testes unitários do contrato de extensão;
- extensão fake in-process;
- extensão fake externa persistente;
- concorrência e multiplexação;
- timeout e cancelamento;
- processo que encerra inesperadamente;
- incompatibilidade de versão da API;
- redaction de credenciais em logs e checkpoints;
- compatibilidade com `mcp_server` e `a2a_agent`;
- testes E2E MCP HTTP e stdio;
- testes E2E A2A com polling, cancelamento e estados interrompidos;
- benchmarks de cold start, warm call, chamadas concorrentes e payloads grandes.

Verificação mínima por fase:

```text
gofmt -w <arquivos alterados>
go test ./...
go vet ./...
```

## 10. Definição de pronto

A migração estará concluída quando:

1. o interpretador não tiver branches específicos para MCP ou A2A;
2. MCP e A2A forem extensões registradas pelo runtime;
3. lint e LSP consumirem metadados das extensões;
4. a sintaxe legada continuar funcionando durante o período de compatibilidade;
5. extensões externas puderem permanecer vivas e atender chamadas concorrentes;
6. permissões e segredos forem controlados pelo host;
7. benchmarks demonstrarem ausência de regressão relevante;
8. uma nova extensão puder ser adicionada sem alterar o parser e o evaluator.

## 11. Ordem recomendada de implementação

```text
contrato público
  -> registry in-process
  -> MCP/A2A como adaptadores
  -> dispatch genérico
  -> lint/LSP orientados por metadados
  -> host externo persistente
  -> manifesto, permissões e distribuição
  -> depreciação da sintaxe legada
```

Essa ordem reduz o risco: primeiro elimina-se o acoplamento conceitual, depois
introduz-se o isolamento de processos. A ausência de compilação do MHL não
entra no caminho crítico; somente a extensão, quando externa, precisa ser
distribuída como executável ou processo interpretado persistente.
