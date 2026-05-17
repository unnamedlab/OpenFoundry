package runner

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"iter"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/openfoundry/openfoundry-go/services/iceberg-object-indexer/internal/sink"
	"github.com/openfoundry/openfoundry-go/services/iceberg-object-indexer/internal/source"
)

// fakeSource yields a fixed slice of rows. If yieldErr is non-nil the
// iterator surfaces it after the (yieldErrAfter)th row.
type fakeSource struct {
	rows          []source.Row
	yieldErr      error
	yieldErrAfter int
	closeErr      error

	scanCalledWithLimit int64
}

func (f *fakeSource) Scan(_ context.Context, limit int64) (iter.Seq2[source.Row, error], error) {
	f.scanCalledWithLimit = limit
	return func(yield func(source.Row, error) bool) {
		for i, row := range f.rows {
			if f.yieldErr != nil && i == f.yieldErrAfter {
				yield(nil, f.yieldErr)
				return
			}
			if !yield(row, nil) {
				return
			}
		}
	}, nil
}
func (f *fakeSource) Close() error { return f.closeErr }

// recordingSink remembers every PUT and lets the test inject failures
// by id.
type recordingSink struct {
	mu        sync.Mutex
	puts      []struct{ Tenant, ID string; Body []byte }
	failBy    map[string]error
}

