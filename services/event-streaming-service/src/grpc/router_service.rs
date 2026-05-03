//! `EventRouter` gRPC server: glue between the wire protocol and the routing
//! table.

use std::collections::BTreeMap;
use std::pin::Pin;
use std::sync::Arc;

use bytes::Bytes;
use futures::{Stream, StreamExt};
use tokio_stream::wrappers::ReceiverStream;
use tonic::{Request, Response, Status};

use crate::backends::{Backend, BackendError, BackendRegistry, Envelope};
use crate::metrics::{self, Metrics, publish_result, resolution_result};
use crate::proto::router::{
    EventMessage, PublishRequest, PublishResponse, SubscribeRequest,
    event_router_server::{EventRouter, EventRouterServer},
};
use crate::router::{BackendId, ResolvedRoute, RouteTable};

/// Marker pattern used in metric labels when the default backend handles a
/// topic that no explicit route matched.
const DEFAULT_PATTERN_LABEL: &str = "<default>";

/// Concrete gRPC server.
#[derive(Clone)]
pub struct EventRouterService {
    table: Arc<RouteTable>,
    backends: Arc<BackendRegistry>,
    metrics: Arc<Metrics>,
}

impl EventRouterService {
    pub fn new(
        table: Arc<RouteTable>,
        backends: Arc<BackendRegistry>,
        metrics: Arc<Metrics>,
    ) -> Self {
        Self {
            table,
            backends,
            metrics,
        }
    }

    /// Wrap into a tonic [`EventRouterServer`] ready to be added to a
    /// [`tonic::transport::Server`].
    pub fn into_server(self) -> EventRouterServer<Self> {
        EventRouterServer::new(self)
    }

    fn resolve(&self, topic: &str) -> Result<(BackendId, String, Arc<dyn Backend>), Status> {
        let (backend_id, pattern_label) = match self.table.resolve(topic) {
            ResolvedRoute::Matched(route) => {
                self.metrics.record_resolution(resolution_result::MATCHED);
                (route.backend(), route.pattern().to_string())
            }
            ResolvedRoute::Default(b) => {
                self.metrics.record_resolution(resolution_result::DEFAULT);
                (b, DEFAULT_PATTERN_LABEL.to_string())
            }
            ResolvedRoute::NoMatch => {
                self.metrics.record_resolution(resolution_result::MISSED);
                return Err(Status::invalid_argument(format!(
                    "no route matches topic `{topic}` and no default_backend is configured"
                )));
            }
        };

        let backend = self.backends.get(backend_id).ok_or_else(|| {
            Status::internal(format!(
                "backend `{backend_id}` is referenced by routing table but not registered"
            ))
        })?;
        Ok((backend_id, pattern_label, backend))
    }
}

#[tonic::async_trait]
impl EventRouter for EventRouterService {
    async fn publish(
        &self,
        request: Request<PublishRequest>,
    ) -> Result<Response<PublishResponse>, Status> {
        let req = request.into_inner();
        if req.topic.is_empty() {
            return Err(Status::invalid_argument("`topic` must not be empty"));
        }

        let (backend_id, pattern_label, backend) = self.resolve(&req.topic)?;
        let envelope = Envelope {
            topic: req.topic.clone(),
            payload: Bytes::from(req.payload),
            headers: req.headers.into_iter().collect::<BTreeMap<_, _>>(),
            schema_id: req.schema_id,
        };

        let timer = self
            .metrics
            .publish_latency_seconds
            .with_label_values(&[backend_id.as_str()])
            .start_timer();

        let res = backend.publish(envelope).await;
        timer.observe_duration();

        match res {
            Ok(()) => {
                self.metrics
                    .record_publish(backend_id, &pattern_label, publish_result::OK);
                Ok(Response::new(PublishResponse {
                    backend: backend_id.as_str().to_string(),
                    matched_pattern: pattern_label,
                }))
            }
            Err(err) => {
                let result_label = match &err {
                    BackendError::Unavailable { .. } => publish_result::BACKEND_UNAVAILABLE,
                    _ => publish_result::BACKEND_ERROR,
                };
                self.metrics
                    .record_publish(backend_id, &pattern_label, result_label);
                Err(backend_error_to_status(err))
            }
        }
    }

    type SubscribeStream =
        Pin<Box<dyn Stream<Item = Result<EventMessage, Status>> + Send + 'static>>;

