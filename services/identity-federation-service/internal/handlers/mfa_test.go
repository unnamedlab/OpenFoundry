package handlers_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base32"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/service"
)

// fakeMFARepo is an in-memory MFARepo for handler tests. Each method
// can be made to fail by setting the matching `*Err` field.
type fakeMFARepo struct {
	user *models.User
	cfg  *models.TOTPConfig

	findTOTPErr    error
	findUserErr    error
	upsertErr      error
	enableErr      error
	disableErr     error
	updateRecErr   error
	recordUsageErr error
}

func (f *fakeMFARepo) FindTOTPConfig(_ context.Context, userID uuid.UUID) (*models.TOTPConfig, error) {
	if f.findTOTPErr != nil {
		return nil, f.findTOTPErr
	}
	if f.cfg == nil || f.cfg.UserID != userID {
		return nil, nil
	}
	cp := *f.cfg
	return &cp, nil
}

func (f *fakeMFARepo) FindUserByID(_ context.Context, id uuid.UUID) (*models.User, error) {
	if f.findUserErr != nil {
		return nil, f.findUserErr
	}
	if f.user == nil || f.user.ID != id {
		return nil, nil
	}
	cp := *f.user
	return &cp, nil
}

func (f *fakeMFARepo) UpsertTOTPSecretEncrypted(_ context.Context, userID uuid.UUID, secretEncrypted, nonce []byte, hashes []string) error {
	if f.upsertErr != nil {
		return f.upsertErr
	}
	f.cfg = &models.TOTPConfig{
		UserID:             userID,
		SecretEncrypted:    append([]byte(nil), secretEncrypted...),
		SecretNonce:        append([]byte(nil), nonce...),
		RecoveryCodeHashes: append([]string(nil), hashes...),
		Enabled:            false,
		CreatedAt:          time.Now().UTC(),
		UpdatedAt:          time.Now().UTC(),
	}
	return nil
}

func (f *fakeMFARepo) EnableTOTP(_ context.Context, userID uuid.UUID, at time.Time) error {
	if f.enableErr != nil {
		return f.enableErr
	}
	if f.cfg == nil || f.cfg.UserID != userID {
		return errors.New("not found")
	}
	t := at
	f.cfg.Enabled = true
	f.cfg.VerifiedAt = &t
	return nil
}

func (f *fakeMFARepo) DisableTOTP(_ context.Context, userID uuid.UUID) error {
	if f.disableErr != nil {
		return f.disableErr
	}
	if f.cfg != nil && f.cfg.UserID == userID {
		f.cfg = nil
	}
	return nil
}

func (f *fakeMFARepo) UpdateRecoveryHashes(_ context.Context, userID uuid.UUID, hashes []string) error {
	if f.updateRecErr != nil {
		return f.updateRecErr
	}
	if f.cfg == nil || f.cfg.UserID != userID {
		return errors.New("not found")
	}
	f.cfg.RecoveryCodeHashes = append([]string(nil), hashes...)
	return nil
}

func (f *fakeMFARepo) RecordTOTPUsage(_ context.Context, userID uuid.UUID, counter int64, at time.Time) error {
	if f.recordUsageErr != nil {
		return f.recordUsageErr
	}
	if f.cfg == nil || f.cfg.UserID != userID {
		return errors.New("not found")
	}
	c, t := counter, at
	f.cfg.LastUsedCounter = &c
	f.cfg.LastUsedAt = &t
	return nil
}

// fakeIssuer satisfies handlers.TokenIssuer with deterministic strings.
// It records the auth methods so tests can assert on them.
type fakeIssuer struct {
	called      bool
	authMethods []string
	err         error
}

func (f *fakeIssuer) IssueTokens(_ context.Context, _ *models.User, methods []string) (string, string, error) {
	f.called = true
	f.authMethods = append([]string(nil), methods...)
	if f.err != nil {
		return "", "", f.err
	}
	return "access-token", "refresh-token", nil
}

