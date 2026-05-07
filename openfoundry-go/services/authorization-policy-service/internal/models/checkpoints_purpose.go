package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// CheckpointPolicy: a sensitivity gate keyed by slug. Seed catalogs
// (ai-private-network, ai-sensitive-tooling) live in the migration.
type CheckpointPolicy struct {
	Slug            string          `json:"slug"`
	Name            string          `json:"name"`
	InteractionType string          `json:"interaction_type"`
	Sensitivity     string          `json:"sensitivity"`
	EnforcementMode string          `json:"enforcement_mode"`
	Prompts         json.RawMessage `json:"prompts"`
	Rules           json.RawMessage `json:"rules"`
	CreatedAt       time.Time       `json:"created_at"`
	UpdatedAt       time.Time       `json:"updated_at"`
}

// SensitiveInteractionConfig links an interaction_type to a policy.
type SensitiveInteractionConfig struct {
	InteractionType             string  `json:"interaction_type"`
	Sensitivity                 string  `json:"sensitivity"`
	RequirePurposeJustification bool    `json:"require_purpose_justification"`
	RequireAuditableRecord      bool    `json:"require_auditable_record"`
	LinkedPolicySlug            *string `json:"linked_policy_slug"`
}

// PurposeTemplate: form scaffold for purpose-of-use justification flows.
type PurposeTemplate struct {
	Slug          string          `json:"slug"`
	Name          string          `json:"name"`
	Summary       string          `json:"summary"`
	Prompts       json.RawMessage `json:"prompts"`
	RequiredTags  json.RawMessage `json:"required_tags"`
}

// PurposeRecord: the audit ledger row written when a sensitive
// interaction is justified.
type PurposeRecord struct {
	ID                   uuid.UUID       `json:"id"`
	InteractionType      string          `json:"interaction_type"`
	ActorID              *uuid.UUID      `json:"actor_id"`
	PurposeJustification *string         `json:"purpose_justification"`
	Status               string          `json:"status"`
	PolicySlug           *string         `json:"policy_slug"`
	Tags                 json.RawMessage `json:"tags"`
	Evidence             json.RawMessage `json:"evidence"`
	CreatedAt            time.Time       `json:"created_at"`
}

// CreatePurposeRecordRequest is the body of POST /api/v1/purpose-records.
type CreatePurposeRecordRequest struct {
	InteractionType      string          `json:"interaction_type"`
	ActorID              *uuid.UUID      `json:"actor_id,omitempty"`
	PurposeJustification *string         `json:"purpose_justification,omitempty"`
	Status               string          `json:"status"`
	PolicySlug           *string         `json:"policy_slug,omitempty"`
	Tags                 json.RawMessage `json:"tags,omitempty"`
	Evidence             json.RawMessage `json:"evidence,omitempty"`
}
