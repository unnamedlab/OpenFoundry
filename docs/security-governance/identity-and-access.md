# Identity and access

> **Sensitive admin surface.** Changes to identity, SSO, role administration,
> and token policy affect every other security layer. Read the
> [Security overview](./security-overview.md) for how this layer composes
> with markings, restricted views, and scoped sessions, and the
> [Shared responsibility model](./shared-responsibility-model.md) for the
> operator-vs-tenant boundary. Anything modeled on a Foundry concept must
> follow the [Foundry public-docs parity policy](../reference/foundry-public-docs-parity-policy.md).

Identity and access are one of the strongest implemented capability areas in the current repo.

## Repository signals

`identity-federation-service` already exposes first-class support for:

- registration and login
- JWT access and refresh flows (with JWKS rotation)
- MFA + WebAuthn
- OIDC sign-in (SAML sign-in flow pending — see [ROADMAP](../../ROADMAP.md))
- SCIM provisioning
- session management
- user, role, group, and permission administration
- control-panel and admin-oriented surfaces

You can see the route surface in `services/identity-federation-service/cmd/identity-federation-service/main.go` and `services/identity-federation-service/internal/server/`.

## Domain building blocks

Relevant internal packages include:

- `internal/domain/jwt` — JWT issuance + validation, JWKS handling
- `internal/domain/rbac` — role-based access primitives
- `internal/domain/mfa` — MFA / WebAuthn enrollment and verification
- `internal/domain/saml` — SAML SP (sign-in flow pending)
- `internal/domain/oauth` — OAuth/OIDC provider integration
- `internal/domain/sessions` — session lifecycle, revocation, scoped claims
- `internal/domain/scim` — SCIM resources

ABAC primitives are owned by `services/authorization-policy-service` (Cedar engine) — see [Policies and authorization](./policies-and-authorization.md).

The shared HTTP layer (`libs/auth-middleware`) extracts claims into `r.Context()` so handlers never parse JWTs themselves; this is enforced by convention in [`CLAUDE.md`](../../CLAUDE.md) §"Conventions".

## SAML and OIDC provider administration (SG.3)

The boot-time OIDC service and SAML registry are seeded from environment
config; the **durable admin source-of-truth** lives in the
`sso_providers` Postgres table introduced in migration
[`0010_slice5c_sso_persistence.sql`](../../services/identity-federation-service/internal/repo/migrations/0010_slice5c_sso_persistence.sql).
A follow-up RFC will hot-load the in-memory registries from this table.

The admin surface is bearer-protected and gated by `authmw.RequireAdmin`:

| Endpoint | Purpose |
|---|---|
| `GET /api/v1/auth/sso/providers` | List every persisted provider with secret fields masked (`client_secret_configured`, `saml_certificate_configured`). |
| `POST /api/v1/auth/sso/providers` | Register a new provider — accepts the full `CreateSsoProviderRequest` shape including `domains[]` and `attribute_mapping`. |
| `PATCH /api/v1/auth/sso/providers/{id}` | Update individual fields; missing keys preserve current values; explicit `null` clears pointer fields. |
| `DELETE /api/v1/auth/sso/providers/{id}` | Remove a row. |
| `POST /api/v1/auth/sso/providers/{id}/refresh-metadata` | For SAML providers, HTTP-GET the metadata URL via [`saml.ResolveMetadataDefaults`](../../services/identity-federation-service/internal/saml/metadata.go), update entity ID / SSO URL / certificate, and stamp `metadata_last_refreshed_at` / `certificate_expires_at`. |
| `GET /api/v1/auth/sso/providers/{id}/health` | Probe the issuer's `.well-known/openid-configuration` (OIDC) or the metadata URL (SAML) and report `overall_status` ∈ {ok, degraded, blocked} with certificate-expiry diagnostics. |

The **claim-mapping shape** is documented in
[`internal/models/sso.go`](../../services/identity-federation-service/internal/models/sso.go)
as `AttributeMapping`:

