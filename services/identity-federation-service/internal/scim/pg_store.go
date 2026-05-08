package scim

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres SQLSTATE codes used by this package. Avoiding the
// pgerrcode dependency keeps the go.mod surface minimal — the
// other identity-federation-service repos use the same inline
// constants.
const (
	pgErrUniqueViolation     = "23505"
	pgErrForeignKeyViolation = "23503"
)

// pgScanner is the shared scan-target abstraction so a single
// scanUserRow / scanGroupRow helper covers both pgx.Row (single
// row) and pgx.Rows (iterator).
type pgScanner interface {
	Scan(dest ...any) error
}

// pg_store.go ports the SQL adapters from
// services/identity-federation-service/src/handlers/scim.rs:
//   - load_user / load_user_by_scim_external_id /
//     update_scim_user / list_users count+select / delete_user
//     soft-delete (via UPDATE is_active = false).
//   - load_group / load_group_by_scim_external_id /
//     create_group INSERT / patch_group UPDATE / delete_group
//     hard-delete / load_group_members.
//   - insert_group_members_tx / replace_group_members_tx /
//     remove_group_members_tx.
//
// Both stores wrap a *pgxpool.Pool and translate the canonical
// Postgres error codes back to the SCIM sentinels:
//   23505 (unique_violation) on users.email / users.scim_external_id /
//         groups.name / groups.scim_external_id → ErrUserNameTaken or
//         ErrGroupNameTaken (the constraint context disambiguates
//         which sentinel to surface).
//   23503 (foreign_key_violation) on group_members.user_id →
//         ErrMemberNotFound.
//
// Schema requirements live in
// internal/repo/migrations/0008_slice8_scim.sql which adds
// `scim_external_id` to users + groups.

// ─── PostgresUserStore ──────────────────────────────────────────────

// PostgresUserStore is the production UserStore backed by the
// `users` table. The constructor takes a *pgxpool.Pool because
// every method runs as a single statement (no shared transaction
// across calls).
type PostgresUserStore struct {
	pool *pgxpool.Pool
}

// NewPostgresUserStore wraps a pgxpool.Pool as a UserStore.
func NewPostgresUserStore(pool *pgxpool.Pool) *PostgresUserStore {
	return &PostgresUserStore{pool: pool}
}

// Compile-time interface assertion.
var _ UserStore = (*PostgresUserStore)(nil)

// pgUserColumns is the canonical projection used by every read
// path. Mirrors the Rust query_as::<_, User> column order so a
// single scanUserRow helper covers ActiveKey + List + GetByID +
// GetByExternalID without per-call schema drift.
const pgUserColumns = `id, email, name, is_active, organization_id,
                       attributes, scim_external_id, created_at, updated_at`

// scanUserRow projects the 9-column SELECT into a UserRecord.
// Used by Get / GetByExternalID / List.
func scanUserRow(s pgScanner) (UserRecord, error) {
	var (
		rec        UserRecord
		orgID      *uuid.UUID
		attributes []byte
		externalID *string
		createdAt  time.Time
		updatedAt  time.Time
	)
	if err := s.Scan(
		&rec.ID, &rec.Email, &rec.Name, &rec.IsActive,
		&orgID, &attributes, &externalID, &createdAt, &updatedAt,
	); err != nil {
		return rec, err
	}
	rec.OrganizationID = orgID
	if len(attributes) > 0 {
		rec.Attributes = json.RawMessage(attributes)
	}
	rec.ScimExternalID = externalID
	rec.CreatedAt = createdAt
	rec.UpdatedAt = updatedAt
	return rec, nil
}

// Get returns the user with the given id, or (nil, nil) when no
// row matches.
func (s *PostgresUserStore) Get(ctx context.Context, id uuid.UUID) (*UserRecord, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+pgUserColumns+` FROM users WHERE id = $1`, id)
	rec, err := scanUserRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// GetByExternalID is the SCIM-idempotency lookup.
