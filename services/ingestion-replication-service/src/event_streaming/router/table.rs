//! Compiled routing table.
//!
//! Patterns from `topic-routes.yaml` are translated to anchored regular
//! expressions and matched in declaration order: the first hit wins, so users
//! can put more specific routes first and broader patterns later.

use std::fmt;

use regex::Regex;
use serde::{Deserialize, Serialize};

/// Identifies a messaging backend that the router knows how to dispatch to.
#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "lowercase")]
pub enum BackendId {
    Nats,
    Kafka,
}

impl BackendId {
    /// Stable lowercase string used in metric labels and log fields.
    pub const fn as_str(self) -> &'static str {
        match self {
            BackendId::Nats => "nats",
            BackendId::Kafka => "kafka",
        }
    }
}

impl fmt::Display for BackendId {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        f.write_str(self.as_str())
    }
}

/// A single route after compilation.
#[derive(Debug, Clone)]
pub struct CompiledRoute {
    pattern: String,
    regex: Regex,
    backend: BackendId,
    schema_id: Option<String>,
    dlq: Option<String>,
}

impl CompiledRoute {
    /// Compile a NATS-style subject pattern into a [`CompiledRoute`].
    ///
    /// Supports `*` (single token) and `>` (one or more trailing tokens).
    pub fn compile(
        pattern: String,
        backend: BackendId,
        schema_id: Option<String>,
        dlq: Option<String>,
    ) -> Result<Self, regex::Error> {
        let regex = Regex::new(&nats_pattern_to_regex(&pattern))?;
        Ok(Self {
            pattern,
            regex,
            backend,
            schema_id,
            dlq,
        })
    }

    pub fn pattern(&self) -> &str {
        &self.pattern
    }

    pub fn backend(&self) -> BackendId {
        self.backend
    }

    pub fn schema_id(&self) -> Option<&str> {
        self.schema_id.as_deref()
    }

    pub fn dlq(&self) -> Option<&str> {
        self.dlq.as_deref()
    }

    pub fn matches(&self, topic: &str) -> bool {
        self.regex.is_match(topic)
    }
}

/// Result of resolving a topic against the [`RouteTable`].
#[derive(Debug)]
pub enum ResolvedRoute<'a> {
    /// A declared route matched the topic.
    Matched(&'a CompiledRoute),
    /// No route matched but a default backend is configured.
    Default(BackendId),
    /// Nothing matched and no default is configured.
    NoMatch,
}

/// Compiled routing table ready to dispatch topics.
#[derive(Debug, Clone)]
pub struct RouteTable {
    entries: Vec<CompiledRoute>,
    default: Option<BackendId>,
}

impl RouteTable {
    pub fn new(entries: Vec<CompiledRoute>, default: Option<BackendId>) -> Self {
        Self { entries, default }
    }

    pub fn entries(&self) -> &[CompiledRoute] {
        &self.entries
    }

    pub fn default_backend(&self) -> Option<BackendId> {
        self.default
    }

    pub fn is_empty(&self) -> bool {
        self.entries.is_empty()
    }

    /// Resolve a concrete topic against the table.
    ///
    /// Routes are evaluated in declaration order and the first match wins.
    pub fn resolve(&self, topic: &str) -> ResolvedRoute<'_> {
        for entry in &self.entries {
            if entry.matches(topic) {
                return ResolvedRoute::Matched(entry);
            }
        }
        match self.default {
            Some(b) => ResolvedRoute::Default(b),
            None => ResolvedRoute::NoMatch,
        }
    }
}

/// Translate a NATS-style subject pattern into an anchored regular expression.
///
/// Rules:
///   * `*` matches one token (no dots).
///   * `>` is only valid as the trailing token and matches one or more
///     dot-separated tokens.
///   * Any other character is matched literally (regex meta-characters are
///     escaped).
fn nats_pattern_to_regex(pattern: &str) -> String {
    let tokens: Vec<&str> = pattern.split('.').collect();
    let mut out = String::with_capacity(pattern.len() * 2 + 4);
    out.push('^');
    for (i, tok) in tokens.iter().enumerate() {
        if i > 0 {
            out.push_str("\\.");
        }
        match *tok {
            "*" => out.push_str("[^.]+"),
            ">" if i == tokens.len() - 1 => out.push_str("[^.]+(?:\\.[^.]+)*"),
            other => out.push_str(&regex::escape(other)),
        }
    }
    out.push('$');
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    fn route(pattern: &str, backend: BackendId) -> CompiledRoute {
        CompiledRoute::compile(pattern.to_string(), backend, None, None).expect("valid pattern")
    }

    #[test]
    fn star_matches_single_token() {
        let r = route("ctrl.*", BackendId::Nats);
        assert!(r.matches("ctrl.heartbeat"));
        assert!(!r.matches("ctrl.heartbeat.v1"));
        assert!(!r.matches("ctrl"));
        assert!(!r.matches("data.x"));
    }

    #[test]
    fn gt_matches_one_or_more_trailing_tokens() {
        let r = route("data.>", BackendId::Kafka);
        assert!(r.matches("data.orders"));
        assert!(r.matches("data.orders.v1"));
        assert!(r.matches("data.orders.v1.created"));
        assert!(!r.matches("data"));
        assert!(!r.matches("ctrl.x"));
    }

    #[test]
    fn literal_pattern_matches_exactly() {
        let r = route("ctrl.heartbeat", BackendId::Nats);
        assert!(r.matches("ctrl.heartbeat"));
        assert!(!r.matches("ctrl.heartbeat.v1"));
        assert!(!r.matches("xctrl.heartbeat"));
    }

    #[test]
    fn first_match_wins() {
        let table = RouteTable::new(
            vec![
                route("ctrl.heartbeat", BackendId::Nats),
                route("ctrl.*", BackendId::Kafka),
            ],
            None,
        );
        match table.resolve("ctrl.heartbeat") {
            ResolvedRoute::Matched(r) => assert_eq!(r.backend(), BackendId::Nats),
            other => panic!("unexpected: {other:?}"),
        }
        match table.resolve("ctrl.boot") {
            ResolvedRoute::Matched(r) => assert_eq!(r.backend(), BackendId::Kafka),
            other => panic!("unexpected: {other:?}"),
        }
    }

    #[test]
    fn falls_back_to_default() {
        let table = RouteTable::new(
            vec![route("ctrl.*", BackendId::Nats)],
            Some(BackendId::Kafka),
        );
        match table.resolve("data.x") {
            ResolvedRoute::Default(BackendId::Kafka) => {}
            other => panic!("unexpected: {other:?}"),
        }
    }

    #[test]
    fn no_match_when_no_default() {
        let table = RouteTable::new(vec![route("ctrl.*", BackendId::Nats)], None);
        assert!(matches!(table.resolve("data.x"), ResolvedRoute::NoMatch));
    }

    #[test]
    fn backend_id_str() {
        assert_eq!(BackendId::Nats.as_str(), "nats");
        assert_eq!(BackendId::Kafka.as_str(), "kafka");
    }

    #[test]
    fn escapes_regex_metacharacters_in_literal_segments() {
        // The `+` is regex-meta but should be matched literally.
        let r = route("plus+sign.x", BackendId::Nats);
        assert!(r.matches("plus+sign.x"));
        assert!(!r.matches("plussign.x"));
    }
}
