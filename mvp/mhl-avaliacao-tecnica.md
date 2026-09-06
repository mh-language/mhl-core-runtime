# MHL | Reavaliação do Runtime

## Parecer de engenharia

**O Runtime está apto a compor o MVP de apresentação à diretoria como motor de execução atrás de gateway, com acesso controlado, dados sintéticos e fluxos já testados. A plataforma pode distribuir runs entre workers; o requisito é definir quem controla as tentativas de cada run. Não encontrei bloqueador desse cenário na suíte executada. As lacunas residuais de R3-R6 limitam cenários específicos de retomada, exposição de estado e operação distribuída; não justificam adiar a apresentação para concluir todo o roadmap.**

Revisão de 06/09/2026. Base: `b76ed72` mais alterações locais, incluindo `inputs.go` e `runlog_test.go` ainda sem commit. Os hashes dos arquivos Go e o estado Git estão em `evidencias-reavaliacao-runtime-2026-09-06/snapshot.json`. Esta avaliação não representa uma release publicada.

Foram lidos os caminhos de execução, pausa, entradas, credenciais, logs e coordenação. A suíte permanente foi repetida sem cache; sondas isoladas testaram condições além das regressões existentes. Nenhuma implementação foi corrigida nesta revisão. As sondas foram retiradas da árvore de código e preservadas como evidência.

### Escopo e regra de decisão do MVP

A apresentação deve demonstrar autoria do fluxo, execução, pausa/aprovação, retomada normal e publicação MCP, usando exemplos controlados. Uma chamada real a modelo externo só deve ser prometida se for ensaiada previamente; os testes locais não comprovam disponibilidade ou qualidade desse provedor. Integrações simuladas devem ser identificadas como tal.

**Critério finito de fechamento:** escolher o roteiro, ensaiá-lo uma vez no mesmo binário/ambiente da apresentação e registrar commit mais alterações locais (ou tag após congelar a versão). Se passar, congelar o escopo. Só reabrir por falha que interrompa esse roteiro ou exponha os dados efetivamente usados. Não é necessário evoluir a coordenação interna para demonstrar um perfil que delega o despacho e a posse das runs à plataforma. Esta revisão executou testes e exemplos, não o roteiro específico da apresentação, ainda não informado.

### Resultado por critério

| Item | Resultado da reavaliação |
|---|---|
| R1 - JSON de sucesso | Corrigido no escopo testado: redação recursiva em resultados legados e explícitos. |
| R2 - Credenciais | Corrigido no escopo testado: valor exato, inferência separada, referência ambígua impede resume. |
| R3 - Logs fragmentados | Parcial: fluxo inicial protegido; retomada após falha/cancelamento reutiliza buffer selado. |
| R4 - Lease confirmado | Parcial: erro de aquisição bloqueia execução; resposta lenta ainda confirma lease expirado. |
| R5 - Liberação condicional | Parcial: protege sucessor em outra réplica; token compartilhado por run não isola tentativas na mesma réplica. |
| R6 - Pausa e output | Parcial: pausa não avalia JSON final; motivo estruturado falta no CLI e estado interno é exposto. |
| R7 - Retry | Corrigido: teto aplicado desde a primeira espera, com cancelamento e classificação preservados. |
| R8 - Dry-run | Corrigido no contrato testado: validação/coerção pura compartilhada com Run. |

**Validação permanente:** suíte Go completa aprovada; vet limpo; build aprovado; 389 asserções funcionais aprovadas, zero falhas e 8 incompletas. Race aprovado em sete pacotes. **Validação adversarial:** cinco sondas falharam nas condições descritas nas próximas páginas. A suíte verde não cobre todos esses contratos.

---

## Recursos e melhorias confirmadas

### Linguagem e execução

MHL reúne pipelines/workflows, agentes, prompts, ferramentas e extensões em arquivos `.mh`. O runtime oferece etapas sequenciais e paralelas, controle de fluxo, loops, entradas tipadas, checkpoints, pausa/retomada e projeção explícita de resultados. CLI, MCP e A2A compartilham o serviço de execução, reduzindo divergências entre superfícies.

O digest vincula o checkpoint à definição relevante; versões de estado e rejeição de valores não serializáveis evitam retomadas silenciosamente incompatíveis. Isso não constitui execução exatamente uma vez: efeitos externos podem repetir na reentrada de uma etapa.

### Proteção de dados e autoria

R1 aplica `runtime.RedactVars` no sucesso do CLI JSON. R2 preserva o valor exato explicitamente resolvido, incluindo `x9!`, números, `true` e espaços. O registro inferido de ambientes mantém heurísticas para evitar falsos positivos. Checkpoints usam referências reidratáveis; referências distintas com conteúdo igual geram erro explícito no resume. O registro continua global ao processo, podendo mascarar texto de outras runs quando a credencial é curta.

