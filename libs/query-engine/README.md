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
