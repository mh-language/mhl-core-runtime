# MHL — reavaliação de maturidade para release estável

**Data:** 23/09/2026  
**Base avaliada:** commit `c11055e7b0b38b75f1bff9ab74c55cff2d5ac74f`, tag `v1.4.0-rc.2`, branch `main` sincronizada com `origin/main`  
**Comparação:** avaliação anterior de 22/09/2026 sobre `v1.4.0-beta.24` (`0e64810`)  
**Escopo:** runtime Go, CLI, servidores MCP/A2A, extensões externas, extensão VS Code, documentação, instaladores, CI, artefatos e processo de release.

## Atualização pós-implementação — árvore de trabalho de 23/09/2026

Os achados executáveis deste relatório foram corrigidos na árvore de trabalho
após a auditoria do commit acima:

- `pause_reason` e `break_reason` agora passam por redação recursiva em texto,
  JSON e status MCP, incluindo arrays, objetos e chaves dinâmicas; chaves de
  segredo persistidas em checkpoint são reidratáveis sem gravar o valor;
- `mhl run --format json` inclui `break_reason`;
- `mhl lint` aceita `json.remove(key)` e valida aridade e tipo da chave;
- fixtures, adapters de referência e E2E dos pacotes de extensão foram
  retirados deste repositório e a CI não os executa mais; testes do mecanismo
  genérico do core (parser, registry, protocolo, instalação e dispatch) foram
  preservados;
- SECURITY, CHANGELOG, README, site e links de documentação foram alinhados ao
  status de release candidate e ao repositório `mhl-packages`;
- o gate ganhou smoke nativo de build, checksum, instalação, execução e
  desinstalação em Linux amd64, macOS arm64 e Windows amd64.

Validação local posterior: `go vet`, `go test -count=1 ./...`, suíte completa
com `-race`, `govulncheck`, 520 asserções funcionais, cross-build, playground,
VS Code compile/audit, smoke do instalador macOS arm64 e MCP E2E (18 PASS,
0 FAIL) aprovados. As regressões de redação também passaram sob race detector.

**Parecer atualizado sobre a árvore de trabalho:** tecnicamente pronta para
publicar uma nova RC congelada. A promoção a estável continua condicionada ao
gate remoto nas três plataformas e ao período de soak de 7–14 dias; isso é uma
evidência operacional, não outra mudança de implementação.

## Parecer executivo

**Decisão recomendada: NO-GO para promover a RC atual diretamente a `v1.4.0` estável.**

O estado do projeto melhorou de forma substancial. Todos os P0 da avaliação anterior foram corrigidos com testes de regressão; a corrida de dados desapareceu, a toolchain publicada está segura, o vazamento no `resume` foi fechado, o shutdown de extensões agora é realmente gracioso e existe um gate único de CI/release. O MHL saiu de **beta madura** para uma **RC tecnicamente forte**.

Contudo, esta rodada encontrou **um novo bloqueador de segurança P0** nas fronteiras de saída da CLI: motivos estruturados de `pause(...)` e motivos textuais de `break` podem expor credenciais registradas. Além disso, a documentação ainda se apresenta como beta/alpha, 11 cenários de extensões oficiais são aceitos como `SKIP`, e `rc.1`/`rc.2` foram criadas com apenas 44 minutos de intervalo, sem evidência do soak recomendado.

O caminho para a versão final agora é curto: corrigir a redação recursiva dos motivos, fechar as pendências de contrato/documentação, publicar uma nova RC e estabilizá-la sem novas funcionalidades.

### Resultado resumido

