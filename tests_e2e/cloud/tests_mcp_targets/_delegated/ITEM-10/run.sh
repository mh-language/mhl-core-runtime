#!/usr/bin/env bash
# ITEM-10 (alvo) — auth por principal (token-file/escopos ou JWT/JWKS)
source "$(dirname "${BASH_SOURCE[0]}")/../../_lib.sh"

STATE="$(mktmp)"; ADDR="127.0.0.1:8768"; BASE="http://$ADDR"
TF="$(mktmp)/tokens.json"
cat > "$TF" <<'EOF'
{
  "tokA":  { "principal": "team-a",  "scopes": ["run"] },
  "tokRO": { "principal": "auditor", "scopes": ["read"] }
}
EOF

FLAGS_OUT="$L/flags.out"; : > "$FLAGS_OUT"
MODE=""; PID=""
# tenta --token-file
"$MHL" serve mcp --http --addr "$ADDR" --token-file "$TF" --state-dir "$STATE" "$WORKFLOWS" >>"$FLAGS_OUT" 2>&1 &
p=$!
for _ in $(seq 1 20); do kill -0 "$p" 2>/dev/null || break; [[ "$("${CURL[@]}" -o /dev/null -w '%{http_code}' "$BASE/healthz" 2>/dev/null)" == "200" ]] && { MODE="token-file"; PID=$p; break; }; sleep 0.2; done
[[ -z "$MODE" ]] && { kill -KILL "$p" 2>/dev/null; wait "$p" 2>/dev/null; }

if [[ -z "$MODE" ]]; then
  # tenta --jwks-url (dummy) só p/ ver se a flag existe
  "$MHL" serve mcp --http --addr "$ADDR" --jwks-url "http://127.0.0.1:1/jwks.json" --token "$TOKEN" --state-dir "$STATE" "$WORKFLOWS" >>"$FLAGS_OUT" 2>&1 &
  p=$!; for _ in $(seq 1 15); do kill -0 "$p" 2>/dev/null || break; [[ "$("${CURL[@]}" -o /dev/null -w '%{http_code}' "$BASE/healthz" 2>/dev/null)" == "200" ]] && { MODE="jwks"; PID=$p; break; }; sleep 0.2; done
  [[ -z "$MODE" ]] && { kill -KILL "$p" 2>/dev/null; wait "$p" 2>/dev/null; }
fi

if [[ -z "$MODE" ]]; then
  need "o servidor deve aceitar --token-file (tokens→principal→escopos) ou --jwks-url (validação de JWT)"
  grep -RniE 'token-file|jwks|scopes|fileVerifier|jwksVerifier' "$RUNTIME_SRC/internal/mcpserver/verifier.go" "$RUNTIME_SRC/internal/cli/serve.go" >>"$FLAGS_OUT" 2>&1 || echo "(nenhuma menção a token-file/jwks/scopes no verifier/serve)" >>"$FLAGS_OUT"
  verdict ""   # PENDING: nem dá p/ testar o resto
fi
track "$PID"
log "servidor no modo: $MODE"

# se token-file: testa credenciais distintas + escopo + rotação
if [[ "$MODE" == "token-file" ]]; then
  code() { "${CURL[@]}" -o "$2" -w '%{http_code}' -X POST "$BASE/mcp" -H 'content-type: application/json' -H "Authorization: Bearer $1" -d "$3"; }
  INIT='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{}}}'
  A_CODE=$(code tokA "$L/teamA.txt" "$INIT"); RO_CODE=$(code tokRO "$L/teamRO.txt" "$INIT")
  log "  tokA initialize=$A_CODE ; tokRO initialize=$RO_CODE"
  [[ "$A_CODE" == 200 && "$RO_CODE" == 200 ]] || need "tokA e tokRO (credenciais distintas) devem autenticar (got $A_CODE / $RO_CODE)"

  ASID=$(awk 'tolower($1)=="mcp-session-id:"{print $2}' <(code tokA /dev/null "$INIT" >/dev/null; :) 2>/dev/null)
  # re-init capturando header
  hf="$(mktemp)"; "${CURL[@]}" -D "$hf" -o /dev/null -X POST "$BASE/mcp" -H 'content-type: application/json' -H "Authorization: Bearer tokRO" -d "$INIT"
  ROSID=$(awk 'tolower($1)=="mcp-session-id:"{print $2}' "$hf" | tr -d '\r'); rm -f "$hf"
  "${CURL[@]}" -o "$L/teamRO-start.json" -X POST "$BASE/mcp" -H 'content-type: application/json' \
    -H "Authorization: Bearer tokRO" -H "Mcp-Session-Id: $ROSID" \
    -d '{"jsonrpc":"2.0","id":2,"method":"run/start","params":{"name":"DocPipeline","arguments":{"repo":"r","approved":"no"}}}'
  RO_START_ERR=$(jget "$L/teamRO-start.json" error.code)
  RO_START_RID=$(jget "$L/teamRO-start.json" result.runId)
  log "  tokRO (escopo read) run/start -> err='$RO_START_ERR' runId='$RO_START_RID'"
  [[ -n "$RO_START_ERR" && -z "$RO_START_RID" ]] || need "um principal de escopo 'read' NÃO deve conseguir run/start (hoje: err='$RO_START_ERR')"

  # rotação: remove tokA, SIGHUP, tokA deve dar 401
  echo '{ "tokRO": { "principal": "auditor", "scopes": ["read"] } }' > "$TF"
  kill -HUP "$PID" 2>/dev/null; sleep 1
  ROT=$(code tokA "$L/rotated.txt" "$INIT")
  log "  após remover tokA + SIGHUP: tokA initialize -> $ROT"
  [[ "$ROT" == 401 ]] || need "revogar um token no --token-file + reload deve invalidá-lo sem restart (hoje: $ROT)"
fi

if [[ "$MODE" == "jwks" ]]; then
  need "modo jwks detectado, mas a suíte ainda não gera um JWT de teste — completar as asserções de assinatura/exp"
fi

verdict "auth por principal: credenciais distintas, escopos e rotação sem restart"
