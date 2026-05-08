package service

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

// TOTP enrolment / verification helpers — RFC 6238 SHA-1, 30-second
// window, 6 digits. Mirrors `domain/mfa.rs` byte-for-byte:
//   - same base32 alphabet (RFC 4648, no padding)
//   - same otpauth URI template
//   - same hash_token (sha256 → URL-safe base64 no padding)
//   - same recovery code generation (10 chars uppercase)
//   - same ±1 window verification

// Enrollment is the result of CreateEnrollment.
type Enrollment struct {
	Secret        string   // base32 (RFC 4648, no padding)
	RecoveryCodes []string // 8 codes, 10 chars uppercase
	OTPAuthURI    string   // otpauth://totp/...
}

// CreateEnrollment generates a fresh secret + recovery codes + the
// otpauth provisioning URI for `email`.
func CreateEnrollment(email string) (*Enrollment, error) {
	secret, err := randomBase32Secret(20)
	if err != nil {
		return nil, err
	}
	codes, err := GenerateRecoveryCodes(8)
	if err != nil {
		return nil, err
	}
	uri := fmt.Sprintf(
		"otpauth://totp/OpenFoundry:%s?secret=%s&issuer=OpenFoundry&algorithm=SHA1&digits=6&period=30",
		url.PathEscape(email), secret,
	)
	return &Enrollment{Secret: secret, RecoveryCodes: codes, OTPAuthURI: uri}, nil
}

// VerifyTOTP returns true when `code` matches the secret in the ±1
// 30-second window. The Rust impl uses the same window.
func VerifyTOTP(secret, code string) bool {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	for offset := int64(-1); offset <= 1; offset++ {
		got, err := generateTOTP(secret, offset)
		if err == nil && got == code {
			return true
		}
	}
	return false
}

// HashToken returns the SHA-256 of `value` URL-safe-base64-encoded
// without padding. Mirrors `domain/security::hash_token`.
//
// Used for recovery-code hashing.
func HashToken(value string) string {
	sum := sha256.Sum256([]byte(value))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

// HashRecoveryCodes returns the JSON-array of hashed codes ready for
// the user_mfa_totp.recovery_code_hashes column.
func HashRecoveryCodes(codes []string) []string {
	out := make([]string, len(codes))
	for i, c := range codes {
		out[i] = HashToken(c)
	}
	return out
}

// ConsumeRecoveryCode searches `hashes` for a match against `code`'s
// hash and returns the surviving hashes when found, ok=false when the
// code is unknown / already used. Mirrors `domain/mfa::consume_recovery_code`.
func ConsumeRecoveryCode(hashes []string, code string) (remaining []string, ok bool) {
	target := HashToken(code)
	consumed := false
	out := make([]string, 0, len(hashes))
	for _, h := range hashes {
		if h == target && !consumed {
			consumed = true
			continue
		}
		out = append(out, h)
	}
	return out, consumed
}

// GenerateRecoveryCodes returns `count` 10-char uppercase codes.
//
// Mirrors `domain/security::generate_recovery_codes`: random_token(6)
// → take the first 10 chars after URL-safe-base64 encoding → uppercase.
func GenerateRecoveryCodes(count int) ([]string, error) {
	codes := make([]string, count)
	for i := 0; i < count; i++ {
		raw := make([]byte, 6)
		if _, err := rand.Read(raw); err != nil {
			return nil, fmt.Errorf("generate recovery code: %w", err)
		}
		token := base64.RawURLEncoding.EncodeToString(raw)
		// Take up to 10 chars (URL-safe base64 of 6 bytes is 8 chars,
		// so most codes are 8 chars; uppercase to match the Rust output).
		if len(token) > 10 {
			token = token[:10]
		}
		codes[i] = strings.ToUpper(token)
	}
	return codes, nil
}

// ─── Internals ──────────────────────────────────────────────────────────

// b32 alphabet with no padding — matches the Rust crate's `base32::Alphabet::RFC4648 {padding: false}`.
var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

func randomBase32Secret(byteLen int) (string, error) {
	buf := make([]byte, byteLen)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}
	return b32.EncodeToString(buf), nil
}

// generateTOTP computes the 6-digit TOTP code for `secret` at the
// counter offset by `offsetWindow` 30-second buckets.
func generateTOTP(secret string, offsetWindow int64) (string, error) {
	secretBytes, err := b32.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", fmt.Errorf("decode base32 secret: %w", err)
	}
	counter := uint64(time.Now().Unix()/30 + offsetWindow)

	mac := hmac.New(sha1.New, secretBytes)
	if err := binary.Write(mac, binary.BigEndian, counter); err != nil {
		return "", err
	}
	digest := mac.Sum(nil)
	if len(digest) != 20 {
		return "", errors.New("totp: unexpected hmac digest length")
	}
	offset := digest[19] & 0x0f
	binPart := (uint32(digest[offset]&0x7f) << 24) |
		(uint32(digest[offset+1]) << 16) |
		(uint32(digest[offset+2]) << 8) |
		uint32(digest[offset+3])
	return fmt.Sprintf("%06d", binPart%1_000_000), nil
}

// generateTOTPAt is a deterministic helper used by tests.
func generateTOTPAt(secret string, t time.Time) (string, error) {
	secretBytes, err := b32.DecodeString(strings.ToUpper(strings.TrimSpace(secret)))
	if err != nil {
		return "", err
	}
	counter := uint64(t.Unix() / 30)
	mac := hmac.New(sha1.New, secretBytes)
	if err := binary.Write(mac, binary.BigEndian, counter); err != nil {
		return "", err
	}
	digest := mac.Sum(nil)
	offset := digest[19] & 0x0f
	binPart := (uint32(digest[offset]&0x7f) << 24) |
		(uint32(digest[offset+1]) << 16) |
		(uint32(digest[offset+2]) << 8) |
		uint32(digest[offset+3])
	return fmt.Sprintf("%06d", binPart%1_000_000), nil
}
