import api from './client';

export interface ListResponse<T> {
	data: T[];
}

export interface StreamingOverview {
	stream_count: number;
	active_topology_count: number;
	window_count: number;
	connector_count: number;
	running_topology_count: number;
	backpressured_topology_count: number;
	live_event_count: number;
}

export interface StreamField {
	name: string;
	data_type: string;
	nullable: boolean;
	semantic_role: string;
}

export interface StreamSchema {
	fields: StreamField[];
	primary_key: string | null;
	watermark_field: string | null;
}

export interface ConnectorBinding {
	connector_type: string;
	endpoint: string;
	format: string;
	config: Record<string, unknown>;
}

export interface StreamProfile {
	high_throughput: boolean;
	compressed: boolean;
	partitions: number | null;
}

export type StreamType =
	| 'STANDARD'
	| 'HIGH_THROUGHPUT'
	| 'COMPRESSED'
	| 'HIGH_THROUGHPUT_COMPRESSED';

export type StreamConsistency = 'AT_LEAST_ONCE' | 'EXACTLY_ONCE';

export type StreamKind = 'INGEST' | 'DERIVED';

export interface StreamDefinition {
	id: string;
	name: string;
	description: string;
	status: string;
	schema: StreamSchema;
	source_binding: ConnectorBinding;
	retention_hours: number;
	partitions: number;
	consistency_guarantee: string;
	stream_profile: StreamProfile;
	stream_type: StreamType;
	compression: boolean;
	ingest_consistency: StreamConsistency;
	pipeline_consistency: StreamConsistency;
	checkpoint_interval_ms: number;
	kind: StreamKind;
	created_at: string;
	updated_at: string;
}

export interface StreamView {
	id: string;
	stream_rid: string;
	view_rid: string;
	schema_json: unknown | null;
	config_json: unknown | null;
	generation: number;
	active: boolean;
	created_by: string;
	created_at: string;
	retired_at: string | null;
}

export interface ResetStreamResponse {
	stream_rid: string;
	old_view_rid: string;
	new_view_rid: string;
	generation: number;
	view: StreamView;
	push_url: string;
	forced: boolean;
}

export interface PushUrlResponse {
	stream_rid: string;
	view_rid: string;
	generation: number;
	push_url: string;
	note: string;
}

export interface StreamConfig {
	stream_type: StreamType;
	compression: boolean;
	partitions: number;
	retention_ms: number;
	ingest_consistency: StreamConsistency;
	pipeline_consistency: StreamConsistency;
	checkpoint_interval_ms: number;
}

export interface UpdateStreamConfigRequest {
	stream_type?: StreamType;
	compression?: boolean;
	partitions?: number;
	retention_ms?: number;
	ingest_consistency?: StreamConsistency;
	pipeline_consistency?: StreamConsistency;
	checkpoint_interval_ms?: number;
}

export interface PushStreamEventsResponse {
	stream_id: string;
	accepted_events: number;
	dead_lettered_events: number;
	first_sequence_no: number | null;
	last_sequence_no: number | null;
}

export interface StreamingDeadLetter {
	id: string;
	stream_id: string;
	payload: Record<string, unknown>;
	event_time: string;
	reason: string;
	validation_errors: string[];
	status: string;
	replay_count: number;
	last_replayed_at: string | null;
	created_at: string;
	updated_at: string;
}

export interface WindowDefinition {
	id: string;
	name: string;
	description: string;
	status: string;
	window_type: string;
	duration_seconds: number;
	slide_seconds: number;
	session_gap_seconds: number;
	allowed_lateness_seconds: number;
	aggregation_keys: string[];
	measure_fields: string[];
	keyed?: boolean;
	key_columns?: string[];
	state_ttl_seconds?: number;
	created_at: string;
	updated_at: string;
}

export interface TopologyNode {
	id: string;
	label: string;
	node_type: string;
	stream_id: string | null;
	window_id: string | null;
	config: Record<string, unknown>;
}

export interface TopologyEdge {
	source_node_id: string;
	target_node_id: string;
	label: string;
}

export interface JoinDefinition {
	join_type: string;
	left_stream_id: string;
	right_stream_id: string;
	table_name: string;
	key_fields: string[];
	window_seconds: number;
}

