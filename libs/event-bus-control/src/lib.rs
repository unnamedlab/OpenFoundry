pub mod connectors;
pub mod contracts;
pub mod publisher;
pub mod schemas;
pub mod subscriber;
pub mod topics;
pub mod workflows;

pub use publisher::{PublishError, Publisher};
pub use schemas::Event;
pub use subscriber::SubscribeError;

pub async fn connect(
    nats_url: &str,
) -> Result<async_nats::jetstream::Context, async_nats::ConnectError> {
    let client = async_nats::connect(nats_url).await?;
    Ok(async_nats::jetstream::new(client))
}
