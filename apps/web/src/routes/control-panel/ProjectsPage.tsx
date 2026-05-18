// SG.6: project security boundary — Control Panel UI.
//
// Lets project owners and admins:
//   - inspect the project's default role, point-of-contact, and refs
//   - edit those fields (PATCH semantics)
//   - manage group-based project roles
//   - run the viewer/editor/owner group-setup shortcut
//   - review the access-request inbox (approve/deny pending requests)

import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';

import {
  bootstrapProjectAccessGroups,
  cancelProjectAccessRequest,
  checkEffectiveAccess,
  createProjectAccessRequest,
  createProjectResourceGrant,
  decideProjectAccessRequest,
  deleteProjectRequiredMarking,
  deleteProjectGroupMembership,
  deleteProjectResourceGrant,
  getProjectAccessRequestForm,
  listProjectAccessRequests,
  listProjectGroupMemberships,
  listProjectPropagationJobs,
  listProjectResourceGrants,
  listProjects,
  updateProject,
  upsertProjectAccessRequestGroupSetting,
  upsertProjectGroupMembership,
  upsertProjectRequiredMarking,
  type EffectiveAccessResponse,
  type GrantPrincipalKind,
  type GrantScopeKind,
  type OntologyProject,
  type ProjectAccessGroupKind,
  type ProjectAccessRequestForm,
  type ProjectAccessRequest,
  type ProjectAccessRequestStatus,
  type ProjectGroupMembership,
  type ProjectResourceGrant,
  type ProjectRole,
  type ViewRequirementPropagationJob,
} from '@/lib/api/tenancy';

const ROLE_OPTIONS: ProjectRole[] = ['discoverer', 'viewer', 'editor', 'owner'];
const STATUS_OPTIONS: ProjectAccessRequestStatus[] = [
  'pending',
  'approved',
  'denied',
  'cancelled',
  'changes_requested',
  'action_required',
  'completed',
];

export function ProjectsPage() {
  const [projects, setProjects] = useState<OntologyProject[]>([]);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const refreshProjects = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const items = await listProjects();
      setProjects(items);
      if (items.length > 0 && !selectedId) {
        setSelectedId(items[0].id);
      }
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to load projects');
    } finally {
      setLoading(false);
    }
  }, [selectedId]);

  useEffect(() => {
    void refreshProjects();
  }, [refreshProjects]);

  const selectedProject = useMemo(
    () => projects.find((p) => p.id === selectedId) ?? null,
    [projects, selectedId],
  );

  return (
    <section className="of-page" style={{ padding: 24, display: 'grid', gap: 16 }}>
      <Link to="/control-panel" style={{ color: 'var(--text-muted)', fontSize: 13 }}>
        ← Control panel
      </Link>

      <header>
        <h1 className="of-heading-xl">Projects (security boundary)</h1>
        <p className="of-text-muted" style={{ marginTop: 4 }}>
          Default role, group memberships, access-request inbox, and the
          viewer/editor/owner group setup shortcut. See{' '}
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

      <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
        <h2 className="of-heading-lg" style={{ fontSize: 14, textTransform: 'uppercase', letterSpacing: '0.12em' }}>
          Projects
        </h2>
        {loading ? (
          <p className="of-text-muted">Loading…</p>
        ) : projects.length === 0 ? (
          <p className="of-text-muted">No projects yet.</p>
        ) : (
          <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
            {projects.map((p) => (
              <button
                key={p.id}
                onClick={() => setSelectedId(p.id)}
                className={selectedId === p.id ? 'of-button of-button--primary' : 'of-button'}
                style={{ fontSize: 13 }}
              >
                {p.display_name} <span style={{ opacity: 0.6 }}>({p.slug})</span>
              </button>
            ))}
          </div>
        )}
      </section>

      {selectedProject && (
        <ProjectDetail
          project={selectedProject}
          onUpdated={(updated) =>
            setProjects((prev) => prev.map((p) => (p.id === updated.id ? updated : p)))
          }
          onError={setError}
        />
      )}
    </section>
  );
}