```jsonc
{
  "subject": "sub",          // claim name → external_id
  "email":   "email",        // claim name → user.email
  "name":    "name",         // claim name → user.name
  "attributes": {            // arbitrary IdP claim → user attribute
    "department": "department"
  },
  "groups": {
    "claim":         "groups",     // claim that carries the group list
    "idp_to_group": { "okta-eng": "eng" },
    "default_role":  "viewer"
  }
}
```

The OIDC and SAML callback flows continue to use the boot-time
defaults (`sub` / `email` / `name`); honouring the persisted mapping at
callback time is the SG.3 follow-up RFC.

## Login troubleshooting (SG.3)

`POST /api/v1/auth/sso/troubleshoot` is **unauthenticated** — the login
page calls it when the user can't get past the email step. The
response classifies the attempt with one of the stable wire constants
from
[`models.LoginTroubleshootState*`](../../services/identity-federation-service/internal/models/sso.go):

- `ok` — at least one healthy provider claims the email's domain.
- `unknown_domain` — no provider claims this email domain.
- `user_disabled` — the user exists but `is_active = false`.
- `provider_disabled` — the matched provider has `enabled = false`.
- `metadata_stale` — SAML metadata hasn't been refreshed in 30+ days.
- `certificate_expired` — SAML signing certificate's NotAfter is in the past.
- `certificate_expiring` — SAML certificate expires within 7 days.
- `configuration_error` — issuer or metadata URL is unreachable.

The `diagnostics[]` array carries one `{code, severity, message}` per
finding so the login UI can render translated, context-specific hints
without re-parsing the state string.

The Control Panel UI at
[`/control-panel/identity-providers`](../../apps/web/src/routes/control-panel/IdentityProvidersPage.tsx)
exposes the full admin surface plus the troubleshoot tool.

## User administration (SG.4)

The user row is the durable identity record. SG.4 (migration
[`0011_slice5d_user_admin.sql`](../../services/identity-federation-service/internal/repo/migrations/0011_slice5d_user_admin.sql))
extends the `users` table with `username`, `realm`, `last_login_at`,
`last_login_ip`, `deleted_at`, `preregistered`, and `invited_by`.
Existing rows backfill `username` from the email localpart and
`realm` from `auth_source`.

The admin surface lives at:

| Endpoint | Purpose |
|---|---|
| `GET /api/v1/users` | Bare-array list of the most recent 200 non-deleted users (legacy SDK shape). |
| `GET /api/v1/users/search` | SG.4 envelope with `{items, total}` and `q` / `organization_id` / `realm` / `status` / `include_deleted` / `limit` / `offset` query params. |
| `GET /api/v1/users/{id}/inspect` | Combined view: user core + roles + groups + token summary + IdP bindings. |
| `POST /api/v1/users/preregister` | Admin-only. Seeds a row with `preregistered = true` and an empty password hash so SSO callback or self-service registration promotes the user. |
| `PATCH /api/v1/users/{id}` | Extended patch surface: name, username, realm, is_active, mfa_enforced, organization_id, attributes. Flipping `is_active` to false automatically revokes every active refresh token (SG.4 token-policy guarantee). |
| `DELETE /api/v1/users/{id}` | Soft-delete by default — sets `deleted_at`, inactivates the user, and revokes refresh tokens in one transaction. Pass `?hard=true` for true row removal (compliance flows). |
| `POST /api/v1/users/{id}/restore` | Clears the soft-delete tombstone. The user stays inactive until an admin re-activates them. |
| `POST /api/v1/users/{id}/revoke-tokens` | Explicit token revocation without changing the user's active state. |

**Login activity** is stamped from the Login handler and both SSO
callbacks (`handlers/auth.go`, `handlers/sso.go`) via
`Repo.StampLogin`. The IP picker honours `X-Forwarded-For` first hop
and falls back to `r.RemoteAddr` so the column reflects the original
client when the gateway terminates TLS.

