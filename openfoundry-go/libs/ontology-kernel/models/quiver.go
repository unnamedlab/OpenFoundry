package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// DefaultChartKind mirrors `default_chart_kind()` in
// `libs/ontology-kernel/src/models/quiver.rs`.
func DefaultChartKind() string { return "line" }

// QuiverVisualFunction mirrors `struct QuiverVisualFunction`.
type QuiverVisualFunction struct {
	ID                  uuid.UUID       `json:"id"                    db:"id"`
	Name                string          `json:"name"                  db:"name"`
	Description         string          `json:"description"           db:"description"`
	PrimaryTypeID       uuid.UUID       `json:"primary_type_id"       db:"primary_type_id"`
	SecondaryTypeID     *uuid.UUID      `json:"secondary_type_id"     db:"secondary_type_id"`
	JoinField           string          `json:"join_field"            db:"join_field"`
	SecondaryJoinField  string          `json:"secondary_join_field"  db:"secondary_join_field"`
	DateField           string          `json:"date_field"            db:"date_field"`
	MetricField         string          `json:"metric_field"          db:"metric_field"`
	GroupField          string          `json:"group_field"           db:"group_field"`
	SelectedGroup       *string         `json:"selected_group"        db:"selected_group"`
	ChartKind           string          `json:"chart_kind"            db:"chart_kind"`
	Shared              bool            `json:"shared"                db:"shared"`
	VegaSpec            json.RawMessage `json:"vega_spec"             db:"vega_spec"`
	OwnerID             uuid.UUID       `json:"owner_id"              db:"owner_id"`
	CreatedAt           time.Time       `json:"created_at"            db:"created_at"`
	UpdatedAt           time.Time       `json:"updated_at"            db:"updated_at"`
}

// QuiverVisualFunctionDraft mirrors `struct QuiverVisualFunctionDraft`.
// `description` and `secondary_join_field` are `#[serde(default)]`,
// `chart_kind` defaults to `default_chart_kind()`, `shared` defaults
// to `false`. Go zero-values + custom UnmarshalJSON below preserve
// those defaults.
type QuiverVisualFunctionDraft struct {
	Name               string     `json:"name"`
	Description        string     `json:"description"`
	PrimaryTypeID      uuid.UUID  `json:"primary_type_id"`
	SecondaryTypeID    *uuid.UUID `json:"secondary_type_id"`
	JoinField          string     `json:"join_field"`
	SecondaryJoinField string     `json:"secondary_join_field"`
	DateField          string     `json:"date_field"`
	MetricField        string     `json:"metric_field"`
	GroupField         string     `json:"group_field"`
	SelectedGroup      *string    `json:"selected_group"`
	ChartKind          string     `json:"chart_kind"`
	Shared             bool       `json:"shared"`
}

// UnmarshalJSON mirrors the Rust default attributes — the only
// non-zero default is `chart_kind`, which falls back to "line".
func (d *QuiverVisualFunctionDraft) UnmarshalJSON(b []byte) error {
	type alias QuiverVisualFunctionDraft
	tmp := struct {
		ChartKind *string `json:"chart_kind"`
		*alias
	}{alias: (*alias)(d)}
	if err := json.Unmarshal(b, &tmp); err != nil {
		return err
	}
	if tmp.ChartKind == nil {
		d.ChartKind = DefaultChartKind()
	} else {
		d.ChartKind = *tmp.ChartKind
	}
	return nil
}

// CreateQuiverVisualFunctionRequest mirrors `struct CreateQuiverVisualFunctionRequest`.
type CreateQuiverVisualFunctionRequest struct {
	Name               string     `json:"name"`
	Description        *string    `json:"description,omitempty"`
	PrimaryTypeID      uuid.UUID  `json:"primary_type_id"`
	SecondaryTypeID    *uuid.UUID `json:"secondary_type_id,omitempty"`
	JoinField          string     `json:"join_field"`
	SecondaryJoinField *string    `json:"secondary_join_field,omitempty"`
	DateField          string     `json:"date_field"`
	MetricField        string     `json:"metric_field"`
	GroupField         string     `json:"group_field"`
	SelectedGroup      *string    `json:"selected_group,omitempty"`
	ChartKind          *string    `json:"chart_kind,omitempty"`
	Shared             *bool      `json:"shared,omitempty"`
}