func (s *PostgresUserStore) GetByExternalID(ctx context.Context, externalID string) (*UserRecord, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+pgUserColumns+` FROM users WHERE scim_external_id = $1`, externalID)
	rec, err := scanUserRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// List returns up to `count` users from `startIndex` (1-based)
// ordered by created_at DESC, optionally filtered. Returns
// (rows, total) where `total` is the unpaginated count under the
// same predicate.
func (s *PostgresUserStore) List(ctx context.Context, filter *EqFilter, startIndex, count int) ([]UserRecord, int, error) {
	if startIndex < 1 {
		startIndex = 1
	}
	limit := count
	if limit < 1 {
		limit = 1
	}
	if limit > 500 {
		limit = 500
	}
	offset := startIndex - 1

	var (
		whereClause string
		args        []any
	)
	if filter != nil {
		switch filter.Attribute {
		case FilterUserName:
			whereClause = "WHERE email = $1"
			args = append(args, filter.Value)
		case FilterExternalID:
			whereClause = "WHERE scim_external_id = $1"
			args = append(args, filter.Value)
		default:
			return nil, 0, ErrUnsupportedFilter
		}
	}

	// Total (unpaginated) count under the same predicate.
	var total int64
	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*)::BIGINT FROM users `+whereClause, args...,
	).Scan(&total); err != nil {
		return nil, 0, err
	}

	// Paged select.
	pagedArgs := append([]any{}, args...)
	pagedArgs = append(pagedArgs, limit, offset)
	limitPlaceholder := fmt.Sprintf("$%d", len(args)+1)
	offsetPlaceholder := fmt.Sprintf("$%d", len(args)+2)
	rows, err := s.pool.Query(ctx,
		`SELECT `+pgUserColumns+` FROM users `+whereClause+
			` ORDER BY created_at DESC LIMIT `+limitPlaceholder+` OFFSET `+offsetPlaceholder,
		pagedArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	out := make([]UserRecord, 0)
	for rows.Next() {
		rec, err := scanUserRow(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, int(total), nil
}

// Put inserts a fresh row when no users.id matches, otherwise
// updates in place. Mirrors the Rust split between create_user
// (INSERT … VALUES … ON CONFLICT (kid) DO UPDATE in spirit) +
// update_scim_user UPDATE … WHERE id.
//
// userName uniqueness is enforced by the existing
// UNIQUE(email) + UNIQUE INDEX on scim_external_id. 23505 from
// either constraint maps to ErrUserNameTaken.
func (s *PostgresUserStore) Put(ctx context.Context, record UserRecord) error {
	attributes := record.Attributes
	if len(attributes) == 0 {
		attributes = json.RawMessage(`{}`)
	}
	_, err := s.pool.Exec(ctx,
		`INSERT INTO users
            (id, email, name, password_hash, is_active, auth_source,
             organization_id, attributes, scim_external_id)
         VALUES ($1, $2, $3, 'SCIM_EXTERNAL_ACCOUNT', $4, 'scim',
                 $5, $6, $7)
         ON CONFLICT (id) DO UPDATE SET
            email           = EXCLUDED.email,
            name            = EXCLUDED.name,
            is_active       = EXCLUDED.is_active,
            organization_id = EXCLUDED.organization_id,
            attributes      = EXCLUDED.attributes,
            scim_external_id= EXCLUDED.scim_external_id,
            updated_at      = NOW()`,
		record.ID, record.Email, record.Name, record.IsActive,
		record.OrganizationID, attributes, record.ScimExternalID)
	if err != nil {
		if isPgUniqueViolation(err) {
			return ErrUserNameTaken
		}
		return err
	}
	return nil
}

// SoftDelete flips is_active = false. Mirrors the Rust
// delete_user — `UPDATE users SET is_active = false WHERE id = $1`.
// Returns (false, nil) when no row matches.
func (s *PostgresUserStore) SoftDelete(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := s.pool.Exec(ctx,
		`UPDATE users SET is_active = false, updated_at = NOW()
          WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// ─── PostgresGroupStore ─────────────────────────────────────────────

