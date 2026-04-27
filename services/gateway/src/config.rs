use serde::Deserialize;

use crate::middleware::rate_limit::RateLimitConfig;

#[derive(Debug, Clone, Deserialize)]
pub struct GatewayConfig {
    #[serde(default = "default_host")]
    pub host: String,
    #[serde(default = "default_port")]
    pub port: u16,
    pub jwt_secret: String,
    #[serde(default)]
    pub redis_url: Option<String>,
    #[serde(default)]
    pub cors_origins: Vec<String>,
    #[serde(default = "default_auth_url")]
    pub auth_service_url: String,
    #[serde(default = "default_identity_federation_service_url")]
    pub identity_federation_service_url: String,
    #[serde(default = "default_oauth_integration_service_url")]
    pub oauth_integration_service_url: String,
    #[serde(default = "default_session_governance_service_url")]
    pub session_governance_service_url: String,
    #[serde(default = "default_authorization_policy_service_url")]
    pub authorization_policy_service_url: String,
    #[serde(default = "default_security_governance_service_url")]
    pub security_governance_service_url: String,
    #[serde(default = "default_tenancy_organizations_service_url")]
    pub tenancy_organizations_service_url: String,
    #[serde(default = "default_cipher_service_url")]
    pub cipher_service_url: String,
    #[serde(default = "default_data_connector_url")]
    pub data_connector_service_url: String,
    #[serde(default = "default_connector_management_service_url")]
    pub connector_management_service_url: String,
    #[serde(default = "default_virtual_table_service_url")]
    pub virtual_table_service_url: String,
    #[serde(default = "default_ingestion_replication_service_url")]
    pub ingestion_replication_service_url: String,
    #[serde(default = "default_dataset_versioning_service_url")]
    pub dataset_versioning_service_url: String,
    #[serde(default = "default_data_asset_catalog_service_url")]
    pub data_asset_catalog_service_url: String,
    #[serde(default = "default_dataset_quality_service_url")]
    pub dataset_quality_service_url: String,
    #[serde(default = "default_query_url")]
    pub query_service_url: String,
    #[serde(default = "default_pipeline_authoring_service_url")]
    pub pipeline_authoring_service_url: String,
    #[serde(default = "default_pipeline_build_service_url")]
    pub pipeline_build_service_url: String,
    #[serde(default = "default_pipeline_schedule_service_url")]
    pub pipeline_schedule_service_url: String,
    #[serde(default = "default_lineage_service_url")]
    pub lineage_service_url: String,
    #[serde(default = "default_ontology_definition_service_url")]
    pub ontology_definition_service_url: String,
    #[serde(default = "default_object_database_service_url")]
    pub object_database_service_url: String,
    #[serde(default = "default_ontology_query_service_url")]
    pub ontology_query_service_url: String,
    #[serde(default = "default_ontology_actions_service_url")]
    pub ontology_actions_service_url: String,
    #[serde(default = "default_ontology_funnel_service_url")]
    pub ontology_funnel_service_url: String,
    #[serde(default = "default_ontology_functions_service_url")]
    pub ontology_functions_service_url: String,
    #[serde(default = "default_ontology_security_service_url")]
    pub ontology_security_service_url: String,
    #[serde(default = "default_ontology_url")]
    pub ontology_service_url: String,
    #[serde(default = "default_workflow_url")]
    pub workflow_service_url: String,
    #[serde(default = "default_approvals_service_url")]
    pub approvals_service_url: String,
    #[serde(default = "default_notebook_service_url")]
    pub notebook_service_url: String,
    #[serde(default = "default_notification_url")]
    pub notification_service_url: String,
    #[serde(default = "default_app_builder_url")]
    pub app_builder_service_url: String,
    #[serde(default = "default_application_curation_service_url")]
    pub application_curation_service_url: String,
    #[serde(default = "default_widget_registry_service_url")]
    pub widget_registry_service_url: String,
    #[serde(default = "default_ml_service_url")]
    pub ml_service_url: String,
    #[serde(default = "default_ml_experiments_service_url")]
    pub ml_experiments_service_url: String,
    #[serde(default = "default_model_catalog_service_url")]
    pub model_catalog_service_url: String,
    #[serde(default = "default_model_deployment_service_url")]
    pub model_deployment_service_url: String,
    #[serde(default = "default_model_evaluation_service_url")]
    pub model_evaluation_service_url: String,
    #[serde(default = "default_model_serving_service_url")]
    pub model_serving_service_url: String,
    #[serde(default = "default_model_inference_history_service_url")]
    pub model_inference_history_service_url: String,
    #[serde(default = "default_ai_service_url")]
    pub ai_service_url: String,
    #[serde(default = "default_llm_catalog_service_url")]
    pub llm_catalog_service_url: String,
    #[serde(default = "default_prompt_workflow_service_url")]
    pub prompt_workflow_service_url: String,
    #[serde(default = "default_knowledge_index_service_url")]
    pub knowledge_index_service_url: String,
    #[serde(default = "default_retrieval_context_service_url")]
    pub retrieval_context_service_url: String,
    #[serde(default = "default_conversation_state_service_url")]
    pub conversation_state_service_url: String,
    #[serde(default = "default_tool_registry_service_url")]
    pub tool_registry_service_url: String,
    #[serde(default = "default_ai_evaluation_service_url")]
    pub ai_evaluation_service_url: String,
    #[serde(default = "default_document_reporting_service_url")]
    pub document_reporting_service_url: String,
    #[serde(default = "default_entity_resolution_service_url")]
    pub entity_resolution_service_url: String,
    #[serde(default = "default_event_streaming_service_url")]
    pub event_streaming_service_url: String,
    #[serde(default = "default_cdc_metadata_service_url")]
    pub cdc_metadata_service_url: String,
    #[serde(default = "default_sql_warehousing_service_url")]
    pub sql_warehousing_service_url: String,
    #[serde(default = "default_time_series_data_service_url")]
    pub time_series_data_service_url: String,
    #[serde(default = "default_report_service_url")]
    pub report_service_url: String,
    #[serde(default = "default_geospatial_intelligence_service_url")]
    pub geospatial_intelligence_service_url: String,
    #[serde(default = "default_global_branch_service_url")]
    pub global_branch_service_url: String,
    #[serde(default = "default_marketplace_catalog_service_url")]
    pub marketplace_catalog_service_url: String,
    #[serde(default = "default_product_distribution_service_url")]
    pub product_distribution_service_url: String,
    #[serde(default = "default_federation_product_exchange_service_url")]
    pub federation_product_exchange_service_url: String,
    #[serde(default = "default_checkpoints_purpose_service_url")]
    pub checkpoints_purpose_service_url: String,
    #[serde(default = "default_network_boundary_service_url")]
    pub network_boundary_service_url: String,
    #[serde(default = "default_retention_policy_service_url")]
    pub retention_policy_service_url: String,
    #[serde(default = "default_lineage_deletion_service_url")]
    pub lineage_deletion_service_url: String,
    #[serde(default = "default_audit_compliance_service_url")]
    pub audit_compliance_service_url: String,
    #[serde(default = "default_audit_service_url")]
    pub audit_service_url: String,
    #[serde(default = "default_sds_service_url")]
    pub sds_service_url: String,
    #[serde(default = "default_nexus_service_url")]
    pub nexus_service_url: String,
    #[serde(default = "default_model_adapter_service_url")]
    pub model_adapter_service_url: String,
    #[serde(default = "default_model_lifecycle_service_url")]
    pub model_lifecycle_service_url: String,
    #[serde(default = "default_agent_runtime_service_url")]
    pub agent_runtime_service_url: String,
    #[serde(default = "default_document_intelligence_service_url")]
    pub document_intelligence_service_url: String,
    #[serde(default = "default_ai_application_generation_service_url")]
    pub ai_application_generation_service_url: String,
    #[serde(default = "default_tabular_analysis_service_url")]
    pub tabular_analysis_service_url: String,
    #[serde(default = "default_ontology_exploratory_analysis_service_url")]
    pub ontology_exploratory_analysis_service_url: String,
    #[serde(default = "default_ontology_timeseries_analytics_service_url")]
    pub ontology_timeseries_analytics_service_url: String,
    #[serde(default = "default_sql_bi_gateway_service_url")]
    pub sql_bi_gateway_service_url: String,
    #[serde(default = "default_notebook_runtime_service_url")]
    pub notebook_runtime_service_url: String,
    #[serde(default = "default_spreadsheet_computation_service_url")]
    pub spreadsheet_computation_service_url: String,
    #[serde(default = "default_analytical_logic_service_url")]
    pub analytical_logic_service_url: String,
    #[serde(default = "default_workflow_automation_service_url")]
    pub workflow_automation_service_url: String,
    #[serde(default = "default_automation_operations_service_url")]
    pub automation_operations_service_url: String,
    #[serde(default = "default_workflow_trace_service_url")]
    pub workflow_trace_service_url: String,
    #[serde(default = "default_application_composition_service_url")]
    pub application_composition_service_url: String,
    #[serde(default = "default_scenario_simulation_service_url")]
    pub scenario_simulation_service_url: String,
    #[serde(default = "default_solution_design_service_url")]
    pub solution_design_service_url: String,
    #[serde(default = "default_developer_console_service_url")]
    pub developer_console_service_url: String,
    #[serde(default = "default_sdk_generation_service_url")]
    pub sdk_generation_service_url: String,
    #[serde(default = "default_managed_workspace_service_url")]
    pub managed_workspace_service_url: String,
    #[serde(default = "default_custom_endpoints_service_url")]
    pub custom_endpoints_service_url: String,
    #[serde(default = "default_mcp_orchestration_service_url")]
    pub mcp_orchestration_service_url: String,
    #[serde(default = "default_compute_modules_control_plane_service_url")]
    pub compute_modules_control_plane_service_url: String,
    #[serde(default = "default_compute_modules_runtime_service_url")]
    pub compute_modules_runtime_service_url: String,
    #[serde(default = "default_monitoring_rules_service_url")]
    pub monitoring_rules_service_url: String,
    #[serde(default = "default_health_check_service_url")]
    pub health_check_service_url: String,
    #[serde(default = "default_execution_observability_service_url")]
    pub execution_observability_service_url: String,
    #[serde(default = "default_telemetry_governance_service_url")]
    pub telemetry_governance_service_url: String,
    #[serde(default = "default_code_security_scanning_service_url")]
    pub code_security_scanning_service_url: String,
    #[serde(default = "default_code_repository_review_service_url")]
    pub code_repository_review_service_url: String,
    #[serde(default)]
    pub rate_limit: RateLimitConfig,
}