**Token policy.** Deactivation, soft-delete, and the explicit revoke
endpoint all call `Repo.RevokeAllUserRefreshTokens(userID, time.Now())`.
The existing refresh-token rotation logic in
[`services/identity-federation-service/internal/service`](../../services/identity-federation-service/internal/service/)
keeps the per-rotation invariants; SG.4 only adds bulk revocation
hooks layered on top.

The Control Panel UI lives at
[`/control-panel/users`](../../apps/web/src/routes/control-panel/UsersPage.tsx)
— search bar, preregister form, per-row activate / deactivate /
soft-delete / restore / revoke-tokens, and an inspect side panel.

## Group administration (SG.5)

Groups extend the user record with three concrete shapes:

| Kind | Membership source | Admin write semantics |
|---|---|---|
| `internal` | Manually managed by admins. | `PUT /groups/{id}/members/{user_id}` accepts an optional `expires_at` for time-bounded grants. |
| `external` | SCIM / IdP-synced. The handler refuses direct member mutations from non-SCIM callers in a follow-up RFC. | Treated as read-only in the UI; SCIM provisioning is the writer. |
| `rule_based` | Membership is the evaluation of `rule_query` against the user attributes graph. | The rule is the source of truth — direct member writes are rejected. |

Schema (migration
[`0012_slice5e_group_admin.sql`](../../services/identity-federation-service/internal/repo/migrations/0012_slice5e_group_admin.sql)):

- `groups` gains `kind`, `display_name`, `realm`, `organization_id`,
  `attributes`, `rule_query`, `status`, `updated_at`. `display_name`
  backfills from `name`.
- `group_members` gains `added_at`, `added_by`, `expires_at` with a
  partial index on non-null `expires_at` for the SG.5 inspect-view
  count.
- New `group_admins(group_id, user_id, scope, granted_by, created_at)`
  with scope ∈ {`manage`, `manage_members`}.
- New `group_nested_members(parent_group_id, member_group_id,
  added_at, added_by)` with a self-reference CHECK and a recursive
  cycle-detection helper in [`internal/repo/rbac.go`](../../services/identity-federation-service/internal/repo/rbac.go).

Admin endpoints (all under `/api/v1`, bearer-protected):

| Endpoint | Purpose |
|---|---|
| `GET /groups` | Bare-array list of active groups (legacy SDK shape). |
| `GET /groups/search` | SG.5 envelope `{items, total}` with `q` / `kind` / `realm` / `organization_id` / `status` / `limit` / `offset`. |
| `POST /groups`, `PATCH /groups/{id}`, `DELETE /groups/{id}` | Extended CRUD. PATCH honours nullable pointers (`**string` / `**uuid.UUID`) so JSON null clears them. |
| `GET /groups/{id}/inspect` | Combined view: group core + member counts (direct + expiring) + admins + nested parents/children + project-access hint pointing at `tenancy-organizations-service`. |
| `GET /groups/{id}/members` | Returns one row per direct member with `expires_at`. |
| `PUT /groups/{id}/members/{user_id}` | Body `{ expires_at? }` for time-bounded grants. |
| `GET/POST/DELETE /groups/{id}/admins[/{user_id}]` | Per-group admin grants. `?scope=manage_members` switches the deletion scope. |
| `GET /groups/{id}/parents`, `GET /groups/{id}/children` | Nested-membership projection. |
| `PUT/DELETE /groups/{id}/nested/{member_id}` | Nested edge CRUD; the repo rejects self-references and cycles via a recursive CTE. |

The Control Panel UI lives at
[`/control-panel/groups`](../../apps/web/src/routes/control-panel/GroupsPage.tsx)
— search/filter, create form (kind / realm / org), per-row archive /
restore / delete, and an inspect side panel that surfaces admins,
nested parents/children, direct members, and time-bounded memberships.

## Access request workflow (SG.9)

