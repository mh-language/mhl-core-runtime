# mhl serve: subida lenta, que cresce com o número de módulos importados

_mh-language/mhl-core-runtime · mhl 1.5.0-alpha.2 · 08 out 2026_

Encontrado no app Senpai, que roda `mhl serve mcp --http` como processo filho. O servidor só responde em `/healthz` depois de validar todos os `.mh` do diretório. Numa árvore de 69 arquivos (15,3 mil linhas), isso leva **~7 s num Apple M4**, e o tempo cresce a cada módulo novo. Pelos números abaixo, o custo vem de reanalisar os mesmos módulos importados uma vez para cada arquivo, e não do tamanho do código.

Os números foram medidos de fora (caixa-preta), sem acesso ao código do runtime. As causas prováveis estão marcadas como hipóteses.

## Impacto

- **Abertura do app e "Reconectar":** a interface espera os ~7 s em toda subida. Numa máquina Windows mais lenta, ou com antivírus inspecionando cada arquivo lido, o mesmo trabalho leva várias vezes mais. O app já precisou subir o prazo de prontidão de 30 s para 120 s, porque o `mhl` estava sendo morto ainda carregando.
- **Troca de agente/modelo:** até hoje, cada troca reiniciava o `mhl` e custava esses ~7 s. Contornamos isso do lado do app (detalhes em "O que já fizemos do nosso lado"), mas a subida continua lenta.
- **Escala:** um único módulo de workflow novo (6 arquivos, ~1.000 linhas) acrescentou ~1 s à subida.

## Ambiente

- macOS 26.6.2, Apple M4 (10 núcleos), 24 GB
- `mhl 1.5.0-alpha.2`
- Árvore servida: 69 arquivos `.mh`, 15.342 linhas, 65 blocos `test { }`, 7 workflows expostos como tools

## Medições

### 1. A subida do `serve` custa o mesmo que um `lint` completo

| Comando | Tempo real | CPU (user) |
|---|---|---|
| `mhl serve mcp --http … workflows` até `/healthz` = 200 (3 execuções) | 7,3 s · 7,4 s · 8,1 s | — |
| `mhl lint workflows` | 6,3 s | 12,7 s |
| Parada do `serve` (SIGINT) | 0,07 s | — |

Memória de pico do `lint`: ~50 MB, ou seja, o tempo não vem de memória. O `stderr` do `serve` não diz nada até ficar pronto (só a linha "7 tool(s) from workflows").

### 2. O custo de um arquivo acompanha o seu fecho de importações, não o seu tamanho

`mhl lint <arquivo>` em cada um dos 69 arquivos, separadamente:

| Arquivo | Linhas | `import`s diretos | Tempo |
|---|---|---|---|
| `delivery/delivery.mh` | 136 | 3 (puxam quase a árvore toda, transitivamente) | 0,55 s |
| `artifact_flow/artifact_flow.mh` | 151 | 8 | 0,52 s |
| `discovery/discovery_artifacts.mh` | 380 | 15 | 0,47 s |
| `artifact_preview/artifact_preview.mh` | 547 | 5 | 0,38 s |
| `shared/artifacts/cite.mh` | 189 | 0 | 0,08 s |
| `shared/core/senpai_config.mh` | 34 | 0 | 0,08 s |

- Um arquivo sem imports custa ~0,08 s, que é essencialmente o custo de subir o processo, independentemente de ter 34 ou 189 linhas.
- `delivery.mh` tem 136 linhas e alcança quase a árvore inteira pelos imports. Validá-lo **com todo esse fecho** leva só 0,55 s.
- **A soma dos 69 tempos individuais dá 12,2 s, praticamente igual aos 12,7 s de CPU do `lint` da árvore inteira.**

Na mesma linha, `mhl serve` sobre `workflows/delivery` sozinho (2 arquivos, que importam quase todo o resto) sobe em **0,82 s**, contra 7,3 s da árvore inteira.