    async fn subscribe(
        &self,
        request: Request<SubscribeRequest>,
    ) -> Result<Response<Self::SubscribeStream>, Status> {
        let req = request.into_inner();
        if req.pattern.is_empty() {
            return Err(Status::invalid_argument("`pattern` must not be empty"));
        }
        let (backend_id, pattern_label, backend) = self.resolve(&req.pattern)?;

        // Open the upstream subscription before we tell the gauge we have a
        // live subscriber so cancellations are accounted for properly.
        let upstream = backend
            .subscribe(&req.pattern)
            .await
            .map_err(backend_error_to_status)?;

        // Use a small bounded channel so we can attach RAII bookkeeping (the
        // gauge decrement) to the lifetime of the spawned task.
        let (tx, rx) = tokio::sync::mpsc::channel::<Result<EventMessage, Status>>(64);
        let metrics = Arc::clone(&self.metrics);
        let pattern_for_metric = pattern_label.clone();

        metrics
            .active_subscriptions
            .with_label_values(&[backend_id.as_str()])
            .inc();

        tokio::spawn(async move {
            let mut upstream = upstream;
            while let Some(msg) = upstream.next().await {
                let item = match msg {
                    Ok(env) => {
                        metrics
                            .events_received_total
                            .with_label_values(&[backend_id.as_str(), &pattern_for_metric])
                            .inc();
                        Ok(envelope_to_message(env))
                    }
                    Err(err) => Err(backend_error_to_status(err)),
                };
                if tx.send(item).await.is_err() {
                    break;
                }
            }
            metrics
                .active_subscriptions
                .with_label_values(&[backend_id.as_str()])
                .dec();
        });

        let stream = ReceiverStream::new(rx);
        Ok(Response::new(Box::pin(stream)))
    }
}

fn envelope_to_message(env: Envelope) -> EventMessage {
    EventMessage {
        topic: env.topic,
        payload: env.payload.to_vec(),
        headers: env.headers.into_iter().collect(),
        schema_id: env.schema_id,
    }
}

fn backend_error_to_status(err: BackendError) -> Status {
    match err {
        BackendError::Unavailable { .. } => Status::unavailable(err.to_string()),
        BackendError::Transport { .. } => Status::internal(err.to_string()),
        BackendError::Rejected { .. } => Status::failed_precondition(err.to_string()),
    }
}

