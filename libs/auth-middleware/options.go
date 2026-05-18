package authmw

// Option configures decoder + middleware behaviour through a
// functional-options pattern. Apply via [DecodeToken] (`opts ...Option`)
// or by translating an [Options] struct used with [Middleware].
type Option func(*Options)

// WithAllowedTokenUses restricts the JWT `token_use` claim to the
// supplied set. Tokens whose `token_use` is missing or outside the
// set are rejected with an invalid-token error.
//
// When this option is not applied (and [WithAnyTokenUse] is not
// applied either), the effective default is []string{"access"} —
// refresh, mfa_challenge, api_key, etc. tokens will not pass through
// the standard /api/v1 routes. This is the desired posture for every
// user-facing route: an MFA-challenge token must never be accepted as
// authentication for a normal API call.
//
// Pass the explicit set when a route legitimately consumes a
// non-"access" token (e.g. a refresh endpoint that takes refresh
// tokens).
func WithAllowedTokenUses(uses ...string) Option {
	return func(o *Options) {
		o.AllowedTokenUses = append([]string(nil), uses...)
		o.AnyTokenUse = false
	}
}

// WithAnyTokenUse disables `token_use` filtering. Reserved for
// special-case decoders — JWKS rotation tooling, refresh-token
// verifier, debugging probes. Never use on a [Middleware] that fronts
// user-facing routes; it dissolves the boundary between access tokens
// and the short-lived tokens issued by the auth state machine
// (mfa_challenge, refresh, etc.).
func WithAnyTokenUse() Option {
	return func(o *Options) {
		o.AnyTokenUse = true
		o.AllowedTokenUses = nil
	}
}

// Apply mutates o by running each fn in order. Returns the mutated
// receiver so callers can chain: `Options{AllowAnonymous: true}.Apply(WithAllowedTokenUses("access","api_key"))`.
func (o Options) Apply(fns ...Option) Options {
	for _, fn := range fns {
		fn(&o)
	}
	return o
}

// effectiveAllowedUses returns (allowed, anyUse). When anyUse is
// true the filter is disabled. When allowed is empty the caller must
// treat that as the no-default sentinel (it never happens through
// this helper; callers should consult anyUse first).
func (o Options) effectiveAllowedUses() (allowed []string, anyUse bool) {
	if o.AnyTokenUse {
		return nil, true
	}
	if len(o.AllowedTokenUses) == 0 {
		return []string{"access"}, false
	}
	return o.AllowedTokenUses, false
}

// decodeOptions translates the Options struct used by [Middleware]
// into the functional-option slice accepted by [DecodeToken].
func (o Options) decodeOptions() []Option {
	switch {
	case o.AnyTokenUse:
		return []Option{WithAnyTokenUse()}
	case len(o.AllowedTokenUses) > 0:
		return []Option{WithAllowedTokenUses(o.AllowedTokenUses...)}
	}
	return nil
}
