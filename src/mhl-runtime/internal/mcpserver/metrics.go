package mcpserver

import (
	"fmt"
	"io"
	"sync/atomic"
	"time"
)

// MetricsSink receives run lifecycle events. Instrumentation — the calls to
// these methods at run start / end — lives in the server because only the
// server sees the lifecycle. Exposition does not: the built-in *promMetrics
// keeps counters and renders Prometheus text at GET /metrics, but a push-based
// sink (OTLP, StatsD) or an extension can implement this interface instead,
// with no /metrics route. Live gauges (runs active / queued, sessions) are
// read straight off the registry at scrape time, so they are not events here.
type MetricsSink interface {
	// ObserveRun records one finished run: outcome is "completed", "failed"
	// or "canceled"; d is its execution time (queue wait excluded).
	ObserveRun(outcome string, d time.Duration)
	// ObserveToolCall records one synchronous tools/call: outcome "ok" or
	// "error".
	ObserveToolCall(outcome string)
	// ObserveClaim records one durable-intake run claimed by this replica; d is
	// how long the claim call took (store round-trip).
	ObserveClaim(d time.Duration)
	// ObserveReconcile records one run returned to `pending` by reconcile
	// (its holder's lease was gone).
	ObserveReconcile()
}

// nopMetrics discards everything — the sink to use when metrics are off.
type nopMetrics struct{}

func (nopMetrics) ObserveRun(string, time.Duration) {}
func (nopMetrics) ObserveToolCall(string)           {}
func (nopMetrics) ObserveClaim(time.Duration)       {}
func (nopMetrics) ObserveReconcile()                {}

// promMetrics is the built-in MetricsSink: in-process atomic counters rendered
// as Prometheus text. Durations are summed in milliseconds (atomic-friendly)
// and divided back to seconds at render time.
type promMetrics struct {
	runsCompleted atomic.Int64
	runsFailed    atomic.Int64
	runsCanceled  atomic.Int64

	runDurMillis atomic.Int64
	runDurCount  atomic.Int64

	toolCallsOK  atomic.Int64
	toolCallsErr atomic.Int64

	intakeClaims      atomic.Int64
	intakeReconciled  atomic.Int64
	claimLatencyMs    atomic.Int64
	claimLatencyCount atomic.Int64
}

func newPromMetrics() *promMetrics { return &promMetrics{} }

func (m *promMetrics) ObserveRun(outcome string, d time.Duration) {
	switch outcome {
	case "completed":
		m.runsCompleted.Add(1)
	case "failed":
		m.runsFailed.Add(1)
	case "canceled":
		m.runsCanceled.Add(1)
	}
	m.runDurMillis.Add(d.Milliseconds())
	m.runDurCount.Add(1)
}

func (m *promMetrics) ObserveToolCall(outcome string) {
	if outcome == "error" {
		m.toolCallsErr.Add(1)
		return
	}
	m.toolCallsOK.Add(1)
}

func (m *promMetrics) ObserveClaim(d time.Duration) {
	m.intakeClaims.Add(1)
	m.claimLatencyMs.Add(d.Milliseconds())
	m.claimLatencyCount.Add(1)
}
func (m *promMetrics) ObserveReconcile() { m.intakeReconciled.Add(1) }

// liveGauges is the point-in-time state the /metrics render pairs with the
// counters — computed from the registry, not tracked as events.
type liveGauges struct {
	runsActive     int
	runsQueued     int
	runsPending    int // durable-intake pending runs, fleet-wide (store scan)
	sessionsActive int
}

// render writes the Prometheus text exposition for m plus g.
func (m *promMetrics) render(w io.Writer, g liveGauges) {
	p := func(format string, args ...any) { fmt.Fprintf(w, format, args...) }

	p("# HELP mhl_serve_runs_total Async run/* executions by terminal outcome.\n")
	p("# TYPE mhl_serve_runs_total counter\n")
	p("mhl_serve_runs_total{outcome=\"completed\"} %d\n", m.runsCompleted.Load())
	p("mhl_serve_runs_total{outcome=\"failed\"} %d\n", m.runsFailed.Load())
	p("mhl_serve_runs_total{outcome=\"canceled\"} %d\n", m.runsCanceled.Load())

	p("# HELP mhl_serve_run_duration_seconds Cumulative run execution time (queue wait excluded).\n")
	p("# TYPE mhl_serve_run_duration_seconds summary\n")
	p("mhl_serve_run_duration_seconds_sum %.3f\n", float64(m.runDurMillis.Load())/1000)
	p("mhl_serve_run_duration_seconds_count %d\n", m.runDurCount.Load())

	p("# HELP mhl_serve_tool_calls_total Synchronous tools/call requests by outcome.\n")
	p("# TYPE mhl_serve_tool_calls_total counter\n")
	p("mhl_serve_tool_calls_total{outcome=\"ok\"} %d\n", m.toolCallsOK.Load())
	p("mhl_serve_tool_calls_total{outcome=\"error\"} %d\n", m.toolCallsErr.Load())

	p("# HELP mhl_serve_runs_active Runs currently executing.\n")
	p("# TYPE mhl_serve_runs_active gauge\n")
	p("mhl_serve_runs_active %d\n", g.runsActive)

	p("# HELP mhl_serve_runs_queued Runs waiting for a concurrency slot.\n")
	p("# TYPE mhl_serve_runs_queued gauge\n")
	p("mhl_serve_runs_queued %d\n", g.runsQueued)

	p("# HELP mhl_serve_runs_pending Durable-intake runs accepted but not yet claimed (fleet-wide).\n")
	p("# TYPE mhl_serve_runs_pending gauge\n")
	p("mhl_serve_runs_pending %d\n", g.runsPending)

	p("# HELP mhl_serve_intake_claims_total Durable-intake runs claimed by this replica.\n")
	p("# TYPE mhl_serve_intake_claims_total counter\n")
	p("mhl_serve_intake_claims_total %d\n", m.intakeClaims.Load())

	p("# HELP mhl_serve_intake_reconciled_total Runs returned to pending by reconcile (holder lease gone).\n")
	p("# TYPE mhl_serve_intake_reconciled_total counter\n")
	p("mhl_serve_intake_reconciled_total %d\n", m.intakeReconciled.Load())

	p("# HELP mhl_serve_intake_claim_latency_seconds Cumulative time spent in the claim call.\n")
	p("# TYPE mhl_serve_intake_claim_latency_seconds summary\n")
	p("mhl_serve_intake_claim_latency_seconds_sum %.3f\n", float64(m.claimLatencyMs.Load())/1000)
	p("mhl_serve_intake_claim_latency_seconds_count %d\n", m.claimLatencyCount.Load())

	p("# HELP mhl_serve_sessions_active Live MCP protocol sessions.\n")
	p("# TYPE mhl_serve_sessions_active gauge\n")
	p("mhl_serve_sessions_active %d\n", g.sessionsActive)
}
