// Package accesspatterns hosts the operation layer for media-set
// access patterns. Mirrors libs/services/media-sets-service/src/handlers/
// access_patterns.rs:
//
//   - register / list / get / per-kind lookup (CRUD)
//   - run: PERSIST / CACHE_TTL cache lookup → worker HTTP call →
//     compute-seconds charged via libs/observability/costmodel +
//     mirrored into the media_compute_seconds_total Prometheus counter
//     → audit envelope `media_set.access_pattern_invoked` enqueued via
//     libs/audit-trail+outbox in the same Postgres transaction as the
//     ledger insert (ADR-0022 atomicity).
//
// The HTTP transport sits behind transformclient.Client — easy to fake
// in tests via the Worker interface below.
package accesspatterns

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	audittrail "github.com/openfoundry/openfoundry-go/libs/audit-trail"
	"github.com/openfoundry/openfoundry-go/libs/observability/costmodel"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/metrics"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/repo"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/transformclient"
)

// Worker is the subset of transformclient.Client that the service
// depends on. The interface lets tests inject a fake without spinning
// up an httptest server.
type Worker interface {
	Transform(ctx context.Context, req transformclient.TransformRequest) (*transformclient.TransformResponse, error)
}

// Repository is the subset of *repo.Repo the service consumes. Same
// rationale as Worker — keeps the service layer test-friendly.
type Repository interface {
	GetMediaSet(ctx context.Context, rid string) (*models.MediaSet, error)
	GetMediaItem(ctx context.Context, rid string) (*models.MediaItem, error)
	GetAccessPattern(ctx context.Context, id string) (*models.AccessPattern, error)
	GetAccessPatternByKind(ctx context.Context, mediaSetRID, kind string) (*models.AccessPattern, error)
	ListAccessPatterns(ctx context.Context, mediaSetRID string) ([]models.AccessPattern, error)
	CreateAccessPattern(ctx context.Context, p repo.CreateAccessPatternParams) (*models.AccessPattern, error)
	LookupCachedOutput(ctx context.Context, patternID, itemRID, paramsHash string) (*repo.CachedOutput, error)
	WriteCacheRow(ctx context.Context, patternID, itemRID, paramsHash, storageURI, outputMime string, bytes int64, expiresAt *time.Time) error
	InsertInvocation(ctx context.Context, tx pgx.Tx, e repo.LedgerEntry) error
	BeginTx(ctx context.Context) (pgx.Tx, error)
}

// AuditEmitter is the function shape libs/audit-trail.EmitToOutbox
// satisfies. Threaded through Service so tests can inject a fake
// without spinning up Postgres.
type AuditEmitter func(ctx context.Context, tx pgx.Tx, event audittrail.AuditEvent, auditCtx audittrail.AuditContext) error

// Service is the operation layer. Wired to a Repository + Worker +
// Prometheus families at boot.
type Service struct {
	Repo    Repository
	Worker  Worker
	Metrics *metrics.Metrics
	// EmitAudit defaults to audittrail.EmitToOutbox in production;
	// tests inject a no-op or a recording fake.
	EmitAudit AuditEmitter
}

// New builds a Service wired to libs/audit-trail's outbox emitter.
func New(r Repository, w Worker, m *metrics.Metrics) *Service {
	return &Service{Repo: r, Worker: w, Metrics: m, EmitAudit: audittrail.EmitToOutbox}
}

// ── Errors -------------------------------------------------------

// ErrBadRequest signals a 400-class failure (validation / FK miss /
// item-set mismatch). The HTTP layer maps it to 400.
type ErrBadRequest struct{ Msg string }

func (e *ErrBadRequest) Error() string { return e.Msg }

// ErrNotFound signals a missing pattern / item / set. HTTP → 404.
type ErrNotFound struct{ What, ID string }

func (e *ErrNotFound) Error() string { return fmt.Sprintf("%s `%s` not found", e.What, e.ID) }

// ── Register / List ---------------------------------------------

