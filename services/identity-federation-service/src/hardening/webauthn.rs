//! S3.1.d — WebAuthn second factor.
//!
//! This module implements the server-side WebAuthn ceremonies used by
//! identity-federation-service. It verifies browser client data,
//! RP-ID binding, authenticator flags, ES256 signatures and counter
//! advancement; private authenticator material never leaves the
//! user's device.

use std::collections::{BTreeMap, HashMap};
use std::sync::{Arc, Mutex};

use async_trait::async_trait;
use base64::Engine;
use base64::engine::general_purpose::{URL_SAFE, URL_SAFE_NO_PAD};
use cassandra_kernel::Migration;
use cassandra_kernel::scylla::{Session, frame::value::CqlTimestamp};
use chrono::{DateTime, Duration, Utc};
use p256::ecdsa::signature::Verifier;
use p256::ecdsa::{Signature, VerifyingKey};
use rand::RngCore;
use serde::{Deserialize, Serialize};
use serde_json::{Value, json};
use sha2::{Digest, Sha256};
use uuid::Uuid;

use crate::sessions_cassandra::KEYSPACE;

const CHALLENGE_TTL_SECS: i64 = 300;
const CHALLENGE_TTL_CQL_SECS: i32 = 600;
const FLAG_UP: u8 = 0x01;
const FLAG_UV: u8 = 0x04;
const FLAG_AT: u8 = 0x40;

#[derive(Debug, Clone)]
pub struct RelyingPartyConfig {
    pub rp_id: String,
    pub rp_origin: String,
    pub rp_name: String,
}

