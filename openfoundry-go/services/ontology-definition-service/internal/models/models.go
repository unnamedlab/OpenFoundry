// Package models holds wire types for ontology-definition-service.
//
// Foundation slice scope: object_types only. Properties, link_types,
// action_types, interfaces, shared property types, ontology_projects
// (~600 LOC of consolidated DDL) all land in follow-up slices once
// the Rust kernel handlers themselves migrate.
package models

import (
	"time"

	"github.com/google/uuid"
)

type ListResponse[T any] struct {
	Items []T `json:"items"`
}

// ObjectType mirrors `ontology_schema.object_types` rows.
type ObjectType struct {
	ID                 uuid.UUID `json:"id"`
	Name               string    `json:"name"`
	DisplayName        string    `json:"display_name"`
	Description        string    `json:"description"`
	PrimaryKeyProperty *string   `json:"primary_key_property"`
	Icon               *string   `json:"icon"`
	Color              *string   `json:"color"`
	OwnerID            uuid.UUID `json:"owner_id"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

type CreateObjectTypeRequest struct {
	Name               string  `json:"name"`
	DisplayName        string  `json:"display_name"`
	Description        string  `json:"description,omitempty"`
	PrimaryKeyProperty *string `json:"primary_key_property,omitempty"`
	Icon               *string `json:"icon,omitempty"`
	Color              *string `json:"color,omitempty"`
}

type UpdateObjectTypeRequest struct {
	DisplayName        *string `json:"display_name,omitempty"`
	Description        *string `json:"description,omitempty"`
	PrimaryKeyProperty *string `json:"primary_key_property,omitempty"`
	Icon               *string `json:"icon,omitempty"`
	Color              *string `json:"color,omitempty"`
}
