# Guia de Exemplos Práticos da Linguagem MHL (Meta-Harness Language)

Este documento reúne todos os exemplos de uso da linguagem **MHL**, cobrindo desde a declaração de módulos, agentes locais e remotos até o uso de servidores MCP, Skills modulares, controle de estado com checkpoints e orquestração de pipelines complexos.

---

## 1. Módulos, Importações e Reuso

A MHL permite organizar o código em arquivos reutilizáveis usando namespaces e importações seletivas.

```mhl
// Importa um módulo completo sob um namespace (alias)
import "./agentes/qualidade.mhl" as qa
import "./tools/system.mhl" as sys

// Importação seletiva de símbolos específicos de um arquivo
use { ClaudeCoder, KimiArchitect } from "./agentes/llms.mhl"
use { SecurityAuditPrompt } from "./prompts/seguranca.mhl"

// Cada símbolo seletivo também pode receber um alias local
use { FeatureStore as store, RunConfig as config, PlanReader as planner } from "./tools/feature.mhl"

// Exportação de componentes para uso em outros arquivos .mhl
export agent CustomAuditor {
    engine: "anthropic/claude-3-5-sonnet"
    temperature: 0.1
}

```

---

## 2. Prompts Dinâmicos

Prompts são cidadãos de primeira classe na linguagem e suportam interpolação de variáveis dinâmicas.

```mhl
prompt SecurityAuditPrompt(file_path: string, code_content: string) {
    """
    Você é um especialista em segurança de software.
    Analise o arquivo '${file_path}' abaixo:

    ```
    ${code_content}
    ```

    Identifique potenciais vulnerabilidades (OWASP Top 10) e retorne correções objetivas.
    """
}

prompt ArchitectureDesignPrompt(task_description: string) {
    """
    Você é o Arquiteto do Sistema. 
    Analise o seguinte requisito e crie uma especificação técnica e plano de componentes:
    
    ${task_description}
    """
}

```

O corpo de um `prompt` também pode vir de um arquivo Markdown externo, resolvido em relação ao diretório do arquivo `.mh` que o declara — a mesma regra de resolução usada por `import`/`use`. Isso permite escrever o prompt como Markdown comum (com front-matter, headings, blocos de código) em vez de escapá-lo dentro de uma string:

```mhl
prompt SecurityAuditPrompt(file_path: string, code_content: string) from "./security-audit.prompt.md"
```

O arquivo carregado é tratado exatamente como um corpo `"""..."""` inline a partir daí: `${param}` continua interpolando normalmente. Como um Markdown trazido de fora tende a conter `${...}` incidental que não é parâmetro nenhum (exemplos de shell, JSON, variáveis de ambiente), um `${...}` pode ser escapado com `\${...}` para renderizar como texto literal em vez de ser validado como parâmetro declarado:

```mhl
prompt DeployRunbook() from "./deploy.prompt.md"
// dentro de deploy.prompt.md: "defina \${TARGET_DIR} antes de rodar o script"
// renderiza como texto literal "${TARGET_DIR}", sem exigir um parâmetro TARGET_DIR
```

---

## 3. Servidores MCP (Model Context Protocol - Stateless)

Declaração de servidores do ecossistema MCP para integração de ferramentas padrão via `stdio` local ou requisições `http` desacopladas.

```mhl
// Servidor MCP Local via stdio (invocado via linha de comando local)
mcp_server PostgresDB {
    transport: "stdio"
    command: "npx"
    args: ["-y", "@modelcontextprotocol/server-postgres", env("DATABASE_URL")]
}

// Servidor MCP Remoto via HTTP Stateless com injeção segura de token
mcp_server GitHubServer {
    transport: "http"
    url: "https://mcp.github.com/v1"
    headers: {
        "Authorization": "Bearer " + env("GITHUB_TOKEN")
    }
}

```

---

## 4. Skills Modulares (Habilidades Encapuladas)

Skills empacotam o conhecimento procedural, prompts especializados e o escopo restrito de ferramentas exigidos para uma habilidade concreta.

```mhl
export skill CodeAuditorSkill {
    description: "Analisa código procurando falhas de segurança OWASP e gargalos de performance."
    
    // Ferramentas que a Skill disponibiliza ao Agente durante a execução
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

    // Instruções de contexto injetadas automaticamente no sistema do Agente
    system_instructions: """
    Você é um auditor de segurança sênior. Siga rigorosamente as normas ISO/IEC 27001.
    Nunca aprove código com credenciais expostas no código-fonte.
    """
}

```

---

## 5. Agentes (APIs Remotas, CLIs Locais e Ollama)

Agentes unificam a invocação de LLMs em nuvem ou locais com políticas de autenticação, resiliência, fallbacks, limitação de taxa e atrelamento de Skills.

```mhl
// 1. Agente em Nuvem com Fallbacks, Rate Limit, Caching e Skills
agent ClaudeCoder {
    engine: "anthropic/claude-3-5-sonnet"
    api_key: env("ANTHROPIC_API_KEY") // Injeção segura via variável de ambiente
    temperature: 0.2
    timeout: 45s
    
    skills: [CodeAuditorSkill]

    retry: {
        max_attempts: 3
        backoff: "exponential"
        delay: 2s
        retry_on: [500, 503, "rate_limit", "timeout"]
    }
    
    // Fallback em cascata caso a Anthropic falhe
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

// 2. Agente Local via CLI (Herda login e sessão do terminal da máquina)
agent LocalClaudeCLI {
    engine: "cli/claude-code"
    command: "claude"
    args: ["--dangerously-skip-permissions", "--non-interactive"]
    timeout: 60s
}

// 3. Agente Local em servidor de inferência On-Premise (Ollama/vLLM)
agent LocalOllamaCoder {
    engine: "ollama/qwen2.5-coder"
    endpoint: "http://localhost:11434"
    temperature: 0.2
}

```

