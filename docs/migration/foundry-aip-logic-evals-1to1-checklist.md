# Foundry AIP Logic and Evals 1:1 parity checklist

Date: 2026-05-11
Scope: public-docs-based parity plan for OpenFoundry's AIP Logic and AIP Evals
surfaces: no-code Logic functions, Logic files, input/block/output boards,
Use LLM blocks, prompts, tools, Query objects, Apply action, Execute function,
Calculator, conditionals, loops, create variables, typed inputs, media/model
inputs, block outputs, final outputs, Ontology edit outputs, debugger, run
panel, run history, unit tests, publishing, versions, comparison view, usage
surfaces, command-line/API invocation, execution modes, project-scoped run
history datasets, Automate integration, branch-aware Logic editing, Logic
metrics, compute usage, evaluation suites, target functions, test cases,
object-set-backed test cases, evaluators, custom evaluation functions, built-in
evaluators, Marketplace evaluator handoffs, multi-target runs, single-test-case
runs, run configurations, iterations, parallelization, run metadata,
experiments, intermediate parameters, Ontology-edit simulations, results
analysis, results datasets, metrics dashboards, trace/debug views, and
production-readiness guardrails for LLM-backed functions.

This document is intentionally implementation-oriented. It does not attempt to
clone Palantir branding, private source code, proprietary assets, screenshots,
or any non-public behavior. The target is **functional parity based on public
Palantir Foundry documentation**: the same product concepts, comparable
Logic/Evals authoring and operational workflows, compatible resource models
where useful, and OpenFoundry-native implementation details that can be tested
locally.

## Parity scope boundary

All checklist work is governed by the
[Foundry public-docs parity policy](../reference/foundry-public-docs-parity-policy.md).
OpenFoundry may implement behavior described in public Palantir documentation,
but contributors must not copy private source, decompile bundles, import
tenant-specific exports, use Palantir branding, or reuse proprietary assets.
The product target is functional parity in an OpenFoundry-native implementation,
not a pixel-perfect clone.

This checklist covers AIP Logic as a no-code function-builder surface and AIP
Evals as a test/evaluation surface for Logic, agent-like functions, and
code-authored functions. It should integrate with the Ontology/Object Views
checklist for object, object set, action, and permission semantics; with the
Functions checklist for published function invocation and versioning; with the
Automate/Rules checklist for Logic effects and staged human review; with the
Media Sets checklist for media-reference inputs; with the Global Branching
checklist for branch-aware Logic resources; with Data Foundation for results
and run-history datasets; and with AIP/model governance for supported LLMs,
capacity, token usage, and security. It should not duplicate those underlying
surfaces.

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
| `P0` | Required for credible demo workflows that build, run, debug, publish, and evaluate a Logic function over Ontology objects and text. |
| `P1` | Required for Foundry-style AIP Logic and AIP Evals parity beyond simple prompt execution. |
| `P2` | Advanced, governance-heavy, branching, experiment, scale, observability, or marketplace-oriented parity. |

## Official Palantir documentation library

These public docs should be treated as the external behavioral contract while
implementing this checklist.

### AIP overview and Logic