### 3. O `serve` também valida os blocos `test { }`, que nunca executa

Mesma árvore, com os 65 blocos `test` removidos (−29% de linhas):

| | Com testes | Sem testes |
|---|---|---|
| `mhl lint` | 6,3 s | 4,1 s (−35%) |
| `mhl serve` até `/healthz` | 7,3 s | 4,4 s (−40%) |

## Hipóteses

1. **Sem memoização entre arquivos (principal).** Cada arquivo do diretório parece ser analisado como raiz independente, reanalisando todo o seu fecho de imports. Um módulo de `shared/` importado direta ou indiretamente por dezenas de arquivos acaba checado dezenas de vezes. Evidências:
   - a soma dos tempos individuais é igual à CPU do lint completo (medição 2);
   - um único arquivo que alcança quase todo o grafo é validado em 0,55 s.
   
   Com o resultado de cada módulo reaproveitado, a árvore inteira deveria custar perto de uma única passada pelo grafo, algo na ordem de 0,5–1 s em vez de 7 s.
2. **Validação de `test { }` no `serve`.** O servidor não executa testes, mas paga ~3 s (~40% da subida) para validá-los (medição 3).
3. **Validação de módulos que não são entrada.** Só 7 dos 69 arquivos declaram workflows expostos. Os outros são módulos de biblioteca, que só precisariam ser checados no fecho das entradas, e uma vez só.

## Reprodução

Qualquer árvore em que muitos arquivos importem os mesmos módulos compartilhados mostra o efeito. Para medir:

```sh
# subida do serve até ficar pronto
now() { python3 -c 'import time; print(time.time())'; }   # date +%N nao existe no macOS
start=$(now)
mhl serve mcp --http --addr 127.0.0.1:18770 --state-dir /tmp/mhl-st workflows >/dev/null 2>&1 &
pid=$!
until curl -sf http://127.0.0.1:18770/healthz >/dev/null; do sleep 0.02; done
python3 -c "print('pronto em %.2fs' % ($(now) - $start))"
kill -INT $pid

# custo por arquivo vs. árvore inteira
time mhl lint workflows
for f in $(find workflows -name '*.mh'); do /usr/bin/time -p mhl lint "$f" 2>&1 | awk -v f="$f" '/^real/ {print $2, f}'; done | sort -rn
```

A árvore do Senpai (`workflows/`) pode ser compartilhada se ajudar a reproduzir.

## Sugestões, por ordem de impacto esperado

1. **Memoizar a análise por módulo dentro do processo**, chaveada pelo caminho canônico, para que cada `.mh` seja analisado uma vez por subida, não uma vez por arquivo que o importa. Isso ataca a hipótese 1 e beneficia `serve`, `lint` e `test`.
2. **No `serve`, não validar o corpo dos blocos `test { }`**, ou validar só a sintaxe deles. Ganho medido: ~40% da subida nesta árvore.
3. **Cache em disco entre execuções**, chaveado por hash de conteúdo (e versão do `mhl`), por exemplo no `--state-dir`. A abertura de um app que reinicia o `serve` com os mesmos arquivos passaria a ser quase imediata.
4. **Responder `/healthz` (ou um `/readyz` separado) com progresso**, ou registrar no `stderr` a fase atual e quantos arquivos faltam. Hoje uma subida lenta e um `mhl` travado são indistinguíveis para quem o supervisiona.

## O que já fizemos do nosso lado

- **Troca de agente/modelo sem reiniciar:** o app passou a gravar a escolha num arquivo que os workflows leem a cada chamada (os `args:` de um `agent` são avaliados a cada `.run()`, o que confirmamos). Assim a troca deixou de reiniciar o `mhl`. A subida em si (abertura do app e "Reconectar") continua pagando os ~7 s.
- **Prazo de prontidão:** o app espera até 120 s antes de desistir do `mhl`, para não matá-lo ainda carregando em máquinas lentas.
