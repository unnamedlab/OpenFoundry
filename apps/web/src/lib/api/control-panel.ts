import api from './client';

export type SupportedLocale = 'en' | 'es';

export interface AppBrandingSettings {
	display_name: string;
	primary_color: string;
	accent_color: string;
	logo_url: string | null;
	favicon_url: string | null;
	show_powered_by: boolean;
}

export type IdentityProviderRuleMatchType = 'email_domain' | 'claim_equals';

export interface IdentityProviderOrganizationRule {
	name: string;
	match_type: IdentityProviderRuleMatchType;
	claim: string | null;
	match_value: string;
	organization_id: string;
	workspace: string | null;
	classification_clearance: string | null;
	roles: string[];
	tenant_tier: string | null;
}

export interface IdentityProviderMapping {
	provider_slug: string;
	default_organization_id: string | null;
	organization_claim: string | null;
	workspace_claim: string | null;
	default_workspace: string | null;
	classification_clearance_claim: string | null;
	default_classification_clearance: string | null;
	role_claim: string | null;
	default_roles: string[];
	allowed_email_domains: string[];
	organization_rules: IdentityProviderOrganizationRule[];
}

export interface ResourceQuotaSettings {
	max_query_limit: number;
	max_distributed_query_workers: number;
	max_pipeline_workers: number;
	max_request_body_bytes: number;
	requests_per_minute: number;
	max_storage_gb: number;
	max_shared_spaces: number;
	max_guest_sessions: number;
}

export interface ResourceManagementPolicy {
	name: string;
	tenant_tier: string;
	applies_to_org_ids: string[];
	applies_to_workspaces: string[];
	quota: ResourceQuotaSettings;
}

export interface UpgradeAssistantCheck {
	id: string;
	label: string;
	owner: string;
	status: string;
	notes: string;
}

export interface UpgradeAssistantStage {
	id: string;
	label: string;
	rollout_percentage: number;
	status: string;
}

export interface UpgradeAssistantSettings {
	current_version: string;
	target_version: string;
	maintenance_window: string;
	rollback_channel: string;
	preflight_checks: UpgradeAssistantCheck[];
	rollout_stages: UpgradeAssistantStage[];
	rollback_steps: string[];
}

export interface ScopedSessionPreset {
	id: string;
	name: string;
	description?: string;
	required_markings: string[];
	allowed_markings: string[];
	enabled: boolean;
}

export interface ScopedSessionConfig {
	enabled: boolean;
	allow_no_scoped_session: boolean;
	always_show_selector: boolean;
	allowed_bypass_groups: string[];
	presets: ScopedSessionPreset[];
}

export interface FileAccessPresetLocalAccessControl {
	id: string;
	kind: string;
	label: string;
	values: string[];
	metadata?: Record<string, unknown>;
}

export interface FileAccessPreset {
	id: string;
	title: string;
	description?: string;
	marking_ids: string[];
	local_access_controls: FileAccessPresetLocalAccessControl[];
	organization_ids: string[];
	supported_resource_kinds: string[];
	default_order: number;
	enabled: boolean;
	created_by?: string;
	created_at?: string;
	updated_by?: string;
	updated_at?: string;
}

export interface FileAccessPresetHistoryEvent {
	id: string;
	actor: string;
	timestamp: string;
	action: string;
	summary: string;
	preset_count: number;
	enabled: boolean;
	guest_organization_behavior: string;
	warning: string;
}

export interface FileAccessPresetConfig {
	enabled: boolean;
	warning: string;
	guest_organization_behavior: 'primary_organization';
	presets: FileAccessPreset[];
	history: FileAccessPresetHistoryEvent[];
}

export interface FileAccessPresetVisibilityRequest {
	organization_id?: string;
	primary_organization_id?: string;
	resource_kind?: string;
}

export interface FileAccessPresetVisibilityResponse {
	warning: string;
	guest_organization_behavior: string;
	effective_organization_id?: string;
	default_preset_id?: string;
	filtered_preset_count: number;
	presets: FileAccessPreset[];
}

export interface ApplicationAccessApplication {
	id: string;
	name: string;
	description?: string;
	category: string;
	lifecycle_stage: string;
	enabled: boolean;
}

