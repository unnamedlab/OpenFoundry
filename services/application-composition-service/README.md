# `application-composition-service` (Go)

## LLM quick context (current code)

Owns App Builder application definitions, pages, templates, widgets, public embeds, and preview metadata.

Agent note: do not confuse with the React frontend in apps/web; this service is the backend app-composition API.

Current surface:
- `/api/v1/apps*`
- `/api/v1/apps/from-template`
- `/api/v1/apps/public/{slug}[/embed]`
- `/api/v1/widgets*`
- `GET /healthz`
- `GET /metrics`

State/dependency hints:
- Contains `7` SQL migration/schema file(s); check service migrations before changing persisted models.
- Main internal packages: `catalog`, `config`, `handlers`, `models`, `repo`, `server`.
- Local service files present: `Dockerfile`.

Configuration signals:
Environment variables referenced by the code:
- `DATABASE_URL`, `HOST`, `ICEBERG_CATALOG_WAREHOUSE_URI`, `ICEBERG_DEFAULT_TENANT`, `ICEBERG_DEFAULT_TOKEN_TTL_SECS`, `ICEBERG_JWT_AUDIENCE`, `ICEBERG_JWT_ISSUER`, `ICEBERG_LONG_LIVED_TOKEN_TTL_SECS`
- `JWT_SECRET`, `METRICS_ADDR`, `OAUTH_INTEGRATION_URL`, `OPENFOUNDRY_JWT_SECRET`, `PORT`, `SERVICE_VERSION`

Keep this section in sync when changing routes, config, or persistence behavior.

Runtime owner for OpenFoundry's Workshop-style application composition surface.
The service persists apps, pages/widgets inside the app definition, versions,
publish snapshots, and public runtime loading by slug.

## Compatibility naming

Application Builder public payloads should follow the frozen terminology in
[`docs/reference/foundry-compatibility-glossary.md`](../../docs/reference/foundry-compatibility-glossary.md):
use `app` for the authored/published application, reserve `module` for a
future first-class Workshop module boundary, use `page` for route-level app
surfaces, `widget` for renderable building blocks, and `variable` for values
that move data between widgets, pages, modules, actions, and functions.

Current app definitions are normalized to the `2026-05-11.ws.1` Workshop app
contract before create, update, and publish. That contract exposes the key wire
fields:

- `app.id`, `app.slug`, `pages`, `settings`, `theme`
- `page.id`, `page.path`, `page.layout`, `page.widgets`, `page.sections`,
  `page.overlays`
- `section.id`, `section.layout`, `section.widgets`, `section.sections`
- `widget.id`, `widget.widget_type`, `widget.props`, `widget.config`,
  `widget.binding`, `widget.bindings`, `widget.events`, `widget.actions`,
  `widget.children`
- compatibility object-set variables under `settings.object_set_variables`
  and Workshop variables under `settings.workshop_variables`
- runtime metadata under `settings.runtime_metadata`, including
  `schema_version` and `public_slug`

WS.18 preview semantics keep the mutable draft and public runtime separated.
`GET /api/v1/apps/{id}/preview` returns the current draft app plus widget
catalog metadata for editor preview, while `/api/v1/apps/public/{slug}` rebuilds
the app from `app_versions.app_snapshot` so published links stay stable after a
draft edit. The web editor preview and public runtime both render through
`AppRenderer`; preview persists `status='draft'` and passes URL/runtime
parameters without publishing a new version.

The web runtime evaluates `settings.workshop_variables` through the Workshop
variable engine and shared object-set executor. Variable definitions may
reference `source_variable_id`, `filter_variable_id`, `source_widget_id`,
`object_set_id` / `saved_object_set_id`, static filters, default values, and
metadata for URL/runtime parameters, aggregations, or search-around object set
execution.

Widget `events` are interpreted by the WS.6 runtime event engine in declaration
order. Supported actions include variable updates, runtime parameter changes,
navigation, URL opening, refresh, action invocation, export, and command-style
events; downstream object-set recompute is driven by the shared variable engine
and refresh keys in the web runtime.

Button Group and Object Table action buttons use the WS.7/WS.16/WS.17 generic
Ontology action flow. The web runtime loads the configured action type, resolves
parameter defaults from static values, variables, active objects, selected object
sets, aggregations, and function-backed variables, shows local and server
validation issues, executes edit/webhook/function action kinds through the
generic single or batch action endpoint, reports per-target partial failures for
bulk runs, then refreshes downstream widgets after success.

