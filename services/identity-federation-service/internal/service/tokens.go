package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/core-models/ids"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/repo"
)

// Issuer composes JWT issuance + refresh-token persistence.
type Issuer struct {
	JWT        *authmw.JWTConfig
	Repo       *repo.Repo
	AccessTTL  time.Duration
	RefreshTTL time.Duration
}

// AccessTokenTTL exposes the access-token TTL through the
// handlers.TokenIssuer interface so handlers can stay decoupled from
// the concrete struct.
func (i *Issuer) AccessTokenTTL() time.Duration { return i.AccessTTL }

// RefreshTokenTTL exposes the configured refresh-token lifetime to
// OAuth grant handlers that persist their own refresh-token table.
func (i *Issuer) RefreshTokenTTL() time.Duration { return i.RefreshTTL }

// IssueTokens creates an access JWT + refresh token for a user.
//
// `authMethods` lists the methods that authenticated this session
// (e.g. ["password"], ["password","totp"], ["webauthn"]); they end up
// in the JWT's auth_methods claim and downstream services use them
// for step-up auth checks.
//
// Returns (accessJWT, refreshTokenPlaintext) — the refresh plaintext
// is delivered to the client; only its SHA-256 digest is persisted.
func (i *Issuer) IssueTokens(ctx context.Context, user *models.User, authMethods []string) (string, string, error) {
	return i.IssueTokensWithScope(ctx, user, authMethods, nil)
}

// IssueTokensWithScope creates an access JWT + refresh token and binds
// the optional session scope to both so refresh keeps the same active
// marking subset.
func (i *Issuer) IssueTokensWithScope(ctx context.Context, user *models.User, authMethods []string, scope *authmw.SessionScope) (string, string, error) {
	now := time.Now()

	access := &authmw.Claims{
		Sub:         user.ID,
		IAT:         now.Unix(),
		EXP:         now.Add(i.AccessTTL).Unix(),
		ISS:         maybe(i.JWT.Issuer),
		AUD:         maybe(i.JWT.Audience),
		JTI:         ids.New(),
		Email:       user.Email,
		Name:        user.Name,
		Roles:       []string{}, // slice 6 fills these in
		Permissions: []string{},
		OrgID:       user.OrganizationID,
		Attributes:  user.Attributes,
		AuthMethods: authMethods,
		TokenUse:    strPtr("access"),
	}
	if scope != nil {
		access.SessionKind = strPtr("scoped_session")
		access.SessionScope = cloneSessionScope(scope)
	}
	if access.Attributes == nil {
		access.Attributes = json.RawMessage(`{}`)
	}

	accessTok, err := authmw.EncodeToken(i.JWT, access)
	if err != nil {
		return "", "", fmt.Errorf("encode access token: %w", err)
	}

	plaintext, err := NewRefreshTokenPlaintext()
	if err != nil {
		return "", "", fmt.Errorf("mint refresh token: %w", err)
	}
	scopeRaw, err := marshalSessionScope(scope)
	if err != nil {
		return "", "", err
	}
	row := &models.RefreshTokenRow{
		ID:           ids.New(),
		UserID:       user.ID,
		TokenHash:    HashRefreshToken(plaintext),
		FamilyID:     ids.New(),
		SessionScope: scopeRaw,
		IssuedAt:     now,
		ExpiresAt:    now.Add(i.RefreshTTL),
	}
	if err := i.Repo.InsertRefreshToken(ctx, row); err != nil {
		return "", "", fmt.Errorf("insert refresh token: %w", err)
	}
	return accessTok, plaintext, nil
}

