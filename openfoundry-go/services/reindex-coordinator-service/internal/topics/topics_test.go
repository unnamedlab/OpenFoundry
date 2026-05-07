package topics

import "testing"

func TestTopicConstantsVerbatim(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"input":    OntologyReindexRequestedV1,
		"data":     OntologyReindexV1,
		"control":  OntologyReindexCompletedV1,
		"consumer": ConsumerGroup,
	}
	expected := map[string]string{
		"input":    "ontology.reindex.requested.v1",
		"data":     "ontology.reindex.v1",
		"control":  "ontology.reindex.completed.v1",
		"consumer": "reindex-coordinator",
	}
	for name, got := range cases {
		if got != expected[name] {
			t.Fatalf("%s topic mismatch: got %q want %q (wire-compat with ontology-indexer + control plane)",
				name, got, expected[name])
		}
	}
}
