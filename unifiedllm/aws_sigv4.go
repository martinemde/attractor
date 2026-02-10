package unifiedllm

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// awsCredentials holds AWS authentication credentials.
type awsCredentials struct {
	AccessKeyID    string
	SecretAccessKey string
	SessionToken   string
}

// awsSigV4Sign signs an HTTP request using AWS Signature Version 4.
// The now parameter allows deterministic signing for testing.
func awsSigV4Sign(req *http.Request, body []byte, creds awsCredentials, region, service string, now time.Time) {
	dateStamp := now.Format("20060102")
	amzDate := now.Format("20060102T150405Z")

	req.Header.Set("x-amz-date", amzDate)
	if creds.SessionToken != "" {
		req.Header.Set("x-amz-security-token", creds.SessionToken)
	}

	payloadHash := sha256Hex(body)
	req.Header.Set("x-amz-content-sha256", payloadHash)

	canonHeaders, signedHeaders := buildCanonicalHeaders(req)

	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURIPath(req.URL),
		canonicalQueryString(req.URL),
		canonHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")

	credentialScope := fmt.Sprintf("%s/%s/%s/aws4_request", dateStamp, region, service)
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		credentialScope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")

	signingKey := awsDeriveSigningKey(creds.SecretAccessKey, dateStamp, region, service)
	signature := hex.EncodeToString(hmacSHA256(signingKey, []byte(stringToSign)))

	req.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		creds.AccessKeyID, credentialScope, signedHeaders, signature,
	))
}

func sha256Hex(data []byte) string {
	h := sha256.Sum256(data)
	return hex.EncodeToString(h[:])
}

func hmacSHA256(key, data []byte) []byte {
	h := hmac.New(sha256.New, key)
	h.Write(data)
	return h.Sum(nil)
}

func awsDeriveSigningKey(secret, dateStamp, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), []byte(dateStamp))
	kRegion := hmacSHA256(kDate, []byte(region))
	kService := hmacSHA256(kRegion, []byte(service))
	return hmacSHA256(kService, []byte("aws4_request"))
}

// canonicalURIPath returns the URI-encoded path for the canonical request.
func canonicalURIPath(u *url.URL) string {
	path := u.EscapedPath()
	if path == "" {
		return "/"
	}
	return path
}

// canonicalQueryString returns sorted, encoded query parameters.
func canonicalQueryString(u *url.URL) string {
	params := u.Query()
	if len(params) == 0 {
		return ""
	}
	var parts []string
	for key, values := range params {
		for _, val := range values {
			parts = append(parts, url.QueryEscape(key)+"="+url.QueryEscape(val))
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, "&")
}

// buildCanonicalHeaders returns canonical headers string and signed headers list.
func buildCanonicalHeaders(req *http.Request) (string, string) {
	headers := make(map[string]string)
	var names []string

	for name, values := range req.Header {
		lower := strings.ToLower(name)
		if lower == "authorization" {
			continue
		}
		headers[lower] = strings.TrimSpace(strings.Join(values, ","))
		names = append(names, lower)
	}

	// Add host header (Go stores it on req.Host, not in req.Header)
	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	if _, exists := headers["host"]; !exists {
		headers["host"] = host
		names = append(names, "host")
	}

	sort.Strings(names)

	// Deduplicate
	unique := names[:0]
	seen := make(map[string]bool, len(names))
	for _, n := range names {
		if !seen[n] {
			seen[n] = true
			unique = append(unique, n)
		}
	}
	names = unique

	var canonical strings.Builder
	for _, name := range names {
		canonical.WriteString(name)
		canonical.WriteByte(':')
		canonical.WriteString(headers[name])
		canonical.WriteByte('\n')
	}

	return canonical.String(), strings.Join(names, ";")
}
