// branch.go hosts the HTTP handlers for the /api/v1/global-branches
// surface. Each lifecycle endpoint that mutates state opens a pgx.Tx
// so the SQL write and the audit-trail outbox emit commit atomically
// (ADR-0022).
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	audittrail "github.com/openfoundry/openfoundry-go/libs/audit-trail"
	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"

	"github.com/openfoundry/openfoundry-go/services/global-branch-service/internal/domain"
	"github.com/openfoundry/openfoundry-go/services/global-branch-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/global-branch-service/internal/repo"
)

// Audit event kinds emitted by Milestone A. The constants are
// declared here (not in libs/audit-trail) so the audit-trail variant
// catalog stays restricted to the kinds the audit-sink + Iceberg
// schema actually recognise. Cross-service consumers that need to
// filter on these will key off the string value, not the typed
// constant.
const (
	auditKindBranchCreated        = audittrail.EventKind("global_branch.created")
	auditKindBranchUpdated        = audittrail.EventKind("global_branch.updated")
	auditKindBranchAbandoned      = audittrail.EventKind("global_branch.abandoned")
	auditKindBranchMerged         = audittrail.EventKind("global_branch.merged")
	auditKindParticipationAdded   = audittrail.EventKind("global_branch.participation_added")
	auditKindParticipationRemoved = audittrail.EventKind("global_branch.participation_removed")
)

// SourceService labels audit events emitted by this binary.
const SourceService = "global-branch-service"

// Repository is the subset of *repo.Repo the handler layer needs.
// Pulled out as an interface so the unit tests in branch_test.go can
// drive the handlers with an in-memory fake.
type Repository interface {
	BeginTx(ctx context.Context) (pgx.Tx, error)

	CreateBranch(ctx context.Context, tx pgx.Tx, b *domain.GlobalBranch) (*domain.GlobalBranch, error)
	GetBranch(ctx context.Context, tenantID, id uuid.UUID) (*domain.GlobalBranch, error)
	GetBranchTx(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID) (*domain.GlobalBranch, error)
	ListBranches(ctx context.Context, f repo.ListFilter) ([]domain.GlobalBranch, error)
	UpdateMetadata(ctx context.Context, tenantID, id uuid.UUID, p repo.UpdateMetadataParams) (*domain.GlobalBranch, error)
	SetStatus(ctx context.Context, tx pgx.Tx, tenantID, id uuid.UUID, status domain.BranchStatus, mergedBy *uuid.UUID, mergedAt *time.Time) (*domain.GlobalBranch, error)

	AddParticipation(ctx context.Context, tx pgx.Tx, p *domain.Participation) (*domain.Participation, error)
	ListParticipations(ctx context.Context, branchID uuid.UUID) ([]domain.Participation, error)
	ListParticipationsTx(ctx context.Context, tx pgx.Tx, branchID uuid.UUID) ([]domain.Participation, error)
	RemoveParticipation(ctx context.Context, tx pgx.Tx, branchID uuid.UUID, service string) (bool, error)
	MarkAllParticipationsMerged(ctx context.Context, tx pgx.Tx, branchID uuid.UUID) (int64, error)
}

// AuditEmitter mirrors libs/audit-trail.EmitToOutbox. Held as a field
// so tests can replace it with a recorder.
type AuditEmitter func(ctx context.Context, tx pgx.Tx, event audittrail.AuditEvent, auditCtx audittrail.AuditContext) error

// Handlers wires the dependencies every endpoint needs.
type Handlers struct {
	Repo  Repository
	Emit  AuditEmitter
	Clock func() time.Time
}

// NewHandlers constructs a Handlers with the canonical audit emitter
// and the real wall clock.
func NewHandlers(r Repository) *Handlers {
	return &Handlers{Repo: r, Emit: audittrail.EmitToOutbox, Clock: func() time.Time { return time.Now().UTC() }}
}

// ── HTTP entry points ──────────────────────────────────────────────

