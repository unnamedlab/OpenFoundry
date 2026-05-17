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

- [ ] `CIP.1` Algorithm registry (`P0`, `todo`)
  - Built-in modes: `AES_256_GCM_SIV` (authenticated, recommended default), `AES_256_GCM` (authenticated, non-deterministic), `SHA_256` and `SHA_512` (with pepper, P1).
  - Each algorithm records key length, nonce policy, output encoding (base64 default), and a stable identifier used in ciphertext envelopes.
  - Docs: [Algorithms and modes](https://www.palantir.com/docs/foundry/cipher/algorithms).

- [ ] `CIP.2` Cipher key resource (`P0`, `todo`)
  - CRUD a `cipher_key` resource: name, algorithm, key material reference (KMS/Vault path), owner, organizations, markings, intended scopes (datasets, object properties, function calls), and creation/expiry metadata.
  - Key material itself is never returned from the API; only opaque key references.
  - Docs: [Key lifecycle](https://www.palantir.com/docs/foundry/cipher/key-lifecycle).

- [ ] `CIP.3` Ciphertext envelope format (`P0`, `todo`)
  - Standard envelope: `{key_id, algorithm_id, nonce, ciphertext, auth_tag, schema_version}` encoded as a single string (base64).
  - Envelope is self-describing so any reader with permission can decrypt without out-of-band metadata.
  - Docs: [Algorithms and modes](https://www.palantir.com/docs/foundry/cipher/algorithms).

### Access policies

- [ ] `CIP.4` Per-key access policy (`P0`, `todo`)
  - Each key has a policy listing roles, groups, and project bindings that may **encrypt**, **decrypt**, or **manage** with that key.
  - Policy is evaluated via the Cedar engine in `libs/authz-cedar-go`; deny by default.
  - Docs: [Access policies](https://www.palantir.com/docs/foundry/cipher/access-policies).

- [ ] `CIP.5` Marking-aware decrypt (`P0`, `todo`)
  - Decrypt requests must satisfy the markings declared on the key and on the surrounding resource (dataset column, object property).
  - Failure returns a typed `MarkingDenied` error and emits an audit event.
  - Docs: [Decrypt-on-read](https://www.palantir.com/docs/foundry/cipher/decrypt-on-read).

### Encrypt and decrypt APIs

- [ ] `CIP.6` Encrypt single value (`P0`, `todo`)
  - `POST /cipher/encrypt` with `{key_id, plaintext, algorithm?}` returns the envelope.
  - Algorithm defaults to the key's declared algorithm; mismatch returns 400.
  - Docs: [Algorithms and modes](https://www.palantir.com/docs/foundry/cipher/algorithms).

- [ ] `CIP.7` Decrypt single value (`P0`, `todo`)
  - `POST /cipher/decrypt` with `{ciphertext}` (envelope) returns plaintext after policy + marking checks.
  - Caller permissions are enforced; service tokens require explicit grant.
  - Docs: [Decrypt-on-read](https://www.palantir.com/docs/foundry/cipher/decrypt-on-read).

### Audit

- [ ] `CIP.8` Per-operation audit (`P0`, `todo`)
  - Every encrypt and decrypt emits an audit event with actor, key id, algorithm, resource RID (when known), success/failure, marking checks result, and request id.
  - Audit consumable from the central audit query surface (Security/Governance).
  - Docs: [Cipher audit](https://www.palantir.com/docs/foundry/cipher/audit).

## Milestone B: deterministic SIV, columns, properties, rotation

### Deterministic mode for join-on-ciphertext

- [ ] `CIP.9` AES-SIV deterministic mode (`P1`, `todo`)
  - Add `AES_256_SIV` to the registry: deterministic encryption such that the same plaintext + key always yields the same ciphertext, enabling joins and grouping on ciphertext without decryption.
  - Document the security trade-off (frequency analysis exposure) and require explicit opt-in on key creation.
  - Docs: [Algorithms and modes](https://www.palantir.com/docs/foundry/cipher/algorithms).

- [ ] `CIP.10` Cipher-aware join contract (`P1`, `todo`)
  - Pipeline Builder transform nodes and OQL planners detect when both join keys are encrypted with the same SIV key and the caller can decrypt, then push the comparison down to ciphertext without decryption.
  - Failing the same-key or same-permission check forces a decrypt-then-join with audit and rate accounting.
  - Docs: [Cipher in Pipeline Builder](https://www.palantir.com/docs/foundry/cipher/pipeline-builder).

### Per-column dataset encryption

- [ ] `CIP.11` Column-level encryption metadata (`P1`, `todo`)
  - Dataset schemas declare per-column `cipher_key_id` and `algorithm` metadata; writers must encrypt with that key, readers see envelopes by default.
  - Docs: [Cipher in Pipeline Builder](https://www.palantir.com/docs/foundry/cipher/pipeline-builder).

- [ ] `CIP.12` Decrypt-on-read view (`P1`, `todo`)
  - Permitted readers can request a decrypted view of the dataset (or selected columns); the view materializes nothing — decryption happens streamingly.
  - Restricted views may pin columns to encrypted-only mode regardless of caller permissions.
  - Docs: [Decrypt-on-read](https://www.palantir.com/docs/foundry/cipher/decrypt-on-read).

### Per-property ontology encryption

- [ ] `CIP.13` Object property encryption (`P1`, `todo`)
  - Object property definitions can declare `cipher_key_id`; the indexer stores the envelope, search ignores the property unless an SIV key + exact-match predicate is used.
  - Action validation runs against decrypted values only when the actor is permitted, otherwise validates against ciphertext predicates.
  - Docs: [Cipher in ontology](https://www.palantir.com/docs/foundry/cipher/ontology).

### Pepper-based hashing

- [ ] `CIP.14` Pepper registry (`P1`, `todo`)
  - Hashing modes (`SHA_256`, `SHA_512`) require a pepper from the pepper registry; pepper material never leaves the cipher service.
  - Policy controls who may register, rotate, and consume each pepper.
  - Docs: [Pepper and hashing](https://www.palantir.com/docs/foundry/cipher/hashing).

- [ ] `CIP.15` Tokenization helper (`P1`, `todo`)
  - `POST /cipher/tokenize` with `{pepper_id, plaintext}` returns a stable hash token usable for analytics joins where reversibility is forbidden.
  - Audit emitted per call.
  - Docs: [Pepper and hashing](https://www.palantir.com/docs/foundry/cipher/hashing).

### Key lifecycle

- [ ] `CIP.16` Key rotation (`P1`, `todo`)
  - Rotate a key to a new key id while preserving the policy. Old envelopes remain decryptable until the old key is retired.
  - Background rewrap job re-encrypts dataset columns / object properties on a schedule.
  - Docs: [Key lifecycle](https://www.palantir.com/docs/foundry/cipher/key-lifecycle).

- [ ] `CIP.17` Key retire and revoke (`P1`, `todo`)
  - Retire: stop new encryptions with the key but keep decrypt available.
  - Revoke: hard-stop both encrypt and decrypt; all dependent reads start failing immediately with a typed error.
  - Both transitions audited and reversible only by admins.
  - Docs: [Key lifecycle](https://www.palantir.com/docs/foundry/cipher/key-lifecycle).

### Functions integration

- [ ] `CIP.18` Cipher client in Functions runtime (`P1`, `todo`)
  - Generated `cipher` namespace in TS and Python OSDKs: `cipher.encrypt(keyId, value)`, `cipher.decrypt(envelope)`, `cipher.tokenize(pepperId, value)`.
  - Calls are policy-checked and audited as if made by the function's caller, not the runtime.
  - Docs: [Cipher in Functions](https://www.palantir.com/docs/foundry/cipher/functions).

## Milestone C: cross-env key wrapping, KMS, batch, rate-cards

### Cross-environment key wrapping

- [ ] `CIP.19` Wrap key for promotion (`P2`, `todo`)
  - When promoting a product (see [Apollo checklist](./foundry-apollo-1to1-checklist.md)) that references a Cipher key, the target environment provisions a new key with the same algorithm/policy; ciphertexts are NOT moved across environments unless the destination policy explicitly imports the source key with a wrapping ceremony.
  - Docs: [Key lifecycle](https://www.palantir.com/docs/foundry/cipher/key-lifecycle).

- [ ] `CIP.20` HSM / KMS backends (`P2`, `todo`)
  - Pluggable backends for key material: Vault Transit, AWS KMS, GCP KMS, Azure Key Vault, on-prem HSM via PKCS#11.
  - Each key declares its backend at creation; the cipher service never materializes raw key bytes when the backend supports envelope encryption.
  - Docs: [Key lifecycle](https://www.palantir.com/docs/foundry/cipher/key-lifecycle).

### Batch and high-throughput

- [ ] `CIP.21` Batch encrypt/decrypt (`P2`, `todo`)
  - `POST /cipher/encrypt-batch` and `/decrypt-batch` with up to N items per call; result preserves input order and reports per-item success/failure.
  - Audit emits one aggregate event per batch with per-item summaries.
  - Docs: [Algorithms and modes](https://www.palantir.com/docs/foundry/cipher/algorithms).

- [ ] `CIP.22` Streaming decrypt for column reads (`P2`, `todo`)
  - High-throughput streaming endpoint used by the dataset reader to decrypt a column without round-tripping each row.
  - Rate-limited per caller; backpressure-aware.
  - Docs: [Decrypt-on-read](https://www.palantir.com/docs/foundry/cipher/decrypt-on-read).

### Rate cards and governance

- [ ] `CIP.23` Per-caller decrypt budgets (`P2`, `todo`)
  - Configurable decrypt budgets per user/group/key with a hard cap that emits an audit-flagged event when exceeded.
  - Docs: [Access policies](https://www.palantir.com/docs/foundry/cipher/access-policies).

- [ ] `CIP.24` Decrypt anomaly detection (`P2`, `todo`)
  - Background job flags unusual decrypt patterns (sudden burst, off-hours, new actor) and notifies key managers via Pulse.
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