// Register validates the request and inserts a new access-pattern
// row. The audit emission happens inside the SQL transaction so a
// retry collapses on the deterministic event_id.
func (s *Service) Register(ctx context.Context, mediaSetRID string, body models.RegisterAccessPatternRequest, createdBy string, auditCtx audittrail.AuditContext) (*models.AccessPattern, error) {
	if body.Kind == "" {
		return nil, &ErrBadRequest{"kind must not be empty"}
	}
	persistence := body.Persistence
	if persistence == "" {
		persistence = models.PersistenceRecompute
	}
	if !persistence.IsValid() {
		return nil, &ErrBadRequest{"persistence must be RECOMPUTE / PERSIST / CACHE_TTL"}
	}
	if persistence == models.PersistenceCacheTTL {
		if body.TTLSeconds == nil || *body.TTLSeconds <= 0 {
			return nil, &ErrBadRequest{"ttl_seconds is required and must be > 0 when persistence = CACHE_TTL"}
		}
	}
	set, err := s.Repo.GetMediaSet(ctx, mediaSetRID)
	if err != nil {
		return nil, err
	}
	if set == nil {
		return nil, &ErrNotFound{What: "media set", ID: mediaSetRID}
	}
	params := []byte(body.Params)
	if len(params) == 0 {
		params = []byte("{}")
	}
	ttl := int64(0)
	if body.TTLSeconds != nil {
		ttl = *body.TTLSeconds
	}
	row, err := s.Repo.CreateAccessPattern(ctx, repo.CreateAccessPatternParams{
		MediaSetRID: mediaSetRID,
		Kind:        body.Kind,
		Params:      params,
		Persistence: string(persistence),
		TTLSeconds:  ttl,
		CreatedBy:   createdBy,
	})
	if err != nil {
		var dup *repo.ErrDuplicateKind
		if errors.As(err, &dup) {
			return nil, &ErrBadRequest{Msg: dup.Error()}
		}
		return nil, err
	}

	// Emit registration audit (no ledger insert — the registration
	// itself is not a billed invocation; matches the Rust caller).
	if err := s.emitAudit(ctx, set, row.Kind, row.Persistence, auditCtx); err != nil {
		return nil, fmt.Errorf("emit audit on register: %w", err)
	}
	return row, nil
}

// List returns every pattern registered on the media set.
func (s *Service) List(ctx context.Context, mediaSetRID string) ([]models.AccessPattern, error) {
	return s.Repo.ListAccessPatterns(ctx, mediaSetRID)
}

// ── Run ---------------------------------------------------------

// RunInput captures everything the run path needs. ItemBytes is
// optional — when nil, a Worker call is still made with an empty
// body and `compute_seconds = 0` (matches Rust on RECOMPUTE without
// inline bytes).
type RunInput struct {
	PatternID string
	ItemRID   string
	// ItemBytes is the source artifact when the caller has it
	// already decoded. When nil, the runtime is invoked with an
	// empty body — useful for kinds whose worker reads the source
	// from object storage rather than the request envelope.
	ItemBytes []byte
	InvokedBy string
	AuditCtx  audittrail.AuditContext
}