function ProjectDetail({
  project,
  onUpdated,
  onError,
}: {
  project: OntologyProject;
  onUpdated: (project: OntologyProject) => void;
  onError: (msg: string) => void;
}) {
  const [defaultRole, setDefaultRole] = useState<ProjectRole>(project.default_role);
  const [pocUserID, setPocUserID] = useState(project.point_of_contact_user_id ?? '');
  const [pocEmail, setPocEmail] = useState(project.point_of_contact_email ?? '');
  const [refsJson, setRefsJson] = useState(JSON.stringify(project.references ?? [], null, 2));
  const [propagateViewRequirements, setPropagateViewRequirements] = useState(
    Boolean(project.propagate_view_requirements_enabled),
  );
  const [propagationJobs, setPropagationJobs] = useState<ViewRequirementPropagationJob[]>([]);
  const [busy, setBusy] = useState(false);

  const refreshPropagationJobs = useCallback(async () => {
    try {
      setPropagationJobs(await listProjectPropagationJobs(project.id));
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to load propagation jobs');
    }
  }, [onError, project.id]);

  useEffect(() => {
    setDefaultRole(project.default_role);
    setPocUserID(project.point_of_contact_user_id ?? '');
    setPocEmail(project.point_of_contact_email ?? '');
    setRefsJson(JSON.stringify(project.references ?? [], null, 2));
    setPropagateViewRequirements(Boolean(project.propagate_view_requirements_enabled));
  }, [project]);

  useEffect(() => {
    void refreshPropagationJobs();
  }, [refreshPropagationJobs]);

  async function save() {
    setBusy(true);
    try {
      let parsedRefs;
      try {
        parsedRefs = JSON.parse(refsJson || '[]');
      } catch {
        onError('references must be valid JSON');
        setBusy(false);
        return;
      }
      const updated = await updateProject(project.id, {
        default_role: defaultRole,
        point_of_contact_user_id: pocUserID.trim() || null,
        point_of_contact_email: pocEmail.trim() || null,
        references: parsedRefs,
        propagate_view_requirements_enabled: propagateViewRequirements,
      });
      onUpdated(updated);
      await refreshPropagationJobs();
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to save');
    } finally {
      setBusy(false);
    }
  }

  return (
    <section style={{ display: 'grid', gap: 16 }}>
      <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
        <header>
          <h2 className="of-heading-lg" style={{ fontSize: 16 }}>{project.display_name}</h2>
          <p className="of-text-muted" style={{ fontSize: 12 }}>
            ID <code>{project.id}</code> · slug <code>{project.slug}</code> · owner{' '}
            <code>{project.owner_id}</code>
            {project.rid && <> · RID <code>{project.rid}</code></>}
          </p>
        </header>
        <div style={{ display: 'grid', gap: 12, gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))' }}>
          <label style={{ fontSize: 12 }}>
            Default role
            <select
              className="of-input"
              value={defaultRole}
              onChange={(e) => setDefaultRole(e.target.value as ProjectRole)}
              style={{ marginTop: 4 }}
            >
              {ROLE_OPTIONS.map((r) => (
                <option key={r} value={r}>{r}</option>
              ))}
            </select>
          </label>
          <label style={{ fontSize: 12 }}>
            Point of contact (user ID)
            <input
              className="of-input"
              value={pocUserID}
              onChange={(e) => setPocUserID(e.target.value)}
              style={{ marginTop: 4 }}
            />
          </label>
          <label style={{ fontSize: 12 }}>
            Point of contact (email)
            <input
              className="of-input"
              type="email"
              value={pocEmail}
              onChange={(e) => setPocEmail(e.target.value)}
              style={{ marginTop: 4 }}
            />
          </label>
        </div>
        <section
          style={{
            border: '1px solid var(--border)',
            borderRadius: 8,
            padding: 12,
            display: 'grid',
            gap: 8,
            background: 'var(--bg-subtle)',
          }}
        >
          <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 13 }}>
            <input
              type="checkbox"
              checked={propagateViewRequirements}
              disabled={
                Boolean(project.propagate_view_requirements_disabled_at) &&
                !project.propagate_view_requirements_enabled
              }
              onChange={(event) => setPropagateViewRequirements(event.target.checked)}
            />
            Propagate view requirements
          </label>
          <p className="of-text-muted" style={{ margin: 0, fontSize: 12, lineHeight: 1.45 }}>
            Legacy compatibility setting. New child folders and project resource
            bindings copy the project/folder view requirement markings at create
            time; existing descendants are left for the migration job. Migrate
            sensitive data to Markings before disabling this setting.
          </p>
          {project.propagate_view_requirements_disabled_at ? (
            <p className="of-text-muted" style={{ margin: 0, fontSize: 12 }}>
              Disabled at <code>{project.propagate_view_requirements_disabled_at}</code>.
              Once disabled, it cannot be re-enabled.
            </p>
          ) : (
            <p className="of-text-muted" style={{ margin: 0, fontSize: 12 }}>
              New projects should leave this off unless migrating an existing
              Foundry project that still depends on propagated view requirements.
            </p>
          )}
          <div style={{ display: 'grid', gap: 6 }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8, alignItems: 'center' }}>
              <strong style={{ fontSize: 12 }}>Propagation jobs</strong>
              <button className="of-button of-button--ghost" style={{ fontSize: 12 }} onClick={() => void refreshPropagationJobs()}>
                Refresh
              </button>
            </div>
            {propagationJobs.length === 0 ? (
              <p className="of-text-muted" style={{ margin: 0, fontSize: 12 }}>No propagation jobs yet.</p>
            ) : (
              <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'grid', gap: 4 }}>
                {propagationJobs.map((job) => {
                  const processed = job.processed_folders + job.processed_resources;
                  const total = job.total_folders + job.total_resources;
                  return (
                    <li key={job.id} style={{ fontSize: 12, display: 'grid', gap: 2 }}>
                      <span>
                        <code>{job.status}</code> · {processed}/{total} processed · changed{' '}
                        {job.changed_folders + job.changed_resources}
                      </span>
                      <span className="of-text-muted">
                        {job.parent_resource_kind} · <code>{job.parent_resource_rid}</code>
                      </span>
                      {job.error_message && <span className="of-status-danger">{job.error_message}</span>}
                    </li>
                  );
                })}
              </ul>
            )}
          </div>
        </section>
        <label style={{ fontSize: 12 }}>
          References (JSON array of {`{kind, id, label?}`})
          <textarea
            className="of-input"
            value={refsJson}
            onChange={(e) => setRefsJson(e.target.value)}
            style={{ marginTop: 4, minHeight: 80, fontFamily: 'var(--font-mono)', fontSize: 11 }}
          />
        </label>
        <div>
          <button className="of-button of-button--primary" disabled={busy} onClick={() => void save()}>
            {busy ? 'Saving…' : 'Save'}
          </button>
        </div>
      </section>

      <GroupMembershipsSection projectID={project.id} onError={onError} />
      <BootstrapSection projectID={project.id} onError={onError} />
      <ResourceGrantsSection projectID={project.id} onError={onError} />
      <EffectiveAccessSection projectID={project.id} onError={onError} />
      <AccessRequestFormSettingsSection projectID={project.id} onError={onError} />
      <AccessRequestsSection projectID={project.id} onError={onError} />
    </section>
  );
}

