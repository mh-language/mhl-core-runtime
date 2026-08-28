package nativeops_test

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mh-language/mhl-core-runtime/internal/features/nativeops"
)

func TestDoSendsHeadersAndJSONBody(t *testing.T) {
	var gotBody map[string]any
	var gotHeader, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotHeader = r.Header.Get("X-Custom")
		if err := json.NewDecoder(r.Body).Decode(&gotBody); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	result, err := nativeops.Do(context.Background(), "POST", srv.URL, nativeops.Options{
		Headers: map[string]string{"X-Custom": "abc"},
		Body:    map[string]any{"text": "hi"},
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if gotMethod != "POST" {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if result["status"] != 201.0 {
		t.Errorf("status = %v, want 201", result["status"])
	}
	if result["body"] != `{"ok":true}` {
		t.Errorf("body = %v", result["body"])
	}
	if result["ok"] != true {
		t.Errorf("ok = %v, want true", result["ok"])
	}
	if gotHeader != "abc" {
		t.Errorf("X-Custom header = %q", gotHeader)
	}
	if gotBody["text"] != "hi" {
		t.Errorf("request body text = %v", gotBody["text"])
	}
	headers, _ := result["headers"].(map[string]any)
	if headers == nil || !strings.Contains(headers["Content-Type"].(string), "application/json") {
		t.Errorf("response headers = %v", result["headers"])
	}
	parsed, _ := result["json"].(map[string]any)
	if parsed["ok"] != true {
		t.Errorf("json = %v, want parsed {ok:true}", result["json"])
	}
}

// TestDoNonSuccessStatusIsNotAnError mirrors cmd.exec's exit_code
// philosophy: a non-2xx response is a normal, inspectable outcome
// (result["status"]), not a Go error.
func TestDoNonSuccessStatusIsNotAnError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	result, err := nativeops.Do(context.Background(), "POST", srv.URL, nativeops.Options{})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if result["status"] != 500.0 {
		t.Errorf("status = %v, want 500", result["status"])
	}
	if result["ok"] != false {
		t.Errorf("ok = %v, want false", result["ok"])
	}
}

func TestDoRaiseForStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := nativeops.Do(context.Background(), "GET", srv.URL, nativeops.Options{RaiseForStatus: true})
	if err == nil {
		t.Fatal("expected an error when raise_for_status is set and status is 404")
	}
}

func TestDoConnectionFailureErrors(t *testing.T) {
	_, err := nativeops.Do(context.Background(), "GET", "http://127.0.0.1:1/unreachable", nativeops.Options{})
	if err == nil {
		t.Fatal("expected an error for an unreachable host")
	}
}

func TestDoVerbsAndQuery(t *testing.T) {
	var gotMethod, gotQuery, gotKept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotQuery = r.URL.Query().Get("q")
		gotKept = r.URL.Query().Get("a")
	}))
	defer srv.Close()

	for _, method := range []string{"GET", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"} {
		_, err := nativeops.Do(context.Background(), method, srv.URL+"?a=1", nativeops.Options{
			Query: map[string]string{"q": "hello world"},
		})
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		if gotMethod != method {
			t.Errorf("method = %q, want %q", gotMethod, method)
		}
		if gotQuery != "hello world" {
			t.Errorf("%s: query q = %q, want %q", method, gotQuery, "hello world")
		}
		if gotKept != "1" {
			t.Errorf("%s: pre-existing query a = %q, want 1", method, gotKept)
		}
	}
}

func TestDoFormBody(t *testing.T) {
	var gotCT, gotForm string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCT = r.Header.Get("Content-Type")
		_ = r.ParseForm()
		gotForm = r.PostForm.Get("name")
	}))
	defer srv.Close()

	_, err := nativeops.Do(context.Background(), "POST", srv.URL, nativeops.Options{
		Form: map[string]string{"name": "ada"},
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if gotCT != "application/x-www-form-urlencoded" {
		t.Errorf("Content-Type = %q", gotCT)
	}
	if gotForm != "ada" {
		t.Errorf("form name = %q", gotForm)
	}
}

func TestDoTextBody(t *testing.T) {
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
	}))
	defer srv.Close()

	raw := "plain text, not json"
	_, err := nativeops.Do(context.Background(), "POST", srv.URL, nativeops.Options{Text: &raw})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if gotBody != raw {
		t.Errorf("body = %q, want %q", gotBody, raw)
	}
}

