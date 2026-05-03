//! `media_set_transactions` row type + REST DTOs.

use std::str::FromStr;

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sqlx::FromRow;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum TransactionState {
    Open,
    Committed,
    Aborted,
}

impl TransactionState {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Open => "OPEN",
            Self::Committed => "COMMITTED",
            Self::Aborted => "ABORTED",
        }
    }

    pub fn is_terminal(self) -> bool {
        matches!(self, Self::Committed | Self::Aborted)
    }
}

impl FromStr for TransactionState {
    type Err = String;
    fn from_str(s: &str) -> Result<Self, Self::Err> {
        Ok(match s {
            "OPEN" => Self::Open,
            "COMMITTED" => Self::Committed,
            "ABORTED" => Self::Aborted,
            other => return Err(format!("unknown TransactionState `{other}`")),
        })
    }
}

#[derive(Debug, Clone, Serialize, Deserialize, FromRow)]
pub struct MediaSetTransaction {
    pub rid: String,
    pub media_set_rid: String,
    pub branch: String,
    pub state: String,
    pub opened_at: DateTime<Utc>,
    pub closed_at: Option<DateTime<Utc>>,
    pub opened_by: String,
}

#[derive(Debug, Clone, Default, Deserialize)]
pub struct OpenTransactionBody {
    /// Defaults to `"main"`.
    #[serde(default)]
    pub branch: Option<String>,
}
