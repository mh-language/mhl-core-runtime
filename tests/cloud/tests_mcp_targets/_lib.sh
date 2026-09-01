# _lib.sh — sourced por cada ITEM-*/run.sh de tests_mcp_targets.
#
# Estes cenários descrevem o COMPORTAMENTO-ALVO de um item em aberto do
# mhl-eks-design.html. Hoje eles FALHAM: cada asserção não satisfeita é
# registrada como PENDING com o que precisa ser implementado. Quando a
# correção landa, o cenário vira MET.
#
# Veredito por cenário:
#   MET      (exit 0)  — todas as asserções-alvo passam: a correção está pronta
#   PENDING  (exit 2)  — falta implementar (lista o que)
#   FAIL     (exit 1)  — o próprio script quebrou (bug no teste / ambiente)

set -u
HERE="$(cd "$(dirname "${BASH_SOURCE[1]}")" && pwd)"
# ROOT = tests/cloud — nearest ancestor holding docs-workflow.mh (works whether
# the scenario lives in ITEM-XX/ or _delegated/ITEM-XX/).
ROOT="$HERE"
while [[ "$ROOT" != "/" && ! -f "$ROOT/docs-workflow.mh" ]]; do ROOT="$(dirname "$ROOT")"; done
MHL="$HERE/mhl"
[[ -x "$MHL" ]] || { cp "$ROOT/mhl" "$MHL" 2>/dev/null && chmod +x "$MHL"; }
[[ -x "$MHL" ]] || { echo "FAIL: binário mhl não encontrado ($ROOT/mhl)"; echo FAIL; exit 1; }
command -v codesign >/dev/null && codesign --force --sign - "$MHL" 2>/dev/null || true

WORKFLOWS="$ROOT"                                 # docs-workflow.mh + slow-build.mh
RUNTIME_SRC="$(cd "$ROOT/../../src/mhl-runtime" 2>/dev/null && pwd || true)"
L="$HERE/logs"; mkdir -p "$L"
CLIENT_LOG="$L/client.log"; : > "$CLIENT_LOG"
TOKEN="mhl-target-$(basename "$HERE")-$(date +%s)"

log() { echo "[$(date '+%H:%M:%S')] $*" | tee -a "$CLIENT_LOG"; }

PEND=()
need() { # <descrição-do-que-falta>  — registra um item PENDING
  PEND+=("$1"); log "  PENDING: $1"
}
verdict() { # <mensagem-de-MET>
  if [[ ${#PEND[@]} -eq 0 ]]; then
    log "RESULTADO: MET — ${1:-comportamento-alvo satisfeito}"; echo MET; exit 0
  fi
  log "RESULTADO: PENDING — ${#PEND[@]} item(ns) a implementar (acima)"
  echo PENDING; exit 2
}

CLEAN_EXTRA=""
_pids=()
track() { _pids+=("$1"); }
cleanup() {
  [[ -n "$CLEAN_EXTRA" ]] && eval "$CLEAN_EXTRA" || true
  for p in "${_pids[@]:-}"; do [[ -n "$p" ]] && kill -KILL "$p" 2>/dev/null; done
  pkill -9 -f "$TOKEN" 2>/dev/null || true
  for d in "${_tmp[@]:-}"; do [[ -n "$d" && -d "$d" ]] && rm -rf "$d"; done
}
_tmp=()
mktmp() { local d; d="$(mktemp -d)"; _tmp+=("$d"); echo "$d"; }
trap cleanup EXIT

CURL=(curl -s --max-time 30)

# boot <addr> <base> <logfile> <state-dir> [extra serve args...] -> echo pid (rc0) | rc1
boot() {
  local addr="$1" base="$2" lf="$3" st="$4"; shift 4
  "$MHL" serve mcp --http --addr "$addr" --token "$TOKEN" --state-dir "$st" "$@" "$WORKFLOWS" >>"$lf" 2>&1 &
  local pid=$!
  for _ in $(seq 1 60); do
    kill -0 "$pid" 2>/dev/null || return 1
    [[ "$("${CURL[@]}" -o /dev/null -w '%{http_code}' "$base/healthz" 2>/dev/null)" == "200" ]] && { echo "$pid"; return 0; }
    sleep 0.2
  done
  kill -KILL "$pid" 2>/dev/null; return 1
}

initsid() { # <base> [principal] -> echo sid
  local hf; hf="$(mktemp)"
  "${CURL[@]}" -D "$hf" -o /dev/null -X POST "$1/mcp" -H 'content-type: application/json' \
    -H "Authorization: Bearer $TOKEN" ${2:+-H "X-Mhl-Principal: $2"} \
    -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{}}}'
  awk 'tolower($1)=="mcp-session-id:"{print $2}' "$hf" | tr -d '\r'; rm -f "$hf"
}
rpc() { # <base> <sid> <json> <out> [principal]
  "${CURL[@]}" -o "$4" -X POST "$1/mcp" -H 'content-type: application/json' \
    -H "Authorization: Bearer $TOKEN" -H "Mcp-Session-Id: $2" ${5:+-H "X-Mhl-Principal: $5"} -d "$3"; }
jget() { python3 -c 'import json,sys
try: d=json.load(open(sys.argv[1]))
except Exception: print(""); sys.exit()
for p in sys.argv[2].split("."):
  d=d.get(p,{}) if isinstance(d,dict) else {}
print("" if d=={} else d)' "$1" "$2"; }