fn default_host() -> String {
    "0.0.0.0".to_string()
}
fn default_port() -> u16 {
    8080
}
fn default_auth_url() -> String {
    "http://localhost:50051".to_string()
}
fn default_identity_federation_service_url() -> String {
    "http://localhost:50112".to_string()
}
fn default_oauth_integration_service_url() -> String {
    "http://localhost:50094".to_string()
}
fn default_session_governance_service_url() -> String {
    "http://localhost:50074".to_string()
}
fn default_authorization_policy_service_url() -> String {
    "http://localhost:50093".to_string()
}
fn default_security_governance_service_url() -> String {
    "http://localhost:50114".to_string()
}
fn default_tenancy_organizations_service_url() -> String {
    "http://localhost:50113".to_string()
}
fn default_cipher_service_url() -> String {
    "http://localhost:50073".to_string()
}
fn default_data_connector_url() -> String {
    "http://localhost:50052".to_string()
}
fn default_connector_management_service_url() -> String {
    "http://localhost:50088".to_string()
}
fn default_virtual_table_service_url() -> String {
    "http://localhost:50089".to_string()
}
fn default_ingestion_replication_service_url() -> String {
    "http://localhost:50090".to_string()
}
fn default_dataset_versioning_service_url() -> String {
    "http://localhost:50078".to_string()
}
fn default_data_asset_catalog_service_url() -> String {
    "http://localhost:50079".to_string()
}
fn default_dataset_quality_service_url() -> String {
    "http://localhost:50072".to_string()
}
fn default_query_url() -> String {
    "http://localhost:50055".to_string()
}
fn default_pipeline_authoring_service_url() -> String {
    "http://localhost:50080".to_string()
}
fn default_pipeline_build_service_url() -> String {
    "http://localhost:50081".to_string()
}
fn default_pipeline_schedule_service_url() -> String {
    "http://localhost:50082".to_string()
}
fn default_lineage_service_url() -> String {
    "http://localhost:50083".to_string()
}
fn default_ontology_definition_service_url() -> String {
    "http://localhost:50103".to_string()
}
fn default_object_database_service_url() -> String {
    "http://localhost:50104".to_string()
}
fn default_ontology_query_service_url() -> String {
    "http://localhost:50105".to_string()
}
fn default_ontology_actions_service_url() -> String {
    "http://localhost:50106".to_string()
}
fn default_ontology_funnel_service_url() -> String {
    "http://localhost:50107".to_string()
}
fn default_ontology_functions_service_url() -> String {
    "http://localhost:50108".to_string()
}
fn default_ontology_security_service_url() -> String {
    "http://localhost:50109".to_string()
}
fn default_ontology_url() -> String {
    "http://localhost:50057".to_string()
}
fn default_workflow_url() -> String {
    "http://localhost:50061".to_string()
}
fn default_approvals_service_url() -> String {
    "http://localhost:50071".to_string()
}
fn default_notebook_service_url() -> String {
    "http://localhost:50062".to_string()
}
fn default_notification_url() -> String {
    "http://localhost:50114".to_string()
}
fn default_app_builder_url() -> String {
    "http://localhost:50063".to_string()
}
fn default_application_curation_service_url() -> String {
    "http://localhost:50101".to_string()
}
fn default_widget_registry_service_url() -> String {
    "http://localhost:50077".to_string()
}
fn default_ml_service_url() -> String {
    "http://localhost:50059".to_string()
}
fn default_ml_experiments_service_url() -> String {
    "http://localhost:50084".to_string()
}
fn default_model_catalog_service_url() -> String {
    "http://localhost:50085".to_string()
}
fn default_model_deployment_service_url() -> String {
    "http://localhost:50086".to_string()
}
fn default_model_evaluation_service_url() -> String {
    "http://localhost:50091".to_string()
}
fn default_model_serving_service_url() -> String {
    "http://localhost:50087".to_string()
}
fn default_model_inference_history_service_url() -> String {
    "http://localhost:50092".to_string()
}
fn default_ai_service_url() -> String {
    "http://localhost:50060".to_string()
}
fn default_llm_catalog_service_url() -> String {
    "http://localhost:50095".to_string()
}
fn default_prompt_workflow_service_url() -> String {
    "http://localhost:50096".to_string()
}
fn default_knowledge_index_service_url() -> String {
    "http://localhost:50097".to_string()
}
fn default_retrieval_context_service_url() -> String {
    "http://localhost:50098".to_string()
}
fn default_conversation_state_service_url() -> String {
    "http://localhost:50099".to_string()
}
fn default_tool_registry_service_url() -> String {
    "http://localhost:50100".to_string()
}
fn default_ai_evaluation_service_url() -> String {
    "http://localhost:50075".to_string()
}
fn default_document_reporting_service_url() -> String {
    "http://localhost:50102".to_string()
}
fn default_entity_resolution_service_url() -> String {
    "http://localhost:50058".to_string()
}

