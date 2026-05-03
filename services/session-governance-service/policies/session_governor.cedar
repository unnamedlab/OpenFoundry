// S3.3 — Cedar policies for `session-governance-service` revocation
// surface. Loaded by the bin via `authz-cedar` (per ADR-0023). The
// PrincipalUuid + ActionId conventions are inherited from
// `libs/authz-cedar`.
//
// Resources owned by this service:
//   - SessionId      (session_id: string)
//   - UserSessionSet (user_id: string)
//
// Actions (corresponding to handlers under `/governance/...`):
//   - Action::"revoke_session"        (POST /governance/sessions/{id}/revoke)
//   - Action::"revoke_user_sessions"  (POST /governance/users/{id}/revoke)
//   - Action::"read_session_status"   (GET  /governance/sessions/{id}/status)

// Only members of the `admin:session-governor` role may operate the
// governance surface. Membership is asserted by the JWT `roles` claim
// and mirrored into Cedar by `authz-cedar` at load time. The platform
// `admin` role keeps break-glass access; authz-cedar projects
// platform admins as members of every Role for emergency reasons.
permit (
    principal in OF::Role::"admin:session-governor",
    action in [
        OF::Action::"revoke_session",
        OF::Action::"revoke_user_sessions",
        OF::Action::"read_session_status"
    ],
    resource
);

// Forbid consumer-mode principals (external/guest sessions) from ever
// invoking governance actions, even if they were mistakenly granted
// the role through a misconfigured group sync.
forbid (
    principal,
    action in [
        OF::Action::"revoke_session",
        OF::Action::"revoke_user_sessions",
        OF::Action::"read_session_status"
    ],
    resource
)
when {
    principal has kind && principal.kind == "consumer"
};

// User-wide revocation is destructive enough that it must be issued
// from an MFA-stepped-up session within the last 5 minutes. Mirrors
// the rotate_jwks gate in `services/identity-federation-service/policies/identity_admin.cedar`.
forbid (
    principal,
    action == OF::Action::"revoke_user_sessions",
    resource
)
unless {
    principal has mfa_age_secs && principal.mfa_age_secs <= 300
};
