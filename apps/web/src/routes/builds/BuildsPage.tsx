import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';

import { listBuildsV1, type Build, type BuildState, type CreateBuildRequest, type ListBuildsParams } from '@/lib/api/buildsV1';
import { runBuildWithExpectationGates } from '@/lib/api/data-expectations';
import { AbortAction, isBuildAbortable } from '@/lib/components/builds/AbortAction';
import { StateBadge } from '@/lib/components/builds/StateBadge';

const BUILD_STATES: BuildState[] = [
  'BUILD_RESOLUTION',
  'BUILD_QUEUED',
  'BUILD_RUNNING',
  'BUILD_ABORTING',
  'BUILD_FAILED',
  'BUILD_ABORTED',
  'BUILD_COMPLETED',
];

const IN_FLIGHT_STATES = new Set<BuildState>([
  'BUILD_RESOLUTION',
  'BUILD_QUEUED',
  'BUILD_RUNNING',
  'BUILD_ABORTING',
]);

const DEFAULT_CREATE_BODY = JSON.stringify(
  {
    pipeline_rid: 'ri.pipeline.example',
    build_branch: 'master',
    output_dataset_rids: ['ri.dataset.example'],
    job_spec_fallback: ['master'],
    force_build: false,
    trigger_kind: 'MANUAL',
    abort_policy: 'DEPENDENT_ONLY',
  },
  null,
  2,
);

interface BuildListItem extends Build {
  jobs?: unknown[];
}

interface BuildFilters {
  state: BuildState | '';
  branch: string;
  pipelineRid: string;
  since: string;
  until: string;
}

function emptyFilters(): BuildFilters {
  return {
    state: '',
    branch: '',
    pipelineRid: '',
    since: '',
    until: '',
  };
}

function normalizeState(value: string | null): BuildState | '' {
  if (!value) return '';
  return BUILD_STATES.includes(value as BuildState) ? (value as BuildState) : '';
}

function toLocalInputValue(value: string | null): string {
  if (!value) return '';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return '';
  const local = new Date(date.getTime() - date.getTimezoneOffset() * 60_000);
  return local.toISOString().slice(0, 16);
}

function toApiDateTime(value: string): string | undefined {
  if (!value) return undefined;
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toISOString();
}

function filtersFromSearchParams(searchParams: URLSearchParams): BuildFilters {
  return {
    state: normalizeState(searchParams.get('state') ?? searchParams.get('status')),
    branch: searchParams.get('branch') ?? '',
    pipelineRid: searchParams.get('pipeline_rid') ?? '',
    since: toLocalInputValue(searchParams.get('since')),
    until: toLocalInputValue(searchParams.get('until')),
  };
}

function filtersToSearchParams(filters: BuildFilters, keepRunOpen: boolean): URLSearchParams {
  const next = new URLSearchParams();
  if (filters.state) next.set('state', filters.state);
  if (filters.branch.trim()) next.set('branch', filters.branch.trim());
  if (filters.pipelineRid.trim()) next.set('pipeline_rid', filters.pipelineRid.trim());
  const since = toApiDateTime(filters.since);
  const until = toApiDateTime(filters.until);
  if (since) next.set('since', since);
  if (until) next.set('until', until);
  if (keepRunOpen) next.set('run', '1');
  return next;
}

function toListParams(filters: BuildFilters): ListBuildsParams {
  return {
    status: filters.state || undefined,
    branch: filters.branch.trim() || undefined,
    pipeline_rid: filters.pipelineRid.trim() || undefined,
    since: toApiDateTime(filters.since),
    until: toApiDateTime(filters.until),
    limit: 100,
  };
}

function formatRid(rid: string, length = 18): string {
  if (!rid) return '-';
  return rid.length > length ? `${rid.slice(0, length)}...` : rid;
}

