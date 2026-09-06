# Design — Intake durável no runtime MHL

**Status:** plano do piloto. Decidido 06/09/2026 — o piloto roda com o runtime
consumindo a própria fila durável; **o admissor Python não será adotado**.
**Relacionado:** `mvp/PLANO.md` (§ "Intake durável no runtime — base do piloto",
P1-10, P1-8, M-19, P0-4), `mvp/docs/architecture.html`,
`mvp/mhl-avaliacao-tecnica.md`.

---

## 1. Problema

O runtime mantém o **registro de runs em memória, por pod**. Um run aceito por
`run/start` mas ainda parado atrás do teto de concorrência não tem existência
durável — sem checkpoint, sem status publicado, porque nenhum passo rodou. Se o
pod morre, esse run **some**: os testes de estresse mediram 436 runs perdidos num
crash e 1.844 num rolling restart.

A arquitetura do lab hoje contorna isso com um serviço externo (o **admissor**
Python + tabela `admission_queue` no Postgres) que grava a intenção antes de
qualquer tentativa no runtime, e ainda faz rate-limit e reconciliação.

**Decisão do piloto:** não adotar esse admissor. A admissão durável, o claim e a
reconciliação passam para dentro do runtime. Só a política de admissão
(rate-limit / quota / prioridade / shedding) fica fora, sem estado, no nginx.

## 2. Objetivo e fronteira

**Objetivo:** o runtime é capaz de iniciar um run `pending` durável — reivindicá-lo
de forma atômica entre réplicas e executá-lo, sobrevivendo a "pod caiu antes de
começar".

**Fronteira (mecanismo × política):**

| No runtime (core) | Fora do runtime (nginx, sem estado) |
|---|---|
| Gravar `pending` durável e devolver o `runId` | Taxa de admissão / shedding de carga |
| Reivindicar `pending` de forma atômica | Prioridade entre `pending` |
| Executar, com lease + takeover para o run em voo | Quota por principal/tenant |
| Reconciliar run órfão | Autenticação do chamador (já é do gateway) |
| `pending`/`paused` como estado de 1ª classe | |

O controlador de admissão é um bloco de rate-limit no `nginx.conf` (ou sidecar
mínimo sem estado) que só responde `429 + Retry-After` sob sobrecarga. Nenhuma
fila durável fora do runtime.

**Não-objetivo:** rate-limit, prioridade e quota dentro do core. Fila durável de
mensagens *chamada pelo workflow* (é outra coisa). Remover gateway/authorizer.

## 3. Decisões de contrato — travar antes de congelar o binário da demo

Estas quatro não podem ser retrofitadas sem quebrar contrato. Congelar a demo sem
elas fixa uma API que bloqueia o caminho do piloto.

1. **O `runId` nasce no `run/start` e é durável desde ali.** O split
   `claim_id → runId` do admissor é gambiarra externa e **nunca** entra no
   contrato do runtime. `run/start` devolve `{ runId, state: "pending" }` na hora.

2. **`pending` e `paused` são estados de 1ª classe** na máquina de estados do run.
   `run/status`, `run/logs` e `run/cancel` têm comportamento definido para um
   `runId` que existe e não começou — não como caso especial, não como
   "unknown runId".

3. **O store expõe uma operação de claim atômico.** Assinatura `ClaimNext` na
   interface de store, com **fallback genérico via CAS** (`LockingKVStore`) para
   quem não otimizar. `mhl-store-postgres` pode sobrescrever com
   `SELECT … FOR UPDATE SKIP LOCKED … RETURNING`. A assinatura é fixada agora,
   mesmo sem consumidor.

4. **Inputs são serializáveis, com resposta definida para "grande demais".**
   `run/start` valida inputs serializáveis (reusa `runtime.CheckpointableVars`,
   M-19) e persiste-os pela variante de checkpoint com redação de credenciais
   (`RedactVarsForCheckpoint`, P0-4) — uma credencial passada como input não pode
   ir em claro para o store. Acima de um teto (proposta: 256 KiB) o `run/start`
   recusa, a menos que um blob store esteja configurado (então faz spill para
   `mhl-blob-s3` / blob do store).

## 4. Desenho-alvo

