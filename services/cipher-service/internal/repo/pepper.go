package repo

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/openfoundry/openfoundry-go/services/cipher-service/internal/domain"
)

type CreatePepperParams struct {
	ID                    uuid.UUID
	TenantID              uuid.UUID
	Name                  string
	Algorithm             domain.Algorithm
	WrappedPepperMaterial []byte
	KMSKeyRef             string
	AccessPolicy          domain.AccessPolicy
}

func (r *Repo) InsertPepper(ctx context.Context, p CreatePepperParams) (*domain.Pepper, error) {
	if p.Algorithm != domain.AlgorithmSHA256 && p.Algorithm != domain.AlgorithmSHA512 {
		return nil, fmt.Errorf("repo: invalid pepper algorithm %q", p.Algorithm)
	}
	policy := p.AccessPolicy
	if isZeroAccessPolicy(policy) {
		policy = domain.DefaultAccessPolicy()
	}
	policyJSON, err := marshalAccessPolicy(policy)
	if err != nil {
		return nil, err
	}
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	now := time.Now().UTC()
	if _, err := tx.Exec(ctx,
		`INSERT INTO cipher_peppers (id, tenant_id, name, algorithm, version, access_policy, created_at)
		 VALUES ($1, $2, $3, $4, 1, $5::jsonb, $6)`,
		p.ID, p.TenantID, p.Name, string(p.Algorithm), policyJSON, now,
	); err != nil {
		if isUniqueViolation(err) {
			return nil, ErrAliasConflict
		}
		return nil, fmt.Errorf("insert pepper: %w", err)
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO cipher_pepper_versions (pepper_id, version, wrapped_pepper_material, kms_key_ref, created_at)
		 VALUES ($1, 1, $2, $3, $4)`, p.ID, p.WrappedPepperMaterial, p.KMSKeyRef, now,
	); err != nil {
		return nil, fmt.Errorf("insert pepper v1: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return &domain.Pepper{ID: p.ID, TenantID: p.TenantID, Name: p.Name, Algorithm: p.Algorithm, PepperMaterialRef: p.KMSKeyRef, Version: 1, AccessPolicy: policy.Clone(), CreatedAt: now}, nil
}

func (r *Repo) GetPepper(ctx context.Context, tenantID, id uuid.UUID) (*domain.Pepper, error) {
	row := r.Pool.QueryRow(ctx, `SELECT p.id,p.tenant_id,p.name,p.algorithm,COALESCE(v.kms_key_ref,''),p.version,p.access_policy,p.created_at,p.rotated_at
	 FROM cipher_peppers p LEFT JOIN cipher_pepper_versions v ON v.pepper_id=p.id AND v.version=p.version
	 WHERE p.id=$1 AND p.tenant_id=$2`, id, tenantID)
	return scanPepper(row)
}

func (r *Repo) GetPepperVersion(ctx context.Context, tenantID, pepperID uuid.UUID, version uint32) (*domain.PepperVersion, error) {
	row := r.Pool.QueryRow(ctx, `SELECT v.pepper_id,v.version,v.wrapped_pepper_material,v.kms_key_ref,v.created_at
	 FROM cipher_pepper_versions v JOIN cipher_peppers p ON p.id=v.pepper_id
	 WHERE v.pepper_id=$1 AND v.version=$2 AND p.tenant_id=$3`, pepperID, version, tenantID)
	v := &domain.PepperVersion{}
	if err := row.Scan(&v.PepperID, &v.Version, &v.WrappedPepperMaterial, &v.KMSKeyRef, &v.CreatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrKeyNotFound
		}
		return nil, fmt.Errorf("get pepper version: %w", err)
	}
	return v, nil
}

func (r *Repo) RotatePepper(ctx context.Context, tenantID, id uuid.UUID, wrapped []byte, kmsRef string) (*domain.Pepper, error) {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var current uint32
	if err := tx.QueryRow(ctx, `SELECT version FROM cipher_peppers WHERE id=$1 AND tenant_id=$2 FOR UPDATE`, id, tenantID).Scan(&current); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrKeyNotFound
		}
		return nil, err
	}
	now := time.Now().UTC()
	next := current + 1
	if _, err := tx.Exec(ctx, `INSERT INTO cipher_pepper_versions (pepper_id,version,wrapped_pepper_material,kms_key_ref,created_at) VALUES ($1,$2,$3,$4,$5)`, id, next, wrapped, kmsRef, now); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE cipher_peppers SET version=$2, rotated_at=$3 WHERE id=$1`, id, next, now); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return r.GetPepper(ctx, tenantID, id)
}

type pepperScanner interface{ Scan(dest ...any) error }

func scanPepper(row pepperScanner) (*domain.Pepper, error) {
	p := &domain.Pepper{}
	var alg string
	var policyRaw []byte
	if err := row.Scan(&p.ID, &p.TenantID, &p.Name, &alg, &p.PepperMaterialRef, &p.Version, &policyRaw, &p.CreatedAt, &p.RotatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrKeyNotFound
		}
		return nil, err
	}
	p.Algorithm = domain.Algorithm(alg)
	policy, err := unmarshalAccessPolicy(policyRaw)
	if err != nil {
		return nil, err
	}
	p.AccessPolicy = policy
	return p, nil
}
