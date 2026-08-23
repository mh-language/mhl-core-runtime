# Specification & Architecture Document: Meta-Harness Language (MHL)

**Versão:** 1.6.0-Skills

**Data:** 18 de Agosto de 2026

**Status:** Aprovado para Implementação

---

## 1. Visão Geral e Objetivos

A **Meta-Harness Language (MHL)** é uma Linguagem de Domínio Específico (DSL) declarativa para orquestração autônoma de múltiplos sub-harnesses, ferramentas locais, servidores do ecossistema **MCP (Model Context Protocol - Stateless Spec `2026-07-28`)**, **Skills modulares** e modelos de IA (Claude, Codex, Kimi, Llama via Ollama, etc.).

### **Princípios de Design**

* **Declarativa & Agnóstica:** Unifica modelos locais (CLI/Ollama/vLLM) e APIs remotas sob a abstração de `agent`.
* **Provedor de Orquestração MCP Stateless:** Suporte ao protocolo MCP atualizado, operando sem conexões de estado/handshake mantidos pelo cliente.
* **Encapsulamento por Skills (Habilidades Modulares):** Empacotamento reutilizável de conhecimento procedural, prompts especializados e escopo restrito de ferramentas.
* **Resiliência e Continuidade (Checkpoints & Resume):** Persistência nativa de estado para retomar pipelines interrompidas do ponto exato onde pararam.
* **Segurança e Gestão Zero-Trust de Credenciais:** Injeção dinâmica de segredos via variáveis de ambiente/cofres locais sem exposição de chaves no código-fonte.
* **Portabilidade & Binário Único:** Compilada/Interpretada por um runtime em **Go**, distribuído como um executável CLI leve (`mhl`), sem dependências externas.
* **Isolamento & Process Management:** Gerenciamento seguro de subprocessos locais (`Setpgid`) para prevenir travamentos e processos zumbis.

---

## 2. Funcionalidades Core

| Funcionalidade | Descrição |
| --- | --- |
| **Agents** | Abstração única para LLMs locais (CLI/Ollama) ou em nuvem (Anthropic/OpenAI/Moonshot). |
| **Skills** | Encapsulamento modular de capacidade procedural (Tools + Prompts + Instruções de Domínio + Contrato I/O). |
| **Security & Auth** | Resolução dinâmica de segredos (`env("KEY")`) e herança de sessão CLI em subprocessos. |
| **MCP Servers (Stateless)** | Declaração de servidores MCP via transporte `stdio` ou HTTP/SSE desacoplados. |
| **State Checkpoints & Resume** | Salvamento automático de progresso por etapa (`step`) permitindo retomada via `mhl run --resume`. |
| **Prompts** | Blocos parametrizáveis com interpolação de variáveis dinâmicas (`${var}`). |
| **Memory Engine** | Camadas de memória: Efêmera (`kv`), Registro Histórico (`append_log`) e Vetorial (`vector`). |
| **Tools** | Interface segura para comandos de sistema (`cmd`), versionamento (`git`), arquivos (`fs`) e rede (`http`). |
| **Pipelines & Loops** | Orquestração com controle de fluxo procedural (`if`, `while`, `try/catch`, `step`). |
| **Traffic Shaping & Cache** | Caching determinístico de respostas e integração com o campo `ttlMs` do MCP para atenuar latência. |
| **Modularização** | Suporte a reuso de código com namespaces (`import`, `use`, `export`). |

---

## 3. Especificação Sintática Completa (.mhl)

### **3.1 Módulos e Importações**

```mhl
import "./agentes/qualidade.mhl" as qa
use { SecurityAudit } from "./prompts/seguranca.mhl"

```

### **3.2 Declaração de Skills Modulares**

```mhl
export skill CodeAuditorSkill {
    description: "Analisa código procurando falhas de segurança OWASP e gargalos de performance."
    
    // Ferramentas restritas disponibilizadas exclusivamente durante o uso desta Skill
    tools: [execution.read_file, execution.git_diff]
    mcp_servers: [PostgresDB]

    input {
        target_file: string
        strict_mode: boolean
    }

    output {
        vulnerabilities_found: int
        report_markdown: string
    }

    system_instructions: """
    Você é um auditor de segurança sênior. Siga rigorosamente as normas ISO/IEC 27001.
    Nunca aprove código com credenciais expostas no código-fonte.
    """
}

```

