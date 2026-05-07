package models

import (
	"time"

	"github.com/google/uuid"
)

// ObjectType mirrors `libs/ontology-kernel/src/models/object_type.rs`
// `struct ObjectType` 1:1.
type ObjectType struct {
	ID                  uuid.UUID `json:"id"                    db:"id"`
	Name                string    `json:"name"                  db:"name"`
	DisplayName         string    `json:"display_name"          db:"display_name"`
	Description         string    `json:"description"           db:"description"`
	PrimaryKeyProperty  *string   `json:"primary_key_property"  db:"primary_key_property"`
	Icon                *string   `json:"icon"                  db:"icon"`
	Color               *string   `json:"color"                 db:"color"`
	OwnerID             uuid.UUID `json:"owner_id"              db:"owner_id"`
	CreatedAt           time.Time `json:"created_at"            db:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"            db:"updated_at"`
}

// CreateObjectTypeRequest mirrors `struct CreateObjectTypeRequest`.
type CreateObjectTypeRequest struct {
	Name               string  `json:"name"`
	DisplayName        *string `json:"display_name,omitempty"`
	Description        *string `json:"description,omitempty"`
	PrimaryKeyProperty *string `json:"primary_key_property,omitempty"`
	Icon               *string `json:"icon,omitempty"`
	Color              *string `json:"color,omitempty"`
}

// UpdateObjectTypeRequest mirrors `struct UpdateObjectTypeRequest`.
type UpdateObjectTypeRequest struct {
	DisplayName        *string `json:"display_name,omitempty"`
	Description        *string `json:"description,omitempty"`
	PrimaryKeyProperty *string `json:"primary_key_property,omitempty"`
	Icon               *string `json:"icon,omitempty"`
	Color              *string `json:"color,omitempty"`
}

// ListObjectTypesQuery mirrors `struct ListObjectTypesQuery`.
type ListObjectTypesQuery struct {
	Page    *int64  `json:"page,omitempty"`
	PerPage *int64  `json:"per_page,omitempty"`
	Search  *string `json:"search,omitempty"`
}

// ListObjectTypesResponse mirrors `struct ListObjectTypesResponse`.
type ListObjectTypesResponse struct {
	Data    []ObjectType `json:"data"`
	Total   int64        `json:"total"`
	Page    int64        `json:"page"`
	PerPage int64        `json:"per_page"`
}
