// Package branches hosts the operation layer for media-set branches.
// Mirrors services/media-sets-service/src/handlers/branches.rs:
//
//   - list / create / delete / reset / merge
//
// Each mutation runs the relevant Cedar check first, persists inside
// a pgx transaction, and emits the matching audit envelope through
// libs/audit-trail's outbox helper. ADR-0022 atomicity preserved.
package branches

import (
	"context"
	"errors"
	"fmt"
	"strings"

	cedar "github.com/cedar-policy/cedar-go"
	"github.com/jackc/pgx/v5"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	audittrail "github.com/openfoundry/openfoundry-go/libs/audit-trail"
	cedarauthzlocal "github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/cedarauthz"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/repo"
)

// ── Errors ───────────────────────────────────────────────────────

// ErrBadRequest signals a 400-class failure (validation, name shape,
// transactionless reset attempt). The HTTP layer maps it to 400 or
// 422 depending on the code.
type ErrBadRequest struct{ Msg string }

func (e *ErrBadRequest) Error() string { return e.Msg }

// ErrNotFound signals a missing branch / parent / set. HTTP → 404.
type ErrNotFound struct{ What, ID string }

func (e *ErrNotFound) Error() string { return fmt.Sprintf("%s `%s` not found", e.What, e.ID) }

// ErrTransactionlessRejectsReset is the 422 envelope from
// `Advanced media set settings.md` ("Transactionless media set
// branches cannot be reset to an empty view").
type ErrTransactionlessRejectsReset struct{ MediaSetRID string }

func (e *ErrTransactionlessRejectsReset) Error() string {
	return fmt.Sprintf("media set `%s` is transactionless; reset is rejected", e.MediaSetRID)
}

// ErrMergeConflict carries the conflicting paths so the handler can
// surface them in the 409 body verbatim.
type ErrMergeConflict struct{ Paths []string }

func (e *ErrMergeConflict) Error() string {
	return fmt.Sprintf("merge refused: %d conflicting paths", len(e.Paths))
}

// ── Dependencies ────────────────────────────────────────────────

// Repository is the subset of *repo.Repo the service consumes.
type Repository interface {
	GetMediaSet(ctx context.Context, rid string) (*models.MediaSet, error)
	GetTransaction(ctx context.Context, rid string) (*models.MediaSetTransaction, error)
	RequireBranch(ctx context.Context, mediaSetRID, branchName string) (*models.MediaSetBranch, error)
	LockBranch(ctx context.Context, tx pgx.Tx, mediaSetRID, branchName string) (*models.MediaSetBranch, error)
	ListBranches(ctx context.Context, mediaSetRID string) ([]models.MediaSetBranch, error)
	CreateBranch(ctx context.Context, tx pgx.Tx, p repo.CreateBranchParams) (*models.MediaSetBranch, error)
	ReparentChildren(ctx context.Context, tx pgx.Tx, mediaSetRID, oldParentRID string, newParentRID *string) error
	SoftDeleteItemsOnBranch(ctx context.Context, tx pgx.Tx, branchRID string) (int64, error)
	DeleteBranchRow(ctx context.Context, tx pgx.Tx, mediaSetRID, branchName string) error
	RewindBranchHead(ctx context.Context, tx pgx.Tx, mediaSetRID, branchName string) (*models.MediaSetBranch, error)
	ListMergeSourceItems(ctx context.Context, tx pgx.Tx, branchRID string) ([]repo.MergeSourceItem, error)
	LiveTargetPaths(ctx context.Context, tx pgx.Tx, branchRID string) (map[string]struct{}, error)
	SoftDeleteAtPath(ctx context.Context, tx pgx.Tx, branchRID, path string) error
	InsertMergedItem(ctx context.Context, tx pgx.Tx, mediaSetRID, branchName, branchRID string, src repo.MergeSourceItem) (skipped bool, err error)
	BeginTx(ctx context.Context) (pgx.Tx, error)
}

// CedarGate is the subset of *cedarauthz.Engine the service uses.
type CedarGate interface {
	CheckMediaSet(ctx context.Context, claims *authmw.Claims, action cedar.EntityUID, set *models.MediaSet) error
}

