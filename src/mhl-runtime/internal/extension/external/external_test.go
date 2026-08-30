package external

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/extension"
)

// ---------------------------------------------------------------------------
// Fake external extension: this test binary re-executed with
// GO_WANT_HELPER_PROCESS=1 runs runFakeExtension instead of the tests. It
// speaks the same newline JSON-RPC the real host expects.
// ---------------------------------------------------------------------------

// testBinary is this compiled test executable — re-executed as the fake
// extension via the GO_WANT_HELPER_PROCESS gate below.
var testBinary string

func TestMain(m *testing.M) {
	if os.Getenv("GO_WANT_HELPER_PROCESS") == "1" {
		runFakeExtension()
		return
	}
	testBinary = os.Args[0]
	os.Exit(m.Run())
}

func runFakeExtension() {
	fmt.Fprintln(os.Stderr, "STARTED", os.Getpid())
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64<<10), maxLine)
	out := bufio.NewWriter(os.Stdout)

	var sendMu sync.Mutex
	send := func(m message) {
		sendMu.Lock()
		defer sendMu.Unlock()
		b, _ := json.Marshal(m)
		out.Write(append(b, '\n'))
		out.Flush()
	}

	calls := 0
	for in.Scan() {
		var m message
		if err := json.Unmarshal(in.Bytes(), &m); err != nil {
			continue
		}
		switch m.Method {
		case "initialize":
			fmt.Fprintln(os.Stderr, "INIT")
			api := APIVersion
			if v := os.Getenv("FAKE_API"); v != "" {
				api = v
			}
			res := initializeResult{APIVersion: api}
			res.Extension.ID = "com.test.fake"
			res.Extension.Version = "9.9.9"
			if os.Getenv("FAKE_DECLARE") == "1" {
				res.Declarations = []extension.DeclarationSpec{{
					Kind:    "fake",
					Methods: []extension.MethodSpec{{Name: "calls", Signature: "calls() -> number"}},
				}}
			}
			send(message{ID: m.ID, Result: mustRaw(res)})
		case "shutdown":
			out.Flush()
			os.Exit(0)
		case "call":
			calls++
			var cp callParams
			_ = json.Unmarshal(m.Params, &cp)
			switch cp.Operation {
			case "echo":
				send(message{ID: m.ID, Result: cp.Args[0]})
			case "calls":
				send(message{ID: m.ID, Result: mustRaw(calls)})
			case "slow":
				// Handled off the read loop so the fake stays responsive to
				// later calls while this one is in flight.
				id := m.ID
				go func() {
					time.Sleep(1500 * time.Millisecond)
					send(message{ID: id, Result: mustRaw("done")})
				}()
			case "boom":
				os.Exit(3)
			case "stderr_then_die":
				fmt.Fprintln(os.Stderr, "panic: fake blew up")
				os.Exit(4)
			case "leak_secret_to_stderr":
				// Resolve a credential, then (mis)handle it by dumping it to
				// stderr and crashing — the host must scrub it from the error.
				reqID := uint64(8888)
				send(message{ID: &reqID, Method: "secret.resolve", Params: mustRaw(secretResolveParams{Reference: `env("FAKE_TOKEN")`})})
				if !in.Scan() {
					return
				}
				var reply message
				_ = json.Unmarshal(in.Bytes(), &reply)
				var tok string
				_ = json.Unmarshal(reply.Result, &tok)
				fmt.Fprintln(os.Stderr, "boom, and by the way the token was "+tok)
				os.Exit(7)
			case "secret":
				// Ask the host for a credential, then return it.
				reqID := uint64(9999)
				send(message{ID: &reqID, Method: "secret.resolve", Params: mustRaw(secretResolveParams{Reference: `env("FAKE_TOKEN")`})})
				if !in.Scan() {
					return
				}
				var reply message
				_ = json.Unmarshal(in.Bytes(), &reply)
				if reply.Error != nil {
					send(message{ID: m.ID, Error: &wireError{Message: "secret denied: " + reply.Error.Message}})
				} else {
					send(message{ID: m.ID, Result: reply.Result})
				}
			case "log_then_ok":
				send(message{Method: "log", Params: mustRaw(logParams{Message: "hello from the extension"})})
				send(message{ID: m.ID, Result: mustRaw("ok")})
			default:
				send(message{ID: m.ID, Error: &wireError{Code: "unknown_op", Message: cp.Operation}})
			}
		}
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

type recordingHost struct {
	mu      sync.Mutex
	secrets map[string]string
	logs    []string
}

func (h *recordingHost) ResolveSecret(ref string) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if v, ok := h.secrets[ref]; ok {
		return v, nil
	}
	return "", fmt.Errorf("no such secret %s", ref)
}
func (h *recordingHost) HTTPClient() *http.Client { return http.DefaultClient }
func (h *recordingHost) Logf(format string, args ...any) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.logs = append(h.logs, fmt.Sprintf(format, args...))
}

