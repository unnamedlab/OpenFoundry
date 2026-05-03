// S3.1.i — Cedar policies for `identity-federation-service` admin
// operations. Loaded by the bin via `authz-cedar` (per ADR-0027).
// The `User`/`Group`/`Role`/`JwksKey`/`ScimUser`/`ScimGroup` entity
// types and the matching admin actions are declared in the bundled
// schema at `libs/authz-cedar/cedar_schema.cedarschema`.
//
// Resources owned by this service:
//   - JwksKey   (kid: string, status: active|grace|retired)
//   - ScimUser  (id: string)
//   - ScimGroup (id: string)
//
// Actions (corresponding to handlers, all under `/_admin/...` or
// `/scim/v2/...`):
//   - Action::"rotate_jwks"
//   - Action::"retire_jwks"
//   - Action::"scim_provision_user"
//   - Action::"scim_deprovision_user"
//   - Action::"scim_provision_group"

// Only members of the `IdentityKeyRotators` group may rotate or
// retire signing keys. The principal must additionally be MFA-
// stepped-up within the last 5 minutes (claim `mfa_age_secs`).
@id("identity-jwks-rotation")
permit (
    principal in Group::"IdentityKeyRotators",
    action in [Action::"rotate_jwks", Action::"retire_jwks"],
    resource is JwksKey
)
when {
    principal has mfa_age_secs && principal.mfa_age_secs <= 300
};

// SCIM provisioning is allowed only from a service-account principal
// holding the `scim_writer` role. Human operators must go through
// the workforce SCIM consumer instead.
@id("identity-scim-provisioning")
permit (
    principal in Role::"scim_writer",
    action in [
        Action::"scim_provision_user",
        Action::"scim_deprovision_user",
        Action::"scim_provision_group"
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
@id("identity-scim-forbid-human")
forbid (
    principal,
    action == Action::"scim_provision_user",
    resource
)
when {
    principal has kind && principal.kind == "human"
};
