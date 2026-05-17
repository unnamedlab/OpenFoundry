// Package signingkeys owns the durable RSA-2048 signing keypairs the
// identity service uses to mint JWTs, plus the publication surface
// (JWKS) verifiers consume.
//
// Status lifecycle (mirrors the jwt_signing_keys table):
//
//	active    — current signer. Exactly one row at a time.
//	retiring  — predecessor kept in JWKS for a short grace window so
//	            in-flight tokens stay verifiable.
//	retired   — dropped from JWKS. Never returned by publication or
//	            verification helpers.
//
// The orchestrator (Manager) is intentionally Vault-free: private
// keys are sealed with AES-256-GCM using a sealing key sourced from
// the environment. The sibling jwksrotation package retains the
// Vault-transit code path for deployments that need it.
package signingkeys

import (
	"crypto/rsa"
	"time"
)

// Status is the persisted lifecycle stage of a signing key.
type Status string

const (
	StatusActive   Status = "active"
	StatusRetiring Status = "retiring"
	StatusRetired  Status = "retired"
)

// AlgorithmRS256 is the only algorithm this package currently mints.
const AlgorithmRS256 = "RS256"

// Record is the row shape stored in jwt_signing_keys.
type Record struct {
	Kid           string
	Algorithm     string
	PublicKeyPEM  string
	PrivateKeyEnc []byte
	CreatedAt     time.Time
	NotBefore     time.Time
	NotAfter      time.Time
	Status        Status
}

// KeyMaterial is the in-memory projection used by signer / verifier
// code paths after the sealed private key has been unwrapped.
type KeyMaterial struct {
	Record     Record
	PrivateKey *rsa.PrivateKey
	PublicKey  *rsa.PublicKey
}

// Jwks is the wire shape served at /.well-known/jwks.json.
type Jwks struct {
	Keys []Jwk `json:"keys"`
}

// Jwk is a single JWKS entry. The shape follows RFC 7517 § 4 for an
// RSA public key: kty, kid, use, alg, n, e — n and e are
// big-endian, base64url-no-pad.
type Jwk struct {
	Kty string `json:"kty"`
	Use string `json:"use"`
	Alg string `json:"alg"`
	Kid string `json:"kid"`
	N   string `json:"n"`
	E   string `json:"e"`
}

// RotationOutcome is the result of a successful Rotate call.
type RotationOutcome struct {
	PreviousKid   string    `json:"previous_kid,omitempty"`
	ActiveKid     string    `json:"active_kid"`
	GraceUntil    time.Time `json:"grace_until,omitempty"`
	RetiredCount  int       `json:"retired_count"`
}
