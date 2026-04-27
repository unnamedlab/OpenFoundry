#[allow(dead_code)]
#[path = "code_repo_base/models/mod.rs"]
mod code_repo_base;

pub use code_repo_base::{branch, comment, commit, file, integration, merge_request, repository};

use serde::{Deserialize, Serialize, de::DeserializeOwned};

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ListResponse<T> {
    pub items: Vec<T>,
}

pub(crate) fn decode_json<T: DeserializeOwned>(
    value: serde_json::Value,
    field: &str,
) -> Result<T, String> {
    serde_json::from_value(value).map_err(|cause| format!("failed to decode {field}: {cause}"))
}
