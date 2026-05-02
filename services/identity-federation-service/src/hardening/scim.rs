//! S3.1.e — SCIM 2.0 provisioning DTOs.
//!
//! Endpoints are mounted by the bin. This module owns the request/
//! response shapes and the route constants so OpenAPI generation +
//! tests can reference them from a single source of truth.

use serde::{Deserialize, Serialize};

pub const ROUTE_USERS: &str = "/scim/v2/Users";
pub const ROUTE_USER_BY_ID: &str = "/scim/v2/Users/{id}";
pub const ROUTE_GROUPS: &str = "/scim/v2/Groups";
pub const ROUTE_GROUP_BY_ID: &str = "/scim/v2/Groups/{id}";

/// SCIM 2.0 standard schema URI for a user.
pub const SCHEMA_USER: &str = "urn:ietf:params:scim:schemas:core:2.0:User";
/// SCIM 2.0 standard schema URI for a group.
pub const SCHEMA_GROUP: &str = "urn:ietf:params:scim:schemas:core:2.0:Group";

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ScimUser {
    pub schemas: Vec<String>,
    pub id: Option<String>,
    #[serde(rename = "userName")]
    pub user_name: String,
    pub name: Option<ScimName>,
    pub emails: Option<Vec<ScimEmail>>,
    pub active: Option<bool>,
    #[serde(rename = "externalId")]
    pub external_id: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ScimName {
    #[serde(rename = "givenName")]
    pub given_name: Option<String>,
    #[serde(rename = "familyName")]
    pub family_name: Option<String>,
    pub formatted: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ScimEmail {
    pub value: String,
    pub primary: Option<bool>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ScimGroup {
    pub schemas: Vec<String>,
    pub id: Option<String>,
    #[serde(rename = "displayName")]
    pub display_name: String,
    pub members: Option<Vec<ScimGroupMember>>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ScimGroupMember {
    pub value: String,
    #[serde(rename = "$ref")]
    pub ref_: Option<String>,
    #[serde(rename = "type")]
    pub type_: Option<String>,
}

/// SCIM 2.0 error body (RFC 7644 §3.12).
#[derive(Debug, Clone, Serialize)]
pub struct ScimError {
    pub schemas: Vec<String>,
    pub status: String,
    pub detail: String,
}

impl ScimError {
    pub fn not_implemented() -> Self {
        Self {
            schemas: vec!["urn:ietf:params:scim:api:messages:2.0:Error".into()],
            status: "501".into(),
            detail: "SCIM endpoint not yet wired (S3.1.e follow-up)".into(),
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn user_round_trip() {
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
            }]),
            active: Some(true),
            external_id: None,
        };
        let json = serde_json::to_string(&u).unwrap();
        let back: ScimUser = serde_json::from_str(&json).unwrap();
        assert_eq!(back.user_name, "alice@acme.com");
    }
}
