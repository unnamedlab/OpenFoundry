// Package kms abstracts the key-encryption-key (KEK) that wraps every
// DEK persisted by cipher-service. The plaintext DEK is the only
// secret an encrypt/decrypt operation touches; the persisted form is
// always KMS.Wrap(DEK), so a database compromise alone never yields
// usable key material.
//
// Two backends ship with Milestone A:
//
//   - LocalKMS — env-var-backed KEK, AES-256-GCM wrap. Intended for
//     dev/test and integration tests. The KEK is 32 hex-encoded bytes
//     pulled from OF_CIPHER_LOCAL_KEK at boot; missing/short keys
//     fail fast so the service never silently falls back to a weaker
//     primitive.
//
//   - AWSKMSStub — placeholder honouring the KMS interface but
//     returning ErrAWSNotImplemented from every call. Wired so the
//     server config can declare the backend today; the real client
//     ships with Milestone C (CIP.20).
//
// External KMS providers (Vault Transit, GCP KMS, Azure Key Vault)
// plug into the same interface — each adds a new file in this
// package and the config picks the implementation by name.
package kms

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
)

// KMS is the contract the cipher-service depends on. Implementations
// must be safe for concurrent use; Wrap/Unwrap are called from every
// encrypt/decrypt request and from key rotation.
type KMS interface {
	// Wrap encrypts a plaintext DEK with the underlying KEK. The
	// returned bytes are opaque to the caller — only the matching
	// Unwrap may decode them.
	Wrap(plainDEK []byte) ([]byte, error)

	// Unwrap reverses Wrap. Returns the original 32-byte DEK on
	// success; the typed error vocabulary lets the handler layer
	// distinguish "wrong KEK" from "malformed material".
	Unwrap(wrapped []byte) ([]byte, error)

	// Ref is the stable identifier for the KEK that wrapped the DEK.
	// Persisted alongside every CipherKeyVersion so a future KEK
	// rotation can re-wrap historical material without losing
	// provenance.
	Ref() string
}

// Sentinel errors. Handlers don't surface them directly to the wire —
// every KMS failure collapses into a 500 — but the audit layer keys
// off the typed errors for incident response.
var (
	// ErrLocalKEKMissing is returned when LocalKMS is asked to boot
	// without OF_CIPHER_LOCAL_KEK in the environment.
	ErrLocalKEKMissing = errors.New("cipher kms: OF_CIPHER_LOCAL_KEK is not set")

	// ErrLocalKEKInvalid wraps decode / length failures on the env-var
	// KEK so the caller fails boot rather than silently downgrading.
	ErrLocalKEKInvalid = errors.New("cipher kms: OF_CIPHER_LOCAL_KEK is malformed")

	// ErrWrappedMaterialInvalid is returned when Unwrap is handed
	// bytes that fail AEAD authentication or are too short.
	ErrWrappedMaterialInvalid = errors.New("cipher kms: wrapped key material is invalid")

	// ErrAWSNotImplemented is the stub-only sentinel returned by
	// AWSKMSStub until Milestone C wires a real client.
	ErrAWSNotImplemented = errors.New("cipher kms: aws backend not implemented")
)

// LocalKMS wraps DEKs with a single 32-byte KEK held in process
// memory. The KEK is loaded once from the environment at boot.
type LocalKMS struct {
	aead cipher.AEAD
	ref  string
}

// LocalKEKEnv is the env var LocalKMS reads its KEK from. Pinned as a
// constant so ops runbooks and tests stay in sync.
const LocalKEKEnv = "OF_CIPHER_LOCAL_KEK"

// NewLocalKMSFromEnv reads LocalKEKEnv (32 bytes, hex-encoded) and
// returns a ready KMS. Use only in dev/test deployments.
func NewLocalKMSFromEnv() (*LocalKMS, error) {
	raw := strings.TrimSpace(os.Getenv(LocalKEKEnv))
	if raw == "" {
		return nil, ErrLocalKEKMissing
	}
	kek, err := hex.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrLocalKEKInvalid, err)
	}
	return NewLocalKMS(kek, "local:env:"+LocalKEKEnv)
}

// NewLocalKMS builds a LocalKMS around an explicit 32-byte KEK. The
// `ref` string is stamped on every wrapped DEK so a future rotation
// has a stable lineage to match against.
func NewLocalKMS(kek []byte, ref string) (*LocalKMS, error) {
	if len(kek) != 32 {
		return nil, fmt.Errorf("%w: KEK must be 32 bytes (got %d)", ErrLocalKEKInvalid, len(kek))
	}
	block, err := aes.NewCipher(kek)
	if err != nil {
		return nil, fmt.Errorf("cipher kms: aes init: %w", err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("cipher kms: gcm init: %w", err)
	}
	if ref == "" {
		ref = "local:anonymous"
	}
	return &LocalKMS{aead: aead, ref: ref}, nil
}

// Wrap returns nonce||ct||tag — the canonical sealed envelope from
// Go's stdlib AEAD interface.
func (l *LocalKMS) Wrap(plainDEK []byte) ([]byte, error) {
	nonce := make([]byte, l.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("cipher kms: nonce: %w", err)
	}
	return l.aead.Seal(nonce, nonce, plainDEK, nil), nil
}

// Unwrap rejects truncated inputs and authentication failures with
// ErrWrappedMaterialInvalid so callers always see the same typed
// error regardless of root cause.
func (l *LocalKMS) Unwrap(wrapped []byte) ([]byte, error) {
	ns := l.aead.NonceSize()
	if len(wrapped) < ns {
		return nil, ErrWrappedMaterialInvalid
	}
	nonce, ct := wrapped[:ns], wrapped[ns:]
	pt, err := l.aead.Open(nil, nonce, ct, nil)
	if err != nil {
		return nil, ErrWrappedMaterialInvalid
	}
	return pt, nil
}

// Ref returns the stable identifier for this KEK. LocalKMS uses
// "local:env:<var>" so audit logs can distinguish a dev KEK from a
// future production KMS reference.
func (l *LocalKMS) Ref() string { return l.ref }

// AWSKMSStub honours the KMS interface but is not wired to AWS yet.
// Construction succeeds; calls return ErrAWSNotImplemented so the
// server can declare the intent (config-driven backend selection)
// without shipping the runtime path.
type AWSKMSStub struct {
	keyARN string
}

// NewAWSKMSStub records the target ARN so Ref() emits something
// useful while the real client is still pending (CIP.20).
func NewAWSKMSStub(keyARN string) *AWSKMSStub {
	if keyARN == "" {
		keyARN = "stub"
	}
	return &AWSKMSStub{keyARN: keyARN}
}

func (s *AWSKMSStub) Wrap(_ []byte) ([]byte, error)   { return nil, ErrAWSNotImplemented }
func (s *AWSKMSStub) Unwrap(_ []byte) ([]byte, error) { return nil, ErrAWSNotImplemented }
func (s *AWSKMSStub) Ref() string                     { return "aws:kms:" + s.keyARN }
