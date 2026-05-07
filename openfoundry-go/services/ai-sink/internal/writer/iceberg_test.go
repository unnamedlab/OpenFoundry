package writer

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/openfoundry/openfoundry-go/services/ai-sink/internal/envelope"
)

type fakeAICatalog struct {
	table IcebergTableAppender
	err   error
	seen  []TableSpec
}

func (f *fakeAICatalog) LoadTable(_ context.Context, spec TableSpec) (IcebergTableAppender, error) {
	f.seen = append(f.seen, spec)
	if f.err != nil {
		return nil, f.err
	}
	return f.table, nil
}

type fakeAITable struct {
	err     error
	batches []AppendBatch
}

func (f *fakeAITable) Append(_ context.Context, batch AppendBatch) error {
	f.batches = append(f.batches, batch)
	return f.err
}

func TestIcebergWriterAppendBuildsRustCompatibleAIBatches(t *testing.T) {
	runID := uuid.MustParse("00000000-0000-7000-8000-000000000123")
	traceID := "trace-1"
	table := &fakeAITable{}
	catalog := &fakeAICatalog{table: table}
	w := NewIcebergWriterWithCatalog("http://catalog", "wh", "of_ai", catalog)

	byTable := map[string][]envelope.AiEventEnvelope{
		envelope.TableResponses: {{EventID: uuid.MustParse("00000000-0000-7000-8000-000000000001"), At: 1700000000000000, Kind: envelope.KindResponse, RunID: &runID, TraceID: &traceID, Producer: "agent-runtime-service", SchemaVersion: 1, Payload: []byte(`{"tokens":42}`)}},
	}
	if err := w.Append(context.Background(), byTable); err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if len(catalog.seen) != 1 || catalog.seen[0].Namespace != "of_ai" || catalog.seen[0].Table != envelope.TableResponses {
		t.Fatalf("LoadTable specs = %#v", catalog.seen)
	}
	if got := catalog.seen[0].PartitionTransform; got != "day(at)" {
		t.Fatalf("partition = %q", got)
	}
	if len(table.batches) != 1 || len(table.batches[0].Rows) != 1 {
		t.Fatalf("batches = %#v", table.batches)
	}
	row := table.batches[0].Rows[0]
	if row["kind"] != "response" || row["run_id"] != runID.String() || row["trace_id"] != traceID {
		t.Fatalf("row = %#v", row)
	}
	if got := table.batches[0].Spec.Schema[7]; got != (FieldSpec{ID: 8, Name: "payload", Type: "string", Required: true}) {
		t.Fatalf("schema[7] = %#v", got)
	}
}

func TestIcebergWriterRejectsEmptyAIBatch(t *testing.T) {
	w := NewIcebergWriterWithCatalog("", "", "of_ai", &fakeAICatalog{table: &fakeAITable{}})
	if err := w.Append(context.Background(), map[string][]envelope.AiEventEnvelope{}); !errors.Is(err, ErrEmptyBatch) {
		t.Fatalf("Append(empty) error = %v", err)
	}
}

func TestIcebergWriterPropagatesAITableNotFound(t *testing.T) {
	w := NewIcebergWriterWithCatalog("", "", "of_ai", &fakeAICatalog{err: ErrTableNotFound})
	batch := map[string][]envelope.AiEventEnvelope{envelope.TablePrompts: {{EventID: uuid.Nil, Kind: envelope.KindPrompt, Payload: []byte(`{}`)}}}
	if err := w.Append(context.Background(), batch); !errors.Is(err, ErrTableNotFound) {
		t.Fatalf("Append() error = %v", err)
	}
}

func TestIcebergWriterPropagatesAISchemaMismatch(t *testing.T) {
	w := NewIcebergWriterWithCatalog("", "", "of_ai", &fakeAICatalog{table: &fakeAITable{err: ErrSchemaMismatch}})
	batch := map[string][]envelope.AiEventEnvelope{envelope.TablePrompts: {{EventID: uuid.Nil, Kind: envelope.KindPrompt, Payload: []byte(`{}`)}}}
	if err := w.Append(context.Background(), batch); !errors.Is(err, ErrSchemaMismatch) {
		t.Fatalf("Append() error = %v", err)
	}
}

func TestIcebergWriterPropagatesAICommitFailure(t *testing.T) {
	w := NewIcebergWriterWithCatalog("", "", "of_ai", &fakeAICatalog{table: &fakeAITable{err: ErrCommitFailed}})
	batch := map[string][]envelope.AiEventEnvelope{envelope.TablePrompts: {{EventID: uuid.Nil, Kind: envelope.KindPrompt, Payload: []byte(`{}`)}}}
	if err := w.Append(context.Background(), batch); !errors.Is(err, ErrCommitFailed) {
		t.Fatalf("Append() error = %v", err)
	}
}
