package providers

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pipelineplan "github.com/openfoundry/openfoundry-go/libs/pipeline-plan"
	pipelineruntime "github.com/openfoundry/openfoundry-go/libs/pipeline-runtime"
)

func sampleRows() []pipelineruntime.Row {
	return []pipelineruntime.Row{
		{"id": "abc", "amount": float64(10)},
		{"id": "def", "amount": float64(20)},
	}
}

func TestHTTPWriter_PostsBatchToAdapter(t *testing.T) {
	t.Parallel()
	var gotMethod, gotPath, gotCT, gotToken string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.Path
		gotCT = r.Header.Get("Content-Type")
		gotToken = r.Header.Get("X-Internal-Token")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	wr, err := NewHTTPWriter(HTTPWriterConfig{
		TableWriterURL: srv.URL,
		CatalogURL:     "http://lakekeeper",
		Warehouse:      "openfoundry",
		InternalToken:  "tok",
	})
	if err != nil {
		t.Fatalf("NewHTTPWriter: %v", err)
	}
	err = wr.Write(context.Background(), "lakekeeper", "default", "transactions_clean",
		pipelineplan.WriteModeCreateOrReplace, sampleRows())
	if err != nil {
		t.Fatalf("Write: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %s, want POST", gotMethod)
	}
	if gotPath != "/openfoundry/iceberg/v1/append" {
		t.Errorf("path = %q", gotPath)
	}
	if gotCT != "application/json" {
		t.Errorf("content-type = %q", gotCT)
	}
	if gotToken != "tok" {
		t.Errorf("X-Internal-Token = %q", gotToken)
	}

	var body appendBatch
	if err := json.Unmarshal(gotBody, &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body.Spec.Catalog != "lakekeeper" || body.Spec.Namespace != "default" || body.Spec.Table != "transactions_clean" {
		t.Errorf("spec target = %s.%s.%s", body.Spec.Catalog, body.Spec.Namespace, body.Spec.Table)
	}
	if body.Spec.Mode != "create_or_replace" {
		t.Errorf("mode = %q", body.Spec.Mode)
	}
	if body.Spec.CatalogURL != "http://lakekeeper" {
		t.Errorf("catalog_url forwarded incorrectly: %q", body.Spec.CatalogURL)
	}
	if body.Spec.Warehouse != "openfoundry" {
		t.Errorf("warehouse forwarded incorrectly: %q", body.Spec.Warehouse)
	}
	if len(body.Rows) != 2 {
		t.Errorf("rows = %d, want 2", len(body.Rows))
	}
}

func TestHTTPWriter_OmitsTokenWhenEmpty(t *testing.T) {
	t.Parallel()
	var gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Internal-Token")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	wr, err := NewHTTPWriter(HTTPWriterConfig{TableWriterURL: srv.URL})
	if err != nil {
		t.Fatalf("NewHTTPWriter: %v", err)
	}
	if err := wr.Write(context.Background(), "c", "n", "t", pipelineplan.WriteModeAppend, nil); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if gotToken != "" {
		t.Errorf("X-Internal-Token should be empty when InternalToken unset, got %q", gotToken)
	}
}

func TestHTTPWriter_404MappedToErrTableNotFound(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte("no such table"))
	}))
	defer srv.Close()
	wr, _ := NewHTTPWriter(HTTPWriterConfig{TableWriterURL: srv.URL})
	err := wr.Write(context.Background(), "c", "n", "t", pipelineplan.WriteModeAppend, sampleRows())
	if !errors.Is(err, ErrTableNotFound) {
		t.Fatalf("expected ErrTableNotFound, got %v", err)
	}
	if !strings.Contains(err.Error(), "no such table") {
		t.Errorf("body snippet missing: %v", err)
	}
}

func TestHTTPWriter_SchemaMismatchMapped(t *testing.T) {
	t.Parallel()
	for _, code := range []int{http.StatusConflict, http.StatusUnprocessableEntity} {
		code := code
		t.Run(http.StatusText(code), func(t *testing.T) {
			t.Parallel()
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(code)
				_, _ = w.Write([]byte("schema drift"))
			}))
			defer srv.Close()
			wr, _ := NewHTTPWriter(HTTPWriterConfig{TableWriterURL: srv.URL})
			err := wr.Write(context.Background(), "c", "n", "t", pipelineplan.WriteModeAppend, sampleRows())
			if !errors.Is(err, ErrSchemaMismatch) {
				t.Fatalf("expected ErrSchemaMismatch, got %v", err)
			}
		})
	}
}

func TestHTTPWriter_5xxMappedToErrCommitFailed(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	wr, _ := NewHTTPWriter(HTTPWriterConfig{TableWriterURL: srv.URL})
	err := wr.Write(context.Background(), "c", "n", "t", pipelineplan.WriteModeAppend, sampleRows())
	if !errors.Is(err, ErrCommitFailed) {
		t.Fatalf("expected ErrCommitFailed, got %v", err)
	}
}

func TestNewHTTPWriter_validatesURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		url  string
	}{
		{"empty", ""},
		{"no scheme", "iceberg-catalog-service:8080"},
		{"only scheme", "http://"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewHTTPWriter(HTTPWriterConfig{TableWriterURL: tc.url}); err == nil {
				t.Errorf("NewHTTPWriter(%q) returned nil error", tc.url)
			}
		})
	}
}
