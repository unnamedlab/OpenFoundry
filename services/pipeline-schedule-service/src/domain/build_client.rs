//! Outbound HTTP client to `pipeline-build-service` for the dispatcher.
//!
//! The dispatcher calls `POST /v1/builds` with a [`PipelineBuildTarget`]-
//! shaped payload and projects the response onto a
//! [`BuildAttemptOutcome`] the dispatcher can map to a `RunOutcome`:
//!
//!   * 202 ACCEPTED with a build_id → `Started { build_rid }`
//!   * 409 CONFLICT with `reason="all outputs fresh"` → `AllOutputsFresh`
//!   * 4xx / 5xx with a body → `RejectedByService { status, reason }`
//!
//! The trait [`BuildServiceClient`] is the seam tests use to swap a
//! `wiremock`-backed fake for the production HTTP client.

use async_trait::async_trait;
use serde::{Deserialize, Serialize};

use crate::domain::trigger::PipelineBuildTarget;

/// What the dispatcher learned from one `POST /v1/builds` attempt.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum BuildAttemptOutcome {
    /// The build-service accepted the request and produced a build RID.
    Started { build_rid: String },
    /// The build-service refused the request because every output is
    /// already fresh (per D1.1.5 P2 staleness). The schedule run maps
    /// to `IGNORED` with `failure_reason="all outputs fresh"`.
    AllOutputsFresh,
    /// Anything else returned by the build-service. The dispatcher
    /// records this as `FAILED` with the supplied reason.
    RejectedByService { status: u16, reason: String },
}

#[async_trait]
pub trait BuildServiceClient: Send + Sync {
    async fn create_build(&self, req: &CreateBuildPayload) -> BuildAttemptOutcome;

    /// Run-as identity propagation. The dispatcher selects the
    /// principal kind based on the schedule's `scope_kind`:
    ///   * `User` → forwards the user's JWT (Bearer);
    ///   * `ProjectScoped` → forwards the service principal's
    ///     short-lived token, prefixed `Bearer sp:`.
    /// Default impl ignores the principal — overridden by the HTTP
    /// client.
    async fn create_build_as(
        &self,
        req: &CreateBuildPayload,
        _principal: &RunAsPrincipal,
    ) -> BuildAttemptOutcome {
        self.create_build(req).await
    }
}

/// Identity propagated to `pipeline-build-service` on dispatch.
#[derive(Debug, Clone, PartialEq, Eq)]
pub enum RunAsPrincipal {
    /// USER mode — forward the user's JWT verbatim.
    UserJwt(String),
    /// PROJECT_SCOPED mode — forward the service principal's token.
    /// Tokens are minted by `service_principal_store::mint_token`
    /// (or its prod equivalent) and look like `sp:<rid>:<jwt>`.
    ServicePrincipalToken(String),
}

impl RunAsPrincipal {
    pub fn as_authorization_header(&self) -> String {
        match self {
            RunAsPrincipal::UserJwt(jwt) => format!("Bearer {jwt}"),
            RunAsPrincipal::ServicePrincipalToken(t) => format!("Bearer sp:{t}"),
        }
    }
}

/// Request payload sent to `pipeline-build-service`. Mirrors that
/// service's `CreateBuildRequest` shape; defined locally so this crate
/// does not depend on `pipeline-build-service`'s models.
#[derive(Debug, Clone, Serialize)]
pub struct CreateBuildPayload {
    pub pipeline_rid: String,
    pub build_branch: String,
    #[serde(skip_serializing_if = "Vec::is_empty", default)]
    pub job_spec_fallback: Vec<String>,
    pub force_build: bool,
    pub output_dataset_rids: Vec<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub trigger_kind: Option<String>,
    #[serde(skip_serializing_if = "Option::is_none")]
    pub abort_policy: Option<String>,
}

impl CreateBuildPayload {
    /// Project a [`PipelineBuildTarget`] onto a build-service request.
    /// `output_dataset_rids` defaults to the pipeline RID itself when
    /// no specific outputs are listed; build-service interprets that
    /// as "build everything the pipeline produces".
    pub fn from_target(target: &PipelineBuildTarget, output_dataset_rids: Vec<String>) -> Self {
        Self {
            pipeline_rid: target.pipeline_rid.clone(),
            build_branch: target.build_branch.clone(),
            job_spec_fallback: target.job_spec_fallback.clone(),
            force_build: target.force_build,
            output_dataset_rids,
            trigger_kind: Some("SCHEDULED".to_string()),
            abort_policy: target.abort_policy.clone(),
        }
    }
}

#[derive(Debug, Deserialize)]
struct CreateBuildResponse {
    #[serde(default)]
    build_id: Option<String>,
    #[serde(default)]
    queued_reason: Option<String>,
}

#[derive(Debug, Deserialize)]
struct ErrorBody {
    #[serde(default)]
    error: Option<String>,
    #[serde(default)]
    reason: Option<String>,
}

