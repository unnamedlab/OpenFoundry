// Package transactions hosts the operation layer for media-set
// transactions (open / commit / abort / list). Mirrors
// services/media-sets-service/src/handlers/transactions.rs.
//
// Atomicity: the close path runs the state flip + the optional
// REPLACE-mode soft-delete + the branch head advance + the audit emit
// inside a single pgx.Tx so a partial close can never leave the row
// in COMMITTED with the head pointer unmoved.
package transactions

import (
	"context"
	"errors"
	"fmt"

	cedar "github.com/cedar-policy/cedar-go"
	"github.com/jackc/pgx/v5"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	audittrail "github.com/openfoundry/openfoundry-go/libs/audit-trail"
	cedarauthzlocal "github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/cedarauthz"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/metrics"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/repo"
)

// MaxItemsPerTransaction is the Foundry per-transaction item cap.
// Mirrors transactions.rs MAX_ITEMS_PER_TRANSACTION.
const MaxItemsPerTransaction int64 = 10_000

// ── Errors ───────────────────────────────────────────────────────

// ErrBadRequest is the 400-class failure (validation + REPLACE on a
// transactionless set + non-terminal close target).
type ErrBadRequest struct{ Msg string }

func (e *ErrBadRequest) Error() string { return e.Msg }

// ErrNotFound is the missing-row failure.
type ErrNotFound struct{ What, ID string }

func (e *ErrNotFound) Error() string { return fmt.Sprintf("%s `%s` not found", e.What, e.ID) }

// ErrTransactionless is the 422 envelope when a transactionless set
// is asked to open / close a transaction.
type ErrTransactionless struct{ MediaSetRID string }

func (e *ErrTransactionless) Error() string {
	return fmt.Sprintf("media set `%s` is transactionless; transactions are not supported", e.MediaSetRID)
}

// ErrTransactionTerminal signals an attempt to close an already-closed
// transaction (COMMITTED → ABORTED, etc.). HTTP → 422.
type ErrTransactionTerminal struct {
	RID   string
	State string
}

func (e *ErrTransactionTerminal) Error() string {
	return fmt.Sprintf("transaction `%s` is already in terminal state `%s`", e.RID, e.State)
}

// ErrTransactionConflict is the 409 envelope when an OPEN transaction
// already exists on (set, branch).
type ErrTransactionConflict struct {
	MediaSetRID string
	Branch      string
}

func (e *ErrTransactionConflict) Error() string {
	return fmt.Sprintf("an OPEN transaction already exists on (media_set=`%s`, branch=`%s`)",
		e.MediaSetRID, e.Branch)
}

// ── Dependencies ────────────────────────────────────────────────

// Repository is the subset of *repo.Repo the service consumes.
type Repository interface {
	GetMediaSet(ctx context.Context, rid string) (*models.MediaSet, error)
	GetTransaction(ctx context.Context, rid string) (*models.MediaSetTransaction, error)
	CreateTransaction(ctx context.Context, tx pgx.Tx, p repo.CreateTransactionParams) (*models.MediaSetTransaction, error)
	CloseTransaction(ctx context.Context, tx pgx.Tx, p repo.CloseTransactionParams) (*models.MediaSetTransaction, error)
	HardDeleteAbortedItems(ctx context.Context, tx pgx.Tx, transactionRID string) error
	SoftDeletePriorReplaceItems(ctx context.Context, tx pgx.Tx, mediaSetRID, branch, transactionRID string) error
	AdvanceBranchHead(ctx context.Context, tx pgx.Tx, mediaSetRID, branchName, txRID string) error
	ListTransactionHistory(ctx context.Context, mediaSetRID string) ([]models.TransactionHistoryEntry, error)
	BeginTx(ctx context.Context) (pgx.Tx, error)
}

// CedarGate is the subset of *cedarauthz.Engine the service uses.
type CedarGate interface {
	CheckMediaSet(ctx context.Context, claims *authmw.Claims, action cedar.EntityUID, set *models.MediaSet) error
}

