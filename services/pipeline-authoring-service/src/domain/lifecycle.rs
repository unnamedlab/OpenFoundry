//! Pipeline lifecycle FSM.
//!
//! Mirrors the Foundry Pipeline Builder release flow described in
//! `docs_original_palantir_foundry/foundry-docs/Data connectivity & integration/Workflows/Building pipelines/Considerations Pipeline Builder and Code Repositories.md`
//! ("Versioning: full versioning workflow on rails for no-code/high-code
//! user collaboration"). Transitions are kept narrow on purpose — the
//! Builder UI exposes only Save (→Draft), Validate (→Validated), Deploy
//! (→Deployed) and Archive — so any other transition is a programming
//! error worth surfacing as a typed `LifecycleError`.
//!
//! The FSM is intentionally orthogonal to the legacy `status` column
//! (`draft`/`active`), which still drives `pipeline_schedule_service`'s
//! `next_run_at` scheduling. `lifecycle` is the new release-state axis.
//!
//! Legal transitions:
//!   Draft     → Validated, Archived
//!   Validated → Deployed,  Draft, Archived
//!   Deployed  → Archived,  Validated   (rollback to fix + redeploy)
//!   Archived  → (terminal)

use serde::{Deserialize, Serialize};
use thiserror::Error;

#[derive(Debug, Clone, Copy, PartialEq, Eq, Hash, Serialize, Deserialize)]
#[serde(rename_all = "UPPERCASE")]
pub enum PipelineLifecycle {
    Draft,
    Validated,
    Deployed,
    Archived,
}

impl PipelineLifecycle {
    pub const ALL: &'static [PipelineLifecycle] = &[
        PipelineLifecycle::Draft,
        PipelineLifecycle::Validated,
        PipelineLifecycle::Deployed,
        PipelineLifecycle::Archived,
    ];

    pub fn as_str(self) -> &'static str {
        match self {
            PipelineLifecycle::Draft => "DRAFT",
            PipelineLifecycle::Validated => "VALIDATED",
            PipelineLifecycle::Deployed => "DEPLOYED",
            PipelineLifecycle::Archived => "ARCHIVED",
        }
    }

    pub fn parse(value: &str) -> Result<Self, LifecycleError> {
        match value.trim().to_ascii_uppercase().as_str() {
            "DRAFT" => Ok(PipelineLifecycle::Draft),
            "VALIDATED" => Ok(PipelineLifecycle::Validated),
            "DEPLOYED" => Ok(PipelineLifecycle::Deployed),
            "ARCHIVED" => Ok(PipelineLifecycle::Archived),
            other => Err(LifecycleError::Unknown(other.to_string())),
        }
    }

    pub fn is_terminal(self) -> bool {
        matches!(self, PipelineLifecycle::Archived)
    }

    /// Apply a state transition. Returns the new state on success or a
    /// typed `LifecycleError` when the transition is illegal.
    pub fn transition(self, target: PipelineLifecycle) -> Result<Self, LifecycleError> {
        if self == target {
            return Ok(self);
        }
        let allowed = match self {
            PipelineLifecycle::Draft => {
                matches!(target, PipelineLifecycle::Validated | PipelineLifecycle::Archived)
            }
            PipelineLifecycle::Validated => matches!(
                target,
                PipelineLifecycle::Deployed | PipelineLifecycle::Draft | PipelineLifecycle::Archived
            ),
            PipelineLifecycle::Deployed => {
                matches!(target, PipelineLifecycle::Validated | PipelineLifecycle::Archived)
            }
            PipelineLifecycle::Archived => false,
        };
        if allowed {
            Ok(target)
        } else {
            Err(LifecycleError::IllegalTransition { from: self, to: target })
        }
    }
}

impl Default for PipelineLifecycle {
    fn default() -> Self {
        PipelineLifecycle::Draft
    }
}

#[derive(Debug, Error, PartialEq, Eq)]
pub enum LifecycleError {
    #[error("illegal pipeline lifecycle transition {from:?} -> {to:?}")]
    IllegalTransition {
        from: PipelineLifecycle,
        to: PipelineLifecycle,
    },
    #[error("unknown pipeline lifecycle value: '{0}'")]
    Unknown(String),
}
