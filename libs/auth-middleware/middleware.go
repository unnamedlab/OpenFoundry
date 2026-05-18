package authmw

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
)

// claimsKey is the context key under which authenticated Claims are stashed.
type claimsKey struct{}

// FromContext extracts the validated Claims attached by Middleware.
// Returns false when the request was not authenticated.
func FromContext(ctx context.Context) (*Claims, bool) {
	c, ok := ctx.Value(claimsKey{}).(*Claims)
	return c, ok
}

// MustFromContext panics when no Claims are present. Use only on
// routes that are protected by Middleware (or Required).
func MustFromContext(ctx context.Context) *Claims {
	c, ok := FromContext(ctx)
	if !ok {
		panic("authmw: claims missing from context — did you forget to mount Middleware?")
	}
	return c
}

// ContextWithClaims returns a copy of ctx with the given claims attached
// under the same key Middleware uses. Intended for tests and for
// in-process composition where the caller has already authenticated.
func ContextWithClaims(ctx context.Context, c *Claims) context.Context {
	return context.WithValue(ctx, claimsKey{}, c)
}

// Options tunes the middleware behaviour.
type Options struct {
	// AllowAnonymous — when true, requests without an Authorization
	// header pass through and FromContext returns false. Useful on
	// edge gateways that mix public and authenticated routes behind
	// one chain. A header that IS present but carries an
	// invalid/expired/wrong-use JWT still triggers a structured log +
	// auth_invalid_token_total{reason=...} counter increment before
	// passing through anonymously — never silent.
	AllowAnonymous bool

	// AllowedTokenUses restricts the JWT `token_use` claim to the
	// listed values. When empty (and AnyTokenUse is false) the
	// effective default is []string{"access"}; use
	// [WithAllowedTokenUses] / [WithAnyTokenUse] via [Options.Apply]
	// when wiring routes that legitimately consume a non-access token
	// (e.g. a refresh endpoint).
	AllowedTokenUses []string

	// AnyTokenUse disables `token_use` filtering. Only for
	// special-case decoders; never use on a Middleware that fronts
	// user-facing routes.
	AnyTokenUse bool
}

// Middleware returns the chi-compatible HTTP middleware that:
//
//   - Extracts `Authorization: Bearer <jwt>` (or rejects 401).
//   - Validates the token against `cfg`.
//   - Stashes the *Claims under FromContext for downstream handlers.
//
// On invalid/expired tokens we emit a JSON error body matching the
// shape used by Rust services so the frontend keeps a single error
// schema.
func Middleware(cfg *JWTConfig, opts ...Options) func(http.Handler) http.Handler {
	o := Options{}
	if len(opts) > 0 {
		o = opts[0]
	}
	decodeOpts := o.decodeOptions()
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeaderPresent := r.Header.Get("Authorization") != ""
			tok, ok := bearerToken(r)
			if !ok {
				if o.AllowAnonymous {
					if authHeaderPresent {
						recordInvalidToken("malformed_header")
						slog.WarnContext(r.Context(),
							"auth: malformed Authorization header on anonymous-allowed route",
							"reason", "malformed_header",
							"remote_addr", r.RemoteAddr,
							"path", r.URL.Path,
						)
					}
					next.ServeHTTP(w, r)
					return
				}
				writeAuthError(w, http.StatusUnauthorized, "missing bearer token")
				return
			}
			claims, err := DecodeToken(cfg, tok, decodeOpts...)
			if err != nil {
				if o.AllowAnonymous {
					reason := classifyError(err)
					recordInvalidToken(reason)
					slog.WarnContext(r.Context(),
						"auth: invalid bearer token on anonymous-allowed route",
						"reason", reason,
						"remote_addr", r.RemoteAddr,
						"path", r.URL.Path,
					)
					next.ServeHTTP(w, r)
					return
				}
				recordInvalidToken(classifyError(err))
				status := http.StatusUnauthorized
				msg := "invalid token"
				if IsExpired(err) {
					msg = "token expired"
				}
				writeAuthError(w, status, msg)
				return
			}
			ctx := context.WithValue(r.Context(), claimsKey{}, claims)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Required is a tiny helper that 401s any request lacking authenticated
// Claims. Mount under Middleware{AllowAnonymous: true} to require auth
// only on a subset of routes.
func Required(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := FromContext(r.Context()); !ok {
			writeAuthError(w, http.StatusUnauthorized, "authentication required")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func bearerToken(r *http.Request) (string, bool) {
	h := r.Header.Get("Authorization")
	if h == "" {
		return "", false
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return "", false
	}
	tok := strings.TrimSpace(h[len(prefix):])
	if tok == "" {
		return "", false
	}
	return tok, true
}

type authErrorBody struct {
	Error string `json:"error"`
}

func writeAuthError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(authErrorBody{Error: msg})
}
