package main

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// Credential sources for the S3 client, in the order newS3Client prefers them:
//
//  1. static      — access_key_id / secret_access_key (+ session_token) props,
//                   already resolved (and redacted) host-side from env()/vault().
//  2. webIdentity  — role_arn + web_identity_token_file props: STS
//                   AssumeRoleWithWebIdentity, the EKS IRSA / Pod Identity path.
//                   The token file is read by this process (it is mounted into
//                   the pod); no ambient AWS_* env is needed.
//  3. imds         — use_imds: true: IMDSv2 on 169.254.169.254, the EC2 / EKS
//                   node instance-role path. Pure HTTP, no env needed.
//  4. anonymous    — nothing configured: requests go out unsigned (public
//                   buckets, or a MinIO allowing anonymous access).
//
// webIdentity and imds cache the short-lived credentials and refresh a few
// minutes before expiry.

type awsCreds struct {
	accessKeyID     string
	secretAccessKey string
	sessionToken    string
	expires         time.Time // zero => non-expiring (static)
	anonymous       bool
}

func (c awsCreds) valid(now time.Time) bool {
	if c.anonymous || c.accessKeyID == "" {
		return false
	}
	return c.expires.IsZero() || c.expires.After(now.Add(5*time.Minute))
}

type credentialSource interface {
	retrieve(ctx context.Context) (awsCreds, error)
}

// --- static / anonymous ----------------------------------------------------

type staticCreds struct{ c awsCreds }

func (s staticCreds) retrieve(context.Context) (awsCreds, error) { return s.c, nil }

type anonCreds struct{}

func (anonCreds) retrieve(context.Context) (awsCreds, error) {
	return awsCreds{anonymous: true}, nil
}

// --- STS AssumeRoleWithWebIdentity (IRSA) ---------------------------------

type webIdentityCreds struct {
	httpc       *http.Client
	tokenFile   string
	roleARN     string
	sessionName string
	stsEndpoint string // e.g. https://sts.us-east-1.amazonaws.com
	now         func() time.Time

	mu     sync.Mutex
	cached awsCreds
}

