// Package domain holds the pure validation + transition rules for the
// document-intelligence sub-domain. No DB, no HTTP — testable as
// table-driven Go.
package domain

import (
	"errors"
	"fmt"
	"strings"

	"github.com/openfoundry/openfoundry-go/services/retrieval-context-service/internal/models"
)

// Sentinel errors mapped by the HTTP layer to status codes.
var (
	ErrInvalidInput         = errors.New("invalid input")
	ErrNotFound             = errors.New("not found")
	ErrPreconditionFailed   = errors.New("precondition failed")
	ErrConflict             = errors.New("conflict")
	ErrUnknownStatus        = errors.New("unknown status")
	ErrIllegalStateTransition = errors.New("illegal state transition")
)

// ValidateCreateJob enforces the invariants for a new job:
// non-empty source_uri + pipeline, well-known status (when set),
// payload of options is valid JSON when present.
func ValidateCreateJob(req models.CreateJobRequest) error {
	if strings.TrimSpace(req.SourceURI) == "" {
		return fmt.Errorf("%w: source_uri is required", ErrInvalidInput)
	}
	if strings.TrimSpace(req.Pipeline) == "" {
		return fmt.Errorf("%w: pipeline is required", ErrInvalidInput)
	}
	if req.Status != nil && !req.Status.IsValid() {
		return fmt.Errorf("%w: %q", ErrUnknownStatus, *req.Status)
	}
	return nil
}

// ValidateUpdateJob enforces invariants on PATCH:
// at least one field must be set, status (when set) must be known,
// transitions must be legal w.r.t. current.
func ValidateUpdateJob(current models.Job, req models.UpdateJobRequest) error {
	if req.Status == nil && req.MimeType == nil && len(req.Options) == 0 {
		return fmt.Errorf("%w: at least one of status, mime_type, options is required", ErrInvalidInput)
	}
	if req.Status != nil {
		if !req.Status.IsValid() {
			return fmt.Errorf("%w: %q", ErrUnknownStatus, *req.Status)
		}
		if !AllowedTransition(current.Status, *req.Status) {
			return fmt.Errorf("%w: %s -> %s", ErrIllegalStateTransition, current.Status, *req.Status)
		}
	}
	return nil
}

// ValidateAppendEvent enforces a known status token.
func ValidateAppendEvent(req models.AppendEventRequest) error {
	if !req.Status.IsValid() {
		return fmt.Errorf("%w: %q", ErrUnknownStatus, req.Status)
	}
	return nil
}

// ValidateRecordExtraction enforces non-empty kind + valid confidence range.
func ValidateRecordExtraction(req models.RecordExtractionRequest) error {
	if strings.TrimSpace(req.ExtractionKind) == "" {
		return fmt.Errorf("%w: extraction_kind is required", ErrInvalidInput)
	}
	if req.Confidence != nil && (*req.Confidence < 0 || *req.Confidence > 1) {
		return fmt.Errorf("%w: confidence must be in [0,1]", ErrInvalidInput)
	}
	return nil
}

// AllowedTransition mirrors the substrate state machine:
//
//	queued -> running | cancelled
//	running -> succeeded | failed | cancelled
//	succeeded/failed/cancelled are terminal (no further transitions).
//
// A no-op transition (X -> X) is allowed so callers can re-stamp the
// same status idempotently — common when consumers replay events.
func AllowedTransition(from, to models.JobStatus) bool {
	if from == to {
		return true
	}
	switch from {
	case models.JobStatusQueued:
		return to == models.JobStatusRunning || to == models.JobStatusCancelled
	case models.JobStatusRunning:
		return to == models.JobStatusSucceeded || to == models.JobStatusFailed || to == models.JobStatusCancelled
	default:
		return false
	}
}
