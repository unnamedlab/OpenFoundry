package authmw

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// AccessClaimsInput is the parameter bag for [BuildAccessClaims].
//
// Mirrors libs/auth-middleware/src/jwt.rs::build_access_claims with
// every argument promoted to a struct field — keeps the call sites
// readable in Go where positional 9-arg calls would be brittle.
type AccessClaimsInput struct {
	UserID      uuid.UUID
	Email       string
	Name        string
	Roles       []string
	Permissions []string
	OrgID       *uuid.UUID
	Attributes  json.RawMessage
	AuthMethods []string
}

// BuildAccessClaims builds an access-token Claims set with the
// canonical TokenUse = "access" and the config's access TTL.
func BuildAccessClaims(cfg *JWTConfig, in AccessClaimsInput) Claims {
	return BuildAccessClaimsWithScope(cfg, in, nil, stringPtr("access"))
}

// BuildAccessClaimsWithScope is the scoped variant of
// [BuildAccessClaims] — additionally sets SessionScope and
// SessionKind, mirroring the Rust function of the same name.
//
// The TokenUse field is always populated with "access" regardless
// of the sessionKind argument (Rust hard-codes this). The
// sessionKind argument therefore controls SessionKind only.
func BuildAccessClaimsWithScope(
	cfg *JWTConfig,
	in AccessClaimsInput,
	sessionScope *SessionScope,
	sessionKind *string,
) Claims {
	now := time.Now().UTC().Unix()
	exp := now + int64(cfg.AccessTTL/time.Second)
	access := "access"
	return baseClaims(
		cfg, in.UserID, now, exp,
		in.Email, in.Name, in.Roles, in.Permissions,
		in.OrgID, in.Attributes, in.AuthMethods,
		&access, nil, sessionKind, sessionScope,
	)
}

// BuildRefreshClaims builds a minimal Claims set for a refresh
// token (empty email/name/roles, the config's refresh TTL,
// TokenUse = "refresh").
func BuildRefreshClaims(cfg *JWTConfig, userID uuid.UUID) Claims {
	now := time.Now().UTC().Unix()
	exp := now + int64(cfg.RefreshTTL/time.Second)
	refresh := "refresh"
	return baseClaims(
		cfg, userID, now, exp,
		"", "", nil, nil,
		nil, json.RawMessage(`{}`), nil,
		&refresh, nil, nil, nil,
	)
}

// APIKeyClaimsInput is the parameter bag for [BuildAPIKeyClaims].
type APIKeyClaimsInput struct {
	UserID      uuid.UUID
	Email       string
	Name        string
	Roles       []string
	Permissions []string
	OrgID       *uuid.UUID
	Attributes  json.RawMessage
	APIKeyID    uuid.UUID
	// ExpiresIn is the time until the key expires. The Rust
	// signature takes raw seconds; Go uses time.Duration for
	// type-safety but the wire effect is identical.
	ExpiresIn time.Duration
}

// BuildAPIKeyClaims builds claims for a long-lived API key. The
// TokenUse and AuthMethods are forced to "api_key".
func BuildAPIKeyClaims(cfg *JWTConfig, in APIKeyClaimsInput) Claims {
	return BuildAPIKeyClaimsWithScope(cfg, in, nil, nil)
}

// BuildAPIKeyClaimsWithScope is the scoped variant of
// [BuildAPIKeyClaims].
func BuildAPIKeyClaimsWithScope(
	cfg *JWTConfig,
	in APIKeyClaimsInput,
	sessionScope *SessionScope,
	sessionKind *string,
) Claims {
	now := time.Now().UTC().Unix()
	exp := now + int64(in.ExpiresIn/time.Second)
	apiKey := "api_key"
	apiKeyID := in.APIKeyID
	return baseClaims(
		cfg, in.UserID, now, exp,
		in.Email, in.Name, in.Roles, in.Permissions,
		in.OrgID, in.Attributes, []string{"api_key"},
		&apiKey, &apiKeyID, sessionKind, sessionScope,
	)
}

// baseClaims is the shared constructor used by every Build*
// helper. It mirrors the Rust `base_claims` private function: the
// JTI defaults to `api_key_id` when present, otherwise a fresh
// UUID v7, so API-key tokens can be revoked by deleting the row
// keyed on api_key_id.
func baseClaims(
	cfg *JWTConfig,
	sub uuid.UUID,
	iat, exp int64,
	email, name string,
	roles, permissions []string,
	orgID *uuid.UUID,
	attributes json.RawMessage,
	authMethods []string,
	tokenUse *string,
	apiKeyID *uuid.UUID,
	sessionKind *string,
	sessionScope *SessionScope,
) Claims {
	jti := uuid.UUID{}
	if apiKeyID != nil {
		jti = *apiKeyID
	} else if v, err := uuid.NewV7(); err == nil {
		jti = v
	} else {
		jti = uuid.New()
	}

	c := Claims{
		Sub:          sub,
		IAT:          iat,
		EXP:          exp,
		JTI:          jti,
		Email:        email,
		Name:         name,
		Roles:        roles,
		Permissions:  permissions,
		OrgID:        orgID,
		Attributes:   attributes,
		AuthMethods:  authMethods,
		TokenUse:     tokenUse,
		APIKeyID:     apiKeyID,
		SessionKind:  sessionKind,
		SessionScope: sessionScope,
	}
	if cfg.Issuer != "" {
		iss := cfg.Issuer
		c.ISS = &iss
	}
	if cfg.Audience != "" {
		aud := cfg.Audience
		c.AUD = &aud
	}
	return c
}

func stringPtr(s string) *string { return &s }
