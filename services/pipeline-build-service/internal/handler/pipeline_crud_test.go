package handler

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/pipeline-build-service/internal/models"
)

func TestPipelineCRUDReturnsExplicit503WithoutRepository(t *testing.T) {
	restore := SetPipelineAuthoringRepository(nil)
	defer restore()
	id := uuid.New().String()
	tests := []struct {
		name   string
		method string
		path   string
		body   string
		h      http.HandlerFunc
	}{
		{name: "list", method: http.MethodGet, path: "/api/v1/pipelines", h: ListPipelines},
		{name: "create", method: http.MethodPost, path: "/api/v1/pipelines", body: `{"name":"p","nodes":[]}`, h: CreatePipeline},
		{name: "get", method: http.MethodGet, path: "/api/v1/pipelines/" + id, h: GetPipeline},
		{name: "patch", method: http.MethodPatch, path: "/api/v1/pipelines/" + id, body: `{"name":"renamed"}`, h: UpdatePipeline},
		{name: "put", method: http.MethodPut, path: "/api/v1/pipelines/" + id, body: `{"name":"renamed"}`, h: UpdatePipeline},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, tc.path, bytes.NewReader([]byte(tc.body)))
			if tc.path != "/api/v1/pipelines" {
				r = requestWithURLParam(tc.method, tc.path, bytes.NewReader([]byte(tc.body)), "id", id)
			}
			rr := httptest.NewRecorder()
			tc.h(rr, r)
			require.Equal(t, http.StatusServiceUnavailable, rr.Code)
			var payload map[string]string
			require.NoError(t, json.NewDecoder(rr.Body).Decode(&payload))
			require.Equal(t, "pipeline_authoring_repository_not_configured", payload["error"])
			require.NotEmpty(t, payload["detail"])
		})
	}
}

func TestPipelineCRUDUsesConfiguredRepository(t *testing.T) {
	repo := newFakePipelineAuthoringRepo()
	restore := SetPipelineAuthoringRepository(repo)
	defer restore()

	createRR := httptest.NewRecorder()
	CreatePipeline(createRR, httptest.NewRequest(http.MethodPost, "/api/v1/pipelines", bytes.NewReader([]byte(`{"name":"daily","description":"initial","nodes":[{"id":"n1","transform_type":"noop"}],"schedule_config":{"enabled":true},"retry_policy":{"max_attempts":2,"retry_on_failure":true,"allow_partial_reexecution":false}}`))))
	require.Equal(t, http.StatusCreated, createRR.Code)
	var created models.Pipeline
	require.NoError(t, json.NewDecoder(createRR.Body).Decode(&created))
	require.Equal(t, "daily", created.Name)
	require.JSONEq(t, `[{"id":"n1","label":"","transform_type":"noop"}]`, string(created.DAG))

	listRR := httptest.NewRecorder()
	ListPipelines(listRR, httptest.NewRequest(http.MethodGet, "/api/v1/pipelines?page=1&per_page=10&status=draft", nil))
	require.Equal(t, http.StatusOK, listRR.Code)
	var list models.ListPipelinesResponse
	require.NoError(t, json.NewDecoder(listRR.Body).Decode(&list))
	require.Equal(t, int64(1), list.Total)
	require.Len(t, list.Data, 1)

	getRR := httptest.NewRecorder()
	GetPipeline(getRR, requestWithURLParam(http.MethodGet, "/api/v1/pipelines/"+created.ID.String(), nil, "id", created.ID.String()))
	require.Equal(t, http.StatusOK, getRR.Code)

	patchRR := httptest.NewRecorder()
	UpdatePipeline(patchRR, requestWithURLParam(http.MethodPatch, "/api/v1/pipelines/"+created.ID.String(), bytes.NewReader([]byte(`{"name":"daily-v2"}`)), "id", created.ID.String()))
	require.Equal(t, http.StatusOK, patchRR.Code)
	var patched models.Pipeline
	require.NoError(t, json.NewDecoder(patchRR.Body).Decode(&patched))
	require.Equal(t, "daily-v2", patched.Name)

	putRR := httptest.NewRecorder()
	UpdatePipeline(putRR, requestWithURLParam(http.MethodPut, "/api/v1/pipelines/"+created.ID.String(), bytes.NewReader([]byte(`{"description":"from put"}`)), "id", created.ID.String()))
	require.Equal(t, http.StatusOK, putRR.Code)
	var put models.Pipeline
	require.NoError(t, json.NewDecoder(putRR.Body).Decode(&put))
	require.Equal(t, "from put", put.Description)
}

