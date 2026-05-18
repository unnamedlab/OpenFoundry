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
	if Algorithm("AES_128_CBC").Valid() {
		t.Fatal("legacy algorithms must not be accepted")
	}
}

func TestStatus_Valid(t *testing.T) {
	t.Parallel()
	for _, s := range []Status{StatusActive, StatusRotating, StatusRetired} {
		if !s.Valid() {
			t.Fatalf("%s must be valid", s)
		}
	}
	if Status("revoked").Valid() {
		t.Fatal("future statuses must opt-in explicitly")
	}
}
