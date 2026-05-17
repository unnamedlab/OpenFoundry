//go:build integration

package handlers_test

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/openfoundry/openfoundry-go/services/sdk-generation-service/internal/generator"
	"github.com/openfoundry/openfoundry-go/services/sdk-generation-service/internal/handlers"
)

// TestGenerateEndpointTypeScript exercises the full pipeline:
// build `of-sdk-gen`, mount the chi route, POST to /api/v1/sdk/generate,
// and inspect the zip for an `index.ts` entry plus an `AuditService`-
// shaped class in the output (proxy for "client actually exists").
//
// The test is skipped when the Node toolchain is not available — CI
// installs Node so this becomes a real gate there.
func TestGenerateEndpointTypeScript(t *testing.T) {
	if _, err := exec.LookPath("npx"); err != nil {
		t.Skip("npx not on PATH; skipping (install Node 18+ to run)")
	}
	repoRoot := findRepoRoot(t)
	bin := buildOfSDKGen(t, repoRoot)

	srv := newServer(t, repoRoot, bin)
	defer srv.Close()

	body := strings.NewReader(`{"service":"audit-compliance-service","language":"ts"}`)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, srv.URL+"/api/v1/sdk/generate", body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, raw)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/zip" {
		t.Fatalf("expected application/zip, got %q", ct)
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	r, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("invalid zip: %v", err)
	}
	if !zipHasFile(r, "index.ts") {
		t.Fatalf("zip missing index.ts; entries: %s", zipEntryList(r))
	}
	if !zipAnyEntryContains(r, ".ts", "Audit") {
		t.Fatalf("no .ts entry references Audit; entries: %s", zipEntryList(r))
	}
}

// TestGenerateEndpointPython mirrors the TS case for openapi-python-client.
func TestGenerateEndpointPython(t *testing.T) {
	if _, err := exec.LookPath("openapi-python-client"); err != nil {
		t.Skip("openapi-python-client not on PATH; skipping (pip install openapi-python-client)")
	}
	repoRoot := findRepoRoot(t)
	bin := buildOfSDKGen(t, repoRoot)

	srv := newServer(t, repoRoot, bin)
	defer srv.Close()

	reqBody, _ := json.Marshal(map[string]string{
		"service":  "notification-alerting-service",
		"language": "py",
	})
	resp, err := http.Post(srv.URL+"/api/v1/sdk/generate", "application/json", bytes.NewReader(reqBody))
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("status=%d body=%s", resp.StatusCode, raw)
	}
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	r, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		t.Fatalf("invalid zip: %v", err)
	}
	if !zipAnyEntryHasSuffix(r, "__init__.py") {
		t.Fatalf("zip missing __init__.py; entries: %s", zipEntryList(r))
	}
}

func newServer(t *testing.T, repoRoot, bin string) *httptest.Server {
	t.Helper()
	gen := &handlers.GenerateHandler{Driver: &generator.Driver{Bin: bin, RepoRoot: repoRoot}}
	r := chi.NewRouter()
	r.Post("/api/v1/sdk/generate", gen.Generate)
	return httptest.NewServer(r)
}

func buildOfSDKGen(t *testing.T, repoRoot string) string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "of-sdk-gen")
	cmd := exec.Command("go", "build", "-o", out, "./tools/of-sdk-gen")
	cmd.Dir = repoRoot
	cmd.Env = append(os.Environ(), "GOCACHE="+filepath.Join(t.TempDir(), "gocache"))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("build of-sdk-gen: %v\n%s", err, stderr.String())
	}
	return out
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dir := cwd
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatalf("repo root not found from %s", cwd)
		}
		dir = parent
	}
}

func zipHasFile(r *zip.Reader, name string) bool {
	for _, f := range r.File {
		if filepath.Base(f.Name) == name {
			return true
		}
	}
	return false
}

func zipAnyEntryHasSuffix(r *zip.Reader, suffix string) bool {
	for _, f := range r.File {
		if strings.HasSuffix(f.Name, suffix) {
			return true
		}
	}
	return false
}

func zipAnyEntryContains(r *zip.Reader, suffix, needle string) bool {
	for _, f := range r.File {
		if !strings.HasSuffix(f.Name, suffix) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			continue
		}
		b, err := io.ReadAll(rc)
		_ = rc.Close()
		if err == nil && strings.Contains(string(b), needle) {
			return true
		}
	}
	return false
}

func zipEntryList(r *zip.Reader) string {
	names := make([]string, 0, len(r.File))
	for _, f := range r.File {
		names = append(names, f.Name)
	}
	return strings.Join(names, ", ")
}
