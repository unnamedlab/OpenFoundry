package cedarauthz

// D1.1.8 P3 — Cedar policy bundle for `iceberg-catalog-service`.
//
// `iceberg-catalog-service` boots the [*AuthzEngine] from the bundled
// schema (per ADR-0027) and seeds it with the policies returned by
// [AllIcebergPolicies]. Each entry is a stand-alone [PolicyRecord] so
// a service operator can rotate / disable an individual rule without
// redeploying the binary.
//
// Policies modeled here mirror the doc invariants:
//
//   - Markings inheritance is a SNAPSHOT at table-creation time; the
//     catalog stamps `IcebergTable.markings` from the parent
//     `IcebergNamespace.markings` and only the explicit `manage_markings`
//     action mutates the row afterwards.
//   - `view` / `read_metadata` require
//     `principal.clearances.containsAll(resource.markings)`.
//     `read_metadata` is granted on the same envelope as `view` because
//     metadata.json describes structure + history but not row data.
//   - `write_data` and `alter_schema` are gated on the same clearance
//     floor + a write role (`editor` / `admin` for users, the OAuth2
//     `api:iceberg-write` scope is mapped to the service-principal
//     branch of each policy).
//   - Marking management is User-only — service principals never
//     re-classify resources.

// AllIcebergPolicies returns every iceberg-catalog policy that should
// be loaded at boot.
func AllIcebergPolicies() []PolicyRecord {
	return []PolicyRecord{
		IcebergNamespaceViewClearance(),
		IcebergNamespaceCreateUserRole(),
		IcebergNamespaceCreateServicePrincipal(),
		IcebergNamespaceDropAdminClearance(),
		IcebergNamespaceUpdatePropertiesUser(),
		IcebergNamespaceUpdatePropertiesServicePrincipal(),
		IcebergNamespaceManageMarkingsAdminOnly(),
		IcebergTableViewClearance(),
		IcebergTableReadMetadataClearance(),
		IcebergTableCreateUserRole(),
		IcebergTableCreateServicePrincipal(),
		IcebergTableDropAdminClearance(),
		IcebergTableWriteDataUser(),
		IcebergTableWriteDataServicePrincipal(),
		IcebergTableAlterSchemaUser(),
		IcebergTableAlterSchemaServicePrincipal(),
		IcebergTableManageMarkingsAdminOnly(),
	}
}

func descPtr(s string) *string { return &s }

// ── Namespaces ──────────────────────────────────────────────────────

func IcebergNamespaceViewClearance() PolicyRecord {
	return PolicyRecord{
		ID:          "iceberg-namespace-view-clearance",
		Version:     1,
		Description: descPtr("View an Iceberg namespace requires tenant + clearance over its markings."),
		Source: `
			permit(
			  principal,
			  action == Action::"iceberg::namespace::view",
			  resource is IcebergNamespace
			) when {
			  principal.tenant == resource.tenant &&
			  principal.clearances.containsAll(resource.markings)
			};
		`,
	}
}

func IcebergNamespaceCreateUserRole() PolicyRecord {
	return PolicyRecord{
		ID:          "iceberg-namespace-create-user",
		Version:     1,
		Description: descPtr("Editors / admins create namespaces inside their tenant."),
		Source: `
			permit(
			  principal is User,
			  action == Action::"iceberg::namespace::create",
			  resource is IcebergNamespace
			) when {
			  principal.tenant == resource.tenant &&
			  (principal.roles.contains("editor") || principal.roles.contains("admin"))
			};
		`,
	}
}

func IcebergNamespaceCreateServicePrincipal() PolicyRecord {
	return PolicyRecord{
		ID:      "iceberg-namespace-create-service-principal",
		Version: 1,
		Description: descPtr("OAuth2 service principals may create namespaces in their tenant. " +
			"Caller must verify scope `api:iceberg-write` separately at the bearer extractor."),
		Source: `
			permit(
			  principal is ServicePrincipal,
			  action == Action::"iceberg::namespace::create",
			  resource is IcebergNamespace
			) when {
			  principal.tenant == resource.tenant
			};
		`,
	}
}

