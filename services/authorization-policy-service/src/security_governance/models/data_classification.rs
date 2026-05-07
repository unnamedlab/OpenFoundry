use serde::{Deserialize, Serialize};
use std::str::FromStr;

#[derive(Debug, Clone, Copy, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum ClassificationLevel {
    Public,
    Confidential,
    Pii,
}

impl ClassificationLevel {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Public => "public",
            Self::Confidential => "confidential",
            Self::Pii => "pii",
        }
    }
}

impl FromStr for ClassificationLevel {
    type Err = String;

    fn from_str(value: &str) -> Result<Self, Self::Err> {
        match value {
            "public" => Ok(Self::Public),
            "confidential" => Ok(Self::Confidential),
            "pii" => Ok(Self::Pii),
            _ => Err(format!("unsupported classification level: {value}")),
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ClassificationCatalogEntry {
    pub classification: ClassificationLevel,
    pub description: String,
}

impl ClassificationCatalogEntry {
    pub fn new(classification: ClassificationLevel, description: &str) -> Self {
        Self {
            classification,
            description: description.to_string(),
        }
    }
}
