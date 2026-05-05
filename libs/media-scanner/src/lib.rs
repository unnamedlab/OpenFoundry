//! Sensitive Data Scanner integration for media sets.
//!
//! Mirrors Foundry's "Sensitive Data Scanner / Media set scanning"
//! doc: per item, run an OCR/extract-text pass and surface a list of
//! findings (per-PII tag) the SDS dashboard ranks. The scanner runs
//! under per-tenant quotas — the trait surfaces a `quota_remaining`
//! check the binary enforces before queueing an item.
//!
//! `services/sds-service/src/main.rs` is currently `fn main(){}` so
//! we keep the scanning surface in this dependency-light crate. The
//! H7 integration test (`sds_scan_finds_pii_in_pdf_ocr.rs`) wires a
//! `MockMediaScanRuntime` against this trait; the production binary
//! will plug in a runtime that calls
//! `media-transform-runtime-service` for OCR + the platform PII
//! taxonomy.

use std::collections::BTreeSet;

use async_trait::async_trait;
use serde::{Deserialize, Serialize};
use thiserror::Error;

/// Foundry "Sensitive Data Scanner" PII tag taxonomy. We keep the
/// list small + camelCased so the JSON wire form matches what the
/// SDS UI dashboard already parses.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Ord, PartialOrd, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub enum PiiTag {
    /// US / EU government IDs — the doc's flagship "high severity"
    /// tag. Maps to SSN/passport/national-id detectors upstream.
    GovernmentId,
    Email,
    PhoneNumber,
    /// Credit-card number — Luhn-validated upstream.
    CreditCard,
    Address,
    DateOfBirth,
    /// Free-form name detection — typically lowest confidence.
    PersonName,
}

impl PiiTag {
    pub fn as_str(self) -> &'static str {
        match self {
            Self::GovernmentId => "GOVERNMENT_ID",
            Self::Email => "EMAIL",
            Self::PhoneNumber => "PHONE_NUMBER",
            Self::CreditCard => "CREDIT_CARD",
            Self::Address => "ADDRESS",
            Self::DateOfBirth => "DATE_OF_BIRTH",
            Self::PersonName => "PERSON_NAME",
        }
    }
}

/// Result of scanning a single item — one row per finding, plus the
/// set of distinct tags hit. The UI badge "PII detected" toggles on
/// `has_findings()`.
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct SdsFinding {
    pub media_set_rid: String,
    pub item_rid: String,
    /// Lowercased tag — lets SDS rules match without case folding.
    pub tag: PiiTag,
    /// Verbatim token surfaced by the upstream detector. Truncated
    /// to 80 chars by the SDS doc; we mirror that cap here.
    pub matched: String,
    /// 0.0..=1.0 — model-reported confidence the upstream returned.
    pub confidence: f32,
    /// Page index for documents (None for images / audio).
    #[serde(skip_serializing_if = "Option::is_none")]
    pub page: Option<u32>,
}

/// Aggregate result returned by [`MediaScanner::scan_item`].
#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct SdsScanReport {
    pub media_set_rid: String,
    pub item_rid: String,
    pub findings: Vec<SdsFinding>,
}

impl SdsScanReport {
    pub fn has_findings(&self) -> bool {
        !self.findings.is_empty()
    }

    /// Distinct PII tags hit. The SDS UI uses this to render the
    /// per-item badge tooltip ("PII detected: GOVERNMENT_ID,
    /// PHONE_NUMBER").
    pub fn distinct_tags(&self) -> BTreeSet<PiiTag> {
        self.findings.iter().map(|f| f.tag).collect()
    }
}

#[derive(Debug, Error, PartialEq, Eq)]
pub enum ScanError {
    #[error("media item `{0}` not found")]
    NotFound(String),
    #[error("quota exhausted for tenant `{0}`")]
    QuotaExhausted(String),
    #[error("upstream OCR runtime returned: {0}")]
    Runtime(String),
    #[error("media kind `{0}` is not scannable (no OCR/extract_text path)")]
    UnscannableKind(String),
}

