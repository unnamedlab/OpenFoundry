package repo

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/openfoundry/openfoundry-go/services/identity-federation-service/internal/models"
)

// FindTOTPConfig returns (nil, nil) when no row exists.
func (r *Repo) FindTOTPConfig(ctx context.Context, userID uuid.UUID) (*models.TOTPConfig, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT user_id, secret, recovery_code_hashes, enabled, verified_at, created_at, updated_at
		 FROM user_mfa_totp WHERE user_id = $1`,
		userID,
	)
	c := &models.TOTPConfig{}
	var hashesRaw []byte
	if err := row.Scan(&c.UserID, &c.Secret, &hashesRaw, &c.Enabled, &c.VerifiedAt, &c.CreatedAt, &c.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if len(hashesRaw) > 0 {
		if err := json.Unmarshal(hashesRaw, &c.RecoveryCodeHashes); err != nil {
			return nil, err
		}
	}
	return c, nil
}

// UpsertTOTPSecret stores the secret + recovery hashes (enabled=false until verify).
func (r *Repo) UpsertTOTPSecret(ctx context.Context, userID uuid.UUID, secret string, recoveryHashes []string) error {
	hashesJSON, err := json.Marshal(recoveryHashes)
	if err != nil {
		return err
	}
	_, err = r.Pool.Exec(ctx,
		`INSERT INTO user_mfa_totp (user_id, secret, recovery_code_hashes, enabled)
		 VALUES ($1, $2, $3, false)
		 ON CONFLICT (user_id) DO UPDATE SET
		   secret = EXCLUDED.secret,
		   recovery_code_hashes = EXCLUDED.recovery_code_hashes,
		   enabled = false,
		   verified_at = NULL,
		   updated_at = NOW()`,
		userID, secret, hashesJSON,
	)
	return err
}

// EnableTOTP marks the configuration verified + enabled.
func (r *Repo) EnableTOTP(ctx context.Context, userID uuid.UUID, at time.Time) error {
	_, err := r.Pool.Exec(ctx,
		`UPDATE user_mfa_totp SET enabled = true, verified_at = $2, updated_at = NOW() WHERE user_id = $1`,
		userID, at,
	)
	return err
}

// DisableTOTP removes the row entirely (Rust crate's behaviour).
func (r *Repo) DisableTOTP(ctx context.Context, userID uuid.UUID) error {
	_, err := r.Pool.Exec(ctx, `DELETE FROM user_mfa_totp WHERE user_id = $1`, userID)
	return err
}

// UpdateRecoveryHashes replaces the hashes (used after consuming a code).
func (r *Repo) UpdateRecoveryHashes(ctx context.Context, userID uuid.UUID, hashes []string) error {
	hashesJSON, err := json.Marshal(hashes)
	if err != nil {
		return err
	}
	_, err = r.Pool.Exec(ctx,
		`UPDATE user_mfa_totp SET recovery_code_hashes = $2, updated_at = NOW() WHERE user_id = $1`,
		userID, hashesJSON,
	)
	return err
}