// IntoDraft mirrors `impl CreateQuiverVisualFunctionRequest::into_draft`.
// All `unwrap_or_default` / `unwrap_or_else` defaults are applied here
// verbatim.
func (r CreateQuiverVisualFunctionRequest) IntoDraft() QuiverVisualFunctionDraft {
	desc := ""
	if r.Description != nil {
		desc = *r.Description
	}
	secondaryJoin := ""
	if r.SecondaryJoinField != nil {
		secondaryJoin = *r.SecondaryJoinField
	}
	chartKind := DefaultChartKind()
	if r.ChartKind != nil {
		chartKind = *r.ChartKind
	}
	shared := false
	if r.Shared != nil {
		shared = *r.Shared
	}
	return QuiverVisualFunctionDraft{
		Name:               r.Name,
		Description:        desc,
		PrimaryTypeID:      r.PrimaryTypeID,
		SecondaryTypeID:    r.SecondaryTypeID,
		JoinField:          r.JoinField,
		SecondaryJoinField: secondaryJoin,
		DateField:          r.DateField,
		MetricField:        r.MetricField,
		GroupField:         r.GroupField,
		SelectedGroup:      r.SelectedGroup,
		ChartKind:          chartKind,
		Shared:             shared,
	}
}

// UpdateQuiverVisualFunctionRequest mirrors `struct UpdateQuiverVisualFunctionRequest`.
//
// `selected_group: Option<Option<String>>` — same three-way semantics
// as `UpdatePropertyRequest.InlineEditConfig`. Carried via
// `*StringUpdate` whose presence/absence/value distinguishes the
// three Rust states.
type UpdateQuiverVisualFunctionRequest struct {
	Name               *string       `json:"name,omitempty"`
	Description        *string       `json:"description,omitempty"`
	PrimaryTypeID      *uuid.UUID    `json:"primary_type_id,omitempty"`
	SecondaryTypeID    *uuid.UUID    `json:"secondary_type_id,omitempty"`
	JoinField          *string       `json:"join_field,omitempty"`
	SecondaryJoinField *string       `json:"secondary_join_field,omitempty"`
	DateField          *string       `json:"date_field,omitempty"`
	MetricField        *string       `json:"metric_field,omitempty"`
	GroupField         *string       `json:"group_field,omitempty"`
	SelectedGroup      *StringUpdate `json:"-"`
	ChartKind          *string       `json:"chart_kind,omitempty"`
	Shared             *bool         `json:"shared,omitempty"`
}

// StringUpdate carries Rust `Option<Option<String>>` semantics. The
// outer pointer being non-nil means the JSON key was present; `Value
// == nil` means the value was `null`.
type StringUpdate struct {
	Value *string
}

// UnmarshalJSON for the request struct detects key presence so the
// three-way distinction survives Go's stdlib decoder, which would
// otherwise skip a custom UnmarshalJSON on a pointer field set to
// JSON `null`.
func (r *UpdateQuiverVisualFunctionRequest) UnmarshalJSON(b []byte) error {
	type alias UpdateQuiverVisualFunctionRequest
	if err := json.Unmarshal(b, (*alias)(r)); err != nil {
		return err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if rawSg, ok := raw["selected_group"]; ok {
		upd := &StringUpdate{}
		if string(rawSg) != "null" {
			var v string
			if err := json.Unmarshal(rawSg, &v); err != nil {
				return err
			}
			upd.Value = &v
		}
		r.SelectedGroup = upd
	}
	return nil
}

// MarshalJSON emits selected_group as `null` (clear), the string
// value (replace), or omits the key entirely (no change).
func (r UpdateQuiverVisualFunctionRequest) MarshalJSON() ([]byte, error) {
	type alias UpdateQuiverVisualFunctionRequest
	base, err := json.Marshal((alias)(r))
	if err != nil {
		return nil, err
	}
	if r.SelectedGroup == nil {
		return base, nil
	}
	var bag map[string]json.RawMessage
	if err := json.Unmarshal(base, &bag); err != nil {
		return nil, err
	}
	if r.SelectedGroup.Value == nil {
		bag["selected_group"] = json.RawMessage("null")
	} else {
		v, err := json.Marshal(*r.SelectedGroup.Value)
		if err != nil {
			return nil, err
		}
		bag["selected_group"] = v
	}
	return json.Marshal(bag)
}

// ListQuiverVisualFunctionsQuery mirrors `struct ListQuiverVisualFunctionsQuery`.
type ListQuiverVisualFunctionsQuery struct {
	Page          *int64  `json:"page,omitempty"`
	PerPage       *int64  `json:"per_page,omitempty"`
	Search        *string `json:"search,omitempty"`
	IncludeShared *bool   `json:"include_shared,omitempty"`
}

// ListQuiverVisualFunctionsResponse mirrors `struct ListQuiverVisualFunctionsResponse`.
type ListQuiverVisualFunctionsResponse struct {
	Data    []QuiverVisualFunction `json:"data"`
	Total   int64                  `json:"total"`
	Page    int64                  `json:"page"`
	PerPage int64                  `json:"per_page"`
}
