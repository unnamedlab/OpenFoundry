package saml

import (
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
)

// xmlEscape mirrors fn `xml_escape` — stringy XML escape for the
// five XML-canonical entities. Used when stitching attribute
// values into the AuthnRequest XML literal.
func xmlEscape(value string) string {
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range value {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&apos;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// stripAllWhitespace mirrors fn `strip_all_whitespace` — drops every
// Unicode-whitespace rune. Used to normalise base64-armoured X509
// blobs before re-chunking them into PEM lines.
func stripAllWhitespace(value string) string {
	var b strings.Builder
	b.Grow(len(value))
	for _, r := range value {
		if !unicode.IsSpace(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// trimmed mirrors fn `trimmed` — returns nil for empty / whitespace
// values so callers can `if v := trimmed(s); v != nil` to detect
// "value present" vs. "value absent".
func trimmed(value string) *string {
	v := strings.TrimSpace(value)
	if v == "" {
		return nil
	}
	return &v
}

// normalizeCertificatePem mirrors fn `normalize_certificate_pem`.
//
// Accepts either a fully-armoured PEM (`-----BEGIN CERTIFICATE-----`
// header present) — returned trimmed + `\n`-suffixed — or a raw
// base64 blob, which gets re-chunked into 64-character lines and
// wrapped in PEM headers. Matches the Rust impl byte-for-byte so
// the same fixtures round-trip identically.
func normalizeCertificatePem(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", errors.New("provider is missing saml_certificate")
	}
	if strings.Contains(value, "-----BEGIN CERTIFICATE-----") {
		return strings.TrimSpace(value) + "\n", nil
	}

	base64Body := stripAllWhitespace(value)
	if base64Body == "" {
		return "", errors.New("provider is missing saml_certificate")
	}

	var b strings.Builder
	b.WriteString("-----BEGIN CERTIFICATE-----\n")
	for i := 0; i < len(base64Body); i += 64 {
		end := i + 64
		if end > len(base64Body) {
			end = len(base64Body)
		}
		b.WriteString(base64Body[i:end])
		b.WriteByte('\n')
	}
	b.WriteString("-----END CERTIFICATE-----\n")
	return b.String(), nil
}

// parseSamlTime mirrors fn `parse_saml_time` — RFC 3339 parser that
// returns UTC. Rust uses `chrono::DateTime::parse_from_rfc3339`; Go's
// `time.RFC3339Nano` covers the same wire formats.
func parseSamlTime(value, label string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		// Try without nanos — some IdPs emit second-precision.
		t2, err2 := time.Parse(time.RFC3339, value)
		if err2 != nil {
			return time.Time{}, fmt.Errorf("invalid %s: %w", label, err)
		}
		return t2.UTC(), nil
	}
	return t.UTC(), nil
}

// claimFirstString mirrors fn `claim_first_string`.
//
// Returns the first non-empty string for `key` from a parsed
// attribute map. The map is shaped after the Rust serde_json::Value
// hierarchy: a single-string attribute lands as `string`, a
// multi-value attribute lands as `[]any` whose elements are
// strings. Anything else returns "" (signal: not present).
func claimFirstString(attrs map[string]any, key string) string {
	v, ok := attrs[key]
	if !ok {
		return ""
	}
	switch vv := v.(type) {
	case string:
		return vv
	case []any:
		for _, e := range vv {
			if s, ok := e.(string); ok && s != "" {
				return s
			}
		}
	case []string:
		for _, s := range vv {
			if s != "" {
				return s
			}
		}
	}
	return ""
}
