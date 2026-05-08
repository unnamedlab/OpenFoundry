package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/libs/ml-kernel-go/domain/predictions"
	"github.com/openfoundry/openfoundry-go/libs/ml-kernel-go/models"
)

// PredictionsHandlers exposes realtime + batch prediction endpoints.
// Mirrors libs/ml-kernel/src/handlers/predictions.rs:
//   - POST /api/v1/deployments/{id}/predictions    (realtime)
//   - GET  /api/v1/predictions/batch                (list)
//   - POST /api/v1/predictions/batch                (create)
type PredictionsHandlers struct {
	Pool *pgxpool.Pool
}

const batchPredictionColumns = `id, deployment_id, status, record_count,
                                output_destination, outputs,
                                created_at, completed_at`

type predictionsScanner interface {
	Scan(...any) error
}

func scanBatchJob(s predictionsScanner) (models.BatchPredictionJob, error) {
	var b models.BatchPredictionJob
	var outputDestination *string
	var outputsRaw []byte
	var completedAt *time.Time
	if err := s.Scan(
		&b.ID, &b.DeploymentID, &b.Status, &b.RecordCount,
		&outputDestination, &outputsRaw, &b.CreatedAt, &completedAt,
	); err != nil {
		return b, err
	}
	b.OutputDestination = outputDestination
	b.CompletedAt = completedAt
	if len(outputsRaw) > 0 {
		_ = json.Unmarshal(outputsRaw, &b.Outputs)
	}
	if b.Outputs == nil {
		b.Outputs = []models.PredictionOutput{}
	}
	return b, nil
}

func (h *PredictionsHandlers) loadDeploymentSplits(ctx context.Context, deploymentID uuid.UUID) ([]models.TrafficSplitEntry, error) {
	row := h.Pool.QueryRow(ctx,
		`SELECT traffic_split FROM ml_deployments WHERE id = $1`, deploymentID)
	var raw []byte
	if err := row.Scan(&raw); err != nil {
		return nil, err
	}
	var splits []models.TrafficSplitEntry
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &splits)
	}
	return splits, nil
}

func (h *PredictionsHandlers) loadVersions(ctx context.Context, splits []models.TrafficSplitEntry) (map[uuid.UUID]predictions.ModelRuntime, error) {
	versions := make(map[uuid.UUID]predictions.ModelRuntime, len(splits))
	for _, s := range splits {
		if _, ok := versions[s.ModelVersionID]; ok {
			continue
		}
		row := h.Pool.QueryRow(ctx,
			`SELECT id, version_number, schema FROM ml_model_versions WHERE id = $1`,
			s.ModelVersionID)
		var id uuid.UUID
		var versionNumber int32
		var schemaRaw []byte
		if err := row.Scan(&id, &versionNumber, &schemaRaw); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				continue
			}
			return nil, err
		}
		var schema map[string]any
		if len(schemaRaw) > 0 {
			_ = json.Unmarshal(schemaRaw, &schema)
		}
		versions[id] = predictions.ModelRuntime{
			VersionNumber: versionNumber,
			Schema:        schema,
		}
	}
	return versions, nil
}

// RealtimePredict handles `POST /api/v1/deployments/{id}/predictions`.
func (h *PredictionsHandlers) RealtimePredict(w http.ResponseWriter, r *http.Request, deploymentID uuid.UUID) {
	var body models.RealtimePredictionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(body.Inputs) == 0 {
		writeError(w, http.StatusBadRequest, "prediction inputs are required")
		return
	}

	splits, err := h.loadDeploymentSplits(r.Context(), deploymentID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "deployment not found")
		return
	}
	if err != nil {
		dbError(w, err)
		return
	}
	if len(splits) == 0 {
		writeError(w, http.StatusBadRequest, "deployment has no traffic split configured")
		return
	}

	versions, err := h.loadVersions(r.Context(), splits)
	if err != nil {
		dbError(w, err)
		return
	}
	if len(versions) == 0 {
		writeError(w, http.StatusNotFound, "deployment versions not found")
		return
	}

	outputs := runPredictions(body.Inputs, splits, versions, body.Explain)
	predictedAt := time.Now().UTC()

	if err := h.persistRealtimeInference(r.Context(), deploymentID, outputs, predictedAt); err != nil {
		// Match Rust: log + continue. The realtime path returns the
		// computed outputs even when the history-write fails.
		_ = err
	}

	writeJSON(w, http.StatusOK, models.RealtimePredictionResponse{
		DeploymentID: deploymentID,
		Outputs:      outputs,
		PredictedAt:  predictedAt,
	})
}

