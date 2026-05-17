// SG.7: Role-sets administration — Control Panel UI.
//
// Surfaces the role-set catalog (project / ontology / restricted_view
// / platform_admin), the rank-ordered roles inside each set, the
// operation catalog, and the delegation-rank checker.

import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';

import {
  addRoleToRoleSet,
  checkRoleSetDelegation,
  createRoleSet,
  deleteRoleSet,
  listOperations,
  listRoleSets,
  removeRoleFromRoleSet,
  type CheckDelegationResponse,
  type OperationCatalogEntry,
  type RoleSetContext,
  type RoleSetResponse,
} from '@/lib/api/role-sets';

const CONTEXTS: RoleSetContext[] = [
  'project',
  'ontology',
  'restricted_view',
  'platform_admin',
];

export function RoleSetsPage() {
  const [filter, setFilter] = useState<RoleSetContext | ''>('');
  const [items, setItems] = useState<RoleSetResponse[]>([]);
  const [operations, setOperations] = useState<OperationCatalogEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const refresh = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const [rs, ops] = await Promise.all([
        listRoleSets(filter || undefined),
        listOperations(),
      ]);
      setItems(rs);
      setOperations(ops.items);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to load role sets');
    } finally {
      setLoading(false);
    }
  }, [filter]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  return (
    <section className="of-page" style={{ padding: 24, display: 'grid', gap: 16 }}>
      <Link to="/control-panel" style={{ color: 'var(--text-muted)', fontSize: 13 }}>
        ← Control panel
      </Link>

      <header>
        <h1 className="of-heading-xl">Role sets &amp; operations</h1>
        <p className="of-text-muted" style={{ marginTop: 4 }}>
          Default Owner / Editor / Viewer / Discoverer roles bundled per
          resource context, plus the low-level operation catalog services
          check. Grants cannot exceed the grantor's rank in the same role set.
          See{' '}
          <a href="/docs/security-governance/policies-and-authorization" target="_blank" rel="noreferrer">
            policies and authorization
          </a>{' '}
          for the parity scope.
        </p>
      </header>

      {error && (
        <div className="of-status-danger" style={{ padding: '10px 14px', borderRadius: 'var(--radius-md)', fontSize: 13 }}>
          {error}
        </div>
      )}

      <FilterBar filter={filter} onChange={setFilter} />
      <CreateForm onCreated={() => void refresh()} onError={setError} />
      <DelegationCheckSection roleSets={items} onError={setError} />

      <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
        <h2 className="of-heading-lg" style={{ fontSize: 14, textTransform: 'uppercase', letterSpacing: '0.12em' }}>
          Role sets
        </h2>
        {loading ? (
          <p className="of-text-muted">Loading…</p>
        ) : items.length === 0 ? (
          <p className="of-text-muted">No role sets match this filter.</p>
        ) : (
          items.map((rs) => (
            <RoleSetCard key={rs.id} roleSet={rs} onChange={() => void refresh()} onError={setError} />
          ))
        )}
      </section>

      <OperationCatalog items={operations} />
    </section>
  );
}

function FilterBar({
  filter,
  onChange,
}: {
  filter: RoleSetContext | '';
  onChange: (next: RoleSetContext | '') => void;
}) {
  return (
    <section className="of-panel" style={{ padding: 16, display: 'flex', gap: 8, alignItems: 'flex-end' }}>
      <label style={{ fontSize: 12 }}>
        Filter by context
        <select
          className="of-input"
          value={filter}
          onChange={(e) => onChange(e.target.value as RoleSetContext | '')}
          style={{ marginTop: 4 }}
        >
          <option value="">all</option>
          {CONTEXTS.map((c) => (
            <option key={c} value={c}>{c}</option>
          ))}
        </select>
      </label>
    </section>
  );
}

