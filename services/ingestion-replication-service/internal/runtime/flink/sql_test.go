package flink

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/openfoundry/openfoundry-go/services/ingestion-replication-service/internal/domain"
)

func newStream(name string) domain.DomainStreamDefinition {
	wm := "event_time"
	return domain.DomainStreamDefinition{
		ID:          uuid.New(),
		Name:        name,
		Description: "",
		Status:      "active",
		Schema: domain.StreamSchema{
			Fields: []domain.StreamField{
				{Name: "event_time", DataType: "timestamp", Nullable: false, SemanticRole: "event_time"},
				{Name: "customer_id", DataType: "string", Nullable: false, SemanticRole: "join_key"},
			},
			WatermarkField: &wm,
		},
		SourceBinding: domain.ConnectorBinding{
			ConnectorType: "kafka",
			Endpoint:      "kafka://stream/orders",
			Format:        "json",
		},
		RetentionHours:       24,
		Partitions:           3,
		ConsistencyGuarantee: "at-least-once",
		StreamType:           "ingest",
		Compression:          false,
		IngestConsistency:    "at-least-once",
		PipelineConsistency:  "at-least-once",
		CheckpointIntervalMS: 2_000,
		Kind:                 "ingest",
		CreatedAt:            time.Now().UTC(),
		UpdatedAt:            time.Now().UTC(),
	}
}

func newTopology(sourceID uuid.UUID) domain.TopologyDefinition {
	jobName := "demo"
	return domain.TopologyDefinition{
		ID:                   uuid.New(),
		Name:                 "demo",
		Status:               "active",
		BackpressurePolicy:   domain.DefaultBackpressurePolicy(),
		SourceStreamIDs:      []uuid.UUID{sourceID},
		SinkBindings: []domain.ConnectorBinding{
			{
				ConnectorType: "iceberg",
				Endpoint:      "s3://openfoundry-iceberg/sink",
				Format:        "parquet",
			},
		},
		StateBackend:         "rocksdb",
		CheckpointIntervalMS: 60_000,
		RuntimeKind:          "flink",
		FlinkJobName:         &jobName,
		ConsistencyGuarantee: "exactly-once",
		CreatedAt:            time.Now().UTC(),
		UpdatedAt:            time.Now().UTC(),
	}
}

func TestRendersSourceSinkAndInsert(t *testing.T) {
	s := newStream("orders")
	topo := newTopology(s.ID)
	out := RenderFlinkSQL(&topo, []domain.DomainStreamDefinition{s})
	for _, want := range []string{
		"CREATE TABLE orders",
		"CREATE TABLE sink_0",
		"INSERT INTO sink_0",
		"EXACTLY_ONCE",
		"execution.checkpointing.interval",
	} {
		if !strings.Contains(out.Script, want) {
			t.Fatalf("script missing %q\n--- script ---\n%s", want, out.Script)
		}
	}
	if len(out.Warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", out.Warnings)
	}
}

func TestWarnsWhenSourceMissingFromCatalogue(t *testing.T) {
	topo := newTopology(uuid.New())
	out := RenderFlinkSQL(&topo, nil)
	found := false
	for _, w := range out.Warnings {
		if strings.Contains(w, "not found") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected a 'not found' warning, got %v", out.Warnings)
	}
}

func TestSanitizeReplacesDashesAndPadsLeadingDigits(t *testing.T) {
	if got := sanitize("foo-bar"); got != "foo_bar" {
		t.Fatalf("sanitize(foo-bar) = %q, want foo_bar", got)
	}
	if got := sanitize("9lives"); got != "_9lives" {
		t.Fatalf("sanitize(9lives) = %q, want _9lives", got)
	}
}

func TestEffectiveExactlyOnceTopologyWins(t *testing.T) {
	s := newStream("orders")
	s.PipelineConsistency = "at-least-once"
	topo := newTopology(s.ID)
	topo.ConsistencyGuarantee = "exactly-once"
	if !EffectiveExactlyOnce(&topo, []domain.DomainStreamDefinition{s}) {
		t.Fatal("topology declaring exactly-once should win")
	}
}

