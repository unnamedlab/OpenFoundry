package handlers

import (
	"net/http"
	"strings"
	"time"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
)

// RefreshCookieName carries the refresh JWT so the SPA's 401→refresh
// path can rotate the access cookie without the JS ever touching the
// refresh secret. The cookie is scoped to /api/v1/auth so it is only
// sent on authentication endpoints, narrowing the CSRF surface.
const RefreshCookieName = "of_refresh"

const refreshCookiePath = "/api/v1/auth"

// SessionCookieConfig drives how /login, /mfa/totp/complete-login,
// /token/refresh and /logout shape the of_session cookie. The default
// is the production posture (Secure, SameSite=Lax, host-only); local
// development flips Secure off via AUTH_COOKIE_SECURE=false because
// browsers reject Secure cookies on plain-http localhost.
type SessionCookieConfig struct {
	Secure   bool
	SameSite http.SameSite
	Domain   string
}

// SetSessionCookie writes the of_session cookie carrying the access
// token. Max-Age tracks the JWT lifetime so the browser drops the
// cookie when the token can no longer authenticate, sparing a stale
// round-trip to /me.
func SetSessionCookie(w http.ResponseWriter, cfg SessionCookieConfig, token string, ttl time.Duration) {
	maxAge := int(ttl.Seconds())
	if maxAge < 0 {
		maxAge = 0
	}
	http.SetCookie(w, &http.Cookie{
		Name:     authmw.SessionCookieName,
		Value:    token,
		Path:     "/",
		Domain:   cfg.Domain,
		Secure:   cfg.Secure,
		HttpOnly: true,
		SameSite: resolveSameSite(cfg.SameSite),
		MaxAge:   maxAge,
	})
}

// SetRefreshCookie writes the of_refresh companion cookie. Its Path
// is narrowed to /api/v1/auth so it travels only with auth endpoints,
// minimising what a malicious cross-site request can do with it.
func SetRefreshCookie(w http.ResponseWriter, cfg SessionCookieConfig, token string, ttl time.Duration) {
	maxAge := int(ttl.Seconds())
	if maxAge < 0 {
		maxAge = 0
	}
	http.SetCookie(w, &http.Cookie{
		Name:     RefreshCookieName,
		Value:    token,
		Path:     refreshCookiePath,
		Domain:   cfg.Domain,
		Secure:   cfg.Secure,
		HttpOnly: true,
		SameSite: resolveSameSite(cfg.SameSite),
		MaxAge:   maxAge,
	})
}

// ClearSessionCookie emits Max-Age<0 cookies so the browser drops the
// of_session and of_refresh entries immediately. Domain/path must
// match the setters or the browser keeps the originals alive.
func ClearSessionCookie(w http.ResponseWriter, cfg SessionCookieConfig) {
	http.SetCookie(w, &http.Cookie{
		Name:     authmw.SessionCookieName,
		Value:    "",
		Path:     "/",
		Domain:   cfg.Domain,
		Secure:   cfg.Secure,
		HttpOnly: true,
		SameSite: resolveSameSite(cfg.SameSite),
		MaxAge:   -1,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     RefreshCookieName,
		Value:    "",
		Path:     refreshCookiePath,
		Domain:   cfg.Domain,
		Secure:   cfg.Secure,
		HttpOnly: true,
		SameSite: resolveSameSite(cfg.SameSite),
		MaxAge:   -1,
	})
}

func resolveSameSite(s http.SameSite) http.SameSite {
	if s == 0 {
		return http.SameSiteLaxMode
	}
	return s
}

// ParseSameSite turns the env string ("Lax"/"Strict"/"None") into the
// stdlib enum. Unknown values fall back to Lax — the safest default
// for an auth cookie that must survive top-level navigations from the
// IdP after SSO redirects.
func ParseSameSite(raw string) http.SameSite {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "strict":
		return http.SameSiteStrictMode
	case "none":
		return http.SameSiteNoneMode
	default:
		return http.SameSiteLaxMode
	}
}
