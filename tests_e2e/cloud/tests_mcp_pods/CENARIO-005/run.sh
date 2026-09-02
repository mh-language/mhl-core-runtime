#!/usr/bin/env bash
# CENARIO-005 — Autenticação por bearer e guarda de Origin
#
# - POST /mcp sem bearer / bearer errado -> 401 ; bearer correto -> 200
# - /healthz e /metrics -> 200 sem bearer
# - POST /mcp com Origin cross-site nao-loopback -> 403 "forbidden origin"
# - subir em 0.0.0.0 sem --token -> aviso "endpoint is unauthenticated" no stderr
#
# O script copia tests/cloud/mhl para esta pasta se necessário e usa-o.

set -u

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MHL="$HERE/mhl"
[[ -x "$MHL" ]] || { cp "$HERE/../../mhl" "$MHL" 2>/dev/null && chmod +x "$MHL"; }
[[ -x "$MHL" ]] || { echo "FALHA: binário mhl não encontrado"; exit 1; }

WORKFLOWS="$HERE/../.."
ADDR="127.0.0.1:8722"
ADDR2="0.0.0.0:8723"            # segundo processo, sem --token
BASE="http://$ADDR"
TOKEN="cenario-005-$(date +%s)"
STATE="$(mktemp -d)"; STATE2="$(mktemp -d)"

L="$HERE/logs"; mkdir -p "$L"
SERVER_LOG="$L/mcp-server.log";        : > "$SERVER_LOG"
SERVER_LOG2="$L/mcp-server-notoken.log"; : > "$SERVER_LOG2"
CLIENT_LOG="$L/client.log";            : > "$CLIENT_LOG"
MATRIX="$L/auth-matrix.txt";           : > "$MATRIX"

log() { echo "[$(date '+%H:%M:%S')] $*" | tee -a "$CLIENT_LOG"; }

cleanup() {
  for p in "${SERVER_PID:-}" "${SERVER_PID2:-}"; do
    [[ -n "$p" ]] && kill -0 "$p" 2>/dev/null && { kill -TERM "$p" 2>/dev/null; wait "$p" 2>/dev/null; }
  done
  rm -rf "$STATE" "$STATE2"
}
trap cleanup EXIT

INIT_BODY='{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{}}}'

# ── servidor principal, COM --token ────────────────────────────────────────
log "iniciando servidor COM --token em $ADDR"
"$MHL" serve mcp --http --addr "$ADDR" --token "$TOKEN" --state-dir "$STATE" \
  "$WORKFLOWS" >>"$SERVER_LOG" 2>&1 &
SERVER_PID=$!

ready=""
for i in $(seq 1 50); do
  kill -0 "$SERVER_PID" 2>/dev/null || { log "FALHA: servidor terminou"; break; }
  [[ "$(curl -s -o /dev/null -w '%{http_code}' "$BASE/healthz")" == "200" ]] && { ready="yes"; break; }
  sleep 0.2
done
[[ "$ready" == "yes" ]] || { log "RESULTADO: NÃO FUNCIONOU — servidor não ficou pronto"; echo "FAIL"; exit 1; }

row() { printf '%-42s -> HTTP %s\n' "$1" "$2" | tee -a "$MATRIX" "$CLIENT_LOG" >/dev/null; }

# ── matriz de credenciais ─────────────────────────────────────────────────
C1=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/mcp" -H 'content-type: application/json' -d "$INIT_BODY")
row "POST /mcp  sem Authorization" "$C1"

C2=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/mcp" -H 'content-type: application/json' -H "Authorization: Bearer errado-$RANDOM" -d "$INIT_BODY")
row "POST /mcp  bearer ERRADO" "$C2"

C3=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/mcp" -H 'content-type: application/json' -H "Authorization: Bearer $TOKEN" -d "$INIT_BODY")
row "POST /mcp  bearer CORRETO" "$C3"

C4=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/healthz")
row "GET  /healthz  sem Authorization" "$C4"
C5=$(curl -s -o /dev/null -w '%{http_code}' "$BASE/metrics")
row "GET  /metrics  sem Authorization" "$C5"

# ── guarda de Origin ─────────────────────────────────────────────────────
C6=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/mcp" -H 'content-type: application/json' -H "Authorization: Bearer $TOKEN" -H "Origin: http://evil.example" -d "$INIT_BODY")
row "POST /mcp  Origin: http://evil.example" "$C6"

C7=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$BASE/mcp" -H 'content-type: application/json' -H "Authorization: Bearer $TOKEN" -H "Origin: http://localhost" -d "$INIT_BODY")
row "POST /mcp  Origin: http://localhost" "$C7"

log "matriz:"; cat "$MATRIX" | tee -a "$CLIENT_LOG"

# ── servidor secundário SEM --token em bind não-loopback ─────────────────
log "iniciando 2º servidor SEM --token em $ADDR2 (espera aviso no stderr)"
"$MHL" serve mcp --http --addr "$ADDR2" --state-dir "$STATE2" \
  "$WORKFLOWS" >>"$SERVER_LOG2" 2>&1 &
SERVER_PID2=$!
sleep 1
WARN=""
grep -qi "endpoint is unauthenticated" "$SERVER_LOG2" && WARN="yes"
log "aviso 'endpoint is unauthenticated' no stderr do 2º servidor? ${WARN:-nao}"
grep -i "unauthenticated\|warning" "$SERVER_LOG2" | tee -a "$CLIENT_LOG" || true

# ── verdite ─────────────────────────────────────────────────────────────
ok="yes"
[[ "$C1" == "401" ]] || { ok="no"; log "  ! sem bearer != 401"; }
[[ "$C2" == "401" ]] || { ok="no"; log "  ! bearer errado != 401"; }
[[ "$C3" == "200" ]] || { ok="no"; log "  ! bearer correto != 200"; }
[[ "$C4" == "200" ]] || { ok="no"; log "  ! /healthz sem bearer != 200"; }
[[ "$C5" == "200" ]] || { ok="no"; log "  ! /metrics sem bearer != 200"; }
[[ "$C6" == "403" ]] || { ok="no"; log "  ! Origin cross-site != 403"; }
[[ "$C7" == "200" ]] || { ok="no"; log "  ! Origin loopback != 200"; }
[[ "$WARN" == "yes" ]] || { ok="no"; log "  ! aviso de endpoint não autenticado ausente"; }

if [[ "$ok" == "yes" ]]; then
  log "RESULTADO: FUNCIONOU — bearer exigido no /mcp, probes livres, Origin cross-site 403, aviso sem token"
  echo "PASS"; exit 0
else
  log "RESULTADO: NÃO FUNCIONOU — veja as verificações acima"
  echo "FAIL"; exit 1
fi
