package lineage

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/services/lineage-service/internal/models"
)

// UpsertNode ports `upsert_node`. Returns the row that PostgreSQL
// committed (label/marking/metadata may have been merged in by the
// ON CONFLICT branch).
func UpsertNode(ctx context.Context, db *pgxpool.Pool, entityID uuid.UUID, kind models.NodeKind, label, marking string, metadata json.RawMessage) (models.LineageNodeRecord, error) {
	if len(metadata) == 0 {
		metadata = json.RawMessage(`{}`)
	}
	row := db.QueryRow(ctx,
		`INSERT INTO lineage_nodes (entity_id, entity_kind, label, marking, metadata)
		      VALUES ($1, $2, $3, $4, $5)
		    ON CONFLICT (entity_id, entity_kind)
		    DO UPDATE SET
		           label = EXCLUDED.label,
		           marking = EXCLUDED.marking,
		           metadata = EXCLUDED.metadata,
		           updated_at = NOW()
		    RETURNING entity_id, entity_kind, label, marking, metadata`,
		entityID, kind.String(), label, marking, metadata,
	)
	return scanNodeRecord(row)
}

// GetNodeRecord ports `get_node_record`. Nil + nil error when missing.
func GetNodeRecord(ctx context.Context, db *pgxpool.Pool, entityID uuid.UUID, kind models.NodeKind) (*models.LineageNodeRecord, error) {
	row := db.QueryRow(ctx,
		`SELECT entity_id, entity_kind, label, marking, metadata
		   FROM lineage_nodes
		  WHERE entity_id = $1 AND entity_kind = $2`,
		entityID, kind.String(),
	)
	rec, err := scanNodeRecord(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// LoadNodeOverlays ports `load_lineage_node_overlays`.
func LoadNodeOverlays(ctx context.Context, db *pgxpool.Pool, keys map[models.NodeKey]struct{}) (map[models.NodeKey]models.LineageNodeRecord, error) {
	overlays := map[models.NodeKey]models.LineageNodeRecord{}
	if len(keys) == 0 {
		return overlays, nil
	}
	ids := make([]uuid.UUID, 0, len(keys))
	for k := range keys {
		ids = append(ids, k.ID)
	}
	rows, err := db.Query(ctx,
		`SELECT entity_id, entity_kind, label, marking, metadata
		   FROM lineage_nodes
		  WHERE entity_id = ANY($1)`,
		ids,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		rec, err := scanNodeRecord(rows)
		if err != nil {
			return nil, err
		}
		kind, ok := models.ParseNodeKind(rec.EntityKind)
		if !ok {
			continue
		}
		key := models.NodeKey{ID: rec.EntityID, Kind: kind}
		if _, want := keys[key]; want {
			overlays[key] = rec
		}
	}
	return overlays, rows.Err()
}

// EnsurePlaceholderNode inserts a lightweight overlay for lineage resources
// whose canonical service is not available to the lineage service. No-op when
// the row already exists.
func EnsurePlaceholderNode(ctx context.Context, db *pgxpool.Pool, entityID uuid.UUID, kind models.NodeKind) error {
	existing, err := GetNodeRecord(ctx, db, entityID, kind)
	if err != nil {
		return err
	}
	if existing != nil {
		return nil
	}
	_, err = UpsertNode(ctx, db, entityID, kind,
		SyntheticLabel(kind, entityID),
		"public",
		json.RawMessage(`{"placeholder":true,"source":"lineage_sync"}`),
	)
	return err
}

// EnsurePlaceholderWorkflow ports `ensure_placeholder_workflow`. No-op
// when the workflow row already exists.
func EnsurePlaceholderWorkflow(ctx context.Context, db *pgxpool.Pool, workflowID uuid.UUID) error {
	return EnsurePlaceholderNode(ctx, db, workflowID, models.KindWorkflow)
}

// LoadPipelineByID ports `load_pipeline_by_id`. The Rust impl runs
// `SELECT * FROM pipelines WHERE id = $1`; we translate to the same
// thing, mapping the wide row into the canonical Pipeline model.
func LoadPipelineByID(ctx context.Context, db *pgxpool.Pool, pipelineID uuid.UUID) (*models.Pipeline, error) {
	row := db.QueryRow(ctx,
		`SELECT id, name, description, owner_id, dag, status, schedule_config, retry_policy,
		        next_run_at, created_at, updated_at
		   FROM pipelines WHERE id = $1`,
		pipelineID,
	)
	var p models.Pipeline
	var dag, scheduleConfig, retryPolicy []byte
	err := row.Scan(
		&p.ID, &p.Name, &p.Description, &p.OwnerID, &dag, &p.Status,
		&scheduleConfig, &retryPolicy, &p.NextRunAt, &p.CreatedAt, &p.UpdatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.DAG = dag
	p.ScheduleConfig = scheduleConfig
	p.RetryPolicy = retryPolicy
	return &p, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanNodeRecord(s rowScanner) (models.LineageNodeRecord, error) {
	var rec models.LineageNodeRecord
	var meta []byte
	if err := s.Scan(&rec.EntityID, &rec.EntityKind, &rec.Label, &rec.Marking, &meta); err != nil {
		return rec, err
	}
	rec.Metadata = meta
	return rec, nil
}
