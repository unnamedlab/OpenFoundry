package repo

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/models"
)

// TransactionRIDPrefix is the canonical Foundry prefix for transaction
// RIDs. Mirrors Rust `TRANSACTION_RID_PREFIX`.
const TransactionRIDPrefix = "ri.foundry.main.media_transaction."

// NewTransactionRID mints a fresh transaction RID.
func NewTransactionRID() string {
	return TransactionRIDPrefix + uuid.New().String()
}

const transactionSelectColumns = `rid, media_set_rid, branch, state, opened_at, closed_at, opened_by`

// GetTransaction returns the transaction row by RID. (nil, nil) when
// absent — handler maps to 404.
func (r *Repo) GetTransaction(ctx context.Context, rid string) (*models.MediaSetTransaction, error) {
	row := r.Pool.QueryRow(ctx,
		`SELECT `+transactionSelectColumns+`, write_mode
		   FROM media_set_transactions
		  WHERE rid = $1`,
		rid,
	)
	v, err := scanTransaction(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return v, err
}

// CreateTransactionParams captures the columns the INSERT writes.
type CreateTransactionParams struct {
	MediaSetRID string
	Branch      string
	WriteMode   string
	OpenedBy    string
}

// CreateTransaction inserts a fresh row in OPEN state. Surfaces
// *ErrTransactionConflict when the partial unique index trips so
// callers can map to 409.
func (r *Repo) CreateTransaction(ctx context.Context, tx pgx.Tx, p CreateTransactionParams) (*models.MediaSetTransaction, error) {
	rid := NewTransactionRID()
	row := tx.QueryRow(ctx,
		`INSERT INTO media_set_transactions
		    (rid, media_set_rid, branch, state, opened_by, write_mode)
		 VALUES ($1, $2, $3, 'OPEN', $4, $5)
		 RETURNING `+transactionSelectColumns+`, write_mode`,
		rid, p.MediaSetRID, p.Branch, p.OpenedBy, p.WriteMode,
	)
	v, err := scanTransaction(row)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, &ErrTransactionConflict{MediaSetRID: p.MediaSetRID, Branch: p.Branch}
		}
		return nil, err
	}
	return v, nil
}

// CloseTransactionParams captures the close mutation.
type CloseTransactionParams struct {
	RID    string
	Target models.TransactionState // COMMITTED or ABORTED
}

// CloseTransaction flips the row to its terminal state and stamps
// closed_at = NOW(). Returns the post-update row.
func (r *Repo) CloseTransaction(ctx context.Context, tx pgx.Tx, p CloseTransactionParams) (*models.MediaSetTransaction, error) {
	row := tx.QueryRow(ctx,
		`UPDATE media_set_transactions
		    SET state = $2, closed_at = NOW()
		  WHERE rid = $1
		 RETURNING `+transactionSelectColumns+`, write_mode`,
		p.RID, string(p.Target),
	)
	return scanTransaction(row)
}

// HardDeleteAbortedItems removes every row staged by an aborted
// transaction. Hard-delete (not soft) since aborted writes were never
// readable to begin with.
func (r *Repo) HardDeleteAbortedItems(ctx context.Context, tx pgx.Tx, transactionRID string) error {
	_, err := tx.Exec(ctx,
		`DELETE FROM media_items WHERE transaction_rid = $1`,
		transactionRID,
	)
	return err
}

// SoftDeletePriorReplaceItems implements the REPLACE-mode semantic:
// every prior live item on the same (media_set, branch) that wasn't
// written by `transactionRID` becomes inaccessible. The path-dedup
// index stays consistent because we only flip deleted_at.
func (r *Repo) SoftDeletePriorReplaceItems(ctx context.Context, tx pgx.Tx, mediaSetRID, branch, transactionRID string) error {
	_, err := tx.Exec(ctx,
		`UPDATE media_items
		    SET deleted_at = NOW()
		  WHERE media_set_rid = $1
		    AND branch = $2
		    AND deleted_at IS NULL
		    AND COALESCE(transaction_rid, '') <> $3`,
		mediaSetRID, branch, transactionRID,
	)
	return err
}

// ListTransactionHistory returns the full per-transaction history feed
// with item-diff counts. Mirrors the Rust SQL verbatim.
func (r *Repo) ListTransactionHistory(ctx context.Context, mediaSetRID string) ([]models.TransactionHistoryEntry, error) {
	rows, err := r.Pool.Query(ctx,
		`WITH stats AS (
		    SELECT
		        t.rid,
		        COUNT(*) FILTER (
		            WHERE i.transaction_rid = t.rid
		              AND i.deduplicated_from IS NULL
		        ) AS items_added,
		        COUNT(*) FILTER (
		            WHERE i.transaction_rid = t.rid
		              AND i.deduplicated_from IS NOT NULL
		        ) AS items_modified,
		        COUNT(*) FILTER (
		            WHERE i.deleted_at IS NOT NULL
		              AND i.media_set_rid = t.media_set_rid
		              AND i.branch = t.branch
		              AND i.transaction_rid IS DISTINCT FROM t.rid
		              AND i.deleted_at >= t.opened_at
		              AND (t.closed_at IS NULL OR i.deleted_at <= t.closed_at)
		        ) AS items_deleted
		      FROM media_set_transactions t
		 LEFT JOIN media_items i
		        ON i.media_set_rid = t.media_set_rid
		       AND (i.transaction_rid = t.rid OR i.deleted_at IS NOT NULL)
		     WHERE t.media_set_rid = $1
		  GROUP BY t.rid
		)
		SELECT t.rid, t.media_set_rid, t.branch, t.state, t.write_mode,
		       t.opened_at, t.closed_at, t.opened_by,
		       COALESCE(s.items_added, 0)    AS items_added,
		       COALESCE(s.items_modified, 0) AS items_modified,
		       COALESCE(s.items_deleted, 0)  AS items_deleted
		  FROM media_set_transactions t
		 LEFT JOIN stats s ON s.rid = t.rid
		 WHERE t.media_set_rid = $1
		 ORDER BY t.opened_at DESC`,
		mediaSetRID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]models.TransactionHistoryEntry, 0)
	for rows.Next() {
		v := models.TransactionHistoryEntry{}
		var closed *interface{}
		_ = closed
		if err := rows.Scan(
			&v.RID, &v.MediaSetRID, &v.Branch, &v.State, &v.WriteMode,
			&v.OpenedAt, &v.ClosedAt, &v.OpenedBy,
			&v.ItemsAdded, &v.ItemsModified, &v.ItemsDeleted,
		); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

func scanTransaction(r rowLikeT) (*models.MediaSetTransaction, error) {
	v := &models.MediaSetTransaction{}
	if err := r.Scan(
		&v.RID, &v.MediaSetRID, &v.Branch, &v.State,
		&v.OpenedAt, &v.ClosedAt, &v.OpenedBy, &v.WriteMode,
	); err != nil {
		return nil, err
	}
	return v, nil
}

// ── Errors ────────────────────────────────────────────────────────

// ErrTransactionConflict is returned when the partial UNIQUE index
// `uq_media_set_transactions_one_open_per_branch` trips.
type ErrTransactionConflict struct {
	MediaSetRID string
	Branch      string
}

func (e *ErrTransactionConflict) Error() string {
	return fmt.Sprintf("an OPEN transaction already exists on (media_set=`%s`, branch=`%s`)",
		e.MediaSetRID, e.Branch)
}
