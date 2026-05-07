package llm

import (
	"strings"

	"github.com/openfoundry/openfoundry-go/libs/ai-kernel-go/models"
)

// toxicTerms is the inline blocklist used by EvaluateText. Pinned
// verbatim from Rust src/domain/llm/guardrails.rs::TOXIC_TERMS so
// drift across the two implementations is a test failure.
var toxicTerms = []string{"idiot", "moron", "stupid", "hate", "kill", "attack"}

// EvaluateText returns a GuardrailVerdict for the given content.
// Rules (verbatim from Rust):
//   - email-shaped tokens → flag pii_email + replace with [redacted-email]
//   - tokens with 9+ digits → flag pii_phone + replace with [redacted-number]
//   - lowercased token containing both "ignore" and "instructions" →
//     flag prompt_injection (high)
//   - lowercased token containing any toxic term → flag toxicity (high)
// Verdict: blocked if any high-severity flag exists that is NOT
// pii_email or pii_phone; redacted_text joins the sanitised tokens.
func EvaluateText(content string) models.GuardrailVerdict {
	flags := []models.GuardrailFlag{}
	sanitized := []string{}

	for _, token := range strings.Fields(content) {
		lowered := strings.ToLower(token)
		numericCount := 0
		for _, ch := range token {
			if ch >= '0' && ch <= '9' {
				numericCount++
			}
		}

		if looksLikeEmail(token) {
			flags = append(flags, models.GuardrailFlag{
				Kind: "pii_email", Severity: "medium", Excerpt: token,
			})
			sanitized = append(sanitized, "[redacted-email]")
			continue
		}
		if numericCount >= 9 {
			flags = append(flags, models.GuardrailFlag{
				Kind: "pii_phone", Severity: "medium", Excerpt: token,
			})
			sanitized = append(sanitized, "[redacted-number]")
			continue
		}

		if strings.Contains(lowered, "ignore") && strings.Contains(lowered, "instructions") {
			flags = append(flags, models.GuardrailFlag{
				Kind: "prompt_injection", Severity: "high", Excerpt: token,
			})
		}
		for _, term := range toxicTerms {
			if strings.Contains(lowered, term) {
				flags = append(flags, models.GuardrailFlag{
					Kind: "toxicity", Severity: "high", Excerpt: token,
				})
				break
			}
		}
		sanitized = append(sanitized, token)
	}

	blocked := false
	for _, f := range flags {
		if f.Severity == "high" && f.Kind != "pii_email" && f.Kind != "pii_phone" {
			blocked = true
			break
		}
	}

	status := "redacted"
	switch {
	case blocked:
		status = "blocked"
	case len(flags) == 0:
		status = "passed"
	}

	return models.GuardrailVerdict{
		Status:       status,
		RedactedText: strings.Join(sanitized, " "),
		Blocked:      blocked,
		Flags:        flags,
	}
}

func looksLikeEmail(token string) bool {
	parts := strings.Split(token, "@")
	if len(parts) != 2 {
		return false
	}
	return strings.Contains(parts[1], ".")
}
