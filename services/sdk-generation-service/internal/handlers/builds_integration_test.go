//go:build integration

package handlers_test

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	authmw "github.com/openfoundry/openfoundry-go/libs/auth-middleware"
	oftesting "github.com/openfoundry/openfoundry-go/libs/testing"
	"github.com/openfoundry/openfoundry-go/services/sdk-generation-service/internal/domain"
	"github.com/openfoundry/openfoundry-go/services/sdk-generation-service/internal/generator/ts"
	"github.com/openfoundry/openfoundry-go/services/sdk-generation-service/internal/handlers"
	"github.com/openfoundry/openfoundry-go/services/sdk-generation-service/internal/ontologyclient"
	"github.com/openfoundry/openfoundry-go/services/sdk-generation-service/internal/repo"
)

// TestBuildPipelineEndToEnd: enqueue → worker runs synchronously →
// artifact materializes on disk → HTTP GET streams it back. Single
// test exercises the entire OSDK v0 surface against a real Postgres.
func TestBuildPipelineEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	pg := oftesting.BootPostgres(ctx, t)
	t.Cleanup(func() { pg.Stop(context.Background()) })

	if err := repo.Migrate(ctx, pg.Pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := &repo.Repo{Pool: pg.Pool}
	tenantID := uuid.New()
	userID := uuid.New()

	artifactDir := t.TempDir()
	artifacts := &handlers.LocalArtifactStore{Dir: artifactDir}
	worker := &handlers.BuildWorker{
		Repo:        store,
		Ontology:    &ontologyclient.StubClient{},
		TSGenerator: &ts.Generator{},
		Artifacts:   artifacts,
	}

	// Wire the handler with an inline dispatcher: run the worker
	// synchronously so the test doesn't have to poll for completion.
	api := &handlers.BuildHandlers{
		Repo:      store,
		Worker:    worker,
		Artifacts: artifacts,
		SpawnAsync: func(id uuid.UUID) {
			if err := worker.ProcessBuild(ctx, id); err != nil {
				t.Errorf("inline worker: %v", err)
			}
		},
	}

	claims := &authmw.Claims{
		Sub:   userID,
		OrgID: &tenantID,
		Roles: []string{"member"},
	}

	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(authmw.ContextWithClaims(req.Context(), claims)))
		})
	})
	r.Post("/api/v1/sdks/builds", api.Create)
	r.Get("/api/v1/sdks/builds", api.List)
	r.Get("/api/v1/sdks/builds/{id}", api.Get)
	r.Get("/api/v1/sdks/builds/{id}/artifact", api.Artifact)

	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)

	// --- POST /sdks/builds ---
	body, _ := json.Marshal(domain.SDKRequest{
		OntologyVersion: "v1.0.0",
		Target:          domain.TargetTypeScript,
	})
	resp, err := http.Post(srv.URL+"/api/v1/sdks/builds", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	if resp.StatusCode != http.StatusAccepted {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, raw)
	}
	var created handlers.CreateBuildResponse
	if err := json.NewDecoder(resp.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	resp.Body.Close()
	if created.BuildID == uuid.Nil {
		t.Fatalf("missing build_id")
	}

	// --- GET /sdks/builds/{id} — synchronous dispatcher means it's done ---
	build := getBuild(t, srv.URL, created.BuildID)
	if build.Status != domain.StatusSucceeded {
		t.Fatalf("status=%q error=%q", build.Status, build.ErrorMessage)
	}
	if build.TenantID != tenantID {
		t.Fatalf("tenant mismatch: %s vs %s", build.TenantID, tenantID)
	}
	if !strings.HasPrefix(build.ArtifactURI, "file://") {
		t.Fatalf("artifact uri: %q", build.ArtifactURI)
	}
	if !strings.HasPrefix(strings.TrimPrefix(build.ArtifactURI, "file://"), filepath.Clean(artifactDir)) {
		t.Fatalf("artifact written outside dir: %q", build.ArtifactURI)
	}

	// --- GET /sdks/builds/{id}/artifact ---
	artResp, err := http.Get(srv.URL + "/api/v1/sdks/builds/" + build.ID.String() + "/artifact")
	if err != nil {
		t.Fatalf("artifact request: %v", err)
	}
	defer artResp.Body.Close()
	if artResp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(artResp.Body)
		t.Fatalf("artifact status=%d body=%s", artResp.StatusCode, raw)
	}
	if ct := artResp.Header.Get("Content-Type"); ct != "application/gzip" {
		t.Errorf("content-type = %q, want application/gzip", ct)
	}
	tarball, err := io.ReadAll(artResp.Body)
	if err != nil {
		t.Fatalf("read artifact: %v", err)
	}
	files := extractTarGz(t, tarball)
	for _, name := range []string{"package.json", "tsconfig.json", "index.ts", "types.ts", "actions.ts", "client.ts"} {
		if _, ok := files[name]; !ok {
			t.Errorf("artifact missing %s", name)
		}
	}
	if !strings.Contains(string(files["types.ts"]), "export interface Customer") {
		t.Errorf("types.ts missing Customer interface")
	}

	// --- GET /sdks/builds ---
	listResp, err := http.Get(srv.URL + "/api/v1/sdks/builds")
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	defer listResp.Body.Close()
	var list []domain.SDKBuild
	if err := json.NewDecoder(listResp.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list) != 1 || list[0].ID != build.ID {
		t.Errorf("unexpected list: %+v", list)
	}
}

