#!/usr/bin/env bash
# CENARIO-020 — mhl-cache-redis: carregar de um .mh e as operações de cache
source "$(dirname "${BASH_SOURCE[0]}")/../_lib.sh"

redis_ensure
redis_flush
redis_project

PFX="c020:"
PROBE="$PROJ/wire.jsonl"
cat > main.mh <<EOF
extension cache C {
    url: env("REDIS_URL")
    key_prefix: "$PFX"
    ttl: "1h"
    log: "$PROBE"
}

pipeline Ops {
    step miss {
        var a = C.get("user:1")
        log(a)
    }
    step put {
        C.set("user:1", { "name": "Ana", "score": 91 })
        var b = C.get("user:1")
        log(b)
        var present = C.has("user:1")
        log(present)
    }
    step counters {
        var n1 = C.incr("hits")
        var n2 = C.incrBy("hits", 4)
        log(n1)
        log(n2)
    }
    step remove {
        C.delete("user:1")
        C.delete("user:1")
        var gone = C.has("user:1")
        log(gone)
    }
}
EOF
cp main.mh "$L/main.mh"

log "mhl extension doctor"
"$MHL" extension doctor > "$L/doctor.out" 2>&1 || { cat "$L/doctor.out" >> "$CLIENT_LOG"; die "extension doctor != 0"; }

log "mhl run main.mh"
"$MHL" run main.mh > "$L/run.out" 2>&1 || { cat "$L/run.out" >> "$CLIENT_LOG"; die "mhl run falhou"; }
cat "$L/run.out" | tee -a "$CLIENT_LOG"
cp "$PROBE" "$L/wire.jsonl" 2>/dev/null || true

OUT=()
while IFS= read -r _l; do OUT+=("$_l"); done < <(grep -vE '^(session:|step:|executed )' "$L/run.out")
log "linhas: ${OUT[*]}"

EX_USER=$(redis_cli EXISTS "${PFX}user:1" | tr -d '\r')
GET_HITS=$(redis_cli GET "${PFX}hits" | tr -d '\r')
redis_cli --scan --pattern "${PFX}*" > "$L/redis-dump.txt" 2>&1 || true
log "redis: EXISTS ${PFX}user:1 = $EX_USER ; GET ${PFX}hits = $GET_HITS"

fails=()
[[ "${OUT[0]:-}" == "null" ]]                          || fails+=("get(ausente) != null (${OUT[0]:-})")
[[ "${OUT[1]:-}" == '{"name":"Ana","score":91}' ]]     || fails+=("get após set != o objeto (${OUT[1]:-})")
[[ "${OUT[2]:-}" == "true" ]]                           || fails+=("has() != true (${OUT[2]:-})")
[[ "${OUT[3]:-}" == "1" ]]                              || fails+=("incr() != 1 (${OUT[3]:-})")
[[ "${OUT[4]:-}" == "5" ]]                              || fails+=("incrBy(4) != 5 (${OUT[4]:-})")
[[ "${OUT[5]:-}" == "false" ]]                          || fails+=("has() após delete != false (${OUT[5]:-})")
[[ "$EX_USER" == "0" ]]                                 || fails+=("redis ainda tem ${PFX}user:1")
[[ "$GET_HITS" == "5" ]]                                || fails+=("redis GET ${PFX}hits != \"5\" ($GET_HITS)")
grep -q '"op":"set"'  "$L/wire.jsonl"                   || fails+=("wire trace sem op set")
grep -q '"op":"incr"' "$L/wire.jsonl"                   || fails+=("wire trace sem op incr")
grep -q '"Ana"' "$L/wire.jsonl" && fails+=("wire trace vazou um valor") || true

if [[ ${#fails[@]} -eq 0 ]]; then
  ok "cache-redis de um .mh; get/set/has/delete + objeto JSON + incr/incrBy verificados no Redis (redis-cli)"
else
  for f in "${fails[@]}"; do log "  ! $f"; done
  die "veja as verificações acima"
fi