// Redact masks any secret this host has handed out (via ResolveSecret).
func (h *recordingHost) Redact(s string) string {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, v := range h.secrets {
		if v != "" {
			s = strings.ReplaceAll(s, v, "[REDACTED]")
		}
	}
	return s
}

func fakeManifest(t *testing.T, perms Permissions, env []string) *Manifest {
	t.Helper()
	m := &Manifest{
		ID:         "com.test.fake",
		Version:    "9.9.9",
		APIVersion: APIVersion,
		Executable: os.Args[0],
		Args:       []string{"-test.run=TestMain"},
		Env:        append([]string{"GO_WANT_HELPER_PROCESS=1"}, env...),
		Perms:      perms,
		Declares: []extension.DeclarationSpec{{
			Kind:       "fake",
			Properties: []extension.PropertySpec{{Name: "target", Type: "string"}},
			Methods: []extension.MethodSpec{
				{Name: "echo", Signature: "echo(x: any) -> any"},
				{Name: "calls", Signature: "calls() -> number"},
			},
		}},
	}
	if err := m.validate(); err != nil {
		t.Fatalf("fake manifest invalid: %v", err)
	}
	return m
}

func bindFake(t *testing.T, m *Manifest, host extension.HostContext) (*External, extension.Instance) {
	t.Helper()
	ext := New(m)
	inst, err := ext.Bind(extension.Declaration{Kind: "fake", Name: "F"}, host)
	if err != nil {
		t.Fatalf("Bind: %v", err)
	}
	t.Cleanup(func() { ext.Close() })
	return ext, inst
}

func callFake(t *testing.T, inst extension.Instance, op string, args ...extension.Value) (extension.Value, error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return inst.Call(ctx, extension.CallRequest{
		Declaration: extension.Declaration{Kind: "fake", Name: "F"},
		Method:      op,
		Args:        args,
	})
}

// ---------------------------------------------------------------------------
// tests
// ---------------------------------------------------------------------------

func TestExternalHandshakeAndCall(t *testing.T) {
	_, inst := bindFake(t, fakeManifest(t, Permissions{}, nil), &recordingHost{})

	got, err := callFake(t, inst, "echo", "hello")
	if err != nil {
		t.Fatalf("echo: %v", err)
	}
	if got != "hello" {
		t.Fatalf("echo = %#v, want %q", got, "hello")
	}
}

// TestExternalReusesOneProcess is the phase-4 acceptance criterion: many
// calls, one process, one handshake.
func TestExternalReusesOneProcess(t *testing.T) {
	ext, inst := bindFake(t, fakeManifest(t, Permissions{}, nil), &recordingHost{})

	for i := 1; i <= 5; i++ {
		got, err := callFake(t, inst, "calls")
		if err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
		if int(got.(float64)) != i {
			t.Fatalf("call %d: extension state = %v, want %d — a fresh process would reset it", i, got, i)
		}
	}

	// Exactly one child, exactly one handshake.
	if n := strings.Count(ext.proc.stderrText(), "STARTED"); n != 1 {
		t.Fatalf("expected 1 process start, stderr shows %d:\n%s", n, ext.proc.stderrText())
	}
	if n := strings.Count(ext.proc.stderrText(), "INIT"); n != 1 {
		t.Fatalf("expected 1 handshake, stderr shows %d", n)
	}
}

func TestExternalConcurrentCallsMultiplex(t *testing.T) {
	_, inst := bindFake(t, fakeManifest(t, Permissions{}, nil), &recordingHost{})
	// Warm up so the single start race is resolved before the fan-out.
	if _, err := callFake(t, inst, "echo", "warm"); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got, err := callFake(t, inst, "echo", fmt.Sprintf("m%d", i))
			if err != nil {
				errs <- err
				return
			}
			if got != fmt.Sprintf("m%d", i) {
				errs <- fmt.Errorf("got %v want m%d — replies crossed", got, i)
			}
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
}

func TestExternalCallTimeoutDoesNotWedge(t *testing.T) {
	_, inst := bindFake(t, fakeManifest(t, Permissions{}, nil), &recordingHost{})

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	_, err := inst.Call(ctx, extension.CallRequest{
		Declaration: extension.Declaration{Kind: "fake", Name: "F"},
		Method:      "slow",
	})
	if err == nil {
		t.Fatal("expected a timeout error from the slow call")
	}

	// The process is still alive; a normal call still works.
	if _, err := callFake(t, inst, "echo", "still-here"); err != nil {
		t.Fatalf("call after timeout: %v", err)
	}
}

