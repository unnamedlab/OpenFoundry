package opentable

import (
	"encoding/json"
	"testing"
)

func TestHasCatalog(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		raw  string
		want bool
	}{
		{"empty", `{}`, false},
		{"iceberg_only", `{"iceberg_tables":[{"selector":"a"}]}`, true},
		{"delta_only", `{"delta_tables":[{"selector":"a"}]}`, true},
		{"both_empty_arrays", `{"iceberg_tables":[],"delta_tables":[]}`, false},
		{"unrelated_keys", `{"bucket":"b"}`, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := HasCatalog(json.RawMessage(tc.raw))
			if got != tc.want {
				t.Fatalf("HasCatalog(%s) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestDiscoverPrefixesAndSorts(t *testing.T) {
	t.Parallel()
	cfg := json.RawMessage(`{
		"iceberg_tables": [
			{"selector":"db.t","metadata_location":"abfss://w@a.dfs/x.json","snapshot_id":"42"}
		],
		"delta_tables": [
			{"selector":"db.d","table_location":"abfss://w@a.dfs/_delta_log/"}
		]
	}`)
	got, err := Discover(cfg, "azure")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Selector != "db.d" || got[0].SourceKind != "azure_delta_table" {
		t.Fatalf("first entry = %+v", got[0])
	}
	if got[1].Selector != "db.t" || got[1].SourceKind != "azure_iceberg_table" {
		t.Fatalf("second entry = %+v", got[1])
	}
	if got[1].SourceSignature == nil || *got[1].SourceSignature != "42" {
		t.Fatalf("signature = %v, want 42", got[1].SourceSignature)
	}
	var meta map[string]any
	if err := json.Unmarshal(got[1].Metadata, &meta); err != nil {
		t.Fatalf("metadata unmarshal: %v", err)
	}
	upstream, _ := meta["upstream"].(map[string]any)
	if upstream["metadata_location"] != "abfss://w@a.dfs/x.json" {
		t.Fatalf("metadata.upstream.metadata_location = %v", upstream["metadata_location"])
	}
}

func TestDiscoverHandlesNumericSnapshotID(t *testing.T) {
	t.Parallel()
	cfg := json.RawMessage(`{
		"iceberg_tables":[{"selector":"a.b","metadata_location":"s3://x/m.json","snapshot_id":1234}]
	}`)
	got, err := Discover(cfg, "s3")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(got) != 1 || got[0].SourceSignature == nil || *got[0].SourceSignature != "1234" {
		t.Fatalf("unexpected: %+v", got)
	}
}

func TestDiscoverDedupKeepsLastEntry(t *testing.T) {
	t.Parallel()
	// Same selector in both arrays — delta path runs after iceberg, so the
	// delta entry should win (matches Rust BTreeMap insert order).
	cfg := json.RawMessage(`{
		"iceberg_tables":[{"selector":"x","metadata_location":"s3://a/m.json"}],
		"delta_tables":[{"selector":"x","table_location":"s3://a/d/"}]
	}`)
	got, err := Discover(cfg, "s3")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(got) != 1 || got[0].SourceKind != "s3_delta_table" {
		t.Fatalf("dedup: got %+v", got)
	}
}

func TestDiscoverIgnoresEntriesWithoutSelector(t *testing.T) {
	t.Parallel()
	cfg := json.RawMessage(`{
		"iceberg_tables":[{"metadata_location":"s3://a/m.json"},{"selector":"good","metadata_location":"s3://a/g.json"}]
	}`)
	got, err := Discover(cfg, "s3")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(got) != 1 || got[0].Selector != "good" {
		t.Fatalf("filter: got %+v", got)
	}
}

func TestDiscoverEmpty(t *testing.T) {
	t.Parallel()
	got, err := Discover(json.RawMessage(`{"bucket":"b"}`), "s3")
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len = %d, want 0", len(got))
	}
}