// AuditEmitter mirrors libs/audit-trail.EmitToOutbox.
type AuditEmitter func(ctx context.Context, tx pgx.Tx, event audittrail.AuditEvent, ctxAudit audittrail.AuditContext) error

// Service wires the repo, Cedar gate, audit emitter, Prometheus families.
type Service struct {
	Repo      Repository
	Cedar     CedarGate
	Metrics   *metrics.Metrics
	EmitAudit AuditEmitter
}

// New constructs a Service with the standard outbox audit emitter.
func New(r Repository, c CedarGate, m *metrics.Metrics) *Service {
	return &Service{Repo: r, Cedar: c, Metrics: m, EmitAudit: audittrail.EmitToOutbox}
}

// ── Operations ──────────────────────────────────────────────────

// OpenInput captures what Open needs.
type OpenInput struct {
	MediaSetRID string
	Body        models.OpenTransactionRequest
	Claims      *authmw.Claims
	AuditCtx    audittrail.AuditContext
}

// Open creates a new OPEN transaction. Cedar gate: media_set::manage.
// REPLACE + transactionless ⇒ 422; concurrent open ⇒ 409.
func (s *Service) Open(ctx context.Context, in OpenInput) (*models.MediaSetTransaction, error) {
	set, err := s.requireSet(ctx, in.MediaSetRID)
	if err != nil {
		return nil, err
	}
	if err := s.Cedar.CheckMediaSet(ctx, in.Claims, cedarauthzlocal.ActionManage(), set); err != nil {
		return nil, err
	}
	if set.TransactionPolicy != "TRANSACTIONAL" {
		return nil, &ErrTransactionless{MediaSetRID: in.MediaSetRID}
	}
	branch := "main"
	if in.Body.Branch != nil && *in.Body.Branch != "" {
		branch = *in.Body.Branch
	}
	mode := models.WriteModeModify
	if in.Body.WriteMode != nil {
		mode = *in.Body.WriteMode
	}
	if !mode.IsValid() {
		return nil, &ErrBadRequest{Msg: "write_mode must be MODIFY or REPLACE"}
	}

	tx, err := s.Repo.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	row, err := s.Repo.CreateTransaction(ctx, tx, repo.CreateTransactionParams{
		MediaSetRID: in.MediaSetRID,
		Branch:      branch,
		WriteMode:   string(mode),
		OpenedBy:    claimSub(in.Claims),
	})
	if err != nil {
		var conflict *repo.ErrTransactionConflict
		if errors.As(err, &conflict) {
			return nil, &ErrTransactionConflict{MediaSetRID: conflict.MediaSetRID, Branch: conflict.Branch}
		}
		return nil, err
	}
	event := audittrail.NewMediaSetTransactionOpened(
		set.RID, set.ProjectRID, set.Markings, row.RID, row.Branch,
	)
	if err := s.EmitAudit(ctx, tx, event, in.AuditCtx); err != nil {
		return nil, fmt.Errorf("emit transaction-opened audit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true

	// Bump the per-set in-flight transaction gauge AFTER commit so
	// the value never reflects an uncommitted OPEN.
	s.Metrics.MediaActiveTransactions.WithLabelValues(in.MediaSetRID).Inc()
	return row, nil
}

// CloseInput captures the close op.
type CloseInput struct {
	RID      string
	Target   models.TransactionState
	Claims   *authmw.Claims
	AuditCtx audittrail.AuditContext
}

// Commit closes the transaction with state COMMITTED. Convenience.
func (s *Service) Commit(ctx context.Context, in CloseInput) (*models.MediaSetTransaction, error) {
	in.Target = models.TxStateCommitted
	return s.close(ctx, in)
}

// Abort closes the transaction with state ABORTED. Convenience.
func (s *Service) Abort(ctx context.Context, in CloseInput) (*models.MediaSetTransaction, error) {
	in.Target = models.TxStateAborted
	return s.close(ctx, in)
}

func (s *Service) close(ctx context.Context, in CloseInput) (*models.MediaSetTransaction, error) {
	if !in.Target.IsTerminal() {
		return nil, &ErrBadRequest{Msg: "transaction can only transition to COMMITTED or ABORTED"}
	}
	current, err := s.Repo.GetTransaction(ctx, in.RID)
	if err != nil {
		return nil, err
	}
	if current == nil {
		return nil, &ErrNotFound{What: "transaction", ID: in.RID}
	}
	if models.TransactionState(current.State).IsTerminal() {
		return nil, &ErrTransactionTerminal{RID: in.RID, State: current.State}
	}
	set, err := s.requireSet(ctx, current.MediaSetRID)
	if err != nil {
		return nil, err
	}
	// Cedar gate on the parent set — only `manage` can close.
	if err := s.Cedar.CheckMediaSet(ctx, in.Claims, cedarauthzlocal.ActionManage(), set); err != nil {
		return nil, err
	}

	tx, err := s.Repo.BeginTx(ctx)
	if err != nil {
		return nil, err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	row, err := s.Repo.CloseTransaction(ctx, tx, repo.CloseTransactionParams{
		RID:    in.RID,
		Target: in.Target,
	})
	if err != nil {
		return nil, err
	}

	if in.Target == models.TxStateAborted {
		if err := s.Repo.HardDeleteAbortedItems(ctx, tx, in.RID); err != nil {
			return nil, err
		}
	} else {
		// COMMITTED: in REPLACE mode, soft-delete every prior live
		// item on the same (set, branch) that wasn't written by
		// this transaction.
		if current.WriteMode == string(models.WriteModeReplace) {
			if err := s.Repo.SoftDeletePriorReplaceItems(ctx, tx, row.MediaSetRID, row.Branch, in.RID); err != nil {
				return nil, err
			}
		}
		// Advance the branch head pointer.
		if err := s.Repo.AdvanceBranchHead(ctx, tx, row.MediaSetRID, row.Branch, in.RID); err != nil {
			return nil, err
		}
	}

	var event audittrail.AuditEvent
	switch in.Target {
	case models.TxStateCommitted:
		event = audittrail.NewMediaSetTransactionCommitted(
			set.RID, set.ProjectRID, set.Markings, row.RID, row.Branch,
		)
	case models.TxStateAborted:
		event = audittrail.NewMediaSetTransactionAborted(
			set.RID, set.ProjectRID, set.Markings, row.RID, row.Branch,
		)
	}
	if err := s.EmitAudit(ctx, tx, event, in.AuditCtx); err != nil {
		return nil, fmt.Errorf("emit transaction-close audit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true

	// Decrement the per-set gauge AFTER commit so a duplicate close
	// (rejected upstream) cannot drive the gauge negative.
	s.Metrics.MediaActiveTransactions.WithLabelValues(row.MediaSetRID).Dec()
	return row, nil
}

// ListInput captures the history op.
type ListInput struct {
	MediaSetRID string
	Claims      *authmw.Claims
}

// ListHistory returns the per-transaction history feed. View clearance
// on the parent set is the gate.
func (s *Service) ListHistory(ctx context.Context, in ListInput) ([]models.TransactionHistoryEntry, error) {
	set, err := s.requireSet(ctx, in.MediaSetRID)
	if err != nil {
		return nil, err
	}
	if err := s.Cedar.CheckMediaSet(ctx, in.Claims, cedarauthzlocal.ActionView(), set); err != nil {
		return nil, err
	}
	return s.Repo.ListTransactionHistory(ctx, in.MediaSetRID)
}

// ── Helpers ─────────────────────────────────────────────────────

func (s *Service) requireSet(ctx context.Context, rid string) (*models.MediaSet, error) {
	set, err := s.Repo.GetMediaSet(ctx, rid)
	if err != nil {
		return nil, err
	}
	if set == nil {
		return nil, &ErrNotFound{What: "media set", ID: rid}
	}
	return set, nil
}

func claimSub(c *authmw.Claims) string {
	if c == nil {
		return ""
	}
	return c.Sub.String()
}
