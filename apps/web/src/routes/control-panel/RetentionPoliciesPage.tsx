import { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';

import {
  createRetentionPolicy,
  listRetentionExecutions,
  listRetentionPolicies,
  runRetentionExecution,
  type RetentionDatasetSelectorKind,
  type RetentionExecutionRun,
  type RetentionPolicy,
  type RetentionPolicyType,
  type RetentionTransactionSelectorKind,
} from '@/lib/api/datasets';

const DANGER_ACK = 'DELETE_CURRENT_DATA';

export function RetentionPoliciesPage() {
  const [policies, setPolicies] = useState<RetentionPolicy[]>([]);
  const [executions, setExecutions] = useState<RetentionExecutionRun[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');

  const [name, setName] = useState('');
  const [policyType, setPolicyType] = useState<RetentionPolicyType>('custom');
  const [spaceId, setSpaceId] = useState('');
  const [datasetRid, setDatasetRid] = useState('');
  const [targetKind, setTargetKind] = useState<'dataset' | 'transaction'>('transaction');
  const [retentionDays, setRetentionDays] = useState(90);
  const [datasetSelectorKind, setDatasetSelectorKind] = useState<RetentionDatasetSelectorKind>('all');
  const [transactionSelectorKind, setTransactionSelectorKind] = useState<RetentionTransactionSelectorKind>('older_than');
  const [branch, setBranch] = useState('master');
  const [count, setCount] = useState(20);
  const [ageDays, setAgeDays] = useState(90);
  const [allowLatestViewDeletion, setAllowLatestViewDeletion] = useState(false);
  const [abortOpenTransactions, setAbortOpenTransactions] = useState(false);
  const [dangerAck, setDangerAck] = useState(false);
  const [legacyYaml, setLegacyYaml] = useState('');
  const [executionDatasetRid, setExecutionDatasetRid] = useState('');
  const [executionAsOfDays, setExecutionAsOfDays] = useState(0);
  const [executionDryRun, setExecutionDryRun] = useState(true);

  const summary = useMemo(() => ({
    total: policies.length,
    recommended: policies.filter((policy) => policyTypeOf(policy) === 'recommended').length,
    custom: policies.filter((policy) => policyTypeOf(policy) === 'custom').length,
    legacy: policies.filter((policy) => policyTypeOf(policy) === 'legacy').length,
  }), [policies]);

  async function refresh() {
    setLoading(true);
    setError('');
    try {
      const [nextPolicies, nextExecutions] = await Promise.all([listRetentionPolicies(), listRetentionExecutions()]);
      setPolicies(nextPolicies);
      setExecutions(nextExecutions);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to load retention policies');
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void refresh();
  }, []);

  async function executeRetention() {
    if (!executionDatasetRid.trim()) {
      setError('Dataset RID is required to execute retention.');
      return;
    }
    setBusy(true);
    setError('');
    setNotice('');
    try {
      const run = await runRetentionExecution({
        dataset_rid: executionDatasetRid.trim(),
        as_of_days: executionAsOfDays,
        recovery_window_days: 7,
        dry_run: executionDryRun,
      });
      setExecutions((current) => [run, ...current]);
      setNotice(`Retention execution ${run.id} completed: marked ${run.marked_transaction_count}, swept ${run.swept_transaction_count}.`);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to execute retention policies');
    } finally {
      setBusy(false);
    }
  }

  async function createPolicy() {
    const dangerous = allowLatestViewDeletion || abortOpenTransactions;
    if (!name.trim()) {
      setError('Policy name is required.');
      return;
    }
    if (dangerous && !dangerAck) {
      setError('Confirm the destructive retention warning before creating this policy.');
      return;
    }
    setBusy(true);
    setError('');
    setNotice('');
    try {
      const created = await createRetentionPolicy({
        name: name.trim(),
        policy_type: policyType,
        space_id: spaceId.trim() || null,
        scope: spaceId.trim() ? `space:${spaceId.trim()}` : 'organization',
        target_kind: targetKind,
        retention_days: retentionDays,
        legal_hold: false,
        purge_mode: 'soft',
        rules: [],
        selector: datasetRid.trim() ? { dataset_rid: datasetRid.trim() } : { all_datasets: true },
        criteria: transactionSelectorKind === 'aborted'
          ? { transaction_state: 'ABORTED' }
          : transactionSelectorKind === 'older_than'
            ? { transaction_age_seconds: ageDays * 24 * 60 * 60 }
            : {},
        dataset_selectors: [buildDatasetSelector(datasetSelectorKind, datasetRid.trim())],
        transaction_selectors: [buildTransactionSelector(transactionSelectorKind, branch, count, ageDays)],
        legacy_deprecation_status: policyType === 'legacy' ? 'deprecated' : undefined,
        legacy_config_yaml: policyType === 'legacy' ? legacyYaml : undefined,
        allow_latest_view_deletion: allowLatestViewDeletion,
        abort_open_transactions: abortOpenTransactions,
        danger_acknowledgement: dangerous ? DANGER_ACK : '',
        grace_period_minutes: 60,
        updated_by: 'control-panel',
      });
      setNotice(`Created retention policy "${created.name}".`);
      setName('');
      setDangerAck(false);
      await refresh();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Create failed');
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="of-page" style={{ padding: 24, display: 'grid', gap: 16 }}>
      <Link to="/control-panel" style={{ color: 'var(--text-muted)', fontSize: 13 }}>← Back to control panel</Link>
      <header>
        <h1 className="of-heading-xl">Retention policies</h1>
        <p className="of-text-muted" style={{ marginTop: 4, maxWidth: 760 }}>
          Configure recommended, custom, and legacy retention policies with structured dataset and transaction selectors.
        </p>
      </header>

      {error && <div className="of-status-danger" style={{ padding: '10px 14px', borderRadius: 6, fontSize: 13 }}>{error}</div>}
      {notice && <div className="of-status-success" style={{ padding: '10px 14px', borderRadius: 6, fontSize: 13 }}>{notice}</div>}

      <section style={{ display: 'grid', gridTemplateColumns: 'repeat(4, minmax(0, 1fr))', gap: 10 }}>
        <Metric label="Policies" value={summary.total} />
        <Metric label="Recommended" value={summary.recommended} />
        <Metric label="Custom" value={summary.custom} />
        <Metric label="Legacy" value={summary.legacy} />
      </section>

      <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 10 }}>
        <p className="of-eyebrow">New Policy</p>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: 8 }}>
          <label style={{ fontSize: 12 }}>Name<input value={name} onChange={(e) => setName(e.target.value)} className="of-input" style={{ marginTop: 4 }} /></label>
          <label style={{ fontSize: 12 }}>Policy type<select value={policyType} onChange={(e) => setPolicyType(e.target.value as RetentionPolicyType)} className="of-input" style={{ marginTop: 4 }}><option value="custom">Custom</option><option value="legacy">Legacy YAML-style</option></select></label>
          <label style={{ fontSize: 12 }}>Space ID<input value={spaceId} onChange={(e) => setSpaceId(e.target.value)} placeholder="optional UUID" className="of-input" style={{ marginTop: 4 }} /></label>
          <label style={{ fontSize: 12 }}>Target<select value={targetKind} onChange={(e) => setTargetKind(e.target.value as 'dataset' | 'transaction')} className="of-input" style={{ marginTop: 4 }}><option value="transaction">Transactions</option><option value="dataset">Datasets</option></select></label>
          <label style={{ fontSize: 12 }}>Retention days<input type="number" min={0} value={retentionDays} onChange={(e) => setRetentionDays(Number(e.target.value) || 0)} className="of-input" style={{ marginTop: 4 }} /></label>
        </div>
        <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))', gap: 8 }}>
          <label style={{ fontSize: 12 }}>Dataset selector<select value={datasetSelectorKind} onChange={(e) => setDatasetSelectorKind(e.target.value as RetentionDatasetSelectorKind)} className="of-input" style={{ marginTop: 4 }}><option value="all">All datasets</option><option value="dataset_rids">Dataset RID</option><option value="folder_rids">Folder RID</option><option value="derived">Derived datasets</option><option value="trash">Trash</option></select></label>
          <label style={{ fontSize: 12 }}>Dataset or folder RID<input value={datasetRid} onChange={(e) => setDatasetRid(e.target.value)} disabled={datasetSelectorKind === 'all' || datasetSelectorKind === 'derived' || datasetSelectorKind === 'trash'} className="of-input" style={{ marginTop: 4 }} /></label>
          <label style={{ fontSize: 12 }}>Transaction selector<select value={transactionSelectorKind} onChange={(e) => setTransactionSelectorKind(e.target.value as RetentionTransactionSelectorKind)} className="of-input" style={{ marginTop: 4 }}><option value="older_than">Older than age</option><option value="transaction_count">Keep latest transactions</option><option value="view_count">Keep latest views</option><option value="only_branch">Only branch</option><option value="not_branch">Exclude branch</option><option value="aborted">Aborted only</option></select></label>
          {(transactionSelectorKind === 'only_branch' || transactionSelectorKind === 'not_branch') && <label style={{ fontSize: 12 }}>Branch<input value={branch} onChange={(e) => setBranch(e.target.value)} className="of-input" style={{ marginTop: 4 }} /></label>}
          {(transactionSelectorKind === 'transaction_count' || transactionSelectorKind === 'view_count') && <label style={{ fontSize: 12 }}>Count<input type="number" min={0} value={count} onChange={(e) => setCount(Number(e.target.value) || 0)} className="of-input" style={{ marginTop: 4 }} /></label>}
          {transactionSelectorKind === 'older_than' && <label style={{ fontSize: 12 }}>Age days<input type="number" min={1} value={ageDays} onChange={(e) => setAgeDays(Number(e.target.value) || 1)} className="of-input" style={{ marginTop: 4 }} /></label>}
        </div>
        {policyType === 'legacy' && <textarea value={legacyYaml} onChange={(e) => setLegacyYaml(e.target.value)} rows={3} placeholder="Legacy retention YAML" className="of-input" />}
        <div className="of-status-warning" style={{ padding: 10, borderRadius: 6, display: 'grid', gap: 6, fontSize: 12 }}>
          <label><input type="checkbox" checked={allowLatestViewDeletion} onChange={(e) => setAllowLatestViewDeletion(e.target.checked)} /> Allow current/latest-view transaction deletion</label>
          <label><input type="checkbox" checked={abortOpenTransactions} onChange={(e) => setAbortOpenTransactions(e.target.checked)} /> Allow aborting open transactions</label>
          {(allowLatestViewDeletion || abortOpenTransactions) && <label><input type="checkbox" checked={dangerAck} onChange={(e) => setDangerAck(e.target.checked)} /> I understand this can delete current data or abort active writes</label>}
        </div>
        <button type="button" onClick={() => void createPolicy()} disabled={busy} className="of-button of-button--primary" style={{ width: 'fit-content' }}>Create policy</button>
      </section>


      <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
        <div>
          <p className="of-eyebrow" style={{ margin: 0 }}>SG.37 execution</p>
          <h2 style={{ margin: '4px 0' }}>Mark-and-sweep execution and recovery windows</h2>
          <p className="of-text-muted" style={{ margin: 0 }}>Execute active policies against a dataset, mark matching transactions, expose 7-day remediation windows, and warn before irreversible sweep.</p>
        </div>
        <div style={{ display: 'grid', gridTemplateColumns: '2fr 1fr 1fr', gap: 10 }}>
          <label style={{ fontSize: 12 }}>Dataset RID<input value={executionDatasetRid} onChange={(e) => setExecutionDatasetRid(e.target.value)} placeholder="ri.datasets.main..." className="of-input" style={{ marginTop: 4 }} /></label>
          <label style={{ fontSize: 12 }}>As-of days<input type="number" min={0} value={executionAsOfDays} onChange={(e) => setExecutionAsOfDays(Number(e.target.value) || 0)} className="of-input" style={{ marginTop: 4 }} /></label>
          <label style={{ alignSelf: 'end', fontSize: 12 }}><input type="checkbox" checked={executionDryRun} onChange={(e) => setExecutionDryRun(e.target.checked)} /> Dry run only</label>
        </div>
        <button type="button" onClick={() => void executeRetention()} disabled={busy} className="of-button" style={{ width: 'fit-content' }}>Run retention execution</button>
        <div style={{ overflow: 'auto' }}>
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12 }}>
            <thead><tr>{['Dataset', 'Status', 'Marked', 'Swept', 'DELETE txns', 'Recovery', 'Warnings'].map((h) => <th key={h} style={{ textAlign: 'left', padding: 6, borderBottom: '1px solid var(--border-default)' }}>{h}</th>)}</tr></thead>
            <tbody>
              {executions.map((run) => (
                <tr key={run.id} style={{ borderBottom: '1px solid var(--border-subtle)' }}>
                  <td style={{ padding: 6 }}>{run.dataset_rid}</td>
                  <td style={{ padding: 6 }}>{run.status}{run.dry_run ? ' / dry-run' : ''}</td>
                  <td style={{ padding: 6 }}>{run.marked_transaction_count}</td>
                  <td style={{ padding: 6 }}>{run.swept_transaction_count}</td>
                  <td style={{ padding: 6 }}>{run.delete_transaction_count}</td>
                  <td style={{ padding: 6 }}>{run.recovery_window_days}d · irreversible {run.irreversible_after ? new Date(run.irreversible_after).toLocaleString() : 'n/a'}</td>
                  <td style={{ padding: 6, color: run.swept_transaction_count > 0 ? '#b91c1c' : 'var(--text-muted)' }}>{run.warnings?.[0] ?? 'None'}</td>
                </tr>
              ))}
              {executions.length === 0 && <tr><td colSpan={7} className="of-text-muted" style={{ padding: 18, textAlign: 'center' }}>No retention execution history.</td></tr>}
            </tbody>
          </table>
        </div>
      </section>

      <section className="of-panel" style={{ padding: 16, overflow: 'auto' }}>
        {loading ? <p className="of-text-muted">Loading retention policies...</p> : (
          <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 12 }}>
            <thead>
              <tr>{['Name', 'Type', 'Scope', 'Selectors', 'Retention', 'Warnings'].map((h) => <th key={h} style={{ textAlign: 'left', padding: 6, borderBottom: '1px solid var(--border-default)' }}>{h}</th>)}</tr>
            </thead>
            <tbody>
              {policies.map((policy) => (
                <tr key={policy.id} style={{ borderBottom: '1px solid var(--border-subtle)' }}>
                  <td style={{ padding: 6, fontWeight: 600 }}>{policy.name}</td>
                  <td style={{ padding: 6 }}>{policyTypeOf(policy)}{policy.legacy_deprecation_status ? ` / ${policy.legacy_deprecation_status}` : ''}</td>
                  <td style={{ padding: 6 }}>{policy.space_id ? `space ${policy.space_id}` : policy.scope || 'organization'}</td>
                  <td style={{ padding: 6 }}>{selectorSummary(policy)}</td>
                  <td style={{ padding: 6 }}>{policy.retention_days}d / {policy.purge_mode}</td>
                  <td style={{ padding: 6, color: hasCritical(policy) ? '#b91c1c' : 'var(--text-muted)' }}>{(policy.warnings ?? []).map((w) => w.code).join(', ') || 'None'}</td>
                </tr>
              ))}
              {policies.length === 0 && <tr><td colSpan={6} className="of-text-muted" style={{ padding: 18, textAlign: 'center' }}>No retention policies.</td></tr>}
            </tbody>
          </table>
        )}
      </section>
    </section>
  );
}

