package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// A tiny, dependency-free Amazon S3 client: just the four object operations
// this extension needs (PutObject, GetObject, DeleteObject, ListObjectsV2)
// with AWS Signature Version 4 request signing, a credential-source seam
// (static / IRSA web-identity / IMDSv2 / anonymous — see creds.go) and
// retry-with-jitter on transient failures. Talks to real S3 and to any
// S3-compatible endpoint (MinIO, R2, Ceph RGW).

const (
	awsService  = "s3"
	aws4Request = "aws4_request"
	amzDateFmt  = "20060102T150405Z"
	scopeDate   = "20060102"
	// hex(sha256("")) — the payload hash for bodyless requests.
	emptyPayloadSHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"

	defaultMaxRetries = 3
	defaultRetryBase  = 200 * time.Millisecond
	defaultRetryCap   = 5 * time.Second
)

type s3Config struct {
	Bucket         string
	Endpoint       string // e.g. "http://localhost:9000"; empty => real AWS S3
	Region         string // default "us-east-1"
	ForcePathStyle bool   // implied true when Endpoint is set

	// Credentials, tried in this order (see creds.go):
	AccessKeyID  string // static
	SecretKey    string // static
	SessionToken string // static, optional

	WebIdentityTokenFile string // IRSA: path to the projected SA token
	RoleARN              string // IRSA: role to assume
	RoleSessionName      string // IRSA: optional, default "mhl-store-s3"

	UseIMDS bool // EC2/EKS node instance role via IMDSv2

	// Reliability / test seams.
	MaxRetries   int    // <0 => default (3); 0 => no retries
	STSEndpoint  string // default https://sts.<region>.amazonaws.com
	IMDSEndpoint string // default http://169.254.169.254
}

type s3Client struct {
	httpc     *http.Client
	scheme    string // "https" / "http"
	host      string // network host[:port] and the signed Host header
	bucket    string
	region    string
	pathStyle bool
	creds     credentialSource
	now       func() time.Time // overridable for tests

	maxRetries int
	retryBase  time.Duration
	retryCap   time.Duration
}

func newS3Client(cfg s3Config) (*s3Client, error) {
	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, fmt.Errorf("store-s3: the `bucket` property is required")
	}
	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}

	httpc := &http.Client{Timeout: 30 * time.Second}
	now := time.Now

	c := &s3Client{
		httpc:      httpc,
		bucket:     cfg.Bucket,
		region:     region,
		now:        now,
		maxRetries: defaultMaxRetries,
		retryBase:  defaultRetryBase,
		retryCap:   defaultRetryCap,
	}
	if cfg.MaxRetries >= 0 {
		c.maxRetries = cfg.MaxRetries
	}

	switch {
	case cfg.Endpoint != "":
		u, err := url.Parse(ensureScheme(cfg.Endpoint))
		if err != nil || u.Host == "" {
			return nil, fmt.Errorf("store-s3: bad `endpoint` %q", cfg.Endpoint)
		}
		c.scheme, c.host, c.pathStyle = u.Scheme, u.Host, true
	case cfg.ForcePathStyle:
		c.scheme, c.host, c.pathStyle = "https", "s3."+region+".amazonaws.com", true
	default:
		c.scheme, c.host, c.pathStyle = "https", cfg.Bucket+".s3."+region+".amazonaws.com", false
	}

	// Credential source, in preference order.
	switch {
	case cfg.AccessKeyID != "" && cfg.SecretKey != "":
		c.creds = staticCreds{c: awsCreds{
			accessKeyID:     cfg.AccessKeyID,
			secretAccessKey: cfg.SecretKey,
			sessionToken:    cfg.SessionToken,
		}}
	case cfg.WebIdentityTokenFile != "" && cfg.RoleARN != "":
		sess := cfg.RoleSessionName
		if sess == "" {
			sess = "mhl-store-s3"
		}
		sts := cfg.STSEndpoint
		if sts == "" {
			sts = "https://sts." + region + ".amazonaws.com"
		}
		c.creds = &webIdentityCreds{
			httpc: httpc, tokenFile: cfg.WebIdentityTokenFile, roleARN: cfg.RoleARN,
			sessionName: sess, stsEndpoint: sts, now: now,
		}
	case cfg.UseIMDS:
		ep := cfg.IMDSEndpoint
		if ep == "" {
			ep = "http://169.254.169.254"
		}
		c.creds = &imdsCreds{httpc: httpc, endpoint: ep, now: now}
	default:
		c.creds = anonCreds{}
	}

	return c, nil
}

