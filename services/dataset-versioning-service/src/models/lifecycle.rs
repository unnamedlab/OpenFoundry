use serde::Deserialize;

#[derive(Debug, Clone, Deserialize)]
pub struct SnapshotRequest {
    #[serde(default)]
    pub message: String,
}

#[derive(Debug, Clone, Deserialize)]
pub struct MutationRequest {
    #[serde(default)]
    pub branch_name: Option<String>,
    #[serde(default)]
    pub message: String,
    #[serde(default)]
    pub row_delta: Option<i64>,
    #[serde(default)]
    pub size_delta_bytes: Option<i64>,
    #[serde(default)]
    pub metadata: serde_json::Value,
}
