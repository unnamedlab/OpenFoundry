# Connector Management Service parity: Go pending vs Rust placeholders

Fecha de corte: 2026-05-08.

## Alcance y metodologia

- Alcance Go: `openfoundry-go/services/connector-management-service/internal/adapters`.
- Alcance Rust comparado: `services/connector-management-service/src/connectors`.
- Busqueda base Go: `rg -n "ErrNotImplemented" openfoundry-go/services/connector-management-service`.
- Clasificacion:
  - **Rust productivo**: el modulo Rust equivalente existe, esta exportado o contiene logica real de validacion, discovery, preview, fetch, streaming o puente de catalogo.
  - **Rust placeholder**: el modulo Rust equivalente esta vacio/no exportado y no contiene capacidades runtime.
  - **Go pendiente**: una o mas capacidades Go devuelven `adapters.ErrNotImplemented` mientras Rust tiene una capacidad productiva equivalente o relacionada.
  - **Compatible placeholder**: Go devuelve `adapters.ErrNotImplemented` porque el equivalente Rust tambien es placeholder.

## Resultado

| adapter | Rust status | Go status | accion necesaria |
|---|---|---|---|
| `azure_blob` | Rust productivo parcial: discovery/open-table catalog para Azure Blob/ADLS/OneLake. | Go pendiente parcial: discovery e ingest spec existen; preview virtual-table y Arrow devuelven `ErrNotImplemented`. | Backlog: completar preview/Arrow o documentar explicitamente que el consumo es solo via catalogo zero-copy. No se agregan tests TODO/failing porque el servicio Go no mantiene tests pendientes aceptados. |
| `bigquery` | Rust productivo: discovery, preview, fetch/bridge tabular. | Go productivo: discovery, preview, Arrow e ingest spec implementados. | Sin accion. |
| `csv` | Rust productivo: preview/fetch CSV. | Go productivo: discovery, preview, Arrow e ingest spec implementados. | Sin accion. |
| `databricks` | Rust productivo parcial: puente tabular/catalogo para Unity/Delta. | Go pendiente parcial: discovery y preview existen; Arrow e ingest spec devuelven `ErrNotImplemented`. | Backlog: portar Arrow/ingest o acotar formalmente el contrato al pushdown/catalog bridge. No se agregan tests TODO/failing porque el servicio Go no mantiene tests pendientes aceptados. |
| `excel` | Rust placeholder: archivo equivalente vacio/no exportado. | Compatible placeholder: las cuatro capacidades Go devuelven `ErrNotImplemented`. | Sin accion de implementacion en esta tarea; mantener como placeholder compatible hasta que Rust deje de ser placeholder. |
| `gcs` | Rust productivo parcial: discovery/listing/fetch de objetos GCS. | Go pendiente parcial: discovery, preview e ingest spec existen; Arrow devuelve `ErrNotImplemented`. | Backlog: implementar Arrow para objetos soportados o registrar que Arrow se delega a materializacion aguas abajo. No se agregan tests TODO/failing porque el servicio Go no mantiene tests pendientes aceptados. |
| `generic` | Rust productivo parcial: discovery de tablas Iceberg/Delta genericas y catalog URL. | Go pendiente parcial: discovery existe; preview, Arrow e ingest spec devuelven `ErrNotImplemented`. | Backlog: alinear preview/Arrow/ingest con el catalog bridge o documentar solo zero-copy. No se agregan tests TODO/failing porque el servicio Go no mantiene tests pendientes aceptados. |
| `graphql` | Rust placeholder: archivo equivalente vacio/no exportado. | Compatible placeholder: las cuatro capacidades Go devuelven `ErrNotImplemented`. | Sin accion de implementacion en esta tarea; mantener como placeholder compatible hasta que Rust deje de ser placeholder. |
| `iot` | Rust productivo parcial: MQTT test/discovery/preview. | Go pendiente parcial: discovery, preview e ingest spec existen; Arrow devuelve `ErrNotImplemented` cuando no hay runner MQTT de Arrow. | Backlog: definir/portar streaming Arrow de MQTT si se requiere paridad completa. No se agregan tests TODO/failing porque el servicio Go no mantiene tests pendientes aceptados. |
| `jdbc` | Rust productivo parcial: puente tabular/catalogo JDBC. | Go pendiente parcial: discovery y preview existen; Arrow e ingest spec devuelven `ErrNotImplemented`. | Backlog: portar Arrow/ingest o acotar el contrato al pushdown/catalog bridge. No se agregan tests TODO/failing porque el servicio Go no mantiene tests pendientes aceptados. |
| `json` | Rust productivo: preview/fetch JSON. | Go productivo: discovery, preview, Arrow e ingest spec implementados. | Sin accion. |
| `kafka` | Rust productivo parcial: broker metadata y preview de mensajes. | Go pendiente parcial: discovery y preview existen; Arrow e ingest spec devuelven `ErrNotImplemented`. | Backlog: portar Arrow/ingest para topics o documentar que Kafka Go solo cubre discovery/preview. No se agregan tests TODO/failing porque el servicio Go no mantiene tests pendientes aceptados. |
| `kinesis` | Rust productivo: shards/records con checkpoint. | Go productivo: discovery, preview, Arrow e ingest spec implementados. | Sin accion. |
| `ldap` | Rust placeholder: archivo equivalente vacio/no exportado. | Compatible placeholder: las cuatro capacidades Go devuelven `ErrNotImplemented`. | Sin accion de implementacion en esta tarea; mantener como placeholder compatible hasta que Rust deje de ser placeholder. |
| `mssql` | Rust placeholder: archivo equivalente vacio/no exportado. | Compatible placeholder parcial: discovery/preview Go usan puente tabular, pero Arrow e ingest spec devuelven `ErrNotImplemented` y Rust no tiene runtime propio. | Sin accion de implementacion grande en esta tarea; si Rust activa MSSQL, reabrir paridad Go para Arrow/ingest. |
| `mysql` | Rust productivo parcial: puente tabular/catalogo MySQL. | Go pendiente parcial: discovery y preview existen; Arrow e ingest spec devuelven `ErrNotImplemented`. | Backlog: portar Arrow/ingest o acotar el contrato al pushdown/catalog bridge. No se agregan tests TODO/failing porque el servicio Go no mantiene tests pendientes aceptados. |
| `odbc` | Rust productivo parcial: puente tabular/catalogo ODBC. | Go pendiente parcial: discovery y preview existen; Arrow e ingest spec devuelven `ErrNotImplemented`. | Backlog: portar Arrow/ingest o acotar el contrato al pushdown/catalog bridge. No se agregan tests TODO/failing porque el servicio Go no mantiene tests pendientes aceptados. |
| `onelake` | Rust productivo parcial: discovery/listing/fetch de OneLake. | Go pendiente parcial: discovery, preview e ingest spec existen; Arrow devuelve `ErrNotImplemented`. | Backlog: implementar Arrow para objetos soportados o registrar delegacion aguas abajo. No se agregan tests TODO/failing porque el servicio Go no mantiene tests pendientes aceptados. |
| `oracle` | Rust placeholder: archivo equivalente vacio/no exportado. | Compatible placeholder parcial: discovery/preview Go usan puente tabular, pero Arrow e ingest spec devuelven `ErrNotImplemented` y Rust no tiene runtime propio. | Sin accion de implementacion grande en esta tarea; si Rust activa Oracle, reabrir paridad Go para Arrow/ingest. |
| `parquet` | Rust productivo: preview/fetch Parquet. | Go productivo: discovery, preview, Arrow e ingest spec implementados. | Sin accion. |
| `power_bi` | Rust productivo parcial: puente tabular/catalogo Power BI. | Go pendiente parcial: discovery y preview existen; Arrow e ingest spec devuelven `ErrNotImplemented`. | Backlog: portar Arrow/ingest o acotar el contrato al pushdown/catalog bridge. No se agregan tests TODO/failing porque el servicio Go no mantiene tests pendientes aceptados. |
| `rest_api` | Rust productivo: REST catalog/preview/fetch. | Go pendiente parcial: discovery, preview e ingest spec existen; Arrow devuelve `ErrNotImplemented`. | Backlog: implementar Arrow para respuestas REST tabulares o registrar uso de ingest spec/fetch. No se agregan tests TODO/failing porque el servicio Go no mantiene tests pendientes aceptados. |
| `s3` | Rust productivo parcial: discovery/open-table catalog y S3 object handling. | Go pendiente parcial: discovery, preview e ingest spec existen; Arrow devuelve `ErrNotImplemented`. | Backlog: implementar Arrow para objetos soportados o registrar delegacion aguas abajo. No se agregan tests TODO/failing porque el servicio Go no mantiene tests pendientes aceptados. |
| `salesforce` | Rust productivo: discovery/preview/fetch Salesforce. | Go productivo: discovery, preview, Arrow e ingest spec implementados. | Sin accion. |
| `sap` | Rust productivo parcial: OData discovery/preview/fetch. | Go pendiente parcial: discovery, preview e ingest spec existen; Arrow devuelve `ErrNotImplemented`. | Backlog: implementar Arrow para entidades OData o registrar uso de ingest spec/fetch. No se agregan tests TODO/failing porque el servicio Go no mantiene tests pendientes aceptados. |
| `sftp` | Rust placeholder: archivo equivalente vacio/no exportado. | Compatible placeholder: las cuatro capacidades Go devuelven `ErrNotImplemented`. | Sin accion de implementacion en esta tarea; mantener como placeholder compatible hasta que Rust deje de ser placeholder. |
| `snowflake` | Rust productivo: discovery, preview, fetch/bridge tabular. | Go productivo: discovery, preview, Arrow e ingest spec implementados. | Sin accion. |
| `tableau` | Rust productivo parcial: puente tabular/catalogo Tableau. | Go pendiente parcial: discovery y preview existen; Arrow e ingest spec devuelven `ErrNotImplemented`. | Backlog: portar Arrow/ingest o acotar el contrato al pushdown/catalog bridge. No se agregan tests TODO/failing porque el servicio Go no mantiene tests pendientes aceptados. |

## Backlog Go pendiente con Rust productivo

Prioridad sugerida por brecha de capacidad:

1. **Arrow + ingest spec pendientes**: `databricks`, `jdbc`, `kafka`, `mysql`, `odbc`, `power_bi`, `tableau`.
2. **Solo Arrow pendiente**: `gcs`, `iot`, `onelake`, `rest_api`, `s3`, `sap`.
3. **Preview/Arrow/ingest pendientes o contrato zero-copy incompleto**: `generic`.
4. **Preview/Arrow pendientes en fuente open-table**: `azure_blob`.

No se crearon tests TODO/failing: el arbol de tests Go del servicio no muestra una convencion de tests pendientes/skip para backlog de conectores, y agregar tests intencionalmente fallidos romperia `go test ./services/connector-management-service/...`. El backlog anterior queda como fuente de seguimiento hasta que se implemente cada conector o se formalice su contrato limitado.
