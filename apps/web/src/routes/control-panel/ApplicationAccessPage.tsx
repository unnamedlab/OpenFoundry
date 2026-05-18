import { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';

import { JsonEditor, parseJsonOr } from '@/lib/components/JsonEditor';
import {
  decideApplicationAccessChangeRequest,
  evaluateApplicationAccess,
  getApplicationAccessChangeRequests,
  getControlPanel,
  updateControlPanel,
  type ApplicationAccessChangeRequest,
  type ApplicationAccessConfig,
  type ApplicationAccessDecision,
  type ApplicationAccessHistoryEvent,
} from '@/lib/api/control-panel';

const FALLBACK_CONFIG: ApplicationAccessConfig = {
  enabled: true,
  default_visibility: 'visible',
  warning: 'Application access controls application launcher, sidebar, and navigation visibility only; server-side permissions still govern every resource and API request.',
  applications: [],
  rules: [],
  approval_policy: {
    mode: 'self_approve',
    reviewer_user_ids: [],
    reviewer_group_ids: [],
    require_distinct_reviewer_for_policy: true,
    instructions: '',
  },
  change_requests: [],
  history: [],
};

function splitList(value: string) {
  return value.split(',').map((item) => item.trim()).filter(Boolean);
}

function formatDate(value?: string) {
  if (!value) return '';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

function badgeStyle(tone: 'green' | 'red' | 'gray') {
  const colors = {
    green: { bg: '#ecfdf5', fg: '#047857', border: '#a7f3d0' },
    red: { bg: '#fef2f2', fg: '#b91c1c', border: '#fecaca' },
    gray: { bg: 'var(--bg-subtle)', fg: 'var(--text-muted)', border: 'var(--border-default)' },
  }[tone];
  return {
    display: 'inline-flex',
    alignItems: 'center',
    borderRadius: 999,
    border: `1px solid ${colors.border}`,
    background: colors.bg,
    color: colors.fg,
    padding: '2px 8px',
    fontSize: 11,
    fontWeight: 600,
  };
}

export function ApplicationAccessPage() {
  const [config, setConfig] = useState<ApplicationAccessConfig>(FALLBACK_CONFIG);
  const [configJson, setConfigJson] = useState(JSON.stringify(FALLBACK_CONFIG, null, 2));
  const [changeRequests, setChangeRequests] = useState<ApplicationAccessChangeRequest[]>([]);
  const [history, setHistory] = useState<ApplicationAccessHistoryEvent[]>([]);
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [saved, setSaved] = useState('');
  const [evaluation, setEvaluation] = useState<ApplicationAccessDecision[]>([]);
  const [evalAppID, setEvalAppID] = useState('control-panel');
  const [evalUserID, setEvalUserID] = useState('');
  const [evalOrgID, setEvalOrgID] = useState('');
  const [evalGroups, setEvalGroups] = useState('');
  const [evalLifecycle, setEvalLifecycle] = useState('');

  const pendingRequests = useMemo(
    () => changeRequests.filter((request) => request.status === 'pending'),
    [changeRequests],
  );

  function hydrate(next: ApplicationAccessConfig) {
    const normalized = next ?? FALLBACK_CONFIG;
    setConfig(normalized);
    setConfigJson(JSON.stringify(normalized, null, 2));
    setChangeRequests(normalized.change_requests ?? []);
    setHistory(normalized.history ?? []);
  }

  async function refresh() {
    setLoading(true);
    setError('');
    try {
      const [settings, requestResp] = await Promise.all([
        getControlPanel(),
        getApplicationAccessChangeRequests().catch(() => null),
      ]);
      hydrate(settings.application_access ?? FALLBACK_CONFIG);
      if (requestResp) {
        setChangeRequests(requestResp.change_requests ?? []);
        setHistory(requestResp.history ?? []);
      }
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to load application access');
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void refresh();
  }, []);

  async function save() {
    setBusy(true);
    setError('');
    setSaved('');
    try {
      const parsed = parseJsonOr<ApplicationAccessConfig>(configJson, config);
      const settings = await updateControlPanel({ application_access: parsed });
      hydrate(settings.application_access ?? FALLBACK_CONFIG);
      const pending = (settings.application_access?.change_requests ?? []).filter((request) => request.status === 'pending');
      setSaved(pending.length > pendingRequests.length ? 'Change request created' : 'Saved and recorded');
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Save failed');
    } finally {
      setBusy(false);
    }
  }

  async function runEvaluation() {
    setBusy(true);
    setError('');
    try {
      const resp = await evaluateApplicationAccess({
        application_id: evalAppID,
        user_id: evalUserID,
        organization_id: evalOrgID,
        group_ids: splitList(evalGroups),
        lifecycle_stage: evalLifecycle,
      });
      setEvaluation(resp.decisions);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Evaluation failed');
    } finally {
      setBusy(false);
    }
  }

  async function decide(id: string, decision: 'approved' | 'rejected') {
    setBusy(true);
    setError('');
    setSaved('');
    try {
      const next = await decideApplicationAccessChangeRequest(id, decision);
      hydrate(next);
      setSaved(decision === 'approved' ? 'Change request approved and applied' : 'Change request rejected');
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Decision failed');
    } finally {
      setBusy(false);
    }
  }

  function addBlockRuleForGroups() {
    const parsed = parseJsonOr<ApplicationAccessConfig>(configJson, config);
    const index = (parsed.rules ?? []).length + 1;
    const next: ApplicationAccessConfig = {
      ...parsed,
      rules: [
        ...(parsed.rules ?? []),
        {
          id: `block-rule-${index}`,
          name: `Block rule ${index}`,
          effect: 'block',
          application_ids: [evalAppID || 'ai'],
          organization_ids: [],
          user_ids: [],
          group_ids: splitList(evalGroups || 'restricted-users'),
          lifecycle_stages: evalLifecycle ? [evalLifecycle] : [],
          enabled: true,
          reason: 'Application access UX scope.',
        },
      ],
    };
    hydrate(next);
    setSaved('');
  }

  return (
    <section className="of-page" style={{ padding: 24, display: 'grid', gap: 16 }}>
      <Link to="/control-panel" style={{ color: 'var(--text-muted)', fontSize: 13 }}>
        Back to Control Panel
      </Link>

      <header>
        <h1 className="of-heading-xl">Application access</h1>
        <p className="of-text-muted" style={{ marginTop: 4 }}>
          Organization-level application visibility and approval history.
        </p>
      </header>

      <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 8, borderColor: '#fbbf24' }}>
        <div style={{ display: 'flex', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}>
          <span style={badgeStyle('gray')}>UX scope only</span>
          <strong style={{ fontSize: 13 }}>Server-side permissions still apply.</strong>
        </div>
        <p className="of-text-muted" style={{ margin: 0, fontSize: 13 }}>
          {config.warning || FALLBACK_CONFIG.warning}
        </p>
      </section>

      {error ? (
        <div className="of-status-danger" style={{ padding: '10px 14px', borderRadius: 'var(--radius-md)', fontSize: 13 }}>
          {error}
        </div>
      ) : null}
      {saved ? (
        <div className="of-status-success" style={{ padding: '10px 14px', borderRadius: 'var(--radius-md)', fontSize: 13 }}>
          {saved}
        </div>
      ) : null}
      {loading ? <p className="of-text-muted">Loading...</p> : null}

      {!loading ? (
        <>
          <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
            <p className="of-eyebrow">Snapshot</p>
            <div style={{ display: 'grid', gap: 10, gridTemplateColumns: 'repeat(auto-fit, minmax(160px, 1fr))' }}>
              <div>
                <p className="of-text-muted" style={{ fontSize: 12, margin: 0 }}>Status</p>
                <strong>{config.enabled ? 'Enabled' : 'Disabled'}</strong>
              </div>
              <div>
                <p className="of-text-muted" style={{ fontSize: 12, margin: 0 }}>Default</p>
                <strong>{config.default_visibility}</strong>
              </div>
              <div>
                <p className="of-text-muted" style={{ fontSize: 12, margin: 0 }}>Applications</p>
                <strong>{config.applications.length}</strong>
              </div>
              <div>
                <p className="of-text-muted" style={{ fontSize: 12, margin: 0 }}>Rules</p>
                <strong>{config.rules.length}</strong>
              </div>
              <div>
                <p className="of-text-muted" style={{ fontSize: 12, margin: 0 }}>Pending</p>
                <strong>{pendingRequests.length}</strong>
              </div>
            </div>
          </section>

          <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'center', flexWrap: 'wrap' }}>
              <p className="of-eyebrow" style={{ margin: 0 }}>Configuration JSON</p>
              <button type="button" className="of-button" onClick={addBlockRuleForGroups}>
                Add block rule
              </button>
            </div>
            <JsonEditor
              label="Application access configuration"
              value={configJson}
              onChange={(value) => {
                setConfigJson(value);
                setSaved('');
              }}
              minHeight={360}
            />
            <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
              <button type="button" className="of-button of-button--primary" disabled={busy} onClick={() => void save()}>
                {busy ? 'Saving...' : 'Request or apply change'}
              </button>
            </div>
          </section>

          <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
            <p className="of-eyebrow">Evaluate visibility</p>
            <div style={{ display: 'grid', gap: 8, gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))' }}>
              <label style={{ fontSize: 13 }}>
                Application ID
                <input className="of-input" value={evalAppID} onChange={(event) => setEvalAppID(event.target.value)} style={{ marginTop: 4 }} />
              </label>
              <label style={{ fontSize: 13 }}>
                User ID
                <input className="of-input" value={evalUserID} onChange={(event) => setEvalUserID(event.target.value)} style={{ marginTop: 4 }} />
              </label>
              <label style={{ fontSize: 13 }}>
                Organization ID
                <input className="of-input" value={evalOrgID} onChange={(event) => setEvalOrgID(event.target.value)} style={{ marginTop: 4 }} />
              </label>
              <label style={{ fontSize: 13 }}>
                Group IDs
                <input className="of-input" value={evalGroups} onChange={(event) => setEvalGroups(event.target.value)} placeholder="group-a, group-b" style={{ marginTop: 4 }} />
              </label>
              <label style={{ fontSize: 13 }}>
                Lifecycle stage
                <input className="of-input" value={evalLifecycle} onChange={(event) => setEvalLifecycle(event.target.value)} placeholder="beta" style={{ marginTop: 4 }} />
              </label>
            </div>
            <div>
              <button type="button" className="of-button" disabled={busy || !evalAppID.trim()} onClick={() => void runEvaluation()}>
                Evaluate
              </button>
            </div>
            {evaluation.length > 0 ? (
              <div style={{ display: 'grid', gap: 8 }}>
                {evaluation.map((decision) => (
                  <article key={decision.application_id} className="of-panel" style={{ padding: 12, display: 'grid', gap: 6 }}>
                    <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8, flexWrap: 'wrap' }}>
                      <strong>{decision.application_id}</strong>
                      <span style={badgeStyle(decision.visible ? 'green' : 'red')}>{decision.visible ? 'visible' : 'hidden'}</span>
                    </div>
                    <p className="of-text-muted" style={{ margin: 0, fontSize: 13 }}>
                      {decision.decision} - {decision.reason}
                    </p>
                    {decision.matched_rule_names.length > 0 ? (
                      <p className="of-text-muted" style={{ margin: 0, fontSize: 12 }}>
                        Rules: {decision.matched_rule_names.join(', ')}
                      </p>
                    ) : null}
                  </article>
                ))}
              </div>
            ) : null}
          </section>

          <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
            <p className="of-eyebrow">Change requests</p>
            {changeRequests.length === 0 ? <p className="of-text-muted">No application access changes recorded.</p> : null}
            <div style={{ display: 'grid', gap: 8 }}>
              {changeRequests.slice().reverse().map((request) => (
                <article key={request.id} className="of-panel" style={{ padding: 12, display: 'grid', gap: 8 }}>
                  <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8, flexWrap: 'wrap' }}>
                    <div>
                      <strong>{request.summary}</strong>
                      <p className="of-text-muted" style={{ margin: '2px 0 0', fontSize: 12 }}>
                        {request.kind} by {request.requested_by} at {formatDate(request.requested_at)}
                      </p>
                    </div>
                    <span style={badgeStyle(request.status === 'approved' ? 'green' : request.status === 'rejected' ? 'red' : 'gray')}>
                      {request.status}
                    </span>
                  </div>
                  {request.status === 'pending' ? (
                    <div style={{ display: 'flex', gap: 8, justifyContent: 'flex-end', flexWrap: 'wrap' }}>
                      <button type="button" className="of-button" disabled={busy} onClick={() => void decide(request.id, 'rejected')}>
                        Reject
                      </button>
                      <button type="button" className="of-button of-button--primary" disabled={busy} onClick={() => void decide(request.id, 'approved')}>
                        Approve
                      </button>
                    </div>
                  ) : null}
                </article>
              ))}
            </div>
          </section>

          <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
            <p className="of-eyebrow">History</p>
            {history.length === 0 ? <p className="of-text-muted">No historical decisions yet.</p> : null}
            <div style={{ display: 'grid', gap: 8 }}>
              {history.slice().reverse().map((event) => (
                <article key={event.id} style={{ display: 'grid', gap: 2, paddingBottom: 8, borderBottom: '1px solid var(--border-subtle)' }}>
                  <strong style={{ fontSize: 13 }}>{event.summary}</strong>
                  <p className="of-text-muted" style={{ margin: 0, fontSize: 12 }}>
                    {event.action} by {event.actor} at {formatDate(event.timestamp)} - {event.application_count} apps, {event.rule_count} rules
                  </p>
                </article>
              ))}
            </div>
          </section>
        </>
      ) : null}
    </section>
  );
}
