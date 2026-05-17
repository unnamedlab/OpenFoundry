import api from './client';

export interface ListResponse<T> {
	items: T[];
}

export type ClassificationLevel = 'public' | 'confidential' | 'pii';
export type AuditEventStatus = 'success' | 'failure' | 'denied';
export type AuditOutcome = 'success' | 'error' | 'unauthorized';
export type AuditSeverity = 'low' | 'medium' | 'high' | 'critical';
export type ComplianceStandard = 'soc2' | 'iso27001' | 'hipaa' | 'gdpr' | 'itar';

export interface AuditEntity {
	kind: string;
	id?: string;
	rid?: string;
	name?: string;
	attributes?: Record<string, unknown>;
}

export interface AuditEvent {
	id: string;
	event_id: string;
	log_entry_id: string;
	sequence_id?: string | null;
	sequence: number;
	previous_hash: string;
	entry_hash: string;
	source_service: string;
	product: string;
	product_version: string;
	producer_type: string;
	channel: string;
	actor: string;
	actor_id: string;
	actor_type: string;
	session_id?: string | null;
	service_account_id?: string | null;
	token_id?: string | null;
	action: string;
	categories: string[];
	resource_type: string;
	resource_id: string;
	entities: AuditEntity[];
	origins: string[];
	origin?: string | null;
	source_origin?: string | null;
	trace_id?: string | null;
	status: AuditEventStatus;
	outcome: AuditOutcome | string;
	severity: AuditSeverity;
	classification: ClassificationLevel;
	subject_id: string | null;
	ip_address: string | null;
	location: string | null;
	metadata: Record<string, unknown>;
	error_metadata: Record<string, unknown>;
	request_fields: Record<string, unknown>;
	result_fields: Record<string, unknown>;
	labels: string[];
	parent_event_id?: string | null;
	initiator_type: string;
	audit_access_tier: string;
	retention_until: string;
	occurred_at: string;
	ingested_at: string;
}

export interface AuditOverview {
	event_count: number;
	critical_event_count: number;
	collector_count: number;
	active_policy_count: number;
	anomaly_count: number;
	gdpr_subject_count: number;
	latest_event: AuditEvent | null;
}

export interface AnomalyAlert {
	id: string;
	title: string;
	description: string;
	severity: string;
	detected_at: string;
	correlation_key: string;
	linked_event_id: string;
	recommended_action: string;
}

export interface EventListResponse {
	items: AuditEvent[];
	anomalies: AnomalyAlert[];
}

export interface CollectorStatus {
	service_name: string;
	subject: string;
	connected: boolean;
	last_event_at: string | null;
	backlog_depth: number;
	health: string;
	next_pull_at: string;
}

export interface ClassificationCatalogEntry {
	classification: ClassificationLevel;
	description: string;
}

export interface AuditPolicy {
	id: string;
	name: string;
	description: string;
	scope: string;
	classification: ClassificationLevel;
	retention_days: number;
	legal_hold: boolean;
	purge_mode: string;
	active: boolean;
	rules: string[];
	updated_by: string;
	created_at: string;
	updated_at: string;
}

export type AuditDeliveryDestinationType = 'siem_api' | 'openfoundry_dataset';
export type AuditDeliveryValidationStatus = 'pending' | 'valid' | 'invalid';
export type AuditDeliveryBackfillStatus = 'idle' | 'running' | 'completed' | 'failed';

export interface AuditDeliveryDestination {
	id: string;
	organization_id?: string | null;
	name: string;
	destination_type: AuditDeliveryDestinationType;
	schema_version: string;
	endpoint_url?: string | null;
	dataset_rid?: string | null;
	enabled: boolean;
	validation_status: AuditDeliveryValidationStatus;
	validation_message: string;
	last_validated_at?: string | null;
	last_backfill_status: AuditDeliveryBackfillStatus;
	last_backfill_started_at?: string | null;
	last_backfill_completed_at?: string | null;
	metadata: Record<string, unknown>;
	created_by: string;
	created_at: string;
	updated_at: string;
}

export interface AuditDeliveryValidationResponse {
	destination_id: string;
	validation_status: AuditDeliveryValidationStatus;
	message: string;
	validated_at?: string | null;
}

export interface AuditDeliveryFile {
	id: string;
	destination_id: string;
	organization_id?: string | null;
	schema_version: string;
	content_format: string;
	start_time: string;
	end_time: string;
	event_count: number;
	duplicate_count: number;
	content_sha256: string;
	content_bytes: number;
	status: string;
	error_message?: string;
	created_at: string;
}

