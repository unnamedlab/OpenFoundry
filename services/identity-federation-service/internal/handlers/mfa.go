package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/service"
)

// MFARepo is the slice of *repo.Repo that MFA depends on. Extracted so
// the handler can be unit-tested with an in-memory fake without
// pulling in Postgres / pgxpool.
type MFARepo interface {
	FindTOTPConfig(ctx context.Context, userID uuid.UUID) (*models.TOTPConfig, error)
	FindUserByID(ctx context.Context, id uuid.UUID) (*models.User, error)
	UpsertTOTPSecretEncrypted(ctx context.Context, userID uuid.UUID, secretEncrypted, nonce []byte, recoveryHashes []string) error
	EnableTOTP(ctx context.Context, userID uuid.UUID, at time.Time) error
	DisableTOTP(ctx context.Context, userID uuid.UUID) error
	UpdateRecoveryHashes(ctx context.Context, userID uuid.UUID, hashes []string) error
	RecordTOTPUsage(ctx context.Context, userID uuid.UUID, counter int64, at time.Time) error
}

// TokenIssuer is the slice of *service.Issuer that MFA depends on.
// Tests fake it; production wiring still passes a concrete *Issuer.
type TokenIssuer interface {
	IssueTokens(ctx context.Context, user *models.User, authMethods []string) (string, string, error)
	AccessTokenTTL() time.Duration
	RefreshTokenTTL() time.Duration
}

// MFA wires GET /status, /factors, POST totp/{enroll,verify,disable,complete-login}.
type MFA struct {
	JWT    *authmw.JWTConfig
	Repo   MFARepo
	Issuer TokenIssuer

	// Sealer encrypts/decrypts the TOTP secret at rest. Nil disables
	// the encrypt-on-enroll path (handlers return 503) — production
	// MUST set MFA_AT_REST_KEY.
	Sealer *service.Sealer

	// SessionCookie shapes the of_session / of_refresh cookies
	// emitted from /mfa/totp/complete-login. Mirrors Auth.SessionCookie.
	SessionCookie SessionCookieConfig
}

