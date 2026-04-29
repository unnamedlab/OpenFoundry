use serde::Deserialize;
use uuid::Uuid;

#[derive(Debug, Clone, Deserialize)]
pub struct AppConfig {
    #[serde(default = "default_host")]
    pub host: String,
    #[serde(default = "default_port")]
    pub port: u16,
    pub database_url: String,
    pub jwt_secret: String,
    #[serde(default = "default_storage_backend")]
    pub storage_backend: String,
    #[serde(default = "default_storage_bucket")]
    pub storage_bucket: String,
    #[serde(default)]
    pub s3_endpoint: Option<String>,
    #[serde(default)]
    pub s3_region: Option<String>,
    #[serde(default)]
    pub s3_access_key: Option<String>,
    #[serde(default)]
    pub s3_secret_key: Option<String>,
    #[serde(default)]
    pub local_storage_root: Option<String>,
    #[serde(default = "default_dataset_quality_service_url")]
    pub dataset_quality_service_url: String,
    #[serde(default)]
    pub lakehouse_prefixes: LakehousePrefixes,
}

#[derive(Debug, Clone, Deserialize)]
pub struct LakehousePrefixes {
    #[serde(default = "default_lakehouse_bronze_prefix")]
    pub bronze: String,
    #[serde(default = "default_lakehouse_silver_prefix")]
    pub silver: String,
    #[serde(default = "default_lakehouse_gold_prefix")]
    pub gold: String,
    #[serde(default = "default_lakehouse_serving_extracts_prefix")]
    pub serving_extracts: String,
}

impl Default for LakehousePrefixes {
    fn default() -> Self {
        Self {
            bronze: default_lakehouse_bronze_prefix(),
            silver: default_lakehouse_silver_prefix(),
            gold: default_lakehouse_gold_prefix(),
            serving_extracts: default_lakehouse_serving_extracts_prefix(),
        }
    }
}

fn default_host() -> String {
    "0.0.0.0".to_string()
}

fn default_port() -> u16 {
    50079
}

fn default_storage_backend() -> String {
    "s3".to_string()
}

fn default_storage_bucket() -> String {
    "datasets".to_string()
}

fn default_dataset_quality_service_url() -> String {
    "http://localhost:50072".to_string()
}

fn default_lakehouse_bronze_prefix() -> String {
    "bronze".to_string()
}

fn default_lakehouse_silver_prefix() -> String {
    "silver".to_string()
}

fn default_lakehouse_gold_prefix() -> String {
    "gold".to_string()
}

fn default_lakehouse_serving_extracts_prefix() -> String {
    "serving/extracts".to_string()
}

impl AppConfig {
    pub fn from_env() -> Result<Self, config::ConfigError> {
        let manifest_dir = std::path::PathBuf::from(env!("CARGO_MANIFEST_DIR"));
        let runtime_env = runtime_env_name();
        config::Config::builder()
            .add_source(
                config::File::from(manifest_dir.join("config/default.toml")).required(false),
            )
            .add_source(
                config::File::from(manifest_dir.join(format!("config/{runtime_env}.toml")))
                    .required(false),
            )
            .add_source(config::Environment::default().separator("__"))
            .build()?
            .try_deserialize()
    }

    pub fn bronze_dataset_storage_path(&self, dataset_id: Uuid) -> String {
        build_dataset_storage_path(&self.lakehouse_prefixes.bronze, dataset_id)
    }
}

fn runtime_env_name() -> String {
    match std::env::var("OPENFOUNDRY_ENV")
        .or_else(|_| std::env::var("APP_ENV"))
        .unwrap_or_else(|_| "default".to_string())
        .to_ascii_lowercase()
        .as_str()
    {
        "development" | "dev" => "default".to_string(),
        "production" => "prod".to_string(),
        other => other.to_string(),
    }
}

pub fn build_dataset_storage_path(prefix: &str, dataset_id: Uuid) -> String {
    let normalized_prefix = prefix.trim().trim_matches('/');
    if normalized_prefix.is_empty() {
        format!("datasets/{dataset_id}")
    } else {
        format!("{normalized_prefix}/{dataset_id}")
    }
}

#[cfg(test)]
mod tests {
    use uuid::Uuid;

    use super::{AppConfig, LakehousePrefixes, build_dataset_storage_path};

    #[test]
    fn defaults_lakehouse_prefixes() {
        let config = AppConfig {
            host: "0.0.0.0".to_string(),
            port: 50079,
            database_url: "postgres://localhost/openfoundry".to_string(),
            jwt_secret: "secret".to_string(),
            storage_backend: "s3".to_string(),
            storage_bucket: "datasets".to_string(),
            s3_endpoint: None,
            s3_region: None,
            s3_access_key: None,
            s3_secret_key: None,
            local_storage_root: None,
            dataset_quality_service_url: "http://localhost:50072".to_string(),
            lakehouse_prefixes: LakehousePrefixes::default(),
        };

        assert_eq!(config.lakehouse_prefixes.bronze, "bronze");
        assert_eq!(config.lakehouse_prefixes.silver, "silver");
        assert_eq!(config.lakehouse_prefixes.gold, "gold");
        assert_eq!(
            config.lakehouse_prefixes.serving_extracts,
            "serving/extracts"
        );
    }

    #[test]
    fn builds_dataset_storage_path_from_prefix() {
        let dataset_id = Uuid::nil();
        assert_eq!(
            build_dataset_storage_path("bronze", dataset_id),
            "bronze/00000000-0000-0000-0000-000000000000"
        );
        assert_eq!(
            build_dataset_storage_path("/bronze/", dataset_id),
            "bronze/00000000-0000-0000-0000-000000000000"
        );
        assert_eq!(
            build_dataset_storage_path("   ", dataset_id),
            "datasets/00000000-0000-0000-0000-000000000000"
        );
    }
}