// Run executes the pattern, threading the worker HTTP call through
// the cache + ledger + audit layers. Mirrors the Rust
// `run_access_pattern_op` shape.
func (s *Service) Run(ctx context.Context, in RunInput) (*models.AccessPatternRunResponse, error) {
	pattern, err := s.Repo.GetAccessPattern(ctx, in.PatternID)
	if err != nil {
		return nil, err
	}
	if pattern == nil {
		return nil, &ErrNotFound{What: "access pattern", ID: in.PatternID}
	}
	item, err := s.Repo.GetMediaItem(ctx, in.ItemRID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, &ErrNotFound{What: "media item", ID: in.ItemRID}
	}
	if item.MediaSetRID != pattern.MediaSetRID {
		return nil, &ErrBadRequest{"item belongs to a different media set than the access pattern"}
	}
	parent, err := s.Repo.GetMediaSet(ctx, pattern.MediaSetRID)
	if err != nil {
		return nil, err
	}
	if parent == nil {
		return nil, &ErrNotFound{What: "media set", ID: pattern.MediaSetRID}
	}
	persistence := models.PersistencePolicy(pattern.Persistence)
	hash := repo.ParamsHash(pattern.Params)

	// ── Cache hit path (PERSIST + CACHE_TTL) ──────────────────────
	if persistence == models.PersistencePersist || persistence == models.PersistenceCacheTTL {
		cached, err := s.Repo.LookupCachedOutput(ctx, pattern.ID, in.ItemRID, hash)
		if err != nil {
			return nil, err
		}
		if cached != nil {
			if err := s.commitInvocation(ctx, parent, pattern, item, repo.LedgerEntry{
				MediaSetRID:    pattern.MediaSetRID,
				PatternID:      pattern.ID,
				Kind:           pattern.Kind,
				ItemRID:        in.ItemRID,
				InputBytes:     item.SizeBytes,
				ComputeSeconds: 0,
				Persistence:    pattern.Persistence,
				CacheHit:       true,
				InvokedBy:      in.InvokedBy,
			}, in.AuditCtx); err != nil {
				return nil, err
			}
			return &models.AccessPatternRunResponse{
				PatternID:        pattern.ID,
				Kind:             pattern.Kind,
				ItemRID:          in.ItemRID,
				Persistence:      pattern.Persistence,
				CacheHit:         true,
				ComputeSeconds:   0,
				OutputMimeType:   cached.OutputMime,
				OutputStorageURI: cached.StorageURI,
			}, nil
		}
	}

	// ── Worker call ───────────────────────────────────────────────
	workerReq := transformclient.TransformRequest{
		Kind:        pattern.Kind,
		MimeType:    item.MimeType,
		Schema:      parent.Schema,
		Params:      pattern.Params,
		BytesBase64: base64.StdEncoding.EncodeToString(in.ItemBytes),
	}
	workerResp, err := s.Worker.Transform(ctx, workerReq)
	if err != nil {
		return nil, fmt.Errorf("invoke media-transform-runtime: %w", err)
	}

	// Worker may surface NOT_IMPLEMENTED with a reason (catalog
	// returned External / NotImplemented). Pass through verbatim.
	if workerResp.Status == transformclient.StatusNotImplemented {
		reason := ""
		if workerResp.Reason != nil {
			reason = *workerResp.Reason
		}
		// Even unimplemented invocations are billed at 0 + emit
		// audit so operators can audit "tried to OCR" attempts.
		if err := s.commitInvocation(ctx, parent, pattern, item, repo.LedgerEntry{
			MediaSetRID:    pattern.MediaSetRID,
			PatternID:      pattern.ID,
			Kind:           pattern.Kind,
			ItemRID:        in.ItemRID,
			InputBytes:     item.SizeBytes,
			ComputeSeconds: 0,
			Persistence:    pattern.Persistence,
			CacheHit:       false,
			InvokedBy:      in.InvokedBy,
		}, in.AuditCtx); err != nil {
			return nil, err
		}
		return &models.AccessPatternRunResponse{
			PatternID:            pattern.ID,
			Kind:                 pattern.Kind,
			ItemRID:              in.ItemRID,
			Persistence:          pattern.Persistence,
			CacheHit:             false,
			ComputeSeconds:       0,
			OutputMimeType:       workerResp.OutputMimeType,
			NotImplementedReason: reason,
		}, nil
	}

	// ── OK path: charge compute-seconds + bump Prometheus ──────────
	compute := workerResp.ComputeSeconds
	if compute == 0 {
		// Worker returned 0 — fall back to the cost-table charge so a
		// future worker that forgets to bill still surfaces a
		// non-zero compute_seconds in the ledger and metric.
		if cs, ok := costmodel.ChargeComputeSeconds(pattern.Kind, uint64(item.SizeBytes)); ok {
			compute = cs
		}
	}
	s.Metrics.MediaComputeSecondsTotal.
		WithLabelValues(pattern.Kind, parent.Schema).
		Add(float64(compute))

	// ── PERSIST / CACHE_TTL: write derived URI + cache row ─────────
	derivedURI := ""
	if persistence == models.PersistencePersist || persistence == models.PersistenceCacheTTL {
		derivedURI = fmt.Sprintf("media-sets/%s/derived/%s/%s/%s",
			pattern.MediaSetRID, pattern.Kind, in.ItemRID, hash)
		var expiresAt *time.Time
		if persistence == models.PersistenceCacheTTL {
			t := time.Now().Add(time.Duration(pattern.TTLSeconds) * time.Second)
			expiresAt = &t
		}
		if err := s.Repo.WriteCacheRow(ctx, pattern.ID, in.ItemRID, hash,
			derivedURI, workerResp.OutputMimeType, item.SizeBytes, expiresAt); err != nil {
			return nil, err
		}
	}

	if err := s.commitInvocation(ctx, parent, pattern, item, repo.LedgerEntry{
		MediaSetRID:    pattern.MediaSetRID,
		PatternID:      pattern.ID,
		Kind:           pattern.Kind,
		ItemRID:        in.ItemRID,
		InputBytes:     item.SizeBytes,
		ComputeSeconds: int64(compute),
		Persistence:    pattern.Persistence,
		CacheHit:       false,
		InvokedBy:      in.InvokedBy,
	}, in.AuditCtx); err != nil {
		return nil, err
	}

	out := &models.AccessPatternRunResponse{
		PatternID:        pattern.ID,
		Kind:             pattern.Kind,
		ItemRID:          in.ItemRID,
		Persistence:      pattern.Persistence,
		CacheHit:         false,
		ComputeSeconds:   compute,
		OutputMimeType:   workerResp.OutputMimeType,
		OutputStorageURI: derivedURI,
	}
	if workerResp.OutputBytesBase64 != nil {
		out.OutputBytesBase64 = *workerResp.OutputBytesBase64
	}
	return out, nil
}

