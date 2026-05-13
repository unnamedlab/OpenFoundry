package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// PropertyInlineEditConfig mirrors
// `libs/ontology-kernel/src/models/property.rs`
// `struct PropertyInlineEditConfig`. Rust serialises with
// `#[serde(default, skip_serializing_if = "Option::is_none")]` on
// `input_name`.
type PropertyInlineEditConfig struct {
	ActionTypeID uuid.UUID `json:"action_type_id"`
	InputName    *string   `json:"input_name,omitempty"`
}

// Property mirrors `struct Property`.
type Property struct {
	ID                     uuid.UUID                 `json:"id"                       db:"id"`
	ObjectTypeID           uuid.UUID                 `json:"object_type_id"           db:"object_type_id"`
	Name                   string                    `json:"name"                     db:"name"`
	DisplayName            string                    `json:"display_name"             db:"display_name"`
	Description            string                    `json:"description"              db:"description"`
	PropertyType           string                    `json:"property_type"            db:"property_type"`
	BaseType               string                    `json:"base_type,omitempty"      db:"-"`
	TypeFamily             string                    `json:"type_family,omitempty"    db:"-"`
	TypeDisplayName        string                    `json:"type_display_name,omitempty" db:"-"`
	ValueShape             string                    `json:"value_shape,omitempty"    db:"-"`
	IsArray                bool                      `json:"is_array"                 db:"-"`
	ArrayItemType          *string                   `json:"array_item_type,omitempty" db:"-"`
	ArrayAllowed           bool                      `json:"array_allowed"            db:"-"`
	Searchable             bool                      `json:"searchable"               db:"-"`
	Filterable             bool                      `json:"filterable"               db:"-"`
	Sortable               bool                      `json:"sortable"                 db:"-"`
	Aggregatable           bool                      `json:"aggregatable"             db:"-"`
	PrimaryKeyEligible     bool                      `json:"primary_key_eligible"     db:"-"`
	TitleKeyEligible       bool                      `json:"title_key_eligible"       db:"-"`
	FormattingEligible     bool                      `json:"formatting_eligible"      db:"-"`
	ObjectSecurityEligible bool                      `json:"object_security_eligible" db:"-"`
	ProminentEligible      bool                      `json:"prominent_eligible"       db:"-"`
	SemanticHints          []string                  `json:"semantic_hints,omitempty" db:"-"`
	DisplayMode            string                    `json:"display_mode"             db:"display_mode"`
	ValueFormatting        json.RawMessage           `json:"value_formatting"         db:"value_formatting"`
	ConditionalFormatting  json.RawMessage           `json:"conditional_formatting"   db:"conditional_formatting"`
	ReducerMetadata        json.RawMessage           `json:"reducer_metadata"         db:"reducer_metadata"`
	Required               bool                      `json:"required"                 db:"required"`
	UniqueConstraint       bool                      `json:"unique_constraint"        db:"unique_constraint"`
	TimeDependent          bool                      `json:"time_dependent"           db:"time_dependent"`
	DefaultValue           json.RawMessage           `json:"default_value"            db:"default_value"`
	ValidationRules        json.RawMessage           `json:"validation_rules"         db:"validation_rules"`
	InlineEditConfig       *PropertyInlineEditConfig `json:"inline_edit_config"       db:"inline_edit_config"`
	CreatedAt              time.Time                 `json:"created_at"               db:"created_at"`
	UpdatedAt              time.Time                 `json:"updated_at"               db:"updated_at"`
}

// CreatePropertyRequest mirrors `struct CreatePropertyRequest`.
type CreatePropertyRequest struct {
	Name                  string                    `json:"name"`
	DisplayName           *string                   `json:"display_name,omitempty"`
	Description           *string                   `json:"description,omitempty"`
	PropertyType          string                    `json:"property_type"`
	Required              *bool                     `json:"required,omitempty"`
	UniqueConstraint      *bool                     `json:"unique_constraint,omitempty"`
	TimeDependent         *bool                     `json:"time_dependent,omitempty"`
	DefaultValue          json.RawMessage           `json:"default_value,omitempty"`
	ValidationRules       json.RawMessage           `json:"validation_rules,omitempty"`
	DisplayMode           *string                   `json:"display_mode,omitempty"`
	ValueFormatting       json.RawMessage           `json:"value_formatting,omitempty"`
	ConditionalFormatting json.RawMessage           `json:"conditional_formatting,omitempty"`
	ReducerMetadata       json.RawMessage           `json:"reducer_metadata,omitempty"`
	InlineEditConfig      *PropertyInlineEditConfig `json:"inline_edit_config,omitempty"`
}

