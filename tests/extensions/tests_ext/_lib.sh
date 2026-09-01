# _lib.sh — sourced by each CENARIO-*/run.sh in tests_ext.
#
# Provides: log / die / ok / skip, ensure_env (build+sign store-probe and mhl),
# new_project (mktemp scratch dir + `mhl extension install` store-probe + cd).
# For the S3 scenarios: s3_ensure (build mhl-store-s3 + bring up local MinIO,
# SKIPping when Docker is unusable), s3_project, s3_mc, s3_wipe.
# For the Postgres store scenarios: pg_ensure, pg_project, pg_psql, pg_wipe.
# For the Postgres DQL scenarios: sqlpg_ensure, sqlpg_project, sqlpg_psql.
# For the Redis cache scenarios: redis_ensure, redis_project, redis_cli, redis_flush.
# Sets HERE (scenario dir), ROOT (tests/extensions), MHL, PROBE_SRC,
# S3_SRC, PG_SRC, SQLPG_SRC, REDIS_SRC, L.

set -u

HERE="$(cd "$(dirname "${BASH_SOURCE[1]}")" && pwd)"
ROOT="$(cd "$HERE/../.." && pwd)"          # tests/extensions
MHL="$ROOT/mhl"
PROBE_SRC="$ROOT/store-probe"
S3_SRC="$(cd "$ROOT/../../src/mhl-extensions/mhl-store-s3" 2>/dev/null && pwd || true)"
S3_COMPOSE="${S3_SRC:+$S3_SRC/docker-compose.yml}"
S3_ENDPOINT="${S3_ENDPOINT:-http://localhost:9000}"
S3_BUCKET="${S3_BUCKET:-mhl-state}"
PG_SRC="$(cd "$ROOT/../../src/mhl-extensions/mhl-store-postgres" 2>/dev/null && pwd || true)"
PG_COMPOSE="${PG_SRC:+$PG_SRC/docker-compose.yml}"
PG_DSN="${PG_DSN:-postgres://mhl:mhl-secret-pw@localhost:5433/mhl_state?sslmode=disable}"
SQLPG_SRC="$(cd "$ROOT/../../src/mhl-extensions/mhl-sql-postgres" 2>/dev/null && pwd || true)"
SQLPG_COMPOSE="${SQLPG_SRC:+$SQLPG_SRC/docker-compose.yml}"
SQL_PG_DSN="${SQL_PG_DSN:-postgres://mhl:mhl-secret-pw@localhost:5434/demo?sslmode=disable}"
REDIS_SRC="$(cd "$ROOT/../../src/mhl-extensions/mhl-cache-redis" 2>/dev/null && pwd || true)"
REDIS_COMPOSE="${REDIS_SRC:+$REDIS_SRC/docker-compose.yml}"
REDIS_URL="${REDIS_URL:-redis://localhost:6380/0}"

L="$HERE/logs"; mkdir -p "$L"
CLIENT_LOG="$L/client.log"; : > "$CLIENT_LOG"

log() { echo "[$(date '+%H:%M:%S')] $*" | tee -a "$CLIENT_LOG"; }
die() { log "RESULTADO: NÃO FUNCIONOU — $*"; echo "FAIL"; exit 1; }
ok()  { log "RESULTADO: FUNCIONOU — $*"; echo "PASS"; exit 0; }
skip() { log "RESULTADO: PULADO — $*"; echo "SKIP"; exit 0; }

PROJECTS=()
_atexit_extra=""
cleanup_all() {
  [[ -n "$_atexit_extra" ]] && eval "$_atexit_extra" || true
  for p in "${PROJECTS[@]:-}"; do [[ -n "$p" && -d "$p" ]] && rm -rf "$p"; done
}
trap cleanup_all EXIT
# a scenario that spawns a server registers its kill here:
#   _atexit_extra='kill -TERM $SRV 2>/dev/null; wait $SRV 2>/dev/null'

mhl_ready() {
  [[ -x "$MHL" ]] || die "binário mhl ausente em $MHL — rode: cp tests/cloud/mhl tests/extensions/mhl"
  command -v codesign >/dev/null && codesign --force --sign - "$MHL" 2>/dev/null || true
}

ensure_env() {
  command -v go >/dev/null || die "go não encontrado no PATH (necessário para compilar store-probe)"
  if [[ ! -x "$PROBE_SRC/bin/store-probe" || "$PROBE_SRC/main.go" -nt "$PROBE_SRC/bin/store-probe" ]]; then
    ( cd "$PROBE_SRC" && go build -o bin/store-probe . ) || die "build de store-probe falhou"
  fi
  command -v codesign >/dev/null && codesign --force --sign - "$PROBE_SRC/bin/store-probe" 2>/dev/null || true
  mhl_ready
}