/// Minimum surface a scanner runtime exposes. The trait stays small
/// so test mocks + production runtimes implement the same shape:
///
///   * `scan_item` — OCR/extract_text the bytes for one item and
///     return findings.
///   * `quota_remaining` — let the SDS dispatcher pre-check before
///     enqueueing N items.
#[async_trait]
pub trait MediaScanner: Send + Sync {
    async fn scan_item(
        &self,
        media_set_rid: &str,
        item_rid: &str,
    ) -> Result<SdsScanReport, ScanError>;

    /// Compute-seconds the tenant has left in the current window.
    /// `None` ⇒ unlimited (typical for dev / small tenants).
    async fn quota_remaining(&self, tenant_id: &str) -> Option<u64>;
}

// ─────────────────────── Mock runtime (test fixture) ──────────────────────

use std::collections::HashMap;
use std::sync::Mutex;

#[derive(Debug, Default)]
pub struct MockMediaScanRuntime {
    /// Pre-scripted reports keyed by `item_rid`.
    pub reports: Mutex<HashMap<String, SdsScanReport>>,
    pub quotas: Mutex<HashMap<String, u64>>,
    pub call_log: Mutex<Vec<(String, String)>>,
}

impl MockMediaScanRuntime {
    pub fn new() -> Self {
        Self::default()
    }

    pub fn put_report(&self, item_rid: &str, report: SdsScanReport) -> &Self {
        self.reports
            .lock()
            .unwrap()
            .insert(item_rid.to_string(), report);
        self
    }

    pub fn put_quota(&self, tenant_id: &str, remaining: u64) -> &Self {
        self.quotas
            .lock()
            .unwrap()
            .insert(tenant_id.to_string(), remaining);
        self
    }

    pub fn calls(&self) -> Vec<(String, String)> {
        self.call_log.lock().unwrap().clone()
    }
}

#[async_trait]
impl MediaScanner for MockMediaScanRuntime {
    async fn scan_item(
        &self,
        media_set_rid: &str,
        item_rid: &str,
    ) -> Result<SdsScanReport, ScanError> {
        self.call_log
            .lock()
            .unwrap()
            .push((media_set_rid.to_string(), item_rid.to_string()));
        self.reports
            .lock()
            .unwrap()
            .get(item_rid)
            .cloned()
            .ok_or_else(|| ScanError::NotFound(item_rid.to_string()))
    }

    async fn quota_remaining(&self, tenant_id: &str) -> Option<u64> {
        self.quotas.lock().unwrap().get(tenant_id).copied()
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    fn finding(item: &str, tag: PiiTag, matched: &str) -> SdsFinding {
        SdsFinding {
            media_set_rid: "ri.foundry.main.media_set.x".into(),
            item_rid: item.into(),
            tag,
            matched: matched.into(),
            confidence: 0.9,
            page: None,
        }
    }

    #[tokio::test]
    async fn scan_returns_scripted_findings() {
        let mock = MockMediaScanRuntime::new();
        mock.put_report(
            "doc-1",
            SdsScanReport {
                media_set_rid: "ri.foundry.main.media_set.x".into(),
                item_rid: "doc-1".into(),
                findings: vec![
                    finding("doc-1", PiiTag::GovernmentId, "123-45-6789"),
                    finding("doc-1", PiiTag::Email, "ops@example.com"),
                ],
            },
        );

        let report = mock.scan_item("ri.foundry.main.media_set.x", "doc-1").await.unwrap();
        assert!(report.has_findings());
        let tags: Vec<&'static str> = report
            .distinct_tags()
            .into_iter()
            .map(|t| t.as_str())
            .collect();
        assert_eq!(tags, vec!["GOVERNMENT_ID", "EMAIL"]);
    }

    #[tokio::test]
    async fn missing_item_surfaces_not_found() {
        let mock = MockMediaScanRuntime::new();
        let err = mock.scan_item("set", "ghost").await.unwrap_err();
        assert_eq!(err, ScanError::NotFound("ghost".into()));
    }
}