export interface CepDefinition {
	pattern_name: string;
	sequence: string[];
	within_seconds: number;
	output_stream: string;
}

export interface BackpressurePolicy {
	max_in_flight: number;
	queue_capacity: number;
	throttle_strategy: string;
}

export interface TopologyDefinition {
	id: string;
	name: string;
	description: string;
	status: string;
	nodes: TopologyNode[];
	edges: TopologyEdge[];
	join_definition: JoinDefinition | null;
	cep_definition: CepDefinition | null;
	backpressure_policy: BackpressurePolicy;
	source_stream_ids: string[];
	sink_bindings: ConnectorBinding[];
	state_backend: string;
	created_at: string;
	updated_at: string;
}

export interface ConnectorCatalogEntry {
	connector_type: string;
	direction: string;
	endpoint: string;
	status: string;
	backlog: number;
	throughput_per_second: number;
	details: Record<string, unknown>;
}

export interface BackpressureSnapshot {
	queue_depth: number;
	queue_capacity: number;
	lag_ms: number;
	throttle_factor: number;
	status: string;
}

export interface StateStoreSnapshot {
	backend: string;
	namespace: string;
	key_count: number;
	disk_usage_mb: number;
	checkpoint_count: number;
	last_checkpoint_at: string;
}

export interface WindowAggregate {
	window_name: string;
	window_type: string;
	bucket_start: string;
	bucket_end: string;
	group_key: string;
	measure_name: string;
	value: number;
}

export interface LiveTailEvent {
	id: string;
	topology_id: string;
	stream_name: string;
	connector_type: string;
	payload: Record<string, unknown>;
	event_time: string;
	processing_time: string;
	tags: string[];
}

export interface CepMatch {
	pattern_name: string;
	matched_sequence: string[];
	confidence: number;
	detected_at: string;
}

export interface TopologyRunMetrics {
	input_events: number;
	output_events: number;
	avg_latency_ms: number;
	p95_latency_ms: number;
	throughput_per_second: number;
	dropped_events: number;
	backpressure_ratio: number;
	join_output_rows: number;
	cep_match_count: number;
	state_entries: number;
}

export interface TopologyRun {
	id: string;
	topology_id: string;
	status: string;
	metrics: TopologyRunMetrics;
	aggregate_windows: WindowAggregate[];
	live_tail: LiveTailEvent[];
	cep_matches: CepMatch[];
	state_snapshot: StateStoreSnapshot;
	backpressure_snapshot: BackpressureSnapshot;
	started_at: string;
	completed_at: string | null;
	created_at: string;
	updated_at: string;
}

export interface TopologyRuntimePreview {
	metrics: TopologyRunMetrics;
	aggregate_windows: WindowAggregate[];
	backpressure_snapshot: BackpressureSnapshot;
	state_snapshot: StateStoreSnapshot;
	backlog_events: number;
	generated_at: string;
}

export interface TopologyRuntimeSnapshot {
	topology: TopologyDefinition;
	latest_run: TopologyRun | null;
	preview: TopologyRuntimePreview | null;
	connector_statuses: ConnectorCatalogEntry[];
	latest_events: LiveTailEvent[];
	latest_matches: CepMatch[];
}

export interface ReplayTopologyResponse {
	topology_id: string;
	stream_ids: string[];
	replay_from_sequence_no: number | null;
	restored_event_count: number;
}

export interface LiveTailResponse {
	events: LiveTailEvent[];
	matches: CepMatch[];
}

export function getOverview() {
	return api.get<StreamingOverview>('/streaming/overview');
}

export function listStreams() {
	return api.get<ListResponse<StreamDefinition>>('/streaming/streams');
}

export function createStream(body: {
	name: string;
	description?: string;
	status?: string;
	schema?: StreamSchema;
	source_binding?: ConnectorBinding;
	retention_hours?: number;
}) {
	return api.post<StreamDefinition>('/streaming/streams', body);
}

