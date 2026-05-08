package saml

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/beevik/etree"
	dsig "github.com/russellhaering/goxmldsig"

	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/models"
)

// VerifySamlSignature mirrors fn `verify_saml_signature` —
// validates every `<ds:Signature>` enveloped in the SAML XML
// against the provider's pinned X509 certificate and returns the
// set of `ID`s the signatures cover (with the leading `#` stripped
// from each `ds:Reference[@URI]`).
//
// The Rust impl uses bergshamra's "any signature anywhere" walk;
// goxmldsig is one-signature-per-call so we walk every signature
// in the document, peel its parent off the tree, validate the
// parent (which goxmldsig verifies covers the inner signature),
// then collect every reference URI from the original signature.
//
// `at` is the validation clock — production passes time.Now(),
// tests pin it so the 2014-vintage IdP fixtures keep working.
//
// Returns the set of validated reference IDs. The caller decides
// which IDs (Response.ID vs Assertion.ID) need to be in the set
// before trusting the assertion.
func VerifySamlSignature(provider *models.SsoProvider, xmlBody string, at time.Time) (map[string]struct{}, error) {
	if provider == nil {
		return nil, errors.New("provider is nil")
	}
	if provider.SamlCertificate == nil || strings.TrimSpace(*provider.SamlCertificate) == "" {
		return nil, errors.New("provider is missing saml_certificate")
	}
	pemBody, err := normalizeCertificatePem(*provider.SamlCertificate)
	if err != nil {
		return nil, err
	}
	cert, err := loadCertFromPEM(pemBody)
	if err != nil {
		return nil, fmt.Errorf("saml signature verification failed: %w", err)
	}

	doc := etree.NewDocument()
	if err := doc.ReadFromString(xmlBody); err != nil {
		return nil, fmt.Errorf("saml signature verification failed: %w", err)
	}
	root := doc.Root()
	if root == nil {
		return nil, errors.New("saml signature verification failed: empty document")
	}

	signatures := findEtreeSignatures(root)
	if len(signatures) == 0 {
		return nil, errors.New("saml signature verification failed: no ds:Signature elements")
	}

	store := &dsig.MemoryX509CertificateStore{Roots: []*x509.Certificate{cert}}
	ctx := dsig.NewDefaultValidationContext(store)
	if !at.IsZero() {
		ctx.Clock = dsig.NewFakeClockAt(at)
	}

	referenceIDs := map[string]struct{}{}
	for _, sig := range signatures {
		parent := sig.Parent()
		if parent == nil {
			continue
		}
		// goxmldsig copies the element internally, but etree's Copy()
		// drops inherited xmlns declarations. Build a self-contained
		// copy with every ancestor namespace baked on so exclusive
		// canonicalisation succeeds even on a nested Assertion.
		parentCopy := copyWithInheritedNamespaces(parent)
		if _, err := ctx.Validate(parentCopy); err != nil {
			return nil, fmt.Errorf("saml signature verification failed: %w", err)
		}
		for _, ref := range collectReferenceURIs(sig) {
			if id := strings.TrimPrefix(ref, "#"); id != "" {
				referenceIDs[id] = struct{}{}
			}
		}
	}

	if len(referenceIDs) == 0 {
		return nil, errors.New("saml signature did not produce any same-document references")
	}
	return referenceIDs, nil
}

// copyWithInheritedNamespaces returns a deep copy of `el` with every
// xmlns declaration inherited from its ancestors injected as
// attributes on the copy. Existing declarations on `el` win — the
// walk is innermost-first.
//
// Required because etree's Element.Copy() preserves the element's
// own attributes but loses any namespace prefixes that are only
// declared on an ancestor. SAML responses commonly declare the
// `saml` and `samlp` prefixes on the outer Response element while
// the Assertion subtree relies on those declarations being in
// scope. When goxmldsig validates a copy of the Assertion in
// isolation, exclusive canonicalisation refuses an undeclared
// prefix, surfacing as `undeclared namespace prefix: 'saml'`.
func copyWithInheritedNamespaces(el *etree.Element) *etree.Element {
	cp := el.Copy()
	declared := map[string]bool{}
	for _, a := range cp.Attr {
		if a.Space == "xmlns" {
			declared[a.Key] = true
		} else if a.Space == "" && a.Key == "xmlns" {
			declared["xmlns"] = true
		}
	}
	for cur := el.Parent(); cur != nil; cur = cur.Parent() {
		for _, a := range cur.Attr {
			if a.Space == "xmlns" && !declared[a.Key] {
				cp.CreateAttr("xmlns:"+a.Key, a.Value)
				declared[a.Key] = true
			} else if a.Space == "" && a.Key == "xmlns" && !declared["xmlns"] {
				cp.CreateAttr("xmlns", a.Value)
				declared["xmlns"] = true
			}
		}
	}
	return cp
}

// loadCertFromPEM decodes the PEM blob and returns the parsed
// X509 certificate. Mirrors `load_x509_cert_pem`.
func loadCertFromPEM(pemBody string) (*x509.Certificate, error) {
	block, _ := pem.Decode([]byte(pemBody))
	if block == nil {
		return nil, errors.New("invalid PEM block")
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, err
	}
	return cert, nil
}

// findEtreeSignatures returns every `<ds:Signature>` in the
// element subtree (DFS, document order). Used both to detect
// "no signature → error" and to walk every signature in the
// nested SAML payload (Response + Assertion can each carry one).
func findEtreeSignatures(el *etree.Element) []*etree.Element {
	var out []*etree.Element
	if isXMLDsigElement(el, "Signature") {
		out = append(out, el)
	}
	for _, child := range el.ChildElements() {
		out = append(out, findEtreeSignatures(child)...)
	}
	return out
}

// collectReferenceURIs returns every `URI` attribute from
// `ds:Reference` children of the signature's `ds:SignedInfo`.
// Empty URIs are dropped — a reference URI of "" means
// same-document but is treated as "no useful coverage" by the
// validators that want to pin Response or Assertion ID.
func collectReferenceURIs(signature *etree.Element) []string {
	signedInfo := childByXMLDsig(signature, "SignedInfo")
	if signedInfo == nil {
		return nil
	}
	var out []string
	for _, ref := range signedInfo.ChildElements() {
		if !isXMLDsigElement(ref, "Reference") {
			continue
		}
		uri := ref.SelectAttrValue("URI", "")
		if uri != "" {
			out = append(out, uri)
		}
	}
	return out
}

func childByXMLDsig(el *etree.Element, local string) *etree.Element {
	for _, c := range el.ChildElements() {
		if isXMLDsigElement(c, local) {
			return c
		}
	}
	return nil
}

// isXMLDsigElement matches the XMLDSig namespace by its standard
// URN. SAML responses use the canonical
// `http://www.w3.org/2000/09/xmldsig#` namespace so both prefixed
// (`ds:Signature`) and default-namespaced forms resolve to the
// same local name.
func isXMLDsigElement(el *etree.Element, local string) bool {
	if el.Tag != local {
		return false
	}
	switch el.Space {
	case "ds", "":
		return true
	}
	// Some IdPs use a custom prefix — fall back to namespace URI lookup.
	ns := el.NamespaceURI()
	return ns == "" || ns == "http://www.w3.org/2000/09/xmldsig#"
}
