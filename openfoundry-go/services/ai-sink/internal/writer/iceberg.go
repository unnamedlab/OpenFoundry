package writer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/openfoundry/openfoundry-go/services/ai-sink/internal/envelope"
)

const (
	aiCatalog            = "lakekeeper"
	aiPartitionTransform = "day(at)"
	aiSortOrder          = "at ASC"
)

var (
	ErrEmptyBatch     = errors.New("iceberg append batch is empty")
	ErrTableNotFound  = errors.New("iceberg table not found")
	ErrSchemaMismatch = errors.New("iceberg table schema mismatch")
	ErrCommitFailed   = errors.New("iceberg commit failed")
)

type FieldSpec struct {
	ID       int    `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	Required bool   `json:"required"`
}

type TableSpec struct {
	Catalog            string      `json:"catalog"`
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

type IcebergCatalog interface {
	LoadTable(ctx context.Context, spec TableSpec) (IcebergTableAppender, error)
}

type IcebergTableAppender interface {
	Append(ctx context.Context, batch AppendBatch) error
}

// IcebergWriter appends AI event batches to the four tables under
// `lakekeeper/of_ai/{prompts,responses,evaluations,traces}`.
//
// Contract parity with the Rust sink:
//   - namespace: `of_ai`
//   - tables: prompts, responses, evaluations, traces
//   - schema: event_id uuid, at timestamptz, kind string, run_id uuid?,
//     trace_id string?, producer string, schema_version uint32, payload string
//   - partition/sort: `day(at)`, `at ASC`
//   - batch append: one durable append per non-empty table group
//
// The default implementation is an explicit OpenFoundry HTTP table-writer
// adapter because iceberg-go does not yet provide a stable, complete
// write-side path equivalent to Rust's `append_record_batches`. The adapter
// must atomically write Parquet data files and commit the Iceberg snapshot;
// HTTP 404 maps to ErrTableNotFound, 409/422 to ErrSchemaMismatch, and all
// other non-2xx responses to ErrCommitFailed. Tests can inject a fake catalog
// with NewIcebergWriterWithCatalog.
type IcebergWriter struct {
	CatalogURL string
	Warehouse  string
	Namespace  string
	catalog    IcebergCatalog
}

func NewIcebergWriter(catalogURL, warehouse, namespace string) *IcebergWriter {
	return NewIcebergWriterWithCatalog(
		catalogURL, warehouse, namespace,
		NewHTTPTableWriterCatalog(catalogURL, warehouse),
	)
}

func NewIcebergWriterWithCatalog(catalogURL, warehouse, namespace string, catalog IcebergCatalog) *IcebergWriter {
	return &IcebergWriter{
		CatalogURL: catalogURL,
		Warehouse:  warehouse,
		Namespace:  namespace,
		catalog:    catalog,
	}
}

func (i *IcebergWriter) Append(ctx context.Context, byTable map[string][]envelope.AiEventEnvelope) error {
	if !hasAnyRows(byTable) {
		return ErrEmptyBatch
	}
	for _, tableName := range []string{envelope.TablePrompts, envelope.TableResponses, envelope.TableEvaluations, envelope.TableTraces} {
		events := byTable[tableName]
		if len(events) == 0 {
			continue
		}
		spec := i.tableSpec(tableName)
		table, err := i.catalog.LoadTable(ctx, spec)
		if err != nil {
			return err
		}
		if err := table.Append(ctx, AppendBatch{Spec: spec, Rows: aiRows(events)}); err != nil {
			return err
		}
	}
	return nil
}

func (i *IcebergWriter) Close() error { return nil }

func (i *IcebergWriter) tableSpec(table string) TableSpec {
	return TableSpec{
		Catalog:            aiCatalog,
		Warehouse:          i.Warehouse,
		Namespace:          i.Namespace,
		Table:              table,
		PartitionTransform: aiPartitionTransform,
		SortOrder:          aiSortOrder,
		Schema:             aiSchema(),
	}
}

func aiRows(events []envelope.AiEventEnvelope) []map[string]any {
	rows := make([]map[string]any, 0, len(events))
	for _, event := range events {
		row := map[string]any{
			"event_id":       event.EventID.String(),
			"at":             event.At,
			"kind":           string(event.Kind),
			"run_id":         nil,
			"trace_id":       nil,
			"producer":       event.Producer,
			"schema_version": event.SchemaVersion,
			"payload":        string(event.Payload),
		}
		if event.RunID != nil {
			row["run_id"] = event.RunID.String()
		}
		if event.TraceID != nil {
			row["trace_id"] = *event.TraceID
		}
		rows = append(rows, row)
	}
	return rows
}

func hasAnyRows(byTable map[string][]envelope.AiEventEnvelope) bool {
	for _, events := range byTable {
		if len(events) > 0 {
			return true
		}
	}
	return false
}

func aiSchema() []FieldSpec {
	return []FieldSpec{
		{ID: 1, Name: "event_id", Type: "uuid", Required: true},
		{ID: 2, Name: "at", Type: "timestamptz", Required: true},
		{ID: 3, Name: "kind", Type: "string", Required: true},
		{ID: 4, Name: "run_id", Type: "uuid", Required: false},
		{ID: 5, Name: "trace_id", Type: "string", Required: false},
		{ID: 6, Name: "producer", Type: "string", Required: true},
		{ID: 7, Name: "schema_version", Type: "uint32", Required: true},
		{ID: 8, Name: "payload", Type: "string", Required: true},
	}
}

type HTTPTableWriterCatalog struct {
	baseURL string
	client  *http.Client
}

func NewHTTPTableWriterCatalog(catalogURL, warehouse string) *HTTPTableWriterCatalog {
	return &HTTPTableWriterCatalog{
		baseURL: strings.TrimRight(catalogURL, "/"),
		client:  &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *HTTPTableWriterCatalog) LoadTable(_ context.Context, spec TableSpec) (IcebergTableAppender, error) {
	if spec.Namespace == "" || spec.Table == "" {
		return nil, fmt.Errorf("%w: namespace/table must be non-empty", ErrTableNotFound)
	}
	return &httpTableWriter{catalog: c, spec: spec}, nil
}

type httpTableWriter struct {
	catalog *HTTPTableWriterCatalog
	spec    TableSpec
}

func (t *httpTableWriter) Append(ctx context.Context, batch AppendBatch) error {
	payload, err := json.Marshal(batch)
	if err != nil {
		return err
	}
	url := t.catalog.baseURL + "/openfoundry/iceberg/v1/append"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.catalog.client.Do(req)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrCommitFailed, err)
	}
	defer resp.Body.Close()
	switch resp.StatusCode {
	case http.StatusOK, http.StatusCreated, http.StatusAccepted, http.StatusNoContent:
		return nil
	case http.StatusNotFound:
		return ErrTableNotFound
	case http.StatusConflict, http.StatusUnprocessableEntity:
		return ErrSchemaMismatch
	default:
		return fmt.Errorf("%w: HTTP %d from table-writer adapter", ErrCommitFailed, resp.StatusCode)
	}
}