export function updateStream(id: string, body: {
	name?: string;
	description?: string;
	status?: string;
	schema?: StreamSchema;
	source_binding?: ConnectorBinding;
	retention_hours?: number;
	partitions?: number;
	consistency_guarantee?: string;
	stream_profile?: StreamProfile;
	stream_type?: StreamType;
	compression?: boolean;
	ingest_consistency?: StreamConsistency;
	pipeline_consistency?: StreamConsistency;
	checkpoint_interval_ms?: number;
}) {
	return api.put<StreamDefinition>(`/streaming/streams/${id}`, body);
}

export function getStreamConfig(id: string) {
	return api.get<StreamConfig>(`/streaming/streams/${id}/config`);
}

export function updateStreamConfig(id: string, body: UpdateStreamConfigRequest) {
	return api.put<StreamConfig>(`/streaming/streams/${id}/config`, body);
}

export function listStreamViews(id: string) {
	return api.get<ListResponse<StreamView>>(`/streaming/streams/${id}/views`);
}

export function getCurrentStreamView(id: string) {
	return api.get<StreamView>(`/streaming/streams/${id}/current-view`);
}

export interface ResetStreamRequest {
	new_schema?: unknown;
	new_config?: unknown;
	force?: boolean;
}

export function resetStream(id: string, body: ResetStreamRequest = {}) {
	return api.post<ResetStreamResponse>(`/streaming/streams/${id}/reset`, body);
}

export function pushStreamEvents(id: string, body: {
	events: Array<{
		payload: Record<string, unknown>;
		event_time?: string;
	}>;
}) {
	return api.post<PushStreamEventsResponse>(`/streaming/streams/${id}/push`, body);
}

export function listDeadLetters(streamId: string) {
	return api.get<ListResponse<StreamingDeadLetter>>(`/streaming/streams/${streamId}/dead-letters`);
}

export function replayDeadLetter(id: string, body?: {
	payload?: Record<string, unknown>;
	event_time?: string;
}) {
	return api.post<{
		dead_letter: StreamingDeadLetter;
		replay_sequence_no: number;
	}>(`/streaming/dead-letters/${id}/replay`, body ?? {});
}

export function listWindows() {
	return api.get<ListResponse<WindowDefinition>>('/streaming/windows');
}

export function createWindow(body: {
	name: string;
	description?: string;
	status?: string;
	window_type?: string;
	duration_seconds?: number;
	slide_seconds?: number;
	session_gap_seconds?: number;
	allowed_lateness_seconds?: number;
	aggregation_keys: string[];
	measure_fields: string[];
}) {
	return api.post<WindowDefinition>('/streaming/windows', body);
}

export function updateWindow(id: string, body: {
	name?: string;
	description?: string;
	status?: string;
	window_type?: string;
	duration_seconds?: number;
	slide_seconds?: number;
	session_gap_seconds?: number;
	allowed_lateness_seconds?: number;
	aggregation_keys?: string[];
	measure_fields?: string[];
}) {
	return api.put<WindowDefinition>(`/streaming/windows/${id}`, body);
}

export function listTopologies() {
	return api.get<ListResponse<TopologyDefinition>>('/streaming/topologies');
}

export function createTopology(body: {
	name: string;
	description?: string;
	status?: string;
	nodes: TopologyNode[];
	edges: TopologyEdge[];
	join_definition?: JoinDefinition | null;
	cep_definition?: CepDefinition | null;
	backpressure_policy?: BackpressurePolicy;
	source_stream_ids: string[];
	sink_bindings: ConnectorBinding[];
	state_backend?: string;
}) {
	return api.post<TopologyDefinition>('/streaming/topologies', body);
}

export function updateTopology(id: string, body: {
	name?: string;
	description?: string;
	status?: string;
	nodes?: TopologyNode[];
	edges?: TopologyEdge[];
	join_definition?: JoinDefinition | null;
	cep_definition?: CepDefinition | null;
	backpressure_policy?: BackpressurePolicy;
	source_stream_ids?: string[];
	sink_bindings?: ConnectorBinding[];
	state_backend?: string;
}) {
	return api.put<TopologyDefinition>(`/streaming/topologies/${id}`, body);
}

export function runTopology(id: string) {
	return api.post<TopologyRun>(`/streaming/topologies/${id}/run`, {});
}

export function replayTopology(id: string, body?: {
	stream_ids?: string[];
	from_sequence_no?: number;
}) {
	return api.post<ReplayTopologyResponse>(`/streaming/topologies/${id}/replay`, body ?? {});
}

