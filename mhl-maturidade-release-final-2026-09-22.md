# MHL — avaliação de maturidade para release estável

**Data:** 22/09/2026  
**Base avaliada:** commit `0e64810`, tag `v1.4.0-beta.24`, branch `main` sincronizada com `origin/main`  
**Escopo:** runtime Go, CLI, servidores MCP/A2A, extensões externas, extensão VS Code, documentação, instaladores, CI e processo de release.

## Parecer executivo

**Decisão recomendada: NO-GO para publicar `v1.4.0` estável agora.**

O projeto está em uma **beta madura e demonstrável**, com boa arquitetura, grande volume de testes e distribuição automatizada. Entretanto, foram confirmados problemas que atingem segurança, concorrência e ciclo de vida de extensões. Também faltam gates de CI capazes de impedir que esses problemas cheguem a uma release.

O MHL pode continuar sendo usado em beta controlada e em demonstrações com dados não sensíveis. A promoção para estável deve ocorrer somente depois de corrigir os bloqueadores deste relatório, executar novamente toda a matriz e congelar um release candidate por um período curto de estabilização.

### Resultado resumido

| Dimensão | Avaliação | Leitura |
|---|---:|---|
| Arquitetura e modularidade | 8/10 | Separação clara entre linguagem, engine, features, servidores e CLI. |
| Funcionalidade | 8/10 | Superfície ampla: linguagem, checkpoints, concorrência, MCP/A2A, LSP e extensões. |
| Testes funcionais | 8/10 | Suíte Go extensa e 520 asserções `.mh` aprovadas no caminho local. |
| Concorrência e confiabilidade | 4/10 | Corrida de dados reproduzível e falha no encerramento gracioso de extensões. |
| Segurança | 3/10 | Artefato publicado com 19 vulnerabilidades alcançáveis da biblioteca padrão; vazamento de fragmento de segredo após resume. |
| CI e release engineering | 5/10 | Build/release automatizados, mas os gates não incluem race, vulnerabilidades nem E2E e cobrem poucos caminhos. |
| Documentação e governança | 4/10 | Changelog e política de segurança não representam a versão atual; links públicos e locais quebrados. |
| **Maturidade global** | **5,5/10** | **Beta madura; ainda não release estável.** |

## Pontos fortes

### 1. Arquitetura legível e extensível

O runtime apresenta limites de pacote claros: `internal/lang`, `internal/engine`, `internal/features`, `internal/extension`, `internal/mcpserver`, `internal/a2aserver`, `internal/lsp` e `internal/cli`. O próprio README técnico documenta a direção das dependências. Isso reduz acoplamento e favorece testes focados.

O mecanismo de extensões tem boas defesas já implementadas:

- lock explícito e pin de SHA-256 do executável e do pacote;
- rejeição de alteração do conteúdo instalado;
- ambiente do subprocesso sem herdar variáveis ambientes;
- resolução de segredos permitida por allow-list;
- limite de 8 MiB por frame do protocolo;
- redaction de segredos em erros e stderr;
- proteção contra traversal na extração de arquivos e recusa de links/dispositivos em tar.

### 2. Cobertura funcional relevante

A base contém 355 arquivos Go, 174 arquivos de teste e 1.426 funções `Test*`. A suíte normal passou integralmente. Os exemplos executáveis também formam uma boa camada funcional:

| Suíte | Resultado |
|---|---|
| `go test ./...` | aprovado |
| `go vet ./...` | aprovado |
| `make build` | aprovado |
| `sample/syntax` | 322 aprovadas, 0 falhas |
| `sample/features` | 198 aprovadas, 0 falhas, 8 incompletas por dependerem de serviços reais |
| Playground web | 7 aprovados, 2 pulados |
| Compilação VS Code | aprovada |
| Cross-build | Linux amd64/arm64, macOS arm64 e Windows amd64/arm64 aprovados |

A cobertura Go total medida foi **60,3%**. Pacotes críticos apresentam bons números em vários pontos: `mcpserver` 82,6%, parser 78,2%, lint 82,5%, LSP 78,3%, runtime 75,4%, extensões externas 79,9% e native ops 83,3%.

### 3. Operação e distribuição já bem encaminhadas