impl RelyingPartyConfig {
    pub fn from_env() -> Self {
        Self {
            rp_id: std::env::var("OF_WEBAUTHN_RP_ID")
                .unwrap_or_else(|_| "openfoundry.local".into()),
            rp_origin: std::env::var("OF_WEBAUTHN_RP_ORIGIN")
                .unwrap_or_else(|_| "https://openfoundry.local".into()),
            rp_name: std::env::var("OF_WEBAUTHN_RP_NAME").unwrap_or_else(|_| "OpenFoundry".into()),
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub enum WebAuthnChallengeKind {
    Register,
    Login,
}

impl WebAuthnChallengeKind {
    fn as_str(&self) -> &'static str {
        match self {
            Self::Register => "register",
            Self::Login => "login",
        }
    }

    fn parse(value: &str) -> Result<Self, WebAuthnError> {
        match value {
            "register" => Ok(Self::Register),
            "login" => Ok(Self::Login),
            other => Err(WebAuthnError::Store(format!(
                "unknown WebAuthn challenge kind {other}"
            ))),
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct WebAuthnCredentialRecord {
    pub id: Uuid,
    pub user_id: Uuid,
    /// Base64-url credential ID.
    pub credential_id: String,
    /// Uncompressed SEC1 P-256 public key (`0x04 || x || y`), base64-url.
    pub public_key: String,
    pub sign_count: u32,
    pub aaguid: String,
    pub user_verified: bool,
    pub created_at: DateTime<Utc>,
    pub last_used_at: Option<DateTime<Utc>>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct WebAuthnChallengeRecord {
    pub id: Uuid,
    pub user_id: Uuid,
    pub kind: WebAuthnChallengeKind,
    pub challenge: String,
    pub expires_at: DateTime<Utc>,
    pub used_at: Option<DateTime<Utc>>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RegisterChallengeResponse {
    pub challenge_id: Uuid,
    pub public_key: Value,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LoginChallengeResponse {
    pub challenge_id: Uuid,
    pub public_key: Value,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RegisterFinishRequest {
    pub challenge_id: Uuid,
    pub credential_id: String,
    pub client_data_json: String,
    pub attestation_object: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LoginFinishRequest {
    pub challenge_id: Uuid,
    pub credential_id: String,
    pub client_data_json: String,
    pub authenticator_data: String,
    pub signature: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RegisterCredentialResponse {
    pub credential_id: String,
    pub aaguid: String,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct LoginCredentialResponse {
    pub credential_id: String,
    pub sign_count: u32,
    pub user_verified: bool,
}

#[derive(Debug, thiserror::Error)]
pub enum WebAuthnError {
    #[error("webauthn configuration error: {0}")]
    Config(String),
    #[error("webauthn challenge error: {0}")]
    Challenge(String),
    #[error("webauthn verification failed: {0}")]
    Verify(String),
    #[error("webauthn store error: {0}")]
    Store(String),
}

#[async_trait]
pub trait WebAuthnStore: Send + Sync {
    async fn ensure_schema(&self) -> Result<(), WebAuthnError>;
    async fn save_challenge(&self, challenge: WebAuthnChallengeRecord)
    -> Result<(), WebAuthnError>;
    async fn consume_challenge(
        &self,
        challenge_id: Uuid,
        kind: WebAuthnChallengeKind,
        now: DateTime<Utc>,
    ) -> Result<WebAuthnChallengeRecord, WebAuthnError>;
    async fn save_credential(
        &self,
        credential: WebAuthnCredentialRecord,
    ) -> Result<(), WebAuthnError>;
    async fn credentials_for_user(
        &self,
        user_id: Uuid,
    ) -> Result<Vec<WebAuthnCredentialRecord>, WebAuthnError>;
    async fn credential_by_id(
        &self,
        credential_id: &str,
    ) -> Result<Option<WebAuthnCredentialRecord>, WebAuthnError>;
    async fn update_login_counter(
        &self,
        credential_id: &str,
        sign_count: u32,
        user_verified: bool,
    ) -> Result<(), WebAuthnError>;
}

#[derive(Debug, Clone)]
pub struct CassandraWebAuthnStore {
    session: Arc<Session>,
}

impl CassandraWebAuthnStore {
    pub fn new(session: Arc<Session>) -> Self {
        Self { session }
    }
}

fn cql_ts(dt: DateTime<Utc>) -> CqlTimestamp {
    CqlTimestamp(dt.timestamp_millis())
}

fn cql_ts_to_dt(ts: CqlTimestamp) -> DateTime<Utc> {
    DateTime::<Utc>::from_timestamp_millis(ts.0).unwrap_or_else(Utc::now)
}

fn store_error(error: impl std::fmt::Display) -> WebAuthnError {
    WebAuthnError::Store(error.to_string())
}

fn parse_uuid(value: &str) -> Result<Uuid, WebAuthnError> {
    Uuid::parse_str(value)
        .map_err(|error| WebAuthnError::Store(format!("invalid uuid in Cassandra row: {error}")))
}

fn sign_count_from_i64(value: i64) -> Result<u32, WebAuthnError> {
    value
        .try_into()
        .map_err(|_| WebAuthnError::Store("credential sign_count must fit u32".to_string()))
}

#[async_trait]
impl WebAuthnStore for CassandraWebAuthnStore {
    async fn ensure_schema(&self) -> Result<(), WebAuthnError> {
        cassandra_kernel::migrate::apply(&self.session, KEYSPACE, WEBAUTHN_MIGRATIONS)
            .await
            .map_err(store_error)?;
        Ok(())
    }

    async fn save_challenge(
        &self,
        challenge: WebAuthnChallengeRecord,
    ) -> Result<(), WebAuthnError> {
        self.session
            .query(
                "INSERT INTO auth_runtime.webauthn_challenge \
                 (challenge_id, user_id, kind, challenge, expires_at, used_at) \
                 VALUES (?, ?, ?, ?, ?, ?) USING TTL ?",
                (
                    challenge.id,
                    challenge.user_id.to_string(),
                    challenge.kind.as_str(),
                    challenge.challenge.as_str(),
                    cql_ts(challenge.expires_at),
                    challenge.used_at.map(cql_ts),
                    CHALLENGE_TTL_CQL_SECS,
                ),
            )
            .await
            .map_err(store_error)?;
        Ok(())
    }

    async fn consume_challenge(
        &self,
        challenge_id: Uuid,
        kind: WebAuthnChallengeKind,
        now: DateTime<Utc>,
    ) -> Result<WebAuthnChallengeRecord, WebAuthnError> {
        let result = self
            .session
            .query(
                "SELECT user_id, kind, challenge, expires_at, used_at \
                 FROM auth_runtime.webauthn_challenge WHERE challenge_id = ?",
                (challenge_id,),
            )
            .await
            .map_err(store_error)?;

        let mut rows = result
            .rows_typed_or_empty::<(String, String, String, CqlTimestamp, Option<CqlTimestamp>)>();
        let Some(row) = rows.next() else {
            return Err(WebAuthnError::Challenge("challenge not found".to_string()));
        };
        let (user_id, stored_kind, challenge, expires_at, used_at) = row.map_err(store_error)?;
        let record = WebAuthnChallengeRecord {
            id: challenge_id,
            user_id: parse_uuid(&user_id)?,
            kind: WebAuthnChallengeKind::parse(&stored_kind)?,
            challenge,
            expires_at: cql_ts_to_dt(expires_at),
            used_at: used_at.map(cql_ts_to_dt),
        };
        if record.kind != kind {
            return Err(WebAuthnError::Challenge(
                "challenge kind mismatch".to_string(),
            ));
        }
        validate_challenge_record(&record, now)?;
        self.session
            .query(
                "UPDATE auth_runtime.webauthn_challenge USING TTL ? \
                 SET used_at = ? WHERE challenge_id = ?",
                (CHALLENGE_TTL_CQL_SECS, cql_ts(now), challenge_id),
            )
            .await
            .map_err(store_error)?;
        Ok(record)
    }

    async fn save_credential(
        &self,
        credential: WebAuthnCredentialRecord,
    ) -> Result<(), WebAuthnError> {
        let user_id = credential.user_id.to_string();
        let created_at = cql_ts(credential.created_at);
        let last_used_at = credential.last_used_at.map(cql_ts);
        let sign_count = i64::from(credential.sign_count);

        self.session
            .query(
                "INSERT INTO auth_runtime.webauthn_credential_by_user \
                 (user_id, created_at, credential_id, id, public_key, sign_count, aaguid, \
                  user_verified, last_used_at) \
                 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
                (
                    user_id.as_str(),
                    created_at,
                    credential.credential_id.as_str(),
                    credential.id,
                    credential.public_key.as_str(),
                    sign_count,
                    credential.aaguid.as_str(),
                    credential.user_verified,
                    last_used_at,
                ),
            )
            .await
            .map_err(store_error)?;

        self.session
            .query(
                "INSERT INTO auth_runtime.webauthn_credential_by_id \
                 (credential_id, id, user_id, public_key, sign_count, aaguid, user_verified, \
                  created_at, last_used_at) \
                 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
                (
                    credential.credential_id.as_str(),
                    credential.id,
                    user_id.as_str(),
                    credential.public_key.as_str(),
                    sign_count,
                    credential.aaguid.as_str(),
                    credential.user_verified,
                    created_at,
                    last_used_at,
                ),
            )
            .await
            .map_err(store_error)?;
        Ok(())
    }

    async fn credentials_for_user(
        &self,
        user_id: Uuid,
    ) -> Result<Vec<WebAuthnCredentialRecord>, WebAuthnError> {
        let user_id_text = user_id.to_string();
        let result = self
            .session
            .query(
                "SELECT created_at, credential_id, id, public_key, sign_count, aaguid, \
                        user_verified, last_used_at \
                 FROM auth_runtime.webauthn_credential_by_user WHERE user_id = ?",
                (user_id_text.as_str(),),
            )
            .await
            .map_err(store_error)?;

        let mut credentials = Vec::new();
        for row in result.rows_typed_or_empty::<(
            CqlTimestamp,
            String,
            Uuid,
            String,
            i64,
            String,
            bool,
            Option<CqlTimestamp>,
        )>() {
            let (
                created_at,
                credential_id,
                id,
                public_key,
                sign_count,
                aaguid,
                user_verified,
                last_used_at,
            ) = row.map_err(store_error)?;
            credentials.push(WebAuthnCredentialRecord {
                id,
                user_id,
                credential_id,
                public_key,
                sign_count: sign_count_from_i64(sign_count)?,
                aaguid,
                user_verified,
                created_at: cql_ts_to_dt(created_at),
                last_used_at: last_used_at.map(cql_ts_to_dt),
            });
        }
        Ok(credentials)
    }

    async fn credential_by_id(
        &self,
        credential_id: &str,
    ) -> Result<Option<WebAuthnCredentialRecord>, WebAuthnError> {
        let result = self
            .session
            .query(
                "SELECT id, user_id, public_key, sign_count, aaguid, user_verified, \
                        created_at, last_used_at \
                 FROM auth_runtime.webauthn_credential_by_id WHERE credential_id = ?",
                (credential_id,),
            )
            .await
            .map_err(store_error)?;

        let mut rows = result.rows_typed_or_empty::<(
            Uuid,
            String,
            String,
            i64,
            String,
            bool,
            CqlTimestamp,
            Option<CqlTimestamp>,
        )>();
        let Some(row) = rows.next() else {
            return Ok(None);
        };
        let (id, user_id, public_key, sign_count, aaguid, user_verified, created_at, last_used_at) =
            row.map_err(store_error)?;
        Ok(Some(WebAuthnCredentialRecord {
            id,
            user_id: parse_uuid(&user_id)?,
            credential_id: credential_id.to_string(),
            public_key,
            sign_count: sign_count_from_i64(sign_count)?,
            aaguid,
            user_verified,
            created_at: cql_ts_to_dt(created_at),
            last_used_at: last_used_at.map(cql_ts_to_dt),
        }))
    }

    async fn update_login_counter(
        &self,
        credential_id: &str,
        sign_count: u32,
        user_verified: bool,
    ) -> Result<(), WebAuthnError> {
        let credential = self
            .credential_by_id(credential_id)
            .await?
            .ok_or_else(|| WebAuthnError::Store("credential not found".to_string()))?;
        let now = Utc::now();
        let sign_count = i64::from(sign_count);

        self.session
            .query(
                "UPDATE auth_runtime.webauthn_credential_by_id \
                 SET sign_count = ?, user_verified = ?, last_used_at = ? \
                 WHERE credential_id = ?",
                (sign_count, user_verified, cql_ts(now), credential_id),
            )
            .await
            .map_err(store_error)?;

        self.session
            .query(
                "UPDATE auth_runtime.webauthn_credential_by_user \
                 SET sign_count = ?, user_verified = ?, last_used_at = ? \
                 WHERE user_id = ? AND created_at = ? AND credential_id = ?",
                (
                    sign_count,
                    user_verified,
                    cql_ts(now),
                    credential.user_id.to_string(),
                    cql_ts(credential.created_at),
                    credential_id,
                ),
            )
            .await
            .map_err(store_error)?;
        Ok(())
    }
}

#[derive(Clone)]
pub struct WebAuthnService {
    config: RelyingPartyConfig,
    store: Arc<dyn WebAuthnStore>,
}

impl WebAuthnService {
    pub fn new(config: RelyingPartyConfig, store: Arc<dyn WebAuthnStore>) -> Self {
        Self { config, store }
    }

    pub async fn ensure_schema(&self) -> Result<(), WebAuthnError> {
        self.store.ensure_schema().await
    }

    pub async fn has_credentials(&self, user_id: Uuid) -> Result<bool, WebAuthnError> {
        Ok(!self.store.credentials_for_user(user_id).await?.is_empty())
    }

    pub async fn register_challenge(
        &self,
        user_id: Uuid,
        user_email: &str,
        display_name: &str,
    ) -> Result<RegisterChallengeResponse, WebAuthnError> {
        let challenge = new_challenge();
        let challenge_id = Uuid::now_v7();
        self.store
            .save_challenge(WebAuthnChallengeRecord {
                id: challenge_id,
                user_id,
                kind: WebAuthnChallengeKind::Register,
                challenge: challenge.clone(),
                expires_at: Utc::now() + Duration::seconds(CHALLENGE_TTL_SECS),
                used_at: None,
            })
            .await?;

        Ok(RegisterChallengeResponse {
            challenge_id,
            public_key: json!({
                "challenge": challenge,
                "rp": { "id": self.config.rp_id, "name": self.config.rp_name },
                "user": {
                    "id": b64(user_id.as_bytes()),
                    "name": user_email,
                    "displayName": display_name
                },
                "pubKeyCredParams": [{ "type": "public-key", "alg": -7 }],
                "timeout": CHALLENGE_TTL_SECS * 1000,
                "attestation": "none",
                "authenticatorSelection": {
                    "residentKey": "preferred",
                    "userVerification": "preferred"
                }
            }),
        })
    }

    pub async fn register_finish(
        &self,
        request: RegisterFinishRequest,
    ) -> Result<RegisterCredentialResponse, WebAuthnError> {
        let now = Utc::now();
        let challenge = self
            .store
            .consume_challenge(request.challenge_id, WebAuthnChallengeKind::Register, now)
            .await?;
        let client_data_json = decode_b64(&request.client_data_json)?;
        let client = parse_client_data(&client_data_json)?;
        verify_client_data(
            &client,
            "webauthn.create",
            &challenge.challenge,
            &self.config,
        )?;

        let attestation = decode_b64(&request.attestation_object)?;
        let auth_data = parse_attestation_auth_data(&attestation)?;
        let parsed = parse_registration_auth_data(&auth_data, &self.config)?;
        if parsed.credential_id != request.credential_id {
            return Err(WebAuthnError::Verify(
                "credential_id does not match authenticator data".to_string(),
            ));
        }

        let credential = WebAuthnCredentialRecord {
            id: Uuid::now_v7(),
            user_id: challenge.user_id,
            credential_id: parsed.credential_id.clone(),
            public_key: parsed.public_key,
            sign_count: parsed.sign_count,
            aaguid: parsed.aaguid.clone(),
            user_verified: parsed.user_verified,
            created_at: now,
            last_used_at: None,
        };
        self.store.save_credential(credential).await?;
        Ok(RegisterCredentialResponse {
            credential_id: parsed.credential_id,
            aaguid: parsed.aaguid,
        })
    }

    pub async fn login_challenge(
        &self,
        user_id: Uuid,
    ) -> Result<LoginChallengeResponse, WebAuthnError> {
        let credentials = self.store.credentials_for_user(user_id).await?;
        if credentials.is_empty() {
            return Err(WebAuthnError::Challenge(
                "user has no WebAuthn credentials".to_string(),
            ));
        }

        let challenge = new_challenge();
        let challenge_id = Uuid::now_v7();
        self.store
            .save_challenge(WebAuthnChallengeRecord {
                id: challenge_id,
                user_id,
                kind: WebAuthnChallengeKind::Login,
                challenge: challenge.clone(),
                expires_at: Utc::now() + Duration::seconds(CHALLENGE_TTL_SECS),
                used_at: None,
            })
            .await?;
        let allow_credentials: Vec<Value> = credentials
            .into_iter()
            .map(|credential| {
                json!({
                    "type": "public-key",
                    "id": credential.credential_id,
                })
            })
            .collect();

        Ok(LoginChallengeResponse {
            challenge_id,
            public_key: json!({
                "challenge": challenge,
                "rpId": self.config.rp_id,
                "allowCredentials": allow_credentials,
                "timeout": CHALLENGE_TTL_SECS * 1000,
                "userVerification": "preferred"
            }),
        })
    }

    pub async fn login_finish(
        &self,
        request: LoginFinishRequest,
    ) -> Result<LoginCredentialResponse, WebAuthnError> {
        let now = Utc::now();
        let challenge = self
            .store
            .consume_challenge(request.challenge_id, WebAuthnChallengeKind::Login, now)
            .await?;
        let credential = self
            .store
            .credential_by_id(&request.credential_id)
            .await?
            .ok_or_else(|| WebAuthnError::Verify("credential not registered".to_string()))?;
        if credential.user_id != challenge.user_id {
            return Err(WebAuthnError::Verify(
                "credential does not belong to challenge user".to_string(),
            ));
        }

        let client_data_json = decode_b64(&request.client_data_json)?;
        let client = parse_client_data(&client_data_json)?;
        verify_client_data(&client, "webauthn.get", &challenge.challenge, &self.config)?;

        let authenticator_data = decode_b64(&request.authenticator_data)?;
        let assertion = parse_assertion_auth_data(&authenticator_data, &self.config)?;
        if assertion.sign_count != 0
            && credential.sign_count != 0
            && assertion.sign_count <= credential.sign_count
        {
            return Err(WebAuthnError::Verify(
                "authenticator sign counter did not advance".to_string(),
            ));
        }

        let signature = decode_b64(&request.signature)?;
        verify_signature(
            &credential.public_key,
            &authenticator_data,
            &client_data_json,
            &signature,
        )?;
        self.store
            .update_login_counter(
                &credential.credential_id,
                assertion.sign_count,
                assertion.user_verified,
            )
            .await?;

        Ok(LoginCredentialResponse {
            credential_id: credential.credential_id,
            sign_count: assertion.sign_count,
            user_verified: assertion.user_verified,
        })
    }
}

#[derive(Debug, Deserialize)]
struct ClientData {
    #[serde(rename = "type")]
    type_: String,
    challenge: String,
    origin: String,
}

#[derive(Debug)]
struct RegistrationAuthData {
    credential_id: String,
    public_key: String,
    sign_count: u32,
    aaguid: String,
    user_verified: bool,
}

#[derive(Debug)]
struct AssertionAuthData {
    sign_count: u32,
    user_verified: bool,
}

fn validate_challenge_record(
    challenge: &WebAuthnChallengeRecord,
    now: DateTime<Utc>,
) -> Result<(), WebAuthnError> {
    if challenge.used_at.is_some() {
        return Err(WebAuthnError::Challenge(
            "challenge already used".to_string(),
        ));
    }
    if challenge.expires_at <= now {
        return Err(WebAuthnError::Challenge("challenge expired".to_string()));
    }
    Ok(())
}

fn parse_client_data(client_data_json: &[u8]) -> Result<ClientData, WebAuthnError> {
    serde_json::from_slice(client_data_json)
        .map_err(|error| WebAuthnError::Verify(format!("invalid clientDataJSON: {error}")))
}

fn verify_client_data(
    client: &ClientData,
    expected_type: &str,
    expected_challenge: &str,
    config: &RelyingPartyConfig,
) -> Result<(), WebAuthnError> {
    if client.type_ != expected_type {
        return Err(WebAuthnError::Verify(format!(
            "unexpected WebAuthn type {}",
            client.type_
        )));
    }
    if client.challenge != expected_challenge {
        return Err(WebAuthnError::Verify(
            "challenge mismatch in clientDataJSON".to_string(),
        ));
    }
    if client.origin != config.rp_origin {
        return Err(WebAuthnError::Verify(format!(
            "origin mismatch: {}",
            client.origin
        )));
    }
    Ok(())
}

fn parse_attestation_auth_data(attestation: &[u8]) -> Result<Vec<u8>, WebAuthnError> {
    let (value, consumed) = CborValue::parse(attestation)?;
    if consumed != attestation.len() {
        return Err(WebAuthnError::Verify(
            "trailing bytes in attestation object".to_string(),
        ));
    }
    let CborValue::Map(map) = value else {
        return Err(WebAuthnError::Verify(
            "attestation object must be a map".to_string(),
        ));
    };
    match map.get(&CborKey::Text("authData".to_string())) {
        Some(CborValue::Bytes(bytes)) => Ok(bytes.clone()),
        _ => Err(WebAuthnError::Verify(
            "attestation object missing authData".to_string(),
        )),
    }
}

fn parse_registration_auth_data(
    auth_data: &[u8],
    config: &RelyingPartyConfig,
) -> Result<RegistrationAuthData, WebAuthnError> {
    let (flags, sign_count) = parse_common_auth_data(auth_data, config)?;
    if flags & FLAG_AT == 0 {
        return Err(WebAuthnError::Verify(
            "registration authenticator data missing attested credential".to_string(),
        ));
    }
    if auth_data.len() < 55 {
        return Err(WebAuthnError::Verify(
            "registration authenticator data too short".to_string(),
        ));
    }
    let aaguid = b64(&auth_data[37..53]);
    let credential_len = u16::from_be_bytes([auth_data[53], auth_data[54]]) as usize;
    let credential_start = 55;
    let credential_end = credential_start + credential_len;
    if auth_data.len() <= credential_end {
        return Err(WebAuthnError::Verify(
            "registration credential id out of bounds".to_string(),
        ));
    }
    let credential_id = b64(&auth_data[credential_start..credential_end]);
    let (cose_key, consumed) = CborValue::parse(&auth_data[credential_end..])?;
    if credential_end + consumed != auth_data.len() {
        return Err(WebAuthnError::Verify(
            "trailing bytes after credential public key".to_string(),
        ));
    }
    let public_key = cose_ec2_public_key(&cose_key)?;

    Ok(RegistrationAuthData {
        credential_id,
        public_key,
        sign_count,
        aaguid,
        user_verified: flags & FLAG_UV != 0,
    })
}

fn parse_assertion_auth_data(
    auth_data: &[u8],
    config: &RelyingPartyConfig,
) -> Result<AssertionAuthData, WebAuthnError> {
    let (flags, sign_count) = parse_common_auth_data(auth_data, config)?;
    Ok(AssertionAuthData {
        sign_count,
        user_verified: flags & FLAG_UV != 0,
    })
}

fn parse_common_auth_data(
    auth_data: &[u8],
    config: &RelyingPartyConfig,
) -> Result<(u8, u32), WebAuthnError> {
    if auth_data.len() < 37 {
        return Err(WebAuthnError::Verify(
            "authenticator data too short".to_string(),
        ));
    }
    let expected_rp_hash = Sha256::digest(config.rp_id.as_bytes());
    if &auth_data[..32] != expected_rp_hash.as_slice() {
        return Err(WebAuthnError::Verify("rp_id hash mismatch".to_string()));
    }
    let flags = auth_data[32];
    if flags & FLAG_UP == 0 {
        return Err(WebAuthnError::Verify(
            "user presence flag is not set".to_string(),
        ));
    }
    let sign_count =
        u32::from_be_bytes([auth_data[33], auth_data[34], auth_data[35], auth_data[36]]);
    Ok((flags, sign_count))
}

fn cose_ec2_public_key(value: &CborValue) -> Result<String, WebAuthnError> {
    let CborValue::Map(map) = value else {
        return Err(WebAuthnError::Verify(
            "COSE public key must be a map".to_string(),
        ));
    };
    let kty = cbor_int(map, 1)?;
    let alg = cbor_int(map, 3)?;
    let crv = cbor_int(map, -1)?;
    if kty != 2 || alg != -7 || crv != 1 {
        return Err(WebAuthnError::Verify(
            "only ES256 P-256 WebAuthn credentials are supported".to_string(),
        ));
    }
    let x = cbor_bytes(map, -2)?;
    let y = cbor_bytes(map, -3)?;
    if x.len() != 32 || y.len() != 32 {
        return Err(WebAuthnError::Verify(
            "P-256 coordinate length must be 32 bytes".to_string(),
        ));
    }
    let mut sec1 = Vec::with_capacity(65);
    sec1.push(0x04);
    sec1.extend_from_slice(x);
    sec1.extend_from_slice(y);
    Ok(b64(&sec1))
}

fn verify_signature(
    public_key: &str,
    authenticator_data: &[u8],
    client_data_json: &[u8],
    signature: &[u8],
) -> Result<(), WebAuthnError> {
    let public_key = decode_b64(public_key)?;
    let verifying_key = VerifyingKey::from_sec1_bytes(&public_key)
        .map_err(|error| WebAuthnError::Verify(format!("invalid public key: {error}")))?;
    let client_hash = Sha256::digest(client_data_json);
    let mut signed = Vec::with_capacity(authenticator_data.len() + client_hash.len());
    signed.extend_from_slice(authenticator_data);
    signed.extend_from_slice(&client_hash);
    let signature = Signature::from_der(signature)
        .map_err(|error| WebAuthnError::Verify(format!("invalid signature DER: {error}")))?;
    verifying_key
        .verify(&signed, &signature)
        .map_err(|error| WebAuthnError::Verify(format!("signature verification failed: {error}")))
}

fn cbor_int(map: &BTreeMap<CborKey, CborValue>, key: i64) -> Result<i64, WebAuthnError> {
    match map.get(&CborKey::Int(key)) {
        Some(CborValue::Int(value)) => Ok(*value),
        _ => Err(WebAuthnError::Verify(format!("COSE key missing int {key}"))),
    }
}

fn cbor_bytes<'a>(
    map: &'a BTreeMap<CborKey, CborValue>,
    key: i64,
) -> Result<&'a [u8], WebAuthnError> {
    match map.get(&CborKey::Int(key)) {
        Some(CborValue::Bytes(value)) => Ok(value),
        _ => Err(WebAuthnError::Verify(format!(
            "COSE key missing bytes {key}"
        ))),
    }
}

fn new_challenge() -> String {
    let mut bytes = [0u8; 32];
    rand::rngs::OsRng.fill_bytes(&mut bytes);
    b64(&bytes)
}

fn b64(bytes: &[u8]) -> String {
    URL_SAFE_NO_PAD.encode(bytes)
}

fn decode_b64(value: &str) -> Result<Vec<u8>, WebAuthnError> {
    URL_SAFE_NO_PAD
        .decode(value)
        .or_else(|_| URL_SAFE.decode(value))
        .map_err(|error| WebAuthnError::Verify(format!("invalid base64url: {error}")))
}

#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord)]
enum CborKey {
    Int(i64),
    Text(String),
}

#[derive(Debug, Clone, PartialEq)]
enum CborValue {
    Int(i64),
    Bytes(Vec<u8>),
    Text(String),
    Map(BTreeMap<CborKey, CborValue>),
    Other,
}

impl CborValue {
    fn parse(input: &[u8]) -> Result<(Self, usize), WebAuthnError> {
        parse_cbor_value(input, 0)
    }
}

fn parse_cbor_value(input: &[u8], offset: usize) -> Result<(CborValue, usize), WebAuthnError> {
    let initial = *input
        .get(offset)
        .ok_or_else(|| WebAuthnError::Verify("unexpected end of CBOR".to_string()))?;
    let major = initial >> 5;
    let additional = initial & 0x1f;
    let (argument, mut cursor) = read_cbor_argument(input, offset + 1, additional)?;
    match major {
        0 => Ok((CborValue::Int(argument as i64), cursor)),
        1 => Ok((CborValue::Int(-1 - argument as i64), cursor)),
        2 => {
            let len = argument as usize;
            let end = cursor + len;
            let bytes = input
                .get(cursor..end)
                .ok_or_else(|| WebAuthnError::Verify("CBOR bytes out of bounds".to_string()))?
                .to_vec();
            Ok((CborValue::Bytes(bytes), end))
        }
        3 => {
            let len = argument as usize;
            let end = cursor + len;
            let bytes = input
                .get(cursor..end)
                .ok_or_else(|| WebAuthnError::Verify("CBOR text out of bounds".to_string()))?;
            let text = String::from_utf8(bytes.to_vec())
                .map_err(|error| WebAuthnError::Verify(format!("invalid CBOR text: {error}")))?;
            Ok((CborValue::Text(text), end))
        }
        4 => {
            for _ in 0..argument {
                let (_, next) = parse_cbor_value(input, cursor)?;
                cursor = next;
            }
            Ok((CborValue::Other, cursor))
        }
        5 => {
            let mut map = BTreeMap::new();
            for _ in 0..argument {
                let (key, next) = parse_cbor_value(input, cursor)?;
                cursor = next;
                let key = match key {
                    CborValue::Int(value) => CborKey::Int(value),
                    CborValue::Text(value) => CborKey::Text(value),
                    _ => {
                        return Err(WebAuthnError::Verify(
                            "unsupported CBOR map key type".to_string(),
                        ));
                    }
                };
                let (value, next) = parse_cbor_value(input, cursor)?;
                cursor = next;
                map.insert(key, value);
            }
            Ok((CborValue::Map(map), cursor))
        }
        6 => parse_cbor_value(input, cursor),
        7 => Ok((CborValue::Other, cursor)),
        _ => Err(WebAuthnError::Verify("invalid CBOR major type".to_string())),
    }
}

fn read_cbor_argument(
    input: &[u8],
    cursor: usize,
    additional: u8,
) -> Result<(u64, usize), WebAuthnError> {
    match additional {
        0..=23 => Ok((additional as u64, cursor)),
        24 => Ok((*input.get(cursor).ok_or_else(cbor_eof)? as u64, cursor + 1)),
        25 => {
            let bytes = input.get(cursor..cursor + 2).ok_or_else(cbor_eof)?;
            Ok((u16::from_be_bytes([bytes[0], bytes[1]]) as u64, cursor + 2))
        }
        26 => {
            let bytes = input.get(cursor..cursor + 4).ok_or_else(cbor_eof)?;
            Ok((
                u32::from_be_bytes([bytes[0], bytes[1], bytes[2], bytes[3]]) as u64,
                cursor + 4,
            ))
        }
        27 => {
            let bytes = input.get(cursor..cursor + 8).ok_or_else(cbor_eof)?;
            Ok((
                u64::from_be_bytes([
                    bytes[0], bytes[1], bytes[2], bytes[3], bytes[4], bytes[5], bytes[6], bytes[7],
                ]),
                cursor + 8,
            ))
        }
        _ => Err(WebAuthnError::Verify(
            "indefinite CBOR values are not supported".to_string(),
        )),
    }
}

fn cbor_eof() -> WebAuthnError {
    WebAuthnError::Verify("unexpected end of CBOR".to_string())
}

const WEBAUTHN_CREDENTIAL_BY_USER_DDL: &str = "\
CREATE TABLE IF NOT EXISTS auth_runtime.webauthn_credential_by_user ( \
    user_id        text, \
    created_at     timestamp, \
    credential_id  text, \
    id             uuid, \
    public_key     text, \
    sign_count     bigint, \
    aaguid         text, \
    user_verified  boolean, \
    last_used_at   timestamp, \
    PRIMARY KEY ((user_id), created_at, credential_id) \
) WITH CLUSTERING ORDER BY (created_at ASC, credential_id ASC) \
  AND compaction = {'class': 'TimeWindowCompactionStrategy', \
                    'compaction_window_unit': 'DAYS', \
                    'compaction_window_size': '1'}";

const WEBAUTHN_CREDENTIAL_BY_ID_DDL: &str = "\
CREATE TABLE IF NOT EXISTS auth_runtime.webauthn_credential_by_id ( \
    credential_id  text PRIMARY KEY, \
    id             uuid, \
    user_id        text, \
    public_key     text, \
    sign_count     bigint, \
    aaguid         text, \
    user_verified  boolean, \
    created_at     timestamp, \
    last_used_at   timestamp \
) WITH compaction = {'class': 'TimeWindowCompactionStrategy', \
                    'compaction_window_unit': 'DAYS', \
                    'compaction_window_size': '1'}";

const WEBAUTHN_CHALLENGE_DDL: &str = "\
CREATE TABLE IF NOT EXISTS auth_runtime.webauthn_challenge ( \
    challenge_id  uuid PRIMARY KEY, \
    user_id       text, \
    kind          text, \
    challenge     text, \
    expires_at    timestamp, \
    used_at       timestamp \
) WITH default_time_to_live = 600 \
  AND compaction = {'class': 'TimeWindowCompactionStrategy', \
                    'compaction_window_unit': 'MINUTES', \
                    'compaction_window_size': '10'}";

const WEBAUTHN_MIGRATIONS: &[Migration] = &[Migration {
    version: 3,
    name: "auth_runtime_webauthn_credentials",
    statements: &[
        WEBAUTHN_CREDENTIAL_BY_USER_DDL,
        WEBAUTHN_CREDENTIAL_BY_ID_DDL,
        WEBAUTHN_CHALLENGE_DDL,
    ],
}];

#[derive(Default)]
pub struct InMemoryWebAuthnStore {
    credentials: Mutex<HashMap<String, WebAuthnCredentialRecord>>,
    challenges: Mutex<HashMap<Uuid, WebAuthnChallengeRecord>>,
}

#[async_trait]
impl WebAuthnStore for InMemoryWebAuthnStore {
    async fn ensure_schema(&self) -> Result<(), WebAuthnError> {
        Ok(())
    }

    async fn save_challenge(
        &self,
        challenge: WebAuthnChallengeRecord,
    ) -> Result<(), WebAuthnError> {
        self.challenges
            .lock()
            .expect("webauthn challenge lock poisoned")
            .insert(challenge.id, challenge);
        Ok(())
    }

    async fn consume_challenge(
        &self,
        challenge_id: Uuid,
        kind: WebAuthnChallengeKind,
        now: DateTime<Utc>,
    ) -> Result<WebAuthnChallengeRecord, WebAuthnError> {
        let mut challenges = self
            .challenges
            .lock()
            .expect("webauthn challenge lock poisoned");
        let challenge = challenges
            .get_mut(&challenge_id)
            .ok_or_else(|| WebAuthnError::Challenge("challenge not found".to_string()))?;
        if challenge.kind != kind {
            return Err(WebAuthnError::Challenge(
                "challenge kind mismatch".to_string(),
            ));
        }
        validate_challenge_record(challenge, now)?;
        challenge.used_at = Some(now);
        Ok(challenge.clone())
    }

    async fn save_credential(
        &self,
        credential: WebAuthnCredentialRecord,
    ) -> Result<(), WebAuthnError> {
        self.credentials
            .lock()
            .expect("webauthn credential lock poisoned")
            .insert(credential.credential_id.clone(), credential);
        Ok(())
    }

    async fn credentials_for_user(
        &self,
        user_id: Uuid,
    ) -> Result<Vec<WebAuthnCredentialRecord>, WebAuthnError> {
        Ok(self
            .credentials
            .lock()
            .expect("webauthn credential lock poisoned")
            .values()
            .filter(|credential| credential.user_id == user_id)
            .cloned()
            .collect())
    }

    async fn credential_by_id(
        &self,
        credential_id: &str,
    ) -> Result<Option<WebAuthnCredentialRecord>, WebAuthnError> {
        Ok(self
            .credentials
            .lock()
            .expect("webauthn credential lock poisoned")
            .get(credential_id)
            .cloned())
    }

    async fn update_login_counter(
        &self,
        credential_id: &str,
        sign_count: u32,
        user_verified: bool,
    ) -> Result<(), WebAuthnError> {
        let mut credentials = self
            .credentials
            .lock()
            .expect("webauthn credential lock poisoned");
        let credential = credentials
            .get_mut(credential_id)
            .ok_or_else(|| WebAuthnError::Store("credential not found".to_string()))?;
        credential.sign_count = sign_count;
        credential.user_verified = user_verified;
        credential.last_used_at = Some(Utc::now());
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use p256::ecdsa::SigningKey;
    use p256::ecdsa::signature::Signer;

    fn service() -> WebAuthnService {
        WebAuthnService::new(
            RelyingPartyConfig {
                rp_id: "openfoundry.local".into(),
                rp_origin: "https://openfoundry.local".into(),
                rp_name: "OpenFoundry".into(),
            },
            Arc::new(InMemoryWebAuthnStore::default()),
        )
    }

    #[test]
    fn cassandra_migration_uses_auth_runtime_after_session_tables() {
        assert_eq!(WEBAUTHN_MIGRATIONS[0].version, 3);
        assert_eq!(
            WEBAUTHN_MIGRATIONS[0].name,
            "auth_runtime_webauthn_credentials"
        );
        assert!(
            WEBAUTHN_CREDENTIAL_BY_USER_DDL.contains("auth_runtime.webauthn_credential_by_user")
        );
        assert!(WEBAUTHN_CREDENTIAL_BY_ID_DDL.contains("auth_runtime.webauthn_credential_by_id"));
        assert!(WEBAUTHN_CHALLENGE_DDL.contains("auth_runtime.webauthn_challenge"));
    }

    fn client_data(type_: &str, challenge: &str) -> Vec<u8> {
        serde_json::to_vec(&json!({
            "type": type_,
            "challenge": challenge,
            "origin": "https://openfoundry.local"
        }))
        .unwrap()
    }

    fn auth_data_prefix(sign_count: u32, flags: u8) -> Vec<u8> {
        let mut auth_data = Vec::new();
        auth_data.extend_from_slice(&Sha256::digest(b"openfoundry.local"));
        auth_data.push(flags);
        auth_data.extend_from_slice(&sign_count.to_be_bytes());
        auth_data
    }

    fn cbor_uint(value: u64) -> Vec<u8> {
        match value {
            0..=23 => vec![value as u8],
            24..=0xff => vec![0x18, value as u8],
            0x100..=0xffff => {
                let bytes = (value as u16).to_be_bytes();
                vec![0x19, bytes[0], bytes[1]]
            }
            _ => {
                let bytes = (value as u32).to_be_bytes();
                vec![0x1a, bytes[0], bytes[1], bytes[2], bytes[3]]
            }
        }
    }

    fn cbor_int(value: i64) -> Vec<u8> {
        if value >= 0 {
            cbor_uint(value as u64)
        } else {
            let encoded = (-1 - value) as u64;
            let mut bytes = cbor_uint(encoded);
            bytes[0] |= 0x20;
            bytes
        }
    }

    fn cbor_bytes(bytes: &[u8]) -> Vec<u8> {
        let mut out = cbor_uint(bytes.len() as u64);
        out[0] |= 0x40;
        out.extend_from_slice(bytes);
        out
    }

    fn cbor_text(text: &str) -> Vec<u8> {
        let mut out = cbor_uint(text.len() as u64);
        out[0] |= 0x60;
        out.extend_from_slice(text.as_bytes());
        out
    }

    fn cbor_map(entries: Vec<(Vec<u8>, Vec<u8>)>) -> Vec<u8> {
        let mut out = cbor_uint(entries.len() as u64);
        out[0] |= 0xa0;
        for (key, value) in entries {
            out.extend_from_slice(&key);
            out.extend_from_slice(&value);
        }
        out
    }

    fn cose_key(signing_key: &SigningKey) -> Vec<u8> {
        let point = signing_key.verifying_key().to_encoded_point(false);
        let x = point.x().unwrap();
        let y = point.y().unwrap();
        cbor_map(vec![
            (cbor_int(1), cbor_int(2)),
            (cbor_int(3), cbor_int(-7)),
            (cbor_int(-1), cbor_int(1)),
            (cbor_int(-2), cbor_bytes(x)),
            (cbor_int(-3), cbor_bytes(y)),
        ])
    }

    fn attestation_object(
        signing_key: &SigningKey,
        credential_id: &[u8],
        sign_count: u32,
    ) -> String {
        let mut auth_data = auth_data_prefix(sign_count, FLAG_UP | FLAG_UV | FLAG_AT);
        auth_data.extend_from_slice(&[0x11; 16]);
        auth_data.extend_from_slice(&(credential_id.len() as u16).to_be_bytes());
        auth_data.extend_from_slice(credential_id);
        auth_data.extend_from_slice(&cose_key(signing_key));
        let attestation = cbor_map(vec![
            (cbor_text("fmt"), cbor_text("none")),
            (cbor_text("authData"), cbor_bytes(&auth_data)),
            (cbor_text("attStmt"), cbor_map(Vec::new())),
        ]);
        b64(&attestation)
    }

    fn assertion_signature(
        signing_key: &SigningKey,
        authenticator_data: &[u8],
        client_data_json: &[u8],
    ) -> String {
        let client_hash = Sha256::digest(client_data_json);
        let mut signed = Vec::new();
        signed.extend_from_slice(authenticator_data);
        signed.extend_from_slice(&client_hash);
        let signature: Signature = signing_key.sign(&signed);
        b64(signature.to_der().as_bytes())
    }

    #[tokio::test]
    async fn register_and_login_with_es256_fixture() {
        let service = service();
        let user_id = Uuid::now_v7();
        let signing_key = SigningKey::random(&mut rand::rngs::OsRng);
        let credential_id = b"credential-1";
        let credential_id_b64 = b64(credential_id);

        let challenge = service
            .register_challenge(user_id, "alice@example.com", "Alice")
            .await
            .unwrap();
        let challenge_value = challenge.public_key["challenge"].as_str().unwrap();
        let register_client = client_data("webauthn.create", challenge_value);
        let registered = service
            .register_finish(RegisterFinishRequest {
                challenge_id: challenge.challenge_id,
                credential_id: credential_id_b64.clone(),
                client_data_json: b64(&register_client),
                attestation_object: attestation_object(&signing_key, credential_id, 1),
            })
            .await
            .unwrap();
        assert_eq!(registered.credential_id, credential_id_b64);
        assert!(service.has_credentials(user_id).await.unwrap());

        let login = service.login_challenge(user_id).await.unwrap();
        let login_challenge = login.public_key["challenge"].as_str().unwrap();
        let login_client = client_data("webauthn.get", login_challenge);
        let authenticator_data = auth_data_prefix(2, FLAG_UP | FLAG_UV);
        let signature = assertion_signature(&signing_key, &authenticator_data, &login_client);
        let finished = service
            .login_finish(LoginFinishRequest {
                challenge_id: login.challenge_id,
                credential_id: credential_id_b64.clone(),
                client_data_json: b64(&login_client),
                authenticator_data: b64(&authenticator_data),
                signature,
            })
            .await
            .unwrap();
        assert_eq!(finished.credential_id, credential_id_b64);
        assert_eq!(finished.sign_count, 2);
        assert!(finished.user_verified);
    }

    #[tokio::test]
    async fn login_rejects_replayed_counter() {
        let service = service();
        let user_id = Uuid::now_v7();
        let signing_key = SigningKey::random(&mut rand::rngs::OsRng);
        let credential_id = b"credential-2";
        let credential_id_b64 = b64(credential_id);

        let challenge = service
            .register_challenge(user_id, "bob@example.com", "Bob")
            .await
            .unwrap();
        let register_client = client_data(
            "webauthn.create",
            challenge.public_key["challenge"].as_str().unwrap(),
        );
        service
            .register_finish(RegisterFinishRequest {
                challenge_id: challenge.challenge_id,
                credential_id: credential_id_b64.clone(),
                client_data_json: b64(&register_client),
                attestation_object: attestation_object(&signing_key, credential_id, 5),
            })
            .await
            .unwrap();

        let login = service.login_challenge(user_id).await.unwrap();
        let login_client = client_data(
            "webauthn.get",
            login.public_key["challenge"].as_str().unwrap(),
        );
        let authenticator_data = auth_data_prefix(5, FLAG_UP);
        let signature = assertion_signature(&signing_key, &authenticator_data, &login_client);
        let error = service
            .login_finish(LoginFinishRequest {
                challenge_id: login.challenge_id,
                credential_id: credential_id_b64,
                client_data_json: b64(&login_client),
                authenticator_data: b64(&authenticator_data),
                signature,
            })
            .await
            .unwrap_err();
        assert!(matches!(error, WebAuthnError::Verify(_)));
    }
}
