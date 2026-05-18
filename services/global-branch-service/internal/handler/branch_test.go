// Handler unit tests. Drive the chi handlers with an in-memory fake of
// the Repository interface so the full lifecycle (auth, validation,
// status mapping, audit emit, transaction Commit/Rollback ordering)
// can be exercised without Postgres.
package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	audittrail "github.com/openfoundry/openfoundry-go/libs/audit-trail"
	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"

	"github.com/openfoundry/openfoundry-go/services/global-branch-service/internal/domain"
	"github.com/openfoundry/openfoundry-go/services/global-branch-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/global-branch-service/internal/repo"
)

// ── Test harness ───────────────────────────────────────────────────

func newTestHandlers(t *testing.T) (*Handlers, *fakeRepo, *recordingEmitter) {
	t.Helper()
	repo := newFakeRepo()
	emitter := &recordingEmitter{}
	h := &Handlers{
		Repo:  repo,
		Emit:  emitter.Emit,
		Clock: func() time.Time { return time.Unix(1_700_000_000, 0).UTC() },
	}
	return h, repo, emitter
}

// authedRequest wraps r so the auth-middleware context lookups succeed.
func authedRequest(r *http.Request, tenant, sub uuid.UUID) *http.Request {
	claims := &authmw.Claims{Sub: sub, OrgID: &tenant}
	return r.WithContext(authmw.ContextWithClaims(r.Context(), claims))
}

// withURLParam adds key/value to the chi route context on r, preserving
// any params already attached by an earlier call.
func withURLParam(r *http.Request, key, value string) *http.Request {
	rctx, _ := r.Context().Value(chi.RouteCtxKey).(*chi.Context)
	if rctx == nil {
		rctx = chi.NewRouteContext()
	}
	rctx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, rctx))
}

func decodeBranch(t *testing.T, body *bytes.Buffer) models.BranchResponse {
	t.Helper()
	var got models.BranchResponse
	if err := json.NewDecoder(body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return got
}

// ── Tests ──────────────────────────────────────────────────────────

func TestCreateBranch_HappyPath(t *testing.T) {
	t.Parallel()
	h, repo, emit := newTestHandlers(t)
	tenant, sub := uuid.New(), uuid.New()

	body := bytes.NewBufferString(`{"name":"release-q3","base_ref":"main","description":"Q3 release"}`)
	req := authedRequest(httptest.NewRequest(http.MethodPost, "/api/v1/global-branches", body), tenant, sub)
	w := httptest.NewRecorder()

	h.CreateBranch(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d want 201; body=%s", w.Code, w.Body.String())
	}
	got := decodeBranch(t, w.Body)
	if got.Name != "release-q3" || got.BaseRef != "main" || got.Status != domain.StatusOpen {
		t.Fatalf("unexpected response: %+v", got)
	}
	if got.TenantID != tenant || got.CreatedBy != sub {
		t.Fatalf("tenant/creator mismatch: %+v", got)
	}
	if len(got.ParticipatingServices) != 0 {
		t.Fatalf("expected empty participating_services, got %v", got.ParticipatingServices)
	}
	if n := len(repo.branches); n != 1 {
		t.Fatalf("repo.branches len=%d want 1", n)
	}
	if !repo.lastTxCommitted {
		t.Fatalf("expected create tx to Commit")
	}
	if len(emit.events) != 1 || emit.events[0].Kind != auditKindBranchCreated {
		t.Fatalf("audit emit: %+v", emit.events)
	}
}

func TestCreateBranch_Unauthenticated(t *testing.T) {
	t.Parallel()
	h, _, _ := newTestHandlers(t)
	body := bytes.NewBufferString(`{"name":"x","base_ref":"main"}`)
	w := httptest.NewRecorder()
	h.CreateBranch(w, httptest.NewRequest(http.MethodPost, "/", body))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d want 401", w.Code)
	}
}

