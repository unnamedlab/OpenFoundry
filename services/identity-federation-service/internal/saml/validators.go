package saml

import (
	"errors"
	"fmt"
	"time"
)

// validateStatusSuccess mirrors fn `validate_status_success` —
// asserts `Status > StatusCode[@Value]` matches StatusSuccess.
// Returns a descriptive error when the status code is missing
// or non-success (auth failure, requester error, etc).
func validateStatusSuccess(response *element) error {
	status := response.findChild(NSProtocol, "Status")
	if status == nil {
		return errors.New("saml response is missing StatusCode")
	}
	statusCode := status.findChild(NSProtocol, "StatusCode")
	if statusCode == nil {
		return errors.New("saml response is missing StatusCode")
	}
	value := statusCode.attribute("Value")
	if value == nil {
		return errors.New("saml response is missing StatusCode")
	}
	if *value == StatusSuccess {
		return nil
	}
	return fmt.Errorf("saml response returned a non-success status: %s", *value)
}

// validateExpectedIssuer mirrors fn `validate_expected_issuer`.
// When the configured provider has a saml_entity_id, every Issuer
// found in the Response and the Assertion must match. Each side is
// optional individually but at least one must be present.
func validateExpectedIssuer(response, assertion *element, expected *string) error {
	expectedIssuer := trimmedPtr(expected)
	if expectedIssuer == nil {
		return nil
	}
	respIssuer := elementText(response.findChild(NSAssertion, "Issuer"))
	assertIssuer := elementText(assertion.findChild(NSAssertion, "Issuer"))
	if respIssuer != "" && respIssuer != *expectedIssuer {
		return fmt.Errorf("saml response issuer mismatch: expected %s, got %s", *expectedIssuer, respIssuer)
	}
	if assertIssuer != "" && assertIssuer != *expectedIssuer {
		return fmt.Errorf("saml assertion issuer mismatch: expected %s, got %s", *expectedIssuer, assertIssuer)
	}
	if respIssuer == "" && assertIssuer == "" {
		return errors.New("saml response is missing issuer")
	}
	return nil
}

// validateConditions mirrors fn `validate_conditions`.
//
// Requires the Assertion to carry a Conditions element with at
// least implicit time bounds. NotBefore + NotOnOrAfter, when
// present, are checked against `now` with the configured clock
// skew applied symmetrically.
func validateConditions(assertion *element, now time.Time, allowedSkewSecs int64) error {
	conditions := assertion.findChild(NSAssertion, "Conditions")
	if conditions == nil {
		return errors.New("saml assertion is missing Conditions")
	}
	skew := clockSkew(allowedSkewSecs)
	if v := conditions.attribute("NotBefore"); v != nil {
		nb, err := parseSamlTime(*v, "conditions NotBefore")
		if err != nil {
			return err
		}
		if now.Before(nb.Add(-skew)) {
			return errors.New("saml assertion is not yet valid")
		}
	}
	if v := conditions.attribute("NotOnOrAfter"); v != nil {
		na, err := parseSamlTime(*v, "conditions NotOnOrAfter")
		if err != nil {
			return err
		}
		if !now.Before(na.Add(skew)) {
			return errors.New("saml assertion has expired")
		}
	}
	return nil
}

// validateAudience mirrors fn `validate_audience`.
//
// Every assertion MUST carry at least one Audience restriction.
// One of them must equal the SP's entity_id or its
// AssertionConsumerServiceURL — many IdPs send the ACS URL as the
// audience instead of the entity_id, so both shapes are accepted.
func validateAudience(assertion *element, sp ServiceProviderConfig) error {
	audiences := assertion.findDescendants(NSAssertion, "Audience")
	if len(audiences) == 0 {
		return errors.New("saml assertion is missing audience restriction")
	}
	for _, a := range audiences {
		text := elementText(a)
		if text == sp.EntityID || text == sp.AssertionConsumerServiceURL {
			return nil
		}
	}
	return fmt.Errorf("saml assertion audience does not match service provider %s", sp.EntityID)
}

