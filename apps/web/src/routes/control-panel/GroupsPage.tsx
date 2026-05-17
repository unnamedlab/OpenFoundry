// SG.5: Group administration — Control Panel UI.
//
// Mirrors the identity-federation-service admin endpoints:
//   - search + paginate groups by q / kind / realm / org / status
//   - create internal / external / rule-based groups
//   - manage administrators (manage / manage_members scope)
//   - manage nested parent ↔ member edges
//   - add direct members with optional expires_at
//   - inspect a group (counts, admins, parents, children, project-access hint)

import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';

import {
  addGroupAdmin,
  addGroupMember,
  addNestedGroup,
  createGroup,
  deleteGroup,
  inspectGroup,
  listGroupMembers,
  patchGroup,
  removeGroupAdmin,
  removeGroupMember,
  removeNestedGroup,
  searchGroups,
  type AdminGroup,
  type GroupAdminScope,
  type GroupInspection,
  type GroupKind,
  type GroupMember,
  type SearchGroupsFilter,
} from '@/lib/api/groups-admin';

const KIND_OPTIONS: GroupKind[] = ['internal', 'external', 'rule_based'];

export function GroupsPage() {
  const [filter, setFilter] = useState<SearchGroupsFilter>({ limit: 50, offset: 0 });
  const [groups, setGroups] = useState<AdminGroup[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');
  const [inspection, setInspection] = useState<GroupInspection | null>(null);
  const [members, setMembers] = useState<GroupMember[]>([]);

  const refresh = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const res = await searchGroups(filter);
      setGroups(res.items);
      setTotal(res.total);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to load groups');
    } finally {
      setLoading(false);
    }
  }, [filter]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const loadInspection = useCallback(async (groupId: string) => {
    try {
      const [insp, mems] = await Promise.all([
        inspectGroup(groupId),
        listGroupMembers(groupId),
      ]);
      setInspection(insp);
      setMembers(mems);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to inspect group');
    }
  }, []);

  async function toggleStatus(g: AdminGroup) {
    try {
      await patchGroup(g.id, { status: g.status === 'active' ? 'archived' : 'active' });
      await refresh();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to update group');
    }
  }

  async function remove(g: AdminGroup) {
    if (!confirm(`Delete group "${g.display_name}"? This removes admins, members, and nested edges.`)) {
      return;
    }
    try {
      await deleteGroup(g.id);
      if (inspection?.group.id === g.id) {
        setInspection(null);
      }
      await refresh();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to delete group');
    }
  }

  const pages = Math.max(1, Math.ceil(total / (filter.limit ?? 50)));
  const currentPage = Math.floor((filter.offset ?? 0) / (filter.limit ?? 50)) + 1;

  return (
    <section className="of-page" style={{ padding: 24, display: 'grid', gap: 16 }}>
      <Link to="/control-panel" style={{ color: 'var(--text-muted)', fontSize: 13 }}>
        ← Control panel
      </Link>

      <header>
        <h1 className="of-heading-xl">Groups</h1>
        <p className="of-text-muted" style={{ marginTop: 4 }}>
          Internal, external, and rule-based groups with optional membership
          expirations and nested membership. See{' '}
          <a href="/docs/security-governance/identity-and-access" target="_blank" rel="noreferrer">
            identity and access
          </a>{' '}
          for parity scope.
        </p>
      </header>

      {error && (
        <div className="of-status-danger" style={{ padding: '10px 14px', borderRadius: 'var(--radius-md)', fontSize: 13 }}>
          {error}
        </div>
      )}

      <FilterBar filter={filter} onChange={setFilter} />

      <CreateForm onCreated={() => void refresh()} onError={setError} />

      <section className="of-panel" style={{ padding: 0, overflow: 'hidden' }}>
        <table style={{ width: '100%', borderCollapse: 'collapse', fontSize: 13 }}>
          <thead style={{ background: 'var(--bg-subtle)', textAlign: 'left' }}>
            <tr>
              <th style={{ padding: 10 }}>Group</th>
              <th style={{ padding: 10 }}>Kind</th>
              <th style={{ padding: 10 }}>Realm</th>
              <th style={{ padding: 10 }}>Status</th>
              <th style={{ padding: 10 }}>Updated</th>
              <th style={{ padding: 10 }}>Actions</th>
            </tr>
          </thead>
          <tbody>
            {loading && (
              <tr><td colSpan={6} style={{ padding: 10 }} className="of-text-muted">Loading…</td></tr>
            )}
            {!loading && groups.length === 0 && (
              <tr><td colSpan={6} style={{ padding: 10 }} className="of-text-muted">No groups match.</td></tr>
            )}
            {groups.map((g) => (
              <tr key={g.id} style={{ borderTop: '1px solid var(--border-subtle)' }}>
                <td style={{ padding: 10 }}>
                  <button
                    onClick={() => void loadInspection(g.id)}
                    style={{ background: 'transparent', border: 'none', padding: 0, color: 'var(--text-accent)', cursor: 'pointer', textDecoration: 'underline' }}
                  >
                    {g.display_name}
                  </button>
                  <div className="of-text-muted" style={{ fontSize: 11 }}>@{g.name}</div>
                </td>
                <td style={{ padding: 10 }}>{g.kind}</td>
                <td style={{ padding: 10 }}>{g.realm}</td>
                <td style={{ padding: 10 }}>
                  <span style={{ color: g.status === 'active' ? '#15803d' : '#92400e' }}>{g.status}</span>
                </td>
                <td style={{ padding: 10 }}>{new Date(g.updated_at).toLocaleString()}</td>
                <td style={{ padding: 10 }}>
                  <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                    <button className="of-button of-button--ghost" onClick={() => void toggleStatus(g)}>
                      {g.status === 'active' ? 'Archive' : 'Restore'}
                    </button>
                    <button className="of-button of-button--ghost" style={{ color: '#b91c1c' }} onClick={() => void remove(g)}>
                      Delete
                    </button>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </section>

      <Pagination
        currentPage={currentPage}
        pages={pages}
        total={total}
        onJump={(page) =>
          setFilter((prev) => ({ ...prev, offset: Math.max(0, (page - 1) * (prev.limit ?? 50)) }))
        }
      />

      {inspection && (
        <InspectionPanel
          inspection={inspection}
          members={members}
          onClose={() => {
            setInspection(null);
            setMembers([]);
          }}
          onRefresh={async () => void (await loadInspection(inspection.group.id))}
          onError={setError}
        />
      )}
    </section>
  );
}

function FilterBar({
  filter,
  onChange,
}: {
  filter: SearchGroupsFilter;
  onChange: (next: SearchGroupsFilter) => void;
}) {
  return (
    <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 8 }}>
      <h2 className="of-heading-lg" style={{ fontSize: 14, textTransform: 'uppercase', letterSpacing: '0.12em' }}>
        Search
      </h2>
      <div style={{ display: 'grid', gap: 8, gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))' }}>
        <label style={{ fontSize: 12 }}>
          Query
          <input
            className="of-input"
            value={filter.q ?? ''}
            onChange={(e) => onChange({ ...filter, q: e.target.value, offset: 0 })}
            style={{ marginTop: 4 }}
          />
        </label>
        <label style={{ fontSize: 12 }}>
          Kind
          <select
            className="of-input"
            value={filter.kind ?? ''}
            onChange={(e) =>
              onChange({ ...filter, kind: (e.target.value || undefined) as GroupKind | undefined, offset: 0 })
            }
            style={{ marginTop: 4 }}
          >
            <option value="">any</option>
            {KIND_OPTIONS.map((k) => (
              <option key={k} value={k}>{k}</option>
            ))}
          </select>
        </label>
        <label style={{ fontSize: 12 }}>
          Realm
          <input
            className="of-input"
            value={filter.realm ?? ''}
            onChange={(e) => onChange({ ...filter, realm: e.target.value || undefined, offset: 0 })}
            style={{ marginTop: 4 }}
          />
        </label>
        <label style={{ fontSize: 12 }}>
          Organization ID
          <input
            className="of-input"
            value={filter.organization_id ?? ''}
            onChange={(e) => onChange({ ...filter, organization_id: e.target.value || undefined, offset: 0 })}
            style={{ marginTop: 4 }}
          />
        </label>
        <label style={{ fontSize: 12 }}>
          Status
          <select
            className="of-input"
            value={filter.status ?? ''}
            onChange={(e) =>
              onChange({
                ...filter,
                status: (e.target.value || undefined) as 'active' | 'archived' | undefined,
                offset: 0,
              })
            }
            style={{ marginTop: 4 }}
          >
            <option value="">any</option>
            <option value="active">active</option>
            <option value="archived">archived</option>
          </select>
        </label>
      </div>
    </section>
  );
}

