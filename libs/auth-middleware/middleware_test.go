package authmw_test

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
)

// capturingHandler is a slog.Handler that stores every record it
// receives so tests can assert on level + attrs.
type capturingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *capturingHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *capturingHandler) WithAttrs(_ []slog.Attr) slog.Handler         { return h }
func (h *capturingHandler) WithGroup(_ string) slog.Handler              { return h }
func (h *capturingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r.Clone())
	return nil
}
func (h *capturingHandler) snapshot() []slog.Record {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]slog.Record, len(h.records))
	copy(out, h.records)
	return out
}

func swapDefaultLogger(t *testing.T) *capturingHandler {
	t.Helper()
	h := &capturingHandler{}
	prev := slog.Default()
	slog.SetDefault(slog.New(h))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return h
}

func findAttr(r slog.Record, key string) (slog.Value, bool) {
	var out slog.Value
	found := false
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == key {
			out = a.Value
			found = true
			return false
		}
		return true
	})
	return out, found
}

func TestAnonymousIgnoresMissingAuth(t *testing.T) {
	// Not parallel: swaps slog.Default to assert silence on the
	// missing-header path.
	cfg, err := authmw.Generate()
	require.NoError(t, err)

	capture := swapDefaultLogger(t)
	baseInvalid := testutil.ToFloat64(authmw.InvalidTokenCounter().WithLabelValues("malformed_header"))

	called := false
	handler := authmw.Middleware(cfg, authmw.Options{AllowAnonymous: true})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, hasClaims := authmw.FromContext(r.Context())
			assert.False(t, hasClaims, "anonymous request must not carry claims")
			called = true
			w.WriteHeader(http.StatusOK)
		}),
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/public", nil)
	handler.ServeHTTP(rec, req)

	assert.True(t, called, "downstream handler must be invoked")
	assert.Equal(t, http.StatusOK, rec.Code)

	// No Authorization header at all → no log, no metric.
	assert.Empty(t, capture.snapshot(), "missing Authorization must not log")
	gotInvalid := testutil.ToFloat64(authmw.InvalidTokenCounter().WithLabelValues("malformed_header"))
	assert.Equal(t, baseInvalid, gotInvalid, "missing Authorization must not increment auth_invalid_token_total")
}

func TestAnonymousLogsInvalidJWT(t *testing.T) {
	// Not parallel: shared slog default + shared counter.
	cfg, err := authmw.Generate()
	require.NoError(t, err)

	capture := swapDefaultLogger(t)
	baseInvalid := testutil.ToFloat64(authmw.InvalidTokenCounter().WithLabelValues("invalid"))

	handler := authmw.Middleware(cfg, authmw.Options{AllowAnonymous: true})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, hasClaims := authmw.FromContext(r.Context())
			assert.False(t, hasClaims, "invalid token must not leak claims downstream")
			w.WriteHeader(http.StatusOK)
		}),
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/public/data", nil)
	req.RemoteAddr = "10.0.0.7:54321"
	req.Header.Set("Authorization", "Bearer not-a-real-jwt")
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code, "anonymous-allowed routes must still serve the request")

	records := capture.snapshot()
	require.NotEmpty(t, records, "invalid bearer token must emit a slog record")
	r := records[0]
	assert.Equal(t, slog.LevelWarn, r.Level)

	reason, ok := findAttr(r, "reason")
	require.True(t, ok, "log must include reason attr")
	assert.Equal(t, "invalid", reason.String())

	remote, ok := findAttr(r, "remote_addr")
	require.True(t, ok, "log must include remote_addr")
	assert.Equal(t, "10.0.0.7:54321", remote.String())

	path, ok := findAttr(r, "path")
	require.True(t, ok, "log must include path")
	assert.Equal(t, "/public/data", path.String())

	got := testutil.ToFloat64(authmw.InvalidTokenCounter().WithLabelValues("invalid"))
	assert.Equal(t, baseInvalid+1, got, "auth_invalid_token_total{reason=\"invalid\"} must increment by 1")
}

func TestAnonymousLogsWrongTokenUse(t *testing.T) {
	cfg, err := authmw.Generate()
	require.NoError(t, err)

	capture := swapDefaultLogger(t)
	baseWrong := testutil.ToFloat64(authmw.InvalidTokenCounter().WithLabelValues("wrong_token_use"))

	// Issue an mfa_challenge token signed by the same cfg.
	now := time.Now()
	use := "mfa_challenge"
	mfa := &authmw.Claims{
		Sub:      uuid.New(),
		IAT:      now.Unix(),
		EXP:      now.Add(5 * time.Minute).Unix(),
		JTI:      uuid.New(),
		Email:    "u@example.com",
		Name:     "U",
		Roles:    []string{},
		TokenUse: &use,
	}
	tok, err := authmw.EncodeToken(cfg, mfa)
	require.NoError(t, err)

	handler := authmw.Middleware(cfg, authmw.Options{AllowAnonymous: true})(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, hasClaims := authmw.FromContext(r.Context())
			assert.False(t, hasClaims, "wrong-token-use must not authenticate the request")
			w.WriteHeader(http.StatusOK)
		}),
	)

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/anywhere", nil)
	req.Header.Set("Authorization", "Bearer "+tok)
	handler.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	records := capture.snapshot()
	require.NotEmpty(t, records)
	reason, ok := findAttr(records[0], "reason")
	require.True(t, ok)
	assert.Equal(t, "wrong_token_use", reason.String())

	got := testutil.ToFloat64(authmw.InvalidTokenCounter().WithLabelValues("wrong_token_use"))
	assert.Equal(t, baseWrong+1, got)
}