// validateSubjectConfirmation mirrors fn
// `validate_subject_confirmation` — locates the bearer
// SubjectConfirmation, validates Recipient (must equal ACS URL),
// optional InResponseTo (must round-trip from AuthnRequest), and
// the mandatory NotOnOrAfter bound.
func validateSubjectConfirmation(
	assertion *element,
	requestID *string,
	sp ServiceProviderConfig,
	now time.Time,
) error {
	var data *element
	for _, sc := range assertion.findDescendants(NSAssertion, "SubjectConfirmation") {
		method := sc.attribute("Method")
		if method == nil || *method != SubjectConfirmationBearer {
			continue
		}
		data = sc.findChild(NSAssertion, "SubjectConfirmationData")
		if data != nil {
			break
		}
	}
	if data == nil {
		return errors.New("saml assertion is missing bearer SubjectConfirmationData")
	}
	if err := validateRequiredAttributeMatch(
		data.attribute("Recipient"),
		sp.AssertionConsumerServiceURL,
		"subject confirmation recipient",
	); err != nil {
		return err
	}
	if err := validateOptionalInResponseTo(
		data.attribute("InResponseTo"),
		requestID,
		"subject confirmation",
	); err != nil {
		return err
	}
	notOnOrAfter := data.attribute("NotOnOrAfter")
	if notOnOrAfter == nil {
		return errors.New("saml subject confirmation is missing NotOnOrAfter")
	}
	expiresAt, err := parseSamlTime(*notOnOrAfter, "subject confirmation NotOnOrAfter")
	if err != nil {
		return err
	}
	if !now.Before(expiresAt.Add(clockSkew(sp.AllowedClockSkewSecs))) {
		return errors.New("saml subject confirmation has expired")
	}
	return nil
}

// validateIssueInstant mirrors fn `validate_issue_instant`.
//
// Asserts the IssueInstant is not in the future beyond the
// allowed clock skew. `label` is interpolated into the error
// message — one of "response" / "assertion" — so callers can
// distinguish at a glance.
func validateIssueInstant(issueInstant *string, now time.Time, allowedSkewSecs int64, label string) error {
	if issueInstant == nil {
		return fmt.Errorf("saml %s is missing IssueInstant", label)
	}
	t, err := parseSamlTime(*issueInstant, fmt.Sprintf("%s IssueInstant", label))
	if err != nil {
		return err
	}
	if t.After(now.Add(clockSkew(allowedSkewSecs))) {
		return fmt.Errorf("saml %s IssueInstant is in the future", label)
	}
	return nil
}

// validateOptionalDestination mirrors fn
// `validate_optional_destination` — when the Response carries a
// Destination attribute, it must equal the ACS URL we host. When
// absent the check is a no-op (some IdPs omit it).
func validateOptionalDestination(destination *string, expected string) error {
	d := trimmedPtr(destination)
	if d == nil {
		return nil
	}
	if *d != expected {
		return fmt.Errorf("saml response destination mismatch: expected %s, got %s", expected, *d)
	}
	return nil
}

// validateOptionalInResponseTo mirrors fn
// `validate_optional_in_response_to` — when we issued an
// AuthnRequest with a request_id, the Response (and the bearer
// SubjectConfirmationData) MUST echo it back via InResponseTo.
// IdP-initiated SAML responses don't carry a request_id so the
// check is skipped when `expected` is nil.
func validateOptionalInResponseTo(actual, expected *string, label string) error {
	exp := trimmedPtr(expected)
	if exp == nil {
		return nil
	}
	act := trimmedPtr(actual)
	if act == nil {
		return fmt.Errorf("saml %s is missing InResponseTo", label)
	}
	if *act == *exp {
		return nil
	}
	return fmt.Errorf("saml %s InResponseTo mismatch: expected %s, got %s", label, *exp, *act)
}

// validateRequiredAttributeMatch mirrors fn
// `validate_required_attribute_match` — generic equality check
// that surfaces a labelled error on mismatch / missing value.
func validateRequiredAttributeMatch(actual *string, expected, label string) error {
	a := trimmedPtr(actual)
	if a == nil {
		return fmt.Errorf("saml %s is missing", label)
	}
	if *a == expected {
		return nil
	}
	return fmt.Errorf("saml %s mismatch: expected %s, got %s", label, expected, *a)
}

// extractAttributes mirrors fn `extract_attributes` — walks the
// assertion's descendants for `Attribute` elements and collapses
// them into the canonical map shape. Single-valued attributes land
// as `string`; multi-valued ones land as `[]string` (so callers
// can use claimFirstString without a type switch on every key).
func extractAttributes(assertion *element) map[string]any {
	out := map[string]any{}
	for _, attr := range assertion.findDescendants(NSAssertion, "Attribute") {
		name := attr.attribute("Name")
		if name == nil {
			continue
		}
		var values []string
		for _, c := range attr.Children {
			if !c.matches(NSAssertion, "AttributeValue") {
				continue
			}
			text := elementText(c)
			if text != "" {
				values = append(values, text)
			}
		}
		switch len(values) {
		case 0:
			continue
		case 1:
			out[*name] = values[0]
		default:
			out[*name] = values
		}
	}
	return out
}

// trimmedPtr is the pointer-aware variant of trimmed: returns nil
// for nil/empty/whitespace input, otherwise a pointer to the
// trimmed string. Used by the validator family because the Rust
// `Option<&str>.and_then(trimmed)` pattern is so common.
func trimmedPtr(s *string) *string {
	if s == nil {
		return nil
	}
	return trimmed(*s)
}
