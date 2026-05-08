// Package connectorclient is the HTTP client media-sets-service uses
// to resolve external endpoints for *virtual* media sets — sets whose
// bytes live in a customer source system (S3, Snowflake, …) rather
// than in OpenFoundry storage.
//
// The wire shape mirrors the Rust `resolve_virtual_download_url`
// helper in services/media-sets-service/src/handlers/items.rs:
//
//	GET {connector_service_url}/sources/{source_rid}
//	200 OK {"endpoint": "https://my-bucket.s3.amazonaws.com"}
//
// The client is intentionally tiny — the full
// connector-management-service has its own port; we only need the
// `endpoint` field today, so re-declaring the descriptor inline keeps
// the dep graph one-way.
package connectorclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// SourceDescriptor is the partial view of /sources/{rid} the resolver
// reads. The connector service returns more fields; we only consume
// what the URL synthesis needs.
type SourceDescriptor struct {
	Endpoint string `json:"endpoint"`
}

// Client is the typed HTTP client. Construct with New.
type Client struct {
	baseURL    string
	httpClient *http.Client
}

// Option mutates a Client at construction time.
type Option func(*Client)

// WithHTTPClient overrides the default 10-second client. Used by tests.
func WithHTTPClient(c *http.Client) Option {
	return func(cl *Client) { cl.httpClient = c }
}

// New builds a Client. baseURL is the connector service base
// (e.g. "http://connector-management-service:50130") with no trailing
// slash; the helper appends "/sources/{rid}".
func New(baseURL string, opts ...Option) *Client {
	c := &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// ResolveSource fetches the descriptor for `sourceRID`. Callers map
// the network / 4xx / 5xx errors to the wire-format the REST and
// gRPC layers surface (`UpstreamUnavailable` on the Rust side).
func (c *Client) ResolveSource(ctx context.Context, sourceRID string) (*SourceDescriptor, error) {
	if sourceRID == "" {
		return nil, errors.New("connectorclient: source rid is empty")
	}
	if c.baseURL == "" {
		return nil, errors.New("connectorclient: base url is not configured")
	}
	url := c.baseURL + "/sources/" + encodePathSegment(sourceRID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("connectorclient: build request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("connectorclient: lookup transport: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("connectorclient: read response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("connectorclient: lookup returned HTTP %d", resp.StatusCode)
	}
	var out SourceDescriptor
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("connectorclient: decode response: %w", err)
	}
	if out.Endpoint == "" {
		return nil, errors.New("connectorclient: response has empty endpoint")
	}
	return &out, nil
}

// encodePathSegment percent-encodes a single path segment safely. RIDs
// like "ri.foundry.main.source.<uuid>" survive intact (`.` is left as
// is per RFC 3986), but `/` is encoded so a hostile RID can't break
// out of the segment.
func encodePathSegment(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case (c >= 'a' && c <= 'z'), (c >= 'A' && c <= 'Z'),
			(c >= '0' && c <= '9'),
			c == '-', c == '_', c == '.', c == '~', c == ':':
			out = append(out, c)
		default:
			out = append(out, []byte(fmt.Sprintf("%%%02X", c))...)
		}
	}
	return string(out)
}