export interface ApplicationAccessRule {
	id: string;
	name: string;
	effect: 'allow' | 'block';
	application_ids: string[];
	organization_ids: string[];
	user_ids: string[];
	group_ids: string[];
	lifecycle_stages: string[];
	enabled: boolean;
	reason?: string;
}

export interface ApplicationAccessApprovalPolicy {
	mode: 'self_approve' | 'review_required';
	reviewer_user_ids: string[];
	reviewer_group_ids: string[];
	require_distinct_reviewer_for_policy: boolean;
	instructions?: string;
}

export interface ApplicationAccessConfig {
	enabled: boolean;
	default_visibility: 'visible' | 'hidden';
	warning: string;
	applications: ApplicationAccessApplication[];
	rules: ApplicationAccessRule[];
	approval_policy: ApplicationAccessApprovalPolicy;
	change_requests: ApplicationAccessChangeRequest[];
	history: ApplicationAccessHistoryEvent[];
}

export interface ApplicationAccessChangeRequest {
	id: string;
	kind: string;
	status: string;
	summary: string;
	warning: string;
	requested_by: string;
	requested_at: string;
	decided_by?: string;
	decided_at?: string;
	applied_at?: string;
	comment?: string;
	proposed_config: ApplicationAccessConfig;
}

export interface ApplicationAccessHistoryEvent {
	id: string;
	request_id: string;
	kind: string;
	action: string;
	actor: string;
	timestamp: string;
	summary: string;
	warning: string;
	rule_count: number;
	application_count: number;
}

export interface ApplicationAccessEvaluateRequest {
	application_id?: string;
	application_ids?: string[];
	user_id?: string;
	group_ids?: string[];
	organization_id?: string;
	lifecycle_stage?: string;
}

export interface ApplicationAccessDecision {
	application_id: string;
	visible: boolean;
	decision: string;
	reason: string;
	lifecycle_stage: string;
	matched_rule_ids: string[];
	matched_rule_names: string[];
	default_visibility: string;
	ux_scope_only: boolean;
}

export interface ApplicationAccessEvaluateResponse {
	warning: string;
	decisions: ApplicationAccessDecision[];
}

export interface ApplicationAccessChangeRequestsResponse {
	change_requests: ApplicationAccessChangeRequest[];
	history: ApplicationAccessHistoryEvent[];
	warning: string;
}

export interface MemberDiscoveryOrganizationConfig {
	organization_id: string;
	organization_slug?: string;
	discover_users: boolean;
	discover_groups: boolean;
	consumer_mode_boundary: boolean;
	notes?: string;
	updated_by?: string;
	updated_at?: string;
}

export interface MemberDiscoveryHistoryEvent {
	id: string;
	organization_id: string;
	organization_slug?: string;
	actor: string;
	timestamp: string;
	discover_users: boolean;
	discover_groups: boolean;
	consumer_mode_boundary: boolean;
	warning: string;
}

export interface MemberDiscoveryConfig {
	default_discover_users: boolean;
	default_discover_groups: boolean;
	warning: string;
	organizations: MemberDiscoveryOrganizationConfig[];
	history: MemberDiscoveryHistoryEvent[];
}

export interface UpgradeReadinessCheck {
	id: string;
	label: string;
	status: string;
	detail: string;
}

export interface UpgradeReadinessResponse {
	current_version: string;
	target_version: string;
	release_channel: string;
	readiness: string;
	checks: UpgradeReadinessCheck[];
	blockers: string[];
	recommended_actions: string[];
	next_stage: UpgradeAssistantStage | null;
	completed_stage_count: number;
	total_stage_count: number;
	preflight_ready_count: number;
	preflight_total_count: number;
	completed_rollout_percentage: number;
	generated_at: string;
}

export interface IdentityProviderMappingPreviewRequest {
	provider_slug: string;
	email: string;
	raw_claims: Record<string, unknown>;
}

export interface IdentityProviderMappingPreviewResponse {
	provider_slug: string;
	email: string;
	mapping_found: boolean;
	matched_rule_name: string | null;
	organization_id: string | null;
	workspace: string | null;
	classification_clearance: string | null;
	role_names: string[];
	tenant_tier: string | null;
	resource_policy_name: string | null;
	quota: ResourceQuotaSettings | null;
	notes: string[];
}