fn default_event_streaming_service_url() -> String {
    "http://localhost:50121".to_string()
}

fn default_cdc_metadata_service_url() -> String {
    "http://localhost:50122".to_string()
}

fn default_sql_warehousing_service_url() -> String {
    "http://localhost:50123".to_string()
}

fn default_time_series_data_service_url() -> String {
    "http://localhost:50124".to_string()
}

fn default_report_service_url() -> String {
    "http://localhost:50064".to_string()
}

fn default_geospatial_intelligence_service_url() -> String {
    "http://localhost:50068".to_string()
}

fn default_global_branch_service_url() -> String {
    "http://localhost:50110".to_string()
}

fn default_marketplace_catalog_service_url() -> String {
    "http://localhost:50066".to_string()
}

fn default_product_distribution_service_url() -> String {
    "http://localhost:50111".to_string()
}

fn default_federation_product_exchange_service_url() -> String {
    "http://localhost:50120".to_string()
}

fn default_checkpoints_purpose_service_url() -> String {
    "http://localhost:50116".to_string()
}

fn default_network_boundary_service_url() -> String {
    "http://localhost:50119".to_string()
}

fn default_retention_policy_service_url() -> String {
    "http://localhost:50117".to_string()
}

fn default_lineage_deletion_service_url() -> String {
    "http://localhost:50118".to_string()
}

