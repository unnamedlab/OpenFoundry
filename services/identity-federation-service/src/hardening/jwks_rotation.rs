//! S3.1.c — JWKS publication, Vault-backed rotation and rollback.
//!
//! The runtime source of truth is `jwks_keys` in the identity
//! control-plane database. Vault Transit keeps private key material;
//! this module stores only stable `kid`s, Vault key versions and
//! public PEMs.

use std::sync::{Arc, Mutex};

use async_trait::async_trait;
use chrono::{DateTime, Duration, Utc};
use serde::{Deserialize, Serialize};
use sqlx::{FromRow, PgPool};

use super::vault_signer::{RotationPolicy, SignError, VaultKeyRef, VaultTransitSigner};

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
#[serde(rename_all = "snake_case")]
pub enum JwksKeyStatus {
    Active,
    Grace,
    Retired,
}

impl JwksKeyStatus {
    fn parse(value: &str) -> Result<Self, JwksRotationError> {
        match value {
            "active" => Ok(Self::Active),
            "grace" => Ok(Self::Grace),
            "retired" => Ok(Self::Retired),
            other => Err(JwksRotationError::Store(format!(
                "unknown JWKS key status {other}"
            ))),
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct PublicKeyEntry {
    pub kid: String,
    pub kty: String,
    /// PEM-encoded public key, mirrored from `pg-schemas.auth_schema.jwks_keys`.
    pub public_pem: String,
    pub activated_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct JwksKeyRecord {
    pub kid: String,
    pub kty: String,
    pub public_pem: String,
    pub vault_key_name: String,
    pub vault_key_version: u32,
    pub status: JwksKeyStatus,
    pub activated_at: DateTime<Utc>,
    pub grace_started_at: Option<DateTime<Utc>>,
    pub retire_after: Option<DateTime<Utc>>,
    pub retired_at: Option<DateTime<Utc>>,
}

impl JwksKeyRecord {
    fn vault_key_ref(&self) -> VaultKeyRef {
        VaultKeyRef {
            name: self.vault_key_name.clone(),
            version: self.vault_key_version,
        }
    }
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct Jwks {
    pub keys: Vec<JwkEntry>,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct JwkEntry {
    pub kid: String,
    pub kty: String,
    pub public_pem: String,
    /// `"sig"` — these keys are publication-only; signing is done by Vault transit.
    #[serde(rename = "use")]
    pub use_: String,
    /// `"active"` or `"grace"` — non-standard, useful in audit/logs.
    pub status: String,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct RotationOutcome {
    pub previous_active_kid: String,
    pub active_kid: String,
    pub grace_until: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct RollbackOutcome {
    pub restored_active_kid: String,
    pub demoted_kid: String,
    pub grace_until: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize, PartialEq, Eq)]
pub struct SignedDigest {
    pub kid: String,
    pub key: VaultKeyRef,
    pub signature: Vec<u8>,
}

#[derive(Debug, thiserror::Error)]
pub enum JwksRotationError {
    #[error("jwks store error: {0}")]
    Store(String),
    #[error("jwks key state error: {0}")]
    State(String),
    #[error("vault transit error: {0}")]
    Vault(#[from] SignError),
}

impl From<sqlx::Error> for JwksRotationError {
    fn from(error: sqlx::Error) -> Self {
        Self::Store(error.to_string())
    }
}

#[async_trait]
pub trait TransitKeyClient: Send + Sync {
    fn configured_key_ref(&self) -> VaultKeyRef;
    async fn sign_with_key(&self, key: &VaultKeyRef, digest: &[u8]) -> Result<Vec<u8>, SignError>;
    async fn latest_key_ref(&self) -> Result<VaultKeyRef, SignError>;
    async fn rotate_key(&self) -> Result<VaultKeyRef, SignError>;
    async fn public_key_pem(&self, key: &VaultKeyRef) -> Result<String, SignError>;
}

#[async_trait]
impl TransitKeyClient for VaultTransitSigner {
    fn configured_key_ref(&self) -> VaultKeyRef {
        self.config().key.clone()
    }

    async fn sign_with_key(&self, key: &VaultKeyRef, digest: &[u8]) -> Result<Vec<u8>, SignError> {
        VaultTransitSigner::sign_with_key(self, key, digest).await
    }

    async fn latest_key_ref(&self) -> Result<VaultKeyRef, SignError> {
        VaultTransitSigner::latest_key_ref(self).await
    }

    async fn rotate_key(&self) -> Result<VaultKeyRef, SignError> {
        VaultTransitSigner::rotate_key(self).await
    }

    async fn public_key_pem(&self, key: &VaultKeyRef) -> Result<String, SignError> {
        VaultTransitSigner::public_key_pem(self, key).await
    }
}

#[async_trait]
pub trait JwksKeyStore: Send + Sync {
    async fn ensure_schema(&self) -> Result<(), JwksRotationError>;
    async fn active_key(&self) -> Result<Option<JwksKeyRecord>, JwksRotationError>;
    async fn grace_keys(&self, now: DateTime<Utc>)
    -> Result<Vec<JwksKeyRecord>, JwksRotationError>;
    async fn upsert_active_seed(&self, record: JwksKeyRecord) -> Result<(), JwksRotationError>;
    async fn rotate_to(
        &self,
        previous: &JwksKeyRecord,
        next: JwksKeyRecord,
        grace_until: DateTime<Utc>,
    ) -> Result<(), JwksRotationError>;
    async fn rollback_to(
        &self,
        restored: &JwksKeyRecord,
        demoted: &JwksKeyRecord,
        grace_until: DateTime<Utc>,
    ) -> Result<(), JwksRotationError>;
}

#[derive(Debug, Clone)]
pub struct PostgresJwksKeyStore {
    pool: PgPool,
}

impl PostgresJwksKeyStore {
    pub fn new(pool: PgPool) -> Self {
        Self { pool }
    }
}

#[derive(Debug, FromRow)]
struct JwksKeyRow {
    kid: String,
    kty: String,
    public_pem: String,
    vault_key_name: String,
    vault_key_version: i32,
    status: String,
    activated_at: DateTime<Utc>,
    grace_started_at: Option<DateTime<Utc>>,
    retire_after: Option<DateTime<Utc>>,
    retired_at: Option<DateTime<Utc>>,
}

impl TryFrom<JwksKeyRow> for JwksKeyRecord {
    type Error = JwksRotationError;

    fn try_from(row: JwksKeyRow) -> Result<Self, Self::Error> {
        Ok(Self {
            kid: row.kid,
            kty: row.kty,
            public_pem: row.public_pem,
            vault_key_name: row.vault_key_name,
            vault_key_version: row.vault_key_version.try_into().map_err(|_| {
                JwksRotationError::Store("vault_key_version must be positive".to_string())
            })?,
            status: JwksKeyStatus::parse(&row.status)?,
            activated_at: row.activated_at,
            grace_started_at: row.grace_started_at,
            retire_after: row.retire_after,
            retired_at: row.retired_at,
        })
    }
}

#[async_trait]
impl JwksKeyStore for PostgresJwksKeyStore {
    async fn ensure_schema(&self) -> Result<(), JwksRotationError> {
        sqlx::query(JWKS_KEYS_DDL).execute(&self.pool).await?;
        sqlx::query(JWKS_KEYS_ACTIVE_INDEX_DDL)
            .execute(&self.pool)
            .await?;
        sqlx::query(JWKS_KEYS_VERSION_INDEX_DDL)
            .execute(&self.pool)
            .await?;
        Ok(())
    }

    async fn active_key(&self) -> Result<Option<JwksKeyRecord>, JwksRotationError> {
        let row = sqlx::query_as::<_, JwksKeyRow>(
            "SELECT kid, kty, public_pem, vault_key_name, vault_key_version, status, \
             activated_at, grace_started_at, retire_after, retired_at \
             FROM jwks_keys WHERE status = 'active' ORDER BY activated_at DESC LIMIT 1",
        )
        .fetch_optional(&self.pool)
        .await?;
        row.map(TryInto::try_into).transpose()
    }

    async fn grace_keys(
        &self,
        now: DateTime<Utc>,
    ) -> Result<Vec<JwksKeyRecord>, JwksRotationError> {
        let rows = sqlx::query_as::<_, JwksKeyRow>(
            "SELECT kid, kty, public_pem, vault_key_name, vault_key_version, status, \
             activated_at, grace_started_at, retire_after, retired_at \
             FROM jwks_keys \
             WHERE status = 'grace' AND (retire_after IS NULL OR retire_after > $1) \
             ORDER BY activated_at DESC",
        )
        .bind(now)
        .fetch_all(&self.pool)
        .await?;
        rows.into_iter().map(TryInto::try_into).collect()
    }

    async fn upsert_active_seed(&self, record: JwksKeyRecord) -> Result<(), JwksRotationError> {
        sqlx::query(
            "INSERT INTO jwks_keys \
             (kid, kty, public_pem, vault_key_name, vault_key_version, status, activated_at, \
              grace_started_at, retire_after, retired_at) \
             VALUES ($1, $2, $3, $4, $5, 'active', $6, NULL, NULL, NULL) \
             ON CONFLICT (kid) DO UPDATE SET \
             public_pem = EXCLUDED.public_pem, status = 'active', retired_at = NULL, \
             updated_at = NOW()",
        )
        .bind(&record.kid)
        .bind(&record.kty)
        .bind(&record.public_pem)
        .bind(&record.vault_key_name)
        .bind(record.vault_key_version as i32)
        .bind(record.activated_at)
        .execute(&self.pool)
        .await?;
        Ok(())
    }

    async fn rotate_to(
        &self,
        previous: &JwksKeyRecord,
        next: JwksKeyRecord,
        grace_until: DateTime<Utc>,
    ) -> Result<(), JwksRotationError> {
        let mut tx = self.pool.begin().await?;
        sqlx::query(
            "UPDATE jwks_keys SET status = 'grace', grace_started_at = NOW(), \
             retire_after = $2, updated_at = NOW() WHERE kid = $1",
        )
        .bind(&previous.kid)
        .bind(grace_until)
        .execute(&mut *tx)
        .await?;
        sqlx::query(
            "INSERT INTO jwks_keys \
             (kid, kty, public_pem, vault_key_name, vault_key_version, status, activated_at, \
              grace_started_at, retire_after, retired_at) \
             VALUES ($1, $2, $3, $4, $5, 'active', $6, NULL, NULL, NULL) \
             ON CONFLICT (kid) DO UPDATE SET \
             public_pem = EXCLUDED.public_pem, status = 'active', activated_at = EXCLUDED.activated_at, \
             grace_started_at = NULL, retire_after = NULL, retired_at = NULL, updated_at = NOW()",
        )
        .bind(&next.kid)
        .bind(&next.kty)
        .bind(&next.public_pem)
        .bind(&next.vault_key_name)
        .bind(next.vault_key_version as i32)
        .bind(next.activated_at)
        .execute(&mut *tx)
        .await?;
        tx.commit().await?;
        Ok(())
    }

    async fn rollback_to(
        &self,
        restored: &JwksKeyRecord,
        demoted: &JwksKeyRecord,
        grace_until: DateTime<Utc>,
    ) -> Result<(), JwksRotationError> {
        let mut tx = self.pool.begin().await?;
        sqlx::query(
            "UPDATE jwks_keys SET status = 'grace', grace_started_at = NOW(), \
             retire_after = $2, updated_at = NOW() WHERE kid = $1",
        )
        .bind(&demoted.kid)
        .bind(grace_until)
        .execute(&mut *tx)
        .await?;
        sqlx::query(
            "UPDATE jwks_keys SET status = 'active', grace_started_at = NULL, \
             retire_after = NULL, retired_at = NULL, updated_at = NOW() WHERE kid = $1",
        )
        .bind(&restored.kid)
        .execute(&mut *tx)
        .await?;
        tx.commit().await?;
        Ok(())
    }
}

#[derive(Clone)]
pub struct JwksRotationService {
    store: Arc<dyn JwksKeyStore>,
    transit: Arc<dyn TransitKeyClient>,
    policy: RotationPolicy,
    kty: String,
}

impl JwksRotationService {
    pub fn new(
        store: Arc<dyn JwksKeyStore>,
        transit: Arc<dyn TransitKeyClient>,
        policy: RotationPolicy,
    ) -> Self {
        Self {
            store,
            transit,
            policy,
            kty: "RSA".to_string(),
        }
    }

    pub async fn ensure_schema(&self) -> Result<(), JwksRotationError> {
        self.store.ensure_schema().await
    }

    pub async fn published_jwks(&self, now: DateTime<Utc>) -> Result<Jwks, JwksRotationError> {
        self.ensure_seeded(now).await?;
        let active = self.active_key().await?;
        let grace = self.store.grace_keys(now).await?;
        Ok(build_jwks_from_records(&active, &grace))
    }

    pub async fn rotate(&self, now: DateTime<Utc>) -> Result<RotationOutcome, JwksRotationError> {
        self.ensure_seeded(now).await?;
        let previous = self.active_key().await?;
        let next_ref = self.transit.rotate_key().await?;
        let public_pem = self.transit.public_key_pem(&next_ref).await?;
        let next = self.record_from_vault_key(next_ref, public_pem, now);
        let grace_until = now + Duration::days(self.policy.grace_days);
        self.store
            .rotate_to(&previous, next.clone(), grace_until)
            .await?;
        Ok(RotationOutcome {
            previous_active_kid: previous.kid,
            active_kid: next.kid,
            grace_until,
        })
    }

    pub async fn rollback(
        &self,
        target_kid: Option<&str>,
        now: DateTime<Utc>,
    ) -> Result<RollbackOutcome, JwksRotationError> {
        self.ensure_seeded(now).await?;
        let active = self.active_key().await?;
        let grace = self.store.grace_keys(now).await?;
        let restored = match target_kid {
            Some(kid) => grace
                .into_iter()
                .find(|key| key.kid == kid)
                .ok_or_else(|| {
                    JwksRotationError::State(format!("rollback target {kid} is not in grace"))
                })?,
            None => grace
                .into_iter()
                .next()
                .ok_or_else(|| JwksRotationError::State("no grace key to roll back to".into()))?,
        };
        let grace_until = now + Duration::days(self.policy.grace_days);
        self.store
            .rollback_to(&restored, &active, grace_until)
            .await?;
        Ok(RollbackOutcome {
            restored_active_kid: restored.kid,
            demoted_kid: active.kid,
            grace_until,
        })
    }

    pub async fn sign_active(&self, digest: &[u8]) -> Result<SignedDigest, JwksRotationError> {
        self.ensure_seeded(Utc::now()).await?;
        let active = self.active_key().await?;
        let key = active.vault_key_ref();
        let signature = self.transit.sign_with_key(&key, digest).await?;
        Ok(SignedDigest {
            kid: active.kid,
            key,
            signature,
        })
    }

    pub async fn active_signing_key(&self) -> Result<(String, VaultKeyRef), JwksRotationError> {
        self.ensure_seeded(Utc::now()).await?;
        let active = self.active_key().await?;
        Ok((active.kid.clone(), active.vault_key_ref()))
    }

    pub async fn sign_key(
        &self,
        key: &VaultKeyRef,
        digest: &[u8],
    ) -> Result<Vec<u8>, JwksRotationError> {
        Ok(self.transit.sign_with_key(key, digest).await?)
    }

    async fn ensure_seeded(&self, now: DateTime<Utc>) -> Result<(), JwksRotationError> {
        if self.store.active_key().await?.is_some() {
            return Ok(());
        }
        let key = self.transit.latest_key_ref().await?;
        let public_pem = self.transit.public_key_pem(&key).await?;
        self.store
            .upsert_active_seed(self.record_from_vault_key(key, public_pem, now))
            .await
    }

    async fn active_key(&self) -> Result<JwksKeyRecord, JwksRotationError> {
        self.store
            .active_key()
            .await?
            .ok_or_else(|| JwksRotationError::State("no active JWKS key".to_string()))
    }

    fn record_from_vault_key(
        &self,
        key: VaultKeyRef,
        public_pem: String,
        activated_at: DateTime<Utc>,
    ) -> JwksKeyRecord {
        JwksKeyRecord {
            kid: stable_kid(&key),
            kty: self.kty.clone(),
            public_pem,
            vault_key_name: key.name,
            vault_key_version: key.version,
            status: JwksKeyStatus::Active,
            activated_at,
            grace_started_at: None,
            retire_after: None,
            retired_at: None,
        }
    }
}

pub fn stable_kid(key: &VaultKeyRef) -> String {
    format!("{}-v{}", key.name, key.version)
}

pub fn build_jwks(
    active: &PublicKeyEntry,
    previous: Option<&PublicKeyEntry>,
    policy: RotationPolicy,
    now: DateTime<Utc>,
) -> Jwks {
    let mut keys = vec![JwkEntry {
        kid: active.kid.clone(),
        kty: active.kty.clone(),
        public_pem: active.public_pem.clone(),
        use_: "sig".into(),
        status: "active".into(),
    }];
    if let Some(prev) = previous {
        if policy.is_in_grace(prev.activated_at, now) {
            keys.push(JwkEntry {
                kid: prev.kid.clone(),
                kty: prev.kty.clone(),
                public_pem: prev.public_pem.clone(),
                use_: "sig".into(),
                status: "grace".into(),
            });
        }
    }
    Jwks { keys }
}

pub fn build_jwks_from_records(active: &JwksKeyRecord, grace: &[JwksKeyRecord]) -> Jwks {
    let mut keys = vec![jwk_from_record(active, "active")];
    keys.extend(grace.iter().map(|record| jwk_from_record(record, "grace")));
    Jwks { keys }
}

fn jwk_from_record(record: &JwksKeyRecord, status: &str) -> JwkEntry {
    JwkEntry {
        kid: record.kid.clone(),
        kty: record.kty.clone(),
        public_pem: record.public_pem.clone(),
        use_: "sig".into(),
        status: status.to_string(),
    }
}

const JWKS_KEYS_DDL: &str = "\
CREATE TABLE IF NOT EXISTS jwks_keys (\
  kid TEXT PRIMARY KEY,\
  kty TEXT NOT NULL DEFAULT 'RSA',\
  public_pem TEXT NOT NULL,\
  vault_key_name TEXT NOT NULL,\
  vault_key_version INTEGER NOT NULL CHECK (vault_key_version > 0),\
  status TEXT NOT NULL CHECK (status IN ('active', 'grace', 'retired')),\
  activated_at TIMESTAMPTZ NOT NULL,\
  grace_started_at TIMESTAMPTZ,\
  retire_after TIMESTAMPTZ,\
  retired_at TIMESTAMPTZ,\
  metadata JSONB NOT NULL DEFAULT '{}'::jsonb,\
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),\
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()\
)";

const JWKS_KEYS_ACTIVE_INDEX_DDL: &str = "CREATE UNIQUE INDEX IF NOT EXISTS idx_jwks_keys_one_active ON jwks_keys ((status)) \
     WHERE status = 'active'";

const JWKS_KEYS_VERSION_INDEX_DDL: &str = "CREATE UNIQUE INDEX IF NOT EXISTS idx_jwks_keys_vault_version ON jwks_keys \
     (vault_key_name, vault_key_version)";

#[derive(Default)]
pub struct InMemoryJwksKeyStore {
    rows: Mutex<Vec<JwksKeyRecord>>,
}

#[async_trait]
impl JwksKeyStore for InMemoryJwksKeyStore {
    async fn ensure_schema(&self) -> Result<(), JwksRotationError> {
        Ok(())
    }

    async fn active_key(&self) -> Result<Option<JwksKeyRecord>, JwksRotationError> {
        Ok(self
            .rows
            .lock()
            .expect("jwks store lock poisoned")
            .iter()
            .find(|row| row.status == JwksKeyStatus::Active)
            .cloned())
    }

    async fn grace_keys(
        &self,
        now: DateTime<Utc>,
    ) -> Result<Vec<JwksKeyRecord>, JwksRotationError> {
        Ok(self
            .rows
            .lock()
            .expect("jwks store lock poisoned")
            .iter()
            .filter(|row| {
                row.status == JwksKeyStatus::Grace
                    && row.retire_after.map(|retire| retire > now).unwrap_or(true)
            })
            .cloned()
            .collect())
    }

    async fn upsert_active_seed(&self, record: JwksKeyRecord) -> Result<(), JwksRotationError> {
        let mut rows = self.rows.lock().expect("jwks store lock poisoned");
        if let Some(existing) = rows.iter_mut().find(|row| row.kid == record.kid) {
            *existing = record;
        } else {
            rows.push(record);
        }
        Ok(())
    }

    async fn rotate_to(
        &self,
        previous: &JwksKeyRecord,
        mut next: JwksKeyRecord,
        grace_until: DateTime<Utc>,
    ) -> Result<(), JwksRotationError> {
        let mut rows = self.rows.lock().expect("jwks store lock poisoned");
        for row in rows.iter_mut() {
            if row.kid == previous.kid {
                row.status = JwksKeyStatus::Grace;
                row.grace_started_at = Some(Utc::now());
                row.retire_after = Some(grace_until);
            }
        }
        next.status = JwksKeyStatus::Active;
        rows.retain(|row| row.kid != next.kid);
        rows.push(next);
        Ok(())
    }

    async fn rollback_to(
        &self,
        restored: &JwksKeyRecord,
        demoted: &JwksKeyRecord,
        grace_until: DateTime<Utc>,
    ) -> Result<(), JwksRotationError> {
        let mut rows = self.rows.lock().expect("jwks store lock poisoned");
        for row in rows.iter_mut() {
            if row.kid == restored.kid {
                row.status = JwksKeyStatus::Active;
                row.grace_started_at = None;
                row.retire_after = None;
                row.retired_at = None;
            } else if row.kid == demoted.kid {
                row.status = JwksKeyStatus::Grace;
                row.grace_started_at = Some(Utc::now());
                row.retire_after = Some(grace_until);
            }
        }
        Ok(())
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[derive(Default)]
    struct FakeTransit {
        latest: Mutex<u32>,
        signatures: Mutex<Vec<VaultKeyRef>>,
    }

    #[async_trait]
    impl TransitKeyClient for FakeTransit {
        fn configured_key_ref(&self) -> VaultKeyRef {
            VaultKeyRef {
                name: "of-jwks-active".into(),
                version: 1,
            }
        }

        async fn sign_with_key(
            &self,
            key: &VaultKeyRef,
            _digest: &[u8],
        ) -> Result<Vec<u8>, SignError> {
            self.signatures
                .lock()
                .expect("signatures lock poisoned")
                .push(key.clone());
            Ok(format!("sig-v{}", key.version).into_bytes())
        }

        async fn latest_key_ref(&self) -> Result<VaultKeyRef, SignError> {
            Ok(VaultKeyRef {
                name: "of-jwks-active".into(),
                version: *self.latest.lock().expect("latest lock poisoned"),
            })
        }

        async fn rotate_key(&self) -> Result<VaultKeyRef, SignError> {
            let mut latest = self.latest.lock().expect("latest lock poisoned");
            *latest += 1;
            Ok(VaultKeyRef {
                name: "of-jwks-active".into(),
                version: *latest,
            })
        }

        async fn public_key_pem(&self, key: &VaultKeyRef) -> Result<String, SignError> {
            Ok(format!(
                "-----BEGIN PUBLIC KEY-----\nversion-{}\n-----END PUBLIC KEY-----",
                key.version
            ))
        }
    }

    fn service() -> JwksRotationService {
        let transit = Arc::new(FakeTransit {
            latest: Mutex::new(1),
            signatures: Mutex::new(Vec::new()),
        });
        JwksRotationService::new(
            Arc::new(InMemoryJwksKeyStore::default()),
            transit,
            RotationPolicy::ASVS_L2_DEFAULT,
        )
    }

    fn key(kid: &str, days_ago: i64) -> PublicKeyEntry {
        PublicKeyEntry {
            kid: kid.into(),
            kty: "EC".into(),
            public_pem: format!("-----BEGIN PUBLIC KEY-----\n{kid}\n-----END PUBLIC KEY-----"),
            activated_at: Utc::now() - Duration::days(days_ago),
        }
    }

    #[test]
    fn stable_kid_is_derived_from_vault_key_and_version() {
        assert_eq!(
            stable_kid(&VaultKeyRef {
                name: "of-jwks-active".into(),
                version: 9,
            }),
            "of-jwks-active-v9"
        );
    }

    #[test]
    fn single_key_outside_grace() {
        let active = key("k2", 5);
        let previous = key("k1", 200);
        let jwks = build_jwks(
            &active,
            Some(&previous),
            RotationPolicy::ASVS_L2_DEFAULT,
            Utc::now(),
        );
        assert_eq!(jwks.keys.len(), 1);
        assert_eq!(jwks.keys[0].kid, "k2");
    }

    #[test]
    fn two_keys_inside_grace() {
        let active = key("k2", 5);
        let previous = key("k1", 95);
        let jwks = build_jwks(
            &active,
            Some(&previous),
            RotationPolicy::ASVS_L2_DEFAULT,
            Utc::now(),
        );
        assert_eq!(jwks.keys.len(), 2);
        assert_eq!(jwks.keys[1].status, "grace");
    }

    #[tokio::test]
    async fn rotation_publishes_new_active_and_previous_grace() {
        let service = service();
        let now = Utc::now();

        let initial = service.published_jwks(now).await.unwrap();
        assert_eq!(initial.keys[0].kid, "of-jwks-active-v1");

        let rotated = service.rotate(now).await.unwrap();
        assert_eq!(rotated.previous_active_kid, "of-jwks-active-v1");
        assert_eq!(rotated.active_kid, "of-jwks-active-v2");

        let jwks = service.published_jwks(now).await.unwrap();
        assert_eq!(jwks.keys.len(), 2);
        assert_eq!(jwks.keys[0].kid, "of-jwks-active-v2");
        assert_eq!(jwks.keys[0].status, "active");
        assert_eq!(jwks.keys[1].kid, "of-jwks-active-v1");
        assert_eq!(jwks.keys[1].status, "grace");
    }

    #[tokio::test]
    async fn rollback_restores_previous_key_without_dropping_new_public_key() {
        let service = service();
        let now = Utc::now();
        service.rotate(now).await.unwrap();

        let rollback = service.rollback(None, now).await.unwrap();
        assert_eq!(rollback.restored_active_kid, "of-jwks-active-v1");
        assert_eq!(rollback.demoted_kid, "of-jwks-active-v2");

        let jwks = service.published_jwks(now).await.unwrap();
        assert_eq!(jwks.keys.len(), 2);
        assert_eq!(jwks.keys[0].kid, "of-jwks-active-v1");
        assert_eq!(jwks.keys[1].kid, "of-jwks-active-v2");
        assert_eq!(jwks.keys[1].status, "grace");
    }

    #[tokio::test]
    async fn active_signing_uses_current_persisted_vault_version() {
        let service = service();
        let now = Utc::now();
        service.rotate(now).await.unwrap();

        let signed = service.sign_active(b"digest").await.unwrap();
        assert_eq!(signed.kid, "of-jwks-active-v2");
        assert_eq!(signed.key.version, 2);
        assert_eq!(signed.signature, b"sig-v2");
    }
}