// RefreshTokens consumes a refresh token, marks it used, and issues a
// new (access, refresh) pair under the SAME family id. Replay
// detection: if the presented token is already revoked, the entire
// family is killed (mirrors the Rust hardening/refresh_family.rs
// behaviour).
//
// Returns ErrRefreshTokenInvalid when the token cannot be exchanged
// (unknown / expired). Replay → ErrRefreshTokenReused (and the family
// is revoked as a side effect).
func (i *Issuer) RefreshTokens(ctx context.Context, plaintext string) (string, string, error) {
	row, err := i.Repo.FindRefreshToken(ctx, HashRefreshToken(plaintext))
	if err != nil {
		return "", "", fmt.Errorf("lookup refresh token: %w", err)
	}
	if row == nil {
		return "", "", ErrRefreshTokenInvalid
	}
	now := time.Now()
	if row.ExpiresAt.Before(now) {
		return "", "", ErrRefreshTokenInvalid
	}
	if row.RevokedAt != nil {
		// Replay — kill the whole family.
		_ = i.Repo.RevokeRefreshFamily(ctx, row.FamilyID, now)
		return "", "", ErrRefreshTokenReused
	}

	user, err := i.Repo.FindUserByID(ctx, row.UserID)
	if err != nil {
		return "", "", fmt.Errorf("lookup user: %w", err)
	}
	if user == nil || !user.IsActive {
		return "", "", ErrRefreshTokenInvalid
	}

	if err := i.Repo.MarkRefreshUsed(ctx, row.ID, now); err != nil {
		return "", "", fmt.Errorf("mark used: %w", err)
	}

	scope, err := unmarshalSessionScope(row.SessionScope)
	if err != nil {
		return "", "", err
	}
	access, err := i.encodeAccessForUser(user, []string{"refresh_token"}, scope)
	if err != nil {
		return "", "", err
	}
	plaintextNew, err := NewRefreshTokenPlaintext()
	if err != nil {
		return "", "", fmt.Errorf("mint refresh token: %w", err)
	}
	newRow := &models.RefreshTokenRow{
		ID:           ids.New(),
		UserID:       user.ID,
		TokenHash:    HashRefreshToken(plaintextNew),
		FamilyID:     row.FamilyID, // SAME family — replay detection works
		SessionScope: row.SessionScope,
		IssuedAt:     now,
		ExpiresAt:    now.Add(i.RefreshTTL),
	}
	if err := i.Repo.InsertRefreshToken(ctx, newRow); err != nil {
		return "", "", fmt.Errorf("insert refresh token: %w", err)
	}
	return access, plaintextNew, nil
}

func (i *Issuer) encodeAccessForUser(user *models.User, authMethods []string, scope *authmw.SessionScope) (string, error) {
	now := time.Now()
	c := &authmw.Claims{
		Sub:         user.ID,
		IAT:         now.Unix(),
		EXP:         now.Add(i.AccessTTL).Unix(),
		ISS:         maybe(i.JWT.Issuer),
		AUD:         maybe(i.JWT.Audience),
		JTI:         ids.New(),
		Email:       user.Email,
		Name:        user.Name,
		Roles:       []string{},
		Permissions: []string{},
		OrgID:       user.OrganizationID,
		Attributes:  user.Attributes,
		AuthMethods: authMethods,
		TokenUse:    strPtr("access"),
	}
	if scope != nil {
		c.SessionKind = strPtr("scoped_session")
		c.SessionScope = cloneSessionScope(scope)
	}
	if c.Attributes == nil {
		c.Attributes = json.RawMessage(`{}`)
	}
	return authmw.EncodeToken(i.JWT, c)
}

// IssueAccessTokenForAPIKey exchanges a usable developer API key for
// a normal access JWT. Downstream services can keep requiring
// token_use="access", while the api_key_id/auth_methods claims retain
// the audit trail back to the revocable opaque key.
func (i *Issuer) IssueAccessTokenForAPIKey(user *models.User, key *models.APIKey) (string, int64, error) {
	now := time.Now().UTC()
	exp := now.Add(i.AccessTTL)
	if key.ExpiresAt != nil && key.ExpiresAt.Before(exp) {
		exp = *key.ExpiresAt
	}
	if !exp.After(now) {
		return "", 0, ErrRefreshTokenInvalid
	}
	apiKeyID := key.ID
	tokenUse := "access"
	c := &authmw.Claims{
		Sub:         user.ID,
		IAT:         now.Unix(),
		EXP:         exp.Unix(),
		ISS:         maybe(i.JWT.Issuer),
		AUD:         maybe(i.JWT.Audience),
		JTI:         uuid.New(),
		Email:       user.Email,
		Name:        user.Name,
		Roles:       append([]string(nil), key.RolesSnapshot...),
		Permissions: append([]string(nil), key.PermissionsSnapshot...),
		OrgID:       user.OrganizationID,
		Attributes:  user.Attributes,
		AuthMethods: []string{"api_key"},
		TokenUse:    &tokenUse,
		APIKeyID:    &apiKeyID,
	}
	if c.Attributes == nil {
		c.Attributes = json.RawMessage(`{}`)
	}
	encoded, err := authmw.EncodeToken(i.JWT, c)
	if err != nil {
		return "", 0, fmt.Errorf("encode api key access token: %w", err)
	}
	return encoded, int64(exp.Sub(now).Seconds()), nil
}

