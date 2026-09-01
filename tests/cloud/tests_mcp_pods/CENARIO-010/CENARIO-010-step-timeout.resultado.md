# CENÁRIO-010 — Resultado da Execução

> Registro gerado pelo agente `tester-mcp-server`.
> O arquivo original [`CENARIO-010-step-timeout.md`](CENARIO-010-step-timeout.md) não foi alterado.

## Cenário 010: Cap de wall-clock por passo (`step … timeout <dur>`)

**Resultado Real:**
- [x] funcionou
- [ ] não funcionou

**Executado em:** 2026-08-31 13:13 -03:00

---

## Ambiente

| Item | Valor |
|---|---|
| Binário | `tests/cloud/tests/CENARIO-010/mhl` (cópia de `tests/cloud/mhl`) |
| Versão | `mhl v1.1.0-alpha-7-g418d7dc-dirty` |
| Servidor | `mhl serve mcp --http --addr 127.0.0.1:8730 --token <gerado> --state-dir <tmp> tests/cloud` |
| Workflow | `SlowBuild` — `Compile` (`sleep 3`), `Package` (`sleep 3`), `Ship timeout 1s` sobre `sleep 3` |
| Script | [`run.sh`](run.sh) |

## Tentativa 1 — `run/start SlowBuild`

Polling de `run/status` ([`logs/status1-*.json`](logs/)):

```
[1..3] working  step=Compile     (sleep 3)
[4..6] working  step=Package     (sleep 3)
[7]    working  step=Ship
[8]    failed   step=Ship
```

`run/status` final ([`logs/status1-final.json`](logs/status1-final.json)):

| Campo | Esperado | Obtido |
|---|---|---|
| `state` | `failed` | `failed` |
| `step` | `Ship` | `Ship` |
| `error` | contém `exceeded its timeout` / `step timeout` | `runtime: step "Ship" exceeded its timeout: step timeout exceeded` |
| `reached` inclui `Compile` e `Package` | sim | `["Compile","Package","Ship"]` |
| `resumable` | true | `true` |

## Tentativa 2 — `run/resume { runId }`

| Passo | Obtido |
|---|---|
| `run/resume` | `state:"working"` |
| `run/status` poll | `working step=Ship` → `failed step=Ship` |
| `error` | `runtime: step "Ship" exceeded its timeout: step timeout exceeded` (mesma classe) |

## Evidência do orçamento renovado ([`logs/mcp-server.log`](logs/mcp-server.log))

```
run started (resume:false)  SlowBuild
  step: Compile
  step: Package
  step: Ship
run failed   durationMs=7040  steps=3      ← 3 + 3 + 1 s

run started (resume:true)   SlowBuild       ← run/resume
  step: Ship
run failed   durationMs=1002  steps=4      ← Ship reexecutado com 1 s CHEIO de orçamento
```

O `durationMs=1002` na leg de resume mostra que o passo `Ship` recebeu um `timeout 1s`
novo — não herdou o orçamento já gasto da 1ª tentativa.

## Evidências

- [x] [`logs/mcp-server.log`](logs/mcp-server.log) — `step: Compile/Package/Ship`, `run failed` ×2 (7040 ms / 1002 ms)
- [x] [`logs/status1-final.json`](logs/status1-final.json) — `failed` em `Ship` com erro de timeout, `reached:["Compile","Package","Ship"]`
- [x] [`logs/run-resume.json`](logs/run-resume.json), [`logs/status2-final.json`](logs/status2-final.json) — resume → falha de novo em `Ship`
- [x] [`logs/status1-1.json`](logs/status1-1.json) … [`logs/status1-8.json`](logs/status1-8.json) — polling completo da 1ª tentativa
- [x] [`logs/client.log`](logs/client.log)

## Observações

- Referência de design: item "Per-step wall-clock cap" — implementado. O erro carrega
  `ErrStepTimeout` (`step timeout exceeded`), então um cliente pode distinguir de um cancel comum.
- Detalhe cosmético (não asserido): após o resume, `reached` vem `["Compile","Package","Ship","Ship"]`
  (o `Ship` é anexado de novo pela leg de resume) e `stepIndex` oscila (`3` na 1ª, `1` na 2ª).
  `state`, `step` e `error` estão corretos.
- Não se alterou `slow-build.mh`; a run **não** completa por design (o `Ship` sempre estoura o `timeout 1s`).
- Servidor encerrado ao fim (nenhum `mhl serve mcp` ativo).

## Conclusão

**PASS.** O passo `Ship` estoura sua cláusula `timeout 1s` e a run termina como `failed` em
`Ship` com um erro que menciona o timeout (`exceeded its timeout: step timeout exceeded`),
com `Compile` e `Package` já em `reached` e `resumable: true`. `run/resume` re-executa `Ship`
com um orçamento de 1 s novo, que estoura de novo (`durationMs ≈ 1002`).
