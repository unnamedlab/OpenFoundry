package authmw

// layer.go ports libs/auth-middleware/src/layer.rs.
//
// The Rust crate exposes its auth wiring via two symbols:
//
//   * fn auth_layer — the async tower::Layer / axum from_fn middleware
//     that extracts a Bearer JWT, validates it, stashes Claims in
//     request extensions, and returns 401 on failure.
//   * struct AuthUser(pub Claims) + FromRequestParts impl — the typed
//     extractor handlers use to pull authenticated identity off a
//     request.
//
// Go has no axum-flavoured FromRequestParts; the idiomatic shape is
// the chi-compatible func(http.Handler) http.Handler middleware
// (already in middleware.go) plus context helpers. This file
// provides the named aliases services translating from Rust expect
// to find — AuthLayer + AuthUser + AuthUser{From,Must}Context — so
// callers don't have to mentally re-map the API.
//
// Behaviourally, AuthLayer(cfg) is identical to Middleware(cfg).
// Wire-format differences are limited to the response body strings:
// Go uses lower-case "missing bearer token" / "authentication
// required" while Rust uses "missing Bearer token" / "not
// authenticated". Both return 401; tests below assert on the Go
// strings.

import (
	"context"
	"net/http"
)

// AuthUser is the typed wrapper around an authenticated *Claims.
// Mirrors `pub struct AuthUser(pub Claims)` in layer.rs.
//
// Use AuthUserFromContext or AuthUserFromRequest to extract it
// after the auth middleware has run; on missing claims both helpers
// return ok=false (the Rust side returns 401 directly via
// FromRequestParts).
type AuthUser struct {
	Claims *Claims
}

// AuthLayer is the named alias for Middleware so call sites
// translating from Rust find the expected symbol. Behaviourally
// identical: extracts Authorization: Bearer <jwt>, validates it
// against cfg, stashes *Claims into the request context, returns
// 401 on missing/invalid/expired token.
//
// The Rust signature takes `axum::extract::State<JwtConfig>`; in Go
// we close over cfg directly — no service wiring difference.
func AuthLayer(cfg *JWTConfig) func(http.Handler) http.Handler {
	return Middleware(cfg)
}

// AuthUserFromContext is the Go analogue of
// `FromRequestParts for AuthUser`. Returns the AuthUser stashed by
// AuthLayer / Middleware. ok=false when the request was not
// authenticated.
func AuthUserFromContext(ctx context.Context) (AuthUser, bool) {
	c, ok := FromContext(ctx)
	if !ok {
		return AuthUser{}, false
	}
	return AuthUser{Claims: c}, true
}

// AuthUserFromRequest is the http.Request flavour. Reaches into
// r.Context() so call sites that already have the request don't
// have to thread the context through.
func AuthUserFromRequest(r *http.Request) (AuthUser, bool) {
	return AuthUserFromContext(r.Context())
}

// MustAuthUser panics when no AuthUser is in context. Use only on
// routes guarded by AuthLayer / Middleware (or Required). Mirrors
// MustFromContext for callers that prefer the typed wrapper.
func MustAuthUser(ctx context.Context) AuthUser {
	user, ok := AuthUserFromContext(ctx)
	if !ok {
		panic("authmw: AuthUser missing from context — did you forget to mount AuthLayer?")
	}
	return user
}