function CreateForm({
  onCreated,
  onError,
}: {
  onCreated: () => void;
  onError: (msg: string) => void;
}) {
  const [slug, setSlug] = useState('');
  const [name, setName] = useState('');
  const [context, setContext] = useState<RoleSetContext>('project');
  const [description, setDescription] = useState('');
  const [busy, setBusy] = useState(false);

  async function create() {
    if (!slug.trim() || !name.trim()) {
      onError('slug and name are required');
      return;
    }
    setBusy(true);
    try {
      await createRoleSet({
        slug: slug.trim(),
        name: name.trim(),
        context,
        description: description.trim() || undefined,
      });
      setSlug('');
      setName('');
      setDescription('');
      onCreated();
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to create role set');
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 8 }}>
      <h2 className="of-heading-lg" style={{ fontSize: 14, textTransform: 'uppercase', letterSpacing: '0.12em' }}>
        Create role set
      </h2>
      <div style={{ display: 'grid', gap: 8, gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))' }}>
        <label style={{ fontSize: 12 }}>
          Slug
          <input className="of-input" value={slug} onChange={(e) => setSlug(e.target.value)} style={{ marginTop: 4 }} />
        </label>
        <label style={{ fontSize: 12 }}>
          Name
          <input className="of-input" value={name} onChange={(e) => setName(e.target.value)} style={{ marginTop: 4 }} />
        </label>
        <label style={{ fontSize: 12 }}>
          Context
          <select
            className="of-input"
            value={context}
            onChange={(e) => setContext(e.target.value as RoleSetContext)}
            style={{ marginTop: 4 }}
          >
            {CONTEXTS.map((c) => (
              <option key={c} value={c}>{c}</option>
            ))}
          </select>
        </label>
        <label style={{ fontSize: 12 }}>
          Description
          <input className="of-input" value={description} onChange={(e) => setDescription(e.target.value)} style={{ marginTop: 4 }} />
        </label>
      </div>
      <div>
        <button className="of-button of-button--primary" disabled={busy} onClick={() => void create()}>
          {busy ? 'Creating…' : 'Create role set'}
        </button>
      </div>
    </section>
  );
}

function RoleSetCard({
  roleSet,
  onChange,
  onError,
}: {
  roleSet: RoleSetResponse;
  onChange: () => void;
  onError: (msg: string) => void;
}) {
  const [roleID, setRoleID] = useState('');
  const [rank, setRank] = useState('1');

  async function add() {
    if (!roleID.trim() || !rank.trim()) {
      return;
    }
    const parsed = parseInt(rank, 10);
    if (!Number.isFinite(parsed) || parsed <= 0) {
      onError('rank must be a positive integer');
      return;
    }
    try {
      await addRoleToRoleSet(roleSet.id, { role_id: roleID.trim(), rank: parsed });
      setRoleID('');
      setRank('1');
      onChange();
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to add role');
    }
  }

  async function remove(rid: string) {
    try {
      await removeRoleFromRoleSet(roleSet.id, rid);
      onChange();
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to remove role');
    }
  }

  async function destroy() {
    if (!confirm(`Delete role set "${roleSet.name}"?`)) {
      return;
    }
    try {
      await deleteRoleSet(roleSet.id);
      onChange();
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to delete role set');
    }
  }

  return (
    <article style={{ padding: 12, borderRadius: 8, background: 'var(--bg-subtle)', display: 'grid', gap: 8 }}>
      <header style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 8 }}>
        <div>
          <strong>{roleSet.name}</strong>
          <div className="of-text-muted" style={{ fontSize: 11 }}>
            {roleSet.context} · slug <code>{roleSet.slug}</code> · ID <code>{roleSet.id}</code>
          </div>
          {roleSet.description && (
            <p style={{ fontSize: 12, margin: '4px 0 0' }}>{roleSet.description}</p>
          )}
        </div>
        <button className="of-button of-button--ghost" style={{ color: '#b91c1c' }} onClick={() => void destroy()}>
          Delete
        </button>
      </header>
      <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'grid', gap: 4, fontSize: 12 }}>
        {roleSet.roles.length === 0 && <li className="of-text-muted">No roles yet.</li>}
        {roleSet.roles.map((r) => (
          <li key={r.role_id} style={{ display: 'flex', justifyContent: 'space-between', gap: 6 }}>
            <span>
              <strong>{r.role_name}</strong> <span className="of-text-muted">(rank {r.rank})</span>{' '}
              <code className="of-text-muted">{r.role_id}</code>
            </span>
            <button className="of-button of-button--ghost" onClick={() => void remove(r.role_id)} style={{ fontSize: 11 }}>
              Remove
            </button>
          </li>
        ))}
      </ul>
      <div style={{ display: 'flex', gap: 6, alignItems: 'flex-end', flexWrap: 'wrap' }}>
        <label style={{ fontSize: 11, flex: '1 1 200px' }}>
          Role ID
          <input className="of-input" value={roleID} onChange={(e) => setRoleID(e.target.value)} style={{ marginTop: 4 }} />
        </label>
        <label style={{ fontSize: 11 }}>
          Rank
          <input
            className="of-input"
            type="number"
            min={1}
            value={rank}
            onChange={(e) => setRank(e.target.value)}
            style={{ marginTop: 4, width: 80 }}
          />
        </label>
        <button className="of-button" onClick={() => void add()}>
          Add role
        </button>
      </div>
    </article>
  );
}

