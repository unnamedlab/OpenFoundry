package sink

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ObjectDB PUTs row bodies to object-database-service over HTTP. The
// path shape matches the Scala IcebergToObjectStoreIndexer:
//   PUT {base}/api/v1/object-database/objects/{tenant}/{id}
type ObjectDB struct {
	base          *url.URL
	client        *http.Client
	internalToken string
}

// NewObjectDB validates the base URL and returns a ready ObjectDB.
// `internalToken` is forwarded as the X-Internal-Token header when
// non-empty (matches the Scala behaviour).
func NewObjectDB(baseURL, internalToken string, timeout time.Duration) (*ObjectDB, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, fmt.Errorf("object-database-url must not be empty")
	}
	u, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("parse object-database-url: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("object-database-url %q must include scheme and host", baseURL)
	}
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	return &ObjectDB{
		base:          u,
		client:        &http.Client{Timeout: timeout},
		internalToken: internalToken,
	}, nil
}

// Put performs the PUT. Non-2xx responses surface as *HTTPError so
// the runner can decide whether to keep going (per-row failure) or
// abort. Transport errors are returned as-is and treated as fatal.
func (s *ObjectDB) Put(ctx context.Context, tenant, id string, body []byte) error {
	path := fmt.Sprintf("/api/v1/object-database/objects/%s/%s",
		url.PathEscape(tenant), url.PathEscape(id))
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, s.base.String()+path, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build put request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if s.internalToken != "" {
		req.Header.Set("X-Internal-Token", s.internalToken)
	}
	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("put %s: %w", path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024))
	return &HTTPError{
		StatusCode: resp.StatusCode,
		Body:       strings.TrimSpace(string(snippet)),
	}
}
