// Lineage-deletion HTTP handlers — mirrors `handlers/deletion.rs`.

package lineagedeletion

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/services/audit-compliance-service/internal/models"
)

// Handlers wires the lineage-deletion HTTP endpoints.
type Handlers struct {
	Pool              *pgxpool.Pool
	HTTPClient        HTTPClient
	Storage           StorageBackend
	LineageServiceURL string
}

// New builds a Handlers with the production deps wired through.
func New(pool *pgxpool.Pool, client HTTPClient, storage StorageBackend, lineageServiceURL string) *Handlers {
	return &Handlers{
		Pool:              pool,
		HTTPClient:        client,
		Storage:           storage,
		LineageServiceURL: lineageServiceURL,
	}
}

// RequestDeletion ports `handlers::deletion::request_deletion`.
//
// `reason` is required; blank reasons emit 400. Each downstream
// failure short-circuits the pipeline with a 500.
func (h *Handlers) RequestDeletion(w http.ResponseWriter, r *http.Request) {
	var body models.LineageDeletionRequestInput
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}
	reason := ""
	if body.Reason != nil {
		reason = strings.TrimSpace(*body.Reason)
	}
	if reason == "" {
		writeJSONErr(w, http.StatusBadRequest, "reason is required")
		return
	}

	impact, err := ComputeImpact(r.Context(), h.HTTPClient, h.LineageServiceURL, body.DatasetID, body.LegalHold)
	if err != nil {
		slog.Error("compute lineage impact", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	deletedPaths := ExecuteSafeDeletion(r.Context(), h.Storage, body.DatasetID, body.HardDelete, &impact)
	resp, err := PersistDeletion(r.Context(), h.Pool, &body, &impact, deletedPaths)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// Defence in depth: keeps `errors` import alive for future helpers.
var _ = errors.New
