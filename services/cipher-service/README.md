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
| `* /api/v1/auth/cipher{,/...}`        | bearer JWT  | 501    |

## Build

```sh
go build -o bin/cipher-service ./services/cipher-service/cmd/cipher-service
```