func TestCreateBranch_TenantMissing(t *testing.T) {
	t.Parallel()
	h, _, _ := newTestHandlers(t)
	claims := &authmw.Claims{Sub: uuid.New()} // no OrgID
	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"name":"x","base_ref":"main"}`))
	req = req.WithContext(authmw.ContextWithClaims(req.Context(), claims))
	w := httptest.NewRecorder()
	h.CreateBranch(w, req)
	if w.Code != http.StatusForbidden {
		t.Fatalf("status=%d want 403", w.Code)
	}
}

func TestCreateBranch_InvalidJSON(t *testing.T) {
	t.Parallel()
	h, _, _ := newTestHandlers(t)
	req := authedRequest(httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{`)), uuid.New(), uuid.New())
	w := httptest.NewRecorder()
	h.CreateBranch(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400", w.Code)
	}
}

func TestCreateBranch_ValidationFails(t *testing.T) {
	t.Parallel()
	h, _, _ := newTestHandlers(t)
	// Missing base_ref → domain.ValidateNew rejects.
	req := authedRequest(httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"name":"x"}`)), uuid.New(), uuid.New())
	w := httptest.NewRecorder()
	h.CreateBranch(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400; body=%s", w.Code, w.Body.String())
	}
}

func TestCreateBranch_NameConflict(t *testing.T) {
	t.Parallel()
	h, repo, _ := newTestHandlers(t)
	tenant, sub := uuid.New(), uuid.New()

	// Pre-seed a branch with the same (tenant, name).
	repo.seedBranch(&domain.GlobalBranch{
		ID: uuid.New(), TenantID: tenant, Name: "release-q3", BaseRef: "main",
		Status: domain.StatusOpen, CreatedBy: sub, CreatedAt: time.Now().UTC(),
	})

	req := authedRequest(
		httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"name":"release-q3","base_ref":"main"}`)),
		tenant, sub,
	)
	w := httptest.NewRecorder()
	h.CreateBranch(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status=%d want 409; body=%s", w.Code, w.Body.String())
	}
}

func TestGetBranch_NotFound(t *testing.T) {
	t.Parallel()
	h, _, _ := newTestHandlers(t)
	req := authedRequest(httptest.NewRequest(http.MethodGet, "/", nil), uuid.New(), uuid.New())
	req = withURLParam(req, "id", uuid.New().String())
	w := httptest.NewRecorder()
	h.GetBranch(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d want 404", w.Code)
	}
}

func TestGetBranch_InvalidID(t *testing.T) {
	t.Parallel()
	h, _, _ := newTestHandlers(t)
	req := authedRequest(httptest.NewRequest(http.MethodGet, "/", nil), uuid.New(), uuid.New())
	req = withURLParam(req, "id", "not-a-uuid")
	w := httptest.NewRecorder()
	h.GetBranch(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400", w.Code)
	}
}

func TestListBranches_RejectsInvalidStatus(t *testing.T) {
	t.Parallel()
	h, _, _ := newTestHandlers(t)
	req := authedRequest(httptest.NewRequest(http.MethodGet, "/?status=banana", nil), uuid.New(), uuid.New())
	w := httptest.NewRecorder()
	h.ListBranches(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400", w.Code)
	}
}

func TestListBranches_FilterAndScope(t *testing.T) {
	t.Parallel()
	h, repo, _ := newTestHandlers(t)
	tenantA, tenantB := uuid.New(), uuid.New()
	sub := uuid.New()
	repo.seedBranch(&domain.GlobalBranch{ID: uuid.New(), TenantID: tenantA, Name: "a-open", BaseRef: "main", Status: domain.StatusOpen, CreatedBy: sub})
	repo.seedBranch(&domain.GlobalBranch{ID: uuid.New(), TenantID: tenantA, Name: "a-merged", BaseRef: "main", Status: domain.StatusMerged, CreatedBy: sub})
	repo.seedBranch(&domain.GlobalBranch{ID: uuid.New(), TenantID: tenantB, Name: "b-open", BaseRef: "main", Status: domain.StatusOpen, CreatedBy: sub})

	req := authedRequest(httptest.NewRequest(http.MethodGet, "/?status=open", nil), tenantA, sub)
	w := httptest.NewRecorder()
	h.ListBranches(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", w.Code)
	}
	var got models.ListResponse[models.BranchResponse]
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Items) != 1 || got.Items[0].Name != "a-open" {
		t.Fatalf("expected only a-open, got %+v", got.Items)
	}
}

