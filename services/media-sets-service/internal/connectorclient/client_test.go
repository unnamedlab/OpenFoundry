package connectorclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveSourceHappyPath(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Contains(t, r.URL.Path, "/sources/ri.foundry.main.source.123")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"endpoint": "https://my-bucket.s3.amazonaws.com"}`))
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL)
	desc, err := c.ResolveSource(context.Background(), "ri.foundry.main.source.123")
	require.NoError(t, err)
	assert.Equal(t, "https://my-bucket.s3.amazonaws.com", desc.Endpoint)
}

func TestResolveSourceRejectsEmptyRID(t *testing.T) {
	t.Parallel()
	c := New("http://gw")
	_, err := c.ResolveSource(context.Background(), "")
	require.Error(t, err)
}

func TestResolveSourceRejectsEmptyBaseURL(t *testing.T) {
	t.Parallel()
	c := New("")
	_, err := c.ResolveSource(context.Background(), "ri.x")
	require.Error(t, err)
}

func TestResolveSource4xxSurfacedAsError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL)
	_, err := c.ResolveSource(context.Background(), "ri.x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestResolveSource5xxSurfacedAsError(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL)
	_, err := c.ResolveSource(context.Background(), "ri.x")
	require.Error(t, err)
}

func TestResolveSourceRejectsEmptyEndpointResponse(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL)
	_, err := c.ResolveSource(context.Background(), "ri.x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty endpoint")
}

func TestResolveSourcePercentEncodesPathSegment(t *testing.T) {
	t.Parallel()
	var seen string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.URL.RawPath
		if seen == "" {
			seen = r.URL.Path
		}
		_, _ = w.Write([]byte(`{"endpoint":"https://x"}`))
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL)
	_, err := c.ResolveSource(context.Background(), "ri/with/slash and space")
	require.NoError(t, err)
	// Slashes inside the RID must be percent-encoded so a hostile
	// RID can't escape the segment.
	assert.Contains(t, seen, "%2F")
	assert.Contains(t, seen, "%20")
}

func TestResolveSourceContextCancel(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"endpoint":"https://x"}`))
	}))
	t.Cleanup(srv.Close)

	c := New(srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.ResolveSource(ctx, "ri.x")
	require.Error(t, err)
}