| Dimensão | Anterior | Atual | Leitura atual |
|---|---:|---:|---|
| Arquitetura e modularidade | 8/10 | 8,5/10 | Limites claros e correções localizadas, sem atalhos arquiteturais. |
| Funcionalidade | 8/10 | 8,5/10 | Superfície ampla e 520 asserções funcionais aprovadas. |
| Testes e evidência | 8/10 | 9/10 | Suítes normal, race, funcional, E2E, playground e npm verdes. |
| Concorrência e confiabilidade | 4/10 | 8,5/10 | Race e shutdown corrigidos; lease lento agora falha fechado. |
| Segurança | 3/10 | 6,5/10 | Toolchain e `resume` corrigidos, mas há vazamento confirmado em motivos de controle. |
| CI e release engineering | 5/10 | 8,5/10 | Gate reutilizável e release pública verde; ainda há `SKIP` silencioso e pouco smoke multiplataforma. |
| Documentação e governança | 4/10 | 6/10 | Política e changelog melhoraram, mas ainda descrevem beta/alpha e há relatórios históricos contraditórios. |
| **Maturidade global** | **5,5/10** | **8/10** | **RC madura; ainda não estável por um P0 confirmado.** |

## Evidência executada

### Runtime e qualidade

| Verificação | Resultado |
|---|---|
| `go vet ./...` | aprovado |
| `go test -count=1 ./...` | aprovado |
| `go test -race -count=1 ./...` | aprovado |
| regressões críticas repetidas com `-race` | aprovadas: race de hooks 20x, shutdown 10x, log/lease 20x |
| `govulncheck ./...` | **nenhuma vulnerabilidade encontrada** |
| `make build` | aprovado |
| `make verify-release` | aprovado |
| cobertura Go | **60,4%** de statements |

A base contém 355 arquivos Go, 174 arquivos `_test.go` e 1.438 funções `Test*`. A cobertura permanece desigual: `mcpserver` 82,6%, extensão externa 79,8%, parser 78,2%, runtime 75,4%, mas interpretador 19,9%.

### Testes funcionais e E2E

| Suíte | Resultado |
|---|---|
| `sample/syntax` | 322 aprovadas, 0 falhas, 0 incompletas |
| `sample/features` | 198 aprovadas, 0 falhas, 8 incompletas por integrações reais |
| Playground web | 7 aprovados, 0 falhas, 2 pulados |
| MCP host E2E | **18 PASS, 0 FAIL**; K8S fora da execução |
| Extensões E2E locais | **10 PASS, 0 FAIL, 11 SKIP** |
| VS Code `npm run compile` | aprovado |
| `npm audit` completo e produção | 0 vulnerabilidades |

Os E2E foram executados numa cópia temporária limpa do commit, com o binário recém-compilado da RC, para não reutilizar artefatos ou alterar resultados versionados.

### Release publicada

A release pública `v1.4.0-rc.2` foi publicada como prerelease e seus workflows **CI** e **Release** concluíram com sucesso. O gate de release executou vet, testes normais, race, `govulncheck`, testes funcionais, cross-build, E2E MCP/extensões, compilação/audit da extensão VS Code e playground.

O artefato público macOS arm64 foi baixado e conferido:

- SHA-256 coincide com `checksums.txt`;
- `go version -m` informa **Go 1.26.6**;
- revisão VCS embutida: `c11055e7b0b38b75f1bff9ab74c55cff2d5ac74f`;
- versão: `mhl 1.4.0-rc.2`;
- release contém cinco builds de runtime, VSIX e checksums.

## Evolução dos achados anteriores

