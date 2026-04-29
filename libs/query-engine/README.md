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
# query-engine

OpenFoundry's wrapper around [Apache DataFusion]: shared `SessionContext`
configuration, custom UDFs, optimizer rules, and table providers used across
data-plane services.

## Modules

| Module             | Purpose                                                                  |
|--------------------|--------------------------------------------------------------------------|
| `context`          | `QueryContext`, a thin facade over `SessionContext`                      |
| `datasource`       | Custom DataFusion table providers (object-store, etc. — work in progress)|
| `udf`              | Project-wide scalar / aggregate UDFs (work in progress)                  |
| `optimizer_rules`  | Custom logical / physical optimizer rules (work in progress)             |
| `flight_provider`  | `TableProvider` backed by a remote Apache Arrow Flight SQL endpoint      |

## Cargo features

| Feature         | Default | What it pulls in                                                        |
|-----------------|---------|-------------------------------------------------------------------------|
| `flight-client` | **on**  | `arrow-flight` (with `flight-sql-experimental`) and `tonic`. Enables `flight_provider`, used by every data-plane service that federates queries. |

To compile a service that does not need outbound Flight SQL federation:

```bash
cargo build -p query-engine --no-default-features
```

## `flight_provider::FlightSqlTableProvider`

Federates a SQL query to another Flight SQL service and exposes the result-set
as a regular DataFusion table.

```rust,no_run
use std::sync::Arc;
use arrow::datatypes::{DataType, Field, Schema};
use datafusion::prelude::SessionContext;
use query_engine::flight_provider::FlightSqlTableProvider;

# async fn demo() -> datafusion::error::Result<()> {
let schema = Arc::new(Schema::new(vec![
    Field::new("id", DataType::Int64, false),
]));

let provider = FlightSqlTableProvider::new(
    "http://datalake.internal:50051",   // tonic endpoint URI
    "SELECT id FROM warehouse.events",  // SQL pushed to the remote service
    schema,
);

let ctx = SessionContext::new();
ctx.register_table("events", Arc::new(provider))?;

let _ = ctx.sql("SELECT count(*) FROM events").await?.collect().await?;
# Ok(()) }
```

### Push-down behaviour

| Push-down  | Status         | Notes                                                                     |
|------------|----------------|---------------------------------------------------------------------------|
| projection | **supported**  | Forwarded to the in-memory exec plan that wraps the fetched batches.      |
| limit      | **supported**  | Trivial: collected batches are sliced before being handed to the plan.    |
| filter     | *out of scope* | `supports_filters_pushdown` returns `Unsupported`; bake predicates into the SQL string instead. DataFusion will still apply the filter locally. |

The provider issues one `execute` RPC per `scan` and then drains every Flight
endpoint advertised by the resulting `FlightInfo`. The schema must be supplied
by the caller because DataFusion needs it at planning time, before any I/O.

## Tests

Integration tests under `tests/flight_provider.rs` spin up an in-process tonic
Flight SQL server bound to an ephemeral local TCP port and execute
`SELECT * FROM t` end-to-end through `register_table`. Run them with:

```bash
cargo test -p query-engine
```

[Apache DataFusion]: https://arrow.apache.org/datafusion/
