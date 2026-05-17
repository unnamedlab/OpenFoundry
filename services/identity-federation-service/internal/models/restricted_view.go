package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// RestrictedView mirrors `models::restricted_view::RestrictedView` in Rust.
//
// Restricted views are CBAC (claims/condition-based access control)
// rules: a (resource, action) tuple gated by `conditions` (JSON ABAC
// expression), `allowed_markings`, `allowed_org_ids`, plus row-level
// + column-level filters that downstream services (datasets,
// ontology) consume to mask data per session.
type RestrictedView struct {
	ID                  uuid.UUID       `json:"id"`
	TenantID            uuid.UUID       `json:"tenant_id"`
	Name                string          `json:"name"`
	Description         *string         `json:"description,omitempty"`
	Resource            string          `json:"resource"`
	Action              string          `json:"action"`
	Conditions          json.RawMessage `json:"conditions"`
	RowFilter           *string         `json:"row_filter,omitempty"`
	HiddenColumns       json.RawMessage `json:"hidden_columns"`
	MarkingColumns      json.RawMessage `json:"marking_columns"`
	AllowedOrgIDs       json.RawMessage `json:"allowed_org_ids"`
	AllowedMarkings     json.RawMessage `json:"allowed_markings"`
	ConsumerModeEnabled bool            `json:"consumer_mode_enabled"`
	AllowGuestAccess    bool            `json:"allow_guest_access"`
	Enabled             bool            `json:"enabled"`
	CreatedBy           *uuid.UUID      `json:"created_by,omitempty"`
	CreatedAt           time.Time       `json:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"`
}

// CreateRestrictedViewRequest is the body of POST /restricted-views.
type CreateRestrictedViewRequest struct {
	Name                 string          `json:"name"`
	Description          *string         `json:"description,omitempty"`
	Resource             string          `json:"resource"`
	Action               string          `json:"action"`
	Conditions           json.RawMessage `json:"conditions,omitempty"`
	RowFilter            *string         `json:"row_filter,omitempty"`
	HiddenColumns        json.RawMessage `json:"hidden_columns,omitempty"`
	MarkingColumns       json.RawMessage `json:"marking_columns,omitempty"`
	AllowedOrgIDs        json.RawMessage `json:"allowed_org_ids,omitempty"`
	AllowedMarkings      json.RawMessage `json:"allowed_markings,omitempty"`
	BackingDatasetSchema json.RawMessage `json:"backing_dataset_schema,omitempty"`
	ConsumerModeEnabled  *bool           `json:"consumer_mode_enabled,omitempty"`
	AllowGuestAccess     *bool           `json:"allow_guest_access,omitempty"`
	Enabled              *bool           `json:"enabled,omitempty"`
}

// UpdateRestrictedViewRequest mirrors CreateRestrictedViewRequest but
// every field is optional — unset fields preserve current values.
type UpdateRestrictedViewRequest struct {
	Name                 *string         `json:"name,omitempty"`
	Description          *string         `json:"description,omitempty"`
	Resource             *string         `json:"resource,omitempty"`
	Action               *string         `json:"action,omitempty"`
	Conditions           json.RawMessage `json:"conditions,omitempty"`
	RowFilter            *string         `json:"row_filter,omitempty"`
	HiddenColumns        json.RawMessage `json:"hidden_columns,omitempty"`
	MarkingColumns       json.RawMessage `json:"marking_columns,omitempty"`
	AllowedOrgIDs        json.RawMessage `json:"allowed_org_ids,omitempty"`
	AllowedMarkings      json.RawMessage `json:"allowed_markings,omitempty"`
	BackingDatasetSchema json.RawMessage `json:"backing_dataset_schema,omitempty"`
	ConsumerModeEnabled  *bool           `json:"consumer_mode_enabled,omitempty"`
	AllowGuestAccess     *bool           `json:"allow_guest_access,omitempty"`
	Enabled              *bool           `json:"enabled,omitempty"`
}
