package mcpserver

import "net/http"

// Operational endpoints for running under an orchestrator. They sit beside the
// single JSON-RPC endpoint (mcpPath) and are deliberately unauthenticated: a
// kubelet probe carries no bearer token, and none of them exposes workflow
// data — /healthz and /readyz return a word, /metrics returns aggregate
// counters (see metrics.go). A deployment scrapes /metrics from inside the pod
// or the service mesh.
const (
	healthzPath = "/healthz"
	readyzPath  = "/readyz"
	metricsPath = "/metrics"
)

// registerOps wires the operational endpoints onto mux.
func (h *httpServer) registerOps(mux *http.ServeMux) {
	mux.HandleFunc(healthzPath, h.handleHealthz)
	mux.HandleFunc(readyzPath, h.handleReadyz)
	mux.HandleFunc(metricsPath, h.handleMetrics)
}

// handleMetrics serves the Prometheus text exposition when the sink is the
// built-in *promMetrics; a push-based sink (OTLP, an extension) has no pull
// endpoint, so it is 404 here.
func (h *httpServer) handleMetrics(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	pm, ok := h.metrics.(*promMetrics)
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	pm.render(w, h.liveGauges())
}

// liveGauges snapshots the point-in-time counts the /metrics render pairs with
// the counters.
func (h *httpServer) liveGauges() liveGauges {
	var g liveGauges
	for _, rn := range h.runs.List() {
		rn.mu.Lock()
		switch rn.state {
		case "working":
			g.runsActive++
		case "queued":
			g.runsQueued++
		}
		rn.mu.Unlock()
	}
	g.sessionsActive = h.sessions.Len()
	return g
}

// handleHealthz is liveness: 200 for as long as the process is up. It does not
// flip during a drain — a draining pod is still alive and must not be
// restarted out from under its in-flight runs.
func (h *httpServer) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

// handleReadyz is readiness: 200 normally, 503 once a shutdown has begun so
// the Service stops routing new traffic here while in-flight runs drain.
func (h *httpServer) handleReadyz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	if h.draining.Load() {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("draining\n"))
		return
	}
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ready\n"))
}
