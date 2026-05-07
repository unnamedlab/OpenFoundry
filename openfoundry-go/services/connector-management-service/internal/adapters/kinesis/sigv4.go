package kinesis

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"sort"
	"strings"
	"time"
)

// signRequest applies AWS Signature V4 to req using the provided
// credentials. The body is hashed in full and the request is mutated
// in-place: x-amz-date is set, x-amz-security-token is added when a
// session token is present, and Authorization is appended.
//
// Mirrors the canonical request / string-to-sign / signing-key
// derivation documented in the AWS Signature V4 reference. Kept
// minimal — only what the Kinesis Data API exercises (POST + JSON
// body + virtual-host endpoint).
func signRequest(req *http.Request, body []byte, accessKey, secretKey, sessionToken, region, service string, now time.Time) {
	if req.URL.Path == "" {
		req.URL.Path = "/"
	}
	amzDate := now.UTC().Format("20060102T150405Z")
	dateStamp := now.UTC().Format("20060102")

	bodyHashBytes := sha256.Sum256(body)
	bodyHash := hex.EncodeToString(bodyHashBytes[:])

	req.Header.Set("X-Amz-Date", amzDate)
	req.Header.Set("X-Amz-Content-Sha256", bodyHash)
	if sessionToken != "" {
		req.Header.Set("X-Amz-Security-Token", sessionToken)
	}
	if req.Header.Get("Host") == "" && req.Host == "" {
		req.Host = req.URL.Host
	}

	canonicalHeaders, signedHeaders := canonicalHeaderBlock(req)
	canonicalRequest := strings.Join([]string{
		req.Method,
		canonicalURI(req.URL.Path),
		canonicalQuery(req.URL.RawQuery),
		canonicalHeaders,
		signedHeaders,
		bodyHash,
	}, "\n")

	credentialScope := dateStamp + "/" + region + "/" + service + "/aws4_request"
	canonicalHash := sha256.Sum256([]byte(canonicalRequest))
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		credentialScope,
		hex.EncodeToString(canonicalHash[:]),
	}, "\n")

	signingKey := deriveSigningKey(secretKey, dateStamp, region, service)
	signature := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))

	authorization := "AWS4-HMAC-SHA256 " +
		"Credential=" + accessKey + "/" + credentialScope + ", " +
		"SignedHeaders=" + signedHeaders + ", " +
		"Signature=" + signature
	req.Header.Set("Authorization", authorization)
}

func canonicalHeaderBlock(req *http.Request) (string, string) {
	host := req.Host
	if host == "" {
		host = req.URL.Host
	}
	headers := map[string]string{"host": host}
	for k, vs := range req.Header {
		lk := strings.ToLower(k)
		if !signedHeader(lk) {
			continue
		}
		headers[lk] = strings.TrimSpace(strings.Join(vs, ","))
	}
	keys := make([]string, 0, len(headers))
	for k := range headers {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var canonical strings.Builder
	for _, k := range keys {
		canonical.WriteString(k)
		canonical.WriteString(":")
		canonical.WriteString(headers[k])
		canonical.WriteString("\n")
	}
	return canonical.String(), strings.Join(keys, ";")
}

func signedHeader(name string) bool {
	switch name {
	case "host", "content-type", "x-amz-date", "x-amz-target", "x-amz-content-sha256", "x-amz-security-token":
		return true
	}
	return false
}

func canonicalURI(path string) string {
	if path == "" {
		return "/"
	}
	return path
}

func canonicalQuery(raw string) string {
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, "&")
	sort.Strings(parts)
	return strings.Join(parts, "&")
}

func deriveSigningKey(secret, date, region, service string) []byte {
	kDate := hmacSHA256([]byte("AWS4"+secret), date)
	kRegion := hmacSHA256(kDate, region)
	kService := hmacSHA256(kRegion, service)
	return hmacSHA256(kService, "aws4_request")
}

func hmacSHA256(key []byte, data string) []byte {
	h := hmac.New(sha256.New, key)
	h.Write([]byte(data))
	return h.Sum(nil)
}