export interface ControlPanelSettings {
	platform_name: string;
	support_email: string;
	docs_url: string;
	status_page_url: string;
	announcement_banner: string;
	maintenance_mode: boolean;
	release_channel: string;
	default_region: string;
	deployment_mode: string;
	allow_self_signup: boolean;
	supported_locales: SupportedLocale[];
	default_locale: SupportedLocale;
	allowed_email_domains: string[];
	default_app_branding: AppBrandingSettings;
	restricted_operations: string[];
	identity_provider_mappings: IdentityProviderMapping[];
	resource_management_policies: ResourceManagementPolicy[];
	upgrade_assistant: UpgradeAssistantSettings;
	scoped_sessions: ScopedSessionConfig;
	application_access: ApplicationAccessConfig;
	member_discovery: MemberDiscoveryConfig;
	file_access_presets: FileAccessPresetConfig;
	updated_by: string | null;
	updated_at: string;
}

export type UpdateControlPanelRequest = Partial<{
	platform_name: string;
	support_email: string;
	docs_url: string;
	status_page_url: string;
	announcement_banner: string;
	maintenance_mode: boolean;
	release_channel: string;
	default_region: string;
	deployment_mode: string;
	allow_self_signup: boolean;
	supported_locales: SupportedLocale[];
	default_locale: SupportedLocale;
	allowed_email_domains: string[];
	default_app_branding: AppBrandingSettings;
	restricted_operations: string[];
	identity_provider_mappings: IdentityProviderMapping[];
	resource_management_policies: ResourceManagementPolicy[];
	upgrade_assistant: UpgradeAssistantSettings;
	scoped_sessions: ScopedSessionConfig;
	application_access: ApplicationAccessConfig;
	member_discovery: MemberDiscoveryConfig;
	file_access_presets: FileAccessPresetConfig;
}>;

export function getControlPanel() {
	return api.get<ControlPanelSettings>('/control-panel');
}

export function updateControlPanel(body: UpdateControlPanelRequest) {
	return api.put<ControlPanelSettings>('/control-panel', body);
}

export function getUpgradeReadiness() {
	return api.get<UpgradeReadinessResponse>('/control-panel/upgrade-readiness');
}

export function previewIdentityProviderMapping(body: IdentityProviderMappingPreviewRequest) {
	return api.post<IdentityProviderMappingPreviewResponse>(
		'/control-panel/identity-provider-mappings/preview',
		body,
	);
}

export function getApplicationAccessChangeRequests() {
	return api.get<ApplicationAccessChangeRequestsResponse>('/control-panel/application-access/change-requests');
}

export function decideApplicationAccessChangeRequest(id: string, decision: 'approved' | 'rejected', comment?: string) {
	return api.post<ApplicationAccessConfig>(`/control-panel/application-access/change-requests/${encodeURIComponent(id)}/decision`, {
		decision,
		comment,
	});
}

export function evaluateApplicationAccess(body: ApplicationAccessEvaluateRequest) {
	return api.post<ApplicationAccessEvaluateResponse>('/application-access/evaluate', body);
}

export function listVisibleFileAccessPresets(body: FileAccessPresetVisibilityRequest = {}) {
	return api.post<FileAccessPresetVisibilityResponse>('/file-access-presets/visible', body);
}

// ── Streaming profiles ──────────────────────────────────────────────
// Parked under control-panel per ADR-0046. Mirrors the Go wire shape
// in services/identity-federation-service/internal/handlers/streaming_profiles.go.

export type StreamingProfileStatus = 'active' | 'paused' | 'error' | 'draft';

export type StreamingProfileConnectorType =
	| 'streaming_kafka'
	| 'streaming_kinesis'
	| 'streaming_sqs'
	| 'streaming_pubsub'
	| 'streaming_aveva_pi'
	| 'streaming_external';

export type StreamingProfileWatermarkPolicy =
	| 'none'
	| 'bounded_out_of_orderness'
	| 'monotonic_event_time'
	| 'ingestion_time';

