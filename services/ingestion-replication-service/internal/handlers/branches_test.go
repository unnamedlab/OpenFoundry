package handlers_test

// Stream-branch endpoint coverage for IRF-8. Mirrors the behaviour
// expected from the Rust handler: alphanumeric/'-/_/'/' name charset,
// 'main' protection, parent-belongs-to-stream gating, head-sequence
// merge semantics, idempotent archive, best-effort cold-tier bridge.
//
// All cases run against in-memory fakes so the build invariant stays
// green without a Postgres or dataset-versioning instance.

import (
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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	"github.com/openfoundry/openfoundry-go/services/ingestion-replication-service/internal/domain/streambranch"
	"github.com/openfoundry/openfoundry-go/services/ingestion-replication-service/internal/handlers"
)

// ─── fakeBranchStore ───────────────────────────────────────────────────

type fakeBranchStore struct {
	mu       sync.Mutex
	streams  map[uuid.UUID]bool
	branches map[uuid.UUID]map[string]*streambranch.StreamBranch
	errOn    string
}

func newFakeBranchStore() *fakeBranchStore {
	return &fakeBranchStore{
		streams:  map[uuid.UUID]bool{},
		branches: map[uuid.UUID]map[string]*streambranch.StreamBranch{},
	}
}

func (f *fakeBranchStore) StreamExists(_ context.Context, id uuid.UUID) (bool, error) {
	if f.errOn == "exists" {
		return false, errors.New("boom")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.streams[id], nil
}

func (f *fakeBranchStore) ParentBranchBelongsTo(_ context.Context, parentID, streamID uuid.UUID) (bool, error) {
	if f.errOn == "parent" {
		return false, errors.New("boom")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, b := range f.branches[streamID] {
		if b.ID == parentID {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeBranchStore) ListBranches(_ context.Context, streamID uuid.UUID) ([]streambranch.StreamBranch, error) {
	if f.errOn == "list" {
		return nil, errors.New("boom")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]streambranch.StreamBranch, 0)
	for _, b := range f.branches[streamID] {
		out = append(out, *b)
	}
	return out, nil
}

func (f *fakeBranchStore) GetBranchByName(_ context.Context, streamID uuid.UUID, name string) (*streambranch.StreamBranch, error) {
	if f.errOn == "get" {
		return nil, errors.New("boom")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	b := f.branches[streamID][name]
	if b == nil {
		return nil, nil
	}
	cp := *b
	return &cp, nil
}

func (f *fakeBranchStore) CreateBranch(_ context.Context, streamID uuid.UUID, name, createdBy string, parent *uuid.UUID, datasetBranchID *string, description string) (*streambranch.StreamBranch, error) {
	if f.errOn == "create" {
		return nil, errors.New("boom")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.branches[streamID] == nil {
		f.branches[streamID] = map[string]*streambranch.StreamBranch{}
	}
	if _, exists := f.branches[streamID][name]; exists {
		return nil, errors.New("duplicate branch name")
	}
	b := &streambranch.StreamBranch{
		ID:              uuid.New(),
		StreamID:        streamID,
		Name:            name,
		ParentBranchID:  parent,
		Status:          "active",
		HeadSequenceNo:  0,
		DatasetBranchID: datasetBranchID,
		Description:     description,
		CreatedBy:       createdBy,
		CreatedAt:       time.Now().UTC(),
	}
	f.branches[streamID][name] = b
	cp := *b
	return &cp, nil
}

func (f *fakeBranchStore) DeleteBranch(_ context.Context, branchID uuid.UUID) error {
	if f.errOn == "delete" {
		return errors.New("boom")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, group := range f.branches {
		for name, b := range group {
			if b.ID == branchID {
				delete(group, name)
				return nil
			}
		}
	}
	return nil
}

func (f *fakeBranchStore) MergeBranches(_ context.Context, sourceID, targetID uuid.UUID, mergedSequenceNo int64) error {
	if f.errOn == "merge" {
		return errors.New("boom")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, group := range f.branches {
		for _, b := range group {
			if b.ID == targetID {
				b.HeadSequenceNo = mergedSequenceNo
			}
			if b.ID == sourceID {
				b.Status = "merged"
			}
		}
	}
	return nil
}

func (f *fakeBranchStore) ArchiveBranch(_ context.Context, streamID, branchID uuid.UUID, name string) (*streambranch.StreamBranch, error) {
	if f.errOn == "archive" {
		return nil, errors.New("boom")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	b := f.branches[streamID][name]
	if b == nil || b.ID != branchID {
		return nil, nil
	}
	now := time.Now().UTC()
	b.Status = "archived"
	b.ArchivedAt = &now
	cp := *b
	return &cp, nil
}

// ─── fake cold tier ────────────────────────────────────────────────────

type fakeColdTier struct {
	calls    []*streambranch.StreamBranch
	accepted bool
	err      error
}

func (f *fakeColdTier) CommitCold(_ context.Context, branch *streambranch.StreamBranch, _ time.Time) (bool, error) {
	cp := *branch
	f.calls = append(f.calls, &cp)
	return f.accepted, f.err
}

type fakeMetricSink struct {
	calls map[string]uint64
}

func (f *fakeMetricSink) RecordStreamRowsArchived(branch string, rows uint64) {
	if f.calls == nil {
		f.calls = map[string]uint64{}
	}
	f.calls[branch] = rows
}

// ─── helpers ───────────────────────────────────────────────────────────

func newWriterClaims() *authmw.Claims {
	return &authmw.Claims{
		Sub:   uuid.New(),
		Email: "writer@example.com",
		Roles: []string{"data_engineer"},
	}
}

func newReaderClaims() *authmw.Claims {
	return &authmw.Claims{
		Sub:   uuid.New(),
		Email: "reader@example.com",
		Roles: []string{"viewer"},
	}
}

func branchReq(t *testing.T, method, target, body string, claims *authmw.Claims, params map[string]string) *http.Request {
	t.Helper()
	var reader = strings.NewReader(body)
	req := httptest.NewRequest(method, target, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
		req.ContentLength = int64(len(body))
	}
	rctx := chi.NewRouteContext()
	for k, v := range params {
		rctx.URLParams.Add(k, v)
	}
	ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
	if claims != nil {
		ctx = authmw.ContextWithClaims(ctx, claims)
	}
	return req.WithContext(ctx)
}

// ─── ListBranches ──────────────────────────────────────────────────────

func TestListBranchesRequiresAuth(t *testing.T) {
	t.Parallel()
	h := &handlers.BranchesHandler{Store: newFakeBranchStore()}
	req := httptest.NewRequest("GET", "/streams/x/branches", nil)
	rec := httptest.NewRecorder()
	h.ListBranches(rec, req)
	assert.Equal(t, http.StatusUnauthorized, rec.Code)
}

func TestListBranchesRejectsBadStreamID(t *testing.T) {
	t.Parallel()
	h := &handlers.BranchesHandler{Store: newFakeBranchStore()}
	req := branchReq(t, "GET", "/streams/x/branches", "", newReaderClaims(), map[string]string{"id": "not-a-uuid"})
	rec := httptest.NewRecorder()
	h.ListBranches(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestListBranchesReturnsEmptyDataArray(t *testing.T) {
	t.Parallel()
	store := newFakeBranchStore()
	streamID := uuid.New()
	store.streams[streamID] = true
	h := &handlers.BranchesHandler{Store: store}
	req := branchReq(t, "GET", "/streams/"+streamID.String()+"/branches", "", newReaderClaims(), map[string]string{"id": streamID.String()})
	rec := httptest.NewRecorder()
	h.ListBranches(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Data []streambranch.StreamBranch `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.NotNil(t, body.Data)
	assert.Len(t, body.Data, 0)
}

func TestListBranchesNotFound(t *testing.T) {
	t.Parallel()
	store := newFakeBranchStore()
	h := &handlers.BranchesHandler{Store: store}
	streamID := uuid.New()
	req := branchReq(t, "GET", "/streams/"+streamID.String()+"/branches", "", newReaderClaims(), map[string]string{"id": streamID.String()})
	rec := httptest.NewRecorder()
	h.ListBranches(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// ─── CreateBranch ──────────────────────────────────────────────────────

func TestCreateBranchRequiresWritePermission(t *testing.T) {
	t.Parallel()
	store := newFakeBranchStore()
	streamID := uuid.New()
	store.streams[streamID] = true
	h := &handlers.BranchesHandler{Store: store}
	req := branchReq(t, "POST", "/streams/"+streamID.String()+"/branches",
		`{"name":"feature-x"}`, newReaderClaims(), map[string]string{"id": streamID.String()})
	rec := httptest.NewRecorder()
	h.CreateBranch(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestCreateBranchRejectsEmptyName(t *testing.T) {
	t.Parallel()
	store := newFakeBranchStore()
	streamID := uuid.New()
	store.streams[streamID] = true
	h := &handlers.BranchesHandler{Store: store}
	req := branchReq(t, "POST", "/streams/"+streamID.String()+"/branches",
		`{"name":"   "}`, newWriterClaims(), map[string]string{"id": streamID.String()})
	rec := httptest.NewRecorder()
	h.CreateBranch(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "name is required")
}

func TestCreateBranchRejectsInvalidCharacters(t *testing.T) {
	t.Parallel()
	store := newFakeBranchStore()
	streamID := uuid.New()
	store.streams[streamID] = true
	h := &handlers.BranchesHandler{Store: store}
	req := branchReq(t, "POST", "/streams/"+streamID.String()+"/branches",
		`{"name":"feature x"}`, newWriterClaims(), map[string]string{"id": streamID.String()})
	rec := httptest.NewRecorder()
	h.CreateBranch(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "alphanumerics")
}

func TestCreateBranchAcceptsAlnumDashUnderscoreSlash(t *testing.T) {
	t.Parallel()
	store := newFakeBranchStore()
	streamID := uuid.New()
	store.streams[streamID] = true
	h := &handlers.BranchesHandler{Store: store}
	for _, name := range []string{"feat-1", "feat_2", "team/feature/3", "ABC"} {
		req := branchReq(t, "POST", "/streams/"+streamID.String()+"/branches",
			`{"name":"`+name+`"}`, newWriterClaims(), map[string]string{"id": streamID.String()})
		rec := httptest.NewRecorder()
		h.CreateBranch(rec, req)
		assert.Equal(t, http.StatusOK, rec.Code, "name=%s body=%s", name, rec.Body.String())
	}
}

func TestCreateBranchStreamNotFound(t *testing.T) {
	t.Parallel()
	store := newFakeBranchStore()
	streamID := uuid.New()
	h := &handlers.BranchesHandler{Store: store}
	req := branchReq(t, "POST", "/streams/"+streamID.String()+"/branches",
		`{"name":"feat"}`, newWriterClaims(), map[string]string{"id": streamID.String()})
	rec := httptest.NewRecorder()
	h.CreateBranch(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestCreateBranchRejectsForeignParent(t *testing.T) {
	t.Parallel()
	store := newFakeBranchStore()
	streamID := uuid.New()
	store.streams[streamID] = true
	h := &handlers.BranchesHandler{Store: store}
	foreign := uuid.New()
	req := branchReq(t, "POST", "/streams/"+streamID.String()+"/branches",
		`{"name":"feat","parent_branch_id":"`+foreign.String()+`"}`,
		newWriterClaims(), map[string]string{"id": streamID.String()})
	rec := httptest.NewRecorder()
	h.CreateBranch(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "parent_branch_id")
}

func TestCreateBranchPersistsActorAndDescription(t *testing.T) {
	t.Parallel()
	store := newFakeBranchStore()
	streamID := uuid.New()
	store.streams[streamID] = true
	h := &handlers.BranchesHandler{Store: store}
	req := branchReq(t, "POST", "/streams/"+streamID.String()+"/branches",
		`{"name":"feat","description":"experiment 17"}`,
		newWriterClaims(), map[string]string{"id": streamID.String()})
	rec := httptest.NewRecorder()
	h.CreateBranch(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var b streambranch.StreamBranch
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &b))
	assert.Equal(t, "feat", b.Name)
	assert.Equal(t, "active", b.Status)
	assert.Equal(t, "experiment 17", b.Description)
	assert.Equal(t, "writer@example.com", b.CreatedBy)
}

// ─── DeleteBranch ──────────────────────────────────────────────────────

func TestDeleteMainBranchRefused(t *testing.T) {
	t.Parallel()
	store := newFakeBranchStore()
	streamID := uuid.New()
	store.streams[streamID] = true
	h := &handlers.BranchesHandler{Store: store}
	req := branchReq(t, "DELETE", "/streams/"+streamID.String()+"/branches/main", "",
		newWriterClaims(), map[string]string{"id": streamID.String(), "name": "main"})
	rec := httptest.NewRecorder()
	h.DeleteBranch(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "'main'")
}

func TestDeleteBranchRequiresWritePermission(t *testing.T) {
	t.Parallel()
	store := newFakeBranchStore()
	streamID := uuid.New()
	store.streams[streamID] = true
	h := &handlers.BranchesHandler{Store: store}
	req := branchReq(t, "DELETE", "/streams/"+streamID.String()+"/branches/feat", "",
		newReaderClaims(), map[string]string{"id": streamID.String(), "name": "feat"})
	rec := httptest.NewRecorder()
	h.DeleteBranch(rec, req)
	assert.Equal(t, http.StatusForbidden, rec.Code)
}

func TestDeleteBranchRefusedWhenHasUncommittedHistory(t *testing.T) {
	t.Parallel()
	store := newFakeBranchStore()
	streamID := uuid.New()
	store.streams[streamID] = true
	store.branches[streamID] = map[string]*streambranch.StreamBranch{
		"feat": {
			ID: uuid.New(), StreamID: streamID, Name: "feat",
			Status: "active", HeadSequenceNo: 5,
		},
	}
	h := &handlers.BranchesHandler{Store: store}
	req := branchReq(t, "DELETE", "/streams/"+streamID.String()+"/branches/feat", "",
		newWriterClaims(), map[string]string{"id": streamID.String(), "name": "feat"})
	rec := httptest.NewRecorder()
	h.DeleteBranch(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "uncommitted history")
}

func TestDeleteBranchAllowedAfterMerge(t *testing.T) {
	t.Parallel()
	store := newFakeBranchStore()
	streamID := uuid.New()
	store.streams[streamID] = true
	store.branches[streamID] = map[string]*streambranch.StreamBranch{
		"feat": {
			ID: uuid.New(), StreamID: streamID, Name: "feat",
			Status: "merged", HeadSequenceNo: 5,
		},
	}
	h := &handlers.BranchesHandler{Store: store}
	req := branchReq(t, "DELETE", "/streams/"+streamID.String()+"/branches/feat", "",
		newWriterClaims(), map[string]string{"id": streamID.String(), "name": "feat"})
	rec := httptest.NewRecorder()
	h.DeleteBranch(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), `"deleted":true`)
}

// ─── MergeBranch ───────────────────────────────────────────────────────

func TestMergeBranchRequiresDifferentTarget(t *testing.T) {
	t.Parallel()
	store := newFakeBranchStore()
	streamID := uuid.New()
	store.streams[streamID] = true
	h := &handlers.BranchesHandler{Store: store}
	req := branchReq(t, "POST", "/streams/"+streamID.String()+"/branches/main:merge",
		`{"target_branch":"main"}`,
		newWriterClaims(), map[string]string{"id": streamID.String(), "name": "main"})
	rec := httptest.NewRecorder()
	h.MergeBranch(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	assert.Contains(t, rec.Body.String(), "target branch must differ")
}

func TestMergeBranchAdvancesTargetHead(t *testing.T) {
	t.Parallel()
	store := newFakeBranchStore()
	streamID := uuid.New()
	store.streams[streamID] = true
	store.branches[streamID] = map[string]*streambranch.StreamBranch{
		"main": {ID: uuid.New(), StreamID: streamID, Name: "main", Status: "active", HeadSequenceNo: 7},
		"feat": {ID: uuid.New(), StreamID: streamID, Name: "feat", Status: "active", HeadSequenceNo: 12},
	}
	h := &handlers.BranchesHandler{Store: store}
	req := branchReq(t, "POST", "/streams/"+streamID.String()+"/branches/feat:merge", "",
		newWriterClaims(), map[string]string{"id": streamID.String(), "name": "feat"})
	rec := httptest.NewRecorder()
	h.MergeBranch(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp streambranch.MergeBranchResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, int64(12), resp.MergedSequenceNo)
	assert.Equal(t, "merged 'feat' into 'main'", resp.Message)
	assert.Equal(t, int64(12), store.branches[streamID]["main"].HeadSequenceNo)
	assert.Equal(t, "merged", store.branches[streamID]["feat"].Status)
}

func TestMergeBranchSourceMissing(t *testing.T) {
	t.Parallel()
	store := newFakeBranchStore()
	streamID := uuid.New()
	store.streams[streamID] = true
	store.branches[streamID] = map[string]*streambranch.StreamBranch{
		"main": {ID: uuid.New(), StreamID: streamID, Name: "main", Status: "active"},
	}
	h := &handlers.BranchesHandler{Store: store}
	req := branchReq(t, "POST", "/streams/"+streamID.String()+"/branches/feat:merge", "",
		newWriterClaims(), map[string]string{"id": streamID.String(), "name": "feat"})
	rec := httptest.NewRecorder()
	h.MergeBranch(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Contains(t, rec.Body.String(), "source branch not found")
}

// ─── ArchiveBranch ─────────────────────────────────────────────────────

func TestArchiveBranchUpdatesStatus(t *testing.T) {
	t.Parallel()
	store := newFakeBranchStore()
	streamID := uuid.New()
	store.streams[streamID] = true
	store.branches[streamID] = map[string]*streambranch.StreamBranch{
		"feat": {ID: uuid.New(), StreamID: streamID, Name: "feat", Status: "active", HeadSequenceNo: 9},
	}
	h := &handlers.BranchesHandler{Store: store}
	req := branchReq(t, "POST", "/streams/"+streamID.String()+"/branches/feat:archive", "",
		newWriterClaims(), map[string]string{"id": streamID.String(), "name": "feat"})
	rec := httptest.NewRecorder()
	h.ArchiveBranch(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	var b streambranch.StreamBranch
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &b))
	assert.Equal(t, "archived", b.Status)
	require.NotNil(t, b.ArchivedAt)
}

func TestArchiveBranchSkipsColdWhenNoDatasetID(t *testing.T) {
	t.Parallel()
	store := newFakeBranchStore()
	streamID := uuid.New()
	store.streams[streamID] = true
	store.branches[streamID] = map[string]*streambranch.StreamBranch{
		"feat": {ID: uuid.New(), StreamID: streamID, Name: "feat", Status: "active"},
	}
	cold := &fakeColdTier{accepted: true}
	h := &handlers.BranchesHandler{Store: store, Cold: cold}
	req := branchReq(t, "POST", "/streams/"+streamID.String()+"/branches/feat:archive",
		`{"commit_cold":true}`,
		newWriterClaims(), map[string]string{"id": streamID.String(), "name": "feat"})
	rec := httptest.NewRecorder()
	h.ArchiveBranch(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Empty(t, cold.calls, "cold tier should be skipped when dataset_branch_id is empty")
}

func TestArchiveBranchTriggersColdAndMetric(t *testing.T) {
	t.Parallel()
	store := newFakeBranchStore()
	streamID := uuid.New()
	datasetBranch := "dataset/feat"
	store.streams[streamID] = true
	store.branches[streamID] = map[string]*streambranch.StreamBranch{
		"feat": {ID: uuid.New(), StreamID: streamID, Name: "feat", Status: "active",
			HeadSequenceNo: 7, DatasetBranchID: &datasetBranch},
	}
	cold := &fakeColdTier{accepted: true}
	metrics := &fakeMetricSink{}
	h := &handlers.BranchesHandler{Store: store, Cold: cold, MetricSink: metrics}
	req := branchReq(t, "POST", "/streams/"+streamID.String()+"/branches/feat:archive",
		`{"commit_cold":true}`,
		newWriterClaims(), map[string]string{"id": streamID.String(), "name": "feat"})
	rec := httptest.NewRecorder()
	h.ArchiveBranch(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Len(t, cold.calls, 1)
	assert.Equal(t, "feat", cold.calls[0].Name)
	assert.Equal(t, uint64(7), metrics.calls["feat"])
}

func TestArchiveBranchSurvivesColdFailure(t *testing.T) {
	t.Parallel()
	store := newFakeBranchStore()
	streamID := uuid.New()
	datasetBranch := "dataset/feat"
	store.streams[streamID] = true
	store.branches[streamID] = map[string]*streambranch.StreamBranch{
		"feat": {ID: uuid.New(), StreamID: streamID, Name: "feat", Status: "active",
			HeadSequenceNo: 3, DatasetBranchID: &datasetBranch},
	}
	cold := &fakeColdTier{err: errors.New("network down")}
	h := &handlers.BranchesHandler{Store: store, Cold: cold}
	req := branchReq(t, "POST", "/streams/"+streamID.String()+"/branches/feat:archive",
		`{"commit_cold":true}`,
		newWriterClaims(), map[string]string{"id": streamID.String(), "name": "feat"})
	rec := httptest.NewRecorder()
	h.ArchiveBranch(rec, req)
	require.Equal(t, http.StatusOK, rec.Code, "archive must succeed even if cold tier fails")
	require.Len(t, cold.calls, 1)
}

// ─── HTTPColdTierBridge ────────────────────────────────────────────────

func TestHTTPColdTierBridgePostsExpectedShape(t *testing.T) {
	t.Parallel()
	var receivedURL string
	var receivedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedURL = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&receivedBody)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	bridge := &handlers.HTTPColdTierBridge{BaseURL: srv.URL}
	streamID := uuid.New()
	dataset := "dataset/feat"
	branch := &streambranch.StreamBranch{
		ID:              uuid.New(),
		StreamID:        streamID,
		Name:            "feat",
		HeadSequenceNo:  17,
		DatasetBranchID: &dataset,
	}
	accepted, err := bridge.CommitCold(context.Background(), branch, time.Now().UTC())
	require.NoError(t, err)
	assert.True(t, accepted)
	expectedPath := "/api/v1/datasets/" + streamID.String() + "/branches/" + dataset + ":commit"
	assert.Equal(t, expectedPath, receivedURL)
	assert.Equal(t, "feat", receivedBody["branch_name"])
	assert.EqualValues(t, 17, receivedBody["head_sequence_no"])
}

func TestHTTPColdTierBridgeRejectsMissingDatasetID(t *testing.T) {
	t.Parallel()
	bridge := &handlers.HTTPColdTierBridge{BaseURL: "http://example.invalid"}
	branch := &streambranch.StreamBranch{ID: uuid.New(), StreamID: uuid.New(), Name: "feat"}
	accepted, err := bridge.CommitCold(context.Background(), branch, time.Now().UTC())
	require.Error(t, err)
	assert.False(t, accepted)
}
