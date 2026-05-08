// Package mediaitems hosts the operation layer for media items.
// Mirrors services/media-sets-service/src/handlers/items.rs:
//
//   - presigned upload (with Foundry path-dedup contract)
//   - presigned download (Cedar gate + audit emit)
//   - list (parent-set view gate + per-item read filter)
//   - get / delete (per-item Cedar gates)
//   - register-virtual (virtual sets only)
//   - patch markings (manage gate; granular override)
//
// Each mutation runs the relevant Cedar check first, persists inside
// a pgx transaction, and emits the matching audit envelope through
// libs/audit-trail's outbox helper. ADR-0022 atomicity preserved end
// to end.
package mediaitems

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	cedar "github.com/cedar-policy/cedar-go"
	"github.com/jackc/pgx/v5"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	audittrail "github.com/openfoundry/openfoundry-go/libs/audit-trail"
	cedarauthzlocal "github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/cedarauthz"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/mediapath"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/storage"
)

// MaxItemsPerTransaction is the Foundry per-transaction cap (Advanced
// media set settings.md → "A maximum of 10,000 items can be written
// in a single transaction"). Hard-coded here because the transactions
// handler isn't ported to Go yet; once it lands, it will own the
// constant.
const MaxItemsPerTransaction int64 = 10_000

// ── Errors ───────────────────────────────────────────────────────

// ErrBadRequest signals a 400-class failure (validation, schema,
// transactional/virtual misuse). The HTTP layer maps it to 400.
type ErrBadRequest struct{ Msg string }

func (e *ErrBadRequest) Error() string { return e.Msg }

// ErrNotFound signals a missing item / set. HTTP → 404.
type ErrNotFound struct{ What, ID string }

func (e *ErrNotFound) Error() string { return fmt.Sprintf("%s `%s` not found", e.What, e.ID) }

// ── Dependencies ────────────────────────────────────────────────

// Repository is the subset of *repo.Repo the service consumes.
type Repository interface {
	GetMediaSet(ctx context.Context, rid string) (*models.MediaSet, error)
	GetMediaItem(ctx context.Context, rid string) (*models.MediaItem, error)
	GetMediaItemFull(ctx context.Context, rid string) (*models.MediaItem, error)
	GetTransaction(ctx context.Context, rid string) (*models.MediaSetTransaction, error)
	ListMediaItems(ctx context.Context, p repo.ListMediaItemsParams) ([]models.MediaItem, error)
	CreateMediaItem(ctx context.Context, tx pgx.Tx, p repo.CreateMediaItemParams) (*models.MediaItem, error)
	SoftDeleteMediaItem(ctx context.Context, tx pgx.Tx, rid string) (bool, error)
	SoftDeletePreviousAtPath(ctx context.Context, tx pgx.Tx, mediaSetRID, branch, path string) (*string, error)
	PatchMediaItemMarkings(ctx context.Context, tx pgx.Tx, rid string, markings []string) (*models.MediaItem, error)
	CountTransactionLiveItems(ctx context.Context, transactionRID string) (int64, error)
	BeginTx(ctx context.Context) (pgx.Tx, error)
}

// CedarGate is the subset of *cedarauthz.Engine the service uses.
type CedarGate interface {
	CheckMediaSet(ctx context.Context, claims *authmw.Claims, action cedar.EntityUID, set *models.MediaSet) error
	CheckMediaItem(ctx context.Context, claims *authmw.Claims, action cedar.EntityUID, item *models.MediaItem, parent *models.MediaSet) error
}

// AuditEmitter mirrors libs/audit-trail.EmitToOutbox so tests can
// inject a no-op. Same shape as the equivalent in accesspatterns.
type AuditEmitter func(ctx context.Context, tx pgx.Tx, event audittrail.AuditEvent, ctxAudit audittrail.AuditContext) error

// VirtualResolver builds the external download URL for a virtual
// media item (sets whose bytes live in an external source system).
// nil is acceptable in production when no connector service is
// configured — virtual download then falls back to the row's
// storage_uri verbatim.
type VirtualResolver interface {
	Resolve(ctx context.Context, set *models.MediaSet, item *models.MediaItem, ttl time.Duration) (*storage.PresignedURL, error)
}