function Pagination({
  currentPage,
  pages,
  total,
  onJump,
}: {
  currentPage: number;
  pages: number;
  total: number;
  onJump: (page: number) => void;
}) {
  return (
    <div className="of-text-muted" style={{ display: 'flex', gap: 12, alignItems: 'center', fontSize: 12 }}>
      <span>
        Page {currentPage} / {pages} · {total} group(s)
      </span>
      <button
        className="of-button of-button--ghost"
        disabled={currentPage <= 1}
        onClick={() => onJump(currentPage - 1)}
      >
        ← Prev
      </button>
      <button
        className="of-button of-button--ghost"
        disabled={currentPage >= pages}
        onClick={() => onJump(currentPage + 1)}
      >
        Next →
      </button>
    </div>
  );
}

function CreateForm({
  onCreated,
  onError,
}: {
  onCreated: () => void;
  onError: (msg: string) => void;
}) {
  const [name, setName] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [description, setDescription] = useState('');
  const [kind, setKind] = useState<GroupKind>('internal');
  const [realm, setRealm] = useState('local');
  const [organizationID, setOrganizationID] = useState('');
  const [busy, setBusy] = useState(false);

  async function create() {
    if (!name.trim()) {
      onError('name is required');
      return;
    }
    setBusy(true);
    try {
      await createGroup({
        name: name.trim(),
        display_name: displayName.trim() || undefined,
        description: description.trim() || undefined,
        kind,
        realm: realm.trim() || undefined,
        organization_id: organizationID.trim() || undefined,
      });
      setName('');
      setDisplayName('');
      setDescription('');
      setOrganizationID('');
      onCreated();
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to create group');
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 8 }}>
      <h2 className="of-heading-lg" style={{ fontSize: 14, textTransform: 'uppercase', letterSpacing: '0.12em' }}>
        Create group
      </h2>
      <div style={{ display: 'grid', gap: 8, gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))' }}>
        <label style={{ fontSize: 12 }}>
          Name (handle)
          <input className="of-input" value={name} onChange={(e) => setName(e.target.value)} style={{ marginTop: 4 }} />
        </label>
        <label style={{ fontSize: 12 }}>
          Display name
          <input className="of-input" value={displayName} onChange={(e) => setDisplayName(e.target.value)} style={{ marginTop: 4 }} />
        </label>
        <label style={{ fontSize: 12 }}>
          Kind
          <select
            className="of-input"
            value={kind}
            onChange={(e) => setKind(e.target.value as GroupKind)}
            style={{ marginTop: 4 }}
          >
            {KIND_OPTIONS.map((k) => (
              <option key={k} value={k}>{k}</option>
            ))}
          </select>
        </label>
        <label style={{ fontSize: 12 }}>
          Realm
          <input className="of-input" value={realm} onChange={(e) => setRealm(e.target.value)} style={{ marginTop: 4 }} />
        </label>
        <label style={{ fontSize: 12 }}>
          Organization ID (optional)
          <input className="of-input" value={organizationID} onChange={(e) => setOrganizationID(e.target.value)} style={{ marginTop: 4 }} />
        </label>
        <label style={{ fontSize: 12 }}>
          Description (optional)
          <input className="of-input" value={description} onChange={(e) => setDescription(e.target.value)} style={{ marginTop: 4 }} />
        </label>
      </div>
      <div>
        <button className="of-button of-button--primary" disabled={busy} onClick={() => void create()}>
          {busy ? 'Creating…' : 'Create group'}
        </button>
      </div>
    </section>
  );
}

