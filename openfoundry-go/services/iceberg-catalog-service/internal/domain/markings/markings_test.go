package markings

import (
	"encoding/json"
	"testing"

	"github.com/google/uuid"
)

func TestTableMarkingsSerialiseWithThreeBuckets(t *testing.T) {
	pid := uuid.Nil
	proj := []MarkingProjection{{MarkingID: pid, Name: "public", Description: "Public"}}
	payload := TableMarkings{
		Effective:              proj,
		Explicit:               proj,
		InheritedFromNamespace: []MarkingProjection{},
	}
	bytes, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var out map[string]json.RawMessage
	if err := json.Unmarshal(bytes, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"effective", "explicit", "inherited_from_namespace"} {
		if _, ok := out[key]; !ok {
			t.Fatalf("missing key %q in %s", key, string(bytes))
		}
	}
}

func TestNamesPreservesOrder(t *testing.T) {
	in := []MarkingProjection{
		{Name: "secret"},
		{Name: "public"},
		{Name: "pii"},
	}
	got := Names(in)
	want := []string{"secret", "public", "pii"}
	if len(got) != len(want) {
		t.Fatalf("length mismatch: %d vs %d", len(got), len(want))
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("at %d: %q vs %q", i, got[i], want[i])
		}
	}
}
