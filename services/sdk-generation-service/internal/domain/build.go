// Package domain holds the OSDK build state-machine types. These are
// shared between the repo, handler, and worker — keeping them out of
// internal/models keeps the wire-stable PrimaryItem/SecondaryItem
// envelopes from leaking into the new build pipeline.
package domain

import (
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Target is the SDK output language. Today only "ts" is wired
// end-to-end; "python" and "java" are accepted at the API boundary so
// callers can pre-register the target choice without needing a v2
// schema once the generators land.
type Target string

const (
	TargetTypeScript Target = "ts"
	TargetPython     Target = "python"
	TargetJava       Target = "java"
)

// ParseTarget normalises wire vocabulary ("typescript", "ts", "TS").
func ParseTarget(s string) (Target, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "ts", "typescript":
		return TargetTypeScript, nil
	case "python", "py":
		return TargetPython, nil
	case "java":
		return TargetJava, nil
	default:
		return "", fmt.Errorf("unsupported target %q", s)
	}
}

// Status is the OSDK build lifecycle.
type Status string

const (
	StatusQueued    Status = "queued"
	StatusBuilding  Status = "building"
	StatusSucceeded Status = "succeeded"
	StatusFailed    Status = "failed"
)

// SDKBuild is a row in `sdk_builds`. A build is the unit of work the
// worker pulls off the queue: one ontology snapshot in, one tarball
// of TS/Python/Java source out.
type SDKBuild struct {
	ID              uuid.UUID  `json:"id"`
	TenantID        uuid.UUID  `json:"tenant_id"`
	OntologyVersion string     `json:"ontology_version"`
	Target          Target     `json:"target"`
	Status          Status     `json:"status"`
	ArtifactURI     string     `json:"artifact_uri,omitempty"`
	ErrorMessage    string     `json:"error_message,omitempty"`
	RequestedBy     uuid.UUID  `json:"requested_by"`
	CreatedAt       time.Time  `json:"created_at"`
	FinishedAt      *time.Time `json:"finished_at,omitempty"`
}

// SDKRequest is the body of POST /api/v1/sdks/builds. The Include
// filters are optional whitelists — empty means "every object/action
// type in the snapshot ends up in the generated package".
type SDKRequest struct {
	TenantID           uuid.UUID `json:"tenant_id"`
	OntologyVersion    string    `json:"ontology_version"`
	Target             Target    `json:"target"`
	IncludeObjectTypes []string  `json:"include_object_types,omitempty"`
	IncludeActionTypes []string  `json:"include_action_types,omitempty"`
}

// Validate enforces invariants the handler layer relies on before it
// touches the repo or the generator.
func (r *SDKRequest) Validate() error {
	if r.TenantID == uuid.Nil {
		return fmt.Errorf("tenant_id is required")
	}
	if strings.TrimSpace(r.OntologyVersion) == "" {
		return fmt.Errorf("ontology_version is required")
	}
	if _, err := ParseTarget(string(r.Target)); err != nil {
		return err
	}
	return nil
}
