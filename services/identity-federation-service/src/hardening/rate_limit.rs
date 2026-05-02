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
use std::time::{Duration, Instant};

use async_trait::async_trait;

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
        assert_ne!(key("u", "1.1.1.1", "/login"), key("u", "1.1.1.1", "/oauth/token"));
    }
}
