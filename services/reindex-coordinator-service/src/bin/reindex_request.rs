//! `reindex-request` — admin CLI to dispatch a single
//! `ontology.reindex.requested.v1` message.
//!
//! This is the **official, supported producer** for the reindex
//! pipeline. Hand-rolled `kafkacat` invocations skip the schema
//! contract and the operator-audit trail; this binary is the
//! single entry point we point operators (and any future UI /
//! REST gateway) at.
//!
//! Behaviour:
//!   * Builds a [`ReindexRequestedV1`] from CLI flags.
//!   * JSON-encodes it with the same `serde` derive the consumer
//!     uses, so a producer/consumer mismatch is impossible.
//!   * Publishes synchronously to
//!     [`ONTOLOGY_REINDEX_REQUESTED_V1`] using
//!     `event-bus-data`'s `KafkaPublisher`, which means the
//!     existing Apicurio schema interceptor and OpenLineage
//!     headers are populated identically to every other producer
//!     in the platform.
//!   * Logs the derived `job_id` so the operator can correlate
//!     the request with `reindex_coordinator.reindex_jobs`
//!     immediately, without waiting for the
//!     `ontology.reindex.completed.v1` event.
//!
//! Usage (inside the cluster, using the Kafka env vars already
//! mounted on every pod):
//!
//! ```bash
//! kubectl run -n openfoundry --rm -it --restart=Never reindex-request \
//!   --image=ghcr.io/openfoundry/reindex-coordinator-service:0.1.0 \
//!   --command -- /usr/local/bin/reindex-request \
//!     --tenant tenant-a --type-id users --page-size 500
//! ```
//!
//! The pod inherits `KAFKA_BOOTSTRAP_SERVERS`,
//! `KAFKA_SASL_USERNAME`, etc. from the same `open-foundry-env`
//! Secret the running coordinator uses, so there is exactly one
//! source of truth for connection settings.

use std::process::ExitCode;

use event_bus_data::{DataPublisher, KafkaPublisher, OpenLineageHeaders};
use reindex_coordinator_service::event::{ReindexRequestedV1, derive_job_id};
use reindex_coordinator_service::topics::ONTOLOGY_REINDEX_REQUESTED_V1;
use tracing_subscriber::EnvFilter;

const SERVICE_NAME: &str = "reindex-request-cli";

#[tokio::main]
async fn main() -> ExitCode {
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| EnvFilter::new("reindex_request=info,event_bus_data=info")),
        )
        .init();

    let args: Vec<String> = std::env::args().collect();
    let cli = match Cli::parse(&args) {
        Ok(c) => c,
        Err(CliError::Help) => {
            print_help(args.first().map(String::as_str).unwrap_or(SERVICE_NAME));
            return ExitCode::SUCCESS;
        }
        Err(e) => {
            eprintln!("error: {e}");
            print_help(args.first().map(String::as_str).unwrap_or(SERVICE_NAME));
            return ExitCode::from(2);
        }
    };

    let payload = ReindexRequestedV1 {
        tenant_id: cli.tenant.clone(),
        type_id: cli.type_id.clone(),
        page_size: cli.page_size,
        request_id: cli.request_id.clone(),
    };
    let job_id = derive_job_id(&payload.tenant_id, payload.type_id.as_deref());

    let body = match serde_json::to_vec(&payload) {
        Ok(b) => b,
        Err(e) => {
            eprintln!("error: failed to encode ReindexRequestedV1: {e}");
            return ExitCode::FAILURE;
        }
    };

    if cli.dry_run {
        // Print the exact bytes we would send so an operator can
        // pipe them into `kafkacat -P` for an air-gapped review
        // before flipping the switch.
        match std::str::from_utf8(&body) {
            Ok(s) => println!("{s}"),
            Err(_) => {
                eprintln!("error: encoded payload is not valid utf-8");
                return ExitCode::FAILURE;
            }
        }
        eprintln!(
            "[dry-run] would publish to {ONTOLOGY_REINDEX_REQUESTED_V1} (job_id={job_id})"
        );
        return ExitCode::SUCCESS;
    }

    let publisher = match KafkaPublisher::from_env(SERVICE_NAME) {
        Ok(p) => p,
        Err(e) => {
            eprintln!("error: failed to build Kafka publisher from env: {e}");
            return ExitCode::FAILURE;
        }
    };

    // OpenLineage header so this manual request is traceable in
    // the same dashboards as the coordinator's downstream events.
    let lineage = OpenLineageHeaders::new(
        std::env::var("OF_OPENLINEAGE_NAMESPACE").unwrap_or_else(|_| "openfoundry".to_string()),
        format!(
            "reindex/{}/{}",
            payload.tenant_id,
            payload.type_id.as_deref().unwrap_or("*"),
        ),
        job_id.to_string(),
        SERVICE_NAME,
    );

    let key = job_id.to_string();
    if let Err(e) = publisher
        .publish(
            ONTOLOGY_REINDEX_REQUESTED_V1,
            Some(key.as_bytes()),
            &body,
            &lineage,
        )
        .await
    {
        eprintln!("error: publish failed: {e}");
        return ExitCode::FAILURE;
    }

    println!(
        "published reindex request: tenant={} type_id={} page_size={} job_id={}",
        payload.tenant_id,
        payload.type_id.as_deref().unwrap_or(""),
        payload
            .page_size
            .map(|s| s.to_string())
            .unwrap_or_else(|| "<default>".to_string()),
        job_id,
    );
    ExitCode::SUCCESS
}

