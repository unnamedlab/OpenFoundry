package cassandrakernel

import "fmt"

// OntologyObjectStoreMigrations returns the DDL required by ObjectStore. The
// caller must ensure the keyspace already exists.
func OntologyObjectStoreMigrations(keyspace string) []Migration {
	return []Migration{
		{Name: "ontology_objects.objects_by_id", DDL: fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.objects_by_id (
				tenant text,
				type_id text,
				primary_key_hash tinyint,
				object_id timeuuid,
				rid text,
				primary_key text,
				owner_id uuid,
				properties_blob blob,
				markings_blob blob,
				organizations uuid,
				revision_number bigint,
				created_at timestamp,
				updated_at timestamp,
				last_updated timestamp,
				last_updater text,
				deleted boolean,
				PRIMARY KEY ((tenant, type_id, primary_key_hash), object_id)
			)`, keyspace)},
		{Name: "ontology_objects.objects_by_id_by_rid", DDL: fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.objects_by_id_by_rid (
				tenant text,
				object_id timeuuid,
				type_id text,
				primary_key_hash tinyint,
				rid text,
				primary_key text,
				owner_id uuid,
				properties_blob blob,
				markings_blob blob,
				organizations uuid,
				revision_number bigint,
				created_at timestamp,
				updated_at timestamp,
				last_updated timestamp,
				last_updater text,
				deleted boolean,
				PRIMARY KEY ((tenant, object_id))
			)`, keyspace)},
		{Name: "ontology_objects.objects_by_type", DDL: fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.objects_by_type (
				tenant text,
				type_id text,
				primary_key_hash tinyint,
				updated_at timestamp,
				object_id timeuuid,
				owner_id uuid,
				markings_blob blob,
				properties_summary text,
				deleted boolean,
				PRIMARY KEY ((tenant, type_id, primary_key_hash), updated_at, object_id)
			) WITH CLUSTERING ORDER BY (updated_at DESC, object_id ASC)`, keyspace)},
		{Name: "ontology_objects.objects_by_owner", DDL: fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.objects_by_owner (
				tenant text,
				owner_id uuid,
				type_id text,
				object_id timeuuid,
				updated_at timestamp,
				deleted boolean,
				PRIMARY KEY ((tenant, owner_id), type_id, object_id)
			)`, keyspace)},
		{Name: "ontology_objects.objects_by_marking", DDL: fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.objects_by_marking (
				tenant text,
				marking_id text,
				object_id timeuuid,
				type_id text,
				owner_id uuid,
				updated_at timestamp,
				deleted boolean,
				PRIMARY KEY ((tenant, marking_id), object_id)
			)`, keyspace)},
	}
}

// OntologyLinkStoreMigrations returns the DDL required by LinkStore. The caller
// must ensure the keyspace already exists.
func OntologyLinkStoreMigrations(keyspace string) []Migration {
	return []Migration{
		{Name: "ontology_indexes.links_outgoing", DDL: fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.links_outgoing (
				tenant text,
				link_type_id text,
				source_rid timeuuid,
				target_rid timeuuid,
				target_type text,
				properties_blob blob,
				markings_blob blob,
				created_at timestamp,
				PRIMARY KEY ((tenant, link_type_id, source_rid), target_rid)
			)`, keyspace)},
		{Name: "ontology_indexes.links_incoming", DDL: fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.links_incoming (
				tenant text,
				link_type_id text,
				target_rid timeuuid,
				source_rid timeuuid,
				source_type text,
				properties_blob blob,
				markings_blob blob,
				created_at timestamp,
				PRIMARY KEY ((tenant, link_type_id, target_rid), source_rid)
			)`, keyspace)},
		{Name: "ontology_indexes.object_property_index", DDL: fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.object_property_index (
				tenant text,
				type_id text,
				property_id text,
				primary_key_hash tinyint,
				value_kind text,
				value_key text,
				null_value boolean,
				object_id timeuuid,
				updated_at timestamp,
				PRIMARY KEY ((tenant, type_id, property_id, primary_key_hash), value_key, object_id)
			) WITH CLUSTERING ORDER BY (value_key ASC, object_id ASC)`, keyspace)},
	}
}

