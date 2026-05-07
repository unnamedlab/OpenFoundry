package engine

import (
	"math"
	"testing"
)

const epsilon = 1e-4

func approxEqual(a, b float32) bool {
	return float32(math.Abs(float64(a-b))) <= epsilon
}

func TestNormalizeText(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"":               "",
		"Hello, World!":  "helloworld",
		"abc-123":        "abc123",
		"  spaced  ":     "spaced",
		"ÁccentS":        "ccents",
		"+1 (415) 555-0100": "14155550100",
	}
	for in, want := range cases {
		if got := NormalizeText(in); got != want {
			t.Fatalf("NormalizeText(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizePhone(t *testing.T) {
	t.Parallel()
	if got := NormalizePhone("+1 (415) 555-0100"); got != "14155550100" {
		t.Fatalf("got %q", got)
	}
	if got := NormalizePhone("no digits"); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestLevenshteinSimilarity(t *testing.T) {
	t.Parallel()
	if got := LevenshteinSimilarity("", ""); !approxEqual(got, 1.0) {
		t.Fatalf("empty/empty got %v", got)
	}
	if got := LevenshteinSimilarity("abc", "abc"); !approxEqual(got, 1.0) {
		t.Fatalf("abc/abc got %v", got)
	}
	// kitten/sitting → distance 3, max len 7, similarity 4/7 ≈ 0.5714 (post-normalize same).
	got := LevenshteinSimilarity("kitten", "sitting")
	want := float32(1.0 - 3.0/7.0)
	if !approxEqual(got, want) {
		t.Fatalf("kitten/sitting got %v want %v", got, want)
	}
	if got := LevenshteinSimilarity("hello", "yellow"); got <= 0 || got >= 1 {
		t.Fatalf("partial match got %v", got)
	}
}

func TestJaroWinklerSimilarity(t *testing.T) {
	t.Parallel()
	if got := JaroWinklerSimilarity("MARTHA", "MARTHA"); !approxEqual(got, 1.0) {
		t.Fatalf("equal got %v", got)
	}
	if got := JaroWinklerSimilarity("", "abc"); !approxEqual(got, 0.0) {
		t.Fatalf("empty got %v", got)
	}
	// Classic example MARTHA/MARHTA → ~0.961 (jaro 0.944, +0.04 prefix bonus).
	got := JaroWinklerSimilarity("MARTHA", "MARHTA")
	if got <= 0.94 || got > 0.97 {
		t.Fatalf("MARTHA/MARHTA got %v", got)
	}
}

func TestSoundex(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"":         "",
		"Robert":   "R163",
		"Rupert":   "R163",
		"Ashcraft": "A226",
		"Tymczak":  "T522",
		"Honeyman": "H555",
	}
	for in, want := range cases {
		if got := Soundex(in); got != want {
			t.Fatalf("Soundex(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMetaphone(t *testing.T) {
	t.Parallel()
	if got := Metaphone(""); got != "" {
		t.Fatalf("empty got %q", got)
	}
	if got := Metaphone("smith"); got != "SNT" {
		t.Fatalf("smith got %q want SNT", got)
	}
	// Note: the Rust simplified metaphone does NOT treat 'y' as a vowel,
	// so smyth → SNYT (not SNT). Verified against the source `match`.
	if got := Metaphone("smyth"); got != "SNYT" {
		t.Fatalf("smyth got %q want SNYT", got)
	}
}

func TestCompareValuesExact(t *testing.T) {
	t.Parallel()
	if CompareValues("exact", "Hello", "hello") != 1.0 {
		t.Fatal("normalize-equal exact should be 1")
	}
	if CompareValues("exact", "Hello", "Hello!") != 1.0 {
		t.Fatal("punctuation should normalize away")
	}
	if CompareValues("exact", "abc", "xyz") != 0.0 {
		t.Fatal("different should be 0")
	}
}

func TestCompareValuesEmailExact(t *testing.T) {
	t.Parallel()
	if CompareValues("email_exact", "  Foo@Bar.com  ", "foo@bar.com") != 1.0 {
		t.Fatal("case-insensitive trim should match")
	}
	if CompareValues("email_exact", "a@b.com", "a@c.com") != 0.0 {
		t.Fatal("different emails")
	}
}

func TestCompareValuesPhoneExact(t *testing.T) {
	t.Parallel()
	// Same digit sequence, different formatting → match.
	if CompareValues("phone_exact", "+1 (415) 555-0100", "+1-415-555-0100") != 1.0 {
		t.Fatal("identical digit stream should match")
	}
	// Differing country prefix → digit streams differ → no match (parity with Rust).
	if CompareValues("phone_exact", "+1 (415) 555-0100", "4155550100") != 0.0 {
		t.Fatal("differing length digit streams should not match")
	}
}

func TestCompareValuesFuzzyTakesMaxOfLevAndJW(t *testing.T) {
	t.Parallel()
	score := CompareValues("fuzzy", "Acme Logistics", "ACME Logstics")
	if score < 0.7 {
		t.Fatalf("fuzzy similar names got %v, expected > 0.7", score)
	}
}

func TestCompareValuesPhonetic(t *testing.T) {
	t.Parallel()
	// soundex(smith)=soundex(smyth)=S530 → 1.
	// metaphone(smith)="SNT", metaphone(smyth)="SNYT" → 0.
	// avg = 0.5 (parity with Rust simplified metaphone).
	if got := CompareValues("phonetic", "smith", "smyth"); got != 0.5 {
		t.Fatalf("phonetic mixed got %v want 0.5", got)
	}
	// Two strings that match both soundex and metaphone → 1.
	if got := CompareValues("phonetic", "robert", "robert"); got != 1.0 {
		t.Fatalf("phonetic full match got %v", got)
	}
	// totally different → 0.
	if got := CompareValues("phonetic", "abc", "xyz"); got != 0.0 {
		t.Fatalf("phonetic mismatch got %v", got)
	}
}