func ensureScheme(s string) string {
	if strings.Contains(s, "://") {
		return s
	}
	return "https://" + s
}

// requestPath is the URL path (bucket-qualified for path-style addressing).
func (c *s3Client) requestPath(objKey string) string {
	if c.pathStyle {
		return "/" + c.bucket + "/" + objKey
	}
	return "/" + objKey
}

// do issues one S3 request, retrying transient failures (transport errors and
// 429 / 5xx) with exponential backoff + full jitter, bounded by maxRetries and
// ctx. Each attempt is freshly signed (the SigV4 timestamp moves).
func (c *s3Client) do(ctx context.Context, method, objKey string, query url.Values, body []byte) (int, []byte, error) {
	cr, err := c.creds.retrieve(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("store-s3: credentials: %w", err)
	}

	var lastStatus int
	var lastBody []byte
	var lastErr error
	for attempt := 0; ; attempt++ {
		st, rb, transportErr := c.attempt(ctx, method, objKey, query, body, cr)
		retryable := transportErr != nil || st == 429 || (st >= 500 && st <= 599)
		if !retryable {
			return st, rb, nil
		}
		lastStatus, lastBody, lastErr = st, rb, transportErr

		if attempt >= c.maxRetries {
			if transportErr != nil {
				return 0, nil, fmt.Errorf("store-s3: %s %s: %w (after %d attempts)",
					method, orRoot(objKey), transportErr, attempt+1)
			}
			return lastStatus, lastBody, nil // let the caller classify the final status
		}
		select {
		case <-ctx.Done():
			if lastErr != nil {
				return 0, nil, errors.Join(lastErr, ctx.Err())
			}
			return 0, nil, ctx.Err()
		case <-time.After(backoff(attempt, c.retryBase, c.retryCap)):
		}
	}
}

// attempt performs exactly one signed request.
func (c *s3Client) attempt(ctx context.Context, method, objKey string, query url.Values, body []byte, cr awsCreds) (int, []byte, error) {
	u := &url.URL{Scheme: c.scheme, Host: c.host, Path: c.requestPath(objKey)}
	if len(query) > 0 {
		u.RawQuery = canonicalQuery(query)
	}

	var rdr io.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), rdr)
	if err != nil {
		return 0, nil, err
	}
	req.Host = c.host

	payloadHash := emptyPayloadSHA256
	if body != nil {
		sum := sha256.Sum256(body)
		payloadHash = hex.EncodeToString(sum[:])
	}
	if cr.anonymous || cr.accessKeyID == "" {
		req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	} else {
		c.sign(req, cr, payloadHash, c.now().UTC())
	}

	resp, err := c.httpc.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	return resp.StatusCode, rb, nil
}

func orRoot(k string) string {
	if k == "" {
		return "?list-type=2"
	}
	return k
}

func (c *s3Client) putObject(ctx context.Context, objKey string, body []byte) error {
	st, rb, err := c.do(ctx, http.MethodPut, objKey, nil, body)
	if err != nil {
		return err
	}
	if st == 200 || st == 201 || st == 204 {
		return nil
	}
	return s3Error("PUT", objKey, st, rb)
}

func (c *s3Client) getObject(ctx context.Context, objKey string) (body []byte, found bool, err error) {
	st, rb, err := c.do(ctx, http.MethodGet, objKey, nil, nil)
	if err != nil {
		return nil, false, err
	}
	switch {
	case st == 200:
		return rb, true, nil
	case st == 404:
		return nil, false, nil
	default:
		return nil, false, s3Error("GET", objKey, st, rb)
	}
}

func (c *s3Client) deleteObject(ctx context.Context, objKey string) error {
	st, rb, err := c.do(ctx, http.MethodDelete, objKey, nil, nil)
	if err != nil {
		return err
	}
	if st == 200 || st == 202 || st == 204 || st == 404 {
		return nil
	}
	return s3Error("DELETE", objKey, st, rb)
}

type listBucketResult struct {
	XMLName               xml.Name `xml:"ListBucketResult"`
	IsTruncated           bool     `xml:"IsTruncated"`
	NextContinuationToken string   `xml:"NextContinuationToken"`
	Contents              []struct {
		Key string `xml:"Key"`
	} `xml:"Contents"`
}

