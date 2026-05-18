package writer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/openfoundry/openfoundry-go/services/action-log-sink/internal/envelope"
)

const (
	actionCatalog            = "lakekeeper"
	actionNamespace          = "default"
	actionTable              = "action_log"
	actionPartitionTransform = "day(applied_at_ms)"
	actionSortOrder          = "applied_at_ms ASC"
)

// FieldSpec / TableSpec / AppendBatch / catalog interfaces mirror the
// shape audit-sink and ai-sink already use against
// iceberg-catalog-service. Keeping the JSON tag layout identical
// means the catalog handler treats all three sinks uniformly.
type FieldSpec struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

type TableSpec struct {
	Catalog            string      `json:"catalog"`
	CatalogURL         string      `json:"catalog_url,omitempty"`
	Warehouse          string      `json:"warehouse,omitempty"`
	Namespace          string      `json:"namespace"`
	Table              string      `json:"table"`
	PartitionTransform string      `json:"partition_transform"`
	SortOrder          string      `json:"sort_order"`
	Schema             []FieldSpec `json:"schema"`
}

type AppendBatch struct {
	Spec TableSpec        `json:"spec"`
	Rows []map[string]any `json:"rows"`
}

// IcebergCatalog is the seam tests inject via NewIcebergWriterWithCatalog.
type IcebergCatalog interface {
	LoadTable(ctx context.Context, spec TableSpec) (IcebergTableAppender, error)
}

// IcebergTableAppender is the per-table writer the catalog returns.
type IcebergTableAppender interface {
	Append(ctx context.Context, batch AppendBatch) error
}

// Now is the timestamp source for the `kafka_ts` column. Overridden
// in tests; production keeps the default.
var Now = func() time.Time { return time.Now().UTC() }

// IcebergWriter buffers a batch and posts it to the OpenFoundry
// Iceberg HTTP append adapter served by iceberg-catalog-service.
type IcebergWriter struct {
	CatalogURL string
	Warehouse  string
	catalog    IcebergCatalog
}

// NewIcebergWriter wires the default HTTP adapter. `tableWriterURL`
// is the base URL of iceberg-catalog-service (the
// /openfoundry/iceberg/v1/append endpoint); `catalogURL` is the
// Lakekeeper REST URL forwarded to the adapter so it can load the
// table metadata.
func NewIcebergWriter(catalogURL, tableWriterURL, warehouse string) *IcebergWriter {
	return NewIcebergWriterWithCatalog(catalogURL, warehouse,
		NewHTTPTableWriterCatalog(tableWriterURL, catalogURL, warehouse))
}

// NewIcebergWriterWithCatalog injects a custom catalog (fakes in
// tests, future adapters in prod).
func NewIcebergWriterWithCatalog(catalogURL, warehouse string, catalog IcebergCatalog) *IcebergWriter {
	return &IcebergWriter{
		CatalogURL: catalogURL,
		Warehouse:  warehouse,
		catalog:    catalog,
	}
}

// Append converts the in-memory batch into the wire AppendBatch and
// posts it via the catalog seam. Empty batches return ErrEmptyBatch.
func (w *IcebergWriter) Append(ctx context.Context, batch []envelope.ActionEnvelope) error {
	if len(batch) == 0 {
		return ErrEmptyBatch
	}
	spec := w.tableSpec()
	table, err := w.catalog.LoadTable(ctx, spec)
	if err != nil {
		return err
	}
	kafkaTSMicros := Now().UnixMicro()
	rows := make([]map[string]any, 0, len(batch))
	for _, e := range batch {
		rows = append(rows, envelopeToRow(e, kafkaTSMicros))
	}
	return table.Append(ctx, AppendBatch{Spec: spec, Rows: rows})
}

// Close is a no-op for the HTTP-adapter implementation.
func (w *IcebergWriter) Close() error { return nil }

func (w *IcebergWriter) tableSpec() TableSpec {
	return TableSpec{
		Catalog:            actionCatalog,
		CatalogURL:         w.CatalogURL,
		Warehouse:          w.Warehouse,
		Namespace:          actionNamespace,
		Table:              actionTable,
		PartitionTransform: actionPartitionTransform,
		SortOrder:          actionSortOrder,
		Schema:             ActionSchema(),
	}
}

