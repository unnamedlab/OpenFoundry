package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// SharedPropertyType mirrors
// `libs/ontology-kernel/src/models/shared_property.rs`
// `struct SharedPropertyType`.
type SharedPropertyType struct {
	ID               uuid.UUID       `json:"id"                db:"id"`
	Name             string          `json:"name"              db:"name"`
	DisplayName      string          `json:"display_name"      db:"display_name"`
	Description      string          `json:"description"       db:"description"`
	PropertyType     string          `json:"property_type"     db:"property_type"`
	Required         bool            `json:"required"          db:"required"`
	UniqueConstraint bool            `json:"unique_constraint" db:"unique_constraint"`
	TimeDependent    bool            `json:"time_dependent"    db:"time_dependent"`
	DefaultValue     json.RawMessage `json:"default_value"     db:"default_value"`
	ValidationRules  json.RawMessage `json:"validation_rules"  db:"validation_rules"`
	OwnerID          uuid.UUID       `json:"owner_id"          db:"owner_id"`
	CreatedAt        time.Time       `json:"created_at"        db:"created_at"`
	UpdatedAt        time.Time       `json:"updated_at"        db:"updated_at"`
}

// ObjectTypeSharedPropertyBinding mirrors
// `struct ObjectTypeSharedPropertyBinding`.
type ObjectTypeSharedPropertyBinding struct {
	ObjectTypeID         uuid.UUID `json:"object_type_id"          db:"object_type_id"`
	SharedPropertyTypeID uuid.UUID `json:"shared_property_type_id" db:"shared_property_type_id"`
	CreatedAt            time.Time `json:"created_at"              db:"created_at"`
}

// CreateSharedPropertyTypeRequest mirrors
// `struct CreateSharedPropertyTypeRequest`.
type CreateSharedPropertyTypeRequest struct {
	Name             string          `json:"name"`
	DisplayName      *string         `json:"display_name,omitempty"`
	Description      *string         `json:"description,omitempty"`
	PropertyType     string          `json:"property_type"`
	Required         *bool           `json:"required,omitempty"`
	UniqueConstraint *bool           `json:"unique_constraint,omitempty"`
	TimeDependent    *bool           `json:"time_dependent,omitempty"`
	DefaultValue     json.RawMessage `json:"default_value,omitempty"`
	ValidationRules  json.RawMessage `json:"validation_rules,omitempty"`
}

// UpdateSharedPropertyTypeRequest mirrors
// `struct UpdateSharedPropertyTypeRequest`.
type UpdateSharedPropertyTypeRequest struct {
	DisplayName      *string         `json:"display_name,omitempty"`
	Description      *string         `json:"description,omitempty"`
	Required         *bool           `json:"required,omitempty"`
	UniqueConstraint *bool           `json:"unique_constraint,omitempty"`
	TimeDependent    *bool           `json:"time_dependent,omitempty"`
	DefaultValue     json.RawMessage `json:"default_value,omitempty"`
	ValidationRules  json.RawMessage `json:"validation_rules,omitempty"`
}

// ListSharedPropertyTypesQuery mirrors
// `struct ListSharedPropertyTypesQuery`.
type ListSharedPropertyTypesQuery struct {
	Page    *int64  `json:"page,omitempty"`
	PerPage *int64  `json:"per_page,omitempty"`
	Search  *string `json:"search,omitempty"`
}

// ListSharedPropertyTypesResponse mirrors
// `struct ListSharedPropertyTypesResponse`.
type ListSharedPropertyTypesResponse struct {
	Data    []SharedPropertyType `json:"data"`
	Total   int64                `json:"total"`
	Page    int64                `json:"page"`
	PerPage int64                `json:"per_page"`
}
