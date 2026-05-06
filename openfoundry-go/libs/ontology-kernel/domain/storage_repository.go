// Declarative storage-inspection repository.
//
// Runtime object/link/funnel counts are computed through storage
// traits in the handler. The remaining counts and PG index inventory
// belong to declarative control-plane metadata, so SQL stays behind
// this repository boundary.
//
// Mirrors `libs/ontology-kernel/src/domain/storage_repository.rs`.

package domain

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/openfoundry/openfoundry-go/libs/ontology-kernel/models"
)

// DefinitionCounts mirrors `struct DefinitionCounts`. Default zeroes
// match Rust `Default` for an empty bag.
type DefinitionCounts struct {
	ObjectTypes         int64 `json:"object_types"`
	Properties          int64 `json:"properties"`
	LinkTypes           int64 `json:"link_types"`
	Interfaces          int64 `json:"interfaces"`
	InterfaceProperties int64 `json:"interface_properties"`
	SharedPropertyTypes int64 `json:"shared_property_types"`
	ActionTypes         int64 `json:"action_types"`
	FunctionPackages    int64 `json:"function_packages"`
	ObjectSets          int64 `json:"object_sets"`
	Projects            int64 `json:"projects"`
	FunnelSources       int64 `json:"funnel_sources"`
}

// StorageIndexDefinition mirrors `struct StorageIndexDefinition`.
type StorageIndexDefinition struct {
	TableName       string `json:"table_name"        db:"table_name"`
	IndexName       string `json:"index_name"        db:"index_name"`
	IndexDefinition string `json:"index_definition"  db:"index_definition"`
}

// inspectedTables mirrors the slice in the Rust source — both the
// `definition_counts` aggregator and the `pg_index_definitions`
// query consume the same set so that the two surfaces never drift.
var inspectedTables = []string{
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

// LoadDefinitionCounts mirrors `definition_counts`. Issues 11
// COUNT(*) queries — kept identical to Rust so the migration window
// surfaces the same load profile against PG.
func LoadDefinitionCounts(ctx context.Context, db *pgxpool.Pool) (DefinitionCounts, error) {
	out := DefinitionCounts{}
	pairs := []struct {
		dst   *int64
		table string
	}{
		{&out.ObjectTypes, "object_types"},
		{&out.Properties, "properties"},
		{&out.LinkTypes, "link_types"},
		{&out.Interfaces, "ontology_interfaces"},
		{&out.InterfaceProperties, "interface_properties"},
		{&out.SharedPropertyTypes, "shared_property_types"},
		{&out.ActionTypes, "action_types"},
		{&out.FunctionPackages, "ontology_function_packages"},
		{&out.ObjectSets, "ontology_object_sets"},
		{&out.Projects, "ontology_projects"},
		{&out.FunnelSources, "ontology_funnel_sources"},
	}
	for _, p := range pairs {
		n, err := countTable(ctx, db, p.table)
		if err != nil {
			return DefinitionCounts{}, err
		}
		*p.dst = n
	}
	return out, nil
}

// LoadObjectTypesAll mirrors `object_types`. Returns every row
// ordered by `created_at DESC`.
func LoadObjectTypesAll(ctx context.Context, db *pgxpool.Pool) ([]models.ObjectType, error) {
	rows, err := db.Query(ctx,
		`SELECT id, name, display_name, description, primary_key_property, icon, color, owner_id, created_at, updated_at
           FROM object_types ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.ObjectType{}
	for rows.Next() {
		var t models.ObjectType
		if err := rows.Scan(
			&t.ID, &t.Name, &t.DisplayName, &t.Description,
			&t.PrimaryKeyProperty, &t.Icon, &t.Color,
			&t.OwnerID, &t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// LoadLinkTypesAll mirrors `link_types`.
func LoadLinkTypesAll(ctx context.Context, db *pgxpool.Pool) ([]models.LinkType, error) {
	rows, err := db.Query(ctx,
		`SELECT `+linkTypeColumns+` FROM link_types ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []models.LinkType{}
	for rows.Next() {
		lt, err := scanLinkType(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, lt)
	}
	return out, rows.Err()
}

// countTable mirrors `count(db, table)`. The table name is
// interpolated into the SQL because PG does not allow a parameter
// in that position; callers MUST only pass values from
// [inspectedTables] or other static control-plane lists. The Rust
// source has the same constraint (it also does string
// interpolation through `format!`).
func countTable(ctx context.Context, db *pgxpool.Pool, table string) (int64, error) {
	if !isInspectedTable(table) {
		return 0, fmt.Errorf("storage_repository: refusing to count untrusted table %q", table)
	}
	var n int64
	if err := db.QueryRow(ctx, `SELECT COUNT(*) FROM `+table).Scan(&n); err != nil {
		return 0, err
	}
	return n, nil
}

func isInspectedTable(table string) bool {
	for _, t := range inspectedTables {
		if t == table {
			return true
		}
	}
	return false
}

// LoadPGIndexDefinitions mirrors `pg_index_definitions`. The list of
// inspected tables is the same one used by [LoadDefinitionCounts] so
// the two surfaces stay in lockstep.
func LoadPGIndexDefinitions(ctx context.Context, db *pgxpool.Pool) ([]StorageIndexDefinition, error) {
	rows, err := db.Query(ctx,
		`SELECT
                tablename AS table_name,
                indexname AS index_name,
                indexdef AS index_definition
           FROM pg_indexes
           WHERE schemaname = 'public'
             AND tablename = ANY($1)
           ORDER BY tablename ASC, indexname ASC`,
		inspectedTables,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []StorageIndexDefinition{}
	for rows.Next() {
		var d StorageIndexDefinition
		if err := rows.Scan(&d.TableName, &d.IndexName, &d.IndexDefinition); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// CassandraIndexDefinitions mirrors `cassandra_index_definitions()`.
// The 6 entries are the static metadata for the Cassandra-side
// runtime tables (links_outgoing, links_incoming, actions_log + 3
// indexed views). Pure data — no IO.
func CassandraIndexDefinitions() []StorageIndexDefinition {
	return []StorageIndexDefinition{
		{
			TableName:       "links_outgoing",
			IndexName:       "links_outgoing_pkey",
			IndexDefinition: "PRIMARY KEY ((tenant, source_id), link_type, target_id)",
		},
		{
			TableName:       "links_incoming",
			IndexName:       "links_incoming_pkey",
			IndexDefinition: "PRIMARY KEY ((tenant, target_id), link_type, source_id)",
		},
		{
			TableName:       "actions_log.actions_log",
			IndexName:       "actions_log_pkey",
			IndexDefinition: "PRIMARY KEY ((tenant, day_bucket), applied_at, action_id)",
		},
		{
			TableName:       "actions_log.actions_by_object",
			IndexName:       "actions_by_object_pkey",
			IndexDefinition: "PRIMARY KEY ((tenant, target_object_id), applied_at, action_id)",
		},
		{
			TableName:       "actions_log.actions_by_action",
			IndexName:       "actions_by_action_pkey",
			IndexDefinition: "PRIMARY KEY ((tenant, action_id, day_bucket), applied_at, event_id)",
		},
		{
			TableName:       "actions_log.actions_by_event",
			IndexName:       "actions_by_event_pkey",
			IndexDefinition: "PRIMARY KEY ((tenant, event_id))",
		},
	}
}
