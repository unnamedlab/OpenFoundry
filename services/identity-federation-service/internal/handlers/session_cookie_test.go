package handlers_test

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/handlers"
)

func sessionCookiesFromResponse(t *testing.T, rec *httptest.ResponseRecorder) map[string]*http.Cookie {
	t.Helper()
	out := map[string]*http.Cookie{}
	for _, c := range rec.Result().Cookies() {
		out[c.Name] = c
	}
	return out
}

func TestSetSessionCookieAppliesSecurityFlags(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	cfg := handlers.SessionCookieConfig{Secure: true, SameSite: http.SameSiteLaxMode}

	handlers.SetSessionCookie(rec, cfg, "jwt-value", time.Hour)
	handlers.SetRefreshCookie(rec, cfg, "refresh-value", 24*time.Hour)

	got := sessionCookiesFromResponse(t, rec)
	access := got[authmw.SessionCookieName]
	require.NotNil(t, access, "of_session must be emitted")
	assert.Equal(t, "jwt-value", access.Value)
	assert.True(t, access.HttpOnly, "of_session must be httpOnly to block XSS")
	assert.True(t, access.Secure, "of_session must be Secure in production posture")
	assert.Equal(t, http.SameSiteLaxMode, access.SameSite)
	assert.Equal(t, "/", access.Path)
	assert.Equal(t, 3600, access.MaxAge, "Max-Age must mirror the JWT TTL in seconds")

	refresh := got[handlers.RefreshCookieName]
	require.NotNil(t, refresh, "of_refresh must be emitted")
	assert.True(t, refresh.HttpOnly)
	assert.True(t, refresh.Secure)
	assert.Equal(t, "/api/v1/auth", refresh.Path, "of_refresh must be narrowed to auth paths")
	assert.Equal(t, 86400, refresh.MaxAge)
}

func TestClearSessionCookieDeletesBothCookies(t *testing.T) {
	t.Parallel()
	rec := httptest.NewRecorder()
	handlers.ClearSessionCookie(rec, handlers.SessionCookieConfig{Secure: true, SameSite: http.SameSiteLaxMode})

	got := sessionCookiesFromResponse(t, rec)
	require.Contains(t, got, authmw.SessionCookieName)
	require.Contains(t, got, handlers.RefreshCookieName)
	for _, name := range []string{authmw.SessionCookieName, handlers.RefreshCookieName} {
		c := got[name]
		assert.Equal(t, "", c.Value, "%s value must be cleared", name)
		assert.True(t, c.MaxAge < 0, "%s must use Max-Age<0 so the browser drops it", name)
	}
}

func TestParseSameSiteFallsBackToLax(t *testing.T) {
	t.Parallel()
	assert.Equal(t, http.SameSiteStrictMode, handlers.ParseSameSite("Strict"))
	assert.Equal(t, http.SameSiteNoneMode, handlers.ParseSameSite("none"))
	assert.Equal(t, http.SameSiteLaxMode, handlers.ParseSameSite(""))
	assert.Equal(t, http.SameSiteLaxMode, handlers.ParseSameSite("garbage"))
}

func TestLogoutClearsCookiesAndIsIdempotent(t *testing.T) {
	t.Parallel()
	a := &handlers.Auth{
		SessionCookie: handlers.SessionCookieConfig{Secure: true, SameSite: http.SameSiteLaxMode},
	}

	rec := httptest.NewRecorder()
	a.Logout(rec, httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil))

	assert.Equal(t, http.StatusNoContent, rec.Code, "logout must succeed without a session")
	got := sessionCookiesFromResponse(t, rec)
	require.Contains(t, got, authmw.SessionCookieName)
	require.Contains(t, got, handlers.RefreshCookieName)
	assert.True(t, got[authmw.SessionCookieName].MaxAge < 0)
	assert.True(t, got[handlers.RefreshCookieName].MaxAge < 0)
}
