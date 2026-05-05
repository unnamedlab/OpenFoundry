//! gRPC face of the virtual-table catalog.
//!
//! The HTTP handlers in `crate::handlers::virtual_tables` and this gRPC
//! server are sibling adapters over `crate::domain::virtual_tables`,
//! mirroring `media-sets-service::grpc`. Internal callers (Pipeline
//! Builder, Code Repositories, ontology services) use the gRPC port;
//! the SvelteKit app uses the HTTP port.

use tonic::{Request, Response, Status};

use crate::AppState;
use crate::domain::virtual_tables::{self, RegistrationKind};
use crate::models::virtual_table::{
    BulkRegisterRequest as DomainBulkRegisterRequest, ListVirtualTablesQuery, Locator,
    RegisterVirtualTableRequest as DomainRegisterRequest, VirtualTableRow,
};
use crate::proto::{
    BulkRegisterError as ProtoBulkError, BulkRegisterRequest, BulkRegisterResponse,
    Capabilities as ProtoCapabilities, DeleteVirtualTableRequest, DeleteVirtualTableResponse,
    DiscoverRemoteCatalogRequest, DiscoverRemoteCatalogResponse, DiscoveredEntry,
    FoundryCompute as ProtoFoundryCompute, GetVirtualTableRequest, ListVirtualTablesRequest,
    ListVirtualTablesResponse, Locator as ProtoLocator, RegisterVirtualTableRequest,
    Schema as ProtoSchema, SchemaColumn, TableType as ProtoTableType, UpdateDetection,
    VirtualTable as ProtoVirtualTable,
    locator::Kind as LocatorKind,
    virtual_table_catalog_server::{VirtualTableCatalog, VirtualTableCatalogServer},
};

pub struct CatalogService {
    state: AppState,
}

impl CatalogService {
    pub fn new(state: AppState) -> Self {
        Self { state }
    }

    pub fn into_server(self) -> VirtualTableCatalogServer<Self> {
        VirtualTableCatalogServer::new(self)
    }
}

#[tonic::async_trait]
impl VirtualTableCatalog for CatalogService {
    async fn list_virtual_tables(
        &self,
        request: Request<ListVirtualTablesRequest>,
    ) -> Result<Response<ListVirtualTablesResponse>, Status> {
        let req = request.into_inner();
        let response = virtual_tables::list_virtual_tables(
            &self.state.db,
            ListVirtualTablesQuery {
                project: optional(req.project_rid),
                source: optional(req.source_rid),
                name: optional(req.name_contains),
                table_type: proto_table_type_to_str(req.table_type),
                limit: if req.page_size > 0 {
                    Some(req.page_size as i64)
                } else {
                    None
                },
                cursor: optional(req.page_token),
            },
        )
        .await
        .map_err(map_err)?;

        Ok(Response::new(ListVirtualTablesResponse {
            items: response.items.into_iter().map(to_proto).collect(),
            next_page_token: response.next_cursor.unwrap_or_default(),
        }))
    }

    async fn get_virtual_table(
        &self,
        request: Request<GetVirtualTableRequest>,
    ) -> Result<Response<ProtoVirtualTable>, Status> {
        let row = virtual_tables::get_virtual_table(&self.state.db, &request.into_inner().rid)
            .await
            .map_err(map_err)?;
        Ok(Response::new(to_proto(row)))
    }

    async fn register_virtual_table(
        &self,
        request: Request<RegisterVirtualTableRequest>,
    ) -> Result<Response<ProtoVirtualTable>, Status> {
        let req = request.into_inner();
        let domain = proto_to_register_request(req.clone())?;
        let row = virtual_tables::register_virtual_table(
            &self.state,
            &req.source_rid,
            None,
            domain,
            RegistrationKind::Manual,
        )
        .await
        .map_err(map_err)?;
        Ok(Response::new(to_proto(row)))
    }

    async fn bulk_register(
        &self,
        request: Request<BulkRegisterRequest>,
    ) -> Result<Response<BulkRegisterResponse>, Status> {
        let req = request.into_inner();
        let mut entries = Vec::with_capacity(req.entries.len());
        for entry in req.entries {
            entries.push(proto_to_register_request(entry)?);
        }
        let domain = DomainBulkRegisterRequest {
            project_rid: req.project_rid.clone(),
            entries,
        };
        let response =
            virtual_tables::bulk_register(&self.state, &req.source_rid, None, domain)
                .await
                .map_err(map_err)?;
        Ok(Response::new(BulkRegisterResponse {
            registered: response.registered.into_iter().map(to_proto).collect(),
            errors: response
                .errors
                .into_iter()
                .map(|e| ProtoBulkError {
                    name: e.name,
                    error: e.error,
                })
                .collect(),
        }))
    }

    async fn delete_virtual_table(
        &self,
        request: Request<DeleteVirtualTableRequest>,
    ) -> Result<Response<DeleteVirtualTableResponse>, Status> {
        virtual_tables::delete_virtual_table(&self.state, &request.into_inner().rid, None)
            .await
            .map_err(map_err)?;
        Ok(Response::new(DeleteVirtualTableResponse { deleted: true }))
    }

