package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/service"
)

// MFA wires GET /status, POST totp/{enroll,verify,disable,complete-login}.
type MFA struct {
	JWT    *authmw.JWTConfig
	Repo   *repo.Repo
	Issuer *service.Issuer
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
	// WebAuthnConfigured stays false until slice 4.
	writeJSON(w, http.StatusOK, out)
}

// Enroll handles POST /api/v1/auth/mfa/totp/enroll. Authenticated.
//
// Returns secret + recovery codes ONCE. Clients must persist; the
// server only stores hashes.
func (m *MFA) Enroll(w http.ResponseWriter, r *http.Request) {
	c := authmw.MustFromContext(r.Context())
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
	hashes := service.HashRecoveryCodes(enrol.RecoveryCodes)
	if err := m.Repo.UpsertTOTPSecret(r.Context(), c.Sub, enrol.Secret, hashes); err != nil {
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
// Verifies the code against the stored secret and flips enabled=true.
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
	if !service.VerifyTOTP(cfg.Secret, body.Code) {
		writeJSONErr(w, http.StatusUnauthorized, "invalid code")
		return
	}
	if err := m.Repo.EnableTOTP(r.Context(), c.Sub, time.Now().UTC()); err != nil {
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
	} else if !service.VerifyTOTP(cfg.Secret, body.Code) {
		writeJSONErr(w, http.StatusUnauthorized, "invalid code")
		return
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
	writeJSON(w, http.StatusOK, models.LoginResponse{
		Status:       models.LoginStatusAuthenticated,
		AccessToken:  access,
		RefreshToken: refresh,
		TokenType:    "Bearer",
		ExpiresIn:    int64(m.Issuer.AccessTTL.Seconds()),
	})
}

// silence unused import in some build configurations
var _ = errors.New