func IcebergNamespaceDropAdminClearance() PolicyRecord {
	return PolicyRecord{
		ID:          "iceberg-namespace-drop-admin",
		Version:     1,
		Description: descPtr("Drop a namespace requires admin + full clearance."),
		Source: `
			permit(
			  principal is User,
			  action == Action::"iceberg::namespace::drop",
			  resource is IcebergNamespace
			) when {
			  principal.tenant == resource.tenant &&
			  principal.roles.contains("admin") &&
			  principal.clearances.containsAll(resource.markings)
			};
		`,
	}
}

func IcebergNamespaceUpdatePropertiesUser() PolicyRecord {
	return PolicyRecord{
		ID:          "iceberg-namespace-update-properties-user",
		Version:     1,
		Description: descPtr("Update namespace properties: editor / admin + clearance."),
		Source: `
			permit(
			  principal is User,
			  action == Action::"iceberg::namespace::update_properties",
			  resource is IcebergNamespace
			) when {
			  principal.tenant == resource.tenant &&
			  (principal.roles.contains("editor") || principal.roles.contains("admin")) &&
			  principal.clearances.containsAll(resource.markings)
			};
		`,
	}
}

func IcebergNamespaceUpdatePropertiesServicePrincipal() PolicyRecord {
	return PolicyRecord{
		ID:          "iceberg-namespace-update-properties-service-principal",
		Version:     1,
		Description: descPtr("Service principals update namespace properties when clearance covers markings."),
		Source: `
			permit(
			  principal is ServicePrincipal,
			  action == Action::"iceberg::namespace::update_properties",
			  resource is IcebergNamespace
			) when {
			  principal.tenant == resource.tenant &&
			  principal.clearances.containsAll(resource.markings)
			};
		`,
	}
}

func IcebergNamespaceManageMarkingsAdminOnly() PolicyRecord {
	return PolicyRecord{
		ID:      "iceberg-namespace-manage-markings-admin",
		Version: 1,
		Description: descPtr("Manage namespace markings is admin-only and User-only " +
			"(service principals never re-classify resources)."),
		Source: `
			permit(
			  principal is User,
			  action == Action::"iceberg::namespace::manage_markings",
			  resource is IcebergNamespace
			) when {
			  principal.tenant == resource.tenant &&
			  principal.roles.contains("admin")
			};
		`,
	}
}

// ── Tables ──────────────────────────────────────────────────────────

func IcebergTableViewClearance() PolicyRecord {
	return PolicyRecord{
		ID:          "iceberg-table-view-clearance",
		Version:     1,
		Description: descPtr("View an Iceberg table requires tenant + clearance over its effective markings."),
		Source: `
			permit(
			  principal,
			  action == Action::"iceberg::table::view",
			  resource is IcebergTable
			) when {
			  principal.tenant == resource.tenant &&
			  principal.clearances.containsAll(resource.markings)
			};
		`,
	}
}

func IcebergTableReadMetadataClearance() PolicyRecord {
	return PolicyRecord{
		ID:      "iceberg-table-read-metadata-clearance",
		Version: 1,
		Description: descPtr("Read metadata.json is on the same envelope as `view` — metadata " +
			"describes structure / history but not row data."),
		Source: `
			permit(
			  principal,
			  action == Action::"iceberg::table::read_metadata",
			  resource is IcebergTable
			) when {
			  principal.tenant == resource.tenant &&
			  principal.clearances.containsAll(resource.markings)
			};
		`,
	}
}

func IcebergTableCreateUserRole() PolicyRecord {
	return PolicyRecord{
		ID:          "iceberg-table-create-user",
		Version:     1,
		Description: descPtr("Create a table: editor / admin within tenant + clearance over namespace markings."),
		Source: `
			permit(
			  principal is User,
			  action == Action::"iceberg::table::create",
			  resource is IcebergNamespace
			) when {
			  principal.tenant == resource.tenant &&
			  (principal.roles.contains("editor") || principal.roles.contains("admin")) &&
			  principal.clearances.containsAll(resource.markings)
			};
		`,
	}
}