func TestAbandonBranch_HappyPath(t *testing.T) {
	t.Parallel()
	h, repo, emit := newTestHandlers(t)
	tenant, sub := uuid.New(), uuid.New()
	br := &domain.GlobalBranch{
		ID: uuid.New(), TenantID: tenant, Name: "feat", BaseRef: "main",
		Status: domain.StatusOpen, CreatedBy: sub, CreatedAt: time.Now().UTC(),
	}
	repo.seedBranch(br)

	req := authedRequest(httptest.NewRequest(http.MethodPost, "/", nil), tenant, sub)
	req = withURLParam(req, "id", br.ID.String())
	w := httptest.NewRecorder()
	h.AbandonBranch(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", w.Code, w.Body.String())
	}
	got := decodeBranch(t, w.Body)
	if got.Status != domain.StatusAbandoned {
		t.Fatalf("status=%q want abandoned", got.Status)
	}
	if len(emit.events) != 1 || emit.events[0].Kind != auditKindBranchAbandoned {
		t.Fatalf("audit emit: %+v", emit.events)
	}
}

func TestAbandonBranch_AlreadyTerminal(t *testing.T) {
	t.Parallel()
	h, repo, emit := newTestHandlers(t)
	tenant, sub := uuid.New(), uuid.New()
	br := &domain.GlobalBranch{
		ID: uuid.New(), TenantID: tenant, Name: "feat", BaseRef: "main",
		Status: domain.StatusMerged, CreatedBy: sub, CreatedAt: time.Now().UTC(),
	}
	repo.seedBranch(br)

	req := authedRequest(httptest.NewRequest(http.MethodPost, "/", nil), tenant, sub)
	req = withURLParam(req, "id", br.ID.String())
	w := httptest.NewRecorder()
	h.AbandonBranch(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status=%d want 409", w.Code)
	}
	if len(emit.events) != 0 {
		t.Fatalf("expected no audit on terminal-branch rejection, got %+v", emit.events)
	}
}

func TestMergeBranch_RejectsConflictParticipation(t *testing.T) {
	t.Parallel()
	h, repo, emit := newTestHandlers(t)
	tenant, sub := uuid.New(), uuid.New()
	br := &domain.GlobalBranch{
		ID: uuid.New(), TenantID: tenant, Name: "feat", BaseRef: "main",
		Status: domain.StatusOpen, CreatedBy: sub, CreatedAt: time.Now().UTC(),
	}
	repo.seedBranch(br)
	repo.seedParticipation(&domain.Participation{
		GlobalBranchID: br.ID, ServiceName: "dataset", LocalBranchRef: "feat",
		Status: domain.ParticipationConflict,
	})

	req := authedRequest(httptest.NewRequest(http.MethodPost, "/", nil), tenant, sub)
	req = withURLParam(req, "id", br.ID.String())
	w := httptest.NewRecorder()
	h.MergeBranch(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status=%d want 409", w.Code)
	}
	if len(emit.events) != 0 {
		t.Fatalf("expected no audit, got %+v", emit.events)
	}
}

func TestMergeBranch_HappyPath(t *testing.T) {
	t.Parallel()
	h, repo, emit := newTestHandlers(t)
	tenant, sub := uuid.New(), uuid.New()
	br := &domain.GlobalBranch{
		ID: uuid.New(), TenantID: tenant, Name: "feat", BaseRef: "main",
		Status: domain.StatusOpen, CreatedBy: sub, CreatedAt: time.Now().UTC(),
	}
	repo.seedBranch(br)
	repo.seedParticipation(&domain.Participation{
		GlobalBranchID: br.ID, ServiceName: "ontology", LocalBranchRef: "feat",
		Status: domain.ParticipationActive,
	})

	req := authedRequest(httptest.NewRequest(http.MethodPost, "/", nil), tenant, sub)
	req = withURLParam(req, "id", br.ID.String())
	w := httptest.NewRecorder()
	h.MergeBranch(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", w.Code, w.Body.String())
	}
	got := decodeBranch(t, w.Body)
	if got.Status != domain.StatusMerged {
		t.Fatalf("status=%q want merged", got.Status)
	}
	if got.MergedBy == nil || *got.MergedBy != sub {
		t.Fatalf("merged_by=%v want %s", got.MergedBy, sub)
	}
	if len(emit.events) != 1 || emit.events[0].Kind != auditKindBranchMerged {
		t.Fatalf("audit emit: %+v", emit.events)
	}
	parts := repo.partsFor(br.ID)
	if len(parts) != 1 || parts[0].Status != domain.ParticipationMerged {
		t.Fatalf("expected participation flipped to merged, got %+v", parts)
	}
}

