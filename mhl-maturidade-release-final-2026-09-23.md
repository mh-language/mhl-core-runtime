# MHL — reavaliação de maturidade para release estável

**Data:** 23/09/2026

**Base avaliada:** `89a5b1229c0e7aa9cc1332d2291051f510c97ee0`

**Branch:** `develop`, sincronizada com `origin/develop`

**Escopo:** runtime Go, CLI, MCP/A2A, mecanismo de extensões, VS Code,
documentação, instaladores, CI e processo de release.

## Parecer executivo

**Decisão técnica: GO para publicar uma nova release candidate congelada.**

Não foi encontrado bloqueador P0 ou P1 confirmado no código atual. Os achados
que motivaram a rodada anterior foram corrigidos: redação de segredos,
checkpoint de chaves dinâmicas, contrato JSON de `break_reason`, lint de
`json.remove`, separação das extensões, documentação e smoke dos instaladores.

**Decisão para promover imediatamente a `v1.4.0` estável: GO condicional.** A
implementação está madura, mas a promoção deve ocorrer somente depois de:

1. um gate remoto final integralmente verde; e
2. um período de 7–14 dias da mesma RC sem nova funcionalidade e sem defeito
   P0/P1.

Essas condições são evidências de release, não novas correções de produto.

### Resultado resumido

| Dimensão | Nota | Avaliação |
|---|---:|---|
| Arquitetura e modularidade | 8,5/10 | Limites claros entre linguagem, engine, features, CLI e extensões. |
| Funcionalidade | 9/10 | 520 asserções funcionais aprovadas no commit atual. |
| Testes e evidência | 9/10 | Testes normais, race, funcionais, E2E e três instaladores exercitados. |
| Concorrência e confiabilidade | 8,5/10 | Nenhuma corrida local; uma reexecução remota do race ficou vermelha e deve ser estabilizada. |
| Segurança | 9/10 | Redação recursiva coberta e `govulncheck` sem vulnerabilidades. |
| CI e release engineering | 8,5/10 | Gate único e multiplataforma; falta encerrar a última execução vermelha. |
| Documentação e governança | 8/10 | Escopo e estado de RC alinhados; melhorias de supply chain continuam recomendadas. |
| **Maturidade global** | **9/10** | **Core tecnicamente pronto para RC final.** |

## Evidência desta reavaliação

### Validação local no commit `89a5b12`

| Verificação | Resultado |
|---|---|
| base do código | commit sincronizado com `origin/develop`; esta avaliação alterou somente este relatório |
| `git diff --check` | aprovado |
| `go vet ./...` | aprovado |
| `go test -count=1 ./...` | aprovado |
| `go test -race -count=1 ./...` | aprovado |
| `go test -race -count=3 ./...` | três repetições adicionais aprovadas |
| `govulncheck ./...` | nenhuma vulnerabilidade encontrada |
| `make verify-release` | Linux amd64, macOS arm64 e Windows amd64 aprovados |
| `sample/syntax` | 322 aprovadas, 0 falhas, 0 incompletas |
| `sample/features` | 198 aprovadas, 0 falhas, 8 incompletas por integrações externas reais |
| playground | 7 aprovados, 0 falhas, 2 testes de integração pulados |
| VS Code `npm run compile` | aprovado |
| VS Code `npm audit --omit=dev` | 0 vulnerabilidades |
| instalador macOS arm64 | build, checksum, instalação, execução e desinstalação aprovados |

### Gate remoto do mesmo commit

