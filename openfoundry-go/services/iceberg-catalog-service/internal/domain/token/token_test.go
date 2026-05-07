package token

import (
	"strings"
	"testing"
)

func TestHashTokenIsDeterministic(t *testing.T) {
	a := Hash("secret")
	b := Hash("secret")
	if a != b {
		t.Fatalf("hash not deterministic: %q vs %q", a, b)
	}
	if len(a) != 64 {
		t.Fatalf("expected 64 hex chars, got %d", len(a))
	}
}

func TestDistinctTokensHaveDistinctHashes(t *testing.T) {
	if Hash("a") == Hash("b") {
		t.Fatalf("hash collision on trivial inputs")
	}
}

func TestMintFormat(t *testing.T) {
	raw, hash, hint, err := Mint()
	if err != nil {
		t.Fatalf("mint: %v", err)
	}
	if !strings.HasPrefix(raw, OftyPrefix) {
		t.Fatalf("raw missing prefix: %q", raw)
	}
	// `ofty_` + 64 hex chars
	if len(raw) != len(OftyPrefix)+64 {
		t.Fatalf("unexpected raw length %d", len(raw))
	}
	if hash != Hash(raw) {
		t.Fatalf("hash mismatch")
	}
	if hint != raw[len(raw)-4:] {
		t.Fatalf("hint mismatch: %q vs %q", hint, raw[len(raw)-4:])
	}
}

func TestHasOftyPrefix(t *testing.T) {
	if !HasOftyPrefix("ofty_abc") {
		t.Fatalf("expected prefix detected")
	}
	if HasOftyPrefix("eyJhbGciOi") {
		t.Fatalf("JWT-shaped string must not be misclassified")
	}
}
