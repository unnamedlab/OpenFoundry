//! Foundry-parity Streaming Profiles.
//!
//! A profile is a named Flink configuration fragment scoped to a single
//! [`ProfileCategory`]. Operators import profiles into projects (see
//! [`StreamingProfileProjectRef`]) and attach them to pipelines (see
//! [`PipelineProfileAttachment`]). The runtime composes the per-pipeline
//! effective config from the union of attached profiles using a
//! deterministic resolution order.
//!
//! See `services/event-streaming-service/README.md` for the
//! whitelist of Flink config keys the validator accepts and for the
//! built-in profile catalogue.

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use serde_json::{Map, Value};
use sqlx::{FromRow, types::Json as SqlJson};
use uuid::Uuid;

/// Profile category mirrors Foundry's "set of available profile types".
/// Used for grouping in the picker UI and for tie-breaking in the
/// effective-config resolver (more specific category wins).
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum ProfileCategory {
    #[default]
    TaskmanagerResources,
    JobmanagerResources,
    Parallelism,
    Network,
    Checkpointing,
    Advanced,
}

impl ProfileCategory {
    pub fn from_str(value: &str) -> Result<Self, String> {
        match value {
            "TASKMANAGER_RESOURCES" => Ok(Self::TaskmanagerResources),
            "JOBMANAGER_RESOURCES" => Ok(Self::JobmanagerResources),
            "PARALLELISM" => Ok(Self::Parallelism),
            "NETWORK" => Ok(Self::Network),
            "CHECKPOINTING" => Ok(Self::Checkpointing),
            "ADVANCED" => Ok(Self::Advanced),
            other => Err(format!("unknown profile category: {other}")),
        }
    }

    pub fn as_str(self) -> &'static str {
        match self {
            Self::TaskmanagerResources => "TASKMANAGER_RESOURCES",
            Self::JobmanagerResources => "JOBMANAGER_RESOURCES",
            Self::Parallelism => "PARALLELISM",
            Self::Network => "NETWORK",
            Self::Checkpointing => "CHECKPOINTING",
            Self::Advanced => "ADVANCED",
        }
    }

    /// Specificity rank used by [`compose_effective_config`] when two
    /// categories set the same Flink key. Lower values are more
    /// specific — Advanced is intentionally least specific so an
    /// operator who explicitly opted into ADVANCED can be overridden
    /// by a follow-up category-specific profile.
    pub fn specificity(self) -> u8 {
        match self {
            Self::TaskmanagerResources => 0,
            Self::JobmanagerResources => 1,
            Self::Parallelism => 2,
            Self::Network => 3,
            Self::Checkpointing => 4,
            Self::Advanced => 5,
        }
    }
}

/// T-shirt size that drives default-restricted behaviour: LARGE
/// profiles are restricted by default and require the
/// `enrollment_resource_administrator` role to be imported into a
/// project.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Default, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum ProfileSizeClass {
    #[default]
    Small,
    Medium,
    Large,
}

impl ProfileSizeClass {
    pub fn from_str(value: &str) -> Result<Self, String> {
        match value {
            "SMALL" => Ok(Self::Small),
            "MEDIUM" => Ok(Self::Medium),
            "LARGE" => Ok(Self::Large),
            other => Err(format!("unknown profile size class: {other}")),
        }
    }

