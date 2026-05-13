package models

import (
	"time"

	"github.com/google/uuid"
)

// LinkType mirrors `libs/ontology-kernel/src/models/link_type.rs`
// `struct LinkType` 1:1.
type LinkType struct {
	ID                    uuid.UUID      `json:"id"             db:"id"`
	Name                  string         `json:"name"           db:"name"`
	DisplayName           string         `json:"display_name"   db:"display_name"`
	Description           string         `json:"description"    db:"description"`
	SourceTypeID          uuid.UUID      `json:"source_type_id" db:"source_type_id"`
	TargetTypeID          uuid.UUID      `json:"target_type_id" db:"target_type_id"`
	Cardinality           string         `json:"cardinality"              db:"cardinality"`
	Label                 string         `json:"label"                    db:"label"`
	ReverseLabel          string         `json:"reverse_label"            db:"reverse_label"`
	Visibility            string         `json:"visibility"               db:"visibility"`
	LinkDatasourceMapping map[string]any `json:"link_datasource_mapping"  db:"link_datasource_mapping"`
	OwnerID               uuid.UUID      `json:"owner_id"                 db:"owner_id"`
	CreatedAt             time.Time      `json:"created_at"     db:"created_at"`
	UpdatedAt             time.Time      `json:"updated_at"     db:"updated_at"`
}

// CreateLinkTypeRequest mirrors `struct CreateLinkTypeRequest`.
type CreateLinkTypeRequest struct {
	Name                  string         `json:"name"`
	DisplayName           *string        `json:"display_name,omitempty"`
	Description           *string        `json:"description,omitempty"`
	SourceTypeID          uuid.UUID      `json:"source_type_id"`
	TargetTypeID          uuid.UUID      `json:"target_type_id"`
	Cardinality           *string        `json:"cardinality,omitempty"`
	Label                 *string        `json:"label,omitempty"`
	ReverseLabel          *string        `json:"reverse_label,omitempty"`
	Visibility            *string        `json:"visibility,omitempty"`
	LinkDatasourceMapping map[string]any `json:"link_datasource_mapping,omitempty"`
}

// UpdateLinkTypeRequest mirrors `struct UpdateLinkTypeRequest`.
type UpdateLinkTypeRequest struct {
	DisplayName           *string        `json:"display_name,omitempty"`
	Description           *string        `json:"description,omitempty"`
	Cardinality           *string        `json:"cardinality,omitempty"`
	Label                 *string        `json:"label,omitempty"`
	ReverseLabel          *string        `json:"reverse_label,omitempty"`
	Visibility            *string        `json:"visibility,omitempty"`
	LinkDatasourceMapping map[string]any `json:"link_datasource_mapping,omitempty"`
}

// ListLinkTypesQuery mirrors `struct ListLinkTypesQuery`.
type ListLinkTypesQuery struct {
	ObjectTypeID *uuid.UUID `json:"object_type_id,omitempty"`
	Page         *int64     `json:"page,omitempty"`
	PerPage      *int64     `json:"per_page,omitempty"`
}
