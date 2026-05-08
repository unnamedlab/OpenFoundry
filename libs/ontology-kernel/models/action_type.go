package models

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// ActionOperationKind mirrors `enum ActionOperationKind` in
// `libs/ontology-kernel/src/models/action_type.rs`. Every variant
// serialises to its snake_case spelling for both serde and sqlx
// (TASK I additions are present verbatim).
type ActionOperationKind string

const (
	ActionOperationKindUpdateObject        ActionOperationKind = "update_object"
	ActionOperationKindCreateLink          ActionOperationKind = "create_link"
	ActionOperationKindDeleteObject        ActionOperationKind = "delete_object"
	ActionOperationKindInvokeFunction      ActionOperationKind = "invoke_function"
	ActionOperationKindInvokeWebhook       ActionOperationKind = "invoke_webhook"
	ActionOperationKindCreateInterface     ActionOperationKind = "create_interface"
	ActionOperationKindModifyInterface     ActionOperationKind = "modify_interface"
	ActionOperationKindDeleteInterface     ActionOperationKind = "delete_interface"
	ActionOperationKindCreateInterfaceLink ActionOperationKind = "create_interface_link"
	ActionOperationKindDeleteInterfaceLink ActionOperationKind = "delete_interface_link"
)

// String mirrors `impl Display for ActionOperationKind`.
func (k ActionOperationKind) String() string { return string(k) }

// ActionInputField mirrors `struct ActionInputField`. `required` is
// `#[serde(default)]` (false when missing), `struct_fields` is
// `skip_serializing_if = "Option::is_none"` so absent / nil
// pointers omit the key entirely.
type ActionInputField struct {
	Name         string             `json:"name"`
	DisplayName  *string            `json:"display_name"`
	Description  *string            `json:"description"`
	PropertyType string             `json:"property_type"`
	Required     bool               `json:"required"`
	DefaultValue json.RawMessage    `json:"default_value"`
	StructFields *[]ActionInputField `json:"struct_fields,omitempty"`
}

// ActionPropertyMapping mirrors `struct ActionPropertyMapping`.
type ActionPropertyMapping struct {
	PropertyName string          `json:"property_name"`
	InputName    *string         `json:"input_name"`
	Value        json.RawMessage `json:"value"`
}

// UpdateObjectActionConfig mirrors `struct UpdateObjectActionConfig`.
// `static_patch` is `#[serde(default)]` Option<Value>` — null when
// missing, encoded as `null` when nil.
type UpdateObjectActionConfig struct {
	PropertyMappings []ActionPropertyMapping `json:"property_mappings"`
	StaticPatch      json.RawMessage         `json:"static_patch"`
}

// ActionFormSchema mirrors `struct ActionFormSchema`. Both inner
// vecs carry `skip_serializing_if = "Vec::is_empty"`.
type ActionFormSchema struct {
	Sections           []ActionFormSection           `json:"sections,omitempty"`
	ParameterOverrides []ActionFormParameterOverride `json:"parameter_overrides,omitempty"`
}

// ActionFormSection mirrors `struct ActionFormSection`. Custom defaults:
//   * `columns` defaults to 1 (`default_action_form_section_columns`).
//   * `visible` defaults to true (`default_action_form_section_visible`).
//   * `parameter_names`, `overrides` default to empty Vec.
//   * Empty Vec fields skip on serialise.
type ActionFormSection struct {
	ID             string                       `json:"id"`
	Title          *string                      `json:"title,omitempty"`
	Description    *string                      `json:"description,omitempty"`
	Columns        uint8                        `json:"columns"`
	Collapsible    bool                         `json:"collapsible"`
	Visible        bool                         `json:"visible"`
	ParameterNames []string                     `json:"parameter_names,omitempty"`
	Overrides      []ActionFormSectionOverride  `json:"overrides,omitempty"`
}

// UnmarshalJSON applies `default_action_form_section_columns()` and
// `default_action_form_section_visible()` for missing keys.
func (s *ActionFormSection) UnmarshalJSON(b []byte) error {
	type alias ActionFormSection
	a := alias{Columns: 1, Visible: true}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	if _, ok := raw["columns"]; !ok {
		a.Columns = 1
	}
	if _, ok := raw["visible"]; !ok {
		a.Visible = true
	}
	*s = ActionFormSection(a)
	return nil
}

