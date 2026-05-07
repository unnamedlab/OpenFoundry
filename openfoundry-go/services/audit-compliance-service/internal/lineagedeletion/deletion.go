// Package lineagedeletion ports the lineage-deletion subsystem 1:1
// from `services/audit-compliance-service/src/lineage_deletion/`.
//
// Surface:
//
//   - ComputeImpact      — HTTP call to lineage-service /api/v1/lineage/datasets/{id}/impact
//   - ExecuteSafeDeletion— storage delete (best-effort) honouring legal_hold
//   - BuildAuditTrace    — JSON audit-trace record
//   - PersistDeletion    — INSERT into lineage_deletion_requests
//   - EmitRetentionDelete— `audit` slog event for the retention runner
package lineagedeletion

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/models"
)

// StorageBackend is the slim abstraction the safe-deletion path needs.
//
// Mirrors `storage_abstraction::StorageBackend::delete` but only the
// `delete(path)` surface — that's the only call the deletion path
// makes today. Keeping this local avoids a dependency on the full
// storage-abstraction lib for a single call.
type StorageBackend interface {
	Delete(ctx context.Context, path string) error
}

// HTTPClient lets unit tests inject a stub instead of pulling reqwest-
// shaped client tests into the lineage-service URL.
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// ComputeImpact ports `domain::deletion::compute_impact`. Falls back
// to a "safe empty" summary when the lineage service is unreachable.
func ComputeImpact(ctx context.Context, client HTTPClient, lineageServiceURL string, datasetID uuid.UUID, legalHold bool) (models.LineageImpactSummary, error) {
	endpoint := fmt.Sprintf("%s/api/v1/lineage/datasets/%s/impact",
		strings.TrimRight(lineageServiceURL, "/"), datasetID)

	fallback := models.LineageImpactSummary{
		DownstreamNodeCount:  0,
		DownstreamDatasetIDs: []uuid.UUID{},
		BlockedByLegalHold:   legalHold,
		ImpactNotes: []string{
			"lineage impact service unavailable; using safe empty fallback",
		},
	}
	if legalHold {
		fallback.ImpactNotes = []string{"deletion is subject to legal hold"}
	}

	if client == nil {
		return fallback, nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fallback, nil
	}
	resp, err := client.Do(req)
	if err != nil {
		return fallback, nil
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fallback, nil
	}
	var payload struct {
		DownstreamDatasetIDs []string `json:"downstream_dataset_ids"`
		DownstreamNodeCount  uint64   `json:"downstream_node_count"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return fallback, nil
	}
	ids := make([]uuid.UUID, 0, len(payload.DownstreamDatasetIDs))
	for _, raw := range payload.DownstreamDatasetIDs {
		if id, err := uuid.Parse(raw); err == nil {
			ids = append(ids, id)
		}
	}
	count := int(payload.DownstreamNodeCount)
	if count == 0 {
		count = len(ids)
	}
	notes := []string{"lineage impact evaluated successfully"}
	if legalHold {
		notes = []string{"legal hold prevents unsafe downstream deletion"}
	}
	return models.LineageImpactSummary{
		DownstreamNodeCount:  count,
		DownstreamDatasetIDs: ids,
		BlockedByLegalHold:   legalHold,
		ImpactNotes:          notes,
	}, nil
}

// ExecuteSafeDeletion ports `domain::deletion::execute_safe_deletion`.
// Deletes at most one primary marker + one per-downstream cascade
// marker. Any per-call storage error is swallowed (best-effort, same
// as the Rust impl).
func ExecuteSafeDeletion(ctx context.Context, storage StorageBackend, datasetID uuid.UUID, hardDelete bool, impact *models.LineageImpactSummary) []string {
	if impact.BlockedByLegalHold {
		return nil
	}
	primary := fmt.Sprintf("datasets/%s", datasetID)
	marker := primary
	if !hardDelete {
		marker = fmt.Sprintf("%s/subject-mask-marker.json", primary)
	}
	if storage != nil {
		_ = storage.Delete(ctx, marker)
	}
	out := []string{marker}
	for _, id := range impact.DownstreamDatasetIDs {
		path := fmt.Sprintf("datasets/%s/lineage-cascade-marker.json", id)
		if storage != nil {
			_ = storage.Delete(ctx, path)
		}
		out = append(out, path)
	}
	return out
}

// BuildAuditTrace ports `domain::deletion::build_audit_trace`. Returns
// a JSON-encoded array of two `DeletionAuditRecord` entries.
func BuildAuditTrace(request *models.LineageDeletionRequestInput, impact *models.LineageImpactSummary, deletedPaths []string) (json.RawMessage, error) {
	impactMeta, err := json.Marshal(map[string]any{
		"dataset_id":             request.DatasetID,
		"downstream_node_count":  impact.DownstreamNodeCount,
		"legal_hold":             request.LegalHold,
	})
	if err != nil {
		return nil, err
	}
	deletionMeta, err := json.Marshal(map[string]any{
		"dataset_id":     request.DatasetID,
		"deleted_paths":  deletedPaths,
		"hard_delete":    request.HardDelete,
	})
	if err != nil {
		return nil, err
	}
	records := []models.DeletionAuditRecord{
		{
			Service:   "lineage-deletion-service",
			Action:    "lineage-impact-computed",
			SubjectID: request.SubjectID,
			Metadata:  impactMeta,
		},
		{
			Service:   "lineage-deletion-service",
			Action:    "safe-deletion-executed",
			SubjectID: request.SubjectID,
			Metadata:  deletionMeta,
		},
	}
	return json.Marshal(records)
}

// PersistDeletion ports `domain::deletion::persist_deletion`.
func PersistDeletion(ctx context.Context, db *pgxpool.Pool, request *models.LineageDeletionRequestInput, impact *models.LineageImpactSummary, deletedPaths []string) (*models.LineageDeletionResponse, error) {
	impactJSON, err := json.Marshal(impact)
	if err != nil {
		return nil, err
	}
	pathsJSON, err := json.Marshal(deletedPaths)
	if err != nil {
		return nil, err
	}
	auditTrace, err := BuildAuditTrace(request, impact, deletedPaths)
	if err != nil {
		return nil, err
	}
	status := "completed"
	if request.LegalHold {
		status = "blocked_legal_hold"
	}
	now := time.Now().UTC()
	id := uuid.New()
	row := db.QueryRow(ctx,
		`INSERT INTO lineage_deletion_requests
		      (id, dataset_id, subject_id, hard_delete, legal_hold, impact, status,
		       deleted_paths, audit_trace, requested_at, completed_at)
		    VALUES ($1, $2, $3, $4, $5, $6::jsonb, $7, $8::jsonb, $9::jsonb, $10, $11)
		    RETURNING id, dataset_id, subject_id, hard_delete, legal_hold, impact,
		              status, deleted_paths, audit_trace, requested_at, completed_at`,
		id, request.DatasetID, request.SubjectID, request.HardDelete, request.LegalHold,
		impactJSON, status, pathsJSON, auditTrace, now, now,
	)
	var (
		retImpact      json.RawMessage
		retPaths       json.RawMessage
		retAuditTrace  json.RawMessage
		retStatus      string
		hardDelete     bool
		legalHold      bool
		retRequestedAt time.Time
		retCompletedAt *time.Time
		retID          uuid.UUID
		retDataset     uuid.UUID
		retSubject     *string
	)
	if err := row.Scan(&retID, &retDataset, &retSubject, &hardDelete, &legalHold,
		&retImpact, &retStatus, &retPaths, &retAuditTrace, &retRequestedAt,
		&retCompletedAt); err != nil {
		return nil, err
	}
	var impactOut models.LineageImpactSummary
	if err := json.Unmarshal(retImpact, &impactOut); err != nil {
		return nil, err
	}
	var pathsOut []string
	if err := json.Unmarshal(retPaths, &pathsOut); err != nil {
		return nil, err
	}
	return &models.LineageDeletionResponse{
		RequestID:    retID,
		DatasetID:    retDataset,
		SubjectID:    retSubject,
		Impact:       impactOut,
		Status:       retStatus,
		DeletedPaths: pathsOut,
		AuditTrace:   retAuditTrace,
		RequestedAt:  retRequestedAt,
		CompletedAt:  retCompletedAt,
	}, nil
}

// EmitRetentionDelete ports `lineage_deletion::domain::audit_emitter::emit_retention_delete`.
//
// Emits one structured slog record under the `audit` channel. The
// audit-compliance collector translates these into rows in the
// audit warehouse via the standard tracing/slog bridge.
func EmitRetentionDelete(record *RetentionAuditRecord) {
	slog.Info("retention policy applied",
		slog.String("target", "audit"),
		slog.String("action", "retention.delete"),
		slog.String("actor", "system"),
		slog.String("policy_id", record.PolicyID.String()),
		slog.String("dataset_rid", record.DatasetRid),
		slog.String("transaction_id", record.TransactionID.String()),
		slog.Int("files_count", record.FilesCount),
		slog.Uint64("bytes_freed", record.BytesFreed),
		slog.Bool("physically_deleted", record.PhysicallyDeleted),
	)
}

// RetentionAuditRecord mirrors `audit_emitter::RetentionAuditRecord`.
type RetentionAuditRecord struct {
	PolicyID          uuid.UUID
	DatasetRid        string
	TransactionID     uuid.UUID
	FilesCount        int
	BytesFreed        uint64
	PhysicallyDeleted bool
}

// httpClientShim wraps `*http.Client` to satisfy HTTPClient without
// callers having to write `client.Do` plumbing.
type httpClientShim struct {
	C *http.Client
}

// NewHTTPClient wraps a stdlib *http.Client.
func NewHTTPClient(c *http.Client) HTTPClient {
	if c == nil {
		c = &http.Client{Timeout: 15 * time.Second}
	}
	return &httpClientShim{C: c}
}

// Do satisfies HTTPClient.
func (h *httpClientShim) Do(req *http.Request) (*http.Response, error) {
	return h.C.Do(req)
}

// readResponseBody is a helper kept here so the per-test fixture can
// simulate the Rust `bytes.into_buf`-style read without exposing
// io.ReadAll across package boundaries.
func readResponseBody(resp *http.Response) (string, error) {
	defer resp.Body.Close()
	buf := bytes.NewBuffer(nil)
	if _, err := io.Copy(buf, resp.Body); err != nil {
		return "", err
	}
	return buf.String(), nil
}