function ResourceGrantsSection({
  projectID,
  onError,
}: {
  projectID: string;
  onError: (msg: string) => void;
}) {
  const [items, setItems] = useState<ProjectResourceGrant[]>([]);
  const [scopeKind, setScopeKind] = useState<GrantScopeKind>('project');
  const [scopeID, setScopeID] = useState('');
  const [principalKind, setPrincipalKind] = useState<GrantPrincipalKind>('user');
  const [principalID, setPrincipalID] = useState('');
  const [role, setRole] = useState<ProjectRole>('viewer');
  const [busy, setBusy] = useState(false);

  const refresh = useCallback(async () => {
    try {
      setItems(await listProjectResourceGrants(projectID));
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to load grants');
    }
  }, [projectID, onError]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  async function create() {
    if (!principalID.trim()) {
      onError('principal_id is required');
      return;
    }
    if (scopeKind === 'folder' && !scopeID.trim()) {
      onError('scope_id is required when scope_kind = folder');
      return;
    }
    setBusy(true);
    try {
      await createProjectResourceGrant(projectID, {
        scope_kind: scopeKind,
        scope_id: scopeKind === 'folder' ? scopeID.trim() : undefined,
        principal_kind: principalKind,
        principal_id: principalID.trim(),
        role,
      });
      setPrincipalID('');
      setScopeID('');
      await refresh();
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to grant');
    } finally {
      setBusy(false);
    }
  }

  async function remove(grantID: string) {
    try {
      await deleteProjectResourceGrant(projectID, grantID);
      await refresh();
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to revoke');
    }
  }

  return (
    <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 8 }}>
      <h3 className="of-heading-lg" style={{ fontSize: 14 }}>Resource grants (SG.8)</h3>
      <p className="of-text-muted" style={{ fontSize: 12 }}>
        Direct project- and folder-scoped grants. Ontology resources inherit
        from their project, so direct grants below the folder level are
        intentionally disallowed.
      </p>
      <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'grid', gap: 4, fontSize: 13 }}>
        {items.length === 0 && <li className="of-text-muted">No direct grants.</li>}
        {items.map((g) => (
          <li key={g.id} style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
            <span>
              <code>{g.principal_kind}:{g.principal_id}</code> · {g.role} ·{' '}
              {g.scope_kind === 'project' ? 'project scope' : `folder ${g.scope_id}`}
            </span>
            <button className="of-button of-button--ghost" onClick={() => void remove(g.id)} style={{ fontSize: 12 }}>
              Revoke
            </button>
          </li>
        ))}
      </ul>
      <div style={{ display: 'grid', gap: 8, gridTemplateColumns: 'repeat(auto-fit, minmax(160px, 1fr))' }}>
        <label style={{ fontSize: 12 }}>
          Scope kind
          <select
            className="of-input"
            value={scopeKind}
            onChange={(e) => setScopeKind(e.target.value as GrantScopeKind)}
            style={{ marginTop: 4 }}
          >
            <option value="project">project</option>
            <option value="folder">folder</option>
          </select>
        </label>
        {scopeKind === 'folder' && (
          <label style={{ fontSize: 12 }}>
            Folder ID
            <input className="of-input" value={scopeID} onChange={(e) => setScopeID(e.target.value)} style={{ marginTop: 4 }} />
          </label>
        )}
        <label style={{ fontSize: 12 }}>
          Principal kind
          <select
            className="of-input"
            value={principalKind}
            onChange={(e) => setPrincipalKind(e.target.value as GrantPrincipalKind)}
            style={{ marginTop: 4 }}
          >
            <option value="user">user</option>
            <option value="group">group</option>
          </select>
        </label>
        <label style={{ fontSize: 12 }}>
          Principal ID
          <input className="of-input" value={principalID} onChange={(e) => setPrincipalID(e.target.value)} style={{ marginTop: 4 }} />
        </label>
        <label style={{ fontSize: 12 }}>
          Role
          <select
            className="of-input"
            value={role}
            onChange={(e) => setRole(e.target.value as ProjectRole)}
            style={{ marginTop: 4 }}
          >
            <option value="discoverer">discoverer</option>
            <option value="viewer">viewer</option>
            <option value="editor">editor</option>
            <option value="owner">owner</option>
          </select>
        </label>
      </div>
      <div>
        <button className="of-button" disabled={busy} onClick={() => void create()}>
          {busy ? 'Granting…' : 'Grant'}
        </button>
      </div>
    </section>
  );
}

