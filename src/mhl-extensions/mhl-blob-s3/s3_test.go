package main

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// awsDocGetObjectVector is the worked example from the AWS documentation
// "Signature Calculations for the Authorization Header: GET Object"
// (service s3, region us-east-1). It pins our SigV4 implementation to a
// value AWS publishes, with no network.
func TestSignMatchesAWSDocVector(t *testing.T) {
	c := &s3Client{scheme: "https", host: "examplebucket.s3.amazonaws.com", region: "us-east-1"}
	cr := awsCreds{
		accessKeyID:     "AKIAIOSFODNN7EXAMPLE",
		secretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
	}
	req, err := http.NewRequest(http.MethodGet, "https://examplebucket.s3.amazonaws.com/test.txt", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Range", "bytes=0-9")

	c.sign(req, cr, emptyPayloadSHA256, time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC))

	auth := req.Header.Get("Authorization")
	const wantSig = "Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"
	if !strings.Contains(auth, wantSig) {
		t.Fatalf("signature mismatch\n got: %s\nwant substring: %s", auth, wantSig)
	}
	const wantSH = "SignedHeaders=host;range;x-amz-content-sha256;x-amz-date"
	if !strings.Contains(auth, wantSH) {
		t.Fatalf("signed headers mismatch\n got: %s\nwant substring: %s", auth, wantSH)
	}
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request,") {
		t.Fatalf("credential scope mismatch: %s", auth)
	}
}

func TestSessionTokenIsSigned(t *testing.T) {
	c := &s3Client{scheme: "https", host: "b.s3.us-east-1.amazonaws.com", region: "us-east-1"}
	cr := awsCreds{accessKeyID: "AK", secretAccessKey: "SK", sessionToken: "session-token-xyz"}
	req, _ := http.NewRequest(http.MethodGet, "https://b.s3.us-east-1.amazonaws.com/k.json", nil)
	c.sign(req, cr, emptyPayloadSHA256, time.Unix(0, 0).UTC())

	if req.Header.Get("X-Amz-Security-Token") != "session-token-xyz" {
		t.Fatal("security token header not set")
	}
	if !strings.Contains(req.Header.Get("Authorization"), "x-amz-security-token") {
		t.Fatalf("security token not in SignedHeaders: %s", req.Header.Get("Authorization"))
	}
}

