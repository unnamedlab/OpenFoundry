package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// CipherPermission: read-only catalog (cipher:use, cipher:govern).
type CipherPermission struct {
	ID          uuid.UUID `json:"id"`
	Resource    string    `json:"resource"`
	Action      string    `json:"action"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`
}

// CipherChannel: a release channel with allowed_operations and license tier.
type CipherChannel struct {
	ID                uuid.UUID       `json:"id"`
	Name              string          `json:"name"`
	ReleaseChannel    string          `json:"release_channel"`
	AllowedOperations json.RawMessage `json:"allowed_operations"`
	LicenseTier       string          `json:"license_tier"`
	Enabled           bool            `json:"enabled"`
	CreatedAt         time.Time       `json:"created_at"`
	UpdatedAt         time.Time       `json:"updated_at"`
}

// CreateCipherChannelRequest: POST /api/v1/cipher-channels.
type CreateCipherChannelRequest struct {
	Name              string          `json:"name"`
	ReleaseChannel    string          `json:"release_channel"`
	AllowedOperations json.RawMessage `json:"allowed_operations,omitempty"`
	LicenseTier       string          `json:"license_tier"`
	Enabled           *bool           `json:"enabled,omitempty"`
}

// UpdateCipherChannelRequest: PATCH semantics — nil preserves.
type UpdateCipherChannelRequest struct {
	ReleaseChannel    *string         `json:"release_channel,omitempty"`
	AllowedOperations json.RawMessage `json:"allowed_operations,omitempty"`
	LicenseTier       *string         `json:"license_tier,omitempty"`
	Enabled           *bool           `json:"enabled,omitempty"`
}

// CipherLicense: registered license with feature allow-list.
type CipherLicense struct {
	ID        uuid.UUID       `json:"id"`
	Name      string          `json:"name"`
	Tier      string          `json:"tier"`
	Features  json.RawMessage `json:"features"`
	IssuedBy  *uuid.UUID      `json:"issued_by"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}

// CreateCipherLicenseRequest: POST /api/v1/cipher-licenses.
type CreateCipherLicenseRequest struct {
	Name     string          `json:"name"`
	Tier     string          `json:"tier"`
	Features json.RawMessage `json:"features,omitempty"`
}

// UpdateCipherLicenseRequest: PATCH semantics.
type UpdateCipherLicenseRequest struct {
	Tier     *string         `json:"tier,omitempty"`
	Features json.RawMessage `json:"features,omitempty"`
}
