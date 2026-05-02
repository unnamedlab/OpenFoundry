// S3.1.i — Cedar policies for `identity-federation-service` admin
// operations. Loaded by the bin via `authz-cedar` (per ADR-0023).
// The PrincipalUuid + ActionId conventions are inherited from
// `libs/authz-cedar`.
//
// Resources owned by this service:
//   - JwksKey            (kid: string, status: active|grace|retired)
//   - ScimUser           (id: string)
//   - ScimGroup          (id: string)
//
// Actions (corresponding to handlers, all under `/_admin/...` or
// `/scim/v2/...`):
//   - Action::"rotate_jwks"
//   - Action::"retire_jwks"
//   - Action::"scim_provision_user"
//   - Action::"scim_deprovision_user"
//   - Action::"scim_provision_group"

// Only members of the `IdentityKeyRotators` group may rotate or
// retire signing keys. The activator must additionally be MFA-
// stepped-up within the last 5 minutes (claim `mfa_age_secs`).
permit (
    principal in OF::Group::"IdentityKeyRotators",
    action in [OF::Action::"rotate_jwks", OF::Action::"retire_jwks"],
    resource is OF::JwksKey
)
when {
    principal has mfa_age_secs && principal.mfa_age_secs <= 300
};

// SCIM provisioning is allowed only from a service-account principal
// holding the `scim_writer` role. Human operators must go through
// the workforce SCIM consumer instead.
permit (
    principal in OF::Role::"scim_writer",
    action in [
        OF::Action::"scim_provision_user",
        OF::Action::"scim_deprovision_user",
        OF::Action::"scim_provision_group"
    ],
    resource
)
when {
    principal has kind && principal.kind == "service_account"
};

// Forbid any human principal (not a service account) from invoking
// SCIM bulk-provisioning, regardless of group membership. This
// catches the common mistake of granting `scim_writer` to a real
// user during onboarding.
forbid (
    principal,
    action == OF::Action::"scim_provision_user",
    resource
)
when {
    principal has kind && principal.kind == "human"
};