#[derive(Debug)]
struct Cli {
    tenant: String,
    type_id: Option<String>,
    page_size: Option<i32>,
    request_id: Option<String>,
    dry_run: bool,
}

#[derive(Debug)]
enum CliError {
    Help,
    Missing(&'static str),
    InvalidPageSize(String),
    UnknownArg(String),
    DanglingFlag(&'static str),
}

impl std::fmt::Display for CliError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            CliError::Help => f.write_str("help requested"),
            CliError::Missing(name) => write!(f, "missing required flag --{name}"),
            CliError::InvalidPageSize(v) => write!(
                f,
                "--page-size must be a positive integer in 1..=10000, got {v:?}"
            ),
            CliError::UnknownArg(a) => write!(f, "unknown argument {a:?}"),
            CliError::DanglingFlag(name) => write!(f, "flag --{name} requires a value"),
        }
    }
}

impl Cli {
    fn parse(argv: &[String]) -> Result<Self, CliError> {
        let mut tenant: Option<String> = None;
        let mut type_id: Option<String> = None;
        let mut page_size: Option<i32> = None;
        let mut request_id: Option<String> = None;
        let mut dry_run = false;

        let mut i = 1;
        while i < argv.len() {
            let arg = &argv[i];
            match arg.as_str() {
                "-h" | "--help" => return Err(CliError::Help),
                "--dry-run" => {
                    dry_run = true;
                }
                "--tenant" | "--tenant-id" => {
                    tenant = Some(take_value(argv, &mut i, "tenant")?);
                }
                "--type-id" | "--type" => {
                    type_id = Some(take_value(argv, &mut i, "type-id")?);
                }
                "--page-size" => {
                    let raw = take_value(argv, &mut i, "page-size")?;
                    let parsed: i32 = raw
                        .parse()
                        .map_err(|_| CliError::InvalidPageSize(raw.clone()))?;
                    if !(1..=10_000).contains(&parsed) {
                        return Err(CliError::InvalidPageSize(raw));
                    }
                    page_size = Some(parsed);
                }
                "--request-id" => {
                    request_id = Some(take_value(argv, &mut i, "request-id")?);
                }
                other if other.starts_with('-') => {
                    return Err(CliError::UnknownArg(other.to_string()));
                }
                other => return Err(CliError::UnknownArg(other.to_string())),
            }
            i += 1;
        }

        Ok(Self {
            tenant: tenant.ok_or(CliError::Missing("tenant"))?,
            type_id,
            page_size,
            request_id,
            dry_run,
        })
    }
}

fn take_value(argv: &[String], i: &mut usize, name: &'static str) -> Result<String, CliError> {
    *i += 1;
    argv.get(*i).cloned().ok_or(CliError::DanglingFlag(name))
}