```
        ┌────────────┐
        │  Cliente   │
        └─────┬──────┘
              │ POST /mcp  (Bearer JWT)
              ▼
        ┌────────────┐      ┌──────────────┐
        │  Gateway   │────▶ │ Authorizer   │  valida JWT → principal
        │  (NGINX)   │◀──── │ + Keycloak   │
        └─────┬──────┘      └──────────────┘
              │ troca JWT → MHL_TOKEN + principal
              │ + bloco de rate-limit / 429 (sem estado, no próprio nginx)
              ▼
╔═══════════ RUNTIME (core) ═══════════════════════════════════════════╗
║                                                                     ║
║  run/start ──▶ grava run `pending` no store  ◀── ①  runId nasce     ║
║               e devolve o runId NA HORA          aqui, durável       ║
║                                                                     ║
║   ┌──────────────┐   ┌──────────────┐   ┌──────────────┐            ║
║   │  Réplica A   │   │  Réplica B   │   │  Réplica C   │  N réplicas ║
║   │ ┌──────────┐ │   │ ┌──────────┐ │   │ ┌──────────┐ │            ║
║   │ │executor  │ │   │ │executor  │ │   │ │executor  │ │  max_conc  ║
║   │ │pool      │ │   │ │pool      │ │   │ │pool      │ │            ║
║   │ └────┬─────┘ │   │ └────┬─────┘ │   │ └────┬─────┘ │            ║
║   │ ┌────▼─────┐ │   │ ┌────▼─────┐ │   │ ┌────▼─────┐ │            ║
║   │ │claim loop│ │   │ │claim loop│ │   │ │claim loop│ │            ║
║   │ └────┬─────┘ │   │ └────┬─────┘ │   │ └────┬─────┘ │            ║
║   └──────┼───────┘   └──────┼───────┘   └──────┼───────┘            ║
║          │  ③ ClaimNext(pending) — claim atômico                    ║
║          │     (CAS genérico  ou  SKIP LOCKED no Postgres)          ║
║          ▼                                                          ║
║   ┌─────────────────────────────────────────────┐                   ║
║   │  STORE CAS  (extension Postgres — obrigatório no piloto)        ║
║   │  ┌───────────────────────────────────────┐  │                   ║
║   │  │ runs: máquina de estados              │  │  ④ inputs inline  ║
║   │  │       (② pending/paused 1ª classe)    │  │     se pequenos;  ║
║   │  │ checkpoints por passo                 │  │     ref de blob   ║
║   │  │ lease por run  (+ heartbeat)          │  │     se grandes;   ║
║   │  │ owner (principal)                     │  │     redação P0-4  ║
║   │  └───────────────────────────────────────┘  │                   ║
║   └─────────────────────────────────────────────┘                   ║
║                                                                     ║
║   reconcile loop (built-in, em toda réplica, com jitter):           ║
║   claimed/working/paused com lease morto  →  volta a `pending`      ║
╚═════════════════════════════════════════════════════════════════════╝
```

## 5. Máquina de estados

```
 run/start          claim (CAS)        lease OK
 ─────────▶ pending ──────────▶ claimed ────────▶ working ─────▶ done
              ▲                    │                │ │
              │                    │                │ └─▶ failed (resumable)
              │   reconcile: lease │ morto/expirado │
              └────────────────────┴────────────────┘
              │                                 pause() │ ▲ run/resume
              │ run/cancel                              ▼ │
              ▼                                       paused
          cancelled
```

| Transição | Quem executa | Mecanismo |
|---|---|---|
| `→ pending` | handler do `run/start` (qualquer réplica) | escrita no store |
| `pending → claimed` | claim loop da réplica que ganhar | `ClaimNext` atômico |
| `claimed → working` | mesma réplica | adquire lease |
| `working → done / failed` | executor | — |
| `working ↔ paused` | `pause()` / `run/resume` | checkpoint; takeover por CAS |
| `* → pending` (recuperação) | reconcile loop (built-in) | lease expirado/ausente |
| `pending → cancelled` | `run/cancel` | CAS; nunca executa |

## 6. O seam `ClaimNext`

```
// no pacote de store
ClaimNext(ctx, filter ClaimFilter) (runID string, rec RunRecord, ok bool, err error)
```

- **Fallback genérico (default):** `List(pending/*)` → escolhe 1 (FIFO por
  `created_at`) → `CompareAndSwap(runID, pendingRec, claimedRec{holder,ts})`.
  Perdedores tentam o próximo. Funciona em qualquer `LockingKVStore`
  (Postgres, Redis). Sob contenção, N réplicas desperdiçam CAS — inofensivo,
  simples.
