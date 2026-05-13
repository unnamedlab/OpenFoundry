package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// OntologyInterface mirrors
// `libs/ontology-kernel/src/models/interface.rs` `struct OntologyInterface`.
type OntologyInterface struct {
	ID          uuid.UUID `json:"id"           db:"id"`
	Name        string    `json:"name"         db:"name"`
	DisplayName string    `json:"display_name" db:"display_name"`
	Description string    `json:"description"  db:"description"`
	OwnerID     uuid.UUID `json:"owner_id"     db:"owner_id"`
	CreatedAt   time.Time `json:"created_at"   db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"   db:"updated_at"`
}

// InterfaceProperty mirrors `struct InterfaceProperty`.
type InterfaceProperty struct {
	ID                     uuid.UUID       `json:"id"                          db:"id"`
	InterfaceID            uuid.UUID       `json:"interface_id"                db:"interface_id"`
	Name                   string          `json:"name"                        db:"name"`
	DisplayName            string          `json:"display_name"                db:"display_name"`
	Description            string          `json:"description"                 db:"description"`
	PropertyType           string          `json:"property_type"               db:"property_type"`
	BaseType               string          `json:"base_type,omitempty"         db:"-"`
	TypeFamily             string          `json:"type_family,omitempty"       db:"-"`
	TypeDisplayName        string          `json:"type_display_name,omitempty" db:"-"`
	ValueShape             string          `json:"value_shape,omitempty"       db:"-"`
	IsArray                bool            `json:"is_array"                   db:"-"`
	ArrayItemType          *string         `json:"array_item_type,omitempty"   db:"-"`
	ArrayAllowed           bool            `json:"array_allowed"              db:"-"`
	Searchable             bool            `json:"searchable"                 db:"-"`
	Filterable             bool            `json:"filterable"                 db:"-"`
	Sortable               bool            `json:"sortable"                   db:"-"`
	Aggregatable           bool            `json:"aggregatable"               db:"-"`
	PrimaryKeyEligible     bool            `json:"primary_key_eligible"       db:"-"`
	TitleKeyEligible       bool            `json:"title_key_eligible"         db:"-"`
	FormattingEligible     bool            `json:"formatting_eligible"        db:"-"`
	ObjectSecurityEligible bool            `json:"object_security_eligible"   db:"-"`
	ProminentEligible      bool            `json:"prominent_eligible"         db:"-"`
	SemanticHints          []string        `json:"semantic_hints,omitempty"   db:"-"`
	Required               bool            `json:"required"                   db:"required"`
	UniqueConstraint       bool            `json:"unique_constraint"          db:"unique_constraint"`
	TimeDependent          bool            `json:"time_dependent"             db:"time_dependent"`
	DefaultValue           json.RawMessage `json:"default_value"              db:"default_value"`
	ValidationRules        json.RawMessage `json:"validation_rules"           db:"validation_rules"`
	CreatedAt              time.Time       `json:"created_at"                 db:"created_at"`
	UpdatedAt              time.Time       `json:"updated_at"                 db:"updated_at"`
}

// ObjectTypeInterfaceBinding mirrors `struct ObjectTypeInterfaceBinding`.
type ObjectTypeInterfaceBinding struct {
	ObjectTypeID uuid.UUID `json:"object_type_id" db:"object_type_id"`
	InterfaceID  uuid.UUID `json:"interface_id"   db:"interface_id"`
	CreatedAt    time.Time `json:"created_at"     db:"created_at"`
}

// CreateInterfaceRequest mirrors `struct CreateInterfaceRequest`.
type CreateInterfaceRequest struct {
	Name        string  `json:"name"`
	DisplayName *string `json:"display_name,omitempty"`
	Description *string `json:"description,omitempty"`
}

// UpdateInterfaceRequest mirrors `struct UpdateInterfaceRequest`.
type UpdateInterfaceRequest struct {
	DisplayName *string `json:"display_name,omitempty"`
	Description *string `json:"description,omitempty"`
}

// ListInterfacesQuery mirrors `struct ListInterfacesQuery`.
type ListInterfacesQuery struct {
	Page    *int64  `json:"page,omitempty"`
	PerPage *int64  `json:"per_page,omitempty"`
	Search  *string `json:"search,omitempty"`
}

// ListInterfacesResponse mirrors `struct ListInterfacesResponse`.
type ListInterfacesResponse struct {
	Data    []OntologyInterface `json:"data"`
	Total   int64               `json:"total"`
	Page    int64               `json:"page"`
	PerPage int64               `json:"per_page"`
}

// CreateInterfacePropertyRequest mirrors `struct CreateInterfacePropertyRequest`.
type CreateInterfacePropertyRequest struct {
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

// UpdateInterfacePropertyRequest mirrors `struct UpdateInterfacePropertyRequest`.
type UpdateInterfacePropertyRequest struct {
	DisplayName      *string         `json:"display_name,omitempty"`
	Description      *string         `json:"description,omitempty"`
	Required         *bool           `json:"required,omitempty"`
	UniqueConstraint *bool           `json:"unique_constraint,omitempty"`
	TimeDependent    *bool           `json:"time_dependent,omitempty"`
	DefaultValue     json.RawMessage `json:"default_value,omitempty"`
	ValidationRules  json.RawMessage `json:"validation_rules,omitempty"`
}
