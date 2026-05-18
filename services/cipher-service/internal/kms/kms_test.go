package kms

import (
	"bytes"
	"context"
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

func TestNewAWSKMSClient_MissingKeyARN(t *testing.T) {
	t.Parallel()
	if _, err := NewAWSKMSClient(context.Background(), "us-east-1", "", ""); !errors.Is(err, ErrAWSKeyMissing) {
		t.Fatalf("missing ARN error = %v, want ErrAWSKeyMissing", err)
	}
}

func TestNewAWSKMSClient_UsesEndpointOverrideAndARNRegion(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "test")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "test")
	t.Setenv("AWS_REGION", "")
	keyARN := "arn:aws:kms:us-west-2:123456789012:key/abc"
	kms, err := NewAWSKMSClient(context.Background(), "", keyARN, "http://localhost:4566")
	if err != nil {
		t.Fatalf("NewAWSKMSClient: %v", err)
	}
	client, ok := kms.client.(*awsKMSHTTPClient)
	if !ok {
		t.Fatalf("client type = %T, want *awsKMSHTTPClient", kms.client)
	}
	if client.region != "us-west-2" {
		t.Fatalf("region = %q, want us-west-2", client.region)
	}
	if client.endpoint != "http://localhost:4566" {
		t.Fatalf("endpoint = %q, want endpoint override", client.endpoint)
	}
}

func TestAWSKMSClient_WrapCallsEncrypt(t *testing.T) {
	t.Parallel()
	fake := &fakeAWSKMSClient{encryptOut: []byte("wrapped-dek")}
	kms := newAWSKMSClientWithClient("arn:aws:kms:us-east-1:123456789012:key/abc", fake, 0)
	dek := []byte("plain-dek")

	wrapped, err := kms.Wrap(dek)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	if !bytes.Equal(wrapped, []byte("wrapped-dek")) {
		t.Fatalf("wrapped = %q, want wrapped-dek", wrapped)
	}
	if fake.encryptCalls != 1 {
		t.Fatalf("encrypt calls = %d, want 1", fake.encryptCalls)
	}
	if fake.encryptKeyID != kms.keyARN {
		t.Fatalf("Encrypt KeyID = %q, want %q", fake.encryptKeyID, kms.keyARN)
	}
	if !bytes.Equal(fake.encryptPlaintext, dek) {
		t.Fatal("Encrypt plaintext mismatch")
	}
}

func TestAWSKMSClient_UnwrapCallsDecrypt(t *testing.T) {
	t.Parallel()
	fake := &fakeAWSKMSClient{decryptOut: []byte("plain-dek")}
	kms := newAWSKMSClientWithClient("arn:aws:kms:us-east-1:123456789012:key/abc", fake, 0)
	wrapped := []byte("wrapped-dek")

	plain, err := kms.Unwrap(wrapped)
	if err != nil {
		t.Fatalf("Unwrap: %v", err)
	}
	if !bytes.Equal(plain, []byte("plain-dek")) {
		t.Fatalf("plain = %q, want plain-dek", plain)
	}
	if fake.decryptCalls != 1 {
		t.Fatalf("decrypt calls = %d, want 1", fake.decryptCalls)
	}
	if !bytes.Equal(fake.decryptCiphertext, wrapped) {
		t.Fatal("Decrypt ciphertext mismatch")
	}
}

func TestAWSKMSClient_RoundtripWithFake(t *testing.T) {
	t.Parallel()
	fake := &fakeAWSKMSClient{}
	kms := newAWSKMSClientWithClient("arn:aws:kms:us-east-1:123456789012:key/abc", fake, 0)
	dek := []byte("plain-dek")

	wrapped, err := kms.Wrap(dek)
	if err != nil {
		t.Fatalf("Wrap: %v", err)
	}
	plain, err := kms.Unwrap(wrapped)
	if err != nil {
		t.Fatalf("Unwrap: %v", err)
	}
	if !bytes.Equal(plain, dek) {
		t.Fatalf("roundtrip = %q, want %q", plain, dek)
	}
}

func TestAWSKMSClient_RefStable(t *testing.T) {
	t.Parallel()
	keyARN := "arn:aws:kms:us-east-1:123456789012:key/abc"
	kms := newAWSKMSClientWithClient(keyARN, &fakeAWSKMSClient{}, 0)
	if got, want := kms.Ref(), "aws:kms:"+keyARN; got != want {
		t.Fatalf("Ref = %q, want %q", got, want)
	}
}

type fakeAWSKMSClient struct {
	encryptCalls     int
	encryptKeyID     string
	encryptPlaintext []byte
	encryptOut       []byte
	encryptErr       error

	decryptCalls      int
	decryptCiphertext []byte
	decryptOut        []byte
	decryptErr        error
}

func (f *fakeAWSKMSClient) Encrypt(_ context.Context, in *awsKMSEncryptInput) (*awsKMSEncryptOutput, error) {
	f.encryptCalls++
	f.encryptKeyID = in.KeyID
	f.encryptPlaintext = append([]byte(nil), in.Plaintext...)
	if f.encryptErr != nil {
		return nil, f.encryptErr
	}
	out := f.encryptOut
	if out == nil {
		out = append([]byte("fake-wrapped:"), in.Plaintext...)
	}
	return &awsKMSEncryptOutput{CiphertextBlob: append([]byte(nil), out...)}, nil
}

func (f *fakeAWSKMSClient) Decrypt(_ context.Context, in *awsKMSDecryptInput) (*awsKMSDecryptOutput, error) {
	f.decryptCalls++
	f.decryptCiphertext = append([]byte(nil), in.CiphertextBlob...)
	if f.decryptErr != nil {
		return nil, f.decryptErr
	}
	out := f.decryptOut
	if out == nil {
		out = bytes.TrimPrefix(in.CiphertextBlob, []byte("fake-wrapped:"))
	}
	return &awsKMSDecryptOutput{Plaintext: append([]byte(nil), out...)}, nil
}