| Achado anterior | Estado | Evidência atual |
|---|---|---|
| B1 — Go 1.26.0 com 19 vulnerabilidades alcançáveis | **Resolvido** | `go.mod` e artefato usam 1.26.6; `govulncheck` limpo. |
| B2 — data race nos hooks de branches paralelos | **Resolvido** | writer por branch; suíte `-race` e repetição focada verdes. |
| B3 — fragmento de segredo após `Seal -> resume` | **Resolvido** | `ringLog.Reopen`, regressão específica e race verdes. |
| B4 — shutdown de extensão matava antes da limpeza | **Resolvido** | espera por saída real; regressão com atraso e E2E 005 verdes. |
| B5 — CI/release sem race, vuln e E2E | **Resolvido com ressalvas** | gate único roda no CI e na tag; `SKIP` de integrações ainda não falha o job. |
| lease adquirido após latência consumir TTL | **Resolvido** | confirmação pós-round-trip e falha fechada testadas. |
| E2E 008/010 desatualizados e binários antigos | **Resolvido** | ambos passam; nenhum binário executável continua versionado. |
| E2E das extensões oficiais 011–021 | **Em aberto** | continuam pulados após migração para `mhl-packages`. |
| instalador podia escolher prerelease no canal estável | **Resolvido** | busca primeiro runtime não-prerelease; fallback explícito enquanto não há stable. |
| descompressão limitada apenas por arquivo | **Resolvido** | orçamento cumulativo, limite de entradas e regressões para tar/zip. |
| `pause_reason` ausente do JSON | **Parcialmente resolvido** | campo adicionado, mas a redação de valores estruturados é insegura. |
| npm/esbuild moderado | **Resolvido** | esbuild 0.28.2; audit completo sem vulnerabilidades. |
| SECURITY/changelog/links | **Parcialmente resolvido** | política real, changelog 1.4 e link público funcionando; versões/status ainda defasados. |

## Bloqueador confirmado

### B1 — P0 — motivos de `pause`/`break` podem expor segredos

A especificação declara `pause(reason?: any)`, portanto o motivo pode ser objeto ou array. Em `internal/cli/run_format.go`, `redactPauseReason` mascara apenas quando o valor raiz é `string`; mapas e arrays passam sem alteração. Já a saída textual em `internal/cli/cli.go` imprime `PauseReason` e `BreakReason` diretamente com `%v`, sem redação.

Uma reprodução com credencial **sintética** registrada pelo próprio runtime via `env("MHL_AUDIT_TOKEN")` confirmou:

```text
pause({token: env("MHL_AUDIT_TOKEN")})
```

No JSON:

```json
"pause_reason": {
  "token": "MHL_AUDIT_SECRET_c7f29e"
}
```

Na saída textual de `pause`:

```text
"AuditPauseRedaction" paused (resume with --resume): map[token:MHL_AUDIT_SECRET_c7f29e]
```

E `break env("MHL_AUDIT_TOKEN")` também expôs o valor integral na saída textual. A saída JSON de `break`, por sua vez, informa `broke: true`, mas omite `break_reason`, apesar de o contrato interno e a documentação de `.run()` tratarem o motivo como dado estruturado.

O servidor MCP não reproduziu essa forma específica porque converte o motivo para JSON compacto e aplica `auth.Redact` antes de responder/persistir. O defeito confirmado está nas fronteiras da CLI e afeta o mesmo commit da RC publicada.

**Impacto:** uma credencial resolvida pode aparecer em terminal, logs de CI ou JSON consumido por automação. Isso viola a expectativa explícita de redação do runtime e impede uma release estável.

**Critério de saída:**

1. aplicar `runtime.RedactValue` recursivamente ao `PauseReason` antes de serializar;
2. aplicar a mesma política a `PauseReason` e `BreakReason` na saída textual;
3. decidir e alinhar o campo `break_reason` do JSON com o contrato público;
4. testar strings, objetos, arrays e mapas aninhados, nos formatos text/JSON e nas respostas MCP;
5. incluir regressão com segredo dividido/embutido em texto e garantir que nenhum valor registrado seja reconstruível.

## Pontos de melhoria ainda abertos

### P1 — gate de extensões oficiais passa com 52% dos cenários pulados

Os cenários 001–010 passam, inclusive o shutdown e o store de teste. Os cenários 011–021, que cobrem S3/MinIO, PostgreSQL, SQL/PostgreSQL e Redis, continuam buscando `src/mhl-extensions/*`, embora esses pacotes tenham migrado para outro repositório. O job fica verde porque `SKIP` retorna sucesso.

