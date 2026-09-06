# PROJETO DE TREINAMENTO: ESCALANDO WORKFLOWS COM AGENTES DE INTELIGÊNCIA ARTIFICIAL

## 1. Introdução

Esta proposta apresenta um treinamento prático para projetar, operar e evoluir workflows de documentação e automação baseados em agentes de inteligência artificial no mercado financeiro, com foco na área de seguros. O curso utiliza como estudo de caso o servidor MHL MCP executado sobre Kubernetes na AWS, com exposição por API Gateway e processamento assíncrono em um cluster Amazon EKS.

O cenário considera vários times, cada um em sua própria conta AWS, acionando um endpoint regional compartilhado. O caso de negócio é a triagem e regulação assistida de sinistros residenciais e de automóveis: o segurado envia relato, fotos e nota fiscal; o sistema consulta a apólice, verifica consistência, aciona antifraude e prepara um parecer. As execuções podem durar vários minutos ou aguardar uma decisão humana por horas ou dias, chamar múltiplos agentes e produzir efeitos financeiros. Por isso, o treinamento aborda tanto a arquitetura AWS quanto os requisitos de confiabilidade, segurança e governança do runtime: estado durável, sessões compartilhadas, isolamento por identidade, controle de concorrência, retomada, cancelamento, idempotência, evidências, auditoria e observabilidade.

## 2. Objetivo

Ao final do treinamento, os participantes deverão ser capazes de:

- avaliar quando um workflow de agentes precisa ser assíncrono;
- desenhar um caminho seguro de requisição com API Gateway, VPC Link, ALB e EKS;
- identificar por que estado de execução e sessões não podem depender da memória de um único pod;
- implementar ou configurar persistência compartilhada para checkpoints, sessões, owner e progresso;
- aplicar isolamento por principal/tenant entre times e execuções;
- dimensionar concorrência, fila, autoscaling e limites para chamadas a provedores de LLM;
- configurar health checks, readiness, graceful shutdown e drain de pods;
- estruturar logs, métricas e recuperação de logs por execução;
- modelar um fluxo human-in-the-loop com `run/start`, `run/status` e `run/resume`;
- separar responsabilidades entre runtime, API Gateway e service mesh;
- validar a solução com cenários de teste e critérios objetivos de aceite.

## 3. Público-alvo

Profissionais que projetam, desenvolvem ou operam plataformas de IA e workloads em nuvem:

- Principal/Staff Software Engineers;
- arquitetos de soluções e de plataforma;
- engenheiros de backend, SRE e DevOps;
- equipes responsáveis por Kubernetes, EKS, integração MCP ou workflows de agentes;
- líderes técnicos que precisam avaliar prontidão para produção.

## 4. Pré-requisitos

É recomendável que os participantes tenham:

- conhecimento básico de APIs HTTP e JSON-RPC/MCP;
- experiência com containers e conceitos fundamentais de Kubernetes;
- familiaridade com AWS, especialmente IAM, VPC e serviços gerenciados;
- noções de execução de workflows, filas e persistência de estado;
- acesso a um ambiente de laboratório ou repositório para os exercícios práticos.

O treinamento pode incluir uma breve revisão dos conceitos de Kubernetes e AWS caso o grupo precise nivelamento.

## 5. Conteúdo programático

### Módulo 1 — O problema de escalar workflows de agentes

- Características de workloads bursty, long-running e com fan-out para LLMs.
- Diferença entre uma chamada síncrona a `tools/call` e uma execução assíncrona.
- Ciclo de vida de uma execução: `run/start` → `run/status` → `run/resume`/`run/cancel`.
- Human-in-the-loop: checkpoint em um gate, aprovação e retomada.
- Riscos de uma arquitetura single-process/single-pod.

### Módulo 2 — Linguagem MHL: do programa ao workflow executável

