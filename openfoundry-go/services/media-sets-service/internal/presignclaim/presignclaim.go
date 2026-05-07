// Package presignclaim mints + verifies the short-lived claim
// embedded in presigned download URLs. Mirrors the Rust functions
// `mint_presign_claim` and `verify_presign_claim` in
// services/media-sets-service/src/domain/cedar.rs.
//
// Claim shape:
//
//	{
//	  "sub":      "<user uuid>",
//	  "item_rid": "ri.foundry.main.media_item.<uuid>",
//	  "markings": ["pii", "secret"],
//	  "iat":      1700000000,
//	  "exp":      1700000300
//	}
//
// HS256 signature with the same JWT_SECRET the rest of the platform
// uses (so identity-federation-service + edge-gateway-service can
// validate the claim without an extra config knob). Default TTL is
// 5 minutes; the storage backend's presign TTL caps the JWT exp so
// a 1-hour storage URL never carries a 1-hour claim.
package presignclaim

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// DefaultTTL is the canonical 5-minute window. Mirrors Rust
// PRESIGN_CLAIM_DEFAULT_TTL_SECS = 300.
const DefaultTTL = 5 * time.Minute

// Claim is the JWT payload. JSON tag layout matches the Rust struct
// 1:1 so a token minted in either language verifies in both.
type Claim struct {
	Sub      string   `json:"sub"`
	ItemRID  string   `json:"item_rid"`
	Markings []string `json:"markings"`
	IAT      int64    `json:"iat"`
	EXP      int64    `json:"exp"`
}

// GetExpirationTime / GetIssuedAt / GetNotBefore / GetIssuer / GetSubject /
// GetAudience implement the jwt.Claims interface.
func (c Claim) GetExpirationTime() (*jwt.NumericDate, error) {
	return jwt.NewNumericDate(time.Unix(c.EXP, 0)), nil
}
func (c Claim) GetIssuedAt() (*jwt.NumericDate, error) {
	return jwt.NewNumericDate(time.Unix(c.IAT, 0)), nil
}
func (c Claim) GetNotBefore() (*jwt.NumericDate, error) { return nil, nil }
func (c Claim) GetIssuer() (string, error)              { return "", nil }
func (c Claim) GetSubject() (string, error)             { return c.Sub, nil }
func (c Claim) GetAudience() (jwt.ClaimStrings, error)  { return nil, nil }

// Signer mints HS256 JWTs against `secret`.
type Signer struct {
	secret []byte
	// CapTTL caps the requested TTL when callers ask for a window
	// longer than the storage presign window. Zero means no cap.
	CapTTL time.Duration
}

// NewSigner builds a Signer. Returns an error if the secret is empty
// (mirrors Rust impl which expects a populated AppState::presign_secret).
func NewSigner(secret []byte) (*Signer, error) {
	if len(secret) == 0 {
		return nil, errors.New("presignclaim: secret must be non-empty")
	}
	return &Signer{secret: append([]byte(nil), secret...)}, nil
}

// Sign returns the encoded JWT. `ttl` must be positive; values <= 0
// fall back to DefaultTTL. When CapTTL is set, the effective window
// is min(ttl, CapTTL).
func (s *Signer) Sign(sub, itemRID string, markings []string, ttl time.Duration) (string, error) {
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	if s.CapTTL > 0 && ttl > s.CapTTL {
		ttl = s.CapTTL
	}
	now := time.Now().UTC()
	claim := Claim{
		Sub:      sub,
		ItemRID:  itemRID,
		Markings: append([]string(nil), markings...),
		IAT:      now.Unix(),
		EXP:      now.Add(ttl).Unix(),
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claim)
	signed, err := tok.SignedString(s.secret)
	if err != nil {
		return "", fmt.Errorf("presignclaim: sign: %w", err)
	}
	return signed, nil
}

// Verifier validates the JWT against `secret` and pins the expected
// item_rid so a stolen URL targeting item A can't be replayed for
// item B.
type Verifier struct {
	secret []byte
	// Leeway tolerates small clock skew when checking exp. Mirrors
	// the Rust verify_presign_claim which sets validation.leeway = 5.
	Leeway time.Duration
}

// NewVerifier builds a Verifier (same secret + 5-second leeway).
func NewVerifier(secret []byte) (*Verifier, error) {
	if len(secret) == 0 {
		return nil, errors.New("presignclaim: secret must be non-empty")
	}
	return &Verifier{secret: append([]byte(nil), secret...), Leeway: 5 * time.Second}, nil
}

// Verify decodes + validates the claim. Returns an error when the
// signature is bad, the claim is expired, or its `item_rid` does
// not match `expectedItemRID`.
func (v *Verifier) Verify(token, expectedItemRID string) (*Claim, error) {
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Name}),
		jwt.WithLeeway(v.Leeway),
	)
	parsed, err := parser.ParseWithClaims(token, &Claim{}, func(t *jwt.Token) (interface{}, error) {
		return v.secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("presignclaim: invalid claim: %w", err)
	}
	c, ok := parsed.Claims.(*Claim)
	if !ok || !parsed.Valid {
		return nil, errors.New("presignclaim: claim shape invalid")
	}
	if c.ItemRID != expectedItemRID {
		return nil, fmt.Errorf("presignclaim: claim targets `%s`, not `%s`", c.ItemRID, expectedItemRID)
	}
	return c, nil
}
