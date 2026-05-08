package saml

import (
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/models"
)

// AuthnRequest mirrors the AuthnRequest builder from
// `domain::saml::build_authorization_url`. It exposes the
// constructed XML, the freshly-minted request ID and the
// issue-instant timestamp, decoupling XML construction from
// relay-state issuance (which the Rust impl couples through
// `oauth::issue_state_with_attributes`). Handlers compose the two
// pieces by:
//
//  1. calling BuildAuthnRequest to get (xml, request_id, instant)
//  2. asking the SSO state issuer to mint a relay-state JWT/row
//     with the request_id stamped in (the request_id MUST round-trip
//     to the callback so the response parser can validate
//     InResponseTo).
//  3. calling AuthorizationURL(destination, xml, relayState) to
//     stitch the redirect.
type AuthnRequest struct {
	XML          string
	RequestID    string
	IssueInstant time.Time
}

// BuildAuthnRequest mints a fresh request ID + issue instant and
// renders the AuthnRequest XML. Mirrors the in-memory portion of fn
// `build_authorization_url` (no URL stitching, no state issuance).
//
// The request_id is `_<uuidv7-simple>` matching the Rust impl
// exactly so existing IdPs that have whitelisted the format
// continue to work.
func BuildAuthnRequest(provider *models.SsoProvider, sp ServiceProviderConfig) (*AuthnRequest, error) {
	return buildAuthnRequestAt(provider, sp, time.Now().UTC(), "_"+uuid.Must(uuid.NewV7()).String())
}

// buildAuthnRequestAt is the deterministic core BuildAuthnRequest
// wraps with a clock + ID generator. Tests pin `now` + `requestID`
// directly.
func buildAuthnRequestAt(
	provider *models.SsoProvider,
	sp ServiceProviderConfig,
	now time.Time,
	requestID string,
) (*AuthnRequest, error) {
	if provider == nil {
		return nil, errors.New("provider is nil")
	}
	if provider.SamlSsoURL == nil || *provider.SamlSsoURL == "" {
		return nil, errors.New("provider is missing saml_sso_url")
	}
	destination := *provider.SamlSsoURL

	issueInstant := now.UTC().Format("2006-01-02T15:04:05Z")
	xml := fmt.Sprintf(
		`<samlp:AuthnRequest xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion" ID="%s" Version="2.0" IssueInstant="%s" ProtocolBinding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST" AssertionConsumerServiceURL="%s" Destination="%s"><saml:Issuer>%s</saml:Issuer></samlp:AuthnRequest>`,
		xmlEscape(requestID),
		xmlEscape(issueInstant),
		xmlEscape(sp.AssertionConsumerServiceURL),
		xmlEscape(destination),
		xmlEscape(sp.EntityID),
	)

	return &AuthnRequest{
		XML:          xml,
		RequestID:    requestID,
		IssueInstant: now.UTC(),
	}, nil
}

// AuthorizationURL stitches the SAMLRequest + RelayState query
// parameters onto the IdP's SingleSignOnService destination. The
// SAMLRequest value is the base64-encoded AuthnRequest XML; the
// caller is responsible for the base64 hop so this helper stays
// pure-string.
//
// Mirrors the URL-construction tail of `build_authorization_url`.
func AuthorizationURL(destination, samlRequest, relayState string) (string, error) {
	if destination == "" {
		return "", errors.New("destination is empty")
	}
	u, err := url.Parse(destination)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("SAMLRequest", samlRequest)
	q.Set("RelayState", relayState)
	u.RawQuery = q.Encode()
	return u.String(), nil
}