// RunByKind backs the per-item shortcut endpoint. Loads the pattern
// by (item.media_set_rid, kind) and delegates to Run.
func (s *Service) RunByKind(ctx context.Context, itemRID, kind string, in RunInput) (*models.AccessPatternRunResponse, error) {
	item, err := s.Repo.GetMediaItem(ctx, itemRID)
	if err != nil {
		return nil, err
	}
	if item == nil {
		return nil, &ErrNotFound{What: "media item", ID: itemRID}
	}
	pattern, err := s.Repo.GetAccessPatternByKind(ctx, item.MediaSetRID, kind)
	if err != nil {
		return nil, err
	}
	if pattern == nil {
		return nil, &ErrNotFound{
			What: "access pattern",
			ID:   fmt.Sprintf("kind=%s on media set %s", kind, item.MediaSetRID),
		}
	}
	in.PatternID = pattern.ID
	in.ItemRID = itemRID
	return s.Run(ctx, in)
}

// ── Helpers -----------------------------------------------------

// commitInvocation runs the ledger insert + audit emission inside one
// pgx transaction so they land atomically (ADR-0022).
func (s *Service) commitInvocation(ctx context.Context, parent *models.MediaSet, pattern *models.AccessPattern, item *models.MediaItem, entry repo.LedgerEntry, auditCtx audittrail.AuditContext) error {
	_ = item // kept on the signature so future callers can extend without churn
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
	if err := s.Repo.InsertInvocation(ctx, tx, entry); err != nil {
		return err
	}
	event := audittrail.NewMediaSetAccessPatternInvoked(
		parent.RID, parent.ProjectRID, parent.Markings,
		pattern.Kind, pattern.Persistence,
	)
	if err := s.EmitAudit(ctx, tx, event, auditCtx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}

// emitAudit handles the registration-time audit envelope, which lives
// in its own short-lived transaction (no ledger row to keep atomic
// with).
func (s *Service) emitAudit(ctx context.Context, set *models.MediaSet, kind, persistence string, auditCtx audittrail.AuditContext) error {
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
	event := audittrail.NewMediaSetAccessPatternInvoked(
		set.RID, set.ProjectRID, set.Markings, kind, persistence,
	)
	if err := s.EmitAudit(ctx, tx, event, auditCtx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return err
	}
	committed = true
	return nil
}