// UpdatePropertyRequest mirrors `struct UpdatePropertyRequest`.
//
// Note the Rust `Option<Option<PropertyInlineEditConfig>>` discriminates
// between "not present" (do not modify) and "present but null" (clear).
// `InlineEditConfig` carries that three-way distinction:
//
//	field absent     ⇒ InlineEditConfig == nil (no change)
//	field == null    ⇒ &{Set: true, Value: nil} (clear)
//	field == { ... } ⇒ &{Set: true, Value: &cfg} (replace)
//
// `UnmarshalJSON` on the request struct decodes the raw object first
// so it can spot the key presence — Go's stdlib decoder skips the
// inner UnmarshalJSON when a value is `null`, so we can't rely on the
// inner type alone.
type UpdatePropertyRequest struct {
	DisplayName           *string                         `json:"display_name,omitempty"`
	Description           *string                         `json:"description,omitempty"`
	Required              *bool                           `json:"required,omitempty"`
	UniqueConstraint      *bool                           `json:"unique_constraint,omitempty"`
	TimeDependent         *bool                           `json:"time_dependent,omitempty"`
	DefaultValue          json.RawMessage                 `json:"default_value,omitempty"`
	ValidationRules       json.RawMessage                 `json:"validation_rules,omitempty"`
	DisplayMode           *string                         `json:"display_mode,omitempty"`
	ValueFormatting       json.RawMessage                 `json:"value_formatting,omitempty"`
	ConditionalFormatting json.RawMessage                 `json:"conditional_formatting,omitempty"`
	ReducerMetadata       json.RawMessage                 `json:"reducer_metadata,omitempty"`
	InlineEditConfig      *PropertyInlineEditConfigUpdate `json:"-"`
}

// PropertyInlineEditConfigUpdate carries the Rust
// `Option<Option<PropertyInlineEditConfig>>` semantics. The carrying
// pointer being non-nil means the field was present; `Value == nil`
// inside the struct means JSON was `null`.
type PropertyInlineEditConfigUpdate struct {
	Value *PropertyInlineEditConfig
}

// UnmarshalJSON detects key presence via a raw decode pass and
// preserves the three-way semantics described above.
func (r *UpdatePropertyRequest) UnmarshalJSON(b []byte) error {
	type alias UpdatePropertyRequest
	if err := json.Unmarshal(b, (*alias)(r)); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if rawIec, ok := raw["inline_edit_config"]; ok {
		upd := &PropertyInlineEditConfigUpdate{}
		if string(rawIec) != "null" {
			var cfg PropertyInlineEditConfig
			if err := json.Unmarshal(rawIec, &cfg); err != nil {
				return err
			}
			upd.Value = &cfg
		}
		r.InlineEditConfig = upd
	}
	return nil
}

// MarshalJSON emits `null` when InlineEditConfig is set with no value,
// the embedded config when set with a value, and omits the key
// entirely when InlineEditConfig is nil.
func (r UpdatePropertyRequest) MarshalJSON() ([]byte, error) {
	type alias UpdatePropertyRequest
	base, err := json.Marshal((alias)(r))
	if err != nil {
		return nil, err
	}
	if r.InlineEditConfig == nil {
		return base, nil
	}
	var bag map[string]json.RawMessage
	if err := json.Unmarshal(base, &bag); err != nil {
		return nil, err
	}
	if r.InlineEditConfig.Value == nil {
		bag["inline_edit_config"] = json.RawMessage("null")
	} else {
		v, err := json.Marshal(r.InlineEditConfig.Value)
		if err != nil {
			return nil, err
		}
		bag["inline_edit_config"] = v
	}
	return json.Marshal(bag)
}

// ExecuteInlineEditRequest mirrors `struct ExecuteInlineEditRequest`.
type ExecuteInlineEditRequest struct {
	Value         json.RawMessage `json:"value"`
	Justification *string         `json:"justification,omitempty"`
}

// ExecuteInlineEditBatchRequest mirrors TASK L bulk inline-edit payload.
// `libs/ontology-kernel/src/models/property.rs` `struct
// ExecuteInlineEditBatchRequest`.
type ExecuteInlineEditBatchRequest struct {
	Edits []ExecuteInlineEditBatchItem `json:"edits"`
}

// ExecuteInlineEditBatchItem mirrors `struct ExecuteInlineEditBatchItem`.
type ExecuteInlineEditBatchItem struct {
	PropertyID    uuid.UUID       `json:"property_id"`
	ObjectID      uuid.UUID       `json:"object_id"`
	Value         json.RawMessage `json:"value"`
	Justification *string         `json:"justification,omitempty"`
}
