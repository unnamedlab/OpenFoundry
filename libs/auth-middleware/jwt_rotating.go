package authmw

import (
	"crypto/rsa"
	"errors"
	"fmt"

	"github.com/golang-jwt/jwt/v5"
)

// ErrUnknownKid is returned by PublicKeyResolver implementations
// when the kid does not map to a verification key (e.g. it points
// at a retired signing key). The JWT decoder converts it into a
// JWTError with errKindInvalid.
var ErrUnknownKid = errors.New("unknown kid")

// PublicKeyResolver returns the RSA public half for a kid. The
// rotating signing-key manager implements this — verifiers walk
// every kid in {active, retiring} but reject retired (or unknown)
// kids.
type PublicKeyResolver func(kid string) (*rsa.PublicKey, error)

// EncodeTokenRS256 mints an RS256 JWT signed with the supplied
// private key. The kid is set on the JWS header so verifiers can
// pick the right public key. issuer / audience copy through to the
// claims so the existing DecodeToken-shaped validation keeps
// working.
//
// Claims format is untouched: this is the same wrapper EncodeToken
// uses, just with the key + kid provided explicitly.
func EncodeTokenRS256(claims *Claims, priv *rsa.PrivateKey, kid string) (string, error) {
	if priv == nil {
		return "", errors.New("nil rsa private key")
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, jwtClaimsWrapper{Claims: claims})
	if kid != "" {
		tok.Header["kid"] = kid
	}
	signed, err := tok.SignedString(priv)
	if err != nil {
		return "", fmt.Errorf("sign jwt: %w", err)
	}
	return signed, nil
}

// DecodeTokenRS256Multi validates an RS256 JWT by resolving its kid
// header against `resolve`. issuer / audience apply when non-empty
// — same semantics as DecodeToken. Mirrors DecodeToken's typed-error
// shape so handlers can keep using IsExpired / JWTError.
func DecodeTokenRS256Multi(token string, resolve PublicKeyResolver, issuer, audience string) (*Claims, error) {
	if resolve == nil {
		return nil, errors.New("nil resolver")
	}
	parser := jwt.NewParser(jwt.WithValidMethods([]string{string(AlgRS256)}))
	wrapper := &jwtClaimsWrapper{Claims: &Claims{}}
	parsed, err := parser.ParseWithClaims(token, wrapper, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		pub, err := resolve(kid)
		if err != nil {
			return nil, err
		}
		if pub == nil {
			return nil, ErrUnknownKid
		}
		return pub, nil
	})
	if err != nil {
		if errors.Is(err, jwt.ErrTokenExpired) {
			return nil, &JWTError{Kind: errKindExpired, Cause: err}
		}
		return nil, &JWTError{Kind: errKindInvalid, Cause: err}
	}
	if !parsed.Valid {
		return nil, &JWTError{Kind: errKindInvalid, Cause: errors.New("token not valid")}
	}
	if issuer != "" && (wrapper.ISS == nil || *wrapper.ISS != issuer) {
		return nil, &JWTError{Kind: errKindInvalid, Cause: fmt.Errorf("issuer mismatch")}
	}
	if audience != "" && (wrapper.AUD == nil || *wrapper.AUD != audience) {
		return nil, &JWTError{Kind: errKindInvalid, Cause: fmt.Errorf("audience mismatch")}
	}
	if wrapper.Claims.IsExpired() {
		return nil, &JWTError{Kind: errKindExpired}
	}
	return wrapper.Claims, nil
}