- **Override `mhl-store-postgres` (Etapa 2):** `UPDATE runs SET state='claimed',
  holder=$1 WHERE id = (SELECT id FROM runs WHERE state='pending' ORDER BY
  priority, created_at FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING …`. Um
  round-trip, sem desperdício, prioridade nativa.
- `--state-dir` puro (sem extension store): **não** suporta claim entre réplicas
  (arquivo por id). O piloto é multi-réplica com **store CAS (Postgres)
  obrigatório**; `--state-dir` puro segue single-writer, documentado.

## 7. Reconcile loop (core, built-in)

Roda em toda réplica, intervalo com jitter (sem leader election — o claim é CAS,
trabalho dobrado é inofensivo). A cada ciclo:

- `claimed` mais velho que `claim_grace` sem lease vivo → volta a `pending`.
- `working` / `paused` com lease expirado e sem heartbeat → volta a `pending`
  (**default, at-least-once** — o replay é absorvido pela idempotência do destino,
  ver P1-8 e a chave `X-Idempotency-Key` do lab). Configurável para
  `failed + resumable` (at-most-once até `run/resume` explícito).
- Run terminal mais velho que TTL → GC.

**Dependência:** um `working` que voltou a `pending` e foi reclamado por outra
réplica, com a réplica antiga (zumbi) ainda gravando checkpoint, exige o
**fencing token de escrita** (P1-10, "próximo incremento"). É pré-requisito da
Etapa 2.

## 8. A camada de admissão (fora do runtime)

Sem estado. Autentica (papel do gateway) e, só sob sobrecarga, responde `429 +
Retry-After` — o cliente reenvia. Rate-limit / prioridade / quota como política de
deploy. Cabe num bloco `limit_req` do `nginx.conf` ou num sidecar mínimo. **Nunca
uma fila durável.** O admissor Python e a `admission_queue` deixam de existir.

## 9. Plano

Etapa 0 entra **antes de congelar a demo**. Etapas 1–2 são a **base do piloto** —
não são condicionadas a demanda; são o que torna o piloto possível.

### Etapa 0 — decisões de contrato (antes de congelar a demo; ~dias)

Não implementa a fila. Fixa em código + docs o que não pode ser retrofitado.
Progresso 06/09/2026:

- [x] Enum de estado do run inclui `pending` e `claimed`; `paused` já existe.
      `internal/mcpserver/runstate.go` — `RunState` + `IsTerminal` /
      `IsPreExecution` / `Valid`. `asyncRun.state` e `RunStatusRec.State`
      retipados; JSON no fio inalterado.
- [x] `run/start`: `runId` durável desde a chamada (já era o comportamento;
      comentário de contrato adicionado). `claim_id` nunca existiu na API do
      runtime. Documentado (`FLOW.md`, `Docs-Servers` tip INPUT LIMITS).
- [x] `run/status` / `run/logs` / `run/cancel` coerentes para um `runId` em
      `pending`/`claimed` (registro sintético — nada ainda gera esses estados).
      `runCancel` cobre qualquer estado pré-execução.
- [x] Assinatura `ClaimNext` na interface de store, com fallback CAS, sem
      consumidor. `internal/mcpserver/claim.go` — `ClaimNexter` (fast path) +
      `claimNextCAS` (scan CAS FIFO por `StartedAt`, `RunStatusRec.Holder`).
- [x] `run/start` valida inputs serializáveis (M-19) e o teto de 256 KiB
      (`checkRunInputs`, também nos `arguments` de `run/resume`).
- [x] Docs: `FLOW.md` (estados, contrato `runId`, seam `ClaimNext`),
      `Docs-Servers.dc.html` (tip INPUT LIMITS). Sem mudança de sintaxe/lint/LSP.

**Etapa 0 completa (06/09/2026).** Testes novos:
`internal/mcpserver/{runstate,claim}_test.go` (4). Suíte completa + vet +
`-race` mcpserver + functional 111/8 verdes.

### Etapa 1 — intake durável (base do piloto) — feita 06/09/2026

- [x] `run/start` grava `intakeRec` + status `pending` (redação P0-4, owner
      sempre persistido). Com store CAS, `run/start` **não lança** — retorna
      `state:"pending"` + `nudgeClaim()`; sem CAS, caminho em memória inalterado;
      falha de escrita → fallback `launch`.