    async fn discover_remote_catalog(
        &self,
        request: Request<DiscoverRemoteCatalogRequest>,
    ) -> Result<Response<DiscoverRemoteCatalogResponse>, Status> {
        let req = request.into_inner();
        let entries = virtual_tables::discover_remote_catalog(
            &self.state,
            &req.source_rid,
            optional(req.path).as_deref(),
        )
        .await
        .map_err(map_err)?;

        Ok(Response::new(DiscoverRemoteCatalogResponse {
            entries: entries
                .into_iter()
                .map(|entry| DiscoveredEntry {
                    display_name: entry.display_name,
                    path: entry.path,
                    kind: entry.kind,
                    registrable: entry.registrable,
                    inferred_table_type: entry
                        .inferred_table_type
                        .as_deref()
                        .map(str_to_proto_table_type)
                        .unwrap_or(ProtoTableType::Unspecified) as i32,
                })
                .collect(),
        }))
    }
}

// ---------------------------------------------------------------------------
// Conversions.
// ---------------------------------------------------------------------------

fn map_err(error: virtual_tables::VirtualTableError) -> Status {
    use virtual_tables::VirtualTableError::*;
    match error {
        SourceNotEnabled(rid) => Status::failed_precondition(format!("source not enabled: {rid}")),
        InvalidProvider(p) => Status::invalid_argument(format!("invalid provider: {p}")),
        InvalidTableType(t) => Status::invalid_argument(format!("invalid table_type: {t}")),
        NotFound(rid) => Status::not_found(format!("virtual table not found: {rid}")),
        LocatorAlreadyRegistered => Status::already_exists("locator already registered"),
        NameAlreadyTaken => Status::already_exists("name already taken"),
        Database(err) => Status::internal(format!("database error: {err}")),
        SchemaInference(msg) => Status::internal(format!("schema inference: {msg}")),
        SourceIncompatible(reason) => {
            Status::failed_precondition(format!("source incompatible: {}", reason.code()))
        }
        IcebergCatalog(msg) => Status::invalid_argument(format!("iceberg catalog: {msg}")),
    }
}

fn optional(value: String) -> Option<String> {
    if value.trim().is_empty() {
        None
    } else {
        Some(value)
    }
}

fn proto_table_type_to_str(value: i32) -> Option<String> {
    let parsed = ProtoTableType::try_from(value).unwrap_or(ProtoTableType::Unspecified);
    match parsed {
        ProtoTableType::Unspecified => None,
        ProtoTableType::Table => Some("TABLE".into()),
        ProtoTableType::View => Some("VIEW".into()),
        ProtoTableType::MaterializedView => Some("MATERIALIZED_VIEW".into()),
        ProtoTableType::ExternalDelta => Some("EXTERNAL_DELTA".into()),
        ProtoTableType::ManagedDelta => Some("MANAGED_DELTA".into()),
        ProtoTableType::ManagedIceberg => Some("MANAGED_ICEBERG".into()),
        ProtoTableType::ParquetFiles => Some("PARQUET_FILES".into()),
        ProtoTableType::AvroFiles => Some("AVRO_FILES".into()),
        ProtoTableType::CsvFiles => Some("CSV_FILES".into()),
        ProtoTableType::Other => Some("OTHER".into()),
    }
}

fn str_to_proto_table_type(value: &str) -> ProtoTableType {
    match value {
        "TABLE" => ProtoTableType::Table,
        "VIEW" => ProtoTableType::View,
        "MATERIALIZED_VIEW" => ProtoTableType::MaterializedView,
        "EXTERNAL_DELTA" => ProtoTableType::ExternalDelta,
        "MANAGED_DELTA" => ProtoTableType::ManagedDelta,
        "MANAGED_ICEBERG" => ProtoTableType::ManagedIceberg,
        "PARQUET_FILES" => ProtoTableType::ParquetFiles,
        "AVRO_FILES" => ProtoTableType::AvroFiles,
        "CSV_FILES" => ProtoTableType::CsvFiles,
        "OTHER" => ProtoTableType::Other,
        _ => ProtoTableType::Unspecified,
    }
}

fn proto_to_locator(locator: Option<ProtoLocator>) -> Result<Locator, Status> {
    let kind = locator
        .and_then(|l| l.kind)
        .ok_or_else(|| Status::invalid_argument("locator is required"))?;
    Ok(match kind {
        LocatorKind::Tabular(t) => Locator::Tabular {
            database: t.database,
            schema: t.schema,
            table: t.table,
        },
        LocatorKind::File(f) => Locator::File {
            bucket: f.bucket,
            prefix: f.prefix,
            format: f.format,
        },
        LocatorKind::Iceberg(i) => Locator::Iceberg {
            catalog: i.catalog,
            namespace: i.namespace,
            table: i.table,
        },
    })
}