// IssueAccessTokenForOAuthClient issues the access JWT returned from
// third-party OAuth grants. Roles are intentionally empty: downstream
// authorization should observe only the narrowed OAuth scope set in
// Permissions, which has already been intersected with the
// user/service account's permissions, the application's max scopes,
// and the request scope.
func (i *Issuer) IssueAccessTokenForOAuthClient(user *models.User, app *models.ThirdPartyApplication, scopes []string, authMethods []string, maxExpiry *time.Time) (string, int64, error) {
	now := time.Now().UTC()
	exp := now.Add(i.AccessTTL)
	if maxExpiry != nil && maxExpiry.Before(exp) {
		exp = *maxExpiry
	}
	if !exp.After(now) {
		return "", 0, ErrRefreshTokenInvalid
	}
	tokenUse := "access"
	sessionKind := "oauth_third_party_application"
	c := &authmw.Claims{
		Sub:         user.ID,
		IAT:         now.Unix(),
		EXP:         exp.Unix(),
		ISS:         maybe(i.JWT.Issuer),
		AUD:         maybe(i.JWT.Audience),
		JTI:         uuid.New(),
		Email:       user.Email,
		Name:        user.Name,
		Roles:       []string{},
		Permissions: append([]string(nil), scopes...),
		OrgID:       user.OrganizationID,
		Attributes:  oauthTokenAttributes(user.Attributes, app, scopes),
		AuthMethods: append([]string(nil), authMethods...),
		TokenUse:    &tokenUse,
		SessionKind: &sessionKind,
	}
	encoded, err := authmw.EncodeToken(i.JWT, c)
	if err != nil {
		return "", 0, fmt.Errorf("encode oauth access token: %w", err)
	}
	return encoded, int64(exp.Sub(now).Seconds()), nil
}

func marshalSessionScope(scope *authmw.SessionScope) (json.RawMessage, error) {
	if scope == nil {
		return nil, nil
	}
	raw, err := json.Marshal(scope)
	if err != nil {
		return nil, fmt.Errorf("marshal session scope: %w", err)
	}
	return json.RawMessage(raw), nil
}

func unmarshalSessionScope(raw json.RawMessage) (*authmw.SessionScope, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var scope authmw.SessionScope
	if err := json.Unmarshal(raw, &scope); err != nil {
		return nil, fmt.Errorf("decode refresh token session scope: %w", err)
	}
	return &scope, nil
}

func cloneSessionScope(scope *authmw.SessionScope) *authmw.SessionScope {
	if scope == nil {
		return nil
	}
	cp := *scope
	cp.AllowedMethods = append([]string(nil), scope.AllowedMethods...)
	cp.AllowedPathPrefixes = append([]string(nil), scope.AllowedPathPrefixes...)
	cp.AllowedSubjectIDs = append([]string(nil), scope.AllowedSubjectIDs...)
	cp.AllowedOrgIDs = append([]uuid.UUID(nil), scope.AllowedOrgIDs...)
	cp.AllowedMarkings = append([]string(nil), scope.AllowedMarkings...)
	cp.RestrictedViewIDs = append([]uuid.UUID(nil), scope.RestrictedViewIDs...)
	return &cp
}

func maybe(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func strPtr(s string) *string { return &s }

func oauthTokenAttributes(raw json.RawMessage, app *models.ThirdPartyApplication, scopes []string) json.RawMessage {
	attrs := make(map[string]any)
	if len(raw) > 0 && string(raw) != "null" {
		_ = json.Unmarshal(raw, &attrs)
	}
	if attrs == nil {
		attrs = make(map[string]any)
	}
	if app != nil {
		attrs["oauth_client_id"] = app.ClientID
		attrs["third_party_application_id"] = app.ID.String()
	}
	attrs["oauth_scopes"] = append([]string(nil), scopes...)
	out, err := json.Marshal(attrs)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return out
}

// Sentinel errors returned by RefreshTokens.
type sentinel string

func (s sentinel) Error() string { return string(s) }

const (
	ErrRefreshTokenInvalid sentinel = "refresh token invalid"
	ErrRefreshTokenReused  sentinel = "refresh token reuse detected"
)

// uuidT keeps go vet happy when uuid isn't used in a particular file.
var _ = uuid.Nil
