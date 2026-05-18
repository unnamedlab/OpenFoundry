# Foundry Compass 1:1 parity checklist

Date: 2026-05-17
Scope: public-docs-based parity plan for OpenFoundry's Compass-equivalent
resource layer: stable Resource IDs (RIDs), filesystem of projects and
folders, resource registry across all services, federated search and
catalog, breadcrumbs and navigation, resource move/rename/trash/restore,
favorites and recents, links between resources, propagated view
requirements, and integrations with every app that exposes resources
(Datasets, Pipelines, Ontology, Functions, Workshop, Slate, Quiver,
Contour, Notepad, Reports, Map, Vertex, Fusion, Agents, AIP Logic,
Notebooks, Code Repos, Models, Compute Modules, Marketplace).

> **Scope distinction.** Compass is the **foundation layer** that makes
> every resource direct-addressable, searchable, organized in a
> filesystem, and discoverable across apps. It is *not* the security
> model (owned by
> [foundry-security-governance-1to1-checklist.md](./foundry-security-governance-1to1-checklist.md))
> but it is the carrier of resource identity that the security model
> applies policies to. Every other checklist's "stable RID" and
> "Compass-discoverable" requirements resolve here.

This document is intentionally implementation-oriented. It does not attempt
to clone Palantir branding, private source code, proprietary assets, or any
non-public behavior. The target is **functional parity based on public
Palantir Foundry documentation**.

## Parity scope boundary

All checklist work is governed by the
[Foundry public-docs parity policy](../reference/foundry-public-docs-parity-policy.md).
OpenFoundry may implement behavior described in public Palantir documentation,
but contributors must not copy private source, decompile bundles, import
tenant-specific exports, use Palantir branding, or reuse proprietary assets.

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
| `P0` | Required for credible resource layer: RID scheme, resource registry, projects/folders, federated search, breadcrumbs, move/rename/trash/restore. |
| `P1` | Required for Foundry-style parity: cross-resource links, favorites/recents, propagated view requirements, audit, bulk operations. |
| `P2` | Advanced parity: full-text catalog with facets, recommendations, dependency graph view, cross-region addressability, restore from deep trash. |

## Official Palantir documentation library

### Product overview

