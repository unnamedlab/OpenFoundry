package storage

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/mediapath"
)

func TestPresignUploadCarriesExpiryAndSignature(t *testing.T) {
	t.Parallel()
	b, err := NewHMACBackend("media", "https://gw.example.test", []byte("secret"))
	require.NoError(t, err)

	key := mediapath.New("ri.set.1", "main", "deadbeef00")
	url1, err := b.PresignUpload(context.Background(), key, "image/png", 30*time.Second)
	require.NoError(t, err)
	assert.WithinDuration(t, time.Now().Add(30*time.Second), url1.ExpiresAt, 5*time.Second)
	assert.Contains(t, url1.URL, "/media/media-sets/ri.set.1/main/de/deadbeef00")
	assert.Contains(t, url1.URL, "expires=")
	assert.Contains(t, url1.URL, "signature=")
	require.Len(t, url1.Headers, 1)
	assert.Equal(t, "Content-Type", url1.Headers[0].Name)
}

func TestPresignSignatureChangesPerMethod(t *testing.T) {
	t.Parallel()
	b, _ := NewHMACBackend("media", "https://gw", []byte("secret"))
	key := mediapath.New("rid", "main", "abc")
	put, _ := b.PresignUpload(context.Background(), key, "image/png", time.Minute)
	get, _ := b.PresignDownload(context.Background(), key, time.Minute)
	pSig, _ := url.Parse(put.URL)
	gSig, _ := url.Parse(get.URL)
	assert.NotEqual(t, pSig.Query().Get("signature"), gSig.Query().Get("signature"),
		"PUT and GET must produce distinct signatures so a stolen GET URL can't replay as a PUT")
}

func TestNewHMACBackendValidatesArguments(t *testing.T) {
	t.Parallel()
	_, err := NewHMACBackend("", "https://gw", []byte("s"))
	require.Error(t, err)
	_, err = NewHMACBackend("b", "", []byte("s"))
	require.Error(t, err)
	_, err = NewHMACBackend("b", "https://gw", nil)
	require.Error(t, err)
}

func TestPresignDefaultTTL(t *testing.T) {
	t.Parallel()
	b, _ := NewHMACBackend("media", "https://gw", []byte("secret"))
	url1, err := b.PresignUpload(context.Background(), mediapath.New("rid", "main", "abc"), "image/png", 0)
	require.NoError(t, err)
	// 0 → defaults to 5 min, never zero.
	assert.WithinDuration(t, time.Now().Add(5*time.Minute), url1.ExpiresAt, 10*time.Second)
}

func TestEndpointTrailingSlashHandling(t *testing.T) {
	t.Parallel()
	// We don't normalise trailing slashes — the URL should still be
	// well-formed since the bucket join uses a single "/" separator.
	b, _ := NewHMACBackend("media", "https://gw.example", []byte("secret"))
	u, _ := b.PresignDownload(context.Background(), mediapath.New("rid", "main", "deadbeef"), 30*time.Second)
	parsed, err := url.Parse(u.URL)
	require.NoError(t, err)
	assert.Equal(t, "gw.example", parsed.Host)
	assert.True(t, strings.HasPrefix(parsed.Path, "/media/media-sets/rid/main/de/deadbeef"))
}