// ActionFormSectionOverride mirrors `struct ActionFormSectionOverride`.
type ActionFormSectionOverride struct {
	Conditions  []ActionFormCondition `json:"conditions,omitempty"`
	Hidden      *bool                 `json:"hidden,omitempty"`
	Columns     *uint8                `json:"columns,omitempty"`
	Title       *string               `json:"title,omitempty"`
	Description *string               `json:"description,omitempty"`
}

// ActionFormParameterOverride mirrors `struct ActionFormParameterOverride`.
type ActionFormParameterOverride struct {
	ParameterName string                `json:"parameter_name"`
	Conditions    []ActionFormCondition `json:"conditions,omitempty"`
	Hidden        *bool                 `json:"hidden,omitempty"`
	Required      *bool                 `json:"required,omitempty"`
	DefaultValue  json.RawMessage       `json:"default_value,omitempty"`
	DisplayName   *string               `json:"display_name,omitempty"`
	Description   *string               `json:"description,omitempty"`
}

// ActionFormCondition mirrors `struct ActionFormCondition`. `operator`
// defaults to `"is"` (`default_action_form_condition_operator`).
type ActionFormCondition struct {
	Left     string          `json:"left"`
	Operator string          `json:"operator"`
	Right    json.RawMessage `json:"right,omitempty"`
}

// UnmarshalJSON applies `default_action_form_condition_operator()`.
func (c *ActionFormCondition) UnmarshalJSON(b []byte) error {
	type alias ActionFormCondition
	a := alias{Operator: "is"}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	if err := json.Unmarshal(b, &a); err != nil {
		return err
	}
	if _, ok := raw["operator"]; !ok {
		a.Operator = "is"
	}
	*c = ActionFormCondition(a)
	return nil
}

// ActionAuthorizationPolicy mirrors `struct ActionAuthorizationPolicy`.
// All Vec / HashMap fields skip when empty, matching serde.
type ActionAuthorizationPolicy struct {
	RequiredPermissionKeys []string                   `json:"required_permission_keys,omitempty"`
	AnyRole                []string                   `json:"any_role,omitempty"`
	AllRoles               []string                   `json:"all_roles,omitempty"`
	AttributeEquals        map[string]json.RawMessage `json:"attribute_equals,omitempty"`
	AllowedMarkings        []string                   `json:"allowed_markings,omitempty"`
	MinimumClearance       *string                    `json:"minimum_clearance,omitempty"`
	DenyGuestSessions      bool                       `json:"deny_guest_sessions"`
}

// ActionTypeRow mirrors `struct ActionTypeRow`.
type ActionTypeRow struct {
	ID                   uuid.UUID       `db:"id"`
	Name                 string          `db:"name"`
	DisplayName          string          `db:"display_name"`
	Description          string          `db:"description"`
	ObjectTypeID         uuid.UUID       `db:"object_type_id"`
	OperationKind        string          `db:"operation_kind"`
	InputSchema          json.RawMessage `db:"input_schema"`
	FormSchema           json.RawMessage `db:"form_schema"`
	Config               json.RawMessage `db:"config"`
	ConfirmationRequired bool            `db:"confirmation_required"`
	PermissionKey        *string         `db:"permission_key"`
	AuthorizationPolicy  json.RawMessage `db:"authorization_policy"`
	OwnerID              uuid.UUID       `db:"owner_id"`
	CreatedAt            time.Time       `db:"created_at"`
	UpdatedAt            time.Time       `db:"updated_at"`
}

// ActionType mirrors `struct ActionType`.
type ActionType struct {
	ID                   uuid.UUID                 `json:"id"`
	Name                 string                    `json:"name"`
	DisplayName          string                    `json:"display_name"`
	Description          string                    `json:"description"`
	ObjectTypeID         uuid.UUID                 `json:"object_type_id"`
	OperationKind        string                    `json:"operation_kind"`
	InputSchema          []ActionInputField        `json:"input_schema"`
	FormSchema           ActionFormSchema          `json:"form_schema"`
	Config               json.RawMessage           `json:"config"`
	ConfirmationRequired bool                      `json:"confirmation_required"`
	PermissionKey        *string                   `json:"permission_key"`
	AuthorizationPolicy  ActionAuthorizationPolicy `json:"authorization_policy"`
	OwnerID              uuid.UUID                 `json:"owner_id"`
	CreatedAt            time.Time                 `json:"created_at"`
	UpdatedAt            time.Time                 `json:"updated_at"`
}