// PresignSigner mints a short-lived claim the gateway validates
// before letting the GET reach storage. Optional — when nil, the
// download URL is returned verbatim.
type PresignSigner interface {
	Sign(sub, itemRID string, markings []string, ttl time.Duration) (string, error)
}

// ── Service ──────────────────────────────────────────────────────

// Service wires the repo, Cedar gate, audit emitter and storage backend.
type Service struct {
	Repo      Repository
	Cedar     CedarGate
	Storage   storage.Backend
	EmitAudit AuditEmitter
	// VirtualResolver is consulted on PresignDownload for virtual
	// sets. nil → fall back to the row's storage_uri verbatim.
	VirtualResolver VirtualResolver
	// PresignSigner is consulted on PresignDownload to append a
	// short-lived claim. nil → no claim is appended.
	PresignSigner PresignSigner
	// PresignTTL is the default URL lifetime when the caller does
	// not supply expires_in_seconds. Mirrors AppState::presign_ttl.
	PresignTTL time.Duration
}

// New constructs a Service with the standard outbox audit emitter.
func New(r Repository, c CedarGate, s storage.Backend, presignTTL time.Duration) *Service {
	if presignTTL <= 0 {
		presignTTL = 5 * time.Minute
	}
	return &Service{Repo: r, Cedar: c, Storage: s, EmitAudit: audittrail.EmitToOutbox, PresignTTL: presignTTL}
}

// ── Operations ──────────────────────────────────────────────────

// PresignUploadInput captures what the upload op needs.
type PresignUploadInput struct {
	MediaSetRID string
	Body        models.PresignedUploadRequest
	Claims      *authmw.Claims
	AuditCtx    audittrail.AuditContext
}

// PresignUploadResult bundles the persisted row + the upload URL.
type PresignUploadResult struct {
	Item *models.MediaItem
	URL  *storage.PresignedURL
}

