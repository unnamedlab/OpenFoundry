//! Schema strict-mode enforcement.
//!
//! `Iceberg tables.md` § "Notable differences" / "Automatic schema
//! evolution" pins the rule:
//!
//! > Iceberg is strict about the schema when writing to an existing
//! > Iceberg table. Any change in schema needs to be made explicitly
//! > via an `ALTER TABLE` command.
//!
//! The catalog enforces this invariant in two places:
//!
//!   1. [`diff_schemas`] computes the structural difference between
//!      the table's current schema and the schema a writer is about
//!      to commit. When the diff is non-empty the commit is rejected
//!      with `422 SCHEMA_INCOMPATIBLE_REQUIRES_ALTER`.
//!   2. The dedicated `POST /alter-schema` endpoint (in
//!      `handlers::rest_catalog::tables`) accepts an explicit list of
//!      schema mutations and bumps `schema-id`.
//!
//! This module is stateless — it just owns the diff algorithm and the
//! response envelope so handlers stay focused on HTTP concerns.

use serde::Serialize;
use serde_json::Value;
use std::collections::BTreeMap;

/// Granular description of one schema-level change. Used by the
/// `422` response envelope so clients (especially the
/// pipeline-authoring UI's "generate ALTER TABLE" CTA) can build the
/// migration without re-running the diff client-side.
#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
#[serde(tag = "kind", rename_all = "kebab-case")]
pub enum SchemaDelta {
    AddedColumn {
        name: String,
        column_type: String,
    },
    DroppedColumn {
        name: String,
        column_type: String,
    },
    ChangedColumnType {
        name: String,
        from: String,
        to: String,
    },
    ChangedColumnRequired {
        name: String,
        from: bool,
        to: bool,
    },
}

/// Full diff envelope. The list is empty when the schemas are
/// equivalent.
#[derive(Debug, Clone, Serialize, PartialEq, Eq)]
pub struct SchemaDiff {
    pub deltas: Vec<SchemaDelta>,
}

impl SchemaDiff {
    pub fn is_compatible(&self) -> bool {
        self.deltas.is_empty()
    }

    pub fn rendered(&self) -> String {
        self.deltas
            .iter()
            .map(|d| match d {
                SchemaDelta::AddedColumn { name, column_type } => {
                    format!("+{name}:{column_type}")
                }
                SchemaDelta::DroppedColumn { name, column_type } => {
                    format!("-{name}:{column_type}")
                }
                SchemaDelta::ChangedColumnType { name, from, to } => {
                    format!("~{name}:{from}→{to}")
                }
                SchemaDelta::ChangedColumnRequired { name, from, to } => {
                    format!("?{name}:{from}→{to}")
                }
            })
            .collect::<Vec<_>>()
            .join(", ")
    }
}

/// Compare two Iceberg schemas (the JSON shape that lives in
/// `iceberg_tables.schema_json`). Returns the deltas that the caller
/// must explicitly apply via the alter-schema endpoint before the
/// commit can land.
pub fn diff_schemas(current: &Value, attempted: &Value) -> SchemaDiff {
    let current_fields = extract_fields(current);
    let attempted_fields = extract_fields(attempted);

    let mut deltas = Vec::new();
    for (name, attr) in attempted_fields.iter() {
        match current_fields.get(name) {
            None => deltas.push(SchemaDelta::AddedColumn {
                name: name.clone(),
                column_type: attr.column_type.clone(),
            }),
            Some(curr) if curr.column_type != attr.column_type => {
                deltas.push(SchemaDelta::ChangedColumnType {
                    name: name.clone(),
                    from: curr.column_type.clone(),
                    to: attr.column_type.clone(),
                });
            }
            Some(curr) if curr.required != attr.required => {
                deltas.push(SchemaDelta::ChangedColumnRequired {
                    name: name.clone(),
                    from: curr.required,
                    to: attr.required,
                });
            }
            _ => {}
        }
    }
    for (name, attr) in current_fields.iter() {
        if !attempted_fields.contains_key(name) {
            deltas.push(SchemaDelta::DroppedColumn {
                name: name.clone(),
                column_type: attr.column_type.clone(),
            });
        }
    }
    SchemaDiff { deltas }
}

#[derive(Debug, Clone)]
struct FieldAttrs {
    column_type: String,
    required: bool,
}

/// Extract the `{ name, type, required }` triples from an Iceberg
/// schema JSON. The function tolerates both v1 (where `type` may be a
/// nested object) and v2 (string scalars).
fn extract_fields(schema: &Value) -> BTreeMap<String, FieldAttrs> {
    let fields = schema
        .get("fields")
        .and_then(Value::as_array)
        .cloned()
        .unwrap_or_default();
    fields
        .into_iter()
        .filter_map(|field| {
            let name = field.get("name")?.as_str()?.to_string();
            let column_type = match field.get("type")? {
                Value::String(s) => s.clone(),
                other => serde_json::to_string(other).unwrap_or_default(),
            };
            let required = field
                .get("required")
                .and_then(Value::as_bool)
                .unwrap_or(false);
            Some((
                name,
                FieldAttrs {
                    column_type,
                    required,
                },
            ))
        })
        .collect()
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    fn schema_with_fields(fields: Vec<Value>) -> Value {
        json!({ "schema-id": 0, "type": "struct", "fields": fields })
    }

    #[test]
    fn identical_schemas_are_compatible() {
        let s = schema_with_fields(vec![
            json!({ "id": 1, "name": "id", "required": true, "type": "long" }),
            json!({ "id": 2, "name": "name", "required": false, "type": "string" }),
        ]);
        assert!(diff_schemas(&s, &s).is_compatible());
    }

    #[test]
    fn added_column_is_detected() {
        let current = schema_with_fields(vec![json!({
            "id": 1, "name": "id", "required": true, "type": "long"
        })]);
        let attempted = schema_with_fields(vec![
            json!({ "id": 1, "name": "id", "required": true, "type": "long" }),
            json!({ "id": 2, "name": "added", "required": false, "type": "string" }),
        ]);
        let diff = diff_schemas(&current, &attempted);
        assert!(!diff.is_compatible());
        assert!(diff.deltas.iter().any(|d| matches!(
            d,
            SchemaDelta::AddedColumn { name, .. } if name == "added"
        )));
    }

    #[test]
    fn dropped_column_is_detected() {
        let current = schema_with_fields(vec![
            json!({ "id": 1, "name": "id", "required": true, "type": "long" }),
            json!({ "id": 2, "name": "removed", "required": false, "type": "string" }),
        ]);
        let attempted = schema_with_fields(vec![json!({
            "id": 1, "name": "id", "required": true, "type": "long"
        })]);
        let diff = diff_schemas(&current, &attempted);
        assert!(diff.deltas.iter().any(|d| matches!(
            d,
            SchemaDelta::DroppedColumn { name, .. } if name == "removed"
        )));
    }

    #[test]
    fn changed_column_type_is_detected() {
        let current = schema_with_fields(vec![json!({
            "id": 1, "name": "id", "required": true, "type": "long"
        })]);
        let attempted = schema_with_fields(vec![json!({
            "id": 1, "name": "id", "required": true, "type": "string"
        })]);
        let diff = diff_schemas(&current, &attempted);
        assert!(diff.deltas.iter().any(|d| matches!(
            d,
            SchemaDelta::ChangedColumnType { name, from, to }
                if name == "id" && from == "long" && to == "string"
        )));
    }
}