// PostgresGroupStore is the production GroupStore backed by the
// `groups` + `group_members` tables. Member-mutation methods use
// transactions so the read-side never observes a partial state.
type PostgresGroupStore struct {
	pool *pgxpool.Pool
}

// NewPostgresGroupStore wraps a pgxpool.Pool as a GroupStore.
func NewPostgresGroupStore(pool *pgxpool.Pool) *PostgresGroupStore {
	return &PostgresGroupStore{pool: pool}
}

// Compile-time interface assertion.
var _ GroupStore = (*PostgresGroupStore)(nil)

const pgGroupColumns = `id, name, scim_external_id`

func scanGroupRow(s pgScanner) (GroupRecord, error) {
	var (
		rec        GroupRecord
		externalID *string
	)
	if err := s.Scan(&rec.ID, &rec.Name, &externalID); err != nil {
		return rec, err
	}
	rec.ScimExternalID = externalID
	return rec, nil
}

// Get returns the group with the given id, or (nil, nil) when
// no row matches.
func (s *PostgresGroupStore) Get(ctx context.Context, id uuid.UUID) (*GroupRecord, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+pgGroupColumns+` FROM groups WHERE id = $1`, id)
	rec, err := scanGroupRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// GetByExternalID is the SCIM-idempotency lookup.
