package saml

import (
	_ "embed"
	"strings"
	"testing"
	"time"

	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/models"
)

//go:embed testdata/response_signed.xml
var responseSignedXML string

//go:embed testdata/response_signed_assertion.xml
var responseSignedAssertionXML string

//go:embed testdata/signing_cert.pem
var signingCertPEM string

func fixtureSignedProvider() *models.SsoProvider {
	cert := signingCertPEM
	return &models.SsoProvider{
		ProviderType:    "saml",
		Enabled:         true,
		SamlCertificate: &cert,
		SamlSsoURL:      ptr("https://idp.example.com/sso"),
	}
}

func fixtureValidationClock() time.Time {
	// goxmldsig (unlike Rust's bergshamra) enforces cert validity at
	// validation time. The OneLogin sample cert in signing_cert.pem
	// is dated 2014-07-17 → 2015-07-17, so we pin "now" to a date
	// inside that window (just past NotBefore). The SAML-level
	// Conditions/SubjectConfirmationData NotOnOrAfter values in the
	// fixtures are 2024-01-18, so they're comfortably in the future
	// from this vantage point.
	return time.Date(2014, 7, 18, 0, 0, 0, 0, time.UTC)
}

func TestVerifySamlSignatureAcceptsSignedResponse(t *testing.T) {
	refs, err := VerifySamlSignature(fixtureSignedProvider(), responseSignedXML, fixtureValidationClock())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	// Response ID is signed in this fixture.
	if _, ok := refs["pfxf63324d7-7ba2-b371-90d6-171637d97253"]; !ok {
		t.Errorf("expected Response ID reference, got %v", keys(refs))
	}
}

func TestVerifySamlSignatureAcceptsSignedAssertion(t *testing.T) {
	refs, err := VerifySamlSignature(fixtureSignedProvider(), responseSignedAssertionXML, fixtureValidationClock())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(refs) == 0 {
		t.Errorf("expected at least one validated reference")
	}
}

func TestVerifySamlSignatureRejectsTampering(t *testing.T) {
	tampered := strings.ReplaceAll(responseSignedXML, "test@example.com", "attacker@example.com")
	_, err := VerifySamlSignature(fixtureSignedProvider(), tampered, fixtureValidationClock())
	if err == nil {
		t.Fatalf("expected signature verification to fail on tampered payload")
	}
	if !strings.Contains(err.Error(), "signature verification failed") {
		t.Errorf("error should mention signature verification: %v", err)
	}
}

func TestVerifySamlSignatureRejectsMissingCertificate(t *testing.T) {
	provider := fixtureSignedProvider()
	provider.SamlCertificate = nil
	_, err := VerifySamlSignature(provider, responseSignedXML, fixtureValidationClock())
	if err == nil || !strings.Contains(err.Error(), "missing saml_certificate") {
		t.Fatalf("got %v", err)
	}
}

func TestVerifySamlSignatureRejectsEmptyCertificate(t *testing.T) {
	empty := "   "
	provider := fixtureSignedProvider()
	provider.SamlCertificate = &empty
	_, err := VerifySamlSignature(provider, responseSignedXML, fixtureValidationClock())
	if err == nil || !strings.Contains(err.Error(), "missing saml_certificate") {
		t.Fatalf("got %v", err)
	}
}

func TestVerifySamlSignatureRejectsNoSignature(t *testing.T) {
	xml := `<?xml version="1.0"?><samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol"></samlp:Response>`
	_, err := VerifySamlSignature(fixtureSignedProvider(), xml, fixtureValidationClock())
	if err == nil || !strings.Contains(err.Error(), "no ds:Signature") {
		t.Fatalf("got %v", err)
	}
}

func TestVerifySamlSignatureRejectsMalformedCertificate(t *testing.T) {
	garbage := "-----BEGIN CERTIFICATE-----\nnot-base64-at-all\n-----END CERTIFICATE-----"
	provider := fixtureSignedProvider()
	provider.SamlCertificate = &garbage
	_, err := VerifySamlSignature(provider, responseSignedXML, fixtureValidationClock())
	if err == nil {
		t.Fatalf("expected error on malformed cert")
	}
}

func TestVerifySamlSignatureRejectsNilProvider(t *testing.T) {
	_, err := VerifySamlSignature(nil, responseSignedXML, fixtureValidationClock())
	if err == nil || !strings.Contains(err.Error(), "provider is nil") {
		t.Fatalf("got %v", err)
	}
}

func keys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
