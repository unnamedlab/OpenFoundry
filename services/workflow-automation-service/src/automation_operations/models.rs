use serde::Deserialize;

#[derive(Debug, Clone, Deserialize)]
pub struct CreatePrimaryRequest {
    pub payload: serde_json::Value,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreateSecondaryRequest {
    pub payload: serde_json::Value,
}
