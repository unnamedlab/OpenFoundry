//! Recurring auto-registration scheduler.
//!
//! Mirrors Foundry's "automatic registration" feature for tabular sources
//! (Databricks, BigQuery, Snowflake, …): periodically discovers all selectors
//! the source exposes and upserts them as `connection_registrations` so the
//! catalog stays current as the upstream system evolves.
//!
//! Sources opt in by adding the following block to `connections.config`:
//!
//! ```jsonc
//! {
//!   "auto_registration": {
//!     "enabled": true,
//!     "interval_secs": 3600,           // optional, defaults to scheduler tick
//!     "registration_mode": "sync",      // or "zero_copy"
//!     "auto_sync": false,
//!     "update_detection": true,
//!     "selectors": ["public.orders"]    // optional allow-list
//!   }
//! }
//! ```
//!
//! Foundry doc reference: `Data connectivity & integration/Core concepts/
//! Virtual tables.md` § "Auto-registration".

use std::time::Duration;

use serde_json::json;

use crate::{
    AppState,
    domain::discovery,
    models::{
        connection::Connection,
        registration::{AutoRegisterRequest, DiscoveredSource},
    },
};

/// Background loop. Polls every `tick_interval` and runs [`tick`].
pub async fn run(state: AppState, tick_interval: Duration) {
    let mut interval = tokio::time::interval(tick_interval);
    interval.set_missed_tick_behavior(tokio::time::MissedTickBehavior::Skip);
    loop {
        interval.tick().await;
        match tick(&state).await {
            Ok(summary) if summary.registered + summary.errors > 0 => {
                tracing::info!(
                    scanned = summary.scanned,
                    registered = summary.registered,
                    errors = summary.errors,
                    "auto-registration tick completed"
                );
            }
            Ok(_) => {}
            Err(error) => tracing::warn!("auto-registration tick failed: {error}"),
        }
    }
}

/// One iteration: scans every connection that has opted into
/// auto-registration, runs discovery, and upserts the resulting selectors.
pub async fn tick(state: &AppState) -> Result<TickSummary, String> {
    let connections =
        sqlx::query_as::<_, Connection>("SELECT * FROM connections ORDER BY created_at DESC")
            .fetch_all(&state.db)
            .await
            .map_err(|error| error.to_string())?;

    let mut summary = TickSummary::default();
    for connection in connections {
        let Some(settings) = AutoRegistrationSettings::from_config(&connection.config) else {
            continue;
        };
        if !settings.enabled {
            continue;
        }
        summary.scanned += 1;

        let discovered = match discovery::discover_sources(state, &connection).await {
            Ok(items) => items,
            Err(error) => {
                tracing::warn!(
                    connection_id = %connection.id,
                    "auto-registration discover failed: {error}"
                );
                summary.errors += 1;
                continue;
            }
        };

        let request = AutoRegisterRequest {
            selectors: settings.selectors.clone(),
            registration_mode: Some(settings.registration_mode.clone()),
            auto_sync: Some(settings.auto_sync),
            update_detection: Some(settings.update_detection),
            default_target_dataset_id: None,
        };
        let mode = match discovery::normalize_registration_mode(Some(&settings.registration_mode)) {
            Ok(mode) => mode,
            Err(error) => {
                tracing::warn!(
                    connection_id = %connection.id,
                    "auto-registration mode invalid: {error}"
                );
                summary.errors += 1;
                continue;
            }
        };
        let selected: Vec<&DiscoveredSource> = discovery::select_sources(&discovered, &request);
        for source in selected {
            match discovery::upsert_registration(
                state,
                connection.id,
                source,
                mode,
                settings.auto_sync,
                settings.update_detection,
                None,
                json!({ "origin": "auto_registration_scheduler" }),
            )
            .await
            {
                Ok(_) => summary.registered += 1,
                Err(error) => {
                    tracing::warn!(
                        connection_id = %connection.id,
                        selector = %source.selector,
                        "upsert_registration failed: {error}"
                    );
                    summary.errors += 1;
                }
            }
        }
    }
    Ok(summary)
}

#[derive(Debug, Default)]
pub struct TickSummary {
    pub scanned: usize,
    pub registered: usize,
    pub errors: usize,
}

struct AutoRegistrationSettings {
    enabled: bool,
    registration_mode: String,
    auto_sync: bool,
    update_detection: bool,
    selectors: Vec<String>,
}

impl AutoRegistrationSettings {
    fn from_config(config: &serde_json::Value) -> Option<Self> {
        let block = config.get("auto_registration")?;
        Some(Self {
            enabled: block
                .get("enabled")
                .and_then(|v| v.as_bool())
                .unwrap_or(false),
            registration_mode: block
                .get("registration_mode")
                .and_then(|v| v.as_str())
                .unwrap_or("sync")
                .to_string(),
            auto_sync: block
                .get("auto_sync")
                .and_then(|v| v.as_bool())
                .unwrap_or(false),
            update_detection: block
                .get("update_detection")
                .and_then(|v| v.as_bool())
                .unwrap_or(true),
            selectors: block
                .get("selectors")
                .and_then(|v| v.as_array())
                .map(|items| {
                    items
                        .iter()
                        .filter_map(|s| s.as_str().map(|s| s.to_string()))
                        .collect()
                })
                .unwrap_or_default(),
        })
    }
}
