//! S3.1.e — SCIM 2.0 provisioning contract.
//!
//! Runtime handlers live in `handlers::scim`; this module owns the
//! RFC 7643/7644 wire shapes, schema URIs and metadata resources so
//! tests and OpenAPI generation have one source of truth.

use serde::{Deserialize, Serialize};
use serde_json::{Map, Value};

pub const ROUTE_USERS: &str = "/scim/v2/Users";
pub const ROUTE_USER_BY_ID: &str = "/scim/v2/Users/{id}";
pub const ROUTE_GROUPS: &str = "/scim/v2/Groups";
pub const ROUTE_GROUP_BY_ID: &str = "/scim/v2/Groups/{id}";
pub const ROUTE_SERVICE_PROVIDER_CONFIG: &str = "/scim/v2/ServiceProviderConfig";
pub const ROUTE_SCHEMAS: &str = "/scim/v2/Schemas";
pub const ROUTE_SCHEMA_BY_ID: &str = "/scim/v2/Schemas/{id}";
pub const ROUTE_RESOURCE_TYPES: &str = "/scim/v2/ResourceTypes";
pub const ROUTE_RESOURCE_TYPE_BY_ID: &str = "/scim/v2/ResourceTypes/{id}";

pub const SCHEMA_USER: &str = "urn:ietf:params:scim:schemas:core:2.0:User";
pub const SCHEMA_GROUP: &str = "urn:ietf:params:scim:schemas:core:2.0:Group";
pub const SCHEMA_LIST_RESPONSE: &str = "urn:ietf:params:scim:api:messages:2.0:ListResponse";
pub const SCHEMA_PATCH_OP: &str = "urn:ietf:params:scim:api:messages:2.0:PatchOp";
pub const SCHEMA_ERROR: &str = "urn:ietf:params:scim:api:messages:2.0:Error";
pub const SCHEMA_SERVICE_PROVIDER_CONFIG: &str =
    "urn:ietf:params:scim:schemas:core:2.0:ServiceProviderConfig";