function EffectiveAccessSection({
  projectID,
  onError,
}: {
  projectID: string;
  onError: (msg: string) => void;
}) {
  const [userID, setUserID] = useState('');
  const [groupIDs, setGroupIDs] = useState('');
  const [scopeKind, setScopeKind] = useState<GrantScopeKind>('project');
  const [scopeID, setScopeID] = useState('');
  const [result, setResult] = useState<EffectiveAccessResponse | null>(null);
  const [busy, setBusy] = useState(false);

  async function probe() {
    if (!userID.trim()) {
      onError('user_id is required');
      return;
    }
    if (scopeKind === 'folder' && !scopeID.trim()) {
      onError('scope_id is required for folder scope');
      return;
    }
    setBusy(true);
    try {
      const res = await checkEffectiveAccess(projectID, {
        user_id: userID.trim(),
        scope_kind: scopeKind,
        scope_id: scopeKind === 'folder' ? scopeID.trim() : undefined,
        group_ids: groupIDs
          .split(',')
          .map((g) => g.trim())
          .filter((g) => g.length > 0),
      });
      setResult(res);
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to resolve');
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 8 }}>
      <h3 className="of-heading-lg" style={{ fontSize: 14 }}>Effective access (SG.8)</h3>
      <p className="of-text-muted" style={{ fontSize: 12 }}>
        Resolves the highest role a user holds on a project or folder scope,
        with a structured breakdown of every contributing source. Visible to
        the inspected user, the project owner, and platform admins only.
      </p>
      <div style={{ display: 'grid', gap: 8, gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))' }}>
        <label style={{ fontSize: 12 }}>
          User ID
          <input className="of-input" value={userID} onChange={(e) => setUserID(e.target.value)} style={{ marginTop: 4 }} />
        </label>
        <label style={{ fontSize: 12 }}>
          Group IDs (comma-separated)
          <input
            className="of-input"
            value={groupIDs}
            onChange={(e) => setGroupIDs(e.target.value)}
            placeholder="optional — gateway forwards from JWT"
            style={{ marginTop: 4 }}
          />
        </label>
        <label style={{ fontSize: 12 }}>
          Scope kind
          <select
            className="of-input"
            value={scopeKind}
            onChange={(e) => setScopeKind(e.target.value as GrantScopeKind)}
            style={{ marginTop: 4 }}
          >
            <option value="project">project</option>
            <option value="folder">folder</option>
          </select>
        </label>
        {scopeKind === 'folder' && (
          <label style={{ fontSize: 12 }}>
            Folder ID
            <input className="of-input" value={scopeID} onChange={(e) => setScopeID(e.target.value)} style={{ marginTop: 4 }} />
          </label>
        )}
      </div>
      <div>
        <button className="of-button" disabled={busy} onClick={() => void probe()}>
          {busy ? 'Resolving…' : 'Resolve'}
        </button>
      </div>
      {result && (
        <>
          <p style={{ fontSize: 13 }}>
            Resolved role:{' '}
            <strong>{result.resolved_role ?? '(none)'}</strong> · {result.sources.length} source(s)
          </p>
          <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'grid', gap: 4, fontSize: 12 }}>
            {result.sources.map((s, i) => (
              <li key={`${s.kind}-${i}`} style={{ padding: 8, borderRadius: 8, background: 'var(--bg-subtle)' }}>
                <strong>{s.role}</strong> via <code>{s.kind}</code>
                {s.grant_id && <> · grant <code>{s.grant_id}</code></>}
                {s.group_id && <> · group <code>{s.group_id}</code></>}
                {s.folder_id && <> · folder <code>{s.folder_id}</code></>}
              </li>
            ))}
          </ul>
        </>
      )}
    </section>
  );
}

