package flink

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNormaliseExtractsVerticesAndEdges(t *testing.T) {
	payload := map[string]any{
		"vertices": []any{
			map[string]any{"id": "v1", "name": "Source", "parallelism": float64(2), "status": "RUNNING"},
			map[string]any{"id": "v2", "name": "Sink", "parallelism": float64(4), "status": "RUNNING"},
		},
		"plan": map[string]any{
			"nodes": []any{
				map[string]any{"id": "v2", "inputs": []any{map[string]any{"id": "v1"}}},
			},
		},
	}
	out := Normalise(payload, "job-x")
	if out.JobID != "job-x" {
		t.Fatalf("JobID = %q", out.JobID)
	}
	if len(out.Vertices) != 2 {
		t.Fatalf("Vertices = %d, want 2", len(out.Vertices))
	}
	if len(out.Edges) != 1 || out.Edges[0].Source != "v1" || out.Edges[0].Target != "v2" {
		t.Fatalf("Edges = %+v", out.Edges)
	}
	// Round-trip JSON shape mirrors the Rust output keys.
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{`"job_id"`, `"vertices"`, `"edges"`, `"raw"`} {
		if !strings.Contains(string(b), key) {
			t.Fatalf("output JSON missing %s: %s", key, b)
		}
	}
}

func TestNormaliseHandlesMissingPlan(t *testing.T) {
	out := Normalise(map[string]any{}, "job-y")
	if len(out.Vertices) != 0 || len(out.Edges) != 0 {
		t.Fatalf("expected empty graph, got %+v", out)
	}
}

func TestFetchJobGraphUsesProvidedJobID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/jobs/abc-123" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"vertices": []any{map[string]any{"id": "v1"}},
			"plan":     map[string]any{"nodes": []any{}},
		})
	}))
	defer srv.Close()

	cfg := FlinkRuntimeConfig{JobManagerURLTemplate: srv.URL}
	out, err := FetchJobGraph(context.Background(), srv.Client(), cfg, "demo", "flink", "abc-123")
	if err != nil {
		t.Fatalf("FetchJobGraph: %v", err)
	}
	if out.JobID != "abc-123" {
		t.Fatalf("JobID = %q", out.JobID)
	}
	if len(out.Vertices) != 1 {
		t.Fatalf("Vertices = %d", len(out.Vertices))
	}
}

func TestFetchJobGraphDiscoversRunningJob(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/jobs":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jobs": []any{
					map[string]any{"id": "old-1", "status": "FINISHED"},
					map[string]any{"id": "live-1", "status": "RUNNING"},
				},
			})
		case "/jobs/live-1":
			_ = json.NewEncoder(w).Encode(map[string]any{"vertices": []any{}})
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	cfg := FlinkRuntimeConfig{JobManagerURLTemplate: srv.URL}
	out, err := FetchJobGraph(context.Background(), srv.Client(), cfg, "demo", "flink", "")
	if err != nil {
		t.Fatalf("FetchJobGraph: %v", err)
	}
	if out.JobID != "live-1" {
		t.Fatalf("expected RUNNING job to win, got %q", out.JobID)
	}
}

func TestFetchJobGraphFallsBackToFirstJob(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/jobs":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"jobs": []any{
					map[string]any{"id": "first-1", "status": "FINISHED"},
					map[string]any{"id": "second-1", "status": "FAILED"},
				},
			})
		case "/jobs/first-1":
			_ = json.NewEncoder(w).Encode(map[string]any{})
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
		}
	}))
	defer srv.Close()

	cfg := FlinkRuntimeConfig{JobManagerURLTemplate: srv.URL}
	out, err := FetchJobGraph(context.Background(), srv.Client(), cfg, "demo", "flink", "")
	if err != nil {
		t.Fatalf("FetchJobGraph: %v", err)
	}
	if out.JobID != "first-1" {
		t.Fatalf("expected first job to be chosen, got %q", out.JobID)
	}
}

func TestFetchJobGraphPropagatesNon2xxStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	defer srv.Close()

	cfg := FlinkRuntimeConfig{JobManagerURLTemplate: srv.URL}
	_, err := FetchJobGraph(context.Background(), srv.Client(), cfg, "demo", "flink", "abc")
	var jg *JobGraphError
	if !errors.As(err, &jg) {
		t.Fatalf("expected JobGraphError, got %v", err)
	}
	if jg.Kind != JobGraphErrStatus || jg.Status != http.StatusBadGateway {
		t.Fatalf("expected 502 Status error, got %+v", jg)
	}
}

func TestFetchJobGraphRejectsMissingJobsArray(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"unexpected": "shape"})
	}))
	defer srv.Close()

	cfg := FlinkRuntimeConfig{JobManagerURLTemplate: srv.URL}
	_, err := FetchJobGraph(context.Background(), srv.Client(), cfg, "demo", "flink", "")
	var jg *JobGraphError
	if !errors.As(err, &jg) {
		t.Fatalf("expected JobGraphError, got %v", err)
	}
	if jg.Kind != JobGraphErrBody {
		t.Fatalf("expected Body error, got %+v", jg)
	}
}

func TestIsNotDeployedDetectsSentinel(t *testing.T) {
	if !IsNotDeployed(ErrJobGraphNotDeployed) {
		t.Fatal("IsNotDeployed should match the sentinel")
	}
	if IsNotDeployed(errors.New("other")) {
		t.Fatal("IsNotDeployed should not match unrelated errors")
	}
	wrapped := &JobGraphError{Kind: JobGraphErrStatus, Status: 500}
	if IsNotDeployed(wrapped) {
		t.Fatal("IsNotDeployed should ignore other JobGraphError kinds")
	}
}

// fakeDoer lets us assert no live HTTP traffic is required for the
// pure normaliser code path.
type fakeDoer struct{ payload []byte }

func (f *fakeDoer) Do(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: 200,
		Body:       io.NopCloser(strings.NewReader(string(f.payload))),
		Header:     make(http.Header),
	}, nil
}

func TestFetchJobGraphAcceptsCustomDoer(t *testing.T) {
	doer := &fakeDoer{payload: []byte(`{"vertices":[{"id":"only"}],"plan":{"nodes":[]}}`)}
	cfg := FlinkRuntimeConfig{JobManagerURLTemplate: "http://ignored"}
	out, err := FetchJobGraph(context.Background(), doer, cfg, "demo", "flink", "j1")
	if err != nil {
		t.Fatalf("FetchJobGraph: %v", err)
	}
	if len(out.Vertices) != 1 {
		t.Fatalf("Vertices = %d", len(out.Vertices))
	}
}
