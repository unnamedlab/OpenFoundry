// Compliance-report + GDPR types.
//
// Mirrors `services/audit-compliance-service/src/models/compliance_report.rs`
// 1:1.
package models

import (
	"fmt"
	"time"
)

// ComplianceStandard enumerates the five supported standards.
type ComplianceStandard string

const (
	StandardSoc2     ComplianceStandard = "soc2"
	StandardIso27001 ComplianceStandard = "iso27001"
	StandardHipaa    ComplianceStandard = "hipaa"
	StandardGdpr     ComplianceStandard = "gdpr"
	StandardItar     ComplianceStandard = "itar"
)

// ParseComplianceStandard mirrors `ComplianceStandard::from_str`.
func ParseComplianceStandard(value string) (ComplianceStandard, error) {
	switch value {
	case "soc2":
		return StandardSoc2, nil
	case "iso27001":
		return StandardIso27001, nil
	case "hipaa":
		return StandardHipaa, nil
	case "gdpr":
		return StandardGdpr, nil
	case "itar":
		return StandardItar, nil
	default:
		return "", fmt.Errorf("unsupported compliance standard: %s", value)
	}
}

// ComplianceFinding mirrors the Rust struct.
type ComplianceFinding struct {
	ControlID string `json:"control_id"`
	Title     string `json:"title"`
	Status    string `json:"status"`
	Evidence  string `json:"evidence"`
}

// ComplianceArtifact mirrors the Rust struct.
type ComplianceArtifact struct {
	FileName   string `json:"file_name"`
	MimeType   string `json:"mime_type"`
	StorageURL string `json:"storage_url"`
	Checksum   string `json:"checksum"`
	SizeBytes  int64  `json:"size_bytes"`
}

// ComplianceReportRequest mirrors the Rust struct.
type ComplianceReportRequest struct {
	Standard    ComplianceStandard `json:"standard"`
	Title       string             `json:"title"`
	Scope       string             `json:"scope"`
	WindowStart time.Time          `json:"window_start"`
	WindowEnd   time.Time          `json:"window_end"`
}

// GdprExportRequest mirrors the Rust struct.
type GdprExportRequest struct {
	SubjectID      string `json:"subject_id"`
	PortableFormat string `json:"portable_format,omitempty"`
}

// EffectivePortableFormat applies the Rust default of "json" when
// the caller omits the field.
func (r *GdprExportRequest) EffectivePortableFormat() string {
	if r.PortableFormat == "" {
		return "json"
	}
	return r.PortableFormat
}

// GdprExportPayload mirrors the Rust struct.
type GdprExportPayload struct {
	SubjectID      string       `json:"subject_id"`
	GeneratedAt    time.Time    `json:"generated_at"`
	PortableFormat string       `json:"portable_format"`
	EventCount     int          `json:"event_count"`
	Resources      []string     `json:"resources"`
	AuditExcerpt   []AuditEvent `json:"audit_excerpt"`
}

// GdprEraseRequest mirrors the Rust struct.
type GdprEraseRequest struct {
	SubjectID  string `json:"subject_id"`
	HardDelete bool   `json:"hard_delete,omitempty"`
	LegalHold  bool   `json:"legal_hold,omitempty"`
}

// GdprEraseResponse mirrors the Rust struct.
type GdprEraseResponse struct {
	SubjectID          string     `json:"subject_id"`
	RequestedAt        time.Time  `json:"requested_at"`
	CompletedAt        *time.Time `json:"completed_at"`
	Status             string     `json:"status"`
	MaskedEventCount   int        `json:"masked_event_count"`
	AffectedResources  []string   `json:"affected_resources"`
	LegalHold          bool       `json:"legal_hold"`
}