fn print_help(prog: &str) {
    eprintln!(
        "{prog} — dispatch a single ontology.reindex.requested.v1\n\
         \n\
         USAGE:\n  \
           {prog} --tenant <id> [--type-id <name>] [--page-size <n>] [--request-id <id>] [--dry-run]\n\
         \n\
         FLAGS:\n  \
           --tenant <id>       Tenant whose ontology objects to reindex (required).\n  \
           --type-id <name>    Restrict to one ontology type. Omit for an all-types scan.\n  \
           --page-size <n>     Cassandra page size override (1..=10000). Default: 1000.\n  \
           --request-id <id>   Optional correlation id, surfaced verbatim on the\n                      ontology.reindex.completed.v1 event.\n  \
           --dry-run           Print the JSON payload to stdout and exit without publishing.\n  \
           -h, --help          Show this message.\n\
         \n\
         ENVIRONMENT:\n  \
           KAFKA_BOOTSTRAP_SERVERS         Required.\n  \
           KAFKA_SASL_USERNAME / _PASSWORD Optional SASL/SCRAM.\n  \
           KAFKA_SASL_MECHANISM            Optional override (defaults via KafkaPublisher::from_env).\n  \
           KAFKA_SECURITY_PROTOCOL         Optional override.\n  \
           OF_OPENLINEAGE_NAMESPACE        Optional, defaults to \"openfoundry\".\n"
    );
}

#[cfg(test)]
mod tests {
    use super::*;

    fn argv(extra: &[&str]) -> Vec<String> {
        let mut v = vec!["reindex-request".to_string()];
        v.extend(extra.iter().map(|s| s.to_string()));
        v
    }

    #[test]
    fn parse_minimum() {
        let cli = Cli::parse(&argv(&["--tenant", "tenant-a"])).unwrap();
        assert_eq!(cli.tenant, "tenant-a");
        assert!(cli.type_id.is_none());
        assert!(cli.page_size.is_none());
        assert!(!cli.dry_run);
    }

    #[test]
    fn parse_full() {
        let cli = Cli::parse(&argv(&[
            "--tenant",
            "tenant-a",
            "--type-id",
            "users",
            "--page-size",
            "500",
            "--request-id",
            "req-1",
            "--dry-run",
        ]))
        .unwrap();
        assert_eq!(cli.tenant, "tenant-a");
        assert_eq!(cli.type_id.as_deref(), Some("users"));
        assert_eq!(cli.page_size, Some(500));
        assert_eq!(cli.request_id.as_deref(), Some("req-1"));
        assert!(cli.dry_run);
    }

    #[test]
    fn missing_tenant_is_rejected() {
        let err = Cli::parse(&argv(&["--type-id", "users"])).unwrap_err();
        assert!(matches!(err, CliError::Missing("tenant")));
    }

    #[test]
    fn page_size_must_be_in_range() {
        assert!(matches!(
            Cli::parse(&argv(&["--tenant", "t", "--page-size", "0"])).unwrap_err(),
            CliError::InvalidPageSize(_)
        ));
        assert!(matches!(
            Cli::parse(&argv(&["--tenant", "t", "--page-size", "10001"])).unwrap_err(),
            CliError::InvalidPageSize(_)
        ));
        assert!(matches!(
            Cli::parse(&argv(&["--tenant", "t", "--page-size", "abc"])).unwrap_err(),
            CliError::InvalidPageSize(_)
        ));
    }

    #[test]
    fn dangling_flag_is_rejected() {
        let err = Cli::parse(&argv(&["--tenant"])).unwrap_err();
        assert!(matches!(err, CliError::DanglingFlag("tenant")));
    }

    #[test]
    fn unknown_flag_is_rejected() {
        let err = Cli::parse(&argv(&["--tenant", "t", "--bogus"])).unwrap_err();
        assert!(matches!(err, CliError::UnknownArg(_)));
    }

    #[test]
    fn payload_round_trips_to_consumer_decoder() {
        // Closes the loop with the consumer: the JSON we ship MUST
        // decode through the same `decode_request` the coordinator
        // uses on the wire. If this test fails the producer and
        // consumer have drifted.
        use reindex_coordinator_service::scan::decode_request;
        let payload = ReindexRequestedV1 {
            tenant_id: "tenant-a".into(),
            type_id: Some("users".into()),
            page_size: Some(500),
            request_id: Some("req-1".into()),
        };
        let bytes = serde_json::to_vec(&payload).unwrap();
        let decoded = decode_request(&bytes).expect("consumer must decode producer output");
        assert_eq!(decoded.tenant_id, payload.tenant_id);
        assert_eq!(decoded.type_id, payload.type_id);
        assert_eq!(decoded.page_size, payload.page_size.unwrap_or(1000));
        assert_eq!(decoded.request_id, payload.request_id);
    }
}
