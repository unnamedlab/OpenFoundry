package connectorresolver

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/connectorclient"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/models"
)

func TestNewReturnsNilWhenClientIsNil(t *testing.T) {
	t.Parallel()
	assert.Nil(t, New(nil))
}

func TestStripExternalSchemeMatchesRustSemantics(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "key.png",
		stripExternalScheme("s3://bucket/key.png"))
	assert.Equal(t, "deep/nested/key",
		stripExternalScheme("s3://bucket/deep/nested/key"))
	assert.Equal(t, "no-scheme/path", stripExternalScheme("no-scheme/path"))
	assert.Equal(t, "", stripExternalScheme("scheme://host"))
}

func TestResolveBuildsExternalURL(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"endpoint":"https://external.example/bucket"}`))
	}))
	t.Cleanup(srv.Close)

	src := "ri.foundry.main.source.abc"
	r := New(connectorclient.New(srv.URL))
	set := &models.MediaSet{RID: "ri.set.1", SourceRID: &src, Virtual: true}
	item := &models.MediaItem{
		StorageURI: "s3://external/key/object.png",
	}
	url, err := r.Resolve(context.Background(), set, item, 30*time.Second)
	require.NoError(t, err)
	require.NotNil(t, url)
	assert.True(t, strings.HasPrefix(url.URL, "https://external.example/bucket/key/object.png?expires="),
		"got %q", url.URL)
	assert.WithinDuration(t, time.Now().Add(30*time.Second), url.ExpiresAt, 5*time.Second)
}

func TestResolveRejectsMissingSourceRID(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("connector should not be hit")
	}))
	t.Cleanup(srv.Close)

	r := New(connectorclient.New(srv.URL))
	set := &models.MediaSet{RID: "ri.set.1"}
	_, err := r.Resolve(context.Background(), set, &models.MediaItem{}, time.Minute)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no source_rid")
}

func TestResolvePropagatesConnectorErrors(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	src := "ri.x"
	r := New(connectorclient.New(srv.URL))
	set := &models.MediaSet{SourceRID: &src}
	_, err := r.Resolve(context.Background(), set, &models.MediaItem{}, time.Minute)
	require.Error(t, err)
}