function GroupMembershipsSection({
  projectID,
  onError,
}: {
  projectID: string;
  onError: (msg: string) => void;
}) {
  const [items, setItems] = useState<ProjectGroupMembership[]>([]);
  const [groupID, setGroupID] = useState('');
  const [role, setRole] = useState<ProjectRole>('viewer');

  const refresh = useCallback(async () => {
    try {
      setItems(await listProjectGroupMemberships(projectID));
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to load group memberships');
    }
  }, [projectID, onError]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  async function add() {
    if (!groupID.trim()) {
      return;
    }
    try {
      await upsertProjectGroupMembership(projectID, groupID.trim(), role);
      setGroupID('');
      await refresh();
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to bind group');
    }
  }

  async function remove(gid: string) {
    try {
      await deleteProjectGroupMembership(projectID, gid);
      await refresh();
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to remove group');
    }
  }

  return (
    <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 8 }}>
      <h3 className="of-heading-lg" style={{ fontSize: 14 }}>Group memberships</h3>
      <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'grid', gap: 4, fontSize: 13 }}>
        {items.length === 0 && <li className="of-text-muted">No group memberships yet.</li>}
        {items.map((m) => (
          <li key={m.group_id} style={{ display: 'flex', justifyContent: 'space-between', gap: 8 }}>
            <span>
              <code>{m.group_id}</code> · {m.role}
            </span>
            <button className="of-button of-button--ghost" onClick={() => void remove(m.group_id)} style={{ fontSize: 12 }}>
              Remove
            </button>
          </li>
        ))}
      </ul>
      <div style={{ display: 'flex', gap: 8, alignItems: 'flex-end', flexWrap: 'wrap' }}>
        <label style={{ fontSize: 12, flex: '1 1 200px' }}>
          Group ID
          <input className="of-input" value={groupID} onChange={(e) => setGroupID(e.target.value)} style={{ marginTop: 4 }} />
        </label>
        <label style={{ fontSize: 12 }}>
          Role
          <select
            className="of-input"
            value={role}
            onChange={(e) => setRole(e.target.value as ProjectRole)}
            style={{ marginTop: 4 }}
          >
            {ROLE_OPTIONS.map((r) => (
              <option key={r} value={r}>{r}</option>
            ))}
          </select>
        </label>
        <button className="of-button" onClick={() => void add()}>Bind group</button>
      </div>
    </section>
  );
}

function BootstrapSection({
  projectID,
  onError,
}: {
  projectID: string;
  onError: (msg: string) => void;
}) {
  const [viewer, setViewer] = useState('');
  const [editor, setEditor] = useState('');
  const [owner, setOwner] = useState('');
  const [busy, setBusy] = useState(false);

  async function run() {
    if (!viewer.trim() && !editor.trim() && !owner.trim()) {
      onError('At least one of viewer/editor/owner group IDs is required.');
      return;
    }
    setBusy(true);
    try {
      await bootstrapProjectAccessGroups(projectID, {
        viewer_group_id: viewer.trim() || undefined,
        editor_group_id: editor.trim() || undefined,
        owner_group_id: owner.trim() || undefined,
      });
      setViewer('');
      setEditor('');
      setOwner('');
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to bootstrap groups');
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 8 }}>
      <h3 className="of-heading-lg" style={{ fontSize: 14 }}>Viewer / editor / owner group setup</h3>
      <p className="of-text-muted" style={{ fontSize: 12 }}>
        Bind three pre-existing groups (created in /control-panel/groups) to the
        viewer / editor / owner roles in one transaction. Group auto-creation
        lives in identity-federation-service.
      </p>
      <div style={{ display: 'grid', gap: 8, gridTemplateColumns: 'repeat(auto-fit, minmax(200px, 1fr))' }}>
        <label style={{ fontSize: 12 }}>
          Viewer group ID
          <input className="of-input" value={viewer} onChange={(e) => setViewer(e.target.value)} style={{ marginTop: 4 }} />
        </label>
        <label style={{ fontSize: 12 }}>
          Editor group ID
          <input className="of-input" value={editor} onChange={(e) => setEditor(e.target.value)} style={{ marginTop: 4 }} />
        </label>
        <label style={{ fontSize: 12 }}>
          Owner group ID
          <input className="of-input" value={owner} onChange={(e) => setOwner(e.target.value)} style={{ marginTop: 4 }} />
        </label>
      </div>
      <div>
        <button className="of-button of-button--primary" disabled={busy} onClick={() => void run()}>
          {busy ? 'Binding…' : 'Bind groups'}
        </button>
      </div>
    </section>
  );
}

