# Foundry Ontology Manager and Object Views 1:1 parity checklist

Date: 2026-05-11
Scope: public-docs-based parity plan for OpenFoundry's Ontology Manager,
Ontology resource modeling, Object Explorer-adjacent exploration, Object Views,
and Object View delivery surfaces: ontologies, object types, properties, shared
properties, value types, link types, action type references, object type groups,
interfaces, interface link constraints, datasource mappings, ontology change
management, ontology permissions, object security policies, restricted-view and
multi-datasource object security, core Object Views, custom Object Views,
full/panel Object Views, Object View URLs, comments on objects, Object View
branching handoffs, and Marketplace packaging.

This document is intentionally implementation-oriented. It does not attempt to
clone Palantir branding, private source code, proprietary assets, screenshots,
or any non-public behavior. The target is **functional parity based on public
Palantir Foundry documentation**: the same product concepts, comparable
builder/reviewer/operator workflows, compatible resource models where useful,
and OpenFoundry-native implementation details that can be tested locally.

## Parity scope boundary

All checklist work is governed by the
[Foundry public-docs parity policy](../reference/foundry-public-docs-parity-policy.md).
OpenFoundry may implement behavior described in public Palantir documentation,
but contributors must not copy private source, decompile bundles, import
tenant-specific exports, use Palantir branding, or reuse proprietary assets.
The product target is functional parity in an OpenFoundry-native implementation,
not a pixel-perfect clone.

This checklist covers the ontology design and object representation layer. It
should integrate with specialized checklists for Pipeline Builder, Data
Foundation, Global Branching, Workshop, Ontology Actions, Functions, Object
Permissioning/Security, and DevOps/Marketplace. It should not duplicate the full
runtime semantics of action execution, function execution, data versioning, or
Workshop app building; it defines how ontology resources and Object Views are
authored, rendered, permissioned, versioned, packaged, and linked.

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
| `P0` | Required to model, view, and navigate Trail Running demo ontology objects with useful object detail pages and links. |
| `P1` | Required for credible Foundry-style ontology management and Object View parity beyond a single demo. |
| `P2` | Advanced, governance-heavy, marketplace, branching, or scale-oriented parity. |

## Official Palantir documentation library

These public docs should be treated as the external behavioral contract while
implementing this checklist.

### Ontology and Ontology Manager

