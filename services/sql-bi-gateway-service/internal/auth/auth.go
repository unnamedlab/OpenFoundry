// Package auth ports services/sql-bi-gateway-service/src/auth.rs:
// JWT extraction and tenant resolution for the Flight SQL surface.
//
// BI clients (Tableau, Superset, JDBC notebooks) authenticate with the
// same OpenFoundry-issued JWT used by every other service: the token
// is sent in the gRPC `authorization: Bearer <token>` metadata header.
// This package decodes the token using auth-middleware/jwt, builds a
// TenantContext from the resulting Claims, and applies the tenant's
// quotas (`max_query_limit`) to incoming statements.
package auth

import (
	"errors"
	"fmt"
	"strings"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
)

// Metadata is the minimal abstraction over the gRPC metadata map that
// the Flight SQL service hands to Authenticator.Authenticate. The
// Rust side uses `tonic::metadata::MetadataMap`; here we accept any
// Get(key) string lookup so non-gRPC callers (in-process tests, the
// HTTP side router) can reuse the same code path.
type Metadata interface {
	Get(key string) string
}

// HeaderMetadata adapts an http.Header-shaped map[string][]string into
// a Metadata. Lookup is case-insensitive on the first match, mirroring
// how MetadataMap.get behaves for ASCII keys.
type HeaderMetadata map[string][]string

func (h HeaderMetadata) Get(key string) string {
	for k, v := range h {
		if strings.EqualFold(k, key) && len(v) > 0 {
			return v[0]
		}
	}
	return ""
}

// AuthenticatedRequest is the auth outcome attached to every Flight
// SQL request. Mirrors the Rust struct.
type AuthenticatedRequest struct {
	Claims *authmw.Claims
	Tenant authmw.TenantContext
}

// Authenticator extracts and validates the bearer JWT presented by BI
// clients on the Flight SQL surface.
type Authenticator struct {
	jwt            *authmw.JWTConfig
	allowAnonymous bool
}

// NewAuthenticator builds an Authenticator using an HS256 secret and
// a flag that toggles the anonymous bypass (intended for local dev /
// CI only — production deployments must keep this false).
func NewAuthenticator(jwtSecret string, allowAnonymous bool) *Authenticator {
	return &Authenticator{
		jwt:            authmw.NewJWTConfig(jwtSecret),
		allowAnonymous: allowAnonymous,
	}
}

// ErrUnauthenticated wraps every auth failure so the Flight SQL
// service can convert them to gRPC `Unauthenticated` statuses.
type ErrUnauthenticated struct {
	Reason string
	Cause  error
}

func (e *ErrUnauthenticated) Error() string {
	if e.Cause != nil {
		return fmt.Sprintf("unauthenticated: %s: %s", e.Reason, e.Cause)
	}
	return "unauthenticated: " + e.Reason
}

func (e *ErrUnauthenticated) Unwrap() error { return e.Cause }

// IsUnauthenticated reports whether err is an *ErrUnauthenticated.
func IsUnauthenticated(err error) bool {
	var typed *ErrUnauthenticated
	return errors.As(err, &typed)
}

// Authenticate extracts the bearer token from the gRPC metadata,
// decodes it and returns the resulting AuthenticatedRequest.
//
// When AllowAnonymous is true and no `authorization` header is
// present, returns (nil, nil) so the caller can fall back to a
// permissive default tenant — used for local development and CI only.
func (a *Authenticator) Authenticate(md Metadata) (*AuthenticatedRequest, error) {
	header := md.Get("authorization")
	if header == "" {
		if a.allowAnonymous {
			return nil, nil
		}
		return nil, &ErrUnauthenticated{Reason: "missing `authorization: Bearer <jwt>` metadata"}
	}

	token := ""
	switch {
	case strings.HasPrefix(header, "Bearer "):
		token = strings.TrimSpace(header[len("Bearer "):])
	case strings.HasPrefix(header, "bearer "):
		token = strings.TrimSpace(header[len("bearer "):])
	default:
		return nil, &ErrUnauthenticated{Reason: "authorization metadata must use the `Bearer` scheme"}
	}
	if token == "" {
		return nil, &ErrUnauthenticated{Reason: "empty bearer token"}
	}

	claims, err := authmw.DecodeToken(a.jwt, token)
	if err != nil {
		return nil, &ErrUnauthenticated{Reason: "invalid jwt", Cause: err}
	}
	if claims.IsExpired() {
		return nil, &ErrUnauthenticated{Reason: "jwt expired"}
	}
	return &AuthenticatedRequest{
		Claims: claims,
		Tenant: authmw.TenantContextFromClaims(claims),
	}, nil
}

// AllowAnonymous reports whether this authenticator was configured to
// allow unauthenticated requests.
func (a *Authenticator) AllowAnonymous() bool { return a.allowAnonymous }

// EnforcedQuotas are the effective quotas applied to a single
// executed statement. Mirrors the Rust struct.
type EnforcedQuotas struct {
	MaxRows uint32
}

// QuotasForTenant builds the per-statement quotas for an
// authenticated tenant. Mirrors `EnforcedQuotas::for_tenant`.
func QuotasForTenant(t authmw.TenantContext) EnforcedQuotas {
	// Rust uses `clamp_query_limit(usize::MAX)` which collapses to the
	// tenant's `max_query_limit`. Same semantics here.
	return EnforcedQuotas{MaxRows: t.Quotas.MaxQueryLimit}
}

// AnonymousDefaultQuotas returns the quotas applied when no JWT was
// presented and the gateway is in `allow_anonymous` development mode.
// Conservative defaults aligned with the `standard` tenant tier.
func AnonymousDefaultQuotas() EnforcedQuotas {
	return EnforcedQuotas{MaxRows: authmw.QuotaStandard().MaxQueryLimit}
}