func (s *PostgresGroupStore) GetByExternalID(ctx context.Context, externalID string) (*GroupRecord, error) {
	row := s.pool.QueryRow(ctx,
		`SELECT `+pgGroupColumns+` FROM groups WHERE scim_external_id = $1`, externalID)
	rec, err := scanGroupRow(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// List returns up to `count` groups ordered by name ASC.
func (s *PostgresGroupStore) List(ctx context.Context, filter *EqFilter, startIndex, count int) ([]GroupRecord, int, error) {
	if startIndex < 1 {
		startIndex = 1
	}
	limit := count
	if limit < 1 {
		limit = 1
	}
	if limit > 500 {
		limit = 500
	}
	offset := startIndex - 1

	var (
		whereClause string
		args        []any
	)
	if filter != nil {
		switch filter.Attribute {
		case FilterDisplayName:
			whereClause = "WHERE name = $1"
			args = append(args, filter.Value)
		case FilterExternalID:
			whereClause = "WHERE scim_external_id = $1"
			args = append(args, filter.Value)
		default:
			return nil, 0, ErrUnsupportedFilter
		}
	}

	var total int64
	if err := s.pool.QueryRow(ctx,
		`SELECT COUNT(*)::BIGINT FROM groups `+whereClause, args...,
	).Scan(&total); err != nil {
		return nil, 0, err
	}

	pagedArgs := append([]any{}, args...)
	pagedArgs = append(pagedArgs, limit, offset)
	limitPlaceholder := fmt.Sprintf("$%d", len(args)+1)
	offsetPlaceholder := fmt.Sprintf("$%d", len(args)+2)
	rows, err := s.pool.Query(ctx,
		`SELECT `+pgGroupColumns+` FROM groups `+whereClause+
			` ORDER BY name LIMIT `+limitPlaceholder+` OFFSET `+offsetPlaceholder,
		pagedArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := make([]GroupRecord, 0)
	for rows.Next() {
		rec, err := scanGroupRow(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, rec)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}
	return out, int(total), nil
}

// Put inserts a fresh row or updates in place. displayName
// uniqueness conflicts surface as ErrGroupNameTaken.
func (s *PostgresGroupStore) Put(ctx context.Context, record GroupRecord) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO groups (id, name, description, scim_external_id)
         VALUES ($1, $2, NULL, $3)
         ON CONFLICT (id) DO UPDATE SET
            name = EXCLUDED.name,
            scim_external_id = EXCLUDED.scim_external_id`,
		record.ID, record.Name, record.ScimExternalID)
	if err != nil {
		if isPgUniqueViolation(err) {
			return ErrGroupNameTaken
		}
		return err
	}
	return nil
}

// Delete hard-removes the row. Returns (false, nil) when no row
// matches. The FK on group_members.group_id has ON DELETE
// CASCADE, so memberships are dropped atomically.
func (s *PostgresGroupStore) Delete(ctx context.Context, id uuid.UUID) (bool, error) {
	tag, err := s.pool.Exec(ctx, `DELETE FROM groups WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	return tag.RowsAffected() > 0, nil
}

// Members returns (user_id, email, name) for every member of
// `groupID`, ordered by email ASC. Mirrors fn load_group_members
// — INNER JOIN against users so soft-deleted / missing members
// silently drop out.
func (s *PostgresGroupStore) Members(ctx context.Context, groupID uuid.UUID) ([]MemberView, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT u.id, u.email, u.name
           FROM users u
     INNER JOIN group_members gm ON gm.user_id = u.id
          WHERE gm.group_id = $1
          ORDER BY u.email`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]MemberView, 0)
	for rows.Next() {
		var m MemberView
		if err := rows.Scan(&m.UserID, &m.Email, &m.Name); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

// AddMembers inserts (groupID, userID) rows. Idempotent on
// the (group_id, user_id) PK via ON CONFLICT DO NOTHING. FK
// violations on user_id surface as ErrMemberNotFound.
func (s *PostgresGroupStore) AddMembers(ctx context.Context, groupID uuid.UUID, userIDs []uuid.UUID) error {
	if len(userIDs) == 0 {
		return nil
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := insertGroupMembersTx(ctx, tx, groupID, userIDs); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ReplaceMembers atomically clears + re-inserts the membership set.
func (s *PostgresGroupStore) ReplaceMembers(ctx context.Context, groupID uuid.UUID, userIDs []uuid.UUID) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx,
		`DELETE FROM group_members WHERE group_id = $1`, groupID); err != nil {
		return err
	}
	if err := insertGroupMembersTx(ctx, tx, groupID, userIDs); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// RemoveAllMembers clears the membership set.
func (s *PostgresGroupStore) RemoveAllMembers(ctx context.Context, groupID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM group_members WHERE group_id = $1`, groupID)
	return err
}

// RemoveMember drops a single membership tuple. Idempotent.
func (s *PostgresGroupStore) RemoveMember(ctx context.Context, groupID, userID uuid.UUID) error {
	_, err := s.pool.Exec(ctx,
		`DELETE FROM group_members WHERE group_id = $1 AND user_id = $2`,
		groupID, userID)
	return err
}

// insertGroupMembersTx performs the per-member INSERT inside the
// caller's transaction. Mirrors fn insert_group_members_tx — FK
// violations on user_id translate to ErrMemberNotFound.
func insertGroupMembersTx(ctx context.Context, tx pgx.Tx, groupID uuid.UUID, userIDs []uuid.UUID) error {
	for _, userID := range userIDs {
		if _, err := tx.Exec(ctx,
			`INSERT INTO group_members (group_id, user_id)
             VALUES ($1, $2) ON CONFLICT DO NOTHING`,
			groupID, userID); err != nil {
			if isPgFKViolation(err) {
				return ErrMemberNotFound
			}
			return err
		}
	}
	return nil
}

// ─── pg error classifiers ───────────────────────────────────────────

// isPgUniqueViolation reports whether `err` is a Postgres unique-
// constraint violation (SQLSTATE 23505).
func isPgUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == pgErrUniqueViolation
}

// isPgFKViolation reports whether `err` is a Postgres foreign-key
// constraint violation (SQLSTATE 23503).
func isPgFKViolation(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	return pgErr.Code == pgErrForeignKeyViolation
}
