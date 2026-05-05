import api from './client';

// ────────────────────────────────────────────────────────────────
// Lineage slice — types + endpoints used by /lineage. Other pipeline
// surfaces (CRUD on pipelines, runs, schedules) will be added when
// the routes that need them are migrated.
// ────────────────────────────────────────────────────────────────

export interface LineageNode {
  id: string;
  kind: string;
  label: string;
  marking: string;
  metadata: Record<string, unknown>;
}

export interface LineageEdge {
  id: string;
  source: string;
  source_kind: string;
  target: string;
  target_kind: string;
  relation_kind: string;
  pipeline_id: string | null;
  workflow_id: string | null;
  node_id: string | null;
  step_id: string | null;
  effective_marking: string;
  metadata: Record<string, unknown>;
}

export interface LineageGraph {
  nodes: LineageNode[];
  edges: LineageEdge[];
}

export interface LineagePathHop {
  source_id: string;
  source_kind: string;
  target_id: string;
  target_kind: string;
  relation_kind: string;
  effective_marking: string;
}

export interface LineageImpactItem {
  id: string;
  kind: string;
  label: string;
  distance: number;
  marking: string;
  effective_marking: string;
  requires_acknowledgement: boolean;
  metadata: Record<string, unknown>;
  path: LineagePathHop[];
}

export interface LineageBuildCandidate {
  id: string;
  kind: string;
  label: string;
  status: string | null;
  distance: number;
  triggerable: boolean;
  marking: string;
  effective_marking: string;
  requires_acknowledgement: boolean;
  blocked_reason: string | null;
  metadata: Record<string, unknown>;
}

export interface LineageImpactAnalysis {
  root: LineageNode;
  propagated_marking: string;
  upstream: LineageImpactItem[];
  downstream: LineageImpactItem[];
  build_candidates: LineageBuildCandidate[];
}

export interface LineageBuildTriggerResult {
  id: string;
  kind: string;
  label: string;
  run_id: string | null;
  status: string;
  message: string | null;
}

export interface LineageBuildResult {
  root: LineageNode;
  dry_run: boolean;
  acknowledged_sensitive_lineage: boolean;
  propagated_marking: string;
  candidates: LineageBuildCandidate[];
  triggered: LineageBuildTriggerResult[];
  skipped: LineageBuildTriggerResult[];
}

export function getFullLineage() {
  return api.get<LineageGraph>('/lineage');
}

export function getDatasetLineageImpact(datasetId: string) {
  return api.get<LineageImpactAnalysis>(`/lineage/datasets/${datasetId}/impact`);
}

export function triggerLineageBuilds(
  datasetId: string,
  body?: {
    include_workflows?: boolean;
    dry_run?: boolean;
    acknowledge_sensitive_lineage?: boolean;
    context?: Record<string, unknown>;
  },
) {
  return api.post<LineageBuildResult>(`/lineage/datasets/${datasetId}/builds`, body ?? {});
}