export function getRuntime(id: string) {
	return api.get<TopologyRuntimeSnapshot>(`/streaming/topologies/${id}/runtime`);
}

export function listConnectors() {
	return api.get<ListResponse<ConnectorCatalogEntry>>('/streaming/connectors');
}

export function getLiveTail() {
	return api.get<LiveTailResponse>('/streaming/live-tail');
}

// ---- Bloque D — Flink runtime ------------------------------------------

export type DeployFlinkResponse = {
	topology_id: string;
	deployment_name: string | null;
	namespace: string | null;
	sql_warnings: string[];
	sql: string;
	message: string;
};

export type FlinkJobGraph = {
	job_id: string | null;
	vertices: Array<{
		id: string;
		name: string | null;
		parallelism: number | null;
		status: string | null;
	}>;
	edges: Array<{ source: string; target: string }>;
	raw: unknown;
	message?: string;
};

export function deployTopologyToFlink(id: string) {
	return api.post<DeployFlinkResponse>(`/streaming/topologies/${id}/deploy`, {});
}

export function getTopologyJobGraph(id: string) {
	return api.get<FlinkJobGraph>(`/streaming/topologies/${id}/job-graph`);
}

// Bloque E1 — branches
export interface StreamBranch {
	id: string;
	stream_id: string;
	name: string;
	parent_branch_id: string | null;
	status: string;
	head_sequence_no: number;
	dataset_branch_id: string | null;
	description: string | null;
	created_by: string;
	created_at: string;
	archived_at: string | null;
}

export interface CreateBranchRequest {
	name: string;
	parent_branch_id?: string | null;
	description?: string | null;
	dataset_branch_id?: string | null;
}

export interface MergeBranchRequest {
	target_branch?: string;
}

export interface MergeBranchResponse {
	source_branch_id: string;
	target_branch_id: string;
	merged_sequence_no: number;
	message: string;
}

export interface ArchiveBranchRequest {
	commit_cold: boolean;
}

export function listBranches(streamId: string) {
	return api.get<ListResponse<StreamBranch>>(`/streaming/streams/${streamId}/branches`);
}
export function createBranch(streamId: string, body: CreateBranchRequest) {
	return api.post<StreamBranch>(`/streaming/streams/${streamId}/branches`, body);
}
export function deleteBranch(streamId: string, name: string) {
	return api.delete<{ deleted: boolean; id: string }>(
		`/streaming/streams/${streamId}/branches/${encodeURIComponent(name)}`
	);
}
export function mergeBranch(streamId: string, name: string, body: MergeBranchRequest) {
	return api.post<MergeBranchResponse>(
		`/streaming/streams/${streamId}/branches/${encodeURIComponent(name)}/merge`,
		body
	);
}
export function archiveBranch(streamId: string, name: string, body: ArchiveBranchRequest) {
	return api.post<StreamBranch>(
		`/streaming/streams/${streamId}/branches/${encodeURIComponent(name)}/archive`,
		body
	);
}

// Bloque E2 — schema validation + history
export interface CompatibilityOutcome {
	mode: string;
	compatible: boolean;
	reason: string | null;
}
export interface ValidateSchemaRequest {
	schema_avro: unknown;
	sample?: unknown;
	compatibility?: string;
}
export interface ValidateSchemaResponse {
	valid: boolean;
	fingerprint: string | null;
	errors: string[];
	warnings: string[];
	compatibility: CompatibilityOutcome | null;
}
export interface StreamSchemaVersion {
	id: string;
	stream_id: string;
	version: number;
	schema_avro: unknown;
	fingerprint: string;
	compatibility: string;
	created_by: string;
	created_at: string;
}

export function validateSchema(streamId: string, body: ValidateSchemaRequest) {
	return api.post<ValidateSchemaResponse>(
		`/streaming/streams/${streamId}/schema/validate`,
		body
	);
}
export function getSchemaHistory(streamId: string) {
	return api.get<ListResponse<StreamSchemaVersion>>(
		`/streaming/streams/${streamId}/schema/history`
	);
}

