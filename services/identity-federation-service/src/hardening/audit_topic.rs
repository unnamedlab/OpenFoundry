//! S3.1.g — Audit topic + event DTOs.
//!
//! All identity events publish to Kafka topic `audit.identity.v1`.
//! Schema is JSON-encoded and validated against an Apicurio Schema
//! Registry entry per ADR-0017. The producer wiring lives in the
//! bin (S4 — `event-bus-control` consumer-driven publisher); this
//! module owns the constants + DTOs so unit tests + Cedar policies
//! can reference them.

use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use uuid::Uuid;

/// Kafka topic for identity audit events.
pub const TOPIC: &str = "audit.identity.v1";

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
#[serde(tag = "kind", rename_all = "snake_case")]
pub enum IdentityAuditEvent {
    Login {
        user_id: String,
        ip: String,
        method: String,
    },
    Logout {
        user_id: String,
    },
    MfaChallenge {
        user_id: String,
        factor: String,
        outcome: MfaOutcome,
    },
    KeyRotation {
        old_kid: String,
        new_kid: String,
        actor: String,
    },
    PasswordReset {
        user_id: String,
        actor: String,
    },
    RefreshTokenReplay {
        user_id: String,
        family_id: Uuid,
    },
    ScimUserProvisioned {
        user_id: String,
        actor: String,
        external_id: Option<String>,
    },
}

#[derive(Debug, Clone, Copy, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum MfaOutcome {
    Pass,
    Fail,
    Lockout,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AuditEnvelope {
    pub event_id: Uuid,
    pub at: DateTime<Utc>,
    pub correlation_id: Uuid,
    pub payload: IdentityAuditEvent,
}

impl AuditEnvelope {
    pub fn new(correlation_id: Uuid, payload: IdentityAuditEvent) -> Self {
        Self {
            event_id: Uuid::now_v7(),
            at: Utc::now(),
            correlation_id,
            payload,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn login_event_round_trip() {
        let env = AuditEnvelope::new(
            Uuid::now_v7(),
            IdentityAuditEvent::Login {
                user_id: "u1".into(),
                ip: "10.0.0.1".into(),
                method: "password".into(),
            },
        );
        let s = serde_json::to_string(&env).unwrap();
        let back: AuditEnvelope = serde_json::from_str(&s).unwrap();
        assert_eq!(back.payload, env.payload);
    }

    #[test]
    fn topic_is_pinned() {
        assert_eq!(TOPIC, "audit.identity.v1");
    }
}