fn default_audit_compliance_service_url() -> String {
    "http://localhost:50115".to_string()
}

fn default_audit_service_url() -> String {
    "http://localhost:50070".to_string()
}

fn default_sds_service_url() -> String {
    "http://localhost:50076".to_string()
}

fn default_nexus_service_url() -> String {
    "http://localhost:50067".to_string()
}

fn default_model_adapter_service_url() -> String {
    "http://localhost:50125".to_string()
}
fn default_model_lifecycle_service_url() -> String {
    "http://localhost:50126".to_string()
}
fn default_agent_runtime_service_url() -> String {
    "http://localhost:50127".to_string()
}
fn default_document_intelligence_service_url() -> String {
    "http://localhost:50128".to_string()
}
fn default_ai_application_generation_service_url() -> String {
    "http://localhost:50129".to_string()
}
fn default_tabular_analysis_service_url() -> String {
    "http://localhost:50130".to_string()
}
fn default_ontology_exploratory_analysis_service_url() -> String {
    "http://localhost:50131".to_string()
}
fn default_ontology_timeseries_analytics_service_url() -> String {
    "http://localhost:50132".to_string()
}
fn default_sql_bi_gateway_service_url() -> String {
    "http://localhost:50133".to_string()
}
fn default_notebook_runtime_service_url() -> String {
    "http://localhost:50134".to_string()
}
fn default_spreadsheet_computation_service_url() -> String {
    "http://localhost:50135".to_string()
}
fn default_analytical_logic_service_url() -> String {
    "http://localhost:50136".to_string()
}
fn default_workflow_automation_service_url() -> String {
    "http://localhost:50137".to_string()
}
fn default_automation_operations_service_url() -> String {
    "http://localhost:50138".to_string()
}
fn default_workflow_trace_service_url() -> String {
    "http://localhost:50139".to_string()
}
fn default_application_composition_service_url() -> String {
    "http://localhost:50140".to_string()
}
fn default_scenario_simulation_service_url() -> String {
    "http://localhost:50141".to_string()
}
fn default_solution_design_service_url() -> String {
    "http://localhost:50142".to_string()
}
fn default_developer_console_service_url() -> String {
    "http://localhost:50143".to_string()
}
fn default_sdk_generation_service_url() -> String {
    "http://localhost:50144".to_string()
}
fn default_managed_workspace_service_url() -> String {
    "http://localhost:50145".to_string()
}
fn default_custom_endpoints_service_url() -> String {
    "http://localhost:50146".to_string()
}
fn default_mcp_orchestration_service_url() -> String {
    "http://localhost:50147".to_string()
}
fn default_compute_modules_control_plane_service_url() -> String {
    "http://localhost:50148".to_string()
}
fn default_compute_modules_runtime_service_url() -> String {
    "http://localhost:50149".to_string()
}
fn default_monitoring_rules_service_url() -> String {
    "http://localhost:50150".to_string()
}
fn default_health_check_service_url() -> String {
    "http://localhost:50151".to_string()
}
fn default_execution_observability_service_url() -> String {
    "http://localhost:50152".to_string()
}
fn default_telemetry_governance_service_url() -> String {
    "http://localhost:50153".to_string()
}
fn default_code_security_scanning_service_url() -> String {
    "http://localhost:50154".to_string()
}
fn default_code_repository_review_service_url() -> String {
    "http://localhost:50155".to_string()
}

impl GatewayConfig {
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