function InspectionPanel({
  inspection,
  members,
  onClose,
  onRefresh,
  onError,
}: {
  inspection: GroupInspection;
  members: GroupMember[];
  onClose: () => void;
  onRefresh: () => Promise<void> | void;
  onError: (msg: string) => void;
}) {
  const g = inspection.group;
  const adminsLabel = useMemo(
    () =>
      inspection.admins.length > 0
        ? inspection.admins.map((a) => `${a.user_id}(${a.scope})`).join(', ')
        : '—',
    [inspection.admins],
  );

  return (
    <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
      <header style={{ display: 'flex', justifyContent: 'space-between' }}>
        <h2 className="of-heading-lg" style={{ fontSize: 14, textTransform: 'uppercase', letterSpacing: '0.12em' }}>
          Inspect: {g.display_name}
        </h2>
        <button className="of-button of-button--ghost" onClick={onClose}>
          Close
        </button>
      </header>
      <dl style={{ display: 'grid', gridTemplateColumns: 'auto 1fr', gap: '4px 12px', fontSize: 12 }}>
        <dt className="of-text-muted">ID</dt>
        <dd><code>{g.id}</code></dd>
        <dt className="of-text-muted">Handle</dt>
        <dd>@{g.name}</dd>
        <dt className="of-text-muted">Kind / status</dt>
        <dd>{g.kind} · {g.status}</dd>
        <dt className="of-text-muted">Members</dt>
        <dd>
          direct {inspection.direct_member_count}, expiring{' '}
          {inspection.expiring_member_count}
        </dd>
        <dt className="of-text-muted">Admins</dt>
        <dd>{adminsLabel}</dd>
        <dt className="of-text-muted">Parents</dt>
        <dd>
          {inspection.parents.length === 0
            ? '—'
            : inspection.parents.map((p) => p.name).join(', ')}
        </dd>
        <dt className="of-text-muted">Children</dt>
        <dd>
          {inspection.children.length === 0
            ? '—'
            : inspection.children.map((c) => c.name).join(', ')}
        </dd>
        <dt className="of-text-muted">Project access</dt>
        <dd>{inspection.project_access_hint}</dd>
      </dl>
      <AdminControls inspection={inspection} onError={onError} onRefresh={onRefresh} />
      <NestedControls inspection={inspection} onError={onError} onRefresh={onRefresh} />
      <MemberControls inspection={inspection} members={members} onError={onError} onRefresh={onRefresh} />
    </section>
  );
}

