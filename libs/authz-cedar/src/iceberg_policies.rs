//! D1.1.8 P3 — Cedar policy bundle for `iceberg-catalog-service`.
//!
//! `iceberg-catalog-service` boots the [`AuthzEngine`] from the
//! bundled schema (per ADR-0027) and seeds it with the policies
//! returned by [`all_iceberg_policies`]. Each entry is a stand-alone
//! [`PolicyRecord`] so a service operator can rotate / disable an
//! individual rule without redeploying the binary.
//!
//! Policies modeled here mirror the doc invariants:
//!
//!   * Markings inheritance is a **snapshot** at table-creation time;
//!     the catalog stamps `IcebergTable.markings` from the parent
//!     `IcebergNamespace.markings` and only the explicit
//!     `manage_markings` action mutates the row afterwards.
//!   * `view` / `read_metadata` require
//!     `principal.clearances.containsAll(resource.markings)`.
//!     `read_metadata` is granted on the same envelope as `view`
//!     because metadata.json describes structure + history but not
//!     row data.
//!   * `write_data` and `alter_schema` are gated on the same clearance
//!     floor + a write role (`editor` / `admin` for users, the OAuth2
//!     `api:iceberg-write` scope is mapped to the service-principal
//!     branch of each policy).
//!   * Marking management is **User-only** — service principals never
//!     re-classify resources.

use crate::PolicyRecord;

/// Every iceberg-catalog policy that should be loaded at boot.
pub fn all_iceberg_policies() -> Vec<PolicyRecord> {
    vec![
        namespace_view_clearance(),
        namespace_create_user_role(),
        namespace_create_service_principal(),
        namespace_drop_admin_clearance(),
        namespace_update_properties_user(),
        namespace_update_properties_service_principal(),
        namespace_manage_markings_admin_only(),
        table_view_clearance(),
        table_read_metadata_clearance(),
        table_create_user_role(),
        table_create_service_principal(),
        table_drop_admin_clearance(),
        table_write_data_user(),
        table_write_data_service_principal(),
        table_alter_schema_user(),
        table_alter_schema_service_principal(),
        table_manage_markings_admin_only(),
    ]
}

// ── Namespaces ───────────────────────────────────────────────────────

pub fn namespace_view_clearance() -> PolicyRecord {
    PolicyRecord {
        id: "iceberg-namespace-view-clearance".into(),
        version: 1,
        description: Some(
            "View an Iceberg namespace requires tenant + clearance over its markings.".into(),
        ),
        source: r#"
            permit(
              principal,
              action == Action::"iceberg::namespace::view",
              resource is IcebergNamespace
            ) when {
              principal.tenant == resource.tenant &&
              principal.clearances.containsAll(resource.markings)
            };
        "#
        .into(),
    }
}

pub fn namespace_create_user_role() -> PolicyRecord {
    PolicyRecord {
        id: "iceberg-namespace-create-user".into(),
        version: 1,
        description: Some("Editors / admins create namespaces inside their tenant.".into()),
        source: r#"
            permit(
              principal is User,
              action == Action::"iceberg::namespace::create",
              resource is IcebergNamespace
            ) when {
              principal.tenant == resource.tenant &&
              (principal.roles.contains("editor") || principal.roles.contains("admin"))
            };
        "#
        .into(),
    }
}

pub fn namespace_create_service_principal() -> PolicyRecord {
    PolicyRecord {
        id: "iceberg-namespace-create-service-principal".into(),
        version: 1,
        description: Some(
            "OAuth2 service principals may create namespaces in their tenant. Caller \
             must verify scope `api:iceberg-write` separately at the bearer extractor."
                .into(),
        ),
        source: r#"
            permit(
              principal is ServicePrincipal,
              action == Action::"iceberg::namespace::create",
              resource is IcebergNamespace
            ) when {
              principal.tenant == resource.tenant
            };
        "#
        .into(),
    }
}

