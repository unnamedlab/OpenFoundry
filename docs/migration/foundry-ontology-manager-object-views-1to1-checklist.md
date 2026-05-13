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

- [ ] `OMOV.10` Object type groups (`P0`, `todo`)
  - Create, edit, delete, search, and permission object type groups.
  - Add/remove groups from object type metadata.
  - Show groups in Ontology Manager search/filtering and Object Explorer home pages.
  - Docs: [Object type groups](https://www.palantir.com/docs/foundry/object-link-types/type-groups/), [Configure Object Explorer](https://www.palantir.com/docs/foundry/object-explorer/configure).

- [ ] `OMOV.11` Object detail drawer and object type graph (`P0`, `todo`)
  - From object type pages, show a graph of linked object types and link types.
  - Selecting a link from the graph should open the link type detail with overview and datasource tabs.
  - Object instance detail drawers should show title, primary key, prominent properties, normal properties, linked objects, and available actions.
  - Docs: [Ontology Manager overview](https://www.palantir.com/docs/foundry/ontology-manager/overview/index.html), [Core Object Views](https://www.palantir.com/docs/foundry/object-views/core-object-views/), [Link types overview](https://www.palantir.com/docs/foundry/object-link-types/link-types-overview).

### Core Object Views

- [ ] `OMOV.12` Automatic core Object View generation (`P0`, `todo`)
  - Generate full and panel core Object Views for every object type.
  - Render title, primary key, prominent properties, normal properties, non-hidden linked objects, and metadata from the current object type configuration.
  - Keep core Object Views available even when a custom Object View exists.
  - Docs: [Object Views overview](https://www.palantir.com/docs/foundry/object-views/overview), [Core Object Views](https://www.palantir.com/docs/foundry/object-views/core-object-views/).

- [ ] `OMOV.13` Core prominent property display (`P0`, `todo`)
  - Render media reference properties with a media viewer, time series properties as charts, geospatial/geotemporal properties on maps, and other prominent properties as elevated cards.
  - Render normal properties in a table and hide hidden properties.
  - Degrade gracefully when media, time series, or map subsystems are not installed.
  - Docs: [Core Object Views](https://www.palantir.com/docs/foundry/object-views/core-object-views/).

- [ ] `OMOV.14` Core linked objects component (`P0`, `todo`)
  - Group linked objects by link type.
  - Preview linked object properties inline.
  - Open a subset of linked objects in a new tab or exploration.
  - Preview a selected linked object in the side panel.
  - Docs: [Core Object Views](https://www.palantir.com/docs/foundry/object-views/core-object-views/), [Pivot to explore linked objects](https://www.palantir.com/docs/foundry/object-explorer/pivot-linked/).

- [ ] `OMOV.15` Object View form factors (`P0`, `todo`)
  - Support full Object Views for comprehensive object detail.
  - Support panel Object Views for compact display inside maps, graphs, Workshop, Object Explorer, and other applications.
  - Provide consistent object title behavior and a way to open a full Object View from panel contexts.
  - Docs: [Object Views overview](https://www.palantir.com/docs/foundry/object-views/overview), [Use full Object Views](https://www.palantir.com/docs/foundry/object-views/use-full-views-in-platform), [Use panel Object Views](https://www.palantir.com/docs/foundry/object-views/use-panel-views-in-platform).

## Milestone B: credible Foundry-style Ontology Manager and Object Views parity

### Advanced ontology modeling

- [ ] `OMOV.16` Shared properties (`P1`, `todo`)
  - Create and manage shared property types that can be reused by multiple object types and interfaces.
  - Centralize display name, API name, description, base type, value type, formatting, and usage discovery.
  - Warn when edits to a shared property affect multiple object types.
  - Docs: [Types reference](https://www.palantir.com/docs/foundry/object-link-types/type-reference), [Properties overview](https://www.palantir.com/docs/foundry/object-link-types/properties-overview/).

- [ ] `OMOV.17` Value types (`P1`, `todo`)
  - Create, version, permission, search, and apply value types within a space.
  - Support semantic metadata, validation constraints, formatting, non-breaking edits, breaking edits, and usage discovery.
  - Enforce value type constraints in property mappings, Pipeline Builder validation, ontology indexing, Object Views, and user edits where applicable.
  - Docs: [Value types overview](https://www.palantir.com/docs/foundry/object-link-types/value-types-overview/).

- [ ] `OMOV.18` Interfaces (`P1`, `todo`)
  - Create interface types with interface properties, display metadata, implementation requirements, and implementing object type lists.
  - Implement interfaces on object types with explicit property mappings.
  - Support interface extension/inheritance where documented and locally modeled.
  - Docs: [Interfaces overview](https://www.palantir.com/docs/foundry/interfaces/interface-overview/), [Create an interface](https://www.palantir.com/docs/foundry/interfaces/create-interface), [Implement an interface](https://www.palantir.com/docs/foundry/interfaces/implement-interface), [Extend an interface](https://www.palantir.com/docs/foundry/interfaces/extend-interface).

- [ ] `OMOV.19` Interface link type constraints (`P1`, `todo`)
  - Define interface link constraints with link target type, target object/interface, cardinality, required flag, description, and API name.
  - Validate that implementing object types provide concrete link types satisfying required constraints.
  - Expose interface link APIs in object search and Object Views when backed by a concrete implementation.
  - Docs: [Interface link type constraints](https://www.palantir.com/docs/foundry/interfaces/interface-link-types-overview), [Edit interface implementation](https://www.palantir.com/docs/foundry/interfaces/edit-interface-implementation).

- [ ] `OMOV.20` Actions on interfaces and Object Views (`P1`, `todo`)
  - Display actions defined on interfaces in Object Explorer and Object Views when the selected object implements the relevant interface.
  - Validate action rule restrictions for interface-backed edits and avoid modifying likely primary-key properties through broad interface actions.
  - Delegate execution semantics to the Ontology Actions checklist.
  - Docs: [Actions on interfaces](https://www.palantir.com/docs/foundry/action-types/actions-on-interfaces/), [Apply Actions in Object Explorer](https://www.palantir.com/docs/foundry/object-explorer/apply-actions/).

- [ ] `OMOV.21` Multi-datasource object type mapping (`P1`, `todo`)
  - Map multiple datasets or restricted views to one object type.
  - Support property-level datasource provenance and nulling properties when the viewer lacks access to a specific backing datasource.
  - Validate primary-key consistency across datasources and document unsupported row-wise MDO patterns.
  - Docs: [Managing object security](https://www.palantir.com/docs/foundry/object-permissioning/managing-object-security/), [Multi-datasource object types](https://www.palantir.com/docs/foundry/object-permissioning/multi-datasource-objects/).

### Change management and history

- [ ] `OMOV.22` Unsaved changes review (`P1`, `todo`)
  - Track unsaved ontology changes globally and per ontology resource.
  - Show changed resource, author, timestamp, diff summary, validation status, and save readiness.
  - Allow discarding individual changes and all unsaved changes owned by the current user.
  - Docs: [Save changes to the Ontology](https://www.palantir.com/docs/foundry/ontology-manager/save-changes), [Review and restore changes](https://www.palantir.com/docs/foundry/ontology-manager/restore-changes).

- [ ] `OMOV.23` Save changes to ontology (`P1`, `todo`)
  - Save a coherent set of ontology changes atomically.
  - Validate API names, links, keys, datasource mappings, interface implementations, action references, permission requirements, and downstream Object View impacts before save.
  - Persist saved change records with author, timestamp, resource list, branch/proposal context, and error details.
  - Docs: [Save changes to the Ontology](https://www.palantir.com/docs/foundry/ontology-manager/save-changes), [Review and restore changes](https://www.palantir.com/docs/foundry/ontology-manager/restore-changes).

- [ ] `OMOV.24` Ontology history and restore (`P1`, `todo`)
  - Show global saved-change history and per-resource history.
  - Filter history by resource type, author, time, visibility, and whether the user can view details.
  - Restore an object type or supported resource to an older version by creating a new unsaved change that must be saved to take effect.
  - Docs: [Review and restore changes](https://www.palantir.com/docs/foundry/ontology-manager/restore-changes).

- [ ] `OMOV.25` Export, edit, and import ontology bundles (`P1`, `todo`)
  - Export selected ontology resources into an OpenFoundry-native bundle.
  - Validate edited bundles before import, including API name uniqueness, missing dependencies, unsafe deletes, permission requirements, and unsupported private fields.
  - Import as unsaved changes for review before saving.
  - Docs: [Export, edit, and import an Ontology](https://www.palantir.com/docs/foundry/ontology-manager/export-import/), [Ontology cleanup](https://www.palantir.com/docs/foundry/ontology-manager/ontology-cleanup).

- [ ] `OMOV.26` Usage and impact analysis (`P1`, `todo`)
  - Show where object types, properties, link types, interfaces, actions, and Object Views are used across Workshop, Functions, Pipeline Builder, Object Explorer, saved explorations, Global Branching, and Marketplace products.
  - Warn before edits that may break downstream apps, functions, object views, or action parameters.
  - Docs: [Viewing usage](https://www.palantir.com/docs/foundry/ontology-manager/viewing-usage), [Object Views overview](https://www.palantir.com/docs/foundry/object-views/overview).

### Object permissions and security

- [ ] `OMOV.27` Ontology resource permissions (`P1`, `todo`)
  - Model ontology resources as project/folder-managed resources with view, edit, manage, and ownership semantics.
  - Enforce that viewing object type definitions differs from viewing object instances.
  - Enforce link edit permissions on both the link type and linked object types, and action edit permissions on the action type plus edited resource types.
  - Docs: [Ontology permissions](https://www.palantir.com/docs/foundry/ontologies/ontology-permissions/), [Object permissioning overview](https://www.palantir.com/docs/foundry/object-permissioning/overview).

- [ ] `OMOV.28` Object instance permission checks (`P1`, `todo`)
  - Require object type visibility and backing datasource or object security policy visibility to see object instances.
  - Ensure Object Views, Object Explorer, linked-object previews, and comments never reveal object data the user cannot access.
  - Render schema-only views when the user can view definitions but not backing data.
  - Docs: [Object permissioning overview](https://www.palantir.com/docs/foundry/object-permissioning/overview), [Ontology permissions](https://www.palantir.com/docs/foundry/ontologies/ontology-permissions/).

- [ ] `OMOV.29` Restricted-view-backed object types (`P1`, `todo`)
  - Allow object types to use restricted views as backing datasources.
  - Enforce row-level policy outcomes in object search, Object Explorer, Object Views, links, and actions.
  - Track policy propagation/update requirements and warn when restricted-view policy changes require re-registration or re-indexing in local storage modes.
  - Docs: [Configure restricted-view-backed object types](https://www.palantir.com/docs/foundry/object-permissioning/configuring-rv-access-controls/), [Managing object security](https://www.palantir.com/docs/foundry/object-permissioning/managing-object-security/).

- [ ] `OMOV.30` Object and property security policies (`P1`, `blocked`)
  - Support object instance policies and property policies when OpenFoundry's security/governance layer supports policy evaluation over object attributes.
  - Include read, edit property, and edit policy-property distinctions where supported.
  - Mark policy enforcement blocked until OpenFoundry has compatible policy primitives and test fixtures.
  - Docs: [Object security policies](https://www.palantir.com/docs/foundry/object-permissioning/object-and-property-policies), [Configure restricted-view-backed object types](https://www.palantir.com/docs/foundry/object-permissioning/configuring-rv-access-controls/).

### Object Explorer-adjacent exploration

- [ ] `OMOV.31` Object Explorer home and search (`P1`, `todo`)
  - Provide an object search/exploration surface for simple keyword search, property filters, object type group browsing, saved explorations, and direct object view opening.
  - Show only object types and objects visible to the user.
  - Docs: [Object Explorer overview](https://www.palantir.com/docs/foundry/object-explorer/overview), [Configure Object Explorer](https://www.palantir.com/docs/foundry/object-explorer/configure), [Object type groups](https://www.palantir.com/docs/foundry/object-link-types/type-groups/).

- [ ] `OMOV.32` Object set filters, pivots, and linked-object exploration (`P1`, `todo`)
  - Filter by properties, linked properties, has-link predicates, numeric/date/string controls, and object references.
  - Pivot from one object type to a linked object type while retaining the selected source object set as link-derived context.
  - Docs: [Filter results](https://www.palantir.com/docs/foundry/object-explorer/filter-results), [Pivot to explore linked objects](https://www.palantir.com/docs/foundry/object-explorer/pivot-linked/).

- [ ] `OMOV.33` Saved explorations and object lists (`P1`, `todo`)
  - Save explorations with query/filter state, layout, privacy, folder/project location, and shareable link.
  - Enforce that saved exploration access does not grant access to underlying objects.
  - Support saved lists where OpenFoundry object set persistence exists.
  - Docs: [Save explorations](https://www.palantir.com/docs/foundry/object-explorer/save-explorations), [Object Explorer overview](https://www.palantir.com/docs/foundry/object-explorer/overview).

- [ ] `OMOV.34` Object Explorer actions and open-in/export affordances (`P1`, `todo`)
  - Show applicable action types for the current object selection or object set, with parameter prefill where unambiguous.
  - Show Open In affordances for compatible OpenFoundry applications and export affordances where policy allows.
  - Enforce selected-object count limits through local product configuration.
  - Docs: [Apply Actions in Object Explorer](https://www.palantir.com/docs/foundry/object-explorer/apply-actions/), [Configure Object Explorer](https://www.palantir.com/docs/foundry/object-explorer/configure).

### Custom Object Views

- [ ] `OMOV.35` Custom Object View default configuration (`P1`, `todo`)
  - Automatically create default custom full and panel Object View configurations for each object type.
  - Default full view should include prominent properties or all non-hidden properties and links.
  - Default panel view should include critical property list content.
  - Keep defaults dynamically synchronized with object type metadata until the view is manually edited.
  - Docs: [Custom Object View configuration](https://www.palantir.com/docs/foundry/object-views/config-overview), [Configure full Object Views](https://www.palantir.com/docs/foundry/object-views/config-object-views), [Configure panel Object Views](https://www.palantir.com/docs/foundry/object-views/config-panel-views/).

- [ ] `OMOV.36` Object View editor shell (`P1`, `todo`)
  - Provide editor header with ontology, object type, form factor selector, Object View version, Workshop module version, selected preview object, save/publish controls, and open-in-object-explorer link.
  - Provide object title bar and manage-tabs controls for full Object Views.
  - Embed a Workshop module editor for tab/module content.
  - Docs: [Configure full Object Views](https://www.palantir.com/docs/foundry/object-views/config-object-views), [Custom Object View configuration](https://www.palantir.com/docs/foundry/object-views/config-overview).

- [ ] `OMOV.37` Full Object View tabs (`P1`, `todo`)
  - Add, reorder, rename, delete, and configure visibility for full Object View tabs.
  - Back each tab with a Workshop module that receives selected object context.
  - Hide the tab title in runtime when only one tab exists, while still showing it in edit mode.
  - Docs: [Configure full Object Views](https://www.palantir.com/docs/foundry/object-views/config-object-views).

- [ ] `OMOV.38` Panel Object View configuration (`P1`, `todo`)
  - Configure compact panel content separately from full view content.
  - Support platform applications and Workshop widgets embedding panel Object Views for selected objects.
  - Provide a title/open-full-view behavior that works in side panels and compact contexts.
  - Docs: [Use panel Object Views](https://www.palantir.com/docs/foundry/object-views/use-panel-views-in-platform), [Configure panel Object Views](https://www.palantir.com/docs/foundry/object-views/config-panel-views/), [Object Views overview](https://www.palantir.com/docs/foundry/object-views/overview).

- [ ] `OMOV.39` Core/custom Object View toggle (`P1`, `todo`)
  - Let users switch between core and custom Object Views wherever the hosting application supports the toggle.
  - Make custom Object Views the default when configured, while keeping core Object Views accessible.
  - In Workshop, document and enforce any local limitation when toggling is not implemented.
  - Docs: [Object Views overview](https://www.palantir.com/docs/foundry/object-views/overview), [Custom Object View configuration](https://www.palantir.com/docs/foundry/object-views/config-overview).

- [ ] `OMOV.40` Object View save, publish, and versions (`P1`, `todo`)
  - Save and publish Object View tab edits and Workshop module edits together unless automatic publishing is disabled.
  - Track Object View version, module version, author, timestamp, change summary, publish state, and rollback target.
  - Support version history and restore paths for custom Object Views.
  - Docs: [Manage custom Object View versions](https://www.palantir.com/docs/foundry/object-views/manage-versions/), [Configure full Object Views](https://www.palantir.com/docs/foundry/object-views/config-object-views).

- [ ] `OMOV.41` Object View permissions (`P1`, `todo`)
  - Enforce edit permissions based on object type Ontology roles or OpenFoundry's project/resource permissions.
  - Require object view admin permissions and input datasource editor permissions when using a datasource-derived compatibility mode.
  - Keep Object View runtime reads constrained by object type, object instance, property, datasource, and restricted-view permissions.
  - Docs: [Custom Object View configuration](https://www.palantir.com/docs/foundry/object-views/config-overview), [Ontology permissions](https://www.palantir.com/docs/foundry/ontologies/ontology-permissions/).

## Milestone C: advanced Object View delivery, branching, packaging, and scale

### Object View delivery and collaboration

- [ ] `OMOV.42` Object View URLs and embeds (`P2`, `todo`)
  - Generate URLs by object type and primary key or by object ID.
  - Support embedded mode that hides surrounding workspace/navigation chrome for iframe-like embeds where product policy allows.
  - Preserve branch, form factor, and selected tab where supported.
  - Docs: [Generate Object View URLs](https://www.palantir.com/docs/foundry/object-views/generate-urls/), [Use full Object Views](https://www.palantir.com/docs/foundry/object-views/use-full-views-in-platform).

- [ ] `OMOV.43` Comments on objects (`P2`, `todo`)
  - Add comments helper from Object View headers.
  - Support object-scoped comment threads, mentions, file/image attachments, permissions, notifications, edit/delete policy, and activity history.
  - Keep Object Explorer comments distinct from Workshop Comment widgets.
  - Docs: [Comment on objects](https://www.palantir.com/docs/foundry/object-views/comment-on-objects/).

- [ ] `OMOV.44` Application embedding matrix (`P2`, `todo`)
  - Embed full and panel Object Views in Object Explorer, Workshop, Map/Vertex-like surfaces, object detail drawers, action success toasts, and generated deep links.
  - Provide fallbacks when a host application uses its own header or cannot support core/custom toggle.
  - Docs: [Use full Object Views](https://www.palantir.com/docs/foundry/object-views/use-full-views-in-platform), [Use panel Object Views](https://www.palantir.com/docs/foundry/object-views/use-panel-views-in-platform), [Configure Object Explorer](https://www.palantir.com/docs/foundry/object-explorer/configure).

### Branching and change isolation

- [ ] `OMOV.45` Object View Global Branching adapter (`P2`, `todo`)
  - Track Object View modules and tab resources on Global Branches.
  - Add, remove, preview, rebase, check, approve, and merge Object View resources through the branch adapter contract.
  - Ensure branched Object Views render against the latest ontology state on the same branch.
  - Docs: [Branching object views](https://www.palantir.com/docs/foundry/object-views/branching-object-views), [Test changes in the Ontology](https://www.palantir.com/docs/foundry/ontologies/test-changes-in-ontology).

- [ ] `OMOV.46` Object View rebase UX (`P2`, `todo`)
  - Show main state, branch state, proposed rebase result, automatically accepted non-conflicting changes, conflicts, and manual resolution choices.
  - Handle OV-managed modules and full Object View tab configuration as distinct rebase resources when needed.
  - Re-run deployability checks after successful rebase.
  - Docs: [Branching object views](https://www.palantir.com/docs/foundry/object-views/branching-object-views).

- [ ] `OMOV.47` Ontology branch/proposal integration (`P2`, `todo`)
  - Let ontology object types, link types, action types, interfaces, shared properties, and Object Views participate in Global Branching proposals.
  - Validate indexing changes and allow users to remove unwanted indexing/resource changes before merge.
  - Surface branch preview state in Ontology Manager and Object View editor.
  - Docs: [Test changes in the Ontology](https://www.palantir.com/docs/foundry/ontologies/test-changes-in-ontology), [Branching object views](https://www.palantir.com/docs/foundry/object-views/branching-object-views).

### Marketplace and DevOps packaging

- [ ] `OMOV.48` Marketplace Object View outputs (`P2`, `todo`)
  - Package selected Object View tabs into OpenFoundry product outputs.
  - Support only Workshop-tab-backed Object View tabs in marketplace packaging unless a local legacy builder compatibility mode is explicitly implemented.
  - Validate dependencies on object types, Workshop modules, widgets, functions, actions, and data resources before packaging.
  - Docs: [Add Object Views to a Marketplace product](https://www.palantir.com/docs/foundry/object-views/marketplace-object-views).

- [ ] `OMOV.49` Product install and remapping behavior (`P2`, `todo`)
  - During product install, map packaged Object Views to installed or existing object types.
  - Preserve selected tabs, module dependencies, permissions, and custom view default status.
  - Provide clear failures for missing object types, unsupported tab builders, missing functions, missing actions, and unavailable widgets.
  - Docs: [Add Object Views to a Marketplace product](https://www.palantir.com/docs/foundry/object-views/marketplace-object-views), [Custom Object View configuration](https://www.palantir.com/docs/foundry/object-views/config-overview).

### Scale, indexing, and operational quality

- [ ] `OMOV.50` Ontology resource indexing and search scale (`P2`, `todo`)
  - Incrementally index ontology resources, properties, links, interfaces, groups, Object Views, usage edges, and saved explorations.
  - Support pagination, type filters, project filters, group filters, fuzzy search, API-name search, and permission-aware result hiding.
  - Docs: [Ontology Manager overview](https://www.palantir.com/docs/foundry/ontology-manager/overview/index.html), [Object type groups](https://www.palantir.com/docs/foundry/object-link-types/type-groups/), [Object Explorer overview](https://www.palantir.com/docs/foundry/object-explorer/overview).

- [ ] `OMOV.51` Object View runtime performance budgets (`P2`, `todo`)
  - Track query count, linked object loading, media loads, map loads, time-series loads, Workshop widget execution, and function-backed display values per Object View render.
  - Warn editors when tabs or panels exceed configured runtime budgets.
  - Cache safe metadata while never caching object data beyond the current user's permission context.
  - Docs: [Core Object Views](https://www.palantir.com/docs/foundry/object-views/core-object-views/), [Use full Object Views](https://www.palantir.com/docs/foundry/object-views/use-full-views-in-platform), [Use panel Object Views](https://www.palantir.com/docs/foundry/object-views/use-panel-views-in-platform).

- [ ] `OMOV.52` Ontology cleanup assistant (`P2`, `todo`)
  - Identify unused object types, properties, link types, groups, interfaces, Object Views, legacy Object View fragments, and orphaned Workshop modules.
  - Require usage-impact review and explicit confirmation before cleanup actions.
  - Convert cleanup actions into unsaved changes or branch proposal changes before applying.
  - Docs: [Ontology cleanup](https://www.palantir.com/docs/foundry/ontology-manager/ontology-cleanup), [Viewing usage](https://www.palantir.com/docs/foundry/ontology-manager/viewing-usage).

- [ ] `OMOV.53` Audit, metrics, and health panels (`P2`, `todo`)
  - Emit audit events for ontology resource CRUD, datasource mapping changes, Object View edits, publish events, imports, exports, restores, branch rebases, marketplace packaging, and permission changes.
  - Show operational panels for stale datasources, broken links, failed Object View widget loads, inaccessible backing data, indexing lag, missing value type validation, and permission mismatches.
  - Docs: [Review and restore changes](https://www.palantir.com/docs/foundry/ontology-manager/restore-changes), [Viewing usage](https://www.palantir.com/docs/foundry/ontology-manager/viewing-usage), [Object Views overview](https://www.palantir.com/docs/foundry/object-views/overview).

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
