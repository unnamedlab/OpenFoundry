import { useEffect, useMemo, useState } from 'react';

import {
  DATA_EXPECTATION_KINDS,
  approveDataExpectation,
  dataExpectationKindLabel,
  defaultDataExpectationDraft,
  deleteDataExpectation,
  evaluateDataExpectationsForPreview,
  listDataExpectationResults,
  listDataExpectations,
  materializeDataExpectation,
  publishExpectationResultsToDataHealth,
  recordDataExpectationResults,
  syncNodeExpectationsToStore,
  upsertDataExpectation,
  withNodeDataExpectations,
  type DataExpectationDefinition,
  type DataExpectationDraft,
  type DataExpectationFailureMode,
  type DataExpectationKind,
  type DataExpectationPreviewInput,
  type DataExpectationResult,
  type DataExpectationResultStatus,
  type DataExpectationScope,
} from '@/lib/api/data-expectations';
import type { BuildEnvelope } from '@/lib/api/buildsV1';
import type { PipelineNode } from '@/lib/api/pipelines';
import { notifications } from '@/lib/stores/notifications';

interface DataExpectationsPanelProps {
  pipelineId: string;
  branchName: string;
  node: PipelineNode;
  preview?: DataExpectationPreviewInput | null;
  onNodeChange?: (node: PipelineNode) => void;
  compact?: boolean;
}

interface BuildExpectationResultsPanelProps {
  build: BuildEnvelope;
}

const SCOPES: DataExpectationScope[] = ['input', 'output'];
const FAILURE_MODES: DataExpectationFailureMode[] = ['ABORT_BUILD', 'WARN'];

