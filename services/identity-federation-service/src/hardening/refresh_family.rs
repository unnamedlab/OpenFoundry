//! S3.1.f — Refresh-token family detection.
//!
//! On every refresh the server rotates the refresh token. The new
//! row points back to the previous one via `rotated_to`. If a token
//! that already has `rotated_to` set is presented again, that means
//! someone is replaying a stolen refresh token: invalidate the
//! whole family.
//!
//! This module is pure logic — it operates on a [`FamilyView`] of
//! the rows the Cassandra adapter returns and emits a
//! [`FamilyDecision`]. The handler that wires it lives in the bin
//! refactor.

use chrono::{DateTime, Utc};
use uuid::Uuid;

#[derive(Debug, Clone)]
pub struct RefreshTokenRow {
    pub token_hash: Vec<u8>,
    pub family_id: Uuid,
    pub user_id: String,
    pub issued_at: DateTime<Utc>,
    pub expires_at: DateTime<Utc>,
    pub revoked_at: Option<DateTime<Utc>>,
    pub rotated_to: Option<Vec<u8>>,
}

/// Snapshot of a refresh-token family at decision time.
#[derive(Debug, Clone)]
pub struct FamilyView {
    pub family_id: Uuid,
    pub presented: RefreshTokenRow,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum FamilyDecision {
    /// Token is current and not yet rotated → mint new pair.
    Accept,
    /// Token has expired or already been revoked → 401, no family
    /// nuke.
    RejectExpired,
    /// Token has already been rotated → replay attack. The whole
    /// family must be revoked atomically by the caller.
    RevokeFamily { reason: ReplayReason },
}

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum ReplayReason {
    AlreadyRotated,
    AlreadyRevoked,
}

pub fn evaluate(view: &FamilyView, now: DateTime<Utc>) -> FamilyDecision {
    let row = &view.presented;
    if row.rotated_to.is_some() {
        return FamilyDecision::RevokeFamily {
            reason: ReplayReason::AlreadyRotated,
        };
    }
    if row.revoked_at.is_some() {
        return FamilyDecision::RevokeFamily {
            reason: ReplayReason::AlreadyRevoked,
        };
    }
    if row.expires_at <= now {
        return FamilyDecision::RejectExpired;
    }
    FamilyDecision::Accept
}

#[cfg(test)]
mod tests {
    use super::*;
    use chrono::Duration;

    fn row(now: DateTime<Utc>) -> RefreshTokenRow {
        RefreshTokenRow {
            token_hash: vec![0xde, 0xad],
            family_id: Uuid::now_v7(),
            user_id: "u1".into(),
            issued_at: now - Duration::minutes(1),
            expires_at: now + Duration::days(30),
            revoked_at: None,
            rotated_to: None,
        }
    }

    #[test]
    fn happy_path_accepts() {
        let now = Utc::now();
        let r = row(now);
        let view = FamilyView {
            family_id: r.family_id,
            presented: r,
        };
        assert_eq!(evaluate(&view, now), FamilyDecision::Accept);
    }

    #[test]
    fn replay_after_rotation_revokes_family() {
        let now = Utc::now();
        let mut r = row(now);
        r.rotated_to = Some(vec![0xbe, 0xef]);
        let view = FamilyView {
            family_id: r.family_id,
            presented: r,
        };
        assert_eq!(
            evaluate(&view, now),
            FamilyDecision::RevokeFamily {
                reason: ReplayReason::AlreadyRotated
            }
        );
    }

    #[test]
    fn expired_token_rejected_without_family_nuke() {
        let now = Utc::now();
        let mut r = row(now);
        r.expires_at = now - Duration::seconds(1);
        let view = FamilyView {
            family_id: r.family_id,
            presented: r,
        };
        assert_eq!(evaluate(&view, now), FamilyDecision::RejectExpired);
    }
}
