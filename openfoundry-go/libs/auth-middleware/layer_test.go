package authmw_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
)

func TestAuthLayerStashesClaimsAndPassesThrough(t *testing.T) {
	t.Parallel()
	cfg := authmw.NewJWTConfig("openfoundry-shared-test-secret-do-not-use-in-prod-aaaa")
	now := time.Now()
	tok, err := authmw.EncodeToken(cfg, &authmw.Claims{
		Sub:   uuid.New(),
		IAT:   now.Unix(),
		EXP:   now.Add(time.Hour).Unix(),
		JTI:   uuid.New(),
		Email: "tester@openfoundry.test",
		Name:  "Tester",
		Roles: []string{"admin"},
	})
	require.NoError(t, err)

	var seen authmw.AuthUser
	handler := authmw.AuthLayer(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var ok bool
		seen, ok = authmw.AuthUserFromRequest(r)
		if !ok {
			http.Error(w, "no user", http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.NotNil(t, seen.Claims)
	assert.Equal(t, "tester@openfoundry.test", seen.Claims.Email)
}

func TestAuthLayerRejectsMissingBearerWith401(t *testing.T) {
	t.Parallel()
	cfg := authmw.NewJWTConfig("openfoundry-shared-test-secret-do-not-use-in-prod-aaaa")
	handler := authmw.AuthLayer(cfg)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Errorf("inner handler should not run")
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestAuthUserFromContextReturnsFalseWhenAbsent(t *testing.T) {
	t.Parallel()
	_, ok := authmw.AuthUserFromContext(context.Background())
	assert.False(t, ok)
}

func TestMustAuthUserPanicsWhenAbsent(t *testing.T) {
	t.Parallel()
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic")
		}
	}()
	authmw.MustAuthUser(context.Background())
}

func TestMustAuthUserReturnsWrappedClaims(t *testing.T) {
	t.Parallel()
	c := &authmw.Claims{Sub: uuid.New(), Email: "u@o.test"}
	ctx := authmw.ContextWithClaims(context.Background(), c)
	user := authmw.MustAuthUser(ctx)
	assert.Equal(t, c, user.Claims)
}
