// Package auth implements bearer-token authentication for the Iceberg
// REST surface. Two token classes are accepted on the same
// `Authorization: Bearer` header — the extractor disambiguates by
// prefix:
//
//   - `ofty_<hex>` — long-lived API tokens minted by
//     `POST /v1/iceberg-clients/api-tokens`. Validated against
//     `iceberg_api_tokens`; scoped to the columns stored on issuance.
//   - Anything else — treated as a JWT and validated as `IcebergClaims`
//     (HS256 with the shared `OPENFOUNDRY_JWT_SECRET`). The `scp`
//     claim carries space-separated scopes; the optional
//     `iceberg_scopes` claim is merged on top for callers that prefer
//     a JSON array.
//
// The extractor enforces the spec's read/write scope distinction:
// `GET`/`HEAD` require `api:iceberg-read`; mutating verbs require
// `api:iceberg-write`.
package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/authz"
	tokendomain "github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/domain/token"
)

// IcebergClaims is the catalog-internal JWT shape. Kept distinct from
// `auth_middleware.Claims` so the iceberg surface doesn't leak Foundry
// roles/permissions to external clients.
type IcebergClaims struct {
	Sub           string   `json:"sub"`
	Iss           string   `json:"iss"`
	Aud           string   `json:"aud"`
	Exp           int64    `json:"exp"`
	Iat           int64    `json:"iat"`
	Scp           string   `json:"scp,omitempty"`
	IcebergScopes []string `json:"iceberg_scopes,omitempty"`
	Tenant        string   `json:"tenant,omitempty"`
}

// jwt-go v5 requires the claim type to satisfy `jwt.Claims`. We supply
// minimal getters; the parser only consults `exp` + `aud` here.

// GetExpirationTime returns the JWT exp claim as a NumericDate.
func (c IcebergClaims) GetExpirationTime() (*jwt.NumericDate, error) {
	return jwt.NewNumericDate(time.Unix(c.Exp, 0)), nil
}

// GetIssuedAt returns the JWT iat claim as a NumericDate.
func (c IcebergClaims) GetIssuedAt() (*jwt.NumericDate, error) {
	return jwt.NewNumericDate(time.Unix(c.Iat, 0)), nil
}

// GetNotBefore — we don't model nbf; jwt-go expects nil when absent.
func (c IcebergClaims) GetNotBefore() (*jwt.NumericDate, error) { return nil, nil }

// GetIssuer returns the JWT iss claim.
func (c IcebergClaims) GetIssuer() (string, error) { return c.Iss, nil }

// GetSubject returns the JWT sub claim.
func (c IcebergClaims) GetSubject() (string, error) { return c.Sub, nil }

// GetAudience returns the JWT aud claim wrapped in jwt-go's ClaimStrings.
func (c IcebergClaims) GetAudience() (jwt.ClaimStrings, error) {
	if c.Aud == "" {
		return nil, nil
	}
	return jwt.ClaimStrings{c.Aud}, nil
}

// AuthenticatedPrincipal is the bearer extractor's output. Carried
// through chi context to handler bodies + the authz engine.
type AuthenticatedPrincipal struct {
	Subject string
	Scopes  map[string]struct{}
	Kind    authz.PrincipalKind
	Tenant  string
}

// AllowsRead reports whether the principal has either `api:iceberg-read`
// or the broader `api:iceberg-write` scope.
func (p *AuthenticatedPrincipal) AllowsRead() bool {
	if p == nil {
		return false
	}
	_, r := p.Scopes["api:iceberg-read"]
	_, w := p.Scopes["api:iceberg-write"]
	return r || w
}

// AllowsWrite reports whether the principal carries `api:iceberg-write`.
func (p *AuthenticatedPrincipal) AllowsWrite() bool {
	if p == nil {
		return false
	}
	_, ok := p.Scopes["api:iceberg-write"]
	return ok
}

// EnforceForMethod returns ErrForbidden when the request method requires
// a scope the principal doesn't carry. GET/HEAD need read; everything
// else needs write.
func (p *AuthenticatedPrincipal) EnforceForMethod(method string) error {
	switch method {
	case http.MethodGet, http.MethodHead:
		if !p.AllowsRead() {
			return &ErrForbidden{Message: "scope `api:iceberg-read` is required"}
		}
	default:
		if !p.AllowsWrite() {
			return &ErrForbidden{Message: "scope `api:iceberg-write` is required"}
		}
	}
	return nil
}