---

## 6. Gerenciamento de Memória

Declaração dos três tipos de armazenamento nativos: efêmera (sessão), chave-valor e banco vetorial (RAG).

```mhl
// Memória Key-Value em memória para o fluxo atual
memory session_mem {
    type: "kv"
    store: "memory"
}

// Memória vetorial para busca semântica RAG no projeto
memory project_rag {
    type: "vector"
    provider: "chroma"
    path: "./.mhl/vector_db"
}

// Memória de histórico persistente em arquivo de log
memory audit_log {
    type: "append_log"
    path: "./logs/audit.log"
}

```

---

## 7. Ferramentas Nativas (Tools)

Mapeamento seguro de comandos de Terminal (Shell), Git, Leitura/Escrita de Arquivos e Requisições HTTP.

```mhl
tool execution {
    // Terminal / Shell Command
    run_tests() -> cmd.exec("dotnet test", timeout: 120s)
    
    // Git
    get_diff() -> git.diff(target: "HEAD~1", capture: true)
    
    // File System
    read_file(path: string) -> fs.read(path)
    write_file(path: string, content: string) -> fs.write(path, content)
    
    // HTTP / Webhooks
    notify_slack(webhook_url: string, message: string) -> http.post(
        url: webhook_url,
        headers: {"Content-Type": "application/json"},
        body: {"text": message}
    )
}

```

---

## 8. Pipelines de Orquestração Completa

Orquestração procedural com controle de fluxo (`step`, `if`, `while`, `try/catch`), checkpoints e retomada de estado.

### **Pipeline Completo de Desenvolvimento Automatizado**

```mhl
pipeline AutoFixPipeline {
    input issue_id: string
    input target_file: string

    // Salva o progresso no disco para permitir 'mhl run --resume' em caso de falha
    checkpoint: {
        enabled: true
        strategy: "per_step"
        storage: "file"
        ttl: 7d
    }

    // Etapa 1: Inspecionar alterações no repositório
    step InspectGit {
        var diff = execution.get_diff()
        if (diff.is_empty()) {
            fail("Nenhuma alteração detectada no repositório para análise.")
        }
        session_mem.set("current_diff", diff)
    }

    // Etapa 2: Executar auditoria especializada usando a Skill
    step AuditWithSkill {
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

    // Etapa 3: Loop de Refinamento e Correção Automatizada
    step RefinementLoop {
        var max_attempts = 3
        var attempt = session_mem.get("attempt", 0) // Recupera o valor se for uma retomada
        var fixed = false
        var last_error = session_mem.get("last_error", "")

        while (!fixed && attempt < max_attempts) {
            try {
                // Tenta gerar a correção usando o agente
                var fix = ClaudeCoder.run(
                    prompt: "Corrija o código baseado no feedback: ${session_mem.get('last_review')}\nErros de execução anterior: ${last_error}"
                )

                // Aplica a alteração no código do projeto
                execution.write_file(fix.file_path, fix.content)

                // Roda os testes para validar a correção
                var test_res = execution.run_tests()

                if (test_res.exit_code == 0) {
                    fixed = true
                    checkpoint.clear() // Limpa o estado salvo após o sucesso
                } else {
                    attempt = attempt + 1
                    last_error = test_res.stderr

                    // Atualiza o estado da sessão antes de persistir o checkpoint
                    session_mem.set("attempt", attempt)
                    session_mem.set("last_error", last_error)
                    checkpoint.save()
                }
            } catch AgentTimeoutError as err {
                audit_log.append("Timeout ao tentar corrigir arquivo: ${err.message}")
                attempt = attempt + 1
            } catch Exception as err {
                audit_log.append("Erro genérico no step: ${err.message}")
                fail("Interrompendo pipeline devido a erro crítico.")
            }
        }

        if (!fixed) {
            execution.notify_slack(env("SLACK_WEBHOOK"), "Falha ao corrigir Issue #${issue_id} automaticamente.")
            fail("Pipeline interrompido após ${max_attempts} tentativas sem sucesso.")
        }
    }

    // Etapa 4: Finalização e Notificação
    step Finalize {
        execution.notify_slack(env("SLACK_WEBHOOK"), "Issue #${issue_id} corrigida e validada com testes!")
    }
}

```

---

## 9. Ponto de Entrada / Execução Principal

Arquivo principal de inicialização (`main.mhl`) responsável por invocar pipelines e ler variáveis de ambiente do sistema.

```mhl
import "./tasks/development_tasks.mhl" as dev

pipeline Main {
    step Run {
        var target_issue = env("ISSUE_ID", "BUG-101")
        var file_to_fix = env("TARGET_FILE", "src/main.py")
        
        dev.AutoFixPipeline.run(
            issue_id: target_issue,
            target_file: file_to_fix
        )
    }
}

```
