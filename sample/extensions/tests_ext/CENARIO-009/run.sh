#!/usr/bin/env bash
# CENARIO-009 — várias declarações, um processo
source "$(dirname "${BASH_SOURCE[0]}")/../_lib.sh"

ensure_env
new_project
DIR="$PROJ/store"; PROBE_LOG="$PROJ/probe.jsonl"; mkdir -p "$DIR"

cat > main.mh <<EOF
extension store A {
    dir: "$DIR"
    log: "$PROBE_LOG"
}
extension store B {
    dir: "$DIR"
    log: "$PROBE_LOG"
}
extension store C {
    dir: "$DIR"
    log: "$PROBE_LOG"
}

pipeline ThreeStores {
    parallel Fan {
        step a1 { A.put("a/1", 1) }
        step a2 { A.put("a/2", 2) }
        step a3 { A.put("a/3", 3) }
        step a4 { A.put("a/4", 4) }
        step b1 { B.put("b/1", 1) }
        step b2 { B.put("b/2", 2) }
        step b3 { B.put("b/3", 3) }
        step b4 { B.put("b/4", 4) }
        step c1 { C.put("c/1", 1) }
        step c2 { C.put("c/2", 2) }
        step c3 { C.put("c/3", 3) }
        step c4 { C.put("c/4", 4) }
    }
    step check {
        var ka = A.list("")
        log(ka)
    }
}
EOF

log "mhl run main.mh (3 declarações store, 12 puts em parallel)"
"$MHL" run main.mh > "$L/run.out" 2>&1 || { cat "$L/run.out" >> "$CLIENT_LOG"; die "mhl run falhou"; }
cp "$PROBE_LOG" "$L/probe.jsonl" 2>/dev/null || true
log "run.out:"; grep -vE '^step:' "$L/run.out" | tee -a "$CLIENT_LOG"
log "probe.jsonl:"; cat "$L/probe.jsonl" | tee -a "$CLIENT_LOG"

INITS=$(grep -c '"ev":"init"' "$L/probe.jsonl" || true)
CALLS=$(grep -c '"ev":"call"' "$L/probe.jsonl" || true)
DECLS=$(grep '"ev":"call"' "$L/probe.jsonl" | python3 -c 'import sys,json
s=set()
for l in sys.stdin:
  try: s.add(json.loads(l).get("decl"))
  except Exception: pass
print(",".join(sorted(x for x in s if x)))')
LIST_LINE=$(grep -vE '^(session:|step:|executed )' "$L/run.out" | tail -1)
KEYS_ON_DISK=$(find "$DIR" -name '*.json' | wc -l | tr -d ' ')

fails=()
[[ "${INITS:-0}" == "1" ]]                 || fails+=("init count = $INITS (esperado 1 — um processo)")
[[ "${CALLS:-0}" == "13" ]]                || fails+=("call count = $CALLS (esperado 13: 12 puts + 1 list)")
[[ "$DECLS" == "A,B,C" ]]                  || fails+=("decls nas chamadas: '$DECLS' (esperado A,B,C)")
[[ "${KEYS_ON_DISK}" == "12" ]]            || fails+=("$KEYS_ON_DISK chaves no disco (esperado 12)")
echo "$LIST_LINE" | grep -q "a/1" && echo "$LIST_LINE" | grep -q "c/4" \
  || fails+=("A.list('') não enxergou as chaves de B/C compartilhadas ($LIST_LINE)")

if [[ ${#fails[@]} -eq 0 ]]; then
  ok "1 processo para A/B/C; 12 puts interleaved sem perda; list compartilhada"
else
  for f in "${fails[@]}"; do log "  ! $f"; done
  die "veja as verificações acima"
fi
