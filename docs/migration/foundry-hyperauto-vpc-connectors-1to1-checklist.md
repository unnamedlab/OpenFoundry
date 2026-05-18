# Foundry HyperAuto, Private Links, and Connector Reference Catalog 1:1 parity checklist

Date: 2026-05-17
Scope: public-docs-based parity plan for OpenFoundry's enterprise-connectivity
surfaces beyond the generic Data Connection product. This checklist drills into
three surfaces that need their own backlog and acceptance criteria: HyperAuto
(SAP-specialized Smart Data Driven Integration with auto-discovery of SAP table
and business object metadata, pre-built ontology adapters, SLT/CDS delta
capture, SAP authorization propagation, multi-system join configuration, ABAP
function module invocation, and the mapping wizard UI); Private Links (VPC
peering, AWS PrivateLink, GCP Private Service Connect, Azure Private Link,
customer-managed endpoints, agent reachability via private routing, private DNS
resolution, IP allowlisting, source-side IAM role assumption, and private-link
health probes); and the Connector type reference catalog (capability matrix,
JDBC driver registry, REST/webhook source patterns, file-store family,
enterprise applications, message buses, time-series historians, and CDC
sources).

> **Scope distinction.** The generic Data Connection product surface — sources,
> connectors, agents, syncs, streams, push ingestion, CDC, virtual tables,
> exports, and webhooks — lives in
> [`foundry-streaming-data-connection-1to1-checklist.md`](./foundry-streaming-data-connection-1to1-checklist.md).
> That file already includes a Milestone E note pointing here. This file is a
> deep-drill into three enterprise surfaces that need their own independently
> trackable backlog: **HyperAuto (SAP)**, **Private Links (VPC connectivity)**,
> and the **Connector type reference catalog**. Items here may cross-link to
> `SDC.*` items rather than duplicate generic source/agent/sync coverage.

This document is intentionally implementation-oriented. It does not attempt to
clone Palantir branding, private source code, proprietary assets, screenshots,
or any non-public behavior. The target is **functional parity based on public
Palantir Foundry documentation**: the same product concepts, comparable
authoring and operator workflows, compatible resource models where useful, and
OpenFoundry-native implementation details that can be tested locally.

## Parity scope boundary

All checklist work is governed by the
[Foundry public-docs parity policy](../reference/foundry-public-docs-parity-policy.md).
OpenFoundry may implement behavior described in public Palantir documentation,
but contributors must not copy private source, decompile bundles, import
tenant-specific exports, use Palantir branding, or reuse proprietary assets.
The product target is functional parity in an OpenFoundry-native implementation,
not a pixel-perfect clone. SAP-side wire formats, ABAP code, RFC modules, BAPIs,
and CDS view definitions belong to SAP and are out of scope for vendoring;
OpenFoundry implements the **integration patterns** described in public docs.

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
| `P0` | Required for credible SAP/VPC/connector-catalog parity sufficient for an enterprise demo. |
| `P1` | Required for Foundry-style HyperAuto/private-link/connector parity beyond a single SAP module or single cloud. |
| `P2` | Advanced, governance-heavy, multi-region, or Marketplace-published parity. |

## Official Palantir documentation library

These public docs should be treated as the external behavioral contract while
implementing this checklist.

### HyperAuto

