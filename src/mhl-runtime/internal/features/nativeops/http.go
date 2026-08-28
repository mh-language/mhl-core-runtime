package nativeops

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// defaultHTTPTimeout mirrors internal/mcp.NewClient's default — a stateless
// HTTP call needs some bound, and it is used whenever a call does not set
// its own `timeout:`.
const defaultHTTPTimeout = 30 * time.Second

// AuthOptions is the optional `auth:` sugar on an http call: exactly one of
// the two forms is applied, and only when no explicit Authorization header
// is already set.
type AuthOptions struct {
	Bearer        string // -> "Authorization: Bearer <Bearer>"
	BasicUser     string // -> "Authorization: Basic base64(user:password)"
	BasicPassword string
}

// TLSOptions is the optional `tls:` block on an http call. Certificates are
// PEM files on disk (the format `openssl`/`certbot` emit); PKCS#12/.pfx is
// not supported.
type TLSOptions struct {
	Cert     string // path to a PEM client certificate
	Key      string // path to its PEM private key
	CA       string // path to an extra PEM CA bundle to trust, added to the system pool
	Insecure bool   // skip server-certificate verification — development only
}

// Options carries every optional parameter an http.* call accepts. The zero
// value is a plain request with the default timeout and no body.
type Options struct {
	Headers         map[string]string
	Query           map[string]string // URL-encoded and merged into the request URL's query
	Body            any               // JSON-encoded; sets Content-Type: application/json
	Text            *string           // raw request body; no Content-Type is forced
	Form            map[string]string // application/x-www-form-urlencoded body
	Timeout         time.Duration     // 0 => defaultHTTPTimeout
	FollowRedirects *bool             // nil => follow (net/http's default)
	RaiseForStatus  bool              // true => a non-2xx response is returned as an error
	Proxy           string            // explicit proxy URL; "" => honour HTTP(S)_PROXY / NO_PROXY
	Auth            *AuthOptions
	TLS             *TLSOptions
}