// Bloque F2 — checkpoints + runtime
export interface Checkpoint {
	id: string;
	topology_id: string;
	status: string;
	last_offsets: unknown;
	state_uri: string | null;
	savepoint_uri: string | null;
	trigger: string;
	duration_ms: number;
	created_at: string;
}

export interface ResetTopologyResponse {
	topology_id: string;
	runtime_kind: string;
	checkpoint_id: string | null;
	restored_offsets: unknown;
	savepoint_uri: string | null;
	message: string;
}

export function listCheckpoints(topologyId: string, last: number | null = null) {
	const qs = last ? `?last=${last}` : '';
	return api.get<ListResponse<Checkpoint>>(
		`/streaming/topologies/${topologyId}/checkpoints${qs}`
	);
}

// Bloque P4 — stream-monitoring metrics rollup.
export interface StreamMetricsResponse {
	stream_id: string;
	window_seconds: number;
	records_ingested: number;
	records_output: number;
	total_lag: number;
	total_throughput: number;
	utilization_pct: number;
	from: string;
	to: string;
}

export function getStreamMetrics(streamId: string, window: '5m' | '30m' | string = '5m') {
	return api.get<StreamMetricsResponse>(
		`/streaming/streams/${streamId}/metrics?window=${encodeURIComponent(window)}`
	);
}

// Bloque P6 — streaming compute usage rollup.
export interface UsageBucket {
	bucket_start: string;
	compute_seconds: number;
	records_processed: number;
}

export interface UsageResponse {
	from: string;
	to: string;
	group: 'hour' | 'day';
	buckets: UsageBucket[];
	total_compute_seconds: number;
	total_records_processed: number;
}

export function getStreamUsage(
	streamId: string,
	options: { from?: string; to?: string; group?: 'hour' | 'day' } = {}
) {
	const params = new URLSearchParams();
	if (options.from) params.set('from', options.from);
	if (options.to) params.set('to', options.to);
	if (options.group) params.set('group', options.group);
	const qs = params.toString();
	return api.get<UsageResponse>(
		`/streaming/streams/${streamId}/usage${qs ? `?${qs}` : ''}`
	);
}

export function getTopologyUsage(
	topologyId: string,
	options: { from?: string; to?: string; group?: 'hour' | 'day' } = {}
) {
	const params = new URLSearchParams();
	if (options.from) params.set('from', options.from);
	if (options.to) params.set('to', options.to);
	if (options.group) params.set('group', options.group);
	const qs = params.toString();
	return api.get<UsageResponse>(
		`/streaming/topologies/${topologyId}/usage${qs ? `?${qs}` : ''}`
	);
}

// Bloque P5 — hybrid hot/cold preview.
export type PreviewMode = 'oldest' | 'hot_only' | 'cold_only';

export interface PreviewRow {
	sequence_no: number | null;
	event_time: string;
	payload: unknown;
	/** Per-record source. The UI keys the "live"/"archived" badge off
	 * this field. */
	source: 'hot' | 'cold';
	snapshot_id: string | null;
	parquet_path: string | null;
}

export interface PreviewResponse {
	/** Aggregate label so the UI knows whether the response is purely
	 * hot, purely cold, or a hybrid mix. */
	source: 'hot' | 'cold' | 'hybrid';
	data: PreviewRow[];
}

export function previewStream(
	streamId: string,
	options: { mode?: PreviewMode; limit?: number; from_offset?: number } = {}
) {
	const params = new URLSearchParams();
	if (options.mode) params.set('from', options.mode);
	if (options.limit !== undefined) params.set('limit', String(options.limit));
	if (options.from_offset !== undefined)
		params.set('from_offset', String(options.from_offset));
	const qs = params.toString();
	return api.get<PreviewResponse>(
		`/streaming/streams/${streamId}/preview${qs ? `?${qs}` : ''}`
	);
}
export function triggerCheckpoint(
	topologyId: string,
	body: { trigger?: string; export_savepoint?: boolean } = {}
) {
	return api.post<Checkpoint>(`/streaming/topologies/${topologyId}/checkpoints`, body);
}
export function resetTopology(
	topologyId: string,
	body: { from_checkpoint_id?: string; savepoint_uri?: string }
) {
	return api.post<ResetTopologyResponse>(
		`/streaming/topologies/${topologyId}/reset`,
		body
	);
}

