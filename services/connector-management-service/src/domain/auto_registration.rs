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

/// Per-connection summary of the most recent scheduler tick. Surfaced via
/// `GET /sources/{id}/registrations/auto/status` so operators can see when
/// the loop last ran and how many selectors it touched without scraping
/// logs. Mirrors the "Automatic registration status" panel in Foundry's
/// source view.
#[derive(Debug, Clone, serde::Serialize)]
pub struct ConnectionTickRecord {
    pub connection_id: uuid::Uuid,
    pub started_at: chrono::DateTime<chrono::Utc>,
    pub finished_at: chrono::DateTime<chrono::Utc>,
    pub discovered: usize,
    pub registered: usize,
    pub errors: usize,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub last_error: Option<String>,
}

static LAST_RUNS: std::sync::OnceLock<
    std::sync::Mutex<std::collections::HashMap<uuid::Uuid, ConnectionTickRecord>>,
> = std::sync::OnceLock::new();

fn last_runs() -> &'static std::sync::Mutex<std::collections::HashMap<uuid::Uuid, ConnectionTickRecord>>
{
    LAST_RUNS.get_or_init(|| std::sync::Mutex::new(std::collections::HashMap::new()))
}

/// Record (or overwrite) the last tick result for a connection. Public so
/// the one-shot HTTP `auto_register` handler can also publish status.
pub fn record_run(record: ConnectionTickRecord) {
    if let Ok(mut guard) = last_runs().lock() {
        guard.insert(record.connection_id, record);
    }
}

/// Read the last recorded tick for a connection, if any.
pub fn last_run(connection_id: uuid::Uuid) -> Option<ConnectionTickRecord> {
    last_runs()
        .lock()
        .ok()
        .and_then(|g| g.get(&connection_id).cloned())
}

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
        let started_at = chrono::Utc::now();
        let mut local_discovered = 0usize;
        let mut local_registered = 0usize;
        let mut local_errors = 0usize;
        let mut last_error: Option<String> = None;

        let discovered = match discovery::discover_sources(state, &connection).await {
            Ok(items) => {
                local_discovered = items.len();
                items
            }
            Err(error) => {
                tracing::warn!(
                    connection_id = %connection.id,
                    "auto-registration discover failed: {error}"
                );
                summary.errors += 1;
                local_errors += 1;
                last_error = Some(error.clone());
                record_run(ConnectionTickRecord {
                    connection_id: connection.id,
                    started_at,
                    finished_at: chrono::Utc::now(),
                    discovered: 0,
                    registered: 0,
                    errors: local_errors,
                    last_error,
                });
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
                local_errors += 1;
                last_error = Some(error);
                record_run(ConnectionTickRecord {
                    connection_id: connection.id,
                    started_at,
                    finished_at: chrono::Utc::now(),
                    discovered: local_discovered,
                    registered: 0,
                    errors: local_errors,
                    last_error,
                });
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
                Ok(_) => {
                    summary.registered += 1;
                    local_registered += 1;
                }
                Err(error) => {
                    tracing::warn!(
                        connection_id = %connection.id,
                        selector = %source.selector,
                        "upsert_registration failed: {error}"
                    );
                    summary.errors += 1;
                    local_errors += 1;
                    last_error = Some(error);
                }
            }
        }
        record_run(ConnectionTickRecord {
            connection_id: connection.id,
            started_at,
            finished_at: chrono::Utc::now(),
            discovered: local_discovered,
            registered: local_registered,
            errors: local_errors,
            last_error,
        });
    }
    Ok(summary)
}

#[derive(Debug, Default)]
pub struct TickSummary {
    pub scanned: usize,
    pub registered: usize,
    pub errors: usize,
}

/// Public, JSON-serialisable view of the per-connection
/// `config.auto_registration` block. Returned by the status endpoint so
/// the UI can render the toggle without re-parsing the raw config.
#[derive(Debug, Clone, serde::Serialize)]
pub struct AutoRegistrationSettingsView {
    pub enabled: bool,
    pub registration_mode: String,
    pub auto_sync: bool,
    pub update_detection: bool,
    pub selectors: Vec<String>,
}

pub fn settings_view(config: &serde_json::Value) -> Option<AutoRegistrationSettingsView> {
    AutoRegistrationSettings::from_config(config).map(|s| AutoRegistrationSettingsView {
        enabled: s.enabled,
        registration_mode: s.registration_mode,
        auto_sync: s.auto_sync,
        update_detection: s.update_detection,
        selectors: s.selectors,
    })
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