function DelegationCheckSection({
  roleSets,
  onError,
}: {
  roleSets: RoleSetResponse[];
  onError: (msg: string) => void;
}) {
  const [roleSetID, setRoleSetID] = useState('');
  const [targetRoleID, setTargetRoleID] = useState('');
  const [grantorID, setGrantorID] = useState('');
  const [result, setResult] = useState<CheckDelegationResponse | null>(null);
  const [busy, setBusy] = useState(false);

  const currentRoles = useMemo(() => {
    const rs = roleSets.find((r) => r.id === roleSetID);
    return rs?.roles ?? [];
  }, [roleSets, roleSetID]);

  async function probe() {
    if (!roleSetID.trim() || !targetRoleID.trim()) {
      return;
    }
    setBusy(true);
    try {
      const resp = await checkRoleSetDelegation(roleSetID.trim(), {
        target_role_id: targetRoleID.trim(),
        grantor_id: grantorID.trim() || undefined,
      });
      setResult(resp);
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to check delegation');
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 8 }}>
      <h2 className="of-heading-lg" style={{ fontSize: 14, textTransform: 'uppercase', letterSpacing: '0.12em' }}>
        Delegation check
      </h2>
      <p className="of-text-muted" style={{ fontSize: 12 }}>
        Verify whether a grantor can grant a target role inside a role set.
        The grantor's highest rank in the set must be ≥ the target role's rank.
      </p>
      <div style={{ display: 'grid', gap: 8, gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))' }}>
        <label style={{ fontSize: 12 }}>
          Role set
          <select
            className="of-input"
            value={roleSetID}
            onChange={(e) => {
              setRoleSetID(e.target.value);
              setTargetRoleID('');
              setResult(null);
            }}
            style={{ marginTop: 4 }}
          >
            <option value="">— select —</option>
            {roleSets.map((rs) => (
              <option key={rs.id} value={rs.id}>{rs.name} ({rs.context})</option>
            ))}
          </select>
        </label>
        <label style={{ fontSize: 12 }}>
          Target role
          <select
            className="of-input"
            value={targetRoleID}
            onChange={(e) => setTargetRoleID(e.target.value)}
            style={{ marginTop: 4 }}
          >
            <option value="">— select —</option>
            {currentRoles.map((r) => (
              <option key={r.role_id} value={r.role_id}>
                {r.role_name} (rank {r.rank})
              </option>
            ))}
          </select>
        </label>
        <label style={{ fontSize: 12 }}>
          Grantor user ID (blank = caller)
          <input className="of-input" value={grantorID} onChange={(e) => setGrantorID(e.target.value)} style={{ marginTop: 4 }} />
        </label>
      </div>
      <div>
        <button className="of-button" disabled={busy} onClick={() => void probe()}>
          {busy ? 'Checking…' : 'Check delegation'}
        </button>
      </div>
      {result && (
        <pre style={{ padding: 12, background: 'var(--bg-surface)', fontSize: 11, fontFamily: 'var(--font-mono)', borderRadius: 8 }}>
          {JSON.stringify(result, null, 2)}
        </pre>
      )}
    </section>
  );
}

function OperationCatalog({ items }: { items: OperationCatalogEntry[] }) {
  const grouped = useMemo(() => {
    const out: Record<string, OperationCatalogEntry[]> = {};
    for (const op of items) {
      const key = op.resource;
      (out[key] ??= []).push(op);
    }
    return out;
  }, [items]);
  const keys = Object.keys(grouped).sort();
  return (
    <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 8 }}>
      <h2 className="of-heading-lg" style={{ fontSize: 14, textTransform: 'uppercase', letterSpacing: '0.12em' }}>
        Operation catalog
      </h2>
      <p className="of-text-muted" style={{ fontSize: 12 }}>
        Low-level <code>resource:action</code> tokens that services check.
        Roles bind to a subset of these to express their capability surface.
      </p>
      {items.length === 0 ? (
        <p className="of-text-muted">No operations seeded.</p>
      ) : (
        <div style={{ display: 'grid', gap: 8, gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))' }}>
          {keys.map((res) => (
            <article key={res} style={{ padding: 8, borderRadius: 8, background: 'var(--bg-subtle)' }}>
              <h3 style={{ fontSize: 12, margin: '0 0 4px', textTransform: 'uppercase', letterSpacing: '0.08em' }}>
                {res}
              </h3>
              <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'grid', gap: 2, fontSize: 11 }}>
                {grouped[res].map((op) => (
                  <li key={op.id}>
                    <code>{op.action}</code>{op.description ? ` — ${op.description}` : ''}
                  </li>
                ))}
              </ul>
            </article>
          ))}
        </div>
      )}
    </section>
  );
}