### **3.3 Autenticação e Servidores MCP (Stateless)**

```mhl
// Servidor MCP Local via stdio
mcp_server PostgresDB {
    transport: "stdio"
    command: "npx"
    args: ["-y", "@modelcontextprotocol/server-postgres", env("DATABASE_URL")]
}

// Servidor MCP Remoto via HTTP Stateless
mcp_server GitHubServer {
    transport: "http"
    url: "https://mcp.github.com/v1"
    headers: {
        "Authorization": "Bearer " + env("GITHUB_TOKEN")
    }
}

```

### **3.4 Agentes Equipados com Skills e Resiliência**

```mhl
agent ClaudeCoder {
    engine: "anthropic/claude-3-5-sonnet"
    api_key: env("ANTHROPIC_API_KEY") 
    temperature: 0.2
    timeout: 45s
    
    // Equipando o agente com habilidades procedurais
    skills: [CodeAuditorSkill]

    retry: {
        max_attempts: 3
        backoff: "exponential"
        delay: 2s
        retry_on: [500, 503, "rate_limit", "timeout"]
    }
    
    fallback: [
        agent { 
            engine: "openai/gpt-4o"
            api_key: env("OPENAI_API_KEY")
            timeout: 30s 
        }
    ]

    cache: {
        strategy: "exact"
        ttl: 24h
        storage: "disk"
    }

    rate_limit: {
        requests_per_minute: 100
        concurrency: 5
        on_exceeded: "queue"
    }

    mcp_servers: [PostgresDB, GitHubServer]
    tools: [execution.read_file, execution.write_file]
}

// Agente Local executado via CLI
agent LocalClaudeCLI {
    engine: "cli/claude-code"
    command: "claude"
    args: ["--dangerously-skip-permissions", "--non-interactive"]
    timeout: 60s
}

```

### **3.5 Memória e Ferramentas Nativas**

```mhl
memory session_mem {
    type: "kv"
    store: "memory"
}

memory project_rag {
    type: "vector"
    provider: "chroma"
    path: "./.mhl/vector_db"
}

tool execution {
    run_tests() -> cmd.exec("dotnet test", timeout: 120s)
    get_diff() -> git.diff(target: "HEAD~1", capture: true)
    read_file(path: string) -> fs.read(path)
    write_file(path: string, content: string) -> fs.write(path, content)
}

```

### **3.6 Pipeline de Execução Invocando Skills e Checkpoints**

```mhl
pipeline AutoFixPipeline {
    input issue_id: string
    input target_file: string

    checkpoint: {
        enabled: true
        strategy: "per_step"
        storage: "file"
        ttl: 7d
    }

    step AuditWithSkill {
        // Invocando uma Skill tipada diretamente pelo Agente
        var audit_res = ClaudeCoder.use_skill(CodeAuditorSkill, {
            target_file: target_file,
            strict_mode: true
        })

        if (audit_res.vulnerabilities_found > 0) {
            session_mem.set("last_review", audit_res.report_markdown)
        } else {
            audit_log.append("Nenhuma vulnerabilidade encontrada no arquivo ${target_file}.")
        }
    }

    step RefinementLoop {
        var max_attempts = 3
        var attempt = session_mem.get("attempt", 0)
        var fixed = false
        var last_error = session_mem.get("last_error", "")

        while (!fixed && attempt < max_attempts) {
            var fix = ClaudeCoder.run(
                prompt: "Corrija o código baseado no feedback: ${session_mem.get('last_review')}"
            )

            execution.write_file(fix.file_path, fix.content)
            var test_res = execution.run_tests()

            if (test_res.exit_code == 0) {
                fixed = true
                checkpoint.clear()
            } else {
                attempt = attempt + 1
                last_error = test_res.stderr

                session_mem.set("attempt", attempt)
                session_mem.set("last_error", last_error)
                checkpoint.save()
            }
        }

        if (!fixed) {
            fail("Pipeline interrompido após ${max_attempts} tentativas.")
        }
    }
}

```

---

## 4. Arquitetura do Runtime (Go Engine) e Resolução de Skills

O motor em Go orquestra o parsing, o **Skill Resolution Subsystem**, a injeção segura de credenciais HTTP, o cliente MCP stateless e o subsistema de **Checkpointing & State Recovery**.

