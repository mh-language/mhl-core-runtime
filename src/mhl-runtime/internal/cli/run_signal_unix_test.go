//go:build !windows

package cli_test

import (
	"bytes"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/cli"
)

// SIGINT cancels `mhl run` (aborting the blocking agent call in flight)
// instead of killing the process: the result is a typed "cancelled" error,
// and the checkpoint it saves lets --resume continue from the cancelled step.
func TestRunSIGINTCancelsAndResumes(t *testing.T) {
	chdirTemp(t)
	src := `
agent Slow {
    command: "bash"
    args: ["-c", "if [ -e go.flag ]; then echo fast; else sleep 30; fi"]
}

pipeline P {
    var out = ""
    step First { log("first") }
    step Wait { out = Slow.run(prompt: "x") }
}
`
	if err := os.WriteFile("p.mh", []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf lockedBuffer
	done := make(chan error, 1)
	go func() { done <- cli.Run([]string{"run", "p.mh", "--format", "jsonl"}, &buf) }()

	// Signal once the slow step is running.
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(buf.String(), `"step":"Wait"`) {
		if time.Now().After(deadline) {
			t.Fatal("Wait never started")
		}
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("want a cancellation error")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("SIGINT did not cancel the run")
	}
	evs := decodeEvents(t, buf.String())
	last := evs[len(evs)-1]
	if last["type"] != "result" || last["kind"] != "cancelled" || last["step"] != "Wait" {
		t.Fatalf("result = %v", last)
	}

	if err := os.WriteFile("go.flag", nil, 0o644); err != nil {
		t.Fatal(err)
	}
	var again bytes.Buffer
	if err := cli.Run([]string{"run", "p.mh", "--resume", "--format", "jsonl"}, &again); err != nil {
		t.Fatalf("resume: %v\n%s", err, again.String())
	}
	if got := strings.Join(stepTrace(decodeEvents(t, again.String())), " "); got != "start:Wait end:Wait:ok" {
		t.Errorf("resumed trace = %s", got)
	}
}

// lockedBuffer is a bytes.Buffer safe to read while the run writes to it.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
