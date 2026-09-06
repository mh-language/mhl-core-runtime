package mcpserver

import (
	"strings"
	"sync"

	"github.com/mh-language/mhl-core-runtime/internal/features/auth"
)

// ringLogMax bounds a run's retained output. Older bytes are dropped once the
// buffer would exceed it; run/logs reports dropped=true when a caller's cursor
// falls into the dropped region.
const ringLogMax = 64 << 10 // 64 KiB

// ringLog is an asyncRun's own bounded, byte-cursored copy of its step/log()
// output. It is an io.Writer (execRun tees execsvc output into it) and is safe
// for concurrent writes (parallel steps) and reads (run/logs).
//
// Redaction is applied on read, streaming-safe: a resolved credential is masked
// even when it is split across two Write calls or lands on a run/logs cursor
// boundary. While the run is live, read holds back the last auth.MaxLen() bytes
// (a secret could still be arriving) and never returns a cursor that divides a
// secret occurrence; Seal releases the tail once no more writes can happen.
type ringLog struct {
	mu      sync.Mutex
	buf     []byte
	written int64 // total bytes ever written; buf holds the last min(written, max)
	sealed  bool  // run finished: no more writes, safe to release the held-back tail
}

func newRingLog() *ringLog { return &ringLog{} }

func (r *ringLog) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, p...)
	r.written += int64(len(p))
	if len(r.buf) > ringLogMax {
		drop := len(r.buf) - ringLogMax
		// Move the drop point forward past any secret it splits so the first
		// retained byte is never the middle of a masked value (which Redact,
		// seeing only the tail, could no longer recognise).
		drop = auth.SafeCutForward(string(r.buf), drop)
		r.buf = append(r.buf[:0], r.buf[drop:]...)
	}
	return len(p), nil
}

// Seal marks the run finished: read may then return through the last written
// byte instead of holding a tail back for a still-arriving secret.
func (r *ringLog) Seal() {
	r.mu.Lock()
	r.sealed = true
	r.mu.Unlock()
}

// read returns the retained output from byte offset since to the current safe
// end, the cursor to pass as `since` next time, and whether anything between
// since and the returned data was dropped. since <= 0 means "from the start of
// what's kept".
//
// The returned text is redacted with auth.MaxLen() bytes of already-delivered
// left context, so a secret straddling `since` is masked as a whole even though
// its prefix went out on the previous read. The advertised cursor is walked
// back out of any secret it lands inside (auth.SafeCut), so the next read's
// left-context join is exact.
func (r *ringLog) read(since int64) (text string, next int64, dropped bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	margin := auth.MaxLen()
	start := r.written - int64(len(r.buf)) // absolute offset of buf[0]

	// How far into buf it is safe to reveal: hold back a secret-length tail
	// while the run is live, then align off any split secret occurrence.
	safeRel := len(r.buf)
	if !r.sealed {
		safeRel -= margin
	}
	if safeRel < 0 {
		safeRel = 0
	}
	safeRel = auth.SafeCut(string(r.buf), safeRel)
	safeEnd := start + int64(safeRel)

	if since >= safeEnd {
		return "", max(since, safeEnd), false
	}

	fromRel := since - start
	if fromRel < 0 {
		fromRel = 0
		dropped = since > 0
	}
	ctxRel := fromRel - int64(margin)
	if ctxRel < 0 {
		ctxRel = 0
	}

	full := auth.Redact(string(r.buf[ctxRel:safeRel]))
	prefix := auth.Redact(string(r.buf[ctxRel:fromRel]))
	return strings.TrimPrefix(full, prefix), safeEnd, dropped
}
