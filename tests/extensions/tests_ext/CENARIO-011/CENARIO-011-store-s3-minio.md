# Cenário 011: `mhl-store-s3` — carregar de um `.mh` e as 4 operações contra o MinIO

**Objetivo:** Verificar que a extensão **oficial** `src/mhl-extensions/mhl-store-s3` (kind
`store`, backend Amazon S3 / MinIO) é carregada por um `.mh` e que `get` / `put`
/ `delete` / `list` fazem round-trip real contra um bucket S3 — confirmado
inspecionando os objetos no bucket com `mc`.

```gherkin
Dado um MinIO local (docker compose de src/mhl-extensions/mhl-store-s3) e o bucket mhl-state
E mhl-store-s3 instalado no projeto (mhl extension install)
E um .mh declara "extension store S { bucket, endpoint, region, access_key_id, secret_access_key, prefix, log }"
  com as credenciais vindas de env()
Quando um step faz S.put("run/demo/checkpoint/DocPipeline","gate") e S.put("session/sess-1", 7)
E outro step faz S.get da chave (hit) e de uma ausente (miss)
E steps fazem list("run/"), delete (2x, idempotente) e list("run/") de novo
Então get retorna "gate"; o miss retorna null
E list("run/") antes = ["run/demo/checkpoint/DocPipeline"], depois do delete = []
E `mc ls` mostra o objeto <prefix>session/sess-1.json no bucket
E o objeto deletado não está mais no bucket
E a wire trace (log jsonl) tem "ev":"init" e "op":"put"
E "mhl extension doctor" sai 0
```

**Resultado Esperado:**
- `mhl extension doctor` sai `0`.
- Linhas de saída, em ordem: `gate` / `null` / `["run/demo/checkpoint/DocPipeline"]` / `[]`.
- `mc ls --recursive local/mhl-state/c011/` inclui `c011/session/sess-1.json` e
  **não** inclui `c011/run/demo/checkpoint/DocPipeline.json`.
- `logs/wire.jsonl`: `"ev":"init"` + linhas `"ev":"call"` (put/get/list/delete).

**Resultado Real:**
- [ ] funcionou
- [ ] não funcionou

### Evidências:
- [ ] `logs/run.out` — saída de `mhl run`
- [ ] `logs/wire.jsonl` — trace da extensão
- [ ] `logs/objects.txt` / `logs/mc-ls.txt` — objetos no bucket
- [ ] `logs/doctor.out`

### Observações:
- Diferente de `store-fs`/`store-probe` (FS local), este cenário exige um
  endpoint S3 alcançável: `s3_ensure` sobe o MinIO via `docker compose` e
  **PULA** o cenário (SKIP, não FAIL) se o Docker não estiver disponível.
- Credenciais e `bucket`/`endpoint` chegam por `env()` — resolvidos host-side
  pelo runtime (e registrados para redação); o processo da extensão não herda
  ambiente.
- SigV4 é assinado pela própria extensão (sem dependências); o teste unitário
  `src/mhl-extensions/mhl-store-s3/s3_test.go` fixa a assinatura no vetor documentado da AWS.

**Executado em:** [Data e hora do teste]

## Como executar

```sh
./run.sh
```
