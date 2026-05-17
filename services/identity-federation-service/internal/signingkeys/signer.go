package signingkeys

import (
	"context"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
)

// IssueRS256 signs the supplied claims with the manager's current
// active key. Sign always uses the active key — never a retiring
// one — matching the contract verifiers expect.
func (m *Manager) IssueRS256(ctx context.Context, claims *authmw.Claims) (string, error) {
	mat, err := m.ActiveKey(ctx)
	if err != nil {
		return "", err
	}
	return authmw.EncodeTokenRS256(claims, mat.PrivateKey, mat.Record.Kid)
}

// VerifyRS256 validates an RS256 token signed by any kid that is
// currently active or retiring. issuer / audience are passed
// through to the auth-middleware validator unchanged; empty values
// skip those checks.
func (m *Manager) VerifyRS256(ctx context.Context, token, issuer, audience string) (*authmw.Claims, error) {
	return authmw.DecodeTokenRS256Multi(token, m.PublicKeyResolver(ctx), issuer, audience)
}
