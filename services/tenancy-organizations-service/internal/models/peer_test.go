package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPeerOrganizationJSONRoundtrip(t *testing.T) {
	t.Parallel()
	last := time.Date(2026, 5, 1, 9, 30, 0, 0, time.UTC)
	in := PeerOrganization{
		ID:                   uuid.New(),
		Slug:                 "acme-data",
		DisplayName:          "Acme Data",
		OrganizationType:     "partner",
		Region:               "us-east-1",
		EndpointURL:          "https://peer.acme.example/v1",
		AuthMode:             "mtls",
		TrustLevel:           "verified",
		PublicKeyFingerprint: "SHA256:abcdef",
		SharedScopes:         []string{"datasets:read", "ontology:read"},
		Status:               "active",
		LifecycleStage:       "production",
		AdminContacts:        []string{"ops@acme.example"},
		LastHandshakeAt:      &last,
		CreatedAt:            time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		UpdatedAt:            time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC),
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	var got PeerOrganization
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, in, got)
}

func TestCreatePeerRequestJSONRoundtrip(t *testing.T) {
	t.Parallel()
	in := CreatePeerRequest{
		Slug:                 "acme-data",
		DisplayName:          "Acme Data",
		OrganizationType:     "vendor",
		Region:               "us-east-1",
		EndpointURL:          "https://peer.acme.example/v1",
		AuthMode:             "mtls",
		TrustLevel:           "verified",
		PublicKeyFingerprint: "SHA256:abcdef",
		SharedScopes:         []string{"datasets:read"},
		AdminContacts:        []string{"ops@acme.example"},
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	var got CreatePeerRequest
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, in, got)
}

func TestCreatePeerRequestDefaultsOrganizationType(t *testing.T) {
	t.Parallel()
	// Mirrors Rust `#[serde(default = "default_organization_type")]` on
	// CreatePeerRequest.organization_type: a missing field falls back to "partner".
	raw := []byte(`{
		"slug": "acme-data",
		"display_name": "Acme Data",
		"region": "us-east-1",
		"endpoint_url": "https://peer.acme.example/v1",
		"auth_mode": "mtls",
		"trust_level": "verified",
		"public_key_fingerprint": "SHA256:abcdef"
	}`)
	var got CreatePeerRequest
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, DefaultPeerOrganizationType, got.OrganizationType)
	assert.Equal(t, "partner", got.OrganizationType)
}

func TestCreatePeerRequestExplicitEmptyOrganizationType(t *testing.T) {
	t.Parallel()
	// An explicit empty string is preserved (matches serde behaviour: only the
	// missing-key branch consults the default).
	raw := []byte(`{
		"slug": "acme-data",
		"display_name": "Acme Data",
		"organization_type": "",
		"region": "us-east-1",
		"endpoint_url": "https://peer.acme.example/v1",
		"auth_mode": "mtls",
		"trust_level": "verified",
		"public_key_fingerprint": "SHA256:abcdef"
	}`)
	var got CreatePeerRequest
	require.NoError(t, json.Unmarshal(raw, &got))
	assert.Equal(t, "", got.OrganizationType)
}

func TestUpdatePeerRequestJSONRoundtrip(t *testing.T) {
	t.Parallel()
	dn := "Acme Data Renamed"
	scopes := []string{"datasets:read", "datasets:write"}
	status := "suspended"
	in := UpdatePeerRequest{
		DisplayName:  &dn,
		SharedScopes: &scopes,
		Status:       &status,
	}
	b, err := json.Marshal(in)
	require.NoError(t, err)
	assert.JSONEq(t, `{
		"display_name":"Acme Data Renamed",
		"shared_scopes":["datasets:read","datasets:write"],
		"status":"suspended"
	}`, string(b))
	var got UpdatePeerRequest
	require.NoError(t, json.Unmarshal(b, &got))
	assert.Equal(t, in, got)
}
