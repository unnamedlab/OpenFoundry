package saml

import (
	"strings"
	"testing"
	"time"
)

func mustParse(t *testing.T, s string) *element {
	t.Helper()
	root, err := parseTree(s)
	if err != nil {
		t.Fatalf("parseTree: %v", err)
	}
	return root
}

const responseSuccess = `<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol">
	<samlp:Status>
		<samlp:StatusCode Value="urn:oasis:names:tc:SAML:2.0:status:Success"/>
	</samlp:Status>
</samlp:Response>`

const responseFailure = `<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol">
	<samlp:Status>
		<samlp:StatusCode Value="urn:oasis:names:tc:SAML:2.0:status:Requester"/>
	</samlp:Status>
</samlp:Response>`

func TestValidateStatusSuccessAccepts(t *testing.T) {
	if err := validateStatusSuccess(mustParse(t, responseSuccess)); err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestValidateStatusSuccessRejectsNonSuccess(t *testing.T) {
	err := validateStatusSuccess(mustParse(t, responseFailure))
	if err == nil || !strings.Contains(err.Error(), "non-success") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateStatusSuccessRejectsMissing(t *testing.T) {
	root := mustParse(t, `<samlp:Response xmlns:samlp="urn:oasis:names:tc:SAML:2.0:protocol"></samlp:Response>`)
	err := validateStatusSuccess(root)
	if err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("got %v", err)
	}
}

const expectedIssuerXML = `<root xmlns="urn:oasis:names:tc:SAML:2.0:protocol" xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion">
	<saml:Issuer>http://idp.example.com/metadata.php</saml:Issuer>
	<assertion xmlns="urn:oasis:names:tc:SAML:2.0:assertion">
		<saml:Issuer>http://idp.example.com/metadata.php</saml:Issuer>
	</assertion>
</root>`

func TestValidateExpectedIssuerAccepts(t *testing.T) {
	root := mustParse(t, expectedIssuerXML)
	assertion := root.findDescendant(NSAssertion, "assertion")
	if assertion == nil {
		t.Fatalf("could not find assertion element")
	}
	expected := "http://idp.example.com/metadata.php"
	if err := validateExpectedIssuer(root, assertion, &expected); err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestValidateExpectedIssuerSkipsWhenNotConfigured(t *testing.T) {
	root := mustParse(t, expectedIssuerXML)
	assertion := root.findDescendant(NSAssertion, "assertion")
	if err := validateExpectedIssuer(root, assertion, nil); err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestValidateExpectedIssuerRejectsMismatch(t *testing.T) {
	tampered := strings.ReplaceAll(expectedIssuerXML, "metadata.php", "tampered.php")
	root := mustParse(t, tampered)
	assertion := root.findDescendant(NSAssertion, "assertion")
	expected := "http://idp.example.com/metadata.php"
	err := validateExpectedIssuer(root, assertion, &expected)
	if err == nil || !strings.Contains(err.Error(), "issuer mismatch") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateExpectedIssuerRejectsAllMissing(t *testing.T) {
	root := mustParse(t, `<root xmlns="urn:oasis:names:tc:SAML:2.0:protocol"><assertion xmlns="urn:oasis:names:tc:SAML:2.0:assertion"></assertion></root>`)
	assertion := root.findDescendant(NSAssertion, "assertion")
	expected := "x"
	err := validateExpectedIssuer(root, assertion, &expected)
	if err == nil || !strings.Contains(err.Error(), "missing issuer") {
		t.Fatalf("got %v", err)
	}
}

const conditionsAssertion = `<saml:Assertion xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion">
	<saml:Conditions NotBefore="2024-01-18T06:20:00Z" NotOnOrAfter="2024-01-18T06:21:48Z">
		<saml:AudienceRestriction>
			<saml:Audience>http://sp.example.com/demo1/metadata.php</saml:Audience>
		</saml:AudienceRestriction>
	</saml:Conditions>
</saml:Assertion>`

func TestValidateConditionsAcceptsWithinWindow(t *testing.T) {
	now := time.Date(2024, 1, 18, 6, 20, 30, 0, time.UTC)
	if err := validateConditions(mustParse(t, conditionsAssertion), now, 0); err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestValidateConditionsRejectsTooEarly(t *testing.T) {
	now := time.Date(2024, 1, 18, 6, 19, 0, 0, time.UTC)
	err := validateConditions(mustParse(t, conditionsAssertion), now, 0)
	if err == nil || !strings.Contains(err.Error(), "not yet valid") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateConditionsRejectsExpired(t *testing.T) {
	now := time.Date(2024, 1, 18, 6, 22, 0, 0, time.UTC)
	err := validateConditions(mustParse(t, conditionsAssertion), now, 0)
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateConditionsHonorsClockSkew(t *testing.T) {
	// 60s past the NotOnOrAfter still passes with 120s skew.
	now := time.Date(2024, 1, 18, 6, 22, 30, 0, time.UTC)
	if err := validateConditions(mustParse(t, conditionsAssertion), now, 120); err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestValidateConditionsRejectsMissing(t *testing.T) {
	root := mustParse(t, `<saml:Assertion xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion"></saml:Assertion>`)
	now := time.Now()
	err := validateConditions(root, now, 0)
	if err == nil || !strings.Contains(err.Error(), "missing Conditions") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateAudienceAcceptsEntityID(t *testing.T) {
	sp := ServiceProviderConfig{EntityID: "http://sp.example.com/demo1/metadata.php"}
	if err := validateAudience(mustParse(t, conditionsAssertion), sp); err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestValidateAudienceAcceptsACSURL(t *testing.T) {
	sp := ServiceProviderConfig{
		EntityID:                    "different",
		AssertionConsumerServiceURL: "http://sp.example.com/demo1/metadata.php",
	}
	if err := validateAudience(mustParse(t, conditionsAssertion), sp); err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestValidateAudienceRejectsMismatch(t *testing.T) {
	sp := ServiceProviderConfig{EntityID: "https://wrong.example.com/metadata"}
	err := validateAudience(mustParse(t, conditionsAssertion), sp)
	if err == nil || !strings.Contains(err.Error(), "audience") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateAudienceRejectsMissing(t *testing.T) {
	root := mustParse(t, `<saml:Assertion xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion"></saml:Assertion>`)
	err := validateAudience(root, ServiceProviderConfig{EntityID: "x"})
	if err == nil || !strings.Contains(err.Error(), "missing audience") {
		t.Fatalf("got %v", err)
	}
}

const subjectConfirmationXML = `<saml:Assertion xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion">
	<saml:Subject>
		<saml:SubjectConfirmation Method="urn:oasis:names:tc:SAML:2.0:cm:bearer">
			<saml:SubjectConfirmationData
				NotOnOrAfter="2024-01-18T06:21:48Z"
				Recipient="http://sp.example.com/demo1/index.php?acs"
				InResponseTo="ONELOGIN_4fee3b046395c4e751011e97f8900b5273d56685"/>
		</saml:SubjectConfirmation>
	</saml:Subject>
</saml:Assertion>`

func defaultSP() ServiceProviderConfig {
	return ServiceProviderConfig{
		EntityID:                    "http://sp.example.com/demo1/metadata.php",
		AssertionConsumerServiceURL: "http://sp.example.com/demo1/index.php?acs",
		AllowedClockSkewSecs:        120,
	}
}

func TestValidateSubjectConfirmationAccepts(t *testing.T) {
	root := mustParse(t, subjectConfirmationXML)
	now := time.Date(2024, 1, 18, 6, 20, 0, 0, time.UTC)
	reqID := "ONELOGIN_4fee3b046395c4e751011e97f8900b5273d56685"
	if err := validateSubjectConfirmation(root, &reqID, defaultSP(), now); err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestValidateSubjectConfirmationRejectsRecipientMismatch(t *testing.T) {
	root := mustParse(t, subjectConfirmationXML)
	now := time.Date(2024, 1, 18, 6, 20, 0, 0, time.UTC)
	sp := defaultSP()
	sp.AssertionConsumerServiceURL = "http://different/acs"
	err := validateSubjectConfirmation(root, nil, sp, now)
	if err == nil || !strings.Contains(err.Error(), "recipient mismatch") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateSubjectConfirmationRejectsExpired(t *testing.T) {
	root := mustParse(t, subjectConfirmationXML)
	now := time.Date(2024, 1, 18, 6, 25, 0, 0, time.UTC)
	err := validateSubjectConfirmation(root, nil, defaultSP(), now)
	if err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateSubjectConfirmationRejectsInResponseToMismatch(t *testing.T) {
	root := mustParse(t, subjectConfirmationXML)
	now := time.Date(2024, 1, 18, 6, 20, 0, 0, time.UTC)
	other := "DIFFERENT_REQUEST_ID"
	err := validateSubjectConfirmation(root, &other, defaultSP(), now)
	if err == nil || !strings.Contains(err.Error(), "InResponseTo mismatch") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateSubjectConfirmationRejectsMissingBearer(t *testing.T) {
	other := strings.ReplaceAll(subjectConfirmationXML, "cm:bearer", "cm:other")
	root := mustParse(t, other)
	err := validateSubjectConfirmation(root, nil, defaultSP(), time.Now().UTC())
	if err == nil || !strings.Contains(err.Error(), "bearer") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateIssueInstantAccepts(t *testing.T) {
	now := time.Date(2024, 1, 18, 6, 20, 0, 0, time.UTC)
	v := "2024-01-18T06:19:50Z"
	if err := validateIssueInstant(&v, now, 0, "response"); err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestValidateIssueInstantRejectsFuture(t *testing.T) {
	now := time.Date(2024, 1, 18, 6, 20, 0, 0, time.UTC)
	v := "2024-01-18T06:25:00Z"
	err := validateIssueInstant(&v, now, 0, "response")
	if err == nil || !strings.Contains(err.Error(), "in the future") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateIssueInstantHonoursClockSkew(t *testing.T) {
	now := time.Date(2024, 1, 18, 6, 20, 0, 0, time.UTC)
	v := "2024-01-18T06:21:30Z"
	if err := validateIssueInstant(&v, now, 120, "assertion"); err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestValidateIssueInstantRejectsMissing(t *testing.T) {
	now := time.Now().UTC()
	err := validateIssueInstant(nil, now, 0, "response")
	if err == nil || !strings.Contains(err.Error(), "missing IssueInstant") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateOptionalDestinationAcceptsAbsent(t *testing.T) {
	if err := validateOptionalDestination(nil, "https://x/"); err != nil {
		t.Fatalf("err: %v", err)
	}
	empty := "  "
	if err := validateOptionalDestination(&empty, "https://x/"); err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestValidateOptionalDestinationRejectsMismatch(t *testing.T) {
	v := "https://wrong/"
	err := validateOptionalDestination(&v, "https://right/")
	if err == nil || !strings.Contains(err.Error(), "destination mismatch") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateOptionalInResponseToSkipsWhenNotIssued(t *testing.T) {
	v := "any"
	if err := validateOptionalInResponseTo(&v, nil, "x"); err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestValidateOptionalInResponseToRequiresEcho(t *testing.T) {
	expected := "REQ_ID"
	err := validateOptionalInResponseTo(nil, &expected, "response")
	if err == nil || !strings.Contains(err.Error(), "missing InResponseTo") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateOptionalInResponseToMatch(t *testing.T) {
	expected := "REQ_ID"
	actual := "REQ_ID"
	if err := validateOptionalInResponseTo(&actual, &expected, "response"); err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestValidateRequiredAttributeMatchAccepts(t *testing.T) {
	v := "expected"
	if err := validateRequiredAttributeMatch(&v, "expected", "label"); err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestValidateRequiredAttributeMatchRejectsMissing(t *testing.T) {
	err := validateRequiredAttributeMatch(nil, "expected", "label")
	if err == nil || !strings.Contains(err.Error(), "label is missing") {
		t.Fatalf("got %v", err)
	}
}

func TestValidateRequiredAttributeMatchRejectsMismatch(t *testing.T) {
	v := "actual"
	err := validateRequiredAttributeMatch(&v, "expected", "label")
	if err == nil || !strings.Contains(err.Error(), "label mismatch") {
		t.Fatalf("got %v", err)
	}
}

const attributesAssertion = `<saml:Assertion xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion">
	<saml:AttributeStatement>
		<saml:Attribute Name="email">
			<saml:AttributeValue>alice@example.com</saml:AttributeValue>
		</saml:Attribute>
		<saml:Attribute Name="eduPersonAffiliation">
			<saml:AttributeValue>users</saml:AttributeValue>
			<saml:AttributeValue>examplerole1</saml:AttributeValue>
		</saml:Attribute>
		<saml:Attribute Name="emptyAttr">
			<saml:AttributeValue/>
		</saml:Attribute>
	</saml:AttributeStatement>
</saml:Assertion>`

func TestExtractAttributesSingleAndMulti(t *testing.T) {
	root := mustParse(t, attributesAssertion)
	got := extractAttributes(root)
	if got["email"] != "alice@example.com" {
		t.Errorf("email: got %v", got["email"])
	}
	multi, ok := got["eduPersonAffiliation"].([]string)
	if !ok || len(multi) != 2 {
		t.Errorf("eduPersonAffiliation: got %T %v", got["eduPersonAffiliation"], got["eduPersonAffiliation"])
	} else {
		if multi[0] != "users" || multi[1] != "examplerole1" {
			t.Errorf("eduPersonAffiliation values: %v", multi)
		}
	}
	if _, present := got["emptyAttr"]; present {
		t.Errorf("empty attribute should be dropped, got %v", got["emptyAttr"])
	}
}

func TestExtractAttributesIgnoresUnnamed(t *testing.T) {
	xml := `<saml:Assertion xmlns:saml="urn:oasis:names:tc:SAML:2.0:assertion">
		<saml:Attribute><saml:AttributeValue>orphan</saml:AttributeValue></saml:Attribute>
	</saml:Assertion>`
	got := extractAttributes(mustParse(t, xml))
	if len(got) != 0 {
		t.Fatalf("expected empty, got %v", got)
	}
}

func TestParseTreeRejectsEmpty(t *testing.T) {
	if _, err := parseTree(""); err == nil {
		t.Fatalf("expected error on empty input")
	}
}

func TestElementHelpers(t *testing.T) {
	root := mustParse(t, `<root xmlns="ns:test"><child a="1">hi</child></root>`)
	if !root.matches("ns:test", "root") {
		t.Errorf("matches with ns failed")
	}
	if !root.matches("", "root") {
		t.Errorf("matches with empty ns should accept any namespace")
	}
	if root.matches("ns:wrong", "root") {
		t.Errorf("matches with wrong ns should fail")
	}
	if root.attribute("missing") != nil {
		t.Errorf("missing attribute should be nil")
	}
	child := root.findChild("ns:test", "child")
	if child == nil {
		t.Fatalf("findChild missed")
	}
	if v := child.attribute("a"); v == nil || *v != "1" {
		t.Errorf("attribute: %v", v)
	}
	if elementText(child) != "hi" {
		t.Errorf("elementText: got %q", elementText(child))
	}
	if root.findDescendant("ns:test", "child") != child {
		t.Errorf("findDescendant missed")
	}
}
