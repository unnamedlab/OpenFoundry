//! `ai-sink` binary substrate. The Kafka consumer + Iceberg writer
//! land in a follow-up PR (S5.3.b runtime). For now the binary boots
//! tracing and exits — same pattern as `audit-sink`.

fn main() {
    tracing_subscriber::fmt().json().with_target(false).init();
    tracing::info!("ai-sink substrate boot — runtime loop pending");
}