function AdminControls({
  inspection,
  onError,
  onRefresh,
}: {
  inspection: GroupInspection;
  onError: (msg: string) => void;
  onRefresh: () => Promise<void> | void;
}) {
  const [userID, setUserID] = useState('');
  const [scope, setScope] = useState<GroupAdminScope>('manage');

  async function add() {
    if (!userID.trim()) {
      return;
    }
    try {
      await addGroupAdmin(inspection.group.id, { user_id: userID.trim(), scope });
      setUserID('');
      await onRefresh();
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to add admin');
    }
  }

  async function remove(userId: string, s: GroupAdminScope) {
    try {
      await removeGroupAdmin(inspection.group.id, userId, s);
      await onRefresh();
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to remove admin');
    }
  }

  return (
    <section style={{ display: 'grid', gap: 8, padding: 12, borderRadius: 8, background: 'var(--bg-subtle)' }}>
      <h3 className="of-heading-lg" style={{ fontSize: 13 }}>Administrators</h3>
      <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'grid', gap: 4, fontSize: 12 }}>
        {inspection.admins.length === 0 && <li className="of-text-muted">No administrators yet.</li>}
        {inspection.admins.map((a) => (
          <li key={`${a.user_id}-${a.scope}`} style={{ display: 'flex', justifyContent: 'space-between', gap: 6 }}>
            <span><code>{a.user_id}</code> · {a.scope}</span>
            <button className="of-button of-button--ghost" onClick={() => void remove(a.user_id, a.scope)} style={{ fontSize: 11 }}>
              Remove
            </button>
          </li>
        ))}
      </ul>
      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap', alignItems: 'flex-end' }}>
        <label style={{ fontSize: 12 }}>
          User ID
          <input className="of-input" value={userID} onChange={(e) => setUserID(e.target.value)} style={{ marginTop: 4 }} />
        </label>
        <label style={{ fontSize: 12 }}>
          Scope
          <select
            className="of-input"
            value={scope}
            onChange={(e) => setScope(e.target.value as GroupAdminScope)}
            style={{ marginTop: 4 }}
          >
            <option value="manage">manage</option>
            <option value="manage_members">manage_members</option>
          </select>
        </label>
        <button className="of-button" onClick={() => void add()}>Add admin</button>
      </div>
    </section>
  );
}

