// Sensitive-Data-Scanning (SDS) request + response payloads.
//
// Mirrors `services/audit-compliance-service/src/sds/models/sensitive_data.rs`
// 1:1.
package models

import (
	"github.com/google/uuid"
)

// SensitiveDataScope enumerates the data-scope buckets used for
// risk-scoring (record/dataset/file get a 2× multiplier; prompts +
// messages stay at 1×).
type SensitiveDataScope string

const (
	SDSScopeRecord  SensitiveDataScope = "record"
	SDSScopeDataset SensitiveDataScope = "dataset"
	SDSScopeFile    SensitiveDataScope = "file"
	SDSScopePrompt  SensitiveDataScope = "prompt"
	SDSScopeMessage SensitiveDataScope = "message"
)

// ParseSensitiveDataScope returns the typed enum for a wire string.
// Unknown strings default to `record` (Rust's `Default::default`).
func ParseSensitiveDataScope(value string) SensitiveDataScope {
	switch value {
	case "dataset":
		return SDSScopeDataset
	case "file":
		return SDSScopeFile
	case "prompt":
		return SDSScopePrompt
	case "message":
		return SDSScopeMessage
	default:
		return SDSScopeRecord
	}
}

// SensitiveDataFinding mirrors the Rust struct.
type SensitiveDataFinding struct {
	Kind       string `json:"kind"`
	Value      string `json:"value"`
	Redacted   string `json:"redacted"`
	MatchCount int    `json:"match_count"`
	Severity   string `json:"severity"`
}

// SensitiveDataScanRequest mirrors the Rust struct (POST `/sds/scan`).
type SensitiveDataScanRequest struct {
	Content string             `json:"content"`
	Redact  *bool              `json:"redact,omitempty"`
	Scope   SensitiveDataScope `json:"scope,omitempty"`
}

// EffectiveRedact applies the Rust default of `true`.
func (r *SensitiveDataScanRequest) EffectiveRedact() bool {
	if r.Redact == nil {
		return true
	}
	return *r.Redact
}

// EffectiveScope applies the Rust default of `record`.
func (r *SensitiveDataScanRequest) EffectiveScope() SensitiveDataScope {
	if r.Scope == "" {
		return SDSScopeRecord
	}
	return r.Scope
}

// SensitiveDataScanResponse mirrors the Rust struct.
type SensitiveDataScanResponse struct {
	Findings        []SensitiveDataFinding `json:"findings"`
	RedactedContent string                 `json:"redacted_content"`
	RiskScore       uint32                 `json:"risk_score"`
}

// RunSensitiveDataScanRequest mirrors the Rust struct (POST `/sds/jobs`).
type RunSensitiveDataScanRequest struct {
	TargetName  string             `json:"target_name"`
	Content     string             `json:"content"`
	Redact      *bool              `json:"redact,omitempty"`
	Scope       SensitiveDataScope `json:"scope,omitempty"`
	RequestedBy *uuid.UUID         `json:"requested_by,omitempty"`
}

// EffectiveRedact applies the same default as the scan request.
func (r *RunSensitiveDataScanRequest) EffectiveRedact() bool {
	if r.Redact == nil {
		return true
	}
	return *r.Redact
}

// EffectiveScope applies the same default as the scan request.
func (r *RunSensitiveDataScanRequest) EffectiveScope() SensitiveDataScope {
	if r.Scope == "" {
		return SDSScopeRecord
	}
	return r.Scope
}

// SDSIssueStatus enumerates open/resolved/suppressed.
type SDSIssueStatus string

const (
	SDSIssueOpen       SDSIssueStatus = "open"
	SDSIssueResolved   SDSIssueStatus = "resolved"
	SDSIssueSuppressed SDSIssueStatus = "suppressed"
)

// MarkSensitiveIssueRequest mirrors the Rust struct.
type MarkSensitiveIssueRequest struct {
	Markings           []string `json:"markings,omitempty"`
	RemediationActions []string `json:"remediation_actions,omitempty"`
	Resolve            bool     `json:"resolve,omitempty"`
}

// SDSMatchCondition mirrors the Rust `MatchCondition` (rule selector).
type SDSMatchCondition struct {
	Field    string `json:"field"`
	Operator string `json:"operator"`
	Value    string `json:"value"`
}

// CreateRemediationRuleRequest mirrors the Rust struct.
type CreateRemediationRuleRequest struct {
	Name               string              `json:"name"`
	Scope              string              `json:"scope"`
	MatchConditions    []SDSMatchCondition `json:"match_conditions,omitempty"`
	RemediationActions []string            `json:"remediation_actions,omitempty"`
	UpdatedBy          *uuid.UUID          `json:"updated_by,omitempty"`
}
