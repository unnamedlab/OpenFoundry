# Foundry Security and Governance 1:1 parity checklist

Date: 2026-05-11
Scope: public-docs-based parity plan for OpenFoundry's security and governance
surfaces: shared responsibility framing, identity provider integrations,
SAML/OIDC authentication, user and group administration, group realms,
organizations, spaces, enrollment/organization permissions, project security
boundaries, discretionary roles, custom role sets, access requests, Approvals
handoffs, mandatory access controls through Markings, marking categories,
marking permissions, marking inheritance and propagation, scoped sessions,
restricted views, granular policies, row-level/object-level permissions,
marking-backed restricted views, project templates, file access presets,
application access controls, user/group discovery controls, OAuth2 third-party
applications, Developer Console scopes, API tokens, service users, network
egress policies, retention policies, email content redaction, audit logs,
audit delivery to SIEM or datasets, audit categories, action logs, security
monitoring, consumer-mode privacy isolation, branch-aware security resources,
Marketplace/product packaging constraints, usage/operations governance, and
production-readiness guardrails for secure collaborative data operations.

This document is intentionally implementation-oriented. It does not attempt to
clone Palantir branding, private source code, proprietary assets, screenshots,
or any non-public behavior. The target is **functional parity based on public
Palantir Foundry documentation**: the same product concepts, comparable
security/governance administration workflows, compatible resource models where
useful, and OpenFoundry-native implementation details that can be tested
locally.

## Parity scope boundary

All checklist work is governed by the
[Foundry public-docs parity policy](../reference/foundry-public-docs-parity-policy.md).
OpenFoundry may implement behavior described in public Palantir documentation,
but contributors must not copy private source, decompile bundles, import
tenant-specific exports, use Palantir branding, or reuse proprietary assets.
The product target is functional parity in an OpenFoundry-native implementation,
not a pixel-perfect clone.

This checklist covers platform-wide security and governance primitives that
other OpenFoundry subsystems must consume. It should integrate with Data
Foundation for datasets, transactions, lineage, retention, restricted views, and
build permissions; with Ontology/Object Views for object security and action
permissions; with Analytics Suite for BI/query/export governance; with AIP
Logic/Evals and Model Integration for user-scoped execution, service accounts,
LLM/model usage attribution, and prompt/payload redaction; with Automate/Rules
for approval and third-party-application ownership; with Streaming/Data
Connection for agent, egress, credential, and source governance; with Global
Branching for branch-aware security-resource changes; and with Product Delivery
for Marketplace packaging. It should not duplicate those product-specific
surfaces.

## Status vocabulary

| Status | Meaning |
| --- | --- |
| `todo` | Not implemented or not yet verified in OpenFoundry. |
| `partial` | Some surface exists, but behavior is incomplete or not wired end-to-end. |
| `blocked` | Requires a platform dependency, public documentation, or product decision. |
| `done` | Implemented, tested, documented, and verified through UI or API smoke tests. |

## Priority vocabulary

| Priority | Meaning |
| --- | --- |
| `P0` | Required for credible secure collaboration: authenticated users, groups, roles, projects, markings, access requests, audit logs, and permission checks. |
| `P1` | Required for Foundry-style governance beyond basic RBAC: restricted views, scoped sessions, OAuth apps, retention, egress, and security monitoring. |
| `P2` | Advanced, governance-heavy, high-scale, cross-organization, Marketplace, branch-aware, privacy, SIEM, or compliance-oriented parity. |

## Official Palantir documentation library

These public docs should be treated as the external behavioral contract while
implementing this checklist.

### Security concepts