// CreateBranch — POST /api/v1/global-branches
func (h *Handlers) CreateBranch(w http.ResponseWriter, r *http.Request) {
	caller, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	var body models.CreateBranchRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}

	branch := &domain.GlobalBranch{
		TenantID:    tenantID,
		Name:        body.Name,
		BaseRef:     body.BaseRef,
		Description: body.Description,
		CreatedBy:   caller.Sub,
		Status:      domain.StatusOpen,
	}
	if err := branch.ValidateNew(); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err.Error())
		return
	}

	tx, err := h.Repo.BeginTx(r.Context())
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(r.Context())
		}
	}()

	created, err := h.Repo.CreateBranch(r.Context(), tx, branch)
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	event := audittrail.AuditEvent{
		Kind:        auditKindBranchCreated,
		ResourceRID: created.ID.String(),
		ProjectRID:  created.TenantID.String(),
		Name:        created.Name,
		Branch:      created.BaseRef,
	}
	if err := h.Emit(r.Context(), tx, event, auditCtxFromRequest(caller, r)); err != nil {
		slog.Error("emit branch.created", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "audit emit failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	committed = true

	writeJSON(w, http.StatusCreated, models.FromDomain(created, nil))
}

// ListBranches — GET /api/v1/global-branches?status=open
func (h *Handlers) ListBranches(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	filter := repo.ListFilter{TenantID: tenantID}
	if s := strings.TrimSpace(r.URL.Query().Get("status")); s != "" {
		status := domain.BranchStatus(s)
		if !status.IsValid() {
			writeJSONErr(w, http.StatusBadRequest, "invalid status filter")
			return
		}
		filter.Status = status
	}
	rows, err := h.Repo.ListBranches(r.Context(), filter)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	items := make([]models.BranchResponse, 0, len(rows))
	for i := range rows {
		// Build the participating-services list per row. With Milestone A
		// list sizes (≤500), the N+1 reads are acceptable; a B-milestone
		// follow-up should switch to a single grouped query.
		parts, err := h.Repo.ListParticipations(r.Context(), rows[i].ID)
		if err != nil {
			writeJSONErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		items = append(items, models.FromDomain(&rows[i], serviceNames(parts)))
	}
	writeJSON(w, http.StatusOK, models.ListResponse[models.BranchResponse]{Items: items})
}

// GetBranch — GET /api/v1/global-branches/{id}
func (h *Handlers) GetBranch(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	branch, err := h.Repo.GetBranch(r.Context(), tenantID, id)
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	parts, err := h.Repo.ListParticipations(r.Context(), id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, models.FromDomain(branch, serviceNames(parts)))
}

// UpdateBranch — PATCH /api/v1/global-branches/{id}
func (h *Handlers) UpdateBranch(w http.ResponseWriter, r *http.Request) {
	caller, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var body models.UpdateBranchRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	updated, err := h.Repo.UpdateMetadata(r.Context(), tenantID, id, repo.UpdateMetadataParams{
		Name:        body.Name,
		Description: body.Description,
	})
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	parts, err := h.Repo.ListParticipations(r.Context(), id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	// PATCH does not emit an audit event in Milestone A — the metadata
	// fields it touches (name, description) are display-only. When B
	// adds rename-on-merge semantics this should grow a global_branch.updated
	// emit guarded by a status check.
	_ = caller
	writeJSON(w, http.StatusOK, models.FromDomain(updated, serviceNames(parts)))
}

// AbandonBranch — POST /api/v1/global-branches/{id}/abandon
func (h *Handlers) AbandonBranch(w http.ResponseWriter, r *http.Request) {
	caller, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	tx, err := h.Repo.BeginTx(r.Context())
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(r.Context())
		}
	}()
	current, err := h.Repo.GetBranchTx(r.Context(), tx, tenantID, id)
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	if current.Status.IsTerminal() {
		writeJSONErr(w, http.StatusConflict, domain.ErrBranchClosed.Error())
		return
	}
	updated, err := h.Repo.SetStatus(r.Context(), tx, tenantID, id, domain.StatusAbandoned, nil, nil)
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	event := audittrail.AuditEvent{
		Kind:        auditKindBranchAbandoned,
		ResourceRID: updated.ID.String(),
		ProjectRID:  updated.TenantID.String(),
		Name:        updated.Name,
	}
	if err := h.Emit(r.Context(), tx, event, auditCtxFromRequest(caller, r)); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "audit emit failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	committed = true

	parts, err := h.Repo.ListParticipations(r.Context(), id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, models.FromDomain(updated, serviceNames(parts)))
}

// MergeBranch — POST /api/v1/global-branches/{id}/merge
//
// Milestone A semantics: flip every non-merged participation to
// `merged` and stamp the branch as merged. A B-milestone iteration
// will replace the in-database flip with a real coordinator that
// reaches out to each participating service.
func (h *Handlers) MergeBranch(w http.ResponseWriter, r *http.Request) {
	caller, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	// Body is optional; decode-failure is silently ignored so callers
	// can send an empty body. Strategy != "coordinated" is rejected so
	// future strategies must opt in explicitly.
	var body models.MergeBranchRequest
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.Strategy != "" && body.Strategy != "coordinated" {
		writeJSONErr(w, http.StatusBadRequest, "only the 'coordinated' merge strategy is supported")
		return
	}

	tx, err := h.Repo.BeginTx(r.Context())
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(r.Context())
		}
	}()
	current, err := h.Repo.GetBranchTx(r.Context(), tx, tenantID, id)
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	parts, err := h.Repo.ListParticipationsTx(r.Context(), tx, id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := current.CanMerge(parts); err != nil {
		writeRepoErr(w, err)
		return
	}
	if _, err := h.Repo.MarkAllParticipationsMerged(r.Context(), tx, id); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	now := h.Clock()
	mergedBy := caller.Sub
	updated, err := h.Repo.SetStatus(r.Context(), tx, tenantID, id, domain.StatusMerged, &mergedBy, &now)
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	event := audittrail.AuditEvent{
		Kind:        auditKindBranchMerged,
		ResourceRID: updated.ID.String(),
		ProjectRID:  updated.TenantID.String(),
		Name:        updated.Name,
		Branch:      updated.BaseRef,
	}
	if err := h.Emit(r.Context(), tx, event, auditCtxFromRequest(caller, r)); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "audit emit failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	committed = true

	merged, err := h.Repo.ListParticipations(r.Context(), id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, models.FromDomain(updated, serviceNames(merged)))
}