func TestExternalProcessCrashSurfacesAndRestarts(t *testing.T) {
	ext, inst := bindFake(t, fakeManifest(t, Permissions{}, nil), &recordingHost{})

	if _, err := callFake(t, inst, "echo", "ping"); err != nil {
		t.Fatal(err)
	}
	if _, err := callFake(t, inst, "boom"); err == nil {
		t.Fatal("expected an error when the extension exits mid-call")
	}
	// Next call transparently respawns.
	got, err := callFake(t, inst, "calls")
	if err != nil {
		t.Fatalf("call after crash: %v", err)
	}
	if int(got.(float64)) != 1 {
		t.Fatalf("restarted process should have reset state, got %v", got)
	}
	if ext.restarts != 1 {
		t.Fatalf("restarts = %d, want 1", ext.restarts)
	}
}

func TestExternalStderrIsQuotedOnCrash(t *testing.T) {
	_, inst := bindFake(t, fakeManifest(t, Permissions{}, nil), &recordingHost{})
	_, err := callFake(t, inst, "stderr_then_die")
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "fake blew up") {
		t.Fatalf("crash error should quote stderr, got: %v", err)
	}
}

func TestExternalAPIVersionMismatch(t *testing.T) {
	_, inst := bindFake(t, fakeManifest(t, Permissions{}, []string{"FAKE_API=99"}), &recordingHost{})
	_, err := callFake(t, inst, "echo", "x")
	if err == nil || !strings.Contains(err.Error(), "API") {
		t.Fatalf("expected an API-version mismatch error, got: %v", err)
	}
}

func TestExternalSecretResolutionIsManifestGated(t *testing.T) {
	host := &recordingHost{secrets: map[string]string{`env("FAKE_TOKEN")`: "s3cr3t"}}

	// Denied: manifest lists no secrets.
	_, denied := bindFake(t, fakeManifest(t, Permissions{}, nil), host)
	if _, err := callFake(t, denied, "secret"); err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("expected the secret to be denied, got: %v", err)
	}

	// Allowed: manifest permits exactly that reference.
	_, allowed := bindFake(t, fakeManifest(t, Permissions{Secrets: []string{`env("FAKE_TOKEN")`}}, nil), host)
	got, err := callFake(t, allowed, "secret")
	if err != nil {
		t.Fatalf("secret (allowed): %v", err)
	}
	if got != "s3cr3t" {
		t.Fatalf("resolved secret = %v, want s3cr3t", got)
	}
}

func TestExternalRedactsSecretsFromCrashOutput(t *testing.T) {
	host := &recordingHost{secrets: map[string]string{`env("FAKE_TOKEN")`: "sk-super-secret-123"}}
	_, inst := bindFake(t, fakeManifest(t, Permissions{Secrets: []string{`env("FAKE_TOKEN")`}}, nil), host)

	_, err := callFake(t, inst, "leak_secret_to_stderr")
	if err == nil {
		t.Fatal("expected an error")
	}
	if strings.Contains(err.Error(), "sk-super-secret-123") {
		t.Fatalf("the resolved secret leaked into the error:\n%v", err)
	}
	if !strings.Contains(err.Error(), "[REDACTED]") {
		t.Fatalf("expected the secret to be masked, got:\n%v", err)
	}
}

func TestExternalForwardsExtensionLogs(t *testing.T) {
	host := &recordingHost{}
	_, inst := bindFake(t, fakeManifest(t, Permissions{}, nil), host)
	if _, err := callFake(t, inst, "log_then_ok"); err != nil {
		t.Fatal(err)
	}
	host.mu.Lock()
	defer host.mu.Unlock()
	joined := strings.Join(host.logs, "\n")
	if !strings.Contains(joined, "hello from the extension") {
		t.Fatalf("host did not receive the extension's log line, got: %q", joined)
	}
}

func TestManifestValidation(t *testing.T) {
	base := func() *Manifest {
		return &Manifest{ID: "x", APIVersion: APIVersion, Executable: "bin/x", Declares: []extension.DeclarationSpec{{Kind: "k"}}}
	}
	if err := base().validate(); err != nil {
		t.Fatalf("valid manifest rejected: %v", err)
	}
	bad := base()
	bad.APIVersion = "0"
	if err := bad.validate(); err == nil {
		t.Fatal("expected an unsupported-api_version error")
	}
	bad = base()
	bad.Declares = nil
	if err := bad.validate(); err == nil {
		t.Fatal("expected a no-kinds error")
	}
}