Se as quatro extensões são parte da promessa da versão estável, isso é impeditivo. Se `v1.4.0` cobre somente o core runtime, a exclusão deve estar explícita no escopo da release.

**Recomendação:** trazer `mhl-packages` por checkout coordenado, mover os E2E para o repositório que possui os pacotes ou fazer o gate falhar quando um cenário marcado como obrigatório for pulado.

### P1 — documentação ainda comunica beta/alpha e contém resultados contraditórios

- `SECURITY.md` diz que a versão suportada é `v1.4.0-beta.*`, embora a atual seja RC;
- `CHANGELOG.md` termina a seção atual em `beta.24` e não registra `rc.1`/`rc.2` nem o hardening;
- README diz que a linguagem ainda está evoluindo;
- site mostra `v1.3.4-beta.3` e a especificação se declara “Evolving / alpha”;
- relatórios E2E versionados ainda exibem binários `v1.1.0-alpha`, datas antigas e a afirmação já falsa de que shutdown é apenas best-effort;
- dois links relativos continuam quebrados em `PLANO.md`;
- `.github/.DS_Store` continua versionado apesar de ignorado.

A versão estável precisa publicar uma única narrativa de status, compatibilidade, suporte e limitações.

### P1 — RC sem período de estabilização

As tags `rc.1` e `rc.2` foram criadas em 22/09/2026 às 20:39 e 21:23, apenas 44 minutos apartadas. Não há evidência de 7–14 dias de soak, carga representativa, restart/resume prolongado ou validação real de Kubernetes e backends externos.

**Recomendação:** após corrigir B1, publicar `rc.3`, congelar features e observar a mesma revisão por pelo menos 7 dias com os gates verdes.

### P1 — incoerência conhecida entre runtime e lint para `memory json.remove`

O runtime executa `Session.remove(...)`, os exemplos funcionais passam e a documentação o apresenta como operação JSON. Porém, dentro de um pipeline normal, `mhl lint` retorna `json memory has no method "remove"`. Os exemplos atuais não detectam isso porque as chamadas estão em blocos `test`, que esse passe de lint não percorre.

Não é um risco de segurança, mas é uma quebra concreta de experiência e contrato para uma versão estável.

### P1 condicional — perfil multi-réplica ainda declarado como hardening

O defeito de lease da avaliação anterior foi corrigido. Ainda assim, a própria política de segurança recomenda `--single-replica` como perfil bem testado e classifica a coordenação distribuída como menos exercitada. Isso é aceitável se multi-réplica permanecer explicitamente experimental; bloqueia a release se for anunciado como suporte de produção pleno.

### P2 — instaladores e plataformas não têm smoke completo no gate

O gate é acionado quando os instaladores mudam, mas não executa `install.sh`/`install.ps1`. A validação desta rodada confirmou sintaxe POSIX, download, checksum e artefato macOS; o CI faz cross-build, não testes funcionais em Windows/macOS nem instalação real nessas plataformas.

### P2 — supply chain e metas por risco

As releases têm SHA-256, mas não assinatura, SBOM ou proveniência verificável. Actions usam tags mutáveis. Não há limiar de cobertura e o interpretador continua em 19,9%, apesar da boa cobertura black-box. São melhorias recomendadas, não bloqueadores isolados.

### P2 — governança operacional

Há duas issues públicas abertas, ambas classificadas como melhoria, e nenhuma pull request aberta. A issue de servidores permanece aberta mesmo com grande parte da capacidade entregue, sinal de backlog pouco reconciliado. O histórico segue concentrado principalmente em um mantenedor.

## Pontos fortes atuais

