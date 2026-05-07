// Package storage abstracts the byte-store backing media items.
// Mirrors the Rust `domain/storage.rs` `BackendMediaStorage` trait
// with one concrete in-process implementation (HMACBackend) so the
// service can issue presigned URLs without a real S3 hop in dev /
// integration tests.
//
// Production swaps HMACBackend for an S3 / MinIO backend that signs
// against the actual bucket; the interface below is what wiring
// expects.
package storage

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/mediapath"
)

// PresignedURL bundles the URL + expiry + extra headers a client must
// echo on the actual byte transfer (e.g. SSE-S3 metadata).
type PresignedURL struct {
	URL       string
	ExpiresAt time.Time
	Headers   []HeaderPair
}

// HeaderPair is a single (name, value) header. Slice form so the
// order survives JSON encoding (canonical signing in S3 cares).
type HeaderPair struct {
	Name  string
	Value string
}

// Backend is the storage abstraction. All methods take a context so a
// real S3 backend can honour cancellation; the in-process HMAC backend
// returns immediately.
type Backend interface {
	// Bucket returns the bucket name the keys are nested under.
	// Surfaced so the application can stamp the canonical s3://
	// URI on the row.
	Bucket() string
	// PresignUpload issues a PUT URL valid for `ttl`.
	PresignUpload(ctx context.Context, key mediapath.Key, mimeType string, ttl time.Duration) (*PresignedURL, error)
	// PresignDownload issues a GET URL valid for `ttl`.
	PresignDownload(ctx context.Context, key mediapath.Key, ttl time.Duration) (*PresignedURL, error)
	// Delete removes the byte payload for `key`. Best-effort —
	// callers ignore errors (the metadata row is the source of
	// truth).
	Delete(ctx context.Context, key mediapath.Key) error
}

// HMACBackend is a simple in-process backend that signs URLs with
// HMAC-SHA256. It does NOT serve bytes — the URL points at a logical
// edge endpoint the gateway exposes; the HMAC pins the (key, expiry)
// pair so a tampered URL fails verification.
//
// Use this in dev + tests; replace with an S3 / MinIO backend in
// production.
type HMACBackend struct {
	bucket   string
	endpoint string
	secret   []byte
}

// NewHMACBackend builds a backend pinned to `bucket` + `endpoint` and
// signing with `secret`. Endpoint is the public URL prefix the gateway
// listens on (e.g. "https://media.example.com").
func NewHMACBackend(bucket, endpoint string, secret []byte) (*HMACBackend, error) {
	if bucket == "" {
		return nil, errors.New("bucket is required")
	}
	if endpoint == "" {
		return nil, errors.New("endpoint is required")
	}
	if len(secret) == 0 {
		return nil, errors.New("secret is required")
	}
	return &HMACBackend{
		bucket:   bucket,
		endpoint: endpoint,
		secret:   append([]byte(nil), secret...),
	}, nil
}

// Bucket returns the configured bucket.
func (b *HMACBackend) Bucket() string { return b.bucket }

// PresignUpload returns a PUT URL whose query string includes the
// expiry epoch + the HMAC-SHA256 signature of "PUT|<key>|<expires>".
func (b *HMACBackend) PresignUpload(_ context.Context, key mediapath.Key, mimeType string, ttl time.Duration) (*PresignedURL, error) {
	expires, signed := b.signedURL("PUT", key, ttl)
	return &PresignedURL{
		URL:       signed,
		ExpiresAt: expires,
		Headers:   []HeaderPair{{Name: "Content-Type", Value: mimeType}},
	}, nil
}

// PresignDownload returns a GET URL.
func (b *HMACBackend) PresignDownload(_ context.Context, key mediapath.Key, ttl time.Duration) (*PresignedURL, error) {
	expires, signed := b.signedURL("GET", key, ttl)
	return &PresignedURL{URL: signed, ExpiresAt: expires}, nil
}

// Delete is a no-op for the HMAC backend. The real S3 backend would
// issue a DeleteObject call here.
func (b *HMACBackend) Delete(_ context.Context, _ mediapath.Key) error { return nil }

func (b *HMACBackend) signedURL(method string, key mediapath.Key, ttl time.Duration) (time.Time, string) {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	expires := time.Now().Add(ttl).UTC()
	canonical := method + "|" + key.ObjectKey() + "|" + strconv.FormatInt(expires.Unix(), 10)
	mac := hmac.New(sha256.New, b.secret)
	mac.Write([]byte(canonical))
	sig := hex.EncodeToString(mac.Sum(nil))
	q := url.Values{}
	q.Set("expires", strconv.FormatInt(expires.Unix(), 10))
	q.Set("signature", sig)
	return expires, fmt.Sprintf("%s/%s/%s?%s", b.endpoint, b.bucket, key.ObjectKey(), q.Encode())
}
