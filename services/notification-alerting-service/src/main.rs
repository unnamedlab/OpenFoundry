mod config;
mod handlers;
mod models;

use std::net::SocketAddr;

use auth_middleware::jwt::JwtConfig;
use axum::{
    Router,
    routing::{get, patch, post},
};
use config::AppConfig;
use event_bus_control::{
    Publisher, connect, subscriber,
    topics::{streams, subjects},
};
use lettre::{
    AsyncSmtpTransport, Tokio1Executor, message::Mailbox,
    transport::smtp::authentication::Credentials,
};
use models::notification::NotificationEvent;
use sqlx::{PgPool, postgres::PgPoolOptions};
use tracing_subscriber::EnvFilter;

const SERVICE_NAME: &str = "notification-alerting-service";

type EmailSender = AsyncSmtpTransport<Tokio1Executor>;

#[derive(Clone)]
pub struct AppState {
    pub db: PgPool,
    pub http_client: reqwest::Client,
    pub email_sender: Option<EmailSender>,
    pub email_from: Option<Mailbox>,
    pub jwt_config: JwtConfig,
    pub notification_bus: Option<NotificationBus>,
}

#[derive(Clone)]
pub struct NotificationBus {
    jetstream: async_nats::jetstream::Context,
    publisher: Publisher,
    subject: String,
}

impl NotificationBus {
    fn new(jetstream: async_nats::jetstream::Context) -> Self {
        let subject = format!("{}.{}", subjects::NOTIFICATIONS, SERVICE_NAME);
        let publisher = Publisher::new(jetstream.clone(), SERVICE_NAME);
        Self {
            jetstream,
            publisher,
            subject,
        }
    }

    pub fn jetstream(&self) -> async_nats::jetstream::Context {
        self.jetstream.clone()
    }

    pub fn subject(&self) -> &str {
        &self.subject
    }

    pub async fn stream(&self) -> Result<async_nats::jetstream::stream::Stream, String> {
        self.jetstream
            .get_stream(streams::NOTIFICATIONS)
            .await
            .map_err(|error| error.to_string())
    }

    async fn publish(&self, event: NotificationEvent) -> Result<(), String> {
        let event_type = event.kind.clone();
        self.publisher
            .publish(&self.subject, &event_type, event)
            .await
            .map_err(|error| error.to_string())
    }
}

impl AppState {
    pub async fn publish_notification_event(&self, event: NotificationEvent) -> Result<(), String> {
        let Some(bus) = self.notification_bus.as_ref() else {
            return Ok(());
        };

        bus.publish(event).await
    }
}

#[tokio::main]
async fn main() -> Result<(), Box<dyn std::error::Error>> {
    tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::try_from_default_env().unwrap_or_else(|_| {
            EnvFilter::new("notification_alerting_service=info,tower_http=info")
        }))
        .init();

    let config = AppConfig::from_env()?;
    let db = PgPoolOptions::new()
        .max_connections(10)
        .connect(&config.database_url)
        .await?;
    sqlx::migrate!("./migrations").run(&db).await?;

    let notification_bus = build_notification_bus(&config).await?;
    let email_sender = build_email_sender(&config);
    let email_from = build_email_from(&config);
    let jwt_config = JwtConfig::new(&config.jwt_secret).with_env_defaults();

    let state = AppState {
        db,
        http_client: reqwest::Client::new(),
        email_sender,
        email_from,
        jwt_config: jwt_config.clone(),
        notification_bus,
    };

    let protected_routes = Router::new()
        .route("/notifications", get(handlers::history::list_notifications))
        .route(
            "/notifications/:notification_id/read",
            patch(handlers::history::mark_read),
        )
        .route(
            "/notifications/read-all",
            post(handlers::history::mark_all_read),
        )
        .route(
            "/notifications/preferences",
            get(handlers::preferences::get_preferences)
                .put(handlers::preferences::update_preferences),
        )
        .route(
            "/notifications/ws-ticket",
            post(handlers::ws::issue_ws_ticket),
        )
        .route(
            "/notifications/send",
            post(handlers::send::send_notification),
        )
        .layer(axum::middleware::from_fn_with_state(
            jwt_config,
            auth_middleware::layer::auth_layer,
        ));

    let app = Router::new()
        .nest(
            "/api/v1",
            Router::new()
                .route("/notifications/ws", get(handlers::ws::notifications_ws))
                .merge(protected_routes),
        )
        .route(
            "/internal/notifications",
            post(handlers::send::internal_send_notification),
        )
        .with_state(state);

    let addr: SocketAddr = format!("{}:{}", config.host, config.port).parse()?;
    tracing::info!(%addr, "starting notification-alerting-service");

    let listener = tokio::net::TcpListener::bind(addr).await?;
    axum::serve(listener, app).await?;
    Ok(())
}

async fn build_notification_bus(config: &AppConfig) -> Result<Option<NotificationBus>, String> {
    let Some(nats_url) = config.nats_url.as_deref() else {
        tracing::warn!("NATS_URL not configured; distributed websocket notifications are disabled");
        return Ok(None);
    };

    let jetstream = connect(nats_url).await.map_err(|error| error.to_string())?;
    subscriber::ensure_stream(
        &jetstream,
        streams::NOTIFICATIONS,
        &[subjects::NOTIFICATIONS],
    )
    .await
    .map_err(|error| error.to_string())?;

    Ok(Some(NotificationBus::new(jetstream)))
}

fn build_email_sender(config: &AppConfig) -> Option<EmailSender> {
    let host = config.smtp_host.as_deref()?;
    let mut builder = AsyncSmtpTransport::<Tokio1Executor>::relay(host)
        .ok()?
        .port(config.smtp_port.unwrap_or(587));

    if let (Some(username), Some(password)) =
        (config.smtp_username.as_ref(), config.smtp_password.as_ref())
    {
        builder = builder.credentials(Credentials::new(username.clone(), password.clone()));
    }

    Some(builder.build())
}

fn build_email_from(config: &AppConfig) -> Option<Mailbox> {
    let address = config.smtp_from_address.as_deref()?.parse().ok()?;
    Some(Mailbox::new(config.smtp_from_name.clone(), address))
}