function NestedControls({
  inspection,
  onError,
  onRefresh,
}: {
  inspection: GroupInspection;
  onError: (msg: string) => void;
  onRefresh: () => Promise<void> | void;
}) {
  const [memberID, setMemberID] = useState('');

  async function add() {
    if (!memberID.trim()) {
      return;
    }
    try {
      await addNestedGroup(inspection.group.id, memberID.trim());
      setMemberID('');
      await onRefresh();
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to nest group');
    }
  }

  async function remove(childID: string) {
    try {
      await removeNestedGroup(inspection.group.id, childID);
      await onRefresh();
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to unnest group');
    }
  }

  return (
    <section style={{ display: 'grid', gap: 8, padding: 12, borderRadius: 8, background: 'var(--bg-subtle)' }}>
      <h3 className="of-heading-lg" style={{ fontSize: 13 }}>Nested children</h3>
      <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'grid', gap: 4, fontSize: 12 }}>
        {inspection.children.length === 0 && <li className="of-text-muted">No nested children.</li>}
        {inspection.children.map((c) => (
          <li key={c.id} style={{ display: 'flex', justifyContent: 'space-between', gap: 6 }}>
            <span>{c.name} <code className="of-text-muted">{c.id}</code></span>
            <button className="of-button of-button--ghost" onClick={() => void remove(c.id)} style={{ fontSize: 11 }}>
              Unnest
            </button>
          </li>
        ))}
      </ul>
      <div style={{ display: 'flex', gap: 8, alignItems: 'flex-end' }}>
        <label style={{ fontSize: 12, flex: '1 1 200px' }}>
          Child group ID
          <input className="of-input" value={memberID} onChange={(e) => setMemberID(e.target.value)} style={{ marginTop: 4 }} />
        </label>
        <button className="of-button" onClick={() => void add()}>Add child</button>
      </div>
    </section>
  );
}

function MemberControls({
  inspection,
  members,
  onError,
  onRefresh,
}: {
  inspection: GroupInspection;
  members: GroupMember[];
  onError: (msg: string) => void;
  onRefresh: () => Promise<void> | void;
}) {
  const [userID, setUserID] = useState('');
  const [expiresAt, setExpiresAt] = useState('');

  async function add() {
    if (!userID.trim()) {
      return;
    }
    try {
      await addGroupMember(
        inspection.group.id,
        userID.trim(),
        expiresAt ? new Date(expiresAt).toISOString() : undefined,
      );
      setUserID('');
      setExpiresAt('');
      await onRefresh();
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to add member');
    }
  }

  async function remove(uid: string) {
    try {
      await removeGroupMember(inspection.group.id, uid);
      await onRefresh();
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to remove member');
    }
  }

  return (
    <section style={{ display: 'grid', gap: 8, padding: 12, borderRadius: 8, background: 'var(--bg-subtle)' }}>
      <h3 className="of-heading-lg" style={{ fontSize: 13 }}>Direct members</h3>
      <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'grid', gap: 4, fontSize: 12 }}>
        {members.length === 0 && <li className="of-text-muted">No direct members.</li>}
        {members.map((m) => (
          <li key={m.user_id} style={{ display: 'flex', justifyContent: 'space-between', gap: 6 }}>
            <span>
              <code>{m.user_id}</code>
              {m.expires_at && (
                <span className="of-text-muted">
                  {' '}
                  · expires {new Date(m.expires_at).toLocaleString()}
                </span>
              )}
            </span>
            <button className="of-button of-button--ghost" onClick={() => void remove(m.user_id)} style={{ fontSize: 11 }}>
              Remove
            </button>
          </li>
        ))}
      </ul>
      <div style={{ display: 'flex', gap: 8, alignItems: 'flex-end' }}>
        <label style={{ fontSize: 12, flex: '1 1 200px' }}>
          User ID
          <input className="of-input" value={userID} onChange={(e) => setUserID(e.target.value)} style={{ marginTop: 4 }} />
        </label>
        <label style={{ fontSize: 12 }}>
          Expires at (optional)
          <input
            className="of-input"
            type="datetime-local"
            value={expiresAt}
            onChange={(e) => setExpiresAt(e.target.value)}
            style={{ marginTop: 4 }}
          />
        </label>
        <button className="of-button" onClick={() => void add()}>Add member</button>
      </div>
    </section>
  );
}