func TestMergeBranch_RejectsUnknownStrategy(t *testing.T) {
	t.Parallel()
	h, repo, _ := newTestHandlers(t)
	tenant, sub := uuid.New(), uuid.New()
	br := &domain.GlobalBranch{
		ID: uuid.New(), TenantID: tenant, Name: "feat", BaseRef: "main",
		Status: domain.StatusOpen, CreatedBy: sub, CreatedAt: time.Now().UTC(),
	}
	repo.seedBranch(br)

	req := authedRequest(httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"strategy":"three-way"}`)), tenant, sub)
	req = withURLParam(req, "id", br.ID.String())
	w := httptest.NewRecorder()
	h.MergeBranch(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400", w.Code)
	}
}

func TestAddParticipant_AndDuplicate(t *testing.T) {
	t.Parallel()
	h, repo, emit := newTestHandlers(t)
	tenant, sub := uuid.New(), uuid.New()
	br := &domain.GlobalBranch{
		ID: uuid.New(), TenantID: tenant, Name: "feat", BaseRef: "main",
		Status: domain.StatusOpen, CreatedBy: sub, CreatedAt: time.Now().UTC(),
	}
	repo.seedBranch(br)

	body := bytes.NewBufferString(`{"service_name":"dataset","local_branch_ref":"feat"}`)
	req := authedRequest(httptest.NewRequest(http.MethodPost, "/", body), tenant, sub)
	req = withURLParam(req, "id", br.ID.String())
	w := httptest.NewRecorder()
	h.AddParticipant(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("status=%d want 201; body=%s", w.Code, w.Body.String())
	}

	// Duplicate participation must 409.
	body = bytes.NewBufferString(`{"service_name":"dataset","local_branch_ref":"feat"}`)
	req = authedRequest(httptest.NewRequest(http.MethodPost, "/", body), tenant, sub)
	req = withURLParam(req, "id", br.ID.String())
	w = httptest.NewRecorder()
	h.AddParticipant(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("dup status=%d want 409", w.Code)
	}
	if len(emit.events) != 1 || emit.events[0].Kind != auditKindParticipationAdded {
		t.Fatalf("audit emit (only first add should produce one): %+v", emit.events)
	}
}

func TestAddParticipant_OnTerminalBranchFails(t *testing.T) {
	t.Parallel()
	h, repo, emit := newTestHandlers(t)
	tenant, sub := uuid.New(), uuid.New()
	br := &domain.GlobalBranch{
		ID: uuid.New(), TenantID: tenant, Name: "feat", BaseRef: "main",
		Status: domain.StatusMerged, CreatedBy: sub, CreatedAt: time.Now().UTC(),
	}
	repo.seedBranch(br)

	body := bytes.NewBufferString(`{"service_name":"dataset","local_branch_ref":"feat"}`)
	req := authedRequest(httptest.NewRequest(http.MethodPost, "/", body), tenant, sub)
	req = withURLParam(req, "id", br.ID.String())
	w := httptest.NewRecorder()
	h.AddParticipant(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("status=%d want 409", w.Code)
	}
	if len(emit.events) != 0 {
		t.Fatalf("expected no audit, got %+v", emit.events)
	}
}

func TestAddParticipant_BadBody(t *testing.T) {
	t.Parallel()
	h, _, _ := newTestHandlers(t)
	req := authedRequest(httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"service_name":"","local_branch_ref":""}`)), uuid.New(), uuid.New())
	req = withURLParam(req, "id", uuid.New().String())
	w := httptest.NewRecorder()
	h.AddParticipant(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status=%d want 400", w.Code)
	}
}