// Silence unused-import warnings until something else picks up `metrics::*`.
#[allow(dead_code)]
fn _force_link_metrics_module() {
    let _ = metrics::publish_result::OK;
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::backends::{Backend, BackendError, EnvelopeStream};
    use crate::router::{BackendId, CompiledRoute, RouteTable};
    use async_trait::async_trait;
    use futures::stream;
    use std::sync::Mutex;
    use tonic::Code;

    /// In-memory backend that records every publish and replays a canned set
    /// of envelopes on subscribe.
    struct MockBackend {
        id: BackendId,
        published: Mutex<Vec<Envelope>>,
        publish_result: Mutex<Result<(), BackendError>>,
        canned_subscription: Mutex<Vec<Envelope>>,
    }

    impl MockBackend {
        fn new(id: BackendId) -> Arc<Self> {
            Arc::new(Self {
                id,
                published: Mutex::new(Vec::new()),
                publish_result: Mutex::new(Ok(())),
                canned_subscription: Mutex::new(Vec::new()),
            })
        }

        fn fail_publish_with(&self, err: BackendError) {
            *self.publish_result.lock().unwrap() = Err(err);
        }

        fn enqueue_subscription(&self, envs: Vec<Envelope>) {
            *self.canned_subscription.lock().unwrap() = envs;
        }
    }

    #[async_trait]
    impl Backend for MockBackend {
        fn id(&self) -> BackendId {
            self.id
        }

        async fn publish(&self, envelope: Envelope) -> Result<(), BackendError> {
            // Replace the result so a subsequent call sees the default Ok(()).
            let result = std::mem::replace(&mut *self.publish_result.lock().unwrap(), Ok(()));
            self.published.lock().unwrap().push(envelope);
            result
        }

        async fn subscribe(&self, _pattern: &str) -> Result<EnvelopeStream, BackendError> {
            let items: Vec<Result<Envelope, BackendError>> = self
                .canned_subscription
                .lock()
                .unwrap()
                .drain(..)
                .map(Ok)
                .collect();
            Ok(Box::pin(stream::iter(items)))
        }
    }

    fn make_service(
        routes: Vec<(&str, BackendId)>,
        default: Option<BackendId>,
        nats: Arc<MockBackend>,
        kafka: Arc<MockBackend>,
    ) -> (EventRouterService, Arc<Metrics>) {
        let entries = routes
            .into_iter()
            .map(|(p, b)| CompiledRoute::compile(p.to_string(), b, None, None).unwrap())
            .collect();
        let table = Arc::new(RouteTable::new(entries, default));

        let mut registry = BackendRegistry::new();
        registry.insert(nats);
        registry.insert(kafka);
        let backends = Arc::new(registry);

        let metrics = Arc::new(Metrics::new());
        let svc = EventRouterService::new(table, backends, Arc::clone(&metrics));
        (svc, metrics)
    }

    #[tokio::test]
    async fn publish_routes_to_matched_backend() {
        let nats = MockBackend::new(BackendId::Nats);
        let kafka = MockBackend::new(BackendId::Kafka);
        let (svc, metrics) = make_service(
            vec![("ctrl.*", BackendId::Nats), ("data.>", BackendId::Kafka)],
            None,
            Arc::clone(&nats),
            Arc::clone(&kafka),
        );

        let resp = svc
            .publish(Request::new(PublishRequest {
                topic: "ctrl.heartbeat".into(),
                payload: b"hi".to_vec(),
                headers: Default::default(),
                schema_id: None,
            }))
            .await
            .expect("publish ok")
            .into_inner();

        assert_eq!(resp.backend, "nats");
        assert_eq!(resp.matched_pattern, "ctrl.*");
        assert_eq!(nats.published.lock().unwrap().len(), 1);
        assert!(kafka.published.lock().unwrap().is_empty());

        let rendered = metrics.render().unwrap();
        assert!(rendered.contains("event_router_events_published_total"));
        assert!(rendered.contains("backend=\"nats\""));
        assert!(rendered.contains("result=\"ok\""));
    }

    #[tokio::test]
    async fn publish_returns_invalid_argument_when_no_match() {
        let nats = MockBackend::new(BackendId::Nats);
        let kafka = MockBackend::new(BackendId::Kafka);
        let (svc, metrics) = make_service(vec![("ctrl.*", BackendId::Nats)], None, nats, kafka);

        let err = svc
            .publish(Request::new(PublishRequest {
                topic: "data.x".into(),
                payload: vec![],
                headers: Default::default(),
                schema_id: None,
            }))
            .await
            .unwrap_err();
        assert_eq!(err.code(), Code::InvalidArgument);
        let rendered = metrics.render().unwrap();
        assert!(rendered.contains("result=\"missed\""));
    }

    #[tokio::test]
    async fn publish_uses_default_backend_when_no_route_matches() {
        let nats = MockBackend::new(BackendId::Nats);
        let kafka = MockBackend::new(BackendId::Kafka);
        let (svc, _metrics) = make_service(
            vec![("ctrl.*", BackendId::Nats)],
            Some(BackendId::Kafka),
            Arc::clone(&nats),
            Arc::clone(&kafka),
        );

        let resp = svc
            .publish(Request::new(PublishRequest {
                topic: "data.orders.v1".into(),
                payload: b"x".to_vec(),
                headers: Default::default(),
                schema_id: None,
            }))
            .await
            .expect("publish ok")
            .into_inner();

        assert_eq!(resp.backend, "kafka");
        assert_eq!(resp.matched_pattern, "<default>");
        assert_eq!(kafka.published.lock().unwrap().len(), 1);
    }

    #[tokio::test]
    async fn publish_propagates_backend_unavailable() {
        let nats = MockBackend::new(BackendId::Nats);
        nats.fail_publish_with(BackendError::Unavailable {
            backend: BackendId::Nats,
            message: "down".into(),
        });
        let kafka = MockBackend::new(BackendId::Kafka);
        let (svc, metrics) = make_service(
            vec![("ctrl.*", BackendId::Nats)],
            None,
            Arc::clone(&nats),
            kafka,
        );

        let err = svc
            .publish(Request::new(PublishRequest {
                topic: "ctrl.x".into(),
                payload: vec![],
                headers: Default::default(),
                schema_id: None,
            }))
            .await
            .unwrap_err();
        assert_eq!(err.code(), Code::Unavailable);
        let rendered = metrics.render().unwrap();
        assert!(rendered.contains("result=\"backend_unavailable\""));
    }

    #[tokio::test]
    async fn publish_rejects_empty_topic() {
        let nats = MockBackend::new(BackendId::Nats);
        let kafka = MockBackend::new(BackendId::Kafka);
        let (svc, _metrics) = make_service(vec![("a.*", BackendId::Nats)], None, nats, kafka);
        let err = svc
            .publish(Request::new(PublishRequest {
                topic: String::new(),
                payload: vec![],
                headers: Default::default(),
                schema_id: None,
            }))
            .await
            .unwrap_err();
        assert_eq!(err.code(), Code::InvalidArgument);
    }

    #[tokio::test]
    async fn subscribe_streams_envelopes_from_backend() {
        let nats = MockBackend::new(BackendId::Nats);
        nats.enqueue_subscription(vec![
            Envelope {
                topic: "ctrl.a".into(),
                payload: Bytes::from_static(b"1"),
                headers: BTreeMap::new(),
                schema_id: None,
            },
            Envelope {
                topic: "ctrl.b".into(),
                payload: Bytes::from_static(b"2"),
                headers: BTreeMap::new(),
                schema_id: None,
            },
        ]);
        let kafka = MockBackend::new(BackendId::Kafka);
        let (svc, _metrics) = make_service(
            vec![("ctrl.*", BackendId::Nats)],
            None,
            Arc::clone(&nats),
            kafka,
        );

        let resp = svc
            .subscribe(Request::new(SubscribeRequest {
                pattern: "ctrl.*".into(),
            }))
            .await
            .expect("subscribe ok");
        let mut stream = resp.into_inner();
        let mut topics = Vec::new();
        while let Some(item) = stream.next().await {
            topics.push(item.unwrap().topic);
            if topics.len() == 2 {
                break;
            }
        }
        assert_eq!(topics, vec!["ctrl.a", "ctrl.b"]);
    }
}