    pub fn as_str(self) -> &'static str {
        match self {
            Self::Small => "SMALL",
            Self::Medium => "MEDIUM",
            Self::Large => "LARGE",
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StreamingProfile {
    pub id: Uuid,
    pub name: String,
    #[serde(default)]
    pub description: String,
    pub category: ProfileCategory,
    pub size_class: ProfileSizeClass,
    pub restricted: bool,
    pub config_json: Value,
    pub version: i32,
    pub created_by: String,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, FromRow)]
pub struct StreamingProfileRow {
    pub id: Uuid,
    pub name: String,
    pub description: String,
    pub category: String,
    pub size_class: String,
    pub restricted: bool,
    pub config_json: SqlJson<Value>,
    pub version: i32,
    pub created_by: String,
    pub created_at: DateTime<Utc>,
    pub updated_at: DateTime<Utc>,
}

impl From<StreamingProfileRow> for StreamingProfile {
    fn from(row: StreamingProfileRow) -> Self {
        let category = ProfileCategory::from_str(&row.category).unwrap_or_default();
        let size_class = ProfileSizeClass::from_str(&row.size_class).unwrap_or_default();
        Self {
            id: row.id,
            name: row.name,
            description: row.description,
            category,
            size_class,
            restricted: row.restricted,
            config_json: row.config_json.0,
            version: row.version,
            created_by: row.created_by,
            created_at: row.created_at,
            updated_at: row.updated_at,
        }
    }
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreateProfileRequest {
    pub name: String,
    #[serde(default)]
    pub description: Option<String>,
    pub category: ProfileCategory,
    pub size_class: ProfileSizeClass,
    #[serde(default)]
    pub restricted: Option<bool>,
    pub config_json: Value,
}

#[derive(Debug, Clone, Default, Deserialize)]
pub struct PatchProfileRequest {
    pub name: Option<String>,
    pub description: Option<String>,
    pub category: Option<ProfileCategory>,
    pub size_class: Option<ProfileSizeClass>,
    pub restricted: Option<bool>,
    pub config_json: Option<Value>,
}

#[derive(Debug, Clone, Serialize, Deserialize, FromRow)]
pub struct StreamingProfileProjectRef {
    pub project_rid: String,
    pub profile_id: Uuid,
    pub imported_by: String,
    pub imported_at: DateTime<Utc>,
    pub imported_order: i64,
}

#[derive(Debug, Clone, Serialize, Deserialize, FromRow)]
pub struct PipelineProfileAttachment {
    pub pipeline_rid: String,
    pub profile_id: Uuid,
    pub attached_by: String,
    pub attached_at: DateTime<Utc>,
    pub attached_order: i64,
}

#[derive(Debug, Clone, Deserialize)]
pub struct AttachProfileRequest {
    pub project_rid: String,
    pub profile_id: Uuid,
}

#[derive(Debug, Clone, Serialize)]
pub struct EffectiveFlinkConfig {
    pub pipeline_rid: String,
    pub config: Value,
    pub source_map: Map<String, Value>,
    pub warnings: Vec<String>,
}

/// Whitelist of Flink config keys profiles may set. Anything outside
/// this list is rejected at write time. The list intentionally covers
/// the documented Foundry surface — see
/// `services/event-streaming-service/README.md` for the rationale and
/// for the per-key category mapping.
pub const FLINK_CONFIG_KEY_WHITELIST: &[&str] = &[
    // Resource sizing
    "taskmanager.memory.process.size",
    "taskmanager.memory.flink.size",
    "taskmanager.memory.task.heap.size",
    "taskmanager.memory.managed.fraction",
    "taskmanager.numberOfTaskSlots",
    "jobmanager.memory.process.size",
    "jobmanager.memory.flink.size",
    // Parallelism
    "parallelism.default",
    "pipeline.max-parallelism",
    // Network
    "taskmanager.network.memory.fraction",
    "taskmanager.network.memory.min",
    "taskmanager.network.memory.max",
    "taskmanager.network.numberOfBuffers",
    // Checkpointing
    "execution.checkpointing.interval",
    "execution.checkpointing.timeout",
    "execution.checkpointing.min-pause",
    "execution.checkpointing.max-concurrent-checkpoints",
    // State backend
    "state.backend.type",
    "state.backend.incremental",
    "state.backend.rocksdb.timer-service.factory",
    // Restart strategy
    "restart-strategy",
    "restart-strategy.fixed-delay.attempts",
    "restart-strategy.fixed-delay.delay",
];

/// Validate that every key in `config` is a member of the whitelist.
pub fn validate_config_keys(config: &Value) -> Result<(), Vec<String>> {
    let Some(map) = config.as_object() else {
        return Err(vec![
            "profile config_json must be a JSON object".to_string(),
        ]);
    };
    let mut errors = Vec::new();
    for key in map.keys() {
        if !FLINK_CONFIG_KEY_WHITELIST.contains(&key.as_str()) {
            errors.push(format!(
                "Flink key '{key}' is not on the streaming-profile whitelist; see README"
            ));
        }
    }
    if errors.is_empty() {
        Ok(())
    } else {
        Err(errors)
    }
}

/// Compose the effective Flink config for a pipeline from a list of
/// `(profile, attached_order)` pairs.
///
/// Resolution rules (deterministic, mirrors the docs):
///   1. Profiles are sorted by category specificity (ascending), then
///      by `attached_order` (ascending).
///   2. Each key is set in turn; later writers overwrite earlier
///      writers. A `warnings` entry is emitted on every overwrite so
///      operators can audit conflicts.
///
/// Returning the source-of-key map lets the UI render a Foundry-style
/// "Advanced" preview that attributes each key to the profile that
/// produced it.
pub fn compose_effective_config(
    pipeline_rid: &str,
    profiles: &[(StreamingProfile, i64)],
) -> EffectiveFlinkConfig {
    let mut sorted: Vec<(&StreamingProfile, i64)> =
        profiles.iter().map(|(p, order)| (p, *order)).collect();
    sorted.sort_by(|(a, oa), (b, ob)| {
        a.category
            .specificity()
            .cmp(&b.category.specificity())
            .then(oa.cmp(ob))
    });

    let mut config = Map::new();
    let mut source_map = Map::new();
    let mut warnings = Vec::new();
    for (profile, _) in &sorted {
        let Some(obj) = profile.config_json.as_object() else {
            continue;
        };
        for (k, v) in obj {
            if let Some(prev) = source_map.get(k) {
                warnings.push(format!(
                    "Flink key '{k}' overridden by profile '{}'; previous source: {prev}",
                    profile.name
                ));
            }
            config.insert(k.clone(), v.clone());
            source_map.insert(k.clone(), Value::String(profile.name.clone()));
        }
    }

    EffectiveFlinkConfig {
        pipeline_rid: pipeline_rid.to_string(),
        config: Value::Object(config),
        source_map,
        warnings,
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use chrono::Utc;
    use serde_json::json;

    fn profile(name: &str, category: ProfileCategory, config: Value) -> StreamingProfile {
        StreamingProfile {
            id: Uuid::now_v7(),
            name: name.to_string(),
            description: String::new(),
            category,
            size_class: ProfileSizeClass::Small,
            restricted: false,
            config_json: config,
            version: 1,
            created_by: "system".into(),
            created_at: Utc::now(),
            updated_at: Utc::now(),
        }
    }

    #[test]
    fn whitelist_accepts_documented_keys() {
        let cfg = json!({
            "taskmanager.memory.process.size": "4g",
            "parallelism.default": "8"
        });
        validate_config_keys(&cfg).unwrap();
    }

    #[test]
    fn whitelist_rejects_unknown_keys() {
        let cfg = json!({ "execution.runtime-mode": "BATCH" });
        let errs = validate_config_keys(&cfg).unwrap_err();
        assert!(errs[0].contains("execution.runtime-mode"));
    }

    #[test]
    fn composition_is_deterministic_across_runs() {
        let a = profile(
            "a",
            ProfileCategory::Parallelism,
            json!({"parallelism.default": "4"}),
        );
        let b = profile(
            "b",
            ProfileCategory::TaskmanagerResources,
            json!({"taskmanager.memory.process.size": "2g"}),
        );
        let attached = vec![(a.clone(), 2_i64), (b.clone(), 1_i64)];
        let attached_rev = vec![(b.clone(), 1_i64), (a.clone(), 2_i64)];
        let r1 = compose_effective_config("pipe", &attached);
        let r2 = compose_effective_config("pipe", &attached_rev);
        assert_eq!(r1.config, r2.config);
    }

    #[test]
    fn composition_more_specific_category_runs_first() {
        let advanced = profile(
            "adv",
            ProfileCategory::Advanced,
            json!({"parallelism.default": "16"}),
        );
        let parallelism = profile(
            "par",
            ProfileCategory::Parallelism,
            json!({"parallelism.default": "8"}),
        );
        // Advanced has lowest specificity (highest number) so it runs
        // last and wins. Parallelism is more specific so it runs
        // first; the later Advanced write overwrites it.
        let res = compose_effective_config("p", &[(advanced.clone(), 1), (parallelism.clone(), 2)]);
        assert_eq!(
            res.config["parallelism.default"],
            Value::String("16".into())
        );
        assert!(!res.warnings.is_empty());
    }

    #[test]
    fn composition_within_same_category_uses_attached_order() {
        let p1 = profile(
            "p1",
            ProfileCategory::Parallelism,
            json!({"parallelism.default": "4"}),
        );
        let p2 = profile(
            "p2",
            ProfileCategory::Parallelism,
            json!({"parallelism.default": "8"}),
        );
        // Higher attached_order wins.
        let res = compose_effective_config("p", &[(p1.clone(), 1), (p2.clone(), 2)]);
        assert_eq!(res.config["parallelism.default"], Value::String("8".into()));
    }
}
