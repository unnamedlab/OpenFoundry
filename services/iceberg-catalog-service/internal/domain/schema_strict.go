// Package domain — schema strict-mode enforcement.
//
// `Iceberg tables.md` § "Notable differences" / "Automatic schema
// evolution" pins the rule:
//
//	> Iceberg is strict about the schema when writing to an existing
//	> Iceberg table. Any change in schema needs to be made explicitly
//	> via an ALTER TABLE command.
//
// The catalog enforces this invariant in two places:
//
//  1. DiffSchemas computes the structural difference between the
//     table's current schema and the schema a writer is about to
//     commit. When the diff is non-empty the commit is rejected
//     with 422 SCHEMA_INCOMPATIBLE_REQUIRES_ALTER.
//  2. The dedicated POST /alter-schema endpoint accepts an explicit
//     list of schema mutations and bumps schema-id.
//
// This file is a 1:1 port of services/iceberg-catalog-service/src/domain/
// schema_strict.rs.
package domain

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Schema-delta kind tags. Kebab-case to match Rust's
// #[serde(tag = "kind", rename_all = "kebab-case")] enum encoding.
const (
	DeltaAddedColumn           = "added-column"
	DeltaDroppedColumn         = "dropped-column"
	DeltaChangedColumnType     = "changed-column-type"
	DeltaChangedColumnRequired = "changed-column-required"
)

// SchemaDelta is a granular description of one schema-level change.
// Used by the 422 response envelope so clients (especially the
// pipeline-authoring UI's "generate ALTER TABLE" CTA) can build the
// migration without re-running the diff client-side.
//
// The JSON shape is the flat-tagged form Rust serde produces:
//
//	added-column / dropped-column → {"kind": ..., "name": ..., "column_type": ...}
//	changed-column-type           → {"kind": ..., "name": ..., "from": "long", "to": "string"}
//	changed-column-required       → {"kind": ..., "name": ..., "from": true, "to": false}
//
// `From`/`To` are kept as `any` because the variant determines the
// underlying type (string for type-changes, bool for required-changes).
type SchemaDelta struct {
	Kind       string `json:"kind"`
	Name       string `json:"name"`
	ColumnType string `json:"column_type,omitempty"`
	From       any    `json:"from,omitempty"`
	To         any    `json:"to,omitempty"`
}

// SchemaDiff is the full diff envelope. The list is empty when the
// schemas are equivalent.
type SchemaDiff struct {
	Deltas []SchemaDelta `json:"deltas"`
}

// IsCompatible reports whether the diff has no deltas.
func (d SchemaDiff) IsCompatible() bool { return len(d.Deltas) == 0 }

// Rendered formats the diff as a comma-separated human-readable list
// matching Rust's `SchemaDiff::rendered` output.
func (d SchemaDiff) Rendered() string {
	parts := make([]string, 0, len(d.Deltas))
	for _, delta := range d.Deltas {
		switch delta.Kind {
		case DeltaAddedColumn:
			parts = append(parts, fmt.Sprintf("+%s:%s", delta.Name, delta.ColumnType))
		case DeltaDroppedColumn:
			parts = append(parts, fmt.Sprintf("-%s:%s", delta.Name, delta.ColumnType))
		case DeltaChangedColumnType:
			parts = append(parts, fmt.Sprintf("~%s:%v→%v", delta.Name, delta.From, delta.To))
		case DeltaChangedColumnRequired:
			parts = append(parts, fmt.Sprintf("?%s:%v→%v", delta.Name, delta.From, delta.To))
		}
	}
	return strings.Join(parts, ", ")
}

type fieldAttrs struct {
	columnType string
	required   bool
}