// AuditEmitter mirrors libs/audit-trail.EmitToOutbox.
type AuditEmitter func(ctx context.Context, tx pgx.Tx, event audittrail.AuditEvent, ctxAudit audittrail.AuditContext) error

// Service wires the repo, Cedar gate, audit emitter.
type Service struct {
	Repo      Repository
	Cedar     CedarGate
	EmitAudit AuditEmitter
}

// New constructs a Service with the standard outbox audit emitter.
func New(r Repository, c CedarGate) *Service {
	return &Service{Repo: r, Cedar: c, EmitAudit: audittrail.EmitToOutbox}
}

// ── Operations ──────────────────────────────────────────────────

// ListInput captures the list op. View clearance on the parent is
// the gate.
type ListInput struct {
	MediaSetRID string
	Claims      *authmw.Claims
}

// List returns every branch on the media set after the view check.
func (s *Service) List(ctx context.Context, in ListInput) ([]models.MediaSetBranch, error) {
	set, err := s.requireSet(ctx, in.MediaSetRID)
	if err != nil {
		return nil, err
	}
	if err := s.Cedar.CheckMediaSet(ctx, in.Claims, cedarauthzlocal.ActionView(), set); err != nil {
		return nil, err
	}
	return s.Repo.ListBranches(ctx, in.MediaSetRID)
}

// CreateInput captures the create op.
type CreateInput struct {
	MediaSetRID string
	Body        models.CreateBranchRequest
	Claims      *authmw.Claims
	AuditCtx    audittrail.AuditContext
}

