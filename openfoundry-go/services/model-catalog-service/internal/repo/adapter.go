package repo

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/services/model-catalog-service/internal/models"
)

type AdapterRepo struct {
	Pool *pgxpool.Pool
}

const adapterCols = `id, slug, name, adapter_kind, artifact_uri, sidecar_image,
                     framework, model_id, status, created_at, updated_at`

func scanAdapter(s scanner) (models.ModelAdapter, error) {
	var a models.ModelAdapter
	err := s.Scan(&a.ID, &a.Slug, &a.Name, &a.AdapterKind, &a.ArtifactURI,
		&a.SidecarImage, &a.Framework, &a.ModelID, &a.Status, &a.CreatedAt, &a.UpdatedAt)
	return a, err
}

type scanner interface{ Scan(...any) error }

func (r *AdapterRepo) ListAdapters(ctx context.Context) ([]models.ModelAdapter, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT `+adapterCols+` FROM model_adapters ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.ModelAdapter, 0)
	for rows.Next() {
		a, err := scanAdapter(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

func (r *AdapterRepo) RegisterAdapter(ctx context.Context, body models.RegisterAdapterRequest) (models.ModelAdapter, error) {
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO model_adapters
                (id, slug, name, adapter_kind, artifact_uri, sidecar_image, framework, model_id, status)
            VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'registered')
            RETURNING `+adapterCols,
		uuid.New(), body.Slug, body.Name, body.AdapterKind, body.ArtifactURI,
		body.SidecarImage, body.Framework, body.ModelID)
	return scanAdapter(row)
}

func (r *AdapterRepo) GetAdapter(ctx context.Context, id uuid.UUID) (*models.ModelAdapter, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT `+adapterCols+` FROM model_adapters WHERE id = $1`, id)
	a, err := scanAdapter(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

const contractCols = `id, adapter_id, version, input_schema, output_schema, created_at`

func scanContract(s scanner) (models.InferenceContract, error) {
	var c models.InferenceContract
	err := s.Scan(&c.ID, &c.AdapterID, &c.Version, &c.InputSchema, &c.OutputSchema, &c.CreatedAt)
	return c, err
}

func (r *AdapterRepo) ListContracts(ctx context.Context, adapterID uuid.UUID) ([]models.InferenceContract, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT `+contractCols+` FROM inference_contracts WHERE adapter_id = $1 ORDER BY created_at DESC`, adapterID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.InferenceContract, 0)
	for rows.Next() {
		c, err := scanContract(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

func (r *AdapterRepo) PublishContract(ctx context.Context, adapterID uuid.UUID, body models.PublishContractRequest) (models.InferenceContract, error) {
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO inference_contracts (id, adapter_id, version, input_schema, output_schema)
           VALUES ($1, $2, $3, $4, $5) RETURNING `+contractCols,
		uuid.New(), adapterID, body.Version, body.InputSchema, body.OutputSchema)
	return scanContract(row)
}