function formatDate(value: string | null | undefined): string {
  if (!value) return '-';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

function formatDuration(start: string | null | undefined, finish: string | null | undefined): string {
  if (!start) return '-';
  const startDate = new Date(start);
  const finishDate = finish ? new Date(finish) : new Date();
  if (Number.isNaN(startDate.getTime()) || Number.isNaN(finishDate.getTime())) return '-';
  const seconds = Math.max(0, Math.floor((finishDate.getTime() - startDate.getTime()) / 1000));
  if (seconds < 60) return `${seconds}s`;
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m`;
  const hours = Math.floor(minutes / 60);
  return `${hours}h ${minutes % 60}m`;
}

function buildJobsCount(build: BuildListItem): string {
  return Array.isArray(build.jobs) ? String(build.jobs.length) : '-';
}

function parseCreateBody(body: string): CreateBuildRequest {
  const parsed = JSON.parse(body) as Partial<CreateBuildRequest>;
  if (!parsed.pipeline_rid || !parsed.build_branch) {
    throw new Error('Build request requires pipeline_rid and build_branch.');
  }
  if (!Array.isArray(parsed.output_dataset_rids) || parsed.output_dataset_rids.length === 0) {
    throw new Error('Build request requires at least one output_dataset_rid.');
  }
  return parsed as CreateBuildRequest;
}

export function BuildsPage() {
  const [searchParams, setSearchParams] = useSearchParams();
  const searchKey = searchParams.toString();

  const [builds, setBuilds] = useState<BuildListItem[]>([]);
  const [activeFilters, setActiveFilters] = useState<BuildFilters>(() => filtersFromSearchParams(searchParams));
  const [stateFilter, setStateFilter] = useState<BuildState | ''>(() => activeFilters.state);
  const [branchFilter, setBranchFilter] = useState(() => activeFilters.branch);
  const [pipelineFilter, setPipelineFilter] = useState(() => activeFilters.pipelineRid);
  const [sinceFilter, setSinceFilter] = useState(() => activeFilters.since);
  const [untilFilter, setUntilFilter] = useState(() => activeFilters.until);
  const [loading, setLoading] = useState(true);
  const [creating, setCreating] = useState(false);
  const [error, setError] = useState('');
  const [feedback, setFeedback] = useState('');
  const [createOpen, setCreateOpen] = useState(() => searchParams.get('run') === '1');
  const [createBody, setCreateBody] = useState(DEFAULT_CREATE_BODY);

  const refresh = useCallback(async (filters: BuildFilters) => {
    setLoading(true);
    setError('');
    try {
      const res = await listBuildsV1(toListParams(filters));
      setBuilds(res.data as BuildListItem[]);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to load builds');
    } finally {
      setLoading(false);
    }
  }, []);

  useEffect(() => {
    const params = new URLSearchParams(searchKey);
    const nextFilters = filtersFromSearchParams(params);
    setActiveFilters(nextFilters);
    setStateFilter(nextFilters.state);
    setBranchFilter(nextFilters.branch);
    setPipelineFilter(nextFilters.pipelineRid);
    setSinceFilter(nextFilters.since);
    setUntilFilter(nextFilters.until);
    if (params.get('run') === '1') setCreateOpen(true);
    void refresh(nextFilters);
  }, [refresh, searchKey]);

  const stats = useMemo(() => {
    const failed = builds.filter((build) => build.state === 'BUILD_FAILED').length;
    const completed = builds.filter((build) => build.state === 'BUILD_COMPLETED').length;
    const aborted = builds.filter((build) => build.state === 'BUILD_ABORTED').length;
    const inFlight = builds.filter((build) => IN_FLIGHT_STATES.has(build.state)).length;
    return { total: builds.length, inFlight, completed, failed, aborted };
  }, [builds]);

  const activeFilterCount = [
    activeFilters.state,
    activeFilters.branch,
    activeFilters.pipelineRid,
    activeFilters.since,
    activeFilters.until,
  ].filter(Boolean).length;

  function filtersFromInputs(): BuildFilters {
    return {
      state: stateFilter,
      branch: branchFilter,
      pipelineRid: pipelineFilter,
      since: sinceFilter,
      until: untilFilter,
    };
  }

  function applyFilters() {
    setSearchParams(filtersToSearchParams(filtersFromInputs(), createOpen), { replace: true });
  }

  function clearFilters() {
    const cleared = emptyFilters();
    setStateFilter(cleared.state);
    setBranchFilter(cleared.branch);
    setPipelineFilter(cleared.pipelineRid);
    setSinceFilter(cleared.since);
    setUntilFilter(cleared.until);
    setSearchParams(filtersToSearchParams(cleared, createOpen), { replace: true });
  }

  function openCreate() {
    setCreateOpen(true);
    const next = new URLSearchParams(searchParams);
    next.set('run', '1');
    setSearchParams(next, { replace: true });
  }

  function closeCreate() {
    setCreateOpen(false);
    const next = new URLSearchParams(searchParams);
    next.delete('run');
    setSearchParams(next, { replace: true });
  }

  async function handleCreate() {
    setCreating(true);
    setError('');
    setFeedback('');
    try {
      const request = parseCreateBody(createBody);
      const response = await runBuildWithExpectationGates(request);
      if (response.state === 'BUILD_ABORTED') {
        setFeedback(`Build ${response.build_id} aborted by data expectation gates: ${response.queued_reason}`);
      } else {
        setFeedback(`Build ${response.build_id} accepted with ${response.job_count} job${response.job_count === 1 ? '' : 's'}.`);
      }
      await refresh(activeFilters);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Create failed');
    } finally {
      setCreating(false);
    }
  }

  return (
    <section className="of-page" style={{ display: 'grid', gap: 10 }}>
      <header className="of-panel" style={{ display: 'grid', gap: 10, padding: 12 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 12, flexWrap: 'wrap' }}>
          <div style={{ minWidth: 280 }}>
            <p className="of-eyebrow">Operations</p>
            <h1 className="of-heading-xl" style={{ marginTop: 4 }}>Builds</h1>
            <p className="of-text-muted" style={{ margin: '4px 0 0', maxWidth: 760 }}>
              Cross-pipeline build history with state filters, run detail navigation, and abort controls for active runs.
            </p>
          </div>
          <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', justifyContent: 'flex-end' }}>
            <button type="button" onClick={() => void refresh(activeFilters)} disabled={loading} className="of-button">
              {loading ? 'Refreshing...' : 'Refresh'}
            </button>
            <button type="button" onClick={openCreate} className="of-button of-button--primary">
              Run build
            </button>
          </div>
        </div>

        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(130px, 1fr))', borderTop: '1px solid var(--border-subtle)', borderLeft: '1px solid var(--border-subtle)' }}>
          {[
            ['Total', stats.total],
            ['In flight', stats.inFlight],
            ['Completed', stats.completed],
            ['Failed', stats.failed],
            ['Aborted', stats.aborted],
          ].map(([label, value]) => (
            <div key={label} style={{ padding: '9px 10px', borderRight: '1px solid var(--border-subtle)', borderBottom: '1px solid var(--border-subtle)' }}>
              <p className="of-eyebrow">{label}</p>
              <strong style={{ display: 'block', marginTop: 4, color: 'var(--text-strong)', fontSize: 18 }}>{value}</strong>
            </div>
          ))}
        </div>
      </header>

      {error && (
        <div className="of-status-danger" style={{ padding: '10px 14px', borderRadius: 'var(--radius-md)', fontSize: 13 }}>
          {error}
        </div>
      )}

      {feedback && (
        <div className="of-status-success" style={{ padding: '10px 14px', borderRadius: 'var(--radius-md)', fontSize: 13 }}>
          {feedback}
        </div>
      )}

      <form
        className="of-toolbar"
        style={{ justifyContent: 'space-between', flexWrap: 'wrap', alignItems: 'flex-end' }}
        onSubmit={(event) => {
          event.preventDefault();
          applyFilters();
        }}
      >
        <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, minWidth: 0, alignItems: 'flex-end' }}>
          <label style={{ display: 'grid', gap: 3, fontSize: 11, fontWeight: 700, color: 'var(--text-muted)' }}>
            State
            <select value={stateFilter} onChange={(e) => setStateFilter(e.target.value as BuildState | '')} className="of-select" style={{ width: 180 }}>
              <option value="">All states</option>
              {BUILD_STATES.map((state) => (
                <option key={state} value={state}>{state}</option>
              ))}
            </select>
          </label>
          <label style={{ display: 'grid', gap: 3, fontSize: 11, fontWeight: 700, color: 'var(--text-muted)' }}>
            Branch
            <input
              value={branchFilter}
              onChange={(e) => setBranchFilter(e.target.value)}
              placeholder="master"
              className="of-input"
              style={{ width: 150 }}
            />
          </label>
          <label style={{ display: 'grid', gap: 3, fontSize: 11, fontWeight: 700, color: 'var(--text-muted)' }}>
            Pipeline RID
            <input
              value={pipelineFilter}
              onChange={(e) => setPipelineFilter(e.target.value)}
              placeholder="ri.pipeline..."
              className="of-input"
              style={{ width: 220, fontFamily: 'var(--font-mono)', fontSize: 12 }}
            />
          </label>
          <label style={{ display: 'grid', gap: 3, fontSize: 11, fontWeight: 700, color: 'var(--text-muted)' }}>
            Since
            <input
              type="datetime-local"
              value={sinceFilter}
              onChange={(e) => setSinceFilter(e.target.value)}
              className="of-input"
              style={{ width: 180 }}
            />
          </label>
          <label style={{ display: 'grid', gap: 3, fontSize: 11, fontWeight: 700, color: 'var(--text-muted)' }}>
            Until
            <input
              type="datetime-local"
              value={untilFilter}
              onChange={(e) => setUntilFilter(e.target.value)}
              className="of-input"
              style={{ width: 180 }}
            />
          </label>
          <button type="submit" className="of-button" disabled={loading}>
            {loading ? 'Applying...' : 'Apply'}
          </button>
          <button type="button" onClick={clearFilters} className="of-button of-button--ghost" disabled={loading || activeFilterCount === 0}>
            Clear
          </button>
        </div>
        <span className="of-text-muted" style={{ fontSize: 12 }}>
          GET /v1/builds
        </span>
      </form>

      <section className="of-panel" style={{ overflow: 'hidden' }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 10, padding: '10px 12px', borderBottom: '1px solid var(--border-default)' }}>
          <div>
            <p className="of-eyebrow">Build queue ({builds.length})</p>
            <p className="of-text-muted" style={{ margin: '3px 0 0', fontSize: 12 }}>
              Open a row for job resolution, outputs, and input details.
            </p>
          </div>
          {activeFilterCount > 0 && (
            <span className="of-chip of-chip-active">
              {activeFilterCount} filter{activeFilterCount === 1 ? '' : 's'}
            </span>
          )}
        </div>

        {loading ? (
          <p className="of-text-muted" style={{ margin: 0, padding: 14 }}>Loading builds...</p>
        ) : builds.length === 0 ? (
          <div style={{ display: 'grid', justifyItems: 'start', gap: 8, padding: 16 }}>
            <p className="of-heading-sm">No builds match these filters.</p>
            <p className="of-text-muted" style={{ margin: 0, fontSize: 12 }}>
              Clear the filters or trigger a build request to populate the history.
            </p>
            <button type="button" onClick={openCreate} className="of-button of-button--primary">
              Run build
            </button>
          </div>
        ) : (
          <div style={{ overflowX: 'auto' }}>
            <table className="of-table">
              <thead>
                <tr>
                  <th>Build</th>
                  <th>State</th>
                  <th>Branch</th>
                  <th>Pipeline</th>
                  <th>Trigger</th>
                  <th>Jobs</th>
                  <th>Started</th>
                  <th>Duration</th>
                  <th>Requested by</th>
                  <th />
                </tr>
              </thead>
              <tbody>
                {builds.map((build) => (
                  <tr key={build.rid} data-abortable={isBuildAbortable(build.state) ? 'true' : undefined}>
                    <td style={{ minWidth: 210 }}>
                      <Link to={`/builds/${encodeURIComponent(build.rid)}`} style={{ fontWeight: 700, fontFamily: 'var(--font-mono)' }} title={build.rid}>
                        {formatRid(build.rid)}
                      </Link>
                      <p className="of-text-muted" style={{ margin: '3px 0 0', fontSize: 11, fontFamily: 'var(--font-mono)' }}>
                        {build.id ? formatRid(build.id, 14) : '-'}
                      </p>
                    </td>
                    <td><StateBadge kind="build" state={build.state} /></td>
                    <td>{build.build_branch || '-'}</td>
                    <td style={{ minWidth: 170, fontFamily: 'var(--font-mono)', fontSize: 12 }} title={build.pipeline_rid}>
                      {formatRid(build.pipeline_rid, 16)}
                    </td>
                    <td>{build.trigger_kind}</td>
                    <td>{buildJobsCount(build)}</td>
                    <td>{formatDate(build.started_at ?? build.queued_at ?? build.created_at)}</td>
                    <td>{formatDuration(build.started_at ?? build.queued_at ?? build.created_at, build.finished_at)}</td>
                    <td style={{ maxWidth: 180 }}>
                      <span title={build.requested_by} style={{ display: 'inline-block', maxWidth: 180, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                        {build.requested_by || '-'}
                      </span>
                    </td>
                    <td style={{ textAlign: 'right', whiteSpace: 'nowrap' }}>
                      <div style={{ display: 'inline-flex', justifyContent: 'flex-end', gap: 6 }}>
                        <Link to={`/builds/${encodeURIComponent(build.rid)}`} className="of-button" style={{ fontSize: 11 }}>
                          Open
                        </Link>
                        {(isBuildAbortable(build.state) || build.state === 'BUILD_ABORTING') && (
                          <AbortAction
                            rid={build.rid}
                            state={build.state}
                            disabled={loading}
                            onAborted={async (nextState) => {
                              setFeedback(`Abort requested for ${formatRid(build.rid)} (${nextState}).`);
                              await refresh(activeFilters);
                            }}
                            onError={(message) => setError(message)}
                          />
                        )}
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </section>

      {createOpen && (
        <section className="of-panel" style={{ padding: 12, display: 'grid', gap: 10 }}>
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 12, flexWrap: 'wrap' }}>
            <div>
              <p className="of-eyebrow">Run build</p>
              <h2 className="of-heading-sm" style={{ marginTop: 4 }}>Create build request</h2>
              <p className="of-text-muted" style={{ margin: '4px 0 0', fontSize: 12 }}>
                POST /v1/builds with a Foundry-aligned build request body.
              </p>
            </div>
            <button type="button" onClick={closeCreate} className="of-button of-button--ghost">
              Close
            </button>
          </div>
          <textarea
            value={createBody}
            onChange={(e) => setCreateBody(e.target.value)}
            className="of-textarea"
            spellCheck={false}
            style={{ fontFamily: 'var(--font-mono)', fontSize: 12, minHeight: 180 }}
          />
          <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', justifyContent: 'space-between' }}>
            <button type="button" onClick={() => setCreateBody(DEFAULT_CREATE_BODY)} className="of-button">
              Reset example
            </button>
            <button type="button" onClick={() => void handleCreate()} disabled={creating} className="of-button of-button--primary">
              {creating ? 'Submitting...' : 'Submit build'}
            </button>
          </div>
        </section>
      )}
    </section>
  );
}
