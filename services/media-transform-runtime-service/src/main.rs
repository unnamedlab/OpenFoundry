//! `media-transform-runtime-service` binary entry point.
//!
//! Boots an Axum server on `MEDIA_TRANSFORM_PORT` (default 50173) with
//! the routes defined in [`media_transform_runtime_service::build_router`].

use std::net::SocketAddr;

use media_transform_runtime_service::{AppState, build_router};
use tracing_subscriber::EnvFilter;

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(
            EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| EnvFilter::new("media_transform_runtime_service=info,tower_http=info")),
        )
        .init();

    let host = std::env::var("MEDIA_TRANSFORM_HOST").unwrap_or_else(|_| "0.0.0.0".into());
    let port: u16 = std::env::var("MEDIA_TRANSFORM_PORT")
        .ok()
        .and_then(|p| p.parse().ok())
        .unwrap_or(50173);
    let addr: SocketAddr = format!("{host}:{port}").parse()?;

    let router = build_router(AppState);
    let listener = tokio::net::TcpListener::bind(addr).await?;
    tracing::info!(%addr, "media-transform-runtime-service listening");
    axum::serve(listener, router.into_make_service()).await?;
    Ok(())
}
