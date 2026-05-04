import { api } from './client';

const AUTHORING_BASE = '/data-integration';

export type ParamType = 'STRING' | 'INTEGER' | 'FLOAT' | 'BOOLEAN';

export interface Param {
  name: string;
  type: ParamType;
  default_value?: unknown;
  required?: boolean;
}

export interface ParameterizedPipeline {
  id: string;
  pipeline_rid: string;
  deployment_key_param: string;
  output_dataset_rids: string[];
  union_view_dataset_rid: string;
  created_at: string;
  updated_at: string;
}

export interface PipelineDeployment {
  id: string;
  parameterized_pipeline_id: string;
  deployment_key: string;
  parameter_values: Record<string, unknown>;
  created_by: string;
  created_at: string;
}

export function enableParameterized(
  pipelineRid: string,
  body: {
    deployment_key_param: string;
    output_dataset_rids: string[];
    union_view_dataset_rid: string;
  },
) {
  return api.post<ParameterizedPipeline>(
    `${AUTHORING_BASE}/pipelines/${encodeURIComponent(pipelineRid)}/parameterized`,
    body,
  );
}

export function createDeployment(
  parameterizedId: string,
  body: { deployment_key: string; parameter_values: Record<string, unknown> },
) {
  return api.post<PipelineDeployment>(
    `${AUTHORING_BASE}/parameterized-pipelines/${parameterizedId}/deployments`,
    body,
  );
}

export function listDeployments(parameterizedId: string) {
  return api.get<{
    parameterized_pipeline_id: string;
    data: PipelineDeployment[];
    total: number;
  }>(`${AUTHORING_BASE}/parameterized-pipelines/${parameterizedId}/deployments`);
}

export function deleteDeployment(parameterizedId: string, deploymentId: string) {
  return api.delete<void>(
    `${AUTHORING_BASE}/parameterized-pipelines/${parameterizedId}/deployments/${deploymentId}`,
  );
}

export function runDeployment(
  parameterizedId: string,
  deploymentId: string,
  trigger: 'MANUAL' = 'MANUAL',
) {
  return api.post<{
    build_id: string;
    pipeline_rid: string;
    parameter_overrides: Record<string, unknown>;
    deployment_key: string;
  }>(
    `${AUTHORING_BASE}/parameterized-pipelines/${parameterizedId}/deployments/${deploymentId}:run`,
    { trigger },
  );
}