pub fn namespace_drop_admin_clearance() -> PolicyRecord {
    PolicyRecord {
        id: "iceberg-namespace-drop-admin".into(),
        version: 1,
        description: Some("Drop a namespace requires admin + full clearance.".into()),
        source: r#"
            permit(
              principal is User,
              action == Action::"iceberg::namespace::drop",
              resource is IcebergNamespace
            ) when {
              principal.tenant == resource.tenant &&
              principal.roles.contains("admin") &&
              principal.clearances.containsAll(resource.markings)
            };
        "#
        .into(),
    }
}

pub fn namespace_update_properties_user() -> PolicyRecord {
    PolicyRecord {
        id: "iceberg-namespace-update-properties-user".into(),
        version: 1,
        description: Some(
            "Update namespace properties: editor / admin + clearance.".into(),
        ),
        source: r#"
            permit(
              principal is User,
              action == Action::"iceberg::namespace::update_properties",
              resource is IcebergNamespace
            ) when {
              principal.tenant == resource.tenant &&
              (principal.roles.contains("editor") || principal.roles.contains("admin")) &&
              principal.clearances.containsAll(resource.markings)
            };
        "#
        .into(),
    }
}

pub fn namespace_update_properties_service_principal() -> PolicyRecord {
    PolicyRecord {
        id: "iceberg-namespace-update-properties-service-principal".into(),
        version: 1,
        description: Some(
            "Service principals update namespace properties when clearance covers markings."
                .into(),
        ),
        source: r#"
            permit(
              principal is ServicePrincipal,
              action == Action::"iceberg::namespace::update_properties",
              resource is IcebergNamespace
            ) when {
              principal.tenant == resource.tenant &&
              principal.clearances.containsAll(resource.markings)
            };
        "#
        .into(),
    }
}

pub fn namespace_manage_markings_admin_only() -> PolicyRecord {
    PolicyRecord {
        id: "iceberg-namespace-manage-markings-admin".into(),
        version: 1,
        description: Some(
            "Manage namespace markings is admin-only and User-only (service \
             principals never re-classify resources)."
                .into(),
        ),
        source: r#"
            permit(
              principal is User,
              action == Action::"iceberg::namespace::manage_markings",
              resource is IcebergNamespace
            ) when {
              principal.tenant == resource.tenant &&
              principal.roles.contains("admin")
            };
        "#
        .into(),
    }
}

// ── Tables ───────────────────────────────────────────────────────────

pub fn table_view_clearance() -> PolicyRecord {
    PolicyRecord {
        id: "iceberg-table-view-clearance".into(),
        version: 1,
        description: Some(
            "View an Iceberg table requires tenant + clearance over its effective markings."
                .into(),
        ),
        source: r#"
            permit(
              principal,
              action == Action::"iceberg::table::view",
              resource is IcebergTable
            ) when {
              principal.tenant == resource.tenant &&
              principal.clearances.containsAll(resource.markings)
            };
        "#
        .into(),
    }
}

pub fn table_read_metadata_clearance() -> PolicyRecord {
    PolicyRecord {
        id: "iceberg-table-read-metadata-clearance".into(),
        version: 1,
        description: Some(
            "Read metadata.json is on the same envelope as `view` — metadata \
             describes structure / history but not row data."
                .into(),
        ),
        source: r#"
            permit(
              principal,
              action == Action::"iceberg::table::read_metadata",
              resource is IcebergTable
            ) when {
              principal.tenant == resource.tenant &&
              principal.clearances.containsAll(resource.markings)
            };
        "#
        .into(),
    }
}

pub fn table_create_user_role() -> PolicyRecord {
    PolicyRecord {
        id: "iceberg-table-create-user".into(),
        version: 1,
        description: Some(
            "Create a table: editor / admin within tenant + clearance over namespace markings."
                .into(),
        ),
        source: r#"
            permit(
              principal is User,
              action == Action::"iceberg::table::create",
              resource is IcebergNamespace
            ) when {
              principal.tenant == resource.tenant &&
              (principal.roles.contains("editor") || principal.roles.contains("admin")) &&
              principal.clearances.containsAll(resource.markings)
            };
        "#
        .into(),
    }
}

