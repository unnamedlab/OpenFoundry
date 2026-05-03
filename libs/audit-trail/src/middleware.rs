//! Hand-rolled `tower::Layer` that emits one structured tracing event per
//! HTTP request handled by an Axum router.
//!
//! The platform's audit-compliance collector subscribes to the `audit`
//! tracing target and forwards each event to the audit warehouse. We keep
//! this implementation as a `tower::Layer` (rather than
//! `axum::middleware::from_fn`) so it composes cleanly with the rest of
//! the tower stack — including services that are not Axum-native — and so
//! callers do not need to know about the request type.

use std::convert::Infallible;
use std::future::Future;
use std::pin::Pin;
use std::task::{Context, Poll};
use std::time::Instant;

use axum::body::Body;
use axum::http::{Request, Response};
use tower::{Layer, Service};

/// Layer factory. Wraps any `tower::Service<Request<Body>>` and surfaces
/// per-request audit metadata (method, path, status, latency) under the
/// `audit` tracing target.
#[derive(Debug, Clone, Copy, Default)]
pub struct AuditLayer;

/// Convenience constructor used by services to keep the call-site terse:
///
/// ```ignore
/// let app = Router::new()
///     .route("/api/v1/widgets", get(list_widgets))
///     .layer(audit_trail::middleware::audit_layer());
/// ```
pub fn audit_layer() -> AuditLayer {
    AuditLayer
}

impl<S> Layer<S> for AuditLayer {
    type Service = AuditService<S>;

    fn layer(&self, inner: S) -> Self::Service {
        AuditService { inner }
    }
}

/// Service produced by [`AuditLayer`]. Captures method + path before the
/// request runs and emits an `info!(target = "audit", ...)` after it
/// completes. Errors propagate unchanged so retry / fallback layers above
/// us still see them.
#[derive(Debug, Clone)]
pub struct AuditService<S> {
    inner: S,
}

impl<S> Service<Request<Body>> for AuditService<S>
where
    S: Service<Request<Body>, Response = Response<Body>, Error = Infallible>
        + Clone
        + Send
        + 'static,
    S::Future: Send + 'static,
{
    type Response = Response<Body>;
    type Error = Infallible;
    type Future = Pin<Box<dyn Future<Output = Result<Self::Response, Self::Error>> + Send>>;

    fn poll_ready(&mut self, cx: &mut Context<'_>) -> Poll<Result<(), Self::Error>> {
        self.inner.poll_ready(cx)
    }

    fn call(&mut self, req: Request<Body>) -> Self::Future {
        // `Service::call` may be polled before `poll_ready`; the standard
        // Tower idiom is to swap in a clone so the `&mut self` borrow ends
        // here and the inner service can be driven from the future.
        let clone = self.inner.clone();
        let mut inner = std::mem::replace(&mut self.inner, clone);

        let method = req.method().clone();
        let path = req.uri().path().to_owned();
        let start = Instant::now();

        Box::pin(async move {
            let response = inner.call(req).await?;
            let status = response.status().as_u16();
            let elapsed_ms = start.elapsed().as_millis();
            tracing::info!(
                target: "audit",
                http_method = %method,
                http_path = %path,
                http_status = status,
                duration_ms = elapsed_ms as u64,
                "request handled"
            );
            Ok(response)
        })
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use axum::Router;
    use axum::http::StatusCode;
    use axum::routing::get;
    use tower::ServiceExt;

    #[tokio::test]
    async fn audit_layer_passes_response_through_unchanged() {
        let app = Router::new()
            .route("/ping", get(|| async { "pong" }))
            .layer(audit_layer());
        let response = app
            .oneshot(Request::builder().uri("/ping").body(Body::empty()).unwrap())
            .await
            .unwrap();
        assert_eq!(response.status(), StatusCode::OK);
    }
}
