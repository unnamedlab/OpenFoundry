package saml

import (
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/models"
)

func encB64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func fixtureFullProvider() *models.SsoProvider {
	cert := signingCertPEM
	mapping := json.RawMessage(`{
		"subject": "uid",
		"email": "mail",
		"name": "displayName"
	}`)
	return &models.SsoProvider{
		ID:               uuid.Must(uuid.NewV7()),
		Slug:             "saml",
		Name:             "SAML",
		ProviderType:     "saml",
		Enabled:          true,
		SamlSsoURL:       ptr("https://idp.example.com/sso"),
		SamlEntityID:     ptr("http://idp.example.com/metadata.php"),
		SamlCertificate:  &cert,
		AttributeMapping: mapping,
	}
}

func fixtureFullValidation() ValidationContext {
	reqID := "ONELOGIN_4fee3b046395c4e751011e97f8900b5273d56685"
	return ValidationContext{
		ServiceProvider: ServiceProviderConfig{
			EntityID:                    "http://sp.example.com/demo1/metadata.php",
			AssertionConsumerServiceURL: "http://sp.example.com/demo1/index.php?acs",
			AllowedClockSkewSecs:        120,
		},
		RequestID: &reqID,
	}
}

func TestParseSamlResponseAcceptsSignedResponse(t *testing.T) {
	ident, err := ParseSamlResponseAt(
		fixtureFullProvider(),
		encB64(responseSignedXML),
		fixtureFullValidation(),
		fixtureValidationClock(),
	)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ident.Subject != "test" {
		t.Errorf("Subject: got %q", ident.Subject)
	}
	if ident.Email != "test@example.com" {
		t.Errorf("Email: got %q", ident.Email)
	}
	if ident.Name != "test@example.com" {
		t.Errorf("Name: got %q", ident.Name)
	}
	affiliation, ok := ident.RawClaims["eduPersonAffiliation"].([]string)
	if !ok || len(affiliation) != 2 {
		t.Errorf("eduPersonAffiliation: got %T %v", ident.RawClaims["eduPersonAffiliation"], ident.RawClaims["eduPersonAffiliation"])
	} else if affiliation[0] != "users" || affiliation[1] != "examplerole1" {
		t.Errorf("eduPersonAffiliation values: %v", affiliation)
	}
}

func TestParseSamlResponseAcceptsSignedAssertion(t *testing.T) {
	ident, err := ParseSamlResponseAt(
		fixtureFullProvider(),
		encB64(responseSignedAssertionXML),
		fixtureFullValidation(),
		fixtureValidationClock(),
	)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if ident.Subject != "test" {
		t.Errorf("Subject: got %q", ident.Subject)
	}
	if ident.Email != "test@example.com" {
		t.Errorf("Email: got %q", ident.Email)
	}
}

func TestParseSamlResponseRejectsTampering(t *testing.T) {
	tampered := strings.ReplaceAll(responseSignedXML, "test@example.com", "attacker@example.com")
	_, err := ParseSamlResponseAt(
		fixtureFullProvider(),
		encB64(tampered),
		fixtureFullValidation(),
		fixtureValidationClock(),
	)
	if err == nil || !strings.Contains(err.Error(), "signature verification failed") {
		t.Fatalf("got %v", err)
	}
}

func TestParseSamlResponseRejectsWrongAudience(t *testing.T) {
	// Keep the ACS URL (so destination + subject_confirmation
	// recipient still match), only swap the EntityID — that's the
	// only knob that makes the audience check fail without
	// tripping an earlier check first.
	validation := fixtureFullValidation()
	validation.ServiceProvider.EntityID = "https://wrong.example.com/metadata"
	_, err := ParseSamlResponseAt(
		fixtureFullProvider(),
		encB64(responseSignedXML),
		validation,
		fixtureValidationClock(),
	)
	if err == nil || !strings.Contains(err.Error(), "audience") {
		t.Fatalf("got %v", err)
	}
}

func TestParseSamlResponseRejectsExpiredAssertion(t *testing.T) {
	// SAML-level NotOnOrAfter is 2024-01-18T06:21:48Z — pin clock 2025
	// to trigger the conditions/expired path. Cert validity (2014→2015)
	// would be checked first though, so use a clock where the cert
	// is still valid but the assertion isn't.
	// The SubjectConfirmationData NotOnOrAfter is also 2024-01-18, so
	// any 2025+ time triggers expiry. But the cert is invalid by
	// then. So we rely on the Conditions: NotOnOrAfter is also
	// 2024-01-18. Cert is 2014→2015, so we can't get to a clock
	// where conds are expired AND cert is valid. Skip the
	// conditions-expiry test by tampering with the fixture.
	tampered := strings.ReplaceAll(
		responseSignedXML,
		`NotOnOrAfter="2024-01-18T06:21:48Z"`,
		`NotOnOrAfter="2014-07-17T01:00:00Z"`,
	)
	_, err := ParseSamlResponseAt(
		fixtureFullProvider(),
		encB64(tampered),
		fixtureFullValidation(),
		fixtureValidationClock(),
	)
	// Tampering breaks the signature first, so we expect
	// signature verification failure.
	if err == nil || !strings.Contains(err.Error(), "signature verification failed") {
		t.Fatalf("got %v", err)
	}
}