// AsAuthzPrincipal projects the bearer view down to the authz engine's
// view. The two structs share field names; the projection exists so
// the authz package never depends on the auth package.
func (p *AuthenticatedPrincipal) AsAuthzPrincipal() *authz.Principal {
	if p == nil {
		return nil
	}
	scopes := make(map[string]struct{}, len(p.Scopes))
	for k, v := range p.Scopes {
		scopes[k] = v
	}
	return &authz.Principal{
		Subject: p.Subject,
		Scopes:  scopes,
		Kind:    p.Kind,
		Tenant:  p.Tenant,
	}
}

// ErrUnauthenticated is returned when the bearer header is missing /
// malformed / fails JWT validation. Mapped to 401 by the middleware.
type ErrUnauthenticated struct {
	Detail string
}

func (e *ErrUnauthenticated) Error() string {
	if e.Detail == "" {
		return "authentication required"
	}
	return "authentication required: " + e.Detail
}

// ErrForbidden is returned when the principal authenticates but lacks
// the scope required for the requested verb. Mapped to 403.
type ErrForbidden struct {
	Message string
}

func (e *ErrForbidden) Error() string {
	if e.Message == "" {
		return "forbidden"
	}
	return e.Message
}

// StoredAPIToken is the validator's view of a row in iceberg_api_tokens.
// Kept here (rather than in `domain/token`) so `internal/repo/auth.go`
// can import this package without a cycle through domain.
type StoredAPIToken struct {
	ID     uuid.UUID
	UserID uuid.UUID
	Scopes []string
	Tenant string
}

// TokenStore is the contract the bearer extractor needs from the data
// layer. Repo implementations satisfy this without needing to depend on
// chi.
type TokenStore interface {
	ValidateAPIToken(ctx context.Context, raw string) (*StoredAPIToken, error)
}

// Config holds the JWT signing/validation parameters + token TTLs used
// by the bearer extractor + OAuth handler.
type Config struct {
	Secret              []byte
	JWTAudience         string
	JWTIssuer           string
	DefaultTokenTTLSecs int64
	DefaultTenant       string
}

// LoadSecret resolves the HS256 secret in the same priority order as
// the Rust service: OPENFOUNDRY_JWT_SECRET, then JWT_SECRET, then a
// dev-only fallback so local boots succeed without env wiring.
func LoadSecret() []byte {
	if v := os.Getenv("OPENFOUNDRY_JWT_SECRET"); v != "" {
		return []byte(v)
	}
	if v := os.Getenv("JWT_SECRET"); v != "" {
		return []byte(v)
	}
	return []byte("iceberg-catalog-dev-secret")
}

type principalCtxKey struct{}

// ContextWithPrincipal stashes the supplied principal on the context.
// Used by Middleware + by tests that bypass the middleware.
func ContextWithPrincipal(ctx context.Context, p *AuthenticatedPrincipal) context.Context {
	return context.WithValue(ctx, principalCtxKey{}, p)
}

// PrincipalFromContext extracts the bearer-extracted principal injected
// by Middleware. Returns false on routes that didn't run the bearer
// chain.
func PrincipalFromContext(ctx context.Context) (*AuthenticatedPrincipal, bool) {
	p, ok := ctx.Value(principalCtxKey{}).(*AuthenticatedPrincipal)
	return p, ok
}

// Authenticate runs the full bearer extraction pipeline once. Used by
// Middleware + by route-local handlers that need to authenticate inline.
func Authenticate(ctx context.Context, header string, cfg *Config, store TokenStore) (*AuthenticatedPrincipal, error) {
	tok, ok := extractBearer(header)
	if !ok {
		return nil, &ErrUnauthenticated{Detail: "missing bearer token"}
	}
	if tokendomain.HasOftyPrefix(tok) {
		if store == nil {
			return nil, &ErrUnauthenticated{Detail: "api token store unavailable"}
		}
		stored, err := store.ValidateAPIToken(ctx, tok)
		if err != nil || stored == nil {
			return nil, &ErrUnauthenticated{Detail: "api token invalid"}
		}
		scopes := scopeSet(stored.Scopes)
		return &AuthenticatedPrincipal{
			Subject: stored.UserID.String(),
			Scopes:  scopes,
			Kind:    authz.PrincipalKindFromScopes(scopes),
			Tenant:  stored.Tenant,
		}, nil
	}
	claims, err := decodeIcebergJWT(tok, cfg)
	if err != nil {
		return nil, &ErrUnauthenticated{Detail: err.Error()}
	}
	scopes := scopeSet(strings.Fields(claims.Scp))
	for _, s := range claims.IcebergScopes {
		scopes[s] = struct{}{}
	}
	return &AuthenticatedPrincipal{
		Subject: claims.Sub,
		Scopes:  scopes,
		Kind:    authz.PrincipalKindFromScopes(scopes),
		Tenant:  claims.Tenant,
	}, nil
}