// OntologyRuntimeMigrations returns the Cassandra/Scylla DDL required by
// ontology-actions-service runtime stores. The caller is responsible for
// creating the keyspace with the desired replication policy before applying
// these table migrations.
func OntologyRuntimeMigrations(keyspace string) []Migration {
	return []Migration{
		{Name: "ontology_objects.objects_by_id", DDL: fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.objects_by_id (
			tenant text,
			object_id timeuuid,
			type_id text,
			owner_id uuid,
			properties text,
			marking frozen<set<text>>,
			organization_id uuid,
			revision_number bigint,
			created_at timestamp,
			updated_at timestamp,
			deleted boolean,
			PRIMARY KEY ((tenant, object_id))
		)`, keyspace)},
		{Name: "ontology_objects.objects_by_type", DDL: fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.objects_by_type (
			tenant text,
			type_id text,
			updated_at timestamp,
			object_id timeuuid,
			owner_id uuid,
			marking frozen<set<text>>,
			properties_summary text,
			deleted boolean,
			PRIMARY KEY ((tenant, type_id), updated_at, object_id)
		) WITH CLUSTERING ORDER BY (updated_at DESC, object_id ASC)`, keyspace)},
		{Name: "ontology_objects.objects_by_owner", DDL: fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.objects_by_owner (
			tenant text,
			owner_id uuid,
			type_id text,
			object_id timeuuid,
			updated_at timestamp,
			deleted boolean,
			PRIMARY KEY ((tenant, owner_id), type_id, object_id)
		)`, keyspace)},
		{Name: "ontology_objects.objects_by_marking", DDL: fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.objects_by_marking (
			tenant text,
			marking_id text,
			object_id timeuuid,
			type_id text,
			owner_id uuid,
			updated_at timestamp,
			deleted boolean,
			PRIMARY KEY ((tenant, marking_id), object_id)
		)`, keyspace)},
		{Name: "ontology_indexes.links_outgoing", DDL: fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.links_outgoing (
			tenant text,
			link_type_id text,
			source_rid timeuuid,
			target_rid timeuuid,
			target_type text,
			properties_blob blob,
			markings_blob blob,
			created_at timestamp,
			PRIMARY KEY ((tenant, link_type_id, source_rid), target_rid)
		) WITH CLUSTERING ORDER BY (target_rid ASC)`, keyspace)},
		{Name: "ontology_indexes.links_incoming", DDL: fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.links_incoming (
			tenant text,
			link_type_id text,
			target_rid timeuuid,
			source_rid timeuuid,
			source_type text,
			properties_blob blob,
			markings_blob blob,
			created_at timestamp,
			PRIMARY KEY ((tenant, link_type_id, target_rid), source_rid)
		) WITH CLUSTERING ORDER BY (source_rid ASC)`, keyspace)},
		{Name: "actions_log.actions_log", DDL: fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.actions_log (
			tenant text,
			day_bucket date,
			applied_at timestamp,
			action_id timeuuid,
			kind text,
			actor_id uuid,
			subject text,
			target_object_id timeuuid,
			target_type_id text,
			payload text,
			status text,
			failure_type text,
			duration_ms int,
			event_id text,
			PRIMARY KEY ((tenant, day_bucket), applied_at, action_id)
		) WITH CLUSTERING ORDER BY (applied_at DESC, action_id ASC)
		  AND default_time_to_live = 7776000
		  AND gc_grace_seconds = 10800`, keyspace)},
		{Name: "actions_log.actions_by_object", DDL: fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.actions_by_object (
			tenant text,
			target_object_id timeuuid,
			applied_at timestamp,
			action_id timeuuid,
			kind text,
			actor_id uuid,
			subject text,
			payload text,
			event_id text,
			PRIMARY KEY ((tenant, target_object_id), applied_at, action_id)
		) WITH CLUSTERING ORDER BY (applied_at DESC, action_id ASC)
		  AND default_time_to_live = 7776000
		  AND gc_grace_seconds = 10800`, keyspace)},
		{Name: "actions_log.actions_by_action", DDL: fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.actions_by_action (
			tenant text,
			action_id timeuuid,
			day_bucket date,
			applied_at timestamp,
			event_id text,
			kind text,
			actor_id uuid,
			subject text,
			target_object_id timeuuid,
			payload text,
			PRIMARY KEY ((tenant, action_id, day_bucket), applied_at, event_id)
		) WITH CLUSTERING ORDER BY (applied_at DESC, event_id ASC)
		  AND default_time_to_live = 7776000
		  AND gc_grace_seconds = 10800`, keyspace)},
		{Name: "actions_log.actions_by_event", DDL: fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.actions_by_event (
			tenant text,
			event_id text,
			action_id timeuuid,
			kind text,
			actor_id uuid,
			subject text,
			target_object_id timeuuid,
			payload text,
			applied_at timestamp,
			day_bucket date,
			PRIMARY KEY ((tenant, event_id))
		) WITH default_time_to_live = 7776000
		  AND gc_grace_seconds = 10800`, keyspace)},
		{Name: "ontology_objects.schemas_by_type", DDL: fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.schemas_by_type (
			type_id text,
			version int,
			json_schema text,
			created_at timestamp,
			PRIMARY KEY ((type_id), version)
		)`, keyspace)},
		{Name: "ontology_objects.schemas_latest", DDL: fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.schemas_latest (
			type_id text PRIMARY KEY,
			version int,
			json_schema text,
			created_at timestamp
		)`, keyspace)},
		{Name: "read_models", DDL: fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.read_models (
			kind text,
			tenant text,
			id text,
			parent_id text,
			payload text,
			version bigint,
			updated_at timestamp,
			PRIMARY KEY ((kind, tenant), id)
		)`, keyspace)},
		{Name: "read_models_by_parent", DDL: fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s.read_models_by_parent (
			kind text,
			tenant text,
			parent_id text,
			id text,
			payload text,
			version bigint,
			updated_at timestamp,
			PRIMARY KEY ((kind, tenant, parent_id), id)
		)`, keyspace)},
	}
}