func TestEffectiveExactlyOnceStreamWins(t *testing.T) {
	s := newStream("orders")
	s.PipelineConsistency = "exactly-once"
	topo := newTopology(s.ID)
	topo.ConsistencyGuarantee = "at-least-once"
	if !EffectiveExactlyOnce(&topo, []domain.DomainStreamDefinition{s}) {
		t.Fatal("stream declaring exactly-once should escalate the topology")
	}
}

func TestJoinDefinitionEmitsView(t *testing.T) {
	left := newStream("orders")
	right := newStream("customers")
	topo := newTopology(left.ID)
	topo.SourceStreamIDs = []uuid.UUID{left.ID, right.ID}
	topo.JoinDefinition = &domain.JoinDefinition{
		JoinType:      "left",
		LeftStreamID:  left.ID,
		RightStreamID: right.ID,
		TableName:     "joined-events",
		KeyFields:     []string{"customer_id"},
		WindowSeconds: 30,
	}
	out := RenderFlinkSQL(&topo, []domain.DomainStreamDefinition{left, right})
	if !strings.Contains(out.Script, "CREATE VIEW joined_events AS") {
		t.Fatalf("expected sanitised view name, got:\n%s", out.Script)
	}
	if !strings.Contains(out.Script, "LEFT JOIN customers r") {
		t.Fatalf("expected LEFT JOIN with right alias, got:\n%s", out.Script)
	}
	if !strings.Contains(out.Script, "l.customer_id = r.customer_id") {
		t.Fatalf("expected key predicate, got:\n%s", out.Script)
	}
}

func TestCepDefinitionEmitsWarning(t *testing.T) {
	s := newStream("orders")
	topo := newTopology(s.ID)
	topo.CepDefinition = &domain.CepDefinition{PatternName: "p", Sequence: []string{"a", "b"}, WithinSeconds: 60, OutputStream: "matches"}
	out := RenderFlinkSQL(&topo, []domain.DomainStreamDefinition{s})
	found := false
	for _, w := range out.Warnings {
		if strings.Contains(w, "CEP") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected CEP warning, got %v", out.Warnings)
	}
}

func TestFilterNodesEmitWhereClause(t *testing.T) {
	s := newStream("orders")
	topo := newTopology(s.ID)
	topo.Nodes = []domain.TopologyNode{
		{ID: "n1", NodeType: "filter", Config: []byte(`{"expression": "amount > 0"}`)},
		{ID: "n2", NodeType: "filter", Config: []byte(`{"expression": "currency = 'EUR'"}`)},
	}
	out := RenderFlinkSQL(&topo, []domain.DomainStreamDefinition{s})
	if !strings.Contains(out.Script, "WHERE amount > 0 AND currency = 'EUR'") {
		t.Fatalf("expected combined WHERE clause, got:\n%s", out.Script)
	}
}

func TestEscapeSQL(t *testing.T) {
	if got := escapeSQL("O'Brien"); got != "O''Brien" {
		t.Fatalf("escapeSQL(O'Brien) = %q, want O''Brien", got)
	}
}

func TestMapTypeFallsBackToString(t *testing.T) {
	if got := mapType("widget"); got != "STRING" {
		t.Fatalf("mapType(widget) = %q, want STRING (default)", got)
	}
	if got := mapType("INT64"); got != "BIGINT" {
		t.Fatalf("mapType(INT64) = %q, want BIGINT", got)
	}
}

func TestJobManagerURLSubstitutesPlaceholders(t *testing.T) {
	cfg := FlinkRuntimeConfig{JobManagerURLTemplate: "http://{deployment}-rest.{namespace}.svc:8081"}
	got := cfg.JobManagerURL("demo", "flink")
	if got != "http://demo-rest.flink.svc:8081" {
		t.Fatalf("JobManagerURL = %q", got)
	}
}
