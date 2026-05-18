# tenancy-organizations-service

Owns organizations, workspace enrollments, Compass project/folder resources,
sharing, trash, favorites, recents, reverse references, and the resource-resolve /
resource-ops helpers.

## Service surface

Endpoints (all under `/api/v1`, JWT-protected):

- `GET    /organizations`            — list (200 most-recent)
- `POST   /organizations`            — create
- `GET    /organizations/{id}`       — fetch
- `PATCH  /organizations/{id}`       — partial update
- `DELETE /organizations/{id}`       — delete
- `GET    /organizations/{id}/enrollments` — list enrollments for an org
- `POST   /enrollments`              — create
- `DELETE /enrollments/{id}`         — delete
- `GET|POST /projects/{id-or-rid}/folders` — list/create nested folder resources
- `PATCH /projects/{id-or-rid}/folders/{folder_id-or-rid}/propagate-view-requirements` — manage the legacy folder-level view-requirement propagation toggle
- `GET /projects/{id-or-rid}/propagate-view-requirements/jobs` — list background re-propagation jobs and progress
- `POST /workspace/resources/{kind}/{id}/move|rename` — RID-preserving resource operations
- `POST /workspace/resources/batch` — preflighted Compass move/trash/share batches with one aggregate audit event
- `GET|PUT /workspace/resources/{kind}/{id}/references` — Compass reverse-reference graph (`depends_on` / `used_by`)
- `GET /workspace/trash`, `POST /workspace/resources/{kind}/{id}/restore`, `DELETE /workspace/resources/{kind}/{id}/purge` — Compass Trash list, restore, and permanent-delete surface
- `GET|POST /workspace/favorites`, `PUT /workspace/favorites/order`, `GET|POST /workspace/favorites/groups`, `PUT /workspace/favorites/groups/order` — synced per-user Compass favorites, groups, and display order
- `GET|POST /workspace/recents` — per-user Compass recent resources, capped and permission-filtered at read time
- `GET    /compass/search`           — permission-aware Compass resource search over project/folder index entries

Plus the standard `/healthz` + `/metrics` foundation surface.

The schema lives at
`internal/repo/migrations/0001_tenancy_organizations_foundation.sql`.

## Configuration

| Env var                   | Required | Default  |
|---------------------------|----------|----------|
| `DATABASE_URL`            | yes      | —        |
| `OPENFOUNDRY_JWT_SECRET` / `JWT_SECRET` | yes      | —        |
| `HOST`                    | no       | `0.0.0.0`|
| `PORT`                    | no       | `50113`  |
| `METRICS_ADDR`            | no       | `0.0.0.0:9090` |
| `SERVICE_VERSION`         | no       | `dev`    |

## Folder resource contract

Folders are persisted in `ontology_project_folders` and exposed as Compass
`FOLDER` resources. Each row carries a stable `rid`; responses project the
owning `project_rid`, `parent_folder_rid`, `space_rid`, trash status, and
policy-inheritance flags. Create requests accept legacy `parent_folder_id`
or canonical `parent_folder_rid`; folder access inherits project policies and
uses folder-scope resource grants for explicit overrides.

## Propagate view requirements contract

`propagate_view_requirements_enabled` is kept only as a legacy compatibility
setting because Palantir documents the feature as planned-deprecated and
recommends migration to Markings. Project and folder rows default the toggle
off, store `propagate_view_requirements_disabled_at` when the setting is
turned off, and reject future re-enable attempts after that timestamp exists.

When enabled, the setting copies a snapshot of inherited
`view_requirement_marking_rids` onto newly created child folders and project
resource bindings. Parent policy changes enqueue
`compass_view_requirement_propagation_jobs`, which update existing descendant
folder snapshots, refresh folder search entries, update project resource
bindings for project-level jobs, and expose processed/changed counts through
the projects API. Successful jobs emit `compass.view_requirements.propagated`
to `audit.events.v1` with parent RID, target/previous markings, changed counts,
and a capped affected-dependent list. The Control Panel project editor shows
the migration note so operators know to secure sensitive data with Markings
before disabling the legacy setting.

## Move / rename contract

Workspace move and rename operations preserve project/folder RIDs. Folder
rename updates both `name` and `slug`, so breadcrumbs derived from project and
folder paths refresh without link churn. Folder moves update parentage; when a
folder subtree crosses projects, the caller must confirm access-policy changes,
the target project must be marking-compatible, and compatible marking changes
must be explicitly confirmed.

## Search index contract

Project and folder lifecycle writes maintain `compass_resource_search_index`
inside the same database transaction. Each entry is keyed by immutable RID and
carries type, display name, owning project, organization RIDs, marking RIDs,
last modified time, owner, tags, summary, long-text catalog body, long-text
source metadata, open URL, and trash state. Long-text sources cover resource
descriptions, README content, ontology object/property descriptions, code
repository READMEs, and dashboard descriptions. Owning services can compose
source bodies with `BuildResourceSearchLongText` and refresh an existing RID's
catalog text with `UpdateResourceSearchLongTextTx`. The same transaction emits
`compass.resource.search.updated.v1` via `outbox.events`, so future
Vespa/OpenSearch indexers can subscribe to resource changes without polling
project or folder tables.

`GET /api/v1/compass/search` reads that projection with permission-aware
filters. Supported query parameters are `q`, `type`, `project` (UUID or
Compass project RID), `owner`, repeated `marking`, `modified`
(`24h`, `7d`, `30d`, or `older`), `limit` (capped at 200), and opaque
`cursor`. Results are ordered by text score, `last_modified_at` descending,
and RID ascending, include a bounded snippet for the best long-text match while
omitting the full long-text body from the HTTP response, and return facets for
type, project, owner, marking, and last-modified buckets.

