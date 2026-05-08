package webauthn

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// PostgresStore implements Store against the webauthn_credentials +
// webauthn_challenges tables (slice 4 schema).
//
// The tables mirror the Cassandra DDL field-for-field; the slice-2b
// follow-up swaps this for a Cassandra implementation without
// changing the Service / handlers surface.
type PostgresStore struct{ Pool *pgxpool.Pool }

// NewPostgresStore returns a PostgresStore.
func NewPostgresStore(pool *pgxpool.Pool) *PostgresStore { return &PostgresStore{Pool: pool} }

// HasCredentials returns true when the user has at least one credential.
func (s *PostgresStore) HasCredentials(ctx context.Context, userID uuid.UUID) (bool, error) {
	var n int64
	err := s.Pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM webauthn_credentials WHERE user_id = $1`, userID,
	).Scan(&n)
	return n > 0, err
}

// ListCredentials returns every credential for `userID`.
func (s *PostgresStore) ListCredentials(ctx context.Context, userID uuid.UUID) ([]Credential, error) {
	rows, err := s.Pool.Query(ctx,
		`SELECT id, user_id, credential_id, public_key, sign_count, transports,
		        attestation_type, aaguid, label, created_at, last_used_at
		 FROM webauthn_credentials WHERE user_id = $1
		 ORDER BY created_at`,
		userID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Credential, 0)
	for rows.Next() {
		c, err := scanCred(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *c)
	}
	return out, rows.Err()
}

// GetCredentialByCredentialID looks up a credential by its WebAuthn id.
func (s *PostgresStore) GetCredentialByCredentialID(ctx context.Context, credentialID []byte) (*Credential, error) {
	row := s.Pool.QueryRow(ctx,
		`SELECT id, user_id, credential_id, public_key, sign_count, transports,
		        attestation_type, aaguid, label, created_at, last_used_at
		 FROM webauthn_credentials WHERE credential_id = $1`,
		credentialID,
	)
	c, err := scanCred(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return c, err
}

// InsertCredential persists a freshly registered credential.
func (s *PostgresStore) InsertCredential(ctx context.Context, c Credential) error {
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO webauthn_credentials
		   (id, user_id, credential_id, public_key, sign_count, transports,
		    attestation_type, aaguid, label, created_at)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		c.ID, c.UserID, c.CredentialID, c.PublicKey, c.SignCount,
		c.Transports, c.AttestationType, c.AAGUID, c.Label, c.CreatedAt,
	)
	return err
}

// UpdateSignCount bumps sign_count + last_used_at after a successful assertion.
func (s *PostgresStore) UpdateSignCount(ctx context.Context, credentialID []byte, signCount uint32, lastUsedAt time.Time) error {
	_, err := s.Pool.Exec(ctx,
		`UPDATE webauthn_credentials SET sign_count = $2, last_used_at = $3 WHERE credential_id = $1`,
		credentialID, signCount, lastUsedAt,
	)
	return err
}

// StoreChallenge persists a ChallengeRecord.
func (s *PostgresStore) StoreChallenge(ctx context.Context, ch ChallengeRecord) error {
	_, err := s.Pool.Exec(ctx,
		`INSERT INTO webauthn_challenges (challenge_id, user_id, kind, session_data, expires_at)
		 VALUES ($1, $2, $3, $4, $5)`,
		ch.ID, ch.UserID, ch.Kind, ch.SessionData, ch.ExpiresAt,
	)
	return err
}

// LoadChallenge returns the ChallengeRecord or nil when absent.
func (s *PostgresStore) LoadChallenge(ctx context.Context, challengeID uuid.UUID) (*ChallengeRecord, error) {
	row := s.Pool.QueryRow(ctx,
		`SELECT challenge_id, user_id, kind, session_data, expires_at
		 FROM webauthn_challenges WHERE challenge_id = $1`,
		challengeID,
	)
	r := &ChallengeRecord{}
	if err := row.Scan(&r.ID, &r.UserID, &r.Kind, &r.SessionData, &r.ExpiresAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return r, nil
}

// DeleteChallenge removes a challenge after successful use.
func (s *PostgresStore) DeleteChallenge(ctx context.Context, challengeID uuid.UUID) error {
	_, err := s.Pool.Exec(ctx,
		`DELETE FROM webauthn_challenges WHERE challenge_id = $1`, challengeID,
	)
	return err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanCred(r rowScanner) (*Credential, error) {
	c := &Credential{}
	if err := r.Scan(
		&c.ID, &c.UserID, &c.CredentialID, &c.PublicKey, &c.SignCount,
		&c.Transports, &c.AttestationType, &c.AAGUID, &c.Label,
		&c.CreatedAt, &c.LastUsedAt,
	); err != nil {
		return nil, fmt.Errorf("scan credential: %w", err)
	}
	return c, nil
}
