//! S3.1.h — Per-(user, IP) rate limiter for `/login`,
//! `/oauth/token`, `/oauth/authorize`.
//!
//! Sliding-window counter, Redis-backed in production. The
//! [`RateLimiter`] trait keeps the handler decoupled from the
//! storage backend; an [`InMemoryRateLimiter`] is provided for
//! tests and CI. The limiter never throws — on backend failure it
//! returns [`RateLimitDecision::Allow`] (fail-open) and emits a
//! tracing warning, so an outage of Redis cannot lock everyone out
//! of `/login`.

use std::collections::HashMap;
use std::sync::Mutex;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

use async_trait::async_trait;
use redis::aio::ConnectionManager;
use uuid::Uuid;

#[derive(Debug, Clone, Copy, PartialEq, Eq)]
pub enum RateLimitDecision {
    Allow,
    Deny { retry_after_secs: u64 },
}

#[derive(Debug, Clone)]
pub struct LimitConfig {
    pub max_requests: u32,
    pub window: Duration,
}

impl LimitConfig {
    pub const LOGIN: Self = Self {
        max_requests: 10,
        window: Duration::from_secs(60),
    };
    pub const OAUTH_TOKEN: Self = Self {
        max_requests: 30,
        window: Duration::from_secs(60),
    };
    pub const OAUTH_AUTHORIZE: Self = Self {
        max_requests: 30,
        window: Duration::from_secs(60),
    };
}

#[async_trait]
pub trait RateLimiter: Send + Sync {
    async fn check(&self, key: &str, cfg: &LimitConfig) -> RateLimitDecision;
}

#[derive(Clone)]
pub struct RedisRateLimiter {
    manager: ConnectionManager,
    prefix: String,
}

impl RedisRateLimiter {
    pub async fn connect(redis_url: &str) -> Result<Self, redis::RedisError> {
        let client = redis::Client::open(redis_url)?;
        let manager = ConnectionManager::new(client).await?;
        Ok(Self {
            manager,
            prefix: "of:identity:rl".to_string(),
        })
    }

    pub fn new(manager: ConnectionManager) -> Self {
        Self {
            manager,
            prefix: "of:identity:rl".to_string(),
        }
    }

    pub fn with_prefix(mut self, prefix: impl Into<String>) -> Self {
        self.prefix = prefix.into();
        self
    }

    fn redis_key(&self, key: &str) -> String {
        redis_key_with_prefix(&self.prefix, key)
    }
}

#[async_trait]
impl RateLimiter for RedisRateLimiter {
    async fn check(&self, key: &str, cfg: &LimitConfig) -> RateLimitDecision {
        match self.check_redis(key, cfg).await {
            Ok(decision) => decision,
            Err(error) => {
                tracing::warn!(%error, key, "redis rate-limit check failed; allowing request");
                RateLimitDecision::Allow
            }
        }
    }
}

impl RedisRateLimiter {
    async fn check_redis(
        &self,
        key: &str,
        cfg: &LimitConfig,
    ) -> Result<RateLimitDecision, redis::RedisError> {
        let mut conn = self.manager.clone();
        let redis_key = self.redis_key(key);
        let now_ms = unix_ms();
        let window_ms = cfg.window.as_millis().max(1) as i64;
        let cutoff = now_ms.saturating_sub(window_ms);
        let member = format!("{now_ms}:{}", Uuid::now_v7());

        let _: () = redis::cmd("ZREMRANGEBYSCORE")
            .arg(&redis_key)
            .arg(0)
            .arg(cutoff)
            .query_async(&mut conn)
            .await?;
        let _: () = redis::cmd("ZADD")
            .arg(&redis_key)
            .arg(now_ms)
            .arg(member)
            .query_async(&mut conn)
            .await?;
        let count: u32 = redis::cmd("ZCARD")
            .arg(&redis_key)
            .query_async(&mut conn)
            .await?;
        let _: () = redis::cmd("PEXPIRE")
            .arg(&redis_key)
            .arg(window_ms)
            .query_async(&mut conn)
            .await?;

        if count > cfg.max_requests {
            return Ok(RateLimitDecision::Deny {
                retry_after_secs: cfg.window.as_secs().max(1),
            });
        }
        Ok(RateLimitDecision::Allow)
    }
}

/// In-memory implementation. Used in unit tests and dev. Stores a
/// timestamp ring per key; oldest entries fall out of the window.
pub struct InMemoryRateLimiter {
    state: Mutex<HashMap<String, Vec<Instant>>>,
}

impl InMemoryRateLimiter {
    pub fn new() -> Self {
        Self {
            state: Mutex::new(HashMap::new()),
        }
    }
}

impl Default for InMemoryRateLimiter {
    fn default() -> Self {
        Self::new()
    }
}

#[async_trait]
impl RateLimiter for InMemoryRateLimiter {
    async fn check(&self, key: &str, cfg: &LimitConfig) -> RateLimitDecision {
        let now = Instant::now();
        let mut map = self.state.lock().expect("rate-limiter lock poisoned");
        let entries = map.entry(key.to_string()).or_default();
        entries.retain(|t| now.duration_since(*t) < cfg.window);
        if entries.len() as u32 >= cfg.max_requests {
            let oldest = *entries.first().unwrap_or(&now);
            let elapsed = now.duration_since(oldest);
            let retry_after = cfg.window.saturating_sub(elapsed).as_secs().max(1);
            return RateLimitDecision::Deny {
                retry_after_secs: retry_after,
            };
        }
        entries.push(now);
        RateLimitDecision::Allow
    }
}

/// Composes the limiter key from user + IP. Either may be empty
/// (e.g. pre-auth `/login`); the limiter still rate-limits per IP.
pub fn key(user_id: &str, ip: &str, route: &str) -> String {
    format!("{route}|{user_id}|{ip}")
}

fn unix_ms() -> i64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis() as i64
}

fn redis_key_with_prefix(prefix: &str, key: &str) -> String {
    format!("{prefix}:{}", key.replace([' ', '\n', '\r', '\t'], "_"))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[tokio::test]
    async fn allows_within_window_then_denies() {
        let l = InMemoryRateLimiter::new();
        let cfg = LimitConfig {
            max_requests: 3,
            window: Duration::from_secs(60),
        };
        for _ in 0..3 {
            assert_eq!(l.check("k", &cfg).await, RateLimitDecision::Allow);
        }
        match l.check("k", &cfg).await {
            RateLimitDecision::Deny { retry_after_secs } => {
                assert!(retry_after_secs > 0);
            }
            _ => panic!("expected Deny"),
        }
    }

    #[test]
    fn key_separates_routes() {
        assert_ne!(
            key("u", "1.1.1.1", "/login"),
            key("u", "1.1.1.1", "/oauth/token")
        );
    }

    #[test]
    fn redis_key_sanitizes_whitespace() {
        assert_eq!(redis_key_with_prefix("p", "a b\nc"), "p:a_b_c");
    }
}
