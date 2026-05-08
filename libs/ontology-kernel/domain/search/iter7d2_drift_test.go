// Tests anchoring 2 wire-compat drifts caught by the iter 7d₂ audit
// against `libs/ontology-kernel/src/domain/search/{fulltext,semantic}.rs`.

package search

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// libs/ontology-kernel/src/domain/search/fulltext.rs `tokenize` —
// Rust's `char::is_alphanumeric` accepts every Unicode Letter (L) +
// Decimal_Number (Nd). The pre-fix Go port hand-rolled a tiny subset
// (Latin-Extended + Greek + Cyrillic + CJK + Hiragana/Katakana +
// Arabic-Indic digits) and silently dropped the rest. Anchor tokens
// from scripts the previous predicate did NOT cover so a regression
// re-introducing the manual list is caught here.
func TestTokenizeCoversNonLatinScripts(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  []string
	}{
		// Hebrew (U+0590..U+05FF) — pre-fix Go would drop entirely.
		{name: "hebrew", input: "שלום world", want: []string{"שלום", "world"}},
		// Arabic letters (U+0600..U+06FF) — pre-fix Go captured digits
		// but not letters. Lower-casing is a no-op for Arabic.
		{name: "arabic", input: "مرحبا alpha", want: []string{"مرحبا", "alpha"}},
		// Devanagari (U+0900..U+097F) — VIRAMA U+094D is Mn without
		// Other_Alphabetic, so it splits the word in BOTH Rust and Go.
		// The post-fix Go matches that exactly. The vowel sign U+0947
		// IS in Other_Alphabetic and stays inside its token.
		{name: "devanagari", input: "नमस्ते beta", want: []string{"नमस", "ते", "beta"}},
		// Thai (U+0E00..U+0E7F) — combining marks U+0E31 / U+0E35 are
		// Other_Alphabetic, so the word is one token in Rust. Pre-fix
		// Go dropped them and split mid-syllable.
		{name: "thai", input: "สวัสดี gamma", want: []string{"สวัสดี", "gamma"}},
		// Korean Hangul (U+AC00..U+D7A3) — pre-fix Go dropped entirely.
		{name: "hangul", input: "안녕 delta", want: []string{"안녕", "delta"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Tokenize(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

// libs/ontology-kernel/src/domain/search/fulltext.rs — common ASCII
// tokenization invariants stay intact after the unicode predicate
// switch. Anchored separately so a regression in the basic shape
// surfaces first.
func TestTokenizeASCIIInvariants(t *testing.T) {
	assert.Equal(t, []string{"customer-health", "review", "2024_q1"},
		Tokenize("Customer-health REVIEW.2024_q1"))
	assert.Empty(t, Tokenize("   . , !"))
	// Underscore + dash kept inside the token; everything else splits.
	assert.Equal(t, []string{"a-b", "c_d"}, Tokenize("a-b/c_d"))
}

// libs/ontology-kernel/src/domain/search/semantic.rs
// `parse_embedding` / `embed_ollama` — when `value_array_to_f32`
// hits a non-numeric entry it returns `Vec::new()` and the
// surrounding `.filter(|emb| !emb.is_empty())` surfaces
// "embedding payload did not include an embedding vector".
//
// The pre-fix Go port returned a typed `non-numeric value in
// embedding` error from `valueArrayToFloat32` directly, which
// drifted the user-visible message. Now `valueArrayToFloat32`
// silently returns an empty slice and the caller's empty-check
// produces the same Rust-canonical message.
func TestValueArrayToFloat32SilentlyEmptiesOnNonNumeric(t *testing.T) {
	out, err := valueArrayToFloat32([]any{1.0, 2.0, "boom", 4.0})
	require.NoError(t, err)
	assert.Empty(t, out, "non-numeric entry must collapse the slice to empty (matches Rust Vec::new())")

	// Sanity: an all-numeric array passes through.
	out, err = valueArrayToFloat32([]any{1.0, 2.0, 3.0})
	require.NoError(t, err)
	assert.Len(t, out, 3)
}

// libs/ontology-kernel/src/domain/search/semantic.rs — the
// caller-side error message must be the canonical
// "embedding payload did not include an embedding vector" string
// regardless of which JSON branch the bad input took. Anchored so
// a regression that surfaces a different error body fails here.
func TestParseOpenAIEmbeddingNonNumericErrorMatchesRustCanonical(t *testing.T) {
	payload := map[string]any{
		"data": []any{
			map[string]any{
				"embedding": []any{0.1, "boom", 0.3},
			},
		},
	}
	_, err := parseOpenAIEmbedding(payload)
	require.Error(t, err)
	if !strings.Contains(err.Error(), "embedding payload did not include an embedding vector") {
		t.Fatalf("error drift: got %q, want canonical Rust message", err.Error())
	}
}