// listKeys returns every object key under s3Prefix, following pagination.
func (c *s3Client) listKeys(ctx context.Context, s3Prefix string) ([]string, error) {
	var out []string
	token := ""
	for {
		q := url.Values{}
		q.Set("list-type", "2")
		if s3Prefix != "" {
			q.Set("prefix", s3Prefix)
		}
		if token != "" {
			q.Set("continuation-token", token)
		}
		st, rb, err := c.do(ctx, http.MethodGet, "", q, nil)
		if err != nil {
			return nil, err
		}
		if st != 200 {
			return nil, s3Error("GET", "?list-type=2", st, rb)
		}
		var r listBucketResult
		if err := xml.Unmarshal(rb, &r); err != nil {
			return nil, fmt.Errorf("store-s3: list: malformed XML: %w", err)
		}
		for _, item := range r.Contents {
			out = append(out, item.Key)
		}
		if !r.IsTruncated || r.NextContinuationToken == "" {
			return out, nil
		}
		token = r.NextContinuationToken
	}
}

// --- SigV4 -------------------------------------------------------------------

func (c *s3Client) sign(req *http.Request, cr awsCreds, payloadHash string, t time.Time) {
	amzDate := t.Format(amzDateFmt)
	dateStamp := t.Format(scopeDate)

	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", payloadHash)
	if cr.sessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", cr.sessionToken)
	}

	type kv struct{ k, v string }
	headers := []kv{{"host", req.URL.Host}}
	for name, vals := range req.Header {
		lk := strings.ToLower(name)
		if lk == "content-type" || lk == "range" || strings.HasPrefix(lk, "x-amz-") {
			headers = append(headers, kv{lk, strings.TrimSpace(strings.Join(vals, ","))})
		}
	}
	sort.Slice(headers, func(i, j int) bool { return headers[i].k < headers[j].k })

	var canonHeaders strings.Builder
	names := make([]string, 0, len(headers))
	for _, h := range headers {
		canonHeaders.WriteString(h.k)
		canonHeaders.WriteByte(':')
		canonHeaders.WriteString(h.v)
		canonHeaders.WriteByte('\n')
		names = append(names, h.k)
	}
	signedHeaders := strings.Join(names, ";")

	canonicalReq := strings.Join([]string{
		req.Method,
		req.URL.EscapedPath(),
		canonicalQuery(req.URL.Query()),
		canonHeaders.String(),
		signedHeaders,
		payloadHash,
	}, "\n")

	scope := dateStamp + "/" + c.region + "/" + awsService + "/" + aws4Request
	crHash := sha256.Sum256([]byte(canonicalReq))
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		scope,
		hex.EncodeToString(crHash[:]),
	}, "\n")

	signingKey := hmacSHA256(
		hmacSHA256(
			hmacSHA256(
				hmacSHA256([]byte("AWS4"+cr.secretAccessKey), []byte(dateStamp)),
				[]byte(c.region)),
			[]byte(awsService)),
		[]byte(aws4Request))
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		cr.accessKeyID, scope, signedHeaders, signature))
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

// canonicalQuery builds the AWS-canonical query string: RFC 3986 percent-
// encoding of every key and value, sorted by key then value, joined with '&'.
func canonicalQuery(v url.Values) string {
	if len(v) == 0 {
		return ""
	}
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var parts []string
	for _, k := range keys {
		vals := append([]string(nil), v[k]...)
		sort.Strings(vals)
		for _, val := range vals {
			parts = append(parts, rfc3986Escape(k)+"="+rfc3986Escape(val))
		}
	}
	return strings.Join(parts, "&")
}

// rfc3986Escape percent-encodes everything except the RFC 3986 unreserved set
// (A-Z a-z 0-9 - _ . ~) — stricter than url.QueryEscape, which AWS requires.
func rfc3986Escape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		switch {
		case ch >= 'A' && ch <= 'Z', ch >= 'a' && ch <= 'z', ch >= '0' && ch <= '9',
			ch == '-', ch == '_', ch == '.', ch == '~':
			b.WriteByte(ch)
		default:
			fmt.Fprintf(&b, "%%%02X", ch)
		}
	}
	return b.String()
}

func s3Error(op, key string, status int, body []byte) error {
	var e struct {
		Code    string `xml:"Code"`
		Message string `xml:"Message"`
	}
	if err := xml.Unmarshal(body, &e); err == nil && e.Code != "" {
		return fmt.Errorf("store-s3: %s %s: %d %s: %s", op, key, status, e.Code, e.Message)
	}
	snippet := strings.TrimSpace(string(body))
	if len(snippet) > 300 {
		snippet = snippet[:300]
	}
	return fmt.Errorf("store-s3: %s %s: HTTP %d: %s", op, key, status, snippet)
}
