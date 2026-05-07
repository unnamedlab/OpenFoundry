package auth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"

	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/audit"
)

// OAuthClientValidator is the contract the `client_credentials` branch
// uses to verify caller-supplied credentials. The production
// implementation hits `oauth-integration-service`; tests substitute
// fakes that always allow / always deny.
type OAuthClientValidator interface {
	ValidateClientCredentials(ctx context.Context, clientID, clientSecret, scope string) error
}

// HTTPClientValidator delegates credential checks to
// `oauth-integration-service`'s `POST /v1/oauth-clients/validate`.
// Non-2xx responses are mapped to ErrForbidden so the surfaced 4xx is
// stable for clients.
type HTTPClientValidator struct {
	BaseURL string
	HTTP    *http.Client
}

// ValidateClientCredentials posts the credentials to the integration
// service. Network errors surface as plain errors so the HTTP layer
// returns 5xx (matching the Rust 503 retry contract).
func (v *HTTPClientValidator) ValidateClientCredentials(ctx context.Context, clientID, clientSecret, scope string) error {
	if v == nil {
		return errors.New("oauth validator not configured")
	}
	httpClient := v.HTTP
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	url := strings.TrimRight(v.BaseURL, "/") + "/v1/oauth-clients/validate"
	body, err := json.Marshal(map[string]any{
		"client_id":     clientID,
		"client_secret": clientSecret,
		"scope":         scope,
	})
	if err != nil {
		return fmt.Errorf("marshal validate body: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build validate request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("oauth validation request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return &ErrForbidden{Message: fmt.Sprintf("oauth client credentials rejected (status %d)", resp.StatusCode)}
	}
	return nil
}

// OAuthTokenForm is the spec-compliant request body. Both
// `application/x-www-form-urlencoded` and `application/json` payloads
// decode into this shape.
type OAuthTokenForm struct {
	GrantType    string `json:"grant_type"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Scope        string `json:"scope"`
	RefreshToken string `json:"refresh_token"`
}

// OAuthTokenResponse mirrors the spec's TokenResponse JSON shape.
type OAuthTokenResponse struct {
	AccessToken     string `json:"access_token"`
	TokenType       string `json:"token_type"`
	ExpiresIn       int64  `json:"expires_in"`
	IssuedTokenType string `json:"issued_token_type"`
	Scope           string `json:"scope"`
	RefreshToken    string `json:"refresh_token,omitempty"`
}

// IssueTokenHandler returns the HTTP handler for
// `POST /iceberg/v1/oauth/tokens`. Branches on `grant_type` per the
// REST Catalog § Authentication spec.
func IssueTokenHandler(cfg *Config, validator OAuthClientValidator) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		form, err := decodeOAuthForm(r)
		if err != nil {
			writeJSONErr(w, http.StatusBadRequest, err.Error())
			return
		}
		switch form.GrantType {
		case "client_credentials":
			handleClientCredentials(w, r, cfg, validator, form)
		case "refresh_token":
			handleRefreshToken(w, r, cfg, form)
		default:
			writeJSONErr(w, http.StatusBadRequest, fmt.Sprintf("unsupported grant_type `%s`", form.GrantType))
		}
	}
}

func handleClientCredentials(w http.ResponseWriter, r *http.Request, cfg *Config, validator OAuthClientValidator, form OAuthTokenForm) {
	clientID, clientSecret, ok := resolveClientCredentials(r.Header.Get("Authorization"), form)
	if !ok {
		writeJSONErr(w, http.StatusBadRequest, "client_id and client_secret required (form or HTTP Basic)")
		return
	}
	if validator != nil {
		if err := validator.ValidateClientCredentials(r.Context(), clientID, clientSecret, form.Scope); err != nil {
			var forbid *ErrForbidden
			if errors.As(err, &forbid) {
				writeJSONErr(w, http.StatusForbidden, err.Error())
				return
			}
			writeJSONErr(w, http.StatusBadGateway, err.Error())
			return
		}
	}
	scope := form.Scope
	if scope == "" {
		scope = "api:iceberg-read api:iceberg-write"
	}
	scopeList := strings.Fields(scope)
	access, err := IssueInternalJWT(cfg, clientID, cfg.JWTIssuer, cfg.JWTAudience, scopeList, cfg.DefaultTokenTTLSecs)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	refresh, err := IssueInternalJWT(cfg, clientID, cfg.JWTIssuer, cfg.JWTAudience, scopeList, cfg.DefaultTokenTTLSecs*24)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	audit.OAuthTokenIssued(nil, "client_credentials", scope)
	writeJSON(w, http.StatusOK, OAuthTokenResponse{
		AccessToken:     access,
		TokenType:       "bearer",
		ExpiresIn:       cfg.DefaultTokenTTLSecs,
		IssuedTokenType: "urn:ietf:params:oauth:token-type:access_token",
		Scope:           scope,
		RefreshToken:    refresh,
	})
}

func handleRefreshToken(w http.ResponseWriter, r *http.Request, cfg *Config, form OAuthTokenForm) {
	if form.RefreshToken == "" {
		writeJSONErr(w, http.StatusBadRequest, "refresh_token is required")
		return
	}
	claims, err := decodeRefreshToken(form.RefreshToken, cfg)
	if err != nil {
		writeJSONErr(w, http.StatusUnauthorized, err.Error())
		return
	}
	scopeList := strings.Fields(claims.Scp)
	access, err := IssueInternalJWT(cfg, claims.Sub, cfg.JWTIssuer, cfg.JWTAudience, scopeList, cfg.DefaultTokenTTLSecs)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	audit.OAuthTokenIssued(nil, "refresh_token", claims.Scp)
	writeJSON(w, http.StatusOK, OAuthTokenResponse{
		AccessToken:     access,
		TokenType:       "bearer",
		ExpiresIn:       cfg.DefaultTokenTTLSecs,
		IssuedTokenType: "urn:ietf:params:oauth:token-type:access_token",
		Scope:           claims.Scp,
	})
	_ = r // ctx-style placeholder: handler doesn't propagate ctx into JWT decode.
}

func decodeOAuthForm(r *http.Request) (OAuthTokenForm, error) {
	contentType := strings.ToLower(strings.TrimSpace(strings.SplitN(r.Header.Get("Content-Type"), ";", 2)[0]))
	if contentType == "application/json" {
		var f OAuthTokenForm
		body, err := io.ReadAll(r.Body)
		if err != nil {
			return f, fmt.Errorf("read body: %w", err)
		}
		if len(bytes.TrimSpace(body)) == 0 {
			return f, errors.New("empty body")
		}
		if err := json.Unmarshal(body, &f); err != nil {
			return f, fmt.Errorf("invalid json body: %w", err)
		}
		return f, nil
	}
	if err := r.ParseForm(); err != nil {
		return OAuthTokenForm{}, fmt.Errorf("invalid form body: %w", err)
	}
	return OAuthTokenForm{
		GrantType:    r.PostForm.Get("grant_type"),
		ClientID:     r.PostForm.Get("client_id"),
		ClientSecret: r.PostForm.Get("client_secret"),
		Scope:        r.PostForm.Get("scope"),
		RefreshToken: r.PostForm.Get("refresh_token"),
	}, nil
}

func resolveClientCredentials(authHeader string, form OAuthTokenForm) (string, string, bool) {
	if form.ClientID != "" && form.ClientSecret != "" {
		return form.ClientID, form.ClientSecret, true
	}
	if v, ok := strings.CutPrefix(authHeader, "Basic "); ok {
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(v))
		if err == nil {
			parts := strings.SplitN(string(decoded), ":", 2)
			if len(parts) == 2 {
				return parts[0], parts[1], true
			}
		}
	}
	return "", "", false
}

// decodeRefreshToken decodes the refresh JWT. We don't validate audience
// here: refresh tokens are minted by us, and PyIceberg sometimes calls
// the endpoint cross-environment (which would otherwise mismatch).
func decodeRefreshToken(token string, cfg *Config) (*IcebergClaims, error) {
	parser := jwt.NewParser(jwt.WithValidMethods([]string{"HS256"}))
	claims := &IcebergClaims{}
	parsed, err := parser.ParseWithClaims(token, claims, func(t *jwt.Token) (any, error) {
		return cfg.Secret, nil
	})
	if err != nil {
		return nil, fmt.Errorf("refresh token rejected: %w", err)
	}
	if !parsed.Valid {
		return nil, errors.New("refresh token not valid")
	}
	return claims, nil
}
