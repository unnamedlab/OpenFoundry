//! Cedar policy bundles for the P3 schedule-scope governance flows.
//!
//! Each helper returns a [`PolicyRecord`] ready to feed into
//! [`PolicyStore::replace_policies`]. Splitting them out keeps the
//! policies version-controlled with the schema (instead of hand-rolled
//! per service) and lets tests assert each rule in isolation.
//!
//! Policies modeled (per the Foundry doc § "Project scope" and the
//! P3 task surface):
//!
//!   * `schedule_edit_owner_or_editor` — the user editing the schedule
//!     must be the owner or hold an editor/owner role on its project.
//!   * `schedule_pause_resume_editor` — pause/resume requires editor.
//!   * `schedule_convert_requires_manage` — converting to project-
//!     scoped requires the *manage* role on every project listed.
//!   * `build_run_service_principal_in_scope` — a service principal
//!     may dispatch a build only when its project_scope_rids contains
//!     the pipeline target's project.
//!   * `build_run_user_clearance` — a user dispatching a build needs
//!     a clearance covering the target's effective markings.

use crate::PolicyRecord;

pub fn all_schedule_policies() -> Vec<PolicyRecord> {
    vec![
        schedule_edit_owner_or_editor(),
        schedule_pause_resume_editor(),
        schedule_convert_requires_manage(),
        build_run_service_principal_in_scope(),
        build_run_user_clearance(),
    ]
}

pub fn schedule_edit_owner_or_editor() -> PolicyRecord {
    PolicyRecord {
        id: "schedule-edit-owner-or-editor".into(),
        version: 1,
        description: Some("Schedule edits require owner or editor role.".into()),
        source: r#"
            permit(
              principal is User,
              action == Action::"schedule::edit",
              resource is Schedule
            ) when {
              principal.roles.contains("owner") ||
              principal.roles.contains("editor")
            };
        "#
        .into(),
    }
}

pub fn schedule_pause_resume_editor() -> PolicyRecord {
    PolicyRecord {
        id: "schedule-pause-resume-editor".into(),
        version: 1,
        description: Some("Pause/resume requires editor role on the schedule's project.".into()),
        source: r#"
            permit(
              principal is User,
              action == Action::"schedule::pause_resume",
              resource is Schedule
            ) when {
              principal.roles.contains("owner") ||
              principal.roles.contains("editor")
            };
        "#
        .into(),
    }
}

pub fn schedule_convert_requires_manage() -> PolicyRecord {
    // Cedar can't iterate a literal list inside a policy; the caller
    // pre-checks that the user has manage-on-all-projects and presents
    // the resource with that pre-computed flag in `principal.roles`
    // (the `manage_all_target_projects` virtual role). The handler
    // computes that flag from the user's project memberships before
    // calling Cedar.
    PolicyRecord {
        id: "schedule-convert-requires-manage".into(),
        version: 1,
        description: Some(
            "Converting a schedule to project-scoped requires manage on every target project."
                .into(),
        ),
        source: r#"
            permit(
              principal is User,
              action == Action::"schedule::convert_to_project_scope",
              resource is Schedule
            ) when {
              principal.roles.contains("manage_all_target_projects")
            };
        "#
        .into(),
    }
}

pub fn build_run_service_principal_in_scope() -> PolicyRecord {
    PolicyRecord {
        id: "build-run-service-principal-in-scope".into(),
        version: 1,
        description: Some(
            "A service principal may dispatch a build when its project_scope_rids contains the pipeline target's project.".into(),
        ),
        source: r#"
            permit(
              principal is ServicePrincipal,
              action == Action::"build::run",
              resource is PipelineBuildTarget
            ) when {
              principal.project_scope_rids.contains(resource.project_rid)
            };
        "#
        .into(),
    }
}

pub fn build_run_user_clearance() -> PolicyRecord {
    PolicyRecord {
        id: "build-run-user-clearance".into(),
        version: 1,
        description: Some(
            "A user may dispatch a build only when their clearances cover the target's markings."
                .into(),
        ),
        source: r#"
            permit(
              principal is User,
              action == Action::"build::run",
              resource is PipelineBuildTarget
            ) when {
              principal.clearances.containsAll(resource.markings)
            };
        "#
        .into(),
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::PolicyStore;

    #[tokio::test]
    async fn all_policies_parse_against_the_schema() {
        let store = PolicyStore::empty().expect("schema parses");
        store
            .replace_policies(&all_schedule_policies())
            .await
            .expect("policies validate against schema");
        assert_eq!(store.len().await, 5);
    }
}