// ListBatchPredictions handles `GET /api/v1/predictions/batch`.
func (h *PredictionsHandlers) ListBatchPredictions(w http.ResponseWriter, r *http.Request) {
	rows, err := h.Pool.Query(r.Context(),
		`SELECT `+batchPredictionColumns+` FROM ml_batch_predictions
          ORDER BY created_at DESC`)
	if err != nil {
		dbError(w, err)
		return
	}
	defer rows.Close()
	out := make([]models.BatchPredictionJob, 0)
	for rows.Next() {
		j, err := scanBatchJob(rows)
		if err != nil {
			dbError(w, err)
			return
		}
		out = append(out, j)
	}
	writeJSON(w, http.StatusOK, models.ListBatchPredictionsResponse{Data: out})
}

// CreateBatchPrediction handles `POST /api/v1/predictions/batch`.
func (h *PredictionsHandlers) CreateBatchPrediction(w http.ResponseWriter, r *http.Request) {
	var body models.CreateBatchPredictionRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(body.Records) == 0 {
		writeError(w, http.StatusBadRequest, "batch prediction records are required")
		return
	}

	splits, err := h.loadDeploymentSplits(r.Context(), body.DeploymentID)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "deployment not found")
		return
	}
	if err != nil {
		dbError(w, err)
		return
	}
	if len(splits) == 0 {
		writeError(w, http.StatusBadRequest, "deployment has no traffic split configured")
		return
	}

	versions, err := h.loadVersions(r.Context(), splits)
	if err != nil {
		dbError(w, err)
		return
	}
	if len(versions) == 0 {
		writeError(w, http.StatusNotFound, "deployment versions not found")
		return
	}

	outputs := runPredictions(body.Records, splits, versions, true)

	now := time.Now().UTC()
	outputsJSON, _ := json.Marshal(outputs)
	row := h.Pool.QueryRow(r.Context(),
		`INSERT INTO ml_batch_predictions
              (id, deployment_id, status, record_count,
               output_destination, outputs, created_at, completed_at)
            VALUES ($1, $2, 'completed', $3, $4, $5, $6, $7)
            RETURNING `+batchPredictionColumns,
		uuid.New(), body.DeploymentID, int64(len(outputs)),
		body.OutputDestination, outputsJSON, now, now)
	job, err := scanBatchJob(row)
	if err != nil {
		dbError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, job)
}

func (h *PredictionsHandlers) persistRealtimeInference(ctx context.Context, deploymentID uuid.UUID, outputs []models.PredictionOutput, predictedAt time.Time) error {
	outputsJSON, _ := json.Marshal(outputs)
	_, err := h.Pool.Exec(ctx,
		`INSERT INTO ml_batch_predictions
              (id, deployment_id, status, record_count,
               output_destination, outputs, created_at, completed_at)
            VALUES ($1, $2, 'realtime', $3, NULL, $4, $5, $6)`,
		uuid.New(), deploymentID, int64(len(outputs)),
		outputsJSON, predictedAt, predictedAt)
	return err
}

// runPredictions applies route_variant + predict_record to each input
// record. Mirrors the filter_map iteration in the Rust source —
// records whose split or runtime can't be resolved are silently
// dropped, matching wire-compat semantics.
func runPredictions(inputs []json.RawMessage, splits []models.TrafficSplitEntry, versions map[uuid.UUID]predictions.ModelRuntime, explain bool) []models.PredictionOutput {
	outputs := make([]models.PredictionOutput, 0, len(inputs))
	for index, raw := range inputs {
		split, ok := predictions.RouteVariant(splits, index)
		if !ok {
			continue
		}
		runtime, ok := versions[split.ModelVersionID]
		if !ok {
			continue
		}
		var input any
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &input)
		}
		outputs = append(outputs, predictions.PredictRecord(input, split, runtime, explain, index))
	}
	return outputs
}