func (s *recordingSink) Put(_ context.Context, tenant, id string, body []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err, ok := s.failBy[id]; ok {
		return err
	}
	cp := make([]byte, len(body))
	copy(cp, body)
	s.puts = append(s.puts, struct{ Tenant, ID string; Body []byte }{tenant, id, cp})
	return nil
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestRun_smokeShortCircuits(t *testing.T) {
	t.Parallel()
	src := &fakeSource{}
	sk := &recordingSink{}
	err := Run(context.Background(), Args{
		SourceTable: "ns.t", TargetTypeID: "tid", IDColumn: "id",
		Smoke: true,
	}, Deps{Source: src, Sink: sk, Log: discardLogger()})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if len(sk.puts) != 0 || src.scanCalledWithLimit != 0 {
		t.Errorf("smoke mode should not call Source.Scan or Sink.Put, got puts=%d scan_called=%v", len(sk.puts), src.scanCalledWithLimit != 0)
	}
}

func TestRun_writesEveryRowAndPropagatesLimit(t *testing.T) {
	t.Parallel()
	rows := []source.Row{
		{"transaction_id": "abc", "qty": int32(3)},
		{"transaction_id": "def", "qty": int32(7)},
		{"transaction_id": "ghi", "qty": int32(11)},
	}
	src := &fakeSource{rows: rows}
	sk := &recordingSink{}
	fixed := time.UnixMilli(1700000000000)

	err := Run(context.Background(), Args{
		SourceTable: "ns.t", TargetTenant: "default",
		TargetTypeID: "TYPE-1", IDColumn: "transaction_id",
		Limit: 100,
	}, Deps{
		Source: src, Sink: sk, Log: discardLogger(),
		Now: func() time.Time { return fixed },
	})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if src.scanCalledWithLimit != 100 {
		t.Errorf("Source.Scan called with limit=%d, want 100", src.scanCalledWithLimit)
	}
	if got := len(sk.puts); got != 3 {
		t.Fatalf("got %d puts, want 3", got)
	}
	for i, p := range sk.puts {
		if p.Tenant != "default" {
			t.Errorf("put[%d].tenant = %q, want %q", i, p.Tenant, "default")
		}
		var body map[string]any
		if err := json.Unmarshal(p.Body, &body); err != nil {
			t.Fatalf("put[%d].body unmarshal: %v", i, err)
		}
		if body["type_id"] != "TYPE-1" {
			t.Errorf("put[%d].body.type_id = %v, want TYPE-1", i, body["type_id"])
		}
		if v, ok := body["version"].(float64); !ok || int64(v) != fixed.UnixMilli() {
			t.Errorf("put[%d].body.version = %v, want %d", i, body["version"], fixed.UnixMilli())
		}
		if v, ok := body["updated_at_ms"].(float64); !ok || int64(v) != fixed.UnixMilli() {
			t.Errorf("put[%d].body.updated_at_ms = %v", i, body["updated_at_ms"])
		}
		markings, _ := body["markings"].([]any)
		if markings == nil {
			t.Errorf("put[%d].body.markings is nil, want []", i)
		}
	}
}

func TestRun_skipsRowsMissingID(t *testing.T) {
	t.Parallel()
	src := &fakeSource{rows: []source.Row{
		{"transaction_id": "abc"},        // ok
		{"other_col": "x"},                // missing id col
		{"transaction_id": ""},            // empty string
		{"transaction_id": nil},           // explicit nil
		{"transaction_id": "def"},        // ok
	}}
	sk := &recordingSink{}
	err := Run(context.Background(), Args{
		SourceTable: "ns.t", TargetTypeID: "T", IDColumn: "transaction_id",
	}, Deps{Source: src, Sink: sk, Log: discardLogger()})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if got := len(sk.puts); got != 2 {
		t.Fatalf("got %d puts, want 2 (only valid rows)", got)
	}
	if sk.puts[0].ID != "abc" || sk.puts[1].ID != "def" {
		t.Errorf("put ids = [%q, %q], want [abc, def]", sk.puts[0].ID, sk.puts[1].ID)
	}
}

func TestRun_continuesOn4xx_abortsOnNonHTTPError(t *testing.T) {
	t.Parallel()
	src := &fakeSource{rows: []source.Row{
		{"id": "ok-1"},
		{"id": "client-err"},
		{"id": "server-err"},
		{"id": "ok-2"},
	}}
	sk := &recordingSink{
		failBy: map[string]error{
			"client-err": &sink.HTTPError{StatusCode: 422, Body: "schema"},
			"server-err": &sink.HTTPError{StatusCode: 503, Body: "down"},
		},
	}
	err := Run(context.Background(), Args{
		SourceTable: "ns.t", TargetTypeID: "T", IDColumn: "id",
	}, Deps{Source: src, Sink: sk, Log: discardLogger()})
	if err != nil {
		t.Fatalf("Run error: %v (4xx/5xx should NOT abort)", err)
	}
	if got := len(sk.puts); got != 2 {
		t.Errorf("recorded %d successful puts, want 2", got)
	}
}

func TestRun_abortsOnTransportError(t *testing.T) {
	t.Parallel()
	src := &fakeSource{rows: []source.Row{
		{"id": "a"},
		{"id": "boom"},
		{"id": "c"},
	}}
	transport := errors.New("connection reset")
	sk := &recordingSink{failBy: map[string]error{"boom": transport}}
	err := Run(context.Background(), Args{
		SourceTable: "ns.t", TargetTypeID: "T", IDColumn: "id",
	}, Deps{Source: src, Sink: sk, Log: discardLogger()})
	if err == nil || !strings.Contains(err.Error(), "connection reset") {
		t.Fatalf("expected wrapped transport error, got %v", err)
	}
	if len(sk.puts) != 1 {
		t.Errorf("expected 1 successful put before abort, got %d", len(sk.puts))
	}
}

func TestRun_propagatesScanError(t *testing.T) {
	t.Parallel()
	src := &fakeSource{
		rows:          []source.Row{{"id": "a"}, {"id": "b"}},
		yieldErr:      errors.New("parquet read failed"),
		yieldErrAfter: 1,
	}
	sk := &recordingSink{}
	err := Run(context.Background(), Args{
		SourceTable: "ns.t", TargetTypeID: "T", IDColumn: "id",
	}, Deps{Source: src, Sink: sk, Log: discardLogger()})
	if err == nil || !strings.Contains(err.Error(), "parquet read failed") {
		t.Fatalf("expected wrapped scan error, got %v", err)
	}
}

func TestRun_validatesMissingDepsOutsideSmoke(t *testing.T) {
	t.Parallel()
	err := Run(context.Background(), Args{
		SourceTable: "ns.t", TargetTypeID: "T", IDColumn: "id",
	}, Deps{Log: discardLogger()})
	if err == nil || !strings.Contains(err.Error(), "source is required") {
		t.Errorf("missing source: got %v, want 'source is required'", err)
	}
	err = Run(context.Background(), Args{
		SourceTable: "ns.t", TargetTypeID: "T", IDColumn: "id",
	}, Deps{Source: &fakeSource{}, Log: discardLogger()})
	if err == nil || !strings.Contains(err.Error(), "sink is required") {
		t.Errorf("missing sink: got %v, want 'sink is required'", err)
	}
}

func TestStringifyID_coversCommonArrowTypes(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		row  source.Row
		col  string
		want string
		ok   bool
	}{
		{"string", source.Row{"k": "abc"}, "k", "abc", true},
		{"empty string", source.Row{"k": ""}, "k", "", false},
		{"nil value", source.Row{"k": nil}, "k", "", false},
		{"missing col", source.Row{}, "k", "", false},
		{"int64", source.Row{"k": int64(42)}, "k", "42", true},
		{"int32", source.Row{"k": int32(-7)}, "k", "-7", true},
		{"uint64 large", source.Row{"k": uint64(18446744073709551615)}, "k", "18446744073709551615", true},
		{"float", source.Row{"k": 3.14}, "k", "3.14", true},
		{"bool true", source.Row{"k": true}, "k", "true", true},
		{"bytes", source.Row{"k": []byte("xyz")}, "k", "xyz", true},
		{"json.Number", source.Row{"k": json.Number("123")}, "k", "123", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := stringifyID(tc.row, tc.col)
			if ok != tc.ok || got != tc.want {
				t.Errorf("stringifyID(%v, %q) = (%q, %v), want (%q, %v)", tc.row, tc.col, got, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestBuildPutBody_matchesScalaShape(t *testing.T) {
	t.Parallel()
	fixed := time.UnixMilli(1700000000000)
	body, err := buildPutBody("TYPE-X", source.Row{"a": int64(1), "b": "two"}, func() time.Time { return fixed })
	if err != nil {
		t.Fatalf("buildPutBody: %v", err)
	}
	var got struct {
		TypeID      string          `json:"type_id"`
		Version     int64           `json:"version"`
		Payload     json.RawMessage `json:"payload"`
		UpdatedAtMs int64           `json:"updated_at_ms"`
		Markings    []string        `json:"markings"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if got.TypeID != "TYPE-X" || got.Version != 1700000000000 || got.UpdatedAtMs != 1700000000000 {
		t.Errorf("envelope mismatch: %+v", got)
	}
	if got.Markings == nil || len(got.Markings) != 0 {
		t.Errorf("markings = %v, want empty []", got.Markings)
	}
	var payload map[string]any
	if err := json.Unmarshal(got.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload["b"] != "two" {
		t.Errorf("payload.b = %v, want 'two'", payload["b"])
	}
}
