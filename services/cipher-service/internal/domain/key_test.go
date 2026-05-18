package domain

import "testing"

// TestStatus_Transitions pins the encrypt/decrypt gating that the rest
// of the service relies on. Updating this table without updating the
// corresponding handler responses is a wire-visible regression.
func TestStatus_Transitions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		status     Status
		canEncrypt bool
		canDecrypt bool
	}{
		{StatusActive, true, true},
		{StatusRotating, true, true},
		{StatusRetired, false, true},
		{StatusRevoked, false, false},
	}
	for _, tc := range cases {
		k := &CipherKey{Status: tc.status}
		if got := k.CanEncrypt(); got != tc.canEncrypt {
			t.Errorf("%s: CanEncrypt = %v, want %v", tc.status, got, tc.canEncrypt)
		}
		if got := k.CanDecrypt(); got != tc.canDecrypt {
			t.Errorf("%s: CanDecrypt = %v, want %v", tc.status, got, tc.canDecrypt)
		}
	}
}

func TestAlgorithm_Valid(t *testing.T) {
	t.Parallel()
	if !AlgorithmAES256GCM.Valid() {
		t.Fatal("AES_256_GCM must be valid")
	}
	if !AlgorithmAES256GCMSIV.Valid() {
		t.Fatal("AES_256_GCM_SIV must be valid (reserved for Milestone B)")
	}
	if !AlgorithmSHA256.Valid() || !AlgorithmSHA512.Valid() {
		t.Fatal("pepper-backed SHA algorithms must be registry-valid")
	}
	if Algorithm("AES_128_CBC").Valid() {
		t.Fatal("legacy algorithms must not be accepted")
	}
	if !AlgorithmAES256GCM.SupportsCipherKeyResource() || !AlgorithmAES256GCMSIV.SupportsCipherKeyResource() || !AlgorithmAES256SIV.SupportsCipherKeyResource() {
		t.Fatal("AEAD algorithms must support cipher key resources")
	}
	if AlgorithmSHA256.SupportsCipherKeyResource() || AlgorithmSHA512.SupportsCipherKeyResource() {
		t.Fatal("hash algorithms must wait for pepper-backed resources")
	}
}

func TestStatus_Valid(t *testing.T) {
	t.Parallel()
	for _, s := range []Status{StatusActive, StatusRotating, StatusRetired, StatusRevoked} {
		if !s.Valid() {
			t.Fatalf("%s must be valid", s)
		}
	}
	if Status("destroyed").Valid() {
		t.Fatal("future statuses must opt-in explicitly")
	}
}

func TestBuiltInAlgorithms_Metadata(t *testing.T) {
	t.Parallel()
	items := BuiltInAlgorithms()
	if len(items) != 5 {
		t.Fatalf("algorithm count = %d, want 5", len(items))
	}
	items[0].ID = "MUTATED"
	again := BuiltInAlgorithms()
	if again[0].ID != AlgorithmAES256GCMSIV {
		t.Fatalf("registry must return defensive copies, got %q", again[0].ID)
	}
	desc, ok := AlgorithmAES256GCMSIV.Descriptor()
	if !ok {
		t.Fatal("GCM-SIV descriptor missing")
	}
	if desc.KeyLengthBytes != 32 || desc.OutputEncoding != "base64" || !desc.RecommendedDefault {
		t.Fatalf("unexpected GCM-SIV descriptor: %+v", desc)
	}
	siv, ok := AlgorithmAES256SIV.Descriptor()
	if !ok || !siv.Deterministic || siv.SecurityNotice == "" {
		t.Fatalf("unexpected AES-SIV descriptor: %+v (ok=%v)", siv, ok)
	}
	sha, ok := AlgorithmSHA512.Descriptor()
	if !ok || !sha.PepperRequired || sha.KeyLengthBytes != 64 {
		t.Fatalf("unexpected SHA-512 descriptor: %+v (ok=%v)", sha, ok)
	}
}
