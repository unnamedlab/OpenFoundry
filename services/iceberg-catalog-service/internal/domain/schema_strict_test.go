// Schema strict-mode diff tests. Ports `#[cfg(test)]` from
// services/iceberg-catalog-service/src/domain/schema_strict.rs and
// adds a few schema-evolution edge cases the Go side benefits from
// pinning explicitly.
package domain_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/openfoundry/openfoundry-go/services/iceberg-catalog-service/internal/domain"
)

func schemaWithFields(fields []map[string]any) json.RawMessage {
	doc := map[string]any{"schema-id": 0, "type": "struct", "fields": fields}
	out, _ := json.Marshal(doc)
	return out
}

func TestIdenticalSchemasAreCompatible(t *testing.T) {
	t.Parallel()
	s := schemaWithFields([]map[string]any{
		{"id": 1, "name": "id", "required": true, "type": "long"},
		{"id": 2, "name": "name", "required": false, "type": "string"},
	})
	diff := domain.DiffSchemas(s, s)
	assert.True(t, diff.IsCompatible())
	assert.Empty(t, diff.Deltas)
}

func TestAddedColumnIsDetected(t *testing.T) {
	t.Parallel()
	current := schemaWithFields([]map[string]any{
		{"id": 1, "name": "id", "required": true, "type": "long"},
	})
	attempted := schemaWithFields([]map[string]any{
		{"id": 1, "name": "id", "required": true, "type": "long"},
		{"id": 2, "name": "added", "required": false, "type": "string"},
	})
	diff := domain.DiffSchemas(current, attempted)
	assert.False(t, diff.IsCompatible())
	require.Len(t, diff.Deltas, 1)
	d := diff.Deltas[0]
	assert.Equal(t, domain.DeltaAddedColumn, d.Kind)
	assert.Equal(t, "added", d.Name)
	assert.Equal(t, "string", d.ColumnType)
}

func TestDroppedColumnIsDetected(t *testing.T) {
	t.Parallel()
	current := schemaWithFields([]map[string]any{
		{"id": 1, "name": "id", "required": true, "type": "long"},
		{"id": 2, "name": "removed", "required": false, "type": "string"},
	})
	attempted := schemaWithFields([]map[string]any{
		{"id": 1, "name": "id", "required": true, "type": "long"},
	})
	diff := domain.DiffSchemas(current, attempted)
	require.Len(t, diff.Deltas, 1)
	d := diff.Deltas[0]
	assert.Equal(t, domain.DeltaDroppedColumn, d.Kind)
	assert.Equal(t, "removed", d.Name)
	assert.Equal(t, "string", d.ColumnType)
}

func TestChangedColumnTypeIsDetected(t *testing.T) {
	t.Parallel()
	current := schemaWithFields([]map[string]any{
		{"id": 1, "name": "id", "required": true, "type": "long"},
	})
	attempted := schemaWithFields([]map[string]any{
		{"id": 1, "name": "id", "required": true, "type": "string"},
	})
	diff := domain.DiffSchemas(current, attempted)
	require.Len(t, diff.Deltas, 1)
	d := diff.Deltas[0]
	assert.Equal(t, domain.DeltaChangedColumnType, d.Kind)
	assert.Equal(t, "id", d.Name)
	assert.Equal(t, "long", d.From)
	assert.Equal(t, "string", d.To)
}

// Edge case: tightening a column from optional → required is itself
// an incompatible change, distinct from the type-change kind.
func TestChangedColumnRequiredIsDetected(t *testing.T) {
	t.Parallel()
	current := schemaWithFields([]map[string]any{
		{"id": 1, "name": "x", "required": false, "type": "long"},
	})
	attempted := schemaWithFields([]map[string]any{
		{"id": 1, "name": "x", "required": true, "type": "long"},
	})
	diff := domain.DiffSchemas(current, attempted)
	require.Len(t, diff.Deltas, 1)
	d := diff.Deltas[0]
	assert.Equal(t, domain.DeltaChangedColumnRequired, d.Kind)
	assert.Equal(t, "x", d.Name)
	assert.Equal(t, false, d.From)
	assert.Equal(t, true, d.To)
}

// Edge case: when both type AND required change for the same column,
// the type-change wins (matches Rust's `match` ordering).
func TestTypeChangeShadowsRequiredChange(t *testing.T) {
	t.Parallel()
	current := schemaWithFields([]map[string]any{
		{"id": 1, "name": "x", "required": false, "type": "long"},
	})
	attempted := schemaWithFields([]map[string]any{
		{"id": 1, "name": "x", "required": true, "type": "string"},
	})
	diff := domain.DiffSchemas(current, attempted)
	require.Len(t, diff.Deltas, 1)
	assert.Equal(t, domain.DeltaChangedColumnType, diff.Deltas[0].Kind)
}

// Edge case: complex types (list/struct/map) re-encode as JSON. A
// list<long> → list<string> change must surface as ChangedColumnType
// even though the field's `type` is a JSON object, not a scalar.
func TestComplexTypeChangeIsDetected(t *testing.T) {
	t.Parallel()
	current := schemaWithFields([]map[string]any{
		{
			"id": 1, "name": "tags", "required": false,
			"type": map[string]any{"type": "list", "element": "long"},
		},
	})
	attempted := schemaWithFields([]map[string]any{
		{
			"id": 1, "name": "tags", "required": false,
			"type": map[string]any{"type": "list", "element": "string"},
		},
	})
	diff := domain.DiffSchemas(current, attempted)
	require.Len(t, diff.Deltas, 1)
	assert.Equal(t, domain.DeltaChangedColumnType, diff.Deltas[0].Kind)
}

func TestRenderedFormatsAllVariants(t *testing.T) {
	t.Parallel()
	diff := domain.SchemaDiff{Deltas: []domain.SchemaDelta{
		{Kind: domain.DeltaAddedColumn, Name: "new", ColumnType: "string"},
		{Kind: domain.DeltaDroppedColumn, Name: "old", ColumnType: "long"},
		{Kind: domain.DeltaChangedColumnType, Name: "id", From: "long", To: "string"},
		{Kind: domain.DeltaChangedColumnRequired, Name: "x", From: false, To: true},
	}}
	got := diff.Rendered()
	assert.Equal(t, "+new:string, -old:long, ~id:long→string, ?x:false→true", got)
}

func TestDiffJSONShapeIsByteExactToRust(t *testing.T) {
	t.Parallel()
	current := schemaWithFields([]map[string]any{
		{"id": 1, "name": "id", "required": true, "type": "long"},
	})
	attempted := schemaWithFields([]map[string]any{
		{"id": 1, "name": "id", "required": true, "type": "string"},
	})
	diff := domain.DiffSchemas(current, attempted)
	out, err := json.Marshal(diff)
	require.NoError(t, err)
	assert.JSONEq(t,
		`{"deltas":[{"kind":"changed-column-type","name":"id","from":"long","to":"string"}]}`,
		string(out),
	)
}

func TestEmptySchemasAreCompatible(t *testing.T) {
	t.Parallel()
	diff := domain.DiffSchemas(nil, nil)
	assert.True(t, diff.IsCompatible())
}

func TestSchemaIncompatibleErrorMessage(t *testing.T) {
	t.Parallel()
	err := &domain.SchemaIncompatibleError{
		Diff: domain.SchemaDiff{Deltas: []domain.SchemaDelta{
			{Kind: domain.DeltaAddedColumn, Name: "z", ColumnType: "string"},
		}},
	}
	assert.Contains(t, err.Error(), "+z:string")
}
