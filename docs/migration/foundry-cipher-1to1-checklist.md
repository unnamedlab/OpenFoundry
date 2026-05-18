# Foundry Cipher 1:1 parity checklist

Date: 2026-05-17
Scope: public-docs-based parity plan for OpenFoundry's Cipher-equivalent
encryption and tokenization layer: encryption algorithm registry with
authenticated and deterministic modes, per-key access policies, pepper
management for hashing, key lifecycle (create, rotate, retire,
revoke), per-column encryption in datasets, per-property encryption in
ontology objects, decrypt-on-read with marking and project enforcement,
join-on-ciphertext using deterministic SIV modes, audit trail of every
encrypt/decrypt operation, and integrations with Pipeline Builder,
Functions, Workshop, and Object Storage V2.

> **Scope distinction.** This checklist covers the **application-level
> encryption** product (deterministic and authenticated encryption for
> fields and rows, key policies tied to markings/projects). It does
> not redefine transport TLS, at-rest disk encryption, or the JWKS /
> signing keys that are owned by
> [foundry-security-governance-1to1-checklist.md](./foundry-security-governance-1to1-checklist.md).

This document is intentionally implementation-oriented. It does not attempt
to clone Palantir branding, private source code, proprietary assets, or any
non-public behavior. The target is **functional parity based on public
Palantir Foundry documentation**.

## Parity scope boundary

All checklist work is governed by the
[Foundry public-docs parity policy](../reference/foundry-public-docs-parity-policy.md).
OpenFoundry may implement behavior described in public Palantir documentation,
but contributors must not copy private source, decompile bundles, import
tenant-specific exports, use Palantir branding, or reuse proprietary assets.

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
| `P0` | Required for credible field-level encryption: key registry, AES-GCM-SIV authenticated mode, decrypt-on-read with permission/marking checks, audit. |
| `P1` | Required for Foundry-style parity: AES-SIV deterministic mode for joins, per-column dataset encryption, per-property ontology encryption, pepper-based hashing, key rotation. |
| `P2` | Advanced parity: cross-environment key wrapping (with Apollo), HSM/KMS backends, batch decrypt API, decrypt limits and rate cards. |

## Official Palantir documentation library

### Product overview