A execução
[`35946132396`](https://github.com/mh-language/mhl-core-runtime/actions/runs/35946132396)
passou integralmente:

- Go vet, testes, race, vulnerabilidades, funcional e cross-build;
- MCP E2E;
- VS Code e playground;
- instaladores em Ubuntu, macOS arm64 e Windows.

A reexecução posterior
[`35946937714`](https://github.com/mh-language/mhl-core-runtime/actions/runs/35946937714)
falhou somente no passo `Test (race)`. Os três smokes de instalador, VS Code e
playground passaram; o MCP E2E foi pulado porque depende do job Go. Como o
mesmo SHA já passou nesse gate e também passou quatro vezes localmente sob
race detector, isso aponta para flakiness, não para uma corrida confirmada.
Mesmo assim, uma tag estável não deve ser criada enquanto a execução final
estiver vermelha.

## Estado dos achados anteriores

| Achado | Estado atual |
|---|---|
| vazamento em `pause_reason` e `break_reason` | **Resolvido**, com redação recursiva em texto, JSON e MCP |
| segredo usado como chave dinâmica | **Resolvido**, inclusive em checkpoint reidratável |
| `break_reason` ausente do JSON | **Resolvido** |
| `json.remove(key)` rejeitado pelo lint | **Resolvido** |
| E2E das implementações de extensões no core | **Resolvido por escopo**: removidos e atribuídos ao `mhl-packages` |
| testes do mecanismo genérico de extensões | **Preservados** no core |
| documentação ainda apresentava beta/alpha | **Resolvido** para release candidate |
| ausência de smoke dos instaladores | **Resolvido** em Linux, macOS e Windows |
| vulnerabilidades da toolchain | **Resolvido**; scanner oficial limpo |
| data race dos hooks | **Resolvido** nas regressões e nas execuções aprovadas |
| shutdown de extensões | **Resolvido** |
| vazamento após `Seal -> resume` | **Resolvido** |

## Pontos fortes

1. O core mantém testes do parser, registry, protocolo, instalação e dispatch
   de extensões sem carregar implementações que pertencem a outro repositório.
2. As fronteiras que podem emitir ou persistir estado aplicam redação de
   credenciais, incluindo objetos, arrays e chaves dinâmicas.
3. O gate de release é o mesmo usado por pushes, pull requests e tags.
4. O binário é testado sob race detector e scanner de vulnerabilidades.
5. Os instaladores são exercitados de ponta a ponta nas três plataformas
   publicadas pelo gate.
6. Os testes funcionais cobrem amplamente a linguagem sem depender de serviços
   externos para o caminho obrigatório.

## Riscos residuais — não bloqueadores no escopo declarado

- Multi-réplica continua documentado como hardening; o perfil recomendado é
  single-replica. Isso só vira impeditivo se multi-réplica for prometido como
  suporte de produção pleno em `v1.4.0`.
- As oito asserções incompletas dependem de LLMs/MCP/A2A reais. Elas não
  representam falha do core, mas limitam a evidência dessas integrações.
- Artefatos têm SHA-256, porém ainda não têm assinatura, SBOM ou proveniência
  verificável. Actions continuam referenciadas por tags mutáveis.
- O mecanismo de extensões não oferece sandbox de sistema operacional; essa
  limitação está declarada e extensões não confiáveis precisam de isolamento
  externo.
- Builds feitos diretamente na `develop` recebem uma versão derivada de uma
  tag beta ancestral por causa da topologia de tags. Releases criadas a partir
  da tag correta recebem a versão exata; portanto é uma questão de ergonomia
  de builds de desenvolvimento, não de artefato publicado.

## Checklist para a versão final

- [x] corrigir os achados de segurança e contrato;
- [x] remover implementações e E2E de extensões deste repositório;
- [x] preservar os testes do mecanismo de extensão do core;
- [x] atualizar documentação, SECURITY e CHANGELOG;
- [x] validar instaladores em Ubuntu, macOS arm64 e Windows;
- [x] obter ao menos uma execução completa e verde do gate no commit candidato;
- [ ] encerrar a flakiness da última execução de `Test (race)` ou obter uma nova
      execução integralmente verde;
- [ ] publicar uma nova RC a partir do commit corrigido;
- [ ] manter a mesma revisão em soak por 7–14 dias, sem features e sem P0/P1;
- [ ] promover exatamente o commit aprovado para `v1.4.0`.

## Conclusão

O MHL não está mais em estado de beta técnico. O core atual é uma RC madura e
não possui bloqueador de implementação conhecido. A recomendação é publicar a
próxima RC, congelar o código e promover o mesmo commit após o gate final verde
e o soak. Não há indicação de nova mudança arquitetural necessária para a
versão final.