Object Table widgets use the WS.8 object-table contract for configured columns,
sort defaults, row-height display controls, active-object outputs,
multi-selected object-set outputs, row actions, and inline-edit enablement.
Published runtime and editor preview share the same runtime variables and action
form behavior for these table outputs.

Filter List widgets use the WS.9 filter-list contract for source object-set
variables, emitted object-set filter variables, default filter values,
user-added or removed filters, and vertical or pill layouts. Downstream Object
Table, Object Set Title, Chart, Map, and Property List widgets consume the
filtered object set through the shared variable engine and object-set executor.

Property List widgets use the WS.10 property-list contract for a single input
object-set variable. Runtime rendering displays the first object from generic
object sets, or the active/selected object published by upstream widgets, then
applies selected property configuration, null hiding, adjacent/below layouts,
value wrapping, and basic type formatting.

Object Set Title widgets use the WS.14 object-set title contract for object-set
counts, single-object title display, object type icons, title overrides, and
empty-state rendering. This lets selected trail detail sections show the active
trail title while aggregate sections continue to show object-set counts.

Chart XY widgets use the WS.11 chart contract for object-set backed layers,
bar/line/scatter series, property aggregations, axes, legend/tooltips, and
selection-as-filter output. A selected category can publish both a reusable
`filter_output` variable and an `object_set_selection` variable for downstream
Object Table, Property List, Chart, and Map widgets.

Metric Card widgets use the WS.12/WS.15 metric contract for grouped
numeric/string metrics, primitive or Function-backed variable inputs, static
fallbacks, number/currency/unit formatting, conditional formatting, and
compact/card/tag/list presentation. Weather cards can bind temperature, wind,
humidity, and status variables without custom widget code, while effort cards
can display lazily cached Function outputs.

Map widgets use the WS.13 map contract for local object layers, overlay layers,
MapLibre rendering, base map settings, viewport tile layers, map-template
parameter mappings, visibility variables, selected-object outputs, drawn shape
outputs, and shape search results. The same widget config renders in editor
preview and the published runtime.

Free-form Analysis widgets use the WS.21 contract for app-bounded object set
exploration. The widget reads one configured input object set, lets runtime
users add filter/table/metric/bar/line/pie/text cards, optionally save local
analysis paths, and emits the currently filtered rows as an
`object_set_selection` output variable consumable by Object Set Title, Object
Table, Property List, Chart, Map, and action defaults.

WS.20 app access is enforced from JWT roles and permission keys:
`apps:view` gates read/preview/version/catalog surfaces, `apps:edit` gates draft
mutation, page CRUD, Slate import, and delete, and `apps:publish` gates publish,
promote, and rollback. Per-app permission keys such as
`app:<uuid>:publish` are also accepted. App create/update/delete/page/slate and
publish/promote operations emit best-effort rows into `app_audit_events`, while
denied edit or publish attempts return `403` and record the denied permission.
The public runtime only serves immutable published snapshots and rejects
archived apps or snapshots whose status is not `published`.

Use `id` for internal UUIDs or local child identifiers, and introduce `rid`
only when a resource needs a stable external identity.

## Workshop editor endpoints

The service owns the backend calls expected by `apps/web/src/lib/api/apps.ts`:

- `GET /api/v1/widgets/catalog` serves the embedded, versioned
  `internal/catalog/widget_catalog.v1.json` contract and returns
  `X-OpenFoundry-Widget-Catalog-Version` / `X-OpenFoundry-Widget-Catalog-Schema`
  headers.
- `GET /api/v1/apps/templates`
- `POST /api/v1/apps/from-template`
- `POST /api/v1/apps/{id}/pages`
- `PATCH /api/v1/apps/{id}/pages/{pageId}`
- `DELETE /api/v1/apps/{id}/pages/{pageId}`
- `GET /api/v1/apps/{id}/preview`
- `GET|POST /api/v1/apps/{id}/slate-package`
- `GET /api/v1/apps/{id}/versions`
- `POST /api/v1/apps/{id}/publish`
- `POST /api/v1/apps/{id}/versions/{versionId}/promote`
- `GET /api/v1/apps/public/{slug}` and `/embed`

## Build & run

```sh
go build -o bin/application-composition-service ./services/application-composition-service/cmd/application-composition-service
go test ./services/application-composition-service/...
```
