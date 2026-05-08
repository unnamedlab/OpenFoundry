package cedarauthz

// Cedar policy bundles for the P3 schedule-scope governance flows.
//
// Each helper returns a [PolicyRecord] ready to feed into
// [PolicyStore.ReplacePolicies]. Splitting them out keeps the policies
// version-controlled with the schema (instead of hand-rolled per
// service) and lets tests assert each rule in isolation.
//
// Policies modeled (per the Foundry doc § "Project scope" and the P3
// task surface):
//
//   - schedule_edit_owner_or_editor — the user editing the schedule
//     must be the owner or hold an editor/owner role on its project.
//   - schedule_pause_resume_editor — pause/resume requires editor.
//   - schedule_convert_requires_manage — converting to project-scoped
//     requires the *manage* role on every project listed.
//   - build_run_service_principal_in_scope — a service principal may
//     dispatch a build only when its project_scope_rids contains the
//     pipeline target's project.
//   - build_run_user_clearance — a user dispatching a build needs a
//     clearance covering the target's effective markings.

// AllSchedulePolicies returns every schedule-scope governance policy.
func AllSchedulePolicies() []PolicyRecord {
	return []PolicyRecord{
		ScheduleEditOwnerOrEditor(),
		SchedulePauseResumeEditor(),
		ScheduleConvertRequiresManage(),
		BuildRunServicePrincipalInScope(),
		BuildRunUserClearance(),
	}
}

func ScheduleEditOwnerOrEditor() PolicyRecord {
	return PolicyRecord{
		ID:          "schedule-edit-owner-or-editor",
		Version:     1,
		Description: descPtr("Schedule edits require owner or editor role."),
		Source: `
			permit(
			  principal is User,
			  action == Action::"schedule::edit",
			  resource is Schedule
			) when {
			  principal.roles.contains("owner") ||
			  principal.roles.contains("editor")
			};
		`,
	}
}

func SchedulePauseResumeEditor() PolicyRecord {
	return PolicyRecord{
		ID:          "schedule-pause-resume-editor",
		Version:     1,
		Description: descPtr("Pause/resume requires editor role on the schedule's project."),
		Source: `
			permit(
			  principal is User,
			  action == Action::"schedule::pause_resume",
			  resource is Schedule
			) when {
			  principal.roles.contains("owner") ||
			  principal.roles.contains("editor")
			};
		`,
	}
}

// ScheduleConvertRequiresManage carries an important caveat baked into
// the Rust impl: Cedar can't iterate a literal list inside a policy.
// The handler pre-checks that the user has manage-on-all-projects and
// presents the resource with a virtual `manage_all_target_projects`
// role on the principal. The handler computes that flag from the
// user's project memberships before calling Cedar.
func ScheduleConvertRequiresManage() PolicyRecord {
	return PolicyRecord{
		ID:          "schedule-convert-requires-manage",
		Version:     1,
		Description: descPtr("Converting a schedule to project-scoped requires manage on every target project."),
		Source: `
			permit(
			  principal is User,
			  action == Action::"schedule::convert_to_project_scope",
			  resource is Schedule
			) when {
			  principal.roles.contains("manage_all_target_projects")
			};
		`,
	}
}

func BuildRunServicePrincipalInScope() PolicyRecord {
	return PolicyRecord{
		ID:      "build-run-service-principal-in-scope",
		Version: 1,
		Description: descPtr("A service principal may dispatch a build when its project_scope_rids " +
			"contains the pipeline target's project."),
		Source: `
			permit(
			  principal is ServicePrincipal,
			  action == Action::"build::run",
			  resource is PipelineBuildTarget
			) when {
			  principal.project_scope_rids.contains(resource.project_rid)
			};
		`,
	}
}

func BuildRunUserClearance() PolicyRecord {
	return PolicyRecord{
		ID:          "build-run-user-clearance",
		Version:     1,
		Description: descPtr("A user may dispatch a build only when their clearances cover the target's markings."),
		Source: `
			permit(
			  principal is User,
			  action == Action::"build::run",
			  resource is PipelineBuildTarget
			) when {
			  principal.clearances.containsAll(resource.markings)
			};
		`,
	}
}