The web Quicksearch shell consumes this endpoint alongside ontology search:
resource rows surface the immutable RID, type, owning project, marking badges,
snippet highlights, and `open_url`, while the frontend resource type registry
controls display labels, icons, and "Open with" targets.

`compass_saved_searches` stores per-user named Quicksearch/Data Catalog queries
with tab, type, project, owner, marking, and last-modified filters. The
workspace API exposes `GET|POST|DELETE /api/v1/workspace/saved-searches`, and
the web search sidebar renders those saved searches alongside the facet list.

`open_url` is a stable RID-based deep link, not a path slug. Project entries use
`/projects/{projectRid}`, folder entries use
`/projects/{projectRid}/folders/{folderRid}`, and optional UI slugs are added
only by the web router. Migration `0017_cmp15_stable_resource_urls.sql`
rewrites existing project/folder search-index rows to this contract.

## Favorites profile contract

`user_favorites` stores each caller's profile-backed Compass resource
shortcuts by `(user_id, resource_kind, resource_id)`. Migration
`0018_cmp16_favorites_profile.sql` adds `group_id`, `display_order`, and
`updated_at`, plus `user_favorite_groups` for named per-user groups. The list
response keeps the existing `{data:[...]}` envelope and adds `groups`, so older
clients still read favorites while newer clients can render grouped, reordered
shortcuts across devices.

## Recents profile contract

`resource_access_log` stores best-effort per-user resource opens written by
`POST /api/v1/workspace/recents`. `GET /api/v1/workspace/recents` deduplicates
the log by `(resource_kind, resource_id)`, returns at most `limit` rows
(`50` by default, capped at `500`), orders by `last_accessed_at DESC`, and
filters every candidate through the caller's current accessible projects.
Projects, folders, resource bindings, project-bound external resources, and
Compass search-indexed resources all disappear from recents when their project
access is revoked or the resource is trashed.

## Recommendations contract

`compass_project_follows` stores explicit per-user follows on projects. `GET
/api/v1/workspace/recommendations` first intersects candidate resources with
the caller's current accessible projects, then scores visible
`compass_resource_search_index` entries using collaborator opens from
`resource_access_log`, the caller's own recent opens, and followed projects.
The response includes the resource search fields, score, reason, signals,
collaborator count, and last activity timestamp; it strips long-text bodies the
same way Compass search does. `GET|POST|DELETE /api/v1/workspace/project-follows`
manages the explicit follow signal.

## Reference graph contract

`compass_resource_references` stores explicit directed edges where a source
resource depends on a target resource. `GET
/api/v1/workspace/resources/{kind}/{id}/references` returns both upstream
`depends_on` edges and reverse `used_by` edges. The read path also derives
project containment from `ontology_project_resources` and project-level
references from `ontology_projects.references`, so existing project metadata
participates in the graph without a backfill.

`PUT /api/v1/workspace/resources/{kind}/{id}/references` replaces explicit
upstream edges for a source resource, rejects self-references, and is limited
to project owners/admins for ontology-owned resources or admins for externally
owned resources. The web details panel consumes `used_by` / `depends_on`, and
move/trash dialogs preflight this endpoint before risky operations.

## Trash workflow contract

Project, folder, and resource-binding deletes are soft deletes. Trash writes
stamp `deleted_at`, `deleted_by`, `trash_retention_days`, and `purge_after`;
`retention_days` defaults to 30 and is bounded to 1..3650 when supplied to the
workspace resource-delete endpoint or batch delete action.

`GET /api/v1/workspace/trash` returns the retention window, purge-after time,
original project/folder placement, and `restore_target_status`. Restore clears
the soft-delete metadata and re-indexes the resource. Folders restore to their
original parent when it still exists and is not trashed; otherwise they restore
to the project root and the response includes a banner for the web UI.

Permanent delete goes through `DELETE /api/v1/workspace/resources/{kind}/{id}/purge`.
Non-admin callers can purge only after `purge_after`; admins can override the
retention window. The purge transaction removes directly affected favorites,
recents, shares, folder-scope grants, and search-index rows, then emits a
marking-aware `compass.resource.purged` event to `audit.events.v1` with the
deleted resource RID, purge mode, and affected dependents.

Resource lifecycle mutations also emit standard Compass audit events in the
same transaction as the resource change. Project/folder/resource-binding create,
move, rename, trash, restore, purge, direct share grant/update/revoke, and
project marking updates produce `compass.resource.*` envelopes with the resource
RID, project RID, markings at event time, path/name deltas, share principal
details, and retention/restore metadata. `audit-sink` consumes `audit.events.v1`
and makes those rows available from `GET /api/v1/audit/events` and export.

`POST /api/v1/workspace/resources/batch` accepts selections from search results
or folder listings and applies `move`, `delete`/`trash`, and `share` only after
every row passes preflight. Folder moves reuse cross-project access-policy and
marking confirmation checks, resource-binding moves require a target project,
trash validates retention bounds and owner/admin policy, and share validates a
single user/group principal plus access level. Any failed preflight marks the
whole response `preflight_failed=true` and leaves resources unchanged. Applied
batches suppress per-resource audit emission and write one
`compass.resource.bulk_operation` event with the `batch_id`, counts, and per-row
targets/errors/share metadata.

## Follow-up slices (deferred)

- Spaces (`tenancy_workspaces` table) — Rust migration `0002`
- Projects (`tenancy_projects` table)
- Sharing rules + invitations
- `resource_resolve` / `resource_ops` helpers (cross-service RID lookup)

These are tracked under todos and the archived inventory at
`docs/archive/INVENTORY-tenancy-organizations-service.md`.

## Build / test

```sh
cd openfoundry-go
go build ./services/tenancy-organizations-service/...
go test -race ./services/tenancy-organizations-service/...
```
