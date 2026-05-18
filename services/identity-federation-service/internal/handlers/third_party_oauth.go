package handlers

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/core-models/ids"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/models"
)

const (
	oauthAuthorizationCodeTTL = 10 * time.Minute
	oauthPKCEMethodS256       = "S256"
	oauthTokenTypeBearer      = "Bearer"
)

type OAuthAccessTokenIssuer interface {
	AccessTokenTTL() time.Duration
	RefreshTokenTTL() time.Duration
	IssueAccessTokenForOAuthClient(user *models.User, app *models.ThirdPartyApplication, scopes []string, authMethods []string, maxExpiry *time.Time) (string, int64, error)
}

type oauthAuthorizationRequest struct {
	ClientID            string
	RedirectURI         string
	ResponseType        string
	Scopes              []string
	State               string
	CodeChallenge       string
	CodeChallengeMethod string
	OrganizationID      *uuid.UUID
	Approve             bool
}

type oauthPreparedAuthorization struct {
	App                 *models.ThirdPartyApplication
	User                *models.User
	OrganizationID      uuid.UUID
	Enablement          models.ThirdPartyApplicationEnablement
	RequestedScopes     []string
	GrantedScopes       []string
	MissingScopes       []string
	CodeChallengeMethod string
}

type oauthHTTPError struct {
	status  int
	message string
}

func (e oauthHTTPError) Error() string { return e.message }

func (h *RBAC) OAuthAuthorizePrompt(w http.ResponseWriter, r *http.Request) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	req, err := oauthAuthorizationRequestFromQuery(r.URL.Query())
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	prepared, err := h.prepareOAuthAuthorization(r.Context(), claims, req)
	if err != nil {
		writeOAuthError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, models.OAuthAuthorizePromptResponse{
		Application:          *prepared.App,
		ClientID:             prepared.App.ClientID,
		RedirectURI:          req.RedirectURI,
		State:                req.State,
		OrganizationID:       prepared.OrganizationID,
		RequestedScopes:      prepared.RequestedScopes,
		GrantedScopes:        prepared.GrantedScopes,
		MissingScopes:        prepared.MissingScopes,
		CodeChallengeMethod:  prepared.CodeChallengeMethod,
		OrganizationConsent:  prepared.Enablement.OrganizationConsent,
		RequiresUserConsent:  true,
		ConsentPrompt:        "Authorize this third-party application only if you trust it. Granted capabilities are limited to the listed scopes and your current OpenFoundry permissions.",
		EnablementConstraint: "This application can only be authorized for organizations where an administrator has enabled it.",
	})
}