function AccessRequestFormSettingsSection({
  projectID,
  onError,
}: {
  projectID: string;
  onError: (msg: string) => void;
}) {
  const [form, setForm] = useState<ProjectAccessRequestForm | null>(null);
  const [groupID, setGroupID] = useState('');
  const [groupKind, setGroupKind] = useState<ProjectAccessGroupKind>('internal');
  const [groupLabel, setGroupLabel] = useState('');
  const [groupReviewers, setGroupReviewers] = useState('');
  const [excluded, setExcluded] = useState(false);
  const [handoffMessage, setHandoffMessage] = useState('');
  const [handoffURL, setHandoffURL] = useState('');
  const [markingID, setMarkingID] = useState('');
  const [markingName, setMarkingName] = useState('');
  const [markingPrompt, setMarkingPrompt] = useState('');
  const [markingReviewers, setMarkingReviewers] = useState('');
  const [busy, setBusy] = useState(false);

  const refresh = useCallback(async () => {
    try {
      setForm(await getProjectAccessRequestForm(projectID));
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to load access request form');
    }
  }, [projectID, onError]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  async function saveGroupSetting() {
    if (!groupID.trim()) {
      onError('group_id is required');
      return;
    }
    setBusy(true);
    try {
      await upsertProjectAccessRequestGroupSetting(projectID, groupID.trim(), {
        group_display_name: groupLabel.trim() || undefined,
        group_kind: groupKind,
        reviewer_user_ids: splitIDs(groupReviewers),
        external_request_message: handoffMessage.trim() || undefined,
        external_request_url: handoffURL.trim() || undefined,
        excluded_from_request_forms: excluded,
      });
      setGroupID('');
      setGroupLabel('');
      setGroupReviewers('');
      setExcluded(false);
      setHandoffMessage('');
      setHandoffURL('');
      await refresh();
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to save group access setting');
    } finally {
      setBusy(false);
    }
  }

  async function saveMarking() {
    if (!markingID.trim() || !markingName.trim()) {
      onError('marking_id and marking_name are required');
      return;
    }
    setBusy(true);
    try {
      await upsertProjectRequiredMarking(projectID, markingID.trim(), {
        marking_name: markingName.trim(),
        reason_prompt: markingPrompt.trim() || undefined,
        reviewer_user_ids: splitIDs(markingReviewers),
      });
      setMarkingID('');
      setMarkingName('');
      setMarkingPrompt('');
      setMarkingReviewers('');
      await refresh();
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to save marking requirement');
    } finally {
      setBusy(false);
    }
  }

  async function removeMarking(id: string) {
    try {
      await deleteProjectRequiredMarking(projectID, id);
      await refresh();
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to remove marking requirement');
    }
  }

  return (
    <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 10 }}>
      <h3 className="of-heading-lg" style={{ fontSize: 14 }}>Access request form (SG.9)</h3>
      <p className="of-text-muted" style={{ fontSize: 12 }}>
        Configure requestable project groups, external handoff copy, hidden
        sensitive groups, and required marking reviewers.
      </p>

      <div style={{ display: 'grid', gap: 8, gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))' }}>
        <label style={{ fontSize: 12 }}>
          Group ID
          <input className="of-input" value={groupID} onChange={(e) => setGroupID(e.target.value)} style={{ marginTop: 4 }} />
        </label>
        <label style={{ fontSize: 12 }}>
          Kind
          <select className="of-input" value={groupKind} onChange={(e) => setGroupKind(e.target.value as ProjectAccessGroupKind)} style={{ marginTop: 4 }}>
            <option value="internal">internal</option>
            <option value="external">external</option>
            <option value="rule_based">rule_based</option>
          </select>
        </label>
        <label style={{ fontSize: 12 }}>
          Label
          <input className="of-input" value={groupLabel} onChange={(e) => setGroupLabel(e.target.value)} style={{ marginTop: 4 }} />
        </label>
        <label style={{ fontSize: 12 }}>
          Reviewer user IDs
          <input className="of-input" value={groupReviewers} onChange={(e) => setGroupReviewers(e.target.value)} style={{ marginTop: 4 }} />
        </label>
        <label style={{ fontSize: 12 }}>
          External message
          <input className="of-input" value={handoffMessage} onChange={(e) => setHandoffMessage(e.target.value)} style={{ marginTop: 4 }} />
        </label>
        <label style={{ fontSize: 12 }}>
          External URL
          <input className="of-input" value={handoffURL} onChange={(e) => setHandoffURL(e.target.value)} style={{ marginTop: 4 }} />
        </label>
        <label style={{ fontSize: 12, display: 'flex', alignItems: 'center', gap: 6, marginTop: 22 }}>
          <input type="checkbox" checked={excluded} onChange={(e) => setExcluded(e.target.checked)} />
          Hide from request form
        </label>
      </div>
      <div>
        <button className="of-button" disabled={busy} onClick={() => void saveGroupSetting()}>
          Save group setting
        </button>
      </div>

      <div style={{ display: 'grid', gap: 8, gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))' }}>
        <label style={{ fontSize: 12 }}>
          Marking ID
          <input className="of-input" value={markingID} onChange={(e) => setMarkingID(e.target.value)} style={{ marginTop: 4 }} />
        </label>
        <label style={{ fontSize: 12 }}>
          Marking name
          <input className="of-input" value={markingName} onChange={(e) => setMarkingName(e.target.value)} style={{ marginTop: 4 }} />
        </label>
        <label style={{ fontSize: 12 }}>
          Reason prompt
          <input className="of-input" value={markingPrompt} onChange={(e) => setMarkingPrompt(e.target.value)} style={{ marginTop: 4 }} />
        </label>
        <label style={{ fontSize: 12 }}>
          Reviewer user IDs
          <input className="of-input" value={markingReviewers} onChange={(e) => setMarkingReviewers(e.target.value)} style={{ marginTop: 4 }} />
        </label>
      </div>
      <div>
        <button className="of-button" disabled={busy} onClick={() => void saveMarking()}>
          Save required marking
        </button>
      </div>

      {form && (
        <div style={{ display: 'grid', gap: 6, fontSize: 12 }}>
          <strong>Visible request targets</strong>
          {form.groups.length === 0 && <span className="of-text-muted">No requestable groups.</span>}
          {form.groups.map((g) => (
            <div key={g.group_id} style={{ padding: 8, borderRadius: 8, background: 'var(--bg-subtle)' }}>
              <code>{g.group_id}</code> · {g.group_display_name || g.role} · {g.group_kind}
              {g.external_request_url && <> · <a href={g.external_request_url} target="_blank" rel="noreferrer">handoff</a></>}
            </div>
          ))}
          {form.required_markings.map((m) => (
            <div key={m.marking_id} style={{ padding: 8, borderRadius: 8, background: 'var(--bg-subtle)', display: 'flex', justifyContent: 'space-between', gap: 8 }}>
              <span><code>{m.marking_id}</code> · {m.marking_name}</span>
              <button className="of-button of-button--ghost" onClick={() => void removeMarking(m.marking_id)}>Remove</button>
            </div>
          ))}
        </div>
      )}
    </section>
  );
}

