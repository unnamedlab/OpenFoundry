# Technical documentation gap analysis — 2026-05-18

## Decision rule

Documentation must strictly follow the implementation. When this analysis finds a disagreement, the remediation is to update, archive, or annotate the documentation from current code evidence. Do **not** reshape working code only to satisfy an older document.

## Scope and evidence gathered

This pass compared the VitePress/top-level technical documentation against the current repository tree and a small set of code-owned runtime manifests. The commands used were:

```sh
find services -mindepth 1 -maxdepth 1 -type d | wc -l
find libs -mindepth 1 -maxdepth 1 -type d | wc -l
find proto -mindepth 1 -maxdepth 1 -type d | wc -l
rg -n "42|33|service binaries|shared packages|microservices|shared libraries" README.md ARCHITECTURE.md docs -g '*.md' -g '!node_modules'
python3 - <<'PY'
from pathlib import Path
services = sorted(p.name for p in Path('services').iterdir() if p.is_dir())
libs = sorted(p.name for p in Path('libs').iterdir() if p.is_dir())
md = '\n'.join(p.read_text(errors='ignore') for p in Path('docs').rglob('*.md') if 'node_modules' not in p.parts)
print('services not mentioned in docs md:')
for service in services:
    if service not in md:
        print(service)
print('libs not mentioned in docs md:')
for lib in libs:
    if lib not in md:
        print(lib)
PY
```

Current filesystem inventory:

| Surface | Current code count | Notes |
| --- | ---: | --- |
| `services/*` directories | 50 | Every directory has repository-owned source or runtime assets. Several are data-plane workers or partially routed services rather than public gateway surfaces. |
| `libs/*` directories | 36 | Includes newer shared packages such as `pipeline-plan`, `pipeline-runtime`, and `restrictedview`. |
| `proto/*` domain directories | 23 | Contract domains include `dataset`, `geospatial`, `ml`, `report`, `streaming`, and `workflow` in addition to the domains covered by most narrative docs. |

## Original high-confidence gaps

| Priority | Gap | Code evidence | Current doc evidence | Impact | Recommended documentation change |
| --- | --- | --- | --- | --- | --- |
| P0 | Top-level service/library counts are stale. | Filesystem currently has 50 service directories and 36 library directories. | `README.md`, `ARCHITECTURE.md`, and `docs/reference/repository-layout.md` still say 42 services and 33 libraries. | New contributors start from an incorrect inventory and may miss real binaries during audits, builds, ownership reviews, or deployment work. | Replace hard-coded counts with generated counts or a script-backed snapshot. If counts stay in prose, add a maintenance note naming `services/*` and `libs/*` as the source of truth. |
| P0 | Services-and-ports documentation is not a complete service inventory and contains wrong default ports for several active services. | Examples from service config: `authorization-policy-service` defaults to `50093`, `dataset-versioning-service` to `50078`, `media-sets-service` to `50156`, `application-composition-service` to `50140`, and `sdk-generation-service` to `50144`. | `docs/architecture/services-and-ports.md` lists `authorization-policy-service` as `50115`, `dataset-versioning-service` as `50117`, `media-sets-service` as `50121 / 50122`, `application-composition-service` as `50118`, and omits many active service directories from the main inventory. | Local debugging, port-forward instructions, gateway aliases, and Helm values can be cross-wired when readers trust the doc over service config. | Split the page into two tables: **public gateway-routed surfaces** and **all binaries/workers**. Generate the default-port column from `internal/config` where possible and explicitly mark services whose config uses `config.yaml` instead of `PORT` defaults. |
| P0 | Documentation says some binaries were absorbed or do not exist even though active service directories exist. | Active directories exist for `cipher-service`, `network-boundary-service`, `global-branch-service`, `report-service`, `function-runtime-service`, `knowledge-index-service`, `action-log-sink`, and `iceberg-object-indexer`. | `docs/architecture/services-and-ports.md` describes `cipher_service_url`, `network_boundary_service_url`, `global_branch_service_url`, `report_service_url`, and `knowledge_index_service_url` as absorbed aliases; `docs/reference/repository-layout.md` explicitly lists `report-service` among binaries that do not exist. | Readers cannot tell whether these are runnable code, legacy aliases, or migration leftovers. This is especially risky for gateway routing and Helm ownership. | For each such service, document current status in one place: `active+routed`, `active+not routed`, `worker only`, `scaffold/stub`, or `legacy alias only`. Remove “does not exist” statements for directories that exist. |
| P0 | The Functions Runtime docs are contradicted by implementation. | `services/function-runtime-service/README.md` describes v0 registry, versioning, execution, `/api/v1/functions/*` routes, persistence tables, tests, and explicit missing gateway/Helm wiring. | `docs/migration/foundry-feature-parity-matrix.md` still says Functions Runtime is `todo`, 0%, with `(none)` and claims no functions runtime service exists. | Product parity status is materially wrong: the code has a service, but routing/deployment gaps remain. Planning based on the matrix will overstate missing implementation and understate integration tasks. | Change the matrix row from “no service exists” to “service exists; not gateway-routed or Helm-wired yet,” and update the checklist to distinguish runtime implementation from gateway/deployment integration. |
| P0 | The Global Branching parity row is stale. | `services/global-branch-service/README.md` says Milestone A hosts lifecycle CRUD, participant coordination, merge conflict checks, audit events, and integration tests. | `docs/migration/foundry-feature-parity-matrix.md` calls it a scaffold stub with only one route. | Roadmap and parity reporting understate existing capability and hide the real remaining gap: frontend/gateway still use the legacy code-repository-review route shape. | Re-audit Global Branching docs against `services/global-branch-service`, then express status as “Milestone A implemented; gateway/frontend cutover pending.” |
| P1 | Helm app values include service keys that do not correspond to `services/*` directories. | `infra/helm/apps/of-apps-ops/values.yaml` includes `tabular-analysis-service`; `infra/helm/apps/of-data-engine/values.yaml` includes `sql-warehousing-service` and `analytical-logic-service`. | Architecture docs describe `sql-warehousing-service` and `tabular-analysis-service` as retired/absorbed into `sql-bi-gateway-service`. | The docs and deployment manifests tell different stories. Even if these values are intentionally legacy placeholders, the documentation does not say how operators should interpret them. | Add a “legacy Helm values / non-binary service keys” section and identify which values are compatibility placeholders versus deployable images. |
| P1 | `docs/reference/repository-layout.md` omits active services and newer libraries. | Missing from the runtime table: `action-log-sink`, `cipher-service`, `function-runtime-service`, `global-branch-service`, `iceberg-object-indexer`, `knowledge-index-service`, `network-boundary-service`, and `report-service`. Missing from the library prose: `pipeline-plan`, `pipeline-runtime`, and `restrictedview`. | The page presents itself as the quick answer to “where should this change live?” | Contributors may put changes in the wrong service or duplicate shared packages because the map is incomplete. | Convert the page to a generated or checked inventory, or add a “last verified by command” block and update all service/library rows. |
| P1 | Gateway ownership docs are too summarized for the actual router. | `services/edge-gateway-service/internal/proxy/router_table.go` contains specific route cases for cipher, network boundaries, checkpoints, retention, dataset quality, app composition, model serving, knowledge index, reports, global branches, and more. | `docs/architecture/services-and-ports.md` lists only selected “important examples.” | Summaries are fine, but readers may treat the abbreviated table as complete route ownership. | Rename the section to “selected route examples” and add a generated route ownership appendix or link to a test/snapshot that enumerates every prefix. |
| P2 | Capability docs mix target parity with current implementation without consistent status labels. | The repository contains many partially implemented service READMEs with precise status notes, while migration docs often use broad `todo`/`partial` labels. | Several capability pages and migration checklists describe Foundry-equivalent target behavior in a way that can read as implemented behavior. | Users may overestimate or underestimate shipped behavior depending on which doc they read. | Adopt a required header for capability pages: `Implemented in code`, `Routed through gateway`, `Deployed by Helm`, `UI available`, and `Target-only / parity backlog`. |

