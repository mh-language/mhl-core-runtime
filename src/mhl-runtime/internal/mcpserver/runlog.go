package mcpserver

import "sync"

// ringLogMax bounds a run's retained output. Older bytes are dropped once the
// buffer would exceed it; run/logs reports dropped=true when a caller's cursor
// falls into the dropped region.
const ringLogMax = 64 << 10 // 64 KiB

// ringLog is an asyncRun's own bounded, byte-cursored copy of its step/log()
// output. It is an io.Writer (execRun tees execsvc output into it) and is safe
// for concurrent writes (parallel steps) and reads (run/logs).
type ringLog struct {
	mu      sync.Mutex
	buf     []byte
	written int64 // total bytes ever written; buf holds the last min(written, max)
}

func newRingLog() *ringLog { return &ringLog{} }

func (r *ringLog) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf = append(r.buf, p...)
	r.written += int64(len(p))
	if len(r.buf) > ringLogMax {
		r.buf = append(r.buf[:0], r.buf[len(r.buf)-ringLogMax:]...)
	}
	return len(p), nil
}

// read returns the retained output from byte offset since to now, the cursor
// to pass as `since` next time, and whether anything between since and the
// returned data was dropped. since <= 0 means "from the start of what's kept".
func (r *ringLog) read(since int64) (text string, next int64, dropped bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	start := r.written - int64(len(r.buf)) // byte offset of buf[0]
	if since < start {
		return string(r.buf), r.written, since > 0
	}
	if since >= r.written {
		return "", r.written, false
	}
	return string(r.buf[since-start:]), r.written, false
}