# s3_ensure — build mhl-store-s3, bring up local MinIO + bucket. SKIPs the
# scenario (exit 0, not a failure) when Docker is unusable. Exports the
# AWS_*/S3_* the scenario `.mh` reads through env().
s3_ensure() {
  command -v go >/dev/null || die "go não encontrado no PATH"
  [[ -n "$S3_SRC" && -f "$S3_COMPOSE" ]] || skip "src/mhl-extensions/mhl-store-s3 não encontrado"
  mhl_ready
  local bin="$S3_SRC/bin/mhl-store-s3"
  if [[ ! -x "$bin" || "$S3_SRC/main.go" -nt "$bin" || "$S3_SRC/s3.go" -nt "$bin" ]]; then
    ( cd "$S3_SRC" && go build -o bin/mhl-store-s3 . ) || die "build de mhl-store-s3 falhou"
  fi
  command -v codesign >/dev/null && codesign --force --sign - "$bin" 2>/dev/null || true

  command -v docker >/dev/null || skip "docker não encontrado — cenário S3 requer MinIO"
  docker info >/dev/null 2>&1   || skip "daemon do docker indisponível — cenário S3 requer MinIO"
  log "subindo MinIO ($S3_COMPOSE)"
  docker compose -f "$S3_COMPOSE" up -d minio            >>"$CLIENT_LOG" 2>&1 || skip "falha ao subir o MinIO"
  docker compose -f "$S3_COMPOSE" run --rm createbucket  >>"$CLIENT_LOG" 2>&1 || skip "falha ao criar o bucket no MinIO"

  export AWS_ACCESS_KEY_ID="${AWS_ACCESS_KEY_ID:-mhl}"
  export AWS_SECRET_ACCESS_KEY="${AWS_SECRET_ACCESS_KEY:-mhl-secret-key}"
  export S3_ENDPOINT S3_BUCKET
}

# s3_mc <mc-command...> — run `mc` inside the compose network (alias `local`).
s3_mc() {
  docker compose -f "$S3_COMPOSE" run --rm -T --entrypoint sh createbucket -c \
    "mc alias set local http://minio:9000 mhl mhl-secret-key >/dev/null 2>&1 && $*"
}

# s3_wipe <prefix> — best-effort delete of everything under bucket/<prefix>.
s3_wipe() {
  s3_mc "mc rm --recursive --force local/$S3_BUCKET/$1 >/dev/null 2>&1; true" >/dev/null 2>&1 || true
}

# pg_ensure — build mhl-store-postgres, bring up local Postgres. SKIPs the
# scenario (exit 0, not a failure) when Docker is unusable. Exports PG_DSN the
# scenario `.mh` reads through env().
pg_ensure() {
  command -v go >/dev/null || die "go não encontrado no PATH"
  [[ -n "$PG_SRC" && -f "$PG_COMPOSE" ]] || skip "src/mhl-extensions/mhl-store-postgres não encontrado"
  mhl_ready
  local bin="$PG_SRC/bin/mhl-store-postgres"
  if [[ ! -x "$bin" || "$PG_SRC/main.go" -nt "$bin" || "$PG_SRC/pg.go" -nt "$bin" ]]; then
    ( cd "$PG_SRC" && go build -o bin/mhl-store-postgres . ) || die "build de mhl-store-postgres falhou"
  fi
  command -v codesign >/dev/null && codesign --force --sign - "$bin" 2>/dev/null || true

  command -v docker >/dev/null || skip "docker não encontrado — cenário Postgres requer o banco"
  docker info >/dev/null 2>&1   || skip "daemon do docker indisponível — cenário Postgres requer o banco"
  log "subindo Postgres ($PG_COMPOSE)"
  docker compose -f "$PG_COMPOSE" up -d --wait >>"$CLIENT_LOG" 2>&1 || skip "falha ao subir o Postgres"
  export PG_DSN
}

# pg_psql <sql> — run psql inside the compose network, tuples-only.
pg_psql() {
  docker compose -f "$PG_COMPOSE" exec -T postgres \
    psql -U mhl -d mhl_state -tAqc "$*"
}

# pg_wipe [table] — TRUNCATE the store table (best effort).
pg_wipe() {
  pg_psql "TRUNCATE TABLE ${1:-mhl_store}" >/dev/null 2>&1 || true
}