fn proto_to_register_request(
    req: RegisterVirtualTableRequest,
) -> Result<DomainRegisterRequest, Status> {
    let table_type = proto_table_type_to_str(req.table_type)
        .ok_or_else(|| Status::invalid_argument("table_type is required"))?;
    let locator = proto_to_locator(req.locator)?;
    Ok(DomainRegisterRequest {
        project_rid: req.project_rid,
        name: optional(req.name),
        parent_folder_rid: optional(req.parent_folder_rid),
        locator,
        table_type,
        markings: req.markings,
    })
}

fn locator_value_to_proto(value: &serde_json::Value) -> Option<ProtoLocator> {
    let kind = value.get("kind")?.as_str()?;
    match kind {
        "tabular" => Some(ProtoLocator {
            kind: Some(LocatorKind::Tabular(crate::proto::TabularLocator {
                database: value
                    .get("database")
                    .and_then(|v| v.as_str())
                    .unwrap_or_default()
                    .into(),
                schema: value
                    .get("schema")
                    .and_then(|v| v.as_str())
                    .unwrap_or_default()
                    .into(),
                table: value
                    .get("table")
                    .and_then(|v| v.as_str())
                    .unwrap_or_default()
                    .into(),
            })),
        }),
        "file" => Some(ProtoLocator {
            kind: Some(LocatorKind::File(crate::proto::FileLocator {
                bucket: value
                    .get("bucket")
                    .and_then(|v| v.as_str())
                    .unwrap_or_default()
                    .into(),
                prefix: value
                    .get("prefix")
                    .and_then(|v| v.as_str())
                    .unwrap_or_default()
                    .into(),
                format: value
                    .get("format")
                    .and_then(|v| v.as_str())
                    .unwrap_or_default()
                    .into(),
            })),
        }),
        "iceberg" => Some(ProtoLocator {
            kind: Some(LocatorKind::Iceberg(crate::proto::IcebergLocator {
                catalog: value
                    .get("catalog")
                    .and_then(|v| v.as_str())
                    .unwrap_or_default()
                    .into(),
                namespace: value
                    .get("namespace")
                    .and_then(|v| v.as_str())
                    .unwrap_or_default()
                    .into(),
                table: value
                    .get("table")
                    .and_then(|v| v.as_str())
                    .unwrap_or_default()
                    .into(),
            })),
        }),
        _ => None,
    }
}

fn to_proto(row: VirtualTableRow) -> ProtoVirtualTable {
    let table_type = str_to_proto_table_type(&row.table_type) as i32;
    let schema_columns: Vec<SchemaColumn> = row
        .schema_inferred
        .as_array()
        .into_iter()
        .flat_map(|arr| arr.iter())
        .filter_map(|item| {
            Some(SchemaColumn {
                name: item.get("name")?.as_str()?.to_string(),
                source_type: item.get("source_type")?.as_str()?.to_string(),
                inferred_type: item.get("inferred_type")?.as_str()?.to_string(),
                nullable: item
                    .get("nullable")
                    .and_then(|v| v.as_bool())
                    .unwrap_or(true),
            })
        })
        .collect();

    let capabilities = row.capabilities_typed().map(|c| ProtoCapabilities {
        read: c.read,
        write: c.write,
        incremental: c.incremental,
        versioning: c.versioning,
        compute_pushdown: c
            .compute_pushdown
            .map(|engine| engine.as_str().to_string())
            .unwrap_or_default(),
        snapshot_supported: c.snapshot_supported,
        append_only_supported: c.append_only_supported,
        foundry_compute: Some(ProtoFoundryCompute {
            python_single_node: c.foundry_compute.python_single_node,
            python_spark: c.foundry_compute.python_spark,
            pipeline_builder_single_node: c.foundry_compute.pipeline_builder_single_node,
            pipeline_builder_spark: c.foundry_compute.pipeline_builder_spark,
        }),
    });

    let last_polled_at = row.last_polled_at.map(|ts| prost_types::Timestamp {
        seconds: ts.timestamp(),
        nanos: ts.timestamp_subsec_nanos() as i32,
    });

    ProtoVirtualTable {
        rid: row.rid,
        source_rid: row.source_rid,
        project_rid: row.project_rid,
        name: row.name,
        parent_folder_rid: row.parent_folder_rid.unwrap_or_default(),
        locator: locator_value_to_proto(&row.locator),
        table_type,
        schema: Some(ProtoSchema {
            columns: schema_columns,
        }),
        capabilities,
        update_detection: Some(UpdateDetection {
            enabled: row.update_detection_enabled,
            interval_seconds: row.update_detection_interval_seconds.unwrap_or(0) as u32,
            last_observed_version: row.last_observed_version.unwrap_or_default(),
            last_polled_at,
        }),
        markings: row.markings,
        properties: Default::default(),
        created_at: Some(prost_types::Timestamp {
            seconds: row.created_at.timestamp(),
            nanos: row.created_at.timestamp_subsec_nanos() as i32,
        }),
        updated_at: Some(prost_types::Timestamp {
            seconds: row.updated_at.timestamp(),
            nanos: row.updated_at.timestamp_subsec_nanos() as i32,
        }),
    }
}
