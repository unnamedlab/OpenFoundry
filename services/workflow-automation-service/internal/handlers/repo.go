package handlers

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/services/workflow-automation-service/internal/models"
)

const workflowColumns = `id, name, description, owner_id, status, trigger_type,
        trigger_config, steps, webhook_secret, next_run_at, last_triggered_at,
        created_at, updated_at`

// LoadWorkflow ports `crud::load_workflow`. Nil + nil-error when the
// row does not exist.
func LoadWorkflow(ctx context.Context, db *pgxpool.Pool, workflowID uuid.UUID) (*models.WorkflowDefinition, error) {
	row := db.QueryRow(ctx, `SELECT `+workflowColumns+` FROM workflows WHERE id = $1`, workflowID)
	w, err := scanWorkflow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &w, nil
}

func scanWorkflow(s rowScanner) (models.WorkflowDefinition, error) {
	var w models.WorkflowDefinition
	var triggerConfig, steps []byte
	if err := s.Scan(
		&w.ID, &w.Name, &w.Description, &w.OwnerID, &w.Status, &w.TriggerType,
		&triggerConfig, &steps, &w.WebhookSecret, &w.NextRunAt, &w.LastTriggeredAt,
		&w.CreatedAt, &w.UpdatedAt,
	); err != nil {
		return w, err
	}
	w.TriggerConfig = triggerConfig
	w.Steps = steps
	return w, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}