func IcebergTableCreateServicePrincipal() PolicyRecord {
	return PolicyRecord{
		ID:      "iceberg-table-create-service-principal",
		Version: 1,
		Description: descPtr("Service principals create tables when their clearance covers " +
			"the parent namespace markings."),
		Source: `
			permit(
			  principal is ServicePrincipal,
			  action == Action::"iceberg::table::create",
			  resource is IcebergNamespace
			) when {
			  principal.tenant == resource.tenant &&
			  principal.clearances.containsAll(resource.markings)
			};
		`,
	}
}

func IcebergTableDropAdminClearance() PolicyRecord {
	return PolicyRecord{
		ID:          "iceberg-table-drop-admin",
		Version:     1,
		Description: descPtr("Drop a table: admin + full clearance."),
		Source: `
			permit(
			  principal is User,
			  action == Action::"iceberg::table::drop",
			  resource is IcebergTable
			) when {
			  principal.tenant == resource.tenant &&
			  principal.roles.contains("admin") &&
			  principal.clearances.containsAll(resource.markings)
			};
		`,
	}
}

func IcebergTableWriteDataUser() PolicyRecord {
	return PolicyRecord{
		ID:          "iceberg-table-write-data-user",
		Version:     1,
		Description: descPtr("Write rows: editor / admin within tenant + clearance over EVERY effective marking."),
		Source: `
			permit(
			  principal is User,
			  action == Action::"iceberg::table::write_data",
			  resource is IcebergTable
			) when {
			  principal.tenant == resource.tenant &&
			  (principal.roles.contains("editor") || principal.roles.contains("admin")) &&
			  principal.clearances.containsAll(resource.markings)
			};
		`,
	}
}

func IcebergTableWriteDataServicePrincipal() PolicyRecord {
	return PolicyRecord{
		ID:      "iceberg-table-write-data-service-principal",
		Version: 1,
		Description: descPtr("OAuth2 service principal writes when clearance covers markings; " +
			"scope `api:iceberg-write` is enforced separately by the bearer extractor."),
		Source: `
			permit(
			  principal is ServicePrincipal,
			  action == Action::"iceberg::table::write_data",
			  resource is IcebergTable
			) when {
			  principal.tenant == resource.tenant &&
			  principal.clearances.containsAll(resource.markings)
			};
		`,
	}
}

func IcebergTableAlterSchemaUser() PolicyRecord {
	return PolicyRecord{
		ID:          "iceberg-table-alter-schema-user",
		Version:     1,
		Description: descPtr("Alter schema: editor / admin within tenant + full clearance."),
		Source: `
			permit(
			  principal is User,
			  action == Action::"iceberg::table::alter_schema",
			  resource is IcebergTable
			) when {
			  principal.tenant == resource.tenant &&
			  (principal.roles.contains("editor") || principal.roles.contains("admin")) &&
			  principal.clearances.containsAll(resource.markings)
			};
		`,
	}
}

func IcebergTableAlterSchemaServicePrincipal() PolicyRecord {
	return PolicyRecord{
		ID:          "iceberg-table-alter-schema-service-principal",
		Version:     1,
		Description: descPtr("Service principal alter-schema: same clearance floor as write_data."),
		Source: `
			permit(
			  principal is ServicePrincipal,
			  action == Action::"iceberg::table::alter_schema",
			  resource is IcebergTable
			) when {
			  principal.tenant == resource.tenant &&
			  principal.clearances.containsAll(resource.markings)
			};
		`,
	}
}

func IcebergTableManageMarkingsAdminOnly() PolicyRecord {
	return PolicyRecord{
		ID:      "iceberg-table-manage-markings-admin",
		Version: 1,
		Description: descPtr("Manage table markings is admin + User-only — service principals " +
			"never re-classify resources."),
		Source: `
			permit(
			  principal is User,
			  action == Action::"iceberg::table::manage_markings",
			  resource is IcebergTable
			) when {
			  principal.tenant == resource.tenant &&
			  principal.roles.contains("admin")
			};
		`,
	}
}