Há releases automatizadas com GoReleaser, arquivos SHA-256, builds sem CGO para cinco plataformas suportadas e empacotamento da extensão VS Code. O artefato público `v1.4.0-beta.24` foi conferido e seu SHA-256 coincide com o publicado.

Os servidores já contemplam autenticação bearer, limite de corpo, proteção de Origin, principal confiável apenas junto com token, ownership de runs, health/readiness, métricas, cancelamento, persistência, retomada e drain.

### 4. Bons sinais de fail-closed

O código tende a recusar execução quando não consegue confirmar invariantes importantes: lock de extensão inválido, hash divergente, store sem CAS em modo multi-réplica, falha de leitura de lease e credencial ausente. Essa postura é apropriada para um runtime de automação com acesso a ferramentas e subprocessos.

## Bloqueadores para a versão estável

### B1 — P0 — artefatos publicados com toolchain vulnerável

O `go.mod` fixa `go 1.26.0`, e o workflow usa esse arquivo para instalar a toolchain. O binário Linux publicado em `v1.4.0-beta.24` foi inspecionado com `go version -m` e confirma `go1.26.0`.

O scanner oficial `govulncheck` encontrou **19 vulnerabilidades alcançáveis** da biblioteca padrão. Elas afetam caminhos usados pelo MHL, incluindo servidor HTTP, TLS, certificados, URLs, HTTP/2 e extração tar. Todas estão corrigidas até Go `1.26.6`.

IDs reportados: `GO-2026-6218`, `6090`, `6089`, `5972`, `5856`, `5039`, `5037`, `5026`, `4971`, `4947`, `4946`, `4918`, `4870`, `4869`, `4866`, `4602`, `4601`, `4600` e `4599`.

**Critério de saída:** atualizar a toolchain para pelo menos Go `1.26.6`, reconstruir todos os artefatos e obter `govulncheck ./...` sem vulnerabilidades alcançáveis. Adicionar esse scanner ao CI e ao workflow de release.

### B2 — P0 — corrida de dados em pipelines paralelos

`go test -race ./...` falhou. A reprodução focada também falhou consistentemente:

```text
go test -race ./internal/execsvc \
  -run TestPipelineStepHooksFireForEachParallelBranch -count=1
```

Etapas paralelas escrevem concorrentemente no mesmo `bytes.Buffer` por `interpreter.writeLog` (`eval.go:764`). O detector reportou writes simultâneos, e o teste perdeu linhas `step_start`/`step_end`. Portanto não é apenas um falso positivo: há corrupção observável da saída.

**Impacto:** logs incompletos/corrompidos, comportamento indefinido em execuções paralelas e risco de outras estruturas compartilhadas seguirem o mesmo padrão.

**Critério de saída:** serializar o writer compartilhado ou usar buffers por branch com merge definido; adicionar regressão e tornar `go test -race ./...` gate obrigatório de CI.

### B3 — P0 — redaction deixa vazar fragmento de segredo após resume

Uma run `failed` ou `canceled` chama `rn.logs.Seal()`. Ao executar `run/resume`, o mesmo `ringLog` é reutilizado e não volta ao estado não selado. Como `sealed=true`, uma leitura de `run/logs` publica imediatamente um prefixo ainda incompleto de um segredo antes que a continuação chegue e possa ser reconhecida pela redaction.

Uma sonda adicionada apenas em cópia temporária da árvore confirmou que, após `Seal`, escrever os primeiros 12 bytes de um segredo registrado e chamar `read` devolve esses 12 bytes em claro. O código relevante não mudou desde a avaliação anterior de 06/09.

**Impacto:** exposição parcial de credenciais por polling de logs depois de retomar uma execução interrompida.

**Critério de saída:** modelar o ciclo de vida por tentativa (`Seal`/`Reopen` ou novo buffer/redactor), testar `failed -> resume`, `canceled -> resume` e `paused -> resume`, incluindo segredo dividido em todos os limites de escrita e leitura.

### B4 — P1 — encerramento “gracioso” de extensões mata o processo cedo demais

O E2E `CENARIO-005` falhou com o binário atual: houve 1 `init` e 7 `call`, mas nenhum evento `shutdown`.

