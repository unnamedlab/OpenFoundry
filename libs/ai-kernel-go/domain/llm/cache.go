// Package llm hosts the pure-logic LLM gateway, guardrail evaluator,
// semantic cache helpers, and prompt-template interpolator. Driver
// wiring (HTTP fan-out, provider call) lives in handlers / runtime.
package llm

import (
	"math"
	"strings"
	"unicode"
)

// NormalizeText is the canonical normalisation used across cache,
// guardrail, and template paths. Mirrors Rust normalize_text:
// lowercase, replace non-alphanumeric/whitespace with space,
// collapse whitespace runs.
func NormalizeText(input string) string {
	var b strings.Builder
	b.Grow(len(input))
	for _, r := range strings.ToLower(input) {
		switch {
		case unicode.IsDigit(r) && r <= 0x7F,
			(r >= 'a' && r <= 'z'),
			unicode.IsSpace(r):
			b.WriteRune(r)
		default:
			b.WriteRune(' ')
		}
	}
	// Collapse whitespace runs.
	return strings.Join(strings.Fields(b.String()), " ")
}

// Fingerprint produces a 16-dim normalised vector of the input.
// Used as a coarse semantic-cache lookup signal.
func Fingerprint(input string) []float32 {
	normalized := NormalizeText(input)
	vector := make([]float32, 16)
	vectorLen := len(vector)
	for i, ch := range []byte(normalized) {
		vector[i%vectorLen] += float32(ch) / 255.0
	}
	var sumSq float32
	for _, v := range vector {
		sumSq += v * v
	}
	magnitude := float32(math.Sqrt(float64(sumSq)))
	if magnitude > 0 {
		for i := range vector {
			vector[i] /= magnitude
		}
	}
	return vector
}

// CosineSimilarity returns the cosine of the angle between two vectors.
// Returns 0 for empty / zero-magnitude inputs.
func CosineSimilarity(left, right []float32) float32 {
	if len(left) == 0 || len(right) == 0 {
		return 0
	}
	length := len(left)
	if len(right) < length {
		length = len(right)
	}
	var dot, lm, rm float32
	for i := 0; i < length; i++ {
		dot += left[i] * right[i]
		lm += left[i] * left[i]
		rm += right[i] * right[i]
	}
	if lm == 0 || rm == 0 {
		return 0
	}
	return dot / float32(math.Sqrt(float64(lm))*math.Sqrt(float64(rm)))
}

// CacheKey is the string identifier used by the semantic cache layer.
func CacheKey(kind, input string) string {
	return kind + ":" + NormalizeText(input)
}
