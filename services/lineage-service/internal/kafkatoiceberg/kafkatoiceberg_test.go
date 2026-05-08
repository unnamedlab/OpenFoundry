package kafkatoiceberg

import "testing"

func TestSourceTopicVerbatim(t *testing.T) {
	t.Parallel()
	if SourceTopic != "lineage.events.v1" {
		t.Fatalf("SourceTopic must remain %q (Kafka wire-compat); got %q",
			"lineage.events.v1", SourceTopic)
	}
}

func TestConsumerGroupVerbatim(t *testing.T) {
	t.Parallel()
	if ConsumerGroup != "lineage-service" {
		t.Fatalf("ConsumerGroup pinned for replica rebalance — got %q", ConsumerGroup)
	}
}

func TestIcebergTargetConstants(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"catalog":   IcebergCatalog,
		"namespace": IcebergNamespace,
		"runs":      IcebergTableRuns,
		"events":    IcebergTableEvents,
		"datasets":  IcebergTableDatasetsIO,
		"transform": PartitionTransform,
	}
	expected := map[string]string{
		"catalog":   "lakekeeper",
		"namespace": "of_lineage",
		"runs":      "runs",
		"events":    "events",
		"datasets":  "datasets_io",
		"transform": "day(event_time)",
	}
	for name, got := range cases {
		if got != expected[name] {
			t.Fatalf("%s mismatch: got %q want %q (Iceberg on-disk format compat)",
				name, got, expected[name])
		}
	}
}
