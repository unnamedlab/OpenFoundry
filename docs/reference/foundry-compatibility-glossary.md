# Foundry compatibility glossary

Date: 2026-05-10  
Status: frozen compatibility contract for Workstream `W0.2`

This glossary freezes the names OpenFoundry should use when building
Foundry-style Pipeline Builder, Workshop, Ontology, Functions, Actions, and
Data Connection capabilities. The goal is **name compatibility**, not private
implementation compatibility. New APIs should prefer the canonical
OpenFoundry names below while accepting listed aliases where older clients or
Foundry-shaped payloads already use them.

Official public documentation anchors:

- [Workshop variables](https://www.palantir.com/docs/foundry/workshop/concepts-variables/)
- [Pipeline Builder overview](https://www.palantir.com/docs/foundry/pipeline-builder/overview)
- [Ontology overview](https://www.palantir.com/docs/foundry/ontology/overview/)
- [Pipeline Builder outputs](https://www.palantir.com/docs/foundry/pipeline-builder/outputs-overview/)
- [Action types overview](https://www.palantir.com/docs/foundry/action-types/overview)
- [Functions in Workshop](https://www.palantir.com/docs/foundry/workshop/functions-use/)

## Naming rules

| Rule | Contract |
| --- | --- |
| Internal identity | Use `id` for internal UUIDs or local child IDs. Examples: `app.id`, `page.id`, `widget.id`, `pipeline.id`, `action_type.id`. |
| External stable identity | Use `rid` for stable resource identifiers intended to cross service, API, branch, or external-tool boundaries. Examples: `dataset_rid`, `pipeline_rid`, `build.rid`, `job_spec_rid`, `transaction_rid`. |
| Typed references | Use `*_id` when the referenced value is an internal UUID or local child ID. Use `*_rid` when it is a stable resource identifier. |
| API names | Use `name` for stable machine/API names when a resource has no separate `api_name`. Use `display_name`, `title`, or `label` for user-facing text. |
| Slugs and paths | Use `slug` for public app runtime URLs. Use `path` for page routes inside an app. |
| Resource kind fields | Use `*_type` for classification strings such as `widget_type`, `transform_type`, `source_type`, `property_type`. Use `*_kind` for behavior enums such as `operation_kind`, `trigger_kind`, `join_kind`. |
| Config payloads | Use `config` for typed behavior config, `props` for widget display/config props, and `settings` for app-level or resource-level preferences. |
| Definition vs execution | Use `type`, `definition`, or `package` for reusable definitions. Use `run`, `execution`, or `build` for one invocation. |
| Compatibility aliases | Accept aliases only at API boundaries. Persist one canonical shape internally. |
| Palantir-style names | Keep familiar Foundry names where they describe user-facing behavior, but do not require Palantir's private ID formats or internal product architecture. |

## Canonical term matrix

| Concept | Canonical OpenFoundry term | Canonical fields | Accepted or reserved aliases | Owning surface | Notes |
| --- | --- | --- | --- | --- | --- |
| Resource RID | `rid` | `rid`, `resource_rid`, `dataset_rid`, `pipeline_rid`, `build.rid`, `branch_rid`, `transaction_rid` | `resource_id` should be avoided for stable external IDs; use `id` or `rid` explicitly. | Cross-platform | Platform-minted resource RIDs use `ri.<service>.<instance>.<type>.<uuid>` and are immutable across rename/move. `id` remains valid for internal UUIDs. New code must use `libs/core-models/rid` for parsing and validation; do not invent Palantir RID prefixes unless the owning resource already has a documented OpenFoundry prefix. |
| Folder resource | `folder` | `id`, `rid`, `project_id`, `project_rid`, `parent_folder_id`, `parent_folder_rid`, `space_rid`, `type`, `trash_status`, `inherits_project_policies`, `policy_overrides_allowed` | `ontology_folder` and `ontology_project_folder` remain legacy workspace aliases. | `tenancy-organizations-service` | Folders are Compass containers below a project or another folder. The RID is immutable across rename/move, and effective access inherits the project role lattice unless folder-scope direct grants override it. |
| Resource move / rename | `move_resource`, `rename_resource` | `resource_kind`, `resource_id`, `target_project_id`, `target_project_rid`, `target_folder_id`, `target_folder_rid`, `confirm_access_policy_change`, `confirm_marking_change`, `name` | `reparent` is an implementation detail for folder moves. | `tenancy-organizations-service/internal/workspace` | Move and rename mutate location/name metadata only. They must preserve `rid`, update derived breadcrumbs through the parent chain, block folder cycles, and require explicit confirmation for cross-project policy or marking changes. |
| Resource search entry | `resource_search_entry` | `rid`, `type`, `display_name`, `owning_project_id`, `owning_project_rid`, `organization_rids`, `marking_rids`, `last_modified_at`, `owner_id`, `tags`, `summary`, `long_text`, `long_text_sources`, `open_url`, `is_deleted` | `catalog document`, `search document`; avoid `resource_id` for index identity. | `tenancy-organizations-service/internal/workspace` | Search entries are lifecycle projections keyed by immutable RID. Mutating resource handlers must update `compass_resource_search_index` and emit `compass.resource.search.updated.v1` through the outbox instead of relying on per-resource polling. `long_text_sources` records description, README, ontology object/property, code repository README, and dashboard-description contributors. |
| Compass search API | `compass_search` | `q`, `type`, `project`, `owner`, `marking`, `modified`, `limit`, `cursor`, `next_cursor`, `data`, `snippet`, `facets` | `quicksearch`, `catalog_search`; keep `/ontology/search` for ontology-specific object/type search. | `tenancy-organizations-service/internal/workspace` | `GET /api/v1/compass/search` must filter by project visibility before returning results. Cursor tokens are opaque and page over text score, `last_modified_at`, and RID. Results include bounded snippets for long-text matches, hide the full `long_text` body, and return facets for type, project, owner, marking, and last-modified buckets (`24h`, `7d`, `30d`, `older`). |
| Compass search UI shell | `quicksearch_shell` | `q`, `tab`, `type`, `project`, `marking`, `modified`, `favorites`, `recents`, `saved_searches`, `open_with`, `snippet`, `facets` | `global search`, `jump-to`, `full results view`. | `apps/web/src/routes/search` | The UI combines ontology search with `compass_search`, keeps the keyboard-focused global search shell, renders resource marking badges, highlighted snippets, saved searches, and facet filters, and derives row icons/open-with targets from the frontend resource type registry. |
| Compass saved search | `compass_saved_search` | `id`, `user_id`, `name`, `query`, `tab`, `type`, `project_id`, `project_rid`, `owner_id`, `marking_rids`, `modified_bucket`, `display_order` | `saved query`, `pinned search`, `search shortcut`. | `tenancy-organizations-service/internal/workspace`, `apps/web/src/routes/search` | Saved searches are per-user profile entries synced through `/api/v1/workspace/saved-searches`. They preserve the Quicksearch tab and Compass resource filters so a named search can be reopened from the user's sidebar. |
| Resource open-with menu | `resource_open_with` | `resource_kind`, `resource_id`, `rid`, `project_id`, `project_rid`, `open_url`, `target_id`, `url_template` | `open with`, `open in`, `launcher`. | `apps/web/src/lib/components/workspace` | The menu is registry-backed and shared by search rows, project/folder list views, and resource detail headers. Targets prefer immutable RIDs, fall back to internal IDs for local bindings, and include an unknown-resource search fallback. |
| Resource breadcrumb | `resource_breadcrumb` | `project_rid`, `folder_rid`, `rid`, `href`, `copy_rid` | `path breadcrumb`, `project path`, `folder path`. | `apps/web/src/lib/components/workspace` | Breadcrumbs are derived from the current project/folder chain, link ancestors to their current open location, and copy immutable RIDs rather than mutable names or slugs. |
| Resource trash entry | `resource_trash_entry` | `resource_kind`, `resource_id`, `project_id`, `display_name`, `deleted_at`, `deleted_by`, `retention_days`, `purge_after`, `original_project_id`, `original_parent_folder_id`, `restore_target_status` | `trash row`, `soft delete`, `deleted resource`. | `tenancy-organizations-service/internal/workspace` | Trash entries preserve the resource RID/ID and original placement metadata. Restore returns to the original parent when available; if a folder's path disappeared, it restores to the project root and returns a banner for the UI. |
| Resource audit event | `resource_audit` | `kind`, `resource_rid`, `project_rid`, `markings_at_event`, `resource_type`, `display_name`, `previous_display_name`, `new_display_name`, `previous_project_rid`, `new_project_rid`, `previous_parent_rid`, `new_parent_rid`, `share_id`, `share_change_type`, `previous_markings` | `resource lifecycle audit`, `Compass audit`, `resource event`. | `libs/audit-trail`, `tenancy-organizations-service/internal/workspace`, `audit-sink` | Standard Compass resource lifecycle events use `compass.resource.created`, `moved`, `renamed`, `trashed`, `restored`, `purged`, `share_changed`, and `markings_changed`. Producers emit through `audit.events.v1` in the resource transaction; `audit-sink` materializes them for central query/export by actor, resource RID, action, and time. |
| Resource bulk operation | `resource_bulk_operation` | `batch_id`, `preflight_failed`, `batch_operation`, `batch_total`, `batch_succeeded`, `batch_failed`, `batch_actions`, `op`, `target_project_rid`, `target_folder_rid`, `retention_days`, `share_principal_kind`, `share_access_level` | `bulk move`, `bulk trash`, `bulk share`, `batch resource operation`. | `libs/audit-trail`, `tenancy-organizations-service/internal/workspace` | Batch move/trash/share requests must preflight every selected row before mutation and abort the whole batch on any policy, retention, marking, or confirmation failure. Successful and failed/preflight batches emit exactly one `compass.resource.bulk_operation` event with per-row outcomes instead of one lifecycle audit event per resource. |
| Resource purge audit event | `resource_purge_audit` | `kind`, `resource_rid`, `project_rid`, `markings_at_event`, `resource_type`, `display_name`, `deleted_at`, `deleted_by`, `purged_by`, `retention_days`, `purge_after`, `purge_mode`, `affected_dependents` | `hard delete audit`, `permanent delete audit`, `purge audit`. | `libs/audit-trail`, `tenancy-organizations-service/internal/workspace` | Permanent deletes emit `compass.resource.purged` to `audit.events.v1`. The event is marking-aware, differentiates `retention_expired` from `admin_override`, and lists directly affected child folders, bindings, grants, favorites, recents, shares, and search-index rows. |
| Resource reference graph | `resource_reference_graph` | `resource_kind`, `resource_id`, `resource_rid`, `depends_on`, `used_by`, `relationship`, `source`, `target`, `derived` | `reverse references`, `used by`, `dependencies`, `lineage edge`. | `libs/core-models/resource`, `tenancy-organizations-service/internal/workspace`, `apps/web/src/lib/components/workspace` | The registry declares legal reference target types. `compass_resource_references` stores explicit directed edges where `source` depends on `target`; workspace reads also derive project binding and project-reference edges. UI surfaces show `used_by` in resource details and warn before move/trash/purge actions. |
| Stable resource URL | `stable_resource_url` | `rid`, `resource_rid`, `project_rid`, `folder_rid`, `open_url`, `slug`, `href` | `deep link`, `open URL`, `resource URL`. | `apps/web`, `tenancy-organizations-service/internal/workspace` | Canonical links identify resources by RID, for example `/projects/{projectRid}[--slug]` and `/projects/{projectRid}[--slug]/folders/{folderRid}[--slug]`. Slugs are visual only; route parsing strips them before API calls, while search-index `open_url` and open-with targets prefer RID paths. |
| Resource favorite | `resource_favorite` | `user_id`, `resource_kind`, `resource_id`, `group_id`, `display_order`, `created_at`, `updated_at` | `starred resource`, `favorite shortcut`, `pinned file`. | `tenancy-organizations-service/internal/workspace`, `apps/web/src/routes/favorites` | Favorites are per-user profile shortcuts synced through the workspace API. `user_favorite_groups` gives each user named groups such as "My ontologies" or "Daily ops"; ordering is persisted server-side so the sidebar, `/favorites`, and Quicksearch jump-to mode render consistently across devices. |
| Resource recent | `resource_recent` | `user_id`, `resource_kind`, `resource_id`, `last_accessed_at`, `limit`, `data` | `recent file`, `recent resource`, `last opened`. | `tenancy-organizations-service/internal/workspace`, `apps/web/src/routes/recent` | Recents are per-user last-opened shortcuts derived from `resource_access_log`. `GET /api/v1/workspace/recents` deduplicates by resource, defaults to 50 rows, orders by `last_accessed_at DESC`, and filters against current project visibility so permission revocations remove entries before the UI or Quicksearch sees them. |
| Resource recommendation | `resource_recommendation` | `resource_rid`, `type`, `display_name`, `score`, `reason`, `signals`, `collaborator_count`, `last_activity_at` | `recommended resource`, `you might want to open`, `personalized recommendation`. | `tenancy-organizations-service/internal/workspace`, `apps/web/src/routes/search` | Recommendations are per-user and privacy-filtered. `GET /api/v1/workspace/recommendations` builds candidates only from projects the caller can currently access, then scores collaborator opens, the caller's recent opens, and explicit project follows. |
| Project follow | `project_follow` | `user_id`, `project_id`, `project_rid`, `created_at` | `followed project`, `watch project`, `project subscription`. | `tenancy-organizations-service/internal/workspace` | `compass_project_follows` stores explicit project follows used as a recommendation signal. `GET|POST|DELETE /api/v1/workspace/project-follows` only lists/follows projects visible to the caller. |
| Propagate view requirements | `propagate_view_requirements` | `propagate_view_requirements_enabled`, `propagate_view_requirements_disabled_at`, `view_requirement_marking_rids`, `compass_view_requirement_propagation_jobs`, `compass.view_requirements.propagated` | `PVR`, `view requirement propagation`, `legacy downstream view requirements`. | `tenancy-organizations-service/internal/handlers`, `apps/web/src/routes/control-panel/ProjectsPage.tsx`, `libs/audit-trail` | This is a planned-deprecated Foundry compatibility setting. New OpenFoundry projects default it off; projects/folders can temporarily enable it to copy inherited view-requirement marking RIDs onto child folders and project resource bindings. Parent policy changes enqueue background jobs with progress counters and emit marking-aware audit events when existing descendants are re-propagated. Disabling stamps `disabled_at` and blocks re-enable; operators should migrate sensitive data to Markings. |
| App | `app` | `id`, `name`, `slug`, `description`, `status`, `pages`, `theme`, `settings`, `template_key`, `published_version_id` | `workshop_app`, `application`, `slate_app` only when importing/exporting Slate packages. | `application-composition-service`, `apps/web` | In OpenFoundry, an `app` is the top-level authored and published operational application. |
| Module | `module` | `module_id`, `module_name`, `module_interface`, `module_variables` | `workshop_module`; current single-module apps may store module-like state under `app.settings`. | `application-composition-service`, `apps/web` | Reserve as a future first-class sub-app unit. Do not overload `page` or `widget` to mean module. |
| Page | `page` | `id`, `name`, `path`, `description`, `layout`, `widgets`, `visible` | `route`, `view` only in UI copy, not wire models. | `application-composition-service`, `apps/web` | A page belongs to an app or module and owns the widgets visible at one route. |
| Widget | `widget` | `id`, `widget_type`, `title`, `description`, `position`, `props`, `binding`, `events`, `children` | `component`, `control`; keep as aliases in docs/UI only. | `application-composition-service`, `apps/web` | `widget_type` is the registry key. Nested widgets belong in `children`, not separate page-level IDs. |
| Variable | `variable` | `variable_id`, `name`, `variable_type`, `definition_type`, `external_id`, `dependencies`, `value`, `settings` | `app_variable`, `module_variable`, `workshop_variable`; existing `object_set_variables` remains a compatibility field. | `application-composition-service`, `apps/web` | Workshop public docs treat variables as data movement through modules. OpenFoundry should model all future variable kinds through this term. |
| Object set | `object_set` | `id`, `name`, `description`, `base_object_type_id`, `filters`, `traversals`, `join`, `projections`, `policy`, `materialized_snapshot` | `object_set_variable` only when the object set is held as a Workshop variable. | `ontology-query-service`, `object-database-service`, `libs/ontology-kernel` | An object set is an ontology query definition or evaluated set of objects. It should not be used to mean a table or raw dataset. |
| Action type | `action_type` | `id`, `name`, `display_name`, `object_type_id`, `operation_kind`, `input_schema`, `form_schema`, `config`, `authorization_policy` | `action` in UI, `apply_action` in API/client code, `inline_edit_action` for property edits. | `ontology-actions-service`, `libs/ontology-kernel`, `apps/web` | Use `action_type` for reusable definitions. Use `action_execution` for a submitted action run. |
| Action execution | `action_execution` | `action_type_id`, `target_object_id`, `parameters`, `justification`, `operation_id`, `status`, `output_parameters` | `apply_action`, `execute_action`, `submission`. | `ontology-actions-service`, `libs/ontology-kernel` | Execution rows belong to audit/action-log surfaces and may include webhook/function outputs. |
| Function package | `function_package` | `id`, `name`, `version`, `runtime`, `entrypoint`, `source`, `capabilities`, `status` | `function`, `foundry_function`; use `function` in user-facing copy. | `ontology-actions-service`, `libs/ontology-kernel` | Use `function_package` for versioned authored code. Use `function_invocation` or `function_run` for one execution. |
| Function invocation | `function_invocation` | `function_package_id`, `function_version`, `parameters`, `object_type_id`, `target_object_id`, `result`, `stdout`, `stderr`, `status` | `function_run`, `simulate_function`, `execute_function`. | `ontology-actions-service`, `libs/python-sidecar` | Inline Python/TypeScript runtimes should map into this execution shape. |
| Pipeline | `pipeline` | `id`, `name`, `description`, `dag`, `nodes`, `status`, `schedule_config`, `retry_policy`, `pipeline_type`, `lifecycle`, `project_id` | `data_integration_pipeline`, `workshop_pipeline` should not be used. | `pipeline-build-service`, `apps/web` | A pipeline is an authored DAG. `dag` is the persisted graph payload; `nodes` is the typed API view. |
| Pipeline node | `pipeline_node` | `id`, `label`, `transform_type`, `config`, `depends_on`, `input_dataset_ids`, `output_dataset_id`, `preview_status`, `validation_status` | `node`, `transform_node`. | `pipeline-build-service`, `apps/web` | A node is an instance in a pipeline graph. A transform is the operation performed by a node. |
| Transform | `transform` | `transform_type`, `config`, `input_schema`, `output_schema`, `runtime`, `engine`, `validation_errors` | `operation`, `step`; avoid `job` for transform definitions. | `pipeline-build-service` | A transform is reusable behavior; a pipeline node configures one transform instance. |
| Build | `build` | `id`, `rid`, `pipeline_rid`, `build_branch`, `state`, `trigger_kind`, `force_build`, `abort_policy`, `queued_at`, `started_at`, `finished_at`, `requested_by` | `pipeline_run` remains a compatibility alias for legacy run surfaces. | `pipeline-build-service` | A build is a durable execution of resolved pipeline jobs. Jobs are child execution units. |
| Job | `job` | `id`, `rid`, `build_id`, `job_spec_rid`, `state`, `attempt`, `output_transaction_rids`, `failure_reason` | `task` should be avoided in pipeline runtime APIs. | `pipeline-build-service` | Jobs are build-time execution units, not authored pipeline nodes. |
| Dataset output | `dataset_output` | `output_dataset_id`, `output_dataset_rid`, `output_dataset_rids`, `transaction_rid`, `schema`, `branch`, `commit_status` | `dataset sink`, `output dataset`; avoid `target_dataset` unless it is user-facing form copy. | `pipeline-build-service`, `dataset-versioning-service` | Use `*_rid` on `/v1/builds` style APIs and `*_id` on internal UUID-backed graph nodes. |
| Object output | `object_output` | `object_type_id`, `primary_key_property`, `property_mappings`, `dataset_rid`, `object_set_id`, `materialization_status` | `ontology_output`, `object_type_output`; accepted as UI labels. | `pipeline-build-service`, ontology services | This is a pipeline output that materializes rows into ontology objects or object type backing. |
| Link output | `link_output` | `link_type_id`, `source_object_type_id`, `target_object_type_id`, `source_key`, `target_key`, `property_mappings` | `ontology_link_output`. | `pipeline-build-service`, ontology services | Adjacent to object output. Required when a pipeline emits relationships between object types. |
| Data connection source | `source` | `id`, `source_rid`, `name`, `connector_type`, `config`, `credentials`, `egress_policies` | `connection`, `external_system`. | `connector-management-service` | Existing `/connections` aliases can stay, but new product docs should say source. |
| Webhook | `webhook` | `id`, `url`, `method`, `headers`, `input_schema`, `output_schema`, `auth_ref`, `output_parameters` | `external_function` only when wired as a function boundary. | `connector-management-service`, `ontology-actions-service` | Webhooks are Data Connection definitions that actions/functions can invoke. |
| Geospatial layer | `geospatial_layer` | `id`, `name`, `kind`, `source`, `style`, `geometry_type`, `feature_count` | `map_layer`, `layer`. | `ontology-exploratory-analysis-service`, `apps/web` | Use `map_layer` in UI if needed; use `geospatial_layer` in backend contracts. |

## Reserved compatibility aliases

| Alias | Canonical target | Policy |
| --- | --- | --- |
| `workshop_app` | `app` | Accept in docs/import metadata only. Persist `app`. |
| `workshop_module` | `module` | Reserve until modules are first-class. Current app definitions may have one implicit module. |
| `component` | `widget` | UI copy may say component, wire models must say widget. |
| `apply_action` | `action_execution` | API method/action verb, not the durable resource name. |
| `pipeline_run` | `build` or `pipeline_run` | Legacy run surfaces may keep `pipeline_run`; Foundry-aligned `/v1` surfaces should say `build`. |
| `output_dataset` | `dataset_output` | Accept as UI label; use `dataset_output` for resource concept and `output_dataset_*` for fields. |
| `ontology_output` | `object_output` or `link_output` | Use when a UI groups both object and link outputs. |
| `source_id` | `source.id` | Internal UUID reference. Use `source_rid` for external identity. |

## Field suffix guide

| Suffix | Meaning | Examples |
| --- | --- | --- |
| `_id` | Internal UUID or local child identifier. | `app_id`, `object_type_id`, `action_type_id`, `widget.id`, `page.id` |
| `_rid` | Stable external resource identifier. | `dataset_rid`, `pipeline_rid`, `build.rid`, `transaction_rid` |
| `_type` | Taxonomy or schema category. | `widget_type`, `property_type`, `transform_type`, `source_type` |
| `_kind` | Behavior enum or execution mode. | `operation_kind`, `trigger_kind`, `join_kind` |
| `_config` | Behavior configuration owned by a runtime. | `schedule_config`, `retry_policy`, `external_config` |
| `_schema` | Typed input/output/form shape. | `input_schema`, `output_schema`, `form_schema` |
| `_mappings` | Explicit source-target field mapping. | `property_mappings`, `column_mappings` |
| `_status` | User-facing lifecycle state. | `preview_status`, `validation_status`, `materialization_status` |
| `_state` or `state` | Durable execution state machine value. | `build.state`, `job.state` |

## API compatibility checklist

- New public payloads must use the canonical terms in the matrix above.
- Existing aliases may be accepted at decode time, but handlers should normalize
  to the canonical model before persistence.
- If both `id` and `rid` exist, route paths and response bodies must document
  which one is accepted. Do not silently accept a RID in a UUID-only route.
- New frontend types should mirror backend JSON tags exactly.
- New docs should link this glossary when introducing Pipeline Builder,
  Workshop, Ontology, Actions, Functions, or Data Connection concepts.
- New migrations should avoid generic names such as `resource_id`,
  `component_id`, `operation`, or `target` when a frozen name exists.

## Service reference map

| Service or surface | Must use these terms |
| --- | --- |
| `services/application-composition-service` | `app`, `module`, `page`, `widget`, `variable` |
| `apps/web/src/lib/api/apps.ts` | `app`, `page`, `widget`, `variable`, `event`, `binding` |
| `services/pipeline-build-service` | `pipeline`, `pipeline_node`, `transform`, `build`, `job`, `dataset_output`, `object_output`, `link_output` |
| `apps/web/src/lib/api/pipelines.ts` | `pipeline`, `pipeline_node`, `transform`, `pipeline_run` compatibility fields |
| `apps/web/src/lib/api/buildsV1.ts` | `build`, `job`, `dataset_output`, `rid` |
| `services/ontology-actions-service` | `action_type`, `action_execution`, `function_package`, `function_invocation`, `object_set` |
| `libs/ontology-kernel` | `object_set`, `action_type`, `function_package`, `object_output`, `link_output` |
| `services/connector-management-service` | `source`, `source_rid`, `webhook`, `output_parameters` |
| `services/ontology-exploratory-analysis-service` | `geospatial_layer`, `map_layer`, `feature`, `cluster`, `tile` |