- [AIP overview](https://www.palantir.com/docs/foundry/aip/overview/)
- [AIP features](https://www.palantir.com/docs/foundry/aip/aip-features/)
- [AIP Logic overview](https://www.palantir.com/docs/foundry/logic)
- [AIP Logic core concepts](https://www.palantir.com/docs/foundry/logic/core-concepts/)
- [AIP Logic getting started](https://www.palantir.com/docs/foundry/logic/getting-started)
- [AIP Logic blocks](https://www.palantir.com/docs/foundry/logic/blocks/)
- [AIP Logic compute usage](https://www.palantir.com/docs/foundry/logic/compute-usage)
- [AIP Logic metrics](https://www.palantir.com/docs/foundry/logic/logic-metrics/)
- [AIP Logic execution mode settings](https://www.palantir.com/docs/foundry/logic/execution-mode-settings/)
- [AIP Logic integration with Automate](https://www.palantir.com/docs/foundry/logic/aip-logic-integration-automate/)
- [Branching AIP Logic](https://www.palantir.com/docs/foundry/logic/branching-logic)

### AIP Evals

- [AIP Evals overview](https://www.palantir.com/docs/foundry/aip-evals/overview/)
- [Evaluation suites for Logic functions](https://www.palantir.com/docs/foundry/aip-evals/getting-started/)
- [Create an evaluation suite](https://www.palantir.com/docs/foundry/aip-evals/create-suite)
- [Run an evaluation suite](https://www.palantir.com/docs/foundry/aip-evals/run-suite/)
- [Run experiments](https://www.palantir.com/docs/foundry/aip-evals/experiments/)
- [Analyze run results](https://www.palantir.com/docs/foundry/aip-evals/analyze-run-results)
- [Use intermediate parameters to evaluate block output](https://www.palantir.com/docs/foundry/logic/evaluations-intermediate-parameters/)
- [Evaluate Ontology edits](https://www.palantir.com/docs/foundry/aip-evals/ontology-edits)
- [Write run results to a dataset](https://www.palantir.com/docs/foundry/aip-evals/results-dataset)
- [View results in metrics dashboard](https://www.palantir.com/docs/foundry/logic/evaluations-metrics-dashboard/)

### Integrated Foundry surfaces

- [Automate overview](https://www.palantir.com/docs/foundry/automate/overview/)
- [AIP Logic integration with Automate](https://www.palantir.com/docs/foundry/logic/aip-logic-integration-automate/)
- [Functions getting started](https://www.palantir.com/docs/foundry/functions/getting-started/)
- [Functions in Workshop](https://www.palantir.com/docs/foundry/workshop/functions-use/)
- [Action types overview](https://www.palantir.com/docs/foundry/action-types/overview)
- [Object Explorer overview](https://www.palantir.com/docs/foundry/object-explorer/overview)
- [Media sets overview](https://www.palantir.com/docs/foundry/media-sets-advanced-formats)

## Target OpenFoundry resource model

The implementation should define stable OpenFoundry-owned resources that can map
to public Foundry concepts without requiring Palantir RID formats. Compatibility
aliases may be accepted at service boundaries, but persisted state should use
OpenFoundry canonical IDs.

| Public Foundry concept | OpenFoundry resource target | Required notes |
| --- | --- | --- |
| Logic file | `logic_file` | Project/folder-managed no-code function resource with inputs, blocks, outputs, version history, execution mode, branch metadata, and permissions. |
| Logic function | `logic_function` | Published callable function produced from a Logic file, compatible with Functions, Workshop, Actions, Automate, and API/curl invocation. |
| Logic input | `logic_input` | Typed input definition supporting primitives, arrays/lists, structs, objects, object lists, object sets, media references, models, and timestamps. |
| Logic block | `logic_block` | Node in the Logic execution graph with type, inputs, outputs, prompt/tool configuration, dependency edges, and debug trace identity. |
| Use LLM block | `logic_llm_block` | Prompt + model + tool-enabled LLM interaction with configured output type and trace metadata. |
| Logic tool | `logic_tool` | Tool definition used by an LLM block: query objects, apply action, execute function, calculator, or OpenFoundry-native extension. |
| Intermediate parameter | `logic_intermediate_parameter` | Exposed block output usable by AIP Evals and result datasets. |
| Logic output | `logic_output` | Final value/object/output definition or Ontology-edit output produced by the Logic function. |
| Ontology edit bundle | `logic_ontology_edit_bundle` | Simulated or staged object/action edits produced by Logic, never applied directly except through action/automation flows. |
| Debug trace | `logic_debug_trace` | Per-run block trace, prompts/tool calls, inputs/outputs, errors, and evaluator trace links with security-aware retention. |
| Logic run | `logic_run` | Invocation instance from preview, Workshop, Action, Automate, API, or Evals with execution mode, status, logs, duration, token/compute usage, and outputs. |
| Logic version | `logic_version` | Saved or published version with diff metadata and comparison-view support. |
| Execution mode | `logic_execution_mode` | User-scoped or project-scoped execution behavior controlling permissions, run-history visibility, imports, and results dataset behavior. |
| Logic metric | `logic_metric` | Success/failure counts, P95 duration, run history, failure category, and telemetry surfaced in Ontology Manager/Workflow Lineage-like views. |
| Evaluation suite | `eval_suite` | Test/evaluation resource with target functions, test cases, evaluators, run configuration defaults, results, and permissions. |
| Target function | `eval_target_function` | A Logic, agent-like, or code-authored published function under test, with input/output signature and version selection. |
| Test case | `eval_test_case` | Manual or object-set-backed input/expected-output row with typed columns, expected values, metadata, and generated name hints. |
| Evaluator | `eval_evaluator` | Built-in, custom function, Logic-backed, or Marketplace evaluator that returns Boolean/numeric metrics and optional debug strings. |
| Metric objective | `eval_metric_objective` | Boolean target or numeric maximize/minimize/threshold configuration used to decide pass/fail. |
| Evaluation run | `eval_run` | Full suite, selected target, experiment, or single-test execution with iterations, parallelization, execution mode, metadata, and results. |
| Experiment | `eval_experiment` | Grid-search run group over model/prompt/parameter combinations with grouped aggregate results. |
| Result dataset | `eval_results_dataset` | Dataset written by project-scoped evaluation runs containing function outputs, evaluator results, metadata, and errors. |
| Results analyzer | `eval_results_analyzer` | LLM-assisted failure clustering and prompt suggestion resource with model/max-category/max-test-case configuration. |

## Milestone A: minimum viable AIP Logic and Evals parity

### AIP Logic application shell and authoring basics

- [x] `AIPLE.1` Logic file CRUD and project placement (`P0`, `done`)
  - Create, get, list, update metadata, move, duplicate, archive/delete, and restore Logic files.
  - Require Logic files to be saved in project folders rather than personal-only home folders when mirroring documented behavior.
  - Track name, description, project/folder, owner, created/updated timestamps, current draft version, published version, execution mode, and permissions.
  - Implemented in `agent-runtime-service` with project/folder-required Logic file metadata, owner/permission-aware CRUD, move, duplicate, archive/delete, and restore endpoints under `/api/v1/agent-runtime/logic/files`.
  - Tests cover the project/folder placement guardrail, execution-mode validation, authentication guardrail, and duplicate placement validation.
  - Docs: [AIP Logic overview](https://www.palantir.com/docs/foundry/logic), [AIP Logic getting started](https://www.palantir.com/docs/foundry/logic/getting-started).

- [x] `AIPLE.2` Logic authoring interface shell (`P0`, `done`)
  - Provide a three-panel authoring UI: inputs/blocks/outputs configuration, debugger, and run panel.
  - Include right sidebar entry points for uses, automations, evaluations, run history, version history, metrics, and execution settings.
  - Preserve OpenFoundry-native UI styling and avoid Palantir screenshots or visual assets.
  - Implemented as the OpenFoundry-native `/logic` authoring route with configuration, debugger, run panel, and right resource rail for uses, automations, evaluations, history, metrics, and execution settings.
  - Docs: [AIP Logic getting started](https://www.palantir.com/docs/foundry/logic/getting-started), [AIP Logic core concepts](https://www.palantir.com/docs/foundry/logic/core-concepts/).

- [x] `AIPLE.3` Logic input board (`P0`, `done`)
  - Define typed inputs for array/list, Boolean, date, double, float, integer, long, media reference, model, object, object list, object set, short, string, struct, and timestamp where local services exist.
  - Validate input API names, required/optional state, default values, object type selections, object set compatibility, and model variable compatibility.
  - Implemented shared input-board validators and UI editing for all supported input types, including typed defaults, object/object-set checks, and model slot compatibility.
  - Docs: [AIP Logic getting started](https://www.palantir.com/docs/foundry/logic/getting-started), [AIP Logic blocks](https://www.palantir.com/docs/foundry/logic/blocks/).

- [ ] `AIPLE.4` Logic block graph and dataflow (`P0`, `todo`)
  - Add, remove, reorder, duplicate, and connect blocks.
  - Type-check block inputs/outputs and prevent cycles unless a documented loop/conditional construct owns the flow.
  - Show which block outputs feed subsequent blocks and final outputs.
  - Docs: [AIP Logic core concepts](https://www.palantir.com/docs/foundry/logic/core-concepts/), [AIP Logic blocks](https://www.palantir.com/docs/foundry/logic/blocks/).

- [x] `AIPLE.5` Use LLM block (`P0`, `done`)
  - Configure model, system/task prompt, tool access, structured output type, and prompt variable references.
  - Support model variables so Evals experiments can swap model values.
  - Record prompt, tool-call, output, token/compute, and error trace metadata in the debugger.
  - Implemented shared Use LLM block config/validation, model-variable binding for Evals, structured output validation, and debugger trace metadata surfaced in the `/logic` authoring shell.
  - Docs: [AIP Logic blocks](https://www.palantir.com/docs/foundry/logic/blocks/), [AIP Logic compute usage](https://www.palantir.com/docs/foundry/logic/compute-usage).

- [x] `AIPLE.6` Query objects tool (`P0`, `done`)
  - Allow LLM blocks to query configured object types and selected properties.
  - Limit accessible object types/properties to what the Logic function/user can read.
  - Provide token-efficiency warnings when too many object types or properties are exposed.
  - Implemented Query objects tool selection in the Use LLM block editor with readable object/property allowlists and token-efficiency warnings for wide/large query context.
  - Docs: [AIP Logic blocks](https://www.palantir.com/docs/foundry/logic/blocks/), [AIP Logic overview](https://www.palantir.com/docs/foundry/logic).

- [x] `AIPLE.7` Apply action tool and Ontology edits (`P0`, `done`)
  - Let LLM blocks propose action-backed Ontology edits using selected action types and parameters.
  - Show proposed edits in debugger during preview without applying them to the real Ontology.
  - Require published Logic plus action or automation invocation before real Ontology edits can be applied.
  - Implemented Apply action tool config/validation, preview-only proposed edit trace metadata, and commit guardrails requiring published Logic plus action/automation invocation.
  - Docs: [AIP Logic getting started](https://www.palantir.com/docs/foundry/logic/getting-started), [AIP Logic blocks](https://www.palantir.com/docs/foundry/logic/blocks/).

- [x] `AIPLE.8` Execute function and calculator tools (`P0`, `done`)
  - Configure calls to TypeScript, Python, existing Logic, and function-on-objects functions where available.
  - Provide calculator tool support for exact mathematical computation in LLM workflows.
  - Validate function signatures, parameter mapping, permissions, and output type compatibility.
  - Implemented Execute function and calculator tool config/validation, permission allowlists, signature/type checks, deterministic arithmetic evaluation, and debugger tool metadata.
  - Docs: [AIP Logic blocks](https://www.palantir.com/docs/foundry/logic/blocks/).

- [x] `AIPLE.9` Create variable, conditionals, and loops (`P0`, `done`)
  - Support create variable blocks for primitive/object-compatible values.
  - Support conditionals and loops with list inputs, element/index variables, output aggregation, and parallel-loop behavior when no actions are present.
  - Validate list/array conversion and loop output type compatibility.
  - Implemented create-variable, conditional, and loop config/validation for source typing, branch output compatibility, list/object-list inputs, array-to-list conversion warnings, element/index loop variables, aggregation output compatibility, ontology-edit/no-action branches, and action-aware parallelization.
  - Docs: [AIP Logic blocks](https://www.palantir.com/docs/foundry/logic/blocks/).

- [x] `AIPLE.10` Logic outputs (`P0`, `done`)
  - Define final outputs as primitive values, objects, object sets/lists, structs, media references, or Ontology edit bundles where locally supported.
  - Support block intermediary outputs and final Logic function output.
  - Enforce that Workshop Markdown usage requires a string output when using Logic as a display function.
  - Implemented final/intermediate output definitions and validators for supported output families, source compatibility, final-output presence, unique API names, Ontology edit bundles, and Workshop Markdown string outputs.
  - Docs: [AIP Logic getting started](https://www.palantir.com/docs/foundry/logic/getting-started), [AIP Logic core concepts](https://www.palantir.com/docs/foundry/logic/core-concepts/).

### Running, debugging, publishing, and usage

- [x] `AIPLE.11` Run panel and preview execution (`P0`, `done`)
  - Execute draft Logic with sample inputs from the run panel.
  - Show latest result, status, duration, run metadata, recent runs, and save-as-test-case shortcuts.
  - Support single-run rerun and input editing without publishing.
  - Implemented local draft preview execution helpers plus an editable run panel with latest result, run status/duration/metadata, recent run selection, rerun, and save-as-test-case shortcut affordances.
  - Docs: [AIP Logic getting started](https://www.palantir.com/docs/foundry/logic/getting-started).

- [x] `AIPLE.12` Logic debugger (`P0`, `done`)
  - Display block-by-block trace, prompt/tool-call details, inputs, outputs, errors, and final result.
  - Allow expanding/collapsing block cards and clearing local tool-call display state.
  - Ensure logs/traces are security-filtered and retained according to execution mode.
  - Implemented debugger trace builders and UI cards for input binding, LLM prompt/tool calls, final output mapping, expandable block details, local tool-call clearing, sensitive-key redaction, and draft-vs-policy retention metadata.
  - Docs: [AIP Logic getting started](https://www.palantir.com/docs/foundry/logic/getting-started), [AIP Logic core concepts](https://www.palantir.com/docs/foundry/logic/core-concepts/).

- [x] `AIPLE.13` Save, publish, and version history (`P0`, `done`)
  - Save draft Logic versions and publish callable Logic functions.
  - Record version history with author, timestamp, block/input/output changes, prompt changes, model changes, and publish status.
  - Provide comparison view for added, edited, and removed blocks.
  - Implemented persistent `logic_versions` and `logic_functions` resources in `agent-runtime-service`, with draft save, version listing/get, comparison, and publish endpoints under `/api/v1/agent-runtime/logic/files/{id}/versions`.
  - Version summaries compute input, block, output, prompt, and model diffs from saved Logic definitions; publishing marks one current callable function and supersedes the previous published version.
  - The `/logic` authoring shell now wires Save draft and Publish actions into local version history, publish status, callable function display, and a comparison view for added, edited, and removed blocks.
  - Tests cover version diff metadata, draft/publish helper behavior, route validation, and JSON definition/signature guardrails.
  - Docs: [AIP Logic getting started](https://www.palantir.com/docs/foundry/logic/getting-started), [AIP Logic core concepts](https://www.palantir.com/docs/foundry/logic/core-concepts/).

- [x] `AIPLE.14` Function usage surfaces (`P0`, `done`)
  - Use published Logic functions in Workshop, action-backed workflows, other Logic functions, function-on-objects style calls, Automate, and API/curl invocation where supported.
  - Block command-line/API invocation for Logic functions that return Ontology edits when mirroring documented limitations.
  - Show usage snippets and links from the Uses sidebar.
  - Implemented `agent-runtime-service` usage discovery at `/api/v1/agent-runtime/logic/files/{id}/uses`, returning Workshop, action workflow, existing Logic, function-on-objects, Automate, and API/curl surfaces with links, requirements, and snippets for the current published Logic function.
  - Added published Logic API invocation at `/api/v1/agent-runtime/logic/functions/{function_rid}/invoke`, including sample input/output echoing for supported return types and a documented conflict response when the published final output returns Ontology edits.
  - The `/logic` Uses sidebar now shows publish-required state, callable function status, per-surface snippets, direct links, and the API/curl limitation while keeping other surfaces available for Ontology-editing Logic.
  - Tests cover surface snippet generation, published function usage metadata, API/curl blocking for final Ontology edits, forwarded base URL handling, and local Uses helper behavior.
  - Docs: [AIP Logic getting started](https://www.palantir.com/docs/foundry/logic/getting-started), [Functions in Workshop](https://www.palantir.com/docs/foundry/workshop/functions-use/).

- [x] `AIPLE.15` User-scoped execution mode (`P0`, `done`)
  - Execute Logic using the permissions of the initiating user.
  - Restrict execution logs so users only see their own logs.
  - Apply short-lived retention for user-scoped execution logs.
  - Added persistent `logic_runs` execution history in `agent-runtime-service`, with execution mode, permission subject, actor, invocation surface, sanitized log payloads, duration, outputs, and `retention_expires_at`.
  - Published Logic invocation now resolves user-scoped execution context to the initiating user, records a run with `logs_visible_to=initiating_user`, and applies 24-hour retention before returning the run and execution context.
  - Added `/api/v1/agent-runtime/logic/files/{id}/runs`, pruning expired rows and returning user-scoped logs only when `actor_id` matches the requesting user.
  - The `/logic` right rail now includes user-scoped execution settings and run history views that show 24-hour retention and hide peer user logs.
  - Tests cover initiating-user permission subject selection, private user-scoped log visibility, 24-hour retention, run route auth, and frontend run-history filtering.
  - Docs: [AIP Logic execution mode settings](https://www.palantir.com/docs/foundry/logic/execution-mode-settings/).

- [x] `AIPLE.16` Basic Logic metrics (`P0`, `done`)
  - Surface success count, failure count, failure categories, recent run history, and P95 duration over recent time windows.
  - Show metrics from Logic detail, Ontology Manager-like resource views, and Workflow Lineage-like execution nodes.
  - Require viewer permission to see metrics.
  - Added `/api/v1/agent-runtime/logic/files/{id}/metrics`, aggregating retained visible `logic_runs` across `24h`, `7d`, `30d`, and `90d` windows with success/failure counts, categorized failures, recent runs, and P95 duration.
  - Metrics access now reuses Logic file viewer permission checks and preserves user-scoped log privacy by only counting runs visible to the requesting actor.
  - Added shared frontend Logic metrics helpers plus API typings for the metrics endpoint.
  - The `/logic` detail rail now renders a Metrics panel, Ontology Manager usage shows Logic resource metrics, and Workflow Lineage includes a Logic execution node with operational metrics.
  - Tests cover backend metric aggregation/window defaults, metrics route auth, and frontend metrics calculation over retained visible runs.
  - Docs: [AIP Logic metrics](https://www.palantir.com/docs/foundry/logic/logic-metrics/).

### Minimum viable AIP Evals

- [x] `AIPLE.17` Evaluation suite CRUD (`P0`, `done`)
  - Create, get, list, update, move, duplicate, archive/delete, and restore evaluation suites.
  - Track suite name, project/folder, owner, target functions, test case columns, evaluators, run history, results dataset, and permissions.
  - Create evaluation suites from Logic preview, Evals sidebar, AIP Evals app, and code-authored published function surfaces where available.
  - Added persistent `eval_suites` resources in `agent-runtime-service`, with project/folder placement, owner, target function JSON, typed test-case column JSON, evaluator JSON, run-history summaries, result dataset RID, permissions, source surface, source resource, and archive lifecycle metadata.
  - Added authenticated CRUD routes under `/api/v1/agent-runtime/eval-suites` for create, get, list, update, move, duplicate, archive, and restore, reusing owner/editor/viewer permission semantics.
  - Added frontend API helpers for evaluation suites plus a `/aip-evals` management surface that demonstrates suite create/list/detail/update/move/duplicate/archive/restore flows.
  - The `/logic` Evaluations rail can create suites from the latest preview run or sidebar setup, and the Functions page links published/code-authored functions into the AIP Evals creation surface.
  - Tests cover suite source validation, auth/placement validation, JSON-array normalization, source defaulting, and existing Logic suite helpers.
  - Docs: [AIP Evals overview](https://www.palantir.com/docs/foundry/aip-evals/overview/), [Evaluation suites for Logic functions](https://www.palantir.com/docs/foundry/aip-evals/getting-started/), [Create an evaluation suite](https://www.palantir.com/docs/foundry/aip-evals/create-suite).

- [x] `AIPLE.18` Target functions (`P0`, `done`)
  - Add Logic, agent-like, and code-authored functions as target functions.
  - Support multiple target functions in one suite and target-specific evaluator mappings.
  - Validate target function input/output signatures and version availability.
  - Added suite-side target validation for Logic, agent-like/chatbot, and code-authored functions, including unique target IDs, supported kind aliases, version selectors, specific-version IDs, and input/output signature shape.
  - Added evaluator validation that requires target-specific mappings for multi-target suites and checks mapped actual outputs against each target signature plus expected values against test-case columns.
  - Extended `/aip-evals` with a target-function builder, multi-target seeded suite, generated test-case columns from target signatures, and per-target exact-match evaluator mappings.
  - Logic-created evaluation previews now carry function signatures and target-specific evaluator mappings, so suites created from Logic can be validated by the same backend path.
  - Tests cover multi-kind targets, unavailable version/signature rejection, and multi-target evaluator mapping validation.
  - Docs: [Create an evaluation suite](https://www.palantir.com/docs/foundry/aip-evals/create-suite).

- [x] `AIPLE.19` Manual test cases and columns (`P0`, `done`)
  - Add manual test cases with name, typed input columns, expected output columns, metadata, and optional generated name hints.
  - Edit test case parameter columns, reorder columns, and validate column types against target function inputs and evaluator mappings.
  - Support adding a test case from a Logic preview run.
  - Added persistent `test_cases` JSON to evaluation suites, with manual/preview/generated source metadata, case names, generated name hints, column-keyed values, and per-case metadata.
  - Added backend validation for typed input, expected-output, and metadata columns, including target input compatibility, evaluator expected-column mappings, ordered column arrays, test-case value type checks, duplicate IDs, and unknown-column rejection.
  - Extended `/aip-evals` with a test-case editor for adding manual cases, adding preview-style cases, generating name hints, editing column type/role, reordering columns, removing columns, and editing case values inline.
  - Logic preview-created suites now persist the preview inputs, expected output seed, preview metadata, and generated name hint as the first evaluation test case.
  - Tests cover column/target type validation, manual test-case value validation, generated hint fields, missing values, and existing multi-target evaluator mapping checks.
  - Docs: [Evaluation suites for Logic functions](https://www.palantir.com/docs/foundry/aip-evals/getting-started/), [Create an evaluation suite](https://www.palantir.com/docs/foundry/aip-evals/create-suite).

- [x] `AIPLE.20` Built-in evaluators and objectives (`P0`, `done`)
  - Support built-in exact match, regex, distance, length, keyword, object/object-set, integer/numeric/floating-point range, and temporal range evaluators where local type support exists.
  - Configure actual/expected mappings, Boolean objectives, numeric maximize/minimize objectives, and thresholds.
  - Compute metric-level, iteration-level, and test-case-level pass/fail status.
  - Added a web built-in evaluator catalog and local evaluator engine for exact match, regex, Levenshtein distance, length, keyword, object/object-set comparison, numeric/integer/floating-point ranges, and temporal ranges.
  - Extended `/aip-evals` with built-in evaluator creation, target-specific actual/expected mapping controls, Boolean objective controls, numeric maximize/minimize threshold controls, evaluator config editors, and metric/iteration/test-case pass summaries.
  - Added backend validation for built-in evaluator names, regex/keyword/range/temporal configs, Boolean objectives, numeric objectives, thresholds, and multi-target mappings before suites are persisted.
  - Tests cover built-in objective/config validation plus metric-level, iteration-level, and test-case-level pass/fail aggregation.
  - Docs: [Create an evaluation suite](https://www.palantir.com/docs/foundry/aip-evals/create-suite), [Run an evaluation suite](https://www.palantir.com/docs/foundry/aip-evals/run-suite/).

- [x] `AIPLE.21` Evaluation run basics (`P0`, `done`)
  - Run full suites and single test cases from AIP Logic sidebar and AIP Evals app.
  - Support target version selection for last-saved Logic, published Logic, and published non-Logic functions.
  - Display aggregate pass percentage, individual test case results, metric results, errors, and debugger links.
  - Added a shared built-in evaluation run engine that runs full suites or selected test cases, applies target version selections, computes aggregate pass rate, metric results, test-case results, errors, duration, and debugger links.
  - Extended `/aip-evals` with run configuration controls, target version selectors, full-suite runs, single-test-case runs, latest result cards, metric rows, errors, and debugger links.
  - Extended the AIP Logic Evaluations sidebar with suite runs, single-test-case execution, target version selectors, latest result status, per-case results, and debugger entry points.
  - Tests cover full-suite and single-case run execution, target version options, aggregate pass percentages, metric results, and debugger link generation.
  - Docs: [Run an evaluation suite](https://www.palantir.com/docs/foundry/aip-evals/run-suite/), [Analyze run results](https://www.palantir.com/docs/foundry/aip-evals/analyze-run-results).

## Milestone B: credible Foundry-style AIP Logic and Evals parity

### Advanced Logic execution and integration

- [x] `AIPLE.22` Project-scoped execution mode (`P1`, `done`)
  - Execute Logic using project permissions when configured.
  - Require all used resources to be imported into the same project and require users to have marking/security access where applicable.
  - Make project-scoped logs visible to project viewers and preserve configurable run history.
  - Project-scoped invocation now selects the project permission subject and switches the logic security boundary, run logs, and run-history dataset visibility to project viewers; project-scoped runs require project import and marking access on used object types, action types, functions, and media references, and a permissioned run-history dataset RID.
  - Added a new `logic_files.run_history_max_rows` column (and optional `run_history_dataset_rid` override) so the configurable run history limit and dataset RID are persisted per Logic file; create/update/duplicate flows accept and clamp the configured value between 1 and 1,000,000.
  - The published Logic invocation pipeline reads the configured limit/override from the Logic file before producing the execution context, and project-scoped append-and-prune semantics now use the configured limit when retaining run-history dataset rows.
  - The Logic authoring Execution settings panel exposes the per-file project-scoped mode, dataset RID override, and configurable max rows, and reflects the policy in the project run-history dataset preview.
  - Tests cover configurable run history settings (limit clamping, override RID, empty fallback) and confirm user-scoped contexts ignore the configurable settings while project-scoped contexts apply them.
  - Docs: [AIP Logic execution mode settings](https://www.palantir.com/docs/foundry/logic/execution-mode-settings/).

- [x] `AIPLE.23` Logic run history dataset (`P1`, `done`)
  - Configure a dataset that records project-scoped Logic execution history.
  - Preserve recent run rows up to a documented or locally configured limit.
  - Include inputs, outputs, status, errors, duration, model, branch/version, user/service context, and trace references subject to permissions.
  - Added project-scoped run-history dataset configuration helpers with stable dataset RID generation, documented 10,000-row default preservation, explicit schema columns, and append-and-prune semantics.
  - Extended `logic_runs` with dataset RID, dataset-row JSON, trace references, branch name, model, and service-context metadata; project-scoped invocations now write one permission-scoped dataset row per execution and prune older rows by configured limit.
  - Dataset rows include inputs, outputs, status, errors, duration, resolved model, branch name, published version ID/number, initiating user, project permission subject, service context, and debugger/lineage trace references.
  - The Logic authoring Execution settings and Run history panels now configure and preview the project run-history dataset, row limit, permission-scoped schema, latest dataset row, and visible retained rows.
  - Tests cover dataset config generation, row construction, row-limit pruning, project-scoped execution context dataset metadata, and persisted row contents.
  - Docs: [AIP Logic execution mode settings](https://www.palantir.com/docs/foundry/logic/execution-mode-settings/).

- [x] `AIPLE.24` Automate integration (`P1`, `done`)
  - Create pre-populated automations from the Logic Uses sidebar when Logic outputs Ontology edits.
  - Support automatic application of edits or staging of action proposals for human review.
  - Show automation event chart, proposals tab, agent proposal detail, proposed action preview, and decision log handoff.
  - Added Logic-to-Automate helpers that detect Ontology edit outputs, build pre-populated automation drafts from published Logic functions, generate workflow payloads, event charts, staged/action-applied proposals, proposed action previews, and decision-log handoff entries.
  - Updated the Logic Uses sidebar so the Automate surface links to `/automate` with function/version/output/action context, shows a pre-populated automation snippet, and blocks Create Automation when no Ontology edit output is available.
  - Added the Logic Automations right-rail panel with edit mode selection, pre-populated draft summary, event chart, proposal detail, proposed action preview, and decision-log handoff.
  - Added an `/automate` app route that can open from Logic Uses, switch between automatic edit application and staged human review, preview the workflow payload, inspect proposals, approve/reject staged proposals, and view the decision log handoff payload.
  - Tests cover Automate availability/blocking, pre-populated draft generation, staged proposals, event buckets, proposed action previews, and approval decision-log handoff.
  - Docs: [AIP Logic integration with Automate](https://www.palantir.com/docs/foundry/logic/aip-logic-integration-automate/), [Automate overview](https://www.palantir.com/docs/foundry/automate/overview/).

- [x] `AIPLE.25` Logic-backed actions (`P1`, `done`)
  - Create function-backed action types that invoke published Logic functions.
  - Support action execution from Workshop and branch-aware preview contexts.
  - Ensure real Ontology edits are only applied through action execution or approved automation flows.
  - Added Logic-backed action draft helpers that turn a published Logic version into an `invoke_function` action type with Logic input schema, HTTP invocation config, form schema, permission policy, Workshop button context, branch-preview context, and guardrails for Ontology edit application.
  - Updated Logic Uses so the action-backed workflow surface opens `/action-types` with a pre-populated action type draft, shows the action JSON snippet, and exposes Workshop/branch preview payloads plus edit-application guardrails.
  - Added Action Types support for Logic-origin prefilled drafts from Uses links and branch-aware execution context controls in Operate.
  - Propagated action execution context through Workshop submissions, action buttons, generic action execution, validation, single execution, and batch execution calls.
  - Tests cover Logic-backed action draft generation, action workflow snippets/links, and edit-application policy that blocks raw Logic/API edits while allowing action execution, branch preview, and approved automation.
  - Docs: [AIP Logic getting started](https://www.palantir.com/docs/foundry/logic/getting-started), [Action types overview](https://www.palantir.com/docs/foundry/action-types/overview).

- [x] `AIPLE.26` Logic compute usage metering (`P1`, `done`)
  - Meter usage per executed block and account for downstream systems invoked by blocks.
  - Attribute usage to Logic file, version, block, user/project, Automate run, action, Workshop widget, or Evals run.
  - Surface cost/usage warnings before expensive run/evaluation/experiment configurations.
  - Added compute-usage domain types and helpers that meter LLM block execution, LLM tool execution, downstream Ontology/action/function calls, Evals target invocations, and built-in evaluator work in compute-seconds.
  - Propagated compute summaries into debugger traces, draft preview metadata, visible run-history records, and project-scoped run-history dataset rows with attribution for Logic file/version, block, actor/project, permission subject, Workshop widget, Automate/action, and Evals run surfaces.
  - Added Logic authoring UI warnings and summaries before draft runs and evaluation suite runs, plus a Compute right-rail panel that breaks down line items, downstream usage, latest run actuals, Evals plans, and attribution.
  - Added AIP Evals app metering for multi-target suite runs and single-case runs, including target/evaluator counts, run-history compute totals, and pre-run warnings for expensive configurations.
  - Tests cover block/tool/downstream metering, definition-level and Evals-level estimates, attribution, warning thresholds, debugger/run metadata, run-history dataset rows, and AIP Evals run compute results.
  - Docs: [AIP Logic compute usage](https://www.palantir.com/docs/foundry/logic/compute-usage).

- [x] `AIPLE.27` Branching AIP Logic adapter (`P1`, `done`)
  - Add, remove, edit, publish, review, rebase, and merge Logic files on Global Branches.
  - Enforce merge requirements such as published state, up-to-date with main, no pending approvals, and publishable state.
  - Keep branched Logic versions isolated from main and other branches.
  - Added a branch adapter model for Logic files with branch resource RIDs, isolated branch draft versions, branched pre-release publications, add/remove/edit/publish/review/rebase/merge operation logs, proposal reviews, manual rebase conflict tracking, and merge results that publish a new main version only after checks pass.
  - Enforced merge checks for active resource presence, branch publication, up-to-date main base, publishable Logic state, and no pending/rejected approvals; removal is blocked once a branch Logic publication exists to mirror documented limitations.
  - Surfaced a Logic authoring Branching rail with branch version/main version status, branch-only callable availability, review controls, rebase/merge controls, merge check details, conflicts, and adapter operation history.
  - Tests cover branch/main/other-branch isolation, branched pre-release availability, merge requirement blocking, reviewer approval flow, merge-to-main publication, manual rebase conflicts, removal behavior, and publishability validation.
  - Docs: [Branching AIP Logic](https://www.palantir.com/docs/foundry/logic/branching-logic).

- [x] `AIPLE.28` Logic permissions and security (`P1`, `done`)
  - Enforce resource view/edit/manage permissions on Logic files and function invocation permissions on published Logic functions.
  - Enforce user/project permission boundaries for tools, object queries, actions, functions, media references, and result datasets.
  - Ensure LLM-accessible data is limited to explicitly configured and permissioned resources.
  - Added separate Logic file owner/manager/editor/viewer/invoker permissions, with manage-only metadata permission edits and published function invocation restricted to owners, managers, editors, invokers, or admins.
  - Added backend security-boundary evaluation before published Logic execution to reject unpermissioned object queries, action tools, function tools, media inputs, run-history datasets, and project-scoped resources missing project imports or marking access.
  - Added shared web helpers that compute permission decisions and LLM-accessible resource exposure for inputs, tools, functions, actions, media sets, and result datasets.
  - Surfaced a Logic authoring Security rail with view/edit/manage/invoke checks, effective user/project subject, LLM-accessible resources, explicit/permissioned/imported/marking status, and actionable security issues.
  - Tests cover default permission separation, invocation/view distinction, explicit resource allowlists, project-scoped import/marking requirements, and blocked media/function/dataset access outside policy.
  - Docs: [AIP Logic overview](https://www.palantir.com/docs/foundry/logic), [AIP Logic execution mode settings](https://www.palantir.com/docs/foundry/logic/execution-mode-settings/).

### Advanced AIP Evals suite construction

- [x] `AIPLE.29` Object-set-backed test cases (`P1`, `done`)
  - Add test cases from object sets and map object, object property, linked object, linked object set, linked property, and static value columns.
  - Support multiple object sets plus manual test cases in one suite.
  - Recompute object-set-backed rows according to local snapshot/refresh semantics.
  - Added shared Evals object-set helpers for column mappings from backing objects, object properties, linked objects, linked object sets, linked properties, and static values into suite test-case columns.
  - Added snapshot-vs-refresh recomputation that preserves manual cases, supports multiple object-set backings in one suite, keeps snapshot rows stable until forced, and refreshes dynamic rows from the latest object-set evaluation.
  - Surfaced object-set-backed test-case controls in the AIP Evals app, including snapshot source creation, refresh source creation, recomputation, source chips, generated row metadata, and object-set source JSON.
  - Extended API typings and backend test-case normalization to accept `object_set` test-case sources with required object-set/object/backing metadata.
  - Tests cover all mapping kinds, multiple object sets plus manual rows, snapshot/refresh semantics, and backend validation for object-set-backed cases.
  - Docs: [Create an evaluation suite](https://www.palantir.com/docs/foundry/aip-evals/create-suite).

- [x] `AIPLE.30` Custom evaluation functions (`P1`, `done`)
  - Select published TypeScript/Python/Logic functions as evaluators.
  - Require at least one Boolean or numeric metric return value and allow struct returns containing multiple metrics.
  - Store string debug outputs from custom evaluators in debug views without treating them as metrics.
  - Added shared custom evaluator helpers that select published TypeScript, Python, or Logic function RIDs, flatten return signatures, extract Boolean/numeric metrics from top-level or struct returns, and separate string return values as debug outputs.
  - Extended suite execution to invoke custom evaluators alongside built-ins, apply per-metric objectives, compute metric/iteration/test-case pass status from metrics only, and carry custom string debug outputs into run/debug views without counting them as metrics.
  - Surfaced custom evaluator creation in the AIP Evals app with published function kind/RID controls, target-specific mappings, metric/debug return chips, validation feedback, metered evaluator compute, and run-result debug output previews.
  - Extended backend evaluator normalization to validate published custom TypeScript/Python/Logic evaluator functions, require return signatures with at least one Boolean or numeric metric, allow struct returns with multiple metrics, and accept string debug returns.
  - Tests cover struct metric extraction, string debug-output separation, custom evaluator compute metering, invalid debug-only evaluators, draft-version rejection, and backend normalization.
  - Docs: [Create an evaluation suite](https://www.palantir.com/docs/foundry/aip-evals/create-suite), [Analyze run results](https://www.palantir.com/docs/foundry/aip-evals/analyze-run-results).

- [x] `AIPLE.31` Marketplace evaluator handoff (`P1`, `done`)
  - Support installed evaluator functions such as rubric grader, contains-key-details, and ROUGE-like evaluators when OpenFoundry Marketplace/product packaging exists.
  - Open setup wizard and dependency installation flow when an evaluator product is missing.
  - Added an OpenFoundry Marketplace evaluator catalog for rubric grader, contains-key-details, and ROUGE score packages, including published evaluator function RIDs, return signatures, metric objectives, capabilities, dependency plans, and setup links.
  - Added Marketplace product packaging seed data for the evaluator listings and package versions, including function packaged resources, manifest metric/debug metadata, and install dependency metadata.
  - Extended AIP Evals to show installed vs setup-required Marketplace evaluators, add installed packages as published evaluator functions, and open a setup panel with dependency installation steps when a package is missing.
  - Extended evaluator validation to accept Marketplace-backed evaluator functions only when product packaging metadata exists and the package is installed before run execution.
  - Tests cover installed package handoff, missing package setup plans, dependency plans, runnable Marketplace evaluator metrics/debug outputs, and backend rejection of uninstalled or unpackaged Marketplace evaluators.
  - Docs: [Create an evaluation suite](https://www.palantir.com/docs/foundry/aip-evals/create-suite).

- [x] `AIPLE.32` Intermediate parameters (`P1`, `done`)
  - Expose selected block outputs as intermediate parameters from Logic authoring UI.
  - Use intermediate parameters as evaluator inputs and include their values in project-scoped results datasets.
  - Support evaluating final outputs and intermediate outputs in the same suite.
  - Added Logic output metadata for exposed intermediate parameters, draft preview values for block-level outputs, and project-scoped run-history dataset rows with `intermediate_parameters`.
  - Extended the Logic authoring Outputs board to toggle exposed block outputs, preview intermediate values, and feed selected parameters into generated AIP Evals suites.
  - Extended suite execution result datasets to include target outputs and intermediate parameter values, with evaluator metric rows retaining actual/expected mapping names for final and intermediate output comparisons.
  - Backend eval-suite validation now accepts `intermediate_parameter` columns, validates them against target output signatures, and treats their generated values as optional in manual test cases.
  - Tests cover draft intermediate values, run-history dataset schema/rows, eval result dataset rows for intermediate parameters, and backend normalization/validation.
  - Docs: [Use intermediate parameters to evaluate block output](https://www.palantir.com/docs/foundry/logic/evaluations-intermediate-parameters/).

- [x] `AIPLE.33` Ontology-edit evaluations (`P1`, `done`)
  - Execute Logic functions that create/edit/delete Ontology objects inside an Ontology simulation during evaluation.
  - Support custom evaluation functions and intermediate parameters to verify simulated edits.
  - Ensure simulated edits cannot alter the real Ontology during tests.
  - Added a local Ontology simulation runner for evaluation outputs that applies create, edit, and delete operations to per-test-case simulated objects while returning `realOntologyMutated=false` and no committed writes.
  - Extended AIP Evals run output rows with Ontology simulation summaries, proposed edit bundles, created/edited/deleted object snapshots, and safety metadata for debugger/result dataset views.
  - Added an Ontology edit simulation suite in the AIP Evals app with a published Logic target, custom TypeScript evaluator metrics for simulated edits, and an intermediate-parameter evaluator for block output verification.
  - Backend evaluator normalization now blocks built-in evaluators from directly consuming `ontology_edit_bundle` outputs and requires a custom evaluator function or intermediate-parameter mapping instead.
  - Tests cover create/edit/delete simulation, custom evaluator metrics, intermediate parameter verification, no-real-write safety flags, and backend validation for Ontology edit evaluator mappings.
  - Docs: [Evaluate Ontology edits](https://www.palantir.com/docs/foundry/aip-evals/ontology-edits).

- [x] `AIPLE.34` Eval run configuration (`P1`, `done`)
  - Support target version selection, input mapping, user/project execution mode, iteration count, test parallelization, and run metadata.
  - Recommend multiple iterations for LLM-backed functions and warn about rate limits at high parallelization.
  - Preserve branch, version, model, custom metadata, execution mode, and run initiator.
  - Extended `BuiltInEvaluationRunConfig` with `iterations`, `parallelization`, `executionMode`, per-target `inputMappings`, and rich `metadata` (run initiator, branch, model, custom labels, custom metadata, notes); the run engine clamps iterations to 1–10 and parallelization to 1–32, resolves target/column input mappings with identity fallback, and propagates the values to the run result `config` object.
  - Added a `warnings` array on every run result populated by `computeEvaluationRunWarnings`: LLM-backed targets/evaluators with a single iteration emit `llm_iteration_recommendation`, parallelization above the documented rate-limit threshold emits `parallelization_rate_limit`, sub-1 parallelization emits `parallelization_disabled`, and partial input-mapping configurations emit `input_mapping_missing`.
  - Run results now preserve branch, version (via `targetVersions`), model, custom labels, custom metadata, execution mode, and the run initiator, so AIP Evals comparisons and result datasets can read them downstream.
  - Extended the AIP Evals run panel with iteration count, test parallelization, execution mode, custom-label, and notes controls plus a per-run warning surface; the result card now shows iterations, parallelization, execution mode, run initiator, and custom labels.
  - Tests cover the new run config plumbing (iterations, parallelization, execution mode, input mappings, metadata propagation), the LLM iteration / parallelization rate-limit warnings, and the unmapped-input warning.
  - Docs: [Run an evaluation suite](https://www.palantir.com/docs/foundry/aip-evals/run-suite/).

- [x] `AIPLE.35` Multi-target runs and comparisons (`P1`, `done`)
  - Run the same suite against multiple target functions and choose included targets per run.
  - Compare results across target functions, versions, and models.
  - Disable incompatible experiment options in multi-target mode when required.
  - Extended `BuiltInEvaluationRunConfig` with `targetIds` filtering (already supported) and a new `targetModels` map for per-target model selection, so the same suite can run against any subset of its target functions in one invocation; the run result carries `targetIds`, `targetVersions`, and `targetModels` so downstream comparisons can group iterations by target, version, and model.
  - Added `buildMultiTargetRunComparison` and `evaluationRunMultiTargetCapabilities` helpers. The comparison returns per-target pass-rate summaries (target id, kind, version, model, pass/fail counts) and per-metric breakdowns with `bestTargetId`/`worstTargetId`; the capability helper reports the `multi_target` flag and lists `disabledExperimentOptions` (`per_target_prompt_sweep`, `single_target_grid_search`, `per_target_evaluator_threshold_sweep`) with reasons.
  - The run engine emits new `multi_target_experiment_disabled`, `multi_target_single_target_run`, and `multi_target_no_targets` warnings so callers can surface the disabled experiment options and remind users to include additional targets for cross-target comparison; the run result now includes `multiTargetComparison` for the run summary surface.
  - Updated the AIP Evals run panel with per-target inclusion checkboxes, an "include all targets" reset, per-target model override fields, a multi-target capability banner listing disabled experiment options, and a comparison panel showing per-target pass rate, version/model, and per-metric breakdowns with best/worst markers.
  - Tests cover multi-target run filtering, per-target model preservation, the comparison summary structure, the disabled experiment option warnings, single-target-from-multi-target warning, capability helper output, and the empty-comparison fallback.
  - Docs: [Create an evaluation suite](https://www.palantir.com/docs/foundry/aip-evals/create-suite), [Run an evaluation suite](https://www.palantir.com/docs/foundry/aip-evals/run-suite/), [Analyze run results](https://www.palantir.com/docs/foundry/aip-evals/analyze-run-results).

- [x] `AIPLE.36` Results dataset integration (`P1`, `done`)
  - Configure a run results dataset in the same project as the evaluation suite.
  - Write function outputs, evaluator results, user metadata, auto-captured metadata, errors, and intermediate parameters when project-scoped execution is used.
  - Document unsupported output cases such as functions that only return Ontology edits.
  - Added `EvaluationResultsDatasetConfig`, `EvaluationResultsDatasetRow`, and `EvaluationResultsDatasetWrite` types plus `createEvaluationResultsDatasetConfig`, `buildEvaluationResultsDatasetWrite`, `evaluationResultsDatasetRid`, and `evaluationResultsDatasetUnsupportedTargets` helpers; dataset RIDs are derived from the suite's `projectId` (and suite id) so the configured results dataset always lives in the same project as the evaluation suite.
  - The dataset schema includes `inputs`, `outputs`, `intermediate_parameters`, `evaluator_results`, `custom_evaluator_debug_outputs`, `errors`, `user_metadata` (run initiator, custom labels, custom metadata, notes), `auto_captured_metadata` (execution mode, branch, model, project id, started/completed timestamps, source, iterations, parallelization), and `ontology_simulation`; permission-scoped columns are flagged so project-scoped permissions can filter sensitive data.
  - `runEvaluationSuiteBuiltIns` now calls `buildEvaluationResultsDatasetWrite` and writes rows only when `executionMode === 'project_scoped'` and the suite has a project id; user-scoped runs short-circuit to `reason: 'user_scoped_execution_skipped'`, missing-project suites short-circuit to `no_project_id`, and runs where every included target only returns Ontology edits short-circuit to `no_supported_target_outputs`. The result also documents per-target skipped rows for ontology-edits-only targets.
  - Added matching warnings (`results_dataset_skipped_user_scoped`, `results_dataset_missing_project`, `results_dataset_unsupported_target`, `results_dataset_no_target_outputs`) so callers can surface why rows weren't written.
  - The AIP Evals run panel now shows the results dataset RID, project id, max rows, schema, latest written row, unsupported targets, and the skip reason chip.
  - Tests cover the per-project dataset RID derivation, project-scoped row writes (with run initiator / labels / metadata / notes / model / branch preserved), the user-scoped skip path, the missing-project skip path, mixed-target skipping for ontology-edits-only targets, the no-supported-target-outputs warning, and the custom RID + max-rows override clamping.
  - Docs: [Write run results to a dataset](https://www.palantir.com/docs/foundry/aip-evals/results-dataset).

### Evals result analysis and dashboards

- [x] `AIPLE.37` Results table and debug view (`P1`, `done`)
  - Show aggregate metrics, per-test-case results, iterations, inputs, expected values, actual outputs, evaluator outputs, debug strings, and errors.
  - Open debug view for individual test cases, including Logic trace, code function preview, evaluator trace, expected vs actual values, and custom evaluator debug outputs.
  - Added `EvaluationResultsTable`, `EvaluationResultsTableRow`, `EvaluationResultsTableEvaluatorOutput`, `EvaluationResultsTableDebugOutput`, `EvaluationDebugView`, and `EvaluationDebugTraceStep` types plus `buildEvaluationResultsTable` and `buildEvaluationDebugView` helpers; rows now expose test-case name, target (kind/version/model), iteration, status, inputs, expected values (resolved from evaluator mappings on the test case), actual outputs, intermediate parameters, per-evaluator metric breakdown, custom evaluator debug strings, errors, and a debugger URL.
  - The debug view returns a per-iteration object with all of the above plus an ordered `traceSteps` list — `logic_trace` (or `agent_like` variant) with version/model/inputs/outputs/intermediate parameters for Logic and agent-like targets, `code_function_preview` (function RID + signature + IO) for code-authored targets, optional `intermediate_parameters` and `ontology_simulation` steps when relevant, and an `evaluator_trace` step with the full per-evaluator metric breakdown.
  - Wired a Results table + debug view panel into the AIP Evals app: an aggregate-metric header, a sortable per-iteration table (status, target kind/version/model, expected vs actual previews, evaluator chips with hover-over reason, debug strings/errors), and an inline debug view drawer with inputs/expected/actual cards, intermediate parameters, expandable trace steps, evaluator trace cards, custom evaluator debug strings, and per-test-case errors.
  - Tests cover the results-table aggregation and row shape (inputs object, expected values, actual outputs, evaluator outputs with reasons, custom evaluator debug strings, debugger href), the debug-view iteration filter (with `iteration=N` URL parameter, logic trace + evaluator trace steps), and the code-function-preview trace step for code-authored targets.
  - Docs: [Analyze run results](https://www.palantir.com/docs/foundry/aip-evals/analyze-run-results), [Run an evaluation suite](https://www.palantir.com/docs/foundry/aip-evals/run-suite/).

- [x] `AIPLE.38` Run-to-run comparison (`P1`, `done`)
  - Compare two evaluation runs side by side and highlight output differences.
  - Compare aggregate metric changes, per-test-case status changes, model/version metadata, and evaluator output differences.
  - Added `EvaluationRunComparison`, `RunComparisonAggregateMetricChange`, `RunComparisonTestCaseChange`, `RunComparisonTargetMetadata`, `RunComparisonEvaluatorDiff`, `RunComparisonOutputDiff`, and `RunComparisonSummary` types plus a `compareEvaluationRuns(suite, baseRun, headRun)` helper that returns base/head pass-rate + pass-count deltas, per-evaluator aggregate metric deltas with `improved`/`regressed`/`unchanged`/`only_in_*` change kinds, per-(test case, target, iteration) status changes (`newly_passed`, `newly_failed`, `still_passed`, `still_failed`, `only_in_base`, `only_in_head`) with output diffs keyed by output api name, per-target version/model metadata diffs (with `versionChanged`/`modelChanged` flags), and per-iteration evaluator diffs (`passed_changed`/`metric_value_changed`/`only_in_*`).
  - Wired a `RunComparisonPanel` into the AIP Evals app with base/head run selectors, aggregate cards (pass rate Δ, iterations Δ, summary counts), target-metadata change cards, an aggregate-metric-changes table, a per-test-case status-change table with inline output-diff previews and base/head debugger trace links, and an evaluator-diff table for flipped or value-changed metrics.
  - Tests cover the happy path where a Logic last_saved run flips a failing case to passing (assertions on `passRateDelta`, `newly_passed`, aggregate improvement, target-metadata version/model diff, output diff on `finalAnswer`, and `passed_changed`/`metric_value_changed` evaluator diff), only-in-base/only-in-head accounting when comparing disjoint test case sets, and the no-op comparison when two identical runs produce unchanged aggregates and an empty evaluator diff array.
  - Docs: [Run an evaluation suite](https://www.palantir.com/docs/foundry/aip-evals/run-suite/), [Analyze run results](https://www.palantir.com/docs/foundry/aip-evals/analyze-run-results).

- [x] `AIPLE.39` Metrics dashboard (`P1`, `done`)
  - Provide charts/statistics for aggregate evaluator metrics and individual test case metrics.
  - Support drill-down into LLM trace viewer and evaluator trace for individual test cases.
  - Support filtering by suite, run, version, model, metric, status, test case, target, and time window.
  - Added `EvaluationMetricsDashboard`, `EvaluationMetricsDashboardFilters`, `EvaluationMetricsDashboardFilterOptions`, `EvaluationMetricsDashboardTrendPoint`, `EvaluationMetricsDashboardTargetStat`, `EvaluationMetricsDashboardTestCaseStat`, `EvaluationMetricsDashboardMetricStat`, `EvaluationMetricsDashboardDrillDown`, and `EvaluationMetricsDashboardInputEntry` types plus `applyMetricsDashboardFilters` and `buildEvaluationMetricsDashboard` helpers; the dashboard aggregates iterations across many suites and runs, computes pass-rate trend, per-target stats, worst-performing test-case stats, per-metric pass-rate + average-value stats, and drill-down links.
  - Filtering supports suite, run, target, version, model, metric name, status, test case, and time window (`startIso`/`endIso`); each filter combines additively so the dashboard recomputes against the filtered iterations and the drill-down list updates in step.
  - Drill-down links include the per-iteration debug view (the AIPLE.37 debugger), an LLM trace deep-link for Logic and agent-like targets, and an evaluator trace deep-link for every iteration so users can jump from a dashboard row into the trace viewer.
  - Wired a `MetricsDashboardPanel` (with `MultiSelectFilter` chip groups) into the AIP Evals app showing overall pass-rate / iterations / duration stats, a pass-rate trend bar chart, an aggregate metric pass-rate bar chart, per-target stats, worst-performing test cases, and a drill-down table with Debug view / LLM trace / Evaluator trace buttons per iteration.
  - Tests cover the dashboard end-to-end (totals, trend, filter options, target stats, drill-down LLM trace presence for Logic targets and absence for code-authored targets) and the filtering helper (`versions`, `status`, `testCaseIds`, `models`, `targetIds`, `metricNames`, and `timeWindow.startIso` future-window exclusion).
  - Docs: [View results in metrics dashboard](https://www.palantir.com/docs/foundry/logic/evaluations-metrics-dashboard/).

- [x] `AIPLE.40` Results analyzer (`P1`, `done`)
  - Generate LLM-assisted failure-pattern summaries for failed test cases.
  - Group failures into root-cause categories with examples, affected test case filters, and prompt suggestions.
  - Configure analyzer model, max categories, and max failing test cases.
  - Added `EvaluationResultsAnalyzerConfig`, `EvaluationResultsAnalyzerConfigResolved`, `EvaluationResultsAnalyzerCategory`, `EvaluationResultsAnalyzerExample`, `EvaluationResultsAnalyzerCategoryKind`, and `EvaluationResultsAnalyzerReport` types plus `buildEvaluationResultsAnalyzer` helper. The categorizer classifies failed iterations from `run.resultDatasetRows` into 14 root-cause buckets — `permission_error`, `validation_error`, `exact_mismatch`, `regex_mismatch`, `length_constraint`, `keyword_missing`, `numeric_out_of_range`, `temporal_out_of_range`, `object_mismatch`, `object_set_mismatch`, `ontology_edit_simulation`, `custom_evaluator_failure`, `runtime_error`, and `other` — using error-message heuristics, evaluator/metric kind, and ontology-simulation presence.
  - Each category carries a curated `promptSuggestion` template, the affected test case ids (`filterChip.testCaseIds`) for downstream filtering, the affected target ids, and up to `maxFailingTestCases` example iterations with actual/expected values, evaluator reasons, errors, and a deep-link to the AIPLE.37 debug view; categories sort by failure count (then by canonical category order) and the top `maxCategories` are returned with `remainingCategoriesCount` tracking truncated buckets.
  - The helper resolves config defaults (`openfoundry.analyzer.v1` analyzer model, `maxCategories=5`, `maxFailingTestCases=5`) and short-circuits with `unsupportedReason='no_iterations'` or `'no_failures'` when nothing to analyze; the summary string reports the model, total failing iterations, total test cases, and category count.
  - Wired a `ResultsAnalyzerPanel` into the AIP Evals app with model / max-categories / max-failing-test-cases inputs, per-category cards (failure count chip, affected test cases / targets chips, example rows with debug-view links, and a prominent prompt-suggestion box), and a remaining-categories counter when the cap kicks in.
  - Tests cover the analyzer happy path (multiple categories including `exact_mismatch` and `numeric_out_of_range` with examples + debugger href), the no-failures short-circuit (`unsupportedReason='no_failures'`), and the `maxCategories` cap with `remainingCategoriesCount` populated.
  - Docs: [Analyze run results](https://www.palantir.com/docs/foundry/aip-evals/analyze-run-results).

## Milestone C: advanced experiments, governance, and production readiness

### Experiments and production quality

- [x] `AIPLE.41` Eval experiments (`P2`, `done`)
  - Enable grid-search experiments over Logic/function parameters such as model, prompt context, thresholds, and evaluator settings.
  - Preview total run count and parameter combinations before execution.
  - Group experiment results by parameter and compare up to locally configured run limits.
  - Added `EvaluationExperimentDimension`, `EvaluationExperimentDimensionKind` (`target_model`, `target_version`, `prompt_variable`, `evaluator_threshold`, `evaluator_config`, `iterations`, `parallelization`), `EvaluationExperimentConfig`, `EvaluationExperimentBaseConfig`, `EvaluationExperimentCombination`, `EvaluationExperimentCombinationParameter`, `EvaluationExperimentPlan`, `EvaluationExperimentRunResult`, `EvaluationExperimentGroupByDimension`, `EvaluationExperimentGroupValue`, and `EvaluationExperimentResults` types plus `buildEvaluationExperimentPlan` and `runEvaluationExperiment` helpers.
  - The plan builder validates dimensions, computes the cartesian product of dimension values, clamps the executed combinations to `maxRuns` (default 24, hard cap 200), estimates compute usage from the per-run plan multiplied by combination count, and emits warnings when the grid exceeds `maxRuns` or 50 combinations.
  - The runner applies each combination to the base run config — `target_model` sets `targetModels[targetId]`, `target_version` sets `targetVersions[targetId]`, `prompt_variable` writes `metadata.customMetadata.promptVariables[targetId][parameterName]`, `evaluator_threshold` and `evaluator_config` write `metadata.customMetadata.evaluatorOverrides[evaluatorId]`, `iterations`/`parallelization` set their named config fields, and stamps `experimentCombinationId` / `experimentCombinationIndex` / `attribution.experimentRunId` so downstream surfaces can group runs back to the experiment.
  - Results group runs by each dimension with `value`, `runCount`, `iterationCount`, `passCount`, `failureCount`, and `averagePassRate`; bestCombinationId / worstCombinationId mark the extremes for comparison.
  - Wired an `EvalExperimentsPanel` into the AIP Evals app with a dimension editor (kind, target, evaluator, parameter name, comma-separated values), `maxRuns` clamp, live plan preview (executed/total combinations, estimated compute seconds, truncation warning, first eight combinations), a run-experiment button, summary card with best/worst combination, group-by-dimension bar charts, and a per-combination results table.
  - Tests cover the plan preview (cartesian product, max_runs truncation, parallelization_rate_limit warning), invalid configurations (missing dimensions, empty values), and the runner (per-target-version grouping, best/worst combination ids, embedded `experimentCombinationId` metadata on each underlying run).
  - Docs: [Run experiments](https://www.palantir.com/docs/foundry/aip-evals/experiments/).

- [x] `AIPLE.42` Model/prompt parameterization (`P2`, `done`)
  - Let Logic authors parameterize model selection and prompt fragments as inputs for experiments.
  - Configure LLM blocks to use model variables and prompt variables safely.
  - Track prompt suggestion application and follow-up evaluation evidence.
  - Built on the existing `LogicLlmBlockConfig.modelBinding` (`fixed` / `model_variable`) and `promptVariableRefs` plumbing from AIPLE.5: added `LogicPromptVariableSafety` (`value` | `fragment`, optional `allowedValues`, optional `maxLength`), `LogicModelVariableSafety` (allowed model ids + default), `LogicParameterizationSafetyConfig`, and `LogicParameterizationResolution` types plus a `resolveLogicBlockParameterization` helper that resolves the model id from the variable or default, substitutes `{{variable}}` placeholders, enforces per-variable allowed-value lists and `maxLength`, escapes `value`-kind variables (collapsing role-tag prompt-injection markers and back-tick fences), and refuses unauthorized fragments — the AIP Evals experiment grid can now sweep `target_model`, `target_version`, and `prompt_variable` dimensions through this safe path.
  - Added `PromptSuggestionApplication`, `PromptSuggestionEvidence`, and `PromptSuggestionEvidenceSummary` types plus `trackPromptSuggestionApplication` and `evaluatePromptSuggestionEvidence` helpers in `apps/web/src/lib/evals/builtins.ts`. Suggestions captured from the AIPLE.40 analyzer carry the source run id, category, applied-by user, applied-to function rid, optional notes, and an optional follow-up run id; the evidence helper compares the base and follow-up runs on the affected test case set and reports pass-rate delta, failure-count delta, affected-failure delta, and a `resolved` flag.
  - Wired the prompt-suggestion lifecycle into the AIP Evals `ResultsAnalyzerPanel`: each analyzer category now has an Apply suggestion button that records a `PromptSuggestionApplication`, and a new "Applied prompt suggestions" subsection shows the recorded suggestions with a follow-up run selector. Selecting a follow-up run reruns `evaluatePromptSuggestionEvidence` and renders the base + follow-up pass rates, affected-failure deltas, and a resolved badge so authors can see whether the suggestion actually closed the failure category.
  - Tests cover the safe parameterization helper (model variable allowed-list enforcement, fragment allow-list, value-kind escaping with role-tag warnings, max-length rejection) and the prompt-suggestion lifecycle (application metadata, evidence without follow-up, evidence with follow-up showing positive pass-rate delta and `resolved=true` when the affected failures clear).
  - Docs: [Run experiments](https://www.palantir.com/docs/foundry/aip-evals/experiments/), [AIP Logic blocks](https://www.palantir.com/docs/foundry/logic/blocks/).

- [x] `AIPLE.43` Production readiness gates (`P2`, `done`)
  - Allow Logic publish/automation/action rollout gates requiring passing evaluation suites, minimum pass percentages, no critical failures, and fresh run timestamps.
  - Show stale or failing Evals warnings before publishing or automation enablement.
  - Store waiver decisions and reviewer comments when a gate is bypassed.
  - Added `LogicPublishGateRequirement`, `LogicPublishGateContext`, `LogicPublishGateSurface` (`logic_publish` / `automation_enable` / `action_rollout`), `LogicPublishGateFinding`, `LogicPublishGateRunSummary`, `LogicPublishGateWaiver`, `LogicPublishGateWaiverInput`, `LogicPublishGateInputSuite`, and `LogicPublishGateResult` types plus `recordLogicPublishGateWaiver` and `evaluateLogicPublishGate` helpers.
  - The gate helper takes a list of suites with their run history, a requirement (required suite ids, min pass rate, max run age in hours, optional critical metric names), and an evaluation timestamp; for each required suite it picks the latest run, checks pass rate and run age, and counts critical metric failures. It emits findings keyed by `no_run::<suiteId>`, `low_pass_rate::<suiteId>`, `stale_run::<suiteId>`, and `critical_metric_failed::<suiteId>::<metricName>`, attaches any matching waivers, and reports `ready`, `rolloutBlocked` (true if any unwaived critical findings), and `bypassedFindingCount`.
  - Waivers are recorded with `recordLogicPublishGateWaiver({ ruleField, reviewerId, reason, ... })` and carry an id, reviewer, reason, and `approvedAtIso` timestamp so the bypass is auditable; passing them into `evaluateLogicPublishGate` flips the matching finding's `waived=true` and `waiver` reference so the gate result preserves the reviewer comments.
  - Wired a `PublishGatesPanel` into the AIP Evals app with a surface selector (Logic publish / Automation enablement / Action rollout), inputs for min pass rate / max run age / critical metric names, a chip-based required-suite picker, a run-summary table, and a per-finding card with severity chip, suite/run/metric context, an inline waiver editor (reason input + Save/Cancel), and a remove-waiver action. A header chip displays `ready` / `waivers applied` / `blocked` based on the live gate result.
  - Tests cover three paths: a blocked rollout (missing suite run + below-threshold pass rate + critical metric failure all flagged), a stale-run warning combined with waivers that close out the unwaived critical findings (`rolloutBlocked` flips to false and `bypassedFindingCount` rises), and a ready rollout when a `last_saved` Logic run satisfies pass rate / freshness / critical metric requirements.
  - Docs: [AIP Evals overview](https://www.palantir.com/docs/foundry/aip-evals/overview/), [AIP Logic integration with Automate](https://www.palantir.com/docs/foundry/logic/aip-logic-integration-automate/).

- [x] `AIPLE.44` Evaluation scheduling and regression monitoring (`P2`, `done`)
  - Schedule evaluation suites against published Logic/function versions.
  - Compare scheduled runs against baselines and alert on metric regression, variance spikes, cost spikes, or new failure categories.
  - Integrate with Data Health and Automate for notification or remediation flows.
  - Added `EvalScheduleCadence` (`hourly` / `daily` / `weekly` / `cron`), `EvalScheduleConfig`, `CreateEvalScheduleInput`, `EvalRegressionThresholds`, `EvalScheduleNotificationChannel` (`data_health` / `automate` / `slack` / `email`), `EvalScheduleRemediationFlow`, `EvalScheduleAlert` / `EvalScheduleAlertKind` (`pass_rate_regression` / `variance_spike` / `cost_spike` / `new_failure_category` / `baseline_missing` / `scheduled_run_missing`), `EvalScheduleNotificationOutcome`, `EvalScheduleRemediationOutcome`, `EvalScheduleRunOutcome`, and `MonitorEvalScheduleInput` types plus `createEvalScheduleConfig`, `nextEvalScheduleRunAt`, and `monitorEvaluationScheduleRun` helpers.
  - `createEvalScheduleConfig` produces an id-stable schedule with default thresholds (5% pass-rate drop, 10% per-metric variance increase, 30% cost spike, alert on new failure categories) and `nextEvalScheduleRunAt` advances the schedule from a configurable anchor for hourly/daily/weekly cadences (with `cron` returning the configured start as a placeholder anchor).
  - `monitorEvaluationScheduleRun` diff's the latest head run against the baseline run: it emits `pass_rate_regression` when the head pass rate drops more than the configured threshold, `variance_spike` per evaluator metric whose failure rate climbs above the threshold, `cost_spike` when the compute-seconds delta exceeds the configured percentage, and `new_failure_category` for every failure-category kind that appears in the head report but not the baseline report. It also surfaces `baseline_missing` and `scheduled_run_missing` alerts so the schedule UI can flag missing context.
  - Notifications and remediation are planned per outcome: each configured channel is `queued` whenever any alert exists and `skipped` otherwise, and the remediation flow is `triggered` when any critical alert fires (otherwise `planned`) so the AIP Evals app can route signals to Data Health, Automate, Slack, or email.
  - Wired an `EvalScheduleMonitoringPanel` into the AIP Evals app with an inline "Add schedule" editor (suite, cadence, cron expression, Data Health check id, Automate automation id, pass-rate drop %, cost spike %), per-schedule cards showing the next run timestamp, pause/resume + remove buttons, headline run stats (pass rate, iterations, baseline pass rate, compute seconds), alert chips with severity, notification status chips, and a remediation chip.
  - Tests cover the regression detection happy path (`pass_rate_regression` + `new_failure_category` alerts, 2 queued notifications, `triggered` remediation, computed `nextEvalScheduleRunAt` anchored at the configured start), the `scheduled_run_missing` case when no head run is provided, and the `baseline_missing` warning path with `planned` remediation when no critical alerts fire.
  - Docs: [AIP Evals overview](https://www.palantir.com/docs/foundry/aip-evals/overview/), [Run an evaluation suite](https://www.palantir.com/docs/foundry/aip-evals/run-suite/).

### Observability, governance, and security

- [x] `AIPLE.45` Logic operational health (`P2`, `done`)
  - Monitor failure rate, P95 duration, token/compute usage, tool failures, action failures, object query failures, model unavailability, run-history dataset failures, and automation proposal backlog.
  - Surface health in Logic detail, Workflow Lineage-like views, Data Health, and project dashboards.
  - Added `LogicOperationalHealthMetric`, `LogicOperationalHealthSurface`, and `LogicOperationalHealthSummary` plus an enhanced `calculateLogicMetrics` helper that derives failure rate, P95 duration, prompt-token and compute totals, tool/action/object-query/model/dataset failure counters, and staged automation backlog health.
  - Wired the Logic Metrics rail to show an operational-health status, health-check grid, near-real-time-style metric surfaces for Logic detail, Workflow Lineage, Data Health, and project dashboards, plus retained failure category and recent-run drilldowns.
  - Tests cover operational-health metric ids/surfaces, failure-rate math, compute/token accumulation, category-specific failure counters, and critical-status escalation.
  - Docs: [AIP Logic metrics](https://www.palantir.com/docs/foundry/logic/logic-metrics/), [AIP Logic compute usage](https://www.palantir.com/docs/foundry/logic/compute-usage).

- [x] `AIPLE.46` AIP security and data minimization guardrails (`P2`, `done`)
  - Show all resources, object types, properties, functions, actions, and media references that a Logic file exposes to LLM blocks.
  - Warn when prompts/tools expose broad object sets or sensitive properties.
  - Add policy hooks for redaction, prompt review, model allowlists, and export/logging restrictions where local governance exists.
  - Extended `LogicFileSecurityPolicy` with local governance hooks and sensitive-property/broad-object thresholds; `buildLogicSecurityBoundary` now returns an LLM exposure inventory, minimization warnings, and hook states for redaction, prompt review, model allowlists, and export/logging restrictions.
  - Wired the Logic Security rail to summarize prompt/object/action/function/media exposure, show sensitive/broad-access warnings, and display enabled/missing local governance hooks alongside the existing permission/resource-boundary checks.
  - Tests cover inventory and hook reporting as well as broad object-set and sensitive-property warnings.
  - Docs: [AIP Logic overview](https://www.palantir.com/docs/foundry/logic), [AIP features](https://www.palantir.com/docs/foundry/aip/aip-features/).

- [x] `AIPLE.47` Audit event stream (`P2`, `done`)
  - Emit immutable audit events for Logic creation/edit/publish/delete, tool/resource exposure, execution mode changes, run invocations, action/automation uses, Evals suite changes, evaluator changes, run results, experiments, and result dataset writes.
  - Filter audit by Logic file, suite, target, user, project, model, object type, action type, run, and time window.
  - Added `AipAuditEvent`, hash-chained immutable append helpers, run-derived audit event emission, and multi-dimensional filtering in `apps/web/src/lib/evals/operations.ts`.
  - Tests cover immutable/frozen audit records, hash chaining, run/dataset-write event emission, and filtering by suite, target, user, project, model, object/action type, run, and time window.
  - Docs: [AIP Logic execution mode settings](https://www.palantir.com/docs/foundry/logic/execution-mode-settings/), [AIP Evals overview](https://www.palantir.com/docs/foundry/aip-evals/overview/).

- [x] `AIPLE.48` Branch-aware Evals and result isolation (`P2`, `done`)
  - Run evaluation suites against branched Logic resources and branch-scoped Ontology/function/action dependencies.
  - Keep branch result datasets and run histories isolated from main unless explicitly published or exported.
  - Compare branch runs to main baselines before merge.
  - Added `planBranchAwareEvaluationRun`, `runBranchAwareEvaluationSuite`, and `compareBranchEvaluationToMain` helpers that bind branch metadata, branch target versions, dependency scope, isolated branch result datasets, and isolated Logic run-history datasets.
  - Tests cover branch-scoped planning and baseline comparison before merge.
  - Docs: [Branching AIP Logic](https://www.palantir.com/docs/foundry/logic/branching-logic), [Run an evaluation suite](https://www.palantir.com/docs/foundry/aip-evals/run-suite/).

- [x] `AIPLE.49` AIP Evals API and SDK surface (`P2`, `done`)
  - Provide OpenFoundry-native APIs for suite CRUD, target functions, test cases, evaluators, run configuration, run execution, result retrieval, result dataset config, experiments, and analyzer jobs.
  - Generate SDK helpers for creating suites, running regression checks, and comparing metrics in CI-like workflows.
  - Added API-client request/response types and endpoints for run execution, run retrieval, result retrieval, result dataset configuration, experiments, experiment runs, and analyzer jobs; added `AIP_EVALS_API_SURFACE`, SDK suite request creation, regression checks, and metric comparison helpers.
  - Tests cover the API surface manifest, SDK-created suite request defaults, regression execution, and baseline comparison.
  - Docs: [AIP Evals overview](https://www.palantir.com/docs/foundry/aip-evals/overview/), [Create an evaluation suite](https://www.palantir.com/docs/foundry/aip-evals/create-suite), [Run an evaluation suite](https://www.palantir.com/docs/foundry/aip-evals/run-suite/).

- [x] `AIPLE.50` Marketplace and reusable evaluator packages (`P2`, `done`)
  - Package evaluator functions, test case templates, Logic files, and example suites as OpenFoundry product outputs where DevOps/Marketplace exists.
  - Support installation/remapping of evaluator dependencies and target function placeholders.
  - Added reusable evaluator package creation and install/remapping helpers that bundle Marketplace evaluator functions, templates, Logic file placeholders, example suites, dependency placeholders, setup steps, and installed evaluator definitions.
  - Tests cover package construction, evaluator installation, setup steps, and target placeholder remapping.
  - Docs: [Create an evaluation suite](https://www.palantir.com/docs/foundry/aip-evals/create-suite), [AIP features](https://www.palantir.com/docs/foundry/aip/aip-features/).

- [x] `AIPLE.51` CI/CD and code-authored function parity (`P2`, `done`)
  - Trigger AIP Evals for code-authored functions from code repository published function pages or CI-like checks.
  - Compare code-authored function versions against Logic and agent-like targets in the same suite.
  - Store results as release evidence before publishing function packages.
  - Added `runCodeFunctionReleaseEvalCheck`, which runs mixed-target suites from a code-function publish surface, compares code-authored functions with Logic/agent-like targets, and emits release evidence with pass-rate gating before package publication.
  - Tests cover mixed-target code-function release evidence and publish-blocking behavior.
  - Docs: [Create an evaluation suite](https://www.palantir.com/docs/foundry/aip-evals/create-suite), [Run an evaluation suite](https://www.palantir.com/docs/foundry/aip-evals/run-suite/).

- [x] `AIPLE.52` Cost-aware experiment and eval planning (`P2`, `done`)
  - Estimate run count, block executions, target invocations, evaluator invocations, LLM/tool usage, and expected cost before running suites or experiments.
  - Enforce per-project or per-user budgets and require confirmation for high-cost experiment grids.
  - Added `planEvaluationCost`, `planExperimentCost`, `runBudgetedEvaluationSuite`, and `runBudgetedExperiment` helpers that calculate target/evaluator/block/tool counts, compute seconds, estimated cost, budget warnings, and confirmation requirements before execution.
  - Tests cover suite and experiment cost estimates, budget overruns, and confirmation blocking.
  - Docs: [AIP Logic compute usage](https://www.palantir.com/docs/foundry/logic/compute-usage), [Run experiments](https://www.palantir.com/docs/foundry/aip-evals/experiments/).

## Milestone D: AIP Console and AIP Now distribution

> **Added 2026-05-17.** This milestone covers the **admin governance
> plane (AIP Console)** and the **solution distribution plane (AIP Now)**
> that wrap AIP Logic / Evals / Agents. Both are first-class products in
> Palantir but were absent from this checklist.

### AIP Console (admin governance)

- [ ] `AIPLE.53` AIP enablement per enrollment (`P1`, `todo`)
  - Master toggle that enables/disables AIP product surfaces (Agents, Threads, Assist, Logic, Evals) for the enrollment; default OFF on first install.
  - Per-organization opt-in within an enabled enrollment.
  - Docs: [AIP enablement](https://palantir.com/docs/foundry/aip/enablement), [AIP Console overview](https://palantir.com/docs/foundry/aip/console).

- [ ] `AIPLE.54` Model availability matrix (`P1`, `todo`)
  - Per-enrollment matrix of LLM providers × models × modalities × regions with admin toggles to enable, disable, or flag as experimental.
  - Experimental models require a per-user opt-in before use; usage flagged as experimental in metrics and audit.
  - Docs: [Model availability](https://palantir.com/docs/foundry/aip/model-availability).

- [ ] `AIPLE.55` Token cost and quota policies (`P1`, `todo`)
  - Per-organization and per-project monthly token/cost budgets with soft-warning + hard-stop thresholds; per-user override available only to admins with audit.
  - Real-time consumption dashboard with per-model breakdown.
  - Docs: [AIP cost](https://palantir.com/docs/foundry/aip/cost).

- [ ] `AIPLE.56` Safety policies (`P2`, `todo`)
  - Per-enrollment configuration of prompt-injection scanning, PII detection, toxicity thresholds, and tool-call rate limits.
  - Per-tool allowlist for production agents (e.g. block `execute_function` for a given agent unless explicitly allowed).
  - Docs: [AIP safety](https://palantir.com/docs/foundry/aip/safety).

- [ ] `AIPLE.57` AIP audit and compliance reports (`P2`, `todo`)
  - Aggregate AIP-specific audit view: who used what model on what data with what tools, queryable by user/project/time-range/marking.
  - Compliance reports (e.g. for SOC2/ISO) generatable from the audit substrate.
  - Docs: [AIP audit](https://palantir.com/docs/foundry/aip/audit).

### AIP Now (solution distribution)

- [ ] `AIPLE.58` AIP Now catalog (`P1`, `todo`)
  - Curated catalog of ready-to-deploy AIP solutions (Agents + Logic + Evals + supporting datasets/ontology) packaged as Marketplace products with an AIP-specific landing experience.
  - Filter by use case, industry, required model providers, and required ontology features.
  - Docs: [AIP Now overview](https://aip.palantir.com/), [Marketplace AIP products](https://palantir.com/docs/foundry/aip/now).

- [ ] `AIPLE.59` Install + enable flow (`P1`, `todo`)
  - One-click install that resolves model availability, sets up required ontology types/objects, deploys the agents/logic, and creates an AIP Console policy entry pre-filled with sensible defaults.
  - Post-install verification (test conversation, eval suite sanity run) before marking the solution active.
  - Docs: [AIP Now overview](https://aip.palantir.com/).

- [ ] `AIPLE.60` Solution evaluation harness (`P2`, `todo`)
  - Every AIP Now solution ships with an eval suite that an admin can re-run after install to confirm parity with the catalog spec; failures block "active" status.
  - Docs: [AIP Now overview](https://aip.palantir.com/).

## Implementation inventory to collect before coding

- [ ] `INV.1` Identify existing OpenFoundry Logic-like, function-builder, prompt-builder, or workflow-builder models that can store inputs, blocks, outputs, versions, and published functions.
- [ ] `INV.2` Identify existing LLM runtime, model registry, AI-service, tool-calling, prompt templating, token usage, and model allowlist primitives.
- [ ] `INV.3` Identify existing Ontology query, object set, action execution, function execution, media reference, calculator, conditional, and loop primitives usable as Logic tools/blocks.
- [ ] `INV.4` Identify existing debugger, run history, telemetry, audit, Workflow Lineage, Data Health, and metrics primitives for Logic executions.
- [ ] `INV.5` Identify existing Functions service package/version/callable-function contracts needed to publish Logic functions and use them in Workshop, Actions, Automate, API, and other Logic functions.
- [ ] `INV.6` Identify existing Automate and action proposal primitives needed for Logic edit staging, approval, proposal visibility, and decision logs.
- [ ] `INV.7` Identify existing Global Branching adapters for Logic files and versioned function resources.
- [ ] `INV.8` Identify existing evaluation/test-case/evaluator models, unit test primitives, run orchestration, result storage, and metrics dashboards.
- [ ] `INV.9` Identify existing built-in evaluator implementations, custom function evaluator support, Marketplace evaluator products, and LLM-as-judge capabilities.
- [ ] `INV.10` Identify existing object-set-backed dataset/test generation, object storage v2 linked object/property traversal, and saved object set APIs.
- [ ] `INV.11` Identify existing result dataset creation/write permissions, project-scoped execution, dataset schema, and Data Foundation integration for Evals results.
- [ ] `INV.12` Identify existing security/governance, marking, project import, redaction, audit, checkpoint, and prompt/tool exposure review primitives.
- [ ] `INV.13` Identify existing code repository function publish pages and CI/CD hooks that can launch evaluation suites for code-authored functions.
- [ ] `INV.14` Produce a machine-readable parity matrix sibling JSON after inventory, following the pattern of [foundry-feature-parity-matrix.json](./foundry-feature-parity-matrix.json).

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
| `logic-service` | Logic file CRUD, inputs/blocks/outputs, draft saves, publish/version history, comparison view, execution mode settings, branch adapter metadata. |
| `logic-runtime-service` | Logic execution graph runtime, LLM blocks, tools, prompts, loops/conditionals, Ontology edit simulation, run panel execution, debugger traces. |
| `ai-service` | Model selection, LLM invocation, token/compute usage, model variables, model allowlists, prompt/tool-call trace storage. |
| `ontology-query-service` | Query objects tool, object set inputs, object list inputs, linked object traversal, permission-aware object data access. |
| `ontology-actions-service` | Apply action tool, Logic-backed actions, action proposal staging, action execution, Ontology edit validation. |
| `functions service` | Execute function tool, published Logic function registration, function version resolution, Workshop/function/action/API invocation contracts. |
| `automation service` | Logic-to-Automate integration, Logic effects, staged human review, proposal activity, automation run links. |
| `aip-evals-service` | Evaluation suites, target functions, test cases, evaluators, run configurations, run orchestration, experiments, analyzer jobs. |
| `eval-results service` | Result persistence, result datasets, metrics dashboards, run comparisons, debug views, trace viewer links. |
| `dataset-versioning-service` | Project-scoped run-history datasets, evaluation results datasets, schema/write validation, lineage to Logic/Evals resources. |
| `global-branch-service` | Branch-scoped Logic resources, proposal participation, rebase/merge checks, branch-specific Evals results isolation. |
| `security/governance service` | Execution-mode permission checks, project imports, resource exposure review, redaction, audit, checkpoint and model policy hooks. |
| `data-health service` | Logic health, eval regression health, run-history dataset health, automation proposal backlog, failed metric alerts. |
| `apps/web` | Logic editor, run panel, debugger, uses sidebar, metrics pages, Evals suite editor, result views, analyzer UI, experiment UI. |

## Acceptance criteria for first complete AIP Logic and Evals milestone

- [ ] A user can create a Logic file in a project folder, define typed inputs, add Use LLM / Query objects / Execute function / Apply action / Calculator / variable blocks, and define final outputs.
- [ ] A user can run draft Logic with sample inputs, inspect block-by-block debugger traces, and see recent run history.
- [ ] Logic preview can propose Ontology edits in simulation without applying them to real objects.
- [ ] A user can save, publish, view version history, compare versions, and use the published Logic function in Workshop or an action-backed flow.
- [ ] Logic execution supports user-scoped permissions and records basic metrics: success/failure counts, P95 duration, failure categories, and recent run links.
- [ ] A user can create an evaluation suite from Logic, add manual test cases, configure built-in evaluators, run the full suite, and run a single test case.
- [ ] Evaluation results show aggregate pass percentage, per-test-case metric results, evaluator outputs, errors, and debugger links.
- [ ] A user can add multiple target functions and compare results across versions/models/functions.
- [ ] A user can expose an intermediate block output and evaluate it with an evaluator.
- [ ] Evaluation suites can evaluate Logic functions that create/edit/delete Ontology objects in simulation without mutating real objects.
- [ ] Project-scoped evaluation runs can write results to a configured dataset with outputs, evaluator results, metadata, and errors.
- [ ] Results analyzer can summarize failed test cases into categories and propose prompt improvements for Logic functions.
- [ ] All OpenFoundry runtime UI is OpenFoundry-native and does not use Palantir branding, screenshots, icons, fonts, or proprietary assets.

## Test plan expectations

- Unit tests for Logic input type validation, block graph validation, prompt variable substitution, LLM tool configuration, object query permission filtering, Apply action simulation, loop/conditional semantics, output type validation, version diffing, execution mode permission decisions, and compute usage attribution.
- API tests for Logic file CRUD, save/publish/version history, run preview, debugger traces, usage snippets, execution mode settings, metrics, evaluation suite CRUD, target functions, test case columns, evaluators, run configs, run execution, experiments, result dataset config, and analyzer jobs.
- Integration tests for Logic querying Ontology objects, Logic applying simulated actions, Logic-backed Workshop display values, Logic-backed action execution, Logic-to-Automate proposal staging, user/project execution modes, result datasets, branch-scoped Logic resources, and Data Health metrics.
- Evals integration tests for manual test cases, object-set-backed test cases, built-in evaluators, custom function evaluators, intermediate parameters, Ontology-edit simulations, multi-target comparisons, iterations/parallelization, single-test runs, experiments, results analyzer, and metrics dashboard traces.
- E2E tests for Logic authoring, run/debug, publish, version compare, Uses sidebar, Automate creation, Evals suite creation, Add-as-test-case from preview, suite run, single-case debug, run comparison, experiment run, results dataset setup, and analyzer prompt suggestions.
- Regression tests proving Logic preview cannot mutate the real Ontology, user-scoped logs are not visible to other users, project-scoped execution requires imported resources, unauthorized object properties are not exposed to LLM blocks, result datasets cannot be written outside permitted projects, and branch-only Logic versions cannot leak into main runtime use.
