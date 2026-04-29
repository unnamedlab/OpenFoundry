# `query-engine`

Apache DataFusion wrappers, custom UDFs, and table providers used across the
OpenFoundry data plane.

## Modules

| Module | Purpose |
| --- | --- |
| `context` | High-level `QueryContext` that wraps `datafusion::SessionContext`. |
| `datasource` | Custom DataFusion table providers (object storage, etc.). |
| `flight_provider` | **Flight SQL** `TableProvider` (see below). |
| `optimizer_rules` | Project-specific physical/logical optimizer rules. |
| `udf` | OpenFoundry-specific scalar / aggregate UDFs. |

## Cargo features

| Feature | Default | Description |
| --- | :---: | --- |
| `flight-client` | ✅ | Enables `FlightSqlTableProvider`, which lets any data-plane service federate queries against a remote Apache Arrow Flight SQL endpoint. Pulls in `arrow-flight` (with the `flight-sql-experimental` feature) and `tonic`. |

The Flight SQL provider is on by default because federation is a core
capability for OpenFoundry services. Disable it with
`--no-default-features` if you ship a build that does not need the extra
dependencies.

## `FlightSqlTableProvider`

`FlightSqlTableProvider` is a DataFusion `TableProvider` backed by a remote
Flight SQL endpoint. Once registered into a `SessionContext`, the remote
result set behaves like any other DataFusion table:

```rust,ignore
use std::sync::Arc;
use datafusion::prelude::SessionContext;
use query_engine::FlightSqlTableProvider;

let provider = FlightSqlTableProvider::try_new(
    "http://flight-sql.internal:50051",
    "SELECT id, name FROM customers",
)
.await?;

let ctx = SessionContext::new();
ctx.register_table("customers", Arc::new(provider))?;
let df = ctx.sql("SELECT name FROM customers LIMIT 10").await?;
df.show().await?;
```

`try_new` connects once to fetch the Arrow schema. If the schema is already
known (for example because it is part of a service contract) you can avoid
that round-trip with `FlightSqlTableProvider::new_with_schema`.

### Push-down

| Operation | Strategy |
| --- | --- |
| Projection (`SELECT col_a, col_b`) | Applied locally on each `RecordBatch` after it arrives from the remote endpoint. |
| `LIMIT n` | The local stream stops as soon as `n` rows have been emitted; the boundary batch is sliced. |
| Filter (`WHERE …`) | **Out of scope.** A generic provider cannot safely translate arbitrary DataFusion `Expr`s into the SQL dialect of an unknown remote engine. Embed any required predicate directly in the `query` you pass to `try_new`. |

### Errors

Connection, transport and Arrow-level failures are reported through
`FlightProviderError`, which converts into `datafusion::error::DataFusionError`
via `From`.

## Tests

The integration test in `tests/flight_provider.rs` spins up a tiny
in-process Flight SQL server with `tonic` on an ephemeral TCP port and
verifies that `SELECT *`, projection push-down, and `LIMIT` push-down all
behave as expected. Run it with:

```bash
cargo test -p query-engine
```
