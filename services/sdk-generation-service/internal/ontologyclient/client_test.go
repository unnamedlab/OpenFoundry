package ontologyclient_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/sdk-generation-service/internal/ontologyclient"
)

func TestHTTPClient_Success(t *testing.T) {
	t.Parallel()
	wantTenant := uuid.New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/ontology/snapshot" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if v := r.URL.Query().Get("version"); v != "v9" {
			t.Errorf("version query = %q, want v9", v)
		}
		if v := r.URL.Query().Get("tenant_id"); v != wantTenant.String() {
			t.Errorf("tenant_id query = %q, want %q", v, wantTenant)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer tok" {
			t.Errorf("auth header = %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(ontologyclient.OntologySnapshot{
			Version: "v9",
			ObjectTypes: []ontologyclient.OntologyObjectType{
				{Name: "Foo", APIName: "foo", Properties: []ontologyclient.OntologyProperty{
					{Name: "id", PropertyType: "string", Required: true},
				}},
			},
		})
	}))
	defer srv.Close()

	c := &ontologyclient.HTTPClient{BaseURL: srv.URL, Token: "tok"}
	snap, err := c.GetOntologySnapshot(context.Background(), wantTenant, "v9")
	if err != nil {
		t.Fatalf("get snapshot: %v", err)
	}
	if snap.Version != "v9" {
		t.Errorf("version = %q", snap.Version)
	}
	if len(snap.ObjectTypes) != 1 || snap.ObjectTypes[0].Name != "Foo" {
		t.Errorf("unexpected object types: %+v", snap.ObjectTypes)
	}
}

func TestHTTPClient_NotFound(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	defer srv.Close()

	c := &ontologyclient.HTTPClient{BaseURL: srv.URL}
	_, err := c.GetOntologySnapshot(context.Background(), uuid.New(), "v1")
	if !errors.Is(err, ontologyclient.ErrSnapshotNotFound) {
		t.Fatalf("expected ErrSnapshotNotFound, got %v", err)
	}
}

func TestHTTPClient_BadStatus(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := &ontologyclient.HTTPClient{BaseURL: srv.URL}
	if _, err := c.GetOntologySnapshot(context.Background(), uuid.New(), "v1"); err == nil {
		t.Fatalf("expected error on 500")
	}
}

func TestStubClient_OverridesVersion(t *testing.T) {
	t.Parallel()
	s := &ontologyclient.StubClient{}
	snap, err := s.GetOntologySnapshot(context.Background(), uuid.New(), "v42")
	if err != nil {
		t.Fatalf("stub: %v", err)
	}
	if snap.Version != "v42" {
		t.Errorf("version = %q, want v42", snap.Version)
	}
	if len(snap.ObjectTypes) == 0 {
		t.Errorf("expected default object types")
	}
}
