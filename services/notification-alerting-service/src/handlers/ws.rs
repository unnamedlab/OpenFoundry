use axum::extract::ws::{Message, WebSocket};
use axum::{
    Json,
    extract::{Query, State, WebSocketUpgrade},
    http::StatusCode,
    response::IntoResponse,
};
use chrono::Utc;
use event_bus::{
    subscriber,
    topics::{streams, subjects},
};
use futures::StreamExt;
use serde::Deserialize;
use uuid::Uuid;

use crate::{
    AppState,
    handlers::send::{latest_notifications, unread_count},
    models::notification::{NotificationEvent, WebSocketQuery, WebSocketTicketResponse},
};

const WS_TICKET_TTL_SECS: i64 = 90;

pub async fn issue_ws_ticket(
    State(state): State<AppState>,
    auth_middleware::layer::AuthUser(claims): auth_middleware::layer::AuthUser,
) -> impl IntoResponse {
    let now = Utc::now().timestamp();
    let ticket_claims = auth_middleware::Claims {
        sub: claims.sub,
        iat: now,
        exp: now + WS_TICKET_TTL_SECS,
        iss: state.jwt_config.issuer().map(str::to_string),
        aud: state.jwt_config.audience().map(str::to_string),
        jti: Uuid::now_v7(),
        email: claims.email,
        name: claims.name,
        roles: vec![],
        permissions: vec![],
        org_id: claims.org_id,
        attributes: serde_json::json!({}),
        auth_methods: vec!["ws_ticket".to_string()],
        token_use: Some("ws_ticket".to_string()),
        api_key_id: None,
        session_kind: claims.session_kind,
        session_scope: claims.session_scope,
    };

    match auth_middleware::jwt::encode_token(&state.jwt_config, &ticket_claims) {
        Ok(ticket) => Json(WebSocketTicketResponse {
            ticket,
            expires_in: WS_TICKET_TTL_SECS,
        })
        .into_response(),
        Err(error) => {
            tracing::error!("failed to issue websocket ticket: {error}");
            StatusCode::INTERNAL_SERVER_ERROR.into_response()
        }
    }
}

pub async fn notifications_ws(
    ws: WebSocketUpgrade,
    State(state): State<AppState>,
    Query(params): Query<WebSocketQuery>,
) -> impl IntoResponse {
    let claims = match auth_middleware::jwt::decode_token(&state.jwt_config, &params.ticket) {
        Ok(claims) if claims.token_use.as_deref() == Some("ws_ticket") => claims,
        Err(_) => return StatusCode::UNAUTHORIZED.into_response(),
        _ => return StatusCode::UNAUTHORIZED.into_response(),
    };

    ws.on_upgrade(move |socket| websocket_loop(socket, state, claims.sub))
}

async fn websocket_loop(mut socket: WebSocket, state: AppState, user_id: uuid::Uuid) {
    let notifications = latest_notifications(&state, user_id, 20)
        .await
        .unwrap_or_default();
    let unread = unread_count(&state, Some(user_id)).await.unwrap_or(0);
    let snapshot = serde_json::json!({
        "kind": "snapshot",
        "data": notifications,
        "unread_count": unread,
    });

    if socket
        .send(Message::Text(snapshot.to_string().into()))
        .await
        .is_err()
    {
        return;
    }

    let Some(bus) = state.notification_bus.clone() else {
        return;
    };

    let stream = match subscriber::ensure_stream(
        &bus.jetstream(),
        streams::NOTIFICATIONS,
        &[subjects::NOTIFICATIONS],
    )
    .await
    {
        Ok(stream) => stream,
        Err(error) => {
            tracing::warn!(?error, "failed to ensure notification stream for websocket");
            return;
        }
    };
    let consumer_name = format!("notifications-ws-{}", Uuid::now_v7());
    let consumer =
        match subscriber::create_consumer(&stream, &consumer_name, Some(bus.subject())).await {
            Ok(consumer) => consumer,
            Err(error) => {
                tracing::warn!(?error, "failed to create notification websocket consumer");
                return;
            }
        };
    let mut messages = match consumer.messages().await {
        Ok(messages) => messages,
        Err(error) => {
            tracing::warn!(?error, "failed to stream notification websocket messages");
            let _ = stream.delete_consumer(&consumer_name).await;
            return;
        }
    };

    while let Some(message) = messages.next().await {
        let Ok(message) = message else {
            tracing::warn!("notification websocket consumer stream failed");
            break;
        };
        let event = match serde_json::from_slice::<NotificationEnvelope>(&message.payload) {
            Ok(envelope) => envelope.payload,
            Err(error) => {
                tracing::warn!(?error, "failed to decode notification event payload");
                let _ = message.ack().await;
                continue;
            }
        };

        if !targets_user(&event, user_id) {
            let _ = message.ack().await;
            continue;
        }

        let payload = match serde_json::to_string(&event) {
            Ok(payload) => payload,
            Err(error) => {
                tracing::warn!(?error, "failed to serialize notification websocket event");
                let _ = message.ack().await;
                continue;
            }
        };

        let _ = message.ack().await;
        if socket.send(Message::Text(payload.into())).await.is_err() {
            break;
        }
    }

    let _ = stream.delete_consumer(&consumer_name).await;
}

fn targets_user(event: &NotificationEvent, user_id: uuid::Uuid) -> bool {
    event
        .user_id
        .map(|target| target == user_id)
        .unwrap_or(true)
}

#[derive(Debug, Deserialize)]
struct NotificationEnvelope {
    payload: NotificationEvent,
}
