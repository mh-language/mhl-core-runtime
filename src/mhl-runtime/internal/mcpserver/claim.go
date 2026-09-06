package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

// ClaimNexter is the optional fast path a durable store implements for intake:
// atomically move one run from `pending` to `claimed` for holder and return it.
// mhl-store-postgres can back this with `SELECT ... FOR UPDATE SKIP LOCKED`.
// A store that does not implement it uses claimNextCAS — the generic
// compare-and-swap scan over its status records.
//
// No caller wires this yet: the per-replica claim loop that consumes it is
// Etapa 1 (see mvp/docs/design-runtime-intake-duravel.md). The signature is
// fixed now, before the demo binary is frozen, so it is not retrofitted onto
// every store extension later.
type ClaimNexter interface {
	ClaimNext(ctx context.Context, holder string) (runID string, rec RunStatusRec, ok bool, err error)
}

// ErrNoClaimBackend is returned by claimNext when the store neither implements
// ClaimNexter nor offers a CAS-capable KV to run the generic path against.
var ErrNoClaimBackend = errors.New("mcpserver: durable intake needs a ClaimNexter store or a cas-capable KV")

// claimNext moves one pending run to claimed for holder. It uses the store's
// ClaimNexter fast path when present, else the generic CAS scan against kv.
// ok == false with a nil error means there is no pending run right now.
func claimNext(ctx context.Context, store any, kv LockingKVStore, holder string) (runID string, rec RunStatusRec, ok bool, err error) {
	if cn, isCN := store.(ClaimNexter); isCN {
		return cn.ClaimNext(ctx, holder)
	}
	if kv == nil || !kv.CASCapable() {
		return "", RunStatusRec{}, false, ErrNoClaimBackend
	}
	return claimNextCAS(ctx, kv, holder, time.Now)
}

// claimNextCAS lists the run status records under kvRunPrefix, takes the oldest
// one in `pending` (by StartedAt), and flips it to `claimed` with a
// CompareAndSwap against the exact bytes it read. A lost race (another replica
// claimed it first) falls through to the next candidate. now is injectable for
// tests.
func claimNextCAS(ctx context.Context, kv LockingKVStore, holder string, now func() time.Time) (string, RunStatusRec, bool, error) {
	keys, err := kv.List(ctx, kvRunPrefix)
	if err != nil {
		return "", RunStatusRec{}, false, err
	}

	type cand struct {
		key, id string
		raw     []byte
		rec     RunStatusRec
	}
	var pending []cand
	for _, k := range keys {
		rest := strings.TrimPrefix(k, kvRunPrefix)
		id, isStatus := strings.CutSuffix(rest, "/status")
		if !isStatus || id == "" || strings.Contains(id, "/") {
			continue
		}
		raw, found, gErr := kv.Get(ctx, k)
		if gErr != nil || !found {
			continue
		}
		var r RunStatusRec
		if json.Unmarshal(raw, &r) != nil || r.State != RunStatePending {
			continue
		}
		pending = append(pending, cand{key: k, id: id, raw: raw, rec: r})
	}
	sort.Slice(pending, func(i, j int) bool {
		return pending[i].rec.StartedAt.Before(pending[j].rec.StartedAt)
	})

	for _, c := range pending {
		claimed := c.rec
		claimed.State = RunStateClaimed
		claimed.Holder = holder
		claimed.UpdatedAt = now()
		swapped, csErr := kv.CompareAndSwap(ctx, c.key, c.raw, claimed)
		if csErr != nil {
			return "", RunStatusRec{}, false, csErr
		}
		if swapped {
			return c.id, claimed, true, nil
		}
	}
	return "", RunStatusRec{}, false, nil
}
