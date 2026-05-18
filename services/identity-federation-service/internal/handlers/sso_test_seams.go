package handlers

import (
	"net/http"

	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/models"
)

// EmitAuthAuditForTest is a thin exported façade over the package-
// private emitAuthAudit so the integration test in
// services/identity-federation-service/internal/handlers can exercise
// the production audit-emission path without driving a full
// OIDC/SAML callback. It is not invoked by any handler at runtime
// and carries no behaviour beyond the wrapper call.
func EmitAuthAuditForTest(s *SSO, r *http.Request, user *models.User, provider, subject, loginEmail string, firstLink bool, authMethods []string, accessToken string) {
	s.emitAuthAudit(r, authAuditInput{
		User:        user,
		Provider:    provider,
		Subject:     subject,
		LoginEmail:  loginEmail,
		FirstLink:   firstLink,
		AuthMethods: authMethods,
		AccessToken: accessToken,
	})
}