As regressões de redação aninhada, saída MCP e identidade da invocação no cache seguem incluídas na suíte aprovada. O cache discrimina comando, argumentos, endpoint, modelo, temperatura e schema. Permanecem pendentes namespace por projeto/principal e política de persistência de conteúdo sensível.

R7 limita o backoff antes da primeira espera; os testes cobrem atraso menor, igual e superior ao teto. R8 extrai `coerceInputs` para uso de Run e Inspect; `count=abc` é recusado para `number`, enquanto valores válidos são convertidos. Dry-run continua uma inspeção estática: não comprova que serviços ou agentes funcionarão em execução.

### Servidores e extensões

MCP oferece execução síncrona e assíncrona, acompanhamento, retomada e cancelamento. A2A possui guardas de bearer, Origin e tamanho de corpo; a identidade ainda precisa ser propagada e autorizada por tarefa para uso multiusuário.

Stores sem CAS exigem reconhecimento explícito de réplica única. A aquisição com erro deixou de executar silenciosamente; `peek` distingue indisponibilidade de ausência. A liberação usa CAS para gravar tombstone, melhorando o caso de takeover entre réplicas diferentes.

PostgreSQL e Redis são opções de store com CAS. S3 passou a extensão de blobs, não backend de coordenação. Nesta revisão, essas integrações não foram reexecutadas contra serviços reais; os smoke tests anteriores são histórico, não evidência nova de exclusão distribuída.

---

## Achados que reabrem critérios

### R3 | P1 | Retomada mantém log selado

`execRun` sela o buffer em estados terminais diferentes de pausa, incluindo falha/cancelamento que podem ser retomados. `runResume` reutiliza `rn.logs`; não há reabertura, e `Write` aceita novos bytes mesmo com `sealed=true`. Assim, as novas leituras deixam de reter a cauda de segurança.

**Reprodução:** `Seal`, escrita do prefixo de uma credencial sintética e polling antes da escrita restante. `TestReviewR3ResumedSealedLog` confirmou a publicação do prefixo. Não se afirma que essa sonda entregou o valor completo; a exposição de fragmentos já viola a fronteira pretendida.

**Correção:** definir o ciclo de vida do sanitizador por tentativa, inclusive falha, cancelamento e resume. Preferir fluxo sanitizado antes da publicação, com cursores estáveis. Testar a sequência completa via MCP, além do buffer isolado. A retenção de toda a cauda durante uma pausa também pode esconder mensagens úteis ao operador.

### R4 | P1 | Latência não integra a validade do lease

`acquire` calcula `expires` antes da chamada ao store e aceita o sucesso sem verificar a validade restante. Um store que responde depois do TTL produz `held=true` com registro já expirado. A sonda `TestReviewR4DelayedAcquire` avançou o relógio controlado durante `PutIfAbsent` e confirmou esse estado, sem espera real.

Além disso, `heartbeatLock` chama `renew(context.Background())` sem deadline; a decisão de cancelar só ocorre quando a chamada retorna. `lastRenew` usa o instante da resposta, não a expiração efetiva persistida. Esses pontos foram constatados por leitura do código; não houve ensaio de heartbeat travado contra serviço real.

**Correção:** propagar expiração efetiva, verificar validade antes de iniciar passos e aplicar orçamento às operações do store. Um watchdog deve cancelar independentemente da conclusão de uma renovação lenta. Testar atrasos, timeouts, perda de resposta, shutdown e renovação próxima da expiração; depois repetir com duas réplicas e store real.

### R5 | P1 | Token não pertence à tentativa que libera

`tokens[runID]` é compartilhado por todas as tentativas no mesmo `runLock`. O defer de cada execução chama `release(ctx, runID)` e busca o token atual, sem carregar o token da aquisição original.

**Reprodução:** A adquire, expira e readquire o mesmo runID; o token muda. A liberação atrasada da tentativa anterior encontra o token novo e encerra a nova aquisição; C consegue adquirir. `TestReviewR5SameReplicaOldRelease` confirmou a sequência na API do lock. O caminho de resume permite takeover local após expiração; não foi simulado o escalonamento completo de duas execuções HTTP nesta sonda.

**Correção:** devolver um handle imutável de aquisição, usado por renew/release e capturado por tentativa. Limpeza local também deve comparar tokens. O CAS para tombstone pode ser mantido. Isso não substitui fencing de checkpoints nem elimina efeitos externos de um worker antigo.

---

## Pausa, contratos e limites restantes

### R6 | P2 | Motivo de pausa ausente no CLI JSON

O erro original de avaliar `json.parse("")` durante pausa foi corrigido. `Result.PauseReason` existe no serviço, mas `runJSON` não tem campo correspondente. `TestReviewR6PauseReasonJSON` confirmou `paused=true` sem motivo estruturado. A mensagem eventualmente presente em logs não substitui um campo estável para consumidores.