func TestDoBodyTextFormMutuallyExclusive(t *testing.T) {
	raw := "x"
	_, err := nativeops.Do(context.Background(), "POST", "http://127.0.0.1:1/x", nativeops.Options{
		Body: map[string]any{"a": 1},
		Text: &raw,
	})
	if err == nil {
		t.Fatal("expected an error when both body and text are set")
	}
}

func TestDoAuthSugar(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
	}))
	defer srv.Close()

	_, err := nativeops.Do(context.Background(), "GET", srv.URL, nativeops.Options{
		Auth: &nativeops.AuthOptions{Bearer: "tok123"},
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if gotAuth != "Bearer tok123" {
		t.Errorf("Authorization = %q", gotAuth)
	}

	_, err = nativeops.Do(context.Background(), "GET", srv.URL, nativeops.Options{
		Auth: &nativeops.AuthOptions{BasicUser: "ada", BasicPassword: "pw"},
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("ada:pw"))
	if gotAuth != want {
		t.Errorf("Authorization = %q, want %q", gotAuth, want)
	}
}

func TestDoExplicitAuthorizationHeaderWins(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
	}))
	defer srv.Close()

	_, err := nativeops.Do(context.Background(), "GET", srv.URL, nativeops.Options{
		Headers: map[string]string{"Authorization": "Bearer explicit"},
		Auth:    &nativeops.AuthOptions{Bearer: "sugar"},
	})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if gotAuth != "Bearer explicit" {
		t.Errorf("Authorization = %q, want the explicit header", gotAuth)
	}
}

func TestDoFollowRedirectsFalse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/from" {
			http.Redirect(w, r, "/to", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	no := false
	result, err := nativeops.Do(context.Background(), "GET", srv.URL+"/from", nativeops.Options{FollowRedirects: &no})
	if err != nil {
		t.Fatalf("Do: %v", err)
	}
	if result["status"] != 302.0 {
		t.Errorf("status = %v, want 302 (redirect not followed)", result["status"])
	}
}

func TestDoTimeoutOverride(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	defer srv.Close()

	start := time.Now()
	_, err := nativeops.Do(context.Background(), "GET", srv.URL, nativeops.Options{Timeout: 30 * time.Millisecond})
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if time.Since(start) > time.Second {
		t.Errorf("timeout override was not applied (took %s)", time.Since(start))
	}
}

func TestDoClientCertificateMTLS(t *testing.T) {
	dir := t.TempDir()
	certPEM, keyPEM := selfSignedCert(t)
	certPath := writeFile(t, dir, "client.pem", certPEM)
	keyPath := writeFile(t, dir, "client.key", keyPEM)

	// A self-signed client cert is its own CA, so the server can verify it
	// straight from a pool holding that same cert.
	clientCAs := x509.NewCertPool()
	clientCAs.AppendCertsFromPEM(certPEM)

	var sawClientCert bool
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawClientCert = r.TLS != nil && len(r.TLS.PeerCertificates) > 0
		w.WriteHeader(http.StatusOK)
	}))
	srv.TLS = &tls.Config{ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: clientCAs}
	srv.StartTLS()
	defer srv.Close()

	// insecure: the point here is the *client* handshake — skip verifying
	// httptest's own generated server cert.
	result, err := nativeops.Do(context.Background(), "GET", srv.URL, nativeops.Options{
		TLS: &nativeops.TLSOptions{Cert: certPath, Key: keyPath, Insecure: true},
	})
	if err != nil {
		t.Fatalf("Do with client cert: %v", err)
	}
	if result["status"] != 200.0 {
		t.Errorf("status = %v, want 200", result["status"])
	}
	if !sawClientCert {
		t.Error("server did not receive the client certificate")
	}
}

