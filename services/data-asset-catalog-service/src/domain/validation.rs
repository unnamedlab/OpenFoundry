use crate::domain::file_format::{
    FORMAT_AVRO, FORMAT_CSV, FORMAT_JSON, FORMAT_PARQUET, FORMAT_TEXT, FORMAT_UNKNOWN,
};

pub const VALID_HEALTH_STATUSES: &[&str] =
    &["unknown", "healthy", "warning", "degraded", "critical"];

pub const VALID_PERMISSION_PRINCIPAL_KINDS: &[&str] = &[
    "user",
    "group",
    "role",
    "organization",
    "project",
    "service",
];

pub const VALID_PERMISSION_SOURCES: &[&str] = &[
    "direct",
    "inherited_from_project",
    "inherited_from_folder",
    "inherited_from_parent",
];

pub const VALID_LINEAGE_DIRECTIONS: &[&str] = &["upstream", "downstream"];
pub const VALID_FILE_ENTRY_TYPES: &[&str] = &["file", "directory"];

pub fn validate_dataset_name(name: &str) -> Result<(), String> {
    let trimmed = name.trim();
    if trimmed.is_empty() {
        return Err("dataset name is required".to_string());
    }
    if trimmed.len() > 255 {
        return Err("dataset name must be 255 characters or fewer".to_string());
    }
    Ok(())
}

pub fn validate_dataset_format(format: &str) -> Result<(), String> {
    let normalized = format.to_ascii_lowercase();
    let valid = [
        FORMAT_PARQUET,
        FORMAT_AVRO,
        FORMAT_CSV,
        FORMAT_JSON,
        FORMAT_TEXT,
        FORMAT_UNKNOWN,
    ];
    if valid.contains(&normalized.as_str()) {
        Ok(())
    } else {
        Err(format!("unsupported dataset format: {format}"))
    }
}

pub fn validate_health_status(status: &str) -> Result<(), String> {
    validate_member(status, VALID_HEALTH_STATUSES, "health_status")
}

pub fn validate_permission_edge(
    principal_kind: &str,
    principal_id: &str,
    role: &str,
    actions: &[String],
    source: &str,
    inherited_from: Option<&str>,
) -> Result<(), String> {
    validate_member(
        principal_kind,
        VALID_PERMISSION_PRINCIPAL_KINDS,
        "principal_kind",
    )?;
    validate_member(source, VALID_PERMISSION_SOURCES, "source")?;
    if principal_id.trim().is_empty() {
        return Err("principal_id is required".to_string());
    }
    if role.trim().is_empty() {
        return Err("role is required".to_string());
    }
    if actions.iter().any(|action| action.trim().is_empty()) {
        return Err("permission actions cannot be empty".to_string());
    }
    match (source, inherited_from) {
        ("direct", None) => Ok(()),
        ("direct", Some(_)) => Err("direct permissions cannot set inherited_from".to_string()),
        (_, Some(value)) if !value.trim().is_empty() => Ok(()),
        _ => Err("inherited permissions require inherited_from".to_string()),
    }
}

pub fn validate_lineage_link(direction: &str, target_rid: &str) -> Result<(), String> {
    validate_member(direction, VALID_LINEAGE_DIRECTIONS, "direction")?;
    if target_rid.trim().is_empty() {
        return Err("target_rid is required".to_string());
    }
    Ok(())
}

pub fn validate_file_index_entry(
    path: &str,
    storage_path: &str,
    entry_type: &str,
    size_bytes: i64,
) -> Result<(), String> {
    if path.trim().is_empty() {
        return Err("file path is required".to_string());
    }
    if storage_path.trim().is_empty() {
        return Err("storage_path is required".to_string());
    }
    validate_member(entry_type, VALID_FILE_ENTRY_TYPES, "entry_type")?;
    if size_bytes < 0 {
        return Err("size_bytes must be non-negative".to_string());
    }
    Ok(())
}

fn validate_member(value: &str, allowed: &[&str], field: &str) -> Result<(), String> {
    if allowed.contains(&value) {
        Ok(())
    } else {
        Err(format!("{field} must be one of: {}", allowed.join(", ")))
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn validates_foundry_dataset_metadata_fields() {
        assert!(validate_dataset_name("orders").is_ok());
        assert!(validate_dataset_name(" ").is_err());
        assert!(validate_dataset_format("parquet").is_ok());
        assert!(validate_dataset_format("excel").is_err());
        assert!(validate_health_status("healthy").is_ok());
        assert!(validate_health_status("green").is_err());
    }

    #[test]
    fn validates_permission_inheritance_shape() {
        assert!(
            validate_permission_edge(
                "group",
                "analysts",
                "viewer",
                &["read".to_string()],
                "direct",
                None,
            )
            .is_ok()
        );
        assert!(
            validate_permission_edge(
                "group",
                "analysts",
                "viewer",
                &["read".to_string()],
                "direct",
                Some("project-a"),
            )
            .is_err()
        );
        assert!(
            validate_permission_edge(
                "group",
                "analysts",
                "viewer",
                &["read".to_string()],
                "inherited_from_project",
                Some("project-a"),
            )
            .is_ok()
        );
    }

    #[test]
    fn validates_lineage_and_files() {
        assert!(validate_lineage_link("upstream", "ri.dataset.parent").is_ok());
        assert!(validate_lineage_link("sideways", "ri.dataset.parent").is_err());
        assert!(validate_file_index_entry("current/data.parquet", "s3://x", "file", 42).is_ok());
        assert!(validate_file_index_entry("current/data.parquet", "s3://x", "file", -1).is_err());
    }
}
