// Package engine ports fusion_base/domain/engine — comparator, blocking,
// rule_matcher, ml_matcher, graph_resolution. Behaviour is 1:1 with the
// Rust source (same scoring formulas, normalization, soundex/metaphone
// tables, levenshtein DP).
package engine

import (
	"strings"
	"unicode"
)

// NormalizeText lowercases and keeps only ASCII alphanumerics.
//
// Mirrors `comparator::normalize_text` exactly: the Rust impl filters
// `is_ascii_alphanumeric()` after `to_lowercase()`. We iterate over runes
// to handle non-ASCII inputs (which get dropped, same as Rust).
func NormalizeText(input string) string {
	var b strings.Builder
	b.Grow(len(input))
	for _, r := range strings.ToLower(input) {
		if r < 128 && (unicode.IsLetter(r) || unicode.IsDigit(r)) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// NormalizePhone keeps only ASCII digits.
func NormalizePhone(input string) string {
	var b strings.Builder
	b.Grow(len(input))
	for _, r := range input {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// LevenshteinSimilarity returns 1 - distance / max(len). Inputs are
// passed through NormalizeText first, identical to the Rust source.
func LevenshteinSimilarity(left, right string) float32 {
	left = NormalizeText(left)
	right = NormalizeText(right)
	if left == "" && right == "" {
		return 1.0
	}
	leftRunes := []rune(left)
	rightRunes := []rune(right)
	prev := make([]int, len(rightRunes)+1)
	curr := make([]int, len(rightRunes)+1)
	for i := 0; i <= len(rightRunes); i++ {
		prev[i] = i
	}
	for li, lc := range leftRunes {
		curr[0] = li + 1
		for ri, rc := range rightRunes {
			cost := 0
			if lc != rc {
				cost = 1
			}
			ins := curr[ri] + 1
			del := prev[ri+1] + 1
			sub := prev[ri] + cost
			curr[ri+1] = minInt(ins, minInt(del, sub))
		}
		prev, curr = curr, prev
	}
	distance := prev[len(rightRunes)]
	maxLen := maxInt(len(leftRunes), len(rightRunes))
	if maxLen < 1 {
		maxLen = 1
	}
	score := 1.0 - float32(distance)/float32(maxLen)
	return clamp01(score)
}

// JaroWinklerSimilarity ports `comparator::jaro_winkler_similarity`.
func JaroWinklerSimilarity(left, right string) float32 {
	left = NormalizeText(left)
	right = NormalizeText(right)
	if left == right {
		return 1.0
	}
	if left == "" || right == "" {
		return 0.0
	}

	leftRunes := []rune(left)
	rightRunes := []rune(right)

	matchDistance := maxInt(len(leftRunes), len(rightRunes)) / 2
	if matchDistance > 0 {
		matchDistance--
	}

	leftMatches := make([]bool, len(leftRunes))
	rightMatches := make([]bool, len(rightRunes))
	matches := 0

	for li, lc := range leftRunes {
		start := li - matchDistance
		if start < 0 {
			start = 0
		}
		end := li + matchDistance + 1
		if end > len(rightRunes) {
			end = len(rightRunes)
		}
		for ri := start; ri < end; ri++ {
			if rightMatches[ri] || lc != rightRunes[ri] {
				continue
			}
			leftMatches[li] = true
			rightMatches[ri] = true
			matches++
			break
		}
	}

	if matches == 0 {
		return 0.0
	}

	transpositions := 0
	rightCursor := 0
	for li, matched := range leftMatches {
		if !matched {
			continue
		}
		for rightCursor < len(rightMatches) && !rightMatches[rightCursor] {
			rightCursor++
		}
		if rightCursor < len(rightRunes) && leftRunes[li] != rightRunes[rightCursor] {
			transpositions++
		}
		rightCursor++
	}

	matchesF := float32(matches)
	jaro := (matchesF/float32(len(leftRunes)) +
		matchesF/float32(len(rightRunes)) +
		(matchesF-float32(transpositions)/2.0)/matchesF) / 3.0

	commonPrefix := 0
	for i := 0; i < minInt(len(leftRunes), len(rightRunes)) && i < 4; i++ {
		if leftRunes[i] != rightRunes[i] {
			break
		}
		commonPrefix++
	}

	return clamp01(jaro + float32(commonPrefix)*0.1*(1.0-jaro))
}

// Soundex returns the 4-character Soundex code as in the Rust source.
func Soundex(input string) string {
	normalized := NormalizeText(input)
	if normalized == "" {
		return ""
	}
	runes := []rune(normalized)
	out := strings.Builder{}
	out.Grow(4)
	out.WriteRune(unicode.ToUpper(runes[0]))
	previousCode := mapSoundex(runes[0])
	for _, r := range runes[1:] {
		code := mapSoundex(r)
		if code != '0' && code != previousCode {
			out.WriteRune(code)
		}
		previousCode = code
		if out.Len() == 4 {
			break
		}
	}
	for out.Len() < 4 {
		out.WriteRune('0')
	}
	return out.String()
}

func mapSoundex(c rune) rune {
	switch c {
	case 'b', 'f', 'p', 'v':
		return '1'
	case 'c', 'g', 'j', 'k', 'q', 's', 'x', 'z':
		return '2'
	case 'd', 't':
		return '3'
	case 'l':
		return '4'
	case 'm', 'n':
		return '5'
	case 'r':
		return '6'
	default:
		return '0'
	}
}

// Metaphone returns the simplified metaphone code used by the Rust source.
func Metaphone(input string) string {
	normalized := NormalizeText(input)
	if normalized == "" {
		return ""
	}
	out := strings.Builder{}
	var previous rune
	for index, c := range normalized {
		if index > 0 && strings.ContainsRune("aeiou", c) {
			continue
		}
		var mapped rune
		switch c {
		case 'b', 'f', 'p', 'v':
			mapped = 'B'
		case 'c', 'g', 'j', 'k', 'q':
			mapped = 'K'
		case 's', 'x', 'z':
			mapped = 'S'
		case 'd', 't':
			mapped = 'T'
		case 'l':
			mapped = 'L'
		case 'm', 'n':
			mapped = 'N'
		case 'r':
			mapped = 'R'
		case 'h', 'w':
			continue
		default:
			mapped = unicode.ToUpper(c)
		}
		if mapped != previous {
			out.WriteRune(mapped)
		}
		previous = mapped
	}
	return out.String()
}

// CompareValues mirrors `comparator::compare_values` — returns 0..1.
func CompareValues(comparator, left, right string) float32 {
	switch comparator {
	case "exact":
		if NormalizeText(left) == NormalizeText(right) {
			return 1.0
		}
		return 0.0
	case "email_exact":
		if strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right)) {
			return 1.0
		}
		return 0.0
	case "phone_exact":
		if NormalizePhone(left) == NormalizePhone(right) {
			return 1.0
		}
		return 0.0
	case "levenshtein":
		return LevenshteinSimilarity(left, right)
	case "jaro_winkler":
		return JaroWinklerSimilarity(left, right)
	case "soundex":
		if Soundex(left) == Soundex(right) {
			return 1.0
		}
		return 0.0
	case "metaphone":
		if Metaphone(left) == Metaphone(right) {
			return 1.0
		}
		return 0.0
	case "phonetic":
		var soundex, metaphone float32
		if Soundex(left) == Soundex(right) {
			soundex = 1.0
		}
		if Metaphone(left) == Metaphone(right) {
			metaphone = 1.0
		}
		return clamp01((soundex + metaphone) / 2.0)
	default:
		lev := LevenshteinSimilarity(left, right)
		jw := JaroWinklerSimilarity(left, right)
		score := lev
		if jw > score {
			score = jw
		}
		return clamp01(score)
	}
}

func clamp01(v float32) float32 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