- O que é a Meta-Harness Language: uma linguagem executável pequena para orquestrar agentes, prompts, ferramentas, memória e serviços externos.
- Princípio central: o programa declara o fluxo, as políticas e os limites; a IA contribui com julgamento onde é útil.
- Estrutura de um programa: arquivos `.mh`, declarações de topo, `import`, `export` e aliases.
- Sintaxe e tipos: blocos sem ponto e vírgula, comentários, strings multilinha, durações, arrays, objetos, tipos estruturais, aliases e `enum`.
- Expressões e composição: operadores, interpolação, lambdas, métodos de strings/arrays/objetos, `match`, encadeamento opcional (`?.`) e coalescência nula (`??`).
- Declaração de agentes: engines CLI/Ollama, `prompt`, `schema`, retry, cache, rate limit, fallback e hooks `before`/`after`.
- Prompts nomeados e tipados: parâmetros por nome, valores padrão, composição e templates Markdown externos.
- Estado nomeado: `memory` para stores persistentes e `mem` para variáveis persistentes de pipelines em loop.
- Ferramentas e operações nativas: `tool`, `cmd`, `fs`, `git`, `json`, `http`, `log`, `time` e `uuid`.
- Integrações externas: `extension mcp` para servidores MCP e `extension a2a` para agentes remotos Agent2Agent, com credenciais resolvidas por `env()`.
- Controle de execução: `pipeline` para fluxo ordenado e `workflow` para máquina de estados com `goto`.
- Resiliência no código: `try/catch/finally`, `fail`, `break`, `pause`, timeouts, checkpoints e retomada.
- Iteração e paralelismo: `loop`, `repeat`, `stop_when`, `max_iterations`, `spawn`, `wait`, fan-out/fan-in e grupos `parallel` com barreira determinística.
- Testes e ferramentas de desenvolvimento: `test`, `describe`, asserções, `incomplete`, `mhl lint`, `mhl test` e `mhl lsp`.
- Publicação: `mhl serve mcp` em stdio ou Streamable HTTP, `mhl serve a2a` e recursos de introspecção `mhl://`.

#### Laboratório do módulo

Os participantes irão transformar uma decisão de regulação em um workflow MHL executável, testável e retomável:

```mhl
memory ClaimsAudit {
    type: "jsonl"
    path: ".mhl/claims-audit.jsonl"
}

workflow RegulateClaim {
    description: "Assess a claim and route it to payment or review."

    input claim_id: string
    input claim_amount: number
    input confidence: number
    input approved: string

    var route = ""
    var cleared = false

    step Assess {
        ClaimsAudit.append({event: "assessed", claim_id: claim_id, at: time.now()})
    }

    step Route {
        if (claim_amount <= 5000 && confidence >= 0.9) {
            route = "automatic"
            cleared = true
        } else {
            route = "human"
            goto HumanReview
        }
    }

    step HumanReview {
        if (!cleared && approved != "yes") {
            pause("awaiting regulator approval for: " + claim_id)
        }
    }

    step Register {
        ClaimsAudit.append({event: route, claim_id: claim_id, at: time.now()})
        log.info("claim routed", claim_id, route)
    }
}
```

O exercício inclui executar com `mhl lint`, iniciar com `mhl run`, inspecionar o estado `paused`, retomar com `--resume` e publicar o workflow como ferramenta MCP. Como extensão, o grupo adicionará um agente com retry/cache, um `prompt` tipado e um teste `describe` para validar o comportamento sem depender de uma chamada real a LLM.

### Módulo 3 — Arquitetura de referência na AWS

- Times em contas AWS distintas e endpoint compartilhado.
- Separação entre plano de controle (governança, catálogo, versões, políticas e avaliações) e plano de dados (execução por LOB/sessão).
- API Gateway HTTP como porta de entrada autenticada.
- JWT authorizer, Cognito/Lambda authorizer ou integração com o provedor de identidade.
- VPC Link, ALB interno e AWS Load Balancer Controller.
- Deployment no EKS, Service e distribuição das requisições entre réplicas.
- Egress para Anthropic/Bedrock via NAT Gateway ou VPC endpoint.
- Trade-off entre runtime gerenciado com isolamento por sessão e runtime próprio em EKS, considerando operação, rede, sidecars e requisitos de isolamento.
- Versionamento imutável de imagem, workflow, prompt, configuração e guardrail, com promoção explícita entre ambientes.