**Correção:** serializar o motivo com redação recursiva, preservando sessão e estado. Cobrir motivo textual e objeto, CLI/MCP e runners normal/loop. Os novos testes permanentes de pausa exercitam o caso normal; não bastam como evidência de toda a matriz de loop.

### R6 | P1 para dados internos | Pausa contorna output explícito

Ao pausar, `execsvc` retorna `publicVars(res.FinalVars)` mesmo com `output:`. A sonda `TestReviewR6PauseOutputBoundary` recebeu `internal_data`, ausente do output declarado. Esta é a política implementada e documentada nas alterações atuais, não um desvio oculto da nova documentação. Contudo, reduz a garantia anterior de que somente a projeção é pública e merece decisão explícita de contrato.

Redação de credenciais registradas não protege automaticamente dados de clientes, documentos ou outros valores internos. Recomenda-se resposta de suspensão mínima, ou projeção parcial declarada separadamente, mantendo o estado completo no checkpoint autorizado. Se a exposição integral for mantida, não apresentar `output:` como barreira de dados em todos os estados.

### Pendências fora de R1-R8

- Isolamento de cache por identidade/projeto e política para conteúdo sensível.
- Autorização por dono da tarefa em A2A; guardas de transporte não equivalem a isolamento multiusuário.
- Fencing de escritas, testes distribuídos reais, fila e limites de recursos.
- Sandbox efetiva de extensões e agentes subprocesso; avisar permissões não impostas não as implementa.
- Idempotência de efeitos e validação opt-in de respostas por schema, ainda dependentes de design.
- Evals de qualidade, latência e custo; métricas de operação e quotas para adoção ampliada.
- Caminhos e comandos obsoletos em `AGENTS.md`, acompanhados em M-18.

---

## Arquitetura composta e responsabilidades

**Premissa informada pelo autor:** o runtime não será exposto diretamente. Haverá gateways, balanceadores, filas duráveis em banco e workers; validadores de inconsistência são uma possibilidade. Esta é a arquitetura-alvo considerada no parecer, não uma declaração de que todos esses componentes foram integrados e testados nesta revisão.

Fluxo proposto: cliente -> gateway/balanceador -> serviço de admissão -> fila durável -> worker -> runtime -> resultado/checkpoint. O gateway também pode servir consultas e aprovações. Validadores e reconciliação consomem estado/eventos sem precisar fazer parte do interpretador.

| Responsabilidade | Local recomendado e contrato mínimo |
|---|---|
| Identidade e acesso | Gateway/controlador: autenticar, autorizar run/tarefa/artefato e propagar identidade confiável. Backend privado e sem rota alternativa que contorne a autorização. |
| Admissão e distribuição | Serviço + fila: aceitar trabalho duravelmente, aplicar prioridades e backpressure. Balanceador distribui tráfego; não é o responsável por posse de execução. |
| Posse e recuperação | Worker/controlador + banco: reivindicar uma tentativa, decidir retry/resume e registrar o resultado antes de confirmar consumo. Definir o que ocorre se o worker cair nessa transição. |
| Semântica de workflow | Core: interpretar, executar etapas, pausar/cancelar, preservar estado compatível e retornar resultado/erro de forma consistente. |
| Persistência e serviços | Extensões/adaptadores: store, cache, blobs, ferramentas e integrações. O core fornece os pontos de integração necessários; cada backend implementa sua política. |
| Validação e auditoria | Validador externo ou extensão: checar schema/regras, reconciliar estado e sinalizar divergências. Aprovação/compensação depende do efeito de negócio. |

### Consequência para os achados anteriores

R4/R5 não impõem reconstruir um orquestrador distribuído no core. Há duas opções legítimas: usar o coordenador interno e corrigir seu contrato, ou delegar a posse das runs ao worker/controlador e evitar o caminho interno afetado. A opção deve ser explícita: sobrepor duas políticas de retry/lease sem definir autoridade cria ambiguidade de recuperação.

Uma frota pode executar muitas runs simultaneamente. É a sobreposição de tentativas da mesma run que exige tratamento. Uma fila durável preserva trabalho aceito, mas pode entregar novamente após falha; o controlador deve definir como identifica a tentativa e como trata resultado gravado antes de uma confirmação perdida. A flag atual de réplica única não é, sozinha, certificação de exclusão por uma fila externa.

Autorização por tarefa A2A também pode ficar fora do core, se o controlador possuir o vínculo de dono e proteger todos os acessos, inclusive consulta/cancelamento. Autenticar um bearer no gateway, isoladamente, não fornece essa autorização. Políticas de saída podem ser aplicadas no controlador antes de retornar a resposta; logs e artefatos internos continuam sujeitos ao acesso definido pela plataforma.

