package service

import (
	"encoding/json"
	"errors"
	"time"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/libs/core-models/ids"
	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/models"
)

// ChallengeTTL is the lifetime of an MFA challenge token.
const ChallengeTTL = 5 * time.Minute

// IssueMFAChallenge mints a short-lived JWT with token_use="mfa_challenge".
//
// The Rust impl issues this when the login password verification
// succeeds but MFA is required; the client then POSTs back the
// challenge_token + the TOTP code to /auth/mfa/totp/complete-login.
func IssueMFAChallenge(jwt *authmw.JWTConfig, user *models.User, authMethod string) (string, error) {
	now := time.Now()
	use := "mfa_challenge"
	c := &authmw.Claims{
		Sub:         user.ID,
		IAT:         now.Unix(),
		EXP:         now.Add(ChallengeTTL).Unix(),
		ISS:         maybe(jwt.Issuer),
		AUD:         maybe(jwt.Audience),
		JTI:         ids.New(),
		Email:       user.Email,
		Name:        user.Name,
		Roles:       []string{},
		Permissions: []string{},
		OrgID:       user.OrganizationID,
		Attributes:  user.Attributes,
		AuthMethods: []string{authMethod},
		TokenUse:    &use,
	}
	if c.Attributes == nil {
		c.Attributes = json.RawMessage(`{}`)
	}
	return authmw.EncodeToken(jwt, c)
}

// ValidateMFAChallenge decodes the token, asserts token_use="mfa_challenge",
// and returns the inner claims (containing the user's id).
func ValidateMFAChallenge(jwt *authmw.JWTConfig, token string) (*authmw.Claims, error) {
	c, err := authmw.DecodeToken(jwt, token)
	if err != nil {
		return nil, err
	}
	if c.TokenUse == nil || *c.TokenUse != "mfa_challenge" {
		return nil, ErrInvalidChallenge
	}
	return c, nil
}

// ErrInvalidChallenge is returned when the JWT decodes but its
// token_use claim is not "mfa_challenge".
var ErrInvalidChallenge = errors.New("invalid mfa challenge token")
