import { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';

import {
  dataConnection,
  type NetworkEgressPolicy,
} from '@/lib/api/data-connection';
import { CreateEgressPolicyModal } from '@/lib/components/data-connection/CreateEgressPolicyModal';

export function EgressPoliciesPage() {
  const [policies, setPolicies] = useState<NetworkEgressPolicy[]>([]);
  const [loading, setLoading] = useState(true);
  const [deleteId, setDeleteId] = useState<string | null>(null);
  const [createOpen, setCreateOpen] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');

  const summary = useMemo(() => ({
    total: policies.length,
    global: policies.filter((policy) => policy.is_global).length,
    proxied: policies.filter((policy) => policy.kind === 'agent_proxy').length,
  }), [policies]);

  async function load() {
    setLoading(true);
    setError('');
    try {
      setPolicies(await dataConnection.listEgressPolicies());
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to load policies');
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void load();
  }, []);

  async function remove(p: NetworkEgressPolicy) {
    if (typeof window !== 'undefined' && !window.confirm(`Delete policy "${p.name}"?`)) return;
    setDeleteId(p.id);
    setNotice('');
    try {
      await dataConnection.deleteEgressPolicy(p.id);
      setNotice(`Policy "${p.name}" deleted.`);
      await load();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Delete failed');
    } finally {
      setDeleteId(null);
    }
  }

  function handleCreated(policy: NetworkEgressPolicy) {
    setNotice(`Policy "${policy.name}" created.`);
    void load();
  }

  return (
    <section className="of-page" style={{ padding: 24, display: 'grid', gap: 16 }}>
      <Link to="/data-connection" style={{ color: 'var(--text-muted)', fontSize: 13 }}>← Back to sources</Link>
      <header style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 12 }}>
        <div>
          <h1 className="of-heading-xl">Egress policies</h1>
          <p className="of-text-muted" style={{ marginTop: 4, maxWidth: 720 }}>
            Allow-listed network destinations + permission groups for source connections.
          </p>
        </div>
        <button type="button" onClick={() => setCreateOpen(true)} className="of-button of-button--primary">
          + Egress policy
        </button>
      </header>

      {error && (
        <div className="of-status-danger" style={{ padding: '10px 14px', borderRadius: 'var(--radius-md)', fontSize: 13 }}>
          {error}
        </div>
      )}

      {notice && (
        <div className="of-status-success" style={{ padding: '10px 14px', borderRadius: 'var(--radius-md)', fontSize: 13 }}>
          {notice}
        </div>
      )}

      <section style={{ display: 'grid', gridTemplateColumns: 'repeat(3, minmax(0, 1fr))', gap: 10 }}>
        <Metric label="Policies" value={summary.total} />
        <Metric label="Global" value={summary.global} />
        <Metric label="Agent proxy" value={summary.proxied} />
      </section>

      {loading ? (
        <p className="of-text-muted">Loading policies...</p>
      ) : (
        <section className="of-panel" style={{ padding: 16, overflow: 'auto' }}>
          <table style={{ width: '100%', fontSize: 12, borderCollapse: 'collapse' }}>
            <thead>
              <tr>
                {['Name', 'Kind', 'Address', 'Port', 'Protocol', 'Proxy', 'Status', 'Organizations', 'Permissions', ''].map((h) => (
                  <th key={h} style={{ textAlign: 'left', padding: 6, borderBottom: '1px solid var(--border-default)' }}>{h}</th>
                ))}
              </tr>
            </thead>
            <tbody>
              {policies.map((p) => (
                <tr key={p.id} style={{ borderBottom: '1px solid var(--border-subtle)' }}>
                  <td style={{ padding: 6, fontWeight: 600 }}>{p.name}</td>
                  <td style={{ padding: 6 }}>{p.kind}</td>
                  <td style={{ padding: 6, fontFamily: 'var(--font-mono)' }}>{p.address.kind}:{p.address.value}</td>
                  <td style={{ padding: 6, fontFamily: 'var(--font-mono)' }}>{p.port.kind === 'any' ? 'any' : p.port.value}</td>
                  <td style={{ padding: 6 }}>{p.protocol ?? 'tcp'}</td>
                  <td style={{ padding: 6 }}>{p.proxy_mode ?? 'none'}</td>
                  <td style={{ padding: 6 }}>{p.status ?? 'active'}</td>
                  <td style={{ padding: 6, fontSize: 11 }}>{(p.allowed_organizations ?? []).join(', ') || (p.is_global ? 'All/global' : 'Source scoped')}</td>
                  <td style={{ padding: 6, fontSize: 11 }}>{p.permissions.join(', ') || 'None'}</td>
                  <td style={{ padding: 6, textAlign: 'right' }}>
                    <button type="button" onClick={() => void remove(p)} disabled={deleteId === p.id} className="of-button" style={{ fontSize: 11, color: '#b91c1c', borderColor: '#fecaca' }}>
                      {deleteId === p.id ? 'Deleting...' : 'Delete'}
                    </button>
                  </td>
                </tr>
              ))}
              {policies.length === 0 && (
                <tr><td colSpan={10} className="of-text-muted" style={{ padding: 18, textAlign: 'center' }}>No egress policies.</td></tr>
              )}
            </tbody>
          </table>
        </section>
      )}

      <CreateEgressPolicyModal
        open={createOpen}
        onClose={() => setCreateOpen(false)}
        onCreated={handleCreated}
      />
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