1. **Correções com regressões reais.** Os antigos bloqueadores receberam testes que reproduzem exatamente a falha, inclusive sob race detector.
2. **Gate único e executado duas vezes.** CI e release chamam a mesma definição, reduzindo divergência entre merge e tag.
3. **Segurança da toolchain restaurada.** Artefato publicado em Go 1.26.6 e scanner oficial limpo.
4. **E2E do core forte.** Todos os 18 cenários MCP locais passaram, incluindo auth, ownership, fila, drain, timeout, restart, logs, schema e cancelamento.
5. **Distribuição verificável.** Cinco plataformas, extensão VS Code e checksums consistentes numa release pública bem formada.
6. **Repositório mais limpo.** Binários antigos foram removidos; E2E agora recebe um artefato fresco do job.
7. **Boas defesas de extensões.** Pin de hash, lock, ambiente restrito, allow-list de segredos, limites de frame e de extração, traversal protegido e aviso explícito sobre permissões OS não impostas.
8. **Documentação das limitações.** Single-replica, ausência de sandbox e falta de assinatura/SBOM estão declarados, mesmo que o status da versão ainda precise ser atualizado.

## Lacunas não validadas

- Kubernetes real (`CENARIO-K8S-001`);
- S3/MinIO, PostgreSQL e Redis das extensões oficiais após a migração;
- chamadas reais a LLMs/MCP/A2A marcadas como incompletas;
- soak/carga prolongada da mesma RC;
- execução funcional nativa em Windows e Linux arm64;
- smoke do `install.ps1` em Windows;
- compatibilidade formal de checkpoints/arquivos entre versões;
- configuração administrativa de branch protection fora do conteúdo do repositório.

Ausência de validação não prova defeito, mas limita a promessa segura de suporte.

## Checklist mínimo para GO

- [x] corrigir a redação recursiva de `pause_reason` e dos motivos textuais de `pause`/`break`;
- [x] adicionar regressões de segredo estruturado nas fronteiras CLI e MCP;
- [x] decidir e documentar `break_reason` no JSON da CLI;
- [x] corrigir `json.remove` no lint;
- [x] definir formalmente que os pacotes de extensão não entram no escopo do core `v1.4.0`;
- [x] declarar a separação e mover a responsabilidade de integração para `mhl-packages`;
- [x] atualizar SECURITY, CHANGELOG, versões/status do site e marcar relatórios E2E históricos;
- [ ] publicar uma nova RC a partir do commit corrigido;
- [ ] manter 7–14 dias sem features, sem P0/P1 novo e com todos os gates obrigatórios verdes;
- [ ] confirmar no gate remoto os novos smokes de instalação em Linux, macOS arm64 e Windows (macOS arm64 já aprovado localmente).

## Conclusão

Os ajustes orientados foram efetivos: o projeto resolveu os defeitos graves que impediam até mesmo um RC confiável e elevou muito a qualidade do processo de release. O core está próximo de uma versão final.

**Recomendação final:** não renomear `v1.4.0-rc.2` para stable. Corrigir o vazamento dos motivos de controle, encerrar as inconsistências de contrato/documentação e usar uma `rc.3` congelada como candidata real. Cumpridos esses pontos e o soak, a avaliação tende a mudar para **GO** sem exigir nova mudança arquitetural.

## Referências

- release: https://github.com/mh-language/mhl-core-runtime/releases/tag/v1.4.0-rc.2
- workflow de release: https://github.com/mh-language/mhl-core-runtime/actions/runs/35801841772
- workflow de CI: https://github.com/mh-language/mhl-core-runtime/actions/runs/35800313407
- scanner oficial Go: https://go.dev/doc/security/vuln/
- `src/mhl-runtime/internal/cli/run_format.go`
- `src/mhl-runtime/internal/cli/cli.go`
- `src/mhl-runtime/internal/engine/runtime/state.go`
- `src/mhl-runtime/internal/execsvc/execsvc.go`
- `src/mhl-runtime/internal/mcpserver/runlog.go`
- `src/mhl-runtime/internal/mcpserver/runlock.go`
- `src/mhl-runtime/internal/extension/external/process.go`
- `.github/workflows/gate.yml`
- `.github/workflows/release.yml`
- `tests_e2e/cloud/tests_mcp_pods/`
- `tests_e2e/extensions/tests_ext/`