pub fn table_create_service_principal() -> PolicyRecord {
    PolicyRecord {
        id: "iceberg-table-create-service-principal".into(),
        version: 1,
        description: Some(
            "Service principals create tables when their clearance covers the \
             parent namespace markings."
                .into(),
        ),
        source: r#"
            permit(
              principal is ServicePrincipal,
              action == Action::"iceberg::table::create",
              resource is IcebergNamespace
            ) when {
              principal.tenant == resource.tenant &&
              principal.clearances.containsAll(resource.markings)
            };
        "#
        .into(),
    }
}

pub fn table_drop_admin_clearance() -> PolicyRecord {
    PolicyRecord {
        id: "iceberg-table-drop-admin".into(),
        version: 1,
        description: Some("Drop a table: admin + full clearance.".into()),
        source: r#"
            permit(
              principal is User,
              action == Action::"iceberg::table::drop",
              resource is IcebergTable
            ) when {
              principal.tenant == resource.tenant &&
              principal.roles.contains("admin") &&
              principal.clearances.containsAll(resource.markings)
            };
        "#
        .into(),
    }
}

pub fn table_write_data_user() -> PolicyRecord {
    PolicyRecord {
        id: "iceberg-table-write-data-user".into(),
        version: 1,
        description: Some(
            "Write rows: editor / admin within tenant + clearance over EVERY \
             effective marking."
                .into(),
        ),
        source: r#"
            permit(
              principal is User,
              action == Action::"iceberg::table::write_data",
              resource is IcebergTable
            ) when {
              principal.tenant == resource.tenant &&
              (principal.roles.contains("editor") || principal.roles.contains("admin")) &&
              principal.clearances.containsAll(resource.markings)
            };
        "#
        .into(),
    }
}

pub fn table_write_data_service_principal() -> PolicyRecord {
    PolicyRecord {
        id: "iceberg-table-write-data-service-principal".into(),
        version: 1,
        description: Some(
            "OAuth2 service principal writes when clearance covers markings; \
             scope `api:iceberg-write` is enforced separately by the bearer \
             extractor."
                .into(),
        ),
        source: r#"
            permit(
              principal is ServicePrincipal,
              action == Action::"iceberg::table::write_data",
              resource is IcebergTable
            ) when {
              principal.tenant == resource.tenant &&
              principal.clearances.containsAll(resource.markings)
            };
        "#
        .into(),
    }
}

pub fn table_alter_schema_user() -> PolicyRecord {
    PolicyRecord {
        id: "iceberg-table-alter-schema-user".into(),
        version: 1,
        description: Some(
            "Alter schema: editor / admin within tenant + full clearance.".into(),
        ),
        source: r#"
            permit(
              principal is User,
              action == Action::"iceberg::table::alter_schema",
              resource is IcebergTable
            ) when {
              principal.tenant == resource.tenant &&
              (principal.roles.contains("editor") || principal.roles.contains("admin")) &&
              principal.clearances.containsAll(resource.markings)
            };
        "#
        .into(),
    }
}

pub fn table_alter_schema_service_principal() -> PolicyRecord {
    PolicyRecord {
        id: "iceberg-table-alter-schema-service-principal".into(),
        version: 1,
        description: Some(
            "Service principal alter-schema: same clearance floor as write_data.".into(),
        ),
        source: r#"
            permit(
              principal is ServicePrincipal,
              action == Action::"iceberg::table::alter_schema",
              resource is IcebergTable
            ) when {
              principal.tenant == resource.tenant &&
              principal.clearances.containsAll(resource.markings)
            };
        "#
        .into(),
    }
}

pub fn table_manage_markings_admin_only() -> PolicyRecord {
    PolicyRecord {
        id: "iceberg-table-manage-markings-admin".into(),
        version: 1,
        description: Some(
            "Manage table markings is admin + User-only — service principals \
             never re-classify resources."
                .into(),
        ),
        source: r#"
            permit(
              principal is User,
              action == Action::"iceberg::table::manage_markings",
              resource is IcebergTable
            ) when {
              principal.tenant == resource.tenant &&
              principal.roles.contains("admin")
            };
        "#
        .into(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::PolicyStore;

    #[tokio::test]
    async fn all_iceberg_policies_validate_against_schema() {
        let store = PolicyStore::empty().expect("schema parses");
        store
            .replace_policies(&all_iceberg_policies())
            .await
            .expect("policies validate against schema");
        assert!(store.len().await >= 17);
    }
}
