// Package export ports
// `services/audit-compliance-service/src/domain/export.rs` 1:1.
//
// Builds a synthesized ComplianceReport for one of the five
// supported standards. Findings are hard-coded per standard (same
// strings as the Rust impl); the artifact metadata + size_bytes are
// derived from the relevant events count + active policies.
package export

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/models"
)

// BuildReport mirrors `build_report`. Pure no-IO; the handler is
// responsible for persisting the row.
func BuildReport(request *models.ComplianceReportRequest, events []models.AuditEvent, policies []models.AuditPolicy) (models.ComplianceReport, error) {
	standard := request.Standard
	relevantEvents := 0
	for i := range events {
		if !events[i].OccurredAt.Before(request.WindowStart) && !events[i].OccurredAt.After(request.WindowEnd) {
			relevantEvents++
		}
	}

	findings := findingsFor(standard)
	findingsRaw, err := json.Marshal(findings)
	if err != nil {
		return models.ComplianceReport{}, err
	}

	generatedAt := time.Now().UTC()
	artifact := models.ComplianceArtifact{
		FileName: fmt.Sprintf("%s-%s.zip", string(standard), generatedAt.Format("200601021504")),
		MimeType: "application/zip",
		StorageURL: fmt.Sprintf("s3://compliance-reports/%s/%d.zip",
			strings.ReplaceAll(request.Scope, " ", "-"),
			generatedAt.Unix()),
		Checksum:  fmt.Sprintf("cmp-%x", generatedAt.Unix()),
		SizeBytes: 32_768 + int64(relevantEvents)*14 + int64(len(policies))*64,
	}
	artifactRaw, err := json.Marshal(artifact)
	if err != nil {
		return models.ComplianceReport{}, err
	}

	return models.ComplianceReport{
		ID:                 uuid.New(),
		Standard:           string(standard),
		Title:              request.Title,
		Scope:              request.Scope,
		WindowStart:        request.WindowStart,
		WindowEnd:          request.WindowEnd,
		GeneratedAt:        generatedAt,
		Status:             "ready",
		Findings:           findingsRaw,
		Artifact:           artifactRaw,
		RelevantEventCount: int64(relevantEvents),
		PolicyCount:        int64(len(policies)),
		ControlSummary:     fmt.Sprintf("%d controls evidenced across %d events", 2, relevantEvents),
		ExpiresAt:          generatedAt.Add(30 * 24 * time.Hour),
	}, nil
}

func findingsFor(standard models.ComplianceStandard) []models.ComplianceFinding {
	switch standard {
	case models.StandardSoc2:
		return []models.ComplianceFinding{
			{ControlID: "CC7.2", Title: "Access monitoring in place", Status: "pass",
				Evidence: "Gateway and auth events are chained in the immutable log."},
			{ControlID: "CC8.1", Title: "Retention policy defined", Status: "pass",
				Evidence: "Retention TTL policies are attached to audit classes and reviewed monthly."},
		}
	case models.StandardIso27001:
		return []models.ComplianceFinding{
			{ControlID: "A.5.34", Title: "Privacy and PII classification", Status: "pass",
				Evidence: "Sensitive events are labeled as PII or confidential before export."},
			{ControlID: "A.8.15", Title: "Logging", Status: "pass",
				Evidence: "Append-only hash chaining preserves the integrity of event history."},
		}
	case models.StandardHipaa:
		return []models.ComplianceFinding{
			{ControlID: "164.312(b)", Title: "Audit controls", Status: "pass",
				Evidence: "Access and disclosure actions are retained with subject linkage."},
			{ControlID: "164.526", Title: "Amendment and erasure workflow", Status: "pass",
				Evidence: "GDPR/erasure workflows mask subject data while preserving traceability."},
		}
	case models.StandardGdpr:
		return []models.ComplianceFinding{
			{ControlID: "Art. 5(1)(e)", Title: "Storage limitation enforced", Status: "pass",
				Evidence: "Audit policies attach explicit retention windows and erase workflows to subject-linked data."},
			{ControlID: "Art. 15/17", Title: "Subject rights workflow", Status: "pass",
				Evidence: "Export and erasure endpoints provide portable exports and subject masking with audit traceability."},
		}
	case models.StandardItar:
		return []models.ComplianceFinding{
			{ControlID: "ITAR 122.5", Title: "Export access review", Status: "pass",
				Evidence: "Controlled exports can be attached to approval and lineage evidence for cross-border restrictions."},
			{ControlID: "ITAR 123.26", Title: "Controlled data handling", Status: "pass",
				Evidence: "Confidential/controlled classifications and governance templates preserve retention and hold requirements."},
		}
	}
	return []models.ComplianceFinding{}
}