### Módulo 4 — Estado distribuído e retomada entre réplicas

- Por que registro de runs, checkpoints e sessões em memória do pod falha com load balancing.
- Contratos de `StateStore`, `CheckpointStore` e `SessionStore`.
- Uso de `--state-dir` para estado compartilhado local/disco e uso de `extension store` para backends externos.
- S3 como backend durável, com SigV4, retry e IRSA/IMDSv2.
- PostgreSQL como backend durável, com upsert atômico e controle de concorrência.
- Reconstrução e retomada de uma execução em outro processo ou pod.
- Registro de progresso (`run-status`) e cancelamento distribuído.

### Módulo 5 — Identidade, isolamento e segurança

- Autenticação no ingress versus autorização no runtime.
- Propagação da identidade verificada por `X-Mhl-Principal` e `--principal-header`.
- Ownership de runs, sessões e logs por principal.
- Proteção contra acesso cruzado em `run/list`, `run/status`, `run/logs` e `run/resume`.
- IRSA e acesso mínimo a Secrets Manager e ao armazenamento de estado.
- O que permanece no service mesh: validação JWT/JWKS e `AuthorizationPolicy` para definir quem pode aprovar.
- Uso do token compartilhado apenas na relação gateway ↔ runtime, como proteção contra spoofing do header.

### Módulo 6 — Concorrência, fila e ciclo de vida do pod

- Limite global de execuções com `--max-concurrent-runs`.
- Semáforo ponderado, fila limitada, estado `queued` e `queuePosition`.
- Shed load e resposta de capacidade (`-32000`) para chamadas síncronas.
- Métricas de execuções ativas e enfileiradas para orientar o HPA.
- Endpoints `/healthz` e `/readyz`.
- Sinalização de drain após `SIGTERM`, parada de novas execuções e conclusão das execuções em andamento.
- `--drain-timeout`, checkpoint de shutdown e retomada após reinício.
- PodDisruptionBudget, deploys, scale-in e node drain sem interromper o trabalho de forma indevida.

### Módulo 7 — Observabilidade e operação

- Logs estruturados em JSON com `runId`, principal, workflow, etapa e resultado.
- Recuperação de logs por execução com `run/logs` e cursor `since`.
- Métricas Prometheus: total de runs, duração, chamadas de ferramentas, sessões, runs ativas e fila.
- Integração com CloudWatch Logs, Amazon Managed Prometheus e ADOT.
- Dashboards por time e workflow.
- Tracing distribuído e contabilização de tokens/custo como evolução da solução.
- Runbooks para falhas de provider, fila cheia, pod em drain e store indisponível.

### Módulo 8 — Contrato MCP e critérios de aceite

- Transporte Streamable HTTP e endpoint `POST /mcp`.
- Descoberta da extensão assíncrona em `initialize`/`server/discover`.
- `capabilities.experimental["mhl.run"]` e metadados `tools/list`.
- Compatibilidade com clientes que ignoram capacidades experimentais.
- Testes de protocolo, isolamento, concorrência, restart, drain, logs e ciclo de vida Kubernetes.
- Como transformar gaps de arquitetura em cenários reproduzíveis e critérios de aceite.

## 6. Laboratório prático — sinistro residencial e auto

O workshop simulará a triagem e a regulação assistida de um sinistro. O objetivo não é fazer o agente “decidir sozinho”, mas reduzir o tempo de ciclo e entregar ao regulador humano um dossiê completo, rastreável e pronto para revisão.

### Cenário

Um segurado abre um aviso pelo aplicativo e envia relato, fotos do dano e nota fiscal. O workflow deve:

1. autenticar o titular, criar ou localizar o `claimId` e garantir idempotência do aviso;
2. extrair fatos dos documentos e imagens com um agente especializado, mantendo as evidências originais;
3. consultar a apólice, a cobertura vigente e as condições gerais aplicáveis ao titular;
4. verificar a consistência entre relato, documentos, imagens e histórico do sinistro;
5. acionar a capacidade antifraude por uma tool com escopo explícito;
6. produzir um parecer com cobertura encontrada, evidências, confiança, inconsistências e versão do prompt/agente;
7. encaminhar para pagamento automático somente quando o valor estiver abaixo do limite e a confiança estiver acima do mínimo definido;
8. enviar casos acima do limite, com baixa confiança ou com inconsistências para a fila do regulador humano;
9. registrar o resultado apenas por uma operação idempotente no workflow/serviço de destino, nunca por uma escrita livre do agente.