- [x] claim loop por réplica (`claim.go`): `claimLoop`/`drainClaims` (nudge +
      ticker de 1s), `claimNextCAS` → `runForClaimed` (local ou reconstruído do
      `intakeRec`) → `runClaimed` (lease + `working` + `execRun`, `resume` se há
      checkpoint), limitado pelo semáforo. `failClaimed` para claim sem intake.
- [x] reconcile: `reconcileClaims` no ticker — `claimed` parado > 2×`runLockTTL`
      sem lease vivo (`peek` positivo) → CAS de volta a `pending`, `Holder` limpo.
      *Falta:* `working`/`paused` órfão → `pending` (dupla execução; Etapa 2).
- [x] `run/cancel` de `pending`/`claimed` → `canceled` sem executar (in-memory +
      status terminal + `RequestCancel` com CAS; caminho durável via
      `reconstructRun` do status `pending`).
- [x] `run/status` adota como remoto um run local reivindicado por outra réplica
      (`adoptIfClaimedElsewhere` via `RunStatusRec.Holder`); owner persistido no
      `run/start` barra não-dono no `reconstructRun`.

Testes novos: `internal/mcpserver/{runstate,claim,intake}_test.go` (8).
`go build`/`vet`/`go test ./...`/`-race` mcpserver+execsvc/functional 111/8 e
runlock+2-réplicas+resume verdes pelo novo caminho.

### Etapa 2 — gate do piloto com tráfego real

- [x] **Fechar R4/R5** (06/09/2026). R5: `leaseHandle` imutável de `acquire`,
      capturado por `execRun`, usado por `renew`/`release` — `runLock.tokens`
      removido. R4: `renew` limitado por `renewBudget` (5s) + watchdog
      independente em `heartbeatLock` que cancela o run se nenhum `renew` tiver
      sucesso dentro da janela segura. Testes migrados para a API de handle +
      `TestRunLock{StaleHandleCannotReleaseLaterAcquisition,RenewRespectsContextDeadline}`.
- [ ] Fencing token de escrita de checkpoint (P1-10) — zumbi que voltou a
      `pending`.
- [ ] `mhl-store-postgres.ClaimNext` com `SKIP LOCKED` + ordenação por prioridade.
- [ ] Métricas `mhl_serve_intake_*` (profundidade `pending`, latência de claim,
      contagem de reconcile).
- [ ] Spill de inputs grandes para blob.
- [ ] **Ensaio distribuído real:** N réplicas + Postgres real; matar pod em cada
      estado (`pending`, `claimed`, `working`, `paused`); confirmar conclusão
      efetiva única com a idempotência do destino — 0 perdidos, 0 duplicados
      além do replay idempotente.

### Fora de escopo imediato

- [ ] `architecture.html` refeito sem admissor / `admission_queue` (runtime ↔
      store CAS direto; rate-limit no nginx) — quando a Etapa 1 estabilizar.

## 10. Questões abertas

- **Prioridade entre `pending`:** FIFO por `created_at` basta na Etapa 1;
  prioridade real precisa de campo no record + `ClaimNext` ordenado (Etapa 2, já
  previsto no override PG).
- **Política de reconcile:** back-to-`pending` (at-least-once, exige idempotência
  no workflow) como default vs `failed + resumable`. Proposta: default
  configurável.
- **Owner na fase `pending`:** persistir o principal no `run/start`
  (`CheckpointStore.WriteOwner` já existe) e barrar `run/status`/`cancel` de
  quem não é dono.

## 11. Critérios de aceite

### Etapa 1

- `run/start` devolve `runId`; matar o processo imediatamente após; outro
  processo com o mesmo store executa o run até o fim.
- 3 réplicas, 100 `run/start` em rajada: cada run executa exatamente uma vez
  (0 perdidos; 0 duplicados além do replay idempotente).
- `run/cancel` em run `pending` → nunca executa.
- Matar réplica com run `claimed` (lease ainda não adquirido) → reconcile devolve
  a `pending` → outra réplica conclui.
- Sem mudança de sintaxe/lint/LSP — é superfície de servidor, não de linguagem.

### Etapa 2 (gate do piloto)

- Ensaio distribuído real acima, verde, contra Postgres real.
- R4/R5 fechados: `TestHeartbeatWatchdog…` e handle de aquisição por tentativa
  cobertos; `-race` limpo.