// AddParticipant — POST /api/v1/global-branches/{id}/participants
func (h *Handlers) AddParticipant(w http.ResponseWriter, r *http.Request) {
	caller, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	var body models.AddParticipantRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid body")
		return
	}
	service := strings.TrimSpace(body.ServiceName)
	localRef := strings.TrimSpace(body.LocalBranchRef)
	if service == "" || localRef == "" {
		writeJSONErr(w, http.StatusBadRequest, "service_name and local_branch_ref are required")
		return
	}

	tx, err := h.Repo.BeginTx(r.Context())
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(r.Context())
		}
	}()
	branch, err := h.Repo.GetBranchTx(r.Context(), tx, tenantID, id)
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	if err := branch.CanAcceptParticipation(); err != nil {
		writeRepoErr(w, err)
		return
	}
	added, err := h.Repo.AddParticipation(r.Context(), tx, &domain.Participation{
		GlobalBranchID: id,
		ServiceName:    service,
		LocalBranchRef: localRef,
		Status:         domain.ParticipationPending,
	})
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	event := audittrail.AuditEvent{
		Kind:        auditKindParticipationAdded,
		ResourceRID: id.String(),
		ProjectRID:  tenantID.String(),
		Name:        branch.Name,
		Branch:      added.LocalBranchRef,
	}
	if err := h.Emit(r.Context(), tx, event, auditCtxFromRequest(caller, r)); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "audit emit failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	committed = true

	writeJSON(w, http.StatusCreated, added)
}