export interface ComplianceFinding {
	control_id: string;
	title: string;
	status: string;
	evidence: string;
}

export interface ComplianceArtifact {
	file_name: string;
	mime_type: string;
	storage_url: string;
	checksum: string;
	size_bytes: number;
}

export interface ComplianceReport {
	id: string;
	standard: ComplianceStandard;
	title: string;
	scope: string;
	window_start: string;
	window_end: string;
	generated_at: string;
	status: string;
	findings: ComplianceFinding[];
	artifact: ComplianceArtifact;
	relevant_event_count: number;
	policy_count: number;
	control_summary: string;
	expires_at: string;
}

export interface GovernanceTemplatePolicy {
	name: string;
	description: string;
	scope: string;
	classification: ClassificationLevel;
	retention_days: number;
	legal_hold: boolean;
	purge_mode: string;
	rules: string[];
}

export interface GovernanceTemplate {
	slug: string;
	name: string;
	summary: string;
	standards: string[];
	default_report_standard: ComplianceStandard;
	checkpoint_prompts: string[];
	sds_remediations: string[];
	policies: GovernanceTemplatePolicy[];
}

export interface GovernanceTemplateApplication {
	id: string;
	template_slug: string;
	template_name: string;
	scope: string;
	standards: string[];
	policy_names: string[];
	checkpoint_prompts: string[];
	sds_remediations: string[];
	default_report_standard: ComplianceStandard;
	applied_by: string;
	applied_at: string;
	updated_at: string;
}

export interface CompliancePostureStandard {
	standard: ComplianceStandard;
	template_available: boolean;
	applied_scope_count: number;
	active_policy_count: number;
	latest_report_status: string | null;
	latest_report_generated_at: string | null;
	coverage_score: number;
	checkpoint_prompt_count: number;
	sds_remediation_count: number;
	evidence_summary: string;
}

export interface CompliancePostureOverview {
	standards: CompliancePostureStandard[];
	supported_capabilities: string[];
	active_template_application_count: number;
	active_legal_hold_policy_count: number;
}

export interface SensitiveDataFinding {
	kind: string;
	redacted: string;
	value: string;
	match_count: number;
}

export interface SensitiveDataScanResponse {
	risk_score: number;
	findings: SensitiveDataFinding[];
	redacted_content: string;
}

export interface GdprExportPayload {
	subject_id: string;
	generated_at: string;
	portable_format: string;
	event_count: number;
	resources: string[];
	audit_excerpt: AuditEvent[];
}

export interface GdprEraseResponse {
	subject_id: string;
	requested_at: string;
	completed_at: string | null;
	status: string;
	masked_event_count: number;
	affected_resources: string[];
	legal_hold: boolean;
}

export function getOverview() {
	return api.get<AuditOverview>('/audit/overview');
}

export function listEvents(filters?: {
	source_service?: string;
	subject_id?: string;
	classification?: string;
	/**
	 * Pre-applied resource RID filter — used by the per-resource
	 * Activity panels (Media-set / Dataset detail pages) to scope the
	 * global audit log to a single subject. Matches `event.resource_id`
	 * exactly.
	 */
	resource_id?: string;
	category?: string;
	trace_id?: string;
	event_id?: string;
}) {
	const search = new URLSearchParams();
	if (filters?.source_service) search.set('source_service', filters.source_service);
	if (filters?.subject_id) search.set('subject_id', filters.subject_id);
	if (filters?.classification) search.set('classification', filters.classification);
	if (filters?.resource_id) search.set('resource_id', filters.resource_id);
	if (filters?.category) search.set('category', filters.category);
	if (filters?.trace_id) search.set('trace_id', filters.trace_id);
	if (filters?.event_id) search.set('event_id', filters.event_id);
	const query = search.toString();
	return api.get<EventListResponse>(`/audit/events${query ? `?${query}` : ''}`);
}

export function appendEvent(body: {
	source_service: string;
	channel: string;
	actor: string;
	action: string;
	resource_type: string;
	resource_id: string;
	status: AuditEventStatus;
	outcome?: AuditOutcome | string;
	severity: AuditSeverity;
	classification: ClassificationLevel;
	categories?: string[];
	entities?: AuditEntity[];
	origins?: string[];
	trace_id?: string | null;
	session_id?: string | null;
	service_account_id?: string | null;
	token_id?: string | null;
	subject_id?: string | null;
	ip_address?: string | null;
	location?: string | null;
	metadata?: Record<string, unknown>;
	error_metadata?: Record<string, unknown>;
	request_fields?: Record<string, unknown>;
	result_fields?: Record<string, unknown>;
	labels?: string[];
	retention_days?: number;
}) {
	return api.post<AuditEvent>('/audit/events', body);
}

