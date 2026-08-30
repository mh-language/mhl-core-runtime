package external

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
)

// maxLine bounds a single wire line so a malicious or broken extension can't
// exhaust memory with one enormous "response".
const maxLine = 8 << 20 // 8 MiB

// errProcessGone is returned to every in-flight and subsequent call once the
// child's stdout closes (it exited, or the pipe broke).
var errProcessGone = errors.New("extension process is not running")

// inboundHandler services the requests an extension initiates back to the
// host: secret resolution and log lines. Both are optional — a nil handler
// rejects secret requests and drops logs.
type inboundHandler interface {
	resolveSecret(ref string) (string, error)
	logLine(msg string)
}

// process is one running extension child, its bidirectional JSON-RPC pipe,
// and the table of calls awaiting a reply. All writes to the child's stdin
// are serialised; replies are delivered to waiters by request id from a
// single reader goroutine.
type process struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stderr *stderrTail
	host   inboundHandler

	writeMu sync.Mutex

	nextID atomic.Uint64

	mu      sync.Mutex
	pending map[uint64]chan message
	closed  bool
	exitErr error
}

// startProcess spawns bin with args, wires its pipes, and launches the reader
// loop. The child inherits none of the parent environment (env is exactly
// what the caller passes) so ambient secrets never reach it.
func startProcess(bin string, args, env []string, host inboundHandler) (*process, error) {
	cmd := exec.Command(bin, args...)
	cmd.Env = env

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	tail := &stderrTail{}
	cmd.Stderr = tail

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	p := &process{
		cmd:     cmd,
		stdin:   stdin,
		stderr:  tail,
		host:    host,
		pending: map[uint64]chan message{},
	}
	go p.readLoop(stdout)
	return p, nil
}

// call sends method/params and waits for the matching reply, honouring ctx
// for cancellation and deadline. It never blocks past the process's life:
// once readLoop sees stdout close, every waiter is released with the exit
// error.
func (p *process) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	id := p.nextID.Add(1)
	ch := make(chan message, 1)

	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, p.exitOrGone()
	}
	p.pending[id] = ch
	p.mu.Unlock()

	defer func() {
		p.mu.Lock()
		delete(p.pending, id)
		p.mu.Unlock()
	}()

	if err := p.write(message{ID: &id, Method: method, Params: mustRaw(params)}); err != nil {
		return nil, err
	}

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case reply := <-ch:
		if reply.Error != nil {
			return nil, reply.Error
		}
		return reply.Result, nil
	}
}

// notify sends a fire-and-forget message (no id, no reply expected).
func (p *process) notify(method string, params any) error {
	return p.write(message{Method: method, Params: mustRaw(params)})
}

func (p *process) write(m message) error {
	line, err := json.Marshal(m)
	if err != nil {
		return err
	}
	line = append(line, '\n')

	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return p.exitOrGone()
	}
	p.mu.Unlock()
	if _, err := p.stdin.Write(line); err != nil {
		return fmt.Errorf("writing to extension: %w", err)
	}
	return nil
}

func (p *process) readLoop(stdout io.Reader) {
	sc := bufio.NewScanner(stdout)
	sc.Buffer(make([]byte, 0, 64<<10), maxLine)

	for sc.Scan() {
		var m message
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			// A line we can't parse is a protocol violation; log and skip
			// rather than tearing the process down for one bad frame.
			if p.host != nil {
				p.host.logLine("extension sent an unparseable line: " + err.Error())
			}
			continue
		}
		switch {
		case m.isResponse():
			p.deliver(m)
		case m.isNotification():
			p.handleNotification(m)
		case m.isRequest():
			p.handleRequest(m)
		}
	}

	err := sc.Err()
	waitErr := p.cmd.Wait()
	if err == nil {
		err = waitErr
	}
	p.shutdown(err)
}

func (p *process) deliver(m message) {
	p.mu.Lock()
	ch := p.pending[*m.ID]
	p.mu.Unlock()
	if ch != nil {
		ch <- m
	}
}

func (p *process) handleNotification(m message) {
	if m.Method == "log" && p.host != nil {
		var lp logParams
		_ = json.Unmarshal(m.Params, &lp)
		p.host.logLine(lp.Message)
	}
}

func (p *process) handleRequest(m message) {
	resp := message{ID: m.ID}
	switch m.Method {
	case "secret.resolve":
		var sp secretResolveParams
		if err := json.Unmarshal(m.Params, &sp); err != nil {
			resp.Error = &wireError{Message: "malformed secret.resolve params"}
			break
		}
		if p.host == nil {
			resp.Error = &wireError{Code: "denied", Message: "secret resolution is not available"}
			break
		}
		v, err := p.host.resolveSecret(sp.Reference)
		if err != nil {
			resp.Error = &wireError{Code: "denied", Message: err.Error()}
			break
		}
		resp.Result = mustRaw(v)
	default:
		resp.Error = &wireError{Code: "unknown_method", Message: "host has no method " + m.Method}
	}
	_ = p.write(resp)
}

// shutdown marks the process dead and fails every waiter. Safe to call more
// than once.
func (p *process) shutdown(cause error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	if cause != nil {
		p.exitErr = cause
	}
	pending := p.pending
	p.pending = map[uint64]chan message{}
	p.mu.Unlock()

	fail := p.exitOrGone()
	for _, ch := range pending {
		ch <- message{Error: &wireError{Message: fail.Error()}}
	}
}

// close asks the extension to stop, closes its stdin, and waits for readLoop
// to observe the exit. The caller is expected to bound this with its own
// timeout and fall back to Kill.
func (p *process) close() {
	_ = p.notify("shutdown", nil)
	_ = p.stdin.Close()
}

// kill force-terminates the child.
func (p *process) kill() {
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
}

func (p *process) isClosed() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.closed
}

func (p *process) exitOrGone() error {
	if p.exitErr != nil {
		return fmt.Errorf("%w: %v", errProcessGone, p.exitErr)
	}
	return errProcessGone
}

// stderrText returns whatever the child last wrote to stderr — appended to a
// call error so a crash is diagnosable.
func (p *process) stderrText() string { return p.stderr.String() }

func mustRaw(v any) json.RawMessage {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	return b
}
