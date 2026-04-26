# Modeling patterns

Modeling patterns help teams decide what should become an object type and what should remain a property, interface, or linked entity.

## Recommended patterns

| Pattern | Use it when | Example |
| --- | --- | --- |
| Primary operational object | the thing has its own lifecycle, permissions, or workflows | case, work order, supplier, incident |
| Supporting entity | the thing should be reusable across many objects | analyst, facility, asset |
| Link-backed relationship | the connection itself matters | assigned-to, depends-on, delivered-by |
| Interface reuse | multiple types share the same semantic contract | reviewable, geo-locatable, schedulable |

## Anti-patterns to avoid

- using one object type per source table without semantic consolidation
- storing relationship meaning inside free-form string properties
- creating giant object types that mix unrelated operational concerns
- using properties where linked entities should exist

## OpenFoundry current vs target

| Topic | Current signals | Target maturity |
| --- | --- | --- |
| object types | route and model support exists | richer design-time governance |
| interfaces | already first-class in `ontology-service` | broader reuse across many apps |
| shared semantics | shared property types already exist | catalogued ontology design patterns |