func TestDoCustomCABundle(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	dir := t.TempDir()
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	caPath := writeFile(t, dir, "ca.pem", caPEM)

	if _, err := nativeops.Do(context.Background(), "GET", srv.URL, nativeops.Options{}); err == nil {
		t.Fatal("expected a verification error without the CA bundle")
	}
	result, err := nativeops.Do(context.Background(), "GET", srv.URL, nativeops.Options{
		TLS: &nativeops.TLSOptions{CA: caPath},
	})
	if err != nil {
		t.Fatalf("Do with custom CA: %v", err)
	}
	if result["status"] != 200.0 {
		t.Errorf("status = %v, want 200", result["status"])
	}
}

func TestDoInsecureSkipsServerVerification(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	if _, err := nativeops.Do(context.Background(), "GET", srv.URL, nativeops.Options{}); err == nil {
		t.Fatal("expected a certificate-verification error without insecure")
	}
	result, err := nativeops.Do(context.Background(), "GET", srv.URL, nativeops.Options{
		TLS: &nativeops.TLSOptions{Insecure: true},
	})
	if err != nil {
		t.Fatalf("Do insecure: %v", err)
	}
	if result["status"] != 200.0 {
		t.Errorf("status = %v, want 200", result["status"])
	}
}

func TestDoBadClientCertPathErrors(t *testing.T) {
	_, err := nativeops.Do(context.Background(), "GET", "https://127.0.0.1:1/x", nativeops.Options{
		TLS: &nativeops.TLSOptions{Cert: "/no/such/cert.pem", Key: "/no/such/key.pem"},
	})
	if err == nil {
		t.Fatal("expected an error for a missing client certificate file")
	}
}

func TestDoExplicitProxyIsUsed(t *testing.T) {
	var proxied string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A forward proxy receives the absolute request URI.
		proxied = r.URL.String()
		w.WriteHeader(http.StatusOK)
	}))
	defer proxy.Close()

	_, err := nativeops.Do(context.Background(), "GET", "http://example.invalid/thing", nativeops.Options{
		Proxy: proxy.URL,
	})
	if err != nil {
		t.Fatalf("Do via proxy: %v", err)
	}
	if proxied != "http://example.invalid/thing" {
		t.Errorf("proxy saw %q, want the absolute target URL", proxied)
	}
}

func TestDoMalformedProxyErrors(t *testing.T) {
	_, err := nativeops.Do(context.Background(), "GET", "http://127.0.0.1:1/x", nativeops.Options{
		Proxy: "://not a url",
	})
	if err == nil {
		t.Fatal("expected an error for a malformed proxy url")
	}
}

func TestDownloadStreamsToFile(t *testing.T) {
	payload := strings.Repeat("mhl-payload-", 500)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		_, _ = w.Write([]byte(payload))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "nested", "artifact.bin")
	result, err := nativeops.Download(context.Background(), srv.URL, dest, nativeops.Options{})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if result["ok"] != true || result["path"] != dest {
		t.Errorf("result = %+v", result)
	}
	if result["bytes"] != float64(len(payload)) {
		t.Errorf("bytes = %v, want %d", result["bytes"], len(payload))
	}
	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if string(got) != payload {
		t.Errorf("downloaded content mismatch (%d bytes)", len(got))
	}
}

func TestDownloadNonSuccessLeavesNoFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("nope"))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "artifact.bin")
	result, err := nativeops.Download(context.Background(), srv.URL, dest, nativeops.Options{})
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	if result["ok"] != false || result["status"] != 404.0 {
		t.Errorf("result = %+v", result)
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("a file was written for a 404 response")
	}
}

func TestDownloadRaiseForStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := nativeops.Download(context.Background(), srv.URL, filepath.Join(t.TempDir(), "x"), nativeops.Options{
		RaiseForStatus: true,
	})
	if err == nil {
		t.Fatal("expected an error with raise_for_status on a 500")
	}
}

func TestDownloadRequiresDestination(t *testing.T) {
	_, err := nativeops.Download(context.Background(), "http://127.0.0.1:1/x", "  ", nativeops.Options{})
	if err == nil {
		t.Fatal("expected an error for a blank destination path")
	}
}

// --- test helpers -------------------------------------------------------

func writeFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

// selfSignedCert returns a PEM certificate and its PEM EC private key, valid
// for 127.0.0.1 and usable as both a client and a (self-signed) CA cert.
func selfSignedCert(t *testing.T) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("gen key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "mhl-test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth, x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("create cert: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatalf("marshal key: %v", err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM
}