// Do issues an HTTP request with the given method (an upper-case verb such
// as "GET" or "POST") to rawURL and returns the response as
// {"status": float64, "body": string, "headers": {string: string},
// "ok": bool, "json": any}. "json" is the parsed body when the response is
// JSON, otherwise nil.
//
// A non-2xx response is not itself an error — same philosophy as Exec's
// exit_code — so a .mh pipeline can branch on response.status; pass
// RaiseForStatus to opt into the raise-on-error-status behaviour. A genuine
// failure to complete the request (DNS, connection refused, timeout, a bad
// client certificate) always errors.
func Do(ctx context.Context, method, rawURL string, opts Options) (map[string]any, error) {
	tag := "http." + strings.ToLower(method)

	timeout := timeoutOf(opts)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := newRequest(ctx, method, rawURL, opts)
	if err != nil {
		return nil, fmt.Errorf("%s %q: %w", tag, rawURL, err)
	}
	client, err := buildClient(opts, timeout)
	if err != nil {
		return nil, fmt.Errorf("%s %q: %w", tag, rawURL, err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %q: %w", tag, rawURL, err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("%s %q: reading response: %w", tag, rawURL, err)
	}

	headers := headerMap(resp.Header)
	ok := resp.StatusCode >= 200 && resp.StatusCode < 300
	if opts.RaiseForStatus && !ok {
		return nil, fmt.Errorf("%s %q: response status %d", tag, rawURL, resp.StatusCode)
	}

	var parsed any
	if strings.Contains(strings.ToLower(resp.Header.Get("Content-Type")), "json") {
		var v any
		if json.Unmarshal(respBody, &v) == nil {
			parsed = v
		}
	}

	return map[string]any{
		"status":  float64(resp.StatusCode),
		"body":    string(respBody),
		"headers": headers,
		"ok":      ok,
		"json":    parsed,
	}, nil
}

// Download issues a GET to rawURL and streams the response body straight to
// destPath instead of returning it — the counterpart to fs.write for
// remote, possibly large or binary, content. The write is atomic (a
// temp file in the destination directory, renamed into place) and parent
// directories are created. On a non-2xx response no file is written and the
// result's "ok" is false (unless RaiseForStatus is set, which errors).
// Returns {"status": float64, "path": string, "bytes": float64,
// "ok": bool, "headers": {string: string}}.
func Download(ctx context.Context, rawURL, destPath string, opts Options) (map[string]any, error) {
	const tag = "http.download"
	if strings.TrimSpace(destPath) == "" {
		return nil, fmt.Errorf("%s: a destination path is required", tag)
	}

	timeout := timeoutOf(opts)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := newRequest(ctx, http.MethodGet, rawURL, opts)
	if err != nil {
		return nil, fmt.Errorf("%s %q: %w", tag, rawURL, err)
	}
	client, err := buildClient(opts, timeout)
	if err != nil {
		return nil, fmt.Errorf("%s %q: %w", tag, rawURL, err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s %q: %w", tag, rawURL, err)
	}
	defer resp.Body.Close()

	headers := headerMap(resp.Header)
	ok := resp.StatusCode >= 200 && resp.StatusCode < 300
	if !ok {
		_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4<<10))
		if opts.RaiseForStatus {
			return nil, fmt.Errorf("%s %q: response status %d", tag, rawURL, resp.StatusCode)
		}
		return map[string]any{
			"status": float64(resp.StatusCode), "path": "", "bytes": float64(0),
			"ok": false, "headers": headers,
		}, nil
	}

	dir := filepath.Dir(destPath)
	if dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("%s: creating %s: %w", tag, dir, err)
		}
	}
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(destPath)+".mhl-*")
	if err != nil {
		return nil, fmt.Errorf("%s: %w", tag, err)
	}
	tmpName := tmp.Name()
	n, copyErr := io.Copy(tmp, resp.Body)
	closeErr := tmp.Close()
	if copyErr != nil || closeErr != nil {
		_ = os.Remove(tmpName)
		if copyErr != nil {
			return nil, fmt.Errorf("%s %q: streaming to %s: %w", tag, rawURL, destPath, copyErr)
		}
		return nil, fmt.Errorf("%s: writing %s: %w", tag, destPath, closeErr)
	}
	if err := os.Rename(tmpName, destPath); err != nil {
		_ = os.Remove(tmpName)
		return nil, fmt.Errorf("%s: finalising %s: %w", tag, destPath, err)
	}

	return map[string]any{
		"status": float64(resp.StatusCode), "path": destPath, "bytes": float64(n),
		"ok": true, "headers": headers,
	}, nil
}

func timeoutOf(opts Options) time.Duration {
	if opts.Timeout > 0 {
		return opts.Timeout
	}
	return defaultHTTPTimeout
}

func headerMap(h http.Header) map[string]any {
	out := make(map[string]any, len(h))
	for k, v := range h {
		out[k] = strings.Join(v, ", ")
	}
	return out
}

// newRequest builds the *http.Request: body (one of Body/Text/Form), query
// merge, headers, and the auth sugar.
func newRequest(ctx context.Context, method, rawURL string, opts Options) (*http.Request, error) {
	body, contentType, err := requestBody(opts)
	if err != nil {
		return nil, err
	}
	target, err := applyQuery(rawURL, opts.Query)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, fmt.Errorf("building request: %w", err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	applyAuth(req, opts.Auth)
	for k, v := range opts.Headers {
		req.Header.Set(k, v)
	}
	return req, nil
}

// buildClient constructs the *http.Client. A custom transport is used only
// when TLS or an explicit proxy is configured; it is cloned from
// http.DefaultTransport so connection pooling, HTTP/2 and the standard
// HTTP(S)_PROXY / NO_PROXY handling are preserved.
func buildClient(opts Options, timeout time.Duration) (*http.Client, error) {
	client := &http.Client{Timeout: timeout}

	if opts.TLS != nil || opts.Proxy != "" {
		tr := &http.Transport{Proxy: http.ProxyFromEnvironment, ForceAttemptHTTP2: true}
		if base, ok := http.DefaultTransport.(*http.Transport); ok {
			tr = base.Clone()
		}
		if opts.TLS != nil {
			cfg, err := tlsConfig(*opts.TLS)
			if err != nil {
				return nil, err
			}
			tr.TLSClientConfig = cfg
		}
		if opts.Proxy != "" {
			pu, err := url.Parse(opts.Proxy)
			if err != nil {
				return nil, fmt.Errorf("proxy %q: %w", opts.Proxy, err)
			}
			tr.Proxy = http.ProxyURL(pu)
		}
		client.Transport = tr
	}

	if opts.FollowRedirects != nil && !*opts.FollowRedirects {
		client.CheckRedirect = func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}
	}
	return client, nil
}