### Fluxo técnico do exercício

O caminho será modelado como:

`App → WAF/API Gateway → orquestrador do caso → workflow MHL por etapa → tools de apólice/documentos/antifraude → parecer → pagamento idempotente ou fila do regulador`

No laboratório, o `orquestrador do caso` será representado por `mhl_run_start`, `mhl_run_status` e `mhl_run_resume`, com `pause()` simulando a espera do regulador. Na arquitetura de produção, a recomendação é que o AWS Step Functions seja o dono do caso e da espera longa, usando task token para a interação humana; o MHL deve ser invocado por etapa e recuperar o contexto em memória persistida. Assim, o treinamento evidencia a diferença entre uma demonstração funcional e uma solução adequada a uma regulação que pode atravessar deploys, timeouts, perícias e dias de espera.

### Exercícios e critérios de aceite

- executar um caso de baixo valor e alta confiança pela rota automática simulada;
- executar um caso de alto valor ou baixa confiança até o estado `paused`;
- consultar `run/status` e `run/logs` em uma réplica diferente;
- retomar o caso com decisão do regulador e verificar o checkpoint;
- repetir a chamada de pagamento e comprovar que a chave de idempotência evita duplicidade;
- tentar acessar o caso com outro principal e comprovar o isolamento;
- reiniciar ou colocar o pod em drain e verificar a retomada do trabalho;
- reconstruir a trilha com entrada, documentos, tools, evidências, versões, decisão e revisor.

Serão usados dados sintéticos, documentos fictícios e serviços simulados. O laboratório não realizará pagamento, alteração de apólice ou consulta a dados reais.

## 7. Metodologia

O treinamento combina exposição conceitual, leitura guiada da arquitetura, desenho colaborativo, demonstrações e exercícios orientados. Cada decisão é relacionada a um risco operacional concreto e validada por um cenário de teste.

A dinâmica sugerida é:

- 40% de fundamentos e arquitetura;
- 40% de laboratório e implementação/configuração;
- 20% de análise de incidentes, revisão e critérios de aceite.

## 8. Duração e formato

Formato recomendado: **2 dias, total de 16 horas**, presencial ou remoto, com turma de até 12 participantes.

### Dia 1 — Arquitetura e execução distribuída

- problema de negócio e modelo assíncrono;
- fundamentos da linguagem MHL e anatomia de um workflow;
- agentes, prompts, memória, ferramentas, tipos e controle de fluxo;
- laboratório de `mhl lint`, `mhl run`, testes e `pause()`;
- arquitetura AWS/EKS;
- sessões, checkpoints e estado compartilhado;
- identidade, isolamento e segurança;
- laboratório de `run/start`, `run/status` e retomada.

### Dia 2 — Produção, operação e validação

- concorrência, fila e autoscaling;
- health checks, drain e shutdown;
- logs, métricas e operação;
- service mesh e autorização de aprovadores;
- laboratório de restart, cancelamento e isolamento;
- revisão da arquitetura e plano de evolução.

## 9. Entregáveis

- material didático e arquitetura de referência;
- roteiro do laboratório e exemplos de chamadas MCP;
- checklist de prontidão para execução multi-réplica no EKS;
- matriz de riscos, decisões e responsabilidades entre runtime, gateway e mesh;
- conjunto de cenários de aceite para persistência, isolamento, concorrência, drain e observabilidade;
- avaliação final da arquitetura do grupo e recomendações de próximos passos.

## 10. Critérios de sucesso

Ao concluir o treinamento, o grupo deverá conseguir explicar, implementar ou validar uma solução na qual:

- o API Gateway seja a única porta de entrada e autentique os clientes;
- o EKS possa distribuir chamadas entre múltiplos pods;
- sessões, checkpoints, owner e progresso não dependam da memória de uma réplica;
- `run/status`, `run/resume` e `run/cancel` funcionem mesmo quando chegam a outro pod;
- a identidade do time seja propagada e respeitada em runs e logs;
- a capacidade do servidor seja protegida por limite e fila;
- o Kubernetes diferencie liveness, readiness e drain;
- logs e métricas permitam investigar uma execução sem acessar diretamente o pod;
- o gate de aprovação humana tenha isolamento no runtime e política de autorização no mesh;
- nenhum efeito financeiro seja executado diretamente pelo loop do agente;
- cada escrita em sistema de registro use autorização no serviço de destino e chave de idempotência gerada pelo workflow;
- o caso seja roteado por valor, confiança e inconsistência, com revisão humana acima do limite;
- a decisão possa ser revisada com as evidências, versões de agente/prompt, tools chamadas e revisor humano;
- dados, memória, conhecimento e orçamento estejam filtrados e contabilizados por LOB/tenant;
- cada requisito relevante tenha um teste ou critério de aceite associado.

## 11. Avaliação de alinhamento com o documento do Dia 6

### Veredito

A arquitetura de referência MHL em API Gateway + ALB + EKS está **parcialmente alinhada** com a proposta do documento. Ela cobre bem a camada de runtime distribuído e já resolve problemas importantes de produção: execução assíncrona, checkpoints, sessões entre réplicas, isolamento por principal, fila, drain, logs e métricas. Porém, o Caso 01 exige uma plataforma de seguros mais ampla. A arquitetura atual ainda precisa explicitar o dono do caso de longa duração, as fronteiras por LOB, o plano de controle, a política determinística por tool, a idempotência das escritas e a trilha regulatória completa.

A avaliação foi feita com base no [documento do Dia 6](dia-06-runtime-e-arquitetura-agentic-em-escala.pdf), especialmente o Caso 01, e na documentação da linguagem em [`docs/site/`](site/README.md), incluindo a referência MHL e o guia de serving.

| Aspecto | Avaliação | Ajuste necessário para o Caso 01 |
| --- | --- | --- |
| Workflow determinístico com passos agentic | **Alinhado** | Manter MHL como esqueleto explícito e usar agentes apenas nos nós ambíguos, evitando swarm e autonomia sem justificativa. |
| Sinistro longo e revisão humana | **Parcial** | Usar `run/*` e `pause()` no laboratório; em produção, Step Functions deve possuir o estado do caso e o task token da espera humana. O MHL deve ser invocado por etapa. |
| Estado e retomada entre pods | **Alinhado como runtime** | Manter `extension store` S3/PostgreSQL ou `--state-dir` para checkpoints e sessões, acrescentando retenção, evidência e estado de negócio do sinistro. |
| Multi-tenant e multi-LOB | **Parcial** | `X-Mhl-Principal` e owner isolam execuções, mas é preciso separar ou escopar por LOB a execution role, CMK, quota, memória e índice de conhecimento; namespaces, ServiceAccounts e endpoints por LOB são opções para o EKS. |
| Autenticação e autorização | **Parcial** | API Gateway/JWT, token gateway↔runtime e IRSA estão corretos. Falta policy determinística por chamada de tool e autorização no sistema de registro; prompt não pode ser o controle de alçada. |
| Escritas com efeito financeiro | **Não coberto explicitamente** | Pagamento, registro de parecer e alteração de sinistro devem passar por serviço transacional/Step Functions, com autorização de destino e chave de idempotência criada pelo workflow. |
| Plano de controle | **Ausente na referência atual** | Adicionar catálogo, registro de versões, promoção dev→hml→prd, avaliação offline/online, IaC/CI-CD e endpoints de produção que não apontem para um `DEFAULT` mutável. |
| Observabilidade e auditoria | **Parcial** | ADOT, CloudWatch, Prometheus, `runId` e logs já são uma boa base. Faltam trace por passo/tool, versão do agente/prompt/guardrail, evidências, revisor, CloudTrail, retenção imutável e custo por LOB/sinistro. |
| Escolha do runtime | **Condicionalmente alinhado** | O documento recomenda AgentCore Runtime como caminho padrão quando isolamento por sessão, identidade e observabilidade são decisivos. EKS é válido quando rede, sidecars e operação própria justificarem a exceção; essa decisão deve ser registrada. |
| Escala e orçamento | **Parcialmente alinhado** | HPA e `--max-concurrent-runs` protegem o serviço. Incluir quota por LOB, capacidade reservada e degradação para picos de evento climático estimados em 5–10× a média diária; essa é uma estimativa de dimensionamento, não um número oficial. |
| Conhecimento e dados pessoais | **Parcial** | Secrets Manager não substitui um repositório de apólices/condições gerais com filtro obrigatório de titularidade/LOB, retenção LGPD e controle de acesso à memória longa. |