A causa aparece no código: `External.Close` lança `p.close()` numa goroutine e espera apenas essa função retornar. `p.close()` envia a notificação e fecha stdin, mas não espera o `readLoop`/processo encerrar. A goroutine termina imediatamente e `External.Close` chama `Kill` se `p.isClosed()` ainda for falso. O comentário afirma que `p.close()` espera o processo, mas a implementação não espera.

**Impacto:** perda de flush, commits ou limpeza final de uma extensão; maior risco para stores e adaptadores que mantêm buffers/estado.

**Critério de saída:** esperar a conclusão real do processo até `shutdownGrace`, matar somente após timeout e manter um E2E determinístico que exija o ack/efeito de shutdown.

### B5 — P1 — os gates de release não detectam os bloqueadores

O CI atual executa vet, build, testes e exemplos somente quando mudam arquivos sob `src/mhl-runtime/**`. Ele não executa race, `govulncheck`, E2E, auditoria npm nem testes do playground. Mudanças isoladas em `sample/`, `vscode-mhl/`, instaladores, `tests_e2e/` e documentação podem chegar sem validação.

O workflow de release executa apenas `make test` antes do empacotamento Go, além da compilação da extensão. Não repete vet, functional, race, vuln ou E2E.

**Critério de saída:** criar um único gate de release reproduzível e obrigatório, acionado por todos os caminhos relevantes. A tag estável só deve ser publicada a partir de um commit que já passou esse gate.

## Pontos de melhoria importantes

### P1 condicional — aquisição de lease pode retornar já expirada

Na coordenação multi-réplica, `runLock.acquire` calcula `Expires` antes de chamar o store. Se `PutIfAbsent` demorar mais que o TTL e finalmente retornar sucesso, o código aceita o lease sem conferir se ainda está fresco. Nesse intervalo, outra réplica pode enxergá-lo como expirado e assumir a mesma run. Os handles imutáveis e o fencing de checkpoints corrigem classes importantes de conflito, mas não impedem efeitos externos duplicados antes da próxima verificação.

Esse item bloqueia uma promessa de execução multi-réplica coordenada. Alternativamente, `v1.4.0` pode declarar suporte somente a `--single-replica` e deixar o perfil distribuído como experimental.

**Recomendação:** calcular/confirmar validade após a resposta do store, impor deadline inferior ao TTL e testar aquisição/renovação com latência, timeout, resposta perdida e takeover.

### P1 — regressivos E2E estão fora de sincronia

O E2E HTTP, executado numa cópia temporária com o binário atual, obteve **17 PASS, 1 FAIL e 1 SKIP**. A falha exige `queuePosition`, embora a documentação atual diga que esse campo não existe. É falha da especificação/teste, não evidência de defeito do runtime.

O E2E de extensões obteve **8 PASS, 2 FAIL e 11 SKIP**:

- `005`: falha real de shutdown descrita em B4;
- `010`: teste desatualizado, pois não passa `--single-replica` para um store sem CAS;
- `011` a `021`: pulados porque ainda procuram `src/mhl-extensions/*`, removido quando as extensões foram movidas para `mh-language/mhl-packages`.

Além disso, há 22 binários Mach-O/arm64 versionados nos diretórios E2E, totalizando 182,2 MiB. Eles reportam versões antigas (`v1.1.0-alpha` ou `v1.2.2-beta.1-dirty`) e podem fazer a suíte validar uma versão diferente do código atual.

**Recomendação:** nunca versionar/copiar binários de teste por cenário. Construir uma vez no job, referenciar o artefato pelo caminho e mover os E2E de integrações para o repositório correto ou para workflow coordenado.

### P1 — canal padrão do instalador pode selecionar prerelease

`install.sh` e `install.ps1` consultam `/releases?per_page=100`, filtram apenas tags iniciadas por `v` e escolhem a primeira. A API também retorna prereleases. Após `v1.4.0` estável, uma futura `v1.5.0-beta.1` mais recente poderá ser instalada por padrão, embora os comentários do release declarem que o canal estável não deve receber beta.

**Recomendação:** resolver explicitamente a release runtime não-prerelease ou oferecer canais `stable` e `beta` separados.

### P1 — limite de descompressão é por arquivo, não pelo pacote total