// requestBody picks the body from at most one of Body/Text/Form and reports
// the Content-Type it implies ("" when the caller must set its own).
func requestBody(opts Options) (io.Reader, string, error) {
	set := 0
	for _, present := range []bool{opts.Body != nil, opts.Text != nil, opts.Form != nil} {
		if present {
			set++
		}
	}
	if set > 1 {
		return nil, "", fmt.Errorf("body, text and form are mutually exclusive")
	}

	switch {
	case opts.Body != nil:
		raw, err := json.Marshal(opts.Body)
		if err != nil {
			return nil, "", fmt.Errorf("encoding body: %w", err)
		}
		return bytes.NewReader(raw), "application/json", nil
	case opts.Text != nil:
		return strings.NewReader(*opts.Text), "", nil
	case opts.Form != nil:
		form := url.Values{}
		for k, v := range opts.Form {
			form.Set(k, v)
		}
		return strings.NewReader(form.Encode()), "application/x-www-form-urlencoded", nil
	default:
		return nil, "", nil
	}
}

// applyQuery merges query into rawURL's existing query string.
func applyQuery(rawURL string, query map[string]string) (string, error) {
	if len(query) == 0 {
		return rawURL, nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("parsing url: %w", err)
	}
	q := u.Query()
	for k, v := range query {
		q.Set(k, v)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// applyAuth sets the Authorization header from the auth sugar, unless one is
// already present (an explicit header wins).
func applyAuth(req *http.Request, auth *AuthOptions) {
	if auth == nil || req.Header.Get("Authorization") != "" {
		return
	}
	switch {
	case auth.Bearer != "":
		req.Header.Set("Authorization", "Bearer "+auth.Bearer)
	case auth.BasicUser != "" || auth.BasicPassword != "":
		creds := auth.BasicUser + ":" + auth.BasicPassword
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(creds)))
	}
}

// tlsConfig builds a *tls.Config from the `tls:` block: an optional PEM
// client certificate (cert+key, both required together), an optional extra
// CA bundle added to the system pool, and the insecure toggle.
func tlsConfig(opts TLSOptions) (*tls.Config, error) {
	cfg := &tls.Config{InsecureSkipVerify: opts.Insecure} //nolint:gosec // opt-in, documented dev-only

	if opts.Cert != "" || opts.Key != "" {
		if opts.Cert == "" || opts.Key == "" {
			return nil, fmt.Errorf("tls: cert and key must be given together")
		}
		pair, err := tls.LoadX509KeyPair(opts.Cert, opts.Key)
		if err != nil {
			return nil, fmt.Errorf("tls: loading client certificate: %w", err)
		}
		cfg.Certificates = []tls.Certificate{pair}
	}

	if opts.CA != "" {
		pem, err := os.ReadFile(opts.CA)
		if err != nil {
			return nil, fmt.Errorf("tls: reading ca bundle: %w", err)
		}
		pool, err := x509.SystemCertPool()
		if err != nil || pool == nil {
			pool = x509.NewCertPool()
		}
		if !pool.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("tls: ca bundle %q contained no certificates", opts.CA)
		}
		cfg.RootCAs = pool
	}

	return cfg, nil
}
