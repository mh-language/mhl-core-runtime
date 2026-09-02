# tests_mcp_targets — cenários-alvo (critérios de aceite)

Diferente de `tests_mcp_pods/` (regressivo do que **já funciona**), aqui cada
cenário descreve o **comportamento desejado** de um item do
[`../mhl-eks-design.html`](../mhl-eks-design.html). Hoje eles **falham** — cada
asserção não satisfeita vira um `PENDING` com *o que precisa ser implementado*.
Quando a correção landa, o cenário vira `MET`.

## Escopo

Só entram aqui itens que ajudam na **distribuição** (rodar em várias réplicas
atrás de um load balancer) e no **isolamento da informação do cliente**
(partição dos dados por tenant). O runtime **não** faz o papel do service mesh:
autenticação, política de autorização e roteamento são do Istio / API Gateway.
O runtime só **consome** a identidade que o mesh já verificou (via
`--principal-header`) e a usa como chave de partição.

Itens que ficam **fora do runtime** (delegados ao mesh / gateway) estão em
[`_delegated/`](_delegated/) — documentados, com o `run.sh` que mostra o gap,
mas fora da lista de trabalho.

## Ativos

| Spec | Item do design | Alvo (distribuição / isolamento) |
|---|---|---|
| [ITEM-01](ITEM-01/ITEM-01-live-registry-distributed-cancel.md) | 01 | `run/status` mostra progresso live entre pods; `run/cancel` alcança a goroutine no pod dono (coordenação, não authz) &mdash; **MET** |
| [ITEM-02](ITEM-02/ITEM-02-shared-sessions.md) | 02 | `Mcp-Session-Id` reconhecido em qualquer réplica; a partição por principal continua valendo entre pods &mdash; **MET** |
| [ITEM-11](ITEM-11/ITEM-11-run-capability-discovery.md) | 11 | `run/*` anunciado em `initialize` com versão &mdash; **MET** |

Toda a lista de distribuição está **MET**; a suíte é mantida como regressivo.

## Delegados ao mesh (`_delegated/`)

| Spec | Item | Quem faz | O que o runtime já dá |
|---|---|---|---|
| [ITEM-04](_delegated/ITEM-04/ITEM-04-tools-call-autopromote.md) | 04 | ergonomia de gateway | atrás de gateway o cliente usa `run/*` (documentado) — não é item de distribuição |
| [ITEM-10](_delegated/ITEM-10/ITEM-10-per-principal-auth.md) | 10 | **Istio / API Gateway** (JWT, tokens, escopos) | consome a identidade verificada via `--principal-header` (item 03) |
| [ITEM-12](_delegated/ITEM-12/ITEM-12-approver-authz.md) | 12 | **Istio `AuthorizationPolicy`** em `POST /mcp/run/resume` | run ligada ao principal: não-dono não vê nem retoma (isolamento — item 12 "parcial" já cobre isso) |

## Veredito

| | exit | |
|---|---|---|
| `MET` | 0 | asserções-alvo passam |
| `PENDING` | 2 | falta implementar (o `run.sh` lista cada item) |
| `FAIL` | 1 | o script quebrou |

Hoje: **3 MET · 0 PENDING · 0 FAIL**.

`run-all.sh` roda só os **ativos** (`ITEM-*/` no topo) e só sai `!= 0` em `FAIL`.

## Rodar

```sh
./run-all.sh
ONLY="01 02" ./run-all.sh
bash _delegated/ITEM-10/run.sh      # um delegado, à parte
```

Pré-requisitos: `tests/cloud/mhl`. Portas 8761–8770. Sem Docker. Os cenários
multi-réplica (01, 02) usam um `--state-dir` compartilhado como análogo de EFS.