pub const SCHEMA_SCHEMA: &str = "urn:ietf:params:scim:schemas:core:2.0:Schema";
pub const SCHEMA_RESOURCE_TYPE: &str = "urn:ietf:params:scim:schemas:core:2.0:ResourceType";
pub const SCHEMA_OPENFOUNDRY_USER_EXTENSION: &str =
    "urn:openfoundry:params:scim:schemas:extension:2.0:User";

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct ScimMeta {
    #[serde(rename = "resourceType")]
    pub resource_type: String,
    pub location: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct ScimUser {
    pub schemas: Vec<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub id: Option<String>,
    #[serde(rename = "userName")]
    pub user_name: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub name: Option<ScimName>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub emails: Option<Vec<ScimEmail>>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub active: Option<bool>,
    #[serde(rename = "externalId", skip_serializing_if = "Option::is_none")]
    pub external_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub meta: Option<ScimMeta>,
    #[serde(flatten, default, skip_serializing_if = "Map::is_empty")]
    pub extensions: Map<String, Value>,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct ScimName {
    #[serde(rename = "givenName", skip_serializing_if = "Option::is_none")]
    pub given_name: Option<String>,
    #[serde(rename = "familyName", skip_serializing_if = "Option::is_none")]
    pub family_name: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub formatted: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct ScimEmail {
    pub value: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub primary: Option<bool>,
    #[serde(rename = "type", skip_serializing_if = "Option::is_none")]
    pub type_: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct ScimGroup {
    pub schemas: Vec<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub id: Option<String>,
    #[serde(rename = "displayName")]
    pub display_name: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub members: Option<Vec<ScimGroupMember>>,
    #[serde(rename = "externalId", skip_serializing_if = "Option::is_none")]
    pub external_id: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub meta: Option<ScimMeta>,
    #[serde(flatten, default, skip_serializing_if = "Map::is_empty")]
    pub extensions: Map<String, Value>,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct ScimGroupMember {
    pub value: String,
    #[serde(rename = "$ref", skip_serializing_if = "Option::is_none")]
    pub ref_: Option<String>,
    #[serde(rename = "type", skip_serializing_if = "Option::is_none")]
    pub type_: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub display: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct ScimListResponse<T> {
    pub schemas: Vec<String>,
    #[serde(rename = "totalResults")]
    pub total_results: usize,
    #[serde(rename = "startIndex")]
    pub start_index: usize,
    #[serde(rename = "itemsPerPage")]
    pub items_per_page: usize,
    #[serde(rename = "Resources")]
    pub resources: Vec<T>,
}

impl<T> ScimListResponse<T> {
    pub fn new(resources: Vec<T>, total_results: usize, start_index: usize) -> Self {
        let items_per_page = resources.len();
        Self {
            schemas: vec![SCHEMA_LIST_RESPONSE.into()],
            total_results,
            start_index,
            items_per_page,
            resources,
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ScimPatchRequest {
    pub schemas: Vec<String>,
    #[serde(rename = "Operations")]
    pub operations: Vec<ScimPatchOperation>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ScimPatchOperation {
    pub op: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub path: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub value: Option<Value>,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct ScimError {
    pub schemas: Vec<String>,
    pub status: String,
    pub detail: String,
    #[serde(rename = "scimType", skip_serializing_if = "Option::is_none")]
    pub scim_type: Option<String>,
}

impl ScimError {
    pub fn new(status: u16, detail: impl Into<String>, scim_type: Option<&str>) -> Self {
        Self {
            schemas: vec![SCHEMA_ERROR.into()],
            status: status.to_string(),
            detail: detail.into(),
            scim_type: scim_type.map(ToString::to_string),
        }
    }

    pub fn unsupported(detail: impl Into<String>) -> Self {
        Self::new(400, detail, Some("mutability"))
    }
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct ServiceProviderConfig {
    pub schemas: Vec<String>,
    pub documentation_uri: String,
    pub patch: Feature,
    pub bulk: Feature,
    pub filter: FilterFeature,
    pub change_password: Feature,
    pub sort: Feature,
    pub etag: Feature,
    pub authentication_schemes: Vec<AuthenticationScheme>,
    pub meta: ScimMeta,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct Feature {
    pub supported: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct FilterFeature {
    pub supported: bool,
    #[serde(rename = "maxResults")]
    pub max_results: usize,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct AuthenticationScheme {
    pub name: String,
    pub description: String,
    #[serde(rename = "type")]
    pub type_: String,
    pub primary: bool,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq)]
pub struct ScimSchemaResource {
    pub schemas: Vec<String>,
    pub id: String,
    pub name: String,
    pub description: String,
    pub attributes: Vec<ScimSchemaAttribute>,
    pub meta: ScimMeta,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct ScimSchemaAttribute {
    pub name: String,
    #[serde(rename = "type")]
    pub type_: String,
    #[serde(rename = "multiValued")]
    pub multi_valued: bool,
    pub required: bool,
    pub mutability: String,
    #[serde(rename = "returned")]
    pub returned_: String,
    pub uniqueness: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct ScimResourceType {
    pub schemas: Vec<String>,
    pub id: String,
    pub name: String,
    pub endpoint: String,
    #[serde(rename = "schema")]
    pub schema_: String,
    pub meta: ScimMeta,
}

pub fn service_provider_config(base_url: &str) -> ServiceProviderConfig {
    let base = base_url.trim_end_matches('/');
    ServiceProviderConfig {
        schemas: vec![SCHEMA_SERVICE_PROVIDER_CONFIG.into()],
        documentation_uri: "https://www.rfc-editor.org/rfc/rfc7644".into(),
        patch: Feature { supported: true },
        bulk: Feature { supported: false },
        filter: FilterFeature {
            supported: true,
            max_results: 500,
        },
        change_password: Feature { supported: false },
        sort: Feature { supported: false },
        etag: Feature { supported: false },
        authentication_schemes: vec![AuthenticationScheme {
            name: "OAuth Bearer Token".into(),
            description: "JWT bearer token issued by OpenFoundry identity-federation-service"
                .into(),
            type_: "oauthbearertoken".into(),
            primary: true,
        }],
        meta: ScimMeta {
            resource_type: "ServiceProviderConfig".into(),
            location: format!("{base}{ROUTE_SERVICE_PROVIDER_CONFIG}"),
        },
    }
}

pub fn schema_resources(base_url: &str) -> Vec<ScimSchemaResource> {
    vec![user_schema(base_url), group_schema(base_url)]
}

pub fn resource_types(base_url: &str) -> Vec<ScimResourceType> {
    let base = base_url.trim_end_matches('/');
    vec![
        ScimResourceType {
            schemas: vec![SCHEMA_RESOURCE_TYPE.into()],
            id: "User".into(),
            name: "User".into(),
            endpoint: ROUTE_USERS.into(),
            schema_: SCHEMA_USER.into(),
            meta: ScimMeta {
                resource_type: "ResourceType".into(),
                location: format!("{base}{ROUTE_RESOURCE_TYPES}/User"),
            },
        },
        ScimResourceType {
            schemas: vec![SCHEMA_RESOURCE_TYPE.into()],
            id: "Group".into(),
            name: "Group".into(),
            endpoint: ROUTE_GROUPS.into(),
            schema_: SCHEMA_GROUP.into(),
            meta: ScimMeta {
                resource_type: "ResourceType".into(),
                location: format!("{base}{ROUTE_RESOURCE_TYPES}/Group"),
            },
        },
    ]
}

fn user_schema(base_url: &str) -> ScimSchemaResource {
    let base = base_url.trim_end_matches('/');
    ScimSchemaResource {
        schemas: vec![SCHEMA_SCHEMA.into()],
        id: SCHEMA_USER.into(),
        name: "User".into(),
        description: "OpenFoundry SCIM user resource".into(),
        attributes: vec![
            attr(
                "userName",
                "string",
                false,
                true,
                "readWrite",
                "default",
                "server",
            ),
            attr(
                "name",
                "complex",
                false,
                false,
                "readWrite",
                "default",
                "none",
            ),
            attr(
                "emails",
                "complex",
                true,
                false,
                "readWrite",
                "default",
                "none",
            ),
            attr(
                "active",
                "boolean",
                false,
                false,
                "readWrite",
                "default",
                "none",
            ),
            attr(
                "externalId",
                "string",
                false,
                false,
                "readWrite",
                "default",
                "none",
            ),
            attr(
                SCHEMA_OPENFOUNDRY_USER_EXTENSION,
                "complex",
                false,
                false,
                "readWrite",
                "default",
                "none",
            ),
        ],
        meta: ScimMeta {
            resource_type: "Schema".into(),
            location: format!("{base}{ROUTE_SCHEMAS}/{SCHEMA_USER}"),
        },
    }
}

fn group_schema(base_url: &str) -> ScimSchemaResource {
    let base = base_url.trim_end_matches('/');
    ScimSchemaResource {
        schemas: vec![SCHEMA_SCHEMA.into()],
        id: SCHEMA_GROUP.into(),
        name: "Group".into(),
        description: "OpenFoundry SCIM group resource".into(),
        attributes: vec![
            attr(
                "displayName",
                "string",
                false,
                true,
                "readWrite",
                "default",
                "server",
            ),
            attr(
                "members",
                "complex",
                true,
                false,
                "readWrite",
                "default",
                "none",
            ),
            attr(
                "externalId",
                "string",
                false,
                false,
                "readWrite",
                "default",
                "none",
            ),
        ],
        meta: ScimMeta {
            resource_type: "Schema".into(),
            location: format!("{base}{ROUTE_SCHEMAS}/{SCHEMA_GROUP}"),
        },
    }
}

fn attr(
    name: &str,
    type_: &str,
    multi_valued: bool,
    required: bool,
    mutability: &str,
    returned_: &str,
    uniqueness: &str,
) -> ScimSchemaAttribute {
    ScimSchemaAttribute {
        name: name.into(),
        type_: type_.into(),
        multi_valued,
        required,
        mutability: mutability.into(),
        returned_: returned_.into(),
        uniqueness: uniqueness.into(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn user_round_trip_uses_scim_field_names() {
        let u = ScimUser {
            schemas: vec![SCHEMA_USER.into()],
            id: None,
            user_name: "alice@acme.com".into(),
            name: Some(ScimName {
                given_name: Some("Alice".into()),
                family_name: Some("Doe".into()),
                formatted: None,
            }),
            emails: Some(vec![ScimEmail {
                value: "alice@acme.com".into(),
                primary: Some(true),
                type_: Some("work".into()),
            }]),
            active: Some(true),
            external_id: None,
            meta: None,
            extensions: Map::new(),
        };
        let json = serde_json::to_value(&u).unwrap();
        assert_eq!(json["userName"], "alice@acme.com");
        assert_eq!(json["name"]["givenName"], "Alice");
        assert_eq!(json["emails"][0]["type"], "work");
        let back: ScimUser = serde_json::from_value(json).unwrap();
        assert_eq!(back.user_name, "alice@acme.com");
    }

    #[test]
    fn error_contract_includes_scim_type_when_relevant() {
        let error = ScimError::new(400, "unsupported filter", Some("invalidFilter"));
        let json = serde_json::to_value(error).unwrap();
        assert_eq!(json["schemas"][0], SCHEMA_ERROR);
        assert_eq!(json["status"], "400");
        assert_eq!(json["scimType"], "invalidFilter");
    }

    #[test]
    fn metadata_contracts_advertise_supported_resources() {
        let config = service_provider_config("https://id.example.test");
        assert!(config.patch.supported);
        assert!(config.filter.supported);
        assert!(!config.bulk.supported);

        let schemas = schema_resources("https://id.example.test");
        assert!(schemas.iter().any(|schema| schema.id == SCHEMA_USER));
        assert!(schemas.iter().any(|schema| schema.id == SCHEMA_GROUP));

        let resource_types = resource_types("https://id.example.test");
        assert_eq!(resource_types[0].endpoint, ROUTE_USERS);
        assert_eq!(resource_types[1].endpoint, ROUTE_GROUPS);
    }

    #[test]
    fn list_response_uses_resources_key() {
        let response = ScimListResponse::new(
            vec![ScimResourceType {
                schemas: vec![SCHEMA_RESOURCE_TYPE.into()],
                id: "User".into(),
                name: "User".into(),
                endpoint: ROUTE_USERS.into(),
                schema_: SCHEMA_USER.into(),
                meta: ScimMeta {
                    resource_type: "ResourceType".into(),
                    location: "/scim/v2/ResourceTypes/User".into(),
                },
            }],
            1,
            1,
        );
        let json = serde_json::to_value(response).unwrap();
        assert_eq!(json["schemas"][0], SCHEMA_LIST_RESPONSE);
        assert_eq!(json["Resources"][0]["schema"], SCHEMA_USER);
    }
}