func TestRFC3986Escape(t *testing.T) {
	cases := map[string]string{
		"abcABC123-_.~": "abcABC123-_.~",
		"a/b":           "a%2Fb",
		"a b+c":         "a%20b%2Bc",
		"tok==/x":       "tok%3D%3D%2Fx",
	}
	for in, want := range cases {
		if got := rfc3986Escape(in); got != want {
			t.Errorf("rfc3986Escape(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCanonicalQuerySortsAndEncodes(t *testing.T) {
	v := url.Values{}
	v.Set("prefix", "mhl/run/")
	v.Set("list-type", "2")
	v.Set("continuation-token", "1/abc+def==")
	got := canonicalQuery(v)
	want := "continuation-token=1%2Fabc%2Bdef%3D%3D&list-type=2&prefix=mhl%2Frun%2F"
	if got != want {
		t.Fatalf("canonicalQuery = %q, want %q", got, want)
	}
}

func TestNewS3ClientAddressing(t *testing.T) {
	t.Run("endpoint implies path style", func(t *testing.T) {
		c, err := newS3Client(s3Config{Bucket: "st", Endpoint: "http://localhost:9000"})
		if err != nil {
			t.Fatal(err)
		}
		if !c.pathStyle || c.scheme != "http" || c.host != "localhost:9000" {
			t.Fatalf("got scheme=%s host=%s pathStyle=%v", c.scheme, c.host, c.pathStyle)
		}
		if c.requestPath("mhl/a.json") != "/st/mhl/a.json" {
			t.Fatalf("requestPath = %s", c.requestPath("mhl/a.json"))
		}
	})
	t.Run("bare AWS is virtual-host style", func(t *testing.T) {
		c, _ := newS3Client(s3Config{Bucket: "st", Region: "eu-west-1"})
		if c.pathStyle || c.host != "st.s3.eu-west-1.amazonaws.com" {
			t.Fatalf("got host=%s pathStyle=%v", c.host, c.pathStyle)
		}
	})
	t.Run("bucket is required", func(t *testing.T) {
		if _, err := newS3Client(s3Config{}); err == nil {
			t.Fatal("expected error for missing bucket")
		}
	})
}

func TestCredentialSourceSelection(t *testing.T) {
	mk := func(cfg s3Config) any {
		cfg.Bucket, cfg.Endpoint = "b", "http://x:1"
		c, err := newS3Client(cfg)
		if err != nil {
			t.Fatal(err)
		}
		return c.creds
	}
	if _, ok := mk(s3Config{AccessKeyID: "a", SecretKey: "b"}).(staticCreds); !ok {
		t.Error("static creds not selected")
	}
	if _, ok := mk(s3Config{WebIdentityTokenFile: "/tok", RoleARN: "arn:x"}).(*webIdentityCreds); !ok {
		t.Error("web identity not selected")
	}
	if _, ok := mk(s3Config{UseIMDS: true}).(*imdsCreds); !ok {
		t.Error("imds not selected")
	}
	if _, ok := mk(s3Config{}).(anonCreds); !ok {
		t.Error("anonymous not selected as the fallback")
	}
}

// fakeS3 is an in-process HTTP server that behaves enough like path-style S3
// for the client's request shaping — GET/PUT/HEAD/DELETE, ListObjectsV2 with a
// delimiter, and server-side copy via x-amz-copy-source.
func fakeS3(t *testing.T, objects map[string][]byte, onReq func(*http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if onReq != nil {
			onReq(r)
		}
		key := strings.TrimPrefix(r.URL.Path, "/st/")
		switch r.Method {
		case http.MethodPut:
			if src := r.Header.Get("X-Amz-Copy-Source"); src != "" {
				srcKey := srcKeyUnescape(strings.TrimPrefix(src, "/st/"))
				sb, ok := objects[srcKey]
				if !ok {
					w.WriteHeader(404)
					return
				}
				cp := make([]byte, len(sb))
				copy(cp, sb)
				objects[key] = cp
				_, _ = w.Write([]byte(`<CopyObjectResult><ETag>"c"</ETag></CopyObjectResult>`))
				return
			}
			b, _ := io.ReadAll(r.Body)
			objects[key] = b
			w.WriteHeader(200)
		case http.MethodHead:
			b, ok := objects[key]
			if !ok {
				w.WriteHeader(404)
				return
			}
			w.Header().Set("Content-Length", strconv.Itoa(len(b)))
			w.Header().Set("ETag", `"abc123"`)
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(200)
		case http.MethodGet:
			if r.URL.Query().Get("list-type") == "2" {
				pfx := r.URL.Query().Get("prefix")
				delim := r.URL.Query().Get("delimiter")
				var sb strings.Builder
				sb.WriteString(`<ListBucketResult><IsTruncated>false</IsTruncated>`)
				seenCP := map[string]bool{}
				for k, v := range objects {
					if !strings.HasPrefix(k, pfx) {
						continue
					}
					if delim != "" {
						rest := k[len(pfx):]
						if i := strings.Index(rest, delim); i >= 0 {
							cp := pfx + rest[:i+len(delim)]
							if !seenCP[cp] {
								seenCP[cp] = true
								sb.WriteString("<CommonPrefixes><Prefix>" + cp + "</Prefix></CommonPrefixes>")
							}
							continue
						}
					}
					sb.WriteString("<Contents><Key>" + k + "</Key><Size>" +
						strconv.Itoa(len(v)) + "</Size><ETag>&quot;e&quot;</ETag></Contents>")
				}
				sb.WriteString(`</ListBucketResult>`)
				_, _ = w.Write([]byte(sb.String()))
				return
			}
			b, ok := objects[key]
			if !ok {
				w.WriteHeader(404)
				return
			}
			w.Header().Set("Content-Type", "text/plain")
			w.Header().Set("ETag", `"abc123"`)
			_, _ = w.Write(b)
		case http.MethodDelete:
			delete(objects, key)
			w.WriteHeader(204)
		}
	}))
}

func srcKeyUnescape(s string) string {
	u, err := url.PathUnescape(s)
	if err != nil {
		return s
	}
	return u
}

// TestRoundTripAgainstFakeS3 exercises put/get/head/exists/list/copy/delete.
func TestRoundTripAgainstFakeS3(t *testing.T) {
	objects := map[string][]byte{}
	srv := fakeS3(t, objects, func(r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Errorf("unsigned request: %s %s", r.Method, r.URL)
		}
	})
	defer srv.Close()

	b := &blob{prefix: "app/"}
	var err error
	b.cli, err = newS3Client(s3Config{Bucket: "st", Endpoint: srv.URL, AccessKeyID: "AK", SecretKey: "SK"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if _, _, ok, err := b.cli.getObject(ctx, b.objKey("q3/summary.csv")); err != nil || ok {
		t.Fatalf("get miss: ok=%v err=%v", ok, err)
	}
	if _, ok, err := b.cli.headObject(ctx, b.objKey("q3/summary.csv")); err != nil || ok {
		t.Fatalf("head miss: ok=%v err=%v", ok, err)
	}
	if err := b.cli.putObject(ctx, b.objKey("q3/summary.csv"), []byte("a,b\n1,2\n"), "text/csv"); err != nil {
		t.Fatal(err)
	}
	body, _, ok, err := b.cli.getObject(ctx, b.objKey("q3/summary.csv"))
	if err != nil || !ok || string(body) != "a,b\n1,2\n" {
		t.Fatalf("get hit: %q ok=%v err=%v", body, ok, err)
	}
	m, ok, err := b.cli.headObject(ctx, b.objKey("q3/summary.csv"))
	if err != nil || !ok || m.Size != 8 || m.ETag != "abc123" {
		t.Fatalf("head hit: %+v ok=%v err=%v", m, ok, err)
	}

	if err := b.cli.copyObject(ctx, b.objKey("q3/summary.csv"), b.objKey("archive/summary.csv")); err != nil {
		t.Fatalf("copy: %v", err)
	}
	if _, ok, _ := b.cli.headObject(ctx, b.objKey("archive/summary.csv")); !ok {
		t.Fatal("copy target missing")
	}

	_ = b.cli.putObject(ctx, b.objKey("q3/raw.json"), []byte("{}"), "")
	objs, cps, err := b.cli.listObjects(ctx, b.prefix+"q3/", "/")
	if err != nil {
		t.Fatal(err)
	}
	if len(objs) != 2 || len(cps) != 0 {
		t.Fatalf("list q3/ delim=/: objs=%v cps=%v", objs, cps)
	}
	objs, cps, _ = b.cli.listObjects(ctx, b.prefix, "/")
	if len(cps) != 2 { // app/q3/ and app/archive/
		t.Fatalf("list app/ delim=/: want 2 common prefixes, got %v (objs %v)", cps, objs)
	}

	if err := b.cli.deleteObject(ctx, b.objKey("q3/summary.csv")); err != nil {
		t.Fatal(err)
	}
	if err := b.cli.deleteObject(ctx, b.objKey("q3/summary.csv")); err != nil {
		t.Fatalf("second delete not idempotent: %v", err)
	}
}

// TestPresignQueryForm checks the presigned-URL shape without a network call:
// the X-Amz-* query params, the signed-headers list, and a signature.
func TestPresignQueryForm(t *testing.T) {
	c, _ := newS3Client(s3Config{Bucket: "b", Region: "us-east-1", AccessKeyID: "AK", SecretKey: "SK"})
	c.now = func() time.Time { return time.Date(2026, 9, 6, 12, 0, 0, 0, time.UTC) }

	u, err := c.presign(context.Background(), "GET", "reports/q3.csv", 15*time.Minute, nil)
	if err != nil {
		t.Fatal(err)
	}
	pu, _ := url.Parse(u)
	q := pu.Query()
	if q.Get("X-Amz-Algorithm") != "AWS4-HMAC-SHA256" {
		t.Errorf("algorithm = %q", q.Get("X-Amz-Algorithm"))
	}
	if q.Get("X-Amz-Expires") != "900" {
		t.Errorf("expires = %q, want 900", q.Get("X-Amz-Expires"))
	}
	if q.Get("X-Amz-SignedHeaders") != "host" {
		t.Errorf("signed headers = %q", q.Get("X-Amz-SignedHeaders"))
	}
	if len(q.Get("X-Amz-Signature")) != 64 {
		t.Errorf("signature not a 64-hex string: %q", q.Get("X-Amz-Signature"))
	}
	if !strings.HasPrefix(q.Get("X-Amz-Credential"), "AK/20260906/us-east-1/s3/aws4_request") {
		t.Errorf("credential = %q", q.Get("X-Amz-Credential"))
	}

	pu2, _ := url.Parse(mustPresign(t, c, "PUT", "up/x", time.Hour, map[string]string{"content-type": "image/png"}))
	if sh := pu2.Query().Get("X-Amz-SignedHeaders"); sh != "content-type;host" {
		t.Errorf("presign_put signed headers = %q, want content-type;host", sh)
	}

	anon, _ := newS3Client(s3Config{Bucket: "b", Endpoint: "http://x:1"})
	if _, err := anon.presign(context.Background(), "GET", "k", time.Minute, nil); err == nil {
		t.Error("anonymous presign should fail")
	}
}

func mustPresign(t *testing.T, c *s3Client, method, key string, d time.Duration, extra map[string]string) string {
	t.Helper()
	u, err := c.presign(context.Background(), method, key, d, extra)
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestRetryThenSucceed(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&n, 1) <= 2 {
			w.WriteHeader(503)
			_, _ = w.Write([]byte(`<Error><Code>SlowDown</Code><Message>slow down</Message></Error>`))
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()

	c, _ := newS3Client(s3Config{Bucket: "st", Endpoint: srv.URL, AccessKeyID: "AK", SecretKey: "SK", MaxRetries: 5})
	c.retryBase, c.retryCap = time.Millisecond, 2*time.Millisecond

	if err := c.putObject(context.Background(), "mhl/k.json", []byte("v"), ""); err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if got := atomic.LoadInt32(&n); got != 3 {
		t.Fatalf("expected 3 attempts (2x503 + 1x200), got %d", got)
	}
}

func TestRetryExhausted(t *testing.T) {
	var n int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&n, 1)
		w.WriteHeader(503)
	}))
	defer srv.Close()

	c, _ := newS3Client(s3Config{Bucket: "st", Endpoint: srv.URL, AccessKeyID: "AK", SecretKey: "SK", MaxRetries: 2})
	c.retryBase, c.retryCap = time.Millisecond, 2*time.Millisecond

	err := c.putObject(context.Background(), "mhl/k.json", []byte("v"), "")
	if err == nil || !strings.Contains(err.Error(), "503") {
		t.Fatalf("expected a 503 error after exhausting retries, got %v", err)
	}
	if got := atomic.LoadInt32(&n); got != 3 {
		t.Fatalf("expected 3 attempts (1 + 2 retries), got %d", got)
	}
}

