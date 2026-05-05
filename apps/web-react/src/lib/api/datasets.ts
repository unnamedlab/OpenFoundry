import api from './client';

// ────────────────────────────────────────────────────────────────
// Jobspec slice — minimal surface needed by /lineage. Full datasets
// API (CRUD, branches, files, etc.) will be added with the routes
// that consume it.
// ────────────────────────────────────────────────────────────────

export interface DatasetJobSpecStatus {
  has_master_jobspec: boolean;
  branches_with_jobspec: string[];
}

export interface DatasetJobSpecRow {
  id: string;
  rid: string;
  pipeline_rid: string;
  branch_name: string;
  output_dataset_rid: string;
  output_branch: string;
  job_spec_json: unknown;
  inputs: unknown;
  content_hash: string;
}

export function listDatasetJobSpecs(
  datasetRid: string,
  params?: { on_branch?: string },
) {
  const query = new URLSearchParams();
  if (params?.on_branch) query.set('on_branch', params.on_branch);
  const qs = query.toString();
  return api.get<DatasetJobSpecRow[]>(
    `/datasets/${encodeURIComponent(datasetRid)}/job-specs${qs ? `?${qs}` : ''}`,
  );
}

/**
 * Roll-up "is there a JobSpec on master?" — used by /lineage to colour
 * datasets blue (has master spec) vs grey (no spec). Falls back to
 * `false` on transient errors so a network blip doesn't flip the whole
 * graph to grey.
 */
export async function loadJobSpecStatus(datasetRid: string): Promise<DatasetJobSpecStatus> {
  try {
    const rows = await listDatasetJobSpecs(datasetRid);
    const branches = Array.from(new Set(rows.map((r) => r.branch_name)));
    return {
      has_master_jobspec: branches.includes('master'),
      branches_with_jobspec: branches.sort(),
    };
  } catch {
    return { has_master_jobspec: false, branches_with_jobspec: [] };
  }
}
