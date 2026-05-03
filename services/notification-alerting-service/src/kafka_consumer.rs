//! Kafka subscription substrate for `notification-alerting-service`.
//!
//! Pinned constants only. The real consumer loop (group rebalance,
//! offset commits, alert-rule evaluation) lands in a follow-up PR
//! that wires `event-bus-data::DataSubscriber`.

/// Topics this service consumes. Mirrors the live ontology topics.
pub const SUBSCRIBE_TOPICS: &[&str] = &["ontology.object.changed.v1", "ontology.action.applied.v1"];

/// Consumer group. Pinned across replicas so Kafka rebalance keeps
/// alerting deterministic.
pub const CONSUMER_GROUP: &str = "notification-alerting-service";

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn topics_pinned() {
        assert_eq!(SUBSCRIBE_TOPICS.len(), 2);
        assert!(SUBSCRIBE_TOPICS.contains(&"ontology.object.changed.v1"));
    }
}
