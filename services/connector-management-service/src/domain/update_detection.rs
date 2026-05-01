//! Update detection for virtual tables.
//!
//! Foundry-aligned: Iceberg/Delta tables advertise a monotonic
//! `snapshot_id` (Iceberg) or table version (Delta `_delta_log/`) on every
//! commit. Foundry's auto-registration loop watches that token to decide
//! whether to re-materialise / re-snapshot a registered table — see
//! `docs_original_palantir_foundry/foundry-docs/Data connectivity & integration/Core concepts/Virtual tables.md`
//! ("Update detection").
//!
//! This module sits between [`super::discovery::discover_sources`] and
//! [`super::discovery::upsert_registration`] inside the auto-registration
//! tick. For each discovered selector it:
//!
//! 1. Looks up the matching `connection_registrations` row.
//! 2. Compares `discovered.source_signature` against
//!    `registration.last_source_signature`.
//! 3. Returns one of [`UpdateState::FirstSeen`], [`UpdateState::Unchanged`],
//!    [`UpdateState::Changed`].
//!
//! Callers can use the result to decide whether to bump the dataset
//! version, fire a webhook, or skip the upsert entirely.
//!
//! The tracker is intentionally non-blocking: rows without a
//! `source_signature` (e.g. agent-bridged sources that don't expose one)
//! are reported as [`UpdateState::Unknown`] and treated as "always re-sync"
//! by the caller.

use sqlx::Row;
use uuid::Uuid;

use crate::{AppState, models::registration::DiscoveredSource};

#[derive(Debug, Clone, Copy, PartialEq, Eq, serde::Serialize)]
#[serde(rename_all = "snake_case")]
pub enum UpdateState {
    /// No registration row exists yet for this selector.
    FirstSeen,
    /// Registration exists but neither side carries a signature.
    Unknown,
    /// Both signatures are present and equal — upstream is identical.
    Unchanged,
    /// Both signatures are present and differ — upstream advanced.
    Changed,
}

impl UpdateState {
    pub fn as_str(self) -> &'static str {
        match self {
            UpdateState::FirstSeen => "first_seen",
            UpdateState::Unknown => "unknown",
            UpdateState::Unchanged => "unchanged",
            UpdateState::Changed => "changed",
        }
    }
}

#[derive(Debug, Clone, serde::Serialize)]
pub struct UpdateOutcome {
    pub selector: String,
    pub state: UpdateState,
    pub previous_signature: Option<String>,
    pub current_signature: Option<String>,
}

/// Inspect a single discovered source against its persisted registration.
pub async fn evaluate(
    state: &AppState,
    connection_id: Uuid,
    discovered: &DiscoveredSource,
) -> Result<UpdateOutcome, String> {
    let row = sqlx::query(
        "SELECT last_source_signature
         FROM connection_registrations
         WHERE connection_id = $1 AND selector = $2",
    )
    .bind(connection_id)
    .bind(&discovered.selector)
    .fetch_optional(&state.db)
    .await
    .map_err(|e| e.to_string())?;

    let previous: Option<String> = row.and_then(|r| r.try_get(0).ok());
    let current = discovered.source_signature.clone();

    let state_kind = match (previous.as_deref(), current.as_deref()) {
        (None, _) => UpdateState::FirstSeen,
        (Some(_), None) => UpdateState::Unknown,
        (Some(prev), Some(curr)) if prev == curr => UpdateState::Unchanged,
        (Some(_), Some(_)) => UpdateState::Changed,
    };

    Ok(UpdateOutcome {
        selector: discovered.selector.clone(),
        state: state_kind,
        previous_signature: previous,
        current_signature: current,
    })
}

/// After [`super::discovery::upsert_registration`] succeeds, persist the
/// new signature so the next tick can compare against it.
pub async fn record_signature(
    state: &AppState,
    connection_id: Uuid,
    selector: &str,
    signature: Option<&str>,
) -> Result<(), String> {
    if let Some(sig) = signature {
        sqlx::query(
            "UPDATE connection_registrations
             SET last_source_signature = $3, updated_at = NOW()
             WHERE connection_id = $1 AND selector = $2",
        )
        .bind(connection_id)
        .bind(selector)
        .bind(sig)
        .execute(&state.db)
        .await
        .map_err(|e| e.to_string())?;
    }
    Ok(())
}