func TestRemoveParticipant_HappyPath(t *testing.T) {
	t.Parallel()
	h, repo, emit := newTestHandlers(t)
	tenant, sub := uuid.New(), uuid.New()
	br := &domain.GlobalBranch{
		ID: uuid.New(), TenantID: tenant, Name: "feat", BaseRef: "main",
		Status: domain.StatusOpen, CreatedBy: sub, CreatedAt: time.Now().UTC(),
	}
	repo.seedBranch(br)
	repo.seedParticipation(&domain.Participation{
		GlobalBranchID: br.ID, ServiceName: "dataset", LocalBranchRef: "feat",
		Status: domain.ParticipationActive,
	})

	req := authedRequest(httptest.NewRequest(http.MethodDelete, "/", nil), tenant, sub)
	req = withURLParam(req, "id", br.ID.String())
	req = withURLParam(req, "service", "dataset")
	w := httptest.NewRecorder()
	h.RemoveParticipant(w, req)
	if w.Code != http.StatusNoContent {
		t.Fatalf("status=%d want 204; body=%s", w.Code, w.Body.String())
	}
	if len(emit.events) != 1 || emit.events[0].Kind != auditKindParticipationRemoved {
		t.Fatalf("audit emit: %+v", emit.events)
	}
	if got := repo.partsFor(br.ID); len(got) != 0 {
		t.Fatalf("participation not removed: %+v", got)
	}
}

func TestRemoveParticipant_NotFound(t *testing.T) {
	t.Parallel()
	h, repo, emit := newTestHandlers(t)
	tenant, sub := uuid.New(), uuid.New()
	br := &domain.GlobalBranch{
		ID: uuid.New(), TenantID: tenant, Name: "feat", BaseRef: "main",
		Status: domain.StatusOpen, CreatedBy: sub, CreatedAt: time.Now().UTC(),
	}
	repo.seedBranch(br)

	req := authedRequest(httptest.NewRequest(http.MethodDelete, "/", nil), tenant, sub)
	req = withURLParam(req, "id", br.ID.String())
	req = withURLParam(req, "service", "unknown")
	w := httptest.NewRecorder()
	h.RemoveParticipant(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("status=%d want 404", w.Code)
	}
	if len(emit.events) != 0 {
		t.Fatalf("expected no audit, got %+v", emit.events)
	}
}

func TestUpdateBranch_HappyPath(t *testing.T) {
	t.Parallel()
	h, repo, _ := newTestHandlers(t)
	tenant, sub := uuid.New(), uuid.New()
	br := &domain.GlobalBranch{
		ID: uuid.New(), TenantID: tenant, Name: "feat", BaseRef: "main",
		Status: domain.StatusOpen, CreatedBy: sub, CreatedAt: time.Now().UTC(),
		Description: "old",
	}
	repo.seedBranch(br)

	body := bytes.NewBufferString(`{"name":"feat-renamed","description":"new"}`)
	req := authedRequest(httptest.NewRequest(http.MethodPatch, "/", body), tenant, sub)
	req = withURLParam(req, "id", br.ID.String())
	w := httptest.NewRecorder()
	h.UpdateBranch(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200; body=%s", w.Code, w.Body.String())
	}
	got := decodeBranch(t, w.Body)
	if got.Name != "feat-renamed" || got.Description != "new" {
		t.Fatalf("unexpected response: %+v", got)
	}
}

