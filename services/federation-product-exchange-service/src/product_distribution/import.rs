//! TASK O — Action-type import for the product-distribution-service.
//!
//! `marketplace-service` packages action types as
//! `MarketplaceArtifact::ActionType { action_type, dependencies }` entries
//! inside `manifest.artifacts`. When a workspace installs a marketplace
//! product, this module is responsible for re-projecting those artifacts
//! into the importing workspace by remapping every dependency UUID
//! (object types, function packages, webhook configurations) to the local
//! IDs supplied by the caller.
//!
//! The function is intentionally pure so the binary entry point (or the
//! unit tests) can drive it without reaching for a database. Higher-level
//! installer flows can then take the rewritten value and POST it to
//! `ontology-actions-service` to materialise the action locally.

use std::collections::HashMap;

use serde::{Deserialize, Serialize};
use serde_json::Value;
use uuid::Uuid;

/// Mirror of `marketplace_service::models::package::ActionTypeDependencies`.
/// Re-declared locally because pulling the full marketplace shim into this
/// crate triggers downstream sqlx inference errors; the on-the-wire shape
/// is identical and the two types serialize/deserialize interchangeably.
#[derive(Debug, Clone, Default, Serialize, Deserialize, PartialEq, Eq)]
pub struct ActionTypeDependencies {
    #[serde(default)]
    pub object_type_ids: Vec<Uuid>,
    #[serde(default)]
    pub function_package_ids: Vec<Uuid>,
    #[serde(default)]
    pub webhooks: Vec<Uuid>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
#[serde(tag = "kind", rename_all = "snake_case")]
pub enum MarketplaceArtifact {
    ActionType {
        action_type: Value,
        #[serde(default)]
        dependencies: ActionTypeDependencies,
    },
}

/// Mapping from source dependency IDs (as exported in the marketplace
/// artifact) to the IDs they should take in the destination workspace.
#[derive(Debug, Clone, Default)]
pub struct DependencyRemap {
    pub object_type_ids: HashMap<Uuid, Uuid>,
    pub function_package_ids: HashMap<Uuid, Uuid>,
    pub webhooks: HashMap<Uuid, Uuid>,
}

#[derive(Debug, Clone)]
pub struct ImportedActionType {
    pub action_type: Value,
    pub remapped_dependencies: ActionTypeDependencies,
}

/// Import a single marketplace artifact, rewriting its action_type payload
/// so every cross-reference points at the destination workspace's IDs.
/// Returns an error if the artifact is not an `ActionType` variant or if a
/// referenced dependency is missing from the remap table.
pub fn import_action_type_artifact(
    artifact: &MarketplaceArtifact,
    remap: &DependencyRemap,
) -> Result<ImportedActionType, String> {
    let MarketplaceArtifact::ActionType {
        action_type,
        dependencies,
    } = artifact;

    let mut payload = action_type.clone();
    let object_type_ids = remap_uuids(
        &dependencies.object_type_ids,
        &remap.object_type_ids,
        "object_type_id",
    )?;
    let function_package_ids = remap_uuids(
        &dependencies.function_package_ids,
        &remap.function_package_ids,
        "function_package_id",
    )?;
    let webhooks = remap_uuids(&dependencies.webhooks, &remap.webhooks, "webhook_id")?;

    // Rewrite well-known fields in the action_type JSON payload. We only
    // touch fields the action-types schema is known to declare; unknown
    // sections are left untouched so callers can extend the format.
    if let Some(object) = payload.as_object_mut() {
        if let Some(serde_json::Value::String(raw)) = object.get("object_type_id") {
            if let Ok(parsed) = Uuid::parse_str(raw) {
                if let Some(target) = remap.object_type_ids.get(&parsed) {
                    object.insert(
                        "object_type_id".to_string(),
                        Value::String(target.to_string()),
                    );
                }
            }
        }
        rewrite_uuid_field_in_value(
            object.get_mut("config"),
            "function_package_id",
            &remap.function_package_ids,
        );
        rewrite_uuid_field_in_value(object.get_mut("config"), "webhook_id", &remap.webhooks);
    }

    Ok(ImportedActionType {
        action_type: payload,
        remapped_dependencies: ActionTypeDependencies {
            object_type_ids,
            function_package_ids,
            webhooks,
        },
    })
}

fn remap_uuids(
    sources: &[Uuid],
    table: &HashMap<Uuid, Uuid>,
    label: &str,
) -> Result<Vec<Uuid>, String> {
    sources
        .iter()
        .map(|id| {
            table
                .get(id)
                .copied()
                .ok_or_else(|| format!("missing remap for {label} {id}"))
        })
        .collect()
}

fn rewrite_uuid_field_in_value(
    value: Option<&mut Value>,
    field: &str,
    table: &HashMap<Uuid, Uuid>,
) {
    let Some(value) = value else {
        return;
    };
    rewrite_uuid_field(value, field, table);
}

fn rewrite_uuid_field(value: &mut Value, field: &str, table: &HashMap<Uuid, Uuid>) {
    match value {
        Value::Object(object) => {
            if let Some(Value::String(raw)) = object.get(field).cloned() {
                if let Ok(parsed) = Uuid::parse_str(&raw) {
                    if let Some(target) = table.get(&parsed) {
                        object.insert(field.to_string(), Value::String(target.to_string()));
                    }
                }
            }
            for child in object.values_mut() {
                rewrite_uuid_field(child, field, table);
            }
        }
        Value::Array(items) => {
            for item in items {
                rewrite_uuid_field(item, field, table);
            }
        }
        _ => {}
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn rewrites_object_type_id_and_dependency_table() {
        let source_object_type = Uuid::now_v7();
        let dest_object_type = Uuid::now_v7();
        let action_id = Uuid::now_v7();

        let artifact = MarketplaceArtifact::ActionType {
            action_type: json!({
                "id": action_id,
                "object_type_id": source_object_type,
                "config": { "function_package_id": Uuid::now_v7() }
            }),
            dependencies: ActionTypeDependencies {
                object_type_ids: vec![source_object_type],
                ..Default::default()
            },
        };
        let mut remap = DependencyRemap::default();
        remap
            .object_type_ids
            .insert(source_object_type, dest_object_type);

        let imported = import_action_type_artifact(&artifact, &remap).unwrap();
        assert_eq!(
            imported
                .action_type
                .get("object_type_id")
                .and_then(|v| v.as_str()),
            Some(dest_object_type.to_string()).as_deref()
        );
        assert_eq!(
            imported.remapped_dependencies.object_type_ids,
            vec![dest_object_type]
        );
    }

    #[test]
    fn missing_remap_is_reported() {
        let artifact = MarketplaceArtifact::ActionType {
            action_type: json!({ "id": Uuid::now_v7(), "object_type_id": Uuid::now_v7() }),
            dependencies: ActionTypeDependencies {
                object_type_ids: vec![Uuid::now_v7()],
                ..Default::default()
            },
        };
        let error =
            import_action_type_artifact(&artifact, &DependencyRemap::default()).unwrap_err();
        assert!(error.contains("missing remap"));
    }
}