Project access requests are owned by
[`tenancy-organizations-service`](../../services/tenancy-organizations-service/),
but they compose with the group-administration model above:

- `ontology_project_access_requests` is the parent request. It records
  the requester, requested users, required reason, scope context, and
  request status.
- `ontology_project_access_request_tasks` records independent
  subtasks for direct project role grants, internal group membership,
  required marking access, and external group handoffs.
- `ontology_project_access_group_settings` is the per-project overlay
  used by the access form: group display label, `internal` /
  `external` / `rule_based` kind, group-admin reviewer IDs, custom
  form metadata, external request message/URL, and the
  `excluded_from_request_forms` flag for sensitive groups.
- `ontology_project_required_markings` lets project owners describe
  required marking-access tasks and their reviewer IDs until the full
  marking service lands under `SG.11`–`SG.16`.

Admin endpoints:

| Endpoint | Purpose |
|---|---|
| `GET /projects/{id}/access-request-form` | Returns visible requestable groups, required markings, and project-owner reviewers. Does not require existing project access because it powers denied-link / Discoverer flows. |
| `PUT/DELETE /projects/{id}/access-request-groups/{group_id}` | Configure or clear the project-local group request overlay, including external handoff and hidden-sensitive-group behavior. |
| `PUT/DELETE /projects/{id}/access-request-markings/{marking_id}` | Configure required marking access prompts and marking reviewer IDs. |
| `POST /projects/{id}/access-requests` | Creates a request with one or more subtasks. Legacy `{requested_role, reason}` still creates a direct project-role task. |
| `GET /projects/{id}/access-requests` | Project request list. Owners/admins see all; requesters see their own. |
| `GET /access-requests/inbox` | Reviewer inbox: project owners see direct-role tasks, configured group admins see group tasks, configured marking reviewers see marking tasks. |
| `POST /projects/{id}/access-requests/{request_id}/decision` | Applies an approve/deny decision to the subtasks the caller is eligible to review. Direct project-role approvals materialize `ontology_project_memberships`. |

The Control Panel project page at
[`/control-panel/projects`](../../apps/web/src/routes/control-panel/ProjectsPage.tsx)
now exposes the request-form configuration, external handoff settings,
required markings, manual request creation, and the task list inside
each request.

## Project templates (SG.26)

Project templates live in
[`tenancy-organizations-service`](../../services/tenancy-organizations-service/)
and standardize secure project creation for a space or globally:

- `ontology_project_templates` stores the repeatable definition:
  default role, point of contact, variables, folder skeleton, generated
  groups, default user/group/project-creator role grants, markings,
  project constraints, and governance tags.
- `ontology_project_template_applications` is the immutable audit row
  written when a project is created from a template. It records actor,
  template key, resolved variables, generated group IDs, applied marking
  RIDs, applied constraints, and validation checks.
- `POST /api/v1/projects/templates` is gated by
  `project_templates:write`, `projects:manage`, or
  `control_panel:write`; `GET /api/v1/projects/templates` optionally
  filters by `space_slug` while keeping global templates visible.
- `POST /api/v1/projects` accepts `template_key` and
  `template_variables`. During deployment the service validates the
  caller has the additional permissions implied by the template:
  group creation/binding, marking application or creation, constraint
  application, and sensitive project-default changes.
- Generated viewer/editor/owner groups are bound through
  `ontology_project_group_memberships` and mirrored into
  `ontology_project_access_group_settings` so access-request forms can
  route or hide those groups consistently with SG.9.

The create-project modal sends the selected template key and shows the
template's default role, groups, markings, constraints, and folder count
before the project is created.

## Application access controls (SG.27)

Application access lives in the identity service Control Panel settings
as an organization UX-scope policy, not as a data authorization system.
It controls whether platform applications are visible in the launcher,
sidebar, or direct navigation surfaces; every backend API and resource
request must still pass the normal server-side permission checks.

