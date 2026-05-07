package writer

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/openfoundry/openfoundry-go/services/audit-sink/internal/envelope"
)

type fakeAuditCatalog struct {
	table IcebergTableAppender
	err   error
	seen  []TableSpec
}

func (f *fakeAuditCatalog) LoadTable(_ context.Context, spec TableSpec) (IcebergTableAppender, error) {
	f.seen = append(f.seen, spec)
	if f.err != nil {
		return nil, f.err
	}
	return f.table, nil
}

type fakeAuditTable struct {
	err     error
	batches []AppendBatch
}

func (f *fakeAuditTable) Append(_ context.Context, batch AppendBatch) error {
	f.batches = append(f.batches, batch)
	return f.err
}

func TestIcebergWriterAppendBuildsRustCompatibleAuditBatch(t *testing.T) {
	corr := "corr-1"
	table := &fakeAuditTable{}
	catalog := &fakeAuditCatalog{table: table}
	w := NewIcebergWriterWithCatalog("http://catalog", "wh", "of_audit", "events", catalog)

	err := w.Append(context.Background(), []envelope.AuditEnvelope{{EventID: uuid.MustParse("00000000-0000-7000-8000-000000000001"), At: 1700000000000000, CorrelationID: &corr, Kind: "auth.login.ok", Payload: []byte(`{"ok":true}`)}})
	if err != nil {
		t.Fatalf("Append() error = %v", err)
	}
	if len(catalog.seen) != 1 || catalog.seen[0].Namespace != "of_audit" || catalog.seen[0].Table != "events" {
		t.Fatalf("LoadTable specs = %#v", catalog.seen)
	}
	if got := catalog.seen[0].PartitionTransform; got != "day(at)" {
		t.Fatalf("partition = %q", got)
	}
	if len(table.batches) != 1 || len(table.batches[0].Rows) != 1 {
		t.Fatalf("batches = %#v", table.batches)
	}
	if got := table.batches[0].Rows[0]["payload"]; got != `{"ok":true}` {
		t.Fatalf("payload = %#v", got)
	}
	if got := table.batches[0].Spec.Schema[0]; got != (FieldSpec{ID: 1, Name: "event_id", Type: "uuid", Required: true}) {
		t.Fatalf("schema[0] = %#v", got)
	}
}

func TestIcebergWriterRejectsEmptyAuditBatch(t *testing.T) {
	w := NewIcebergWriterWithCatalog("", "", "of_audit", "events", &fakeAuditCatalog{table: &fakeAuditTable{}})
	if err := w.Append(context.Background(), nil); !errors.Is(err, ErrEmptyBatch) {
		t.Fatalf("Append(empty) error = %v", err)
	}
}

func TestIcebergWriterPropagatesTableNotFound(t *testing.T) {
	w := NewIcebergWriterWithCatalog("", "", "of_audit", "events", &fakeAuditCatalog{err: ErrTableNotFound})
	if err := w.Append(context.Background(), []envelope.AuditEnvelope{{EventID: uuid.Nil, Payload: []byte(`{}`)}}); !errors.Is(err, ErrTableNotFound) {
		t.Fatalf("Append() error = %v", err)
	}
}

func TestIcebergWriterPropagatesSchemaMismatch(t *testing.T) {
	w := NewIcebergWriterWithCatalog("", "", "of_audit", "events", &fakeAuditCatalog{table: &fakeAuditTable{err: ErrSchemaMismatch}})
	if err := w.Append(context.Background(), []envelope.AuditEnvelope{{EventID: uuid.Nil, Payload: []byte(`{}`)}}); !errors.Is(err, ErrSchemaMismatch) {
		t.Fatalf("Append() error = %v", err)
	}
}

func TestIcebergWriterPropagatesCommitFailure(t *testing.T) {
	w := NewIcebergWriterWithCatalog("", "", "of_audit", "events", &fakeAuditCatalog{table: &fakeAuditTable{err: ErrCommitFailed}})
	if err := w.Append(context.Background(), []envelope.AuditEnvelope{{EventID: uuid.Nil, Payload: []byte(`{}`)}}); !errors.Is(err, ErrCommitFailed) {
		t.Fatalf("Append() error = %v", err)
	}
}