// Create mints a fresh branch under a parent (`from_branch`, default
// "main"), copying its head transaction (or the explicit
// `from_transaction_rid`) so the new branch sees the same view from
// the start. Mirrors `create_branch_op`.
func (s *Service) Create(ctx context.Context, in CreateInput) (*models.MediaSetBranch, error) {
	name := strings.TrimSpace(in.Body.Name)
	if name == "" {
		return nil, &ErrBadRequest{Msg: "branch name must not be empty"}
	}
	if strings.ContainsAny(name, "/ ") || strings.ContainsAny(name, "\t\n") {
		return nil, &ErrBadRequest{Msg: "branch name must not contain `/` or whitespace"}
	}
	set, err := s.requireSet(ctx, in.MediaSetRID)
	if err != nil {
		return nil, err
	}
	if err := s.Cedar.CheckMediaSet(ctx, in.Claims, cedarauthzlocal.ActionManage(), set); err != nil {
		return nil, err
	}
	parentName := "main"
	if in.Body.FromBranch != nil && *in.Body.FromBranch != "" {
		parentName = *in.Body.FromBranch
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
	parent, err := s.Repo.LockBranch(ctx, tx, in.MediaSetRID, parentName)
	if err != nil {
		return nil, err
	}
	if parent == nil {
		return nil, &ErrNotFound{What: "parent branch", ID: parentName}
	}

	headRID := parent.HeadTransactionRID
	if in.Body.FromTransactionRID != nil {
		trimmed := strings.TrimSpace(*in.Body.FromTransactionRID)
		if trimmed == "" {
			headRID = nil
		} else {
			txn, err := s.Repo.GetTransaction(ctx, trimmed)
			if err != nil {
				return nil, err
			}
			if txn == nil || txn.MediaSetRID != in.MediaSetRID {
				return nil, &ErrNotFound{What: "transaction", ID: trimmed}
			}
			headRID = &trimmed
		}
	}

	row, err := s.Repo.CreateBranch(ctx, tx, repo.CreateBranchParams{
		MediaSetRID:        in.MediaSetRID,
		BranchName:         name,
		ParentBranchRID:    &parent.BranchRID,
		HeadTransactionRID: headRID,
		CreatedBy:          claimSub(in.Claims),
	})
	if err != nil {
		var dup *repo.ErrBranchExists
		if errors.As(err, &dup) {
			return nil, &ErrBadRequest{Msg: dup.Error()}
		}
		return nil, err
	}

	// Re-use MediaSetCreated as the closest analogue (mirrors the
	// Rust comment about the absence of a dedicated branch-event
	// kind today).
	event := audittrail.NewMediaSetCreated(
		set.RID, set.ProjectRID, set.Markings,
		"branch:"+row.BranchName, set.Schema, set.TransactionPolicy, set.Virtual,
	)
	if err := s.EmitAudit(ctx, tx, event, in.AuditCtx); err != nil {
		return nil, fmt.Errorf("emit branch-create audit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return row, nil
}

// DeleteInput captures the delete op.
type DeleteInput struct {
	MediaSetRID string
	BranchName  string
	Claims      *authmw.Claims
	AuditCtx    audittrail.AuditContext
}

// Delete drops the branch row, re-parents children, soft-deletes
// every live item on the branch. The implicit `main` branch is
// rejected with a 400 (matches Rust).
func (s *Service) Delete(ctx context.Context, in DeleteInput) error {
	if in.BranchName == "main" {
		return &ErrBadRequest{Msg: "cannot delete the implicit `main` branch"}
	}
	set, err := s.requireSet(ctx, in.MediaSetRID)
	if err != nil {
		return err
	}
	if err := s.Cedar.CheckMediaSet(ctx, in.Claims, cedarauthzlocal.ActionManage(), set); err != nil {
		return err
	}
	branch, err := s.Repo.RequireBranch(ctx, in.MediaSetRID, in.BranchName)
	if err != nil {
		return err
	}
	if branch == nil {
		return &ErrNotFound{What: "branch", ID: in.BranchName}
	}

	tx, err := s.Repo.BeginTx(ctx)
	if err != nil {
		return err
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(ctx)
		}
	}()
	if err := s.Repo.ReparentChildren(ctx, tx, in.MediaSetRID, branch.BranchRID, branch.ParentBranchRID); err != nil {
		return err
	}
	if _, err := s.Repo.SoftDeleteItemsOnBranch(ctx, tx, branch.BranchRID); err != nil {
		return err
	}
	if err := s.Repo.DeleteBranchRow(ctx, tx, in.MediaSetRID, in.BranchName); err != nil {
		return err
	}
	event := audittrail.NewMediaSetDeleted(set.RID, set.ProjectRID, set.Markings)
	if err := s.EmitAudit(ctx, tx, event, in.AuditCtx); err != nil {
		return fmt.Errorf("emit branch-delete audit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

// ResetInput captures the reset op.
type ResetInput struct {
	MediaSetRID string
	BranchName  string
	Claims      *authmw.Claims
	AuditCtx    audittrail.AuditContext
}

// Reset performs the "git reset --hard" semantic: every live item on
// the branch is soft-deleted and the head pointer rewinds to NULL.
// TRANSACTIONAL sets only — transactionless attempts are rejected
// with ErrTransactionlessRejectsReset (HTTP 422).
func (s *Service) Reset(ctx context.Context, in ResetInput) (*models.ResetBranchResponse, error) {
	set, err := s.requireSet(ctx, in.MediaSetRID)
	if err != nil {
		return nil, err
	}
	if err := s.Cedar.CheckMediaSet(ctx, in.Claims, cedarauthzlocal.ActionManage(), set); err != nil {
		return nil, err
	}
	if set.TransactionPolicy != "TRANSACTIONAL" {
		return nil, &ErrTransactionlessRejectsReset{MediaSetRID: in.MediaSetRID}
	}
	branch, err := s.Repo.RequireBranch(ctx, in.MediaSetRID, in.BranchName)
	if err != nil {
		return nil, err
	}
	if branch == nil {
		return nil, &ErrNotFound{What: "branch", ID: in.BranchName}
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
	purged, err := s.Repo.SoftDeleteItemsOnBranch(ctx, tx, branch.BranchRID)
	if err != nil {
		return nil, err
	}
	updated, err := s.Repo.RewindBranchHead(ctx, tx, in.MediaSetRID, in.BranchName)
	if err != nil {
		return nil, err
	}
	priorHead := ""
	if branch.HeadTransactionRID != nil {
		priorHead = *branch.HeadTransactionRID
	}
	event := audittrail.NewMediaSetTransactionAborted(
		set.RID, set.ProjectRID, set.Markings, priorHead, in.BranchName,
	)
	if err := s.EmitAudit(ctx, tx, event, in.AuditCtx); err != nil {
		return nil, fmt.Errorf("emit branch-reset audit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return &models.ResetBranchResponse{Branch: *updated, ItemsSoftDeleted: purged}, nil
}

// MergeInput captures the merge op.
type MergeInput struct {
	MediaSetRID  string
	SourceBranch string
	Body         models.MergeBranchRequest
	Claims       *authmw.Claims
	AuditCtx     audittrail.AuditContext
}

// Merge copies every live source item to the target branch. The
// resolution policy decides what happens on overlapping paths:
//
//   - LATEST_WINS (default) — soft-delete target row, copy source row.
//   - FAIL_ON_CONFLICT      — return ErrMergeConflict with the paths.
func (s *Service) Merge(ctx context.Context, in MergeInput) (*models.MergeBranchResponse, error) {
	target := strings.TrimSpace(in.Body.TargetBranch)
	if target == "" {
		return nil, &ErrBadRequest{Msg: "target_branch is required"}
	}
	if target == in.SourceBranch {
		return nil, &ErrBadRequest{Msg: "target_branch must differ from source_branch"}
	}
	resolution := in.Body.Resolution
	if resolution == "" {
		resolution = models.MergeLatestWins
	}
	if !resolution.IsValid() {
		return nil, &ErrBadRequest{Msg: "resolution must be LATEST_WINS or FAIL_ON_CONFLICT"}
	}
	set, err := s.requireSet(ctx, in.MediaSetRID)
	if err != nil {
		return nil, err
	}
	if err := s.Cedar.CheckMediaSet(ctx, in.Claims, cedarauthzlocal.ActionManage(), set); err != nil {
		return nil, err
	}
	source, err := s.Repo.RequireBranch(ctx, in.MediaSetRID, in.SourceBranch)
	if err != nil {
		return nil, err
	}
	if source == nil {
		return nil, &ErrNotFound{What: "source branch", ID: in.SourceBranch}
	}
	dst, err := s.Repo.RequireBranch(ctx, in.MediaSetRID, target)
	if err != nil {
		return nil, err
	}
	if dst == nil {
		return nil, &ErrNotFound{What: "target branch", ID: target}
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
	srcItems, err := s.Repo.ListMergeSourceItems(ctx, tx, source.BranchRID)
	if err != nil {
		return nil, err
	}
	tgtPaths, err := s.Repo.LiveTargetPaths(ctx, tx, dst.BranchRID)
	if err != nil {
		return nil, err
	}
	conflicts := make([]string, 0)
	for _, it := range srcItems {
		if _, ok := tgtPaths[it.Path]; ok {
			conflicts = append(conflicts, it.Path)
		}
	}
	if resolution == models.MergeFailOnConflict && len(conflicts) > 0 {
		return nil, &ErrMergeConflict{Paths: conflicts}
	}

	var copied, overwritten, skipped int64
	for _, it := range srcItems {
		_, isConflict := tgtPaths[it.Path]
		if isConflict {
			if err := s.Repo.SoftDeleteAtPath(ctx, tx, dst.BranchRID, it.Path); err != nil {
				return nil, err
			}
			overwritten++
		} else {
			copied++
		}
		dup, err := s.Repo.InsertMergedItem(ctx, tx, in.MediaSetRID, target, dst.BranchRID, it)
		if err != nil {
			return nil, err
		}
		if dup {
			skipped++
		}
	}

	event := audittrail.NewMediaSetTransactionCommitted(
		set.RID, set.ProjectRID, set.Markings,
		fmt.Sprintf("merge:%s->%s", in.SourceBranch, target), target,
	)
	if err := s.EmitAudit(ctx, tx, event, in.AuditCtx); err != nil {
		return nil, fmt.Errorf("emit merge audit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return &models.MergeBranchResponse{
		SourceBranch:     in.SourceBranch,
		TargetBranch:     target,
		Resolution:       string(resolution),
		PathsCopied:      copied,
		PathsOverwritten: overwritten,
		PathsSkipped:     skipped,
	}, nil
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
