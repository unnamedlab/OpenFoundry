// Package repo holds SQL queries + embedded migrations for
// ingestion-replication-service.
package repo

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/services/ingestion-replication-service/internal/models"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

func Migrate(ctx context.Context, pool *pgxpool.Pool) error {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("read migrations dir: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".sql") {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		body, err := migrationsFS.ReadFile("migrations/" + name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		if _, err := pool.Exec(ctx, string(body)); err != nil {
			return fmt.Errorf("apply %s: %w", name, err)
		}
	}
	return nil
}

type Repo struct{ Pool *pgxpool.Pool }

const ingestJobSelect = `SELECT id, name, namespace, spec, status,
	kafka_connector_name, flink_deployment_name, error, created_at, updated_at
	FROM ingest_jobs`

func (r *Repo) ListIngestJobs(ctx context.Context, namespace, status string) ([]models.IngestJob, error) {
	clauses := []string{}
	args := []any{}
	if namespace != "" {
		clauses = append(clauses, fmt.Sprintf("namespace = $%d", len(args)+1))
		args = append(args, namespace)
	}
	if status != "" {
		clauses = append(clauses, fmt.Sprintf("status = $%d", len(args)+1))
		args = append(args, status)
	}
	sql := ingestJobSelect
	if len(clauses) > 0 {
		sql += " WHERE " + strings.Join(clauses, " AND ")
	}
	sql += " ORDER BY created_at DESC LIMIT 500"
	rows, err := r.Pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.IngestJob, 0)
	for rows.Next() {
		v, err := scanIngestJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (r *Repo) GetIngestJob(ctx context.Context, id uuid.UUID) (*models.IngestJob, error) {
	row := r.Pool.QueryRow(ctx, ingestJobSelect+` WHERE id = $1`, id)
	v, err := scanIngestJob(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (r *Repo) CreateIngestJob(ctx context.Context, body *models.CreateIngestJobRequest) (*models.IngestJob, error) {
	id := uuid.New()
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO ingest_jobs (id, name, namespace, spec)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, name, namespace, spec, status,
		           kafka_connector_name, flink_deployment_name, error,
		           created_at, updated_at`,
		id, strings.TrimSpace(body.Name), strings.TrimSpace(body.Namespace), body.Spec,
	)
	return scanIngestJob(row)
}

func (r *Repo) UpdateIngestJob(ctx context.Context, id uuid.UUID, body *models.UpdateIngestJobRequest) (*models.IngestJob, error) {
	current, err := r.GetIngestJob(ctx, id)
	if err != nil || current == nil {
		return current, err
	}
	status := current.Status
	if body.Status != nil {
		status = *body.Status
	}
	kafka := current.KafkaConnectorName
	if body.KafkaConnectorName != nil {
		kafka = body.KafkaConnectorName
	}
	flink := current.FlinkDeploymentName
	if body.FlinkDeploymentName != nil {
		flink = body.FlinkDeploymentName
	}
	errMsg := current.Error
	if body.Error != nil {
		errMsg = body.Error
	}
	row := r.Pool.QueryRow(ctx,
		`UPDATE ingest_jobs SET
		    status = $2, kafka_connector_name = $3,
		    flink_deployment_name = $4, error = $5, updated_at = $6
		  WHERE id = $1
		  RETURNING id, name, namespace, spec, status,
		            kafka_connector_name, flink_deployment_name, error,
		            created_at, updated_at`,
		id, status, kafka, flink, errMsg, time.Now().UTC(),
	)
	return scanIngestJob(row)
}

func (r *Repo) DeleteIngestJob(ctx context.Context, id uuid.UUID) (bool, error) {
	cmd, err := r.Pool.Exec(ctx, `DELETE FROM ingest_jobs WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return cmd.RowsAffected() > 0, nil
}

type rowLikeT interface{ Scan(...any) error }

func scanIngestJob(r rowLikeT) (*models.IngestJob, error) {
	v := &models.IngestJob{}
	if err := r.Scan(&v.ID, &v.Name, &v.Namespace, &v.Spec, &v.Status,
		&v.KafkaConnectorName, &v.FlinkDeploymentName, &v.Error,
		&v.CreatedAt, &v.UpdatedAt); err != nil {
		return nil, err
	}
	return v, nil
}
