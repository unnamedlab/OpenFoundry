package saml

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/models"
)

// ParseSamlResponse mirrors fn `parse_saml_response` — the
// production entry point that uses time.Now() as the validation
// clock. Tests should use ParseSamlResponseAt to pin the clock.
func ParseSamlResponse(
	provider *models.SsoProvider,
	samlResponse string,
	validation ValidationContext,
) (*Identity, error) {
	return ParseSamlResponseAt(provider, samlResponse, validation, time.Now().UTC())
}

// ParseSamlResponseAt mirrors fn `parse_saml_response_at`. Decodes
// the base64-armoured SAML XML, validates every signature against
// the provider's pinned cert, then runs the full RFC 7522
// validation chain (status / destination / in_response_to /
// issue_instant / signed-coverage / issuer / conditions / audience
// / subject_confirmation) before extracting the canonical
// SamlIdentity.
//
// Failure mode: every validation step returns the same shape of
// error the Rust impl emits so log scraping remains unchanged
// across the migration. The base64 decode + UTF-8 decode are first
// in the chain because they run independently of the signature
// verification (the IdP cert can't help us decode malformed
// transport).
func ParseSamlResponseAt(
	provider *models.SsoProvider,
	samlResponse string,
	validation ValidationContext,
	now time.Time,
) (*Identity, error) {
	if provider == nil {
		return nil, errors.New("provider is nil")
	}
	decoded, err := base64.StdEncoding.DecodeString(samlResponse)
	if err != nil {
		return nil, fmt.Errorf("failed to base64-decode SAMLResponse: %w", err)
	}
	xmlBody := string(decoded)

	signedRefs, err := VerifySamlSignature(provider, xmlBody, now)
	if err != nil {
		return nil, err
	}

	root, err := parseTree(xmlBody)
	if err != nil {
		return nil, err
	}
	if !root.matches(NSProtocol, "Response") {
		return nil, errors.New("saml response root element must be samlp:Response")
	}

	if err := validateStatusSuccess(root); err != nil {
		return nil, err
	}
	if err := validateOptionalDestination(
		strPtrAttr(root, "Destination"),
		validation.ServiceProvider.AssertionConsumerServiceURL,
	); err != nil {
		return nil, err
	}
	if err := validateOptionalInResponseTo(
		strPtrAttr(root, "InResponseTo"),
		validation.RequestID,
		"response",
	); err != nil {
		return nil, err
	}
	if err := validateIssueInstant(
		strPtrAttr(root, "IssueInstant"),
		now,
		validation.ServiceProvider.AllowedClockSkewSecs,
		"response",
	); err != nil {
		return nil, err
	}

	assertions := root.findDescendants(NSAssertion, "Assertion")
	switch len(assertions) {
	case 0:
		return nil, errors.New("saml response is missing Assertion")
	case 1:
		// happy path
	default:
		return nil, errors.New("saml response contains multiple assertions")
	}
	assertion := assertions[0]

	responseID := strPtrAttr(root, "ID")
	assertionID := strPtrAttr(assertion, "ID")
	if assertionID == nil {
		assertionID = strPtrAttr(assertion, "AssertionID")
	}
	responseSigned := responseID != nil && containsKey(signedRefs, *responseID)
	assertionSigned := assertionID != nil && containsKey(signedRefs, *assertionID)
	if !responseSigned && !assertionSigned {
		return nil, errors.New("saml response signature does not cover either the Response or the Assertion")
	}

	if err := validateExpectedIssuer(root, assertion, provider.SamlEntityID); err != nil {
		return nil, err
	}
	if err := validateIssueInstant(
		strPtrAttr(assertion, "IssueInstant"),
		now,
		validation.ServiceProvider.AllowedClockSkewSecs,
		"assertion",
	); err != nil {
		return nil, err
	}
	if err := validateConditions(assertion, now, validation.ServiceProvider.AllowedClockSkewSecs); err != nil {
		return nil, err
	}
	if err := validateAudience(assertion, validation.ServiceProvider); err != nil {
		return nil, err
	}
	if err := validateSubjectConfirmation(assertion, validation.RequestID, validation.ServiceProvider, now); err != nil {
		return nil, err
	}

	nameID := elementText(assertion.findDescendant(NSAssertion, "NameID"))
	if nameID == "" {
		return nil, errors.New("saml assertion is missing NameID")
	}

	attributes := extractAttributes(assertion)
	attributes["NameID"] = nameID

	subjectKey := mappingValue(provider.AttributeMapping, "subject", "NameID")
	emailKey := mappingValue(provider.AttributeMapping, "email", "email")
	nameKey := mappingValue(provider.AttributeMapping, "name", "name")

	email := claimFirstString(attributes, emailKey)
	if email == "" {
		return nil, errors.New("saml response is missing email attribute")
	}
	subject := claimFirstString(attributes, subjectKey)
	if subject == "" {
		subject = nameID
	}
	name := claimFirstString(attributes, nameKey)
	if name == "" {
		name = email
	}

	return &Identity{
		Subject:   subject,
		Email:     email,
		Name:      name,
		RawClaims: attributes,
	}, nil
}

// strPtrAttr is the *element-aware variant of element.attribute that
// returns nil for both "attribute missing" and "attribute is empty
// after trim". Mirrors the Rust `node.attribute(name)` shape.
func strPtrAttr(el *element, name string) *string {
	if v, ok := el.rawAttribute(name); ok {
		return &v
	}
	return nil
}

func containsKey(set map[string]struct{}, key string) bool {
	_, ok := set[key]
	return ok
}

// mappingValue reads a string slot from the SsoProvider's
// attribute_mapping JSON column. Missing / non-string / empty
// values fall back to `defaultKey`. The Rust impl uses
// `provider.attribute_mapping.get(slot).and_then(Value::as_str)`
// — same behaviour.
func mappingValue(mapping json.RawMessage, slot, defaultKey string) string {
	if len(mapping) == 0 {
		return defaultKey
	}
	var m map[string]any
	if err := json.Unmarshal(mapping, &m); err != nil {
		return defaultKey
	}
	if v, ok := m[slot]; ok {
		if s, ok := v.(string); ok && s != "" {
			return s
		}
	}
	return defaultKey
}