Validadores reduzem inconsistências detectáveis e ajudam na recuperação. Um validador posterior não desfaz automaticamente um pagamento ou envio duplicado; nesses fluxos, usar a chave de idempotência do sistema de destino ou aprovação antes do efeito. Para um fluxo que só gera uma proposta revisável, reexecução pode ser aceitável e não requer a mesma infraestrutura.

### Extensões possíveis, conforme demanda do piloto

**Já presentes:** store PostgreSQL/Redis com CAS e blob S3; o mecanismo de extensões registra tipos de declaração e anuncia capacidades. **Propostas, não entregas verificadas:** adaptador de fila em banco; validador de schema/regras; política de cache por projeto; emissão de eventos de auditoria. Dispatch e reconciliação podem ser serviços externos, sem nova sintaxe MHL.

Fencing pode residir no store, mas o chamador precisa fornecer uma identidade de tentativa que o store consiga conferir; não basta instalar um backend sem esse contrato. Da mesma forma, uma extensão de fila chamada pelo workflow não substitui automaticamente a fila que agenda o próprio workflow. O core só precisa crescer quando faltar uma interface estável indispensável à integração escolhida.

---

## Evidência e próximos critérios de saída

### Execuções desta revisão

| Verificação | Evidência e alcance |
|---|---|
| Suíte permanente | `go test -count=1 ./...`: aprovada, sem as sondas adicionais. Inclui regressões originais e novos testes R1-R8. |
| Análise e build | `go vet ./...`: exit 0. `make functional-test`: build CGO desabilitado e exemplos aprovados. |
| Exemplos | Syntax: 278 aprovadas. Features: 111 aprovadas, 8 incompletas. Integração incompleta não foi contada como aprovação. |
| Concorrência | Race aprovado em mcpserver, execsvc, interpreter, runtime, auth, traffic e a2aserver. Não comprova protocolo distribuído. |
| Sondas adicionais | Três falhas em lock/log e duas no CLI de pausa. Valores sintéticos, relógio controlado; código e logs preservados. |
| Serviços reais | Nenhum novo teste distribuído, PostgreSQL, Redis ou MinIO nesta revisão. Smoke histórico não encerra R4/R5. |

### Fontes locais e reprodução

Diretório de evidências: `output/relatorios/evidencias-reavaliacao-runtime-2026-09-06/`. Contém `snapshot.json`, `go-test.log`, `vet.log`, `functional.log`, `race.log`, `probes.log`, `pause-probes.log` e as duas fontes das sondas. O `LEIA-ME.md` descreve como inseri-las temporariamente nos pacotes apropriados.

Código principal: `internal/mcpserver/{runlog,runlock,runs}.go`; `internal/features/auth/resolver.go`; `internal/execsvc/{execsvc,inputs,inspect}.go`; `internal/cli/{run_format,run_dryrun}.go`; `internal/features/traffic/retry.go`. Caminhos relativos ao módulo `src/mhl-runtime`.

### Priorização por cenário real de adoção

| Cenário | Decisão e risco relevante |
|---|---|
| Diretoria: plataforma controlada, dados sintéticos, fluxo conhecido | Apresentável. Ensaiar o caminho de admissão/execução escolhido e congelar sua versão; identificar componentes simulados. |
| Piloto interno: equipe confiável, documentos reais | Antes de incluir dados confidenciais, verificar o que sai na pausa e no cache. R6 é exposição efetiva de variáveis, não apenas hipótese. Não usar retomada após falha/cancelamento com credenciais reais em logs sem tratar R3. |
| Workers concorrentes e recuperação | Se usar o lease interno, R4/R5 devem ser tratados. Se a posse for externa, validar esse contrato e evitar o caminho afetado. Não há exigência genérica de uma única réplica para runs distintas. |
| Serviço compartilhado por usuários independentes | Autorização por tarefa, cache por identidade e isolamento podem ser implementados na plataforma/adaptadores. Verificar a proteção ponta a ponta, sem impor que tudo pertença ao core. |

**Agora:** concluir a apresentação; não acrescentar capacidades. **Após patrocínio:** selecionar um piloto e resolver apenas as pendências acionadas por seus dados, usuários e operação. **Na expansão:** atribuir coordenação, fencing, quotas e isolamento ao componente adequado. Avaliar garantias do conjunto em cenários reais de queda/redelivery, sem transformar toda capacidade da plataforma em requisito do core.

O plano distingue prontidão do MVP de completude do contrato geral. R1/R2/R7/R8 permanecem concluídos no escopo verificado; R3/R4/R5/R6 ficam parciais no backlog técnico, sem bloquear automaticamente a apresentação. As versões anteriores dos relatórios foram arquivadas para evitar que evidência histórica seja apresentada como estado atual.
