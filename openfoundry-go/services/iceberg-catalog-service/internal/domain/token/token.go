// Package token holds pure value types + helpers for the
// `iceberg_api_tokens` long-lived bearer secret. SQL lives in
// `internal/repo`; the splits keeps the Cedar-free types reusable from
// the auth handlers without dragging in pgx.
package token

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// APIToken mirrors a row from `iceberg_api_tokens`. The token secret is
// never persisted in plaintext — only its SHA-256 hash and a 4-character
// hint are kept. The struct excludes `token_hash` because no caller
// outside the validator needs it.
type APIToken struct {
	ID         uuid.UUID  `json:"id"`
	UserID     uuid.UUID  `json:"user_id"`
	Name       string     `json:"name"`
	TokenHint  string     `json:"token_hint"`
	Scopes     []string   `json:"scopes"`
	ExpiresAt  *time.Time `json:"expires_at"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
}

// IssuedToken is what the issuer surfaces exactly once. After the HTTP
// response is written the raw token is dropped — the caller must store
// it themselves.
type IssuedToken struct {
	Record   APIToken `json:"record"`
	RawToken string   `json:"raw_token"`
}

// OftyPrefix is the marker on the wire that disambiguates long-lived
// API tokens from JWTs in the bearer header.
const OftyPrefix = "ofty_"

// Hash returns the lowercase hex SHA-256 of the supplied raw token.
// Matches the Rust `domain::token::hash_token` byte-for-byte so tokens
// minted by either implementation validate against the same column.
func Hash(raw string) string {
	digest := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(digest[:])
}

// Mint generates a fresh `ofty_<64hex>` raw token + companion fields.
// Callers persist the hash; the raw value is shown to the user once.
func Mint() (raw, hash, hint string, err error) {
	var bytes [32]byte
	if _, err = rand.Read(bytes[:]); err != nil {
		return "", "", "", fmt.Errorf("token rand: %w", err)
	}
	raw = OftyPrefix + hex.EncodeToString(bytes[:])
	hash = Hash(raw)
	hint = raw[len(raw)-4:]
	return raw, hash, hint, nil
}

// HasOftyPrefix reports whether the supplied bearer credential is one
// of our long-lived API tokens. The bearer extractor branches on this
// to skip JWT decoding and hit the database instead.
func HasOftyPrefix(raw string) bool { return strings.HasPrefix(raw, OftyPrefix) }
