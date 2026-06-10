// s3_state.go — Sajon Remote State Backend (AWS S3)
//
// Provides transparent remote storage of sajon.lock on AWS S3.
// Uses AWS Signature Version 4 over net/http — zero external dependencies.
//
// When SAJON_REMOTE_BUCKET is set together with AWS_ACCESS_KEY_ID and
// AWS_SECRET_ACCESS_KEY the lock file is downloaded from S3 before every
// command and uploaded back after every successful mutation.
//
// S3 key:    sajon.lock      (at the bucket root)
// Region:    AWS_DEFAULT_REGION env var, defaulting to "us-east-1"
//
// Error policy:
//   - Download: a missing object (404) is treated as a fresh state and returns
//     an empty LockFile — NOT an error.  Any other HTTP error IS fatal so the
//     compiler never silently overwrites remote state with an empty one.
//   - Upload: any failure is always fatal.  A partial upload could corrupt
//     state for the whole team.
package emitter

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// ── Package-level remote state configuration ──────────────────────────────────

// RemoteStateConfig holds the AWS credentials and bucket name that the lock
// layer uses when remote state is enabled.  Populated once at startup by
// ConfigureRemoteState(); never mutated afterwards.
type RemoteStateConfig struct {
	Enabled   bool
	Bucket    string
	Region    string
	AccessKey string
	SecretKey string
}

// remoteState is the package-level singleton populated by ConfigureRemoteState.
var remoteState RemoteStateConfig

// ConfigureRemoteState enables S3-backed state storage.
// Call this at program startup before any ReadLockFile / Flush invocation.
func ConfigureRemoteState(bucket, region, accessKey, secretKey string) {
	if bucket == "" || accessKey == "" || secretKey == "" {
		return // incomplete config — stay in local mode
	}
	if region == "" {
		region = "us-east-1"
	}
	remoteState = RemoteStateConfig{
		Enabled:   true,
		Bucket:    bucket,
		Region:    region,
		AccessKey: accessKey,
		SecretKey: secretKey,
	}
}

// IsRemoteStateEnabled returns true when S3-backed state has been configured.
func IsRemoteStateEnabled() bool { return remoteState.Enabled }

// RemoteStateBucket returns the configured bucket name (empty if not configured).
func RemoteStateBucket() string { return remoteState.Bucket }

// ── S3 lock key ───────────────────────────────────────────────────────────────

const s3LockKey = "sajon.lock"

// ── Download ──────────────────────────────────────────────────────────────────

// S3DownloadLockFile fetches sajon.lock from the remote S3 bucket into memory.
// Returns an empty LockFile (no error) when the object does not yet exist
// (first-time deployment).  Any other S3 error is returned as a hard failure
// so the compiler never silently starts from scratch when state exists.
func S3DownloadLockFile() (*LockFile, error) {
	rs := remoteState
	endpoint := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s",
		rs.Bucket, rs.Region, s3LockKey)

	req, err := s3BuildRequest("GET", endpoint, rs.Region, rs.AccessKey, rs.SecretKey, nil, "")
	if err != nil {
		return nil, fmt.Errorf("s3 build GET request: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("s3 GET sajon.lock: %w", err)
	}
	defer resp.Body.Close()

	// 404 → no state yet, treat as fresh deployment
	if resp.StatusCode == http.StatusNotFound {
		fmt.Printf("  [☁] S3: sajon.lock not found in bucket '%s' — treating as fresh deployment.\n", rs.Bucket)
		return &LockFile{Resources: make(map[string]LockResource)}, nil
	}

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		snippet := string(body)
		if len(snippet) > 200 {
			snippet = snippet[:200] + "…"
		}
		return nil, fmt.Errorf("s3 GET sajon.lock returned HTTP %d: %s", resp.StatusCode, snippet)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("s3 read sajon.lock body: %w", err)
	}

	// Strip UTF-8 BOM if present
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))

	var lf LockFile
	if jsonErr := json.Unmarshal(data, &lf); jsonErr != nil {
		fmt.Printf("  [⚠️ ] S3: sajon.lock in bucket '%s' is corrupted (invalid JSON) — treating as fresh deployment.\n", rs.Bucket)
		return &LockFile{Resources: make(map[string]LockResource)}, nil
	}
	if lf.Resources == nil {
		lf.Resources = make(map[string]LockResource)
	}

	fmt.Printf("  [☁] S3: sajon.lock downloaded from s3://%s/%s (%d resource(s))\n",
		rs.Bucket, s3LockKey, len(lf.Resources))
	return &lf, nil
}

// ── Upload ────────────────────────────────────────────────────────────────────

// S3UploadLockFile marshals lf to JSON and PUTs it to the remote S3 bucket.
// This is always a fatal failure if the upload does not succeed — partial
// uploads corrupt the shared team state.
func S3UploadLockFile(lf *LockFile) error {
	rs := remoteState

	data, err := json.MarshalIndent(lf, "", "  ")
	if err != nil {
		return fmt.Errorf("s3 marshal sajon.lock: %w", err)
	}

	endpoint := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s",
		rs.Bucket, rs.Region, s3LockKey)

	req, err := s3BuildRequest("PUT", endpoint, rs.Region, rs.AccessKey, rs.SecretKey,
		data, "application/json")
	if err != nil {
		return fmt.Errorf("s3 build PUT request: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("s3 PUT sajon.lock: %w", err)
	}
	defer resp.Body.Close()
	io.ReadAll(resp.Body) // drain

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("s3 PUT sajon.lock returned HTTP %d", resp.StatusCode)
	}

	fmt.Printf("  [☁] S3: sajon.lock uploaded → s3://%s/%s (%d resource(s))\n",
		rs.Bucket, s3LockKey, len(lf.Resources))
	return nil
}

