import { useEffect, useMemo, useState } from 'react';

import {
  RESOURCE_HEALTH_CHECK_CATALOG,
  RESOURCE_HEALTH_CHECK_KINDS,
  defaultResourceHealthCheckDraft,
  deleteResourceHealthCheck,
  listResourceHealthChecks,
  materializeResourceHealthCheck,
  resourceHealthCheckDescription,
  resourceHealthCheckLabel,
  resourceHealthCheckSummary,
  setResourceHealthCheckEnabled,
  upsertResourceHealthCheck,
  type ResourceHealthCheckComparator,
  type ResourceHealthCheckDefinition,
  type ResourceHealthCheckDraft,
  type ResourceHealthCheckKind,
  type ResourceHealthCheckResourceType,
  type ResourceHealthCheckSeverity,
  type ResourceHealthCheckSurface,
} from '@/lib/api/resource-health-checks';

interface ResourceHealthChecksPanelProps {
  resourceRid?: string | null;
  resourceName: string;
  resourceType: ResourceHealthCheckResourceType;
  sourceSurface: ResourceHealthCheckSurface;
  availableKinds: ResourceHealthCheckKind[];
  defaultGroup?: string;
  defaultMonitoringView?: string;
  compact?: boolean;
}

const SEVERITIES: ResourceHealthCheckSeverity[] = ['INFO', 'MODERATE', 'CRITICAL'];
const COMPARATORS: ResourceHealthCheckComparator[] = ['ANY_FAILURE', 'GT', 'GTE', 'LT', 'LTE', 'EQ', 'BETWEEN', 'CHANGED'];

