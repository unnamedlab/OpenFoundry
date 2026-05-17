# Foundry Fusion (spreadsheet) 1:1 parity checklist

Date: 2026-05-17
Scope: public-docs-based parity plan for OpenFoundry's Fusion **spreadsheet**
application: workbook resource, sheets, formulas, named ranges, lookup and
join formulas, sync to and from datasets, XLS/CSV import and export, data
validation, dropdowns, locked cells, conditional formatting, comments and
edit history, and integration with Workshop, Quiver, Notepad/Reports, and
Pipeline Builder.

> **Naming disambiguation.** Palantir Foundry uses the name "Fusion" for
> the spreadsheet product. OpenFoundry's existing `apps/web/src/routes/
> fusion/` route currently hosts the entity-resolution (MDM) experience.
> This checklist tracks the **spreadsheet** product; the MDM surface
> should be renamed under a different path (e.g. `entity-resolution`) so
> this product can claim the `fusion` route.

This document is intentionally implementation-oriented. It does not attempt
to clone Palantir branding, private source code, proprietary assets,
screenshots, or any non-public behavior. The target is **functional parity
based on public Palantir Foundry documentation**: the same product concepts,
comparable workflows, compatible resource models where useful, and
OpenFoundry-native implementation details that can be tested locally.

## Parity scope boundary

All checklist work is governed by the
[Foundry public-docs parity policy](../reference/foundry-public-docs-parity-policy.md).
OpenFoundry may implement behavior described in public Palantir documentation,
but contributors must not copy private source, decompile bundles, import
tenant-specific exports, use Palantir branding, or reuse proprietary assets.

This checklist does **not** redefine dataset, branching, or governance
models — those are owned by their respective checklists. It defines the
spreadsheet runtime, the sync-to-dataset contract, and the formula
evaluator.

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
| `P0` | Required for a credible spreadsheet: workbook CRUD, sheets, cells, formulas, named ranges, import/export, validation, dataset sync (one-way). |
| `P1` | Required for Foundry-style parity: bidirectional sync, lookup/join formulas against datasets, locked cells, dropdowns, comments, edit history. |
| `P2` | Advanced, governance-heavy, or scale-oriented parity (formula sandboxing, large-sheet streaming, marking-aware sync). |

## Official Palantir documentation library

### Product overview

- [Fusion overview](https://www.palantir.com/docs/foundry/fusion/overview)
- [Fusion application](https://www.palantir.com/docs/foundry/fusion/application)

### Concepts

- [Workbooks and sheets](https://www.palantir.com/docs/foundry/fusion/workbooks)
- [Formulas reference](https://www.palantir.com/docs/foundry/fusion/formulas)
- [Sync sheets with datasets](https://www.palantir.com/docs/foundry/fusion/sync-with-datasets)
- [Validation and dropdowns](https://www.palantir.com/docs/foundry/fusion/validation)
- [Edit history and comments](https://www.palantir.com/docs/foundry/fusion/history)

### Integrations

- [Workshop Fusion embed](https://www.palantir.com/docs/foundry/workshop/widgets/fusion)
- [Notepad Fusion embed](https://www.palantir.com/docs/foundry/notepad/fusion-embed)
- [Quiver Fusion sources](https://www.palantir.com/docs/foundry/quiver/fusion-sources)

## Milestone A: credible spreadsheet with one-way dataset sync

### Workbook and sheet model

- [ ] `FUS.1` Workbook resource (`P0`, `todo`)
  - CRUD a `fusion_workbook` resource with title, description, owning project, organizations, markings, sheets list, named ranges, validation rules, dropdown lists, and edit history.
  - Stable RID and Compass-discoverable.
  - Docs: [Fusion overview](https://www.palantir.com/docs/foundry/fusion/overview), [Workbooks and sheets](https://www.palantir.com/docs/foundry/fusion/workbooks).

- [ ] `FUS.2` Sheet model (`P0`, `todo`)
  - Sheets contain rows, columns, cells; per-cell value, formula, format, validation reference, and locked flag.
  - Support at least 100k rows and 200 columns per sheet (P0); larger sheets are P2.
  - Docs: [Workbooks and sheets](https://www.palantir.com/docs/foundry/fusion/workbooks).

- [ ] `FUS.3` Named ranges (`P0`, `todo`)
  - Named ranges scoped to a workbook or a single sheet.
  - Use named ranges in formulas, validations, and sync targets.
  - Docs: [Workbooks and sheets](https://www.palantir.com/docs/foundry/fusion/workbooks).

### Formula evaluator

- [ ] `FUS.4` Formula engine (`P0`, `todo`)
  - Implement a deterministic formula engine with arithmetic, logical, text, date, and lookup categories.
  - Required functions for P0: `SUM`, `AVERAGE`, `COUNT`, `IF`, `AND`, `OR`, `NOT`, `CONCAT`, `LEFT`, `RIGHT`, `MID`, `LEN`, `TRIM`, `LOWER`, `UPPER`, `DATE`, `TODAY`, `NOW`, `YEAR`, `MONTH`, `DAY`, `WEEKDAY`, `INDEX`, `MATCH`, `VLOOKUP`, `XLOOKUP`, `IFERROR`.
  - Treat all formula evaluation as sandboxed (no network, no filesystem).
  - Docs: [Formulas reference](https://www.palantir.com/docs/foundry/fusion/formulas).

- [ ] `FUS.5` Dependency graph and recalculation (`P0`, `todo`)
  - Maintain a per-sheet dependency graph and recalc only dirty cells on edit.
  - Detect cycles and surface them on the offending cells with an error indicator.
  - Docs: [Formulas reference](https://www.palantir.com/docs/foundry/fusion/formulas).

- [ ] `FUS.6` Formula errors (`P0`, `todo`)
  - Standard error tokens: `#VALUE!`, `#REF!`, `#DIV/0!`, `#NAME?`, `#N/A`, `#CYCLE!`, `#PERM!`.
  - Tooltips with human-readable error description.
  - Docs: [Formulas reference](https://www.palantir.com/docs/foundry/fusion/formulas).

### Import, export, validation

- [ ] `FUS.7` Import XLS/XLSX/CSV (`P0`, `todo`)
  - Upload an XLS/XLSX/CSV to seed a new workbook or replace selected sheets.
  - Preserve formulas where Excel-compatible; otherwise import as values with a warning.
  - Docs: [Workbooks and sheets](https://www.palantir.com/docs/foundry/fusion/workbooks).

- [ ] `FUS.8` Export XLSX/CSV (`P0`, `todo`)
  - Export full workbook or selected sheets to XLSX (formulas preserved) and CSV (values only).
  - Include marking labels in the exported file header (no Palantir branding).
  - Docs: [Workbooks and sheets](https://www.palantir.com/docs/foundry/fusion/workbooks).

- [ ] `FUS.9` Data validation rules (`P0`, `todo`)
  - Per-cell or per-range validation: list-of-values, number range, date range, regex.
  - Reject or warn on invalid edits; show validation indicator on the cell.
  - Docs: [Validation and dropdowns](https://www.palantir.com/docs/foundry/fusion/validation).

### One-way sync to datasets

- [ ] `FUS.10` Sync workbook → dataset (`P0`, `todo`)
  - Configure a sheet to publish to a target dataset on save/commit; map columns to dataset schema with explicit type coercion.
  - Append, overwrite-snapshot, or upsert-by-key modes.
  - Record a transaction id on the target dataset and link back to the workbook RID + revision.
  - Docs: [Sync sheets with datasets](https://www.palantir.com/docs/foundry/fusion/sync-with-datasets).

- [ ] `FUS.11` Source dataset → sheet (`P0`, `todo`)
  - Configure a sheet to seed from a dataset (one-time copy or refresh-on-open).
  - Show source transaction id and timestamp in the sheet header.
  - Docs: [Sync sheets with datasets](https://www.palantir.com/docs/foundry/fusion/sync-with-datasets).

## Milestone B: bidirectional sync, dataset-aware formulas, governance

### Bidirectional sync

- [ ] `FUS.12` Bidirectional sync semantics (`P1`, `todo`)
  - Two-way binding between a sheet and a dataset with a primary key column.
  - Conflict detection on row-level edits made both in the sheet and in upstream pipelines; surface a "needs review" badge.
  - Docs: [Sync sheets with datasets](https://www.palantir.com/docs/foundry/fusion/sync-with-datasets).

- [ ] `FUS.13` Sync history and revert (`P1`, `todo`)
  - Per-sync run history showing rows added/updated/deleted, target transaction id, author, and timestamp.
  - One-click revert of a sync run, producing a compensating dataset transaction.
  - Docs: [Sync sheets with datasets](https://www.palantir.com/docs/foundry/fusion/sync-with-datasets).

### Dataset-aware formulas

- [ ] `FUS.14` `DATASET.LOOKUP` formula (`P1`, `todo`)
  - Resolve `DATASET.LOOKUP("dataset_rid", "key_column", value, "return_column")` against a dataset's latest committed transaction.
  - Permission-aware: the caller must hold view permission on the source dataset.
  - Docs: [Formulas reference](https://www.palantir.com/docs/foundry/fusion/formulas).

- [ ] `FUS.15` `DATASET.QUERY` formula (`P1`, `todo`)
  - Resolve `DATASET.QUERY("dataset_rid", "WHERE col = 'x'")` returning a small result set rendered as a spill range.
  - Cap on rows and bytes per call; surface caps in the cell footer.
  - Docs: [Formulas reference](https://www.palantir.com/docs/foundry/fusion/formulas).

- [ ] `FUS.16` `OBJECTS.LOOKUP` formula (`P1`, `todo`)
  - Resolve `OBJECTS.LOOKUP("object_type", primary_key, "property")` against the ontology object set.
  - Permission and marking aware.
  - Docs: [Formulas reference](https://www.palantir.com/docs/foundry/fusion/formulas).

### Dropdowns, locked cells, conditional formatting

- [ ] `FUS.17` Dropdowns from datasets (`P1`, `todo`)
  - Bind a dropdown's allowed values to a dataset column or an object set property with optional filter.
  - Refresh dropdown values on workbook open and on explicit "refresh sources".
  - Docs: [Validation and dropdowns](https://www.palantir.com/docs/foundry/fusion/validation).

- [ ] `FUS.18` Locked cells and protected ranges (`P1`, `todo`)
  - Lock individual cells or entire ranges; only specific roles or users can edit locked cells.
  - Surface a "locked" indicator and a tooltip explaining the policy.
  - Docs: [Workbooks and sheets](https://www.palantir.com/docs/foundry/fusion/workbooks).

- [ ] `FUS.19` Conditional formatting (`P1`, `todo`)
  - Rules: value range, comparison to another cell, contains text, custom formula.
  - Up to 16 rules per sheet; clear precedence rules between them.
  - Docs: [Workbooks and sheets](https://www.palantir.com/docs/foundry/fusion/workbooks).

### Comments and edit history

- [ ] `FUS.20` Cell comments (`P1`, `todo`)
  - Thread comments anchored on a cell with @-mentions and resolve state.
  - Notifications via Pulse for mentions.
  - Docs: [Edit history and comments](https://www.palantir.com/docs/foundry/fusion/history).

- [ ] `FUS.21` Edit history (`P1`, `todo`)
  - Per-cell history of value/formula changes with author and timestamp.
  - Show diff in a sidebar on hover; revert a single cell to a prior value.
  - Docs: [Edit history and comments](https://www.palantir.com/docs/foundry/fusion/history).

### Integrations

- [ ] `FUS.22` Workshop Fusion widget (`P1`, `todo`)
  - Workshop widget embedding a workbook or a single sheet, read-only or editable, with two-way variable binding for selection.
  - Docs: [Workshop Fusion embed](https://www.palantir.com/docs/foundry/workshop/widgets/fusion).

- [ ] `FUS.23` Notepad/Reports Fusion embed (`P1`, `todo`)
  - Embed a workbook range in Notepad/Reports; freeze the content at a specific revision for printable snapshots.
  - Docs: [Notepad Fusion embed](https://www.palantir.com/docs/foundry/notepad/fusion-embed).

- [ ] `FUS.24` Quiver Fusion source (`P1`, `todo`)
  - Use a sheet range as a Quiver data source for ad-hoc time-series analyses (small data only).
  - Docs: [Quiver Fusion sources](https://www.palantir.com/docs/foundry/quiver/fusion-sources).

## Milestone C: scale, governance, advanced parity

### Scale and performance

- [ ] `FUS.25` Large-sheet streaming render (`P2`, `todo`)
  - Virtualized row/column rendering for >1M cells.
  - Stream cell updates from server when sheets are co-edited.
  - Docs: [Workbooks and sheets](https://www.palantir.com/docs/foundry/fusion/workbooks).

- [ ] `FUS.26` Server-side recompute for heavy formulas (`P2`, `todo`)
  - Push `DATASET.QUERY`, `DATASET.LOOKUP`, `OBJECTS.LOOKUP` evaluation to a server worker pool; cache by inputs hash.
  - Docs: [Formulas reference](https://www.palantir.com/docs/foundry/fusion/formulas).

### Governance

- [ ] `FUS.27` Marking-aware sync (`P2`, `todo`)
  - Workbook inherits markings from referenced datasets/object sets; sync targets must carry the union of markings (or admin override with audit).
  - Docs: [Sync sheets with datasets](https://www.palantir.com/docs/foundry/fusion/sync-with-datasets).

- [ ] `FUS.28` Restricted-view enforcement on lookups (`P2`, `todo`)
  - `DATASET.QUERY`/`LOOKUP` against a restricted view applies the caller's row-level filter; cell values respect the per-user visibility.
  - Docs: [Formulas reference](https://www.palantir.com/docs/foundry/fusion/formulas).

- [ ] `FUS.29` Branch-aware workbooks (`P2`, `todo`)
  - Honor active branch from the Global Branching taskbar: lookups read branched datasets when set.
  - Workbook edits on a branch are scoped to the branch until merge.
  - Docs: [Sync sheets with datasets](https://www.palantir.com/docs/foundry/fusion/sync-with-datasets).

- [ ] `FUS.30` Audit trail for sheet edits (`P2`, `todo`)
  - Emit audit events for every value/formula change, dropdown source change, locked-cell change, and sync run.
  - Audit events are markings-aware and queryable from the Audit checklist.
  - Docs: [Edit history and comments](https://www.palantir.com/docs/foundry/fusion/history).

## Implementation inventory to collect before coding

- [ ] `INV.1` Rename current `apps/web/src/routes/fusion/` to `entity-resolution/` so this product can claim the `fusion` route.
- [ ] `INV.2` Identify a formula evaluator library (or design one) that meets sandboxing requirements.
- [ ] `INV.3` Identify the dataset write path that workbook sync will use (must produce a real dataset transaction).
- [ ] `INV.4` Identify the marking propagation path for sync targets.
- [ ] `INV.5` Identify the branch-aware read contract with `dataset-versioning-service`.
- [ ] `INV.6` Produce a parity matrix sibling JSON entry once a first implementation inventory is in place.

## Suggested service boundaries

| Surface | Responsibilities |
| --- | --- |
| `fusion-spreadsheet-service` | Workbook CRUD, sheet CRUD, named ranges, validations, dropdown sources, edit history, comments, sync configuration. |
| `fusion-formula-runtime` | Formula evaluator (sandboxed), dependency graph, recalc, dataset-aware formulas with permission checks. |
| `dataset-versioning-service` | Sync target transactions, conflict detection on bidirectional sync. |
| `object-storage-v2` | `OBJECTS.LOOKUP` resolution with marking-aware filters. |
| `apps/web` | Spreadsheet UI shell, virtualized grid, formula bar, validation/dropdown UX, sync UX, comments and history sidebars. |