// TestBuildPipelineFailsClosedOnSnapshotMiss verifies that a missing
// ontology snapshot lands the build in `failed` rather than leaving it
// stuck at `building`.
func TestBuildPipelineFailsClosedOnSnapshotMiss(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	pg := oftesting.BootPostgres(ctx, t)
	t.Cleanup(func() { pg.Stop(context.Background()) })
	if err := repo.Migrate(ctx, pg.Pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	store := &repo.Repo{Pool: pg.Pool}
	tenantID := uuid.New()

	worker := &handlers.BuildWorker{
		Repo:        store,
		Ontology:    failingOntology{},
		TSGenerator: &ts.Generator{},
		Artifacts:   &handlers.LocalArtifactStore{Dir: t.TempDir()},
	}

	build := &domain.SDKBuild{
		ID:              uuid.New(),
		TenantID:        tenantID,
		OntologyVersion: "missing",
		Target:          domain.TargetTypeScript,
		Status:          domain.StatusQueued,
		RequestedBy:     uuid.New(),
	}
	if err := store.CreateBuild(ctx, build, nil, nil); err != nil {
		t.Fatalf("create build: %v", err)
	}
	if err := worker.ProcessBuild(ctx, build.ID); err == nil {
		t.Fatalf("expected error from process")
	}
	got, err := store.GetBuild(ctx, build.ID)
	if err != nil {
		t.Fatalf("get build: %v", err)
	}
	if got.Status != domain.StatusFailed {
		t.Errorf("status = %q, want failed", got.Status)
	}
	if got.ErrorMessage == "" {
		t.Errorf("expected error message")
	}
}

type failingOntology struct{}

func (failingOntology) GetOntologySnapshot(_ context.Context, _ uuid.UUID, _ string) (*ontologyclient.OntologySnapshot, error) {
	return nil, errors.New("upstream unavailable")
}

func getBuild(t *testing.T, base string, id uuid.UUID) domain.SDKBuild {
	t.Helper()
	resp, err := http.Get(base + "/api/v1/sdks/builds/" + id.String())
	if err != nil {
		t.Fatalf("get build: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("get status=%d body=%s", resp.StatusCode, raw)
	}
	var out domain.SDKBuild
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return out
}

func extractTarGz(t *testing.T, body []byte) map[string][]byte {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("gzip: %v", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	out := map[string][]byte{}
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("tar next: %v", err)
		}
		buf := &bytes.Buffer{}
		if _, err := io.Copy(buf, tr); err != nil {
			t.Fatalf("tar copy: %v", err)
		}
		out[hdr.Name] = buf.Bytes()
	}
	return out
}
