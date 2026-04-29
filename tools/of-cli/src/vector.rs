use std::{fs, path::PathBuf};

use anyhow::{Context, Result};
use serde::{Deserialize, Serialize};
use serde_json::json;
use vector_store::{BackendConfig, BackendKind, Cursor, build_backend};

fn parse_backend_kind(s: &str) -> Result<BackendKind, String> {
    match s.to_lowercase().as_str() {
        "pgvector" => Ok(BackendKind::Pgvector),
        "vespa" => Ok(BackendKind::Vespa),
        other => Err(format!(
            "unknown backend '{other}'; expected pgvector or vespa"
        )),
    }
}

#[derive(Debug, clap::Args)]
pub struct ReindexArgs {
    /// Source backend kind (pgvector | vespa).
    #[arg(long, value_parser = parse_backend_kind)]
    pub from: BackendKind,

    /// Target backend kind (pgvector | vespa).
    #[arg(long = "to", value_parser = parse_backend_kind)]
    pub to_backend: BackendKind,

    /// Tenant ID to reindex.
    #[arg(long)]
    pub tenant: String,

    /// Namespace to reindex (default: all namespaces via empty string).
    #[arg(long, default_value = "")]
    pub namespace: String,

    /// Number of records per batch.
    #[arg(long, default_value_t = 100)]
    pub batch_size: usize,

    /// Source backend database URL (for pgvector) or Vespa base URL.
    #[arg(long)]
    pub from_url: String,

    /// Target backend database URL (for pgvector) or Vespa base URL.
    #[arg(long)]
    pub to_url: String,

    /// Count and validate records without writing to target.
    #[arg(long, default_value_t = false)]
    pub dry_run: bool,

    /// Path to checkpoint file (cursor is persisted here after each confirmed batch).
    #[arg(long)]
    pub checkpoint_file: Option<PathBuf>,
}

#[derive(Debug, Serialize, Deserialize)]
struct CheckpointState {
    cursor: Option<String>,
    batches_done: usize,
    records_done: usize,
}

impl Default for CheckpointState {
    fn default() -> Self {
        Self {
            cursor: None,
            batches_done: 0,
            records_done: 0,
        }
    }
}

pub async fn run_reindex(args: &ReindexArgs) -> Result<()> {
    let source_config = make_backend_config(args.from, &args.from_url);
    let target_config = make_backend_config(args.to_backend, &args.to_url);

    let source = build_backend(&source_config)
        .await
        .context("failed to connect to source backend")?;
    let target = if !args.dry_run {
        Some(
            build_backend(&target_config)
                .await
                .context("failed to connect to target backend")?,
        )
    } else {
        None
    };

    let mut state = load_checkpoint(args.checkpoint_file.as_deref())?;
    let mut cursor = state.cursor.as_deref().map(|s| Cursor(s.to_string()));

    eprintln!(
        "{}reindexing tenant={} namespace={} from={:?} to={:?}",
        if args.dry_run { "[dry-run] " } else { "" },
        args.tenant,
        args.namespace,
        args.from,
        args.to_backend,
    );
    if state.batches_done > 0 {
        eprintln!(
            "resuming from checkpoint: batches_done={} records_done={}",
            state.batches_done, state.records_done
        );
    }

    loop {
        let (records, next_cursor) = source
            .iter_embeddings(&args.tenant, &args.namespace, cursor, args.batch_size)
            .await
            .context("iter_embeddings failed")?;

        if records.is_empty() {
            break;
        }

        let batch_size = records.len();
        let mut validation_errors = 0usize;

        if args.dry_run {
            for record in &records {
                if record.vector.is_empty() {
                    validation_errors += 1;
                }
            }
        } else if let Some(ref t) = target {
            for record in records {
                t.upsert(record)
                    .await
                    .context("upsert to target backend failed")?;
            }
        }

        state.batches_done += 1;
        state.records_done += batch_size;
        state.cursor = next_cursor.as_ref().map(|c| c.0.clone());

        let line = json!({
            "batch": state.batches_done,
            "records_in_batch": batch_size,
            "total_records": state.records_done,
            "dry_run": args.dry_run,
            "validation_errors": validation_errors,
            "cursor": state.cursor,
        });
        println!("{line}");

        if let Some(ref path) = args.checkpoint_file {
            save_checkpoint(path, &state).context("failed to save checkpoint")?;
        }

        cursor = next_cursor;
        if cursor.is_none() {
            break;
        }
    }

    let summary = json!({
        "status": "complete",
        "total_batches": state.batches_done,
        "total_records": state.records_done,
        "dry_run": args.dry_run,
        "tenant": args.tenant,
        "namespace": args.namespace,
    });
    println!("{summary}");
    Ok(())
}

fn make_backend_config(kind: BackendKind, url: &str) -> BackendConfig {
    match kind {
        BackendKind::Pgvector => BackendConfig {
            kind,
            database_url: Some(url.to_string()),
            vespa_url: None,
            dim: 768,
        },
        BackendKind::Vespa => BackendConfig {
            kind,
            database_url: None,
            vespa_url: Some(url.to_string()),
            dim: 768,
        },
    }
}

fn load_checkpoint(path: Option<&std::path::Path>) -> Result<CheckpointState> {
    let Some(path) = path else {
        return Ok(CheckpointState::default());
    };
    if !path.exists() {
        return Ok(CheckpointState::default());
    }
    let data = fs::read_to_string(path).context("failed to read checkpoint file")?;
    serde_json::from_str(&data).context("failed to parse checkpoint file")
}

fn save_checkpoint(path: &std::path::Path, state: &CheckpointState) -> Result<()> {
    let data = serde_json::to_string_pretty(state)?;
    fs::write(path, data).context("failed to write checkpoint file")
}
