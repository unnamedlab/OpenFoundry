package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/service"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/webauthn"
)

// WebAuthn wires the 4 WebAuthn endpoints + the FinishLogin path.
type WebAuthn struct {
	JWT     *authmw.JWTConfig
	Repo    *repo.Repo
	Service *webauthn.Service
	Issuer  *service.Issuer
}

// RegisterChallenge handles POST /api/v1/auth/mfa/webauthn/register/challenge (auth).
//
// Returns the PublicKeyCredentialCreationOptions ready for navigator.credentials.create
// + the challenge_id the client posts back to /finish.
func (h *WebAuthn) RegisterChallenge(w http.ResponseWriter, r *http.Request) {
	c := authmw.MustFromContext(r.Context())
	user, err := h.Repo.FindUserByID(r.Context(), c.Sub)
	if err != nil || user == nil {
		writeJSONErr(w, http.StatusInternalServerError, "user lookup failed")
		return
	}
	options, chID, err := h.Service.BeginRegistration(r.Context(), user)
	if err != nil {
		slog.Error("webauthn register challenge", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"challenge_id": chID,
		"options":      options,
	})
}

// RegisterFinish handles POST /api/v1/auth/mfa/webauthn/register/finish (auth).
type registerFinishBody struct {
	ChallengeID string          `json:"challenge_id"`
	Response    json.RawMessage `json:"response"`
}

func (h *WebAuthn) RegisterFinish(w http.ResponseWriter, r *http.Request) {
	c := authmw.MustFromContext(r.Context())
	var body registerFinishBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	chID, err := uuid.Parse(body.ChallengeID)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid challenge_id")
		return
	}
	user, err := h.Repo.FindUserByID(r.Context(), c.Sub)
	if err != nil || user == nil {
		writeJSONErr(w, http.StatusInternalServerError, "user lookup failed")
		return
	}
	parsed, err := protocol.ParseCredentialCreationResponseBody(strings.NewReader(string(body.Response)))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid attestation response")
		return
	}
	cred, err := h.Service.FinishRegistration(r.Context(), user, chID, parsed)
	if err != nil {
		switch {
		case errors.Is(err, webauthn.ErrChallengeMismatch),
			errors.Is(err, webauthn.ErrChallengeExpired):
			writeJSONErr(w, http.StatusForbidden, err.Error())
		default:
			slog.Error("webauthn register finish", slog.String("error", err.Error()))
			writeJSONErr(w, http.StatusInternalServerError, "internal error")
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"credential_id": cred.ID,
		"transports":    cred.Transports,
	})
}

// LoginChallenge handles POST /api/v1/auth/mfa/webauthn/login/challenge.
//
// Unauth at the bearer layer — the client carries an MFA challenge
// token from the slice-1 login flow; we extract the user id from it.
type loginChallengeBody struct {
	ChallengeToken string `json:"challenge_token"`
}

func (h *WebAuthn) LoginChallenge(w http.ResponseWriter, r *http.Request) {
	var body loginChallengeBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	claims, err := service.ValidateMFAChallenge(h.JWT, body.ChallengeToken)
	if err != nil {
		writeJSONErr(w, http.StatusUnauthorized, "invalid challenge")
		return
	}
	user, err := h.Repo.FindUserByID(r.Context(), claims.Sub)
	if err != nil || user == nil {
		writeJSONErr(w, http.StatusInternalServerError, "user lookup failed")
		return
	}
	options, chID, err := h.Service.BeginLogin(r.Context(), user)
	if err != nil {
		if errors.Is(err, webauthn.ErrNoCredentials) {
			writeJSONErr(w, http.StatusForbidden, "no webauthn credentials")
			return
		}
		slog.Error("webauthn login challenge", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"challenge_id": chID,
		"options":      options,
	})
}

// LoginFinish handles POST /api/v1/auth/mfa/webauthn/login/finish.
//
// Unauth — same MFA-challenge flow as TOTP complete-login. Returns
// access + refresh tokens on success.
type loginFinishBody struct {
	ChallengeToken string          `json:"challenge_token"`
	ChallengeID    string          `json:"challenge_id"`
	Response       json.RawMessage `json:"response"`
}

func (h *WebAuthn) LoginFinish(w http.ResponseWriter, r *http.Request) {
	var body loginFinishBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	claims, err := service.ValidateMFAChallenge(h.JWT, body.ChallengeToken)
	if err != nil {
		writeJSONErr(w, http.StatusUnauthorized, "invalid challenge")
		return
	}
	chID, err := uuid.Parse(body.ChallengeID)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid challenge_id")
		return
	}
	user, err := h.Repo.FindUserByID(r.Context(), claims.Sub)
	if err != nil || user == nil {
		writeJSONErr(w, http.StatusInternalServerError, "user lookup failed")
		return
	}
	parsed, err := protocol.ParseCredentialRequestResponseBody(strings.NewReader(string(body.Response)))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid assertion response")
		return
	}
	if _, err := h.Service.FinishLogin(r.Context(), user, chID, parsed); err != nil {
		switch {
		case errors.Is(err, webauthn.ErrChallengeMismatch),
			errors.Is(err, webauthn.ErrChallengeExpired):
			writeJSONErr(w, http.StatusForbidden, err.Error())
		default:
			slog.Error("webauthn login finish", slog.String("error", err.Error()))
			writeJSONErr(w, http.StatusInternalServerError, "internal error")
		}
		return
	}
	access, refresh, ierr := h.Issuer.IssueTokens(r.Context(), user, []string{"password", "webauthn"})
	if ierr != nil {
		slog.Error("webauthn login finish: issue tokens", slog.String("error", ierr.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSONLoginResponse(w, access, refresh, int64(h.Issuer.AccessTTL.Seconds()))
}

func writeJSONLoginResponse(w http.ResponseWriter, access, refresh string, expiresIn int64) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":        "authenticated",
		"access_token":  access,
		"refresh_token": refresh,
		"token_type":    "Bearer",
		"expires_in":    expiresIn,
	})
}