func TestWebIdentityCredentials(t *testing.T) {
	sts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		if r.FormValue("Action") != "AssumeRoleWithWebIdentity" || r.FormValue("WebIdentityToken") != "jwt-token-here" {
			t.Errorf("unexpected STS request: %v", r.Form)
		}
		_, _ = w.Write([]byte(`<AssumeRoleWithWebIdentityResponse><AssumeRoleWithWebIdentityResult><Credentials>
			<AccessKeyId>WIA</AccessKeyId><SecretAccessKey>WIS</SecretAccessKey>
			<SessionToken>WIT</SessionToken><Expiration>2999-01-01T00:00:00Z</Expiration>
			</Credentials></AssumeRoleWithWebIdentityResult></AssumeRoleWithWebIdentityResponse>`))
	}))
	defer sts.Close()

	tokFile := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(tokFile, []byte("jwt-token-here\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	var gotAuth, gotTok string
	s3 := fakeS3(t, map[string][]byte{}, func(r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotTok = r.Header.Get("X-Amz-Security-Token")
	})
	defer s3.Close()

	c, err := newS3Client(s3Config{
		Bucket: "st", Endpoint: s3.URL,
		WebIdentityTokenFile: tokFile, RoleARN: "arn:aws:iam::1:role/r", STSEndpoint: sts.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := c.getObject(context.Background(), "mhl/k.json"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotAuth, "Credential=WIA/") {
		t.Fatalf("request not signed with STS creds: %q", gotAuth)
	}
	if gotTok != "WIT" {
		t.Fatalf("session token from STS not forwarded: %q", gotTok)
	}
}

func TestIMDSCredentials(t *testing.T) {
	imds := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/latest/api/token":
			if r.Header.Get("X-aws-ec2-metadata-token-ttl-seconds") == "" {
				t.Error("IMDSv2 token request missing TTL header")
			}
			_, _ = w.Write([]byte("imds-session-token"))
		case r.URL.Path == "/latest/meta-data/iam/security-credentials/":
			if r.Header.Get("X-aws-ec2-metadata-token") != "imds-session-token" {
				t.Error("metadata request missing IMDSv2 token header")
			}
			_, _ = w.Write([]byte("noderole"))
		case r.URL.Path == "/latest/meta-data/iam/security-credentials/noderole":
			_, _ = w.Write([]byte(`{"Code":"Success","AccessKeyId":"IMA","SecretAccessKey":"IMS","Token":"IMT","Expiration":"2999-01-01T00:00:00Z"}`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer imds.Close()

	var gotAuth string
	s3 := fakeS3(t, map[string][]byte{}, func(r *http.Request) { gotAuth = r.Header.Get("Authorization") })
	defer s3.Close()

	c, err := newS3Client(s3Config{Bucket: "st", Endpoint: s3.URL, UseIMDS: true, IMDSEndpoint: imds.URL})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.putObject(context.Background(), "mhl/k.json", []byte("v"), ""); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotAuth, "Credential=IMA/") {
		t.Fatalf("request not signed with IMDS creds: %q", gotAuth)
	}
}

func TestAnonymousRequestsAreUnsigned(t *testing.T) {
	var sawAuth bool
	var sawHash string
	s3 := fakeS3(t, map[string][]byte{}, func(r *http.Request) {
		sawAuth = r.Header.Get("Authorization") != ""
		sawHash = r.Header.Get("X-Amz-Content-Sha256")
	})
	defer s3.Close()

	c, _ := newS3Client(s3Config{Bucket: "st", Endpoint: s3.URL})
	if _, _, _, err := c.getObject(context.Background(), "mhl/k.json"); err != nil {
		t.Fatal(err)
	}
	if sawAuth {
		t.Fatal("anonymous request carried an Authorization header")
	}
	if sawHash == "" {
		t.Fatal("anonymous request missing X-Amz-Content-Sha256")
	}
}