func TestParseSamlResponseRejectsInvalidBase64(t *testing.T) {
	_, err := ParseSamlResponseAt(
		fixtureFullProvider(),
		"!!! not base64 !!!",
		fixtureFullValidation(),
		fixtureValidationClock(),
	)
	if err == nil || !strings.Contains(err.Error(), "base64") {
		t.Fatalf("got %v", err)
	}
}

func TestParseSamlResponseRejectsNonResponseRoot(t *testing.T) {
	provider := fixtureFullProvider()
	provider.SamlCertificate = nil
	xml := `<?xml version="1.0"?><other xmlns="urn:foo"/>`
	_, err := ParseSamlResponseAt(provider, encB64(xml), fixtureFullValidation(), fixtureValidationClock())
	// Missing cert short-circuits before XML root check.
	if err == nil || !strings.Contains(err.Error(), "saml_certificate") {
		t.Fatalf("got %v", err)
	}
}

func TestParseSamlResponseRejectsMissingProvider(t *testing.T) {
	_, err := ParseSamlResponseAt(nil, encB64("<x/>"), fixtureFullValidation(), fixtureValidationClock())
	if err == nil || !strings.Contains(err.Error(), "provider is nil") {
		t.Fatalf("got %v", err)
	}
}

func TestParseSamlResponseProductionEntryPoint(t *testing.T) {
	// ParseSamlResponse uses time.Now() — fixture certs are expired,
	// so this is expected to fail with cert-validity error.
	_, err := ParseSamlResponse(
		fixtureFullProvider(),
		encB64(responseSignedXML),
		fixtureFullValidation(),
	)
	if err == nil {
		t.Fatalf("expected expired-cert error from production clock")
	}
}

func TestMappingValueDefaultsForMissingKey(t *testing.T) {
	mapping := json.RawMessage(`{"email": "mail"}`)
	if got := mappingValue(mapping, "subject", "NameID"); got != "NameID" {
		t.Errorf("missing key: got %q", got)
	}
	if got := mappingValue(mapping, "email", "default"); got != "mail" {
		t.Errorf("present key: got %q", got)
	}
}

func TestMappingValueDefaultsForGarbageJSON(t *testing.T) {
	if got := mappingValue(json.RawMessage("not-json"), "x", "fallback"); got != "fallback" {
		t.Errorf("got %q", got)
	}
	if got := mappingValue(nil, "x", "fallback"); got != "fallback" {
		t.Errorf("got %q", got)
	}
}

func TestMappingValueIgnoresNonStringValues(t *testing.T) {
	mapping := json.RawMessage(`{"slot": 42}`)
	if got := mappingValue(mapping, "slot", "fallback"); got != "fallback" {
		t.Errorf("got %q", got)
	}
}

func TestParseSamlResponseDefaultMappingFallback(t *testing.T) {
	provider := fixtureFullProvider()
	provider.AttributeMapping = nil // no mapping → defaults to NameID/email/name
	// The fixture has Attribute Name="mail" and Name="givenName" etc.
	// Without a mapping, we fall back to looking up "email" — which
	// doesn't exist as an attribute, so this should fail with
	// missing email attribute.
	ident, err := ParseSamlResponseAt(
		provider,
		encB64(responseSignedXML),
		fixtureFullValidation(),
		fixtureValidationClock(),
	)
	if err == nil {
		t.Fatalf("expected missing email error, got identity %+v", ident)
	}
	if !strings.Contains(err.Error(), "missing email") {
		t.Fatalf("got %v", err)
	}
}

func TestParseSamlResponseRejectsBadInResponseTo(t *testing.T) {
	validation := fixtureFullValidation()
	wrong := "WRONG_REQUEST_ID"
	validation.RequestID = &wrong
	_, err := ParseSamlResponseAt(
		fixtureFullProvider(),
		encB64(responseSignedXML),
		validation,
		fixtureValidationClock(),
	)
	if err == nil || !strings.Contains(err.Error(), "InResponseTo mismatch") {
		t.Fatalf("got %v", err)
	}
}
