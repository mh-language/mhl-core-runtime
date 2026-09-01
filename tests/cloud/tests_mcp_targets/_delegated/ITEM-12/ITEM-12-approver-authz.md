# ITEM-12 (alvo): `approvers:` no gate, avaliado contra o principal verificado

> **Fora do escopo do runtime — delegado ao mesh.** "Quem pode aprovar" é
> política de **autorização**, não do runtime. Use uma **`AuthorizationPolicy`
> do Istio** em `POST /mcp/run/resume` (há o wrapper `POST /mcp/<method>` para
> isolar esse path) permitindo só os principals aprovadores.
> A parte de **isolamento** que era do runtime **já está feita** (item 12
> "parcial"): a run é ligada ao principal e o owner é persistido, então um
> não-dono recebe `-32602` em `run/status` / `run/resume`, inclusive
> pós-restart e entre réplicas (CENARIO-006 / CENARIO-011 / ITEM-02).
> `approvers:` **não** vira propriedade da linguagem.

**Item do design:** 12 — *HITL gates have no approver authorization* (parcial).
**Estado hoje:** `run/resume` só checa **posse** da run. Não existe `approvers:`;
escrevê-lo no `checkpoint` passa no `mhl lint` e é silenciosamente ignorado.

## Comportamento-alvo

```gherkin
Dado um DocPipeline com  checkpoint: { ..., approvers: ["carol"] }
E `mhl serve mcp --http --token T --principal-header X-Mhl-Principal`

Quando alice (dona da run, NÃO está em approvers) faz run/resume {approved:yes}
Então -32001 forbidden — "principal alice não está em approvers"
E a run continua parada no gate

Quando carol (está em approvers) faz run/resume {approved:yes}
Então a run vai a "completed"

E `mhl lint` num workflow com `approvers: ["carol"]` -> sem "unknown property"
E `mhl lint` com `approvers: 123` (tipo errado) -> erro
```

## Critério de aceite

1. `mhl lint` reconhece `approvers:` no `checkpoint`/gate (lista de strings);
   um valor não-lista é erro de lint.
2. `run/resume` por um principal **fora** de `approvers` → recusado com erro de
   autorização (não `completed`), mesmo sendo o dono da run.
3. `run/resume` por um principal **em** `approvers` → `completed`.
4. (regressão) não-dono continua barrado por posse (`-32602`).

## Como implementar (pistas)

- `internal/lang/ast` + `internal/lang/lint`: adicionar `approvers []string` ao
  bloco `checkpoint` (ou ao `step` do gate); `lint.checkPipelineProperties` /
  o leitor de `checkpoint` passam a conhecê-lo; validar tipo.
- `internal/mcpserver/runs.go` `runResume`: além de `ownedRun`, checar
  `principal ∈ checkpoint.approvers` quando a lista existe; senão `-32001`.
- Propagar `approvers` no checkpoint persistido para o resume pós-restart.
- Docs: `docs/site/reference.html` (surface da linguagem).

## Evidências (logs/)
- `lint-ok.txt` (approvers válido), `lint-badtype.txt` (approvers:123)
- `alice-resume.json` (forbidden), `carol-resume.json` (completed)
- `bob-status.json` (regressão: -32602)

**Verificado por:** `./run.sh` — hoje **PENDING**.
