package mcpserver

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func buildTestHTTP(t *testing.T, cfg HTTPConfig) *httpServer {
	t.Helper()
	return buildTestHTTPCtx(t, context.Background(), cfg)
}

func buildTestHTTPCtx(t *testing.T, ctx context.Context, cfg HTTPConfig) *httpServer {
	t.Helper()
	dir := t.TempDir()
	if cfg.Dir == "" {
		if err := os.WriteFile(filepath.Join(dir, "wf.mh"),
			[]byte("pipeline P {\n step S { var x = 1 }\n}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cfg.Dir = dir
	}
	_, h, err := buildHTTP(ctx, cfg, io.Discard)
	if err != nil {
		t.Fatalf("buildHTTP: %v", err)
	}
	t.Cleanup(func() { _ = h.cps.Close() })
	return h
}

func TestReadyzFlipsWhenDraining(t *testing.T) {
	h := buildTestHTTP(t, HTTPConfig{})

	rec := httptest.NewRecorder()
	h.handleReadyz(rec, httptest.NewRequest(http.MethodGet, readyzPath, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("ready: got %d, want 200", rec.Code)
	}

	h.draining.Store(true)
	rec = httptest.NewRecorder()
	h.handleReadyz(rec, httptest.NewRequest(http.MethodGet, readyzPath, nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("draining: got %d, want 503", rec.Code)
	}

	// healthz stays 200 while draining — the process is alive.
	rec = httptest.NewRecorder()
	h.handleHealthz(rec, httptest.NewRequest(http.MethodGet, healthzPath, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("healthz while draining: got %d, want 200", rec.Code)
	}
}

func TestDrainZeroTimeoutCancelsImmediately(t *testing.T) {
	h := buildTestHTTP(t, HTTPConfig{DrainTimeout: 0})
	start := time.Now()
	h.drain(io.Discard)
	if time.Since(start) > 500*time.Millisecond {
		t.Errorf("drain with 0 timeout took %s, expected near-instant", time.Since(start))
	}
	if !h.draining.Load() {
		t.Error("draining flag not set")
	}
	if h.runsCtx.Err() == nil {
		t.Error("runsCtx not cancelled")
	}
}

func TestAcquireSlotWaitShedsLoad(t *testing.T) {
	h := buildTestHTTP(t, HTTPConfig{MaxConcurrentRuns: 1})

	rel, ok := h.tryAcquireSlot()
	if !ok {
		t.Fatal("first slot should be free")
	}
	// The only slot is held: a bounded wait must give up, not block forever.
	start := time.Now()
	_, ok = h.acquireSlotWait(context.Background(), 50*time.Millisecond)
	if ok {
		t.Fatal("acquireSlotWait succeeded with no free slot")
	}
	if d := time.Since(start); d < 40*time.Millisecond || d > 500*time.Millisecond {
		t.Errorf("wait was %s, expected ~50ms", d)
	}
	rel()
	if _, ok := h.tryAcquireSlot(); !ok {
		t.Error("slot not released")
	}
}

// Unlimited concurrency (sem == nil): every acquire is a no-op that succeeds.
func TestSlotsUnlimitedByDefault(t *testing.T) {
	h := buildTestHTTP(t, HTTPConfig{})
	for i := 0; i < 100; i++ {
		if _, ok := h.tryAcquireSlot(); !ok {
			t.Fatalf("tryAcquireSlot failed at %d with unlimited concurrency", i)
		}
	}
}

func TestDrainWaitsForWorkingRuns(t *testing.T) {
	h := buildTestHTTP(t, HTTPConfig{DrainTimeout: 3 * time.Second})
	rn := &asyncRun{id: "r1", state: "working", done: make(chan struct{})}
	h.runs.Put(rn)

	go func() {
		time.Sleep(150 * time.Millisecond)
		rn.mu.Lock()
		rn.state = "completed"
		rn.mu.Unlock()
	}()

	start := time.Now()
	h.drain(io.Discard)
	elapsed := time.Since(start)
	if elapsed < 100*time.Millisecond {
		t.Errorf("drain returned in %s — did not wait for the working run", elapsed)
	}
	if elapsed > 2*time.Second {
		t.Errorf("drain took %s — did not notice the run completed", elapsed)
	}
	if h.runsCtx.Err() == nil {
		t.Error("runsCtx not cancelled after drain")
	}
}

// A SIGTERM cancels the server context, but async runs descend from runsCtx,
// which is detached from it: they must keep running until drain() decides to
// stop them. Without context.WithoutCancel in buildHTTP the signal propagates
// straight through and --drain-timeout is a no-op.
func TestRunsSurviveSignalUntilDrain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	h := buildTestHTTPCtx(t, ctx, HTTPConfig{DrainTimeout: 3 * time.Second})

	runCtx, runCancel := context.WithCancel(h.runsCtx)
	defer runCancel()

	cancel() // simulate SIGTERM reaching the process context
	time.Sleep(50 * time.Millisecond)

	if h.runsCtx.Err() != nil {
		t.Fatal("runsCtx cancelled by the signal — should only fall to drain()")
	}
	if runCtx.Err() != nil {
		t.Fatal("an in-flight run's context was cancelled by the signal")
	}

	h.drain(io.Discard) // no working runs → returns fast, then runsCancel()
	if h.runsCtx.Err() == nil {
		t.Fatal("runsCtx not cancelled after drain")
	}
	if runCtx.Err() == nil {
		t.Fatal("run context not cancelled after drain")
	}
}