function Metric({ label, value }: { label: string; value: number }) {
  return (
    <div className="of-panel-muted" style={{ padding: 12 }}>
      <p className="of-eyebrow" style={{ margin: 0 }}>{label}</p>
      <p className="of-heading-lg" style={{ margin: '4px 0 0' }}>{value}</p>
    </div>
  );
}

function buildDatasetSelector(kind: RetentionDatasetSelectorKind, value: string) {
  if (kind === 'dataset_rids') return { mode: 'select', kind, dataset_rids: value ? [value] : [] };
  if (kind === 'folder_rids') return { mode: 'select', kind, folder_rids: value ? [value] : [] };
  return { mode: 'select', kind };
}

function buildTransactionSelector(kind: RetentionTransactionSelectorKind, branch: string, count: number, ageDays: number) {
  if (kind === 'only_branch' || kind === 'not_branch') return { kind, branch };
  if (kind === 'transaction_count' || kind === 'view_count') return { kind, count };
  if (kind === 'older_than') return { kind, duration_seconds: ageDays * 24 * 60 * 60 };
  return { kind };
}

function policyTypeOf(policy: RetentionPolicy) {
  return policy.policy_type ?? (policy.is_system ? 'recommended' : 'custom');
}

function selectorSummary(policy: RetentionPolicy) {
  const dataset = (policy.dataset_selectors ?? []).map((selector) => `${selector.mode}:${selector.kind}`).join(', ') || 'legacy selector';
  const transaction = (policy.transaction_selectors ?? []).map((selector) => selector.kind).join(', ') || 'default closed transactions';
  return `${dataset} / ${transaction}`;
}

function hasCritical(policy: RetentionPolicy) {
  return (policy.warnings ?? []).some((warning) => warning.severity === 'critical');
}