export function ResourceHealthChecksPanel({
  resourceRid,
  resourceName,
  resourceType,
  sourceSurface,
  availableKinds,
  defaultGroup = 'Resource health',
  defaultMonitoringView = '',
  compact = false,
}: ResourceHealthChecksPanelProps) {
  const safeRid = resourceRid || '';
  const availableSet = useMemo(() => new Set(availableKinds), [availableKinds]);
  const firstKind = availableKinds[0] ?? 'status';
  const [checks, setChecks] = useState<ResourceHealthCheckDefinition[]>([]);
  const [editingId, setEditingId] = useState('');
  const [draft, setDraft] = useState<ResourceHealthCheckDraft>(() => (
    defaultResourceHealthCheckDraft({ kind: firstKind, group: defaultGroup, monitoringView: defaultMonitoringView })
  ));

  function reload() {
    setChecks(safeRid ? listResourceHealthChecks(safeRid) : []);
  }

  useEffect(() => {
    reload();
  }, [safeRid]);

  useEffect(() => {
    if (!availableSet.has(draft.kind) && firstKind) {
      setDraft(defaultResourceHealthCheckDraft({ kind: firstKind, group: defaultGroup, monitoringView: defaultMonitoringView }));
      setEditingId('');
    }
  }, [availableSet, defaultGroup, defaultMonitoringView, draft.kind, firstKind]);

  function patchDraft(patch: Partial<ResourceHealthCheckDraft>) {
    setDraft((current) => ({ ...current, ...patch }));
  }

  function selectKind(kind: ResourceHealthCheckKind) {
    setDraft(defaultResourceHealthCheckDraft({ kind, group: defaultGroup, monitoringView: defaultMonitoringView }));
    setEditingId('');
  }

  function editCheck(check: ResourceHealthCheckDefinition) {
    setEditingId(check.id);
    setDraft({
      kind: check.kind,
      severity: check.severity,
      comparator: check.comparator,
      threshold: check.threshold,
      secondary_threshold: check.secondary_threshold,
      unit: check.unit,
      column: check.column,
      group: check.group,
      monitoring_view: check.monitoring_view,
      escalate_after_failures: check.escalate_after_failures,
      notes: check.notes,
      create_issue_on_failure: check.create_issue_on_failure,
      issue_prompt: check.issue_prompt,
    });
  }

  function saveCheck() {
    if (!safeRid || !availableSet.has(draft.kind)) return;
    const existing = checks.find((check) => check.id === editingId) ?? null;
    upsertResourceHealthCheck(materializeResourceHealthCheck({
      draft,
      existing,
      resourceRid: safeRid,
      resourceName,
      resourceType,
      sourceSurface,
    }));
    reload();
    setEditingId('');
    setDraft(defaultResourceHealthCheckDraft({ kind: draft.kind, group: defaultGroup, monitoringView: defaultMonitoringView }));
  }

  function removeCheck(id: string) {
    deleteResourceHealthCheck(id);
    if (editingId === id) setEditingId('');
    reload();
  }

  function toggleEnabled(check: ResourceHealthCheckDefinition) {
    setResourceHealthCheckEnabled(check.id, !check.enabled);
    reload();
  }

  if (!safeRid) {
    return (
      <section className={compact ? undefined : 'of-panel-muted'} style={{ padding: 12, fontSize: 12, color: 'var(--text-muted)' }}>
        Health checks can be configured once the resource has a stable RID.
      </section>
    );
  }

  return (
    <section className={compact ? undefined : 'of-panel'} style={{ padding: compact ? '12px 0 0' : 16, display: 'grid', gap: 12, borderTop: compact ? '1px solid var(--border-subtle)' : undefined }}>
      <header style={{ display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'start', flexWrap: 'wrap' }}>
        <div>
          <p className="of-eyebrow">Resource health checks</p>
          <h2 className="of-heading-sm" style={{ marginTop: 4 }}>{resourceName}</h2>
          <p className="of-text-muted" style={{ margin: '4px 0 0', fontSize: 12 }}>
            {checks.length} configured | {availableKinds.length} signal type(s) available from this panel.
          </p>
        </div>
        {editingId && (
          <button type="button" className="of-button" onClick={() => {
            setEditingId('');
            setDraft(defaultResourceHealthCheckDraft({ kind: firstKind, group: defaultGroup, monitoringView: defaultMonitoringView }));
          }}>
            New check
          </button>
        )}
      </header>

      <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
        {RESOURCE_HEALTH_CHECK_KINDS.map((kind) => {
          const available = availableSet.has(kind);
          return (
            <button
              key={kind}
              type="button"
              className={`of-chip ${draft.kind === kind ? 'of-chip-active' : ''}`}
              onClick={() => selectKind(kind)}
              disabled={!available}
              title={available ? resourceHealthCheckDescription(kind) : 'No local signal is available for this check in the current panel.'}
              style={{ border: 0, cursor: available ? 'pointer' : 'not-allowed', opacity: available ? 1 : 0.45 }}
            >
              {resourceHealthCheckLabel(kind)}
            </button>
          );
        })}
      </div>

      <div className="of-panel-muted" style={{ padding: 12, display: 'grid', gap: 10 }}>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(150px, 1fr))', gap: 10 }}>
          <label style={{ fontSize: 12 }}>
            Check type
            <select value={draft.kind} onChange={(event) => selectKind(event.target.value as ResourceHealthCheckKind)} className="of-input" style={{ marginTop: 4 }}>
              {RESOURCE_HEALTH_CHECK_CATALOG.map((entry) => (
                <option key={entry.kind} value={entry.kind} disabled={!availableSet.has(entry.kind)}>
                  {entry.label}{availableSet.has(entry.kind) ? '' : ' (unavailable)'}
                </option>
              ))}
            </select>
          </label>
          <label style={{ fontSize: 12 }}>
            Severity
            <select value={draft.severity} onChange={(event) => patchDraft({ severity: event.target.value as ResourceHealthCheckSeverity })} className="of-input" style={{ marginTop: 4 }}>
              {SEVERITIES.map((severity) => <option key={severity} value={severity}>{severity}</option>)}
            </select>
          </label>
          <label style={{ fontSize: 12 }}>
            Comparator
            <select value={draft.comparator} onChange={(event) => patchDraft({ comparator: event.target.value as ResourceHealthCheckComparator })} className="of-input" style={{ marginTop: 4 }}>
              {COMPARATORS.map((comparator) => <option key={comparator} value={comparator}>{comparator}</option>)}
            </select>
          </label>
          <label style={{ fontSize: 12 }}>
            Threshold
            <input value={draft.threshold} onChange={(event) => patchDraft({ threshold: event.target.value })} className="of-input" style={{ marginTop: 4 }} placeholder="1, 24, failed" />
          </label>
          <label style={{ fontSize: 12 }}>
            Secondary threshold
            <input value={draft.secondary_threshold} onChange={(event) => patchDraft({ secondary_threshold: event.target.value })} className="of-input" style={{ marginTop: 4 }} placeholder="optional" />
          </label>
          <label style={{ fontSize: 12 }}>
            Unit
            <input value={draft.unit} onChange={(event) => patchDraft({ unit: event.target.value })} className="of-input" style={{ marginTop: 4 }} placeholder="minutes, hours, rows" />
          </label>
          <label style={{ fontSize: 12 }}>
            Column
            <input value={draft.column} onChange={(event) => patchDraft({ column: event.target.value })} className="of-input" style={{ marginTop: 4 }} placeholder="timestamp or value column" />
          </label>
          <label style={{ fontSize: 12 }}>
            Escalate after failures
            <input
              type="number"
              min={1}
              value={draft.escalate_after_failures}
              onChange={(event) => patchDraft({ escalate_after_failures: Math.max(1, Number(event.target.value) || 1) })}
              className="of-input"
              style={{ marginTop: 4 }}
            />
          </label>
          <label style={{ fontSize: 12 }}>
            Group
            <input value={draft.group} onChange={(event) => patchDraft({ group: event.target.value })} className="of-input" style={{ marginTop: 4 }} />
          </label>
          <label style={{ fontSize: 12 }}>
            Monitoring view
            <input value={draft.monitoring_view} onChange={(event) => patchDraft({ monitoring_view: event.target.value })} className="of-input" style={{ marginTop: 4 }} placeholder="Project, folder, or saved view" />
          </label>
        </div>
        <label style={{ display: 'flex', gap: 8, alignItems: 'center', fontSize: 12 }}>
          <input type="checkbox" checked={draft.create_issue_on_failure} onChange={(event) => patchDraft({ create_issue_on_failure: event.target.checked })} />
          Create an issue when this check fails
        </label>
        {draft.create_issue_on_failure && (
          <label style={{ fontSize: 12 }}>
            Issue prompt
            <input value={draft.issue_prompt} onChange={(event) => patchDraft({ issue_prompt: event.target.value })} className="of-input" style={{ marginTop: 4 }} placeholder="Summarize failure, owner, and next diagnostic step" />
          </label>
        )}
        <label style={{ fontSize: 12 }}>
          Notes
          <textarea value={draft.notes} onChange={(event) => patchDraft({ notes: event.target.value })} className="of-input" rows={3} style={{ marginTop: 4 }} />
        </label>
        <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
          <button type="button" className="of-button of-button--primary" onClick={saveCheck} disabled={!availableSet.has(draft.kind)}>
            {editingId ? 'Update check' : 'Add check'}
          </button>
          <span className="of-text-muted" style={{ fontSize: 12 }}>
            {resourceHealthCheckDescription(draft.kind)}
          </span>
        </div>
      </div>

      {checks.length === 0 ? (
        <div className="of-text-muted" style={{ fontSize: 12 }}>
          No resource-level checks configured yet.
        </div>
      ) : (
        <div style={{ display: 'grid', gap: 8 }}>
          {checks.map((check) => (
            <article key={check.id} style={{ padding: 10, border: '1px solid var(--border-subtle)', borderRadius: 'var(--radius-md)', display: 'grid', gap: 6 }}>
              <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8, flexWrap: 'wrap' }}>
                <div>
                  <strong style={{ fontSize: 13 }}>{resourceHealthCheckLabel(check.kind)}</strong>
                  <p className="of-text-muted" style={{ margin: '3px 0 0', fontSize: 12 }}>{resourceHealthCheckSummary(check)}</p>
                </div>
                <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                  <button type="button" className="of-button" onClick={() => toggleEnabled(check)}>{check.enabled ? 'Pause' : 'Resume'}</button>
                  <button type="button" className="of-button" onClick={() => editCheck(check)}>Edit</button>
                  <button type="button" className="of-button" onClick={() => removeCheck(check.id)}>Delete</button>
                </div>
              </div>
              <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                <span className="of-chip">{check.source_surface}</span>
                <span className="of-chip">{check.group || 'No group'}</span>
                <span className="of-chip">{check.monitoring_view || 'No monitoring view'}</span>
                <span className={`of-chip ${check.enabled ? 'of-status-success' : 'of-status-warning'}`}>{check.enabled ? 'Enabled' : 'Paused'}</span>
              </div>
              {check.notes && <p style={{ margin: 0, fontSize: 12 }}>{check.notes}</p>}
              {check.create_issue_on_failure && (
                <p className="of-text-muted" style={{ margin: 0, fontSize: 12 }}>
                  Issue prompt: {check.issue_prompt || 'Default issue prompt'}
                </p>
              )}
            </article>
          ))}
        </div>
      )}
    </section>
  );
}
