package saml

import (
	"encoding/base64"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/models"
)

func ptr(s string) *string { return &s }

func fixtureProvider() *models.SsoProvider {
	return &models.SsoProvider{
		ID:           uuid.Must(uuid.NewV7()),
		Slug:         "saml",
		Name:         "SAML",
		ProviderType: "saml",
		Enabled:      true,
		SamlSsoURL:   ptr("https://idp.example.com/sso"),
		SamlEntityID: ptr("http://idp.example.com/metadata.php"),
	}
}

func fixtureServiceProvider() ServiceProviderConfig {
	return ServiceProviderConfig{
		EntityID:                    "http://sp.example.com/metadata",
		AssertionConsumerServiceURL: "http://sp.example.com/acs",
		AllowedClockSkewSecs:        120,
	}
}

func TestBuildAuthnRequestAtRendersExpectedXML(t *testing.T) {
	now := time.Date(2024, 5, 6, 12, 30, 45, 0, time.UTC)
	provider := fixtureProvider()
	sp := fixtureServiceProvider()
	req, err := buildAuthnRequestAt(provider, sp, now, "_fixed-id")
	if err != nil {
		t.Fatalf("err: %v", err)
	}

	wantContains := []string{
		`xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol"`,
		`xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion"`,
		`ID="_fixed-id"`,
		`Version="2.0"`,
		`IssueInstant="2024-05-06T12:30:45Z"`,
		`ProtocolBinding="urn:oasis:names:tc:SAML:2.0:bindings:HTTP-POST"`,
		`AssertionConsumerServiceURL="http://sp.example.com/acs"`,
		`Destination="https://idp.example.com/sso"`,
		`<saml:Issuer>http://sp.example.com/metadata</saml:Issuer>`,
	}
	for _, want := range wantContains {
		if !strings.Contains(req.XML, want) {
			t.Errorf("XML missing %q\nfull: %s", want, req.XML)
		}
	}

	if req.RequestID != "_fixed-id" {
		t.Errorf("RequestID: got %q", req.RequestID)
	}
	if !req.IssueInstant.Equal(now) {
		t.Errorf("IssueInstant: got %v, want %v", req.IssueInstant, now)
	}
}

func TestBuildAuthnRequestAtEscapesUnsafeAttributes(t *testing.T) {
	now := time.Date(2024, 5, 6, 0, 0, 0, 0, time.UTC)
	provider := fixtureProvider()
	provider.SamlSsoURL = ptr(`https://idp.example.com/sso?a=b&c=d`)
	sp := fixtureServiceProvider()
	sp.EntityID = `http://sp/<dangerous>`

	req, err := buildAuthnRequestAt(provider, sp, now, "_id")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.Contains(req.XML, `a=b&amp;c=d`) {
		t.Errorf("URL & not escaped: %s", req.XML)
	}
	if !strings.Contains(req.XML, `&lt;dangerous&gt;`) {
		t.Errorf("entity id not escaped: %s", req.XML)
	}
	if strings.Contains(req.XML, `<dangerous>`) {
		t.Errorf("raw < leaked: %s", req.XML)
	}
}

func TestBuildAuthnRequestRejectsMissingSsoURL(t *testing.T) {
	provider := fixtureProvider()
	provider.SamlSsoURL = nil
	if _, err := BuildAuthnRequest(provider, fixtureServiceProvider()); err == nil {
		t.Fatalf("expected error on missing saml_sso_url")
	}
}

func TestBuildAuthnRequestRejectsEmptySsoURL(t *testing.T) {
	provider := fixtureProvider()
	provider.SamlSsoURL = ptr("")
	if _, err := BuildAuthnRequest(provider, fixtureServiceProvider()); err == nil {
		t.Fatalf("expected error on empty saml_sso_url")
	}
}

func TestBuildAuthnRequestRejectsNilProvider(t *testing.T) {
	if _, err := BuildAuthnRequest(nil, fixtureServiceProvider()); err == nil {
		t.Fatalf("expected error on nil provider")
	}
}

func TestBuildAuthnRequestPrefixesIDWithUnderscore(t *testing.T) {
	req, err := BuildAuthnRequest(fixtureProvider(), fixtureServiceProvider())
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.HasPrefix(req.RequestID, "_") {
		t.Errorf("RequestID should start with _: %q", req.RequestID)
	}
	if len(req.RequestID) < 5 {
		t.Errorf("RequestID looks too short: %q", req.RequestID)
	}
}

func TestAuthorizationURLEncodesParams(t *testing.T) {
	dest := "https://idp.example.com/sso"
	samlReq := base64.StdEncoding.EncodeToString([]byte(`<samlp:AuthnRequest/>`))
	relay := "state-token=with-special&chars"

	got, err := AuthorizationURL(dest, samlReq, relay)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	parsed, err := url.Parse(got)
	if err != nil {
		t.Fatalf("invalid URL: %v", err)
	}
	q := parsed.Query()
	if q.Get("SAMLRequest") != samlReq {
		t.Errorf("SAMLRequest decode: got %q", q.Get("SAMLRequest"))
	}
	if q.Get("RelayState") != relay {
		t.Errorf("RelayState decode: got %q", q.Get("RelayState"))
	}
	if parsed.Host != "idp.example.com" {
		t.Errorf("host: got %s", parsed.Host)
	}
	if parsed.Path != "/sso" {
		t.Errorf("path: got %s", parsed.Path)
	}
}

func TestAuthorizationURLPreservesExistingQuery(t *testing.T) {
	got, err := AuthorizationURL("https://idp.example.com/sso?org=acme", "encoded", "rs")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	parsed, _ := url.Parse(got)
	q := parsed.Query()
	if q.Get("org") != "acme" {
		t.Errorf("existing query lost: %v", q)
	}
	if q.Get("SAMLRequest") != "encoded" {
		t.Errorf("SAMLRequest missing: %v", q)
	}
}

func TestAuthorizationURLRejectsEmptyDestination(t *testing.T) {
	if _, err := AuthorizationURL("", "x", "y"); err == nil {
		t.Fatalf("expected error on empty destination")
	}
}
