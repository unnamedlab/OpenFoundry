// Package models holds the wire-format types for application-composition-service.
package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// PrimaryItem mirrors `composition_views`.
type PrimaryItem struct {
	ID        uuid.UUID       `json:"id"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

type CreatePrimaryRequest struct {
	Payload json.RawMessage `json:"payload"`
}

// SecondaryItem mirrors `composition_bindings`.
type SecondaryItem struct {
	ID        uuid.UUID       `json:"id"`
	ParentID  uuid.UUID       `json:"parent_id"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

type CreateSecondaryRequest struct {
	Payload json.RawMessage `json:"payload"`
}