// IntoAction mirrors `TryFrom<ActionTypeRow> for ActionType`. Per
// Rust, JSON parse failures fall back to defaults
// (`unwrap_or_default()`).
func (row ActionTypeRow) IntoAction() ActionType {
	var input []ActionInputField
	if len(row.InputSchema) > 0 {
		_ = json.Unmarshal(row.InputSchema, &input)
	}
	var form ActionFormSchema
	if len(row.FormSchema) > 0 {
		_ = json.Unmarshal(row.FormSchema, &form)
	}
	var policy ActionAuthorizationPolicy
	if len(row.AuthorizationPolicy) > 0 {
		_ = json.Unmarshal(row.AuthorizationPolicy, &policy)
	}
	return ActionType{
		ID:                   row.ID,
		Name:                 row.Name,
		DisplayName:          row.DisplayName,
		Description:          row.Description,
		ObjectTypeID:         row.ObjectTypeID,
		OperationKind:        row.OperationKind,
		InputSchema:          input,
		FormSchema:           form,
		Config:               row.Config,
		ConfirmationRequired: row.ConfirmationRequired,
		PermissionKey:        row.PermissionKey,
		AuthorizationPolicy:  policy,
		OwnerID:              row.OwnerID,
		CreatedAt:            row.CreatedAt,
		UpdatedAt:            row.UpdatedAt,
	}
}

// CreateActionTypeRequest mirrors `struct CreateActionTypeRequest`.
type CreateActionTypeRequest struct {
	Name                 string                     `json:"name"`
	DisplayName          *string                    `json:"display_name,omitempty"`
	Description          *string                    `json:"description,omitempty"`
	ObjectTypeID         uuid.UUID                  `json:"object_type_id"`
	OperationKind        string                     `json:"operation_kind"`
	InputSchema          *[]ActionInputField        `json:"input_schema,omitempty"`
	FormSchema           *ActionFormSchema          `json:"form_schema,omitempty"`
	Config               json.RawMessage            `json:"config,omitempty"`
	ConfirmationRequired *bool                      `json:"confirmation_required,omitempty"`
	PermissionKey        *string                    `json:"permission_key,omitempty"`
	AuthorizationPolicy  *ActionAuthorizationPolicy `json:"authorization_policy,omitempty"`
}

// UpdateActionTypeRequest mirrors `struct UpdateActionTypeRequest`.
type UpdateActionTypeRequest struct {
	DisplayName          *string                    `json:"display_name,omitempty"`
	Description          *string                    `json:"description,omitempty"`
	OperationKind        *string                    `json:"operation_kind,omitempty"`
	InputSchema          *[]ActionInputField        `json:"input_schema,omitempty"`
	FormSchema           *ActionFormSchema          `json:"form_schema,omitempty"`
	Config               json.RawMessage            `json:"config,omitempty"`
	ConfirmationRequired *bool                      `json:"confirmation_required,omitempty"`
	PermissionKey        *string                    `json:"permission_key,omitempty"`
	AuthorizationPolicy  *ActionAuthorizationPolicy `json:"authorization_policy,omitempty"`
}

// ListActionTypesQuery mirrors `struct ListActionTypesQuery`.
type ListActionTypesQuery struct {
	ObjectTypeID *uuid.UUID `json:"object_type_id,omitempty"`
	Page         *int64     `json:"page,omitempty"`
	PerPage      *int64     `json:"per_page,omitempty"`
	Search       *string    `json:"search,omitempty"`
}

// ListActionTypesResponse mirrors `struct ListActionTypesResponse`.
type ListActionTypesResponse struct {
	Data    []ActionType `json:"data"`
	Total   int64        `json:"total"`
	Page    int64        `json:"page"`
	PerPage int64        `json:"per_page"`
}

