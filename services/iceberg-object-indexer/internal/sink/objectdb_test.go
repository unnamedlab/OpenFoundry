package sink

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestObjectDB_PutSuccess(t *testing.T) {
	t.Parallel()
	var (
		gotPath  string
		gotMethod string
		gotCT    string
		gotToken string
		gotBody  []byte
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotCT = r.Header.Get("Content-Type")
		gotToken = r.Header.Get("X-Internal-Token")
		gotBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c, err := NewObjectDB(srv.URL, "internal-secret", 2*time.Second)
	if err != nil {
		t.Fatalf("NewObjectDB: %v", err)
	}
	if err := c.Put(context.Background(), "default", "tx-1", []byte(`{"hello":"world"}`)); err != nil {
		t.Fatalf("Put error: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %s, want PUT", gotMethod)
	}
	if gotPath != "/api/v1/object-database/objects/default/tx-1" {
		t.Errorf("path = %q", gotPath)
	}
	if gotCT != "application/json" {
		t.Errorf("content-type = %q", gotCT)
	}
	if gotToken != "internal-secret" {
		t.Errorf("X-Internal-Token = %q", gotToken)
	}
	if string(gotBody) != `{"hello":"world"}` {
		t.Errorf("body = %s", gotBody)
	}
}

func TestObjectDB_PutOmitsTokenWhenEmpty(t *testing.T) {
	t.Parallel()
	var gotToken string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("X-Internal-Token")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, err := NewObjectDB(srv.URL, "", 0) // empty token, default timeout
	if err != nil {
		t.Fatalf("NewObjectDB: %v", err)
	}
	if err := c.Put(context.Background(), "t", "id", []byte("{}")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if gotToken != "" {
		t.Errorf("X-Internal-Token should be absent, got %q", gotToken)
	}
}

func TestObjectDB_PutSurfacesHTTPError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = w.Write([]byte("schema mismatch: foo"))
	}))
	defer srv.Close()

	c, _ := NewObjectDB(srv.URL, "", 0)
	err := c.Put(context.Background(), "t", "id", []byte("{}"))
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("expected *HTTPError, got %T (%v)", err, err)
	}
	if httpErr.StatusCode != http.StatusUnprocessableEntity {
		t.Errorf("StatusCode = %d, want 422", httpErr.StatusCode)
	}
	if !strings.Contains(httpErr.Body, "schema mismatch") {
		t.Errorf("Body = %q", httpErr.Body)
	}
}

func TestObjectDB_PutEscapesPathSegments(t *testing.T) {
	t.Parallel()
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Use RawPath so URL-escaped characters are preserved.
		if r.URL.RawPath != "" {
			gotPath = r.URL.RawPath
		} else {
			gotPath = r.URL.Path
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c, _ := NewObjectDB(srv.URL, "", 0)
	// id contains characters that must be percent-escaped.
	if err := c.Put(context.Background(), "tenant a", "id/with slash", []byte("{}")); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !strings.Contains(gotPath, "tenant%20a") || !strings.Contains(gotPath, "id%2Fwith%20slash") {
		t.Errorf("path = %q (must escape segments)", gotPath)
	}
}

func TestNewObjectDB_validatesURL(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		url  string
	}{
		{"empty", ""},
		{"no scheme", "object-database-service:8080"},
		{"only scheme", "http://"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewObjectDB(tc.url, "", 0); err == nil {
				t.Errorf("NewObjectDB(%q) returned nil error", tc.url)
			}
		})
	}
}