// ── S3 Delete ─────────────────────────────────────────────────────────────────

// S3DeleteLockFile deletes the sajon.lock object from the remote S3 bucket.
// A 404 response is treated as success (already gone).
func S3DeleteLockFile() error {
	rs := remoteState
	endpoint := fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s",
		rs.Bucket, rs.Region, s3LockKey)

	req, err := s3BuildRequest("DELETE", endpoint, rs.Region, rs.AccessKey, rs.SecretKey, nil, "")
	if err != nil {
		return fmt.Errorf("s3 build DELETE request: %w", err)
	}

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("s3 DELETE sajon.lock: %w", err)
	}
	defer resp.Body.Close()
	io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusNoContent || resp.StatusCode == http.StatusOK ||
		resp.StatusCode == http.StatusNotFound {
		fmt.Printf("  [☁] S3: sajon.lock deleted from s3://%s/%s\n", rs.Bucket, s3LockKey)
		return nil
	}
	return fmt.Errorf("s3 DELETE sajon.lock returned HTTP %d", resp.StatusCode)
}

// ── AWS Signature V4 for S3 ───────────────────────────────────────────────────
// Standalone signing functions (no struct receiver) so they can be called
// independently of AWSEmitter.

func s3BuildRequest(method, endpoint, region, accessKey, secretKey string, body []byte, contentType string) (*http.Request, error) {
	t := time.Now().UTC()
	amzDate  := t.Format("20060102T150405Z")
	dateStamp := t.Format("20060102")

	var bodyReader io.Reader
	if len(body) > 0 {
		bodyReader = bytes.NewReader(body)
	}

	req, err := http.NewRequest(method, endpoint, bodyReader)
	if err != nil {
		return nil, err
	}

	// Required headers
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", s3Sha256Hex(body))
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	// Derive Host from the URL
	parsedURL, err := url.Parse(endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse s3 endpoint: %w", err)
	}
	req.Header.Set("Host", parsedURL.Host)

	// ── Canonical request ──────────────────────────────────────────────────
	canonicalHeaders, signedHeaders := s3CanonicalHeaders(req)

	canonicalURI := parsedURL.EscapedPath()
	if canonicalURI == "" {
		canonicalURI = "/"
	}
	canonicalQueryString := s3CanonicalQueryString(parsedURL.RawQuery)

	bodyHash := s3Sha256Hex(body)
	canonicalRequest := strings.Join([]string{
		method,
		canonicalURI,
		canonicalQueryString,
		canonicalHeaders,
		signedHeaders,
		bodyHash,
	}, "\n")

	// ── String to sign ─────────────────────────────────────────────────────
	credScope := strings.Join([]string{dateStamp, region, "s3", "aws4_request"}, "/")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		credScope,
		s3Sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	// ── Signing key ────────────────────────────────────────────────────────
	signingKey := s3DeriveSigningKey(secretKey, dateStamp, region, "s3")
	signature  := hex.EncodeToString(s3HmacSHA256(signingKey, []byte(stringToSign)))

	// ── Authorization header ───────────────────────────────────────────────
	authHeader := fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		accessKey, credScope, signedHeaders, signature,
	)
	req.Header.Set("Authorization", authHeader)

	return req, nil
}

func s3CanonicalHeaders(req *http.Request) (string, string) {
	type kv struct{ k, v string }
	var headers []kv
	for k, vs := range req.Header {
		lk := strings.ToLower(k)
		headers = append(headers, kv{lk, strings.TrimSpace(strings.Join(vs, ","))})
	}
	sort.Slice(headers, func(i, j int) bool { return headers[i].k < headers[j].k })

	var canonical, signed strings.Builder
	for _, h := range headers {
		canonical.WriteString(h.k + ":" + h.v + "\n")
		if signed.Len() > 0 {
			signed.WriteByte(';')
		}
		signed.WriteString(h.k)
	}
	return canonical.String(), signed.String()
}

func s3CanonicalQueryString(rawQuery string) string {
	if rawQuery == "" {
		return ""
	}
	params, _ := url.ParseQuery(rawQuery)
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		for _, v := range params[k] {
			parts = append(parts, url.QueryEscape(k)+"="+url.QueryEscape(v))
		}
	}
	return strings.Join(parts, "&")
}

func s3DeriveSigningKey(secretKey, dateStamp, region, service string) []byte {
	kDate    := s3HmacSHA256([]byte("AWS4"+secretKey), []byte(dateStamp))
	kRegion  := s3HmacSHA256(kDate, []byte(region))
	kService := s3HmacSHA256(kRegion, []byte(service))
	return s3HmacSHA256(kService, []byte("aws4_request"))
}

func s3HmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func s3Sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}