export function DataExpectationsPanel({
  pipelineId,
  branchName,
  node,
  preview = null,
  onNodeChange,
  compact = false,
}: DataExpectationsPanelProps) {
  const [expectations, setExpectations] = useState<DataExpectationDefinition[]>([]);
  const [results, setResults] = useState<DataExpectationResult[]>([]);
  const [editingId, setEditingId] = useState('');
  const [draft, setDraft] = useState<DataExpectationDraft>(() => defaultDataExpectationDraft('output'));
  const [error, setError] = useState('');

  function reload() {
    syncNodeExpectationsToStore(pipelineId, branchName, node);
    setExpectations(listDataExpectations({ pipelineId, nodeId: node.id }));
    setResults(listDataExpectationResults({ pipelineId, nodeId: node.id }));
  }

  useEffect(() => {
    reload();
  }, [pipelineId, branchName, node.id]);

  const duplicateName = useMemo(() => {
    const name = draft.name.trim().toLowerCase();
    if (!name) return false;
    return expectations.some((expectation) => expectation.id !== editingId && expectation.name.trim().toLowerCase() === name);
  }, [draft.name, editingId, expectations]);

  const lastResults = useMemo(() => {
    const byExpectation = new Map<string, DataExpectationResult>();
    for (const result of results) {
      if (!byExpectation.has(result.expectation_id)) byExpectation.set(result.expectation_id, result);
    }
    return byExpectation;
  }, [results]);

  function patchDraft(patch: Partial<DataExpectationDraft>) {
    setDraft((current) => ({ ...current, ...patch }));
  }

  function editExpectation(expectation: DataExpectationDefinition) {
    setEditingId(expectation.id);
    setDraft({
      scope: expectation.scope,
      name: expectation.name,
      kind: expectation.kind,
      column: expectation.column,
      expected_value: expectation.expected_value,
      failure_mode: expectation.failure_mode,
      enabled: expectation.enabled,
    });
    setError('');
  }

  function clearDraft(scope: DataExpectationScope = draft.scope) {
    setEditingId('');
    setDraft(defaultDataExpectationDraft(scope));
    setError('');
  }

  function persistExpectation() {
    if (!draft.name.trim()) {
      setError('Expectation name is required.');
      return;
    }
    if (duplicateName) {
      setError('Check names must be unique within a transform.');
      return;
    }
    const existing = expectations.find((expectation) => expectation.id === editingId) ?? null;
    const definition = materializeDataExpectation({
      draft,
      pipelineId,
      node,
      branchName,
      existing,
    });
    upsertDataExpectation(definition);
    const nextDefinitions = [
      definition,
      ...expectations.filter((expectation) => expectation.id !== definition.id),
    ];
    onNodeChange?.(withNodeDataExpectations(node, nextDefinitions));
    clearDraft(definition.scope);
    reload();
    notifications.success('Data expectation saved');
  }

  function removeExpectation(id: string) {
    deleteDataExpectation(id);
    const nextDefinitions = expectations.filter((expectation) => expectation.id !== id);
    onNodeChange?.(withNodeDataExpectations(node, nextDefinitions));
    reload();
  }

  function approve(id: string) {
    approveDataExpectation(id);
    onNodeChange?.(withNodeDataExpectations(node, listDataExpectations({ pipelineId, nodeId: node.id })));
    reload();
    notifications.success('Expectation change marked reviewed');
  }

  function evaluateNow() {
    const active = listDataExpectations({ pipelineId, nodeId: node.id });
    const evaluated = evaluateDataExpectationsForPreview(active, preview);
    recordDataExpectationResults(evaluated);
    publishExpectationResultsToDataHealth(evaluated);
    onNodeChange?.(withNodeDataExpectations(node, listDataExpectations({ pipelineId, nodeId: node.id })));
    reload();
    const failing = evaluated.filter((result) => result.status === 'failed').length;
    if (failing > 0) notifications.warning(`${failing} expectation(s) failed`);
    else notifications.success('Data expectations evaluated');
  }

  return (
    <section className={compact ? undefined : 'of-panel'} style={{ padding: compact ? '12px 0 0' : 16, display: 'grid', gap: 12, borderTop: compact ? '1px solid var(--border-subtle)' : undefined }}>
      <header style={{ display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'start', flexWrap: 'wrap' }}>
        <div>
          <p className="of-eyebrow">Data expectations</p>
          <h3 className="of-heading-sm" style={{ marginTop: 4 }}>{node.label || node.id}</h3>
          <p className="of-text-muted" style={{ margin: '4px 0 0', fontSize: 12 }}>
            Define input pre-conditions and output post-conditions. Failed expectations can abort builds or continue as warnings.
          </p>
        </div>
        <button type="button" className="of-button" onClick={evaluateNow} disabled={expectations.length === 0}>
          Evaluate now
        </button>
      </header>

      {error && (
        <div className="of-status-danger" style={{ padding: '8px 10px', borderRadius: 'var(--radius-md)', fontSize: 12 }}>
          {error}
        </div>
      )}

      <div className="of-panel-muted" style={{ padding: 12, display: 'grid', gap: 10 }}>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(150px, 1fr))', gap: 8 }}>
          <label style={{ fontSize: 12 }}>
            Name
            <input value={draft.name} onChange={(event) => patchDraft({ name: event.target.value })} className="of-input" style={{ marginTop: 4 }} />
          </label>
          <label style={{ fontSize: 12 }}>
            Scope
            <select value={draft.scope} onChange={(event) => patchDraft({ scope: event.target.value as DataExpectationScope })} className="of-input" style={{ marginTop: 4 }}>
              {SCOPES.map((scope) => <option key={scope} value={scope}>{scope === 'input' ? 'Input pre-condition' : 'Output post-condition'}</option>)}
            </select>
          </label>
          <label style={{ fontSize: 12 }}>
            Expectation
            <select value={draft.kind} onChange={(event) => patchDraft({ kind: event.target.value as DataExpectationKind })} className="of-input" style={{ marginTop: 4 }}>
              {DATA_EXPECTATION_KINDS.map((kind) => <option key={kind.value} value={kind.value}>{kind.label}</option>)}
            </select>
          </label>
          <label style={{ fontSize: 12 }}>
            Failure behavior
            <select value={draft.failure_mode} onChange={(event) => patchDraft({ failure_mode: event.target.value as DataExpectationFailureMode })} className="of-input" style={{ marginTop: 4 }}>
              {FAILURE_MODES.map((mode) => <option key={mode} value={mode}>{mode === 'ABORT_BUILD' ? 'Abort build' : 'Warn and continue'}</option>)}
            </select>
          </label>
          <label style={{ fontSize: 12 }}>
            Column
            <input value={draft.column} onChange={(event) => patchDraft({ column: event.target.value })} className="of-input" style={{ marginTop: 4 }} placeholder="id, status, imported_at" />
          </label>
          <label style={{ fontSize: 12 }}>
            Expected value
            <input value={draft.expected_value} onChange={(event) => patchDraft({ expected_value: event.target.value })} className="of-input" style={{ marginTop: 4 }} placeholder="row count or comma-separated columns" />
          </label>
        </div>
        <label className="of-chip" style={{ width: 'fit-content', cursor: 'pointer' }}>
          <input type="checkbox" checked={draft.enabled} onChange={(event) => patchDraft({ enabled: event.target.checked })} style={{ margin: 0 }} />
          Enabled
        </label>
        <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
          <button type="button" className="of-button of-button--primary" onClick={persistExpectation} disabled={duplicateName}>
            {editingId ? 'Save expectation' : 'Add expectation'}
          </button>
          {editingId && (
            <button type="button" className="of-button" onClick={() => clearDraft()}>
              New expectation
            </button>
          )}
        </div>
      </div>

      {expectations.length === 0 ? (
        <p className="of-text-muted" style={{ margin: 0, fontSize: 13 }}>
          No expectations are defined for this transform yet.
        </p>
      ) : (
        <div style={{ display: 'grid', gap: 8 }}>
          {expectations.map((expectation) => {
            const result = expectation.last_result ?? lastResults.get(expectation.id);
            return (
              <article key={expectation.id} style={{ border: '1px solid var(--border-subtle)', borderRadius: 'var(--radius-md)', padding: 10, display: 'grid', gap: 6 }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8, alignItems: 'start', flexWrap: 'wrap' }}>
                  <div>
                    <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', alignItems: 'center' }}>
                      <strong style={{ fontSize: 13 }}>{expectation.name}</strong>
                      <span className="of-chip">{expectation.scope === 'input' ? 'Pre-condition' : 'Post-condition'}</span>
                      <span className={`of-chip ${expectation.failure_mode === 'ABORT_BUILD' ? 'of-status-danger' : 'of-status-warning'}`}>
                        {expectation.failure_mode === 'ABORT_BUILD' ? 'Aborts build' : 'Warning'}
                      </span>
                      <span className={`of-chip ${expectation.review_status === 'APPROVED' ? 'of-status-success' : 'of-status-warning'}`}>
                        {expectation.review_status === 'APPROVED' ? 'Reviewed' : 'Needs review'}
                      </span>
                    </div>
                    <p className="of-text-muted" style={{ margin: '4px 0 0', fontSize: 12 }}>
                      {dataExpectationKindLabel(expectation.kind)}
                      {expectation.column ? ` | ${expectation.column}` : ''}
                      {expectation.expected_value ? ` | ${expectation.expected_value}` : ''}
                    </p>
                  </div>
                  <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                    {expectation.review_status !== 'APPROVED' && (
                      <button type="button" className="of-button" onClick={() => approve(expectation.id)}>Mark reviewed</button>
                    )}
                    <button type="button" className="of-button" onClick={() => editExpectation(expectation)}>Edit</button>
                    <button type="button" className="of-button" onClick={() => removeExpectation(expectation.id)}>Delete</button>
                  </div>
                </div>
                {result ? (
                  <div className={`of-chip ${resultTone(result.status, result.failure_mode)}`} style={{ width: 'fit-content' }}>
                    {result.status}: {result.message}
                  </div>
                ) : (
                  <p className="of-text-muted" style={{ margin: 0, fontSize: 12 }}>Not evaluated yet.</p>
                )}
              </article>
            );
          })}
        </div>
      )}
    </section>
  );
}

