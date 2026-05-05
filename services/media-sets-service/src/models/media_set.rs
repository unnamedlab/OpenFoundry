//! `media_sets` row type + REST request/response DTOs.

use std::str::FromStr;

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use sqlx::FromRow;

/// Media kind a set accepts. Mirrors the proto `MediaSetSchema` enum
/// without the `MEDIA_SET_SCHEMA_` prefix; serialised as
/// `SCREAMING_SNAKE_CASE` so it round-trips cleanly through the
/// Foundry-style media reference JSON.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum MediaSetSchema {
    Image,
    Audio,
    Video,
    Document,
    Spreadsheet,
    Email,
    /// H7 — DICOM (medical imaging) media set per Foundry's "Add a
    /// DICOM media set" doc. Behaves like an image schema at the
    /// storage layer; the runtime catalog routes it through
    /// `render_dicom_image_layer` for window/level rendering.
    Dicom,
}

impl MediaSetSchema {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Image => "IMAGE",
            Self::Audio => "AUDIO",
            Self::Video => "VIDEO",
            Self::Document => "DOCUMENT",
            Self::Spreadsheet => "SPREADSHEET",
            Self::Email => "EMAIL",
            Self::Dicom => "DICOM",
        }
    }
}

impl FromStr for MediaSetSchema {
    type Err = String;
    fn from_str(s: &str) -> Result<Self, Self::Err> {
        Ok(match s {
            "IMAGE" => Self::Image,
            "AUDIO" => Self::Audio,
            "VIDEO" => Self::Video,
            "DOCUMENT" => Self::Document,
            "SPREADSHEET" => Self::Spreadsheet,
            "EMAIL" => Self::Email,
            "DICOM" => Self::Dicom,
            other => return Err(format!("unknown MediaSetSchema `{other}`")),
        })
    }
}

#[derive(Debug, Clone, Copy, PartialEq, Eq, Serialize, Deserialize)]
#[serde(rename_all = "SCREAMING_SNAKE_CASE")]
pub enum TransactionPolicy {
    Transactionless,
    Transactional,
}

impl TransactionPolicy {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::Transactionless => "TRANSACTIONLESS",
            Self::Transactional => "TRANSACTIONAL",
        }
    }
}

impl Default for TransactionPolicy {
    fn default() -> Self {
        // Foundry default: items are visible immediately, no rollback.
        Self::Transactionless
    }
}

impl FromStr for TransactionPolicy {
    type Err = String;
    fn from_str(s: &str) -> Result<Self, Self::Err> {
        Ok(match s {
            "TRANSACTIONLESS" => Self::Transactionless,
            "TRANSACTIONAL" => Self::Transactional,
            other => return Err(format!("unknown TransactionPolicy `{other}`")),
        })
    }
}

/// Postgres row + outer-facing DTO for a media set.
#[derive(Debug, Clone, Serialize, Deserialize, FromRow)]
pub struct MediaSet {
    pub rid: String,
    pub project_rid: String,
    pub name: String,
    pub schema: String,
    pub allowed_mime_types: Vec<String>,
    pub transaction_policy: String,
    pub retention_seconds: i64,
    #[sqlx(rename = "virtual")]
    pub virtual_: bool,
    pub source_rid: Option<String>,
    pub markings: Vec<String>,
    pub created_at: DateTime<Utc>,
    pub created_by: String,
}

#[derive(Debug, Clone, Deserialize)]
pub struct CreateMediaSetRequest {
    pub name: String,
    pub project_rid: String,
    pub schema: MediaSetSchema,
    #[serde(default)]
    pub allowed_mime_types: Vec<String>,
    #[serde(default)]
    pub transaction_policy: TransactionPolicy,
    #[serde(default)]
    pub retention_seconds: i64,
    #[serde(default)]
    pub virtual_: bool,
    #[serde(default)]
    pub source_rid: Option<String>,
    #[serde(default)]
    pub markings: Vec<String>,
}

#[derive(Debug, Clone, Default, Deserialize)]
pub struct ListMediaSetsQuery {
    /// Required: only sets within this project are returned.
    pub project_rid: Option<String>,
    pub limit: Option<i64>,
    pub offset: Option<i64>,
}