- [Compass overview](https://www.palantir.com/docs/foundry/compass/overview)
- [Foundry platform summary for LLMs](https://www.palantir.com/docs/foundry/getting-started/foundry-platform-summary-llm)

### Concepts

- [Projects and resources](https://www.palantir.com/docs/foundry/getting-started/projects-and-resources/)
- [Filesystem Get Resource API](https://www.palantir.com/docs/foundry/api/filesystem-v2-resources/resources/get-resource/)
- [Resource Identifier specification](https://github.com/palantir/resource-identifier)
- [Filesystem Create Folder API](https://www.palantir.com/docs/foundry/api/filesystem-v2-resources/folders/create-folder/)
- [Filesystem Get Folder API](https://www.palantir.com/docs/foundry/api/filesystem-v2-resources/folders/get-folder/)
- [Quicksearch](https://www.palantir.com/docs/foundry/getting-started/quicksearch)
- [Data Catalog](https://www.palantir.com/docs/foundry/compass/data-catalog)
- [Trash and restore](https://www.palantir.com/docs/foundry/compass/trash)
- [Migrate from and disable "Propagate view requirements" setting](https://www.palantir.com/docs/foundry/platform-security-management/disabling-propagate-view-requirements/)

### Integrations

- [Resource type registry](https://www.palantir.com/docs/foundry/compass/resource-types)
- [Cross-app navigation](https://www.palantir.com/docs/foundry/compass/navigation)
- [Audit on resources](https://www.palantir.com/docs/foundry/compass/audit)

## Milestone A: credible RIDs, registry, filesystem, search

### Resource ID scheme

- [x] `CMP.1` RID format (`P0`, `done`)
  - Format: `ri.<service-id>.<instance-id>.<type-id>.<uuid>` (e.g. `ri.dataset.main.dataset.b8e1...`).
  - RIDs are immutable for the lifetime of the resource (rename and move do not change the RID).
  - Documented as the canonical reference everywhere a resource is mentioned in URLs, API payloads, audit events, OSDK methods, and Marketplace bundles.
  - Implementation: `libs/core-models/rid` validates the public RID grammar, exposes UUID-locator parsing for platform-minted resources, mints UUIDv7-backed RIDs, and is used by the canonical dataset RID helper.
  - Documentation: `README.md`, `ARCHITECTURE.md`, and `docs/reference/foundry-compatibility-glossary.md` define RID as the cross-platform stable identifier and require `libs/core-models/rid` for new parsing/validation.
  - Verification: `go test ./libs/core-models/...`.
  - Docs: [Projects and resources](https://www.palantir.com/docs/foundry/getting-started/projects-and-resources/), [Filesystem Get Resource API](https://www.palantir.com/docs/foundry/api/filesystem-v2-resources/resources/get-resource/), [Resource Identifier specification](https://github.com/palantir/resource-identifier).

- [ ] `CMP.2` RID minting (`P0`, `todo`)
  - Central RID minter or per-service deterministic generator (UUIDv7 inside the `<uuid>` slot) with collision detection on insert into the registry.
  - Docs: [Resources, RIDs, and projects](https://www.palantir.com/docs/foundry/compass/resources).

- [ ] `CMP.3` Resource type registry (`P0`, `todo`)
  - A central registry of resource types: id, display name, owning service, default icon, supported actions (move, rename, trash, restore, share), and the open-app URL template.
  - Adding a new type requires a registry entry; unknown types render with a placeholder.
  - Docs: [Resource type registry](https://www.palantir.com/docs/foundry/compass/resource-types).

### Filesystem

- [ ] `CMP.4` Project resource (`P0`, `todo`)
  - `project` is a top-level container with owner, organizations, markings, default queue (see [Resource Management](./foundry-resource-management-1to1-checklist.md)), and per-role access policies.
  - Projects can be nested inside spaces (admin-defined groupings).
  - Docs: [Filesystem Create Project API](https://www.palantir.com/docs/foundry/api/filesystem-v2-resources/projects/create-project/), [Filesystem Get Project API](https://www.palantir.com/docs/foundry/api/filesystem-v2-resources/projects/get-project/), [Filesystem List Spaces API](https://www.palantir.com/docs/foundry/api/filesystem-v2-resources/spaces/list-spaces/).

- [x] `CMP.5` Folder resource (`P0`, `done`)
  - `folder` containers nestable inside a project (or another folder); each folder has a stable RID and inherits the project's policies unless overridden.
  - Implementation: `tenancy-organizations-service` persists project folders with immutable `ri.compass.main.folder.<uuid>` RIDs, exposes Compass metadata (`project_rid`, `parent_folder_rid`, `space_rid`, `type`, `trash_status`), and accepts either legacy `parent_folder_id` UUIDs or canonical `parent_folder_rid` values for nested creation.
  - Policy inheritance: project owner/default/user/group roles still apply to every folder; folder-scope direct grants remain the explicit override path and are resolved across the folder ancestor chain by effective access.
  - Documentation: `README.md`, `ARCHITECTURE.md`, `docs/reference/foundry-compatibility-glossary.md`, and `services/tenancy-organizations-service/README.md` define the folder resource contract.
  - Verification: `go test ./services/tenancy-organizations-service/internal/models ./services/tenancy-organizations-service/internal/handlers` and `go test ./services/tenancy-organizations-service/internal/workspace -run '^$'`.
  - Docs: [Filesystem Create Folder API](https://www.palantir.com/docs/foundry/api/filesystem-v2-resources/folders/create-folder/), [Filesystem Get Folder API](https://www.palantir.com/docs/foundry/api/filesystem-v2-resources/folders/get-folder/), [Filesystem Get Resource API](https://www.palantir.com/docs/foundry/api/filesystem-v2-resources/resources/get-resource/).

- [x] `CMP.6` Move and rename (`P0`, `done`)
  - Move and rename preserve the RID and update breadcrumbs everywhere the resource is referenced.
  - Move across projects checks marking compatibility and asks for explicit confirmation when access policies change.
  - Implementation: workspace move/rename operations update folder `project_id`, `parent_folder_id`, `name`, `slug`, and descendant folder project membership while never updating project/folder `rid` columns. Breadcrumbs remain derived from the stable RID plus the current project/parent folder chain, so rename/reparent changes are reflected wherever folders are listed or resolved.
  - Cross-project move: folder subtree moves require source and target project ownership/admin access, reject cycles, update folder-scope grant references to the new project, require `confirm_access_policy_change=true`, compare source/target `marking_rids`, reject incompatible target markings, and require `confirm_marking_change=true` when compatible markings differ.
  - Documentation: `README.md`, `ARCHITECTURE.md`, `docs/reference/foundry-compatibility-glossary.md`, and `services/tenancy-organizations-service/README.md` define the RID-preserving move/rename contract.
  - Verification: `go test ./services/tenancy-organizations-service/internal/workspace ./services/tenancy-organizations-service/internal/models ./services/tenancy-organizations-service/internal/handlers`.
  - Docs: [Filesystem Get Resource API](https://www.palantir.com/docs/foundry/api/filesystem-v2-resources/resources/get-resource/), [Filesystem Get Folder API](https://www.palantir.com/docs/foundry/api/filesystem-v2-resources/folders/get-folder/).

### Federated search and catalog

- [x] `CMP.7` Search index (`P0`, `done`)
  - Per-resource index entry: RID, type, display name, owning project, organizations, markings, last modified, owner, tags, summary.
  - Index updated on resource create/update/move/trash via the event bus, no per-resource polling.
  - Implementation: `tenancy-organizations-service` owns `compass_resource_search_index`, a per-resource catalog projection for projects and folders with RID, type, display name, owning project RID/UUID, organization RIDs, marking RIDs, owner, tags, summary, open URL, trash state, and a Postgres `search_vector`.
  - Event flow: project/folder create, update, move, rename, duplicate, trash, restore, and purge paths update the index in the same transaction and emit `compass.resource.search.updated.v1` through the transactional outbox, so downstream Vespa/OpenSearch consumers can subscribe without polling resource tables.
  - Documentation: `README.md`, `ARCHITECTURE.md`, `docs/reference/foundry-compatibility-glossary.md`, and `services/tenancy-organizations-service/README.md` define the Compass search-index contract.
  - Verification: `go test ./services/tenancy-organizations-service/internal/workspace -run 'SearchIndex|CMP7|Move|Rename|BatchResponse|CMP6' -count=1`, `go test ./services/tenancy-organizations-service/internal/models ./services/tenancy-organizations-service/internal/handlers`, `go test ./libs/outbox ./libs/core-models/...`, and `git diff --check`.
  - Docs: [Quicksearch](https://www.palantir.com/docs/foundry/getting-started/quicksearch), [Data Catalog](https://www.palantir.com/docs/foundry/compass/data-catalog), [Compass overview](https://www.palantir.com/docs/foundry/compass/overview).

- [x] `CMP.8` Search API (`P0`, `done`)
  - `GET /compass/search?q=...&type=...&project=...&owner=...&marking=...` with permission-aware filtering (results never leak resources the caller cannot see).
  - Cursor pagination with bounded result count; tiebreak by last-modified.
  - Implementation: `GET /api/v1/compass/search` queries `compass_resource_search_index` with filters for `q`, `type`, `project` (UUID or project RID), `owner`, repeated `marking`, `limit`, and opaque `cursor`.
  - Permission model: the handler intersects every search with `domain.ListAccessibleProjects`; inaccessible project filters return an empty result set rather than leaking project existence.
  - Pagination: results are bounded to `limit <= 200`, ordered by text score, `last_modified_at DESC`, and `rid ASC`; the cursor encodes the previous page's score, timestamp, and RID so pagination is stable.
  - Documentation: `README.md` and `services/tenancy-organizations-service/README.md` define the API surface.
  - Verification: `go test ./services/tenancy-organizations-service/internal/workspace -run 'CompassSearch|SearchIndex|CMP7|CMP8' -count=1` and `go test ./services/tenancy-organizations-service/internal/server ./services/tenancy-organizations-service/internal/models ./services/tenancy-organizations-service/internal/handlers`.
  - Docs: [Quicksearch](https://www.palantir.com/docs/foundry/getting-started/quicksearch), [Data Catalog](https://www.palantir.com/docs/foundry/compass/data-catalog).

- [x] `CMP.9` Search UI shell (`P0`, `done`)
  - Global search box (keyboard shortcut) with type filters, project filter, marking badges, recents/favorites, and an "open with..." menu derived from the resource type registry.
  - Implementation: `apps/web/src/routes/search/SearchPage.tsx` keeps the Quicksearch-style global search shell and `Cmd/Ctrl+J` focus shortcut, combines ontology results with `GET /api/v1/compass/search`, and loads workspace recents/favorites for jump-to mode.
  - Filters and metadata: the Files tab now blends ontology file kinds with registry-backed Compass resource types, applies the Compass project and marking filters through the search API, and renders resource RID/type/open URL, tags, last-modified metadata, and marking badges.
  - Open-with registry: `apps/web/src/lib/compass/resourceTypeRegistry.ts` mirrors the Compass resource type definitions needed by the frontend for display labels, icons, supported actions, open-app URL templates, unknown-type fallback, and per-type "Open with" targets.
  - Documentation: `README.md`, `ARCHITECTURE.md`, `docs/reference/foundry-compatibility-glossary.md`, and `services/tenancy-organizations-service/README.md` define the Search UI shell contract.
  - Verification: `pnpm --filter @open-foundry/web exec eslint src/routes/search/SearchPage.tsx src/lib/api/workspace.ts src/lib/compass/resourceTypeRegistry.ts`; `pnpm --filter @open-foundry/web check` is still blocked by the pre-existing `apps/web/src/lib/api/client.test.ts` expectation that `ApiClient` is exported.
  - Docs: [Quicksearch](https://www.palantir.com/docs/foundry/getting-started/quicksearch), [Data Catalog](https://www.palantir.com/docs/foundry/compass/data-catalog).

### Navigation

- [x] `CMP.10` Breadcrumbs (`P0`, `done`)
  - Standard breadcrumb component bound to a resource's project/folder path, with click-to-open and copy-RID actions.
  - Implementation: `apps/web/src/lib/components/workspace/ProjectBreadcrumb.tsx` is now the standard resource breadcrumb component. It builds `Projects → Project → Folder...` paths from project/folder metadata, links each ancestor to its current open route, marks the current crumb with `aria-current`, and exposes per-crumb copy-RID buttons.
  - RID behavior: project crumbs copy `rid` when present or the canonical fallback `ri.compass.main.project.<uuid>`; folder crumbs copy their stable `rid` or `ri.compass.main.folder.<uuid>`. Rename/move changes therefore update labels and links while copied references remain immutable.
  - Wired surfaces: `ProjectDetailPage` renders project-root breadcrumbs and `ProjectFolderPage` renders full folder ancestry; folder headers now display the folder RID and parent folder/project RID context.
  - Documentation: `README.md`, `ARCHITECTURE.md`, and `docs/reference/foundry-compatibility-glossary.md` define the breadcrumb contract.
  - Verification: `pnpm --filter @open-foundry/web exec eslint src/lib/components/workspace/ProjectBreadcrumb.tsx src/routes/projects/ProjectDetailPage.tsx src/routes/projects/ProjectFolderPage.tsx` and `git diff --check -- apps/web/src/lib/components/workspace/ProjectBreadcrumb.tsx apps/web/src/routes/projects/ProjectDetailPage.tsx apps/web/src/routes/projects/ProjectFolderPage.tsx apps/web/src/styles/app.css`.
  - Docs: [Move and share resources](https://www.palantir.com/docs/foundry/compass/move-and-share-resources), [Use Project navigation panel](https://www.palantir.com/docs/foundry/compass/use-project-navigation-panel), [Use Project details panel](https://www.palantir.com/docs/foundry/compass/use-project-details-panel).

- [x] `CMP.11` Open-with menu (`P0`, `done`)
  - For each resource, the registry declares the apps that can open it (e.g. dataset → Dataset Preview, Pipeline Builder, Code Workbook, Quiver).
  - Open-with menu surfaces in search results, list views, and resource detail headers.
  - Implementation: `apps/web/src/lib/compass/resourceTypeRegistry.ts` now resolves open-with targets from either Compass search results or generic resource contexts (`kind`, `id`, `rid`, `project_rid`, `open_url`). Dataset resources expose Dataset Preview, Data Catalog, Pipeline Builder, Code Workbook, and Quiver targets; dashboards and workflows are also first-class registry entries instead of falling through to unknown-resource search.
  - UI component: `apps/web/src/lib/components/workspace/OpenWithMenu.tsx` is the shared menu for all resource surfaces. It expands registry URL templates with immutable RIDs when present, falls back to internal IDs for bound resources that do not yet carry a platform RID, and keeps an unknown-type fallback.
  - Wired surfaces: `/search` Compass results use the shared component; `ProjectDetailPage` renders the menu in project headers and project file-list rows; `ProjectFolderPage` renders the menu in folder headers and folder list rows; `ResourceDetailsPanel` renders the menu in resource detail headers.
  - Documentation: `README.md`, `ARCHITECTURE.md`, and `docs/reference/foundry-compatibility-glossary.md` define the open-with contract.
  - Verification: `pnpm --filter @open-foundry/web exec eslint src/lib/compass/resourceTypeRegistry.ts src/lib/components/workspace/OpenWithMenu.tsx src/lib/components/workspace/ResourceDetailsPanel.tsx src/routes/search/SearchPage.tsx src/routes/projects/ProjectDetailPage.tsx src/routes/projects/ProjectFolderPage.tsx`, `pnpm --filter @open-foundry/web check`, and `git diff --check -- apps/web/src/lib/compass/resourceTypeRegistry.ts apps/web/src/lib/components/workspace/OpenWithMenu.tsx apps/web/src/lib/components/workspace/ResourceDetailsPanel.tsx apps/web/src/routes/search/SearchPage.tsx apps/web/src/routes/projects/ProjectDetailPage.tsx apps/web/src/routes/projects/ProjectFolderPage.tsx apps/web/src/styles/app.css docs/migration/foundry-compass-1to1-checklist.md README.md ARCHITECTURE.md docs/reference/foundry-compatibility-glossary.md`.
  - Docs: [Quicksearch](https://www.palantir.com/docs/foundry/compass/quicksearch), [Use Project navigation panel](https://www.palantir.com/docs/foundry/compass/use-project-navigation-panel), [Use Project details panel](https://www.palantir.com/docs/foundry/compass/use-project-details-panel), [Data Catalog](https://www.palantir.com/docs/foundry/compass/data-catalog). The older [Cross-app navigation](https://www.palantir.com/docs/foundry/compass/navigation) URL currently redirects to Palantir's 404 page.

### Trash and restore

- [x] `CMP.12` Trash workflow (`P0`, `done`)
  - Trash a resource (instead of hard-delete) with a configurable retention window (default 30 days).
  - Restore returns the resource to its original path; if the path is gone, restore goes to the project root with a banner.
  - Implementation: `services/tenancy-organizations-service/internal/repo/migrations/0015_cmp12_trash_workflow.sql` adds `trash_retention_days`, `purge_after`, and original-placement columns to project, folder, and resource-binding trash rows. `DELETE /api/v1/ontology/projects/{id}` now soft-deletes projects instead of hard-deleting them.
  - API behavior: `DELETE /api/v1/workspace/resources/{kind}/{id}` accepts optional `retention_days` (default `30`, bounded `1..3650`), stores the trash retention window, and updates the Compass search index with `compass.resource.trashed.v1`. `GET /api/v1/workspace/trash` returns retention metadata, `purge_after`, original placement IDs, and a restore-target status.
  - Restore placement: `POST /api/v1/workspace/resources/{kind}/{id}/restore` returns `{ restored, restored_to_original_path, restored_to_project_id, restored_to_folder_id?, banner? }`. Folders restore to their original parent when it still exists and is not trashed; if the parent path is gone, they restore to the project root and the UI displays the returned banner.
  - UI: project and project-folder Trash surfaces show retention/purge-after metadata, root-restore warnings, and restore banners from the API.
  - Documentation: `README.md`, `ARCHITECTURE.md`, `docs/reference/foundry-compatibility-glossary.md`, and `services/tenancy-organizations-service/README.md` define the Trash workflow contract.
  - Verification: `go test ./services/tenancy-organizations-service/internal/workspace -run 'Trash|SoftDelete|Batch|Kind' -count=1`, `go test ./services/tenancy-organizations-service/internal/handlers -run 'Project|Folder|CMP|SG6' -count=1`, `pnpm --filter @open-foundry/web exec eslint src/lib/api/workspace.ts src/routes/projects/ProjectDetailPage.tsx src/routes/projects/ProjectFolderPage.tsx src/routes/projects/ProjectsListPage.tsx`, and `pnpm --filter @open-foundry/web check`.
  - Docs: [Use Project navigation panel](https://www.palantir.com/docs/foundry/compass/use-project-navigation-panel). The older [Trash and restore](https://www.palantir.com/docs/foundry/compass/trash) URL currently redirects to Palantir's 404 page.

- [x] `CMP.13` Hard delete with audit (`P0`, `done`)
  - Hard delete after retention or by admin action emits a marking-aware audit event listing dependents that were affected.
  - Implementation: `PurgeTrashed` now locks the trashed row, captures a pre-delete snapshot, allows permanent delete only after `purge_after` unless the caller has the `admin` role, cleans directly affected workspace surface rows, folder-scope grants, and search-index rows in the same transaction, and emits `compass.resource.purged` to `audit.events.v1`.
  - Audit payload: `libs/audit-trail` defines the Compass purge event with `resource_rid`, `project_rid`, `markings_at_event`, `resource_type`, `deleted_at`, `deleted_by`, `purged_by`, `retention_days`, `purge_after`, `purge_mode` (`retention_expired` or `admin_override`), and `affected_dependents`.
  - Documentation: `README.md`, `ARCHITECTURE.md`, `docs/reference/foundry-compatibility-glossary.md`, and `services/tenancy-organizations-service/README.md` define the hard-delete audit contract.
  - Verification: `go test ./libs/audit-trail`, `go test ./services/tenancy-organizations-service/internal/workspace -run 'Trash|Purge|Audit|Kind' -count=1`.
  - Docs: [Use Project navigation panel](https://www.palantir.com/docs/foundry/compass/use-project-navigation-panel). The older [Trash and restore](https://www.palantir.com/docs/foundry/compass/trash) URL currently redirects to Palantir's 404 page.

## Milestone B: links, favorites, propagation, audit, bulk

### Cross-resource links

- [x] `CMP.14` Reverse-reference graph (`P1`, `done`)
  - For each resource, the registry tracks which other resources depend on it (e.g. a dashboard depends on a query depends on a dataset).
  - Surface "used by" in resource detail; warn on trash/move operations.
  - Implementation: `libs/core-models/resource` now declares `ReferenceTargets` per resource type and validates that every target type is registered. `FOUNDRY_QUERY`, `WORKSHOP_DASHBOARD`, and `FOUNDRY_WORKFLOW` are first-class registry entries so dashboard → query → dataset and workflow/pipeline dependencies can be represented instead of falling through to unknown types.
  - Graph store/API: `services/tenancy-organizations-service/internal/repo/migrations/0016_cmp14_resource_references.sql` adds `compass_resource_references`, a directed edge table where `source` depends on `target`. `GET /api/v1/workspace/resources/{kind}/{id}/references` returns `{ depends_on, used_by }`; `PUT /api/v1/workspace/resources/{kind}/{id}/references` lets owning services/admins replace explicit upstream edges with self-edge and batch-size validation. The read path also derives edges from project resource bindings and `ontology_projects.references`.
  - UI: `ResourceDetailsPanel` renders "Used by" and "Depends on" sections; `MoveDialog`, `ProjectDetailPage`, and `ProjectFolderPage` preflight the graph and warn when move/trash/purge actions may affect upstream or downstream resources.
  - Documentation: `README.md`, `ARCHITECTURE.md`, `docs/reference/foundry-compatibility-glossary.md`, and `services/tenancy-organizations-service/README.md` define the reverse-reference graph contract.
  - Verification: `go test ./libs/core-models/resource`, `go test ./services/tenancy-organizations-service/internal/server ./services/tenancy-organizations-service/internal/workspace ./libs/core-models/resource`, and `pnpm --filter @open-foundry/web check`.
  - Docs: [Projects and resources](https://www.palantir.com/docs/foundry/getting-started/projects-and-resources/), [Compass overview](https://www.palantir.com/docs/foundry/compass/overview), [Use Project navigation panel](https://www.palantir.com/docs/foundry/compass/use-project-navigation-panel), [Projects & roles](https://www.palantir.com/docs/foundry/security/projects-and-roles). The older [Resources, RIDs, and projects](https://www.palantir.com/docs/foundry/compass/resources) URL currently redirects to Palantir's 404 page.

- [x] `CMP.15` Stable resource URLs (`P1`, `done`)
  - Every app's resource URL contains the RID and not a path slug; renames never invalidate links.
  - Optional human-readable slugs allowed only as visual sugar in the URL.
  - Implementation: `apps/web/src/lib/compass/stableResourceUrls.ts` centralizes RID-based route construction. Project URLs are `/projects/{projectRid}[--slug]`; folder URLs are `/projects/{projectRid}[--slug]/folders/{folderRid}[--slug]`. Route parsing strips optional `--slug` suffixes before API calls, so renames only change visual sugar and never invalidate links.
  - Routing: `apps/web/src/router.tsx` accepts canonical folder routes and keeps the legacy `/projects/{project}/{folder}` route working. `ProjectDetailPage` and `ProjectFolderPage` normalize RID/UUID route params before loading resources, while generated links in Home, Projects, Search, Recents, and breadcrumbs now use stable RID paths.
  - Search/open-with: the frontend resource type registry uses RID templates for app open URLs and folder open-with targets now use `/projects/{project_rid}/folders/{rid}`. `0017_cmp15_stable_resource_urls.sql` rewrites existing Compass search-index `open_url` values to RID-based project/folder routes.
  - Documentation: `README.md`, `ARCHITECTURE.md`, `docs/reference/foundry-compatibility-glossary.md`, and `services/tenancy-organizations-service/README.md` define the stable resource URL contract.
  - Verification: `pnpm --filter @open-foundry/web exec vitest run src/lib/compass/stableResourceUrls.test.ts`, `pnpm --filter @open-foundry/web exec eslint src/lib/compass/stableResourceUrls.ts src/lib/compass/stableResourceUrls.test.ts src/router.tsx src/lib/components/workspace/ProjectBreadcrumb.tsx src/lib/compass/resourceTypeRegistry.ts src/routes/projects/ProjectsListPage.tsx src/routes/projects/ProjectDetailPage.tsx src/routes/projects/ProjectFolderPage.tsx src/routes/recent/RecentPage.tsx src/routes/search/SearchPage.tsx src/routes/Home.tsx src/routes/ontology-manager/OntologyManagerPage.tsx`, and `go test ./services/tenancy-organizations-service/internal/workspace ./libs/core-models/resource`. `pnpm --filter @open-foundry/web check` was attempted twice but hung with no TypeScript output and was terminated to avoid leaving background compiler processes.
  - Docs: [Projects and resources](https://www.palantir.com/docs/foundry/getting-started/projects-and-resources/), [Filesystem Get Resource API](https://www.palantir.com/docs/foundry/api/filesystem-v2-resources/resources/get-resource/), [Use Project navigation panel](https://www.palantir.com/docs/foundry/compass/use-project-navigation-panel), [Move and share resources](https://www.palantir.com/docs/foundry/compass/move-and-share-resources/). The older [Cross-app navigation](https://www.palantir.com/docs/foundry/compass/navigation) URL currently redirects to Palantir's 404 page.

### Favorites and recents

- [x] `CMP.16` Favorites (`P1`, `done`)
  - Per-user favorites list with reorderable display and groups (e.g. "My ontologies", "Daily ops").
  - Synced across devices via the user profile store.
  - Persistence/API: `0018_cmp16_favorites_profile.sql` extends `user_favorites` with `group_id`, `display_order`, and `updated_at`, adds `user_favorite_groups`, and keeps favorites keyed by `(user_id, resource_kind, resource_id)`. `/api/v1/workspace/favorites` remains backward-compatible with `{data:[...]}` while adding `groups`; new endpoints create/list groups and persist favorite/group ordering.
  - UI: `/favorites` renders profile-synced resource shortcuts grouped by user-defined sections, supports group creation, move-to-group, reorder within groups, group reorder, and removal. The sidebar exposes Favorites, Quicksearch still consumes ordered favorites in jump-to mode, and Compass search rows can add resources with the star action.
  - Documentation: `README.md`, `ARCHITECTURE.md`, `docs/reference/foundry-compatibility-glossary.md`, and `services/tenancy-organizations-service/README.md` define the favorites profile contract.
  - Verification: `go test ./services/tenancy-organizations-service/internal/workspace -run 'Favorite|Favorites' -count=1`, `pnpm --filter @open-foundry/web exec eslint src/lib/api/workspace.ts src/routes/favorites/FavoritesPage.tsx src/routes/search/SearchPage.tsx src/router.tsx src/lib/components/Sidebar.tsx src/lib/components/ui/Glyph.tsx`, and `git diff --check`.
  - Docs: [Orientation and navigation](https://www.palantir.com/docs/foundry/getting-started/orientation-and-nav/), [Compass overview](https://www.palantir.com/docs/foundry/compass/overview), [Projects and resources](https://www.palantir.com/docs/foundry/getting-started/projects-and-resources/). The older [Cross-app navigation](https://www.palantir.com/docs/foundry/compass/navigation) URL currently redirects to Palantir's 404 page.

- [x] `CMP.17` Recents (`P1`, `done`)
  - Per-user recents list capped at N items (default 50), ordered by last-opened.
  - Recents respect permission revocations (a recent that became forbidden disappears).
  - Backend: `resource_access_log` remains the per-user last-opened event stream written by best-effort `POST /api/v1/workspace/recents`. `GET /api/v1/workspace/recents` now evaluates `domain.ListAccessibleProjects`, deduplicates by `(resource_kind, resource_id)`, filters projects/folders/resource bindings/project-bound resources/search-indexed resources through current project visibility and trash state, sorts by `last_accessed_at DESC`, and applies `limit` after visibility filtering (`50` default, `500` maximum).
  - UI: `/recent` renders the permission-filtered list with stable RID-based links for every supported resource kind, resolves labels when available, and Quicksearch jump-to continues to consume the same server-filtered recents API.
  - Documentation: `README.md`, `ARCHITECTURE.md`, `docs/reference/foundry-compatibility-glossary.md`, and `services/tenancy-organizations-service/README.md` define the recents profile contract.
  - Verification: `go test ./services/tenancy-organizations-service/internal/workspace -run 'Recent|Recents|ResourceRID' -count=1`, `pnpm --filter @open-foundry/web exec eslint src/lib/api/workspace.ts src/routes/recent/RecentPage.tsx src/routes/search/SearchPage.tsx`, and `git diff --check`.
  - Docs: [Orientation and navigation](https://www.palantir.com/docs/foundry/getting-started/orientation-and-nav/), [Quicksearch](https://www.palantir.com/docs/foundry/getting-started/quicksearch), [Projects and resources](https://www.palantir.com/docs/foundry/getting-started/projects-and-resources/). The older [Cross-app navigation](https://www.palantir.com/docs/foundry/compass/navigation) URL currently redirects to Palantir's 404 page.

### Propagated view requirements

- [x] `CMP.18` Propagate view requirements toggle (`P1`, `done`)
  - A project (or folder) can opt into "propagate view requirements", which copies its required markings/clearances down to all child resources on create.
  - Documented as deprecation-eligible; clear migration notes in the admin UI.
  - Backend: migration `0019_cmp18_propagate_view_requirements.sql` adds the legacy project/folder toggle, disabled timestamp, and `view_requirement_marking_rids` snapshots for folders and project resource bindings. New folders inherit the nearest enabled folder requirement or the enabled project markings; resource bindings copy project markings on create. Existing descendants are intentionally left for `CMP.19`.
  - API/UI: `PATCH /api/v1/projects/{id}/folders/{folder_id}/propagate-view-requirements` manages folder-level compatibility settings, while `PATCH /api/v1/projects/{id}` manages project-level settings. The Control Panel Projects page surfaces the legacy toggle with migration notes and blocks re-enable after disable.
  - Compatibility rule: new projects default the setting off, disabled settings stamp `propagate_view_requirements_disabled_at`, and re-enabling after disable returns `409` with guidance to migrate to Markings.
  - Documentation: `README.md`, `ARCHITECTURE.md`, `docs/reference/foundry-compatibility-glossary.md`, and `services/tenancy-organizations-service/README.md` define the legacy propagation contract.
  - Verification: `go test ./services/tenancy-organizations-service/internal/models ./services/tenancy-organizations-service/internal/handlers ./services/tenancy-organizations-service/internal/server ./services/tenancy-organizations-service/internal/workspace -run 'CMP18|Propagate|OntologyProject|Folder|SearchIndex' -count=1`, `pnpm --filter @open-foundry/web exec eslint src/lib/api/ontology.ts src/lib/api/tenancy.ts src/routes/control-panel/ProjectsPage.tsx`, and `git diff --check`.
  - Docs: [Migrate from and disable "Propagate view requirements" setting](https://www.palantir.com/docs/foundry/platform-security-management/disabling-propagate-view-requirements/), [Guidance on removing markings](https://www.palantir.com/docs/foundry/building-pipelines/remove-markings/). The older [Propagate view requirements](https://www.palantir.com/docs/foundry/compass/propagation) URL currently redirects to Palantir's 404 page.

- [x] `CMP.19` Inheritance audit and re-propagation (`P1`, `done`)
  - On policy change at a parent, propagate to descendants in the background with progress reporting; emit audit events.
  - Backend: migration `0020_cmp19_view_requirement_propagation_jobs.sql` adds `compass_view_requirement_propagation_jobs` with `pending/running/succeeded/failed`, parent resource RID, target/previous marking snapshots, processed/changed folder and resource counts, timestamps, and error reporting.
  - Re-propagation: project and folder propagation policy changes enqueue a background job after the parent update commits. Jobs update descendant folder `view_requirement_marking_rids`, refresh folder search-index entries, update project resource-binding snapshots for project-level jobs, and keep progress counters visible while they run.
  - Audit: completed jobs emit marking-aware `compass.view_requirements.propagated` events through `audit.events.v1`, including the parent RID, job id, target/previous markings, changed counts, and a capped `affected_dependents` list.
  - API/UI: `GET /api/v1/projects/{id}/propagate-view-requirements/jobs` and `GET /api/v1/projects/{id}/propagate-view-requirements/jobs/{job_id}` expose progress. The Control Panel Projects page lists recent propagation jobs below the legacy toggle.
  - Documentation: `README.md`, `ARCHITECTURE.md`, `docs/reference/foundry-compatibility-glossary.md`, and `services/tenancy-organizations-service/README.md` define the background propagation/audit contract.
  - Verification: `go test ./libs/audit-trail ./services/tenancy-organizations-service/internal/models ./services/tenancy-organizations-service/internal/handlers ./services/tenancy-organizations-service/internal/server ./services/tenancy-organizations-service/internal/workspace -run 'CMP18|CMP19|Propagate|Propagation|OntologyProject|Folder|CompassView|SearchIndex' -count=1`, `pnpm --filter @open-foundry/web exec eslint src/lib/api/tenancy.ts src/routes/control-panel/ProjectsPage.tsx`, and `git diff --check`.
  - Docs: [Migrate from and disable "Propagate view requirements" setting](https://www.palantir.com/docs/foundry/platform-security-management/disabling-propagate-view-requirements/), [Guidance on removing markings](https://www.palantir.com/docs/foundry/building-pipelines/remove-markings/). The older [Propagate view requirements](https://www.palantir.com/docs/foundry/compass/propagation) URL currently redirects to Palantir's 404 page.

### Audit and bulk operations

- [ ] `CMP.20` Resource audit (`P1`, `todo`)
  - Standard audit events for create, move, rename, trash, restore, hard-delete, sharing change, marking change.
  - Audit consumable from the central audit query surface.
  - Docs: [Audit on resources](https://www.palantir.com/docs/foundry/compass/audit).

- [ ] `CMP.21` Bulk move/trash/share (`P1`, `todo`)
  - Bulk operations from search results or folder listings with pre-flight policy checks and a single audit event per batch.
  - Docs: [Filesystem Get Resource API](https://www.palantir.com/docs/foundry/api/filesystem-v2-resources/resources/get-resource/), [Filesystem Get Folder API](https://www.palantir.com/docs/foundry/api/filesystem-v2-resources/folders/get-folder/).

## Milestone C: catalog, recommendations, dependency graph, multi-region

### Full-text catalog

- [ ] `CMP.22` Long-text catalog index (`P2`, `todo`)
  - Index resource descriptions, README content, ontology object/property descriptions, code repo READMEs, dashboard descriptions; surface in search with snippet highlighting.
  - Docs: [Quicksearch](https://www.palantir.com/docs/foundry/getting-started/quicksearch), [Data Catalog](https://www.palantir.com/docs/foundry/compass/data-catalog).

- [ ] `CMP.23` Facets and saved searches (`P2`, `todo`)
  - Facets on type, project, owner, marking, last-modified bucket.
  - Save a search as a named query that appears in the user's sidebar.
  - Docs: [Quicksearch](https://www.palantir.com/docs/foundry/getting-started/quicksearch), [Data Catalog](https://www.palantir.com/docs/foundry/compass/data-catalog).

### Recommendations and dependency graph

- [ ] `CMP.24` Resource recommendations (`P2`, `todo`)
  - Per-user "you might want to open" recommendations based on collaborator activity, recent opens, and explicit follows on a project.
  - Privacy-respecting: no surfacing of resources the caller cannot see.
  - Docs: [Quicksearch](https://www.palantir.com/docs/foundry/getting-started/quicksearch), [Data Catalog](https://www.palantir.com/docs/foundry/compass/data-catalog).

- [ ] `CMP.25` Dependency graph view (`P2`, `todo`)
  - Interactive graph showing direct and transitive dependencies of a resource, with type filters and click-to-open.
  - Re-uses the reverse-reference graph.
  - Docs: [Resources, RIDs, and projects](https://www.palantir.com/docs/foundry/compass/resources).

### Multi-region

- [ ] `CMP.26` Cross-region resource addressing (`P2`, `todo`)
  - RIDs are globally unique; a resource in another region is addressable but access requires explicit cross-region grant (Apollo region policies apply).
  - Search optionally federates across regions per admin policy.
  - Docs: [Resources, RIDs, and projects](https://www.palantir.com/docs/foundry/compass/resources).

- [ ] `CMP.27` Deep trash and admin recovery (`P2`, `todo`)
  - After standard retention, admins can recover resources from a deep-trash archive for a configurable window (e.g. 90 days) with full audit.
  - Docs: [Trash and restore](https://www.palantir.com/docs/foundry/compass/trash).

## Implementation inventory to collect before coding

- [ ] `INV.1` Identify every existing service that mints RIDs today and verify they follow a uniform scheme.
- [ ] `INV.2` Identify the current event-bus topics that resource changes flow on (overlap with `libs/event-bus-*`).
- [ ] `INV.3` Identify the existing search backend (OpenSearch/Vespa) and confirm permission-aware filtering is available.
- [ ] `INV.4` Identify the existing project/folder hierarchy if any; if absent, design it from scratch.
- [ ] `INV.5` Identify which apps currently use path-based URLs vs RID-based URLs and plan a migration.
- [ ] `INV.6` Produce a parity matrix sibling JSON entry once a first implementation inventory is in place.

## Suggested service boundaries

| Surface | Responsibilities |
| --- | --- |
| `compass-service` | Resource registry, RID minting/validation, project/folder CRUD, move/rename/trash/restore, sharing, audit emission. |
| `compass-search-service` | Indexer (subscribed to resource events), search API with permission-aware filtering, facets, saved searches. |
| `compass-graph-service` | Reverse-reference graph, dependency view, "used by" answers for the UI. |
| `event-bus-control` | Resource lifecycle events that the search and graph services subscribe to. |
| `apps/web` | Global search bar, projects/folders shell, resource detail headers, breadcrumbs, favorites/recents sidebar, trash UI. |
