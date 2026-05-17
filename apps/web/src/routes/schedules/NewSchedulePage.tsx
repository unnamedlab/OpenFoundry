import { useEffect, useMemo, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';

import { createSchedule, listSchedules, type Schedule, type ScheduleScopeKind, type ScheduleTarget, type Trigger } from '@/lib/api/schedules';
import { getDatasetHealth, type DatasetHealthResponse } from '@/lib/api/datasets';
import { getFullLineage, type LineageGraph, type LineageNode } from '@/lib/api/pipelines';
import { notifications } from '@/lib/stores/notifications';

type TriggerKind = 'time' | 'event';
type BuildStrategy = 'STALE_ONLY' | 'FORCE';
type TargetStrategy = 'single' | 'with_dependencies' | 'descendants' | 'connecting';

interface TargetSelection {
  id: string;
  strategy: TargetStrategy;
  datasetRid: string;
  inputDatasetRid: string;
  targetDatasetRid: string;
}

interface TargetPreviewRow {
  nodeId: string;
  datasetRid: string;
  label: string;
  strategy: TargetStrategy;
  reason: string;
  distance: number;
}

interface TargetPreview {
  rows: TargetPreviewRow[];
  warnings: string[];
}

type GuardrailSeverity = 'info' | 'warning' | 'critical';

interface ScheduleGuardrail {
  code: string;
  severity: GuardrailSeverity;
  title: string;
  message: string;
  recommendation: string;
}

interface TargetHealthProbe {
  rid: string;
  status: 'checked' | 'missing' | 'error';
  checkCount: number | null;
  message?: string;
}

const TARGET_STRATEGIES: Array<{ value: TargetStrategy; label: string; hint: string }> = [
  { value: 'single', label: 'One dataset', hint: 'Build only the selected dataset.' },
  { value: 'with_dependencies', label: 'Dataset plus dependencies', hint: 'Build the dataset and every upstream dataset in the graph.' },
  { value: 'descendants', label: 'All descendants', hint: 'Build datasets that depend on the selected dataset.' },
  { value: 'connecting', label: 'Connecting datasets', hint: 'Build datasets on directed paths from input to target.' },
];

const BROAD_TARGET_THRESHOLD = 20;
const HEALTH_PROBE_LIMIT = 12;

function makeTargetSelection(seed = ''): TargetSelection {
  return {
    id: `target-${Date.now()}-${Math.random().toString(36).slice(2, 8)}`,
    strategy: 'single',
    datasetRid: seed,
    inputDatasetRid: seed,
    targetDatasetRid: '',
  };
}

function metadataString(metadata: Record<string, unknown>, key: string) {
  const value = metadata[key];
  return typeof value === 'string' && value.trim() ? value.trim() : '';
}

function parseRIDList(value: string) {
  return value
    .split(/[\n,]/)
    .map((entry) => entry.trim())
    .filter(Boolean);
}

function datasetRidForNode(node: LineageNode) {
  return metadataString(node.metadata ?? {}, 'rid') || metadataString(node.metadata ?? {}, 'dataset_rid') || node.id;
}

function findDatasetNode(graph: LineageGraph | null, rid: string) {
  const needle = rid.trim();
  if (!needle || !graph) return null;
  return graph.nodes.find((node) => (
    node.kind === 'dataset' &&
    (node.id === needle || datasetRidForNode(node) === needle)
  )) ?? null;
}

function sortedUniquePreviewRows(rows: TargetPreviewRow[]) {
  const byRid = new Map<string, TargetPreviewRow>();
  for (const row of rows) {
    const existing = byRid.get(row.datasetRid);
    if (!existing || row.distance < existing.distance) byRid.set(row.datasetRid, row);
  }
  return [...byRid.values()].sort((a, b) => a.distance - b.distance || a.label.localeCompare(b.label));
}

function healthCheckCount(health: DatasetHealthResponse | null) {
  if (!health) return null;
  const extras = health.extras ?? {};
  for (const key of ['active_check_count', 'health_check_count', 'check_count', 'checks_count']) {
    const value = extras[key];
    if (typeof value === 'number' && Number.isFinite(value)) return value;
  }
  for (const key of ['checks', 'health_checks', 'policies']) {
    const value = extras[key];
    if (Array.isArray(value)) return value.length;
  }
  return null;
}

function isProductionSchedule(name: string, projectRid: string, branch: string) {
  const haystack = `${name} ${projectRid} ${branch}`.toLowerCase();
  return /\b(prod|production)\b/.test(haystack) || ['main', 'master', 'production', 'prod'].includes(branch.trim().toLowerCase());
}

function guardrailTone(severity: GuardrailSeverity) {
  if (severity === 'critical') return 'of-status-danger';
  if (severity === 'warning') return 'of-status-warning';
  return 'of-status-info';
}

function buildAdjacency(graph: LineageGraph, direction: 'incoming' | 'outgoing') {
  const map = new Map<string, string[]>();
  for (const edge of graph.edges) {
    const from = direction === 'incoming' ? edge.target : edge.source;
    const to = direction === 'incoming' ? edge.source : edge.target;
    const list = map.get(from) ?? [];
    list.push(to);
    map.set(from, list);
  }
  return map;
}

function collectReachableDatasets(
  graph: LineageGraph,
  start: LineageNode,
  direction: 'incoming' | 'outgoing',
  includeStart: boolean,
  strategy: TargetStrategy,
  reason: string,
) {
  const byId = new Map(graph.nodes.map((node) => [node.id, node]));
  const adjacency = buildAdjacency(graph, direction);
  const seen = new Set<string>();
  const rows: TargetPreviewRow[] = [];
  const queue: Array<{ id: string; distance: number }> = [{ id: start.id, distance: 0 }];
  while (queue.length > 0) {
    const current = queue.shift();
    if (!current || seen.has(current.id)) continue;
    seen.add(current.id);
    const node = byId.get(current.id);
    if (node?.kind === 'dataset' && (includeStart || current.id !== start.id)) {
      rows.push({
        nodeId: node.id,
        datasetRid: datasetRidForNode(node),
        label: node.label,
        strategy,
        reason,
        distance: current.distance,
      });
    }
    for (const next of adjacency.get(current.id) ?? []) queue.push({ id: next, distance: current.distance + 1 });
  }
  return rows;
}

function collectConnectingDatasets(graph: LineageGraph, input: LineageNode, target: LineageNode) {
  const byId = new Map(graph.nodes.map((node) => [node.id, node]));
  const adjacency = buildAdjacency(graph, 'outgoing');
  const rows: TargetPreviewRow[] = [];
  const queue: Array<{ id: string; path: string[] }> = [{ id: input.id, path: [input.id] }];
  const bestSeen = new Set<string>();
  while (queue.length > 0) {
    const current = queue.shift();
    if (!current) continue;
    const key = `${current.id}:${current.path.length}`;
    if (bestSeen.has(key)) continue;
    bestSeen.add(key);
    if (current.id === target.id) {
      current.path.forEach((id, index) => {
        if (id === input.id) return;
        const node = byId.get(id);
        if (node?.kind !== 'dataset') return;
        rows.push({
          nodeId: node.id,
          datasetRid: datasetRidForNode(node),
          label: node.label,
          strategy: 'connecting',
          reason: id === target.id ? 'connecting target' : 'connecting path',
          distance: index,
        });
      });
      continue;
    }
    for (const next of adjacency.get(current.id) ?? []) {
      if (current.path.includes(next)) continue;
      queue.push({ id: next, path: [...current.path, next] });
    }
  }
  return rows;
}

function hasDirectedPath(graph: LineageGraph, from: string, to: string) {
  if (from === to) return false;
  const adjacency = buildAdjacency(graph, 'outgoing');
  const queue = [from];
  const seen = new Set<string>();
  while (queue.length > 0) {
    const current = queue.shift();
    if (!current || seen.has(current)) continue;
    seen.add(current);
    for (const next of adjacency.get(current) ?? []) {
      if (next === to) return true;
      queue.push(next);
    }
  }
  return false;
}

function redundantTargetPairs(graph: LineageGraph | null, rows: TargetPreviewRow[]) {
  if (!graph || rows.length < 2) return [];
  const datasetRows = rows.filter((row) => row.nodeId);
  const pairs: Array<{ upstream: string; downstream: string }> = [];
  for (const upstream of datasetRows) {
    for (const downstream of datasetRows) {
      if (upstream.nodeId === downstream.nodeId) continue;
      if (hasDirectedPath(graph, upstream.nodeId, downstream.nodeId)) {
        pairs.push({ upstream: upstream.label, downstream: downstream.label });
        if (pairs.length >= 3) return pairs;
      }
    }
  }
  return pairs;
}

function previewScheduleTargets(graph: LineageGraph | null, selections: TargetSelection[]): TargetPreview {
  const rows: TargetPreviewRow[] = [];
  const warnings: string[] = [];
  for (const selection of selections) {
    if (selection.strategy === 'connecting') {
      const input = findDatasetNode(graph, selection.inputDatasetRid);
      const target = findDatasetNode(graph, selection.targetDatasetRid);
      if (!input || !target || !graph) {
        warnings.push(`Connecting target set ${selection.id} needs input and target datasets present in lineage.`);
        continue;
      }
      const connecting = collectConnectingDatasets(graph, input, target);
      if (connecting.length === 0) warnings.push(`No directed build path was found from ${selection.inputDatasetRid} to ${selection.targetDatasetRid}.`);
      rows.push(...connecting);
      continue;
    }

    const node = findDatasetNode(graph, selection.datasetRid);
    if (!node) {
      const rid = selection.datasetRid.trim();
      if (selection.strategy === 'single' && rid) {
        rows.push({
          nodeId: rid,
          datasetRid: rid,
          label: rid,
          strategy: 'single',
          reason: 'manual target',
          distance: 0,
        });
      } else if (rid) {
        warnings.push(`${rid} is not in the loaded lineage graph; only graph-visible datasets can resolve ${selection.strategy}.`);
      }
      continue;
    }
    if (!graph) {
      warnings.push('Lineage graph is still loading.');
      continue;
    }
    if (selection.strategy === 'single') {
      rows.push({
        nodeId: node.id,
        datasetRid: datasetRidForNode(node),
        label: node.label,
        strategy: 'single',
        reason: 'target',
        distance: 0,
      });
    } else if (selection.strategy === 'with_dependencies') {
      rows.push(...collectReachableDatasets(graph, node, 'incoming', true, selection.strategy, 'target or upstream dependency'));
    } else {
      rows.push(...collectReachableDatasets(graph, node, 'outgoing', false, selection.strategy, 'downstream descendant'));
    }
  }
  const sorted = sortedUniquePreviewRows(rows);
  if (sorted.length === 0) warnings.push('No build targets resolved. Add datasets or choose a different strategy.');
  return { rows: sorted, warnings: [...new Set(warnings)] };
}

export function NewSchedulePage() {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const seededDataset = searchParams.get('event_target') ?? searchParams.get('dataset') ?? '';

  const [name, setName] = useState(seededDataset ? 'Build dataset on update' : 'New build schedule');
  const [description, setDescription] = useState('');
  const [projectRid, setProjectRid] = useState('ri.foundry.main.project.default');
  const [folderRid, setFolderRid] = useState('');
  const [targetSelections, setTargetSelections] = useState<TargetSelection[]>(() => [makeTargetSelection(seededDataset)]);
  const [branch, setBranch] = useState('master');
  const [buildStrategy, setBuildStrategy] = useState<BuildStrategy>('STALE_ONLY');
  const [scopeKind, setScopeKind] = useState<ScheduleScopeKind>('USER');
  const [projectScopeText, setProjectScopeText] = useState('ri.foundry.main.project.default');
  const [runAsIdentity, setRunAsIdentity] = useState('');
  const [scheduleStatusCheckPlanned, setScheduleStatusCheckPlanned] = useState(false);
  const [triggerKind, setTriggerKind] = useState<TriggerKind>(seededDataset ? 'event' : 'time');
  const [cron, setCron] = useState('0 * * * *');
  const [timeZone, setTimeZone] = useState('UTC');
  const [eventType, setEventType] = useState<'DATA_UPDATED' | 'NEW_LOGIC' | 'JOB_SUCCEEDED'>('DATA_UPDATED');
  const [eventDatasetRid, setEventDatasetRid] = useState(seededDataset);
  const [lineageGraph, setLineageGraph] = useState<LineageGraph | null>(null);
  const [lineageLoading, setLineageLoading] = useState(false);
  const [lineageError, setLineageError] = useState('');
  const [overlapSchedules, setOverlapSchedules] = useState<Schedule[]>([]);
  const [overlapLoading, setOverlapLoading] = useState(false);
  const [targetHealth, setTargetHealth] = useState<Record<string, TargetHealthProbe>>({});
  const [targetHealthLoading, setTargetHealthLoading] = useState(false);
  const [paused, setPaused] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const targetPreview = useMemo(() => previewScheduleTargets(lineageGraph, targetSelections), [lineageGraph, targetSelections]);
  const previewTrigger = useMemo(() => buildTrigger(), [triggerKind, cron, timeZone, eventType, eventDatasetRid, branch]);
  const previewTarget = useMemo(() => buildTarget(targetPreview), [targetPreview, branch, buildStrategy, targetSelections, scheduleStatusCheckPlanned, scopeKind, runAsIdentity]);
  const previewRIDKey = targetPreview.rows.map((row) => row.datasetRid).join('|');

  async function refreshLineage() {
    setLineageLoading(true);
    setLineageError('');
    try {
      setLineageGraph(await getFullLineage());
    } catch (cause) {
      setLineageError(cause instanceof Error ? cause.message : 'Failed to load lineage graph');
      setLineageGraph(null);
    } finally {
      setLineageLoading(false);
    }
  }

  useEffect(() => {
    void refreshLineage();
  }, []);

  useEffect(() => {
    if (!projectScopeText.trim() || projectScopeText === 'ri.foundry.main.project.default') {
      setProjectScopeText(projectRid.trim() || 'ri.foundry.main.project.default');
    }
  }, [projectRid]);

  useEffect(() => {
    const targetRids = targetPreview.rows.map((row) => row.datasetRid).filter(Boolean);
    if (targetRids.length === 0) {
      setOverlapSchedules([]);
      return;
    }
    let cancelled = false;
    async function refreshOverlaps() {
      setOverlapLoading(true);
      try {
        const result = await listSchedules({
          files: targetRids,
          branch: branch.trim() || undefined,
          limit: 100,
        });
        if (!cancelled) setOverlapSchedules(result.data ?? []);
      } catch {
        if (!cancelled) setOverlapSchedules([]);
      } finally {
        if (!cancelled) setOverlapLoading(false);
      }
    }
    void refreshOverlaps();
    return () => {
      cancelled = true;
    };
  }, [previewRIDKey, branch]);

  useEffect(() => {
    const targetRids = targetPreview.rows.map((row) => row.datasetRid).filter(Boolean).slice(0, HEALTH_PROBE_LIMIT);
    if (targetRids.length === 0) {
      setTargetHealth({});
      setTargetHealthLoading(false);
      return;
    }
    let cancelled = false;
    async function refreshHealth() {
      setTargetHealthLoading(true);
      const entries = await Promise.all(targetRids.map(async (rid): Promise<[string, TargetHealthProbe]> => {
        try {
          const health = await getDatasetHealth(rid);
          return [rid, {
            rid,
            status: 'checked',
            checkCount: healthCheckCount(health),
            message: health.last_build_status ? `Last build ${health.last_build_status}` : undefined,
          }];
        } catch (cause) {
          return [rid, {
            rid,
            status: 'error',
            checkCount: null,
            message: cause instanceof Error ? cause.message : 'Health metadata unavailable',
          }];
        }
      }));
      if (!cancelled) setTargetHealth(Object.fromEntries(entries));
      if (!cancelled) setTargetHealthLoading(false);
    }
    void refreshHealth();
    return () => {
      cancelled = true;
    };
  }, [previewRIDKey]);

  function updateTargetSelection(id: string, patch: Partial<TargetSelection>) {
    setTargetSelections((current) => current.map((entry) => (entry.id === id ? { ...entry, ...patch } : entry)));
  }

  function addTargetSelection() {
    setTargetSelections((current) => [...current, makeTargetSelection()]);
  }

  function removeTargetSelection(id: string) {
    setTargetSelections((current) => (current.length === 1 ? current : current.filter((entry) => entry.id !== id)));
  }

  function buildTrigger(): Trigger {
    if (triggerKind === 'event') {
      return {
        kind: {
          event: {
            type: eventType,
            target_rid: eventDatasetRid.trim(),
            branch_filter: branch.trim() ? [branch.trim()] : undefined,
          },
        },
      };
    }
    return {
      kind: {
        time: {
          cron: cron.trim() || '0 * * * *',
          time_zone: timeZone.trim() || 'UTC',
          flavor: 'UNIX_5',
        },
      },
    };
  }

  function buildTarget(preview: TargetPreview): ScheduleTarget {
    const outputDatasetRids = preview.rows.map((row) => row.datasetRid);
    const primaryDatasetRid = outputDatasetRids[0] ?? '';
    const targetSetMetadata = targetSelections.map((selection) => ({
      strategy: selection.strategy,
      dataset_rid: selection.datasetRid.trim() || undefined,
      input_dataset_rid: selection.inputDatasetRid.trim() || undefined,
      target_dataset_rid: selection.targetDatasetRid.trim() || undefined,
    }));
    return {
      kind: {
        dataset_build: {
          dataset_rid: primaryDatasetRid,
          output_dataset_rids: outputDatasetRids,
          build_branch: branch.trim() || 'master',
          force_build: buildStrategy === 'FORCE',
          schedule_target_strategy: targetSelections.length > 1 ? 'mixed' : targetSelections[0]?.strategy ?? 'single',
          target_sets: targetSetMetadata,
          preview: {
            resolved_at: new Date().toISOString(),
            branch: branch.trim() || 'master',
            target_count: outputDatasetRids.length,
            warnings: preview.warnings,
          },
          guardrails: {
            schedule_status_check_planned: scheduleStatusCheckPlanned,
            ownership_scope: scopeKind,
            run_as_identity_configured: Boolean(runAsIdentity.trim()),
          },
        },
      },
    };
  }

  async function submit() {
    setBusy(true);
    setError('');
    try {
      const created = await createSchedule({
        project_rid: projectRid.trim() || 'ri.foundry.main.project.default',
        folder_rid: folderRid.trim() || null,
        name: name.trim() || 'New build schedule',
        description,
        trigger: buildTrigger(),
        target: buildTarget(targetPreview),
        paused,
        branch: branch.trim() || 'master',
        build_strategy: buildStrategy,
        scope_kind: scopeKind,
        project_scope_rids: scopeKind === 'PROJECT_SCOPED' ? parseRIDList(projectScopeText || projectRid) : [],
        run_as_identity: runAsIdentity.trim() || null,
      });
      notifications.success('Schedule created');
      navigate(`/schedules/${encodeURIComponent(created.rid)}`);
    } catch (cause) {
      const message = cause instanceof Error ? cause.message : 'Failed to create schedule';
      setError(message);
      notifications.error(message);
    } finally {
      setBusy(false);
    }
  }

  const canSubmit = Boolean(
    name.trim() &&
    projectRid.trim() &&
    targetPreview.rows.length > 0 &&
    (triggerKind === 'time' || eventDatasetRid.trim()),
  );

  const guardrails = useMemo<ScheduleGuardrail[]>(() => {
    const out: ScheduleGuardrail[] = [];
    const targetRids = new Set(targetPreview.rows.map((row) => row.datasetRid));
    const targetCount = targetRids.size;
    const production = isProductionSchedule(name, projectRid, branch);
    const overlapping = overlapSchedules.filter((schedule) => schedule.target_rids.some((rid) => targetRids.has(rid)));
    const redundantPairs = redundantTargetPairs(lineageGraph, targetPreview.rows);
    const broadStrategies = new Set(targetSelections.map((selection) => selection.strategy));
    const healthProbes = Object.values(targetHealth);
    const missingHealth = healthProbes.filter((probe) => probe.status !== 'checked' || probe.checkCount === 0);

    if (targetCount > BROAD_TARGET_THRESHOLD) {
      out.push({
        code: 'over_broad_targets',
        severity: 'warning',
        title: 'Broad build target set',
        message: `${targetCount} datasets resolve from this schedule.`,
        recommendation: 'Prefer a connecting build or split the schedule into smaller logical target sets.',
      });
    } else if (targetCount > 8 && (broadStrategies.has('with_dependencies') || broadStrategies.has('descendants'))) {
      out.push({
        code: 'broad_strategy',
        severity: 'info',
        title: 'Full graph strategy selected',
        message: `${targetCount} datasets will be built through dependencies or descendants.`,
        recommendation: 'Use connecting datasets when the intended pipeline boundary is known.',
      });
    }

    if (overlapping.length > 0) {
      out.push({
        code: 'schedule_overlap',
        severity: 'warning',
        title: 'Schedule overlap detected',
        message: `${overlapping.length} existing schedule${overlapping.length === 1 ? '' : 's'} already touch at least one selected target on this branch.`,
        recommendation: `Review ${overlapping.slice(0, 3).map((schedule) => schedule.name).join(', ')} and keep one scheduled build per dataset or pipeline section.`,
      });
    }

    if (redundantPairs.length > 0) {
      out.push({
        code: 'redundant_downstream_targets',
        severity: 'warning',
        title: 'Possible redundant downstream builds',
        message: `Targets include upstream/downstream pairs such as ${redundantPairs.map((pair) => `${pair.upstream} -> ${pair.downstream}`).join(', ')}.`,
        recommendation: 'Keep terminal datasets as targets and use a connecting build to avoid scheduling every pipeline step separately.',
      });
    }

    if (scopeKind === 'USER' && !runAsIdentity.trim()) {
      out.push({
        code: 'missing_owner',
        severity: 'warning',
        title: 'No durable run-as owner configured',
        message: 'This user-scoped schedule will rely on the editing user permissions.',
        recommendation: 'Use a project-scoped schedule where possible, or set a stable service user as run-as identity.',
      });
    } else if (scopeKind === 'PROJECT_SCOPED' && parseRIDList(projectScopeText || projectRid).length === 0) {
      out.push({
        code: 'missing_project_scope',
        severity: 'warning',
        title: 'Project scope is empty',
        message: 'Project-scoped schedules need at least one project RID.',
        recommendation: 'Add the owning project RID so builds are not tied to a single user account.',
      });
    }

    if (buildStrategy === 'FORCE' && targetCount > 1) {
      out.push({
        code: 'expensive_force_build',
        severity: 'critical',
        title: 'Multi-dataset force build',
        message: `Force build will recompute ${targetCount} datasets even when they are fresh.`,
        recommendation: 'Reserve force builds for raw Data Connection syncs and split them away from derived dataset schedules.',
      });
    } else if (buildStrategy === 'FORCE') {
      out.push({
        code: 'force_build',
        severity: 'info',
        title: 'Force build enabled',
        message: 'This schedule will build even fresh targets.',
        recommendation: 'Confirm the target is a raw/external sync or switch back to stale-only builds.',
      });
    }

    if (healthProbes.length > 0 && missingHealth.length > 0) {
      out.push({
        code: 'missing_health_checks',
        severity: 'warning',
        title: 'Health checks not verified',
        message: `${missingHealth.length} of ${healthProbes.length} sampled target${healthProbes.length === 1 ? '' : 's'} have no visible active health check metadata.`,
        recommendation: 'Add schedule status, schedule duration, freshness, or dataset checks before treating this schedule as production-ready.',
      });
    }

    if (production && !scheduleStatusCheckPlanned) {
      out.push({
        code: 'production_schedule_status_check',
        severity: 'warning',
        title: 'Production schedule needs status monitoring',
        message: 'The name, project, or branch looks production-facing.',
        recommendation: 'Add a schedule status Data Health check so failures across the full scheduled build are visible.',
      });
    }

    return out;
  }, [targetPreview.rows, targetSelections, overlapSchedules, lineageGraph, targetHealth, name, projectRid, branch, scopeKind, runAsIdentity, projectScopeText, buildStrategy, scheduleStatusCheckPlanned]);

  return (
    <main className="of-page" style={{ padding: 24, display: 'grid', gap: 16, maxWidth: 1180, margin: '0 auto' }}>
      <nav style={{ display: 'flex', gap: 8, alignItems: 'center', fontSize: 13 }}>
        <Link to="/build-schedules" style={{ color: 'var(--text-muted)' }}>Build schedules</Link>
        <span className="of-text-muted">/</span>
        <span className="of-text-muted">New schedule</span>
      </nav>

      <header className="of-panel" style={{ padding: 16, display: 'flex', justifyContent: 'space-between', gap: 16, alignItems: 'flex-start' }}>
        <div>
          <h1 className="of-heading-xl" style={{ margin: 0 }}>New schedule</h1>
          <p className="of-text-muted" style={{ margin: '4px 0 0' }}>Create a time or event-driven build schedule with previewed lineage target sets.</p>
        </div>
        <button type="button" className="of-button of-button--primary" onClick={() => void submit()} disabled={busy || !canSubmit}>
          {busy ? 'Creating...' : 'Create schedule'}
        </button>
      </header>

      {error && (
        <p role="alert" className="of-status-danger" style={{ padding: '10px 12px', borderRadius: 'var(--radius-md)', margin: 0 }}>
          {error}
        </p>
      )}

      <div style={{ display: 'grid', gridTemplateColumns: 'minmax(360px, 1fr) minmax(320px, 420px)', gap: 16, alignItems: 'start' }}>
        <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 14 }}>
          <section style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
            <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>
              <span className="of-eyebrow">Name</span>
              <input className="of-input" value={name} onChange={(event) => setName(event.target.value)} disabled={busy} />
            </label>
            <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>
              <span className="of-eyebrow">Project RID</span>
              <input className="of-input" value={projectRid} onChange={(event) => setProjectRid(event.target.value)} disabled={busy} />
            </label>
          </section>

          <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>
            <span className="of-eyebrow">Description</span>
            <textarea className="of-textarea" value={description} onChange={(event) => setDescription(event.target.value)} disabled={busy} style={{ minHeight: 72 }} />
          </label>

          <section style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
            <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>
              <span className="of-eyebrow">Folder RID</span>
              <input className="of-input" value={folderRid} onChange={(event) => setFolderRid(event.target.value)} disabled={busy} placeholder="Optional" />
            </label>
            <button type="button" className="of-button" onClick={() => void refreshLineage()} disabled={busy || lineageLoading} style={{ alignSelf: 'end' }}>
              {lineageLoading ? 'Previewing lineage...' : 'Preview build targets'}
            </button>
          </section>

          <section style={{ display: 'grid', gridTemplateColumns: '1fr 1fr 1fr', gap: 12 }}>
            <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>
              <span className="of-eyebrow">Branch</span>
              <input className="of-input" value={branch} onChange={(event) => setBranch(event.target.value)} disabled={busy} />
            </label>
            <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>
              <span className="of-eyebrow">Build strategy</span>
              <select className="of-select" value={buildStrategy} onChange={(event) => setBuildStrategy(event.target.value as BuildStrategy)} disabled={busy}>
                <option value="STALE_ONLY">Stale only</option>
                <option value="FORCE">Force build</option>
              </select>
            </label>
            <label style={{ display: 'flex', gap: 8, alignItems: 'end', fontSize: 12, paddingBottom: 8 }}>
              <input type="checkbox" checked={paused} onChange={(event) => setPaused(event.target.checked)} disabled={busy} />
              Create paused
            </label>
          </section>

          <section className="of-panel-muted" style={{ padding: 12, display: 'grid', gap: 12 }}>
            <div>
              <h2 className="of-heading-sm" style={{ margin: 0 }}>Ownership and monitoring</h2>
              <p className="of-text-muted" style={{ margin: '3px 0 0', fontSize: 12 }}>
                Guardrails use these fields to flag owner drift and production readiness.
              </p>
            </div>
            <section style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
              <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>
                <span className="of-eyebrow">Scope</span>
                <select className="of-select" value={scopeKind} onChange={(event) => setScopeKind(event.target.value as ScheduleScopeKind)} disabled={busy}>
                  <option value="USER">User scoped</option>
                  <option value="PROJECT_SCOPED">Project scoped</option>
                </select>
              </label>
              <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>
                <span className="of-eyebrow">Run-as identity</span>
                <input className="of-input" value={runAsIdentity} onChange={(event) => setRunAsIdentity(event.target.value)} disabled={busy} placeholder="service-user or project principal" />
              </label>
            </section>
            {scopeKind === 'PROJECT_SCOPED' && (
              <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>
                <span className="of-eyebrow">Project scope RIDs</span>
                <input className="of-input" value={projectScopeText} onChange={(event) => setProjectScopeText(event.target.value)} disabled={busy} placeholder="Comma-separated project RIDs" />
              </label>
            )}
            <label style={{ display: 'flex', gap: 8, alignItems: 'center', fontSize: 12 }}>
              <input type="checkbox" checked={scheduleStatusCheckPlanned} onChange={(event) => setScheduleStatusCheckPlanned(event.target.checked)} disabled={busy} />
              Schedule status Data Health check planned
            </label>
          </section>

          <section className="of-panel-muted" style={{ padding: 12, display: 'grid', gap: 12 }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'center' }}>
              <div>
                <h2 className="of-heading-sm" style={{ margin: 0 }}>Target dataset sets</h2>
                <p className="of-text-muted" style={{ margin: '3px 0 0', fontSize: 12 }}>
                  Add one or more target sets. Multiple rows are saved as a mixed target strategy.
                </p>
              </div>
              <button type="button" className="of-button" onClick={addTargetSelection} disabled={busy}>
                Add target set
              </button>
            </div>

            {lineageError && (
              <div className="of-status-warning" style={{ padding: '8px 10px', borderRadius: 'var(--radius-md)', fontSize: 12 }}>
                {lineageError}
              </div>
            )}

            <div style={{ display: 'grid', gap: 10 }}>
              {targetSelections.map((selection, index) => (
                <section key={selection.id} style={{ border: '1px solid var(--border-subtle)', borderRadius: 'var(--radius-md)', padding: 10, display: 'grid', gap: 10 }}>
                  <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'center' }}>
                    <strong style={{ fontSize: 12 }}>Target set {index + 1}</strong>
                    <button type="button" className="of-button of-button--ghost" onClick={() => removeTargetSelection(selection.id)} disabled={busy || targetSelections.length === 1}>
                      Remove
                    </button>
                  </div>
                  <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>
                    <span className="of-eyebrow">Schedule target strategy</span>
                    <select
                      className="of-select"
                      value={selection.strategy}
                      onChange={(event) => updateTargetSelection(selection.id, { strategy: event.target.value as TargetStrategy })}
                      disabled={busy}
                    >
                      {TARGET_STRATEGIES.map((entry) => (
                        <option key={entry.value} value={entry.value}>{entry.label}</option>
                      ))}
                    </select>
                    <span className="of-text-muted" style={{ fontSize: 11 }}>
                      {TARGET_STRATEGIES.find((entry) => entry.value === selection.strategy)?.hint}
                    </span>
                  </label>

                  {selection.strategy === 'connecting' ? (
                    <section style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
                      <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>
                        <span className="of-eyebrow">Input dataset RID</span>
                        <input
                          className="of-input"
                          value={selection.inputDatasetRid}
                          onChange={(event) => updateTargetSelection(selection.id, { inputDatasetRid: event.target.value })}
                          disabled={busy}
                        />
                      </label>
                      <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>
                        <span className="of-eyebrow">Target dataset RID</span>
                        <input
                          className="of-input"
                          value={selection.targetDatasetRid}
                          onChange={(event) => updateTargetSelection(selection.id, { targetDatasetRid: event.target.value })}
                          disabled={busy}
                        />
                      </label>
                    </section>
                  ) : (
                    <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>
                      <span className="of-eyebrow">Dataset RID</span>
                      <input
                        className="of-input"
                        value={selection.datasetRid}
                        onChange={(event) => {
                          const value = event.target.value;
                          updateTargetSelection(selection.id, { datasetRid: value, inputDatasetRid: value });
                          if (triggerKind === 'event' && index === 0) setEventDatasetRid(value);
                        }}
                        disabled={busy}
                      />
                    </label>
                  )}
                </section>
              ))}
            </div>
          </section>

          <section className="of-panel-muted" style={{ padding: 12, display: 'grid', gap: 12 }}>
            <div style={{ display: 'flex', gap: 8, alignItems: 'center', justifyContent: 'space-between' }}>
              <h2 className="of-heading-sm" style={{ margin: 0 }}>Trigger</h2>
              <select className="of-select" value={triggerKind} onChange={(event) => setTriggerKind(event.target.value as TriggerKind)} disabled={busy} style={{ width: 160 }}>
                <option value="time">Time</option>
                <option value="event">Dataset event</option>
              </select>
            </div>

            {triggerKind === 'time' ? (
              <section style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
                <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>
                  <span className="of-eyebrow">Cron</span>
                  <input className="of-input" value={cron} onChange={(event) => setCron(event.target.value)} disabled={busy} />
                </label>
                <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>
                  <span className="of-eyebrow">Time zone</span>
                  <input className="of-input" value={timeZone} onChange={(event) => setTimeZone(event.target.value)} disabled={busy} />
                </label>
              </section>
            ) : (
              <section style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 12 }}>
                <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>
                  <span className="of-eyebrow">Event</span>
                  <select className="of-select" value={eventType} onChange={(event) => setEventType(event.target.value as typeof eventType)} disabled={busy}>
                    <option value="DATA_UPDATED">Data updated</option>
                    <option value="NEW_LOGIC">New logic</option>
                    <option value="JOB_SUCCEEDED">Job succeeded</option>
                  </select>
                </label>
                <label style={{ display: 'grid', gap: 4, fontSize: 12 }}>
                  <span className="of-eyebrow">Event dataset RID</span>
                  <input className="of-input" value={eventDatasetRid} onChange={(event) => setEventDatasetRid(event.target.value)} disabled={busy} />
                </label>
              </section>
            )}
          </section>
        </section>

        <aside className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
          <div>
            <p className="of-eyebrow">Definition preview</p>
            <h2 className="of-heading-md" style={{ marginTop: 4 }}>{name || 'New schedule'}</h2>
          </div>
          <dl style={{ display: 'grid', gridTemplateColumns: '120px minmax(0, 1fr)', gap: '8px 12px', margin: 0, fontSize: 12 }}>
            <dt className="of-text-muted">Project</dt>
            <dd style={{ margin: 0, overflowWrap: 'anywhere' }}>{projectRid}</dd>
            <dt className="of-text-muted">Folder</dt>
            <dd style={{ margin: 0, overflowWrap: 'anywhere' }}>{folderRid || '-'}</dd>
            <dt className="of-text-muted">Branch</dt>
            <dd style={{ margin: 0 }}>{branch || 'master'}</dd>
            <dt className="of-text-muted">Strategy</dt>
            <dd style={{ margin: 0 }}>{targetSelections.length > 1 ? 'MIXED' : targetSelections[0]?.strategy.toUpperCase()} · {buildStrategy}</dd>
            <dt className="of-text-muted">Pause state</dt>
            <dd style={{ margin: 0 }}>{paused ? 'Paused' : 'Active'}</dd>
          </dl>

          <section className="of-panel-muted" style={{ padding: 12, display: 'grid', gap: 8 }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'center' }}>
              <div>
                <p className="of-eyebrow">Exact build target preview</p>
                <strong style={{ fontSize: 18 }}>{targetPreview.rows.length}</strong>
                <span className="of-text-muted" style={{ fontSize: 12 }}> dataset{targetPreview.rows.length === 1 ? '' : 's'}</span>
              </div>
              <span className={lineageLoading ? 'of-chip of-status-info' : 'of-chip'}>
                {lineageLoading ? 'Loading' : lineageGraph ? 'Lineage loaded' : 'Manual only'}
              </span>
            </div>
            {targetPreview.warnings.length > 0 && (
              <div className="of-status-warning" style={{ padding: '8px 10px', borderRadius: 'var(--radius-md)', fontSize: 11 }}>
                {targetPreview.warnings.join(' ')}
              </div>
            )}
            <div style={{ display: 'grid', gap: 6, maxHeight: 240, overflow: 'auto' }}>
              {targetPreview.rows.map((row) => (
                <div key={row.datasetRid} style={{ border: '1px solid var(--border-subtle)', borderRadius: 'var(--radius-sm)', padding: 8 }}>
                  <div style={{ fontSize: 12, fontWeight: 600, overflowWrap: 'anywhere' }}>{row.label}</div>
                  <div className="of-eyebrow" style={{ marginTop: 3 }}>
                    {row.strategy} · {row.reason} · distance {row.distance}
                  </div>
                  <code style={{ display: 'block', marginTop: 4, fontSize: 11, overflowWrap: 'anywhere' }}>{row.datasetRid}</code>
                </div>
              ))}
            </div>
          </section>

          <section className="of-panel-muted" style={{ padding: 12, display: 'grid', gap: 8 }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'center' }}>
              <div>
                <p className="of-eyebrow">Best-practice guardrails</p>
                <strong style={{ fontSize: 18 }}>{guardrails.length}</strong>
                <span className="of-text-muted" style={{ fontSize: 12 }}> finding{guardrails.length === 1 ? '' : 's'}</span>
              </div>
              <span className="of-chip">
                {overlapLoading || targetHealthLoading ? 'Checking' : 'Ready'}
              </span>
            </div>
            {guardrails.length === 0 ? (
              <p className="of-text-muted" style={{ margin: 0, fontSize: 12 }}>
                No schedule guardrails are currently firing.
              </p>
            ) : (
              <div style={{ display: 'grid', gap: 8 }}>
                {guardrails.map((guardrail) => (
                  <article key={guardrail.code} className={guardrailTone(guardrail.severity)} style={{ padding: '8px 10px', borderRadius: 'var(--radius-md)' }}>
                    <strong style={{ display: 'block', fontSize: 12 }}>{guardrail.title}</strong>
                    <p style={{ margin: '3px 0 0', fontSize: 11 }}>{guardrail.message}</p>
                    <p style={{ margin: '3px 0 0', fontSize: 11 }}>{guardrail.recommendation}</p>
                  </article>
                ))}
              </div>
            )}
          </section>

          <pre style={{ margin: 0, padding: 12, background: 'var(--bg-subtle)', borderRadius: 'var(--radius-md)', overflow: 'auto', fontSize: 11 }}>
            {JSON.stringify({ trigger: previewTrigger, target: previewTarget }, null, 2)}
          </pre>
        </aside>
      </div>
    </main>
  );
}
