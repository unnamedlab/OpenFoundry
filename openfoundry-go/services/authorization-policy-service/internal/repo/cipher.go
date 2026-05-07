package repo

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/openfoundry/openfoundry-go/services/authorization-policy-service/internal/models"
)

// ─── Cipher permissions (read-only catalog) ─────────────────────────

func (r *Repo) ListCipherPermissions(ctx context.Context) ([]models.CipherPermission, error) {
	rows, err := r.Pool.Query(ctx,
		`SELECT id, resource, action, description, created_at
		   FROM cipher_permissions ORDER BY resource, action`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.CipherPermission, 0)
	for rows.Next() {
		var v models.CipherPermission
		if err := rows.Scan(&v.ID, &v.Resource, &v.Action, &v.Description, &v.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ─── Cipher channels ────────────────────────────────────────────────

const channelSelect = `SELECT id, name, release_channel, allowed_operations,
	license_tier, enabled, created_at, updated_at FROM cipher_channels`

func (r *Repo) ListCipherChannels(ctx context.Context) ([]models.CipherChannel, error) {
	rows, err := r.Pool.Query(ctx, channelSelect+` ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.CipherChannel, 0)
	for rows.Next() {
		v, err := scanCipherChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (r *Repo) GetCipherChannel(ctx context.Context, id uuid.UUID) (*models.CipherChannel, error) {
	row := r.Pool.QueryRow(ctx, channelSelect+` WHERE id = $1`, id)
	v, err := scanCipherChannel(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (r *Repo) CreateCipherChannel(ctx context.Context, body *models.CreateCipherChannelRequest) (*models.CipherChannel, error) {
	id := uuid.New()
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO cipher_channels
		    (id, name, release_channel, allowed_operations, license_tier, enabled)
		 VALUES ($1, $2, $3, $4, $5, $6)
		 RETURNING id, name, release_channel, allowed_operations, license_tier,
		           enabled, created_at, updated_at`,
		id, body.Name, body.ReleaseChannel,
		defaultJSON(body.AllowedOperations, "[]"),
		body.LicenseTier, enabled,
	)
	return scanCipherChannel(row)
}

func (r *Repo) UpdateCipherChannel(ctx context.Context, id uuid.UUID, body *models.UpdateCipherChannelRequest) (*models.CipherChannel, error) {
	current, err := r.GetCipherChannel(ctx, id)
	if err != nil || current == nil {
		return current, err
	}
	rc := current.ReleaseChannel
	if body.ReleaseChannel != nil {
		rc = *body.ReleaseChannel
	}
	ops := current.AllowedOperations
	if len(body.AllowedOperations) > 0 {
		ops = body.AllowedOperations
	}
	tier := current.LicenseTier
	if body.LicenseTier != nil {
		tier = *body.LicenseTier
	}
	enabled := current.Enabled
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	row := r.Pool.QueryRow(ctx,
		`UPDATE cipher_channels SET
		    release_channel = $2, allowed_operations = $3,
		    license_tier = $4, enabled = $5, updated_at = $6
		  WHERE id = $1
		  RETURNING id, name, release_channel, allowed_operations,
		            license_tier, enabled, created_at, updated_at`,
		id, rc, ops, tier, enabled, time.Now().UTC(),
	)
	return scanCipherChannel(row)
}

func (r *Repo) DeleteCipherChannel(ctx context.Context, id uuid.UUID) (bool, error) {
	cmd, err := r.Pool.Exec(ctx, `DELETE FROM cipher_channels WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return cmd.RowsAffected() > 0, nil
}

func scanCipherChannel(r rowLikeT) (*models.CipherChannel, error) {
	v := &models.CipherChannel{}
	if err := r.Scan(&v.ID, &v.Name, &v.ReleaseChannel, &v.AllowedOperations,
		&v.LicenseTier, &v.Enabled, &v.CreatedAt, &v.UpdatedAt); err != nil {
		return nil, err
	}
	return v, nil
}

// ─── Cipher licenses ────────────────────────────────────────────────

const licenseSelect = `SELECT id, name, tier, features, issued_by,
	created_at, updated_at FROM cipher_licenses`

func (r *Repo) ListCipherLicenses(ctx context.Context) ([]models.CipherLicense, error) {
	rows, err := r.Pool.Query(ctx, licenseSelect+` ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.CipherLicense, 0)
	for rows.Next() {
		v, err := scanCipherLicense(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *v)
	}
	return out, rows.Err()
}

func (r *Repo) GetCipherLicense(ctx context.Context, id uuid.UUID) (*models.CipherLicense, error) {
	row := r.Pool.QueryRow(ctx, licenseSelect+` WHERE id = $1`, id)
	v, err := scanCipherLicense(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

func (r *Repo) CreateCipherLicense(ctx context.Context, body *models.CreateCipherLicenseRequest, issuedBy *uuid.UUID) (*models.CipherLicense, error) {
	id := uuid.New()
	row := r.Pool.QueryRow(ctx,
		`INSERT INTO cipher_licenses (id, name, tier, features, issued_by)
		 VALUES ($1, $2, $3, $4, $5)
		 RETURNING id, name, tier, features, issued_by, created_at, updated_at`,
		id, body.Name, body.Tier, defaultJSON(body.Features, "[]"), issuedBy,
	)
	return scanCipherLicense(row)
}

func (r *Repo) UpdateCipherLicense(ctx context.Context, id uuid.UUID, body *models.UpdateCipherLicenseRequest) (*models.CipherLicense, error) {
	current, err := r.GetCipherLicense(ctx, id)
	if err != nil || current == nil {
		return current, err
	}
	tier := current.Tier
	if body.Tier != nil {
		tier = *body.Tier
	}
	feats := current.Features
	if len(body.Features) > 0 {
		feats = body.Features
	}
	row := r.Pool.QueryRow(ctx,
		`UPDATE cipher_licenses SET tier = $2, features = $3, updated_at = $4
		  WHERE id = $1
		  RETURNING id, name, tier, features, issued_by, created_at, updated_at`,
		id, tier, feats, time.Now().UTC(),
	)
	return scanCipherLicense(row)
}

func (r *Repo) DeleteCipherLicense(ctx context.Context, id uuid.UUID) (bool, error) {
	cmd, err := r.Pool.Exec(ctx, `DELETE FROM cipher_licenses WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return cmd.RowsAffected() > 0, nil
}

func scanCipherLicense(r rowLikeT) (*models.CipherLicense, error) {
	v := &models.CipherLicense{}
	if err := r.Scan(&v.ID, &v.Name, &v.Tier, &v.Features, &v.IssuedBy,
		&v.CreatedAt, &v.UpdatedAt); err != nil {
		return nil, err
	}
	return v, nil
}
