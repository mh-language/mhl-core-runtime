# _delegated — itens fora do runtime

Estes cenários descrevem comportamentos que **não** devem virar código no
runtime do `mhl`. Autorização, política de acesso e gestão de credenciais são
do **service mesh / API Gateway** (Istio). O runtime só recebe a identidade que
o mesh já verificou (via `--principal-header X-Mhl-Principal`, item 03 do
design) e a usa como chave de partição dos dados do cliente.

Cada `run.sh` ainda roda (mostra o gap com o mhl atual), mas **não** entra no
`run-all.sh` nem na lista de trabalho.

| Spec | Item | Feito por | Como configurar | O runtime já entrega |
|---|---|---|---|---|
| [ITEM-04](ITEM-04/ITEM-04-tools-call-autopromote.md) | 04 — `tools/call` longo | ergonomia de gateway, não distribuição | atrás do API Gateway o cliente **deve** usar `run/start` → `run/status` → `run/resume` (o `docs-workflow.mh` documenta isso); a capability `experimental["mhl.run"]` (ITEM-11) anuncia que a família existe | `run/*` completo + descoberta |
| [ITEM-10](ITEM-10/ITEM-10-per-principal-auth.md) | 10 — auth por principal | **Istio** (`RequestAuthentication` + JWKS) ou o **API Gateway** (Cognito / Lambda authorizer) | validar o JWT no ingress e injetar o `sub` no header `X-Mhl-Principal`; o `--token` continua sendo só o segredo compartilhado gateway↔mhl (anti-spoof do header) | `--principal-header` → a identidade verificada vira o `owner` das runs, isolando `run/list`/`run/status`/`run/logs`/`run/resume` por principal (CENARIO-006) |
| [ITEM-12](ITEM-12/ITEM-12-approver-authz.md) | 12 — authz de aprovador | **Istio `AuthorizationPolicy`** | isolar `POST /mcp/run/resume` (há um wrapper `POST /mcp/<method>` para isso) numa policy que só deixa passar os principals aprovadores | a run é ligada ao principal e o owner é persistido → um não-dono recebe `-32602` em `run/status`/`run/resume`, inclusive pós-restart e entre réplicas (CENARIO-006 / CENARIO-011 / ITEM-02) |

Rodar um à parte:

```sh
bash _delegated/ITEM-10/run.sh
```
