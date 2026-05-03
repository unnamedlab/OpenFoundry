//! OpenLineage header model attached to every Kafka record published through
//! the data plane.
//!
//! Headers are encoded as UTF-8 strings under fixed `ol-*` keys so any
//! consumer (Rust, Python, Java) can extract them without a schema registry
//! lookup.

use chrono::{DateTime, Utc};
use rdkafka::message::{Header, Headers, OwnedHeaders};

/// Well-known header keys (lowercase, hyphenated) for OpenLineage propagation.
pub mod keys {
    pub const NAMESPACE: &str = "ol-namespace";
    pub const JOB_NAME: &str = "ol-job-name";
    pub const RUN_ID: &str = "ol-run-id";
    pub const EVENT_TIME: &str = "ol-event-time";
    pub const PRODUCER: &str = "ol-producer";
    pub const SCHEMA_URL: &str = "ol-schema-url";
}

/// In-memory representation of the OpenLineage facets we propagate.
#[derive(Debug, Clone)]
pub struct OpenLineageHeaders {
    /// OpenLineage `namespace` (e.g. `of://datasets`).
    pub namespace: String,
    /// OpenLineage `job.name` of the producing job.
    pub job_name: String,
    /// OpenLineage `run.runId` (UUID/ULID-shaped, but kept as a string so
    /// upstream producers can use whatever id format they prefer).
    pub run_id: String,
    /// `eventTime` for this record. Defaults to "now" when not provided.
    pub event_time: DateTime<Utc>,
    /// URL identifying the producer (typically a `https://github.com/...`
    /// or `pkg:` URI per the OpenLineage spec).
    pub producer: String,
    /// Optional schema registry / contract URL for the payload.
    pub schema_url: Option<String>,
}

impl OpenLineageHeaders {
    pub fn new(
        namespace: impl Into<String>,
        job_name: impl Into<String>,
        run_id: impl Into<String>,
        producer: impl Into<String>,
    ) -> Self {
        Self {
            namespace: namespace.into(),
            job_name: job_name.into(),
            run_id: run_id.into(),
            event_time: Utc::now(),
            producer: producer.into(),
            schema_url: None,
        }
    }

    pub fn with_schema_url(mut self, url: impl Into<String>) -> Self {
        self.schema_url = Some(url.into());
        self
    }

    pub fn with_event_time(mut self, ts: DateTime<Utc>) -> Self {
        self.event_time = ts;
        self
    }

    /// Convert to `OwnedHeaders` for attachment to a Kafka record.
    pub fn to_kafka_headers(&self) -> OwnedHeaders {
        let event_time = self.event_time.to_rfc3339();
        let mut headers = OwnedHeaders::new()
            .insert(Header {
                key: keys::NAMESPACE,
                value: Some(self.namespace.as_str()),
            })
            .insert(Header {
                key: keys::JOB_NAME,
                value: Some(self.job_name.as_str()),
            })
            .insert(Header {
                key: keys::RUN_ID,
                value: Some(self.run_id.as_str()),
            })
            .insert(Header {
                key: keys::EVENT_TIME,
                value: Some(event_time.as_str()),
            })
            .insert(Header {
                key: keys::PRODUCER,
                value: Some(self.producer.as_str()),
            });
        if let Some(schema) = &self.schema_url {
            headers = headers.insert(Header {
                key: keys::SCHEMA_URL,
                value: Some(schema.as_str()),
            });
        }
        headers
    }

    /// Try to parse the OpenLineage headers from a borrowed `Headers` view.
    /// Returns `None` if any required field is missing or malformed.
    pub fn from_kafka_headers<H: Headers>(headers: &H) -> Option<Self> {
        let mut namespace = None;
        let mut job_name = None;
        let mut run_id = None;
        let mut event_time: Option<DateTime<Utc>> = None;
        let mut producer = None;
        let mut schema_url = None;

        for i in 0..headers.count() {
            let h = headers.get(i);
            let value = match h.value.and_then(|v| std::str::from_utf8(v).ok()) {
                Some(v) => v.to_string(),
                None => continue,
            };
            match h.key {
                keys::NAMESPACE => namespace = Some(value),
                keys::JOB_NAME => job_name = Some(value),
                keys::RUN_ID => run_id = Some(value),
                keys::EVENT_TIME => {
                    event_time = DateTime::parse_from_rfc3339(&value)
                        .ok()
                        .map(|dt| dt.with_timezone(&Utc));
                }
                keys::PRODUCER => producer = Some(value),
                keys::SCHEMA_URL => schema_url = Some(value),
                _ => {}
            }
        }

        Some(Self {
            namespace: namespace?,
            job_name: job_name?,
            run_id: run_id?,
            event_time: event_time?,
            producer: producer?,
            schema_url,
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use chrono::TimeZone;

    #[test]
    fn roundtrip_through_kafka_headers_preserves_all_fields() {
        let when = Utc.with_ymd_and_hms(2026, 4, 29, 12, 0, 0).unwrap();
        let original = OpenLineageHeaders::new(
            "of://datasets",
            "etl.daily_orders",
            "01HXY...run",
            "https://github.com/unnamedlab/OpenFoundry",
        )
        .with_event_time(when)
        .with_schema_url("https://schemas.openfoundry.dev/orders/v1");

        let kafka = original.to_kafka_headers();
        let parsed =
            OpenLineageHeaders::from_kafka_headers(&kafka).expect("headers should parse back");

        assert_eq!(parsed.namespace, original.namespace);
        assert_eq!(parsed.job_name, original.job_name);
        assert_eq!(parsed.run_id, original.run_id);
        assert_eq!(parsed.event_time, original.event_time);
        assert_eq!(parsed.producer, original.producer);
        assert_eq!(parsed.schema_url, original.schema_url);
    }

    #[test]
    fn missing_required_field_yields_none() {
        let headers = OwnedHeaders::new().insert(Header {
            key: keys::NAMESPACE,
            value: Some("of://x"),
        });
        assert!(OpenLineageHeaders::from_kafka_headers(&headers).is_none());
    }
}
