# Foundry Approvals, Checkpoints, Sensitive Data Scanner, and Data Lifetime 1:1 parity checklist

Date: 2026-05-17
Scope: public-docs-based parity plan for OpenFoundry's governance-adjacent
applications that gate, review, and inspect sensitive operations across the
platform: the standalone **Approvals** application (reviewer queue, approval
templates and policies, SLA tracking, escalation rules, decision audit log,
access-request integration, Marketplace publishing approvals, dataset upload
approvals, action-level approvals, branch-merge approvals, configurable
approver pools and per-resource owners, delegation and re-assignment,
out-of-office handoff, bulk decisions, reviewer transparency); **Checkpoints**
(justification-required gating, optional dual-control / 2-person rule,
checkpoint policy resources, checkpoint event log, integration with exports,
Marketplace publishing, production datasets, Compute Modules, Slate
publishing, bypass override with elevated audit, time-limited approval
windows); the **Sensitive Data Scanner** (scan-policy resources with dataset
and object selectors and pattern bundles, scheduled scans, on-write scans,
scan results / findings for PII / PHI / PCI / secrets / custom categories,
severity classification, remediation workflow including masking and Cipher
encryption and Marking application and quarantine, suppression list,
false-positive feedback, scan-engine plugins, integration with Markings); and
the **Data Lifetime dashboard** (per-dataset and per-object lifetime view
covering created / last build / last read / freshness / decay / scheduled
deletion, dataset-aging classification, "what would I lose if I deleted
this?" report, export as audit evidence, integration with the Retention
policies defined in `foundry-resource-management-1to1-checklist.md`).

> Scope distinction — This file is a focused spin-off from
> [`foundry-security-governance-1to1-checklist.md`](./foundry-security-governance-1to1-checklist.md).
> Identity, SSO, Markings, role sets, and the platform audit log ingestion
> pipeline remain owned by the security-governance checklist. This file owns
> the standalone **Approvals** application, **Checkpoints**, the **Sensitive
> Data Scanner**, and the **Data Lifetime dashboard**. Approvals task
> primitives that already exist as `security_approval_task` records in the
> security-governance checklist are referenced here, but their reviewer
> queue UX, SLA semantics, escalation, and delegation semantics live in this
> file. The security-governance file should cross-link back here from any
> mention of the Approvals app, Checkpoints, the scanner, or the lifetime
> dashboard.

This document is intentionally implementation-oriented. It does not attempt to
clone Palantir branding, private source code, proprietary assets, screenshots,
or any non-public behavior. The target is **functional parity based on public
Palantir Foundry documentation**: the same product concepts, comparable
reviewer / gate / scan / lifetime workflows, compatible resource models where
useful, and OpenFoundry-native implementation details that can be tested
locally.

## Parity scope boundary

All checklist work is governed by the
[Foundry public-docs parity policy](../reference/foundry-public-docs-parity-policy.md).
OpenFoundry may implement behavior described in public Palantir documentation,
but contributors must not copy private source, decompile bundles, import
tenant-specific exports, use Palantir branding, or reuse proprietary assets.
The product target is functional parity in an OpenFoundry-native
implementation, not a pixel-perfect clone.

This checklist covers four cross-cutting governance applications. Approvals
must integrate with access-request flows owned by
[security-governance](./foundry-security-governance-1to1-checklist.md), with
Marketplace publishing flows owned by
[product-delivery](./foundry-product-delivery-1to1-checklist.md), and with
action-type submission owned by
[ontology](./foundry-ontology-1to1-checklist.md). Checkpoints must intercept
sensitive operations across exports, Marketplace, production datasets,
Compute Modules, and Slate publishing without re-implementing those product
surfaces. The Sensitive Data Scanner must compose with Markings owned by
security-governance and with Cipher / encryption controls owned by the
data-foundation checklist. The Data Lifetime dashboard must consume the
retention-policy resources defined in
[`foundry-resource-management-1to1-checklist.md`](./foundry-resource-management-1to1-checklist.md)
and the dataset/transaction lineage owned by data-foundation.

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
| `P0` | Required for credible governance: an Approvals queue with reviewer decisions, a Checkpoint policy that gates one sensitive operation end-to-end, a basic scanner pattern with surfaced findings, and a read-only Data Lifetime dashboard. |
| `P1` | Required for Foundry-style governance: approval templates / SLA / escalation, dual-control checkpoints with event log, scanner remediation workflow with Markings integration, and lifetime decay classification with audit export. |
| `P2` | Advanced parity: bulk decisions, delegation and out-of-office handoff, checkpoint bypass override with elevated audit, custom scanner plugins, cross-dataset lifetime views, usage dashboards, and multi-tenant escalation routing. |

## Official Palantir documentation library

These public docs should be treated as the external behavioral contract while
implementing this checklist.

