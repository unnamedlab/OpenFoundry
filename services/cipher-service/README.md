# cipher-service

Stub backend for `/api/v1/auth/cipher/*`. The edge gateway already
routes that prefix to the `Cipher` upstream
([`router_table.go`](../edge-gateway-service/internal/proxy/router_table.go)),
but no upstream existed yet — every call surfaced as a 502 in the web
UI. This binary fills that gap so the gateway sees a real HTTP server
returning the canonical envelope:

```json
{ "code": "not_implemented", "service": "cipher-service", "milestone": "A" }
```

Each milestone in
[`docs/migration/foundry-cipher-1to1-checklist.md`](../../docs/migration/foundry-cipher-1to1-checklist.md)
will land real handlers — encrypt/decrypt, key lifecycle, audit — and
peel routes off the catch-all stub. Until then, treat 501 as the
documented success case.

## Endpoints

| Route                                | Auth        | Status |
|--------------------------------------|-------------|--------|
| `GET /healthz`                       | public      | 200    |
| `GET /metrics`                       | public      | 200    |
| `GET /_meta/capabilities`            | public      | 200    |
| `GET /api/v1/auth/cipher/algorithms` | bearer JWT  | 200    |
| `POST /api/v1/auth/cipher/keys`       | bearer JWT  | 201    |
| `POST /api/v1/auth/cipher/keys/{id}/rotate` | bearer JWT | 200 |
| `POST /api/v1/auth/cipher/keys/{id}/rotate-new` | bearer JWT | 200 |
| `POST /api/v1/auth/cipher/keys/{id}/wrap-for-promotion` | bearer JWT | 200 |
| `POST /api/v1/auth/cipher/keys/{id}/retire` | bearer JWT | 200 |
| `POST /api/v1/auth/cipher/keys/{id}/revoke` | bearer JWT | 200 |
| `GET /api/v1/auth/cipher/keys`        | bearer JWT  | 200    |
| `GET /api/v1/auth/cipher/keys/{id}`   | bearer JWT  | 200    |
| `PATCH /api/v1/auth/cipher/keys/{id}` | bearer JWT  | 200    |
| `DELETE /api/v1/auth/cipher/keys/{id}`| bearer JWT  | 204    |
| `POST /api/v1/auth/cipher/peppers`    | bearer JWT  | 201    |
| `POST /api/v1/auth/cipher/peppers/{id}/rotate` | bearer JWT | 200 |
| `POST /api/v1/auth/cipher/encrypt`    | bearer JWT  | 200    |
| `POST /api/v1/auth/cipher/encrypt-batch` | bearer JWT | 200 |
| `POST /api/v1/auth/cipher/decrypt`    | bearer JWT  | 200    |
| `POST /api/v1/auth/cipher/decrypt-batch` | bearer JWT | 200 |
| `POST /api/v1/auth/cipher/decrypt-stream` | bearer JWT | 200 |
| `POST /api/v1/auth/cipher/tokenize`   | bearer JWT  | 200    |
| `* /api/v1/auth/cipher{,/...}`        | bearer JWT  | 501    |

## Build

```sh
go build -o bin/cipher-service ./services/cipher-service/cmd/cipher-service
```

## Pepper hashing and lifecycle

`SHA_256` and `SHA_512` algorithms are consumed through tenant-scoped pepper registry entries. Pepper bytes are generated inside cipher-service, stored only as KMS-wrapped version rows, and never returned by the API. `POST /api/v1/auth/cipher/tokenize` returns stable, irreversible HMAC tokens for analytics joins.

Key lifecycle now includes version rotation, successor-key rotation, retire, and revoke. Retired keys reject new encryptions while keeping old envelopes decryptable; revoked keys reject both encrypt and decrypt immediately.

## Milestone C governance

Promotion wrapping (`/keys/{id}/wrap-for-promotion`) returns a target-key provisioning plan that preserves algorithm, backend and policy while keeping ciphertexts in the source environment unless an import ceremony is approved. Keys now carry `kms_backend` metadata for local, Vault Transit, AWS KMS, GCP KMS, Azure Key Vault and PKCS#11-backed deployments. Batch and streaming decrypt paths preserve input order, share policy/marking enforcement, and can be guarded by per-caller decrypt budgets with anomaly detection hooks for new actors, off-hours access and sudden bursts.