func TestListParticipants_Sorted(t *testing.T) {
	t.Parallel()
	h, repo, _ := newTestHandlers(t)
	tenant, sub := uuid.New(), uuid.New()
	br := &domain.GlobalBranch{
		ID: uuid.New(), TenantID: tenant, Name: "feat", BaseRef: "main",
		Status: domain.StatusOpen, CreatedBy: sub, CreatedAt: time.Now().UTC(),
	}
	repo.seedBranch(br)
	repo.seedParticipation(&domain.Participation{GlobalBranchID: br.ID, ServiceName: "zeta", LocalBranchRef: "feat", Status: domain.ParticipationActive})
	repo.seedParticipation(&domain.Participation{GlobalBranchID: br.ID, ServiceName: "alpha", LocalBranchRef: "feat", Status: domain.ParticipationActive})

	req := authedRequest(httptest.NewRequest(http.MethodGet, "/", nil), tenant, sub)
	req = withURLParam(req, "id", br.ID.String())
	w := httptest.NewRecorder()
	h.ListParticipants(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status=%d want 200", w.Code)
	}
	var got models.ListResponse[domain.Participation]
	if err := json.NewDecoder(w.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Items) != 2 || got.Items[0].ServiceName != "alpha" || got.Items[1].ServiceName != "zeta" {
		t.Fatalf("unexpected order: %+v", got.Items)
	}
}

func TestAuditEmitFailure_RollsBack(t *testing.T) {
	t.Parallel()
	h, repo, emit := newTestHandlers(t)
	emit.err = errors.New("simulated outbox failure")
	tenant, sub := uuid.New(), uuid.New()

	body := bytes.NewBufferString(`{"name":"x","base_ref":"main"}`)
	req := authedRequest(httptest.NewRequest(http.MethodPost, "/", body), tenant, sub)
	w := httptest.NewRecorder()
	h.CreateBranch(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d want 500", w.Code)
	}
	if repo.lastTxCommitted {
		t.Fatalf("expected tx Rollback, but it committed")
	}
	if !repo.lastTxRolledBack {
		t.Fatalf("expected tx Rollback to fire")
	}
	// The fake repo doesn't model transactional persistence — the
	// production repo does (the INSERT lives inside the pgx.Tx that
	// gets rolled back). The handler-level assertion that matters
	// here is the Rollback/Commit ordering above.
}

func TestAuditContextFromRequest_FallbackIDs(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodPost, "/", nil)
	r.Header.Set("X-Forwarded-For", "10.0.0.5, 10.0.0.6")
	r.Header.Set("User-Agent", "ua")
	claims := &authmw.Claims{Sub: uuid.New()}
	ctx := auditCtxFromRequest(claims, r)
	if ctx.IP != "10.0.0.5" {
		t.Fatalf("IP=%q want 10.0.0.5", ctx.IP)
	}
	if ctx.SourceService != SourceService {
		t.Fatalf("source=%q want %q", ctx.SourceService, SourceService)
	}
	if strings.TrimSpace(ctx.RequestID) == "" {
		t.Fatalf("RequestID fallback should not be empty")
	}
}

// ── Fakes ─────────────────────────────────────────────────────────

// recordingEmitter satisfies the AuditEmitter signature and stashes every
// event so tests can assert on emission counts and ordering.
type recordingEmitter struct {
	mu     sync.Mutex
	err    error
	events []audittrail.AuditEvent
}

func (e *recordingEmitter) Emit(_ context.Context, _ pgx.Tx, event audittrail.AuditEvent, _ audittrail.AuditContext) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.err != nil {
		return e.err
	}
	e.events = append(e.events, event)
	return nil
}

// fakeTx is a no-op pgx.Tx for handler tests. Only Commit / Rollback are
// observable; the data-plane methods (Exec / Query / QueryRow / …) are
// never called by the fake repo and panic if exercised, which surfaces
// implementation drift loudly.
type fakeTx struct {
	committed  *bool
	rolledBack *bool
}