```
 ┌──────────────────────────────────────────────────────────────┐
 │                  MHL CLI (mhl run --resume)                  │
 └──────────────────────────────┬───────────────────────────────┘
                                │
 ┌──────────────────────────────▼───────────────────────────────┐
 │               1. Lexer & Parser (Participle v2)              │
 ├──────────────────────────────────────────────────────────────┤
 │ Transforma scripts .mhl na AST em Go                         │
 └──────────────────────────────┬───────────────────────────────┘
                                │
 ┌──────────────────────────────▼───────────────────────────────┐
 │             2. Skill & Dependency Injector                   │
 ├──────────────────────────────────────────────────────────────┤
 │ Aplica o escopo restrito de Tools, MCP Servers e System      │
 │ Instructions da Skill ao payload do Agente em execução       │
 └──────────────────────────────┬───────────────────────────────┘
                                │
 ┌──────────────────────────────▼───────────────────────────────┐
 │               3. Auth & State Recovery Manager               │
 ├──────────────────────────────────────────────────────────────┤
 │ Injeta credenciais de ambiente e restaura progresso salvo    │
 └──────────────────────────────┬───────────────────────────────┘
                                │
 ┌──────────────────────────────▼───────────────────────────────┐
 │                     4. MHL Runtime Engine                    │
 ├──────────────────────────────┬───────────────────────────────┤
 │ • Cache Manager (SHA-256/TTL)│ • Stateless MCP HTTP/Stdio    │
 │ • Traffic Shaping (Semáforo) │ • Process Group CLI Manager   │
 └───────┬──────────────────────┴───────────────────────┬───────┘
         │                                              │
 ┌───────▼──────────────────────┐            ┌──────────▼───────────────┐
 │  Local Process & MCP Tools   │            │ Remote & Local LLM Engine│
 │(OS Cmd, Git, Stdio MCP, CLI) │            │ (Injeta Skills & Prompts)│
 └──────────────────────────────┘            └──────────────────────────┘

```

### **4.1 Resolução e Injeção de Skills em Go**

```go
package runtime

import (
	"fmt"
)

type SkillDefinition struct {
	Name               string
	Description        string
	Tools              []string
	MCPServers         []string
	InputSchema        map[string]string
	OutputSchema       map[string]string
	SystemInstructions string
}

type SkillRuntimeResolver struct{}

// PrepareAgentPayloadForSkill funde as instruções da Skill ao contexto de chamada do Agente
func (s *SkillRuntimeResolver) PrepareAgentPayloadForSkill(
	agent *AgentConfig, 
	skill *SkillDefinition, 
	inputArgs map[string]interface{},
) (*AgentInvocationPayload, error) {

	// 1. Concatena instruções do Agente com as Instruções Específicas da Skill
	combinedInstructions := fmt.Sprintf(
		"%s\n\n[ACTIVE SKILL: %s]\n%s", 
		agent.BaseSystemPrompt, 
		skill.Name, 
		skill.SystemInstructions,
	)

	// 2. Sandboxing de Ferramentas: Agente recebe APENAS as ferramentas declaradas na Skill
	scopedTools := append([]string{}, skill.Tools...)
	scopedMCP := append([]string{}, skill.MCPServers...)

	return &AgentInvocationPayload{
		Engine:             agent.Engine,
		SystemInstructions: combinedInstructions,
		ActiveTools:        scopedTools,
		ActiveMCPServers:   scopedMCP,
		InputParameters:    inputArgs,
	}, nil
}

```

---

## 5. Matriz de Conceitos e Abstrações da Linguagem

| Abstração MHL | Responsabilidade | Exemplo |
| --- | --- | --- |
| **`tool` / `mcp_server**` | Execução de infraestrutura e I/O. | `cmd.exec("dotnet test")`, `fs.read()`, `PostgresDB` |
| **`prompt`** | Template de texto formatado com variáveis. | `prompt Review(code) { "..." }` |
| **`agent`** | Provedor de IA, credenciais, timeouts e resiliência. | `engine: "anthropic/claude-3-5-sonnet"` |
| **`skill`** | **Capacidade procedural** (Tools + Prompts + Instruções + Contrato I/O). | `CodeAuditorSkill`, `SQLQueryOptimizerSkill` |
| **`pipeline`** | Orquestrador de fluxo, loops, checkpoints e condições. | `step`, `while`, `try/catch`, `checkpoint` |

---

## 6. Estratégia de Distribuição e CLI