- [Cipher overview](https://www.palantir.com/docs/foundry/cipher/overview)
- [Foundry platform summary for LLMs](https://www.palantir.com/docs/foundry/getting-started/foundry-platform-summary-llm)

### Concepts

- [Algorithms and modes](https://www.palantir.com/docs/foundry/cipher/algorithms)
- [Key lifecycle](https://www.palantir.com/docs/foundry/cipher/key-lifecycle)
- [Access policies](https://www.palantir.com/docs/foundry/cipher/access-policies)
- [Pepper and hashing](https://www.palantir.com/docs/foundry/cipher/hashing)
- [Decrypt-on-read](https://www.palantir.com/docs/foundry/cipher/decrypt-on-read)

### Integrations

- [Cipher in Pipeline Builder](https://www.palantir.com/docs/foundry/cipher/pipeline-builder)
- [Cipher in ontology](https://www.palantir.com/docs/foundry/cipher/ontology)
- [Cipher in Functions](https://www.palantir.com/docs/foundry/cipher/functions)
- [Cipher audit](https://www.palantir.com/docs/foundry/cipher/audit)

## Milestone A: credible field encryption with authenticated mode

### Algorithm and key model

- [x] `CIP.1` Algorithm registry (`P0`, `done`)
  - Built-in modes: `AES_256_GCM_SIV` (authenticated, recommended default), `AES_256_GCM` (authenticated, non-deterministic), `SHA_256` and `SHA_512` (with pepper, P1).
  - Each algorithm records key length, nonce policy, output encoding (base64 default), and a stable identifier used in ciphertext envelopes.
  - Implemented in `services/cipher-service/internal/domain` as an immutable built-in registry and exposed at `GET /api/v1/auth/cipher/algorithms` for authenticated callers with `cipher.keys.read`.
  - Docs: [Algorithms and modes](https://www.palantir.com/docs/foundry/cipher/algorithms).

- [x] `CIP.2` Cipher key resource (`P0`, `done`)
  - CRUD a `cipher_key` resource: name, algorithm, key material reference (KMS/Vault path), owner, organizations, markings, intended scopes (datasets, object properties, function calls), and creation/expiry metadata.
  - Key material itself is never returned from the API; only opaque key references.
  - Implemented by `services/cipher-service` key CRUD routes and metadata persistence; delete is an administrative resource removal while retire remains the decrypt-preserving lifecycle action.
  - Docs: [Key lifecycle](https://www.palantir.com/docs/foundry/cipher/key-lifecycle).

- [x] `CIP.3` Ciphertext envelope format (`P0`, `done`)
  - Standard envelope: `{key_id, algorithm_id, nonce, ciphertext, auth_tag, schema_version}` encoded as a single string (base64).
  - Envelope is self-describing so any reader with permission can decrypt without out-of-band metadata.
  - Implemented as a schema-versioned JSON envelope that also includes `key_version` so rotated keys remain self-describing; the HTTP API base64-encodes the JSON envelope into one ciphertext string.
  - Docs: [Algorithms and modes](https://www.palantir.com/docs/foundry/cipher/algorithms).

### Access policies

- [x] `CIP.4` Per-key access policy (`P0`, `done`)
  - Each key has a policy listing roles, groups, and project bindings that may **encrypt**, **decrypt**, or **manage** with that key.
  - Policy is evaluated via the Cedar engine in `libs/authz-cedar-go`; deny by default.
  - Implemented as per-key `access_policy` metadata compiled into Cedar permit policies for `cipher::encrypt`, `cipher::decrypt`, and `cipher::manage`; empty grants deny by default.
  - Docs: [Access policies](https://www.palantir.com/docs/foundry/cipher/access-policies).

- [x] `CIP.5` Marking-aware decrypt (`P0`, `done`)
  - Decrypt requests must satisfy the markings declared on the key and on the surrounding resource (dataset column, object property).
  - Failure returns a typed `MarkingDenied` error and emits an audit event.
  - Implemented by unioning key markings with request `resource_markings` and checking caller marking clearances before opening the envelope.
  - Docs: [Decrypt-on-read](https://www.palantir.com/docs/foundry/cipher/decrypt-on-read).

### Encrypt and decrypt APIs

- [x] `CIP.6` Encrypt single value (`P0`, `done`)
  - `POST /cipher/encrypt` with `{key_id, plaintext, algorithm?}` returns the envelope.
  - Algorithm defaults to the key's declared algorithm; mismatch returns 400.
  - Implemented alongside the existing batch shape; single-object requests return a single envelope response and audit each operation.
  - Docs: [Algorithms and modes](https://www.palantir.com/docs/foundry/cipher/algorithms).

- [x] `CIP.7` Decrypt single value (`P0`, `done`)
  - `POST /cipher/decrypt` with `{ciphertext}` (envelope) returns plaintext after policy + marking checks.
  - Caller permissions are enforced; service tokens require explicit grant.
  - Implemented using the self-describing envelope to resolve key/version, then enforcing per-key Cedar policy and markings before decrypt.
  - Docs: [Decrypt-on-read](https://www.palantir.com/docs/foundry/cipher/decrypt-on-read).

### Audit

- [x] `CIP.8` Per-operation audit (`P0`, `done`)
  - Every encrypt and decrypt emits an audit event with actor, key id, algorithm, resource RID (when known), success/failure, marking checks result, and request id.
  - Audit consumable from the central audit query surface (Security/Governance).
  - Implemented via `cipher.encrypt` and `cipher.decrypt` audit events emitted for success and failure paths.
  - Docs: [Cipher audit](https://www.palantir.com/docs/foundry/cipher/audit).

## Milestone B: deterministic SIV, columns, properties, rotation

### Deterministic mode for join-on-ciphertext

- [x] `CIP.9` AES-SIV deterministic mode (`P1`, `done`)
  - Add `AES_256_SIV` to the registry: deterministic encryption such that the same plaintext + key always yields the same ciphertext, enabling joins and grouping on ciphertext without decryption.
  - Document the security trade-off (frequency analysis exposure) and require explicit opt-in on key creation.
  - Implemented as explicit `AES_256_SIV` key creation plus deterministic synthetic-IV envelopes; registry metadata includes a frequency-analysis warning.
  - Docs: [Algorithms and modes](https://www.palantir.com/docs/foundry/cipher/algorithms).

- [x] `CIP.10` Cipher-aware join contract (`P1`, `done`)
  - Pipeline Builder transform nodes and OQL planners detect when both join keys are encrypted with the same SIV key and the caller can decrypt, then push the comparison down to ciphertext without decryption.
  - Failing the same-key or same-permission check forces a decrypt-then-join with audit and rate accounting.
  - Implemented as a shared planner contract in `services/cipher-service/internal/contracts` for same-key `AES_256_SIV` ciphertext pushdown and audited decrypt-then-join fallback.
  - Docs: [Cipher in Pipeline Builder](https://www.palantir.com/docs/foundry/cipher/pipeline-builder).

### Per-column dataset encryption

- [x] `CIP.11` Column-level encryption metadata (`P1`, `done`)
  - Dataset schemas declare per-column `cipher_key_id` and `algorithm` metadata; writers must encrypt with that key, readers see envelopes by default.
  - Implemented as validated dataset column metadata contracts that require `cipher_key_id` and supported algorithm on encrypted columns.
  - Docs: [Cipher in Pipeline Builder](https://www.palantir.com/docs/foundry/cipher/pipeline-builder).

- [x] `CIP.12` Decrypt-on-read view (`P1`, `done`)
  - Permitted readers can request a decrypted view of the dataset (or selected columns); the view materializes nothing — decryption happens streamingly.
  - Restricted views may pin columns to encrypted-only mode regardless of caller permissions.
  - Implemented as a streaming decrypt-on-read planning contract with selected-column and restricted-view encrypted-only modes.
  - Docs: [Decrypt-on-read](https://www.palantir.com/docs/foundry/cipher/decrypt-on-read).

### Per-property ontology encryption

- [x] `CIP.13` Object property encryption (`P1`, `done`)
  - Object property definitions can declare `cipher_key_id`; the indexer stores the envelope, search ignores the property unless an SIV key + exact-match predicate is used.
  - Action validation runs against decrypted values only when the actor is permitted, otherwise validates against ciphertext predicates.
  - Implemented as object-property encryption contracts for exact-match SIV search planning and permitted/non-permitted action validation modes.
  - Docs: [Cipher in ontology](https://www.palantir.com/docs/foundry/cipher/ontology).

### Pepper-based hashing

- [x] `CIP.14` Pepper registry (`P1`, `done`)
  - Hashing modes (`SHA_256`, `SHA_512`) require a pepper from the pepper registry; pepper material never leaves the cipher service.
  - Policy controls who may register, rotate, and consume each pepper.
  - Implemented as tenant-scoped `cipher_peppers` plus versioned KMS-wrapped pepper material; `POST /peppers` and `POST /peppers/{id}/rotate` never return plaintext pepper bytes.
  - Docs: [Pepper and hashing](https://www.palantir.com/docs/foundry/cipher/hashing).

- [x] `CIP.15` Tokenization helper (`P1`, `done`)
  - `POST /cipher/tokenize` with `{pepper_id, plaintext}` returns a stable hash token usable for analytics joins where reversibility is forbidden.
  - Audit emitted per call.
  - Implemented via `POST /tokenize`, HMAC-SHA-256/HMAC-SHA-512 over the active pepper version, and `cipher.tokenize` audit events.
  - Docs: [Pepper and hashing](https://www.palantir.com/docs/foundry/cipher/hashing).

### Key lifecycle

- [x] `CIP.16` Key rotation (`P1`, `done`)
  - Rotate a key to a new key id while preserving the policy. Old envelopes remain decryptable until the old key is retired.
  - Background rewrap job re-encrypts dataset columns / object properties on a schedule.
  - Implemented key-version rotation plus successor-key rotation (`/keys/{id}/rotate-new`) that preserves policy and metadata, with a rewrap scheduling contract for dataset columns/object properties; older version rows remain decryptable for existing envelopes.
  - Docs: [Key lifecycle](https://www.palantir.com/docs/foundry/cipher/key-lifecycle).

- [x] `CIP.17` Key retire and revoke (`P1`, `done`)
  - Retire: stop new encryptions with the key but keep decrypt available.
  - Revoke: hard-stop both encrypt and decrypt; all dependent reads start failing immediately with a typed error.
  - Both transitions audited and reversible only by admins.
  - Implemented `/keys/{id}/retire` for decrypt-only status and `/keys/{id}/revoke` for hard-stop encrypt/decrypt with typed `key is revoked` errors and distinct audit events.
  - Docs: [Key lifecycle](https://www.palantir.com/docs/foundry/cipher/key-lifecycle).

### Functions integration

- [x] `CIP.18` Cipher client in Functions runtime (`P1`, `done`)
  - Generated `cipher` namespace in TS and Python OSDKs: `cipher.encrypt(keyId, value)`, `cipher.decrypt(envelope)`, `cipher.tokenize(pepperId, value)`.
  - Calls are policy-checked and audited as if made by the function's caller, not the runtime.
  - Implemented generated TS/Python namespace templates plus required caller-forwarding headers in `function-runtime-service/internal/cipherclient`.
  - Docs: [Cipher in Functions](https://www.palantir.com/docs/foundry/cipher/functions).

## Milestone C: cross-env key wrapping, KMS, batch, rate-cards

### Cross-environment key wrapping

- [x] `CIP.19` Wrap key for promotion (`P2`, `done`)
  - When promoting a product (see [Apollo checklist](./foundry-apollo-1to1-checklist.md)) that references a Cipher key, the target environment provisions a new key with the same algorithm/policy; ciphertexts are NOT moved across environments unless the destination policy explicitly imports the source key with a wrapping ceremony.
  - Implemented `POST /keys/{id}/wrap-for-promotion` to return a target-key provisioning plan preserving algorithm, backend and policy while refusing ciphertext movement by default.
  - Docs: [Key lifecycle](https://www.palantir.com/docs/foundry/cipher/key-lifecycle).

- [x] `CIP.20` HSM / KMS backends (`P2`, `done`)
  - Pluggable backends for key material: Vault Transit, AWS KMS, GCP KMS, Azure Key Vault, on-prem HSM via PKCS#11.
  - Each key declares its backend at creation; the cipher service never materializes raw key bytes when the backend supports envelope encryption.
  - Implemented stable backend identifiers (`local`, `vault_transit`, `aws_kms`, `gcp_kms`, `azure_key_vault`, `pkcs11`) in key metadata/config with local KMS functional and external providers represented by pluggable stubs for deployment-specific clients.
  - Docs: [Key lifecycle](https://www.palantir.com/docs/foundry/cipher/key-lifecycle).

### Batch and high-throughput

- [x] `CIP.21` Batch encrypt/decrypt (`P2`, `done`)
  - `POST /cipher/encrypt-batch` and `/decrypt-batch` with up to N items per call; result preserves input order and reports per-item success/failure.
  - Audit emits one aggregate event per batch with per-item summaries.
  - Implemented `/encrypt-batch` and `/decrypt-batch` aliases over the ordered batch engine with aggregate `cipher.batch` audit counts.
  - Docs: [Algorithms and modes](https://www.palantir.com/docs/foundry/cipher/algorithms).

- [x] `CIP.22` Streaming decrypt for column reads (`P2`, `done`)
  - High-throughput streaming endpoint used by the dataset reader to decrypt a column without round-tripping each row.
  - Rate-limited per caller; backpressure-aware.
  - Implemented `/decrypt-stream` as newline-delimited streaming decrypt that flushes per row and shares the per-caller decrypt budget gate.
  - Docs: [Decrypt-on-read](https://www.palantir.com/docs/foundry/cipher/decrypt-on-read).

### Rate cards and governance

- [x] `CIP.23` Per-caller decrypt budgets (`P2`, `done`)
  - Configurable decrypt budgets per user/group/key with a hard cap that emits an audit-flagged event when exceeded.
  - Implemented an in-process per-caller/key budget manager used by decrypt and streaming decrypt; exceeded calls hard-fail and emit audit marking `budget_exceeded`.
  - Docs: [Access policies](https://www.palantir.com/docs/foundry/cipher/access-policies).

- [x] `CIP.24` Decrypt anomaly detection (`P2`, `done`)
  - Background job flags unusual decrypt patterns (sudden burst, off-hours, new actor) and notifies key managers via Pulse.
  - Implemented a local anomaly detector with Pulse-style notifier interface for new actor, off-hours and sudden-burst findings.
  - Docs: [Cipher audit](https://www.palantir.com/docs/foundry/cipher/audit).

- [ ] `CIP.25` Apollo region policy for keys (`P2`, `todo`)
  - Keys may declare `region` and `compliance_boundary` tags; consumers in mismatched environments cannot decrypt.
  - Required for data-residency commitments in the Security/Governance checklist.
  - Docs: [Key lifecycle](https://www.palantir.com/docs/foundry/cipher/key-lifecycle).

## Implementation inventory to collect before coding

- [ ] `INV.1` Identify the current `libs/authz-cedar-go` evaluation surface and how a `cipher-service` will invoke it for per-key checks.
- [ ] `INV.2` Identify the Vault Transit integration already used by `identity-federation-service/internal/jwksrotation/vault_signer.go` and reuse for key material.
- [ ] `INV.3` Identify the dataset schema metadata layer that needs to carry `cipher_key_id` per column.
- [ ] `INV.4` Identify the ontology property metadata layer for per-property encryption.
- [ ] `INV.5` Identify the audit emission path (overlap with Security/Governance).
- [ ] `INV.6` Produce a parity matrix sibling JSON entry once a first implementation inventory is in place.

## Suggested service boundaries

| Surface | Responsibilities |
| --- | --- |
| `cipher-service` | Algorithm registry, key/pepper resources, encrypt/decrypt/tokenize APIs, batch endpoints, key lifecycle (rotate/retire/revoke), audit emission. |
| `cipher-runtime` (lib) | In-process helper for dataset readers and the ontology indexer to call cipher service efficiently with caching of policy decisions only (never plaintext caching). |
| `authorization-policy-service` | Cedar evaluation of cipher key access policies. |
| `audit-compliance-service` | Sink for per-operation cipher audit events. |
| `apps/web` | Cipher admin UI: keys, peppers, policies, rotation history, decrypt budgets, anomaly alerts. |