// DiffSchemas compares two Iceberg schemas (the JSON shape that lives
// in iceberg_tables.schema_json) and returns the deltas the caller
// must explicitly apply via the alter-schema endpoint before the
// commit can land.
//
// `current` and `attempted` are raw JSON values; either may be nil
// (treated as the empty schema). The function tolerates both v1
// (where `type` may be a nested object) and v2 (string scalars) per
// the Iceberg spec.
func DiffSchemas(current, attempted json.RawMessage) SchemaDiff {
	currentFields := extractFields(current)
	attemptedFields := extractFields(attempted)

	var deltas []SchemaDelta

	// Walk attempted fields in alphabetical order to match Rust's
	// BTreeMap iteration (so the diff list is deterministic and
	// byte-exact across both implementations).
	attemptedNames := sortedKeys(attemptedFields)
	for _, name := range attemptedNames {
		attr := attemptedFields[name]
		curr, ok := currentFields[name]
		switch {
		case !ok:
			deltas = append(deltas, SchemaDelta{
				Kind:       DeltaAddedColumn,
				Name:       name,
				ColumnType: attr.columnType,
			})
		case curr.columnType != attr.columnType:
			deltas = append(deltas, SchemaDelta{
				Kind: DeltaChangedColumnType,
				Name: name,
				From: curr.columnType,
				To:   attr.columnType,
			})
		case curr.required != attr.required:
			deltas = append(deltas, SchemaDelta{
				Kind: DeltaChangedColumnRequired,
				Name: name,
				From: curr.required,
				To:   attr.required,
			})
		}
	}

	for _, name := range sortedKeys(currentFields) {
		if _, ok := attemptedFields[name]; ok {
			continue
		}
		deltas = append(deltas, SchemaDelta{
			Kind:       DeltaDroppedColumn,
			Name:       name,
			ColumnType: currentFields[name].columnType,
		})
	}

	if deltas == nil {
		deltas = []SchemaDelta{}
	}
	return SchemaDiff{Deltas: deltas}
}

// extractFields pulls the {name, type, required} triples from an
// Iceberg schema JSON. Tolerates both v1 (`type` as a nested object)
// and v2 (string scalars).
func extractFields(schema json.RawMessage) map[string]fieldAttrs {
	out := make(map[string]fieldAttrs)
	if len(schema) == 0 {
		return out
	}
	var doc struct {
		Fields []json.RawMessage `json:"fields"`
	}
	if err := json.Unmarshal(schema, &doc); err != nil {
		return out
	}
	for _, raw := range doc.Fields {
		var field map[string]json.RawMessage
		if err := json.Unmarshal(raw, &field); err != nil {
			continue
		}
		nameRaw, ok := field["name"]
		if !ok {
			continue
		}
		var name string
		if err := json.Unmarshal(nameRaw, &name); err != nil {
			continue
		}
		typeRaw, ok := field["type"]
		if !ok {
			continue
		}
		columnType := decodeColumnType(typeRaw)
		required := false
		if reqRaw, ok := field["required"]; ok {
			_ = json.Unmarshal(reqRaw, &required)
		}
		out[name] = fieldAttrs{columnType: columnType, required: required}
	}
	return out
}

// decodeColumnType mirrors Rust's serde match on Value::String vs
// other shapes: scalar strings round-trip as themselves; complex
// types (struct/list/map) round-trip through a JSON re-encode so the
// diff treats `{"type":"list","element":"long"}` and
// `{"type":"list","element":"string"}` as different column types.
func decodeColumnType(raw json.RawMessage) string {
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return asString
	}
	out, err := json.Marshal(raw)
	if err != nil {
		return ""
	}
	return string(out)
}

func sortedKeys(m map[string]fieldAttrs) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// SchemaIncompatibleError is returned when DiffSchemas reports a
// non-empty delta and the caller (CommitTable handler) is operating
// in strict mode. Mirrors Rust's ApiError::SchemaIncompatible
// envelope so the 422 response shape stays byte-exact.
type SchemaIncompatibleError struct {
	CurrentSchema   json.RawMessage `json:"current_schema"`
	AttemptedSchema json.RawMessage `json:"attempted_schema"`
	Diff            SchemaDiff      `json:"diff"`
}

// Error implements `error` for SchemaIncompatibleError.
func (e *SchemaIncompatibleError) Error() string {
	return "schema strict-mode: writes diverge from current schema (" + e.Diff.Rendered() + ")"
}