O download de extensão é limitado a 256 MiB, mas a extração tar/zip limita cada arquivo individualmente e não contabiliza o total descomprimido. Um arquivo comprimido hostil pode expandir muitos arquivos grandes e exaurir disco. No ZIP, o código também não rejeita explicitamente a cópia que ultrapassa `maxArchiveBytes`; ele apenas usa `LimitReader`.

**Recomendação:** aplicar orçamento cumulativo de bytes e quantidade de entradas, falhar ao ler `limit+1`, limitar razão de compressão e adicionar testes de zip/tar bomb.

### P1 — documentação de segurança e release está desatualizada

- `SECURITY.md` é um template e declara suporte fictício a `5.1.x`/`4.0.x`, sem canal de reporte.
- `CHANGELOG.md` termina em `1.2.0-alpha — Unreleased`, enquanto o código está em `1.4.0-beta.24`.
- o README declara a documentação como “work in progress”.
- o link público `.../reference.html` usado no README retorna HTTP 404; o arquivo publicado é `Docs-Reference.dc.html`.
- foram encontrados 14 links locais quebrados em Markdown versionado.

Uma release estável precisa declarar claramente compatibilidade, suporte, migração e limitações conhecidas.

### P2 — cobertura desigual

A cobertura total de 60,3% é aceitável para beta, mas o interpretador ficou em **19,9% por statements** no relatório agregado, apesar dos testes black-box da CLI. O número isolado não é bloqueador, porém torna mais difícil demonstrar que os ramos de concorrência, erro e recuperação estão cobertos.

Definir metas por risco é melhor que uma meta global: concorrência, resume/checkpoint, redaction, extensões, autenticação e extração de pacotes devem ter regressões explícitas.

### P2 — permissões de extensões são parcialmente consultivas

`permissions.secrets` é imposto, mas `network`, `filesystem` e `subprocess` apenas geram aviso. A extensão roda com os privilégios do processo host. Isso pode ser um contrato válido se estiver explícito, mas não deve ser apresentado como sandbox.

### P2 — contrato de pausa ainda é ambíguo

`execsvc.Result` carrega `PauseReason`, mas `mhl run --format json` expõe apenas `paused=true`; não há campo estruturado para o motivo. Além disso, durante a pausa o serviço retorna todas as variáveis públicas parciais e não aplica a projeção `output:`. Isso evita avaliar uma projeção que talvez ainda dependa de etapas futuras, mas significa que `output:` não funciona como fronteira de dados nesse estado.

**Recomendação:** adicionar `pause_reason` ao JSON e decidir/documentar se a pausa deve retornar estado completo, uma projeção parcial própria ou somente metadados. Se `output:` for apresentado como mecanismo de minimização de dados, o comportamento atual precisa mudar.

### P2 — supply chain pode ser fortalecida

As releases têm checksums, mas não há assinatura, SBOM ou proveniência verificável. Actions são referenciadas por tags mutáveis (`@v4`, `@v5`, `@v6`). Para uma release estável, recomenda-se pin por commit, geração de SBOM e assinatura/proveniência dos artefatos.

### P2 — dependência de desenvolvimento da extensão VS Code

`npm audit --omit=dev` retornou zero vulnerabilidades. O audit completo encontrou 1 vulnerabilidade moderada em `esbuild <= 0.24.2`, usada no desenvolvimento. Não afeta diretamente o bundle de produção, mas deve ser atualizada antes de estabilizar a cadeia de build.

### P2 — velocidade de mudança ainda é de beta

Entre 06/09 e 22/09 foram publicadas 23 betas (`beta.2` a `beta.24`). Desde 01/09, o histórico contém 51 commits `feat` e 17 `fix`, além de merges. A cadência mostra evolução intensa, não congelamento de contrato. Quase todo o histórico é de um único mantenedor, elevando risco de continuidade e revisão independente.

## Lacunas não validadas nesta revisão

Não foram comprovados nesta rodada:

- Kubernetes real (`CENARIO-K8S-001` foi pulado);
- PostgreSQL, Redis, S3/MinIO e SQL externos, pois os testes ainda apontam para o caminho antigo;
- chamadas reais a provedores LLM, MCP e A2A marcadas como `incomplete`;
- carga/soak da versão `beta.24`;
- comportamento funcional em runners Windows e macOS; houve cross-build, mas os testes rodaram em macOS e o CI atual roda em Ubuntu;
- compatibilidade formal de arquivos/checkpoints entre versões beta;
- estado de issues e suporte operacional fora do repositório.

