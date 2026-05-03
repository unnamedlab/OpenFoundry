//! P3 — Schedule manifests packaged into a Marketplace product.
//!
//! Mirrors the Foundry doc § "Add schedule to a Marketplace product":
//! a product carries zero or more `ScheduleManifest`s, each describing
//! a schedule the install flow should materialise in the destination
//! stack. The manifest holds the trigger / target shape from the P1
//! schedule contract (proto / JSONB) so the install pass can POST
//! straight into pipeline-schedule-service.

use serde::{Deserialize, Serialize};
use serde_json::Value;
use uuid::Uuid;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ScheduleManifest {
    pub name: String,
    #[serde(default)]
    pub description: String,
    /// Serialised `Trigger` proto — same shape persisted in
    /// `pipeline-schedule-service.schedules.trigger_json`.
    pub trigger: Value,
    /// Serialised `ScheduleTarget` proto. The `pipeline_rid` (or
    /// `dataset_rid`) embedded here is *relative* to the product
    /// namespace; the install pass remaps it to the destination
    /// stack's namespace via `RidMapping`.
    pub target: Value,
    #[serde(default)]
    pub scope_kind: String,
    #[serde(default)]
    pub defaults: ScheduleDefaults,
}

#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct ScheduleDefaults {
    #[serde(default)]
    pub time_zone: Option<String>,
    #[serde(default)]
    pub timezone_override: Option<String>,
    #[serde(default)]
    pub force_build: Option<bool>,
}

/// Request body for `POST /v1/products/{rid}/schedules`. Idempotent:
/// re-posting the same manifest under the same name overwrites the
/// previous entry on the version row.
#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AddScheduleManifestRequest {
    pub product_version_id: Uuid,
    pub manifest: ScheduleManifest,
}

/// Mapping table the install pass uses to rewrite `pipeline_rid`
/// references from the product namespace into the destination stack.
#[derive(Debug, Clone, Default, Serialize, Deserialize)]
pub struct RidMapping {
    pub pipeline: std::collections::BTreeMap<String, String>,
    pub dataset: std::collections::BTreeMap<String, String>,
}

impl RidMapping {
    /// Substitute every known source RID inside `value` with its
    /// destination form. Recurses into objects + arrays.
    pub fn rewrite(&self, value: &mut Value) {
        match value {
            Value::String(s) => {
                if let Some(replacement) = self.pipeline.get(s).or_else(|| self.dataset.get(s)) {
                    *s = replacement.clone();
                }
            }
            Value::Array(arr) => {
                for v in arr {
                    self.rewrite(v);
                }
            }
            Value::Object(obj) => {
                for (_, v) in obj.iter_mut() {
                    self.rewrite(v);
                }
            }
            _ => {}
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use serde_json::json;

    #[test]
    fn rid_mapping_rewrites_pipeline_rid_inside_target() {
        let mut target = json!({
            "kind": {
                "pipeline_build": {
                    "pipeline_rid": "ri.foundry.product.pipeline.alpha",
                    "build_branch": "master"
                }
            }
        });
        let mut mapping = RidMapping::default();
        mapping.pipeline.insert(
            "ri.foundry.product.pipeline.alpha".into(),
            "ri.foundry.main.pipeline.alpha-installed".into(),
        );
        mapping.rewrite(&mut target);
        assert_eq!(
            target["kind"]["pipeline_build"]["pipeline_rid"],
            json!("ri.foundry.main.pipeline.alpha-installed")
        );
    }

    #[test]
    fn rid_mapping_leaves_unmapped_rids_alone() {
        let mut value = json!({"target_rid": "ri.foundry.product.dataset.x"});
        let mapping = RidMapping::default();
        let snapshot = value.clone();
        mapping.rewrite(&mut value);
        assert_eq!(value, snapshot);
    }
}
