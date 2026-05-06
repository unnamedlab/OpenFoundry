package domain

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// libs/ontology-kernel/src/domain/storage_repository.rs
// `cassandra_index_definitions()` returns the 6 static metadata rows
// for the Cassandra runtime tables. Pin name + index_definition
// strings byte-for-byte so handlers see identical wire shapes during
// the migration.
func TestCassandraIndexDefinitionsContents(t *testing.T) {
	defs := CassandraIndexDefinitions()
	require.Len(t, defs, 6)

	wantTables := []string{
		"links_outgoing",
		"links_incoming",
		"actions_log.actions_log",
		"actions_log.actions_by_object",
		"actions_log.actions_by_action",
		"actions_log.actions_by_event",
	}
	for i, want := range wantTables {
		assert.Equal(t, want, defs[i].TableName)
	}

	// Spot-check a few full lines.
	assert.Equal(t, "PRIMARY KEY ((tenant, source_id), link_type, target_id)", defs[0].IndexDefinition)
	assert.Equal(t, "PRIMARY KEY ((tenant, target_id), link_type, source_id)", defs[1].IndexDefinition)
	assert.Equal(t, "PRIMARY KEY ((tenant, event_id))", defs[5].IndexDefinition)
}

// libs/ontology-kernel/src/domain/storage_repository.rs — the slice
// of inspected tables is the single source of truth for both
// definition_counts (11 COUNT queries) and pg_index_definitions
// (ANY($1) filter). Drift between the two surfaces was historically a
// source of bugs.
func TestInspectedTablesIsTheCanonicalSet(t *testing.T) {
	want := []string{
		"object_types",
		"properties",
		"link_types",
		"ontology_interfaces",
		"interface_properties",
		"shared_property_types",
		"action_types",
		"ontology_function_packages",
		"ontology_object_sets",
		"ontology_funnel_sources",
		"ontology_projects",
	}
	assert.Equal(t, want, inspectedTables)

	// The guard rejects unknown tables — protects countTable from
	// accidental string interpolation of arbitrary input.
	assert.True(t, isInspectedTable("object_types"))
	assert.False(t, isInspectedTable("'); DROP TABLE users;--"))
	assert.False(t, isInspectedTable(""))
}

// libs/ontology-kernel/src/domain/binding_repository.rs
// `BindingRepoError::constraint(&self) -> Option<&str>` returns the
// constraint name when the underlying error is a `Database` error
// carrying a constraint, otherwise None.
func TestBindingRepoErrorConstraint(t *testing.T) {
	pgErr := &pgconn.PgError{ConstraintName: "object_type_bindings_pkey"}
	wrapped := &BindingRepoError{SQL: pgErr}
	assert.Equal(t, "object_type_bindings_pkey", wrapped.Constraint())

	plain := &BindingRepoError{SQL: errors.New("not a pg error")}
	assert.Equal(t, "", plain.Constraint())

	decode := &BindingRepoError{Decode: "boom"}
	assert.Equal(t, "", decode.Constraint())
}

// libs/ontology-kernel/src/domain/binding_repository.rs — Decode
// variant carries the verbatim Rust message body. Display impl
// surfaces the SQL error first, falling back to Decode.
func TestBindingRepoErrorDisplay(t *testing.T) {
	sqlErr := errors.New("syntax error at or near \"FROM\"")
	wrapped := &BindingRepoError{SQL: sqlErr}
	assert.Equal(t, "syntax error at or near \"FROM\"", wrapped.Error())
	// Unwrap exposes the underlying error for errors.Is / errors.As.
	assert.True(t, errors.Is(wrapped, sqlErr))

	decode := &BindingRepoError{Decode: "failed to decode property_mapping: invalid type"}
	assert.Equal(t, "failed to decode property_mapping: invalid type", decode.Error())
}

// libs/ontology-kernel/src/domain/storage_repository.rs
// `DefinitionCounts` zero-value JSON shape. Every field renders as
// 0 (matches Rust `Default`).
func TestDefinitionCountsZeroValue(t *testing.T) {
	d := DefinitionCounts{}
	assert.Equal(t, int64(0), d.ObjectTypes)
	assert.Equal(t, int64(0), d.FunnelSources)
	assert.Equal(t, int64(0), d.Projects)
}