- `ApplicationAccessConfig` stores the application catalog, lifecycle
  stage, default visibility, allow/block rules, approval policy,
  pending change requests, and decision history.
- Rules can match application IDs, lifecycle stages, organization IDs,
  user IDs, and group IDs. Block rules take precedence. When default
  visibility is `hidden`, a matching allow rule is required.
- `POST /api/v1/application-access/evaluate` returns an explicit
  `ux_scope_only` decision with matched rule IDs and names, allowing app
  shells to hide platform surfaces without widening or replacing
  authorization.
- The web sidebar evaluates launcher entries against that endpoint and
  hides applications denied by UX-scope rules.
- `PUT /api/v1/control-panel` generates a change request and history
  entry for application-access edits. Configuration changes self-approve
  by default; approval-policy changes can require a distinct reviewer.
- The Control Panel page at
  [`/control-panel/application-access`](../../apps/web/src/routes/control-panel/ApplicationAccessPage.tsx)
  exposes the JSON editor, visibility evaluator, pending approvals, and
  history.

> Parity reference:
> [Configure application access](https://www.palantir.com/docs/foundry/administration/configure-application-access/) ·
> [Approvals overview](https://www.palantir.com/docs/foundry/approvals/overview/).

## User and group visibility controls (SG.28)

Member discovery settings live in Control Panel as privacy controls for
organization-scoped user and group lookup. They do not change roles,
group membership, resource ACLs, markings, restricted views, or any
other authorization state.

- `MemberDiscoveryConfig` stores global defaults plus per-organization
  `discover_users`, `discover_groups`, and `consumer_mode_boundary`
  overrides.
- Non-admin user/group list, search, detail, and inspection endpoints
  return `member_discovery_disabled` when discovery is disabled for the
  caller's organization.
- Administrators keep visibility through `admin`, `organization_admin`,
  `control_panel:*`, `users:*`, or `groups:*` grants, matching the
  documented rule that administrative functions continue to work.
- Every denial includes the warning that existing permissions remain in
  force while user-defined logic depending on user/group lookup may fail.
- The Control Panel page at
  [`/control-panel/member-discovery`](../../apps/web/src/routes/control-panel/MemberDiscoveryPage.tsx)
  edits defaults, organization overrides, consumer-mode boundary notes,
  and history.

> Parity reference:
> [Configure user and group visibility](https://www.palantir.com/docs/foundry/administration/configure-user-and-group-visibility) ·
> [Configure Foundry for consumer mode](https://www.palantir.com/docs/foundry/consumer-mode/foundry-consumer-setup).

## File access presets (SG.29)

File access presets live in Control Panel as named resource-creation
shortcuts for mandatory markings and local classification controls. A
preset is configuration only: it pre-fills supported creation forms, but
does not grant membership in a marking and does not bypass resource
authorization.

- `FileAccessPresetConfig` stores enabled state, the primary-org guest
  behavior, ordered presets, local access controls, supported resource
  kinds, organization scoping, and retained change history.
- `POST /api/v1/file-access-presets/visible` is the user-facing
  evaluation endpoint. It returns only presets the caller can apply for
  the requested resource kind and effective organization.
- Visibility requires `Apply marking` permission for every marking in
  the preset. OpenFoundry accepts global `markings:apply` /
  `markings:write` / `markings:manage` grants or per-marking apply IDs
  carried in claims.
- Guest sessions resolve presets against the caller's primary
  organization rather than the host organization when a primary
  organization is known.
- The create-project modal consumes the visibility endpoint for the
  `project` resource kind and sends selected preset markings to
  `POST /api/v1/projects`; tenancy merges those markings with any
  project-template markings before indexing the project.
- The Control Panel page at
  [`/control-panel/file-access-presets`](../../apps/web/src/routes/control-panel/FileAccessPresetsPage.tsx)
  edits presets, local controls JSON, default ordering, org scope,
  supported resource kinds, and history.

> Parity reference:
> [Configure file access presets](https://www.palantir.com/docs/foundry/administration/configure-file-access-presets/) ·
> [Markings](https://www.palantir.com/docs/foundry/security/markings/).

## Developer API Token Governance (SG.30)

Developer API tokens are temporary user-generated credentials for local
development. They are not production application credentials: use OAuth
clients or service users for production integrations.

- `POST /api/v1/api-keys` creates an opaque `ofapikey_...` secret,
  returns it once, and persists only its SHA-256 hash plus a visible
  prefix.
- Creation requires an explicit `expires_at` within 30 days. The token
  stores role and permission snapshots from the creating user; requested
  scopes must be a subset of those effective permissions unless the user
  is an administrator.
- `POST /api/v1/auth/api-key/exchange` turns a usable opaque key into a
  short-lived access JWT with `auth_methods=["api_key"]` and
  `api_key_id` for audit correlation. Exchange fails after expiry,
  revocation, owner disablement/deletion, or 30 days without a
  successful owner login.
- `GET /api/v1/api-keys` lists metadata and status only. The plaintext
  secret is never shown again after creation. `DELETE /api/v1/api-keys/{id}`
  revokes a key without deleting its audit metadata.
- `POST /api/v1/api-keys/leak-scan` checks local snippets or diffs for
  `ofapikey_...` patterns, redacts matches, and escalates warnings when
  the visible prefix matches one of the caller's known keys.
- The Settings API key panel requires expiry at creation, shows the
  non-production warning, exposes revoke controls, and includes the
  local token exposure check.

> Parity reference:
> [API authentication](https://www.palantir.com/docs/foundry/api/v2/general/overview/authentication/) ·
> [Manage users](https://www.palantir.com/docs/foundry/platform-security-management/manage-users).

## Third-Party Application Registration (SG.31)

Third-party application registration is the durable OAuth2 client
registry for integrations that should use production OAuth flows rather
than developer API tokens. Developer Console is the preferred
management surface; Control Panel remains available as a fallback when
Developer Console is not enabled locally.

- `POST /api/v1/third-party-applications` registers an OAuth2 client
  with owner users, managing organization, discovery organizations,
  redirect URIs, requested scopes, client type, and enabled grant types.
- Confidential clients receive a one-time `of3pa_secret_...` client
  secret. OpenFoundry stores only the SHA-256 hash, visible prefix, and
  credential timestamp. Secrets can be rotated through
  `POST /api/v1/third-party-applications/{id}/rotate-secret`.
- Public clients are limited to `authorization_code` and require PKCE in
  the downstream authorization flow. `client_credentials` is accepted
  only for confidential clients.
- When a confidential client enables `client_credentials`, identity
  creates a service user whose username is the generated client ID. The
  service user starts with no resource access; administrators must grant
  roles/permissions explicitly.
- `PUT /api/v1/third-party-applications/{id}/organizations/{org}/enablement`
  records organization-specific enablement, project-scope placeholders,
  marking restrictions, and organization-consent flags. SG.32 consumes
  this when implementing authorization-code consent and token issuance.
- Administration is gated by the third-party application administrator
  role or OAuth client management permissions (`oauth_clients:manage` /
  `third_party_applications:manage`), with read-only listing allowed via
  the matching read permissions.
- The Control Panel fallback UI lives at
  [`/control-panel/third-party-applications`](../../apps/web/src/routes/control-panel/ThirdPartyApplicationsPage.tsx).

> Parity reference:
> [Third-party applications overview](https://www.palantir.com/docs/foundry/platform-security-third-party/third-party-apps-overview/) ·
> [Registering third-party applications](https://www.palantir.com/docs/foundry/platform-security-third-party/register-3pa/) ·
> [Writing OAuth2 clients for Foundry](https://www.palantir.com/docs/foundry/platform-security-third-party/writing-oauth2-clients).

## Why this matters

This gives OpenFoundry a strong foundation for identity-aware operational workflows, not only for simple API authentication.
