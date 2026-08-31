#!/usr/bin/env bash
# CENARIO-004 — Probes de saúde (liveness / readiness)
#
# Dado que o servidor MCP está em execução
# Quando um probe faz GET /healthz e GET /readyz sem bearer
# Então /healthz -> 200 "ok", /readyz -> 200 "ready", /metrics -> 200
# E POST /mcp sem bearer -> 401
#
# O script copia sample/cloud/mhl para esta pasta se necessário e usa-o.

set -u

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MHL="$HERE/mhl"
[[ -x "$MHL" ]] || { cp "$HERE/../../mhl" "$MHL" 2>/dev/null && chmod +x "$MHL"; }
[[ -x "$MHL" ]] || { echo "FALHA: binário mhl não encontrado ($HERE/mhl nem sample/cloud/mhl)"; exit 1; }

WORKFLOWS="$HERE/../.."          # sample/cloud (docs-workflow.mh / slow-build.mh)
ADDR="127.0.0.1:8721"
BASE="http://$ADDR"
TOKEN="cenario-004-$(date +%s)"
STATE="$(mktemp -d)"

L="$HERE/logs"; mkdir -p "$L"
SERVER_LOG="$L/mcp-server.log"; : > "$SERVER_LOG"
CLIENT_LOG="$L/client.log";     : > "$CLIENT_LOG"
PROBES="$L/probes.txt";         : > "$PROBES"

log() { echo "[$(date '+%H:%M:%S')] $*" | tee -a "$CLIENT_LOG"; }

cleanup() {
  if [[ -n "${SERVER_PID:-}" ]] && kill -0 "$SERVER_PID" 2>/dev/null; then
    kill -TERM "$SERVER_PID" 2>/dev/null; wait "$SERVER_PID" 2>/dev/null
  fi
  rm -rf "$STATE"
}
trap cleanup EXIT

# ── Dado que o servidor MCP está em execução ─────────────────────────────────
log "iniciando: $MHL serve mcp --http --addr $ADDR --token <gerado>"
"$MHL" serve mcp --http --addr "$ADDR" --token "$TOKEN" --state-dir "$STATE" \
  "$WORKFLOWS" >>"$SERVER_LOG" 2>&1 &
SERVER_PID=$!
log "servidor PID=$SERVER_PID"

ready=""
for i in $(seq 1 50); do
  kill -0 "$SERVER_PID" 2>/dev/null || { log "FALHA: processo do servidor terminou"; break; }
  [[ "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/healthz")" == "200" ]] && { ready="yes"; break; }
  sleep 0.2
done
[[ "$ready" == "yes" ]] || { log "RESULTADO: NÃO FUNCIONOU — servidor não ficou pronto"; echo "FAIL"; exit 1; }

# ── Quando um probe consulta os endpoints operacionais SEM bearer ───────────
probe() { # nome  url
  local name="$1" url="$2" code body
  body=$(curl -s -w '\n%{http_code}' "$url")
  code=$(printf '%s\n' "$body" | tail -1)
  body=$(printf '%s\n' "$body" | sed '$d' | head -1)
  printf '%-26s -> HTTP %s   %s\n' "$name" "$code" "$body" | tee -a "$PROBES" "$CLIENT_LOG" >/dev/null
  echo "$code"
}

log "probes sem Authorization:"
HZ=$(probe "GET /healthz (no auth)" "$BASE/healthz")
RZ=$(probe "GET /readyz  (no auth)" "$BASE/readyz")
MZ=$(probe "GET /metrics (no auth)" "$BASE/metrics")

# controle: /mcp continua exigindo bearer
MCP_NOAUTH=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/mcp" \
  -H 'content-type: application/json' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{}}}')
log "POST /mcp (no auth) -> HTTP $MCP_NOAUTH (esperado 401)"

# corpo textual dos probes p/ evidência
{ echo "--- /healthz ---"; curl -s "$BASE/healthz";
  echo "--- /readyz ---";  curl -s "$BASE/readyz";
  echo "--- /metrics (head) ---"; curl -s "$BASE/metrics" | head -20; } >> "$PROBES"

# ── Então ... ──────────────────────────────────────────────────────────────
ok="yes"
[[ "$HZ" == "200" ]]         || { ok="no"; log "  ! /healthz != 200"; }
[[ "$RZ" == "200" ]]         || { ok="no"; log "  ! /readyz != 200"; }
[[ "$MZ" == "200" ]]         || { ok="no"; log "  ! /metrics != 200 sem bearer"; }
[[ "$MCP_NOAUTH" == "401" ]] || { ok="no"; log "  ! POST /mcp sem bearer != 401"; }
curl -s "$BASE/healthz" | grep -q "ok"        || { ok="no"; log "  ! corpo de /healthz != ok"; }
curl -s "$BASE/readyz"  | grep -q "ready"      || { ok="no"; log "  ! corpo de /readyz != ready"; }
curl -s "$BASE/metrics" | grep -q "mhl_serve_" || { ok="no"; log "  ! /metrics sem métricas mhl_serve_*"; }

if [[ "$ok" == "yes" ]]; then
  log "RESULTADO: FUNCIONOU — /healthz e /readyz 200 sem auth; /metrics 200 sem auth; /mcp 401 sem auth"
  echo "PASS"; exit 0
else
  log "RESULTADO: NÃO FUNCIONOU — veja as verificações acima"
  echo "FAIL"; exit 1
fi