- [Ontologies overview](https://www.palantir.com/docs/foundry/ontologies/ontologies-overview)
- [Ontology Manager overview](https://www.palantir.com/docs/foundry/ontology-manager/overview/index.html)
- [Ontology Manager navigation](https://www.palantir.com/docs/foundry/ontology-manager/navigation)
- [Viewing usage in Ontology Manager](https://www.palantir.com/docs/foundry/ontology-manager/viewing-usage)
- [Save changes to the Ontology](https://www.palantir.com/docs/foundry/ontology-manager/save-changes)
- [Review and restore changes](https://www.palantir.com/docs/foundry/ontology-manager/restore-changes)
- [Export, edit, and import an Ontology](https://www.palantir.com/docs/foundry/ontology-manager/export-import/)
- [Ontology cleanup](https://www.palantir.com/docs/foundry/ontology-manager/ontology-cleanup)
- [Test changes in the Ontology](https://www.palantir.com/docs/foundry/ontologies/test-changes-in-ontology)

### Ontology resources and data modeling

- [Types reference](https://www.palantir.com/docs/foundry/object-link-types/type-reference)
- [Create an object type](https://www.palantir.com/docs/foundry/object-link-types/create-object-type/)
- [Edit object types](https://www.palantir.com/docs/foundry/object-link-types/edit-object-types)
- [Object type metadata reference](https://www.palantir.com/docs/foundry/object-link-types/object-type-metadata)
- [Properties overview](https://www.palantir.com/docs/foundry/object-link-types/properties-overview/)
- [Edit object type properties](https://www.palantir.com/docs/foundry/object-link-types/edit-object-type-properties)
- [Add value formatting](https://www.palantir.com/docs/foundry/object-link-types/value-formatting/)
- [Add conditional formatting](https://www.palantir.com/docs/foundry/object-link-types/conditional-formatting)
- [Property reducers](https://www.palantir.com/docs/foundry/object-link-types/property-reducers/)
- [Value types overview](https://www.palantir.com/docs/foundry/object-link-types/value-types-overview/)
- [Object type groups](https://www.palantir.com/docs/foundry/object-link-types/type-groups/)
- [Link types overview](https://www.palantir.com/docs/foundry/object-link-types/link-types-overview)
- [Create a link type](https://www.palantir.com/docs/foundry/object-link-types/create-link-type/)

### Interfaces and polymorphism

- [Interfaces overview](https://www.palantir.com/docs/foundry/interfaces/interface-overview/)
- [Create an interface](https://www.palantir.com/docs/foundry/interfaces/create-interface)
- [Implement an interface](https://www.palantir.com/docs/foundry/interfaces/implement-interface)
- [Edit an interface definition](https://www.palantir.com/docs/foundry/interfaces/edit-interface-definition)
- [Edit an interface implementation](https://www.palantir.com/docs/foundry/interfaces/edit-interface-implementation)
- [Interface link type constraints](https://www.palantir.com/docs/foundry/interfaces/interface-link-types-overview)
- [Extend an interface](https://www.palantir.com/docs/foundry/interfaces/extend-interface)
- [Actions on interfaces](https://www.palantir.com/docs/foundry/action-types/actions-on-interfaces/)

### Object permissions and object security

- [Object permissioning overview](https://www.palantir.com/docs/foundry/object-permissioning/overview)
- [Ontology permissions](https://www.palantir.com/docs/foundry/ontologies/ontology-permissions/)
- [Managing object security](https://www.palantir.com/docs/foundry/object-permissioning/managing-object-security/)
- [Configure restricted-view-backed object types](https://www.palantir.com/docs/foundry/object-permissioning/configuring-rv-access-controls/)
- [Multi-datasource object types](https://www.palantir.com/docs/foundry/object-permissioning/multi-datasource-objects/)
- [Object security policies](https://www.palantir.com/docs/foundry/object-permissioning/object-and-property-policies)

### Object Explorer and object navigation

- [Object Explorer overview](https://www.palantir.com/docs/foundry/object-explorer/overview)
- [Configure Object Explorer](https://www.palantir.com/docs/foundry/object-explorer/configure)
- [Filter results](https://www.palantir.com/docs/foundry/object-explorer/filter-results)
- [Pivot to explore linked objects](https://www.palantir.com/docs/foundry/object-explorer/pivot-linked/)
- [Save explorations](https://www.palantir.com/docs/foundry/object-explorer/save-explorations)
- [Apply Actions in Object Explorer](https://www.palantir.com/docs/foundry/object-explorer/apply-actions/)

### Object Views

- [Object Views overview](https://www.palantir.com/docs/foundry/object-views/overview)
- [Core Object Views](https://www.palantir.com/docs/foundry/object-views/core-object-views/)
- [Custom Object View configuration](https://www.palantir.com/docs/foundry/object-views/config-overview)
- [Use full Object Views in the platform](https://www.palantir.com/docs/foundry/object-views/use-full-views-in-platform)
- [Configure full Object Views](https://www.palantir.com/docs/foundry/object-views/config-object-views)
- [Use panel Object Views in the platform](https://www.palantir.com/docs/foundry/object-views/use-panel-views-in-platform)
- [Configure panel Object Views](https://www.palantir.com/docs/foundry/object-views/config-panel-views/)
- [Manage custom Object View versions](https://www.palantir.com/docs/foundry/object-views/manage-versions/)
- [Generate Object View URLs](https://www.palantir.com/docs/foundry/object-views/generate-urls/)
- [Comment on objects](https://www.palantir.com/docs/foundry/object-views/comment-on-objects/)
- [Branching object views](https://www.palantir.com/docs/foundry/object-views/branching-object-views)
- [Add Object Views to a Marketplace product](https://www.palantir.com/docs/foundry/object-views/marketplace-object-views)

## Target OpenFoundry resource model

The implementation should define stable OpenFoundry-owned resources that can map
to public Foundry concepts without requiring Palantir RID formats. Compatibility
aliases may be accepted at service boundaries, but persisted state should use
OpenFoundry canonical IDs.

| Public Foundry concept | OpenFoundry resource target | Required notes |
| --- | --- | --- |
| Ontology | `ontology` | Space-scoped artifact containing object types, link types, action types, interfaces, shared properties, object type groups, and Object View configuration. |
| Ontology resource | `ontology_resource` | Project/folder-managed resource with type, API name, display name, description, status, permissions, history, and branch metadata. |
| Object type | `object_type` | Schema definition for real-world entities/events with properties, primary key, title key, metadata, groups, interfaces, datasources, and display configuration. |
| Property | `object_property` | Object type property with API name, display name, base type, value type, formatting, visibility, prominence, datasource mapping, and edit/security metadata. |
| Shared property | `shared_property_type` | Reusable property metadata used across object types and interfaces. |
| Value type | `value_type` | Space-scoped semantic wrapper over a base field type with validation constraints, formatting, permissions, and versions. |
| Link type | `link_type` | Relationship schema between two object types or interface targets, including cardinality, display metadata, and optional backing datasource. |
| Object type group | `object_type_group` | Searchable categorization primitive displayed in Ontology Manager and Object Explorer. |
| Interface | `interface_type` | Abstract schema with interface properties, interface link constraints, implementation requirements, and implementing object types. |
| Interface implementation | `interface_implementation` | Mapping from one object type to one interface, including property mappings and concrete link mappings. |
| Ontology change | `ontology_change` | Unsaved or saved change record with author, timestamp, resource target, diff summary, restore state, and branch/proposal context. |
| Ontology import/export | `ontology_bundle` | OpenFoundry-native portable representation for editing, reviewing, validating, importing, and exporting ontology resources. |
| Core Object View | `core_object_view` | Automatically generated full and panel representation for every object type based on object metadata, properties, links, and prominent display hints. |
| Custom Object View | `custom_object_view` | Workshop-backed, user-managed representation that can override the default object display while preserving access to core Object View. |
| Object View form factor | `object_view_form_factor` | Full or panel rendering target with separate module/tabs/layout constraints. |
| Object View tab | `object_view_tab` | Tab metadata plus a Workshop module backing full Object View content. |
| Object View module | `object_view_workshop_module` | Workshop module bound to a selected object context and Object View form factor. |
| Object View version | `object_view_version` | Saved/published version metadata for object view configuration and module content. |
| Object comment | `object_comment` | Comment thread attached to an object, with mentions, attachments, permissions, and activity history. |
| Object Explorer exploration | `object_exploration` | Saved object set query, filters, layout, visualization state, privacy, and open-in/action/export behavior. |
| Marketplace Object View output | `marketplace_object_view_output` | Product output that packages selected Workshop-tab-backed Object View tabs. |

## Milestone A: minimum viable Ontology Manager and Object Views parity

### Ontology shell and resource discovery

- [x] `OMOV.1` Ontology and space relationship (`P0`, `done`)
  - Model an ontology as a space-scoped artifact with organization visibility and project/folder placement.
  - Support private and shared ontology access semantics in OpenFoundry-native terms.
  - Display ontology metadata, owning space, organizations, and linked resources from the Ontology Manager home page.
  - Docs: [Ontologies overview](https://www.palantir.com/docs/foundry/ontologies/ontologies-overview), [Ontology Manager overview](https://www.palantir.com/docs/foundry/ontology-manager/overview/index.html).

- [x] `OMOV.2` Ontology Manager application shell (`P0`, `done`)
  - Provide navigation for object types, link types, action types, interfaces, shared properties, object type groups, Object Views, usage, unsaved changes, history, import/export, and cleanup.
  - Include global search, resource filters, recently edited resources, warning/error banners, and project/security context.
  - Docs: [Ontology Manager overview](https://www.palantir.com/docs/foundry/ontology-manager/overview/index.html), [Ontology Manager navigation](https://www.palantir.com/docs/foundry/ontology-manager/navigation).

- [x] `OMOV.3` Ontology resource registry (`P0`, `done`)
  - Store object types, link types, action types, interfaces, shared properties, object type groups, Object Views, and backing datasource registrations as first-class resources.
  - Track API name, display name, plural display name where applicable, description, project/folder, visibility, status, branch state, usage count, and last edited metadata.
  - Docs: [Types reference](https://www.palantir.com/docs/foundry/object-link-types/type-reference), [Viewing usage](https://www.palantir.com/docs/foundry/ontology-manager/viewing-usage).

### Object types and properties

- [x] `OMOV.4` Object type creation helper (`P0`, `done`)
  - Implement guided object type creation with datasource selection, metadata, property generation, primary key, title key, group selection, save location, and optional generated actions handoff.
  - Support creating an object type without an existing datasource by selecting a location for a generated permissions dataset or OpenFoundry-native placeholder.
  - Warn when unsupported datasource column types cannot back object properties.
  - Docs: [Create an object type](https://www.palantir.com/docs/foundry/object-link-types/create-object-type/), [Properties overview](https://www.palantir.com/docs/foundry/object-link-types/properties-overview/).

- [ ] `OMOV.5` Object type metadata editor (`P0`, `todo`)
  - Edit display name, plural display name, description, icon/color, API name before dependent use, groups, visibility, and object display preferences.
  - Preserve API-name stability warnings once user applications or functions reference the object type.
  - Docs: [Create an object type](https://www.palantir.com/docs/foundry/object-link-types/create-object-type/), [Object type metadata reference](https://www.palantir.com/docs/foundry/object-link-types/object-type-metadata), [Object type groups](https://www.palantir.com/docs/foundry/object-link-types/type-groups/).

- [x] `OMOV.6` Property editor and datasource mapping (`P0`, `done`)
  - Add, edit, hide, delete, and map properties to backing datasource columns.
  - Support “add all unmapped columns,” map a column to a new property, primary key selection, title key selection, and type inference from datasource columns.
  - Validate API names, reserved words, duplicate property IDs, unsupported primary/title key types, and unmapped required keys.
  - Docs: [Create an object type](https://www.palantir.com/docs/foundry/object-link-types/create-object-type/), [Edit object type properties](https://www.palantir.com/docs/foundry/object-link-types/edit-object-type-properties), [Properties overview](https://www.palantir.com/docs/foundry/object-link-types/properties-overview/).

- [x] `OMOV.7` Property base types and metadata (`P0`, `done`)
  - Support common primitive base types, decimal/numeric types, Boolean, date/time/timestamp, arrays where locally supported, geospatial/geohash/geoshape, media reference, time series reference, binary/file references, and object reference patterns.
  - Store whether each type is eligible for primary key, title key, filtering, sorting, aggregation, formatting, object security, and Object View prominent display.
  - Docs: [Properties overview](https://www.palantir.com/docs/foundry/object-link-types/properties-overview/), [Types reference](https://www.palantir.com/docs/foundry/object-link-types/type-reference).

- [x] `OMOV.8` Property formatting and visibility (`P0`, `done`)
  - Configure hidden, normal, and prominent property display modes.
  - Support value formatting, numeric/date/time formatting, conditional formatting, and property reducer metadata where OpenFoundry has rendering support.
  - Ensure Object Views react to hidden/prominent settings.
  - Docs: [Core Object Views](https://www.palantir.com/docs/foundry/object-views/core-object-views/), [Add value formatting](https://www.palantir.com/docs/foundry/object-link-types/value-formatting/), [Add conditional formatting](https://www.palantir.com/docs/foundry/object-link-types/conditional-formatting), [Property reducers](https://www.palantir.com/docs/foundry/object-link-types/property-reducers/).

### Link types, groups, and basic navigation

- [x] `OMOV.9` Link type creation and overview (`P0`, `done`)
  - Create link types between object types, including self-links and links with one-to-one, one-to-many, many-to-one, and many-to-many cardinalities.
  - Configure source/target object types, display names, API names, descriptions, labels, reverse labels, visibility, and cardinality.
  - For many-to-many links, support link-datasource mapping and key mapping.
  - Docs: [Link types overview](https://www.palantir.com/docs/foundry/object-link-types/link-types-overview), [Create a link type](https://www.palantir.com/docs/foundry/object-link-types/create-link-type/).

- [x] `OMOV.10` Object type groups (`P0`, `done`)
  - Create, edit, delete, search, and permission object type groups.
  - Add/remove groups from object type metadata.
  - Show groups in Ontology Manager search/filtering and Object Explorer home pages.
  - Docs: [Object type groups](https://www.palantir.com/docs/foundry/object-link-types/type-groups/), [Configure Object Explorer](https://www.palantir.com/docs/foundry/object-explorer/configure).

- [x] `OMOV.11` Object detail drawer and object type graph (`P0`, `done`)
  - From object type pages, show a graph of linked object types and link types.
  - Selecting a link from the graph should open the link type detail with overview and datasource tabs.
  - Object instance detail drawers should show title, primary key, prominent properties, normal properties, linked objects, and available actions.
  - Docs: [Ontology Manager overview](https://www.palantir.com/docs/foundry/ontology-manager/overview/index.html), [Core Object Views](https://www.palantir.com/docs/foundry/object-views/core-object-views/), [Link types overview](https://www.palantir.com/docs/foundry/object-link-types/link-types-overview).

### Core Object Views

- [x] `OMOV.12` Automatic core Object View generation (`P0`, `done`)
  - Generate full and panel core Object Views for every object type.
  - Render title, primary key, prominent properties, normal properties, non-hidden linked objects, and metadata from the current object type configuration.
  - Keep core Object Views available even when a custom Object View exists.
  - Docs: [Object Views overview](https://www.palantir.com/docs/foundry/object-views/overview), [Core Object Views](https://www.palantir.com/docs/foundry/object-views/core-object-views/).

- [x] `OMOV.13` Core prominent property display (`P0`, `done`)
  - Render media reference properties with a media viewer, time series properties as charts, geospatial/geotemporal properties on maps, and other prominent properties as elevated cards.
  - Render normal properties in a table and hide hidden properties.
  - Degrade gracefully when media, time series, or map subsystems are not installed.
  - Docs: [Core Object Views](https://www.palantir.com/docs/foundry/object-views/core-object-views/).

- [x] `OMOV.14` Core linked objects component (`P0`, `done`)
  - Group linked objects by link type.
  - Preview linked object properties inline.
  - Open a subset of linked objects in a new tab or exploration.
  - Preview a selected linked object in the side panel.
  - Docs: [Core Object Views](https://www.palantir.com/docs/foundry/object-views/core-object-views/), [Pivot to explore linked objects](https://www.palantir.com/docs/foundry/object-explorer/pivot-linked/).

- [x] `OMOV.15` Object View form factors (`P0`, `done`)
  - Support full Object Views for comprehensive object detail.
  - Support panel Object Views for compact display inside maps, graphs, Workshop, Object Explorer, and other applications.
  - Provide consistent object title behavior and a way to open a full Object View from panel contexts.
  - Implementation note: OpenFoundry now uses shared Object View title/full-link helpers, URL-addressable full Object Views, compact panel tabs, and Object Explorer panel previews with an explicit "Open full Object View" action.
  - Docs: [Object Views overview](https://www.palantir.com/docs/foundry/object-views/overview), [Use full Object Views](https://www.palantir.com/docs/foundry/object-views/use-full-views-in-platform), [Use panel Object Views](https://www.palantir.com/docs/foundry/object-views/use-panel-views-in-platform).

## Milestone B: credible Foundry-style Ontology Manager and Object Views parity

### Advanced ontology modeling

- [x] `OMOV.16` Shared properties (`P1`, `done`)
  - Create and manage shared property types that can be reused by multiple object types and interfaces.
  - Centralize display name, API name, description, base type, value type, formatting, and usage discovery.
  - Warn when edits to a shared property affect multiple object types.
  - Implementation note: Ontology Manager now provides shared property create/edit/delete, object-type attach/detach, value-type linkage, usage summaries, and multi-binding edit warnings.
  - Docs: [Types reference](https://www.palantir.com/docs/foundry/object-link-types/type-reference), [Properties overview](https://www.palantir.com/docs/foundry/object-link-types/properties-overview/).

- [x] `OMOV.17` Value types (`P1`, `done`)
  - Create, version, permission, search, and apply value types within a space.
  - Support semantic metadata, validation constraints, formatting, non-breaking edits, breaking edits, and usage discovery.
  - Enforce value type constraints in property mappings, Pipeline Builder validation, ontology indexing, Object Views, and user edits where applicable.
  - Implementation note: Value types are modeled as space-scoped semantic resources with local create/edit/delete/search, version metadata for breaking edits, permission fields, usage discovery, formatting fallback in Object Views, and inline-edit constraint validation for properties carrying value type metadata.
  - Docs: [Value types overview](https://www.palantir.com/docs/foundry/object-link-types/value-types-overview/).

- [x] `OMOV.18` Interfaces (`P1`, `done`)
  - Create interface types with interface properties, display metadata, implementation requirements, and implementing object type lists.
  - Implement interfaces on object types with explicit property mappings.
  - Support interface extension/inheritance where documented and locally modeled.
  - Implementation note: Interfaces now expose local extension bindings, implementation detail mappings, required-mapping validation, and implementing object-type coverage in the Interfaces workbench.
  - Docs: [Interfaces overview](https://www.palantir.com/docs/foundry/interfaces/interface-overview/), [Create an interface](https://www.palantir.com/docs/foundry/interfaces/create-interface), [Implement an interface](https://www.palantir.com/docs/foundry/interfaces/implement-interface), [Extend an interface](https://www.palantir.com/docs/foundry/interfaces/extend-interface).

- [x] `OMOV.19` Interface link type constraints (`P1`, `done`)
  - Define interface link constraints with link target type, target object/interface, cardinality, required flag, description, and API name.
  - Validate that implementing object types provide concrete link types satisfying required constraints.
  - Expose interface link APIs in object search and Object Views when backed by a concrete implementation.
  - Implementation note: Interface link constraints are modeled with target kind/target/cardinality/required metadata, editable in the Interfaces workbench, and validated against concrete implementation link mappings.
  - Docs: [Interface link type constraints](https://www.palantir.com/docs/foundry/interfaces/interface-link-types-overview), [Edit interface implementation](https://www.palantir.com/docs/foundry/interfaces/edit-interface-implementation).

- [x] `OMOV.20` Actions on interfaces and Object Views (`P1`, `done`)
  - Display actions defined on interfaces in Object Explorer and Object Views when the selected object implements the relevant interface.
  - Validate action rule restrictions for interface-backed edits and avoid modifying likely primary-key properties through broad interface actions.
  - Delegate execution semantics to the Ontology Actions checklist.
  - Implementation note: Object Explorer and Object Views merge interface actions inherited from implemented interfaces, while helpers flag broad interface actions that would modify likely primary keys or undeclared interface fields.
  - Docs: [Actions on interfaces](https://www.palantir.com/docs/foundry/action-types/actions-on-interfaces/), [Apply Actions in Object Explorer](https://www.palantir.com/docs/foundry/object-explorer/apply-actions/).

- [x] `OMOV.21` Multi-datasource object type mapping (`P1`, `done`)
  - Map multiple datasets or restricted views to one object type.
  - Support property-level datasource provenance and nulling properties when the viewer lacks access to a specific backing datasource.
  - Validate primary-key consistency across datasources and document unsupported row-wise MDO patterns.
  - Implementation note: Object-type bindings now expose property-level provenance/null-on-inaccessible metadata, helper validation for primary-key consistency, masking helpers for datasource access, and an object-type datasource panel that summarizes multi-datasource mappings and unsupported row-wise MDO constraints.
  - Docs: [Managing object security](https://www.palantir.com/docs/foundry/object-permissioning/managing-object-security/), [Multi-datasource object types](https://www.palantir.com/docs/foundry/object-permissioning/multi-datasource-objects/).

### Change management and history

- [x] `OMOV.22` Unsaved changes review (`P1`, `done`)
  - Track unsaved ontology changes globally and per ontology resource.
  - Show changed resource, author, timestamp, diff summary, validation status, and save readiness.
  - Allow discarding individual changes and all unsaved changes owned by the current user.
  - Implementation note: Project working-state changes now have shared review helpers and an Ontologies changes table showing resource, author, timestamp, diff summary, validation status, save readiness, per-change discard, and current-user bulk discard.
  - Docs: [Save changes to the Ontology](https://www.palantir.com/docs/foundry/ontology-manager/save-changes), [Review and restore changes](https://www.palantir.com/docs/foundry/ontology-manager/restore-changes).

- [x] `OMOV.23` Save changes to ontology (`P1`, `done`)
  - Save a coherent set of ontology changes atomically.
  - Validate API names, links, keys, datasource mappings, interface implementations, action references, permission requirements, and downstream Object View impacts before save.
  - Persist saved change records with author, timestamp, resource list, branch/proposal context, and error details.
  - Implementation note: The projects API exposes atomic save and saved-record history endpoints; saves validate staged changes, persist success/failure records with resource lists and context, and clear only saved working-state changes in the same transaction.
  - Docs: [Save changes to the Ontology](https://www.palantir.com/docs/foundry/ontology-manager/save-changes), [Review and restore changes](https://www.palantir.com/docs/foundry/ontology-manager/restore-changes).

- [x] `OMOV.24` Ontology history and restore (`P1`, `done`)
  - Show global saved-change history and per-resource history.
  - Filter history by resource type, author, time, visibility, and whether the user can view details.
  - Restore an object type or supported resource to an older version by creating a new unsaved change that must be saved to take effect.
  - Implementation note: Ontology Manager now loads project saved-change records, renders global and per-resource history with resource-type/author/time/visibility/detail-access filters, masks restricted details, and stages restore operations as unsaved working-state changes with provenance back to the saved record.
  - Docs: [Review and restore changes](https://www.palantir.com/docs/foundry/ontology-manager/restore-changes).

- [x] `OMOV.25` Export, edit, and import ontology bundles (`P1`, `done`)
  - Export selected ontology resources into an OpenFoundry-native bundle.
  - Validate edited bundles before import, including API name uniqueness, missing dependencies, unsafe deletes, permission requirements, and unsupported private fields.
  - Import as unsaved changes for review before saving.
  - Implementation note: Ontology Manager now builds OpenFoundry-native JSON bundles for selected registry resources and value types, validates edited bundles for schema support, API-name conflicts, dependency gaps, unsafe deletes, action permission declarations, private fields, and cross-ontology conditional formatting, then imports valid resources as unsaved working-state changes that must be reviewed and saved.
  - Docs: [Export, edit, and import an Ontology](https://www.palantir.com/docs/foundry/ontology-manager/export-import/), [Ontology cleanup](https://www.palantir.com/docs/foundry/ontology-manager/ontology-cleanup).

- [x] `OMOV.26` Usage and impact analysis (`P1`, `done`)
  - Show where object types, properties, link types, interfaces, actions, and Object Views are used across Workshop, Functions, Pipeline Builder, Object Explorer, saved explorations, Global Branching, and Marketplace products.
  - Warn before edits that may break downstream apps, functions, object views, or action parameters.
  - Implementation note: Ontology Manager now builds a usage-impact graph from Object Views, function-backed actions, Workshop apps, Pipeline Builder pipelines, Object Explorer saved object sets, Global Branching resource links, and Marketplace package manifests; the Usage tab summarizes references, modeled reads/writes/active users, product coverage, and risk by resource, and unsaved changes now surface downstream-impact warnings before save.
  - Docs: [Viewing usage](https://www.palantir.com/docs/foundry/ontology-manager/viewing-usage), [Object Views overview](https://www.palantir.com/docs/foundry/object-views/overview).

### Object permissions and security

- [x] `OMOV.27` Ontology resource permissions (`P1`, `done`)
  - Model ontology resources as project/folder-managed resources with view, edit, manage, and ownership semantics.
  - Enforce that viewing object type definitions differs from viewing object instances.
  - Enforce link edit permissions on both the link type and linked object types, and action edit permissions on the action type plus edited resource types.
  - Implementation note: Ontology Manager now derives per-resource permission decisions from project/folder placement, ownership, project memberships, elevated ontology roles, and object-data permissions; the Permissions tab separates object type definition visibility from object instance visibility and staged link/action edits now report every required ontology resource edit grant before save.
  - Docs: [Ontology permissions](https://www.palantir.com/docs/foundry/ontologies/ontology-permissions/), [Object permissioning overview](https://www.palantir.com/docs/foundry/object-permissioning/overview).

- [x] `OMOV.28` Object instance permission checks (`P1`, `done`)
  - Require object type visibility and backing datasource or object security policy visibility to see object instances.
  - Ensure Object Views, Object Explorer, linked-object previews, and comments never reveal object data the user cannot access.
  - Render schema-only views when the user can view definitions but not backing data.
  - Implementation note: OpenFoundry now computes object-instance visibility separately from object type definition visibility, requiring backing datasource access or explicit object security policy visibility before values render. Object Views, Object Explorer search/results, saved object-set previews, linked-object previews, actions, timelines/comments, and raw payloads now redact or skip object values in schema-only mode while preserving schema/property metadata.
  - Docs: [Object permissioning overview](https://www.palantir.com/docs/foundry/object-permissioning/overview), [Ontology permissions](https://www.palantir.com/docs/foundry/ontologies/ontology-permissions/).

- [x] `OMOV.29` Restricted-view-backed object types (`P1`, `done`)
  - Allow object types to use restricted views as backing datasources.
  - Enforce row-level policy outcomes in object search, Object Explorer, Object Views, links, and actions.
  - Track policy propagation/update requirements and warn when restricted-view policy changes require re-registration or re-indexing in local storage modes.
  - Implementation note: Object type datasource settings now support restricted-view backing metadata, local row-policy rules, storage mode/version tracking, and propagation warnings for local registrations/indexes. Object instance policy evaluation now checks restricted-view visibility and row outcomes before search results, Object Explorer rows, Object Views, linked-object previews, and action surfaces can expose object values.
  - Docs: [Configure restricted-view-backed object types](https://www.palantir.com/docs/foundry/object-permissioning/configuring-rv-access-controls/), [Managing object security](https://www.palantir.com/docs/foundry/object-permissioning/managing-object-security/).

- [x] `OMOV.30` Object and property security policies (`P1`, `done; enforcement-blocked`)
  - Support object instance policies and property policies when OpenFoundry's security/governance layer supports policy evaluation over object attributes.
  - Include read, edit property, and edit policy-property distinctions where supported.
  - Mark policy enforcement blocked until OpenFoundry has compatible policy primitives and test fixtures.
  - Implementation note: OpenFoundry now models object security policy support status, object read decisions, property read redaction, normal edit-property decisions, and policy-property edit decisions. Attribute-backed enforcement remains explicitly blocked without object-attribute policy primitives and compatible fixtures; blocked policies render schema-only or null protected properties conservatively, disable unsafe edits/actions, and surface status warnings in object type datasource/security settings.
  - Docs: [Object security policies](https://www.palantir.com/docs/foundry/object-permissioning/object-and-property-policies), [Configure restricted-view-backed object types](https://www.palantir.com/docs/foundry/object-permissioning/configuring-rv-access-controls/).

### Object Explorer-adjacent exploration

- [x] `OMOV.31` Object Explorer home and search (`P1`, `done`)
  - Provide an object search/exploration surface for simple keyword search, property filters, object type group browsing, saved explorations, and direct object view opening.
  - Show only object types and objects visible to the user.
  - Implementation note: Object Explorer now loads visible object types, configured type groups, saved object-set explorations, and recent objects through permission-aware helpers; the home surface supports keyword/semantic search, object type browsing by group, property-filtered object queries, visible saved explorations, and direct Object View opening while preserving schema-only behavior when users can view definitions but not object data.
  - Docs: [Object Explorer overview](https://www.palantir.com/docs/foundry/object-explorer/overview), [Configure Object Explorer](https://www.palantir.com/docs/foundry/object-explorer/configure), [Object type groups](https://www.palantir.com/docs/foundry/object-link-types/type-groups/).

- [x] `OMOV.32` Object set filters, pivots, and linked-object exploration (`P1`, `done`)
  - Filter by properties, linked properties, has-link predicates, numeric/date/string controls, and object references.
  - Pivot from one object type to a linked object type while retaining the selected source object set as link-derived context.
  - Implementation note: Object Explorer now builds typed property filters with numeric/date/string/boolean controls, linked-object filters for has-link, linked property, and object-reference cases, and link-aware pivots using the ontology query `search_around` contract. Pivoted or linked-filtered results carry source-object context and can be saved as OpenFoundry object-set explorations with result-ID filters plus reverse traversals back to the source object set.
  - Docs: [Filter results](https://www.palantir.com/docs/foundry/object-explorer/filter-results), [Pivot to explore linked objects](https://www.palantir.com/docs/foundry/object-explorer/pivot-linked/).

- [x] `OMOV.33` Saved explorations and object lists (`P1`, `done`)
  - Save explorations with query/filter state, layout, privacy, folder/project location, and shareable link.
  - Enforce that saved exploration access does not grant access to underlying objects.
  - Support saved lists where OpenFoundry object set persistence exists.
  - Implementation note: Object Explorer now saves explorations and object lists into Object Set persistence with query state, selected object IDs, layout columns/view, privacy, project/folder placement, and stable share links. Public saved-exploration metadata can be opened without granting object instance access; schema-only users see the saved context but preview/materialize/object rows remain blocked until datasource or object-policy visibility allows them.
  - Docs: [Save explorations](https://www.palantir.com/docs/foundry/object-explorer/save-explorations), [Object Explorer overview](https://www.palantir.com/docs/foundry/object-explorer/overview).

- [x] `OMOV.34` Object Explorer actions and open-in/export affordances (`P1`, `done`)
  - Show applicable action types for the current object selection or object set, with parameter prefill where unambiguous.
  - Show Open In affordances for compatible OpenFoundry applications and export affordances where policy allows.
  - Enforce selected-object count limits through local product configuration.
  - Implementation note: Object Explorer now has a perspective affordance panel for the active result set with Actions, Open In, and Export sections. Actions are filtered for the current object type, hidden when configured for Object Explorer hiding, prefilled only when an object-reference or object-list parameter is unambiguous, and blocked above the local 1,000-object action limit. Open In links route compatible selections to Object Views, graph, map, Workshop apps, and reports; exports provide object ID copy plus CSV/JSON downloads only when object data is viewable and within configured export limits.
  - Docs: [Apply Actions in Object Explorer](https://www.palantir.com/docs/foundry/object-explorer/apply-actions/), [Configure Object Explorer](https://www.palantir.com/docs/foundry/object-explorer/configure).

### Custom Object Views

- [x] `OMOV.35` Custom Object View default configuration (`P1`, `done`)
  - Automatically create default custom full and panel Object View configurations for each object type.
  - Default full view should include prominent properties or all non-hidden properties and links.
  - Default panel view should include critical property list content.
  - Keep defaults dynamically synchronized with object type metadata until the view is manually edited.
  - Implementation note: Object View helpers now synthesize configured full and panel defaults for every object type, using prominent properties when present, all non-hidden properties as the fallback, visible links for full views, and compact critical property lists for panel views. Generated defaults carry `default_sync` metadata tied to object type/property/link metadata and are re-synchronized while marked `synced`; editor mutations mark the configuration `manual`, preserving user-managed views and generating only missing form factors.
  - Docs: [Custom Object View configuration](https://www.palantir.com/docs/foundry/object-views/config-overview), [Configure full Object Views](https://www.palantir.com/docs/foundry/object-views/config-object-views), [Configure panel Object Views](https://www.palantir.com/docs/foundry/object-views/config-panel-views/).

- [x] `OMOV.36` Object View editor shell (`P1`, `done`)
  - Provide editor header with ontology, object type, form factor selector, Object View version, Workshop module version, selected preview object, save/publish controls, and open-in-object-explorer link.
  - Provide object title bar and manage-tabs controls for full Object Views.
  - Embed a Workshop module editor for tab/module content.
  - Implementation note: The Object Views route now renders a configured Object View editor shell with breadcrumb-style ontology/object type/form factor context, Object View and Workshop module version chips, preview-object selection, Save draft/Save and publish controls, and an Object Explorer link. Full views include an object title bar with tab selection and tab settings, while the editor body embeds a Workshop-module-shaped editor for module name, object context, and widget bindings backed by `tabs` and module metadata in the Object View config.
  - Docs: [Configure full Object Views](https://www.palantir.com/docs/foundry/object-views/config-object-views), [Custom Object View configuration](https://www.palantir.com/docs/foundry/object-views/config-overview).

- [x] `OMOV.37` Full Object View tabs (`P1`, `done`)
  - Add, reorder, rename, delete, and configure visibility for full Object View tabs.
  - Back each tab with a Workshop module that receives selected object context.
  - Hide the tab title in runtime when only one tab exists, while still showing it in edit mode.
  - Implementation note: Full Object View configs now expose tab helpers for add, reorder, rename, delete, visibility changes, and runtime tab-title evaluation. The Object View editor's Manage tabs panel supports those operations directly, preserves at least one tab, selects a sensible successor when deleting, and creates a user-managed Workshop module for every new tab with `selectedObject` context and default widgets derived from the current Object View sections.
  - Docs: [Configure full Object Views](https://www.palantir.com/docs/foundry/object-views/config-object-views).

- [x] `OMOV.38` Panel Object View configuration (`P1`, `done`)
  - Configure compact panel content separately from full view content.
  - Support platform applications and Workshop widgets embedding panel Object Views for selected objects.
  - Provide a title/open-full-view behavior that works in side panels and compact contexts.
  - Implementation note: Object View configs now carry a dedicated `panel_config` with compact property lists, section kinds, density, title/open-full-view behavior, supported embedding hosts, and Workshop widget metadata independent of full-view tab configuration. The Object Views editor exposes panel-only controls for density, property budgets, host enablement, Workshop widget selected-object bindings, and runtime open-full-view links; runtime helpers resolve panel titles and host-specific embedding state without leaking full-view tab behavior into compact panels.
  - Docs: [Use panel Object Views](https://www.palantir.com/docs/foundry/object-views/use-panel-views-in-platform), [Configure panel Object Views](https://www.palantir.com/docs/foundry/object-views/config-panel-views/), [Object Views overview](https://www.palantir.com/docs/foundry/object-views/overview).

- [x] `OMOV.39` Core/custom Object View toggle (`P1`, `done`)
  - Let users switch between core and custom Object Views wherever the hosting application supports the toggle.
  - Make custom Object Views the default when configured, while keeping core Object Views accessible.
  - In Workshop, document and enforce any local limitation when toggling is not implemented.
  - Implementation note: Object View runtime resolution now has a reusable core/custom toggle policy per host. Supported hosts such as Object Views, Object Explorer, Map, Vertex, Gaia, and detail drawers can switch between custom and core views, custom configured views become the selected default whenever present, and core views remain available as an explicit toggle option. Workshop is modeled as a local limitation: it uses the default view and disables core/custom switching until the Workshop widget host implements the toggle.
  - Docs: [Object Views overview](https://www.palantir.com/docs/foundry/object-views/overview), [Custom Object View configuration](https://www.palantir.com/docs/foundry/object-views/config-overview).

- [x] `OMOV.40` Object View save, publish, and versions (`P1`, `done`)
  - Save and publish Object View tab edits and Workshop module edits together unless automatic publishing is disabled.
  - Track Object View version, module version, author, timestamp, change summary, publish state, and rollback target.
  - Support version history and restore paths for custom Object Views.
  - Implementation note: Configured Object View configs now carry version history records with Object View version, active Workshop module version, author, timestamp, change summary, publish state, published version, snapshots, and rollback/restore provenance. Saving creates a single version for tab configuration plus the active module; automatic publishing produces one Save and publish path, while disabling it exposes separate Save draft and Publish flows. The editor's history panel lists prior versions and can restore any version as an editable draft that must be saved to become the next version.
  - Docs: [Manage custom Object View versions](https://www.palantir.com/docs/foundry/object-views/manage-versions/), [Configure full Object Views](https://www.palantir.com/docs/foundry/object-views/config-object-views).

- [x] `OMOV.41` Object View permissions (`P1`, `done`)
  - Enforce edit permissions based on object type Ontology roles or OpenFoundry's project/resource permissions.
  - Require object view admin permissions and input datasource editor permissions when using a datasource-derived compatibility mode.
  - Keep Object View runtime reads constrained by object type, object instance, property, datasource, and restricted-view permissions.
  - Implementation note: Object View configs now declare native vs datasource-derived compatibility metadata and input datasource IDs. A reusable edit-permission decision helper allows native edits through object type Ontology edit roles or resource edit permission, and requires Object View Admin/manage plus editor access to an input datasource for datasource-derived compatibility. Object View and Object Explorer runtime previews now share a single redaction path that composes object type visibility, object instance access, object/property policies, datasource-binding nulling, restricted-view row outcomes, neighbor redaction, and schema-only fallbacks.
  - Docs: [Custom Object View configuration](https://www.palantir.com/docs/foundry/object-views/config-overview), [Ontology permissions](https://www.palantir.com/docs/foundry/ontologies/ontology-permissions/).

## Milestone C: advanced Object View delivery, branching, packaging, and scale

### Object View delivery and collaboration

- [x] `OMOV.42` Object View URLs and embeds (`P2`, `done`)
  - Generate URLs by object type and primary key or by object ID.
  - Support embedded mode that hides surrounding workspace/navigation chrome for iframe-like embeds where product policy allows.
  - Preserve branch, form factor, and selected tab where supported.
  - Implementation note: Object View URL helpers now generate OpenFoundry links by object type plus primary-key property/value or by object ID, with branch, form factor, selected tab, core/custom mode, and `embedded=true` preservation. Embed host policy gates iframe-style links and `/object-views?embedded=true` hides workspace sidebar/topbar chrome while rendering a compact Object View surface. The Object Views route parses direct URL state, resolves object IDs or primary-key matches, applies branch/tab selection, and shows generated primary-key, object-ID, and embedded URL variants in the publish panel.
  - Docs: [Generate Object View URLs](https://www.palantir.com/docs/foundry/object-views/generate-urls/), [Use full Object Views](https://www.palantir.com/docs/foundry/object-views/use-full-views-in-platform).

- [x] `OMOV.43` Comments on objects (`P2`, `done`)
  - Add comments helper from Object View headers.
  - Support object-scoped comment threads, mentions, file/image attachments, permissions, notifications, edit/delete policy, and activity history.
  - Keep Object Explorer comments distinct from Workshop Comment widgets.
  - Implementation note: Object comment helpers now model object-scoped threads with explicit Object View/Object Explorer surfaces, mention parsing, file/image attachment metadata, in-app mention notifications, edit/delete permissions, and activity history. Object View headers and Object Explorer panel headers expose a Comments helper that remains hidden for schema-only object data and labels Object Explorer comments as distinct from Workshop Comment widgets, preserving a separate workshop widget thread ID for compatibility.
  - Docs: [Comment on objects](https://www.palantir.com/docs/foundry/object-views/comment-on-objects/).

- [x] `OMOV.44` Application embedding matrix (`P2`, `done`)
  - Embed full and panel Object Views in Object Explorer, Workshop, Map/Vertex-like surfaces, object detail drawers, action success toasts, and generated deep links.
  - Provide fallbacks when a host application uses its own header or cannot support core/custom toggle.
  - Implementation note: OpenFoundry now has a reusable Object View application embedding matrix that covers Object Explorer, Workshop, Map, Vertex, Gaia-like surfaces, object detail drawers, action success toasts, and generated deep links. Each host records full/panel delivery mode, host-owned header fallback behavior, core/custom toggle support, generated full/panel/embed URLs, and open-full/deep-link fallbacks. Object View publish tooling displays the matrix beside generated URLs, Object Explorer and detail drawers consume matrix-derived links, Workshop action success toasts expose Object View links for create/modify results, and panel host defaults now include Gaia-like compact selected-object surfaces.
  - Docs: [Use full Object Views](https://www.palantir.com/docs/foundry/object-views/use-full-views-in-platform), [Use panel Object Views](https://www.palantir.com/docs/foundry/object-views/use-panel-views-in-platform), [Configure Object Explorer](https://www.palantir.com/docs/foundry/object-explorer/configure).

### Branching and change isolation

- [x] `OMOV.45` Object View Global Branching adapter (`P2`, `done`)
  - Track Object View modules and tab resources on Global Branches.
  - Add, remove, preview, rebase, check, approve, and merge Object View resources through the branch adapter contract.
  - Ensure branched Object Views render against the latest ontology state on the same branch.
  - Implementation note: Object Views now expose a Global Branch adapter contract that materializes full Object View tab resources separately from OV-managed Workshop module resources, including full-tab modules and object-instance panel modules. The adapter supports add, remove, preview, rebase, deployability check, approve, and merge operations; removing a full tabs resource cascades to associated tab modules, preview records the latest same-branch ontology signature, deployability checks enforce rebase, publish permission, legacy-field, approval, and preview freshness requirements, and merge only succeeds after checks pass. The Object View publish panel surfaces branch resource status, checks, and branch-preserving resource links, while the Global Branching app can link Object View tab/module resource types.
  - Docs: [Branching object views](https://www.palantir.com/docs/foundry/object-views/branching-object-views), [Test changes in the Ontology](https://www.palantir.com/docs/foundry/ontologies/test-changes-in-ontology).

- [x] `OMOV.46` Object View rebase UX (`P2`, `done`)
  - Show main state, branch state, proposed rebase result, automatically accepted non-conflicting changes, conflicts, and manual resolution choices.
  - Handle OV-managed modules and full Object View tab configuration as distinct rebase resources when needed.
  - Re-run deployability checks after successful rebase.
  - Implementation note: Object View branching now builds a rebase-dialog model with separate rows for full Object View tab resources and OV-managed module resources, including main state, branch state, proposed result, auto-accepted non-conflicting changes, changed fields, conflict counts, and manual resolution choices (`main`, `branch`, or `custom`). The Object View publish panel renders the rebase model as a three-column comparison and allows conflict resolution before finishing rebase; finishing rebase updates branch rebase metadata and reruns deployability checks through the Object View branch adapter.
  - Docs: [Branching object views](https://www.palantir.com/docs/foundry/object-views/branching-object-views).

- [x] `OMOV.47` Ontology branch/proposal integration (`P2`, `done`)
  - Let ontology object types, link types, action types, interfaces, shared properties, and Object Views participate in Global Branching proposals.
  - Validate indexing changes and allow users to remove unwanted indexing/resource changes before merge.
  - Surface branch preview state in Ontology Manager and Object View editor.
  - Implementation note: OpenFoundry now builds a unified ontology proposal integration model for Global Branching that turns staged object type, link type, action type, interface, shared property, and Object View changes into proposal resources, review tasks, dependency checks, branch preview state, and mergeability checks. Indexing changes are validated as separate proposal items, required indexing cannot be silently removed, optional indexing/resource changes can be removed before merge, and both Ontology Manager and the Object View editor surface the branch proposal preview with Object View adapter checks included.
  - Docs: [Test changes in the Ontology](https://www.palantir.com/docs/foundry/ontologies/test-changes-in-ontology), [Branching object views](https://www.palantir.com/docs/foundry/object-views/branching-object-views).

### Marketplace and DevOps packaging

- [x] `OMOV.48` Marketplace Object View outputs (`P2`, `done`)
  - Package selected Object View tabs into OpenFoundry product outputs.
  - Support only Workshop-tab-backed Object View tabs in marketplace packaging unless a local legacy builder compatibility mode is explicitly implemented.
  - Validate dependencies on object types, Workshop modules, widgets, functions, actions, and data resources before packaging.
  - Implementation note: Object Views now build `marketplace_object_view_output` product resources from selected full-view tabs, with a manifest entry that records the Object View, object type, selected tabs, backing Workshop modules, widget IDs, dependency refs, source branch, and Workshop-tab-only compatibility mode. Packaging is blocked for non-configured/panel views, missing tabs, tabs without Workshop modules, and legacy-builder metadata unless explicit compatibility is enabled; dependency validation covers object types, Workshop modules, widgets, functions, action types, and backing data resources before the output JSON is surfaced in the Object View publish panel.
  - Docs: [Add Object Views to a Marketplace product](https://www.palantir.com/docs/foundry/object-views/marketplace-object-views).

- [x] `OMOV.49` Product install and remapping behavior (`P2`, `done`)
  - During product install, map packaged Object Views to installed or existing object types.
  - Preserve selected tabs, module dependencies, permissions, and custom view default status.
  - Provide clear failures for missing object types, unsupported tab builders, missing functions, missing actions, and unavailable widgets.
  - Implementation note: Marketplace product installs now build an Object View install plan from packaged `marketplace_object_view_output` resources, resolving each packaged object type through explicit remaps, existing IDs, or API-name matches before synthesizing installed configured views. The plan preserves selected full-view tabs, Workshop module dependency refs, object-type-managed permission semantics, and custom-view default status, while blocking install with actionable failures for missing object types, unsupported legacy/non-Workshop tabs, missing Workshop modules, missing functions/actions, and unavailable widgets. The Marketplace install panel surfaces the remap table, preserved counts, and failure list before the normal product install action can proceed.
  - Docs: [Add Object Views to a Marketplace product](https://www.palantir.com/docs/foundry/object-views/marketplace-object-views), [Custom Object View configuration](https://www.palantir.com/docs/foundry/object-views/config-overview).

### Scale, indexing, and operational quality

- [x] `OMOV.50` Ontology resource indexing and search scale (`P2`, `done`)
  - Incrementally index ontology resources, properties, links, interfaces, groups, Object Views, usage edges, and saved explorations.
  - Support pagination, type filters, project filters, group filters, fuzzy search, API-name search, and permission-aware result hiding.
  - Implementation note: Ontology Manager now builds an incremental OpenFoundry-native search index over registry resources, object type properties, link/action/interface/group/Object View entries, usage edges, and Object Explorer saved explorations/lists. The index reuses unchanged documents from the previous revision, records upserted/removed counts, exposes kind/project/group facets, and searches with pagination, resource type filters, project filters, group filters, API-name-only matching, fuzzy token matching, and permission-aware hiding. The Registry panel now uses the index directly, showing rematerialization counts, hidden-result warnings, filters, and paginated search results.
  - Docs: [Ontology Manager overview](https://www.palantir.com/docs/foundry/ontology-manager/overview/index.html), [Object type groups](https://www.palantir.com/docs/foundry/object-link-types/type-groups/), [Object Explorer overview](https://www.palantir.com/docs/foundry/object-explorer/overview).

- [x] `OMOV.51` Object View runtime performance budgets (`P2`, `done`)
  - Track query count, linked object loading, media loads, map loads, time-series loads, Workshop widget execution, and function-backed display values per Object View render.
  - Warn editors when tabs or panels exceed configured runtime budgets.
  - Cache safe metadata while never caching object data beyond the current user's permission context.
  - Implementation note: Object View configs now carry an optional `runtime_budgets` block (`per_render`, `per_tab`, `per_panel`) with sensible defaults. The ontology API exposes `measureObjectViewRuntimeUsage` (queries, linked-object loads, media, map, time-series, Workshop widget executions, function-backed display values) and `evaluateObjectViewRuntimeBudgets`, which together drive the Object View editor: the header shows a Within/Exceeded budgets chip and the run-totals strip, a dedicated panel lists each warning, tab buttons surface a per-tab badge with the count and message, and editors can edit per-render limits or disable enforcement inline. A new `ObjectViewMetadataCache` keyed by `(object_view_id, form_factor, permission_context_key)` stores only safe metadata (counts, prominent/panel property names, link type ids, section kinds, budgets) and exposes `cacheObjectViewSafeMetadata` / `getObjectViewSafeMetadata` / `invalidateObjectViewMetadataCache`; object instance data, summaries, and neighbors are never cached, so the cache never leaks data beyond the current user's permission context.
  - Docs: [Core Object Views](https://www.palantir.com/docs/foundry/object-views/core-object-views/), [Use full Object Views](https://www.palantir.com/docs/foundry/object-views/use-full-views-in-platform), [Use panel Object Views](https://www.palantir.com/docs/foundry/object-views/use-panel-views-in-platform).

- [x] `OMOV.52` Ontology cleanup assistant (`P2`, `done`)
  - Identify unused object types, properties, link types, groups, interfaces, Object Views, legacy Object View fragments, and orphaned Workshop modules.
  - Require usage-impact review and explicit confirmation before cleanup actions.
  - Convert cleanup actions into unsaved changes or branch proposal changes before applying.
  - Implementation note: The ontology API now exposes `buildOntologyCleanupAssistant`, which fans out across `objectTypes`, `linkTypes`, `interfaces`, `sharedPropertyTypes`, `valueTypes`, `objectViews`, and `objectTypeGroups`, reusing `buildOntologyUsageImpactAnalysis` to detect unused object types, unused non-primary/non-title properties, unused link/shared/value types, empty groups, orphan custom Object Views, unreferenced configured Object Views, legacy Object View fragments (`legacy_builder` / `legacy_fields_modified`), and orphan Workshop modules embedded in user-managed tabs with no widgets. Each `OntologyCleanupCandidate` carries severity, usage_count, reference_summary, warnings, and `delete_supported` flags. `createOntologyCleanupStagedChanges` requires explicit `confirmed: true` and selected ids before emitting any staged change; otherwise it returns `confirmation_required: true` and zero changes. Emitted staged changes are `action: "delete"`, `source: "ontology_cleanup_assistant"`, run through `reviewUnsavedOntologyChanges`, and flow into the existing project working-state → branch proposal pipeline via `buildOntologyBranchProposalIntegration` so they appear as proposal resources. The Ontology Manager `cleanup` section is now a real `CleanupAssistantPanel` (replacing the placeholder) that lists candidates grouped by kind with severity/usage chips, requires an explicit "I have reviewed the downstream usage impact" checkbox before staging, and posts the staged changes via `replaceProjectWorkingState` so they show up in the Unsaved changes review.
  - Docs: [Ontology cleanup](https://www.palantir.com/docs/foundry/ontology-manager/ontology-cleanup), [Viewing usage](https://www.palantir.com/docs/foundry/ontology-manager/viewing-usage).

- [x] `OMOV.53` Audit, metrics, and health panels (`P2`, `done`)
  - Emit audit events for ontology resource CRUD, datasource mapping changes, Object View edits, publish events, imports, exports, restores, branch rebases, marketplace packaging, and permission changes.
  - Show operational panels for stale datasources, broken links, failed Object View widget loads, inaccessible backing data, indexing lag, missing value type validation, and permission mismatches.
  - Implementation note: The ontology API now exposes `buildOntologyAuditEventLog`, which synthesizes a unified audit timeline (`OntologyAuditEvent[]`) from `OntologySavedChangeRecord` entries, pending `OntologyStagedChange` working changes, Object View `version_history` publish entries, `metadata.branch_rebased_at` rebases, marketplace packaging outputs, and permission-bearing change payloads. Each event carries category (`resource_crud`, `datasource_mapping`, `object_view_edit`, `object_view_publish`, `import`, `export`, `restore`, `branch_rebase`, `marketplace_packaging`, `permission_change`), status (`saved`, `pending`, `failed`, `info`), actor, timestamp, source, and a stable `resource_kind/resource_id` pair. A second function, `buildOntologyHealthReport`, runs seven operational detectors and emits `OntologyHealthIssue[]` with `OntologyHealthSeverity` (`info`/`warning`/`critical`) and remediation guidance: stale datasources (object types with backing dataset and `updated_at` older than the threshold), broken links (source/target object type missing from the catalog), widget load failures (empty visible tabs, legacy builder, runtime failures supplied via `widgetFailures` input), inaccessible backing data (no binding configured, or principal can view definition but not instances), indexing lag (`restricted_view_policy_version` ahead of `restricted_view_indexed_policy_version`), missing value type validation (`property.value_type_id` not present in the active value type list), and permission mismatches (owner without edit rights, blocked staged changes from `OntologyPermissionAnalysis.change_checks`). Both builders are pure and feed a new "Audit & health" section in `OntologyManagerPage`, with filters by category/status/actor for the timeline and category-card click-throughs plus severity filtering for the health panel; the nav badge totals are wired into `shellNavCount` so the section surface mirrors how many events + issues are currently active.
  - Docs: [Review and restore changes](https://www.palantir.com/docs/foundry/ontology-manager/restore-changes), [Viewing usage](https://www.palantir.com/docs/foundry/ontology-manager/viewing-usage), [Object Views overview](https://www.palantir.com/docs/foundry/object-views/overview).

## Milestone D: staged edits, function types, derived properties, monitors, schema versioning

> **Added 2026-05-17.** Closes the gaps that make the ontology a real
> semantic platform rather than a CRUD layer: staged edits with human
> review, function types as first-class contracts, derived (computed)
> properties, monitors/triggers, and full ontology schema versioning.

### Staged edits and Function-Type-backed Actions

- [ ] `OMOV.27` Staged-edit substrate (`P1`, `todo`)
  - Action and Function executions accumulate writes as a `staged_edit_set` with object-level diffs; nothing materializes until `commit()`.
  - Staged sets are addressable resources (Compass RID) with markings, owner, expiration, and audit.
  - Required by Function-backed Actions (see [Functions runtime checklist](./foundry-functions-runtime-1to1-checklist.md)).
  - Docs: [Staged writes](https://palantir.com/docs/foundry/functions/staged-writes).

- [ ] `OMOV.28` Reviewer workflow for staged edits (`P1`, `todo`)
  - Action types may declare `requires_review: true`; staged edits surface in a reviewer queue with diff view and reviewer comments.
  - Approve/reject decisions audited; reject returns rationale to the original caller.
  - Docs: [Staged writes](https://palantir.com/docs/foundry/functions/staged-writes).

### Function types as first-class contracts

- [ ] `OMOV.29` Function type binding for Actions (`P1`, `todo`)
  - Action types may bind their behavior to a function type version (see [Functions runtime](./foundry-functions-runtime-1to1-checklist.md)); compatibility checks gate publication.
  - The ontology surface lists which action types are function-backed and their current implementation pointer per environment.
  - Docs: [Function-backed actions](https://palantir.com/docs/foundry/functions/function-backed-actions).

- [ ] `OMOV.30` Function type browse from Ontology Manager (`P1`, `todo`)
  - Function types are visible in the ontology manager catalog, filterable by signature and side-effect declaration, with deep links to the Functions runtime UI.
  - Docs: [Function types](https://palantir.com/docs/foundry/functions/function-types).

### Derived (computed) properties

- [ ] `OMOV.31` Derived property definition (`P1`, `todo`)
  - Object type properties may declare a derivation expression: a function type call, a constant expression, an aggregation over linked objects, or a SQL-like projection.
  - Derivations evaluated on read by default; expensive ones can be marked `materialized` and recomputed by the indexer.
  - Docs: [Derived properties](https://palantir.com/docs/foundry/ontology/derived-properties).

- [ ] `OMOV.32` Derived property dependency tracking (`P1`, `todo`)
  - The schema records which source properties each derived property depends on; updates to sources mark derived values stale.
  - Object Storage V2 invalidates materialized derived values incrementally.
  - Docs: [Derived properties](https://palantir.com/docs/foundry/ontology/derived-properties).

### Monitors and triggers

- [ ] `OMOV.33` Object monitor resource (`P1`, `todo`)
  - `object_monitor` rows attached to an object type with: trigger (object created/updated/deleted, property crossed threshold, link added/removed), filter predicate, evaluation cadence, action to run (function/action invocation, notification, automation rule).
  - Stable RID, Compass-discoverable.
  - Docs: [Object monitors](https://palantir.com/docs/foundry/ontology/monitors).

- [ ] `OMOV.34` Monitor evaluation engine (`P1`, `todo`)
  - Continuous (event-driven) and scheduled (cron) evaluation backends; idempotent firing with dedup window.
  - Per-monitor audit of every firing with input, decision, action result.
  - Docs: [Object monitors](https://palantir.com/docs/foundry/ontology/monitors).

- [ ] `OMOV.35` Monitor backpressure and quota (`P2`, `todo`)
  - Hard cap on firings per minute per monitor; soft-throttle with notification when nearing cap.
  - Per-project monitor count quota.
  - Docs: [Object monitors](https://palantir.com/docs/foundry/ontology/monitors).

### Ontology schema versioning

- [ ] `OMOV.36` Ontology version resource (`P1`, `todo`)
  - Every published change to ontology types/properties/links/actions/interfaces produces an immutable `ontology_version` with content hash, author, timestamp, changelog.
  - OSDK generation (see [OSDK checklist](./foundry-osdk-1to1-checklist.md)) and Functions runtime pin to a specific ontology version.
  - Docs: [Ontology versioning](https://palantir.com/docs/foundry/ontology/versioning).

- [ ] `OMOV.37` Rollback to prior ontology version (`P1`, `todo`)
  - Admin can roll back to a prior published version; dependent OSDKs and apps are listed before commit and the rollback is recorded with audit.
  - Forward-incompatible rollbacks (data already written in the new shape) are blocked unless a migration is provided.
  - Docs: [Ontology versioning](https://palantir.com/docs/foundry/ontology/versioning).

- [ ] `OMOV.38` Ontology version diff view (`P2`, `todo`)
  - UI diff between any two versions showing added/removed/changed types, properties, links, actions, and the impact on dependent resources.
  - Docs: [Ontology versioning](https://palantir.com/docs/foundry/ontology/versioning).

## Implementation inventory to collect before coding

- [ ] `INV.1` Identify existing OpenFoundry ontology definition models for object types, properties, links, actions, interfaces, shared properties, and groups.
- [ ] `INV.2` Identify existing Ontology Manager, Ontology Design, Object Explorer, object detail drawer, and Object View frontend routes/components.
- [ ] `INV.3` Identify existing datasource mapping, schema mapper, dataset versioning, restricted view, and multi-datasource support.
- [ ] `INV.4` Identify existing object query, object set, linked-object traversal, search, aggregation, and object action invocation APIs.
- [ ] `INV.5` Identify existing permission, marking, organization, project, folder, and resource-role primitives that can protect ontology resources and object instances.
- [ ] `INV.6` Identify existing Workshop module/widget persistence that can back custom Object View full tabs and panel form factors.
- [ ] `INV.7` Identify existing media, time series, geospatial, map, chart, function, and action widgets that core/custom Object Views can render.
- [ ] `INV.8` Identify existing comments, notifications, mentions, file attachments, audit, and activity feed primitives for object comments.
- [ ] `INV.9` Identify existing Global Branching branch adapter hooks for Object Views and ontology resources.
- [ ] `INV.10` Identify existing DevOps/Marketplace product packaging models that can include Object View tabs and dependencies.
- [ ] `INV.11` Identify legacy ontology editor, legacy Object View builder, and import/export compatibility surfaces that should be read-only, migratable, or blocked.
- [ ] `INV.12` Produce a machine-readable parity matrix sibling JSON after inventory, following the pattern of [foundry-feature-parity-matrix.json](./foundry-feature-parity-matrix.json).

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
| `ontology-definition-service` | Ontology resource CRUD, object/link/interface/shared property/group models, datasource mappings, change management, history, restore, import/export, cleanup planning. |
| `ontology-query-service` | Object reads, search, object sets, linked-object traversal, aggregations, saved explorations, permission-aware Object Explorer queries. |
| `object-database-service` | Object materialization, link materialization, object instance access checks, MDO/restricted-view projection, indexing state. |
| `ontology-actions-service` | Action type references in Object Views/Object Explorer, interface action eligibility, action success object-view links, object edit security handoffs. |
| `workshop service` | Custom Object View full/panel modules, tab-backed Workshop module editing, object context variables, save/publish/version behavior. |
| `object-view service` | Core Object View generation, custom Object View configuration, form factors, runtime rendering contract, Object View URLs, comments integration, branch adapter. |
| `global-branch-service` | Branch-scoped ontology/Object View resources, proposal participation, preview status, rebase, merge checks, deployability checks. |
| `security/governance service` | Ontology resource permissions, object instance policies, restricted-view policy integration, property-level policy decisions, audit of security-affecting edits. |
| `devops/marketplace service` | Product packaging for Object View tabs, dependency analysis, install/remap behavior, marketplace validation. |
| `apps/web` | Ontology Manager UI, Object Explorer UI, Object View renderer/editor, object detail drawer, comments helper, branch/rebase UI, marketplace packaging UI. |

## Acceptance criteria for first complete Ontology Manager and Object Views milestone

- [ ] A user can create an ontology-scoped object type from a datasource, configure metadata, map properties, select primary/title keys, save changes, and view generated objects.
- [ ] A user can create link types, navigate linked object graphs, and inspect linked objects from an object detail view.
- [ ] A user can create object type groups and see them in Ontology Manager search/filtering and Object Explorer home navigation.
- [ ] Core Object Views are generated for every object type and render full and panel form factors with prominent properties, normal properties, hidden-property behavior, and linked objects.
- [ ] Object Views enforce object type, backing datasource, restricted-view, and property visibility rules.
- [ ] A user can configure a custom full Object View tab backed by a Workshop module, preview it against a selected object, save/publish it, and toggle back to the core view where supported.
- [ ] A user can configure a panel Object View and see it embedded in at least one object detail side panel.
- [ ] Ontology Manager records unsaved changes, saved history, per-resource history, and restore-as-unsaved-change behavior.
- [ ] Object Explorer can search/filter an object set, pivot through links, save an exploration, open Object Views, and invoke an applicable action with selected-object prefill.
- [ ] Object View URLs can deep-link to an object by object ID or primary-key route and can render embedded mode when allowed.
- [ ] Object View comments can be added from the Object View header without confusing them with Workshop comment widgets.
- [ ] All OpenFoundry runtime UI is OpenFoundry-native and does not use Palantir branding, screenshots, icons, fonts, or proprietary assets.

## Test plan expectations

- Unit tests for ontology resource validation, API-name rules, property mapping, primary/title key eligibility, property formatting metadata, value type constraints, shared property usage, link cardinality, interface implementation validation, Object View generation, and permission decisions.
- API tests for ontology CRUD, object type CRUD, property CRUD, link type CRUD, group CRUD, interface CRUD, datasource mappings, change save/history/restore, import/export validation, Object View configuration/versioning, Object View URL resolution, comments, and saved explorations.
- Integration tests for datasource-backed object type materialization, restricted-view-backed object visibility, MDO property nulling, Object Explorer filtering/pivots, Object View runtime rendering, Workshop-backed Object View editing, action prefill from Object Explorer, and Object View branch adapter behavior.
- E2E tests for object type creation helper, property editor, link type graph navigation, core Object View rendering, custom Object View full tab editing, panel Object View embedding, object comments, Object Explorer saved exploration, and ontology history restore.
- Regression tests proving hidden properties, restricted objects, MDO-inaccessible properties, and branch-only Object View changes cannot leak to unauthorized users or main runtime views.
