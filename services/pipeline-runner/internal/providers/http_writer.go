package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	pipelineplan "github.com/openfoundry/openfoundry-go/libs/pipeline-plan"
	pipelineruntime "github.com/openfoundry/openfoundry-go/libs/pipeline-runtime"
)

// HTTPWriter is the pipelineruntime.Writer that posts row batches to
// the OpenFoundry Iceberg HTTP append adapter served by
// iceberg-catalog-service (`POST /openfoundry/iceberg/v1/append`).
// Same pattern services/audit-sink and services/ai-sink (Phase B)
// use; apache/iceberg-go's write-side is not stable enough for
// direct use (ADR-0045 § Consequences).
type HTTPWriter struct {
	base          *url.URL
	client        *http.Client
	internalToken string
	// catalog & warehouse are forwarded to the adapter so it can
	// resolve the Iceberg table metadata.
	catalogURL string
	warehouse  string
}

// HTTPWriterConfig — TableWriterURL is the iceberg-catalog-service
// base URL; CatalogURL + Warehouse are forwarded so the adapter can
// load the target table; InternalToken is the optional shared secret
// the dev cluster uses.
type HTTPWriterConfig struct {
	TableWriterURL string
	CatalogURL     string
	Warehouse      string
	InternalToken  string
	// Timeout per HTTP request. Defaults to 30s.
	Timeout time.Duration
}

// NewHTTPWriter validates the URLs and returns a ready writer.
func NewHTTPWriter(cfg HTTPWriterConfig) (*HTTPWriter, error) {
	if strings.TrimSpace(cfg.TableWriterURL) == "" {
		return nil, fmt.Errorf("http writer: TableWriterURL must not be empty")
	}
	u, err := url.Parse(strings.TrimRight(cfg.TableWriterURL, "/"))
	if err != nil {
		return nil, fmt.Errorf("http writer: parse TableWriterURL: %w", err)
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("http writer: TableWriterURL %q must include scheme and host", cfg.TableWriterURL)
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &HTTPWriter{
		base:          u,
		client:        &http.Client{Timeout: timeout},
		internalToken: cfg.InternalToken,
		catalogURL:    strings.TrimRight(cfg.CatalogURL, "/"),
		warehouse:     cfg.Warehouse,
	}, nil
}

// ErrTableNotFound surfaces a 404 from the adapter (the target table
// does not exist; create it before running this Plan).
var ErrTableNotFound = errors.New("iceberg adapter: table not found")

// ErrSchemaMismatch surfaces 409/422 (the rows do not match the
// table schema the adapter resolved).
var ErrSchemaMismatch = errors.New("iceberg adapter: schema mismatch")

// ErrCommitFailed is the umbrella for transport errors and any
// other non-2xx response.
var ErrCommitFailed = errors.New("iceberg adapter: commit failed")

// HTTPError carries the raw status code + body snippet for the
// runtime to log; ErrCommitFailed / ErrSchemaMismatch /
// ErrTableNotFound wrap it when relevant.
type HTTPError struct {
	StatusCode int
	Body       string
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("iceberg adapter HTTP %d: %s", e.StatusCode, e.Body)
}

type appendSpec struct {
	Catalog            string   `json:"catalog"`
	CatalogURL         string   `json:"catalog_url,omitempty"`
	Warehouse          string   `json:"warehouse,omitempty"`
	Namespace          string   `json:"namespace"`
	Table              string   `json:"table"`
	Mode               string   `json:"mode"`
	PartitionTransform string   `json:"partition_transform,omitempty"`
	SortOrder          string   `json:"sort_order,omitempty"`
	Schema             []string `json:"schema,omitempty"` // empty — adapter infers / validates
}

type appendBatch struct {
	Spec appendSpec        `json:"spec"`
	Rows []map[string]any  `json:"rows"`
}

// Write implements pipelineruntime.Writer. Posts one batch per call;
// empty batches short-circuit (a Plan that yields zero rows is valid
// and may legitimately want to publish an empty snapshot — the
// adapter handles that case).
func (w *HTTPWriter) Write(ctx context.Context, catalog, namespace, table string, mode pipelineplan.WriteMode, rows []pipelineruntime.Row) error {
	out := make([]map[string]any, len(rows))
	for i, r := range rows {
		out[i] = map[string]any(r)
	}
	body := appendBatch{
		Spec: appendSpec{
			Catalog:    catalog,
			CatalogURL: w.catalogURL,
			Warehouse:  w.warehouse,
			Namespace:  namespace,
			Table:      table,
			Mode:       string(mode),
		},
		Rows: out,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("http writer: marshal batch: %w", err)
	}
	urlPath := w.base.String() + "/openfoundry/iceberg/v1/append"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, urlPath, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("http writer: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if w.internalToken != "" {
		req.Header.Set("X-Internal-Token", w.internalToken)
	}
	resp, err := w.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCommitFailed, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 4*1024))
	hErr := &HTTPError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(snippet))}
	switch resp.StatusCode {
	case http.StatusNotFound:
		return fmt.Errorf("%w: %s", ErrTableNotFound, hErr)
	case http.StatusConflict, http.StatusUnprocessableEntity:
		return fmt.Errorf("%w: %s", ErrSchemaMismatch, hErr)
	default:
		return fmt.Errorf("%w: %s", ErrCommitFailed, hErr)
	}
}
