package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
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
// for the store's key mapping and the client's request shaping.
func fakeS3(t *testing.T, objects map[string][]byte, onReq func(*http.Request)) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if onReq != nil {
			onReq(r)
		}
		key := strings.TrimPrefix(r.URL.Path, "/st/")
		switch r.Method {
		case http.MethodPut:
			b := make([]byte, r.ContentLength)
			_, _ = r.Body.Read(b)
			objects[key] = b
			w.WriteHeader(200)
		case http.MethodGet:
			if r.URL.Query().Get("list-type") == "2" {
				pfx := r.URL.Query().Get("prefix")
				var sb strings.Builder
				sb.WriteString(`<ListBucketResult><IsTruncated>false</IsTruncated>`)
				for k := range objects {
					if strings.HasPrefix(k, pfx) {
						sb.WriteString("<Contents><Key>" + k + "</Key></Contents>")
					}
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
			_, _ = w.Write(b)
		case http.MethodDelete:
			delete(objects, key)
			w.WriteHeader(204)
		}
	}))
}

// TestRoundTripAgainstFakeS3 exercises put/get/delete/list end to end.
func TestRoundTripAgainstFakeS3(t *testing.T) {
	objects := map[string][]byte{}
	srv := fakeS3(t, objects, func(r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			t.Errorf("unsigned request: %s %s", r.Method, r.URL)
		}
	})
	defer srv.Close()

	st := &store{prefix: "mhl/"}
	var err error
	st.cli, err = newS3Client(s3Config{Bucket: "st", Endpoint: srv.URL, AccessKeyID: "AK", SecretKey: "SK"})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if b, ok, err := st.cli.getObject(ctx, st.objKey("run/1/checkpoint/P")); err != nil || ok || b != nil {
		t.Fatalf("get miss: b=%v ok=%v err=%v", b, ok, err)
	}
	if err := st.cli.putObject(ctx, st.objKey("run/1/checkpoint/P"), []byte(`{"step":"gate"}`)); err != nil {
		t.Fatal(err)
	}
	b, ok, err := st.cli.getObject(ctx, st.objKey("run/1/checkpoint/P"))
	if err != nil || !ok {
		t.Fatalf("get hit: ok=%v err=%v", ok, err)
	}
	var v map[string]any
	if json.Unmarshal(b, &v); v["step"] != "gate" {
		t.Fatalf("round-tripped value = %v", v)
	}
	if err := st.cli.putObject(ctx, st.objKey("session/abc"), []byte(`"s"`)); err != nil {
		t.Fatal(err)
	}
	keys, err := st.listLogical(ctx, "run/")
	if err != nil || len(keys) != 1 || keys[0] != "run/1/checkpoint/P" {
		t.Fatalf("listLogical(run/) = %v err=%v", keys, err)
	}
	all, _ := st.listLogical(ctx, "")
	if len(all) != 2 {
		t.Fatalf("listLogical() = %v", all)
	}
	if err := st.cli.deleteObject(ctx, st.objKey("run/1/checkpoint/P")); err != nil {
		t.Fatal(err)
	}
	if err := st.cli.deleteObject(ctx, st.objKey("run/1/checkpoint/P")); err != nil {
		t.Fatalf("second delete not idempotent: %v", err)
	}
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

	if err := c.putObject(context.Background(), "mhl/k.json", []byte("v")); err != nil {
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

	err := c.putObject(context.Background(), "mhl/k.json", []byte("v"))
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
	if _, _, err := c.getObject(context.Background(), "mhl/k.json"); err != nil {
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
	if err := c.putObject(context.Background(), "mhl/k.json", []byte("v")); err != nil {
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
	if _, _, err := c.getObject(context.Background(), "mhl/k.json"); err != nil {
		t.Fatal(err)
	}
	if sawAuth {
		t.Fatal("anonymous request carried an Authorization header")
	}
	if sawHash == "" {
		t.Fatal("anonymous request missing X-Amz-Content-Sha256")
	}
}