Essas ausências não significam defeito, mas impedem alegar prontidão geral de produção.

## Plano mínimo de promoção

### Fase 1 — eliminar bloqueadores

1. Atualizar para Go `1.26.6+`, reconstruir e rodar `govulncheck`.
2. Corrigir a corrida no writer de logs/hooks paralelos.
3. Corrigir o ciclo Seal/Resume do `ringLog` e adicionar matriz de regressão de segredo fragmentado.
4. Corrigir o shutdown gracioso de extensões.
5. Corrigir os E2E 008/010 e reconectar os cenários de integrações ao repositório `mhl-packages`.

### Fase 2 — transformar evidência em gate

O gate obrigatório do release candidate deve executar:

```text
go vet ./...
go test -count=1 ./...
go test -race -count=1 ./...
govulncheck ./...
make functional-test
E2E MCP host completo
E2E extensões completo, sem FAIL e com SKIPs explicitamente aceitos
npm ci && npm run compile && npm audit --omit=dev
node --test docs/site/playground.test.cjs
cross-build da matriz publicada
smoke dos instaladores em Linux, macOS e Windows
```

### Fase 3 — preparar o RC

1. Atualizar `CHANGELOG.md`, `SECURITY.md`, README e links.
2. Declarar plataformas, política de compatibilidade e limitações conhecidas.
3. Corrigir o canal stable/beta do instalador.
4. Publicar `v1.4.0-rc.1`, sem novas funcionalidades.
5. Manter 7–14 dias de soak com carga representativa e pelo menos um ensaio de restart/resume, concorrência, extensão real e instalação por plataforma.
6. Promover exatamente o commit/artefato aprovado para `v1.4.0`; qualquer correção reinicia o RC.

## Checklist objetivo de GO

A decisão muda para **GO** somente quando todos os itens abaixo forem verdadeiros:

- [ ] zero vulnerabilidades alcançáveis no `govulncheck` usando a toolchain de release;
- [ ] `go test -race ./...` verde;
- [ ] regressão de segredo em `failed/canceled -> resume` verde;
- [ ] shutdown de extensão confirmado sem kill prematuro;
- [ ] E2E HTTP e extensões sem `FAIL` e executando o binário do commit corrente;
- [ ] CI/release executando todos os gates obrigatórios;
- [ ] changelog, política de segurança, documentação e instaladores coerentes;
- [ ] matriz de artefatos instalada e testada;
- [ ] RC congelado e soak concluído sem P0/P1 aberto.

## Conclusão

O MHL já tem substância de produto: arquitetura boa, linguagem ampla, testes numerosos e distribuição funcional. O problema não é falta de capacidades; é que a estabilização ainda não acompanhou a velocidade de evolução.

**Recomendação final:** manter `v1.4.0-beta.24` como beta, abrir um ciclo exclusivamente de hardening e não remover o sufixo de prerelease até que os quatro defeitos confirmados e os gates de release estejam resolvidos. Com foco e sem novas features, o caminho para um RC é curto e mensurável.

## Referências de evidência

- `src/mhl-runtime/go.mod`
- `.github/workflows/ci.yml`
- `.github/workflows/release.yml`
- `.goreleaser.yaml`
- `src/mhl-runtime/internal/engine/interpreter/eval.go`
- `src/mhl-runtime/internal/execsvc/pipeline_hooks_test.go`
- `src/mhl-runtime/internal/mcpserver/runlog.go`
- `src/mhl-runtime/internal/mcpserver/runs.go`
- `src/mhl-runtime/internal/extension/external/external.go`
- `src/mhl-runtime/internal/extension/external/process.go`
- `src/mhl-runtime/internal/cli/extension_archive.go`
- `tests_e2e/cloud/tests_mcp_pods/`
- `tests_e2e/extensions/tests_ext/`
- scanner oficial: https://go.dev/doc/security/vuln/
- release pública: https://github.com/mh-language/mhl-core-runtime/releases/tag/v1.4.0-beta.24