func (w *webIdentityCreds) retrieve(ctx context.Context) (awsCreds, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.cached.valid(w.now()) {
		return w.cached, nil
	}

	tok, err := os.ReadFile(w.tokenFile)
	if err != nil {
		return awsCreds{}, fmt.Errorf("reading web_identity_token_file %q: %w", w.tokenFile, err)
	}

	form := url.Values{}
	form.Set("Action", "AssumeRoleWithWebIdentity")
	form.Set("Version", "2011-06-15")
	form.Set("RoleArn", w.roleARN)
	form.Set("RoleSessionName", w.sessionName)
	form.Set("WebIdentityToken", strings.TrimSpace(string(tok)))
	form.Set("DurationSeconds", "3600")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(w.stsEndpoint, "/")+"/", strings.NewReader(form.Encode()))
	if err != nil {
		return awsCreds{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/xml")

	resp, err := w.httpc.Do(req)
	if err != nil {
		return awsCreds{}, fmt.Errorf("STS AssumeRoleWithWebIdentity: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != 200 {
		return awsCreds{}, fmt.Errorf("STS AssumeRoleWithWebIdentity: HTTP %d: %s",
			resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var parsed struct {
		AccessKeyID  string `xml:"AssumeRoleWithWebIdentityResult>Credentials>AccessKeyId"`
		SecretKey    string `xml:"AssumeRoleWithWebIdentityResult>Credentials>SecretAccessKey"`
		SessionToken string `xml:"AssumeRoleWithWebIdentityResult>Credentials>SessionToken"`
		Expiration   string `xml:"AssumeRoleWithWebIdentityResult>Credentials>Expiration"`
	}
	if err := xml.Unmarshal(body, &parsed); err != nil || parsed.AccessKeyID == "" {
		return awsCreds{}, fmt.Errorf("STS AssumeRoleWithWebIdentity: unexpected response: %s",
			strings.TrimSpace(string(body)))
	}
	exp, _ := time.Parse(time.RFC3339, parsed.Expiration)
	w.cached = awsCreds{
		accessKeyID:     parsed.AccessKeyID,
		secretAccessKey: parsed.SecretKey,
		sessionToken:    parsed.SessionToken,
		expires:         exp,
	}
	return w.cached, nil
}

// --- IMDSv2 (EC2 / EKS node instance role) --------------------------------

type imdsCreds struct {
	httpc    *http.Client
	endpoint string // http://169.254.169.254
	now      func() time.Time

	mu     sync.Mutex
	cached awsCreds
}

func (m *imdsCreds) retrieve(ctx context.Context) (awsCreds, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cached.valid(m.now()) {
		return m.cached, nil
	}
	base := strings.TrimRight(m.endpoint, "/")

	// 1. session token
	tReq, _ := http.NewRequestWithContext(ctx, http.MethodPut, base+"/latest/api/token", nil)
	tReq.Header.Set("X-aws-ec2-metadata-token-ttl-seconds", "21600")
	tResp, err := m.httpc.Do(tReq)
	if err != nil {
		return awsCreds{}, fmt.Errorf("IMDSv2 token: %w", err)
	}
	tokBytes, _ := io.ReadAll(io.LimitReader(tResp.Body, 8<<10))
	tResp.Body.Close()
	if tResp.StatusCode != 200 {
		return awsCreds{}, fmt.Errorf("IMDSv2 token: HTTP %d", tResp.StatusCode)
	}
	imdsTok := strings.TrimSpace(string(tokBytes))

	get := func(path string) ([]byte, int, error) {
		r, _ := http.NewRequestWithContext(ctx, http.MethodGet, base+path, nil)
		r.Header.Set("X-aws-ec2-metadata-token", imdsTok)
		resp, err := m.httpc.Do(r)
		if err != nil {
			return nil, 0, err
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
		return b, resp.StatusCode, nil
	}

	// 2. role name
	roleBytes, st, err := get("/latest/meta-data/iam/security-credentials/")
	if err != nil {
		return awsCreds{}, fmt.Errorf("IMDS role: %w", err)
	}
	if st != 200 || len(roleBytes) == 0 {
		return awsCreds{}, fmt.Errorf("IMDS role: HTTP %d (no instance role attached?)", st)
	}
	role := strings.TrimSpace(strings.SplitN(string(roleBytes), "\n", 2)[0])

	// 3. the credentials
	credBytes, st, err := get("/latest/meta-data/iam/security-credentials/" + role)
	if err != nil {
		return awsCreds{}, fmt.Errorf("IMDS credentials: %w", err)
	}
	if st != 200 {
		return awsCreds{}, fmt.Errorf("IMDS credentials: HTTP %d", st)
	}
	var parsed struct {
		Code            string
		AccessKeyID     string
		SecretAccessKey string
		Token           string
		Expiration      string
	}
	if err := json.Unmarshal(credBytes, &parsed); err != nil || parsed.AccessKeyID == "" {
		return awsCreds{}, fmt.Errorf("IMDS credentials: unexpected response")
	}
	exp, _ := time.Parse(time.RFC3339, parsed.Expiration)
	m.cached = awsCreds{
		accessKeyID:     parsed.AccessKeyID,
		secretAccessKey: parsed.SecretAccessKey,
		sessionToken:    parsed.Token,
		expires:         exp,
	}
	return m.cached, nil
}

// --- retry backoff -------------------------------------------------------

// backoff returns the sleep before retry attempt n (0-based): exponential
// base<<n, capped at capDur, then full jitter.
func backoff(n int, base, capDur time.Duration) time.Duration {
	d := base << n
	if d > capDur || d <= 0 {
		d = capDur
	}
	return time.Duration(rand.Int63n(int64(d)) + 1)
}
