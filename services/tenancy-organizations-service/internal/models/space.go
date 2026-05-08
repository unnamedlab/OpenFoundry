package models

import (
	"time"

	"github.com/google/uuid"
)

// NexusSpace mirrors `models::space::NexusSpace`.
type NexusSpace struct {
	ID             uuid.UUID   `json:"id"`
	Slug           string      `json:"slug"`
	DisplayName    string      `json:"display_name"`
	Description    string      `json:"description"`
	SpaceKind      string      `json:"space_kind"`
	OwnerPeerID    *uuid.UUID  `json:"owner_peer_id"`
	Region         string      `json:"region"`
	MemberPeerIDs  []uuid.UUID `json:"member_peer_ids"`
	GovernanceTags []string    `json:"governance_tags"`
	Status         string      `json:"status"`
	CreatedAt      time.Time   `json:"created_at"`
	UpdatedAt      time.Time   `json:"updated_at"`
}

// CreateSpaceRequest is the body of POST /spaces.
//
// Rust uses `#[serde(default)]` on `member_peer_ids` and `governance_tags`,
// so a missing field decodes as the empty slice — Go's nil-slice unmarshal
// already matches that.
type CreateSpaceRequest struct {
	Slug           string      `json:"slug"`
	DisplayName    string      `json:"display_name"`
	Description    string      `json:"description"`
	SpaceKind      string      `json:"space_kind"`
	OwnerPeerID    *uuid.UUID  `json:"owner_peer_id,omitempty"`
	Region         string      `json:"region"`
	MemberPeerIDs  []uuid.UUID `json:"member_peer_ids"`
	GovernanceTags []string    `json:"governance_tags"`
	Status         string      `json:"status"`
}

// UpdateSpaceRequest is the body of PATCH /spaces/:id.
type UpdateSpaceRequest struct {
	DisplayName    *string      `json:"display_name,omitempty"`
	Description    *string      `json:"description,omitempty"`
	OwnerPeerID    *uuid.UUID   `json:"owner_peer_id,omitempty"`
	Region         *string      `json:"region,omitempty"`
	MemberPeerIDs  *[]uuid.UUID `json:"member_peer_ids,omitempty"`
	GovernanceTags *[]string    `json:"governance_tags,omitempty"`
	Status         *string      `json:"status,omitempty"`
}