function AccessRequestsSection({
  projectID,
  onError,
}: {
  projectID: string;
  onError: (msg: string) => void;
}) {
  const [status, setStatus] = useState<ProjectAccessRequestStatus | ''>('pending');
  const [items, setItems] = useState<ProjectAccessRequest[]>([]);

  const refresh = useCallback(async () => {
    try {
      setItems(await listProjectAccessRequests(projectID, status || undefined));
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to load access requests');
    }
  }, [projectID, status, onError]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  async function decide(req: ProjectAccessRequest, decision: 'approved' | 'denied') {
    const reason = window.prompt(`Reason for ${decision}? (optional)`) ?? undefined;
    try {
      await decideProjectAccessRequest(projectID, req.id, decision, reason || undefined);
      await refresh();
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to decide');
    }
  }

  async function cancel(req: ProjectAccessRequest) {
    try {
      await cancelProjectAccessRequest(projectID, req.id);
      await refresh();
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to cancel');
    }
  }

  return (
    <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 8 }}>
      <h3 className="of-heading-lg" style={{ fontSize: 14 }}>Access requests</h3>
      <div style={{ display: 'flex', gap: 8, alignItems: 'flex-end' }}>
        <label style={{ fontSize: 12 }}>
          Status filter
          <select
            className="of-input"
            value={status}
            onChange={(e) => setStatus(e.target.value as ProjectAccessRequestStatus | '')}
            style={{ marginTop: 4 }}
          >
            <option value="">all</option>
            {STATUS_OPTIONS.map((s) => (
              <option key={s} value={s}>{s}</option>
            ))}
          </select>
        </label>
        <ManualRequestForm projectID={projectID} onCreated={() => void refresh()} onError={onError} />
      </div>
      <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'grid', gap: 6, fontSize: 12 }}>
        {items.length === 0 && <li className="of-text-muted">No access requests for this filter.</li>}
        {items.map((req) => (
          <li
            key={req.id}
            style={{ padding: 8, borderRadius: 8, background: 'var(--bg-subtle)', display: 'grid', gap: 4 }}
          >
            <div>
              <strong>{req.requested_role}</strong> requested by <code>{req.requested_by}</code> ·{' '}
              <span style={{ color: statusColor(req.status) }}>{req.status}</span>
            </div>
            {req.reason && <div>{req.reason}</div>}
            {req.scope_resource_kind && (
              <div className="of-text-muted">
                scope: {req.scope_resource_kind}/{req.scope_resource_id}
              </div>
            )}
            {req.tasks && req.tasks.length > 0 && (
              <div style={{ display: 'grid', gap: 4 }}>
                {req.tasks.map((task) => (
                  <div key={task.id} style={{ padding: 6, borderRadius: 6, background: '#fff' }}>
                    <code>{task.task_type}</code> · {task.status} · target <code>{task.target_user_id}</code>
                    {task.requested_role && <> · role {task.requested_role}</>}
                    {task.group_id && <> · group <code>{task.group_id}</code></>}
                    {task.marking_name && <> · marking {task.marking_name}</>}
                    {task.external_request_url && <> · <a href={task.external_request_url} target="_blank" rel="noreferrer">external handoff</a></>}
                  </div>
                ))}
              </div>
            )}
            <div style={{ display: 'flex', gap: 6 }}>
              {req.status === 'pending' && (
                <>
                  <button className="of-button of-button--ghost" onClick={() => void decide(req, 'approved')}>
                    Approve
                  </button>
                  <button className="of-button of-button--ghost" onClick={() => void decide(req, 'denied')}>
                    Deny
                  </button>
                  <button
                    className="of-button of-button--ghost"
                    style={{ color: '#92400e' }}
                    onClick={() => void cancel(req)}
                  >
                    Cancel
                  </button>
                </>
              )}
            </div>
          </li>
        ))}
      </ul>
    </section>
  );
}

