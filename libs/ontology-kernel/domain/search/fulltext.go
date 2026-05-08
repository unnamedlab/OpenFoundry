// Lexical (token + exact-match) full-text scorer for ontology search.
//
// Mirrors `libs/ontology-kernel/src/domain/search/fulltext.rs`.
// Pure logic, no IO. Used by the hybrid orchestrator in
// [search.go] as the lexical half of the score.

package search

import (
	"strings"
	"unicode"
)

// Tokenize mirrors `pub fn tokenize`. Splits on any character that
// is not alphanumeric, `_`, or `-`, drops empty fragments, and
// lower-cases each surviving token. Uses Go's `unicode.IsLetter` +
// `unicode.IsDigit` so the alphanumeric predicate matches Rust's
// `char::is_alphanumeric` across every Unicode script (Hebrew,
// Arabic letters, Devanagari, Thai, Hangul, Latin Extended Additional,
// …) — the previous hand-rolled predicate covered only Latin-Extended
// + Greek + Cyrillic + CJK + Hiragana/Katakana + Arabic-Indic digits,
// which would tokenize non-Latin queries differently between Rust and
// Go.
func Tokenize(input string) []string {
	out := []string{}
	current := strings.Builder{}
	flush := func() {
		if current.Len() > 0 {
			out = append(out, strings.ToLower(current.String()))
			current.Reset()
		}
	}
	for _, r := range input {
		if isTokenChar(r) {
			current.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

// isTokenChar matches Rust:
//
//	|c: char| !c.is_alphanumeric() && c != '_' && c != '-'
//
// inverted — return true when the character SHOULD stay inside the
// current token. Rust's `is_alphanumeric` is the union of:
//
//   - Alphabetic property: Letter (L) ∪ Letter_Number (Nl) ∪
//     Other_Alphabetic (the property that pulls in script-specific
//     vowel marks like Thai U+0E31, Arabic combining marks, …),
//   - Numeric: Number (N), which `unicode.IsNumber` covers (Nd, Nl, No).
//
// Go's `unicode.IsLetter` only covers L (Lu, Ll, Lt, Lm, Lo), so a
// Thai word like "สวัสดี" — whose vowel marks are Mn-with-
// Other_Alphabetic — would split mid-token without `Other_Alphabetic`.
// Adding `unicode.In(r, unicode.Other_Alphabetic)` plus
// `unicode.IsNumber` brings the predicate in line with Rust across
// every script we exercise.
func isTokenChar(r rune) bool {
	if r == '_' || r == '-' {
		return true
	}
	if unicode.IsLetter(r) || unicode.IsNumber(r) {
		return true
	}
	return unicode.In(r, unicode.Other_Alphabetic)
}

// LexicalScore mirrors `pub fn score` from `fulltext.rs`. Returns
// a [0, ~1.5] lexical score over (title, body) given the query —
// see the Rust comments for the coverage + exact-match weighting.
//
// (Renamed from `Score` because the Go port flattens
// `domain::search::{fulltext, semantic}` into a single package; the
// equivalent semantic.rs entry point is [SemanticScore].)
func LexicalScore(query, title, body string) float32 {
	queryTokens := Tokenize(query)
	if len(queryTokens) == 0 {
		return 0
	}
	titleTokens := Tokenize(title)
	bodyTokens := Tokenize(body)

	querySet := stringSet(queryTokens)
	titleSet := stringSet(titleTokens)
	bodySet := stringSet(bodyTokens)

	titleHits := float32(0)
	bodyHits := float32(0)
	for token := range querySet {
		if titleSet[token] {
			titleHits++
		}
		if bodySet[token] {
			bodyHits++
		}
	}
	denom := float32(len(querySet))
	if denom < 1 {
		denom = 1
	}
	coverage := (titleHits*1.5 + bodyHits) / denom

	loweredQuery := strings.ToLower(strings.TrimSpace(query))
	loweredTitle := strings.ToLower(title)
	loweredBody := strings.ToLower(body)

	exactTitle := float32(0)
	if loweredQuery != "" && strings.Contains(loweredTitle, loweredQuery) {
		exactTitle = 0.35
	}
	exactBody := float32(0)
	if loweredQuery != "" && strings.Contains(loweredBody, loweredQuery) {
		exactBody = 0.15
	}

	scaled := coverage / 2.5
	if scaled > 1.0 {
		scaled = 1.0
	}
	return scaled + exactTitle + exactBody
}

func stringSet(values []string) map[string]bool {
	out := make(map[string]bool, len(values))
	for _, v := range values {
		out[v] = true
	}
	return out
}