/// Production reqwest-backed implementation.
#[derive(Clone)]
pub struct HttpBuildServiceClient {
    base_url: String,
    inner: reqwest::Client,
    auth_header: Option<String>,
}

impl HttpBuildServiceClient {
    pub fn new(base_url: impl Into<String>, inner: reqwest::Client) -> Self {
        Self {
            base_url: base_url.into(),
            inner,
            auth_header: None,
        }
    }

    pub fn with_bearer(mut self, token: impl Into<String>) -> Self {
        self.auth_header = Some(format!("Bearer {}", token.into()));
        self
    }
}

#[async_trait]
impl BuildServiceClient for HttpBuildServiceClient {
    async fn create_build(&self, req: &CreateBuildPayload) -> BuildAttemptOutcome {
        self.create_build_inner(req, None).await
    }

    async fn create_build_as(
        &self,
        req: &CreateBuildPayload,
        principal: &RunAsPrincipal,
    ) -> BuildAttemptOutcome {
        self.create_build_inner(req, Some(principal.as_authorization_header())).await
    }
}

impl HttpBuildServiceClient {
    async fn create_build_inner(
        &self,
        req: &CreateBuildPayload,
        principal_header: Option<String>,
    ) -> BuildAttemptOutcome {
        let url = format!(
            "{}/v1/builds",
            self.base_url.trim_end_matches('/')
        );
        let mut request = self.inner.post(&url).json(req);
        // Per-call principal overrides the static auth header so the
        // schedule dispatcher can hand the build-service the schedule's
        // run-as identity rather than the schedule-service's own service
        // principal token.
        if let Some(header) = principal_header.as_ref().or(self.auth_header.as_ref()) {
            request = request.header("Authorization", header);
        }
        let response = match request.send().await {
            Ok(r) => r,
            Err(e) => {
                return BuildAttemptOutcome::RejectedByService {
                    status: 0,
                    reason: format!("transport error: {e}"),
                };
            }
        };
        let status = response.status();
        let body_bytes = response.bytes().await.unwrap_or_default();

        if status == reqwest::StatusCode::CONFLICT {
            // 409 from build-service with reason="all outputs fresh"
            // (D1.1.5 P2) maps to IGNORED.
            if let Ok(err) = serde_json::from_slice::<ErrorBody>(&body_bytes) {
                let reason = err
                    .reason
                    .or(err.error)
                    .unwrap_or_else(|| "conflict".to_string())
                    .to_lowercase();
                if reason.contains("all outputs fresh") || reason.contains("up-to-date") {
                    return BuildAttemptOutcome::AllOutputsFresh;
                }
                return BuildAttemptOutcome::RejectedByService {
                    status: status.as_u16(),
                    reason,
                };
            }
            return BuildAttemptOutcome::RejectedByService {
                status: status.as_u16(),
                reason: "conflict".to_string(),
            };
        }

        if status.is_success() {
            return match serde_json::from_slice::<CreateBuildResponse>(&body_bytes) {
                Ok(parsed) => match parsed.build_id {
                    Some(id) => BuildAttemptOutcome::Started {
                        build_rid: format!("ri.foundry.main.build.{id}"),
                    },
                    None => BuildAttemptOutcome::RejectedByService {
                        status: status.as_u16(),
                        reason: parsed
                            .queued_reason
                            .unwrap_or_else(|| "missing build_id".to_string()),
                    },
                },
                Err(e) => BuildAttemptOutcome::RejectedByService {
                    status: status.as_u16(),
                    reason: format!("response parse error: {e}"),
                },
            };
        }

        // Anything else — map as failed.
        let reason = match serde_json::from_slice::<ErrorBody>(&body_bytes) {
            Ok(err) => err
                .error
                .or(err.reason)
                .unwrap_or_else(|| status.to_string()),
            Err(_) => String::from_utf8_lossy(&body_bytes).into_owned(),
        };
        BuildAttemptOutcome::RejectedByService {
            status: status.as_u16(),
            reason,
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn from_target_sets_scheduled_trigger_kind() {
        let target = PipelineBuildTarget {
            pipeline_rid: "ri.foundry.main.pipeline.x".into(),
            build_branch: "master".into(),
            job_spec_fallback: vec!["ri.foundry.main.job_spec.fallback".into()],
            force_build: false,
            abort_policy: Some("DEPENDENT_ONLY".into()),
        };
        let payload = CreateBuildPayload::from_target(&target, vec!["ri.dataset.x".into()]);
        assert_eq!(payload.trigger_kind.as_deref(), Some("SCHEDULED"));
        assert_eq!(payload.abort_policy.as_deref(), Some("DEPENDENT_ONLY"));
        assert_eq!(payload.output_dataset_rids, vec!["ri.dataset.x".to_string()]);
    }
}