export interface StreamingProfile {
	id: string;
	name: string;
	description?: string;
	connector_type: StreamingProfileConnectorType;
	status: StreamingProfileStatus;
	parallelism: number;
	watermark_policy: StreamingProfileWatermarkPolicy;
	checkpoint_interval_ms: number;
	source_config: Record<string, unknown>;
	destination_dataset_id?: string;
	last_event_at?: string;
	throughput_eps?: number;
	created_by?: string;
	created_at?: string;
	updated_by?: string;
	updated_at?: string;
}

export interface ListStreamingProfilesResponse {
	items: StreamingProfile[];
	total: number;
}

export interface ListStreamingProfilesFilter {
	status?: StreamingProfileStatus;
	connector_type?: StreamingProfileConnectorType;
}

export interface CreateStreamingProfileRequest {
	id?: string;
	name: string;
	description?: string;
	connector_type: StreamingProfileConnectorType;
	status?: StreamingProfileStatus;
	parallelism?: number;
	watermark_policy?: StreamingProfileWatermarkPolicy;
	checkpoint_interval_ms?: number;
	source_config?: Record<string, unknown>;
	destination_dataset_id?: string;
}

export type UpdateStreamingProfileRequest = Partial<{
	name: string;
	description: string;
	connector_type: StreamingProfileConnectorType;
	parallelism: number;
	watermark_policy: StreamingProfileWatermarkPolicy;
	checkpoint_interval_ms: number;
	source_config: Record<string, unknown>;
	destination_dataset_id: string;
}>;

export const STREAMING_PROFILE_CONNECTOR_TYPES: ReadonlyArray<{
	value: StreamingProfileConnectorType;
	label: string;
}> = [
	{ value: 'streaming_kafka', label: 'Apache Kafka' },
	{ value: 'streaming_kinesis', label: 'Amazon Kinesis' },
	{ value: 'streaming_sqs', label: 'Amazon SQS' },
	{ value: 'streaming_pubsub', label: 'Google Cloud Pub/Sub' },
	{ value: 'streaming_aveva_pi', label: 'Aveva PI' },
	{ value: 'streaming_external', label: 'External (Magritte)' },
];

export const STREAMING_PROFILE_WATERMARK_POLICIES: ReadonlyArray<{
	value: StreamingProfileWatermarkPolicy;
	label: string;
}> = [
	{ value: 'none', label: 'None' },
	{ value: 'bounded_out_of_orderness', label: 'Bounded out-of-orderness' },
	{ value: 'monotonic_event_time', label: 'Monotonic event time' },
	{ value: 'ingestion_time', label: 'Ingestion time' },
];

export const STREAMING_PROFILE_STATUSES: ReadonlyArray<{
	value: StreamingProfileStatus;
	label: string;
}> = [
	{ value: 'active', label: 'Active' },
	{ value: 'paused', label: 'Paused' },
	{ value: 'error', label: 'Error' },
	{ value: 'draft', label: 'Draft' },
];

export function listStreamingProfiles(filter: ListStreamingProfilesFilter = {}) {
	const params = new URLSearchParams();
	if (filter.status) params.set('status', filter.status);
	if (filter.connector_type) params.set('connector_type', filter.connector_type);
	const query = params.toString();
	return api.get<ListStreamingProfilesResponse>(
		`/control-panel/streaming-profiles${query ? `?${query}` : ''}`,
	);
}

export function getStreamingProfile(id: string) {
	return api.get<StreamingProfile>(`/control-panel/streaming-profiles/${encodeURIComponent(id)}`);
}

export function createStreamingProfile(body: CreateStreamingProfileRequest) {
	return api.post<StreamingProfile>('/control-panel/streaming-profiles', body);
}

export function updateStreamingProfile(id: string, body: UpdateStreamingProfileRequest) {
	return api.patch<StreamingProfile>(
		`/control-panel/streaming-profiles/${encodeURIComponent(id)}`,
		body,
	);
}

export function deleteStreamingProfile(id: string) {
	return api.delete<void>(`/control-panel/streaming-profiles/${encodeURIComponent(id)}`);
}

export function pauseStreamingProfile(id: string) {
	return api.post<StreamingProfile>(
		`/control-panel/streaming-profiles/${encodeURIComponent(id)}:pause`,
		{},
	);
}

export function resumeStreamingProfile(id: string) {
	return api.post<StreamingProfile>(
		`/control-panel/streaming-profiles/${encodeURIComponent(id)}:resume`,
		{},
	);
}