export function listCollectors() {
	return api.get<CollectorStatus[]>('/audit/collectors');
}

export function listAnomalies() {
	return api.get<AnomalyAlert[]>('/audit/anomalies');
}

export function listClassifications() {
	return api.get<ClassificationCatalogEntry[]>('/audit/classifications');
}

export function listPolicies() {
	return api.get<ListResponse<AuditPolicy>>('/audit/policies');
}

export function createPolicy(body: {
	name: string;
	description?: string;
	scope: string;
	classification: ClassificationLevel;
	retention_days: number;
	legal_hold?: boolean;
	purge_mode: string;
	active?: boolean;
	rules?: string[];
	updated_by: string;
}) {
	return api.post<AuditPolicy>('/audit/policies', body);
}

export function updatePolicy(
	id: string,
	body: Partial<{
		name: string;
		description: string;
		scope: string;
		classification: ClassificationLevel;
		retention_days: number;
		legal_hold: boolean;
		purge_mode: string;
		active: boolean;
		rules: string[];
		updated_by: string;
	}>,
) {
	return api.patch<AuditPolicy>(`/audit/policies/${id}`, body);
}

export function listAuditDeliveryDestinations() {
	return api.get<ListResponse<AuditDeliveryDestination>>('/audit/delivery/destinations');
}

export function createAuditDeliveryDestination(body: {
	organization_id?: string;
	name: string;
	destination_type: AuditDeliveryDestinationType;
	schema_version?: string;
	endpoint_url?: string | null;
	dataset_rid?: string | null;
	enabled?: boolean;
	metadata?: Record<string, unknown>;
}) {
	return api.post<AuditDeliveryDestination>('/audit/delivery/destinations', body);
}

export function validateAuditDeliveryDestination(id: string) {
	return api.post<AuditDeliveryValidationResponse>(`/audit/delivery/destinations/${id}/validate`, {});
}

export function backfillAuditDeliveryDestination(
	id: string,
	body: {
		start_time: string;
		end_time: string;
	},
) {
	return api.post<AuditDeliveryFile>(`/audit/delivery/destinations/${id}/backfill`, body);
}

export function listAuditDeliveryFiles(filters?: {
	organization_id?: string;
	start_time?: string;
	end_time?: string;
	schema_version?: string;
}) {
	const search = new URLSearchParams();
	if (filters?.organization_id) search.set('organization_id', filters.organization_id);
	if (filters?.start_time) search.set('start_time', filters.start_time);
	if (filters?.end_time) search.set('end_time', filters.end_time);
	if (filters?.schema_version) search.set('schema_version', filters.schema_version);
	const query = search.toString();
	return api.get<ListResponse<AuditDeliveryFile>>(`/audit/delivery/files${query ? `?${query}` : ''}`);
}

export function getAuditDeliveryFileContent(id: string) {
	return api.text(`/audit/delivery/files/${id}/content`, {
		headers: { Accept: 'application/x-ndjson' },
	});
}

export function listGovernanceTemplates() {
	return api.get<GovernanceTemplate[]>('/audit/governance/templates');
}

export function applyGovernanceTemplate(
	slug: string,
	body: {
		scope?: string;
		updated_by: string;
	},
) {
	return api.post<ListResponse<AuditPolicy>>(`/audit/governance/templates/${slug}/apply`, body);
}

export function listGovernanceApplications() {
	return api.get<ListResponse<GovernanceTemplateApplication>>('/audit/governance/applications');
}

export function getCompliancePosture() {
	return api.get<CompliancePostureOverview>('/audit/compliance/posture');
}

export function listReports() {
	return api.get<ListResponse<ComplianceReport>>('/audit/reports');
}

export function generateReport(body: {
	standard: ComplianceStandard;
	title: string;
	scope: string;
	window_start: string;
	window_end: string;
}) {
	return api.post<ComplianceReport>('/audit/reports/generate', body);
}

export function scanSensitiveData(body: { content: string; redact?: boolean }) {
	return api.post<SensitiveDataScanResponse>('/audit/sds/scan', body);
}

export function exportSubjectData(body: { subject_id: string; portable_format?: string }) {
	return api.post<GdprExportPayload>('/audit/gdpr/export', body);
}

export function eraseSubjectData(body: { subject_id: string; hard_delete?: boolean; legal_hold?: boolean }) {
	return api.post<GdprEraseResponse>('/audit/gdpr/erase', body);
}
