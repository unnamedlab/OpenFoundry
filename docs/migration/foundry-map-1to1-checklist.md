# Foundry Map 1:1 parity checklist

Date: 2026-05-17
Scope: public-docs-based parity plan for OpenFoundry's Map application:
map projects, base maps, vector and raster layers, point/line/polygon/track/
choropleth/cluster/heatmap rendering, drawing and annotation tools,
bounding-box and spatial queries against object sets, time-aware playback,
geospatial datasource adapters, geocoding, projections, tile services, and
integrations with Workshop, Object Explorer, Object Views, Quiver, and
Pipeline Builder.

This document is intentionally implementation-oriented. It does not attempt
to clone Palantir branding, private source code, proprietary assets,
screenshots, or any non-public behavior. The target is **functional parity
based on public Palantir Foundry documentation**: the same product concepts,
comparable end-to-end workflows, compatible resource models where useful,
and OpenFoundry-native implementation details that can be tested locally.

## Parity scope boundary

All checklist work is governed by the
[Foundry public-docs parity policy](../reference/foundry-public-docs-parity-policy.md).
OpenFoundry may implement behavior described in public Palantir documentation,
but contributors must not copy private source, decompile bundles, import
tenant-specific exports, use Palantir branding, or reuse proprietary assets.
The product target is functional parity in an OpenFoundry-native
implementation, not a pixel-perfect clone.

This checklist covers the Map app and its tile, layer, drawing, and
spatial-query surfaces. It does not redefine the dataset, ontology, branching,
or governance models — those are owned by their respective checklists. It
should integrate with Workshop (map widget), Object Explorer (map preset),
Object Views (location panel), Quiver (spatial joins on time series), and
Pipeline Builder (geospatial transform nodes).

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
| `P0` | Required for credible Map app with object-set rendering, layer styling, drawing, and bounding-box queries. |
| `P1` | Required for Foundry-style geospatial parity beyond a single demo (tracks, clustering, choropleth, time playback). |
| `P2` | Advanced, governance-heavy, or scale-oriented parity (custom projections, raster ingest, vector tile precomputation, restricted-view enforcement on maps). |

## Official Palantir documentation library

### Product overview