// ActionSchema returns the canonical 16-column schema mirrored from
// the Iceberg DDL in infra/dev/action-log-sink.yaml header. Exported
// so callers can introspect (admin tooling, fixtures).
func ActionSchema() []FieldSpec {
	return []FieldSpec{
		{ID: 1, Name: "event_id", Type: "string", Required: true},
		{ID: 2, Name: "action_type_id", Type: "string", Required: true},
		{ID: 3, Name: "action_name", Type: "string", Required: true},
		{ID: 4, Name: "object_type_id", Type: "string", Required: true},
		{ID: 5, Name: "object_id", Type: "string", Required: false},
		{ID: 6, Name: "tenant", Type: "string", Required: true},
		{ID: 7, Name: "actor_sub", Type: "string", Required: true},
		{ID: 8, Name: "actor_email", Type: "string", Required: false},
		{ID: 9, Name: "organization_id", Type: "string", Required: false},
		{ID: 10, Name: "status", Type: "string", Required: true},
		{ID: 11, Name: "parameters", Type: "string", Required: false},
		{ID: 12, Name: "previous_state", Type: "string", Required: false},
		{ID: 13, Name: "new_state", Type: "string", Required: false},
		{ID: 14, Name: "target_classification", Type: "string", Required: false},
		{ID: 15, Name: "applied_at_ms", Type: "long", Required: true},
		{ID: 16, Name: "kafka_ts", Type: "timestamptz", Required: false},
	}
}

func envelopeToRow(e envelope.ActionEnvelope, kafkaTSMicros int64) map[string]any {
	row := map[string]any{
		"event_id":              e.EventID,
		"action_type_id":        e.ActionTypeID,
		"action_name":           e.ActionName,
		"object_type_id":        e.ObjectTypeID,
		"object_id":             nullableString(e.ObjectID),
		"tenant":                e.Tenant,
		"actor_sub":             e.ActorSub,
		"actor_email":           nullableString(e.ActorEmail),
		"organization_id":       nullableString(e.OrganizationID),
		"status":                e.Status,
		"parameters":            nullableString(e.Parameters),
		"previous_state":        nullableString(e.PreviousState),
		"new_state":             nullableString(e.NewState),
		"target_classification": nullableString(e.TargetClassification),
		"applied_at_ms":         e.AppliedAtMs,
		"kafka_ts":              kafkaTSMicros,
	}
	return row
}

func nullableString(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

// ---- HTTP adapter (default IcebergCatalog impl) ----

type HTTPTableWriterCatalog struct {
	baseURL    string
	catalogURL string
	warehouse  string
	client     *http.Client
}

func NewHTTPTableWriterCatalog(tableWriterURL, catalogURL, warehouse string) *HTTPTableWriterCatalog {
	return &HTTPTableWriterCatalog{
		baseURL:    strings.TrimRight(tableWriterURL, "/"),
		catalogURL: strings.TrimRight(catalogURL, "/"),
		warehouse:  warehouse,
		client:     &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *HTTPTableWriterCatalog) LoadTable(_ context.Context, spec TableSpec) (IcebergTableAppender, error) {
	if c.baseURL == "" {
		return nil, fmt.Errorf("%w: table-writer URL must be non-empty", ErrCommitFailed)
	}
	if spec.CatalogURL == "" {
		spec.CatalogURL = c.catalogURL
	}
	if spec.Warehouse == "" {
		spec.Warehouse = c.warehouse
	}
	return &httpTableWriter{catalog: c, spec: spec}, nil
}

type httpTableWriter struct {
	catalog *HTTPTableWriterCatalog
	spec    TableSpec
}

func (t *httpTableWriter) Append(ctx context.Context, batch AppendBatch) error {
	if batch.Spec.Catalog == "" || batch.Spec.Namespace == "" || batch.Spec.Table == "" {
		batch.Spec = t.spec
	}
	if batch.Spec.CatalogURL == "" {
		batch.Spec.CatalogURL = t.spec.CatalogURL
	}
	if batch.Spec.Warehouse == "" {
		batch.Spec.Warehouse = t.spec.Warehouse
	}
	body, err := json.Marshal(batch)
	if err != nil {
		return err
	}
	url := t.catalog.baseURL + "/openfoundry/iceberg/v1/append"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.catalog.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCommitFailed, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		_, _ = io.Copy(io.Discard, resp.Body)
		return nil
	}
	detail := tableWriterErrorDetail(resp)
	switch resp.StatusCode {
	case http.StatusNotFound:
		return fmt.Errorf("%w%s", ErrTableNotFound, detail)
	case http.StatusConflict, http.StatusUnprocessableEntity:
		return fmt.Errorf("%w%s", ErrSchemaMismatch, detail)
	default:
		return fmt.Errorf("%w: HTTP %d from table-writer adapter%s", ErrCommitFailed, resp.StatusCode, detail)
	}
}

func tableWriterErrorDetail(resp *http.Response) string {
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4*1024))
	if err != nil || len(bytes.TrimSpace(body)) == 0 {
		return ""
	}
	return ": " + string(bytes.TrimSpace(body))
}