func (h *RBAC) OAuthConsent(w http.ResponseWriter, r *http.Request) {
	claims, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	var body models.OAuthConsentRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	req := oauthAuthorizationRequestFromConsent(body)
	prepared, err := h.prepareOAuthAuthorization(r.Context(), claims, req)
	if err != nil {
		writeOAuthError(w, err)
		return
	}
	if !body.Approve {
		writeJSON(w, http.StatusOK, models.OAuthConsentResponse{
			RedirectURI:      oauthRedirectWithParams(req.RedirectURI, map[string]string{"error": "access_denied", "state": req.State}),
			State:            req.State,
			Error:            "access_denied",
			ErrorDescription: "user denied authorization",
		})
		return
	}
	now := time.Now().UTC()
	codePlaintext, err := newOAuthAuthorizationCodePlaintext()
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	code := &models.ThirdPartyOAuthAuthorizationCode{
		ID:                  ids.New(),
		CodeHash:            hashOAuthOpaqueToken(codePlaintext),
		ApplicationID:       prepared.App.ID,
		ClientID:            prepared.App.ClientID,
		UserID:              prepared.User.ID,
		OrganizationID:      prepared.OrganizationID,
		RedirectURI:         req.RedirectURI,
		State:               req.State,
		CodeChallenge:       req.CodeChallenge,
		CodeChallengeMethod: prepared.CodeChallengeMethod,
		RequestedScopes:     prepared.RequestedScopes,
		GrantedScopes:       prepared.GrantedScopes,
		CreatedAt:           now,
		ExpiresAt:           now.Add(oauthAuthorizationCodeTTL),
	}
	if err := h.Repo.CreateThirdPartyOAuthAuthorizationCode(r.Context(), code); err != nil {
		slog.Error("create oauth authorization code", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := h.Repo.UpsertThirdPartyOAuthConsent(r.Context(), prepared.App.ID, prepared.User.ID, prepared.OrganizationID, prepared.GrantedScopes, now); err != nil {
		slog.Error("upsert oauth consent", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusCreated, models.OAuthConsentResponse{
		RedirectURI:   oauthRedirectWithParams(req.RedirectURI, map[string]string{"code": codePlaintext, "state": req.State}),
		Code:          codePlaintext,
		State:         req.State,
		GrantedScopes: prepared.GrantedScopes,
		ExpiresAt:     code.ExpiresAt,
	})
}

func (h *RBAC) OAuthToken(w http.ResponseWriter, r *http.Request) {
	if h.OAuthIssuer == nil {
		writeJSONErr(w, http.StatusServiceUnavailable, "oauth issuer unavailable")
		return
	}
	body, err := decodeOAuthTokenRequest(r)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	applyOAuthBasicAuth(r, &body)
	switch strings.TrimSpace(body.GrantType) {
	case models.OAuthGrantAuthorizationCode:
		h.oauthAuthorizationCodeToken(w, r, body)
	case models.OAuthGrantRefreshToken:
		h.oauthRefreshToken(w, r, body)
	case models.OAuthGrantClientCredentials:
		h.oauthClientCredentialsToken(w, r, body)
	default:
		writeJSONErr(w, http.StatusBadRequest, "unsupported grant_type")
	}
}

func (h *RBAC) OAuthRevoke(w http.ResponseWriter, r *http.Request) {
	body, err := decodeOAuthRevokeRequest(r)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	applyOAuthBasicAuthToRevoke(r, &body)
	token := strings.TrimSpace(body.Token)
	if token == "" {
		writeJSONErr(w, http.StatusBadRequest, "token required")
		return
	}
	now := time.Now().UTC()
	row, err := h.Repo.FindThirdPartyOAuthRefreshToken(r.Context(), hashOAuthOpaqueToken(token))
	if err != nil {
		slog.Error("lookup oauth refresh token for revocation", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if row != nil {
		app, secretHash, err := h.Repo.GetThirdPartyApplicationByClientID(r.Context(), row.ClientID)
		if err != nil {
			slog.Error("lookup oauth client for revocation", slog.String("error", err.Error()))
			writeJSONErr(w, http.StatusInternalServerError, "internal error")
			return
		}
		if app != nil {
			clientID := body.ClientID
			if clientID == "" {
				clientID = row.ClientID
			}
			if err := validateOAuthClientAuthentication(app, secretHash, clientID, body.ClientSecret); err != nil {
				writeOAuthError(w, err)
				return
			}
		}
	}
	if err := h.Repo.RevokeThirdPartyOAuthRefreshTokenByHash(r.Context(), hashOAuthOpaqueToken(token), now); err != nil {
		slog.Error("revoke oauth refresh token", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	w.WriteHeader(http.StatusOK)
}

func (h *RBAC) ListOAuthAuthorizations(w http.ResponseWriter, r *http.Request) {
	userID := authCallerID(r)
	if userID == uuid.Nil {
		writeJSONErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	items, err := h.Repo.ListThirdPartyOAuthAuthorizations(r.Context(), userID, time.Now().UTC())
	if err != nil {
		slog.Error("list oauth authorizations", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, models.ListOAuthAuthorizationsResponse{Items: items, Total: len(items)})
}

func (h *RBAC) RevokeOAuthAuthorization(w http.ResponseWriter, r *http.Request) {
	userID := authCallerID(r)
	if userID == uuid.Nil {
		writeJSONErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid id")
		return
	}
	ok, err := h.Repo.RevokeThirdPartyOAuthRefreshTokenByID(r.Context(), userID, id, time.Now().UTC())
	if err != nil {
		slog.Error("revoke oauth authorization", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !ok {
		writeJSONErr(w, http.StatusNotFound, "not found")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h *RBAC) oauthAuthorizationCodeToken(w http.ResponseWriter, r *http.Request, body models.OAuthTokenRequest) {
	if strings.TrimSpace(body.Code) == "" || strings.TrimSpace(body.RedirectURI) == "" || strings.TrimSpace(body.CodeVerifier) == "" {
		writeJSONErr(w, http.StatusBadRequest, "code, redirect_uri, and code_verifier are required")
		return
	}
	app, secretHash, err := h.Repo.GetThirdPartyApplicationByClientID(r.Context(), strings.TrimSpace(body.ClientID))
	if err != nil {
		slog.Error("lookup oauth client", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := validateOAuthClientAuthentication(app, secretHash, body.ClientID, body.ClientSecret); err != nil {
		writeOAuthError(w, err)
		return
	}
	if !containsThirdPartyString(app.EnabledGrantTypes, models.ThirdPartyGrantAuthorizationCode) {
		writeJSONErr(w, http.StatusBadRequest, "client is not enabled for authorization_code")
		return
	}
	now := time.Now().UTC()
	code, err := h.Repo.ConsumeThirdPartyOAuthAuthorizationCode(r.Context(), hashOAuthOpaqueToken(body.Code), now)
	if err != nil {
		slog.Error("consume oauth authorization code", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if code == nil || code.ClientID != app.ClientID || code.RedirectURI != strings.TrimSpace(body.RedirectURI) {
		writeJSONErr(w, http.StatusUnauthorized, "invalid authorization code")
		return
	}
	if !verifyOAuthPKCE(code.CodeChallengeMethod, code.CodeChallenge, strings.TrimSpace(body.CodeVerifier)) {
		writeJSONErr(w, http.StatusUnauthorized, "invalid code_verifier")
		return
	}
	user, err := h.Repo.FindUserByID(r.Context(), code.UserID)
	if err != nil {
		slog.Error("lookup oauth subject user", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if user == nil || !user.IsActive || user.DeletedAt != nil {
		writeJSONErr(w, http.StatusUnauthorized, "subject user inactive")
		return
	}
	if _, ok := enabledThirdPartyApplicationForOrganization(app, code.OrganizationID); !ok {
		writeJSONErr(w, http.StatusForbidden, "application is not enabled for this organization")
		return
	}
	access, expiresIn, err := h.OAuthIssuer.IssueAccessTokenForOAuthClient(user, app, code.GrantedScopes, []string{"oauth_authorization_code"}, nil)
	if err != nil {
		slog.Error("issue oauth authorization_code access token", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusUnauthorized, "token issuance failed")
		return
	}
	refreshPlaintext, refreshRow, err := h.newOAuthRefreshToken(app, user.ID, code.OrganizationID, code.GrantedScopes, ids.New(), now)
	if err != nil {
		slog.Error("mint oauth refresh token", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := h.Repo.InsertThirdPartyOAuthRefreshToken(r.Context(), refreshRow); err != nil {
		slog.Error("insert oauth refresh token", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	writeJSON(w, http.StatusOK, models.OAuthTokenResponse{
		AccessToken:  access,
		TokenType:    oauthTokenTypeBearer,
		ExpiresIn:    expiresIn,
		RefreshToken: refreshPlaintext,
		Scope:        strings.Join(code.GrantedScopes, " "),
	})
}

func (h *RBAC) oauthRefreshToken(w http.ResponseWriter, r *http.Request, body models.OAuthTokenRequest) {
	refresh := strings.TrimSpace(body.RefreshToken)
	if refresh == "" {
		writeJSONErr(w, http.StatusBadRequest, "refresh_token is required")
		return
	}
	now := time.Now().UTC()
	row, err := h.Repo.FindThirdPartyOAuthRefreshToken(r.Context(), hashOAuthOpaqueToken(refresh))
	if err != nil {
		slog.Error("lookup oauth refresh token", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if row == nil || !row.ExpiresAt.After(now) {
		writeJSONErr(w, http.StatusUnauthorized, "invalid refresh_token")
		return
	}
	if row.RevokedAt != nil || row.UsedAt != nil {
		_ = h.Repo.RevokeThirdPartyOAuthRefreshTokenFamily(r.Context(), row.FamilyID, now)
		writeJSONErr(w, http.StatusUnauthorized, "invalid refresh_token")
		return
	}
	clientID := strings.TrimSpace(body.ClientID)
	if clientID == "" {
		clientID = row.ClientID
	}
	app, secretHash, err := h.Repo.GetThirdPartyApplicationByClientID(r.Context(), clientID)
	if err != nil {
		slog.Error("lookup oauth refresh client", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if app == nil || app.ID != row.ApplicationID || row.ClientID != app.ClientID {
		writeJSONErr(w, http.StatusUnauthorized, "invalid refresh_token")
		return
	}
	if err := validateOAuthClientAuthentication(app, secretHash, clientID, body.ClientSecret); err != nil {
		writeOAuthError(w, err)
		return
	}
	user, err := h.Repo.FindUserByID(r.Context(), row.SubjectUserID)
	if err != nil {
		slog.Error("lookup oauth refresh subject", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if user == nil || !user.IsActive || user.DeletedAt != nil {
		writeJSONErr(w, http.StatusUnauthorized, "subject user inactive")
		return
	}
	if _, ok := enabledThirdPartyApplicationForOrganization(app, row.OrganizationID); !ok {
		writeJSONErr(w, http.StatusForbidden, "application is not enabled for this organization")
		return
	}
	roles, permissions, err := h.Repo.ListUserSecuritySnapshot(r.Context(), user.ID)
	if err != nil {
		slog.Error("oauth refresh security snapshot", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	requested := parseOAuthScopes(body.Scope, body.Scopes)
	effectiveRequested := requested
	if len(effectiveRequested) == 0 {
		effectiveRequested = row.Scopes
	}
	if len(requested) == 0 {
		requested = row.Scopes
	}
	refreshCeiling := oauthScopeCeiling(row.Scopes, app.Scopes)
	granted, _ := deriveThirdPartyOAuthScopes(requested, refreshCeiling, roles, permissions)
	if len(effectiveRequested) > 0 && len(granted) == 0 {
		writeJSONErr(w, http.StatusForbidden, "requested refresh scopes exceed current token, application, or user permissions")
		return
	}
	access, expiresIn, err := h.OAuthIssuer.IssueAccessTokenForOAuthClient(user, app, granted, []string{"oauth_refresh_token"}, nil)
	if err != nil {
		slog.Error("issue oauth refresh access token", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusUnauthorized, "token issuance failed")
		return
	}
	refreshPlaintext, refreshRow, err := h.newOAuthRefreshToken(app, user.ID, row.OrganizationID, granted, row.FamilyID, now)
	if err != nil {
		slog.Error("mint rotated oauth refresh token", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	rotated, err := h.Repo.RotateThirdPartyOAuthRefreshToken(r.Context(), row.ID, refreshRow, now)
	if err != nil {
		slog.Error("rotate oauth refresh token", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if !rotated {
		_ = h.Repo.RevokeThirdPartyOAuthRefreshTokenFamily(r.Context(), row.FamilyID, now)
		writeJSONErr(w, http.StatusUnauthorized, "invalid refresh_token")
		return
	}
	writeJSON(w, http.StatusOK, models.OAuthTokenResponse{
		AccessToken:  access,
		TokenType:    oauthTokenTypeBearer,
		ExpiresIn:    expiresIn,
		RefreshToken: refreshPlaintext,
		Scope:        strings.Join(granted, " "),
	})
}

func (h *RBAC) oauthClientCredentialsToken(w http.ResponseWriter, r *http.Request, body models.OAuthTokenRequest) {
	app, secretHash, err := h.Repo.GetThirdPartyApplicationByClientID(r.Context(), strings.TrimSpace(body.ClientID))
	if err != nil {
		slog.Error("lookup oauth client_credentials client", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if err := validateOAuthClientAuthentication(app, secretHash, body.ClientID, body.ClientSecret); err != nil {
		writeOAuthError(w, err)
		return
	}
	if app.ClientType != models.ThirdPartyClientTypeConfidential || !containsThirdPartyString(app.EnabledGrantTypes, models.ThirdPartyGrantClientCredentials) {
		writeJSONErr(w, http.StatusBadRequest, "client is not enabled for client_credentials")
		return
	}
	if app.ServiceUserID == nil {
		writeJSONErr(w, http.StatusBadRequest, "client has no service user")
		return
	}
	orgID := app.ManagingOrganizationID
	if strings.TrimSpace(body.OrganizationID) != "" {
		parsed, err := uuid.Parse(strings.TrimSpace(body.OrganizationID))
		if err != nil {
			writeJSONErr(w, http.StatusBadRequest, "invalid organization_id")
			return
		}
		orgID = parsed
	}
	if _, ok := enabledThirdPartyApplicationForOrganization(app, orgID); !ok {
		writeJSONErr(w, http.StatusForbidden, "application is not enabled for this organization")
		return
	}
	serviceUser, err := h.Repo.FindUserByID(r.Context(), *app.ServiceUserID)
	if err != nil {
		slog.Error("lookup oauth service user", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	if serviceUser == nil || !serviceUser.IsActive || serviceUser.DeletedAt != nil {
		writeJSONErr(w, http.StatusUnauthorized, "service user inactive")
		return
	}
	roles, permissions, err := h.Repo.ListUserSecuritySnapshot(r.Context(), serviceUser.ID)
	if err != nil {
		slog.Error("oauth client_credentials security snapshot", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
		return
	}
	requested := parseOAuthScopes(body.Scope, body.Scopes)
	effectiveRequested := requested
	if len(effectiveRequested) == 0 {
		effectiveRequested = app.Scopes
	}
	granted, _ := deriveThirdPartyOAuthScopes(requested, app.Scopes, roles, permissions)
	if len(effectiveRequested) > 0 && len(granted) == 0 {
		writeJSONErr(w, http.StatusForbidden, "requested scopes exceed application or service-user permissions")
		return
	}
	subject := *serviceUser
	subject.OrganizationID = &orgID
	access, expiresIn, err := h.OAuthIssuer.IssueAccessTokenForOAuthClient(&subject, app, granted, []string{"oauth_client_credentials"}, nil)
	if err != nil {
		slog.Error("issue oauth client_credentials access token", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusUnauthorized, "token issuance failed")
		return
	}
	writeJSON(w, http.StatusOK, models.OAuthTokenResponse{
		AccessToken: access,
		TokenType:   oauthTokenTypeBearer,
		ExpiresIn:   expiresIn,
		Scope:       strings.Join(granted, " "),
	})
}

func (h *RBAC) prepareOAuthAuthorization(ctx context.Context, claims *authmw.Claims, req oauthAuthorizationRequest) (*oauthPreparedAuthorization, error) {
	req.ClientID = strings.TrimSpace(req.ClientID)
	req.RedirectURI = strings.TrimSpace(req.RedirectURI)
	req.ResponseType = strings.TrimSpace(req.ResponseType)
	req.State = strings.TrimSpace(req.State)
	req.CodeChallenge = strings.TrimSpace(req.CodeChallenge)
	req.CodeChallengeMethod = strings.TrimSpace(req.CodeChallengeMethod)
	if req.ResponseType == "" {
		req.ResponseType = "code"
	}
	if req.CodeChallengeMethod == "" {
		req.CodeChallengeMethod = oauthPKCEMethodS256
	}
	if req.ClientID == "" || req.RedirectURI == "" {
		return nil, oauthHTTPError{status: http.StatusBadRequest, message: "client_id and redirect_uri are required"}
	}
	if req.ResponseType != "code" {
		return nil, oauthHTTPError{status: http.StatusBadRequest, message: "response_type must be code"}
	}
	if req.State == "" {
		return nil, oauthHTTPError{status: http.StatusBadRequest, message: "state is required for CSRF protection"}
	}
	if req.CodeChallenge == "" || req.CodeChallengeMethod != oauthPKCEMethodS256 {
		return nil, oauthHTTPError{status: http.StatusBadRequest, message: "PKCE code_challenge with S256 is required"}
	}
	if len(req.Scopes) == 0 {
		return nil, oauthHTTPError{status: http.StatusBadRequest, message: "scope is required for authorization_code consent"}
	}
	user, err := h.Repo.FindUserByID(ctx, claims.Sub)
	if err != nil {
		return nil, err
	}
	if user == nil || !user.IsActive || user.DeletedAt != nil {
		return nil, oauthHTTPError{status: http.StatusUnauthorized, message: "subject user inactive"}
	}
	orgID, err := resolveOAuthAuthorizationOrganization(req.OrganizationID, claims, user)
	if err != nil {
		return nil, err
	}
	app, _, err := h.Repo.GetThirdPartyApplicationByClientID(ctx, req.ClientID)
	if err != nil {
		return nil, err
	}
	if app == nil {
		return nil, oauthHTTPError{status: http.StatusBadRequest, message: "unknown client_id"}
	}
	if !containsThirdPartyString(app.EnabledGrantTypes, models.ThirdPartyGrantAuthorizationCode) {
		return nil, oauthHTTPError{status: http.StatusBadRequest, message: "client is not enabled for authorization_code"}
	}
	if !containsThirdPartyString(app.RedirectURIs, req.RedirectURI) {
		return nil, oauthHTTPError{status: http.StatusBadRequest, message: "redirect_uri is not registered for client"}
	}
	enablement, ok := enabledThirdPartyApplicationForOrganization(app, orgID)
	if !ok {
		return nil, oauthHTTPError{status: http.StatusForbidden, message: "application is not enabled for this organization"}
	}
	roles, permissions, err := h.Repo.ListUserSecuritySnapshot(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	requested := req.Scopes
	effectiveRequested := requested
	if len(effectiveRequested) == 0 {
		effectiveRequested = app.Scopes
	}
	granted, missing := deriveThirdPartyOAuthScopes(requested, app.Scopes, roles, permissions)
	if len(effectiveRequested) > 0 && len(granted) == 0 {
		return nil, oauthHTTPError{status: http.StatusForbidden, message: "requested scopes exceed application or user permissions"}
	}
	return &oauthPreparedAuthorization{
		App:                 app,
		User:                user,
		OrganizationID:      orgID,
		Enablement:          enablement,
		RequestedScopes:     normalizeAPIKeyStringSet(requested),
		GrantedScopes:       granted,
		MissingScopes:       missing,
		CodeChallengeMethod: req.CodeChallengeMethod,
	}, nil
}

func (h *RBAC) newOAuthRefreshToken(app *models.ThirdPartyApplication, subjectUserID, organizationID uuid.UUID, scopes []string, familyID uuid.UUID, now time.Time) (string, *models.ThirdPartyOAuthRefreshToken, error) {
	plain, err := newOAuthRefreshTokenPlaintext()
	if err != nil {
		return "", nil, err
	}
	row := &models.ThirdPartyOAuthRefreshToken{
		ID:             ids.New(),
		TokenHash:      hashOAuthOpaqueToken(plain),
		FamilyID:       familyID,
		ApplicationID:  app.ID,
		ClientID:       app.ClientID,
		SubjectUserID:  subjectUserID,
		OrganizationID: organizationID,
		Scopes:         normalizeAPIKeyStringSet(scopes),
		IssuedAt:       now,
		ExpiresAt:      now.Add(h.OAuthIssuer.RefreshTokenTTL()),
	}
	return plain, row, nil
}

func oauthAuthorizationRequestFromQuery(values url.Values) (oauthAuthorizationRequest, error) {
	var orgID *uuid.UUID
	if raw := strings.TrimSpace(values.Get("organization_id")); raw != "" {
		parsed, err := uuid.Parse(raw)
		if err != nil {
			return oauthAuthorizationRequest{}, fmt.Errorf("invalid organization_id")
		}
		orgID = &parsed
	}
	return oauthAuthorizationRequest{
		ClientID:            values.Get("client_id"),
		RedirectURI:         values.Get("redirect_uri"),
		ResponseType:        values.Get("response_type"),
		Scopes:              parseOAuthScopes(values.Get("scope"), nil),
		State:               values.Get("state"),
		CodeChallenge:       values.Get("code_challenge"),
		CodeChallengeMethod: values.Get("code_challenge_method"),
		OrganizationID:      orgID,
	}, nil
}

func oauthAuthorizationRequestFromConsent(body models.OAuthConsentRequest) oauthAuthorizationRequest {
	var orgID *uuid.UUID
	if body.OrganizationID != uuid.Nil {
		orgID = &body.OrganizationID
	}
	return oauthAuthorizationRequest{
		ClientID:            body.ClientID,
		RedirectURI:         body.RedirectURI,
		ResponseType:        body.ResponseType,
		Scopes:              parseOAuthScopes(body.Scope, body.Scopes),
		State:               body.State,
		CodeChallenge:       body.CodeChallenge,
		CodeChallengeMethod: body.CodeChallengeMethod,
		OrganizationID:      orgID,
		Approve:             body.Approve,
	}
}

func decodeOAuthTokenRequest(r *http.Request) (models.OAuthTokenRequest, error) {
	var body models.OAuthTokenRequest
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return body, fmt.Errorf("invalid body")
		}
		return body, nil
	}
	if err := r.ParseForm(); err != nil {
		return body, fmt.Errorf("invalid form body")
	}
	body.GrantType = r.PostForm.Get("grant_type")
	body.Code = r.PostForm.Get("code")
	body.RedirectURI = r.PostForm.Get("redirect_uri")
	body.ClientID = r.PostForm.Get("client_id")
	body.ClientSecret = r.PostForm.Get("client_secret")
	body.CodeVerifier = r.PostForm.Get("code_verifier")
	body.RefreshToken = r.PostForm.Get("refresh_token")
	body.Scope = r.PostForm.Get("scope")
	body.OrganizationID = r.PostForm.Get("organization_id")
	return body, nil
}

func decodeOAuthRevokeRequest(r *http.Request) (models.OAuthRevokeRequest, error) {
	var body models.OAuthRevokeRequest
	if strings.Contains(r.Header.Get("Content-Type"), "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return body, fmt.Errorf("invalid body")
		}
		return body, nil
	}
	if err := r.ParseForm(); err != nil {
		return body, fmt.Errorf("invalid form body")
	}
	body.Token = r.PostForm.Get("token")
	body.TokenTypeHint = r.PostForm.Get("token_type_hint")
	body.ClientID = r.PostForm.Get("client_id")
	body.ClientSecret = r.PostForm.Get("client_secret")
	return body, nil
}

func applyOAuthBasicAuth(r *http.Request, body *models.OAuthTokenRequest) {
	if user, pass, ok := r.BasicAuth(); ok {
		if body.ClientID == "" {
			body.ClientID = user
		}
		if body.ClientSecret == "" {
			body.ClientSecret = pass
		}
	}
}

func applyOAuthBasicAuthToRevoke(r *http.Request, body *models.OAuthRevokeRequest) {
	if user, pass, ok := r.BasicAuth(); ok {
		if body.ClientID == "" {
			body.ClientID = user
		}
		if body.ClientSecret == "" {
			body.ClientSecret = pass
		}
	}
}

func validateOAuthClientAuthentication(app *models.ThirdPartyApplication, secretHash *string, clientID, clientSecret string) error {
	clientID = strings.TrimSpace(clientID)
	if app == nil || clientID == "" || clientID != app.ClientID {
		return oauthHTTPError{status: http.StatusUnauthorized, message: "invalid client"}
	}
	if app.ClientType == models.ThirdPartyClientTypePublic {
		return nil
	}
	if strings.TrimSpace(clientSecret) == "" || secretHash == nil {
		return oauthHTTPError{status: http.StatusUnauthorized, message: "client authentication required"}
	}
	if subtle.ConstantTimeCompare([]byte(hashOAuthClientSecret(clientSecret)), []byte(*secretHash)) != 1 {
		return oauthHTTPError{status: http.StatusUnauthorized, message: "invalid client secret"}
	}
	return nil
}

func resolveOAuthAuthorizationOrganization(requested *uuid.UUID, claims *authmw.Claims, user *models.User) (uuid.UUID, error) {
	var userOrg *uuid.UUID
	if claims != nil && claims.OrgID != nil {
		userOrg = claims.OrgID
	}
	if userOrg == nil && user != nil {
		userOrg = user.OrganizationID
	}
	if userOrg == nil || *userOrg == uuid.Nil {
		return uuid.Nil, oauthHTTPError{status: http.StatusBadRequest, message: "organization_id is required for OAuth authorization"}
	}
	if requested != nil && *requested != uuid.Nil && *requested != *userOrg {
		return uuid.Nil, oauthHTTPError{status: http.StatusForbidden, message: "users can only authorize applications for their active organization"}
	}
	return *userOrg, nil
}

func enabledThirdPartyApplicationForOrganization(app *models.ThirdPartyApplication, organizationID uuid.UUID) (models.ThirdPartyApplicationEnablement, bool) {
	if app == nil || organizationID == uuid.Nil {
		return models.ThirdPartyApplicationEnablement{}, false
	}
	for _, enablement := range app.Enablements {
		if enablement.OrganizationID == organizationID && enablement.Enabled {
			return enablement, true
		}
	}
	return models.ThirdPartyApplicationEnablement{}, false
}

func deriveThirdPartyOAuthScopes(requested, appScopes, roles, permissions []string) ([]string, []string) {
	requested = normalizeAPIKeyStringSet(requested)
	appScopes = normalizeAPIKeyStringSet(appScopes)
	roles = normalizeAPIKeyStringSet(roles)
	permissions = normalizeAPIKeyStringSet(permissions)
	if len(requested) == 0 {
		if len(appScopes) > 0 {
			requested = appScopes
		} else if containsString(roles, "admin") || containsString(permissions, "*:*") {
			requested = []string{"*:*"}
		} else {
			requested = permissions
		}
	}
	granted := make([]string, 0, len(requested))
	missing := make([]string, 0)
	for _, scope := range requested {
		if len(appScopes) > 0 && !scopeAllowedByPermissions(scope, appScopes) {
			missing = append(missing, scope)
			continue
		}
		if containsString(roles, "admin") || scopeAllowedByPermissions(scope, permissions) {
			granted = append(granted, scope)
			continue
		}
		missing = append(missing, scope)
	}
	return normalizeAPIKeyStringSet(granted), normalizeAPIKeyStringSet(missing)
}

func oauthScopeCeiling(first, second []string) []string {
	first = normalizeAPIKeyStringSet(first)
	second = normalizeAPIKeyStringSet(second)
	if len(first) == 0 {
		return nil
	}
	if len(second) == 0 {
		return first
	}
	out := make([]string, 0, len(first))
	for _, scope := range first {
		if scopeAllowedByPermissions(scope, second) {
			out = append(out, scope)
		}
	}
	return normalizeAPIKeyStringSet(out)
}

func parseOAuthScopes(scope string, scopes []string) []string {
	out := make([]string, 0, len(scopes)+4)
	for _, value := range scopes {
		for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == ' ' || r == ',' || r == '\n' || r == '\t' }) {
			if strings.TrimSpace(part) != "" {
				out = append(out, strings.TrimSpace(part))
			}
		}
	}
	for _, part := range strings.FieldsFunc(scope, func(r rune) bool { return r == ' ' || r == ',' || r == '\n' || r == '\t' }) {
		if strings.TrimSpace(part) != "" {
			out = append(out, strings.TrimSpace(part))
		}
	}
	return normalizeAPIKeyStringSet(out)
}

func verifyOAuthPKCE(method, challenge, verifier string) bool {
	if method != oauthPKCEMethodS256 || challenge == "" || verifier == "" {
		return false
	}
	sum := sha256.Sum256([]byte(verifier))
	expected := base64.RawURLEncoding.EncodeToString(sum[:])
	return subtle.ConstantTimeCompare([]byte(expected), []byte(challenge)) == 1
}

func oauthRedirectWithParams(raw string, params map[string]string) string {
	parsed, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	query := parsed.Query()
	for key, value := range params {
		if value != "" {
			query.Set(key, value)
		}
	}
	parsed.RawQuery = query.Encode()
	return parsed.String()
}

func newOAuthAuthorizationCodePlaintext() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "ofoauth_code_" + base64.RawURLEncoding.EncodeToString(buf), nil
}

func newOAuthRefreshTokenPlaintext() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "ofoauth_rt_" + base64.RawURLEncoding.EncodeToString(buf), nil
}

func hashOAuthOpaqueToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func writeOAuthError(w http.ResponseWriter, err error) {
	if err == nil {
		return
	}
	var httpErr oauthHTTPError
	if ok := errorAsOAuthHTTPError(err, &httpErr); ok {
		writeJSONErr(w, httpErr.status, httpErr.message)
		return
	}
	slog.Error("oauth handler", slog.String("error", err.Error()))
	writeJSONErr(w, http.StatusInternalServerError, "internal error")
}

func errorAsOAuthHTTPError(err error, target *oauthHTTPError) bool {
	if err == nil {
		return false
	}
	if value, ok := err.(oauthHTTPError); ok {
		*target = value
		return true
	}
	return false
}