// Status handles GET /api/v1/auth/mfa/status. Authenticated.
func (m *MFA) Status(w http.ResponseWriter, r *http.Request) {
	c := authmw.MustFromContext(r.Context())
	cfg, err := m.Repo.FindTOTPConfig(r.Context(), c.Sub)
	if err != nil {
		slog.Error("mfa status: find totp", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := models.MFAStatusResponse{}
	if cfg != nil {
		out.TOTPEnabled = cfg.Enabled
	}
	writeJSON(w, http.StatusOK, out)
}

// ListFactors handles GET /api/v1/auth/mfa/factors. Authenticated.
//
// Returns the per-type MFA state for the caller. WebAuthn is reported
// best-effort: the slice-4 service owns those credentials and there is
// no live wiring on this handler yet, so the entry is omitted when no
// TOTP factor exists either. The frontend uses this to render the
// per-factor management table.
func (m *MFA) ListFactors(w http.ResponseWriter, r *http.Request) {
	c := authmw.MustFromContext(r.Context())
	cfg, err := m.Repo.FindTOTPConfig(r.Context(), c.Sub)
	if err != nil {
		slog.Error("mfa factors: find totp", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	out := models.ListFactorsResponse{Factors: []models.Factor{}}
	if cfg != nil {
		out.Factors = append(out.Factors, models.Factor{
			Type:        models.FactorTypeTOTP,
			Enabled:     cfg.Enabled,
			ConfirmedAt: cfg.VerifiedAt,
			LastUsedAt:  cfg.LastUsedAt,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// Enroll handles POST /api/v1/auth/mfa/totp/enroll. Authenticated.
//
// The plaintext secret + recovery codes are returned to the client
// ONCE (so the authenticator app can be provisioned via the otpauth
// URI / QR). On the server side the secret is sealed with AES-GCM
// under MFA_AT_REST_KEY before persistence — the column never holds
// plaintext for new enrolments.
func (m *MFA) Enroll(w http.ResponseWriter, r *http.Request) {
	c := authmw.MustFromContext(r.Context())
	if m.Sealer == nil {
		writeJSONErr(w, http.StatusServiceUnavailable, "mfa at-rest key not configured")
		return
	}
	user, err := m.Repo.FindUserByID(r.Context(), c.Sub)
	if err != nil {
		slog.Error("mfa enroll: find user", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if user == nil {
		writeJSONErr(w, http.StatusNotFound, "user not found")
		return
	}

	enrol, err := service.CreateEnrollment(user.Email)
	if err != nil {
		slog.Error("mfa enroll: create enrolment", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	ciphertext, nonce, err := m.Sealer.Seal([]byte(enrol.Secret))
	if err != nil {
		slog.Error("mfa enroll: seal", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	hashes := service.HashRecoveryCodes(enrol.RecoveryCodes)
	if err := m.Repo.UpsertTOTPSecretEncrypted(r.Context(), c.Sub, ciphertext, nonce, hashes); err != nil {
		slog.Error("mfa enroll: upsert", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, models.EnrollTOTPResponse{
		Secret: enrol.Secret, RecoveryCodes: enrol.RecoveryCodes, OTPAuthURI: enrol.OTPAuthURI,
	})
}

// Verify handles POST /api/v1/auth/mfa/totp/verify. Authenticated.
//
// Confirms enrolment: checks the code against the stored secret in a
// ±1 window, flips enabled=true, and records the accepted counter to
// block replays.
func (m *MFA) Verify(w http.ResponseWriter, r *http.Request) {
	c := authmw.MustFromContext(r.Context())
	var body models.VerifyTOTPRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	cfg, err := m.Repo.FindTOTPConfig(r.Context(), c.Sub)
	if err != nil {
		slog.Error("mfa verify: find", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if cfg == nil {
		writeJSONErr(w, http.StatusNotFound, "no totp enrolment")
		return
	}
	secret, ok := m.recoverSecret(cfg)
	if !ok {
		writeJSONErr(w, http.StatusInternalServerError, "totp secret unreadable")
		return
	}
	counter, ok := service.VerifyTOTPCounter(secret, body.Code)
	if !ok || replayed(cfg, counter) {
		writeJSONErr(w, http.StatusUnauthorized, "invalid code")
		return
	}
	now := time.Now().UTC()
	if err := m.Repo.RecordTOTPUsage(r.Context(), c.Sub, counter, now); err != nil {
		slog.Error("mfa verify: record usage", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := m.Repo.EnableTOTP(r.Context(), c.Sub, now); err != nil {
		slog.Error("mfa verify: enable", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": true})
}

// Disable handles POST /api/v1/auth/mfa/totp/disable. Authenticated.
func (m *MFA) Disable(w http.ResponseWriter, r *http.Request) {
	c := authmw.MustFromContext(r.Context())
	if err := m.Repo.DisableTOTP(r.Context(), c.Sub); err != nil {
		slog.Error("mfa disable", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"enabled": false})
}

// CompleteLogin handles POST /api/v1/auth/mfa/totp/complete-login. UNAUTH
// at the bearer layer — the request carries its own MFA challenge token
// minted by the slice-1 login flow and a TOTP code (or recovery code).
func (m *MFA) CompleteLogin(w http.ResponseWriter, r *http.Request) {
	var body models.CompleteLoginRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	if body.ChallengeToken == "" || (body.Code == "" && body.RecoveryCode == "") {
		writeJSONErr(w, http.StatusBadRequest, "challenge_token + (code or recovery_code) required")
		return
	}

	claims, err := service.ValidateMFAChallenge(m.JWT, body.ChallengeToken)
	if err != nil {
		writeJSONErr(w, http.StatusUnauthorized, "invalid challenge")
		return
	}

	cfg, err := m.Repo.FindTOTPConfig(r.Context(), claims.Sub)
	if err != nil {
		slog.Error("mfa complete: find totp", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if cfg == nil || !cfg.Enabled {
		writeJSONErr(w, http.StatusForbidden, "mfa not enabled")
		return
	}

	authMethod := "totp"
	if body.RecoveryCode != "" {
		remaining, ok := service.ConsumeRecoveryCode(cfg.RecoveryCodeHashes, body.RecoveryCode)
		if !ok {
			writeJSONErr(w, http.StatusUnauthorized, "invalid recovery code")
			return
		}
		if err := m.Repo.UpdateRecoveryHashes(r.Context(), claims.Sub, remaining); err != nil {
			slog.Error("mfa complete: update recovery hashes", slog.String("error", err.Error()))
			writeJSONErr(w, http.StatusInternalServerError, "internal error")
			return
		}
		authMethod = "recovery_code"
	} else {
		secret, ok := m.recoverSecret(cfg)
		if !ok {
			writeJSONErr(w, http.StatusInternalServerError, "totp secret unreadable")
			return
		}
		counter, ok := service.VerifyTOTPCounter(secret, body.Code)
		if !ok || replayed(cfg, counter) {
			writeJSONErr(w, http.StatusUnauthorized, "invalid code")
			return
		}
		if err := m.Repo.RecordTOTPUsage(r.Context(), claims.Sub, counter, time.Now().UTC()); err != nil {
			slog.Error("mfa complete: record usage", slog.String("error", err.Error()))
			writeJSONErr(w, http.StatusInternalServerError, "internal error")
			return
		}
	}

	user, err := m.Repo.FindUserByID(r.Context(), claims.Sub)
	if err != nil {
		slog.Error("mfa complete: find user", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if user == nil || !user.IsActive {
		writeJSONErr(w, http.StatusForbidden, "user disabled")
		return
	}

	access, refresh, err := m.Issuer.IssueTokens(r.Context(), user, []string{"password", authMethod})
	if err != nil {
		slog.Error("mfa complete: issue tokens", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	SetSessionCookie(w, m.SessionCookie, access, m.Issuer.AccessTokenTTL())
	SetRefreshCookie(w, m.SessionCookie, refresh, m.Issuer.RefreshTokenTTL())
	writeJSON(w, http.StatusOK, models.LoginResponse{
		Status:       models.LoginStatusAuthenticated,
		AccessToken:  access,
		RefreshToken: refresh,
		TokenType:    "Bearer",
		ExpiresIn:    int64(m.Issuer.AccessTokenTTL().Seconds()),
	})
}

// recoverSecret prefers the sealed column; on legacy rows (pre-0013)
// it falls back to the plaintext column so existing enrolments keep
// working until the user re-enrolls.
func (m *MFA) recoverSecret(cfg *models.TOTPConfig) (string, bool) {
	if len(cfg.SecretEncrypted) > 0 && len(cfg.SecretNonce) > 0 {
		if m.Sealer == nil {
			return "", false
		}
		pt, err := m.Sealer.Open(cfg.SecretEncrypted, cfg.SecretNonce)
		if err != nil {
			slog.Error("mfa: open sealed secret", slog.String("error", err.Error()))
			return "", false
		}
		return string(pt), true
	}
	if cfg.Secret != "" {
		return cfg.Secret, true
	}
	return "", false
}

// replayed reports whether the matching counter has already been
// accepted (or is older than the last accepted counter).
func replayed(cfg *models.TOTPConfig, counter int64) bool {
	return cfg.LastUsedCounter != nil && counter <= *cfg.LastUsedCounter
}

var _ = errors.New
