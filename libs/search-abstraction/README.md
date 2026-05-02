# `search-abstraction`

Search backend abstraction for OpenFoundry — re-exports the canonical
`SearchBackend` trait from `storage-abstraction` and ships two HTTP
clients behind cargo features:

| Feature      | Backend     | Use         |
|--------------|-------------|-------------|
| `vespa`      | Vespa       | Production  |
| `opensearch` | OpenSearch  | Dev / CI    |

See [ADR-0028](../../docs/architecture/adr/ADR-0028-search-backend-abstraction.md)
for the rationale.

## Status

S0.4 ships the trait wiring + typed clients. The HTTP request /
response wiring against real Vespa and OpenSearch endpoints is the
subject of stream **S0.8 — Search abstraction**; until then every
client method returns a `RepoError::Backend("… pending S0.8")` so
callers can compose against the trait today.

## Example

```rust,ignore
use search_abstraction::{OpenSearchBackend, SearchBackend, SearchQuery, TenantId};

# async fn demo() -> Result<(), Box<dyn std::error::Error>> {
let backend = OpenSearchBackend::new("http://opensearch:9200");
let _hits = backend
    .search(
        SearchQuery {
            tenant: TenantId("acme".into()),
            type_id: None,
            q: Some("kraken".into()),
            filters: Default::default(),
            page: Default::default(),
        },
        Default::default(),
    )
    .await?;
# Ok(())
# }
```