A ferramenta é distribuída como um executável estático compilado via Go para Linux, macOS e Windows.

```bash
# Execução padrão do pipeline
mhl run pipeline.mhl --input issue_id="BUG-102" --input target_file="src/main.py"

# Retomada do pipeline do ponto da falha
mhl run pipeline.mhl --resume

# Inspecionar Skills disponíveis no projeto
mhl skills list

```

## 7. Estrutura de Arquivos do Código-Fonte Go (/src)
A organização adota o padrão pkg/ para bibliotecas reutilizáveis, internal/ para lógica de domínio protegida da MHL e cmd/ para os pontos de entrada do CLI executável.

```
mhl/
├── cmd/
│   └── mhl/                       # Ponto de entrada da CLI executável
│       └── main.go
├── internal/                      # Código privado do motor (não importável por outros projetos)
│   ├── ast/                       # Definição dos nós da Abstract Syntax Tree (AST)
│   │   ├── agent.go
│   │   ├── pipeline.go
│   │   ├── skill.go
│   │   └── program.go
│   ├── parser/                    # Lexer e Parser usando Participle v2
│   │   ├── lexer.go
│   │   ├── parser.go
│   │   └── parser_test.go
│   ├── resolver/                  # Análise Semântica e Grafo de Dependências (DAG)
│   │   ├── imports.go
│   │   └── type_checker.go
│   ├── runtime/                   # Core Engine de Execução
│   │   ├── engine.go              # Interpretador da AST
│   │   ├── evaluator.go           # Avaliação de expressões e loops
│   │   ├── state.go              # Gerenciador de Checkpoints e --resume
│   │   └── context.go            # Contexto de execução e variáveis
│   ├── adapters/                  # Adaptadores de Agentes e Provedores
│   │   ├── adapter.go             # Interface BaseHarnessAdapter
│   │   ├── anthropic.go           # Adaptador Claude API
│   │   ├── openai.go              # Adaptador OpenAI/Codex API
│   │   ├── moonshot.go            # Adaptador Kimi API
│   │   ├── ollama.go              # Adaptador Ollama/vLLM Local
│   │   └── cli.go                 # Adaptador Subprocesso CLI (claude-code)
│   ├── mcp/                       # Cliente MCP Stateless (Spec 2026-07-28)
│   │   ├── client.go              # Cliente HTTP Stateless / Stdio
│   │   ├── protocol.go            # Structs JSON-RPC 2.0 e _meta
│   │   └── registry.go            # Catálogo e cache de ferramentas MCP
│   ├── skills/                    # Injetor e Resolver de Skills
│   │   └── resolver.go            # Fusão de prompts, escopo de ferramentas e I/O
│   ├── tools/                     # Implementação de Ferramentas Nativas
│   │   ├── shell.go               # Comandos de sistema (cmd.exec)
│   │   ├── git.go                 # Operações Git
│   │   ├── fs.go                  # File System
│   │   └── http.go                # Cliente HTTP
│   ├── traffic/                   # Resiliência e Traffic Shaping
│   │   ├── cache.go               # Caching SHA-256 e TTL
│   │   ├── rate_limit.go          # Token Bucket e Semáforos
│   │   └── retry.go               # Exponential Backoff
│   └── auth/                      # Injeção e Resolução de Credenciais
│       └── resolver.go            # Leitura de ENV e Cofres locais
├── pkg/                           # Código público exportável (ex: SDK para incorporar MHL em Go)
│   └── mhlsdk/
│       └── sdk.go
├── test/                          # Testes de Integração e E2E
│   └── fixtures/                  # Scripts .mhl de teste
├── go.mod
├── go.sum
└── Makefile
```

---

## 8. Roadmap de Implementação

* **Fase 1 (MVP Core, Auth & Skills):** Finalização do parser Participle em Go, suporte à declaração e injeção de Skills, gerenciador de credenciais e invocação CLI local/APIs.
* **Fase 2 (Stateless MCP & Resiliency):** Cliente HTTP/Stdio MCP sob a especificação `2026-07-28`, motor de retries, fallbacks e controle de Rate Limits.
* **Fase 3 (Checkpointing & State Recovery):** Sistema de salvamento em `.mhl/state` e opção `--resume` no CLI.
* **Fase 4 (Cache & Tooling):** Respeito ao `ttlMs` do MCP para auto-caching de ferramentas, sistema de imports e extensão LSP para IDEs.