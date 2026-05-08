package audit

import "testing"

// Mirrors the Rust unit `fingerprint_is_stable_and_case_insensitive`.
func TestFingerprintStableAndCaseInsensitive(t *testing.T) {
	t.Parallel()
	a := Fingerprint("SELECT 1")
	b := Fingerprint("select 1")
	c := Fingerprint("  select   1  ")
	if len(a) != 16 {
		t.Fatalf("expected 16-hex digest, got %d", len(a))
	}
	if a != b {
		t.Fatalf("expected case-insensitive equality: %s vs %s", a, b)
	}
	if a == c {
		t.Fatalf("internal whitespace must change the fingerprint: %s == %s", a, c)
	}
}

// Mirrors `different_statements_produce_different_fingerprints`.
func TestFingerprintDifferentStatements(t *testing.T) {
	t.Parallel()
	if Fingerprint("SELECT 1") == Fingerprint("SELECT 2") {
		t.Fatal("different statements must produce different fingerprints")
	}
}

// Sanity: empty SQL still produces a deterministic 16-hex digest
// (the offset basis itself).
func TestFingerprintEmpty(t *testing.T) {
	t.Parallel()
	got := Fingerprint("")
	if got != "cbf29ce484222325" {
		t.Fatalf("FNV-1a-64 offset basis mismatch: %s", got)
	}
}
