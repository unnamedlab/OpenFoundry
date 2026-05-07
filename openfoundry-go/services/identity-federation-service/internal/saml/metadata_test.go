package saml

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const metadataExample = `<EntityDescriptor xmlns="urn:oasis:names:tc:SAML:2.0:metadata" entityID="https://idp.example.com/metadata"><IDPSSODescriptor><SingleSignOnService Location="https://idp.example.com/sso"/><KeyDescriptor><KeyInfo xmlns="http://www.w3.org/2000/09/xmldsig#"><X509Data><X509Certificate>ABC123</X509Certificate></X509Data></KeyInfo></KeyDescriptor></IDPSSODescriptor></EntityDescriptor>`

func TestParseMetadataDefaultsExtractsAll(t *testing.T) {
	got, err := ParseMetadataDefaults(metadataExample)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.EntityID == nil || *got.EntityID != "https://idp.example.com/metadata" {
		t.Fatalf("EntityID: got %v", got.EntityID)
	}
	if got.SsoURL == nil || *got.SsoURL != "https://idp.example.com/sso" {
		t.Fatalf("SsoURL: got %v", got.SsoURL)
	}
	if got.Certificate == nil || *got.Certificate != "ABC123" {
		t.Fatalf("Certificate: got %v", got.Certificate)
	}
}

func TestParseMetadataDefaultsStripsCertWhitespace(t *testing.T) {
	xml := `<EntityDescriptor entityID="urn:idp"><X509Certificate>
		AAAA
		BBBB
		CCCC
	</X509Certificate></EntityDescriptor>`
	got, err := ParseMetadataDefaults(xml)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.Certificate == nil || *got.Certificate != "AAAABBBBCCCC" {
		t.Fatalf("Certificate: got %v", got.Certificate)
	}
}

func TestParseMetadataDefaultsTakesFirstSingleSignOnService(t *testing.T) {
	xml := `<EntityDescriptor entityID="x">
		<SingleSignOnService Binding="HTTP-POST" Location="https://first/"/>
		<SingleSignOnService Binding="HTTP-Redirect" Location="https://second/"/>
	</EntityDescriptor>`
	got, err := ParseMetadataDefaults(xml)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.SsoURL == nil || *got.SsoURL != "https://first/" {
		t.Fatalf("SsoURL: got %v", got.SsoURL)
	}
}

func TestParseMetadataDefaultsAllOptional(t *testing.T) {
	got, err := ParseMetadataDefaults(`<root><other/></root>`)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.EntityID != nil || got.SsoURL != nil || got.Certificate != nil {
		t.Fatalf("expected all nil, got %+v", got)
	}
}

func TestParseMetadataDefaultsRejectsMalformed(t *testing.T) {
	if _, err := ParseMetadataDefaults("<not-xml"); err == nil {
		t.Fatalf("expected parse error")
	}
}

func TestParseMetadataDefaultsTrimsAttributeWhitespace(t *testing.T) {
	xml := `<EntityDescriptor entityID="  trimmed  "/>`
	got, err := ParseMetadataDefaults(xml)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.EntityID == nil || *got.EntityID != "trimmed" {
		t.Fatalf("EntityID: got %v", got.EntityID)
	}
}

func TestResolveMetadataDefaultsHTTPRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(metadataExample))
	}))
	defer srv.Close()

	got, err := ResolveMetadataDefaults(context.Background(), srv.Client(), srv.URL)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got.EntityID == nil || !strings.Contains(*got.EntityID, "idp.example.com") {
		t.Fatalf("EntityID: got %v", got.EntityID)
	}
}

func TestResolveMetadataDefaultsRejectsNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := ResolveMetadataDefaults(context.Background(), srv.Client(), srv.URL)
	if err == nil {
		t.Fatalf("expected non-2xx error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("error should mention status 500: %v", err)
	}
}