function ManualRequestForm({
  projectID,
  onCreated,
  onError,
}: {
  projectID: string;
  onCreated: () => void;
  onError: (msg: string) => void;
}) {
  const [mode, setMode] = useState<'direct' | 'group' | 'marking'>('direct');
  const [role, setRole] = useState<ProjectRole>('viewer');
  const [reason, setReason] = useState('');
  const [targetUsers, setTargetUsers] = useState('');
  const [groupID, setGroupID] = useState('');
  const [markingID, setMarkingID] = useState('');
  const [markingName, setMarkingName] = useState('');
  const [markingReviewers, setMarkingReviewers] = useState('');
  const [busy, setBusy] = useState(false);

  async function submit() {
    if (!reason.trim()) {
      onError('reason is required');
      return;
    }
    if (mode === 'group' && !groupID.trim()) {
      onError('group_id is required');
      return;
    }
    if (mode === 'marking' && !markingID.trim()) {
      onError('marking_id is required');
      return;
    }
    setBusy(true);
    try {
      const requestedFor = splitIDs(targetUsers);
      await createProjectAccessRequest(projectID, {
        request_type: 'additional_project_access',
        requested_role: role,
        requested_for_user_ids: requestedFor.length > 0 ? requestedFor : undefined,
        reason: reason.trim(),
        project_role_requests: mode === 'direct' ? [{ role }] : undefined,
        group_membership_requests: mode === 'group' ? [{ group_id: groupID.trim(), role }] : undefined,
        marking_access_requests: mode === 'marking'
          ? [{
              marking_id: markingID.trim(),
              marking_name: markingName.trim() || undefined,
              reviewer_user_ids: splitIDs(markingReviewers),
            }]
          : undefined,
      });
      setReason('');
      setTargetUsers('');
      setGroupID('');
      setMarkingID('');
      setMarkingName('');
      setMarkingReviewers('');
      onCreated();
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to submit');
    } finally {
      setBusy(false);
    }
  }

  return (
    <div style={{ display: 'flex', gap: 6, alignItems: 'flex-end', marginLeft: 'auto', flexWrap: 'wrap' }}>
      <label style={{ fontSize: 12 }}>
        Type
        <select
          className="of-input"
          value={mode}
          onChange={(e) => setMode(e.target.value as 'direct' | 'group' | 'marking')}
          style={{ marginTop: 4 }}
        >
          <option value="direct">direct role</option>
          <option value="group">group membership</option>
          <option value="marking">marking access</option>
        </select>
      </label>
      <label style={{ fontSize: 12 }}>
        Role
        <select
          className="of-input"
          value={role}
          onChange={(e) => setRole(e.target.value as ProjectRole)}
          style={{ marginTop: 4 }}
        >
          {ROLE_OPTIONS.map((r) => (
            <option key={r} value={r}>{r}</option>
          ))}
        </select>
      </label>
      <label style={{ fontSize: 12, flex: '1 1 160px' }}>
        Target users
        <input
          className="of-input"
          value={targetUsers}
          onChange={(e) => setTargetUsers(e.target.value)}
          placeholder="optional, comma-separated"
          style={{ marginTop: 4 }}
        />
      </label>
      {mode === 'group' && (
        <label style={{ fontSize: 12, flex: '1 1 180px' }}>
          Group ID
          <input
            className="of-input"
            value={groupID}
            onChange={(e) => setGroupID(e.target.value)}
            style={{ marginTop: 4 }}
          />
        </label>
      )}
      {mode === 'marking' && (
        <>
          <label style={{ fontSize: 12, flex: '1 1 180px' }}>
            Marking ID
            <input
              className="of-input"
              value={markingID}
              onChange={(e) => setMarkingID(e.target.value)}
              style={{ marginTop: 4 }}
            />
          </label>
          <label style={{ fontSize: 12, flex: '1 1 140px' }}>
            Marking name
            <input
              className="of-input"
              value={markingName}
              onChange={(e) => setMarkingName(e.target.value)}
              style={{ marginTop: 4 }}
            />
          </label>
          <label style={{ fontSize: 12, flex: '1 1 180px' }}>
            Marking reviewers
            <input
              className="of-input"
              value={markingReviewers}
              onChange={(e) => setMarkingReviewers(e.target.value)}
              style={{ marginTop: 4 }}
            />
          </label>
        </>
      )}
      <label style={{ fontSize: 12, flex: '1 1 200px' }}>
        Reason
        <input
          className="of-input"
          value={reason}
          onChange={(e) => setReason(e.target.value)}
          style={{ marginTop: 4 }}
        />
      </label>
      <button className="of-button" disabled={busy} onClick={() => void submit()}>
        Request
      </button>
    </div>
  );
}

function statusColor(s: ProjectAccessRequestStatus): string {
  switch (s) {
    case 'pending':
      return '#92400e';
    case 'approved':
      return '#15803d';
    case 'denied':
      return '#b91c1c';
    case 'cancelled':
      return '#475569';
    case 'action_required':
      return '#1a4f9e';
    case 'completed':
      return '#15803d';
    case 'changes_requested':
      return '#92400e';
    default:
      return '#475569';
  }
}

function splitIDs(value: string): string[] {
  return value
    .split(',')
    .map((item) => item.trim())
    .filter((item) => item.length > 0);
}
