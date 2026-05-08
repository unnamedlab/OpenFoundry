package schema_test

// Ports the #[cfg(test)] block from
// services/ingestion-replication-service/src/event_streaming/models/schema_bridge.rs.

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/openfoundry/openfoundry-go/libs/core-models/dataset"
	bridge "github.com/openfoundry/openfoundry-go/services/ingestion-replication-service/internal/domain/schema"
)

func uint8Ptr(v uint8) *uint8 { return &v }

func nullableInt(name string) dataset.SchemaField {
	return dataset.SchemaField{
		Name:     name,
		Type:     dataset.FTInteger,
		Nullable: true,
	}
}

// schema_round_trips_through_avro_for_every_documented_type.
func TestSchemaRoundTripsThroughAvroForEveryDocumentedType(t *testing.T) {
	t.Parallel()
	schema := &dataset.Schema{
		Format: "avro",
		Fields: []dataset.SchemaField{
			{Name: "b", Type: dataset.FTBoolean},
			{Name: "by", Type: dataset.FTByte},
			{Name: "sh", Type: dataset.FTShort},
			{Name: "i", Type: dataset.FTInteger},
			{Name: "l", Type: dataset.FTLong},
			{Name: "f", Type: dataset.FTFloat},
			{Name: "d", Type: dataset.FTDouble},
			{Name: "dec", Type: dataset.FTDecimal, Precision: uint8Ptr(18), Scale: uint8Ptr(6)},
			{Name: "s", Type: dataset.FTString},
			{Name: "bin", Type: dataset.FTBinary},
			{Name: "dt", Type: dataset.FTDate},
			{Name: "ts", Type: dataset.FTTimestamp},
			{
				Name: "arr",
				Type: dataset.FTArray,
				ArraySubType: func() *dataset.SchemaField {
					f := nullableInt("item")
					return &f
				}(),
			},
			{
				Name: "mp",
				Type: dataset.FTMap,
				MapKeyType: &dataset.SchemaField{
					Name: "key",
					Type: dataset.FTString,
				},
				MapValueType: func() *dataset.SchemaField {
					f := nullableInt("value")
					return &f
				}(),
			},
			{
				Name:       "st",
				Type:       dataset.FTStruct,
				SubSchemas: []dataset.SchemaField{nullableInt("inner")},
			},
		},
	}

	avro, err := bridge.SchemaToAvro(schema)
	if err != nil {
		t.Fatalf("SchemaToAvro: %v", err)
	}
	parsed, err := bridge.AvroToSchema(avro)
	if err != nil {
		t.Fatalf("AvroToSchema: %v", err)
	}

	// Decimal, Date, Timestamp, Boolean, Long, Float, Double, String,
	// Binary, Array, Map, Struct survive the round-trip. Byte/Short
	// collapse to Integer (Avro doesn't distinguish); we accept that
	// asymmetry because the dataset writer does the same. We assert
	// the kinds survive without losing composite shape.
	if len(parsed.Fields) != len(schema.Fields) {
		t.Fatalf("field count mismatch: got %d want %d", len(parsed.Fields), len(schema.Fields))
	}
	wantNames := []string{
		"b", "by", "sh", "i", "l", "f", "d", "dec", "s", "bin", "dt", "ts",
		"arr", "mp", "st",
	}
	for i, want := range wantNames {
		if parsed.Fields[i].Name != want {
			t.Fatalf("field[%d] name = %q, want %q", i, parsed.Fields[i].Name, want)
		}
	}

	dec := parsed.Fields[7]
	if dec.Type != dataset.FTDecimal {
		t.Fatalf("dec type = %s, want DECIMAL", dec.Type)
	}
	if dec.Precision == nil || *dec.Precision != 18 {
		t.Fatalf("dec precision = %v, want 18", dec.Precision)
	}
	if dec.Scale == nil || *dec.Scale != 6 {
		t.Fatalf("dec scale = %v, want 6", dec.Scale)
	}
	if parsed.Fields[10].Type != dataset.FTDate {
		t.Fatalf("dt type = %s, want DATE", parsed.Fields[10].Type)
	}
	if parsed.Fields[11].Type != dataset.FTTimestamp {
		t.Fatalf("ts type = %s, want TIMESTAMP", parsed.Fields[11].Type)
	}
	if parsed.Fields[12].Type != dataset.FTArray {
		t.Fatalf("arr type = %s, want ARRAY", parsed.Fields[12].Type)
	}
	if parsed.Fields[13].Type != dataset.FTMap {
		t.Fatalf("mp type = %s, want MAP", parsed.Fields[13].Type)
	}
	if parsed.Fields[14].Type != dataset.FTStruct {
		t.Fatalf("st type = %s, want STRUCT", parsed.Fields[14].Type)
	}
}

// nullable_encoding_uses_avro_union.
func TestNullableEncodingUsesAvroUnion(t *testing.T) {
	t.Parallel()
	schema := &dataset.Schema{
		Format: "avro",
		Fields: []dataset.SchemaField{{Name: "n", Type: dataset.FTLong, Nullable: true}},
	}
	avro, err := bridge.SchemaToAvro(schema)
	if err != nil {
		t.Fatalf("SchemaToAvro: %v", err)
	}
	obj := avro.(map[string]any)
	field := obj["fields"].([]any)[0].(map[string]any)
	union, ok := field["type"].([]any)
	if !ok {
		t.Fatalf("expected union slice for nullable field type, got %T", field["type"])
	}
	if union[0] != "null" {
		t.Fatalf("expected first union branch to be 'null', got %v", union[0])
	}
}

// unsupported_avro_type_surfaces_loud_error.
func TestUnsupportedAvroTypeSurfacesLoudError(t *testing.T) {
	t.Parallel()
	bad := []byte(`{"type":"record","name":"X","fields":[{"name":"weird","type":"fixed"}]}`)
	_, err := bridge.AvroJSONToSchema(bad)
	if err == nil {
		t.Fatalf("expected error for unsupported type")
	}
	var be *bridge.BridgeError
	if !errors.As(err, &be) || be.Kind != bridge.BridgeErrUnsupported {
		t.Fatalf("expected BridgeErrUnsupported, got %v", err)
	}
}

// AvroJSONToSchema decodes a JSON byte stream and round-trips simple
// schemas — exercises the JSON convenience wrapper not covered by the
// Rust suite (Rust hands AvroToSchema a serde_json::Value directly).
func TestAvroJSONToSchemaRoundTripsBytes(t *testing.T) {
	t.Parallel()
	schema := &dataset.Schema{
		Format: "avro",
		Fields: []dataset.SchemaField{
			{Name: "id", Type: dataset.FTString},
			{Name: "n", Type: dataset.FTLong, Nullable: true},
		},
	}
	bytes, err := bridge.SchemaToAvroJSON(schema)
	if err != nil {
		t.Fatalf("SchemaToAvroJSON: %v", err)
	}
	if !json.Valid(bytes) {
		t.Fatalf("emitted JSON is not valid: %s", bytes)
	}
	parsed, err := bridge.AvroJSONToSchema(bytes)
	if err != nil {
		t.Fatalf("AvroJSONToSchema: %v", err)
	}
	if parsed.Format != "avro" {
		t.Fatalf("format = %s, want avro", parsed.Format)
	}
	if len(parsed.Fields) != 2 ||
		parsed.Fields[0].Name != "id" || parsed.Fields[0].Type != dataset.FTString ||
		parsed.Fields[1].Name != "n" || parsed.Fields[1].Type != dataset.FTLong ||
		!parsed.Fields[1].Nullable {
		t.Fatalf("unexpected parsed fields: %+v", parsed.Fields)
	}
}