func (t *fakeTx) Commit(_ context.Context) error {
	*t.committed = true
	return nil
}
func (t *fakeTx) Rollback(_ context.Context) error {
	*t.rolledBack = true
	return nil
}
func (t *fakeTx) Begin(_ context.Context) (pgx.Tx, error) { panic("fakeTx.Begin not supported") }
func (t *fakeTx) CopyFrom(_ context.Context, _ pgx.Identifier, _ []string, _ pgx.CopyFromSource) (int64, error) {
	panic("fakeTx.CopyFrom not supported")
}
func (t *fakeTx) SendBatch(_ context.Context, _ *pgx.Batch) pgx.BatchResults {
	panic("fakeTx.SendBatch not supported")
}
func (t *fakeTx) LargeObjects() pgx.LargeObjects { panic("fakeTx.LargeObjects not supported") }
func (t *fakeTx) Prepare(_ context.Context, _, _ string) (*pgconn.StatementDescription, error) {
	panic("fakeTx.Prepare not supported")
}
func (t *fakeTx) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	panic("fakeTx.Exec not supported")
}
func (t *fakeTx) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	panic("fakeTx.Query not supported")
}
func (t *fakeTx) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	panic("fakeTx.QueryRow not supported")
}
func (t *fakeTx) Conn() *pgx.Conn { panic("fakeTx.Conn not supported") }

// fakeRepo is the in-memory drop-in for the Repository interface used by
// the handler tests. Operations write to the maps directly; the fakeTx
// state pinned by lastTxCommitted / lastTxRolledBack lets tests assert
// that handlers commit on success and rollback on failure.
type fakeRepo struct {
	mu               sync.Mutex
	branches         map[uuid.UUID]*domain.GlobalBranch
	participations  map[uuid.UUID][]domain.Participation
	lastTxCommitted bool
	lastTxRolledBack bool
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		branches:       map[uuid.UUID]*domain.GlobalBranch{},
		participations: map[uuid.UUID][]domain.Participation{},
	}
}

func (r *fakeRepo) seedBranch(b *domain.GlobalBranch) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if b.ID == uuid.Nil {
		b.ID = uuid.New()
	}
	r.branches[b.ID] = b
}

func (r *fakeRepo) seedParticipation(p *domain.Participation) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.participations[p.GlobalBranchID] = append(r.participations[p.GlobalBranchID], *p)
}

func (r *fakeRepo) partsFor(branchID uuid.UUID) []domain.Participation {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]domain.Participation, len(r.participations[branchID]))
	copy(out, r.participations[branchID])
	return out
}

// BeginTx resets the per-tx flags and returns a fresh fakeTx. Each call
// overwrites the previous tx so tests should inspect lastTxCommitted /
// lastTxRolledBack immediately after the handler call.
func (r *fakeRepo) BeginTx(_ context.Context) (pgx.Tx, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastTxCommitted = false
	r.lastTxRolledBack = false
	return &fakeTx{committed: &r.lastTxCommitted, rolledBack: &r.lastTxRolledBack}, nil
}

func (r *fakeRepo) CreateBranch(_ context.Context, _ pgx.Tx, b *domain.GlobalBranch) (*domain.GlobalBranch, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.branches {
		if existing.TenantID == b.TenantID && existing.Name == b.Name {
			return nil, &repo.ErrBranchNameConflict{TenantID: b.TenantID, Name: b.Name}
		}
	}
	if b.ID == uuid.Nil {
		b.ID = uuid.New()
	}
	if b.CreatedAt.IsZero() {
		b.CreatedAt = time.Now().UTC()
	}
	clone := *b
	r.branches[b.ID] = &clone
	out := clone
	return &out, nil
}

func (r *fakeRepo) GetBranch(_ context.Context, tenantID, id uuid.UUID) (*domain.GlobalBranch, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.findBranchLocked(tenantID, id)
}

func (r *fakeRepo) GetBranchTx(_ context.Context, _ pgx.Tx, tenantID, id uuid.UUID) (*domain.GlobalBranch, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.findBranchLocked(tenantID, id)
}

func (r *fakeRepo) findBranchLocked(tenantID, id uuid.UUID) (*domain.GlobalBranch, error) {
	b, ok := r.branches[id]
	if !ok || b.TenantID != tenantID {
		return nil, domain.ErrBranchNotFound
	}
	clone := *b
	return &clone, nil
}

func (r *fakeRepo) ListBranches(_ context.Context, f repo.ListFilter) ([]domain.GlobalBranch, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]domain.GlobalBranch, 0, len(r.branches))
	for _, b := range r.branches {
		if b.TenantID != f.TenantID {
			continue
		}
		if f.Status != "" && b.Status != f.Status {
			continue
		}
		out = append(out, *b)
	}
	return out, nil
}