func (f *fakeIssuer) AccessTokenTTL() time.Duration  { return time.Hour }
func (f *fakeIssuer) RefreshTokenTTL() time.Duration { return 7 * 24 * time.Hour }

func newMFAFixture(t *testing.T) (*handlers.MFA, *fakeMFARepo, *fakeIssuer, *models.User) {
	t.Helper()
	key := make([]byte, 32)
	_, err := rand.Read(key)
	require.NoError(t, err)
	sealer, err := service.NewSealerFromBase64Key(base64.StdEncoding.EncodeToString(key))
	require.NoError(t, err)
	user := &models.User{ID: uuid.New(), Email: "alice@example.com", Name: "Alice", IsActive: true}
	repo := &fakeMFARepo{user: user}
	issuer := &fakeIssuer{}
	h := &handlers.MFA{
		JWT:           authmw.NewJWTConfig("openfoundry-test-secret-aaaaaaaaaaaaaaaa"),
		Repo:          repo,
		Issuer:        issuer,
		Sealer:        sealer,
		SessionCookie: handlers.SessionCookieConfig{Secure: false, SameSite: http.SameSiteLaxMode},
	}
	return h, repo, issuer, user
}

func authedReq(t *testing.T, method, path string, body any, sub uuid.UUID) *http.Request {
	t.Helper()
	var rdr *bytes.Buffer
	if body != nil {
		buf, err := json.Marshal(body)
		require.NoError(t, err)
		rdr = bytes.NewBuffer(buf)
	} else {
		rdr = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	ctx := authmw.ContextWithClaims(req.Context(), &authmw.Claims{Sub: sub})
	return req.WithContext(ctx)
}

// totpCodeForSecret returns the currently valid 6-digit code for `secret`
// by brute-forcing the 1e6 search space against the public verifier.
//
// 1e6 worst-case iterations of a SHA-1 HMAC is sub-millisecond on
// modern hardware — fine for tests and keeps the helper from reaching
// into service internals.
func totpCodeForSecret(t *testing.T, secret string) (code string, counter int64) {
	t.Helper()
	for guess := 0; guess < 1_000_000; guess++ {
		c := fmt.Sprintf("%06d", guess)
		if cnt, ok := service.VerifyTOTPCounter(secret, c); ok {
			return c, cnt
		}
	}
	t.Fatalf("no TOTP code found for secret %q", secret)
	return "", 0
}

// secretFromEnrollResponse parses the otpauth URI in the enroll
// response and extracts the base32 secret so the test can compute a
// matching code without exposing internal repo state.
func secretFromEnrollResponse(t *testing.T, body []byte) string {
	t.Helper()
	var resp models.EnrollTOTPResponse
	require.NoError(t, json.Unmarshal(body, &resp))
	u, err := url.Parse(resp.OTPAuthURI)
	require.NoError(t, err)
	sec := u.Query().Get("secret")
	require.NotEmpty(t, sec)
	// And confirm it is in fact base32 of 20 bytes.
	_, err = base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(sec)
	require.NoError(t, err)
	return sec
}

// --- Enroll -------------------------------------------------------------

func TestEnrollReturnsSecretAndStoresCiphertext(t *testing.T) {
	t.Parallel()
	h, repo, _, user := newMFAFixture(t)

	req := authedReq(t, http.MethodPost, "/api/v1/auth/mfa/totp/enroll", nil, user.ID)
	rec := httptest.NewRecorder()
	h.Enroll(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	secret := secretFromEnrollResponse(t, rec.Body.Bytes())

	require.NotNil(t, repo.cfg, "row must exist after enrol")
	assert.NotEqual(t, []byte(secret), repo.cfg.SecretEncrypted,
		"DB column must hold ciphertext, not the base32 secret")
	assert.NotEmpty(t, repo.cfg.SecretEncrypted)
	assert.Len(t, repo.cfg.SecretNonce, 12)
	assert.Empty(t, repo.cfg.Secret,
		"legacy plaintext column must stay empty for new enrolments")
	assert.False(t, repo.cfg.Enabled, "enrol leaves the factor disabled until verify")
}

func TestEnrollWithoutSealerReturns503(t *testing.T) {
	t.Parallel()
	h, _, _, user := newMFAFixture(t)
	h.Sealer = nil

	req := authedReq(t, http.MethodPost, "/api/v1/auth/mfa/totp/enroll", nil, user.ID)
	rec := httptest.NewRecorder()
	h.Enroll(rec, req)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

// --- Verify (acceptance criterion: enroll → ConfirmTotp with code) ------

func TestVerifyConfirmsEnrolment(t *testing.T) {
	t.Parallel()
	h, repo, _, user := newMFAFixture(t)

	rec := httptest.NewRecorder()
	h.Enroll(rec, authedReq(t, http.MethodPost, "/enroll", nil, user.ID))
	require.Equal(t, http.StatusOK, rec.Code)
	secret := secretFromEnrollResponse(t, rec.Body.Bytes())

	code, counter := totpCodeForSecret(t, secret)

	rec = httptest.NewRecorder()
	h.Verify(rec, authedReq(t, http.MethodPost, "/verify",
		models.VerifyTOTPRequest{Code: code}, user.ID))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	assert.True(t, repo.cfg.Enabled, "verify must flip enabled=true")
	require.NotNil(t, repo.cfg.LastUsedCounter, "verify must record the consumed counter")
	assert.Equal(t, counter, *repo.cfg.LastUsedCounter)
}

func TestVerifyRejectsBadCode(t *testing.T) {
	t.Parallel()
	h, _, _, user := newMFAFixture(t)

	rec := httptest.NewRecorder()
	h.Enroll(rec, authedReq(t, http.MethodPost, "/enroll", nil, user.ID))
	require.Equal(t, http.StatusOK, rec.Code)

	rec = httptest.NewRecorder()
	h.Verify(rec, authedReq(t, http.MethodPost, "/verify",
		models.VerifyTOTPRequest{Code: "000000"}, user.ID))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// Replay detection: a code accepted once must not be accepted again,
// even when re-presented inside the same 30s window.
func TestVerifyRejectsReplay(t *testing.T) {
	t.Parallel()
	h, _, _, user := newMFAFixture(t)

	rec := httptest.NewRecorder()
	h.Enroll(rec, authedReq(t, http.MethodPost, "/enroll", nil, user.ID))
	require.Equal(t, http.StatusOK, rec.Code)
	secret := secretFromEnrollResponse(t, rec.Body.Bytes())
	code, _ := totpCodeForSecret(t, secret)

	rec = httptest.NewRecorder()
	h.Verify(rec, authedReq(t, http.MethodPost, "/verify",
		models.VerifyTOTPRequest{Code: code}, user.ID))
	require.Equal(t, http.StatusOK, rec.Code)

	// Same code, same window.
	rec = httptest.NewRecorder()
	h.Verify(rec, authedReq(t, http.MethodPost, "/verify",
		models.VerifyTOTPRequest{Code: code}, user.ID))
	assert.Equal(t, http.StatusUnauthorized, rec.Code,
		"replaying a previously accepted code must be rejected")
}

// --- Status / ListFactors / Disable -------------------------------------

func TestStatusReflectsEnabledFlag(t *testing.T) {
	t.Parallel()
	h, repo, _, user := newMFAFixture(t)
	repo.cfg = &models.TOTPConfig{UserID: user.ID, Enabled: true}

	rec := httptest.NewRecorder()
	h.Status(rec, authedReq(t, http.MethodGet, "/status", nil, user.ID))
	require.Equal(t, http.StatusOK, rec.Code)

	var s models.MFAStatusResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &s))
	assert.True(t, s.TOTPEnabled)
}

func TestListFactorsIncludesTOTP(t *testing.T) {
	t.Parallel()
	h, repo, _, user := newMFAFixture(t)
	confirmed := time.Now().Add(-time.Hour).UTC()
	used := time.Now().Add(-time.Minute).UTC()
	repo.cfg = &models.TOTPConfig{
		UserID:      user.ID,
		Enabled:     true,
		VerifiedAt:  &confirmed,
		LastUsedAt:  &used,
	}

	rec := httptest.NewRecorder()
	h.ListFactors(rec, authedReq(t, http.MethodGet, "/factors", nil, user.ID))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())

	var out models.ListFactorsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	require.Len(t, out.Factors, 1)
	assert.Equal(t, models.FactorTypeTOTP, out.Factors[0].Type)
	assert.True(t, out.Factors[0].Enabled)
	require.NotNil(t, out.Factors[0].ConfirmedAt)
	require.NotNil(t, out.Factors[0].LastUsedAt)
}

func TestListFactorsEmptyWhenNoFactor(t *testing.T) {
	t.Parallel()
	h, _, _, user := newMFAFixture(t)

	rec := httptest.NewRecorder()
	h.ListFactors(rec, authedReq(t, http.MethodGet, "/factors", nil, user.ID))
	require.Equal(t, http.StatusOK, rec.Code)

	var out models.ListFactorsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	assert.Empty(t, out.Factors)
}

func TestDisableClearsFactor(t *testing.T) {
	t.Parallel()
	h, repo, _, user := newMFAFixture(t)
	repo.cfg = &models.TOTPConfig{UserID: user.ID, Enabled: true}

	rec := httptest.NewRecorder()
	h.Disable(rec, authedReq(t, http.MethodPost, "/disable", nil, user.ID))
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Nil(t, repo.cfg)
}

// --- Verify edge cases (no auth context, malformed body, no enrolment) --

func TestVerifyRejectsMalformedBody(t *testing.T) {
	t.Parallel()
	h, _, _, user := newMFAFixture(t)
	req := httptest.NewRequest(http.MethodPost, "/verify", bytes.NewBufferString("{not json"))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(authmw.ContextWithClaims(req.Context(), &authmw.Claims{Sub: user.ID}))
	rec := httptest.NewRecorder()
	h.Verify(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestVerifyWithoutEnrolmentReturns404(t *testing.T) {
	t.Parallel()
	h, _, _, user := newMFAFixture(t)
	rec := httptest.NewRecorder()
	h.Verify(rec, authedReq(t, http.MethodPost, "/verify",
		models.VerifyTOTPRequest{Code: "000000"}, user.ID))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// --- Enroll edge cases --------------------------------------------------

func TestEnrollWithMissingUserReturns404(t *testing.T) {
	t.Parallel()
	h, repo, _, _ := newMFAFixture(t)
	repo.user = nil
	rec := httptest.NewRecorder()
	h.Enroll(rec, authedReq(t, http.MethodPost, "/enroll", nil, uuid.New()))
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// --- CompleteLogin ------------------------------------------------------

// mintChallenge mints a valid challenge_token for `userID` using the
// handler's JWT config, exactly like service.IssueMFAChallenge does.
func mintChallenge(t *testing.T, h *handlers.MFA, user *models.User) string {
	t.Helper()
	tok, err := service.IssueMFAChallenge(h.JWT, user, "password")
	require.NoError(t, err)
	return tok
}

func TestCompleteLoginRejectsMalformedBody(t *testing.T) {
	t.Parallel()
	h, _, _, _ := newMFAFixture(t)
	req := httptest.NewRequest(http.MethodPost, "/complete-login", bytes.NewBufferString("not-json"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.CompleteLogin(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCompleteLoginRejectsMissingFields(t *testing.T) {
	t.Parallel()
	h, _, _, _ := newMFAFixture(t)
	// challenge_token missing
	req := httptest.NewRequest(http.MethodPost, "/complete-login",
		bytes.NewBufferString(`{"code":"123456"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.CompleteLogin(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCompleteLoginRejectsInvalidChallenge(t *testing.T) {
	t.Parallel()
	h, _, _, _ := newMFAFixture(t)
	rec := httptest.NewRecorder()
	h.CompleteLogin(rec, httptest.NewRequest(http.MethodPost, "/complete-login",
		bytes.NewBufferString(`{"challenge_token":"garbage","code":"123456"}`)))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestCompleteLoginRejectsWhenFactorDisabled(t *testing.T) {
	t.Parallel()
	h, _, _, user := newMFAFixture(t)
	chal := mintChallenge(t, h, user)
	body := fmt.Sprintf(`{"challenge_token":%q,"code":"123456"}`, chal)
	rec := httptest.NewRecorder()
	h.CompleteLogin(rec, httptest.NewRequest(http.MethodPost, "/complete-login",
		bytes.NewBufferString(body)))
	assert.Equal(t, http.StatusForbidden, rec.Code, "no factor → 403")
}

func TestCompleteLoginRejectsWrongCode(t *testing.T) {
	t.Parallel()
	h, repo, _, user := newMFAFixture(t)
	// Plant an enabled factor with a known plaintext secret on the
	// legacy column (exercises recoverSecret's fallback path).
	repo.cfg = &models.TOTPConfig{
		UserID:  user.ID,
		Enabled: true,
		Secret:  "JBSWY3DPEHPK3PXP",
	}
	chal := mintChallenge(t, h, user)
	body := fmt.Sprintf(`{"challenge_token":%q,"code":"000000"}`, chal)
	rec := httptest.NewRecorder()
	h.CompleteLogin(rec, httptest.NewRequest(http.MethodPost, "/complete-login",
		bytes.NewBufferString(body)))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

// CompleteLoginHappyPath drives the end-to-end exchange of an MFA
// challenge token for a full token pair via a valid TOTP code. The
// fixture: enroll → verify → CompleteLogin.
func TestCompleteLoginExchangesChallengeForTokens(t *testing.T) {
	t.Parallel()
	h, repo, issuer, user := newMFAFixture(t)

	// Enroll + verify to produce an enabled, encrypted factor.
	rec := httptest.NewRecorder()
	h.Enroll(rec, authedReq(t, http.MethodPost, "/enroll", nil, user.ID))
	require.Equal(t, http.StatusOK, rec.Code)
	secret := secretFromEnrollResponse(t, rec.Body.Bytes())
	code, _ := totpCodeForSecret(t, secret)

	rec = httptest.NewRecorder()
	h.Verify(rec, authedReq(t, http.MethodPost, "/verify",
		models.VerifyTOTPRequest{Code: code}, user.ID))
	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, repo.cfg.Enabled)

	// Clear last_used so the next code (likely same window) is accepted.
	repo.cfg.LastUsedCounter = nil

	chal := mintChallenge(t, h, user)
	body := fmt.Sprintf(`{"challenge_token":%q,"code":%q}`, chal, code)
	rec = httptest.NewRecorder()
	h.CompleteLogin(rec, httptest.NewRequest(http.MethodPost, "/complete-login",
		bytes.NewBufferString(body)))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.True(t, issuer.called)
	assert.Equal(t, []string{"password", "totp"}, issuer.authMethods,
		"successful TOTP exchange must stamp auth_methods=[password,totp] — that is the mfa_passed signal")

	var out models.LoginResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	assert.Equal(t, "access-token", out.AccessToken)
	assert.Equal(t, "refresh-token", out.RefreshToken)

	cookies := map[string]*http.Cookie{}
	for _, c := range rec.Result().Cookies() {
		cookies[c.Name] = c
	}
	require.Contains(t, cookies, authmw.SessionCookieName, "mfa complete-login must emit of_session")
	require.Contains(t, cookies, handlers.RefreshCookieName, "mfa complete-login must emit of_refresh")
	assert.Equal(t, "access-token", cookies[authmw.SessionCookieName].Value)
	assert.True(t, cookies[authmw.SessionCookieName].HttpOnly)
	assert.Equal(t, "refresh-token", cookies[handlers.RefreshCookieName].Value)
}

func TestCompleteLoginConsumesRecoveryCode(t *testing.T) {
	t.Parallel()
	h, repo, issuer, user := newMFAFixture(t)

	// Plant an enabled factor with one known recovery code.
	hashed := service.HashRecoveryCodes([]string{"AAAA-AAAA", "BBBB-BBBB"})
	repo.cfg = &models.TOTPConfig{
		UserID:             user.ID,
		Enabled:            true,
		Secret:             "JBSWY3DPEHPK3PXP",
		RecoveryCodeHashes: hashed,
	}

	chal := mintChallenge(t, h, user)
	body := fmt.Sprintf(`{"challenge_token":%q,"recovery_code":"AAAA-AAAA"}`, chal)
	rec := httptest.NewRecorder()
	h.CompleteLogin(rec, httptest.NewRequest(http.MethodPost, "/complete-login",
		bytes.NewBufferString(body)))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	assert.True(t, issuer.called)
	assert.Equal(t, []string{"password", "recovery_code"}, issuer.authMethods)
	assert.Len(t, repo.cfg.RecoveryCodeHashes, 1, "consumed code must be removed")
}

func TestCompleteLoginRejectsUnknownRecoveryCode(t *testing.T) {
	t.Parallel()
	h, repo, _, user := newMFAFixture(t)
	repo.cfg = &models.TOTPConfig{
		UserID:             user.ID,
		Enabled:            true,
		Secret:             "JBSWY3DPEHPK3PXP",
		RecoveryCodeHashes: service.HashRecoveryCodes([]string{"AAAA-AAAA"}),
	}
	chal := mintChallenge(t, h, user)
	body := fmt.Sprintf(`{"challenge_token":%q,"recovery_code":"ZZZZ-ZZZZ"}`, chal)
	rec := httptest.NewRecorder()
	h.CompleteLogin(rec, httptest.NewRequest(http.MethodPost, "/complete-login",
		bytes.NewBufferString(body)))
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestCompleteLoginRejectsReplayedCode(t *testing.T) {
	t.Parallel()
	h, repo, _, user := newMFAFixture(t)

	// Enabled factor; pre-set LastUsedCounter so any current code is
	// flagged as replayed (counter <= last_used_counter).
	repo.cfg = &models.TOTPConfig{
		UserID:  user.ID,
		Enabled: true,
		Secret:  "JBSWY3DPEHPK3PXP",
	}
	huge := int64(1 << 40)
	repo.cfg.LastUsedCounter = &huge

	code, _ := totpCodeForSecret(t, "JBSWY3DPEHPK3PXP")
	chal := mintChallenge(t, h, user)
	body := fmt.Sprintf(`{"challenge_token":%q,"code":%q}`, chal, code)
	rec := httptest.NewRecorder()
	h.CompleteLogin(rec, httptest.NewRequest(http.MethodPost, "/complete-login",
		bytes.NewBufferString(body)))
	assert.Equal(t, http.StatusUnauthorized, rec.Code,
		"a fresh code whose counter is <= LastUsedCounter must be rejected as a replay")
}

// --- Repo error paths (drive the 500 branches in every handler) --------

func TestStatusReturns500OnRepoError(t *testing.T) {
	t.Parallel()
	h, repo, _, user := newMFAFixture(t)
	repo.findTOTPErr = errors.New("db down")
	rec := httptest.NewRecorder()
	h.Status(rec, authedReq(t, http.MethodGet, "/status", nil, user.ID))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestListFactorsReturns500OnRepoError(t *testing.T) {
	t.Parallel()
	h, repo, _, user := newMFAFixture(t)
	repo.findTOTPErr = errors.New("db down")
	rec := httptest.NewRecorder()
	h.ListFactors(rec, authedReq(t, http.MethodGet, "/factors", nil, user.ID))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestEnrollReturns500OnFindUserError(t *testing.T) {
	t.Parallel()
	h, repo, _, user := newMFAFixture(t)
	repo.findUserErr = errors.New("db down")
	rec := httptest.NewRecorder()
	h.Enroll(rec, authedReq(t, http.MethodPost, "/enroll", nil, user.ID))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestEnrollReturns500OnUpsertError(t *testing.T) {
	t.Parallel()
	h, repo, _, user := newMFAFixture(t)
	repo.upsertErr = errors.New("db down")
	rec := httptest.NewRecorder()
	h.Enroll(rec, authedReq(t, http.MethodPost, "/enroll", nil, user.ID))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestVerifyReturns500OnFindError(t *testing.T) {
	t.Parallel()
	h, repo, _, user := newMFAFixture(t)
	repo.findTOTPErr = errors.New("db down")
	rec := httptest.NewRecorder()
	h.Verify(rec, authedReq(t, http.MethodPost, "/verify",
		models.VerifyTOTPRequest{Code: "000000"}, user.ID))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestVerifyReturns500OnRecordUsageError(t *testing.T) {
	t.Parallel()
	h, repo, _, user := newMFAFixture(t)
	repo.cfg = &models.TOTPConfig{UserID: user.ID, Enabled: true, Secret: "JBSWY3DPEHPK3PXP"}
	repo.recordUsageErr = errors.New("db down")
	code, _ := totpCodeForSecret(t, "JBSWY3DPEHPK3PXP")
	rec := httptest.NewRecorder()
	h.Verify(rec, authedReq(t, http.MethodPost, "/verify",
		models.VerifyTOTPRequest{Code: code}, user.ID))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestVerifyReturns500OnEnableError(t *testing.T) {
	t.Parallel()
	h, repo, _, user := newMFAFixture(t)
	repo.cfg = &models.TOTPConfig{UserID: user.ID, Secret: "JBSWY3DPEHPK3PXP"}
	repo.enableErr = errors.New("db down")
	code, _ := totpCodeForSecret(t, "JBSWY3DPEHPK3PXP")
	rec := httptest.NewRecorder()
	h.Verify(rec, authedReq(t, http.MethodPost, "/verify",
		models.VerifyTOTPRequest{Code: code}, user.ID))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestDisableReturns500OnError(t *testing.T) {
	t.Parallel()
	h, repo, _, user := newMFAFixture(t)
	repo.disableErr = errors.New("db down")
	rec := httptest.NewRecorder()
	h.Disable(rec, authedReq(t, http.MethodPost, "/disable", nil, user.ID))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestCompleteLoginReturns500OnFindTOTPError(t *testing.T) {
	t.Parallel()
	h, repo, _, user := newMFAFixture(t)
	repo.findTOTPErr = errors.New("db down")
	chal := mintChallenge(t, h, user)
	body := fmt.Sprintf(`{"challenge_token":%q,"code":"123456"}`, chal)
	rec := httptest.NewRecorder()
	h.CompleteLogin(rec, httptest.NewRequest(http.MethodPost, "/complete-login",
		bytes.NewBufferString(body)))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestCompleteLoginReturns500OnUpdateRecoveryError(t *testing.T) {
	t.Parallel()
	h, repo, _, user := newMFAFixture(t)
	repo.cfg = &models.TOTPConfig{
		UserID:             user.ID,
		Enabled:            true,
		RecoveryCodeHashes: service.HashRecoveryCodes([]string{"AAAA-AAAA"}),
	}
	repo.updateRecErr = errors.New("db down")
	chal := mintChallenge(t, h, user)
	body := fmt.Sprintf(`{"challenge_token":%q,"recovery_code":"AAAA-AAAA"}`, chal)
	rec := httptest.NewRecorder()
	h.CompleteLogin(rec, httptest.NewRequest(http.MethodPost, "/complete-login",
		bytes.NewBufferString(body)))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestCompleteLoginReturns500OnFindUserError(t *testing.T) {
	t.Parallel()
	h, repo, _, user := newMFAFixture(t)
	repo.cfg = &models.TOTPConfig{
		UserID:             user.ID,
		Enabled:            true,
		RecoveryCodeHashes: service.HashRecoveryCodes([]string{"AAAA-AAAA"}),
	}
	repo.findUserErr = errors.New("db down")
	chal := mintChallenge(t, h, user)
	body := fmt.Sprintf(`{"challenge_token":%q,"recovery_code":"AAAA-AAAA"}`, chal)
	rec := httptest.NewRecorder()
	h.CompleteLogin(rec, httptest.NewRequest(http.MethodPost, "/complete-login",
		bytes.NewBufferString(body)))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestCompleteLoginReturns500OnIssuerError(t *testing.T) {
	t.Parallel()
	h, repo, issuer, user := newMFAFixture(t)
	issuer.err = errors.New("vault down")
	repo.cfg = &models.TOTPConfig{
		UserID:             user.ID,
		Enabled:            true,
		RecoveryCodeHashes: service.HashRecoveryCodes([]string{"AAAA-AAAA"}),
	}
	chal := mintChallenge(t, h, user)
	body := fmt.Sprintf(`{"challenge_token":%q,"recovery_code":"AAAA-AAAA"}`, chal)
	rec := httptest.NewRecorder()
	h.CompleteLogin(rec, httptest.NewRequest(http.MethodPost, "/complete-login",
		bytes.NewBufferString(body)))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestCompleteLoginReturns500OnRecordUsageError(t *testing.T) {
	t.Parallel()
	h, repo, _, user := newMFAFixture(t)
	repo.cfg = &models.TOTPConfig{UserID: user.ID, Enabled: true, Secret: "JBSWY3DPEHPK3PXP"}
	repo.recordUsageErr = errors.New("db down")
	code, _ := totpCodeForSecret(t, "JBSWY3DPEHPK3PXP")
	chal := mintChallenge(t, h, user)
	body := fmt.Sprintf(`{"challenge_token":%q,"code":%q}`, chal, code)
	rec := httptest.NewRecorder()
	h.CompleteLogin(rec, httptest.NewRequest(http.MethodPost, "/complete-login",
		bytes.NewBufferString(body)))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

// recoverSecret should fail (500) when the row has sealed bytes but
// the handler has no sealer wired — that's a config drift we want to
// surface loudly.
func TestVerifyReturns500WhenSecretIsUnreadable(t *testing.T) {
	t.Parallel()
	h, repo, _, user := newMFAFixture(t)
	repo.cfg = &models.TOTPConfig{
		UserID:          user.ID,
		Enabled:         true,
		SecretEncrypted: []byte("ciphertext"),
		SecretNonce:     make([]byte, 12),
	}
	h.Sealer = nil // sealed row + no key → unreadable
	rec := httptest.NewRecorder()
	h.Verify(rec, authedReq(t, http.MethodPost, "/verify",
		models.VerifyTOTPRequest{Code: "123456"}, user.ID))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
}

func TestCompleteLoginRejectsInactiveUser(t *testing.T) {
	t.Parallel()
	h, repo, _, user := newMFAFixture(t)
	user.IsActive = false
	repo.user = user

	repo.cfg = &models.TOTPConfig{
		UserID:             user.ID,
		Enabled:            true,
		RecoveryCodeHashes: service.HashRecoveryCodes([]string{"AAAA-AAAA"}),
	}
	chal := mintChallenge(t, h, user)
	body := fmt.Sprintf(`{"challenge_token":%q,"recovery_code":"AAAA-AAAA"}`, chal)
	rec := httptest.NewRecorder()
	h.CompleteLogin(rec, httptest.NewRequest(http.MethodPost, "/complete-login",
		bytes.NewBufferString(body)))
	assert.Equal(t, http.StatusForbidden, rec.Code)
}
