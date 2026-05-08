package saml

import (
	"strings"
	"testing"
	"time"
)

func TestXMLEscapeAllEntities(t *testing.T) {
	got := xmlEscape(`a&b<c>d"e'f`)
	want := `a&amp;b&lt;c&gt;d&quot;e&apos;f`
	if got != want {
		t.Fatalf("xmlEscape: got %q, want %q", got, want)
	}
}

func TestXMLEscapePreservesUnicode(t *testing.T) {
	got := xmlEscape("café — naïve")
	if got != "café — naïve" {
		t.Fatalf("unexpected re-encoding: %q", got)
	}
}

func TestStripAllWhitespaceRemovesEverything(t *testing.T) {
	got := stripAllWhitespace("  a b\tc\nd\re  ")
	if got != "abcde" {
		t.Fatalf("stripAllWhitespace: got %q", got)
	}
}

func TestTrimmedNilOnEmpty(t *testing.T) {
	if v := trimmed(""); v != nil {
		t.Fatalf("trimmed(\"\") should be nil, got %v", v)
	}
	if v := trimmed("   \t\n"); v != nil {
		t.Fatalf("trimmed(whitespace) should be nil, got %v", v)
	}
}

func TestTrimmedTrimsWhitespace(t *testing.T) {
	v := trimmed("  hello\n")
	if v == nil || *v != "hello" {
		t.Fatalf("trimmed: got %v", v)
	}
}

func TestNormalizeCertificatePemAlreadyArmoured(t *testing.T) {
	in := "-----BEGIN CERTIFICATE-----\nABCDEF\n-----END CERTIFICATE-----"
	out, err := normalizeCertificatePem(in)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !strings.HasPrefix(out, "-----BEGIN CERTIFICATE-----") {
		t.Fatalf("missing PEM header: %q", out)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Fatalf("missing trailing newline: %q", out)
	}
}

func TestNormalizeCertificatePemBareBase64Chunks(t *testing.T) {
	// 130 chars of base64 → 3 lines (64 + 64 + 2)
	body := strings.Repeat("A", 130)
	out, err := normalizeCertificatePem(body)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	expectedLines := []string{
		"-----BEGIN CERTIFICATE-----",
		strings.Repeat("A", 64),
		strings.Repeat("A", 64),
		"AA",
		"-----END CERTIFICATE-----",
		"", // trailing newline produces an empty final element
	}
	got := strings.Split(out, "\n")
	if len(got) != len(expectedLines) {
		t.Fatalf("line count: got %d (%v), want %d (%v)", len(got), got, len(expectedLines), expectedLines)
	}
	for i, line := range expectedLines {
		if got[i] != line {
			t.Fatalf("line %d: got %q, want %q", i, got[i], line)
		}
	}
}

func TestNormalizeCertificatePemRejectsEmpty(t *testing.T) {
	if _, err := normalizeCertificatePem(""); err == nil {
		t.Fatalf("expected error on empty input")
	}
	if _, err := normalizeCertificatePem("   "); err == nil {
		t.Fatalf("expected error on whitespace input")
	}
}

func TestParseSamlTimeAcceptsRFC3339(t *testing.T) {
	got, err := parseSamlTime("2024-01-18T06:21:48Z", "test")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := time.Date(2024, 1, 18, 6, 21, 48, 0, time.UTC)
	if !got.Equal(want) {
		t.Fatalf("parseSamlTime: got %v, want %v", got, want)
	}
}

func TestParseSamlTimeRejectsGarbage(t *testing.T) {
	_, err := parseSamlTime("not-a-time", "test")
	if err == nil {
		t.Fatalf("expected error on garbage input")
	}
	if !strings.Contains(err.Error(), "invalid test") {
		t.Fatalf("error message should include label: %v", err)
	}
}

func TestClockSkewClampsNegative(t *testing.T) {
	if got := clockSkew(-5); got != 0 {
		t.Fatalf("clockSkew(-5): got %v, want 0", got)
	}
	if got := clockSkew(0); got != 0 {
		t.Fatalf("clockSkew(0): got %v, want 0", got)
	}
	if got := clockSkew(120); got != 120*time.Second {
		t.Fatalf("clockSkew(120): got %v", got)
	}
}

func TestClaimFirstStringSingleValue(t *testing.T) {
	attrs := map[string]any{"email": "alice@example.com"}
	if got := claimFirstString(attrs, "email"); got != "alice@example.com" {
		t.Fatalf("got %q", got)
	}
}

func TestClaimFirstStringPicksFirstNonEmptyFromArray(t *testing.T) {
	attrs := map[string]any{"role": []any{"", "admin", "user"}}
	if got := claimFirstString(attrs, "role"); got != "admin" {
		t.Fatalf("got %q", got)
	}
}

func TestClaimFirstStringMissingKey(t *testing.T) {
	attrs := map[string]any{"a": "x"}
	if got := claimFirstString(attrs, "missing"); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestClaimFirstStringIgnoresNonStringValues(t *testing.T) {
	attrs := map[string]any{"v": 42}
	if got := claimFirstString(attrs, "v"); got != "" {
		t.Fatalf("got %q", got)
	}
}
