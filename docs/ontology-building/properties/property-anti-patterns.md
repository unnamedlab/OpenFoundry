# Property anti-patterns

Properties are easy to add, which makes them one of the easiest places to introduce semantic debt.

## Anti-patterns

| Anti-pattern | Why it hurts | Better alternative |
| --- | --- | --- |
| using strings for everything | loses validation and meaning | choose precise property types |
| encoding links in text fields | blocks graph-aware workflows | use link types or supporting objects |
| overusing required fields | makes ingestion and migration brittle | require only true invariants |
| ambiguous metric names | breaks analytics consistency | use domain language plus clear units |
| putting workflow state in many duplicated fields | causes semantic drift | centralize state semantics deliberately |

## Design rule of thumb

If a field affects search, workflows, permissions, reports, or UI behavior, treat it as semantic product design, not just schema plumbing.
