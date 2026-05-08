package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// DefaultPeerOrganizationType mirrors Rust's `default_organization_type` =
// "partner", applied via `#[serde(default = ...)]` on `CreatePeerRequest`.
const DefaultPeerOrganizationType = "partner"

// PeerOrganization mirrors `models::peer::PeerOrganization`.
type PeerOrganization struct {
	ID                   uuid.UUID  `json:"id"`
	Slug                 string     `json:"slug"`
	DisplayName          string     `json:"display_name"`
	OrganizationType     string     `json:"organization_type"`
	Region               string     `json:"region"`
	EndpointURL          string     `json:"endpoint_url"`
	AuthMode             string     `json:"auth_mode"`
	TrustLevel           string     `json:"trust_level"`
	PublicKeyFingerprint string     `json:"public_key_fingerprint"`
	SharedScopes         []string   `json:"shared_scopes"`
	Status               string     `json:"status"`
	LifecycleStage       string     `json:"lifecycle_stage"`
	AdminContacts        []string   `json:"admin_contacts"`
	LastHandshakeAt      *time.Time `json:"last_handshake_at"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

// CreatePeerRequest is the body of POST /peers.
type CreatePeerRequest struct {
	Slug                 string   `json:"slug"`
	DisplayName          string   `json:"display_name"`
	OrganizationType     string   `json:"organization_type"`
	Region               string   `json:"region"`
	EndpointURL          string   `json:"endpoint_url"`
	AuthMode             string   `json:"auth_mode"`
	TrustLevel           string   `json:"trust_level"`
	PublicKeyFingerprint string   `json:"public_key_fingerprint"`
	SharedScopes         []string `json:"shared_scopes"`
	AdminContacts        []string `json:"admin_contacts"`
}

// UnmarshalJSON applies Rust's `#[serde(default = "default_organization_type")]`:
// when `organization_type` is absent from the payload, fall back to "partner".
// An explicit empty string is preserved as-is to keep wire semantics with
// serde's "missing-key vs. present-empty" distinction.
func (r *CreatePeerRequest) UnmarshalJSON(data []byte) error {
	type alias CreatePeerRequest
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*r = CreatePeerRequest(a)
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if _, present := raw["organization_type"]; !present {
		r.OrganizationType = DefaultPeerOrganizationType
	}
	return nil
}

// UpdatePeerRequest is the body of PATCH /peers/:id.
type UpdatePeerRequest struct {
	DisplayName      *string   `json:"display_name,omitempty"`
	OrganizationType *string   `json:"organization_type,omitempty"`
	Region           *string   `json:"region,omitempty"`
	EndpointURL      *string   `json:"endpoint_url,omitempty"`
	TrustLevel       *string   `json:"trust_level,omitempty"`
	SharedScopes     *[]string `json:"shared_scopes,omitempty"`
	Status           *string   `json:"status,omitempty"`
	LifecycleStage   *string   `json:"lifecycle_stage,omitempty"`
	AdminContacts    *[]string `json:"admin_contacts,omitempty"`
}