// Bloque P3 — Streaming profiles
export type ProfileCategory =
	| 'TASKMANAGER_RESOURCES'
	| 'JOBMANAGER_RESOURCES'
	| 'PARALLELISM'
	| 'NETWORK'
	| 'CHECKPOINTING'
	| 'ADVANCED';

export type ProfileSizeClass = 'SMALL' | 'MEDIUM' | 'LARGE';

export interface StreamingProfile {
	id: string;
	name: string;
	description: string;
	category: ProfileCategory;
	size_class: ProfileSizeClass;
	restricted: boolean;
	config_json: Record<string, string>;
	version: number;
	created_by: string;
	created_at: string;
	updated_at: string;
}

export interface StreamingProfileProjectRef {
	project_rid: string;
	profile_id: string;
	imported_by: string;
	imported_at: string;
	imported_order: number;
}

export interface PipelineProfileAttachment {
	pipeline_rid: string;
	profile_id: string;
	attached_by: string;
	attached_at: string;
	attached_order: number;
}

export interface EffectiveFlinkConfig {
	pipeline_rid: string;
	config: Record<string, string>;
	source_map: Record<string, string>;
	warnings: string[];
}

export interface CreateStreamingProfileRequest {
	name: string;
	description?: string;
	category: ProfileCategory;
	size_class: ProfileSizeClass;
	restricted?: boolean;
	config_json: Record<string, string>;
}

export interface PatchStreamingProfileRequest {
	name?: string;
	description?: string;
	category?: ProfileCategory;
	size_class?: ProfileSizeClass;
	restricted?: boolean;
	config_json?: Record<string, string>;
}

export function listStreamingProfiles(filters: { category?: ProfileCategory; size_class?: ProfileSizeClass } = {}) {
	const params = new URLSearchParams();
	if (filters.category) params.set('category', filters.category);
	if (filters.size_class) params.set('size_class', filters.size_class);
	const qs = params.toString();
	const url = qs ? `/streaming/streaming-profiles?${qs}` : '/streaming/streaming-profiles';
	return api.get<ListResponse<StreamingProfile>>(url);
}

export function createStreamingProfile(body: CreateStreamingProfileRequest) {
	return api.post<StreamingProfile>('/streaming/streaming-profiles', body);
}

export function patchStreamingProfile(id: string, body: PatchStreamingProfileRequest) {
	return api.patch<StreamingProfile>(`/streaming/streaming-profiles/${id}`, body);
}

export function listStreamingProfileProjectRefs(profileId: string) {
	return api.get<ListResponse<StreamingProfileProjectRef>>(
		`/streaming/streaming-profiles/${profileId}/project-refs`
	);
}

export function importStreamingProfileToProject(projectRid: string, profileId: string) {
	return api.post<StreamingProfileProjectRef>(
		`/streaming/projects/${encodeURIComponent(projectRid)}/streaming-profile-refs/${profileId}`,
		{}
	);
}

export function removeStreamingProfileFromProject(projectRid: string, profileId: string) {
	return api.delete<{ removed: boolean; warning: string }>(
		`/streaming/projects/${encodeURIComponent(projectRid)}/streaming-profile-refs/${profileId}`
	);
}

export function listPipelineStreamingProfiles(pipelineRid: string) {
	return api.get<ListResponse<StreamingProfile>>(
		`/streaming/pipelines/${encodeURIComponent(pipelineRid)}/streaming-profiles`
	);
}

export function attachProfileToPipeline(pipelineRid: string, body: { project_rid: string; profile_id: string }) {
	return api.post<PipelineProfileAttachment>(
		`/streaming/pipelines/${encodeURIComponent(pipelineRid)}/streaming-profiles`,
		body
	);
}

export function detachProfileFromPipeline(pipelineRid: string, profileId: string) {
	return api.delete<{ detached: boolean }>(
		`/streaming/pipelines/${encodeURIComponent(pipelineRid)}/streaming-profiles/${profileId}`
	);
}

export function getPipelineEffectiveFlinkConfig(pipelineRid: string) {
	return api.get<EffectiveFlinkConfig>(
		`/streaming/pipelines/${encodeURIComponent(pipelineRid)}/effective-flink-config`
	);
}