type fakePipelineAuthoringRepo struct {
	items map[uuid.UUID]models.Pipeline
}

func newFakePipelineAuthoringRepo() *fakePipelineAuthoringRepo {
	return &fakePipelineAuthoringRepo{items: map[uuid.UUID]models.Pipeline{}}
}

func (f *fakePipelineAuthoringRepo) ListPipelines(context.Context, models.ListPipelinesQuery) (models.ListPipelinesResponse, error) {
	items := make([]models.Pipeline, 0, len(f.items))
	for _, p := range f.items {
		items = append(items, p)
	}
	return models.ListPipelinesResponse{Data: items, Total: int64(len(items)), Page: 1, PerPage: 50}, nil
}

func (f *fakePipelineAuthoringRepo) CreatePipeline(_ context.Context, req models.CreatePipelineRequest, ownerID uuid.UUID) (*models.Pipeline, error) {
	dag, err := json.Marshal(req.Nodes)
	if err != nil {
		return nil, err
	}
	description := ""
	if req.Description != nil {
		description = *req.Description
	}
	status := "draft"
	if req.Status != nil {
		status = *req.Status
	}
	now := time.Now().UTC()
	p := models.Pipeline{ID: uuid.New(), Name: req.Name, Description: description, OwnerID: ownerID, DAG: dag, Status: status, ScheduleConfig: json.RawMessage(`{}`), RetryPolicy: json.RawMessage(`{"max_attempts":1,"retry_on_failure":false,"allow_partial_reexecution":true}`), CreatedAt: now, UpdatedAt: now}
	if req.ScheduleConfig != nil {
		p.ScheduleConfig, err = json.Marshal(req.ScheduleConfig)
		if err != nil {
			return nil, err
		}
	}
	if req.RetryPolicy != nil {
		p.RetryPolicy, err = json.Marshal(req.RetryPolicy)
		if err != nil {
			return nil, err
		}
	}
	f.items[p.ID] = p
	return &p, nil
}

func (f *fakePipelineAuthoringRepo) GetPipeline(_ context.Context, id uuid.UUID) (*models.Pipeline, error) {
	p, ok := f.items[id]
	if !ok {
		return nil, nil
	}
	return &p, nil
}

func (f *fakePipelineAuthoringRepo) UpdatePipeline(_ context.Context, id uuid.UUID, req models.UpdatePipelineRequest) (*models.Pipeline, error) {
	p, ok := f.items[id]
	if !ok {
		return nil, nil
	}
	if req.Name != nil {
		p.Name = *req.Name
	}
	if req.Description != nil {
		p.Description = *req.Description
	}
	if req.Status != nil {
		p.Status = *req.Status
	}
	if req.Nodes != nil {
		dag, err := json.Marshal(*req.Nodes)
		if err != nil {
			return nil, err
		}
		p.DAG = dag
	}
	p.UpdatedAt = time.Now().UTC()
	f.items[id] = p
	return &p, nil
}

func (f *fakePipelineAuthoringRepo) DeletePipeline(_ context.Context, id uuid.UUID) (bool, error) {
	if _, ok := f.items[id]; !ok {
		return false, nil
	}
	delete(f.items, id)
	return true, nil
}
