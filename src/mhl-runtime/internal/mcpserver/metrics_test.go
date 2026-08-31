package mcpserver

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPromMetricsRender(t *testing.T) {
	m := newPromMetrics()
	m.ObserveRun("completed", 1500*time.Millisecond)
	m.ObserveRun("completed", 500*time.Millisecond)
	m.ObserveRun("failed", 200*time.Millisecond)
	m.ObserveToolCall("ok")
	m.ObserveToolCall("error")
	m.ObserveToolCall("ok")

	var sb strings.Builder
	m.render(&sb, liveGauges{runsActive: 2, runsQueued: 3, sessionsActive: 4})
	out := sb.String()

	for _, want := range []string{
		`mhl_serve_runs_total{outcome="completed"} 2`,
		`mhl_serve_runs_total{outcome="failed"} 1`,
		`mhl_serve_runs_total{outcome="canceled"} 0`,
		"mhl_serve_run_duration_seconds_sum 2.200",
		"mhl_serve_run_duration_seconds_count 3",
		`mhl_serve_tool_calls_total{outcome="ok"} 2`,
		`mhl_serve_tool_calls_total{outcome="error"} 1`,
		"mhl_serve_runs_active 2",
		"mhl_serve_runs_queued 3",
		"mhl_serve_sessions_active 4",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q\n---\n%s", want, out)
		}
	}
}

func TestMetricsEndpoint(t *testing.T) {
	h := buildTestHTTP(t, HTTPConfig{})

	// A working run and a couple of sessions should show up as gauges.
	h.runs.Put(&asyncRun{id: "a", state: "working", logs: newRingLog(), done: make(chan struct{})})
	h.runs.Put(&asyncRun{id: "b", state: "queued", logs: newRingLog(), done: make(chan struct{})})
	h.sessions.Put(&session{id: "s1"})

	rec := httptest.NewRecorder()
	h.handleMetrics(rec, httptest.NewRequest(http.MethodGet, metricsPath, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"mhl_serve_runs_active 1",
		"mhl_serve_runs_queued 1",
		"mhl_serve_sessions_active 1",
		"# TYPE mhl_serve_runs_total counter",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("/metrics missing %q\n---\n%s", want, body)
		}
	}
}

// A non-prom sink has no pull endpoint.
func TestMetricsEndpoint404ForNonPromSink(t *testing.T) {
	h := buildTestHTTP(t, HTTPConfig{})
	h.metrics = nopMetrics{}
	rec := httptest.NewRecorder()
	h.handleMetrics(rec, httptest.NewRequest(http.MethodGet, metricsPath, nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404 for a non-prom sink", rec.Code)
	}
}

func TestRingLogBoundedAndCursored(t *testing.T) {
	r := newRingLog()
	_, _ = r.Write([]byte("hello\n"))
	text, next, dropped := r.read(0)
	if text != "hello\n" || dropped {
		t.Fatalf("read(0) = %q dropped=%v", text, dropped)
	}
	if next != 6 {
		t.Fatalf("next = %d, want 6", next)
	}
	// Poll from the cursor: nothing new.
	text, _, _ = r.read(next)
	if text != "" {
		t.Errorf("read(next) = %q, want empty", text)
	}
	_, _ = r.Write([]byte("world\n"))
	text, _, _ = r.read(next)
	if text != "world\n" {
		t.Errorf("incremental read = %q, want %q", text, "world\n")
	}

	// Overflow the ring: oldest bytes drop, an old cursor reports dropped.
	big := strings.Repeat("x", ringLogMax+100)
	_, _ = r.Write([]byte(big))
	_, _, dropped = r.read(next)
	if !dropped {
		t.Error("expected dropped=true after the ring wrapped past the cursor")
	}
	text, _, _ = r.read(0)
	if len(text) > ringLogMax {
		t.Errorf("retained %d bytes, want <= %d", len(text), ringLogMax)
	}
}
