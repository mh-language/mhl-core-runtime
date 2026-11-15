package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/execsvc"
	"github.com/mh-language/mhl-core-runtime/internal/features/auth"
)

// eventWriter is `mhl run --format jsonl`: one JSON object per line on
// stdout, streamed as the run progresses, so a caller (an editor, a
// dashboard, a CI step) can
// follow a run live instead of waiting for the final result.
//
//	{"type":"run_start","pipeline","session","steps":[...],"ts"}
//	{"type":"step_start","step","index","total","ts"}
//	{"type":"step_end","step","index","status","target?","error?","ms","ts"}
//	{"type":"log","text","ts"}          one per line the run prints
//	{"type":"result", ...}              the --format json object, last line
//
// Every method is safe for concurrent use: the steps of a `parallel` group
// report from their own goroutines.
type eventWriter struct {
	mu      sync.Mutex
	out     io.Writer
	partial bytes.Buffer // log text not yet terminated by a newline
}

func newEventWriter(out io.Writer) *eventWriter { return &eventWriter{out: out} }

func eventTime() string { return time.Now().UTC().Format(time.RFC3339Nano) }

// emit writes one event line. The caller must hold mu.
func (w *eventWriter) emit(ev any) {
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	data = append(data, '\n')
	_, _ = w.out.Write(data)
}

func (w *eventWriter) send(ev any) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.emit(ev)
}

// Write makes eventWriter the run's output stream: each complete line
// becomes a "log" event.
func (w *eventWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.partial.Write(p)
	for {
		line, err := w.partial.ReadString('\n')
		if err != nil {
			// no newline yet — keep the fragment for the next Write
			w.partial.Reset()
			w.partial.WriteString(line)
			return len(p), nil
		}
		w.emit(map[string]any{"type": "log", "text": auth.Redact(line[:len(line)-1]), "ts": eventTime()})
	}
}

// flush emits any trailing, unterminated log text.
func (w *eventWriter) flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.partial.Len() > 0 {
		w.emit(map[string]any{"type": "log", "text": auth.Redact(w.partial.String()), "ts": eventTime()})
		w.partial.Reset()
	}
}

// hook wires the writer into an execsvc request.
func (w *eventWriter) hook(req *execsvc.Request) {
	req.Out = w
	req.OnStart = func(pipeline, session string, steps []string) {
		w.send(map[string]any{"type": "run_start", "pipeline": pipeline, "session": session, "steps": steps, "ts": eventTime()})
	}
	req.OnStep = func(step string, index, total int) {
		w.send(map[string]any{"type": "step_start", "step": step, "index": index, "total": total, "ts": eventTime()})
	}
	req.OnStepEnd = func(step string, index int, status, target string, err error, elapsed time.Duration) {
		ev := map[string]any{"type": "step_end", "step": step, "index": index, "status": status, "ms": elapsed.Milliseconds(), "ts": eventTime()}
		if target != "" {
			ev["target"] = target
		}
		if err != nil {
			ev["error"] = auth.Redact(err.Error())
		}
		w.send(ev)
	}
}

// result emits the final "result" event: the --format json object.
func (w *eventWriter) result(res *execsvc.Result, runErr error, file string) {
	w.flush()
	var buf bytes.Buffer
	writeRunJSON(&buf, res, runErr, file, "")
	var obj map[string]any
	if err := json.Unmarshal(buf.Bytes(), &obj); err != nil {
		obj = map[string]any{"ok": false, "error": err.Error()}
	}
	obj["type"] = "result"
	w.send(obj)
}