// RemoveParticipant — DELETE /api/v1/global-branches/{id}/participants/{service}
func (h *Handlers) RemoveParticipant(w http.ResponseWriter, r *http.Request) {
	caller, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	service := strings.TrimSpace(chi.URLParam(r, "service"))
	if service == "" {
		writeJSONErr(w, http.StatusBadRequest, "service required")
		return
	}

	tx, err := h.Repo.BeginTx(r.Context())
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(r.Context())
		}
	}()
	branch, err := h.Repo.GetBranchTx(r.Context(), tx, tenantID, id)
	if err != nil {
		writeRepoErr(w, err)
		return
	}
	removed, err := h.Repo.RemoveParticipation(r.Context(), tx, id, service)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !removed {
		writeJSONErr(w, http.StatusNotFound, "participation not found")
		return
	}
	event := audittrail.AuditEvent{
		Kind:        auditKindParticipationRemoved,
		ResourceRID: id.String(),
		ProjectRID:  tenantID.String(),
		Name:        branch.Name,
		Branch:      service,
	}
	if err := h.Emit(r.Context(), tx, event, auditCtxFromRequest(caller, r)); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "audit emit failed")
		return
	}
	if err := tx.Commit(r.Context()); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	committed = true

	w.WriteHeader(http.StatusNoContent)
}

// ListParticipants — GET /api/v1/global-branches/{id}/participants
func (h *Handlers) ListParticipants(w http.ResponseWriter, r *http.Request) {
	_, tenantID, ok := h.requireTenant(w, r)
	if !ok {
		return
	}
	id, ok := parseID(w, r)
	if !ok {
		return
	}
	if _, err := h.Repo.GetBranch(r.Context(), tenantID, id); err != nil {
		writeRepoErr(w, err)
		return
	}
	parts, err := h.Repo.ListParticipations(r.Context(), id)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, models.ListResponse[domain.Participation]{Items: parts})
}

// ── helpers ─────────────────────────────────────────────────────────

// requireTenant pulls the authenticated caller and tenant from the
// request context. Responds with 401/403 directly and returns ok=false
// so the calling handler can early-return without nested ifs.
func (h *Handlers) requireTenant(w http.ResponseWriter, r *http.Request) (*authmw.Claims, uuid.UUID, bool) {
	caller, ok := authmw.FromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusUnauthorized, "authentication required")
		return nil, uuid.Nil, false
	}
	tenant, ok := authmw.TenantFromContext(r.Context())
	if !ok {
		writeJSONErr(w, http.StatusForbidden, "tenant scope required")
		return nil, uuid.Nil, false
	}
	return caller, tenant, true
}

func parseID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	raw := strings.TrimSpace(chi.URLParam(r, "id"))
	id, err := uuid.Parse(raw)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid branch id")
		return uuid.Nil, false
	}
	return id, true
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if body == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(body)
}

func writeJSONErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

func writeRepoErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, domain.ErrBranchNotFound):
		writeJSONErr(w, http.StatusNotFound, "branch not found")
	case errors.Is(err, domain.ErrBranchClosed):
		writeJSONErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, domain.ErrParticipationExists):
		writeJSONErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, domain.ErrCannotMergeWithConflicts):
		writeJSONErr(w, http.StatusConflict, err.Error())
	case errors.Is(err, domain.ErrInvalidStatus):
		writeJSONErr(w, http.StatusUnprocessableEntity, err.Error())
	default:
		var nameConflict *repo.ErrBranchNameConflict
		if errors.As(err, &nameConflict) {
			writeJSONErr(w, http.StatusConflict, nameConflict.Error())
			return
		}
		slog.Error("global-branch handler", slog.String("error", err.Error()))
		writeJSONErr(w, http.StatusInternalServerError, "internal error")
	}
}

func auditCtxFromRequest(claims *authmw.Claims, r *http.Request) audittrail.AuditContext {
	requestID := strings.TrimSpace(r.Header.Get("X-Request-Id"))
	if requestID == "" {
		requestID = uuid.New().String()
	}
	return audittrail.AuditContext{
		ActorID:       claims.Sub.String(),
		IP:            clientIP(r),
		UserAgent:     r.Header.Get("User-Agent"),
		RequestID:     requestID,
		CorrelationID: r.Header.Get("X-Correlation-Id"),
		SourceService: SourceService,
	}
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if first := strings.SplitN(xff, ",", 2)[0]; strings.TrimSpace(first) != "" {
			return strings.TrimSpace(first)
		}
	}
	return r.Header.Get("X-Real-Ip")
}

func serviceNames(parts []domain.Participation) []string {
	names := make([]string, 0, len(parts))
	for _, p := range parts {
		names = append(names, p.ServiceName)
	}
	return names
}
