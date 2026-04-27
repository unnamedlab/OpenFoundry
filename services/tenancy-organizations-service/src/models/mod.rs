pub mod control_plane;
pub mod enrollment;
pub mod organization;
pub mod peer;
pub mod project;
pub mod space;

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