export function BuildExpectationResultsPanel({ build }: BuildExpectationResultsPanelProps) {
  const buildIds = [build.id, build.rid].filter(Boolean);
  const results = buildIds.flatMap((buildId) => listDataExpectationResults({ buildId }));
  const uniqueResults = Array.from(new Map(results.map((result) => [result.id, result])).values());
  if (uniqueResults.length === 0) {
    return (
      <section className="of-panel" style={{ padding: 16 }}>
        <p className="of-eyebrow">Data expectations</p>
        <p className="of-text-muted" style={{ margin: '6px 0 0', fontSize: 13 }}>No expectation results were published for this build.</p>
      </section>
    );
  }
  return (
    <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 10 }}>
      <div>
        <p className="of-eyebrow">Data expectations</p>
        <h2 className="of-heading-sm" style={{ marginTop: 4 }}>{uniqueResults.length} result(s)</h2>
      </div>
      <div style={{ display: 'grid', gap: 8 }}>
        {uniqueResults.map((result) => (
          <div key={result.id} style={{ display: 'grid', gap: 4, borderTop: '1px solid var(--border-subtle)', paddingTop: 8 }}>
            <div style={{ display: 'flex', gap: 8, justifyContent: 'space-between', flexWrap: 'wrap' }}>
              <strong style={{ fontSize: 13 }}>{result.expectation_name}</strong>
              <span className={`of-chip ${resultTone(result.status, result.failure_mode)}`}>{result.status}</span>
            </div>
            <p style={{ margin: 0, fontSize: 12 }}>{result.message}</p>
            <p className="of-text-muted" style={{ margin: 0, fontSize: 12 }}>
              {result.scope} | {result.node_label} | {new Date(result.observed_at).toLocaleString()}
            </p>
          </div>
        ))}
      </div>
    </section>
  );
}

function resultTone(status: DataExpectationResultStatus, failureMode: DataExpectationFailureMode) {
  if (status === 'passed') return 'of-status-success';
  if (status === 'failed' && failureMode === 'ABORT_BUILD') return 'of-status-danger';
  if (status === 'failed' || status === 'warning') return 'of-status-warning';
  return 'of-status-info';
}