- [Security overview](https://www.palantir.com/docs/foundry/security/overview/)
- [Shared security responsibility model](https://www.palantir.com/docs/foundry/security/shared-security-responsibility-model)
- [Protecting identity](https://www.palantir.com/docs/foundry/security/single-sign-on-security/)
- [Users and groups](https://www.palantir.com/docs/foundry/security/users-and-groups/)
- [Organizations and spaces](https://www.palantir.com/docs/foundry/security/orgs-and-spaces/)
- [Projects and roles](https://www.palantir.com/docs/foundry/security/projects-and-roles)
- [Markings](https://www.palantir.com/docs/foundry/security/markings/)
- [Restricted views](https://www.palantir.com/docs/foundry/security/restricted-views)
- [Checking permissions](https://www.palantir.com/docs/foundry/security/checking-permissions/)
- [Securing a business application](https://www.palantir.com/docs/foundry/security/securing-a-business-application)
- [Protecting your self-hosted Foundry installation](https://www.palantir.com/docs/foundry/security/protect-foundry-installation)

### Platform security management

- [Manage users](https://www.palantir.com/docs/foundry/platform-security-management/manage-users)
- [Manage groups](https://www.palantir.com/docs/foundry/platform-security-management/manage-groups)
- [Manage organizations and spaces](https://www.palantir.com/docs/foundry/platform-security-management/manage-orgs-and-spaces/)
- [Manage roles](https://www.palantir.com/docs/foundry/platform-security-management/manage-roles/)
- [Manage markings](https://www.palantir.com/docs/foundry/platform-security-management/manage-markings/)
- [Manage restricted views](https://www.palantir.com/docs/foundry/platform-security-management/manage-restricted-views/)
- [Manage Project templates](https://www.palantir.com/docs/foundry/platform-security-management/manage-project-templates)

### Administration, identity, and platform controls

- [Administration overview](https://www.palantir.com/docs/foundry/administration/overview/)
- [Control Panel](https://www.palantir.com/docs/foundry/administration/control-panel/)
- [Enrollments and organizations permissions](https://www.palantir.com/docs/foundry/administration/enrollments-and-organizations-permissions/)
- [Authentication overview](https://www.palantir.com/docs/foundry/authentication/overview/)
- [SAML getting started](https://www.palantir.com/docs/foundry/authentication/saml-getting-started)
- [Configure scoped sessions](https://www.palantir.com/docs/foundry/administration/configure-scoped-sessions/)
- [Configure application access](https://www.palantir.com/docs/foundry/administration/configure-application-access/)
- [Configure user and group visibility](https://www.palantir.com/docs/foundry/administration/configure-user-and-group-visibility)
- [Configure file access presets](https://www.palantir.com/docs/foundry/administration/configure-file-access-presets/)
- [Configure network egress](https://www.palantir.com/docs/foundry/administration/configure-egress/)
- [Email content redaction](https://www.palantir.com/docs/foundry/email/email-content-redaction/)
- [Consumer mode overview](https://www.palantir.com/docs/foundry/consumer-mode/overview/)
- [Configure Foundry for consumer mode](https://www.palantir.com/docs/foundry/consumer-mode/foundry-consumer-setup)

### Third-party applications and APIs

- [Third-party applications overview](https://www.palantir.com/docs/foundry/platform-security-third-party/third-party-apps-overview/)
- [Enabling third-party applications](https://www.palantir.com/docs/foundry/platform-security-third-party/enabling-3pa-access/)
- [Writing OAuth2 clients for Foundry](https://www.palantir.com/docs/foundry/platform-security-third-party/writing-oauth2-clients)
- [Developer Console application scopes](https://www.palantir.com/docs/foundry/developer-console/application-scopes)
- [API authentication](https://www.palantir.com/docs/foundry/api/v2/general/overview/authentication/)
- [Automate third-party application ownership](https://www.palantir.com/docs/foundry/automate/third-party-app-ownership/)

### Audit, retention, and compliance-adjacent controls

- [Audit logs overview](https://www.palantir.com/docs/foundry/security/audit-logs-overview)
- [Monitor audit logs](https://www.palantir.com/docs/foundry/security/monitor-audit-logs)
- [Audit log categories](https://www.palantir.com/docs/foundry/security/audit-log-categories)
- [Action log](https://www.palantir.com/docs/foundry/action-types/action-log)
- [Retention overview](https://www.palantir.com/docs/foundry/retention/overview/)
- [Managing retention policies](https://www.palantir.com/docs/foundry/retention/manage-retention-policies)
- [Retention policy execution](https://www.palantir.com/docs/foundry/retention/policy-execution/)
- [Enrollments and organizations retention policies](https://www.palantir.com/docs/foundry/administration/enrollments-and-organizations-retention)

### Integrated object, AI, and operational governance

- [Object permissioning: configuring restricted-view-backed object types](https://www.palantir.com/docs/foundry/object-permissioning/configuring-rv-access-controls/)
- [Object permissioning: managing object security](https://www.palantir.com/docs/foundry/object-permissioning/managing-object-security/)
- [AI FDE security and governance](https://www.palantir.com/docs/foundry/ai-fde/security-and-governance/)
- [Approvals overview](https://www.palantir.com/docs/foundry/approvals/overview/)
- [Resource Management usage types](https://www.palantir.com/docs/foundry/resource-management/usage-types)
- [Foundry platform summary for LLMs](https://www.palantir.com/docs/foundry/getting-started/foundry-platform-summary-llm)

## Target OpenFoundry resource model

The implementation should define stable OpenFoundry-owned resources that can map
to public Foundry concepts without requiring Palantir RID formats. Compatibility
aliases may be accepted at service boundaries, but persisted state should use
OpenFoundry canonical IDs.

| Public Foundry concept | OpenFoundry resource target | Required notes |
| --- | --- | --- |
| Enrollment | `security_enrollment` | Top-level platform tenancy boundary with organizations, spaces, identity providers, egress, retention, and administration roles. |
| Organization | `security_organization` | Strict membership boundary with users, guests, groups, markings, app access, scoped sessions, visibility settings, and organization roles. |
| Space | `security_space` | Store/administration boundary for projects, data retention policy scope, quotas, and resource settings. |
| User | `security_user` | Human or service principal identity with immutable ID, username, organization, attributes, groups, tokens, activity state, and audit links. |
| User attribute | `security_user_attribute` | Key/value or multi-valued identity attribute sourced from IdP or internal admin flows and used by policies/restricted views. |
| Group | `security_group` | Internal, external, or rule-based group with realm, members, nested groups, attributes, visibility, administrators, and project access. |
| Realm | `security_realm` | Identity provider or group source namespace used for users, groups, external group management, and restricted-view group matching. |
| Identity provider | `security_identity_provider` | SAML/OIDC/Central Auth style configuration with metadata, domains, attribute maps, group assignment, and certificate lifecycle. |
| Project | `security_project` | Primary collaborative security boundary with default role, explicit role grants, access requests, project templates, references, and markings. |
| Folder/file resource | `security_file_resource` | Folder or platform resource with role grants, inherited permissions, inherited markings, direct markings, lineage, and audit events. |
| Role | `security_role` | Discretionary permission bundle such as Owner/Editor/Viewer/Discoverer or custom role with operation grants and delegation rules. |
| Role set | `security_role_set` | Organization-context-specific role collection for projects, ontology, restricted views, or other resource contexts. |
| Operation | `security_operation` | Low-level permission checked by services to authorize create/read/edit/delete/manage/build/apply actions. |
| Access request | `security_access_request` | Request for project/group/marking/role access with reason, target grants, approvals workflow, status, and audit provenance. |
| Approval task | `security_approval_task` | Workflow task for access, app access, marking, egress, or policy changes with approvers, decisions, comments, and SLAs. |
| Marking category | `security_marking_category` | Administrative grouping for markings with visibility, manage permissions, immutable lifecycle, and ownership metadata. |
| Marking | `security_marking` | Mandatory access control requirement with category, members, manage/apply/remove permissions, inheritance, propagation, and audit. |
| Marking grant | `security_marking_grant` | Membership or administrative grant for a user/group on a marking, including grant source and optional expiration. |
| Scoped session | `security_scoped_session` | Organization session preset that limits active markings for a user session, with bypass/no-session settings and banner metadata. |
| Restricted view | `security_restricted_view` | Dataset-backed row-level permission resource with policy, input dataset, output view, assumed markings, transaction history, and limitations. |
| Granular policy | `security_granular_policy` | Rule tree comparing user attributes, groups, organizations, markings, columns, constants, and logical operators. |
| Object security policy | `security_object_policy` | Ontology object visibility policy derived from restricted views or object-level permission configuration. |
| Project template | `security_project_template` | Governance template for project creation, default roles, groups, markings, folder structure, constraints, and access patterns. |
| File access preset | `security_file_access_preset` | Optional preset of markings/CBAC-like controls shown to eligible users during resource creation where locally supported. |
| Application access policy | `security_application_access_policy` | Organization-level control for application/platform visibility, lifecycle-stage settings, change requests, and user/group allow/block rules. |
| Member discovery policy | `security_member_discovery_policy` | Organization privacy setting controlling user/group discoverability while preserving administrator visibility. |
| Third-party application | `security_oauth_application` | OAuth client registered in Developer Console/Control Panel with enablements, owners, grants, redirects, and service user. |
| OAuth scope policy | `security_oauth_scope_policy` | Maximum and requested scopes controlling token capabilities for authorization code or client credentials grants. |
| API token | `security_api_token` | Temporary user token or OAuth access/refresh token with expiry, scopes, owner, revocation, and audit metadata. |
| Network egress policy | `security_egress_policy` | Direct, agent-proxy, or same-region bucket policy with destination, ports, agents, state, importer/viewer grants, approvals, and revocation. |
| Retention policy | `security_retention_policy` | Space-scoped dataset transaction deletion policy with dataset selectors, transaction selectors, execution runs, and irreversible-delete safeguards. |
| Email redaction policy | `security_email_redaction_policy` | Organization policy controlling redacted/unredacted notification content by domain, group, or global mode. |
| Audit log event | `security_audit_event` | Normalized audit record with actor/session/service, action, time, resource entities, categories, origins, trace/event IDs, and sensitive handling. |
| Audit delivery config | `security_audit_delivery_config` | SIEM/API/dataset export configuration for audit log files, schema versions, date ranges, and per-organization outputs. |
| Action log object | `security_action_log_object` | Ontology object/log record representing an action submission and linked edited objects for decision/edit auditability. |
| Security finding | `security_finding` | Detected anomalous or policy-violating event from audit monitoring, permission drift, stale users, risky egress, or unsafe export. |

## Milestone A: minimum viable security and governance parity

### Identity, organizations, users, and groups

- [x] `SG.1` Security overview and shared responsibility UX (`P0`, `done` 2026-05-14)
  - Provide an OpenFoundry-native security landing page that explains identity, discretionary roles, mandatory markings, organizations, restricted views, audit logs, retention, and network controls.
  - Clearly distinguish platform operator responsibilities from customer/administrator responsibilities for data, identity, access configuration, and monitoring.
  - Link from every sensitive admin workflow to applicable local policy and public-docs parity guidance.
  - Docs: [Security overview](https://www.palantir.com/docs/foundry/security/overview/), [Shared security responsibility model](https://www.palantir.com/docs/foundry/security/shared-security-responsibility-model).
  - Implementation:
    - Landing page covering all seven control layers — [`docs/security-governance/security-overview.md`](../security-governance/security-overview.md).
    - Operator-vs-tenant responsibility split and decision tree — [`docs/security-governance/shared-responsibility-model.md`](../security-governance/shared-responsibility-model.md).
    - Cross-link banners on every sensitive admin doc page: [identity-and-access](../security-governance/identity-and-access.md), [policies-and-authorization](../security-governance/policies-and-authorization.md), [restricted-views-and-data-controls](../security-governance/restricted-views-and-data-controls.md), [audit-and-traceability](../security-governance/audit-and-traceability.md), [abac-and-cbac-model](../security-governance/abac-and-cbac-model/index.md), [policy-evaluation-flows](../security-governance/policy-evaluation-flows/index.md), [audit-model](../security-governance/audit-model/index.md).
    - Sidebar entries wired in [`docs/.vitepress/config.mts`](../.vitepress/config.mts).
    - Section index updated — [`docs/security-governance/index.md`](../security-governance/index.md).
    - In-app guidance link from the Control Panel header to the security overview and the shared responsibility model — [`apps/web/src/routes/control-panel/ControlPanelPage.tsx`](../../apps/web/src/routes/control-panel/ControlPanelPage.tsx). Deep-linking from other admin surfaces (audit explorer, auth setup, future markings/egress/retention UIs) is tracked in their respective checklist items (`SG.16`–`SG.18`, `SG.34`–`SG.38`).

- [x] `SG.2` Enrollment, organization, and space model (`P0`, `done` 2026-05-14)
  - Create and manage enrollments, organizations, and spaces with immutable IDs, names, metadata, administrators, guest memberships, quotas, and settings.
  - Enforce organization membership before resource discovery and access while preserving guest/cross-organization collaboration semantics where implemented.
  - Surface organization and space settings through a Control Panel-like administration UI.
  - Docs: [Organizations and spaces](https://www.palantir.com/docs/foundry/security/orgs-and-spaces/), [Manage organizations and spaces](https://www.palantir.com/docs/foundry/platform-security-management/manage-orgs-and-spaces/), [Control Panel](https://www.palantir.com/docs/foundry/administration/control-panel/).
  - Implementation:
    - Migration [`0008_org_admins_guests_spaces.sql`](../../services/tenancy-organizations-service/internal/repo/migrations/0008_org_admins_guests_spaces.sql) extends `tenancy_organizations` with `description`, `contact_email`, `metadata`, `settings`, `quotas` and adds three new tables: `tenancy_organization_admins`, `tenancy_organization_guests`, `tenancy_spaces`. The new spaces table is distinct from the federation `nexus_spaces`.
    - Wire types in [`internal/models/models.go`](../../services/tenancy-organizations-service/internal/models/models.go) expose `Organization` (extended), `OrganizationAdmin`, `OrganizationGuest`, `TenancySpace` and their create/update request envelopes.
    - Repo methods in [`internal/repo/repo.go`](../../services/tenancy-organizations-service/internal/repo/repo.go) cover CRUD for all four entities plus `IsOrganizationMember` / `IsOrganizationAdmin` enforcement queries that consult enrollments, admin grants, and active guest records together.
    - HTTP surface in [`internal/handlers/organization_governance.go`](../../services/tenancy-organizations-service/internal/handlers/organization_governance.go) and routes in [`internal/server/server.go`](../../services/tenancy-organizations-service/internal/server/server.go):
      - `GET/POST/DELETE /api/v1/organizations/{id}/admins[/{user_id}]`
      - `GET/POST/DELETE /api/v1/organizations/{id}/guests[/{user_id}]`
      - `GET/POST /api/v1/organizations/{id}/spaces` + `GET/PATCH/DELETE /api/v1/tenancy-spaces/{id}`
      - `GET /api/v1/organizations/{id}/membership` — composite membership probe other services call when JWT claims alone are insufficient.
    - Server fix: [`server.New`](../../services/tenancy-organizations-service/internal/server/server.go) now accepts the existing nexus `SpacesHandlers` and dependency probes, mounting the capabilities registry on `/_meta/health`. Nexus federation spaces are reachable under `/api/v1/nexus/spaces`.
    - Membership enforcement: JWT-level org gating already runs through [`Claims.AllowsOrgID`](../../libs/auth-middleware/claims.go) on every protected handler; the new `IsOrganizationMember` query and `/membership` endpoint extend the check to consult the persistent admin/guest tables. Documented in [`docs/security-governance/security-overview.md`](../security-governance/security-overview.md) §"Organizations and spaces".
    - Tests: [`internal/handlers/organization_governance_test.go`](../../services/tenancy-organizations-service/internal/handlers/organization_governance_test.go) pins the SG.2 wire shapes and covers the no-DB validation paths (missing `user_id` for admin creation, same-org guest primary, missing slug for space creation). `go test ./services/tenancy-organizations-service/...` is green.
    - Frontend client [`apps/web/src/lib/api/tenancy.ts`](../../apps/web/src/lib/api/tenancy.ts) plus admin UI [`apps/web/src/routes/control-panel/TenancyPage.tsx`](../../apps/web/src/routes/control-panel/TenancyPage.tsx) at `/control-panel/tenancy`, with a link from the Control Panel header. The page covers metadata/settings/quotas JSON editing, administrator/guest CRUD, space creation/deletion and an on-demand membership probe.
    - Router entry added in [`apps/web/src/router.tsx`](../../apps/web/src/router.tsx); the page typechecks under `tsc -b --noEmit` and lints clean.
    - Out-of-scope follow-ups tracked under their own checklist items: cross-organization collaboration semantics (`SG.41`), consumer-mode privacy (`SG.42`), per-organization scoped sessions (`SG.24`), egress import grants (`SG.34`–`SG.35`), and the full Control Panel administration shell (`SG.18`).

- [x] `SG.3` Authentication provider integrations (`P0`, `done` 2026-05-14)
  - Support SAML and OIDC identity provider configuration with domains, entity/issuer metadata, redirect/ACS URLs, signing certificates, and metadata refresh.
  - Map required and optional identity provider claims into OpenFoundry user attributes and group assignments.
  - Provide login troubleshooting states for unknown domains, disabled users, certificate/metadata failures, and stale group mappings.
  - Docs: [Authentication overview](https://www.palantir.com/docs/foundry/authentication/overview/), [SAML getting started](https://www.palantir.com/docs/foundry/authentication/saml-getting-started), [Protecting identity](https://www.palantir.com/docs/foundry/security/single-sign-on-security/).
  - Implementation:
    - Migration [`0010_slice5c_sso_persistence.sql`](../../services/identity-federation-service/internal/repo/migrations/0010_slice5c_sso_persistence.sql) creates `sso_providers` with full OIDC + SAML columns, `domains` JSONB allow-list, and metadata-refresh diagnostics (`metadata_last_refreshed_at`, `metadata_last_error`, `certificate_expires_at`).
    - Wire types in [`internal/models/sso.go`](../../services/identity-federation-service/internal/models/sso.go): extended `SsoProvider` + `SsoProviderResponse` (masks secrets + adds `saml_certificate_configured`), typed `AttributeMapping`/`GroupMapping`, `CreateSsoProviderRequest`, PATCH-shaped `UpdateSsoProviderRequest` (uses `**string` so JSON null clears pointer fields), `SsoProviderHealth`, `LoginTroubleshoot{Request,Response,Issue}` with eight stable state constants.
    - Repo in [`internal/repo/sso_providers.go`](../../services/identity-federation-service/internal/repo/sso_providers.go): list / get / get-by-slug / list-for-domain (JSONB `@>` containment query) / insert / update / delete / `RecordSsoMetadataRefresh`. Domain lists are lower-cased and deduplicated on write.
    - Admin handlers in [`internal/handlers/sso_admin.go`](../../services/identity-federation-service/internal/handlers/sso_admin.go) plus routes in [`internal/server/server.go`](../../services/identity-federation-service/internal/server/server.go), gated by `authmw.RequireAdmin()`:
      - `GET /api/v1/auth/sso/providers` (admin list — distinct from the public `/auth/sso/providers` consumed by the login page).
      - `POST/GET/PATCH/DELETE /api/v1/auth/sso/providers[/{id}]`.
      - `POST /api/v1/auth/sso/providers/{id}/refresh-metadata` — calls [`saml.ResolveMetadataDefaults`](../../services/identity-federation-service/internal/saml/metadata.go), persists the harvested entity ID / SSO URL / certificate, parses the X.509 cert's NotAfter and stamps it.
      - `GET /api/v1/auth/sso/providers/{id}/health` — probes `.well-known/openid-configuration` (OIDC) or the metadata URL (SAML) and reports `overall_status` ∈ {ok, degraded, blocked} with certificate-expiry diagnostics.
    - Unauthenticated login troubleshoot: `POST /api/v1/auth/sso/troubleshoot` (mounted on the public `/api/v1/auth` group). Classifies the attempt with stable wire constants (`ok`, `unknown_domain`, `user_disabled`, `provider_disabled`, `metadata_stale`, `certificate_expired`, `certificate_expiring`, `configuration_error`) and returns structured `diagnostics[]` so the login UI can render translated hints.
    - Pre-existing server.New compile break (test expecting old signature) fixed in [`internal/server/restricted_views_routes_test.go`](../../services/identity-federation-service/internal/server/restricted_views_routes_test.go).
    - Tests: [`internal/handlers/sso_admin_test.go`](../../services/identity-federation-service/internal/handlers/sso_admin_test.go) pins the SG.3 wire shape, asserts that `IntoResponse()` masks `client_secret` and `saml_certificate` (string-match on the marshalled JSON), covers the no-DB validation paths and pins the wire vocabulary for `LoginTroubleshootState*`. `go test ./services/identity-federation-service/...` is green; `go vet` clean.
    - Frontend client [`apps/web/src/lib/api/auth.ts`](../../apps/web/src/lib/api/auth.ts) extended with `getSsoProvider`, `updateSsoProvider`, `refreshSsoProviderMetadata`, `checkSsoProviderHealth`, `troubleshootSsoLogin`, plus the `SsoProviderHealth` and `LoginTroubleshootResponse` types. Admin UI page [`apps/web/src/routes/control-panel/IdentityProvidersPage.tsx`](../../apps/web/src/routes/control-panel/IdentityProvidersPage.tsx) at `/control-panel/identity-providers` covers register/edit/enable/disable/refresh/health-probe/delete with an inline create form (separate OIDC vs SAML field groups) and the troubleshoot tool. Router entry added in [`apps/web/src/router.tsx`](../../apps/web/src/router.tsx); Control Panel header now links it. `tsc -b --noEmit` clean, eslint clean.
    - Out-of-scope follow-ups tracked under their own checklist items: hot-loading the persisted providers into the in-memory OIDC service / SAML registry, honouring `AttributeMapping` at callback time, group→role auto-mapping (`SG.5`), claim-driven org assignment (`SG.4`), and per-org IdP rules (covered partially by control-panel `identity_provider_mappings` and to be fully migrated under `SG.42`).

- [x] `SG.4` User administration (`P0`, `done` 2026-05-14)
  - List, search, view, preregister, activate/inactivate, delete/undelete where supported, and inspect users.
  - Track user ID, username, organization, realm, groups, attributes, login activity, token state, guest memberships, and audit links.
  - Disable or invalidate tokens for inactive/disabled users according to configurable policy.
  - Docs: [Manage users](https://www.palantir.com/docs/foundry/platform-security-management/manage-users), [Users and groups](https://www.palantir.com/docs/foundry/security/users-and-groups/).
  - Implementation:
    - Migration [`0011_slice5d_user_admin.sql`](../../services/identity-federation-service/internal/repo/migrations/0011_slice5d_user_admin.sql) adds `username`, `realm`, `last_login_at`, `last_login_ip`, `deleted_at`, `preregistered`, `invited_by` to `users`; backfills `username` from the email localpart and `realm` from `auth_source`; adds the `LOWER(username)` unique index and indexes on `deleted_at` / `realm`.
    - Wire types in [`internal/models/models.go`](../../services/identity-federation-service/internal/models/models.go) extend `User` with the new fields (password_hash never serialised). [`internal/models/rbac.go`](../../services/identity-federation-service/internal/models/rbac.go) extends `UpdateUserRequest` to honour username/realm/organization_id/attributes patches, and adds `PreregisterUserRequest`, `ListUsersFilter`, `ListUsersResponse`, `UserInspection`, `GroupBrief`, `TokenSummary`, `ExternalBinding`.
    - Repo work in [`internal/repo/repo.go`](../../services/identity-federation-service/internal/repo/repo.go) (canonical `userSelectColumns`, `FindUserByEmail/ID/Username`, refactored `scanUser`) and [`internal/repo/rbac.go`](../../services/identity-federation-service/internal/repo/rbac.go) — `ListUsersFiltered` (substring search + org/realm/status filters + offset pagination + total count), `PreregisterUser` (transactional with role + group seeding), `SoftDeleteUser` (sets `deleted_at`, inactivates, revokes tokens in one tx), `UndeleteUser`, `RevokeAllUserRefreshTokens`, `StampLogin`, `SummarizeUserTokens`, `ListUserExternalIdentities`, `ListUserGroups`, `CountActiveAPIKeys`.
    - Handlers in [`internal/handlers/rbac.go`](../../services/identity-federation-service/internal/handlers/rbac.go) (extended `UpdateUser` auto-revokes tokens on `is_active=false` transition; `DeleteUser` defaults to soft delete with `?hard=true` escape hatch) and new file [`internal/handlers/user_admin.go`](../../services/identity-federation-service/internal/handlers/user_admin.go) (`SearchUsers`, `PreregisterUser`, `RestoreUser`, `RevokeUserTokens`, `InspectUser`).
    - Login activity stamping wired in [`auth.go`](../../services/identity-federation-service/internal/handlers/auth.go) (Login) and [`sso.go`](../../services/identity-federation-service/internal/handlers/sso.go) (OIDC callback + SAML ACS), with a `clientIP` helper that honours `X-Forwarded-For` first-hop.
    - Routes registered in [`internal/server/server.go`](../../services/identity-federation-service/internal/server/server.go): `GET /users/search`, `GET /users/{id}/inspect`, `POST /users/{id}/restore`, `POST /users/{id}/revoke-tokens`, `POST /users/preregister` (gated by `authmw.RequireAdmin()`).
    - Tests in [`internal/handlers/user_admin_test.go`](../../services/identity-federation-service/internal/handlers/user_admin_test.go) pin the SG.4 wire shape (`User`, `UserInspection`, `ListUsersResponse`), assert `password_hash` is not serialised, and cover the no-DB validation paths (bad status, bad org UUID, empty body, missing auth claims). `go test ./services/identity-federation-service/...` is green; `go vet` clean.
    - Frontend client [`apps/web/src/lib/api/users-admin.ts`](../../apps/web/src/lib/api/users-admin.ts) + admin page [`apps/web/src/routes/control-panel/UsersPage.tsx`](../../apps/web/src/routes/control-panel/UsersPage.tsx) at `/control-panel/users` — search/filter bar, preregister form, per-row activate / deactivate / soft-delete / restore / revoke-tokens, inspect side panel. Router entry in [`apps/web/src/router.tsx`](../../apps/web/src/router.tsx); Control Panel header now links it. `tsc -b --noEmit` clean, eslint clean.
    - Out-of-scope follow-ups tracked under their own checklist items: cross-service guest-membership fetch from `tenancy-organizations-service` (`SG.2`), audit-event link surfacing (`SG.16`–`SG.17`), token-policy configurability per organization (`SG.30`), and group-mapping recertification (`SG.5` / `SG.52`).

- [x] `SG.5` Group administration (`P0`, `done` 2026-05-14)
  - Support internal, external, and rule-based groups with immutable group IDs, display names, descriptions, realm, nested members, attributes, and organization visibility.
  - Provide group administrators with manage permissions and manage membership permissions, including optional membership expiration.
  - Show direct and inherited project access for each group to support safe membership decisions.
  - Docs: [Manage groups](https://www.palantir.com/docs/foundry/platform-security-management/manage-groups), [Users and groups](https://www.palantir.com/docs/foundry/security/users-and-groups/).
  - Implementation:
    - Migration [`0012_slice5e_group_admin.sql`](../../services/identity-federation-service/internal/repo/migrations/0012_slice5e_group_admin.sql) adds `kind` (`internal`/`external`/`rule_based`), `display_name`, `realm`, `organization_id`, `attributes` JSONB, `rule_query` JSONB, `status`, and `updated_at` to `groups`; extends `group_members` with `added_at` / `added_by` / `expires_at` (partial index on non-null `expires_at`); creates `group_admins` (scope ∈ {`manage`, `manage_members`}) and `group_nested_members` (self-reference + cycle CHECKs).
    - Wire types in [`internal/models/rbac.go`](../../services/identity-federation-service/internal/models/rbac.go): extended `Group`, new `CreateGroupRequest`/`UpdateGroupRequest` (nullable pointers via `**T` so JSON null clears fields), `ListGroupsFilter`, `ListGroupsResponse`, `GroupAdmin`, `CreateGroupAdminRequest`, `GroupMember`, `AddGroupMemberRequest`, `GroupNestedEdge`, `GroupInspection`. Stable wire constants for kinds, statuses, and admin scopes.
    - Repo work in [`internal/repo/rbac.go`](../../services/identity-federation-service/internal/repo/rbac.go): canonical `groupSelectColumns`, refactored `scanGroupSingle` / `scanGroupRows`, `ListGroupsFiltered` (substring search + kind/realm/org/status filters + offset pagination + total count), extended `CreateGroup`/`UpdateGroup`/`AddGroupMember` (time-bounded membership via upsert), `CountGroupMembers` (direct + expiring), `ListGroupMembers`, `ListGroupAdmins`/`UpsertGroupAdmin`/`DeleteGroupAdmin`/`IsGroupAdmin`, `AddGroupNested`/`RemoveGroupNested` (rejects self-reference + cycles via a recursive CTE), `ListGroupParents`/`ListGroupChildren`.
    - Handlers in [`internal/handlers/rbac.go`](../../services/identity-federation-service/internal/handlers/rbac.go) (extended `AddGroupMember` to accept an `expires_at` body and stamp `added_by` from the caller's claims) and new file [`internal/handlers/group_admin.go`](../../services/identity-federation-service/internal/handlers/group_admin.go) (`SearchGroups`, `InspectGroup`, `ListGroupMembers`, `ListGroupAdmins`/`AddGroupAdmin`/`RemoveGroupAdmin`, `ListGroupParents`/`ListGroupChildren`, `AddNestedGroup`/`RemoveNestedGroup`). `InspectGroup` includes a `project_access_hint` string pointing at the canonical project-access lookup on `tenancy-organizations-service`.
    - Routes registered in [`internal/server/server.go`](../../services/identity-federation-service/internal/server/server.go): `GET /groups/search`, `GET /groups/{id}/inspect`, `GET /groups/{id}/members`, `GET/POST/DELETE /groups/{id}/admins[/{user_id}]`, `GET /groups/{id}/parents`, `GET /groups/{id}/children`, `PUT/DELETE /groups/{id}/nested/{member_id}`.
    - Tests in [`internal/handlers/group_admin_test.go`](../../services/identity-federation-service/internal/handlers/group_admin_test.go) pin the SG.5 wire shapes (`Group`, `GroupAdmin`, `GroupMember`, `GroupInspection`), lock the kind/status/scope vocabulary, and cover the no-DB validation paths (bad kind, bad status, bad organization_id, missing user_id, bad admin scope). `go test ./services/identity-federation-service/...` is green; `go vet` clean.
    - Frontend client [`apps/web/src/lib/api/groups-admin.ts`](../../apps/web/src/lib/api/groups-admin.ts) + admin page [`apps/web/src/routes/control-panel/GroupsPage.tsx`](../../apps/web/src/routes/control-panel/GroupsPage.tsx) at `/control-panel/groups` — search/filter bar, create form (kind / realm / org), per-row archive / restore / delete, inspect side panel with admin/nested/member sub-forms (datetime-local picker for member expiration). Router entry in [`apps/web/src/router.tsx`](../../apps/web/src/router.tsx); Control Panel header now links it. `tsc -b --noEmit` clean, eslint clean.
    - Out-of-scope follow-ups tracked under their own checklist items: cross-service group→project access fetch from `tenancy-organizations-service` (SG.2 / SG.9), SCIM-driven `external` group sync (covered by the existing `internal/scim` package — see [SCIM users/groups](https://www.palantir.com/docs/foundry/scim/)), rule-based membership evaluation engine (`SG.20` granular policies and `SG.43` attribute cache semantics), and project-access-impact recertification flows (`SG.52`).

### Projects, roles, and discretionary access

- [x] `SG.6` Project security boundary (`P0`, `done` 2026-05-17)
  - Treat projects as primary collaborative security boundaries with folders/resources, default roles, explicit grants, references, and point-of-contact metadata.
  - Recommend group-based project roles and provide viewer/editor/owner group setup shortcuts.
  - Ensure file/folder requests inside a project resolve to project-level access requests unless OpenFoundry intentionally diverges.
  - Docs: [Projects and roles](https://www.palantir.com/docs/foundry/security/projects-and-roles), [Compass overview](https://www.palantir.com/docs/foundry/compass/overview).
  - Implementation:
    - Migration [`0009_sg6_project_security_boundary.sql`](../../services/tenancy-organizations-service/internal/repo/migrations/0009_sg6_project_security_boundary.sql) extends `ontology_projects` with `default_role` (CHECK ∈ {`discoverer`, `viewer`, `editor`, `owner`}), `point_of_contact_user_id`, `point_of_contact_email`, and `references` JSONB. Widens the user-membership `role` CHECK to admit `discoverer`. Adds two new tables: `ontology_project_group_memberships` (project-level group grants) and `ontology_project_access_requests` (project- or resource-scoped requests with status, decision, reason, and decided_by/decided_at).
    - Wire types in [`internal/models/project.go`](../../services/tenancy-organizations-service/internal/models/project.go): added `OntologyProjectRoleDiscoverer` (rank floor), extended `OntologyProject` with `DefaultRole` / `PointOfContactUserID` / `PointOfContactEmail` / `References`. Updated `CreateOntologyProjectRequest` / `UpdateOntologyProjectRequest` (the latter uses `**T` for nullable pointers so JSON null clears them). New types `OntologyProjectGroupMembership`, `UpsertProjectGroupMembershipRequest`, `OntologyProjectAccessRequest`, `CreateProjectAccessRequestRequest`, `DecideProjectAccessRequestRequest`, `EnsureProjectAccessGroupsRequest`/`Response`, and stable wire constants for the four access-request statuses.
    - Handlers extended in [`internal/handlers/projects.go`](../../services/tenancy-organizations-service/internal/handlers/projects.go) (`scanProjectRow` helper, `CreateProject` accepts the new fields, `UpdateProject` honours the triple-state semantics for contact / references, both round-trip through `loadProject` to return the full SG.6 shape). New file [`internal/handlers/projects_sg6.go`](../../services/tenancy-organizations-service/internal/handlers/projects_sg6.go) implements `ListProjectGroupMemberships`, `UpsertProjectGroupMembership`, `DeleteProjectGroupMembership`, `EnsureProjectAccessGroups` (the viewer/editor/owner group-setup shortcut — transactional), `CreateProjectAccessRequest`, `ListProjectAccessRequests` (admins/owners see all; requesters see their own; `?status=` filter), `DecideProjectAccessRequest` (approve/deny with reason, decoupled from the grant materialisation), and `CancelProjectAccessRequest` (requester-only).
    - Routes registered in [`internal/server/server.go`](../../services/tenancy-organizations-service/internal/server/server.go): `GET/PUT /projects/{id}/group-memberships`, `DELETE /projects/{id}/group-memberships/{group_id}`, `POST /projects/{id}/access-groups:bootstrap`, `GET/POST /projects/{id}/access-requests`, `POST /projects/{id}/access-requests/{request_id}/decision`, `POST /projects/{id}/access-requests/{request_id}:cancel`.
    - Tests in [`internal/handlers/projects_sg6_test.go`](../../services/tenancy-organizations-service/internal/handlers/projects_sg6_test.go) pin the SG.6 wire shapes (`OntologyProject` extension, `OntologyProjectGroupMembership`, `OntologyProjectAccessRequest`), lock the access-request status vocabulary, and cover no-DB validation paths (empty body, bad decision, bootstrap-with-no-ids). Updated [`project_test.go`](../../services/tenancy-organizations-service/internal/models/project_test.go) for the new lattice ranks. `go test ./services/tenancy-organizations-service/...` is green; `go vet` clean.
    - Frontend client extended in [`apps/web/src/lib/api/tenancy.ts`](../../apps/web/src/lib/api/tenancy.ts) with project + group-membership + access-request types and helpers. New admin page [`apps/web/src/routes/control-panel/ProjectsPage.tsx`](../../apps/web/src/routes/control-panel/ProjectsPage.tsx) at `/control-panel/projects` — project picker, default-role / contact / references editor, group-memberships sub-form, viewer/editor/owner bootstrap sub-form, and an access-request inbox with approve/deny/cancel actions plus a self-request form. Router entry in [`apps/web/src/router.tsx`](../../apps/web/src/router.tsx); Control Panel header now links it. `tsc -b --noEmit` clean, eslint clean.
    - Docs updated in [`docs/security-governance/security-overview.md`](../security-governance/security-overview.md) §"Discretionary roles" to describe the project security boundary and the project-level access-request resolution rule.
    - Out-of-scope follow-ups tracked under their own checklist items: marking-driven access-request approval routing (`SG.13` / `SG.16`), automatic Approvals-style decision queue (`SG.9`), Marketplace-aware project templates (`SG.46`), and the file/folder-creation UI integration that turns "I can't open this folder" into the scoped access-request prefill (covered by the existing workspace handlers in `services/tenancy-organizations-service/internal/workspace/`).

- [x] `SG.7` Role and operation model (`P0`, `done` 2026-05-17)
  - Implement default Owner, Editor, Viewer, and Discoverer-style roles plus custom roles/role sets where locally supported.
  - Map each role to low-level operations checked by services and ensure role delegation cannot exceed the grantor's own role level.
  - Support context-specific role sets for projects, ontology, restricted views, and platform administration.
  - Docs: [Projects and roles](https://www.palantir.com/docs/foundry/security/projects-and-roles), [Manage roles](https://www.palantir.com/docs/foundry/platform-security-management/manage-roles/).
  - Implementation:
    - Migration [`0008_sg7_role_sets_and_operations.sql`](../../services/authorization-policy-service/internal/repo/migrations/0008_sg7_role_sets_and_operations.sql) introduces `role_sets` (context CHECK ∈ {`project`, `ontology`, `restricted_view`, `platform_admin`}) and `role_set_roles (rank INT > 0, UNIQUE(role_set_id, rank))`. Seeds the operation catalog (project / ontology / restricted_view / platform `resource:action` rows), the four default role sets, the per-context Owner/Editor/Viewer/Discoverer roles, and the `role_permissions` bindings that turn each role into its capability surface.
    - Wire types in [`internal/models/rbac.go`](../../services/authorization-policy-service/internal/models/rbac.go): `RoleSet`, `RoleSetRole`, `RoleSetResponse`, `CreateRoleSetRequest`, `UpdateRoleSetRequest`, `AddRoleToRoleSetRequest`, `OperationCatalogEntry`, `ListOperationsResponse`, `CheckDelegationRequest`/`Response`, plus stable wire constants for the four role-set contexts.
    - Repo work in [`internal/repo/role_sets.go`](../../services/authorization-policy-service/internal/repo/role_sets.go): `ListRoleSets` (tenant-scoped, optional `context` filter), `GetRoleSet`/`GetRoleSetBySlug` with eager-loaded members, `CreateRoleSet`/`UpdateRoleSet`/`DeleteRoleSet`, `ListRoleSetRoles`/`AddRoleToRoleSet` (upsert) / `RemoveRoleFromRoleSet`, `ListOperationCatalog`, and the SG.7 delegation primitive: `HighestRankInRoleSet` (resolves via direct `user_roles` and `group_roles → group_members`), `RankOfRoleInSet`, and `CheckDelegation` (returns the structured answer).
    - Handlers in [`internal/handlers/role_sets.go`](../../services/authorization-policy-service/internal/handlers/role_sets.go) wire the HTTP surface and gate every route behind `requirePermission(roles, read|write)` (the existing APS gating). `CheckRoleSetDelegation` defaults the grantor to the authenticated subject when no `grantor_id` is supplied.
    - Delegation rank enforced on the existing `POST /users/{id}/roles` handler: when callers supply `?role_set_id=<uuid>`, the handler calls `Repo.CheckDelegation` and returns 403 with `delegation denied: <reason>` if the grantor's rank is below the target. Admin-role short-circuit preserved to match the existing permission-gating semantics. Updated in [`internal/handlers/rbac.go`](../../services/authorization-policy-service/internal/handlers/rbac.go).
    - Routes registered in [`internal/server/server.go`](../../services/authorization-policy-service/internal/server/server.go): `GET /role-sets`, `POST /role-sets`, `GET/PATCH/DELETE /role-sets/{id}`, `POST /role-sets/{id}/roles`, `DELETE /role-sets/{id}/roles/{role_id}`, `POST /role-sets/{id}/delegation:check`, `GET /operations`.
    - Tests in [`internal/handlers/role_sets_test.go`](../../services/authorization-policy-service/internal/handlers/role_sets_test.go) pin the SG.7 wire shapes (`RoleSetResponse`, `RoleSetRole`, `CheckDelegationResponse`, `OperationCatalogEntry`), lock the role-set context vocabulary, and cover no-DB validation paths (bad context filter, bad create context, empty create body, missing target role on delegation check, non-positive rank). `go test ./services/authorization-policy-service/...` is green; `go vet` clean.
    - Frontend client [`apps/web/src/lib/api/role-sets.ts`](../../apps/web/src/lib/api/role-sets.ts) + admin page [`apps/web/src/routes/control-panel/RoleSetsPage.tsx`](../../apps/web/src/routes/control-panel/RoleSetsPage.tsx) at `/control-panel/role-sets` — context filter, create form, per-role-set role-management cards with rank input, a delegation-check tool that surfaces the structured `{allowed, grantor_rank, target_rank, reason}` answer, and the grouped operation catalog. Router entry in [`apps/web/src/router.tsx`](../../apps/web/src/router.tsx); Control Panel header now links it. `tsc -b --noEmit` clean, eslint clean.
    - Docs updated in [`docs/security-governance/policies-and-authorization.md`](../security-governance/policies-and-authorization.md) §"Role sets, operations, and delegation rank (SG.7)" with the seeded contexts, the admin endpoint table, and the delegation-rank invariant statement.
    - Out-of-scope follow-ups tracked under their own checklist items: hot-loading the role-set catalog into the in-process Cedar policy bundle ([Policy bundles in-process](../security-governance/policy-bundles.md)), per-resource grant tables that pin `(principal, resource, role_set, role)` (`SG.8`), enforcing delegation rank in the per-project group-membership flow (extending the existing `tenancy-organizations-service` SG.6 endpoints), and ontology-resource-level role grants (`SG.23`).

- [x] `SG.8` Role inheritance and direct grants (`P0`, `done` 2026-05-17)
  - Inherit roles from projects/folders to child resources and allow direct resource grants only where product semantics permit.
  - Resolve effective permissions from user grants, nested group grants, default roles, organization membership, markings, and scoped session.
  - Show effective-access explanations for a selected user/resource without leaking protected resource details to unauthorized administrators.
  - Docs: [Projects and roles](https://www.palantir.com/docs/foundry/security/projects-and-roles), [Checking permissions](https://www.palantir.com/docs/foundry/security/checking-permissions/).
  - Implementation:
    - Migration [`0010_sg8_role_inheritance_and_direct_grants.sql`](../../services/tenancy-organizations-service/internal/repo/migrations/0010_sg8_role_inheritance_and_direct_grants.sql) creates `ontology_project_resource_grants` with explicit `scope_kind ∈ (project, folder)`, `principal_kind ∈ (user, group)`, `role ∈ (discoverer, viewer, editor, owner)`, a CHECK that pairs `scope_kind = 'project'` with a NULL `scope_id` and `scope_kind = 'folder'` with a non-NULL one, plus the principal/scope/project unique index. Ontology-resource-level direct grants are intentionally excluded — those kinds inherit from their project per the existing domain rules.
    - Wire types in [`internal/models/project.go`](../../services/tenancy-organizations-service/internal/models/project.go): `ProjectResourceGrant`, `CreateProjectResourceGrantRequest`, `ListProjectResourceGrantsResponse`, `EffectiveAccessSource`, `EffectiveAccessResponse`, and stable wire constants for the four grant scope/principal vocabularies plus the ten `EffectiveAccessSource*` source-kind strings.
    - Handlers and resolver in [`internal/handlers/projects_sg8.go`](../../services/tenancy-organizations-service/internal/handlers/projects_sg8.go): `ListProjectResourceGrants` (view-access required + optional `scope_kind/scope_id/principal_kind/principal_id` filters), `CreateProjectResourceGrant` (project owner / admin only; validates the `scope_kind ↔ scope_id` rule and that the folder actually belongs to the project), `DeleteProjectResourceGrant`, and `CheckEffectiveAccess` which walks: (1) project owner, (2) project default role, (3) user-direct membership, (4) group-via-project memberships, (5) project-scope direct grants, (6) folder-scope direct grants + every ancestor via a recursive CTE on `parent_folder_id`. Sources are insertion-sorted by rank so `sources[0]` is the winning role; `resolved_role` is set explicitly. Anti-leak gate: the inspected user, the project owner, or a platform admin only — non-admin callers get a 403 without disclosing project existence.
    - Routes registered in [`internal/server/server.go`](../../services/tenancy-organizations-service/internal/server/server.go): `GET/POST /projects/{id}/resource-grants`, `DELETE /projects/{id}/resource-grants/{grant_id}`, `GET /projects/{id}/effective-access?user_id=…&scope_kind=…&scope_id=…&group_ids=…`. `group_ids` is supplied by the gateway from the user's JWT groups attribute because cross-service group-membership lookup lives in identity-federation-service.
    - Tests in [`internal/handlers/projects_sg8_test.go`](../../services/tenancy-organizations-service/internal/handlers/projects_sg8_test.go) pin the SG.8 wire shapes (`ProjectResourceGrant`, `EffectiveAccessResponse`, ten source-kind strings, four scope/principal constants) and cover no-DB validation paths (bad scope_kind, `scope_kind=project` with `scope_id`, missing `user_id`, folder scope without `scope_id`, malformed `group_ids`). `go test ./services/tenancy-organizations-service/...` is green; `go vet` clean.
    - Frontend client [`apps/web/src/lib/api/tenancy.ts`](../../apps/web/src/lib/api/tenancy.ts) extended with `ProjectResourceGrant`, `EffectiveAccessSource`, `EffectiveAccessResponse`, ten `EffectiveAccessSourceKind` wire literals, and the CRUD / resolver helpers. Admin UI [`apps/web/src/routes/control-panel/ProjectsPage.tsx`](../../apps/web/src/routes/control-panel/ProjectsPage.tsx) now hosts a "Resource grants (SG.8)" sub-section and an "Effective access (SG.8)" probe that surfaces the ordered `sources[]` breakdown beneath the project detail editor. `tsc -b --noEmit` clean, eslint clean.
    - Docs updated in [`docs/security-governance/security-overview.md`](../security-governance/security-overview.md) §"Discretionary roles" to describe direct grants, inheritance, and the effective-access resolver's anti-leak gate.
    - Out-of-scope follow-ups tracked under their own checklist items: marking-aware composition into the effective-access resolver (`SG.13`–`SG.15`), scoped-session intersection on the resolver output (`SG.24`–`SG.25`), cross-service group-membership auto-lookup so callers don't have to forward `group_ids` (`SG.43`), restricted-view per-row visibility composition (`SG.21`–`SG.22`), and visualising the access graph as a UI graph (`SG.10`).

- [x] `SG.9` Access request workflow (`P0`, `done` 2026-05-17)
  - Let users request project access, additional project access, group membership for a project role, and required marking access with a reason.
  - Route internal group membership requests to group administrators and direct project role requests to project owners.
  - Support external group request handoff messages/URLs and per-project exclusion of sensitive groups from request forms.
  - Docs: [Projects and roles](https://www.palantir.com/docs/foundry/security/projects-and-roles), [Manage groups](https://www.palantir.com/docs/foundry/platform-security-management/manage-groups), [Approvals overview](https://www.palantir.com/docs/foundry/approvals/overview/).
  - Implementation:
    - Migration [`0011_sg9_access_request_workflow.sql`](../../services/tenancy-organizations-service/internal/repo/migrations/0011_sg9_access_request_workflow.sql) extends `ontology_project_access_requests` with `request_type`, `requested_for_user_ids`, `completed_at`, and the expanded status vocabulary. It adds three tables:
      - `ontology_project_access_request_tasks` for independently routed `group_membership`, `project_role`, `marking_access`, and `external_group_handoff` subtasks.
      - `ontology_project_access_group_settings` for project-local access-form overlays: group kind, group-admin reviewer IDs, custom form JSON, external handoff message/URL, and `excluded_from_request_forms`.
      - `ontology_project_required_markings` for required marking-access prompts and reviewer IDs until the full marking service lands under `SG.11`–`SG.16`.
    - Wire types in [`internal/models/project.go`](../../services/tenancy-organizations-service/internal/models/project.go): expanded `OntologyProjectAccessRequest`, `ProjectAccessRequestTask`, `ProjectAccessRequestGroupSetting`, `ProjectRequiredMarking`, `ProjectAccessRequestFormResponse`, plus stable constants for request type, task type/status, request status, and project access group kind.
    - Handler work in [`internal/handlers/projects_sg9.go`](../../services/tenancy-organizations-service/internal/handlers/projects_sg9.go) plus the upgraded SG.6 handlers:
      - `GET /projects/{id}/access-request-form` returns visible requestable groups, required markings, and direct-role reviewers without requiring pre-existing project access.
      - `PUT/DELETE /projects/{id}/access-request-groups/{group_id}` configures internal/external/rule-based request behavior, reviewer IDs, handoff copy/URL, and per-project hidden sensitive groups.
      - `PUT/DELETE /projects/{id}/access-request-markings/{marking_id}` configures required marking access prompts and marking reviewers.
      - `POST /projects/{id}/access-requests` accepts legacy direct-role requests and SG.9 multi-task requests (`project_role_requests`, `group_membership_requests`, `marking_access_requests`) with a required reason.
      - `GET /access-requests/inbox` exposes a reviewer inbox: project owners for direct project-role tasks, configured group admins for group tasks, configured marking reviewers for marking tasks, and platform admins for all tasks.
      - `POST /projects/{id}/access-requests/{request_id}/decision` applies a reviewer decision only to eligible subtasks; direct project-role approvals materialize `ontology_project_memberships`.
    - External groups are represented as `external_group_handoff` tasks in `action_required` state with the configured handoff message/URL, matching the public-doc behavior that externally managed groups redirect users outside Foundry rather than being approved locally.
    - Tests: [`internal/handlers/projects_sg9_test.go`](../../services/tenancy-organizations-service/internal/handlers/projects_sg9_test.go) covers task-status summarization, request-type validation, and reviewer UUID JSON decoding; [`internal/models/project_test.go`](../../services/tenancy-organizations-service/internal/models/project_test.go) pins the SG.9 task and form wire shapes. `go test ./services/tenancy-organizations-service/...` is green; `pnpm --dir apps/web exec tsc -b --noEmit` is green.
    - Frontend client [`apps/web/src/lib/api/tenancy.ts`](../../apps/web/src/lib/api/tenancy.ts) includes the SG.9 request/task/form/settings types and API helpers. Admin UI [`apps/web/src/routes/control-panel/ProjectsPage.tsx`](../../apps/web/src/routes/control-panel/ProjectsPage.tsx) adds access-request form configuration, external group handoff fields, sensitive-group exclusion, required marking reviewer setup, multi-task manual request creation, and task rendering in the access-request inbox.
    - Docs updated in [`docs/security-governance/security-overview.md`](../security-governance/security-overview.md) and [`docs/security-governance/identity-and-access.md`](../security-governance/identity-and-access.md).
    - Out-of-scope follow-ups tracked under their own checklist items: full marking category/marking membership persistence (`SG.11`–`SG.16`), cross-service identity lookup to auto-populate group-admin reviewer IDs from `identity-federation-service` (`SG.43`), notification delivery for reviewers (`SG.48`), and a dedicated visual access graph / permission-checking workbench (`SG.10`).

- [ ] `SG.10` Permission checking and access graph (`P0`, `todo`)
  - Provide a permission-checking tool that explains whether a user satisfies organization, marking, scoped session, role, and restricted-view requirements.
  - Visualize access graph relationships among users, groups, projects, resources, markings, and roles.
  - Connect lineage-based marking propagation explanations to dataset/resource lineage views.
  - Docs: [Checking permissions](https://www.palantir.com/docs/foundry/security/checking-permissions/), [Markings](https://www.palantir.com/docs/foundry/security/markings/).

### Markings and mandatory access controls

- [x] `SG.11` Marking categories (`P0`, `done` 2026-05-17)
  - Create marking categories with metadata, category visibility, administrators, and category permissions.
  - Treat category deletion as unsupported or blocked if mirroring documented immutable-category behavior.
  - Audit category creation and permission changes.
  - Docs: [Manage markings](https://www.palantir.com/docs/foundry/platform-security-management/manage-markings/).
  - Implementation:
    - Migration [`0009_sg11_marking_categories.sql`](../../services/authorization-policy-service/internal/repo/migrations/0009_sg11_marking_categories.sql) adds `marking_categories`, `marking_category_permissions`, and `marking_category_audit_events`, plus `markings:read|write|audit` permission seeds.
    - API routes in [`authorization-policy-service/internal/server/server.go`](../../services/authorization-policy-service/internal/server/server.go): `GET/POST/PATCH/DELETE /marking-categories`, `PUT/DELETE /marking-categories/{id}/permissions`, and `GET /marking-categories/{id}/audit-events`.
    - Deletion parity: `DELETE /marking-categories/{id}` never removes rows; it writes `category.delete_blocked` audit evidence and returns `405` with "hide the category instead".

- [x] `SG.12` Marking CRUD and lifecycle (`P0`, `done` 2026-05-17)
  - Create markings inside categories with stable IDs, display names, descriptions, administrators, removers, appliers, members, and audit metadata.
  - Prevent marking deletion or category moves if mirroring documented marking immutability.
  - Support discoverability and metadata visibility according to marking/category permissions.
  - Docs: [Markings](https://www.palantir.com/docs/foundry/security/markings/), [Manage markings](https://www.palantir.com/docs/foundry/platform-security-management/manage-markings/).
  - Implementation:
    - Migration [`0010_sg12_markings.sql`](../../services/authorization-policy-service/internal/repo/migrations/0010_sg12_markings.sql) adds `markings`, `marking_permissions`, and `marking_audit_events`.
    - API routes in [`authorization-policy-service/internal/server/server.go`](../../services/authorization-policy-service/internal/server/server.go): `GET/POST /marking-categories/{id}/markings`, `GET/PATCH/DELETE /markings/{id}`, `PUT /markings/{id}/category`, `PUT/DELETE /markings/{id}/permissions`, and `GET /markings/{id}/audit-events`.
    - Stable IDs: creation accepts an optional caller-supplied UUID and otherwise generates one.
    - Lifecycle parity: marking deletion and category moves never mutate state; they write `marking.delete_blocked` / `marking.category_move_blocked` audit evidence and return `405`.
    - Discoverability: hidden categories are visible only to marking writers/auditors, category viewers/admins, or principals with a permission on any marking in the category. Marking metadata is redacted for ordinary readers unless they hold category/marking administrator rights.
    - Frontend client [`apps/web/src/lib/api/marking-categories.ts`](../../apps/web/src/lib/api/marking-categories.ts) and UI [`apps/web/src/routes/control-panel/MarkingCategoriesPage.tsx`](../../apps/web/src/routes/control-panel/MarkingCategoriesPage.tsx) cover category and marking CRUD, permission grant/revoke, audit inspection, delete-block checks, and category-move block checks.
    - Out-of-scope follow-ups tracked under `SG.13`–`SG.15`: applying/removing markings to resources, resource access enforcement, inherited markings, and lineage propagation.

- [x] `SG.13` Marking permission model (`P0`, `done` 2026-05-17)
  - Implement distinct manage permissions, apply marking, remove marking, and member/access grants.
  - Ensure apply/manage permissions do not imply membership and therefore do not imply access to marked data.
  - Require remove marking and apply marking or equivalent expand-access permission before a marking can be removed from protected resources.
  - Docs: [Manage markings](https://www.palantir.com/docs/foundry/platform-security-management/manage-markings/), [Markings](https://www.palantir.com/docs/foundry/security/markings/).
  - Implementation:
    - Migration [`0011_sg13_marking_permission_model.sql`](../../services/authorization-policy-service/internal/repo/migrations/0011_sg13_marking_permission_model.sql) adds direct `resource_markings` plus `resource_marking_audit_events` for apply/remove attempts and denials.
    - [`Repo.CheckMarkingPermission`](../../services/authorization-policy-service/internal/repo/marking_permissions.go) resolves user and group grants into distinct `can_manage`, `can_apply`, `can_remove`, and `is_member` flags; only `is_member` sets `can_access_marked_data`.
    - API routes in [`authorization-policy-service/internal/server/server.go`](../../services/authorization-policy-service/internal/server/server.go): `POST /markings/{id}/permission-check`, `GET/POST /resource-markings`, and `POST /resource-markings/remove`.
    - Apply rule: direct resource marking application requires `Apply marking` plus caller-supplied resource "Update Markings" authorization evidence.
    - Remove rule: direct resource marking removal requires `Remove marking`, resource "Update Markings" authorization evidence, and either `Apply marking` or an equivalent expand-access flag; denied attempts are audited.
    - Frontend client [`apps/web/src/lib/api/marking-categories.ts`](../../apps/web/src/lib/api/marking-categories.ts) and UI [`apps/web/src/routes/control-panel/MarkingCategoriesPage.tsx`](../../apps/web/src/routes/control-panel/MarkingCategoriesPage.tsx) expose the permission check plus direct apply/remove workbench.
    - Out-of-scope follow-ups tracked under `SG.14`–`SG.15`: inherited markings, lineage propagation, cross-service resource role proofing, and build/output declassification guards.

- [x] `SG.14` Marking enforcement and inheritance (`P0`, `done` 2026-05-17)
  - Require users to satisfy all applied markings plus organization and role requirements before reading or acting on a resource.
  - Inherit markings through project/folder hierarchy and data dependencies/lineage to derived resources.
  - Show marking source paths, direct versus inherited markings, and lineage-derived marking provenance.
  - Docs: [Markings](https://www.palantir.com/docs/foundry/security/markings/), [Checking permissions](https://www.palantir.com/docs/foundry/security/checking-permissions/).
  - Implementation:
    - Migration [`0012_sg14_marking_enforcement_inheritance.sql`](../../services/authorization-policy-service/internal/repo/migrations/0012_sg14_marking_enforcement_inheritance.sql) adds `resource_marking_edges` for `hierarchy` and `lineage` inheritance facts.
    - [`Repo.EffectiveResourceMarkings`](../../services/authorization-policy-service/internal/repo/marking_enforcement.go) walks upstream inheritance paths from a target resource, folds in direct SG.13 markings, and returns source paths with `direct`, `hierarchy`, `lineage`, or `mixed` provenance.
    - [`Repo.CheckResourceAccess`](../../services/authorization-policy-service/internal/repo/marking_enforcement.go) requires organization evidence, caller-supplied role evidence, and membership in every effective marking. Hierarchy/direct markings gate `resource_access`; lineage-derived markings gate `data_access`.
    - API routes in [`authorization-policy-service/internal/server/server.go`](../../services/authorization-policy-service/internal/server/server.go): `GET /resource-markings/effective`, `GET/PUT/DELETE /resource-marking-edges`, and `POST /resource-access:check`.
    - Frontend client [`apps/web/src/lib/api/marking-categories.ts`](../../apps/web/src/lib/api/marking-categories.ts) and UI [`apps/web/src/routes/control-panel/MarkingCategoriesPage.tsx`](../../apps/web/src/routes/control-panel/MarkingCategoriesPage.tsx) expose inheritance edge management, effective marking provenance, and resource/data access checks.
    - Out-of-scope follow-up tracked under `SG.15`: build/runtime services should emit lineage edges automatically and block output publishing/declassification at publish time.

- [x] `SG.15` Marking-aware builds and outputs (`P0`, `done` 2026-05-17)
  - Propagate markings from datasets, media sets, code resources, object types, functions, and model artifacts into derived resources.
  - Block output publishing when marking removal is attempted without required permissions.
  - Provide transaction/security diff views showing marking changes between versions, branches, or builds where local versioning exists.
  - Docs: [Markings](https://www.palantir.com/docs/foundry/security/markings/), [Manage markings](https://www.palantir.com/docs/foundry/platform-security-management/manage-markings/).
  - Implementation:
    - Migration [`0013_sg15_marking_aware_build_outputs.sql`](../../services/authorization-policy-service/internal/repo/migrations/0013_sg15_marking_aware_build_outputs.sql) adds `resource_marking_build_events`, a build/transaction audit surface for output marking diffs.
    - [`Repo.PublishMarkingBuild`](../../services/authorization-policy-service/internal/repo/marking_builds.go) accepts arbitrary input/output resource refs (`dataset`, `media_set`, `code_resource`, `object_type`, `function`, `model_artifact`, etc.), writes `lineage` inheritance edges to outputs, and computes before/after effective marking diffs.
    - Publishing is blocked when replacing output lineage would remove an effective marking and the actor lacks the SG.13 removal rule: resource update-markings evidence plus `Remove marking` and either `Apply marking` or expand-access equivalent.
    - API routes in [`authorization-policy-service/internal/server/server.go`](../../services/authorization-policy-service/internal/server/server.go): `POST /resource-marking-builds:publish` and `GET /resource-marking-build-events`.
    - Frontend client [`apps/web/src/lib/api/marking-categories.ts`](../../apps/web/src/lib/api/marking-categories.ts) and UI [`apps/web/src/routes/control-panel/MarkingCategoriesPage.tsx`](../../apps/web/src/routes/control-panel/MarkingCategoriesPage.tsx) expose dry-run/apply build publishing plus build/transaction diff history.
    - Out-of-scope follow-up: individual build/runtime services should call this endpoint from their publish/commit paths; SG.15 now provides the shared authorization primitive and diff store.

### Audit and basic governance

- [x] `SG.16` Audit log event model (`P0`, `done` 2026-05-17)
  - Capture actor, session/service account, action name, categories, timestamp, resource entities, origins, trace ID, event ID, outcome, and error metadata for security-relevant operations.
  - Treat audit logs as sensitive resources requiring separate access controls and retention policy.
  - Normalize service-initiated follow-up events and user-initiated request correlation.
  - Docs: [Audit logs overview](https://www.palantir.com/docs/foundry/security/audit-logs-overview), [Monitor audit logs](https://www.palantir.com/docs/foundry/security/monitor-audit-logs).
  - Implementation:
    - Migration [`0007_sg16_audit_log_event_model.sql`](../../services/audit-compliance-service/internal/repo/migrations/0007_sg16_audit_log_event_model.sql) extends `audit_events` with `event_id`, `log_entry_id`, `sequence_id`, actor/session/service-account context, product/producer metadata, `categories`, `entities`, `origins`, `trace_id`, `outcome`, error/request/result fields, parent-event correlation, access tier, and indexes for trace/category/entity investigation.
    - [`AppendAuditEventRequest`](../../services/audit-compliance-service/internal/models/audit_event.go) and [`AuditEvent`](../../services/audit-compliance-service/internal/models/models.go) expose the normalized event model while keeping the legacy hash-chain fields (`id`, `sequence`, `previous_hash`, `entry_hash`) intact.
    - [`Repo.PersistAuditEvent`](../../services/audit-compliance-service/internal/repo/events.go) defaults missing audit.3-style fields from headers/body, derives user versus service initiation, preserves event/log-entry IDs when supplied, and stores JSON object/array fields with validation.
    - Audit reads are now gated by a separate [`security.CanViewAuditLogs`](../../services/audit-compliance-service/internal/domain/security/security.go) check requiring `audit-logs:view`, `audit:read`, `audit:view`, `admin`, `auditor`, or `security-auditor` before classification/org/subject filters are applied.
    - The migration seeds a system retention policy `AUDIT_LOG_SECURITY_RETENTION` for `audit_log` resources with legal-hold support, and the Audit UI/API now displays and filters categories and trace IDs.

- [x] `SG.17` Audit delivery basics (`P0`, `done` 2026-05-17)
  - Deliver audit logs to an external SIEM API/export mechanism and optionally to governed OpenFoundry datasets for in-platform analysis.
  - Support date-range listing, content retrieval, schema versioning, duplicate detection, and per-organization export destinations.
  - Provide setup UI with validation, backfill status, and access controls.
  - Docs: [Monitor audit logs](https://www.palantir.com/docs/foundry/security/monitor-audit-logs), [Audit log categories](https://www.palantir.com/docs/foundry/security/audit-log-categories).
  - Implementation:
    - Migration [`0008_sg17_audit_delivery_basics.sql`](../../services/audit-compliance-service/internal/repo/migrations/0008_sg17_audit_delivery_basics.sql) adds `audit_delivery_destinations` and `audit_delivery_files` for per-organization delivery setup, validation/backfill status, schema versioning, content checksums, and duplicate counts.
    - [`repo/audit_delivery.go`](../../services/audit-compliance-service/internal/repo/audit_delivery.go) validates `siem_api` and `openfoundry_dataset` destinations, materializes date-range `audit.3` NDJSON snapshots, computes duplicate `log_entry_id` counts, and serves content retrieval metadata.
    - API routes in [`server.go`](../../services/audit-compliance-service/internal/server/server.go) expose destination list/create/validate/backfill plus delivery-file list/content endpoints under `/api/v1/audit/delivery/*`.
    - [`security.CanManageAuditDelivery`](../../services/audit-compliance-service/internal/domain/security/security.go) separates delivery setup/backfill permissions from ordinary audit-log read access.
    - Frontend client [`apps/web/src/lib/api/audit.ts`](../../apps/web/src/lib/api/audit.ts) and [`AuditDeliveryPanel`](../../apps/web/src/lib/components/audit/AuditDeliveryPanel.tsx) add the setup UI with validation status, backfill status, date-range listing, and NDJSON preview in the Audit workspace.
    - Documentation updated in [Audit and traceability](../security-governance/audit-and-traceability.md), [Audit model](../security-governance/audit-model/), and [Security overview](../security-governance/security-overview.md).
    - Out-of-scope follow-up: a scheduled delivery worker and real external push/dataset-write adapters should consume this registry; SG.17 provides the secure registry, backfill, polling-style retrieval API, and UI.

- [ ] `SG.18` Admin Control Panel shell (`P0`, `todo`)
  - Provide a centralized administration application for identity, users, groups, organizations, roles, markings, application access, egress, retention, email, audit, and third-party apps.
  - Gate each admin module by platform/organization/enrollment operations.
  - Show change request status and audit history for sensitive admin changes.
  - Docs: [Control Panel](https://www.palantir.com/docs/foundry/administration/control-panel/), [Administration overview](https://www.palantir.com/docs/foundry/administration/overview/).

## Milestone B: credible Foundry-style security and governance parity

### Restricted views and granular permissions

- [ ] `SG.19` Restricted view resource CRUD (`P1`, `todo`)
  - Create restricted views backed by datasets with project/folder placement, owners, policy, assumed markings, transactions, and output/view metadata.
  - Enforce create restricted view resource, create restricted view for dataset, edit policy, read policy, view transaction, build, and read restricted view permissions.
  - Prevent use of restricted views as transform inputs when mirroring documented reproducibility limitations.
  - Docs: [Restricted views](https://www.palantir.com/docs/foundry/security/restricted-views), [Manage restricted views](https://www.palantir.com/docs/foundry/platform-security-management/manage-restricted-views/).

- [x] `SG.20` Granular policy editor (`P1`, `done` 2026-05-17)
  - Build policies from user attributes, group membership, organization IDs, column values, constants, arrays, and logical operators.
  - Support comparisons such as equality, inequality/range, greater/less than, and intersects according to local type capabilities.
  - Require stable IDs rather than mutable names for users, groups, and organizations where policies depend on identity entities.
  - Docs: [Restricted views](https://www.palantir.com/docs/foundry/security/restricted-views), [Manage restricted views](https://www.palantir.com/docs/foundry/platform-security-management/manage-restricted-views/).
  - Implementation:
    - [`granularPolicy.ts`](../../apps/web/src/lib/restricted-views/granularPolicy.ts) defines the canonical `granular_policy` JSON shape with `and`/`or` groups, comparison rules, operands for user attributes, user groups, user organization IDs, user IDs, backing-dataset columns, constants, and arrays.
    - [`GranularPolicyEditor`](../../apps/web/src/lib/components/restricted-views/GranularPolicyEditor.tsx) provides a structured Control Panel editor for rule operands and operators (`=`, `!=`, ranges, `in`, `contains`, `intersects`) while keeping the generated policy JSON visible/editable for advanced cases.
    - [`RestrictedViewsPage`](../../apps/web/src/routes/control-panel/RestrictedViewsPage.tsx) now embeds the editor, blocks save while policy validation fails, and serializes the canonical granular policy into the restricted-view `policy` field.
    - Backend validation in [`handlers/restricted_views.go`](../../services/identity-federation-service/internal/handlers/restricted_views.go) accepts canonical policies, validates nested logical groups/comparisons, and rejects mutable user/group/organization names by requiring UUID or UUID-array constants when identity operands are compared.
    - Tests cover canonical policy round-tripping, stable-ID enforcement, and backend rejection of named group values: [`granularPolicy.test.ts`](../../apps/web/src/lib/restricted-views/granularPolicy.test.ts) and [`restricted_views_test.go`](../../services/identity-federation-service/internal/handlers/restricted_views_test.go). Runtime query rewrite/enforcement is completed under `SG.21`.

- [x] `SG.21` Restricted view query enforcement (`P1`, `done` 2026-05-17)
  - Rewrite preview, SQL, analytics, object, and API reads through the policy engine so users only see rows they are allowed to see.
  - Combine dynamic policy definition, user attributes, group membership, marking membership, and scoped session state at request time.
  - Explain that transaction history cannot fully reconstruct historical user attributes/group membership unless OpenFoundry implements an explicit snapshot extension.
  - Docs: [Restricted views](https://www.palantir.com/docs/foundry/security/restricted-views), [Manage restricted views](https://www.palantir.com/docs/foundry/platform-security-management/manage-restricted-views/).
  - Implementation:
    - [`libs/restrictedview`](../../libs/restrictedview/policy.go) is the shared runtime policy engine for canonical `granular_policy` JSON. It evaluates user attributes, user group IDs, user/org IDs, row columns, constants, arrays, markings, allowed organizations, restricted-view scoped sessions, guest sessions, and consumer-mode state at read time.
    - Dataset previews in [`views_schema_preview.go`](../../services/dataset-versioning-service/internal/handlers/views_schema_preview.go) apply restricted-view headers/query policy context before returning rows and redact configured hidden columns. Preview responses include the explicit identity-history caveat.
    - Flight SQL / analytics reads in [`flightsql/server.go`](../../services/sql-bi-gateway-service/internal/flightsql/server.go) rewrite eligible `SELECT` statements with the shared policy engine when the caller's JWT attributes carry restricted-view policy context.
    - Ontology/object API reads in [`objects_bridge.go`](../../services/object-database-service/internal/handlers/objects_bridge.go) filter list/query/get object results through the shared evaluator and return the restricted-view decision metadata for query/list calls.
    - The ABAC decision response in [`abac.go`](../../services/authorization-policy-service/internal/domain/abac.go) now composes restricted-view granular policy outcomes and returns the transaction-history identity snapshot caveat.
    - Tests: [`policy_test.go`](../../libs/restrictedview/policy_test.go) covers user-attribute/group/org/marking/scope evaluation, preview row filtering/redaction, SQL rewrite, and the historical identity snapshot caveat.

- [x] `SG.22` Marking-backed restricted views (`P1`, `done` 2026-05-17)
  - Support restricted views based on one or more string-array marking columns with a marking typeclass/hint.
  - Require users to satisfy row-level marking IDs in addition to role, organization, resource markings, and scoped session.
  - Validate input dataset schema and reject invalid marking IDs or unsupported column types.
  - Docs: [Restricted views](https://www.palantir.com/docs/foundry/security/restricted-views), [Markings](https://www.palantir.com/docs/foundry/security/markings/).
  - Implementation:
    - [`libs/restrictedview`](../../libs/restrictedview/policy.go) now treats explicit `marking_columns` as row-level MAC columns. Every ID in those `ARRAY<STRING>` cells must be satisfied by the caller's active marking membership or allowed organization IDs, after ordinary role, organization, resource marking, guest, consumer-mode, and restricted-view scoped-session checks.
    - The shared policy engine detects the `marking_type.mandatory` typeclass from dataset schema metadata, validates configured/annotated marking columns, rejects unsupported non-`ARRAY<STRING>` shapes, and rejects non-UUID row values for explicit marking-backed columns while preserving legacy named marking allowlists for non-SG.22 policies.
    - [`restricted_views.go`](../../services/identity-federation-service/internal/handlers/restricted_views.go) persists and validates `marking_columns`, accepts optional `backing_dataset_schema` validation input, and the [`0015_sg22_marking_backed_restricted_views.sql`](../../services/identity-federation-service/internal/repo/migrations/0015_sg22_marking_backed_restricted_views.sql) migration stores the column metadata.
    - [`models.go`](../../services/dataset-versioning-service/internal/models/models.go) validates dataset schemas that opt into `marking_type.mandatory`, and preview/object/ABAC/SQL readers consume `marking_columns` through the shared evaluator or SQL predicate rewrite.
    - The Control Panel restricted-view form exposes a `Marking columns` field in [`RestrictedViewsPage.tsx`](../../apps/web/src/routes/control-panel/RestrictedViewsPage.tsx), and tests cover schema hints, row-level marking/org enforcement, invalid IDs, SQL rewrite predicates, and handler validation.

- [x] `SG.23` Restricted-view-backed object types (`P1`, `done` 2026-05-17)
  - Allow Ontology object types to use restricted views as backing data sources and inherit row/object-level permissions.
  - Enforce object manager permissions, dataset view permissions, restricted view edit/read permissions, and object data source permissions.
  - Propagate restricted view changes into indexed object types and downstream applications.
  - Docs: [Object permissioning: configuring restricted-view-backed object types](https://www.palantir.com/docs/foundry/object-permissioning/configuring-rv-access-controls/), [Object permissioning: managing object security](https://www.palantir.com/docs/foundry/object-permissioning/managing-object-security/).
  - Implementation:
    - [`models.ObjectType`](../../services/ontology-definition-service/internal/models/models.go) and [`0003_sg23_restricted_view_backed_object_types.sql`](../../services/ontology-definition-service/internal/repo/migrations/0003_sg23_restricted_view_backed_object_types.sql) persist `backing_datasource_type`, `backing_restricted_view_id`, restricted-view policy JSON, policy / registered / indexed versions, storage mode, timestamps, and a propagation status that flags Object Storage V1-style re-registration or re-index requirements.
    - [`handlers.go`](../../services/ontology-definition-service/internal/handlers/handlers.go) validates restricted-view datasource writes and requires `ontology:manage`, `object_type_datasource:manage`, dataset read, restricted-view read/policy-read, and policy-edit permissions when applicable; [`0015_sg23_object_type_datasource_operations.sql`](../../services/authorization-policy-service/internal/repo/migrations/0015_sg23_object_type_datasource_operations.sql) seeds the object datasource operation catalog.
    - [`object_type_policies.go`](../../services/object-database-service/internal/handlers/object_type_policies.go) resolves object type backing metadata from `ontology-definition-service`; object list/get/query routes now fail closed unless the caller can read the restricted view and object datasource metadata, then filter rows through [`libs/restrictedview`](../../libs/restrictedview/policy.go).
    - The Ontology Manager datasource tab in [`ObjectTypeDetailPage.tsx`](../../apps/web/src/routes/ontology/ObjectTypeDetailPage.tsx) now persists restricted-view backing configuration through `updateObjectType` instead of local storage only, while still showing policy propagation warnings for registered/indexed object applications.

### Scoped sessions, templates, and privacy controls

- [x] `SG.24` Scoped session configuration (`P1`, `done`)
  - Create scoped session presets based on markings and configure enablement, no-scoped-session bypass, always-show-selector behavior, and allowed bypass groups.
  - Limit selectable sessions to users who are members of all required markings.
  - Show active scoped session banner and allow permitted users to change sessions with a workspace refresh.
  - Docs: [Configure scoped sessions](https://www.palantir.com/docs/foundry/administration/configure-scoped-sessions/), [Markings](https://www.palantir.com/docs/foundry/security/markings/).
  - Implementation notes:
    - [`ControlPanelSettings.scoped_sessions`](../../services/identity-federation-service/internal/handlers/control_panel.go) now stores enablement, no-scoped-session bypass policy, always-show-selector behavior, allowed bypass groups, and normalized marking-backed presets.
    - [`scoped_sessions.go`](../../services/identity-federation-service/internal/handlers/scoped_sessions.go) exposes authenticated options/select endpoints. It computes full marking membership from stored user attributes, filters presets by required markings, limits bypass to configured groups/admin roles, and issues scoped JWTs whose refresh tokens retain `session_scope`.
    - [`ScopedSessionsPage.tsx`](../../apps/web/src/routes/control-panel/ScopedSessionsPage.tsx) adds the Control Panel editor, while [`ScopedSessionBanner.tsx`](../../apps/web/src/lib/components/ScopedSessionBanner.tsx) shows the active session and switches sessions with a workspace reload.

- [x] `SG.25` Scoped session enforcement (`P1`, `done` 2026-05-17)
  - Apply scoped session marking subset across filesystem, Ontology, analytics, API, and application requests.
  - Ensure users with no scoped session see their full marking set only if organization policy allows bypass.
  - Prevent cross-pollination by blocking access to resources outside the active scope even when the user normally has the required marking membership.
  - Docs: [Configure scoped sessions](https://www.palantir.com/docs/foundry/administration/configure-scoped-sessions/), [Markings](https://www.palantir.com/docs/foundry/security/markings/).
  - Implementation notes:
    - [`Claims.AllowedMarkings`](../../libs/auth-middleware/claims.go) is now the canonical effective marking set for downstream services: an active `session_scope.allowed_markings` subset wins over stored membership, classification clearance, and admin role widening; no active scope falls back to the normal full-clearance cascade.
    - Cedar principal hydration, object/media Cedar gates, Ontology object access, Ontology Query masks, restricted-view/ABAC marking checks, Iceberg/catalog marking guards, and the edge gateway forwarded `x-openfoundry-allowed-markings` header now consume the effective active set instead of reading raw session scope ad hoc.
    - [`CheckResourceAccess`](../../services/authorization-policy-service/internal/repo/marking_enforcement.go) reports scoped-session requirements separately from marking membership, including per-marking `membership_satisfied`, `scoped_session_satisfied`, and source-aware resource-vs-lineage data requirements.
    - [`scoped_sessions.go`](../../services/identity-federation-service/internal/handlers/scoped_sessions.go) issues an explicit full-marking session scope for the no-scoped-session option only after the organization bypass policy permits it, so refresh and downstream data planes do not infer bypass from an absent scope.

- [x] `SG.26` Project templates (`P1`, `done` 2026-05-17)
  - Create project templates with default role grants, generated owner/editor/viewer groups, default role, markings, folder structure, point of contact, and constraints.
  - Validate template users have permissions to create groups, apply markings, and set project defaults before execution.
  - Support governance frameworks by making secure project setup repeatable and auditable.
  - Docs: [Manage Project templates](https://www.palantir.com/docs/foundry/platform-security-management/manage-project-templates), [Projects and roles](https://www.palantir.com/docs/foundry/security/projects-and-roles).
  - Implementation notes:
    - [`0016_sg26_project_templates.sql`](../../services/tenancy-organizations-service/internal/repo/migrations/0016_sg26_project_templates.sql) adds durable project templates plus immutable template-application audit rows for variables, generated groups, markings, constraints, validation checks, actor, and created project.
    - [`projects_sg26.go`](../../services/tenancy-organizations-service/internal/handlers/projects_sg26.go) exposes template list/create/get APIs, applies templates during `POST /projects`, creates template folder skeletons, binds generated viewer/editor/owner groups to project roles, records request-form metadata for those groups, applies project marking requirements into `marking_rids`/required markings, and blocks deployment when the caller lacks group, marking, constraint, or project-default permissions.
    - [`CreateProjectModal.tsx`](../../apps/web/src/lib/components/projects/CreateProjectModal.tsx) now sends `template_key` and default role from the template picker, and surfaces template security contents before project creation.

- [x] `SG.27` Application access controls (`P1`, `done` 2026-05-17)
  - Configure organization-level platform/application visibility by user or group allow/block rules and lifecycle stage.
  - Generate change requests and keep a historical record for application access changes and approval policy changes.
  - Clearly label application access as UX scope control, not a substitute for server-side permissions.
  - Docs: [Configure application access](https://www.palantir.com/docs/foundry/administration/configure-application-access/), [Approvals overview](https://www.palantir.com/docs/foundry/approvals/overview/).
  - Implementation notes:
    - [`ControlPanelSettings.ApplicationAccess`](../../services/identity-federation-service/internal/handlers/control_panel.go) stores the organization catalog, lifecycle stage, default visibility, allow/block rules, approval policy, pending change requests, and immutable history events. Rules match application IDs, lifecycle stages, organization IDs, user IDs, and group IDs; block rules win over allow rules and hidden-by-default mode requires an allow rule.
    - `PUT /api/v1/control-panel` now creates application-access change requests and records approved/rejected history. Configuration changes self-approve by default; approval-policy changes create a pending request that must be approved by a distinct reviewer when the policy requires it.
    - `POST /api/v1/application-access/evaluate` evaluates application visibility for the active caller or a supplied user/group/org context and returns matched rule IDs plus an explicit `ux_scope_only` result, so consumers can hide launcher/sidebar entries without treating that answer as data authorization.
    - [`ApplicationAccessPage.tsx`](../../apps/web/src/routes/control-panel/ApplicationAccessPage.tsx) adds the Control Panel surface at `/control-panel/application-access`: JSON-backed configuration editor, evaluation probe, change-request approval/rejection actions, and history. The page prominently labels application access as UX-scope only and links from the main Control Panel page.
    - [`Sidebar.tsx`](../../apps/web/src/lib/components/Sidebar.tsx) calls the evaluation endpoint for launcher applications and hides entries denied by application-access rules while leaving backend authorization untouched.
    - Tests in [`control_panel_test.go`](../../services/identity-federation-service/internal/handlers/control_panel_test.go) cover group block rules, hidden-by-default allowlists, and distinct-reviewer approval-policy changes.

- [x] `SG.28` User and group visibility controls (`P1`, `done` 2026-05-17)
  - Allow administrators to disable user and/or group discovery within an organization while preserving administrator visibility.
  - Preserve existing permissions while warning that user-defined logic depending on user/group discovery may fail.
  - Use this as a privacy boundary for consumer-mode or cross-organization use cases.
  - Docs: [Configure user and group visibility](https://www.palantir.com/docs/foundry/administration/configure-user-and-group-visibility), [Configure Foundry for consumer mode](https://www.palantir.com/docs/foundry/consumer-mode/foundry-consumer-setup).
  - Implementation notes:
    - [`ControlPanelSettings.MemberDiscovery`](../../services/identity-federation-service/internal/handlers/control_panel.go) stores global defaults, per-organization discover-users/discover-groups overrides, consumer-mode boundary flags, warning text, and change history.
    - [`member_discovery.go`](../../services/identity-federation-service/internal/handlers/member_discovery.go) gates user/group discovery endpoints for non-admin callers while preserving administrator visibility through `admin`, `organization_admin`, `control_panel:*`, `users:*`, and `groups:*` grants.
    - User and group search/list/detail/inspection endpoints now return `member_discovery_disabled` with the documented warning when discovery is disabled for the caller's organization. The policy does not mutate roles, memberships, group grants, resource ACLs, or restricted-view authorization.
    - [`MemberDiscoveryPage.tsx`](../../apps/web/src/routes/control-panel/MemberDiscoveryPage.tsx) adds the Control Panel surface at `/control-panel/member-discovery`, with default toggles, organization overrides, consumer-mode boundary labeling, notes, and history.
    - Tests in [`control_panel_test.go`](../../services/identity-federation-service/internal/handlers/control_panel_test.go) cover history/warning persistence and non-admin blocking with admin visibility preserved.

- [x] `SG.29` File access presets (`P1`, `done` 2026-05-17)
  - Configure access presets that apply a named set of markings or local classification controls during resource creation where supported.
  - Enforce preset visibility based on apply-marking permission for every marking in the preset.
  - Support default preset ordering and guest-organization behavior when OpenFoundry implements multi-organization guests.
  - Docs: [Configure file access presets](https://www.palantir.com/docs/foundry/administration/configure-file-access-presets/), [Markings](https://www.palantir.com/docs/foundry/security/markings/).
  - Implementation notes:
    - [`ControlPanelSettings.FileAccessPresets`](../../services/identity-federation-service/internal/handlers/control_panel.go) stores enabled state, warning text, guest primary-organization behavior, ordered presets, local classification controls, supported resource kinds, organization scoping, audit metadata, and change history.
    - `POST /api/v1/file-access-presets/visible` returns only presets visible to the caller for the requested organization/resource kind. A preset is omitted unless the caller has global `markings:apply`/`markings:write`/`markings:manage` or per-marking apply grants for every marking in the preset.
    - Guest sessions use the supplied/derived primary organization when selecting presets, matching the documented rule that guests see presets from their primary organization rather than the host organization.
    - [`FileAccessPresetsPage.tsx`](../../apps/web/src/routes/control-panel/FileAccessPresetsPage.tsx) adds the Control Panel UI at `/control-panel/file-access-presets`, including preset editing, local controls JSON, default ordering, known-marking lookup, visibility probe, and history.
    - [`CreateProjectModal.tsx`](../../apps/web/src/lib/components/projects/CreateProjectModal.tsx) consumes visible project presets and sends the selected preset's markings during project creation. [`CreateProject`](../../services/tenancy-organizations-service/internal/handlers/projects.go) stores those markings in `marking_rids`, requires apply-marking permission, and merges them with template-applied markings.
    - Tests in [`control_panel_test.go`](../../services/identity-federation-service/internal/handlers/control_panel_test.go) cover history/order normalization, all-markings apply-permission visibility, and primary-organization behavior for guest sessions.

### OAuth, tokens, and third-party applications

- [x] `SG.30` Developer API token governance (`P1`, `done` 2026-05-17)
  - Allow users to create, view, and revoke temporary API tokens with explicit expiry and warnings against production use.
  - Ensure temporary tokens inherit the creating user's permissions and become unusable after expiry, revocation, or user inactivity/disablement.
  - Detect and warn on committed/shared-token patterns where local secret scanning exists.
  - Docs: [API authentication](https://www.palantir.com/docs/foundry/api/v2/general/overview/authentication/), [Manage users](https://www.palantir.com/docs/foundry/platform-security-management/manage-users).
  - Implementation notes:
    - [`api_keys`](../../services/identity-federation-service/internal/repo/migrations/0019_sg30_developer_api_tokens.sql) now stores visible prefixes, scope/permission/role snapshots, explicit expirations, status metadata, and the non-production warning while retaining only the SHA-256 token hash.
    - `POST /api/v1/api-keys` requires `expires_at`, caps developer token TTL at 30 days, derives scopes from the caller's current role/permission snapshot, and returns the plaintext `ofapikey_...` secret exactly once.
    - `POST /api/v1/auth/api-key/exchange` validates the opaque token against expiry, revocation, owner disablement/deletion, and the documented 30-day inactivity rule before issuing a short-lived access JWT carrying `auth_methods=["api_key"]` and `api_key_id`.
    - `GET /api/v1/api-keys` and `DELETE /api/v1/api-keys/{id}` provide token metadata viewing and revocation; expired/revoked keys are unusable even when their hash remains for auditability.
    - `POST /api/v1/api-keys/leak-scan` detects `ofapikey_...` patterns in pasted local diffs/config snippets, redacts matches, and escalates warnings when a match has the caller's known prefix. The Settings API key panel exposes expiry-required creation, warnings, revocation, and local exposure checks.

- [x] `SG.31` Third-party application registration (`P1`, `done` 2026-05-17)
  - Register OAuth2 third-party applications with owners, redirect URIs, client type, enabled grants, scopes, service user, credentials, and organization enablement.
  - Gate administration by third-party application administrator and OAuth client management permissions.
  - Support Developer Console-first management while providing Control Panel fallback where local product decisions allow it.
  - Docs: [Third-party applications overview](https://www.palantir.com/docs/foundry/platform-security-third-party/third-party-apps-overview/), [Writing OAuth2 clients for Foundry](https://www.palantir.com/docs/foundry/platform-security-third-party/writing-oauth2-clients).
  - Implementation notes:
    - [`third_party_applications`](../../services/identity-federation-service/internal/repo/migrations/0020_sg31_third_party_applications.sql) stores OAuth client registrations with client ID, client type, grants, redirect URIs, scopes, owner users, managing organization, discovery organizations, optional service user, secret prefix/hash metadata, and Developer Console-first management metadata.
    - Confidential clients receive a one-time `of3pa_secret_...` client secret; only the SHA-256 hash and visible prefix are persisted. `POST /api/v1/third-party-applications/{id}/rotate-secret` rotates the secret and returns the new value once.
    - `client_credentials` is rejected for public clients. Confidential clients that enable `client_credentials` get an OpenFoundry service user whose username matches the generated client ID, mirroring the documented service-user grant behavior.
    - `PUT /api/v1/third-party-applications/{id}/organizations/{organization_id}/enablement` records per-organization enablement, project scope placeholders, marking restrictions, and organization-consent flags for the SG.32 authorization/consent flow.
    - Administration routes are gated by `admin`, `third_party_application_admin` / `third_party_application_administrator`, or `oauth_clients:manage` / `third_party_applications:manage`; read-only listing also accepts `oauth_clients:read` / `third_party_applications:read`.
    - [`ThirdPartyApplicationsPage.tsx`](../../apps/web/src/routes/control-panel/ThirdPartyApplicationsPage.tsx) adds the Control Panel fallback at `/control-panel/third-party-applications`, with registration, one-time secret display, rotation, revocation, and organization enablement controls.

- [x] `SG.32` Third-party application enablement and consent (`P1`, `done` 2026-05-18)
  - Require organization-specific application enablement before users can authorize an OAuth application.
  - Implement authorization-code consent prompts, CSRF state validation, PKCE, refresh-token scopes, and revocation.
  - Ensure token capabilities are the intersection of user/service permissions, application maximum scope, and requested scope.
  - Docs: [Enabling third-party applications](https://www.palantir.com/docs/foundry/platform-security-third-party/enabling-3pa-access/), [Developer Console application scopes](https://www.palantir.com/docs/foundry/developer-console/application-scopes), [API authentication](https://www.palantir.com/docs/foundry/api/v2/general/overview/authentication/).
  - Implementation notes:
    - [`0021_sg32_third_party_oauth_consent.sql`](../../services/identity-federation-service/internal/repo/migrations/0021_sg32_third_party_oauth_consent.sql) adds one-time authorization codes, OAuth refresh tokens, consent records, expiry indexes, token-family replay revocation, and per-user authorization listing metadata.
    - `GET /api/v1/oauth2/authorize` validates bearer identity, organization-specific app enablement, registered redirect URI, explicit authorization-code scope, non-empty CSRF `state`, and S256 PKCE before returning a consent prompt with requested, granted, and missing scopes.
    - `POST /api/v1/oauth2/authorize/consent` records user consent and returns a short-lived `ofoauth_code_...` authorization code plus the redirect URI carrying `code` and echoed `state`; denial returns an `access_denied` redirect payload.
    - `POST /api/v1/oauth2/token` supports `authorization_code`, `refresh_token`, and `client_credentials`; confidential clients authenticate with their one-time/rotated secret, public clients rely on PKCE, refresh tokens rotate, and reuse kills the refresh-token family.
    - Access JWTs issued through OAuth carry empty role sets and permissions equal only to the computed scope intersection of subject permissions, application maximum scopes, existing refresh-token scopes when applicable, and requested scopes. Client-credentials tokens use the registered service user and still require organization enablement.
    - `POST /api/v1/oauth2/revoke`, `GET /api/v1/oauth2/authorizations`, and `DELETE /api/v1/oauth2/authorizations/{id}` provide token and user-facing authorization revocation. User deactivation/deletion also revokes third-party OAuth refresh tokens.
    - Disabling an application's organization enablement or revoking the application revokes matching third-party OAuth refresh tokens so old authorizations do not become active again after re-enablement.
    - Tests in [`third_party_oauth_test.go`](../../services/identity-federation-service/internal/handlers/third_party_oauth_test.go) cover scope intersection, admin scope bounding, S256 PKCE verification, OAuth client authentication, and organization enablement lookup.

- [ ] `SG.33` Service users and client credentials (`P1`, `todo`)
  - Create service users for confidential OAuth applications and client-credentials workloads.
  - Manage service user project/resource roles independently of individual employees to support long-running automations and integrations.
  - Rotate client secrets and audit service-user operations as first-class actors.
  - Docs: [Automate third-party application ownership](https://www.palantir.com/docs/foundry/automate/third-party-app-ownership/), [Writing OAuth2 clients for Foundry](https://www.palantir.com/docs/foundry/platform-security-third-party/writing-oauth2-clients).

### Network, retention, and notification governance

- [ ] `SG.34` Network egress policies (`P1`, `todo`)
  - Configure direct, agent-proxy, and same-region bucket egress policies with address, DNS/IP/CIDR, ports, agents, SNI behavior, bucket access level, and policy state.
  - Enforce pending approval, active, paused, and revoked states at workload runtime.
  - Require users to import explicit egress policies into workloads and treat importer grants as high-risk permissions.
  - Docs: [Configure network egress](https://www.palantir.com/docs/foundry/administration/configure-egress/).

- [ ] `SG.35` Egress approval and audit (`P1`, `todo`)
  - Route new egress policies and sensitive state changes through approval workflows for information security officers or equivalent roles.
  - Audit every attach/import/use/revoke/pause event and identify workloads that may export data to external systems.
  - Surface egress IP ranges, agent hosts, overlapping policy warnings, and same-region S3 bucket policy requirements.
  - Docs: [Configure network egress](https://www.palantir.com/docs/foundry/administration/configure-egress/), [Monitor audit logs](https://www.palantir.com/docs/foundry/security/monitor-audit-logs).

- [ ] `SG.36` Retention policies (`P1`, `todo`)
  - Manage recommended, custom, and legacy retention policy types with space scope and explicit deprecation status for legacy YAML-style policies where relevant.
  - Configure dataset selectors, transaction selectors, branch selectors, transaction count/age rules, include/exclude logic, and maximum policy counts.
  - Warn strongly before allowing current/latest-view transaction deletion or aborting open transactions.
  - Docs: [Retention overview](https://www.palantir.com/docs/foundry/retention/overview/), [Managing retention policies](https://www.palantir.com/docs/foundry/retention/manage-retention-policies), [Enrollments and organizations retention policies](https://www.palantir.com/docs/foundry/administration/enrollments-and-organizations-retention).

- [ ] `SG.37` Retention execution and recovery windows (`P1`, `todo`)
  - Execute retention policies by marking matching dataset transactions for deletion and periodically removing data according to the retention engine.
  - Record DELETE transactions when policies remove current view data, if local dataset semantics follow that pattern.
  - Expose execution history, deleted transaction counts, recovery/remediation windows, and irreversible deletion warnings.
  - Docs: [Retention policy execution](https://www.palantir.com/docs/foundry/retention/policy-execution/), [Managing retention policies](https://www.palantir.com/docs/foundry/retention/manage-retention-policies).

- [ ] `SG.38` Email content redaction (`P1`, `todo`)
  - Redact customer-sensitive notification content by default and include in-platform links instead of full payloads.
  - Support selected-user/group/domain allowlists for unredacted notifications only after explicit risk acknowledgment and admin permission.
  - Ensure action/automation/workflow notification systems respect strict or group redaction settings.
  - Docs: [Email content redaction](https://www.palantir.com/docs/foundry/email/email-content-redaction/), [Action type notifications](https://www.palantir.com/docs/foundry/action-types/notifications/).

### Governance-aware operational logs

- [ ] `SG.39` Action log objects (`P1`, `todo`)
  - Model action submissions as log object types with action RID, action type RID/version, timestamp, submitting user, edited object primary keys, summary, parameters, and selected context properties.
  - Automatically link log objects to edited objects where Ontology links exist.
  - Enforce action log object type permissions before action-log-backed actions can be applied.
  - Docs: [Action log](https://www.palantir.com/docs/foundry/action-types/action-log).

- [ ] `SG.40` Security audit monitoring (`P1`, `todo`)
  - Provide starter queries, dashboards, and monitors for admin changes, permission grants, marking changes, failed access, egress use, export events, token creation, and anomalous activity.
  - Support both external SIEM handoff and lightweight in-platform audit analysis.
  - Restrict audit datasets/dashboards to qualified security personnel.
  - Docs: [Monitor audit logs](https://www.palantir.com/docs/foundry/security/monitor-audit-logs), [Audit log categories](https://www.palantir.com/docs/foundry/security/audit-log-categories).

## Milestone C: advanced, scale, privacy, and compliance parity

### Cross-organization and consumer privacy

- [ ] `SG.41` Cross-organization collaboration (`P2`, `todo`)
  - Support primary organization membership, guest organization membership, cross-organization discovery, and shared resource access without exposing unrelated users/groups.
  - Provide B2B/B2C setup guidance for organizations, identity providers, group visibility, and security boundaries.
  - Prevent guest users from applying primary-organization-inappropriate access presets or seeing hidden internal groups.
  - Docs: [Organizations and spaces](https://www.palantir.com/docs/foundry/security/orgs-and-spaces/), [Manage B2B and B2C collaboration](https://www.palantir.com/docs/foundry/guides-and-workflows/b2b-b2c-collaboration), [Configure user and group visibility](https://www.palantir.com/docs/foundry/administration/configure-user-and-group-visibility).

- [ ] `SG.42` Consumer mode governance (`P2`, `todo`)
  - Configure consumer organizations with restricted platform access, hidden user/group discovery, required application access policies, and consumer-facing app/resource grants.
  - Support authentication and authorization patterns for in-platform consumer apps, Foundry-hosted OAuth apps, and client credentials apps where implemented.
  - Monitor consumer access, privacy boundaries, and app-level navigation restrictions.
  - Docs: [Consumer mode overview](https://www.palantir.com/docs/foundry/consumer-mode/overview/), [Configure Foundry for consumer mode](https://www.palantir.com/docs/foundry/consumer-mode/foundry-consumer-setup), [Configure application access](https://www.palantir.com/docs/foundry/administration/configure-application-access/).

- [ ] `SG.43` Attribute and group cache semantics (`P2`, `todo`)
  - Define refresh/caching behavior for IdP attributes, group memberships, user inactivity, and object security policies.
  - Show administrators when a policy decision may be affected by cached identity data.
  - Provide explicit cache invalidation or short TTLs for high-risk group/attribute changes where feasible.
  - Docs: [Object permissioning: managing object security](https://www.palantir.com/docs/foundry/object-permissioning/managing-object-security/), [Authentication overview](https://www.palantir.com/docs/foundry/authentication/overview/).

### Branching, Marketplace, and productization

- [ ] `SG.44` Branch-aware restricted views (`P2`, `todo`)
  - Add restricted views to branches, build/test policy changes on branches, and propagate branch restricted-view changes to indexed object types.
  - Preserve documented permissions for viewing, editing, approving, and merging branched restricted views or document intentional OpenFoundry divergence.
  - Warn that branch upstream marking differences may expose different data in branch contexts.
  - Docs: [Restricted views](https://www.palantir.com/docs/foundry/security/restricted-views), [Foundry Branching overview](https://www.palantir.com/docs/foundry/foundry-branching/overview).

- [ ] `SG.45` Branch-aware marking and permission diffs (`P2`, `todo`)
  - Show security diffs for roles, markings, restricted view policies, project references, object security, actions, and egress imports across branch/main changes.
  - Require approvals for branch merges that reduce security controls or expand access.
  - Prevent branch-only access expansions from affecting mainline runtime users before merge.
  - Docs: [Checking permissions](https://www.palantir.com/docs/foundry/security/checking-permissions/), [Manage markings](https://www.palantir.com/docs/foundry/platform-security-management/manage-markings/).

- [ ] `SG.46` Marketplace packaging of governance resources (`P2`, `todo`)
  - Package supported restricted views, project templates, application access metadata, dashboards, and dependent security configuration in OpenFoundry product bundles.
  - Package restricted view policy without packaging data, and validate install-time mappings for datasets, groups, markings, and organization-specific IDs.
  - Enforce documented Marketplace limitations for restricted view policy features or record OpenFoundry-specific extensions.
  - Docs: [Restricted views](https://www.palantir.com/docs/foundry/security/restricted-views), [Manage Project templates](https://www.palantir.com/docs/foundry/platform-security-management/manage-project-templates).

### Advanced privacy, AI, and data-governance controls

- [ ] `SG.47` AI/user-scoped execution governance (`P2`, `todo`)
  - Ensure AI assistant and AI-backed functions operate under the invoking user's identity or explicitly configured service identity without privilege escalation.
  - Require user approval for mutating actions and attribute LLM usage/rate limits to the correct user or service account.
  - Audit AI-generated operations through both AI session logs and standard audit logs.
  - Docs: [AI FDE security and governance](https://www.palantir.com/docs/foundry/ai-fde/security-and-governance/), [Foundry platform summary for LLMs](https://www.palantir.com/docs/foundry/getting-started/foundry-platform-summary-llm).

- [ ] `SG.48` Prompt and payload redaction (`P2`, `todo`)
  - Redact or summarize sensitive data in prompts, model inputs/outputs, logs, debug traces, audit records, and notification payloads according to markings and policy.
  - Prevent unauthorized embedded data from being sent to model providers or external services.
  - Provide policy-driven retention and deletion for prompt/payload records.
  - Docs: [AI FDE security and governance](https://www.palantir.com/docs/foundry/ai-fde/security-and-governance/), [Email content redaction](https://www.palantir.com/docs/foundry/email/email-content-redaction/), [Audit logs overview](https://www.palantir.com/docs/foundry/security/audit-logs-overview).

- [ ] `SG.49` Export governance and justifications (`P2`, `todo`)
  - Apply checkpoint/justification requirements to CSV, dataset, media, dashboard, BI, Notepad, model, object set, and API exports where configured.
  - Include resource, filters, parameters, branch, markings, restricted view policy, user, destination, and justification in export provenance.
  - Block exports that would violate markings, restricted views, object security, application policy, or egress policy.
  - Docs: [Security overview](https://www.palantir.com/docs/foundry/security/overview/), [Audit logs overview](https://www.palantir.com/docs/foundry/security/audit-logs-overview), [Configure network egress](https://www.palantir.com/docs/foundry/administration/configure-egress/).

- [ ] `SG.50` Data classification and sensitivity workflow (`P2`, `todo`)
  - Provide a governance workflow to identify sensitive datasets, assign markings, create restricted views, set retention, and configure export/egress limitations.
  - Support patterns for PII/PHI/CUI/classified-like sensitivity categories using OpenFoundry-native markings and project templates.
  - Track data owner, sensitivity rationale, training/access prerequisites, and review cadence.
  - Docs: [Markings](https://www.palantir.com/docs/foundry/security/markings/), [Securing a business application](https://www.palantir.com/docs/foundry/security/securing-a-business-application), [Enrollments and organizations retention policies](https://www.palantir.com/docs/foundry/administration/enrollments-and-organizations-retention).

### Compliance operations and security posture

- [ ] `SG.51` Security findings and case management (`P2`, `todo`)
  - Create findings from audit-monitor detections, permission drift, stale users/groups, risky egress, export anomalies, token leaks, or retention failures.
  - Assign findings, track status, comments, remediation tasks, evidence links, and closure approvals.
  - Link findings to audit events, resources, actors, and policy decisions.
  - Docs: [Monitor audit logs](https://www.palantir.com/docs/foundry/security/monitor-audit-logs), [Audit log categories](https://www.palantir.com/docs/foundry/security/audit-log-categories).

- [ ] `SG.52` Access reviews and recertification (`P2`, `todo`)
  - Schedule reviews of project roles, marking memberships, group membership, third-party app enablements, service users, egress importers, and admin permissions.
  - Provide reviewers with effective-access graphs and group project-access impact views.
  - Require attestations, removals, or exceptions before review closure.
  - Docs: [Manage groups](https://www.palantir.com/docs/foundry/platform-security-management/manage-groups), [Checking permissions](https://www.palantir.com/docs/foundry/security/checking-permissions/), [Manage markings](https://www.palantir.com/docs/foundry/platform-security-management/manage-markings/).

- [ ] `SG.53` Least-privilege recommendations (`P2`, `todo`)
  - Recommend group-based grants, default Discoverer project roles, specific egress importers, scoped sessions, and resource-specific OAuth scopes.
  - Detect overbroad owner/editor grants, unused groups, stale tokens, unscoped OAuth applications, unrestricted app access, and unredacted email settings.
  - Provide safe remediation proposals with approval workflows.
  - Docs: [Projects and roles](https://www.palantir.com/docs/foundry/security/projects-and-roles), [Configure network egress](https://www.palantir.com/docs/foundry/administration/configure-egress/), [Developer Console application scopes](https://www.palantir.com/docs/foundry/developer-console/application-scopes).

- [ ] `SG.54` Self-hosted security checklist (`P2`, `todo`)
  - Document additional host, network, audit, patch, certificate, backup, and monitoring requirements for self-hosted OpenFoundry deployments.
  - Collect host/security logs and integrate them with platform audit logs for incident investigation.
  - Provide environment-specific hardening guidance without implying Palantir-managed infrastructure guarantees.
  - Docs: [Protecting your self-hosted Foundry installation](https://www.palantir.com/docs/foundry/security/protect-foundry-installation), [Shared security responsibility model](https://www.palantir.com/docs/foundry/security/shared-security-responsibility-model).

- [ ] `SG.55` Governance usage and budget controls (`P2`, `todo`)
  - Attribute usage to users, groups, projects, workloads, service accounts, OAuth apps, audit exports, retention jobs, and egress-enabled workloads.
  - Support monitors and budgets for anomalous compute/storage/API/query usage that may indicate misuse or misconfiguration.
  - Integrate security findings with Resource Management anomalies.
  - Docs: [Resource Management usage types](https://www.palantir.com/docs/foundry/resource-management/usage-types), [Monitor audit logs](https://www.palantir.com/docs/foundry/security/monitor-audit-logs).

- [ ] `SG.56` Governance policy as code (`P2`, `todo`)
  - Represent project templates, roles, marking grants, restricted view policies, app access, egress, retention, and audit monitors as versioned declarative config.
  - Support dry-run, diff, approval, rollout, rollback, and drift detection.
  - Ensure policy-as-code cannot bypass UI/API permission checks and all changes remain audited.
  - Docs: [Manage roles](https://www.palantir.com/docs/foundry/platform-security-management/manage-roles/), [Manage restricted views](https://www.palantir.com/docs/foundry/platform-security-management/manage-restricted-views/), [Control Panel](https://www.palantir.com/docs/foundry/administration/control-panel/).

## Milestone D: hierarchical markings, RV non-input enforcement, data residency

> **Added 2026-05-17.** Closes three concrete gaps identified in the
> 2026-05-17 parity audit: markings are currently flat, restricted views
> are not blocked as transform inputs, and there is no data-residency /
> region story.

### Hierarchical markings

- [ ] `SG.27` Hierarchical marking model (`P1`, `todo`)
  - Replace flat marking labels with a hierarchical model where a marking can declare a parent (`synthetic ⊂ deidentified ⊂ identifiable` and similar lineages).
  - Caller clearance for a child implies clearance for its ancestors; queries that filter on an ancestor automatically include descendants (admin-toggleable).
  - Migration tool to convert existing flat markings into a single-level hierarchy with no behavior change.
  - Docs: [Marking hierarchies](https://www.palantir.com/docs/foundry/security/markings#hierarchies).

- [ ] `SG.28` Hierarchical marking admin UI (`P1`, `todo`)
  - Marking admin view shows the full tree, allows parent reassignment with impact preview, and warns on cycles or orphaned descendants.
  - Docs: [Marking hierarchies](https://www.palantir.com/docs/foundry/security/markings#hierarchies).

- [ ] `SG.29` Hierarchy-aware propagation (`P1`, `todo`)
  - Derived datasets, indexer pipelines, and Action writeback paths propagate the most specific marking encountered (deepest in the hierarchy) rather than a flat union.
  - Audit events record both the applied marking and its hierarchy path.
  - Docs: [Marking propagation](https://www.palantir.com/docs/foundry/security/marking-propagation).

### Restricted-view non-input enforcement

- [ ] `SG.30` Block restricted views as transform inputs (`P1`, `todo`)
  - Pipeline Builder, Code Repos transforms, and dataset-build APIs reject a build whose input set includes any restricted view; surface a clear "use the underlying dataset" error with documentation link.
  - Required to preserve reproducibility: restricted views are per-caller and not deterministic.
  - Docs: [Restricted views constraints](https://www.palantir.com/docs/foundry/security/restricted-views#non-input-rule).

- [ ] `SG.31` Auditor explanation for RV-input refusals (`P1`, `todo`)
  - When a build is refused due to RV inputs, the audit event records the user, the involved RV RIDs, and the policy that produced the refusal.
  - Docs: [Restricted views constraints](https://www.palantir.com/docs/foundry/security/restricted-views#non-input-rule).

### Data residency and regions

- [ ] `SG.32` Region tag on resources (`P2`, `todo`)
  - Every Compass resource carries a region tag derived from the project's region; cross-region read/write requires explicit user action and audit.
  - Docs: [Data residency](https://www.palantir.com/docs/foundry/security/data-residency).

- [ ] `SG.33` Egress policy per region (`P2`, `todo`)
  - Network egress policies declare allowed destination regions; calls to out-of-region destinations require an additional approval recorded in audit.
  - Required to satisfy GDPR-EU and similar residency commitments.
  - Docs: [Data residency](https://www.palantir.com/docs/foundry/security/data-residency).

- [ ] `SG.34` Region pinning in OSDK and Functions (`P2`, `todo`)
  - OSDK clients and Functions runtime carry a region context; out-of-region reads/writes return a typed `RegionDeniedError` (see [OSDK checklist](./foundry-osdk-1to1-checklist.md)).
  - Docs: [Data residency](https://www.palantir.com/docs/foundry/security/data-residency).

### Multi-tenant isolation hardening

- [ ] `SG.35` Row-level security on tenant tables (`P1`, `todo`)
  - For every shared Postgres table that stores per-tenant data, enable RLS policies that filter by `tenant_id` from the session context derived from the JWT.
  - Migration step + linter that fails CI when a new table lacks an RLS policy in tenant-scoped schemas.
  - Docs: [Multi-tenant isolation](https://www.palantir.com/docs/foundry/security/multi-tenancy).

- [ ] `SG.36` Tenant boundary verification tests (`P1`, `todo`)
  - Integration tests that attempt cross-tenant reads through every service's repo layer and expect rejection.
  - Run in `make test-integration` with testcontainers.
  - Docs: [Multi-tenant isolation](https://www.palantir.com/docs/foundry/security/multi-tenancy).

### Cryptographic hardening

- [ ] `SG.37` Refresh token rotation with revocation list (`P2`, `todo`)
  - Refresh tokens rotate on every use; previous token immediately invalidated via a token revocation list with short TTL.
  - Required for FedRAMP-style session integrity.
  - Docs: [Identity tokens](https://www.palantir.com/docs/foundry/security/identity-tokens).

- [ ] `SG.38` Token binding to caller fingerprint (`P2`, `todo`)
  - Optional cryptographic binding of tokens to the originating device/IP fingerprint; mismatches reject with audit.
  - Off by default; admin opt-in per organization.
  - Docs: [Identity tokens](https://www.palantir.com/docs/foundry/security/identity-tokens).

## Implementation inventory checklist

- [ ] `INV.1` Identify existing OpenFoundry identity, session, API token, SSO, SAML/OIDC, user, group, realm, and service-account primitives.
- [ ] `INV.2` Inventory current project/folder/resource permission models, default roles, custom roles, operation checks, inheritance, and effective-access explanation APIs.
- [ ] `INV.3` Inventory existing marking/classification/security-label support, lineage propagation, branch diffing, and mandatory-access enforcement points.
- [ ] `INV.4` Inventory current access request, Approvals, workflow/task, notification, and group membership expiration capabilities.
- [ ] `INV.5` Inventory restricted view, row-level permission, user attribute policy, object security, Ontology indexing, and SQL/query rewrite capabilities.
- [ ] `INV.6` Inventory Control Panel/admin UI, organization settings, application access, user/group discovery, scoped session, and consumer-mode support.
- [ ] `INV.7` Inventory OAuth2/Developer Console/third-party app registration, client credentials, scopes, consent, service users, credential rotation, and token revocation.
- [ ] `INV.8` Inventory network egress policy, Data Connection agent, external transform/function/model egress, PrivateLink/VPN/ingress, credential vault, and secret redaction support.
- [ ] `INV.9` Inventory retention policy, dataset selector, transaction selector, branch deletion, deletion transaction, recovery/remediation, and irreversible delete support.
- [ ] `INV.10` Inventory audit logging, audit schemas, audit export/API delivery, SIEM integrations, audit datasets, audit dashboards, categories, and anomaly monitoring.
- [ ] `INV.11` Inventory email redaction, export checkpoint, action log, object edit history, application logging, prompt/payload redaction, and sensitive-data handling primitives.
- [ ] `INV.12` Identify public-doc limitations OpenFoundry should mirror exactly versus intentionally diverge from, such as marking immutability, restricted-view transform limitations, app access not being security, and branch restricted-view experimental semantics.
- [ ] `INV.13` Produce a machine-readable parity matrix sibling JSON after inventory, following the pattern of [foundry-feature-parity-matrix.json](./foundry-feature-parity-matrix.json).

## Suggested service boundaries

> **Reader note (2026-05-14)** — The services in the table below are
> *target* decomposition proposals, not a current inventory of
> binaries. Some have been built under consolidated names after S8
> (`marketplace-service` → `federation-product-exchange-service`;
> `approvals-service` → `workflow-automation-service/internal/approvals`;
> `ontology-security-service` → `authorization-policy-service`;
> `ai-service` → `agent-runtime-service` + `llm-catalog-service`).
> Others are not yet implemented. For the canonical list of binaries
> on disk today, see
> [`docs/architecture/services-and-ports.md`](../architecture/services-and-ports.md).

| Surface | Responsibilities |
| --- | --- |
| `identity-service` | Users, sessions, SAML/OIDC providers, user attributes, realms, login state, token invalidation, and inactivity handling. |
| `group-management-service` | Internal/external/rule groups, nested membership, group permissions, membership expiration, group attributes, project access views. |
| `organization-service` | Enrollments, organizations, spaces, guest membership, organization roles, member discovery, scoped session settings, consumer-mode boundaries. |
| `authorization-service` | Role/operation checks, effective permissions, inheritance, role sets, access graph, policy decision logging, API authorization middleware. |
| `project-access-service` | Project roles, default roles, group role grants, access requests, project templates, references, access request hiding/exclusions. |
| `marking-service` | Marking categories, markings, memberships, apply/remove/manage permissions, inheritance, lineage propagation, scoped session enforcement. |
| `granular-policy-service` | Restricted view policies, row-level policy evaluation, user attribute comparisons, marking-backed row checks, policy explainability. |
| `restricted-view-service` | Restricted view CRUD, builds/previews, transactions, object type backing, branch behavior, Marketplace policy packaging. |
| `approvals-service` | Access requests, application-access changes, egress/retention/policy approvals, approval policies, tasks, comments, and decision audit. |
| `oauth-app-service` | Third-party application registration, organization enablement, OAuth grants, scopes, service users, consent, secret rotation, revocation. |
| `egress-governance-service` | Network egress policies, direct/agent/bucket egress, states, importer/viewer grants, workload attachment, overlap/risk detection. |
| `retention-service` | Retention policies, selectors, execution runs, deletion marking, recovery/remediation windows, dangerous flag controls. |
| `audit-service` | Audit event ingestion, normalization, categories, sensitive audit storage, audit API, dataset/SIEM delivery, duplicate handling. |
| `security-monitoring-service` | Audit queries, anomaly detection, security findings, access reviews, least-privilege recommendations, dashboards, alerts. |
| `notification-governance-service` | Email redaction, notification content policy, domain/group allowlists, action/automation notification enforcement. |
| `ontology-security-service` | Object security, restricted-view-backed object types, action log object creation, object edit history and action permission checks. |
| `resource-management service` | Usage attribution, budgets, monitors, anomalous usage, project/service/user/OAuth app usage rollups. |
| `apps/web` | Control Panel-like UI, security landing pages, access graph UI, marking/restricted-view editors, approvals inbox, audit dashboards. |

## Acceptance criteria for first complete Security and Governance milestone

- [ ] A platform administrator can configure an identity provider, map user attributes/groups, view users, preregister users, and inspect inactive/disabled token behavior.
- [ ] An organization administrator can manage groups, nested memberships, group permissions, contact details, membership expiration, and group project-access impact.
- [ ] A project owner can configure default roles and group role grants, and users can request project/group/marking access with approval routing and reason capture.
- [ ] OpenFoundry enforces organization membership, all required markings, scoped session state, and discretionary roles before resource discovery/read/write operations.
- [ ] Marking administrators can create categories/markings, grant manage/apply/remove/member permissions, apply markings to projects/resources, and inspect inheritance/lineage propagation.
- [ ] Permission-checking tools can explain why a user can or cannot access a resource using organizations, markings, roles, groups, inheritance, and restricted view policy where applicable.
- [ ] Restricted views can be created over datasets with granular policies, queried by eligible users, used to back object types, and blocked from transform inputs when matching documented limitations.
- [ ] Scoped sessions can be configured for an organization and restrict filesystem and Ontology access to the active marking subset.
- [ ] Third-party OAuth applications can be registered/enabled, request scoped authorization, use service users for client credentials, and have token scopes intersect with underlying user/service permissions.
- [ ] Network egress policies require explicit import/use permissions, enforce pending/active/paused/revoked state, and audit every sensitive lifecycle event.
- [ ] Retention policies can select datasets/transactions, execute deletion according to policy, expose execution history, and warn on irreversible or latest-view deletion settings.
- [ ] Audit logs capture who/what/when/where for security-relevant operations and can be delivered to a SIEM-like endpoint or OpenFoundry datasets with restricted access.
- [ ] Email notifications are redacted by default and only become unredacted under explicit administrator-controlled policies.
- [ ] Application access controls and user/group discovery controls can reduce user-visible surface area while clearly documenting that server-side permissions remain authoritative.
- [ ] All OpenFoundry runtime UI is OpenFoundry-native and does not use Palantir branding, screenshots, icons, fonts, or proprietary assets.

## Test plan expectations

- Unit tests for effective permission resolution, role inheritance, nested groups, organization membership, marking conjunction, marking apply/remove permissions, scoped session filtering, access request routing, and disabled/inactive token invalidation.
- Unit tests for restricted view policy parsing/type checking, user attribute comparisons, group/organization ID matching, marking-backed row checks, branch policy diffing, and transform-input rejection.
- Unit tests for OAuth scope intersection, authorization-code PKCE/state validation, client credentials service-user permissions, token expiry/revocation, application enablement, and unscoped-app warnings.
- Unit tests for egress policy address/port validation, policy state enforcement, importer/viewer grants, retention selectors, dangerous retention flags, email redaction policy matching, and audit event normalization.
- API tests for users, groups, organizations, spaces, roles, projects, access requests, approvals, marking categories, markings, scoped sessions, restricted views, third-party apps, tokens, egress policies, retention policies, audit exports, and action logs.
- Integration tests for SAML/OIDC login attribute mapping, group-driven project access, marking propagation through dataset builds, restricted-view-backed object types, scoped-session Ontology reads, OAuth app access, egress-enabled workload execution, retention deletion, SIEM audit export, and redacted notifications.
- E2E tests for admin identity setup, secure project template creation, project access request approval, marking application/removal approval, restricted view creation/query/Object Explorer use, scoped session login/change, OAuth consent, egress policy approval/import, retention policy run, and audit monitoring investigation.
- Observability tests for access-denied decisions, permission graph generation, marking propagation events, policy evaluation latency, audit log delivery lag, egress failures, token misuse, stale user/group findings, retention execution health, and security monitor alerts.
- Regression tests proving unauthorized users cannot discover protected resources, markings cannot be bypassed by owner roles, scoped sessions reduce access, restricted views cannot feed transforms, external BI/API reads obey row-level policy, app access controls do not replace server-side authorization, OAuth tokens cannot exceed user/service permissions, revoked egress policies fail workloads, email redaction cannot be bypassed by custom notifications, and audit logs are restricted as sensitive data.