- [HyperAuto overview](https://www.palantir.com/docs/foundry/data-connection/hyperauto-overview)
- [HyperAuto for SAP](https://www.palantir.com/docs/foundry/data-connection/hyperauto-sap)
- [HyperAuto SAP CDS source](https://www.palantir.com/docs/foundry/data-connection/hyperauto-sap-cds)
- [HyperAuto SAP SLT source](https://www.palantir.com/docs/foundry/data-connection/hyperauto-sap-slt)
- [HyperAuto business objects](https://www.palantir.com/docs/foundry/data-connection/hyperauto-business-objects)

### Private links / VPC connectivity

- [Private links overview](https://www.palantir.com/docs/foundry/data-connection/private-links-overview)
- [AWS PrivateLink](https://www.palantir.com/docs/foundry/data-connection/private-links-aws)
- [GCP Private Service Connect](https://www.palantir.com/docs/foundry/data-connection/private-links-gcp)
- [Azure Private Link](https://www.palantir.com/docs/foundry/data-connection/private-links-azure)

### Connector type reference

- [Connector types overview](https://www.palantir.com/docs/foundry/data-connection/connector-types-overview)
- [Connector reference](https://www.palantir.com/docs/foundry/data-connection/connector-reference)

### SAP family

- [SAP connectors](https://www.palantir.com/docs/foundry/data-connection/sap-connectors)

### JDBC drivers

- [JDBC drivers](https://www.palantir.com/docs/foundry/data-connection/jdbc-drivers)

## Milestone A: minimum viable HyperAuto/Private Links/Connector catalog parity

### HyperAuto bootstrap

- [ ] `HVC.1` HyperAuto application shell (`P0`, `todo`)
  - Provide an OpenFoundry-native HyperAuto entry point under Data Connection with views for Systems, Business Objects, Mappings, Discoveries, and Runs.
  - Show clear entry points for Add SAP system, Browse business objects, Start discovery, and Open mapping wizard.
  - Docs: [HyperAuto overview](https://www.palantir.com/docs/foundry/data-connection/hyperauto-overview).

- [ ] `HVC.2` SAP system registration (`P0`, `todo`)
  - Register an SAP system as a HyperAuto-managed source with system kind (S/4HANA, ECC, BW, BW/4HANA, IBP), client number, language, and connectivity method (JDBC, CDS, SLT, RFC).
  - Persist as an OpenFoundry `data_source` extension `sap_system` linked to a `connector_type` in the registry.
  - Cross-link to `SDC.*` source CRUD; HyperAuto does not duplicate generic source ownership/permissions.
  - Docs: [HyperAuto for SAP](https://www.palantir.com/docs/foundry/data-connection/hyperauto-sap).

- [ ] `HVC.3` SAP table auto-discovery via JDBC (`P0`, `todo`)
  - Enumerate tables, views, and column metadata for a registered SAP system over JDBC, including DD03L/DD02L-style data dictionary introspection.
  - Persist discovered metadata as `sap_table_descriptor` records with system, schema, table, column types, key fields, and discovery timestamp.
  - Docs: [HyperAuto for SAP](https://www.palantir.com/docs/foundry/data-connection/hyperauto-sap), [JDBC drivers](https://www.palantir.com/docs/foundry/data-connection/jdbc-drivers).

- [ ] `HVC.4` Single HyperAuto business object adapter (`P0`, `todo`)
  - Ship one pre-built business object adapter (Customer for SD, or General Ledger Account for FI) that maps SAP tables to an Ontology-ready object type.
  - Adapter is declarative (YAML/JSON) and lives in `libs/hyperauto-adapters/` so contributors can add modules without touching service code.
  - Docs: [HyperAuto business objects](https://www.palantir.com/docs/foundry/data-connection/hyperauto-business-objects).

### Private Links bootstrap

- [ ] `HVC.5` Private link resource model (`P0`, `todo`)
  - Introduce `private_link_endpoint` resource with cloud (AWS, GCP, Azure), endpoint type, target service identifier, region, VPC/VNet ID, subnet IDs, DNS zone, and approval state.
  - Persist binding from `data_source` to `private_link_endpoint` so the agent dispatcher knows when to route via private routing.
  - Docs: [Private links overview](https://www.palantir.com/docs/foundry/data-connection/private-links-overview).

- [ ] `HVC.6` AWS PrivateLink endpoint registration (`P0`, `todo`)
  - Register a customer-managed AWS PrivateLink endpoint with VPC endpoint service name, region, allowed principals, and DNS aliases.
  - Validate endpoint reachability through a synthetic TCP probe issued by the connection agent.
  - Docs: [AWS PrivateLink](https://www.palantir.com/docs/foundry/data-connection/private-links-aws).

- [ ] `HVC.7` Source-side IAM role assumption (AWS) (`P0`, `todo`)
  - Configure a `data_source` to assume an AWS role via STS AssumeRole with external ID, session name, duration, and optional MFA gating.
  - Persist short-lived credentials in the same secret store used by `connection_credential`; never log raw secrets.
  - Docs: [AWS PrivateLink](https://www.palantir.com/docs/foundry/data-connection/private-links-aws), [Connector reference](https://www.palantir.com/docs/foundry/data-connection/connector-reference).

### Connector capability matrix

- [ ] `HVC.8` Connector capability matrix endpoint (`P0`, `todo`)
  - Expose `GET /api/v1/connector-types/{id}/capability-matrix` returning supported auth methods, sync modes (batch, file, table, streaming, CDC), worker compatibility, network requirements, and OS-level constraints.
  - Drive the source setup wizard from this endpoint; never hardcode capability per connector in the frontend.
  - Cross-link to `SDC.2` connector registry.
  - Docs: [Connector types overview](https://www.palantir.com/docs/foundry/data-connection/connector-types-overview).

- [ ] `HVC.9` JDBC driver registry baseline (`P0`, `todo`)
  - Register PostgreSQL, MySQL, and Microsoft SQL Server JDBC drivers in a `jdbc_driver` registry with driver class, default port, JDBC URL template, supported auth methods, and required client libraries.
  - Validate that the connection-management-service exposes registry CRUD with audit emission.
  - Docs: [JDBC drivers](https://www.palantir.com/docs/foundry/data-connection/jdbc-drivers).

- [ ] `HVC.10` REST API source pattern (`P0`, `todo`)
  - Provide a reusable REST API source pattern in the catalog with base URL, auth modes (none, basic, bearer, OAuth2 client credentials), pagination strategies (cursor, page, offset, link-header), retry/backoff, and rate-limit headers.
  - Cross-link to `SDC.*` REST source items; this catalog entry should reuse, not reimplement.
  - Docs: [Connector reference](https://www.palantir.com/docs/foundry/data-connection/connector-reference).

- [ ] `HVC.11` Connector catalog browse UI (`P0`, `todo`)
  - Add an `apps/web` Connector Library page that lists catalog entries with capability badges, supported sync modes, required worker, and a "Set up source" call-to-action.
  - Each entry deep-links to source setup with the connector kind preselected.
  - Docs: [Connector types overview](https://www.palantir.com/docs/foundry/data-connection/connector-types-overview).

## Milestone B: credible Foundry-style enterprise connectivity parity

### HyperAuto delta capture and adapters

- [ ] `HVC.12` SLT-based delta capture (`P1`, `todo`)
  - Support an SLT-style change stream where the SAP-side log queue is replayed into an OpenFoundry stream/CDC sync without polling the source tables.
  - Persist replication configuration, queue lag metrics, and last-applied LSN.
  - Cross-link to `SDC.*` CDC sync items.
  - Docs: [HyperAuto SAP SLT source](https://www.palantir.com/docs/foundry/data-connection/hyperauto-sap-slt).

- [ ] `HVC.13` CDS-based extraction (`P1`, `todo`)
  - Support extraction of SAP CDS views with delta token tracking, deletion handling, and authorization-aware projection.
  - Persist CDS view metadata and incremental token in the HyperAuto run history.
  - Docs: [HyperAuto SAP CDS source](https://www.palantir.com/docs/foundry/data-connection/hyperauto-sap-cds).

- [ ] `HVC.14` Module adapter pack (FI/CO, MM, SD, PP) (`P1`, `todo`)
  - Ship declarative adapter packs covering Finance, Controlling, Materials Management, Sales & Distribution, and Production Planning core business objects.
  - Each pack maps to canonical Ontology object types and link types with documented source tables and join paths.
  - Docs: [HyperAuto business objects](https://www.palantir.com/docs/foundry/data-connection/hyperauto-business-objects).

- [ ] `HVC.15` SAP authorization propagation (`P1`, `todo`)
  - Propagate SAP user authorizations (PFCG roles or analytic privileges) so that downstream Ontology objects respect the same restrictions in restricted views.
  - Document mapping between SAP role names and OpenFoundry role-binding seeds for tenant operators.
  - Docs: [HyperAuto for SAP](https://www.palantir.com/docs/foundry/data-connection/hyperauto-sap).

- [ ] `HVC.16` Multi-system join configuration (`P1`, `todo`)
  - Allow a HyperAuto mapping to join data from two registered SAP systems (e.g., S/4HANA + BW/4HANA) with a typed join key, conflict resolution policy, and per-system filter.
  - Persist as a `hyperauto_join_spec` resource consumed by the pipeline-build-service runner.
  - Docs: [HyperAuto for SAP](https://www.palantir.com/docs/foundry/data-connection/hyperauto-sap).

### Private links across clouds

- [ ] `HVC.17` GCP Private Service Connect endpoint registration (`P1`, `todo`)
  - Register a customer-managed PSC endpoint with service attachment URI, network/subnetwork, target project, and DNS zone.
  - Validate reachability through a synthetic probe issued from the connection agent.
  - Docs: [GCP Private Service Connect](https://www.palantir.com/docs/foundry/data-connection/private-links-gcp).

- [ ] `HVC.18` Azure Private Link endpoint registration (`P1`, `todo`)
  - Register an Azure private endpoint with resource group, VNet, subnet, target resource ID, and private DNS zone group.
  - Validate reachability through a synthetic probe issued from the connection agent.
  - Docs: [Azure Private Link](https://www.palantir.com/docs/foundry/data-connection/private-links-azure).

- [ ] `HVC.19` VPC peering option (`P1`, `todo`)
  - Support classic VPC peering as a less-preferred but supported connectivity mode, with CIDR overlap pre-check and route table audit.
  - Surface a clear UI warning recommending PrivateLink/PSC/Azure Private Link where supported.
  - Docs: [Private links overview](https://www.palantir.com/docs/foundry/data-connection/private-links-overview).

- [ ] `HVC.20` Private DNS resolution inside private networks (`P1`, `todo`)
  - Configure agent DNS resolution to resolve source hostnames against private DNS zones; refuse to route via public DNS for sources marked private-only.
  - Document the resolver configuration shipped with the connection agent container.
  - Docs: [Private links overview](https://www.palantir.com/docs/foundry/data-connection/private-links-overview).

- [ ] `HVC.21` IP allowlisting per source (`P1`, `todo`)
  - Configure a source-side IP allowlist that records expected outbound source IPs (NAT gateway, agent egress IP) and verifies them against the agent's advertised IP at heartbeat time.
  - Emit an audit event when an agent reports an IP outside the allowlist.
  - Docs: [Private links overview](https://www.palantir.com/docs/foundry/data-connection/private-links-overview).

- [ ] `HVC.22` GCP Workload Identity federation (`P1`, `todo`)
  - Allow a `data_source` to federate to a GCP service account via Workload Identity Federation with audience, subject mapping, and token lifetime.
  - Docs: [GCP Private Service Connect](https://www.palantir.com/docs/foundry/data-connection/private-links-gcp).

- [ ] `HVC.23` Azure Managed Identity assumption (`P1`, `todo`)
  - Allow a `data_source` to assume an Azure user-assigned managed identity with tenant, client ID, and target scope.
  - Docs: [Azure Private Link](https://www.palantir.com/docs/foundry/data-connection/private-links-azure).

- [ ] `HVC.24` Private link health probe (`P1`, `todo`)
  - Schedule periodic synthetic TCP/TLS probes per `private_link_endpoint`; surface results in Data Health and emit alerts on consecutive failures.
  - Cross-link to `SDC.*` data health items.
  - Docs: [Private links overview](https://www.palantir.com/docs/foundry/data-connection/private-links-overview).

### Connector catalog depth

- [ ] `HVC.25` Full JDBC driver registry (`P1`, `todo`)
  - Extend the JDBC driver registry to include Oracle, Snowflake, BigQuery, Redshift, Synapse, and Databricks drivers with version pinning and checksum verification.
  - Docs: [JDBC drivers](https://www.palantir.com/docs/foundry/data-connection/jdbc-drivers).

- [ ] `HVC.26` JDBC driver auto-validation (`P1`, `todo`)
  - Validate registered drivers at registry-write time by loading the driver class in a sandboxed worker and round-tripping a no-op query against a stub server.
  - Reject drivers with mismatched checksum or missing required classes.
  - Docs: [JDBC drivers](https://www.palantir.com/docs/foundry/data-connection/jdbc-drivers).

- [ ] `HVC.27` Webhook source pattern in catalog (`P1`, `todo`)
  - Add a webhook source pattern entry to the catalog covering inbound HTTP with HMAC signature verification, replay protection, and routing to a `stream_dataset` or `webhook` resource.
  - Cross-link to `SDC.*` webhook items.
  - Docs: [Connector reference](https://www.palantir.com/docs/foundry/data-connection/connector-reference).

- [ ] `HVC.28` File-store family (`P1`, `todo`)
  - Add catalog entries for AWS S3, Azure ADLS Gen2, Google Cloud Storage, on-prem SFTP, and generic network filesystem (NFS/SMB).
  - Capability matrix advertises file-sync, table-sync (Parquet/Delta), and streaming-sync where supported.
  - Docs: [Connector reference](https://www.palantir.com/docs/foundry/data-connection/connector-reference).

- [ ] `HVC.29` Enterprise application connectors (`P1`, `todo`)
  - Add catalog entries for Salesforce, Workday, and Microsoft 365 with OAuth2 auth, REST pagination strategy, and standard objects (Account, Worker, User/Group).
  - Docs: [Connector reference](https://www.palantir.com/docs/foundry/data-connection/connector-reference).

- [ ] `HVC.30` Message-bus connector family (`P1`, `todo`)
  - Add catalog entries for Kafka, AWS Kinesis, Azure Event Hubs, GCP Pub/Sub, JMS, MQTT, and AMQP with per-bus auth methods, consumer-group semantics, and streaming-sync support.
  - Cross-link to `SDC.*` streaming sync items.
  - Docs: [Connector reference](https://www.palantir.com/docs/foundry/data-connection/connector-reference).

- [ ] `HVC.31` CDC source family (`P1`, `todo`)
  - Add catalog entries for Debezium, AWS DMS, and Azure Data Factory CDC, with source-database matrix, ordering guarantees, and deletion-handling semantics.
  - Cross-link to `SDC.*` CDC sync items.
  - Docs: [Connector reference](https://www.palantir.com/docs/foundry/data-connection/connector-reference).

- [ ] `HVC.32` SAP connector family entry (`P1`, `todo`)
  - Add catalog entries for HANA, BW, S/4HANA RFC, and SAP Concur, all linking to HyperAuto where applicable.
  - Docs: [SAP connectors](https://www.palantir.com/docs/foundry/data-connection/sap-connectors).

## Milestone C: advanced parity

- [ ] `HVC.33` HyperAuto mapping wizard UI (`P2`, `todo`)
  - Interactive UI to pick an SAP system, browse business objects, preview source-to-Ontology mappings, override per-field transforms, and persist as a versioned mapping.
  - Show validation warnings for missing source fields, type mismatches, and authorization gaps.
  - Docs: [HyperAuto business objects](https://www.palantir.com/docs/foundry/data-connection/hyperauto-business-objects).

- [ ] `HVC.34` ABAP function module / RFC invocation (`P2`, `todo`)
  - Allow a HyperAuto run to invoke a whitelisted ABAP function module (RFC/BAPI) with typed parameters; the function module reference list is operator-curated.
  - Persist invocation history with parameter hashes (never raw payloads) and result codes.
  - Docs: [HyperAuto for SAP](https://www.palantir.com/docs/foundry/data-connection/hyperauto-sap).

- [ ] `HVC.35` IBP and EHS module support (`P2`, `todo`)
  - Extend HyperAuto adapters to cover SAP IBP (Integrated Business Planning) and EHS (Environment, Health, and Safety) modules with their canonical business objects.
  - Docs: [HyperAuto business objects](https://www.palantir.com/docs/foundry/data-connection/hyperauto-business-objects).

- [ ] `HVC.36` Cross-region private endpoints (`P2`, `todo`)
  - Support a `private_link_endpoint` exposed in multiple regions with active/passive or active/active routing and a documented failover policy.
  - Docs: [Private links overview](https://www.palantir.com/docs/foundry/data-connection/private-links-overview).

- [ ] `HVC.37` Time-series historian connectors (`P2`, `todo`)
  - Add catalog entries for OSIsoft/AVEVA PI System, Honeywell PHD, AVEVA Historian, and GE Proficy with tag browsing, batched range reads, and streaming-sync semantics.
  - Document worker requirements (Windows-only drivers route via agent).
  - Docs: [Connector reference](https://www.palantir.com/docs/foundry/data-connection/connector-reference).

- [ ] `HVC.38` Marketplace-published custom connectors (`P2`, `todo`)
  - Allow third-party connectors built against the connector SDK to be packaged as Marketplace products with capability declarations; install registers the connector with the catalog.
  - Cross-link to `SDC.*` connector marketplace items.
  - Docs: [Connector types overview](https://www.palantir.com/docs/foundry/data-connection/connector-types-overview).

- [ ] `HVC.39` Private-link policy attestation (`P2`, `todo`)
  - Require an operator attestation per `private_link_endpoint` before traffic is routed, with attestation owner, expiry, and audit trail.
  - Docs: [Private links overview](https://www.palantir.com/docs/foundry/data-connection/private-links-overview).

- [ ] `HVC.40` HyperAuto run history and lineage (`P2`, `todo`)
  - Persist HyperAuto run history (discovery, mapping apply, delta capture) with lineage edges into pipeline-build-service builds and dataset-versioning-service transactions.
  - Surface in the HyperAuto Runs view with filters by system, business object, and status.
  - Docs: [HyperAuto overview](https://www.palantir.com/docs/foundry/data-connection/hyperauto-overview).

- [ ] `HVC.41` SAP authorization propagation parity test suite (`P2`, `todo`)
  - End-to-end test suite that seeds an SAP PFCG-style role set, runs HyperAuto extraction, and asserts that the resulting Ontology restricted views block users whose SAP role does not grant the corresponding org/value.
  - Docs: [HyperAuto for SAP](https://www.palantir.com/docs/foundry/data-connection/hyperauto-sap).

- [ ] `HVC.42` Connector capability matrix CLI (`P2`, `todo`)
  - Ship a `of-cli connectors describe <type>` command that prints the capability matrix and known limitations; useful in CI for catalog drift checks.
  - Docs: [Connector types overview](https://www.palantir.com/docs/foundry/data-connection/connector-types-overview).

- [ ] `HVC.43` Private-link admin UI (`P2`, `todo`)
  - Add an `apps/web` Private Link admin page with endpoint CRUD, probe history, attached sources, and approval workflow integration.
  - Docs: [Private links overview](https://www.palantir.com/docs/foundry/data-connection/private-links-overview).

- [ ] `HVC.44` Connector contract conformance tests (`P2`, `todo`)
  - Generic conformance test harness that any connector implementation must pass: capability matrix accuracy, credential redaction, retry/backoff, cancellation, and observability emission.
  - Docs: [Connector types overview](https://www.palantir.com/docs/foundry/data-connection/connector-types-overview).

- [ ] `HVC.45` HyperAuto delta token retention policy (`P2`, `todo`)
  - Configure retention for SLT/CDS delta tokens with a documented replay window and explicit "drop token" admin action that requires re-bootstrap.
  - Docs: [HyperAuto SAP SLT source](https://www.palantir.com/docs/foundry/data-connection/hyperauto-sap-slt).

- [ ] `HVC.46` Endpoint topology export (`P2`, `todo`)
  - Export the full `private_link_endpoint` topology as a machine-readable document (JSON) usable for compliance attestation and disaster-recovery planning.
  - Docs: [Private links overview](https://www.palantir.com/docs/foundry/data-connection/private-links-overview).

## Implementation inventory to collect before coding

- [ ] `INV.1` Audit the existing `connector-management-service` for connector registry, source CRUD, credential models, and capability declarations; map gaps against `HVC.8`–`HVC.11`.
- [ ] `INV.2` Audit existing JDBC support across services, including any vendored driver shims, default ports, and URL templates, to seed `HVC.9` and `HVC.25`.
- [ ] `INV.3` Identify existing networking/egress resources (`egress_policy`, agent proxy config) and decide whether `private_link_endpoint` lives as a sibling resource or inside `edge-gateway-service` networking config.
- [ ] `INV.4` Identify existing CDC and streaming-sync primitives in `ingestion-replication-service` to determine where SLT/CDS delta capture for HyperAuto plugs in.
- [ ] `INV.5` Identify existing SAP-related code, fixtures, or stubs and decide whether HyperAuto lives as its own service (`hyperauto-service`) or as a plugin module inside `connector-management-service`.
- [ ] `INV.6` Identify existing Ontology object type registration and link type APIs that HyperAuto adapter packs must target without bypassing Ontology governance.
- [ ] `INV.7` Audit existing audit-trail event taxonomy for connector, agent, credential, and IAM-role-assumption events; extend if private-link probes or AssumeRole calls are missing event kinds.
- [ ] `INV.8` Produce a machine-readable parity matrix sibling JSON after inventory, following the pattern of [foundry-feature-parity-matrix.json](./foundry-feature-parity-matrix.json), keyed on `HVC.*` IDs.

## Suggested service boundaries

> **Reader note (2026-05-17)** — The services below are *target* decomposition
> proposals, not a current inventory of binaries. The canonical list of
> binaries on disk today lives in
> [`docs/architecture/services-and-ports.md`](../architecture/services-and-ports.md).
> Where consolidation is cheaper than a new binary, prefer extending an
> existing service.

| Surface | Responsibilities |
| --- | --- |
| `hyperauto-service` (or HyperAuto plugin inside `connector-management-service`) | SAP system registration, table auto-discovery, business object adapter loading, mapping CRUD, SLT/CDS delta capture orchestration, SAP authorization propagation, ABAP RFC invocation gateway, run history. |
| `private-link-controller` (or networking config inside `edge-gateway-service`) | `private_link_endpoint` CRUD across AWS/GCP/Azure, VPC peering registration, IP allowlist enforcement, private DNS resolver configuration, source-side IAM role assumption, synthetic health probes, cross-region failover. |
| `connector-catalog-service` (or extend `connector-management-service`) | Connector type registry, capability matrix endpoint, JDBC driver registry with auto-validation, REST/webhook source patterns, file-store family entries, enterprise application connectors, message-bus connectors, time-series historians, CDC sources, Marketplace publication. |
| `apps/web` | Connector Library page, HyperAuto wizard (Systems, Business Objects, Mappings, Discoveries, Runs), Private Link admin page (endpoint CRUD, probes, attached sources, attestation), source setup wizard reuse from the catalog capability matrix. |

## Acceptance criteria

- [ ] An operator can register an SAP system, run HyperAuto auto-discovery over JDBC, browse discovered tables, and install at least one pre-built business object adapter that materializes an Ontology object type with documented lineage.
- [ ] An SLT-style delta stream or a CDS view with a delta token can drive an OpenFoundry CDC/streaming sync end-to-end, with replay-from-token verified in tests.
- [ ] A multi-system join across two SAP systems can be configured in the mapping wizard and produce a single Ontology object type with conflict-resolution applied.
- [ ] An operator can register an AWS PrivateLink, GCP Private Service Connect, and Azure Private Link endpoint and attach each to a `data_source`; synthetic probes succeed and reach the source through private routing.
- [ ] A source-side IAM assumption (AWS AssumeRole, GCP Workload Identity, Azure Managed Identity) works without exposing long-lived credentials; rotation and audit events fire on each assumption.
- [ ] The connector catalog UI lists at least JDBC (PostgreSQL/MySQL/MSSQL/Oracle/Snowflake/BigQuery/Redshift/Synapse/Databricks), file-store (S3/ADLS/GCS/SFTP), enterprise applications (Salesforce/Workday/M365), message buses (Kafka/Kinesis/Event Hubs/Pub/Sub/JMS/MQTT/AMQP), and CDC sources (Debezium/DMS/ADF CDC), each with a capability matrix.
- [ ] The capability matrix endpoint drives the source setup wizard for every connector kind; no capability is hardcoded in the frontend.
- [ ] SAP authorization propagation is verified by a parity test suite: a user without the corresponding SAP role cannot read the resulting Ontology restricted view.
- [ ] Private-link health probes surface in Data Health and emit alerts on consecutive failures; an admin attestation must be valid before traffic is routed.
- [ ] All OpenFoundry runtime UI is OpenFoundry-native and does not use Palantir branding, screenshots, icons, fonts, or proprietary assets; no SAP-owned wire formats, ABAP code, or CDS view definitions are vendored.

## Test plan expectations

- Unit tests for SAP system registration validation, business object adapter loading, capability matrix serialization, JDBC driver registry checksum verification, private-link endpoint validation per cloud, IP allowlist enforcement, IAM role assumption parameter validation, and HyperAuto mapping versioning.
- API tests for HyperAuto SAP system CRUD, discovery runs, business object adapter listing, mapping CRUD; for `private_link_endpoint` CRUD, attachment to sources, probe scheduling, and attestation; for connector catalog browse, capability matrix retrieval, and JDBC driver registry CRUD.
- Integration tests for end-to-end JDBC discovery against a containerized SAP stub, SLT/CDS delta replay with a recorded token sequence, multi-system join materialization, private-link synthetic probe success/failure paths, and AWS AssumeRole / GCP Workload Identity / Azure Managed Identity credential acquisition against cloud SDK fakes.
- E2E tests for the HyperAuto wizard (system registration, discovery, adapter install, mapping apply, run history view), the Private Link admin UI (endpoint CRUD, probe history, source attachment, attestation workflow), and the Connector Library browse-and-set-up flow.
- Regression tests proving SAP authorization propagation cannot be bypassed via direct Ontology read, that private-only sources never resolve through public DNS, that AssumeRole/Workload-Identity/Managed-Identity credentials are never logged in raw form, and that JDBC drivers with mismatched checksums are refused at registry-write time.