### Arquitetura-alvo recomendada para o workshop

Para aproximar a referência do Caso 01, o desenho didático deve apresentar o EKS/MHL como uma camada de execução dentro de uma arquitetura maior:

1. o canal passa pelo WAF e API Gateway, que autentica e deriva o principal;
2. um orquestrador de caso cria o `claimId`, controla idempotência, orçamento e estado de negócio;
3. o Step Functions coordena as etapas, esperas externas e task token do regulador;
4. o MHL executa etapas de extração, consulta, consistência, antifraude e composição do parecer;
5. tools de escrita chamam serviços transacionais protegidos, nunca o sistema de registro diretamente a partir do agente;
6. S3/Object Lock ou store equivalente preserva evidências e a trilha de auditoria, enquanto S3/PostgreSQL mantém checkpoints e estado operacional;
7. OTEL/ADOT, X-Ray, CloudTrail e CUR correlacionam execução, tool calls, versões, revisor e custo por LOB.

Assim, a arquitetura atual não precisa ser descartada: ela deve ser reposicionada como o runtime próprio em EKS para as etapas MHL. A principal mudança é não tratá-la como dona exclusiva de uma sessão que pode ficar aguardando um humano por dias.

## 12. Estado atual e plano de evolução

O estudo de caso parte de uma implementação já validada em regressões do projeto:

- 18/18 cenários da suíte single-pod `tests/cloud/tests_mcp_pods/`;
- 21/21 cenários da suíte de extensões `tests/extensions/tests_ext/`;
- cenários de distribuição para registro live/cancelamento, sessões compartilhadas e descoberta da extensão `run/*` atendidos;
- backends de produção para `extension store` em S3 e PostgreSQL validados ponta a ponta;
- health/readiness, drain, checkpoint de shutdown, limite de concorrência, logs por run e isolamento por principal implementados e verificados.

O plano de evolução discutido no curso diferencia claramente o que já está pronto do que ainda exige trabalho de desenho:

- tracing OpenTelemetry e contabilização de tokens/custo por time e workflow;
- integração final de JWT/JWKS no API Gateway ou Istio;
- `AuthorizationPolicy` para autorização específica de aprovadores;
- eventual promoção automática de uma chamada longa de `tools/call` para uma execução assíncrona;
- orquestração de casos de seguro com Step Functions, task token e retomada fora da sessão viva;
- catálogo/versionamento de agentes, prompts, guardrails, políticas e endpoints por ambiente;
- política determinística por tool, escrita idempotente e trilha imutável de evidências;
- isolamento operacional e financeiro por LOB, incluindo quota, CMK, memória, conhecimento e tags de custo.

Os números e o status acima refletem a nota de arquitetura de **31/08/2026**, baseada na versão `mhl v1.1.0-alpha-7-g418d7dc`; devem ser atualizados caso o treinamento seja realizado sobre uma versão posterior.

## 13. Encerramento

O treinamento prepara a equipe para tratar workflows de agentes como uma plataforma distribuída: com contratos explícitos, estado durável, identidade, limites operacionais, recuperação e observabilidade. O resultado esperado não é apenas uma implantação funcional, mas uma arquitetura que possa crescer de um único pod para vários times e réplicas sem perder confiabilidade, segurança ou capacidade de diagnóstico.