// Middleware authenticates the request, enforces the read/write split
// based on the verb, and stashes the principal in context for downstream
// handlers.
func Middleware(cfg *Config, store TokenStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			principal, err := Authenticate(r.Context(), r.Header.Get("Authorization"), cfg, store)
			if err != nil {
				WriteAuthError(w, err)
				return
			}
			if err := principal.EnforceForMethod(r.Method); err != nil {
				WriteAuthError(w, err)
				return
			}
			ctx := ContextWithPrincipal(r.Context(), principal)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// IssueInternalJWT mints an HS256 iceberg-flavoured JWT. Used by the
// OAuth2 `client_credentials` grant + by tests that need a synthetic
// bearer token.
func IssueInternalJWT(cfg *Config, sub, iss, aud string, scopes []string, ttlSecs int64) (string, error) {
	if cfg == nil {
		return "", errors.New("nil bearer config")
	}
	now := time.Now().Unix()
	claims := IcebergClaims{
		Sub:           sub,
		Iss:           iss,
		Aud:           aud,
		Iat:           now,
		Exp:           now + ttlSecs,
		Scp:           strings.Join(scopes, " "),
		IcebergScopes: append([]string{}, scopes...),
		Tenant:        cfg.DefaultTenant,
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := tok.SignedString(cfg.Secret)
	if err != nil {
		return "", fmt.Errorf("jwt encode: %w", err)
	}
	return signed, nil
}

// WriteAuthError serialises the supplied error into the catalog's
// shared envelope shape. Always writes a JSON body; the status code is
// inferred from the error type.
func WriteAuthError(w http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	msg := err.Error()
	typeName := "InternalServerException"
	var unauth *ErrUnauthenticated
	var forbid *ErrForbidden
	switch {
	case errors.As(err, &unauth):
		status = http.StatusUnauthorized
		typeName = "AuthenticationException"
	case errors.As(err, &forbid):
		status = http.StatusForbidden
		typeName = "ForbiddenException"
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"message": msg,
			"type":    typeName,
			"code":    status,
		},
	})
}

func extractBearer(header string) (string, bool) {
	if header == "" {
		return "", false
	}
	if v, ok := strings.CutPrefix(header, "Bearer "); ok {
		return strings.TrimSpace(v), strings.TrimSpace(v) != ""
	}
	if v, ok := strings.CutPrefix(header, "bearer "); ok {
		return strings.TrimSpace(v), strings.TrimSpace(v) != ""
	}
	return "", false
}

func decodeIcebergJWT(token string, cfg *Config) (*IcebergClaims, error) {
	if cfg == nil {
		return nil, errors.New("nil bearer config")
	}
	parser := jwt.NewParser(jwt.WithValidMethods([]string{"HS256"}))
	claims := &IcebergClaims{}
	parsed, err := parser.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		return cfg.Secret, nil
	})
	if err != nil {
		return nil, err
	}
	if !parsed.Valid {
		return nil, errors.New("token not valid")
	}
	if cfg.JWTAudience != "" && claims.Aud != cfg.JWTAudience {
		return nil, fmt.Errorf("audience mismatch: %q", claims.Aud)
	}
	if claims.Exp != 0 && time.Now().Unix() > claims.Exp {
		return nil, errors.New("token expired")
	}
	return claims, nil
}

func scopeSet(scopes []string) map[string]struct{} {
	out := make(map[string]struct{}, len(scopes))
	for _, s := range scopes {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		out[s] = struct{}{}
	}
	return out
}
