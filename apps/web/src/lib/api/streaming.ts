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
	created_at: string;
	updated_at: string;
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
}) {
	return api.put<StreamDefinition>(`/streaming/streams/${id}`, body);
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

export function listCheckpoints(topologyId: string) {
	return api.get<ListResponse<Checkpoint>>(
		`/streaming/topologies/${topologyId}/checkpoints`
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