## Service inventory deltas to reconcile

These current `services/*` directories were either absent from `docs/architecture/services-and-ports.md`'s primary service map, described as absorbed elsewhere, or contradicted by `docs/reference/repository-layout.md`:

- `action-log-sink`
- `cipher-service`
- `function-runtime-service`
- `global-branch-service`
- `iceberg-object-indexer`
- `knowledge-index-service`
- `network-boundary-service`
- `report-service`

This list does **not** mean every item must be publicly routed. It means each item needs a code-derived status in the docs.

## P0 remediation status

The P0 items above were remediated in the follow-up documentation pass on
2026-05-18:

- Top-level counts now say 50 service directories and 36 shared libraries in
  `README.md`, `ARCHITECTURE.md`, and `docs/reference/repository-layout.md`,
  with `services/*` and `libs/*` documented as the source of truth.
- `docs/architecture/services-and-ports.md` is split into gateway-routed
  public surfaces and a complete all-binaries/workers inventory, with
  code-derived local default ports and explicit statuses for stubs, workers,
  not-routed services, and integration-pending services.
- Active service directories that were previously described as absorbed or
  non-existent (`cipher-service`, `network-boundary-service`,
  `global-branch-service`, `report-service`, `function-runtime-service`,
  `knowledge-index-service`, `action-log-sink`, and `iceberg-object-indexer`)
  now have code-derived status rows.
- `docs/migration/foundry-feature-parity-matrix.md`, its JSON companion, and
  the Functions Runtime / Global Branching checklists now describe the current
  implementations and separate remaining integration gaps from missing code.

## Suggested remediation plan

1. **Establish a documentation source-of-truth policy.** Add a short doc rule that `services/*`, `libs/*`, service `README.md`, `internal/config`, gateway router tests, and Helm values outrank narrative docs.
2. **Fix the inventory pages first.** Update `README.md`, `ARCHITECTURE.md`, `docs/reference/repository-layout.md`, and `docs/architecture/services-and-ports.md` so they agree on counts and service status.
3. **Re-audit parity rows with live service READMEs.** Start with Functions Runtime and Global Branching because they have direct, high-confidence contradictions.
4. **Separate implementation status from integration status.** A service can be implemented but not gateway-routed, not Helm-wired, or not exposed in the UI. The docs should use those separate labels instead of a single broad status.
5. **Add a lightweight drift check.** A small script can fail CI when hard-coded docs counts diverge from `find services` / `find libs`, or when `docs/reference/repository-layout.md` omits a service directory.

## Proposed next audit commands

```sh
# Exact service and library inventories.
find services -mindepth 1 -maxdepth 1 -type d | sed 's#services/##' | sort
find libs -mindepth 1 -maxdepth 1 -type d | sed 's#libs/##' | sort

# Default ports declared by Go service config.
for s in services/*; do
  if [ -f "$s/internal/config/config.go" ]; then
    rg -n "parseUint16\(os.Getenv\(\"PORT\"\)|DefaultPort|Server.Port" "$s/internal/config/config.go" | sed "s#^#$(basename "$s"): #"
  fi
done

# Docs that still hard-code old inventory counts.
rg -n "42 Go microservices|42 Go microservice|42 service binaries|33 shared libraries|33 shared packages|libs/ contains 33" README.md ARCHITECTURE.md docs -g '*.md' -g '!node_modules'
```