# sqlpg_ensure — build mhl-sql-postgres, bring up the seeded Postgres (:5434).
# SKIPs (exit 0) when Docker is unusable. Exports SQL_PG_DSN.
sqlpg_ensure() {
  command -v go >/dev/null || die "go não encontrado no PATH"
  [[ -n "$SQLPG_SRC" && -f "$SQLPG_COMPOSE" ]] || skip "src/mhl-extensions/mhl-sql-postgres não encontrado"
  mhl_ready
  local bin="$SQLPG_SRC/bin/mhl-sql-postgres"
  if [[ ! -x "$bin" || "$SQLPG_SRC/main.go" -nt "$bin" || "$SQLPG_SRC/sql.go" -nt "$bin" ]]; then
    ( cd "$SQLPG_SRC" && go build -o bin/mhl-sql-postgres . ) || die "build de mhl-sql-postgres falhou"
  fi
  command -v codesign >/dev/null && codesign --force --sign - "$bin" 2>/dev/null || true

  command -v docker >/dev/null || skip "docker não encontrado — cenário sql-postgres requer o banco"
  docker info >/dev/null 2>&1   || skip "daemon do docker indisponível"
  log "subindo Postgres semeado ($SQLPG_COMPOSE)"
  docker compose -f "$SQLPG_COMPOSE" up -d --wait >>"$CLIENT_LOG" 2>&1 || skip "falha ao subir o Postgres semeado"
  export SQL_PG_DSN
}

# sqlpg_psql <sql> — psql tuples-only na rede do compose (db `demo`).
sqlpg_psql() {
  docker compose -f "$SQLPG_COMPOSE" exec -T postgres \
    psql -U mhl -d demo -tAqc "$*"
}

# sqlpg_project [name] -> instala mhl-sql-postgres num projeto scratch
sqlpg_project() { _new_project "$SQLPG_SRC" "sql-postgres" "${1:-}"; }

# redis_ensure — build mhl-cache-redis, bring up local Redis (:6380). SKIPs
# (exit 0) when Docker is unusable. Exports REDIS_URL.
redis_ensure() {
  command -v go >/dev/null || die "go não encontrado no PATH"
  [[ -n "$REDIS_SRC" && -f "$REDIS_COMPOSE" ]] || skip "src/mhl-extensions/mhl-cache-redis não encontrado"
  mhl_ready
  local bin="$REDIS_SRC/bin/mhl-cache-redis"
  if [[ ! -x "$bin" || "$REDIS_SRC/main.go" -nt "$bin" || "$REDIS_SRC/resp.go" -nt "$bin" || "$REDIS_SRC/cache.go" -nt "$bin" ]]; then
    ( cd "$REDIS_SRC" && go build -o bin/mhl-cache-redis . ) || die "build de mhl-cache-redis falhou"
  fi
  command -v codesign >/dev/null && codesign --force --sign - "$bin" 2>/dev/null || true

  command -v docker >/dev/null || skip "docker não encontrado — cenário Redis requer o servidor"
  docker info >/dev/null 2>&1   || skip "daemon do docker indisponível"
  log "subindo Redis ($REDIS_COMPOSE)"
  docker compose -f "$REDIS_COMPOSE" up -d --wait >>"$CLIENT_LOG" 2>&1 || skip "falha ao subir o Redis"
  export REDIS_URL
}

# redis_cli <args...> — redis-cli dentro do container.
redis_cli() {
  docker compose -f "$REDIS_COMPOSE" exec -T redis redis-cli "$@"
}

# redis_flush — apaga o db de teste (best effort).
redis_flush() { redis_cli FLUSHDB >/dev/null 2>&1 || true; }

# redis_project [name] -> instala mhl-cache-redis num projeto scratch
redis_project() { _new_project "$REDIS_SRC" "cache-redis" "${1:-}"; }

# new_project [name] -> exports PROJ, cd's into it, installs store-probe there
new_project() { _new_project "$PROBE_SRC" "store-probe" "${1:-}"; }

# s3_project [name] -> same, installing mhl-store-s3 instead
s3_project() { _new_project "$S3_SRC" "store-s3" "${1:-}"; }

# pg_project [name] -> same, installing mhl-store-postgres instead
pg_project() { _new_project "$PG_SRC" "store-postgres" "${1:-}"; }

_new_project() { # <ext-src-dir> <label> <name>
  local src="$1" label="$2" name="${3:-}"
  local pj; pj="$(mktemp -d)"
  PROJ="$pj"; PROJECTS+=("$pj")
  ( cd "$pj" && "$MHL" extension install "$src" ) > "$L/extension-install-${label}${name:+-$name}.log" 2>&1 \
    || { cat "$L/extension-install-${label}${name:+-$name}.log" >> "$CLIENT_LOG"; die "mhl extension install ($label) falhou"; }
  cd "$pj"
}

# jq-free JSON field reader: jget <file> <dotted.path>
jget() {
  python3 -c 'import json,sys
try: d=json.load(open(sys.argv[1]))
except Exception: print(""); sys.exit(0)
for p in sys.argv[2].split("."):
  d = d.get(p, {}) if isinstance(d, dict) else {}
print("" if d == {} else d)' "$1" "$2"
}
