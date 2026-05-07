package models

import (
	"time"

	"github.com/google/uuid"
)

// TOTPConfig mirrors the `user_mfa_totp` row + the Rust crate's
// `TotpConfiguration` model.
type TOTPConfig struct {
	UserID             uuid.UUID  `json:"user_id"`
	Secret             string     `json:"-"` // never serialised
	RecoveryCodeHashes []string   `json:"recovery_code_hashes"`
	Enabled            bool       `json:"enabled"`
	VerifiedAt         *time.Time `json:"verified_at,omitempty"`
	CreatedAt          time.Time  `json:"created_at"`
	UpdatedAt          time.Time  `json:"updated_at"`
}

// MFAStatusResponse is the payload of GET /auth/mfa/status.
type MFAStatusResponse struct {
	TOTPEnabled        bool `json:"totp_enabled"`
	WebAuthnConfigured bool `json:"webauthn_configured"`
}

// EnrollTOTPResponse is the payload of POST /auth/mfa/totp/enroll.
//
// Returns the secret + recovery codes ONCE — clients must persist them
// because they are never available again from the server.
type EnrollTOTPResponse struct {
	Secret        string   `json:"secret"`
	RecoveryCodes []string `json:"recovery_codes"`
	OTPAuthURI    string   `json:"otpauth_uri"`
}

// VerifyTOTPRequest is the body of POST /auth/mfa/totp/verify.
type VerifyTOTPRequest struct {
	Code string `json:"code"`
}

// CompleteLoginRequest is the body of POST /auth/mfa/totp/complete-login.
type CompleteLoginRequest struct {
	ChallengeToken string `json:"challenge_token"`
	Code           string `json:"code"`
	RecoveryCode   string `json:"recovery_code,omitempty"`
}