func (r *fakeRepo) UpdateMetadata(_ context.Context, tenantID, id uuid.UUID, p repo.UpdateMetadataParams) (*domain.GlobalBranch, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, err := r.findBranchLocked(tenantID, id)
	if err != nil {
		return nil, err
	}
	if p.Name != nil {
		trimmed := strings.TrimSpace(*p.Name)
		if trimmed == "" {
			return nil, errors.New("name cannot be empty")
		}
		for _, other := range r.branches {
			if other.ID != id && other.TenantID == tenantID && other.Name == trimmed {
				return nil, &repo.ErrBranchNameConflict{TenantID: tenantID, Name: trimmed}
			}
		}
		b.Name = trimmed
	}
	if p.Description != nil {
		b.Description = strings.TrimSpace(*p.Description)
	}
	r.branches[id] = b
	clone := *b
	return &clone, nil
}

func (r *fakeRepo) SetStatus(_ context.Context, _ pgx.Tx, tenantID, id uuid.UUID, status domain.BranchStatus, mergedBy *uuid.UUID, mergedAt *time.Time) (*domain.GlobalBranch, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	b, err := r.findBranchLocked(tenantID, id)
	if err != nil {
		return nil, err
	}
	b.Status = status
	if status == domain.StatusMerged {
		if mergedAt != nil {
			b.MergedAt = mergedAt
		} else {
			now := time.Now().UTC()
			b.MergedAt = &now
		}
		b.MergedBy = mergedBy
	}
	r.branches[id] = b
	clone := *b
	return &clone, nil
}

func (r *fakeRepo) AddParticipation(_ context.Context, _ pgx.Tx, p *domain.Participation) (*domain.Participation, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.participations[p.GlobalBranchID] {
		if existing.ServiceName == p.ServiceName {
			return nil, domain.ErrParticipationExists
		}
	}
	if p.Status == "" {
		p.Status = domain.ParticipationPending
	}
	r.participations[p.GlobalBranchID] = append(r.participations[p.GlobalBranchID], *p)
	out := *p
	return &out, nil
}

func (r *fakeRepo) ListParticipations(_ context.Context, branchID uuid.UUID) ([]domain.Participation, error) {
	return r.listParticipations(branchID), nil
}

func (r *fakeRepo) ListParticipationsTx(_ context.Context, _ pgx.Tx, branchID uuid.UUID) ([]domain.Participation, error) {
	return r.listParticipations(branchID), nil
}

func (r *fakeRepo) listParticipations(branchID uuid.UUID) []domain.Participation {
	r.mu.Lock()
	defer r.mu.Unlock()
	src := r.participations[branchID]
	out := make([]domain.Participation, len(src))
	copy(out, src)
	// Repo guarantees alphabetic-by-service ordering; the fake mirrors
	// that so callers don't get a different shape than production.
	sortParticipations(out)
	return out
}

func (r *fakeRepo) RemoveParticipation(_ context.Context, _ pgx.Tx, branchID uuid.UUID, service string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	parts := r.participations[branchID]
	kept := parts[:0]
	removed := false
	for _, p := range parts {
		if p.ServiceName == service && !removed {
			removed = true
			continue
		}
		kept = append(kept, p)
	}
	r.participations[branchID] = kept
	return removed, nil
}

func (r *fakeRepo) MarkAllParticipationsMerged(_ context.Context, _ pgx.Tx, branchID uuid.UUID) (int64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	parts := r.participations[branchID]
	var n int64
	for i := range parts {
		if parts[i].Status != domain.ParticipationMerged {
			parts[i].Status = domain.ParticipationMerged
			now := time.Now().UTC()
			parts[i].LastSyncedAt = &now
			n++
		}
	}
	r.participations[branchID] = parts
	return n, nil
}

func sortParticipations(s []domain.Participation) {
	for i := 1; i < len(s); i++ {
		j := i
		for j > 0 && s[j-1].ServiceName > s[j].ServiceName {
			s[j-1], s[j] = s[j], s[j-1]
			j--
		}
	}
}
