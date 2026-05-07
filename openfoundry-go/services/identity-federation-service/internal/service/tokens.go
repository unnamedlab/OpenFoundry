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
	row := &models.RefreshTokenRow{
		ID:        ids.New(),
		UserID:    user.ID,
		TokenHash: HashRefreshToken(plaintext),
		FamilyID:  ids.New(),
		IssuedAt:  now,
		ExpiresAt: now.Add(i.RefreshTTL),
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

	access, err := i.encodeAccessForUser(user, []string{"refresh_token"})
	if err != nil {
		return "", "", err
	}
	plaintextNew, err := NewRefreshTokenPlaintext()
	if err != nil {
		return "", "", fmt.Errorf("mint refresh token: %w", err)
	}
	newRow := &models.RefreshTokenRow{
		ID:        ids.New(),
		UserID:    user.ID,
		TokenHash: HashRefreshToken(plaintextNew),
		FamilyID:  row.FamilyID, // SAME family — replay detection works
		IssuedAt:  now,
		ExpiresAt: now.Add(i.RefreshTTL),
	}
	if err := i.Repo.InsertRefreshToken(ctx, newRow); err != nil {
		return "", "", fmt.Errorf("insert refresh token: %w", err)
	}
	return access, plaintextNew, nil
}

func (i *Issuer) encodeAccessForUser(user *models.User, authMethods []string) (string, error) {
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
	if c.Attributes == nil {
		c.Attributes = json.RawMessage(`{}`)
	}
	return authmw.EncodeToken(i.JWT, c)
}

func maybe(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func strPtr(s string) *string { return &s }

// Sentinel errors returned by RefreshTokens.
type sentinel string

func (s sentinel) Error() string { return string(s) }

const (
	ErrRefreshTokenInvalid sentinel = "refresh token invalid"
	ErrRefreshTokenReused  sentinel = "refresh token reuse detected"
)

// uuidT keeps go vet happy when uuid isn't used in a particular file.
var _ = uuid.Nil