// ValidateActionRequest mirrors `struct ValidateActionRequest`.
// `parameters` is `#[serde(default)]` Value.
type ValidateActionRequest struct {
	TargetObjectID *uuid.UUID      `json:"target_object_id,omitempty"`
	Parameters     json.RawMessage `json:"parameters"`
}

// ValidateActionResponse mirrors `struct ValidateActionResponse`.
type ValidateActionResponse struct {
	Valid   bool            `json:"valid"`
	Errors  []string        `json:"errors"`
	Preview json.RawMessage `json:"preview"`
}

// ExecuteActionRequest mirrors `struct ExecuteActionRequest`.
type ExecuteActionRequest struct {
	TargetObjectID *uuid.UUID      `json:"target_object_id,omitempty"`
	Parameters     json.RawMessage `json:"parameters"`
	Justification  *string         `json:"justification,omitempty"`
}

// ExecuteBatchActionRequest mirrors `struct ExecuteBatchActionRequest`.
type ExecuteBatchActionRequest struct {
	TargetObjectIDs []uuid.UUID     `json:"target_object_ids"`
	Parameters      json.RawMessage `json:"parameters"`
	Justification   *string         `json:"justification,omitempty"`
}

// ExecuteActionResponse mirrors `struct ExecuteActionResponse`.
type ExecuteActionResponse struct {
	Action         ActionType      `json:"action"`
	TargetObjectID *uuid.UUID      `json:"target_object_id"`
	Deleted        bool            `json:"deleted"`
	Preview        json.RawMessage `json:"preview"`
	Object         json.RawMessage `json:"object"`
	Link           json.RawMessage `json:"link"`
	Result         json.RawMessage `json:"result"`
}

// ExecuteBatchActionResponse mirrors `struct ExecuteBatchActionResponse`.
type ExecuteBatchActionResponse struct {
	Action    ActionType        `json:"action"`
	Total     int               `json:"total"`
	Succeeded int               `json:"succeeded"`
	Failed    int               `json:"failed"`
	Results   []json.RawMessage `json:"results"`
}

// ActionWhatIfBranch mirrors `struct ActionWhatIfBranch`.
type ActionWhatIfBranch struct {
	ID             uuid.UUID       `json:"id"               db:"id"`
	ActionID       uuid.UUID       `json:"action_id"        db:"action_id"`
	TargetObjectID *uuid.UUID      `json:"target_object_id" db:"target_object_id"`
	Name           string          `json:"name"             db:"name"`
	Description    string          `json:"description"      db:"description"`
	Parameters     json.RawMessage `json:"parameters"       db:"parameters"`
	Preview        json.RawMessage `json:"preview"          db:"preview"`
	BeforeObject   json.RawMessage `json:"before_object"    db:"before_object"`
	AfterObject    json.RawMessage `json:"after_object"     db:"after_object"`
	Deleted        bool            `json:"deleted"          db:"deleted"`
	OwnerID        uuid.UUID       `json:"owner_id"         db:"owner_id"`
	CreatedAt      time.Time       `json:"created_at"       db:"created_at"`
	UpdatedAt      time.Time       `json:"updated_at"       db:"updated_at"`
}

// CreateActionWhatIfBranchRequest mirrors `struct CreateActionWhatIfBranchRequest`.
type CreateActionWhatIfBranchRequest struct {
	TargetObjectID *uuid.UUID      `json:"target_object_id,omitempty"`
	Parameters     json.RawMessage `json:"parameters"`
	Name           *string         `json:"name,omitempty"`
	Description    *string         `json:"description,omitempty"`
}

// ListActionWhatIfBranchesQuery mirrors `struct ListActionWhatIfBranchesQuery`.
type ListActionWhatIfBranchesQuery struct {
	TargetObjectID *uuid.UUID `json:"target_object_id,omitempty"`
	Page           *int64     `json:"page,omitempty"`
	PerPage        *int64     `json:"per_page,omitempty"`
}

// ListActionWhatIfBranchesResponse mirrors `struct ListActionWhatIfBranchesResponse`.
type ListActionWhatIfBranchesResponse struct {
	Data    []ActionWhatIfBranch `json:"data"`
	Total   int64                `json:"total"`
	Page    int64                `json:"page"`
	PerPage int64                `json:"per_page"`
}
