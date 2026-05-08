// Package saml ports `domain::saml` from the Rust crate. Sub-slice
// 3.7a.1 lays the foundation: pure-domain types + format helpers
// (XML escape, certificate normalisation, time parsing, claim
// extraction). The XML parser, AuthnRequest builder and signature
// verification land in 3.7a.2 / 3.7a.3 / 3.7a.4 respectively.
//
// The Go port keeps the public-API names from the Rust source so the
// follow-up slices can wire it with no surprise. SsoProvider lives
// in `services/identity-federation-service/internal/models` to
// preserve the Rust separation between persistence rows and
// domain-layer logic.
package saml

import "time"

// SAML 2.0 namespace + status constants. Keep in sync with
// `src/domain/saml.rs` — they're exported (lowercase) so the rest
// of the package can reference them and tests can pin the URIs.
const (
	NSAssertion              = "urn:oasis:names:tc:SAML:2.0:assertion"
	NSProtocol               = "urn:oasis:names:tc:SAML:2.0:protocol"
	StatusSuccess            = "urn:oasis:names:tc:SAML:2.0:status:Success"
	SubjectConfirmationBearer = "urn:oasis:names:tc:SAML:2.0:cm:bearer"
)

// MetadataDefaults mirrors `SamlMetadataDefaults` — the three fields
// the metadata parser harvests from an IdP-published EntityDescriptor:
// entity_id (issuer), sso_url (SingleSignOnService Location) and
// certificate (the X509Certificate body). Each is optional because
// the parser is lenient — partial metadata is surfaced and the
// admin UI fills the gaps.
type MetadataDefaults struct {
	EntityID    *string
	SsoURL      *string
	Certificate *string
}

// Identity mirrors `SamlIdentity` — the canonical shape the SAML
// callback returns to the SSO router. Subject is the IdP-side
// stable identifier (NameID or the mapped attribute), email + name
// follow the provider's attribute_mapping, and RawClaims is the
// full attribute map (for downstream IdP claim → role mapping).
type Identity struct {
	Subject   string
	Email     string
	Name      string
	RawClaims map[string]any
}

// ServiceProviderConfig mirrors `SamlServiceProviderConfig` — the SP
// side of the SAML conversation. Sourced from service config (we
// host the SP, the IdP is external). AllowedClockSkewSecs gates
// IssueInstant / Conditions / SubjectConfirmationData time checks.
type ServiceProviderConfig struct {
	EntityID                       string
	AssertionConsumerServiceURL    string
	AllowedClockSkewSecs           int64
}

// ValidationContext bundles ServiceProvider + the originating
// AuthnRequest ID so the response parser can validate
// InResponseTo on both the Response and the
// SubjectConfirmationData (when present).
type ValidationContext struct {
	ServiceProvider ServiceProviderConfig
	RequestID       *string
}

// clockSkew clamps the configured skew window to a non-negative
// duration. Mirrors fn `clock_skew`.
func clockSkew(seconds int64) time.Duration {
	if seconds < 0 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}