// PresignUpload registers a media item, applies path dedup, emits the
// upload audit envelope, and returns a PUT URL pointing at the
// content-addressed storage key. Mirrors `presigned_upload_op`.
func (s *Service) PresignUpload(ctx context.Context, in PresignUploadInput) (*PresignUploadResult, error) {
	if strings.TrimSpace(in.Body.Path) == "" {
		return nil, &ErrBadRequest{Msg: "path must not be empty"}
	}
	set, err := s.Repo.GetMediaSet(ctx, in.MediaSetRID)
	if err != nil {
		return nil, err
	}
	if set == nil {
		return nil, &ErrNotFound{What: "media set", ID: in.MediaSetRID}
	}
	if set.Virtual {
		return nil, &ErrBadRequest{Msg: "virtual media sets do not accept presigned uploads — register items with POST /media-sets/{rid}/virtual-items instead"}
	}
	// Cedar pre-check: media_set::manage on the parent.
	if err := s.Cedar.CheckMediaSet(ctx, in.Claims, cedarauthzlocal.ActionManage(), set); err != nil {
		return nil, err
	}
	branch := "main"
	if in.Body.Branch != nil && *in.Body.Branch != "" {
		branch = *in.Body.Branch
	}
	transactionRID, err := s.resolveTransactionRID(ctx, set, branch, in.Body)
	if err != nil {
		return nil, err
	}
	newRID := repo.NewMediaItemRID()
	sha := ""
	if in.Body.SHA256 != nil {
		sha = *in.Body.SHA256
	}
	if sha == "" {
		// Mirrors Rust placeholder: deterministic per-RID synthetic
		// hash so two concurrent registrations don't collide on
		// storage_uri before the upload completes.
		seed := strings.ReplaceAll(strings.TrimPrefix(newRID, repo.MediaItemRIDPrefix), "-", "")
		sum := sha256.Sum256([]byte(seed))
		sha = hex.EncodeToString(sum[:])
	}
	key := mediapath.New(in.MediaSetRID, branch, sha)
	storageURI := mediapath.StorageURI(s.Storage.Bucket(), key)

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
	dedup, err := s.Repo.SoftDeletePreviousAtPath(ctx, tx, in.MediaSetRID, branch, in.Body.Path)
	if err != nil {
		return nil, err
	}
	size := int64(0)
	if in.Body.SizeBytes != nil {
		size = *in.Body.SizeBytes
	}
	row, err := s.Repo.CreateMediaItem(ctx, tx, repo.CreateMediaItemParams{
		RID:              newRID,
		MediaSetRID:      in.MediaSetRID,
		Branch:           branch,
		TransactionRID:   transactionRID,
		Path:             in.Body.Path,
		MimeType:         in.Body.MimeType,
		SizeBytes:        size,
		SHA256:           sha,
		Metadata:         json.RawMessage("{}"),
		StorageURI:       storageURI,
		DeduplicatedFrom: dedup,
		RetentionSeconds: set.RetentionSeconds,
	})
	if err != nil {
		return nil, fmt.Errorf("create media item: %w", err)
	}
	// Cedar item-write gate AFTER row materialises (the policy
	// inspects effective markings, which only exist post-INSERT).
	if err := s.Cedar.CheckMediaItem(ctx, in.Claims, cedarauthzlocal.ActionItemWrite(), row, set); err != nil {
		return nil, err
	}
	uploadTx := ""
	if row.TransactionRID != "" {
		uploadTx = row.TransactionRID
	}
	event := audittrail.NewMediaItemUploaded(
		row.RID, row.MediaSetRID, set.ProjectRID,
		cedarauthzlocal.EffectiveItemMarkings(set, row),
		row.Path, row.MimeType, row.SizeBytes, row.SHA256, uploadTx,
	)
	if err := s.EmitAudit(ctx, tx, event, in.AuditCtx); err != nil {
		return nil, fmt.Errorf("emit upload audit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true

	ttl := s.PresignTTL
	if in.Body.ExpiresInSeconds != nil && *in.Body.ExpiresInSeconds > 0 {
		ttl = time.Duration(*in.Body.ExpiresInSeconds) * time.Second
	}
	url, err := s.Storage.PresignUpload(ctx, key, row.MimeType, ttl)
	if err != nil {
		return nil, fmt.Errorf("presign upload: %w", err)
	}
	return &PresignUploadResult{Item: row, URL: url}, nil
}

// resolveTransactionRID enforces the full TRANSACTIONAL contract on
// uploads (mirrors the Rust handler):
//
//   - body.transaction_rid is required.
//   - the transaction must belong to the same media set.
//   - it must live on the same branch as the upload.
//   - it must still be OPEN.
//   - the per-transaction item cap (10k) must not be reached.
func (s *Service) resolveTransactionRID(ctx context.Context, set *models.MediaSet, branch string, body models.PresignedUploadRequest) (string, error) {
	if set.TransactionPolicy != "TRANSACTIONAL" {
		return "", nil
	}
	if body.TransactionRID == nil || *body.TransactionRID == "" {
		return "", &ErrBadRequest{Msg: "transactional media set requires a transaction_rid"}
	}
	txn, err := s.Repo.GetTransaction(ctx, *body.TransactionRID)
	if err != nil {
		return "", err
	}
	if txn == nil || txn.MediaSetRID != set.RID {
		return "", &ErrNotFound{What: "transaction", ID: *body.TransactionRID}
	}
	if txn.Branch != branch {
		return "", &ErrBadRequest{Msg: fmt.Sprintf(
			"transaction `%s` is on branch `%s`, not `%s`", txn.RID, txn.Branch, branch)}
	}
	if models.TransactionState(txn.State).IsTerminal() {
		return "", &ErrBadRequest{Msg: fmt.Sprintf(
			"transaction `%s` is already in terminal state `%s`", txn.RID, txn.State)}
	}
	live, err := s.Repo.CountTransactionLiveItems(ctx, txn.RID)
	if err != nil {
		return "", err
	}
	if live >= MaxItemsPerTransaction {
		return "", &ErrBadRequest{Msg: fmt.Sprintf(
			"transaction `%s` already has %d items (cap %d)",
			txn.RID, live, MaxItemsPerTransaction)}
	}
	return txn.RID, nil
}

// PresignDownloadInput captures what the download op needs.
type PresignDownloadInput struct {
	ItemRID          string
	ExpiresInSeconds *uint64
	Claims           *authmw.Claims
	AuditCtx         audittrail.AuditContext
}

// PresignDownloadResult bundles the row + URL.
type PresignDownloadResult struct {
	Item *models.MediaItem
	URL  *storage.PresignedURL
}

// PresignDownload runs `media_item::read`, mints a GET URL, and emits
// the download audit envelope. Virtual sets currently fall back to
// the persisted storage_uri (the Rust impl talks to connector-management;
// that wiring will land alongside that service's port).
func (s *Service) PresignDownload(ctx context.Context, in PresignDownloadInput) (*PresignDownloadResult, error) {
	item, err := s.Repo.GetMediaItem(ctx, in.ItemRID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, &ErrNotFound{What: "media item", ID: in.ItemRID}
	}
	parent, err := s.Repo.GetMediaSet(ctx, item.MediaSetRID)
	if err != nil {
		return nil, err
	}
	if parent == nil {
		return nil, &ErrNotFound{What: "media set", ID: item.MediaSetRID}
	}
	if err := s.Cedar.CheckMediaItem(ctx, in.Claims, cedarauthzlocal.ActionItemRead(), item, parent); err != nil {
		return nil, err
	}

	ttl := s.PresignTTL
	if in.ExpiresInSeconds != nil && *in.ExpiresInSeconds > 0 {
		ttl = time.Duration(*in.ExpiresInSeconds) * time.Second
	}
	var url *storage.PresignedURL
	if parent.Virtual {
		// Virtual: ask connector-management-service for the source
		// endpoint when configured, fall back to storage_uri.
		if s.VirtualResolver != nil {
			url, err = s.VirtualResolver.Resolve(ctx, parent, item, ttl)
			if err != nil {
				return nil, fmt.Errorf("virtual resolver: %w", err)
			}
		}
		if url == nil {
			url = &storage.PresignedURL{URL: item.StorageURI, ExpiresAt: time.Now().Add(ttl).UTC()}
		}
	} else {
		key := mediapath.New(item.MediaSetRID, item.Branch, item.SHA256)
		url, err = s.Storage.PresignDownload(ctx, key, ttl)
		if err != nil {
			return nil, fmt.Errorf("presign download: %w", err)
		}
	}

	// Append the gateway-validated claim when a signer is wired.
	// The Rust impl always mints; here it is opt-in so tests + dev
	// loops without the signing key keep working.
	if s.PresignSigner != nil {
		effective := cedarauthzlocal.EffectiveItemMarkings(parent, item)
		claim, err := s.PresignSigner.Sign(in.Claims.Sub.String(), item.RID, effective, ttl)
		if err != nil {
			return nil, fmt.Errorf("mint presign claim: %w", err)
		}
		separator := "?"
		if strings.Contains(url.URL, "?") {
			separator = "&"
		}
		url.URL = url.URL + separator + "claim=" + claim
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
	event := audittrail.NewMediaItemDownloaded(
		item.RID, item.MediaSetRID, parent.ProjectRID,
		cedarauthzlocal.EffectiveItemMarkings(parent, item),
		item.SizeBytes, uint64(ttl/time.Second),
	)
	if err := s.EmitAudit(ctx, tx, event, in.AuditCtx); err != nil {
		return nil, fmt.Errorf("emit download audit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return &PresignDownloadResult{Item: item, URL: url}, nil
}

// ListInput captures what the list op needs.
type ListInput struct {
	MediaSetRID string
	Branch      string
	Prefix      *string
	Cursor      *string
	Limit       int
	Claims      *authmw.Claims
}

// List runs `media_set::view` on the parent, fetches the page, and
// per-item filters to entries the caller can read. The filtering
// guarantee mirrors Foundry's "fast partial list rather than a hard
// 403" contract.
func (s *Service) List(ctx context.Context, in ListInput) ([]models.MediaItem, error) {
	parent, err := s.Repo.GetMediaSet(ctx, in.MediaSetRID)
	if err != nil {
		return nil, err
	}
	if parent == nil {
		return nil, &ErrNotFound{What: "media set", ID: in.MediaSetRID}
	}
	if err := s.Cedar.CheckMediaSet(ctx, in.Claims, cedarauthzlocal.ActionView(), parent); err != nil {
		return nil, err
	}
	rows, err := s.Repo.ListMediaItems(ctx, repo.ListMediaItemsParams{
		MediaSetRID: in.MediaSetRID,
		Branch:      in.Branch,
		PathPrefix:  in.Prefix,
		Cursor:      in.Cursor,
		Limit:       in.Limit,
	})
	if err != nil {
		return nil, err
	}
	visible := make([]models.MediaItem, 0, len(rows))
	for i := range rows {
		err := s.Cedar.CheckMediaItem(ctx, in.Claims, cedarauthzlocal.ActionItemRead(), &rows[i], parent)
		if err == nil {
			visible = append(visible, rows[i])
		}
	}
	return visible, nil
}

// Get returns a single item subject to read clearance.
func (s *Service) Get(ctx context.Context, claims *authmw.Claims, itemRID string) (*models.MediaItem, error) {
	item, err := s.Repo.GetMediaItem(ctx, itemRID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, &ErrNotFound{What: "media item", ID: itemRID}
	}
	parent, err := s.Repo.GetMediaSet(ctx, item.MediaSetRID)
	if err != nil {
		return nil, err
	}
	if parent == nil {
		return nil, &ErrNotFound{What: "media set", ID: item.MediaSetRID}
	}
	if err := s.Cedar.CheckMediaItem(ctx, claims, cedarauthzlocal.ActionItemRead(), item, parent); err != nil {
		return nil, err
	}
	return item, nil
}

// DeleteInput captures the delete op shape.
type DeleteInput struct {
	ItemRID  string
	Claims   *authmw.Claims
	AuditCtx audittrail.AuditContext
}

// Delete soft-deletes the item, emits the delete audit, and best-
// effort removes the byte payload. Idempotent: a second call is a
// no-op (no audit re-emit, no error).
func (s *Service) Delete(ctx context.Context, in DeleteInput) error {
	item, err := s.Repo.GetMediaItemFull(ctx, in.ItemRID)
	if err != nil {
		return err
	}
	if item == nil {
		return &ErrNotFound{What: "media item", ID: in.ItemRID}
	}
	if item.DeletedAt != nil {
		// Already deleted — idempotent no-op (Cedar gate not
		// enforced on a deleted resource).
		return nil
	}
	parent, err := s.Repo.GetMediaSet(ctx, item.MediaSetRID)
	if err != nil {
		return err
	}
	if parent == nil {
		return &ErrNotFound{What: "media set", ID: item.MediaSetRID}
	}
	if err := s.Cedar.CheckMediaItem(ctx, in.Claims, cedarauthzlocal.ActionItemDelete(), item, parent); err != nil {
		return err
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
	deleted, err := s.Repo.SoftDeleteMediaItem(ctx, tx, in.ItemRID)
	if err != nil {
		return err
	}
	if deleted {
		event := audittrail.NewMediaItemDeleted(
			item.RID, item.MediaSetRID, parent.ProjectRID,
			cedarauthzlocal.EffectiveItemMarkings(parent, item),
			item.SizeBytes,
		)
		if err := s.EmitAudit(ctx, tx, event, in.AuditCtx); err != nil {
			return fmt.Errorf("emit delete audit: %w", err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true

	if deleted && item.SHA256 != "" {
		// Best-effort byte cleanup. The metadata row stays for audit.
		key := mediapath.New(item.MediaSetRID, item.Branch, item.SHA256)
		_ = s.Storage.Delete(ctx, key)
	}
	return nil
}

// RegisterVirtualInput captures the virtual-item registration shape.
type RegisterVirtualInput struct {
	MediaSetRID string
	Body        models.RegisterVirtualItemRequest
	Claims      *authmw.Claims
	AuditCtx    audittrail.AuditContext
}

// RegisterVirtual mints a media-item row that points at an external
// source. Virtual sets only — bytes never enter Foundry storage.
func (s *Service) RegisterVirtual(ctx context.Context, in RegisterVirtualInput) (*models.MediaItem, error) {
	if strings.TrimSpace(in.Body.PhysicalPath) == "" || strings.TrimSpace(in.Body.ItemPath) == "" {
		return nil, &ErrBadRequest{Msg: "physical_path and item_path are required"}
	}
	set, err := s.Repo.GetMediaSet(ctx, in.MediaSetRID)
	if err != nil {
		return nil, err
	}
	if set == nil {
		return nil, &ErrNotFound{What: "media set", ID: in.MediaSetRID}
	}
	if !set.Virtual {
		return nil, &ErrBadRequest{Msg: "register-virtual-item is only valid on virtual media sets"}
	}
	if err := s.Cedar.CheckMediaSet(ctx, in.Claims, cedarauthzlocal.ActionManage(), set); err != nil {
		return nil, err
	}

	branch := "main"
	if in.Body.Branch != nil && *in.Body.Branch != "" {
		branch = *in.Body.Branch
	}
	mime := ""
	if in.Body.MimeType != nil {
		mime = *in.Body.MimeType
	}
	size := int64(0)
	if in.Body.SizeBytes != nil {
		size = *in.Body.SizeBytes
	}
	sha := ""
	if in.Body.SHA256 != nil {
		sha = *in.Body.SHA256
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
	dedup, err := s.Repo.SoftDeletePreviousAtPath(ctx, tx, in.MediaSetRID, branch, in.Body.ItemPath)
	if err != nil {
		return nil, err
	}
	row, err := s.Repo.CreateMediaItem(ctx, tx, repo.CreateMediaItemParams{
		RID:              repo.NewMediaItemRID(),
		MediaSetRID:      in.MediaSetRID,
		Branch:           branch,
		TransactionRID:   "",
		Path:             in.Body.ItemPath,
		MimeType:         mime,
		SizeBytes:        size,
		SHA256:           sha,
		Metadata:         json.RawMessage(`{"virtual": true}`),
		StorageURI:       in.Body.PhysicalPath,
		DeduplicatedFrom: dedup,
		RetentionSeconds: set.RetentionSeconds,
	})
	if err != nil {
		return nil, err
	}
	if err := s.Cedar.CheckMediaItem(ctx, in.Claims, cedarauthzlocal.ActionItemWrite(), row, set); err != nil {
		return nil, err
	}
	event := audittrail.NewVirtualMediaItemRegistered(
		row.RID, row.MediaSetRID, set.ProjectRID,
		cedarauthzlocal.EffectiveItemMarkings(set, row),
		in.Body.PhysicalPath, in.Body.ItemPath,
	)
	if err := s.EmitAudit(ctx, tx, event, in.AuditCtx); err != nil {
		return nil, fmt.Errorf("emit virtual register audit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return row, nil
}

// PatchMarkingsInput captures the per-item markings override.
type PatchMarkingsInput struct {
	ItemRID  string
	Markings []string
	Claims   *authmw.Claims
	AuditCtx audittrail.AuditContext
}

// PatchMarkings replaces the row's markings array. Only operators
// with `media_set::manage` on the parent can tighten or relax the
// override (mirrors PATCH /media-sets/{rid}/markings spec).
func (s *Service) PatchMarkings(ctx context.Context, in PatchMarkingsInput) (*models.MediaItem, error) {
	item, err := s.Repo.GetMediaItem(ctx, in.ItemRID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, &ErrNotFound{What: "media item", ID: in.ItemRID}
	}
	parent, err := s.Repo.GetMediaSet(ctx, item.MediaSetRID)
	if err != nil {
		return nil, err
	}
	if parent == nil {
		return nil, &ErrNotFound{What: "media set", ID: item.MediaSetRID}
	}
	if err := s.Cedar.CheckMediaSet(ctx, in.Claims, cedarauthzlocal.ActionManage(), parent); err != nil {
		return nil, err
	}
	previous := make([]string, 0, len(item.Markings))
	for _, m := range item.Markings {
		previous = append(previous, strings.ToLower(m))
	}
	normalised := repo.NormalizeMarkings(in.Markings)

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
	row, err := s.Repo.PatchMediaItemMarkings(ctx, tx, in.ItemRID, normalised)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, &ErrNotFound{What: "media item", ID: in.ItemRID}
	}
	event := audittrail.NewMediaItemMarkingOverridden(
		row.RID, row.MediaSetRID, parent.ProjectRID,
		cedarauthzlocal.EffectiveItemMarkings(parent, row),
		previous,
	)
	if err := s.EmitAudit(ctx, tx, event, in.AuditCtx); err != nil {
		return nil, fmt.Errorf("emit markings audit: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	committed = true
	return row, nil
}

