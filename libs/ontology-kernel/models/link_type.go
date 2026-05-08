package models

import (
	"time"

	"github.com/google/uuid"
)

// LinkType mirrors `libs/ontology-kernel/src/models/link_type.rs`
// `struct LinkType` 1:1.
type LinkType struct {
	ID            uuid.UUID `json:"id"             db:"id"`
	Name          string    `json:"name"           db:"name"`
	DisplayName   string    `json:"display_name"   db:"display_name"`
	Description   string    `json:"description"    db:"description"`
	SourceTypeID  uuid.UUID `json:"source_type_id" db:"source_type_id"`
	TargetTypeID  uuid.UUID `json:"target_type_id" db:"target_type_id"`
	Cardinality   string    `json:"cardinality"    db:"cardinality"`
	OwnerID       uuid.UUID `json:"owner_id"       db:"owner_id"`
	CreatedAt     time.Time `json:"created_at"     db:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"     db:"updated_at"`
}

// CreateLinkTypeRequest mirrors `struct CreateLinkTypeRequest`.
type CreateLinkTypeRequest struct {
	Name         string    `json:"name"`
	DisplayName  *string   `json:"display_name,omitempty"`
	Description  *string   `json:"description,omitempty"`
	SourceTypeID uuid.UUID `json:"source_type_id"`
	TargetTypeID uuid.UUID `json:"target_type_id"`
	Cardinality  *string   `json:"cardinality,omitempty"`
}

// UpdateLinkTypeRequest mirrors `struct UpdateLinkTypeRequest`.
type UpdateLinkTypeRequest struct {
	DisplayName *string `json:"display_name,omitempty"`
	Description *string `json:"description,omitempty"`
	Cardinality *string `json:"cardinality,omitempty"`
}

// ListLinkTypesQuery mirrors `struct ListLinkTypesQuery`.
type ListLinkTypesQuery struct {
	ObjectTypeID *uuid.UUID `json:"object_type_id,omitempty"`
	Page         *int64     `json:"page,omitempty"`
	PerPage      *int64     `json:"per_page,omitempty"`
}