- [Map overview](https://www.palantir.com/docs/foundry/map/overview)
- [Foundry platform summary for LLMs](https://www.palantir.com/docs/foundry/getting-started/foundry-platform-summary-llm)

### Geospatial concepts

- [Geospatial overview](https://www.palantir.com/docs/foundry/geospatial/overview)
- [Vector tiles and rendering](https://www.palantir.com/docs/foundry/geospatial/vector-tiles)
- [Map projections and coordinate systems](https://www.palantir.com/docs/foundry/geospatial/projections)
- [Spatial queries](https://www.palantir.com/docs/foundry/geospatial/spatial-queries)
- [Geocoding](https://www.palantir.com/docs/foundry/geospatial/geocoding)

### Map app workflows

- [Map application](https://www.palantir.com/docs/foundry/map/application)
- [Layers and styling](https://www.palantir.com/docs/foundry/map/layers)
- [Drawing and annotations](https://www.palantir.com/docs/foundry/map/drawing)
- [Time-aware playback](https://www.palantir.com/docs/foundry/map/timeline)

### Integrations

- [Workshop map widget](https://www.palantir.com/docs/foundry/workshop/widgets/map)
- [Object Explorer map preset](https://www.palantir.com/docs/foundry/object-explorer/map-preset)
- [Object Views location panels](https://www.palantir.com/docs/foundry/object-views/location-panels)

## Milestone A: credible Map app with object-set rendering and drawing

### Map projects and base maps

- [ ] `MAP.1` Map project resource (`P0`, `todo`)
  - CRUD a `map_project` resource with title, description, default center, default zoom, default projection, base map style id, organizations, markings, and owning project.
  - Persist last-viewed state per user (center, zoom, active layers).
  - Surface as a Compass-discoverable resource with a stable RID and standard permission semantics.
  - Docs: [Map overview](https://www.palantir.com/docs/foundry/map/overview), [Map application](https://www.palantir.com/docs/foundry/map/application).

- [ ] `MAP.2` Base map registry (`P0`, `todo`)
  - Ship at least three base map styles: streets, satellite, dark. Allow admin to register additional MapLibre/Mapbox-style JSON specs at org level.
  - Validate each style's tile endpoint, attribution, max zoom, and supported languages.
  - Docs: [Map application](https://www.palantir.com/docs/foundry/map/application).

- [ ] `MAP.3` Projection and coordinate-system handling (`P0`, `todo`)
  - Default to EPSG:3857 (Web Mercator) for rendering and EPSG:4326 (WGS84) for storage and exchange.
  - Reproject inbound geometries on ingest, not on every render.
  - Document the two-projection contract for users in the map sidebar.
  - Docs: [Map projections and coordinate systems](https://www.palantir.com/docs/foundry/geospatial/projections).

### Layer model

- [ ] `MAP.4` Layer resource model (`P0`, `todo`)
  - `map_layer` rows attached to a `map_project` with: kind (point/line/polygon/track/choropleth/cluster/heatmap), source kind (object set, dataset query, vector tile URL template, GeoJSON inline), style spec, filter spec, popup template, visibility default, and z-order.
  - Layers must record their source RID for lineage and permission enforcement.
  - Docs: [Layers and styling](https://www.palantir.com/docs/foundry/map/layers).

- [ ] `MAP.5` Object-set-backed layers (`P0`, `todo`)
  - Bind a layer to an object type's geo property (point or shape) plus optional filters from Object Explorer.
  - Stream features in viewport-sized batches with server-side bounding-box pushdown to Object Storage V2.
  - Respect object-level permissions and markings on every batch.
  - Docs: [Spatial queries](https://www.palantir.com/docs/foundry/geospatial/spatial-queries), [Object Explorer map preset](https://www.palantir.com/docs/foundry/object-explorer/map-preset).

- [ ] `MAP.6` Dataset-backed layers (`P0`, `todo`)
  - Render datasets exposing a geometry column (WKT/WKB/GeoJSON) via a server-side tile service (XYZ Mercator).
  - Generate vector tiles on demand with caching keyed by dataset transaction id.
  - Docs: [Vector tiles and rendering](https://www.palantir.com/docs/foundry/geospatial/vector-tiles).

### Styling, popups, and tooltips

- [ ] `MAP.7` Style expressions (`P0`, `todo`)
  - Support style expressions for fill color, stroke color, opacity, radius, icon, label, and dash pattern based on property values (categorical and quantitative).
  - Provide a styling sidebar with a live preview against a representative sample of features.
  - Docs: [Layers and styling](https://www.palantir.com/docs/foundry/map/layers).

- [ ] `MAP.8` Popups and tooltips (`P0`, `todo`)
  - Per-layer popup template referencing object/dataset properties, with link-out to Object View, Object Explorer, or a dataset row.
  - Tooltips with title + 2-3 properties on hover; full popup on click.
  - Docs: [Map application](https://www.palantir.com/docs/foundry/map/application).

### Drawing, annotations, and selection

- [ ] `MAP.9` Drawing tools (`P0`, `todo`)
  - Point, line, polygon, circle, and rectangle drawing with snapping and freehand modes.
  - Persist drawings as `map_annotation` rows scoped to the project, the current user, or shared with collaborators.
  - Export drawings as GeoJSON.
  - Docs: [Drawing and annotations](https://www.palantir.com/docs/foundry/map/drawing).

- [ ] `MAP.10` Bounding-box and lasso selection (`P0`, `todo`)
  - Select features inside a drawn polygon/rectangle to produce an `object_set` selection that flows into Object Explorer, Object Views, or downstream Actions.
  - Show count + first 50 selected features in the sidebar.
  - Docs: [Spatial queries](https://www.palantir.com/docs/foundry/geospatial/spatial-queries).

## Milestone B: Foundry-style geospatial parity beyond demo

### Advanced layer types

- [ ] `MAP.11` Track layer (`P1`, `todo`)
  - Render time-ordered point sequences per entity as connected polylines with directional arrows.
  - Group by an entity key (e.g. vehicle id) and color by speed/heading/elapsed time.
  - Docs: [Layers and styling](https://www.palantir.com/docs/foundry/map/layers).

- [ ] `MAP.12` Choropleth layer (`P1`, `todo`)
  - Bind a region polygon dataset (admin boundaries, hexbins) to a numeric measure with classed/continuous color ramps.
  - Support H3 cells, GeoHash buckets, and arbitrary polygon datasets.
  - Docs: [Layers and styling](https://www.palantir.com/docs/foundry/map/layers).

- [ ] `MAP.13` Cluster and heatmap layers (`P1`, `todo`)
  - Server-side clustering at low zoom with expandable spider/donut clusters at high zoom.
  - Heatmap layer driven by a numeric weight property; configurable radius and intensity.
  - Docs: [Layers and styling](https://www.palantir.com/docs/foundry/map/layers).

### Time-aware playback

- [ ] `MAP.14` Timeline control (`P1`, `todo`)
  - Per-project timeline component reading a layer's time property, with play/pause, scrubbing, speed selection, and absolute time labels.
  - Show layer features only when their time value falls inside the current window.
  - Docs: [Time-aware playback](https://www.palantir.com/docs/foundry/map/timeline).

### Geocoding and search

- [ ] `MAP.15` Forward geocoding (`P1`, `todo`)
  - Address/place search box that returns ranked candidates with bbox, lat/lon, and admin context.
  - Pluggable geocoder backend (Nominatim, Photon, or commercial provider) selected per enrollment.
  - Docs: [Geocoding](https://www.palantir.com/docs/foundry/geospatial/geocoding).

- [ ] `MAP.16` Reverse geocoding (`P1`, `todo`)
  - Click-to-place lookup that returns nearest address, admin levels, and POI matches.
  - Use the same backend as forward geocoding by default.
  - Docs: [Geocoding](https://www.palantir.com/docs/foundry/geospatial/geocoding).

### Workshop and Object Views integration

- [ ] `MAP.17` Workshop map widget parity (`P1`, `todo`)
  - Expose a Workshop widget bound to an object set variable with the same layer/style/popup model as the standalone Map app.
  - Two-way variable binding for the active selection and hovered feature.
  - Docs: [Workshop map widget](https://www.palantir.com/docs/foundry/workshop/widgets/map).

- [ ] `MAP.18` Object View location panel (`P1`, `todo`)
  - Render an embeddable map panel in Object Views showing the object's location plus configurable neighbor object sets.
  - Docs: [Object Views location panels](https://www.palantir.com/docs/foundry/object-views/location-panels).

## Milestone C: advanced parity, governance, and scale

### Tile service and precomputation

- [ ] `MAP.19` XYZ vector tile service (`P2`, `todo`)
  - Serve `/api/v1/geospatial/tiles/{layer_id}/{z}/{x}/{y}.mvt` with on-disk and Redis caching.
  - Cache key includes dataset/object-set transaction id and applied filters.
  - Honor permissions and markings at tile-generation time (do not pre-bake forbidden cells).
  - Docs: [Vector tiles and rendering](https://www.palantir.com/docs/foundry/geospatial/vector-tiles).

- [ ] `MAP.20` Tile precomputation jobs (`P2`, `todo`)
  - For high-traffic layers, schedule batch tile generation via Pipeline Builder so the live request path is a cache read.
  - Track precomputation freshness per layer and surface "stale" badges in the map sidebar.
  - Docs: [Vector tiles and rendering](https://www.palantir.com/docs/foundry/geospatial/vector-tiles).

### Raster and OGC interoperability

- [ ] `MAP.21` Raster layer ingest (`P2`, `todo`)
  - Ingest Cloud-Optimized GeoTIFF (COG) into a `raster_dataset` resource with overviews.
  - Render via a `/raster/{id}/{z}/{x}/{y}.png` endpoint with configurable color ramps and stretch.
  - Docs: [Vector tiles and rendering](https://www.palantir.com/docs/foundry/geospatial/vector-tiles).

- [ ] `MAP.22` OGC service compatibility (`P2`, `todo`)
  - Expose WMS, WMTS, and WFS endpoints for selected layers (admin-toggled, marking-aware).
  - Document which projections and content types are supported per endpoint.
  - Docs: [Vector tiles and rendering](https://www.palantir.com/docs/foundry/geospatial/vector-tiles).

### Governance and scale

- [ ] `MAP.23` Permission and marking enforcement on tiles (`P2`, `todo`)
  - Every tile request resolves the caller's clearances and filters features before encoding.
  - Restricted views surface only their permitted rows; the map never leaks geometry the caller cannot see.
  - Docs: [Spatial queries](https://www.palantir.com/docs/foundry/geospatial/spatial-queries).

- [ ] `MAP.24` Spatial query pushdown to Object Storage V2 (`P2`, `todo`)
  - Translate bounding-box, polygon-contains, and radius-search predicates into Object Storage V2 spatial-index lookups (R-tree / H3 / S2).
  - Avoid loading features client-side just to filter them.
  - Docs: [Spatial queries](https://www.palantir.com/docs/foundry/geospatial/spatial-queries).

- [ ] `MAP.25` Map export and snapshotting (`P2`, `todo`)
  - Export the current viewport as PNG/PDF/GeoJSON with attribution and timestamp.
  - Snapshot a map project at the current dataset/object-set transactions for embedding in Notepad/Reports.
  - Docs: [Map application](https://www.palantir.com/docs/foundry/map/application).

## Implementation inventory to collect before coding

- [ ] `INV.1` Identify existing OpenFoundry services that own geometries on objects and on datasets.
- [ ] `INV.2` Identify the tile-generation runtime (libs/geospatial-tiles) and its caching backends.
- [ ] `INV.3` Identify the geocoding provider contract and which adapters exist.
- [ ] `INV.4` Identify the Object Storage V2 spatial index status and pushdown contract.
- [ ] `INV.5` Identify the Workshop map widget current capabilities and gaps.
- [ ] `INV.6` Identify the marking-aware filter path for tile generation.
- [ ] `INV.7` Produce a parity matrix sibling JSON entry under `foundry-feature-parity-matrix.json` once a first implementation inventory is in place.

## Suggested service boundaries

| Surface | Responsibilities |
| --- | --- |
| `map-service` | Map project + layer + annotation CRUD, base map registry, timeline state, export jobs. |
| `geospatial-tile-service` | Vector tile generation (MVT), raster tile generation, cache management, OGC endpoints. |
| `geocoding-service` | Forward/reverse geocoding adapter, provider abstraction, quota and rate limit handling. |
| `object-storage-v2` | Spatial-index pushdown for bounding-box, polygon-contains, radius queries (see Object Storage V2 checklist). |
| `apps/web` | Map app shell, layer sidebar, drawing/annotation tools, Workshop map widget, Object View location panel. |
