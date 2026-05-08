// Package connectorresolver adapts connectorclient.Client to the
// mediaitems.VirtualResolver interface so the download path can mint
// external URLs for virtual media sets without a circular import.
//
// Mirrors the Rust `resolve_virtual_download_url` helper:
//
//   - Look up the descriptor for `set.source_rid`.
//   - Strip the `<scheme>://<host>/` prefix from `item.storage_uri`.
//   - Build "<endpoint>/<path>?expires=<epoch>".
package connectorresolver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/connectorclient"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/models"
	"github.com/openfoundry/openfoundry-go/services/media-sets-service/internal/storage"
)

// Resolver wraps a connectorclient.Client.
type Resolver struct {
	Client *connectorclient.Client
}

// New builds a Resolver. Returns nil if `c` is nil so callers can
// pass the result directly to mediaitems.Service.VirtualResolver
// without worrying about nil-vs-empty-impl edge cases.
func New(c *connectorclient.Client) *Resolver {
	if c == nil {
		return nil
	}
	return &Resolver{Client: c}
}

// Resolve produces an external download URL for `item`, valid for
// `ttl`. Returns an error wrapped with the Rust-style
// "UpstreamUnavailable" semantics so the HTTP layer can surface 503.
func (r *Resolver) Resolve(ctx context.Context, set *models.MediaSet, item *models.MediaItem, ttl time.Duration) (*storage.PresignedURL, error) {
	if set.SourceRID == nil || *set.SourceRID == "" {
		return nil, fmt.Errorf("virtual media set `%s` has no source_rid", set.RID)
	}
	desc, err := r.Client.ResolveSource(ctx, *set.SourceRID)
	if err != nil {
		return nil, fmt.Errorf("connectorresolver: lookup: %w", err)
	}
	if desc.Endpoint == "" {
		return nil, errors.New("connectorresolver: empty endpoint")
	}
	pathInSource := stripExternalScheme(item.StorageURI)
	expires := time.Now().Add(ttl).UTC()
	url := fmt.Sprintf("%s/%s?expires=%d",
		strings.TrimRight(desc.Endpoint, "/"),
		strings.TrimLeft(pathInSource, "/"),
		expires.Unix(),
	)
	return &storage.PresignedURL{URL: url, ExpiresAt: expires}, nil
}

// stripExternalScheme drops a leading "<scheme>://<host>/" prefix
// from `uri` so we can re-attach the connector's endpoint cleanly.
// Mirrors the Rust `strip_external_scheme` helper.
func stripExternalScheme(uri string) string {
	if idx := strings.Index(uri, "://"); idx >= 0 {
		rest := uri[idx+3:]
		if slash := strings.IndexByte(rest, '/'); slash >= 0 {
			return rest[slash+1:]
		}
		return ""
	}
	return uri
}