### Approvals

- [Approvals overview](https://www.palantir.com/docs/foundry/approvals/overview)
- [Configure approvals](https://www.palantir.com/docs/foundry/approvals/configure-approvals)
- [Reviewer workflow](https://www.palantir.com/docs/foundry/approvals/reviewer-workflow)
- [Escalation](https://www.palantir.com/docs/foundry/approvals/escalation)

### Checkpoints

- [Checkpoints overview](https://www.palantir.com/docs/foundry/checkpoints/overview)
- [Configure checkpoints](https://www.palantir.com/docs/foundry/checkpoints/configure)
- [Dual-control checkpoints](https://www.palantir.com/docs/foundry/checkpoints/dual-control)

### Sensitive Data Scanner

- [Sensitive Data Scanner](https://www.palantir.com/docs/foundry/security/sensitive-data-scanner)
- [Sensitive Data Scanner policies](https://www.palantir.com/docs/foundry/security/sensitive-data-scanner-policies)
- [Sensitive Data Scanner findings](https://www.palantir.com/docs/foundry/security/sensitive-data-scanner-findings)

### Data Lifetime

- [Data Lifetime overview](https://www.palantir.com/docs/foundry/data-lifetime/overview)
- [Data Lifetime dashboard](https://www.palantir.com/docs/foundry/data-lifetime/dashboard)

## Milestone A: minimum viable Approvals + Checkpoints + Scanner + Lifetime parity

### Approvals app foundations

- [ ] `ACS.1` Approvals reviewer queue and decision capture (`P0`, `todo`)
  - Provide a dedicated Approvals application surface with an inbox listing every pending task assigned to the caller, the caller's groups, or per-resource owners the caller belongs to.
  - Capture a structured decision (`approved`, `denied`, `needs_info`, `withdrawn`) with reviewer comment, timestamp, and reviewer principal ID against the underlying approval task resource.
  - Distinguish task type (access request, app access, marking grant, egress, retention, policy, Marketplace publish, dataset upload, action submission, branch merge) so the inbox can group and filter without re-implementing each subsystem.
  - Docs: [Approvals overview](https://www.palantir.com/docs/foundry/approvals/overview), [Reviewer workflow](https://www.palantir.com/docs/foundry/approvals/reviewer-workflow).

- [ ] `ACS.2` Approval task lifecycle and audit (`P0`, `todo`)
  - Persist an approval task with stable ID, type, originating resource reference, requester, reason, current state, history of state transitions, and reviewer transparency record (who opened the task, who acted, when).
  - Reject duplicate decisions and reject decisions from principals who are no longer eligible reviewers at decision time.
  - Emit a security audit event for every state transition through the canonical audit pipeline owned by [security-governance](./foundry-security-governance-1to1-checklist.md).
  - Docs: [Approvals overview](https://www.palantir.com/docs/foundry/approvals/overview).

- [ ] `ACS.3` Configurable approver pools (`P0`, `todo`)
  - Express approver pools as the union of explicit groups, per-resource owner sets resolved at decision time (project owners, dataset owners, marking administrators), and platform-admin escape hatches.
  - Allow either "any reviewer in pool decides" or "every group in pool must decide once" semantics per approval template.
  - Resolve resource-owner pools dynamically against the source-of-truth service rather than snapshotting at task creation.
  - Docs: [Configure approvals](https://www.palantir.com/docs/foundry/approvals/configure-approvals).

- [ ] `ACS.4` Access-request integration handoff (`P0`, `todo`)
  - Wire the existing `security_access_request` / project-access-request workflow so that creating a request produces approval tasks visible in the Approvals inbox.
  - Decisions made in the Approvals inbox materialize into the underlying access grants (project membership, group membership, marking access) through the owning service, not the Approvals service directly.
  - Cancelling an access request from the originating UI cancels every linked approval task.
  - Docs: [Approvals overview](https://www.palantir.com/docs/foundry/approvals/overview).

### Checkpoints foundations

- [ ] `ACS.5` Checkpoint policy resource (`P0`, `todo`)
  - Define a `checkpoint_policy` resource scoped to a tenant with: trigger operation kind (export, Marketplace publish, production dataset write, Compute Module run, Slate publish), selector matching resource type and optional resource ID prefixes / Markings / tags, justification-required flag, optional dual-control flag, time-limited approval window in minutes, and policy state (`draft`, `active`, `disabled`).
  - Persist policy authorship, last edited by, and policy version for diffability.
  - Reject mutually-incompatible flags such as dual-control without justification-required.
  - Docs: [Checkpoints overview](https://www.palantir.com/docs/foundry/checkpoints/overview), [Configure checkpoints](https://www.palantir.com/docs/foundry/checkpoints/configure).

- [ ] `ACS.6` Checkpoint gate on a single sensitive operation (`P0`, `todo`)
  - Wire one initial sensitive operation (recommended: dataset export) through an OpenFoundry-native `checkpoint-gate` middleware: services call the checkpoints service with the operation descriptor, the service returns either a `pass` token or a `pending_checkpoint` reference, and the caller must hold the `pass` token to actually perform the operation.
  - Issue justification capture inline; persist justification text on the checkpoint event.
  - Audit every gate decision (passed, denied, pending, expired) through the canonical audit pipeline.
  - Docs: [Checkpoints overview](https://www.palantir.com/docs/foundry/checkpoints/overview).

- [ ] `ACS.7` Checkpoint event log (`P0`, `todo`)
  - Emit a checkpoint event for every triggered checkpoint with policy ID, operation kind, target resource reference, actor, justification, decision, time-to-decision, and approver(s).
  - Expose a per-resource and per-policy view of recent checkpoint events to administrators and resource owners.
  - Mark dual-control events distinctly so downstream audit / SIEM consumers can filter on them.
  - Docs: [Checkpoints overview](https://www.palantir.com/docs/foundry/checkpoints/overview).

### Sensitive Data Scanner foundations

- [ ] `ACS.8` Scan-policy resource and pattern bundles (`P0`, `todo`)
  - Define a `scan_policy` resource with: dataset selectors (project IDs, dataset RIDs, prefix globs), object selectors (object type RIDs and property filters where applicable), pattern bundle reference, schedule expression (cron-like), on-write toggle, severity floor, and policy state (`draft`, `active`, `disabled`).
  - Ship at least one OpenFoundry-native pattern bundle covering email addresses, US/EU phone numbers, US SSNs, credit-card numbers (Luhn-checked), and generic API-key shapes. Pattern bundle format must be plain-text-reviewable and version-pinned.
  - Reject overlapping cron schedules per dataset to avoid duplicate findings.
  - Docs: [Sensitive Data Scanner](https://www.palantir.com/docs/foundry/security/sensitive-data-scanner), [Sensitive Data Scanner policies](https://www.palantir.com/docs/foundry/security/sensitive-data-scanner-policies).

- [ ] `ACS.9` Scheduled and on-write scan execution (`P0`, `todo`)
  - Run scheduled scans driven by the policy cron expression and surface "next scheduled run at" on every active policy.
  - Trigger on-write scans by subscribing to dataset transaction commit events emitted by data-foundation; bound per-scan latency budgets so on-write does not block dataset writes when scanning is slow.
  - Persist a `scan_run` record with start/end timestamps, policy reference, target reference, run kind (`scheduled` | `on_write` | `manual`), and outcome (`succeeded`, `partial`, `failed`).
  - Docs: [Sensitive Data Scanner](https://www.palantir.com/docs/foundry/security/sensitive-data-scanner).

- [ ] `ACS.10` Scan findings with severity classification (`P0`, `todo`)
  - Emit a `scan_finding` per matched pattern occurrence with: scan run ID, policy ID, target resource reference, finding category (`pii`, `phi`, `pci`, `secret`, `custom`), pattern identifier, severity (`low`, `medium`, `high`, `critical`) derived from the pattern bundle, sample location reference (column/row coordinates or object/property path) without raw value disclosure, and detected-at timestamp.
  - Default-deny disclosure of raw matched values; show redacted previews unless the viewer holds an explicit `scanner:finding.read_raw` operation grant.
  - Surface findings in a per-dataset and per-policy view with severity filters.
  - Docs: [Sensitive Data Scanner findings](https://www.palantir.com/docs/foundry/security/sensitive-data-scanner-findings).

### Data Lifetime dashboard foundations

- [ ] `ACS.11` Per-dataset lifetime read view (`P0`, `todo`)
  - Provide a read-only Data Lifetime view per dataset showing: created-at, last build at, last read at, freshness (max-staleness budget if a retention/SLA policy applies), decay status (`fresh`, `aging`, `stale`, `expired`), and any scheduled deletion drawn from active retention policies.
  - Resolve the underlying timestamps by querying data-foundation lineage and retention-service rather than duplicating dataset metadata.
  - Refuse to display lifetime details to principals lacking discoverer-or-higher access to the dataset, matching the standard project security boundary.
  - Docs: [Data Lifetime overview](https://www.palantir.com/docs/foundry/data-lifetime/overview), [Data Lifetime dashboard](https://www.palantir.com/docs/foundry/data-lifetime/dashboard).

- [ ] `ACS.12` Per-object lifetime read view (`P0`, `todo`)
  - Extend the per-dataset view with a per-object lifetime sample when the dataset backs an object type: created-at, last action-applied-at, last read-at if recorded, and any object-level scheduled deletion derived from retention policy.
  - Bound per-view object lookups to avoid full-scan cost on large object types; require an explicit "sample N objects" parameter.
  - Docs: [Data Lifetime dashboard](https://www.palantir.com/docs/foundry/data-lifetime/dashboard).

- [ ] `ACS.13` Approvals admin landing page (`P0`, `todo`)
  - Ship an OpenFoundry-native `/control-panel/approvals` admin landing page that lists active approval templates, configured approver pools, and outstanding tasks broken down by type.
  - Link out to the underlying access-request, Marketplace, dataset-upload, action, and branch-merge surfaces so admins can audit configuration without leaving the page.
  - Docs: [Approvals overview](https://www.palantir.com/docs/foundry/approvals/overview).

## Milestone B: credible Foundry-style parity

### Approvals templates, SLA, and escalation

- [ ] `ACS.14` Approval templates (`P1`, `todo`)
  - Express reusable approval templates: name, description, default approver pool, decision semantics (`any` | `all`), required reason fields, optional attachment requirements, SLA in hours, escalation chain reference, and visibility (`tenant` | `organization` | `space`).
  - Allow templates to be cloned and versioned; preserve prior versions for ongoing tasks.
  - Reject deletion of templates that have open or recently-decided tasks within the audit retention window.
  - Docs: [Configure approvals](https://www.palantir.com/docs/foundry/approvals/configure-approvals).

- [ ] `ACS.15` SLA tracking on approval tasks (`P1`, `todo`)
  - Stamp every approval task with `sla_due_at = created_at + template.sla_hours` and surface SLA status (`on_track`, `at_risk`, `breached`) in the reviewer inbox.
  - Recompute SLA windows when a task is reassigned via delegate / out-of-office, optionally extending the deadline per template policy.
  - Emit SLA-breach audit events as a distinct audit category so security monitoring can alert on chronic breach patterns.
  - Docs: [Reviewer workflow](https://www.palantir.com/docs/foundry/approvals/reviewer-workflow), [Escalation](https://www.palantir.com/docs/foundry/approvals/escalation).

- [ ] `ACS.16` Escalation rules (`P1`, `todo`)
  - Express escalation chains as ordered tiers (initial reviewer pool, tier-1 escalation pool, tier-2 escalation pool, final administrator pool) with per-tier wait times.
  - When SLA breaches, automatically promote the task to the next tier and notify the next-tier reviewers via the notification-alerting service.
  - Preserve the original reviewer pool's visibility so the escalation event is auditable.
  - Docs: [Escalation](https://www.palantir.com/docs/foundry/approvals/escalation).

- [ ] `ACS.17` Marketplace publishing approval integration (`P1`, `todo`)
  - Wire Marketplace publishing (federation-product-exchange-service) so that publishing a product version creates an approval task using a Marketplace-publish template.
  - The publishing service must hold the resulting approval token before the product version becomes consumable.
  - Surface the linked task on the product version detail page with reviewer transparency.
  - Docs: [Approvals overview](https://www.palantir.com/docs/foundry/approvals/overview), [Configure approvals](https://www.palantir.com/docs/foundry/approvals/configure-approvals).

- [ ] `ACS.18` Dataset upload approval integration (`P1`, `todo`)
  - Wire dataset upload (data-foundation) so that uploads into projects matching an upload-approval template require a decision before the new transaction becomes visible to non-uploader principals.
  - Allow the template to skip approval when the uploader holds editor-or-higher access to the destination project, matching the documented "trusted contributor" behavior.
  - Docs: [Configure approvals](https://www.palantir.com/docs/foundry/approvals/configure-approvals).

- [ ] `ACS.19` Action-level approval integration (`P1`, `todo`)
  - Allow action types (ontology) to be tagged with an approval template; submitting such an action enqueues an approval task and defers the action's effect until the task is approved.
  - Persist the action payload in encrypted form against the approval task so reviewers can inspect intent without bypassing the action's own permission model.
  - Docs: [Reviewer workflow](https://www.palantir.com/docs/foundry/approvals/reviewer-workflow).

- [ ] `ACS.20` Branch-merge approval integration (`P1`, `todo`)
  - Allow branch-merge operations (global-branching) to be gated by an approval template; the merge call must hold the approval token before the merge commits.
  - Persist a diff summary of the branch against the merge target on the approval task for reviewer transparency.
  - Docs: [Reviewer workflow](https://www.palantir.com/docs/foundry/approvals/reviewer-workflow).

### Checkpoints dual-control and breadth

- [ ] `ACS.21` Dual-control (2-person rule) (`P1`, `todo`)
  - Extend checkpoint policy with a `dual_control` flag requiring two distinct principals to approve before the `pass` token is issued.
  - Enforce that the operation initiator and the dual-control approvers are all distinct principals, and reject self-approval.
  - Persist both approver decisions and timestamps on the checkpoint event so audit reflects the 2-person rule.
  - Docs: [Dual-control checkpoints](https://www.palantir.com/docs/foundry/checkpoints/dual-control).

- [ ] `ACS.22` Time-limited approval windows (`P1`, `todo`)
  - Enforce the policy's approval window: once issued, a `pass` token expires after `approval_window_minutes` and the gated operation must be performed within that window or re-request.
  - Reject `pass` tokens at the gate when expired, even if the underlying policy was satisfied at decision time.
  - Surface remaining window time in the requester UI.
  - Docs: [Configure checkpoints](https://www.palantir.com/docs/foundry/checkpoints/configure).

- [ ] `ACS.23` Checkpoint integration with Marketplace publish (`P1`, `todo`)
  - Layer checkpoint gating on top of the Marketplace-publish approval (`ACS.17`) so that publishing additionally captures justification and (where policy demands) dual-control.
  - Allow a single policy to compose approval and checkpoint requirements without double-prompting the requester.
  - Docs: [Configure checkpoints](https://www.palantir.com/docs/foundry/checkpoints/configure).

- [ ] `ACS.24` Checkpoint integration with production datasets (`P1`, `todo`)
  - Wire writes against datasets tagged `production` (data-foundation tag) through a production-write checkpoint policy with required justification.
  - Allow per-organization opt-out for non-regulated tenants; default-on for tenants that have any retention policy with the `production` selector active.
  - Docs: [Checkpoints overview](https://www.palantir.com/docs/foundry/checkpoints/overview).

- [ ] `ACS.25` Checkpoint integration with Compute Modules (`P1`, `todo`)
  - Wire Compute Module runs that touch tagged production or marked datasets through a Compute-Module-run checkpoint with justification capture; persist the resolved input and output references on the checkpoint event.
  - Docs: [Checkpoints overview](https://www.palantir.com/docs/foundry/checkpoints/overview).

- [ ] `ACS.26` Checkpoint integration with Slate publishing (`P1`, `todo`)
  - Wire Slate application publishing through a Slate-publish checkpoint that captures justification and (per policy) dual-control before the published application becomes user-visible.
  - Persist the Slate version reference and rolled-out audience on the checkpoint event.
  - Docs: [Checkpoints overview](https://www.palantir.com/docs/foundry/checkpoints/overview).

### Sensitive Data Scanner remediation

- [ ] `ACS.27` Remediation workflow (`P1`, `todo`)
  - Allow per-finding remediation: `mask` (overwrite with redaction in derived restricted view), `encrypt` (route through the Cipher integration owned by data-foundation), `mark` (apply a Marking from security-governance), or `quarantine` (move the dataset/transaction into a quarantine project with restricted access).
  - Persist the remediation decision, decided-by principal, and remediation status on the finding.
  - Allow bulk remediation across findings sharing a pattern within a single scan run.
  - Docs: [Sensitive Data Scanner findings](https://www.palantir.com/docs/foundry/security/sensitive-data-scanner-findings).

- [ ] `ACS.28` Markings integration for scanner (`P1`, `todo`)
  - When a `mark` remediation is selected, call into the markings service to apply the configured marking; on failure (insufficient apply-marking permission), keep the finding in `remediation_pending` rather than silently succeeding.
  - Allow scan policies to declare a default marking per finding category (`pii_default_marking`, `phi_default_marking`, `pci_default_marking`, `secret_default_marking`).
  - Docs: [Sensitive Data Scanner](https://www.palantir.com/docs/foundry/security/sensitive-data-scanner).

- [ ] `ACS.29` Suppression list and false-positive feedback (`P1`, `todo`)
  - Support a per-policy or per-dataset suppression list keyed on `(pattern_id, location_path)` so that a confirmed false positive does not re-fire on each subsequent scan.
  - Capture a `false_positive_reason` and a reviewing principal on each suppression entry; expose suppression decisions in the scan policy detail view.
  - Re-evaluate suppression entries when the policy's pattern bundle version changes and force re-confirmation if the pattern semantics changed.
  - Docs: [Sensitive Data Scanner findings](https://www.palantir.com/docs/foundry/security/sensitive-data-scanner-findings).

### Data Lifetime classification and export

- [ ] `ACS.30` Dataset-aging classification engine (`P1`, `todo`)
  - Classify each dataset into a coarse decay bucket (`fresh` < 7d since last build, `aging` 7d-30d, `stale` 30d-180d, `dormant` > 180d, `scheduled_for_deletion` when an active retention policy covers it).
  - Allow the bucket thresholds to be overridden per organization through Control Panel.
  - Surface a tenant-wide aging summary on the Data Lifetime dashboard.
  - Docs: [Data Lifetime overview](https://www.palantir.com/docs/foundry/data-lifetime/overview).

- [ ] `ACS.31` Export Data Lifetime view as audit evidence (`P1`, `todo`)
  - Allow administrators to export a Data Lifetime view (per dataset, per project, or tenant-wide) as a stable JSON / CSV artifact with a content hash and a timestamp suitable for external audit.
  - Persist export requests as audit events with the requesting principal and scope.
  - Reject exports that would include datasets the requesting principal cannot discover; document the scope intersection clearly.
  - Docs: [Data Lifetime dashboard](https://www.palantir.com/docs/foundry/data-lifetime/dashboard).

- [ ] `ACS.32` Retention policy integration on lifetime view (`P1`, `todo`)
  - For every dataset shown in the lifetime view, resolve any active retention policy from the retention-service and display: policy name, scope, next scheduled run, and the user-friendly "scheduled deletion at" if applicable.
  - Surface a banner when the retention policy has the documented "irreversible" flag set, matching the warning surfaces required by [foundry-resource-management-1to1-checklist.md](./foundry-resource-management-1to1-checklist.md).
  - Docs: [Data Lifetime overview](https://www.palantir.com/docs/foundry/data-lifetime/overview).

## Milestone C: advanced parity

### Advanced Approvals

- [ ] `ACS.33` Bulk decision UX (`P2`, `todo`)
  - Allow a reviewer to select multiple homogeneous tasks (same template, same target resource type) in the inbox and decide them in a single action with one shared reason.
  - Enforce that bulk decisions still produce per-task audit events and per-task notifications; do not collapse them into a single audit row.
  - Reject bulk decisions across templates with conflicting decision semantics (`any` vs `all`).
  - Docs: [Reviewer workflow](https://www.palantir.com/docs/foundry/approvals/reviewer-workflow).

- [ ] `ACS.34` Delegate / re-assign and out-of-office handoff (`P2`, `todo`)
  - Allow a reviewer to delegate any specific task to another eligible reviewer, with an audit trail capturing the delegating principal, the delegated-to principal, and the reason.
  - Allow a reviewer to configure an out-of-office window (`from`, `to`, `delegate_to_principal_id`) so the system auto-reassigns tasks arriving in that window.
  - Reject delegation to an ineligible reviewer; preserve the original reviewer-pool visibility for transparency.
  - Docs: [Reviewer workflow](https://www.palantir.com/docs/foundry/approvals/reviewer-workflow).

- [ ] `ACS.35` Reviewer transparency log (`P2`, `todo`)
  - Persist per-task transparency records: principals who opened the task, when, and the source surface (Approvals inbox, deep-link, API), without exposing internal IPs to non-admins.
  - Surface "seen by" rows on the reviewer task detail to make passive review visible to the requester.
  - Docs: [Approvals overview](https://www.palantir.com/docs/foundry/approvals/overview).

- [ ] `ACS.36` Multi-tenant escalation routing (`P2`, `todo`)
  - Allow escalation chains to target per-organization administrator pools so escalations in a tenant route to that tenant's administrators rather than a single platform-wide pool.
  - Default to platform admins only when the tenant has no configured escalation administrators.
  - Docs: [Escalation](https://www.palantir.com/docs/foundry/approvals/escalation).

### Advanced Checkpoints

- [ ] `ACS.37` Bypass override with elevated audit (`P2`, `todo`)
  - Allow a holder of the `checkpoint:bypass` operation to override a denied or pending checkpoint, emitting a distinct elevated-audit event (`checkpoint.bypass_override`) with mandatory `bypass_reason`.
  - Auto-notify tenant administrators and security-monitoring on every bypass; surface bypasses on the checkpoint event log with strong visual distinction.
  - Rate-limit bypasses per principal per 24 hours to prevent silent normalization.
  - Docs: [Checkpoints overview](https://www.palantir.com/docs/foundry/checkpoints/overview), [Dual-control checkpoints](https://www.palantir.com/docs/foundry/checkpoints/dual-control).

- [ ] `ACS.38` Per-resource checkpoint policy overrides (`P2`, `todo`)
  - Allow a resource owner to attach an additional checkpoint policy to a specific resource (more strict only — cannot relax tenant-wide policy).
  - Surface the effective policy stack on the resource detail page so users understand which policies will fire.
  - Docs: [Configure checkpoints](https://www.palantir.com/docs/foundry/checkpoints/configure).

### Advanced Sensitive Data Scanner

- [ ] `ACS.39` Custom scanner plugins (`P2`, `todo`)
  - Allow tenants to register a custom scanner plugin (OpenFoundry-native: a registered Compute Module or a gRPC endpoint) that the scanner service calls with a row/column or object/property batch and that returns findings in the canonical finding shape.
  - Enforce per-plugin authentication via service-user credentials and per-call timeout / payload-size caps.
  - Disallow plugins from accessing raw data outside the dataset they were invoked on; pass references rather than payloads where possible.
  - Docs: [Sensitive Data Scanner policies](https://www.palantir.com/docs/foundry/security/sensitive-data-scanner-policies).

- [ ] `ACS.40` Pattern bundle versioning and rollout (`P2`, `todo`)
  - Version pattern bundles with semantic versions; pin scan policies to a specific version and allow controlled rollout to newer versions with a diff preview that estimates "findings added / dropped" against a sample dataset.
  - Reject rollout when the projected delta exceeds a configurable safety threshold without an admin override.
  - Docs: [Sensitive Data Scanner policies](https://www.palantir.com/docs/foundry/security/sensitive-data-scanner-policies).

### Advanced Data Lifetime

- [ ] `ACS.41` Cross-dataset lifetime view (`P2`, `todo`)
  - Provide a tenant-wide cross-dataset lifetime view that supports filtering by project, marking, age bucket, and "covered by retention policy yes/no", with stable sort and CSV/JSON export.
  - Reject queries that would scan more than a configured row budget without an admin override.
  - Docs: [Data Lifetime dashboard](https://www.palantir.com/docs/foundry/data-lifetime/dashboard).

- [ ] `ACS.42` "What would I lose?" deletion-impact report (`P2`, `todo`)
  - Allow a project owner to request a deletion-impact report for any candidate dataset / project: enumerate downstream lineage consumers (datasets, restricted views, object types, actions, Slate apps, dashboards), latest read times, and active subscriptions.
  - Treat the report as audit evidence; persist it with a stable content hash and a 30-day retention floor.
  - Docs: [Data Lifetime dashboard](https://www.palantir.com/docs/foundry/data-lifetime/dashboard).

- [ ] `ACS.43` Lifetime usage dashboards (`P2`, `todo`)
  - Surface per-tenant lifetime usage dashboards: dataset count by age bucket over time, scheduled-deletion volume by month, and a "stale-but-still-read" anomaly list highlighting datasets that are old yet still actively read.
  - Wire dashboards into the existing observability stack rather than re-implementing visualization.
  - Docs: [Data Lifetime overview](https://www.palantir.com/docs/foundry/data-lifetime/overview).

## Implementation inventory to collect before coding

- [ ] `INV.1` Inventory the existing access-request, approval-task, and approver-pool primitives across `tenancy-organizations-service`, `identity-federation-service`, and `authorization-policy-service` so the Approvals app composes rather than duplicates.
- [ ] `INV.2` Inventory every sensitive operation across the platform that currently lacks a justification capture or 2-person rule but is documented as Checkpoints-eligible: export, Marketplace publish, production-dataset write, Compute Module run, Slate publish, branch merge.
- [ ] `INV.3` Inventory the platform audit pipeline contract (event categories, ingestion endpoints, retention) so Approvals, Checkpoints, and Scanner emit through the canonical audit pipeline rather than per-service log buckets.
- [ ] `INV.4` Inventory the Markings apply / remove permission model from security-governance to confirm the scanner can apply Markings as a remediation without bypassing apply-marking authorization checks.
- [ ] `INV.5` Inventory the Cipher / dataset-encryption integration points owned by data-foundation that the scanner's `encrypt` remediation will route through.
- [ ] `INV.6` Inventory the retention-policy resource shape and its execution pipeline so the Data Lifetime dashboard can resolve "scheduled deletion at" cheaply.
- [ ] `INV.7` Inventory the lineage / last-build-at / last-read-at telemetry currently emitted by data-foundation and ontology to confirm the Data Lifetime view can be powered without new dataplane instrumentation.
- [ ] `INV.8` Inventory existing notification delivery channels (email, in-app, webhook) so SLA-breach and escalation events route through the notification-alerting service rather than per-service ad-hoc emailers.
- [ ] `INV.9` Identify the minimal pattern-bundle format (regex + metadata + post-match validator for Luhn / mod-checks) and confirm it can be reviewed in plain text in pull requests.
- [ ] `INV.10` Produce a machine-readable parity matrix sibling JSON after inventory, following the pattern of [foundry-feature-parity-matrix.json](./foundry-feature-parity-matrix.json).

## Suggested service boundaries

> **Reader note (2026-05-18)** — The services in the table below are
> target decomposition proposals, not a current inventory of binaries.
> `approvals-service` does not exist as a current `services/` directory;
> the live approval substrate is `workflow-automation-service/internal/approvals`
> inside `workflow-automation-service`. For the canonical service list, see
> [`docs/reference/repository-layout.md`](../reference/repository-layout.md).

| Surface | Responsibilities |
| --- | --- |
| `approvals-service` | Approval templates, approval tasks, reviewer pools, decisions, SLA tracking, escalation chains, delegation and out-of-office handoff, bulk decisions, reviewer transparency. Composes upstream access-request / Marketplace / dataset-upload / action / branch-merge surfaces. |
| `checkpoints-service` | Checkpoint policies, the `checkpoint-gate` middleware contract, justification capture, dual-control coordination, time-limited approval windows, checkpoint event log, bypass override with elevated audit. |
| `sensitive-data-scanner-service` | Scan policies, pattern bundle registry, scheduled and on-write scan execution, scan runs and findings, suppression lists, remediation workflow (mask / encrypt / mark / quarantine), custom scanner plugin gateway. |
| `data-lifetime-service` | Per-dataset and per-object lifetime read views, dataset-aging classification, cross-dataset lifetime queries, "what would I lose?" deletion-impact reports, lifetime export as audit evidence; resolves underlying timestamps from data-foundation and retention-service. |
| `notification-alerting-service` (reuse) | SLA-breach, escalation, checkpoint-bypass, scan-finding, and remediation-status notifications via in-app / email / webhook channels. Not re-implemented here. |
| `apps/web` | Approvals inbox UI, Approvals admin landing page, Checkpoints admin and event log UI, Sensitive Data Scanner policies / findings / remediation UI, Data Lifetime dashboard UI. |

## Acceptance criteria

- [ ] A platform administrator can configure an approval template with an approver pool, an SLA, and an escalation chain; a user can file a request, a reviewer can decide it from the Approvals inbox, and an audit event is emitted for every state transition.
- [ ] A platform administrator can configure a checkpoint policy on a dataset export operation with justification-required and dual-control flags; an attempted export captures justification, requires two distinct approvers, and is gated by a time-limited `pass` token.
- [ ] A platform administrator can configure a scan policy with at least one PII pattern bundle on a scheduled and on-write basis; findings surface with severity classification and redacted previews and can be remediated by masking, encrypting via Cipher, applying a Marking, or quarantining.
- [ ] A project owner can open the Data Lifetime dashboard for a dataset and see created / last-build / last-read / decay bucket / scheduled-deletion, can export the view as audit evidence, and can request a "what would I lose?" deletion-impact report.
- [ ] Approval-task SLA breaches escalate to the next tier automatically and emit a distinct audit category for security monitoring.
- [ ] A `checkpoint:bypass` override emits an elevated-audit event with mandatory reason, notifies tenant administrators, and is rate-limited.
- [ ] All OpenFoundry runtime UI is OpenFoundry-native and does not use Palantir branding, screenshots, icons, fonts, or proprietary assets.

## Test plan expectations

- Unit tests for approver-pool resolution (group union, dynamic resource-owner pools, platform-admin escape hatch), decision-semantics enforcement (`any` vs `all`), SLA-due-at computation, escalation tier promotion on breach, delegation eligibility, out-of-office auto-reassignment, and bulk-decision homogeneity checks.
- Unit tests for checkpoint policy validation (mutually-incompatible flags rejected), justification capture, dual-control distinct-principal enforcement, `pass` token issuance and expiry, bypass-override audit emission, and rate-limiting.
- Unit tests for pattern bundle parsing (regex compile + post-match validators for Luhn / mod-checks), scan-policy schedule validation, on-write subscription wiring, severity derivation, finding redaction, suppression-list matching, and remediation status transitions.
- Unit tests for dataset-aging bucket classification, retention-policy resolution on the lifetime view, deletion-impact report enumeration over lineage, and export content-hash stability.
- API tests for templates, tasks, decisions, delegations, SLA states, escalation transitions; checkpoint policies, gate calls, dual-control approvals, bypass overrides; scan policies, scan runs, findings, suppressions, remediations; lifetime views, exports, deletion-impact reports.
- Integration tests with testcontainers for: access-request approval round-trip producing a project membership grant; Marketplace publish blocked until approval + checkpoint pass; on-write scanner picking up a new transaction commit and emitting a finding within the documented latency budget; lifetime view reflecting an actual retention-policy run.
- E2E tests for: configure a template + escalation chain, file a request, escalate on breach; configure a dual-control export checkpoint, perform the export, dual approve; configure a PII scan policy, view a finding, apply a `mark` remediation; open the Data Lifetime dashboard, export an audit-evidence artifact, request a deletion-impact report.
- Observability tests for SLA-breach event volume, escalation latency, checkpoint pass / deny / bypass counters, scan-run duration and finding counts per category, suppression hit rate, lifetime-view query latency, and deletion-impact report generation time.
- Regression tests proving: a reviewer cannot decide a task they are no longer eligible for; a bypass-override always emits the elevated audit event and is rate-limited; a scanner remediation that fails apply-marking authorization leaves the finding in `remediation_pending` rather than silently succeeding; a deletion-impact report cannot include datasets the requester cannot discover; and the Approvals UI is OpenFoundry-native with no Palantir branding.
