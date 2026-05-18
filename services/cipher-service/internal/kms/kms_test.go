package kms

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

func randomKEK(t *testing.T) []byte {
	t.Helper()
	kek := make([]byte, 32)
	if _, err := rand.Read(kek); err != nil {
		t.Fatalf("rand: %v", err)
	}
	return kek
}

func TestLocalKMS_Roundtrip(t *testing.T) {
	t.Parallel()
	kms, err := NewLocalKMS(randomKEK(t), "local:test")
	if err != nil {
		t.Fatalf("NewLocalKMS: %v", err)
	}
	dek := make([]byte, 32)
	if _, err := rand.Read(dek); err != nil {
		t.Fatalf("rand: %v", err)
	}

	wrapped, err := kms.Wrap(dek)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if bytes.Equal(wrapped, dek) {
		t.Fatal("wrapped material must not equal plaintext DEK")
	}
	got, err := kms.Unwrap(wrapped)
	if err != nil {
		t.Fatalf("Unwrap: %v", err)
	}
	if !bytes.Equal(got, dek) {
		t.Fatal("roundtrip mismatch")
	}
	if kms.Ref() != "local:test" {
		t.Fatalf("Ref = %q, want local:test", kms.Ref())
	}
}

func TestLocalKMS_BadKEKSize(t *testing.T) {
	t.Parallel()
	_, err := NewLocalKMS(make([]byte, 16), "")
	if !errors.Is(err, ErrLocalKEKInvalid) {
		t.Fatalf("expected ErrLocalKEKInvalid, got %v", err)
	}
}

func TestLocalKMS_Unwrap_Tampered(t *testing.T) {
	t.Parallel()
	kms, err := NewLocalKMS(randomKEK(t), "")
	if err != nil {
		t.Fatalf("NewLocalKMS: %v", err)
	}
	dek := make([]byte, 32)
	if _, err := rand.Read(dek); err != nil {
		t.Fatalf("rand: %v", err)
	}
	wrapped, err := kms.Wrap(dek)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	wrapped[len(wrapped)-1] ^= 0x01
	_, err = kms.Unwrap(wrapped)
	if !errors.Is(err, ErrWrappedMaterialInvalid) {
		t.Fatalf("expected ErrWrappedMaterialInvalid, got %v", err)
	}
}

func TestLocalKMS_Unwrap_TooShort(t *testing.T) {
	t.Parallel()
	kms, err := NewLocalKMS(randomKEK(t), "")
	if err != nil {
		t.Fatalf("NewLocalKMS: %v", err)
	}
	_, err = kms.Unwrap([]byte{0x00, 0x01})
	if !errors.Is(err, ErrWrappedMaterialInvalid) {
		t.Fatalf("expected ErrWrappedMaterialInvalid, got %v", err)
	}
}

func TestLocalKMS_Unwrap_WrongKEK(t *testing.T) {
	t.Parallel()
	a, err := NewLocalKMS(randomKEK(t), "a")
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewLocalKMS(randomKEK(t), "b")
	if err != nil {
		t.Fatal(err)
	}
	dek := make([]byte, 32)
	if _, err := rand.Read(dek); err != nil {
		t.Fatal(err)
	}
	wrapped, err := a.Wrap(dek)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Unwrap(wrapped); !errors.Is(err, ErrWrappedMaterialInvalid) {
		t.Fatalf("cross-KMS unwrap must fail with ErrWrappedMaterialInvalid, got %v", err)
	}
}

func TestNewLocalKMSFromEnv_Missing(t *testing.T) {
	t.Setenv(LocalKEKEnv, "")
	_, err := NewLocalKMSFromEnv()
	if !errors.Is(err, ErrLocalKEKMissing) {
		t.Fatalf("expected ErrLocalKEKMissing, got %v", err)
	}
}

func TestNewLocalKMSFromEnv_BadHex(t *testing.T) {
	t.Setenv(LocalKEKEnv, "not-hex-just-letters")
	_, err := NewLocalKMSFromEnv()
	if !errors.Is(err, ErrLocalKEKInvalid) {
		t.Fatalf("expected ErrLocalKEKInvalid, got %v", err)
	}
}

func TestNewLocalKMSFromEnv_OK(t *testing.T) {
	t.Setenv(LocalKEKEnv, hex.EncodeToString(randomKEK(t)))
	kms, err := NewLocalKMSFromEnv()
	if err != nil {
		t.Fatalf("NewLocalKMSFromEnv: %v", err)
	}
	if !strings.HasPrefix(kms.Ref(), "local:env:") {
		t.Fatalf("Ref = %q, want local:env: prefix", kms.Ref())
	}
}

func TestAWSKMSStub_ReturnsNotImplemented(t *testing.T) {
	t.Parallel()
	stub := NewAWSKMSStub("arn:aws:kms:us-east-1:123456789012:key/abc")
	if _, err := stub.Wrap([]byte{0x00}); !errors.Is(err, ErrAWSNotImplemented) {
		t.Fatalf("Wrap error = %v, want ErrAWSNotImplemented", err)
	}
	if _, err := stub.Unwrap([]byte{0x00}); !errors.Is(err, ErrAWSNotImplemented) {
		t.Fatalf("Unwrap error = %v, want ErrAWSNotImplemented", err)
	}
	if !strings.HasPrefix(stub.Ref(), "aws:kms:") {
		t.Fatalf("Ref must start with aws:kms:")
	}
}
