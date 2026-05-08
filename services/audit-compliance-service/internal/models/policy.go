// Audit-policy CRUD types.
//
// Mirrors `services/audit-compliance-service/src/models/policy.rs` 1:1.
package models

import "encoding/json"

// CreateAuditPolicyRequest mirrors the Rust struct of the same name.
//
// `Description`, `LegalHold` and `Active` honour serde defaults
// (`""`, `false`, `true`).
type CreateAuditPolicyRequest struct {
	Name           string              `json:"name"`
	Description    string              `json:"description,omitempty"`
	Scope          string              `json:"scope"`
	Classification ClassificationLevel `json:"classification"`
	RetentionDays  int32               `json:"retention_days"`
	LegalHold      bool                `json:"legal_hold,omitempty"`
	PurgeMode      string              `json:"purge_mode"`
	// Active defaults to true on the Rust side; *bool here so callers
	// can omit it and the handler knows when to apply the default.
	Active    *bool    `json:"active,omitempty"`
	Rules     []string `json:"rules,omitempty"`
	UpdatedBy string   `json:"updated_by"`
}

// EffectiveActive returns the Active flag with the Rust default of
// `true` applied.
func (r *CreateAuditPolicyRequest) EffectiveActive() bool {
	if r.Active == nil {
		return true
	}
	return *r.Active
}

// UpdateAuditPolicyRequest mirrors the Rust struct of the same name —
// every field is optional and applied "if present".
type UpdateAuditPolicyRequest struct {
	Name           *string              `json:"name,omitempty"`
	Description    *string              `json:"description,omitempty"`
	Scope          *string              `json:"scope,omitempty"`
	Classification *ClassificationLevel `json:"classification,omitempty"`
	RetentionDays  *int32               `json:"retention_days,omitempty"`
	LegalHold      *bool                `json:"legal_hold,omitempty"`
	PurgeMode      *string              `json:"purge_mode,omitempty"`
	Active         *bool                `json:"active,omitempty"`
	Rules          *[]string            `json:"rules,omitempty"`
	UpdatedBy      *string              `json:"updated_by,omitempty"`
}

// MarshalRules returns the rules slice as a JSONB-friendly value.
func MarshalRules(rules []string) (json.RawMessage, error) {
	if rules == nil {
		rules = []string{}
	}
	return json.Marshal(rules)
}

// UnmarshalRules decodes a JSONB-shaped column back to a string slice.
func UnmarshalRules(raw json.RawMessage) ([]string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return []string{}, nil
	}
	var out []string
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}
