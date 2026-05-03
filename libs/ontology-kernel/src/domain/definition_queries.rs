//! Control-plane definition lookups shared by ontology handlers.
//!
//! Declarative ontology metadata remains on PostgreSQL during S1, but
//! runtime handlers should not embed raw `sqlx` queries inline. This
//! module centralises the remaining PG-backed lookups behind a small
//! kernel interface so handlers consume typed functions instead.

use sqlx::PgPool;
use uuid::Uuid;

use crate::models::{
    action_type::ActionTypeRow, link_type::LinkType, object_type::ObjectType, property::Property,
};

pub async fn load_actions_for_object_type(
    db: &PgPool,
    object_type_id: Uuid,
) -> Result<Vec<ActionTypeRow>, sqlx::Error> {
    sqlx::query_as::<_, ActionTypeRow>(
        r#"SELECT id, name, display_name, description, object_type_id, operation_kind,
                  input_schema, form_schema, config, confirmation_required, permission_key, authorization_policy,
                  owner_id,
                  created_at, updated_at
           FROM action_types
           WHERE object_type_id = $1
           ORDER BY created_at DESC"#,
    )
    .bind(object_type_id)
    .fetch_all(db)
    .await
}

pub async fn load_action_type(
    db: &PgPool,
    action_id: Uuid,
) -> Result<Option<ActionTypeRow>, sqlx::Error> {
    sqlx::query_as::<_, ActionTypeRow>(
        r#"SELECT id, name, display_name, description, object_type_id, operation_kind, input_schema,
		          form_schema, config, confirmation_required, permission_key, authorization_policy, owner_id,
		          created_at, updated_at
		   FROM action_types WHERE id = $1"#,
    )
    .bind(action_id)
    .fetch_optional(db)
    .await
}

pub async fn load_property_for_object_type(
    db: &PgPool,
    object_type_id: Uuid,
    property_id: Uuid,
) -> Result<Option<Property>, sqlx::Error> {
    sqlx::query_as::<_, Property>(
        r#"SELECT id, object_type_id, name, display_name, description, property_type, required,
                  unique_constraint, time_dependent, default_value, validation_rules,
                  inline_edit_config, created_at, updated_at
           FROM properties
           WHERE id = $1 AND object_type_id = $2"#,
    )
    .bind(property_id)
    .bind(object_type_id)
    .fetch_optional(db)
    .await
}

pub async fn object_type_exists(db: &PgPool, object_type_id: Uuid) -> Result<bool, sqlx::Error> {
    sqlx::query_scalar::<_, bool>("SELECT EXISTS(SELECT 1 FROM object_types WHERE id = $1)")
        .bind(object_type_id)
        .fetch_one(db)
        .await
}

pub async fn load_object_type(
    db: &PgPool,
    object_type_id: Uuid,
) -> Result<Option<ObjectType>, sqlx::Error> {
    sqlx::query_as::<_, ObjectType>("SELECT * FROM object_types WHERE id = $1")
        .bind(object_type_id)
        .fetch_optional(db)
        .await
}

pub async fn load_link_type(
    db: &PgPool,
    link_type_id: Uuid,
) -> Result<Option<LinkType>, sqlx::Error> {
    sqlx::query_as::<_, LinkType>("SELECT * FROM link_types WHERE id = $1")
        .bind(link_type_id)
        .fetch_optional(db)
        .await
}

pub async fn load_object_type_display_name(
    db: &PgPool,
    object_type_id: Uuid,
) -> Result<Option<String>, sqlx::Error> {
    sqlx::query_scalar::<_, String>("SELECT display_name FROM object_types WHERE id = $1")
        .bind(object_type_id)
        .fetch_optional(db)
        .await
}

pub async fn load_object_types_by_ids(
    db: &PgPool,
    type_ids: &[Uuid],
) -> Result<Vec<ObjectType>, sqlx::Error> {
    sqlx::query_as::<_, ObjectType>("SELECT * FROM object_types WHERE id = ANY($1)")
        .bind(type_ids)
        .fetch_all(db)
        .await
}
