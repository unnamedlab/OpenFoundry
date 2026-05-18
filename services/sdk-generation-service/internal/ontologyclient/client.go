package ontologyclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Client is the contract the generator depends on. Production uses
// HTTPClient (talks to the gateway); tests and the dev workflow use
// StubClient.
type Client interface {
	GetOntologySnapshot(ctx context.Context, tenantID uuid.UUID, version string) (*OntologySnapshot, error)
}

// ErrSnapshotNotFound is returned when the upstream returns 404 for
// the requested (tenant, version) pair. Handlers translate this into a
// 404 SDKBuild response so callers can distinguish "missing snapshot"
// from "upstream broken".
var ErrSnapshotNotFound = errors.New("ontology snapshot not found")

// HTTPClient talks to the gateway-fronted ontology-definition-service.
//
// TODO(osdk): the actual endpoint isn't implemented yet on the
// producer side — ontology-definition-service exposes CRUD on object
// types / link types / property metadata, but no "give me a versioned
// snapshot of the whole catalog" route. Until that endpoint lands the
// generator wires StubClient in main.go when ONTOLOGY_SERVICE_URL is
// empty (dev) and HTTPClient when it is set (prod). The expected
// endpoint is:
//
//	GET {ONTOLOGY_SERVICE_URL}/api/v1/ontology/snapshot?version=<v>
//	Authorization: Bearer <service token>
//
// returning a JSON body shaped like OntologySnapshot above.
type HTTPClient struct {
	// BaseURL points at the gateway (preferred) or directly at the
	// ontology-definition-service. No trailing slash.
	BaseURL string
	// Token is the bearer token forwarded with every request. Empty
	// means no Authorization header — fine for in-cluster setups where
	// network policy fronts the gate.
	Token string
	// HTTP is the transport used. nil means use a default 30s client.
	HTTP *http.Client
}

// GetOntologySnapshot fetches the snapshot for (tenantID, version) over HTTP.
func (c *HTTPClient) GetOntologySnapshot(ctx context.Context, tenantID uuid.UUID, version string) (*OntologySnapshot, error) {
	if c.BaseURL == "" {
		return nil, errors.New("ontologyclient: BaseURL is required")
	}
	if strings.TrimSpace(version) == "" {
		return nil, errors.New("ontologyclient: version is required")
	}
	q := url.Values{}
	q.Set("version", version)
	if tenantID != uuid.Nil {
		q.Set("tenant_id", tenantID.String())
	}
	endpoint := strings.TrimRight(c.BaseURL, "/") + "/api/v1/ontology/snapshot?" + q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	httpClient := c.HTTP
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 30 * time.Second}
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ontology snapshot request: %w", err)
	}
	defer resp.Body.Close()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return nil, ErrSnapshotNotFound
	case resp.StatusCode < 200 || resp.StatusCode >= 300:
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("ontology snapshot: status=%d body=%s", resp.StatusCode, string(body))
	}

	var snap OntologySnapshot
	if err := json.NewDecoder(resp.Body).Decode(&snap); err != nil {
		return nil, fmt.Errorf("decode snapshot: %w", err)
	}
	if snap.Version == "" {
		snap.Version = version
	}
	return &snap, nil
}
