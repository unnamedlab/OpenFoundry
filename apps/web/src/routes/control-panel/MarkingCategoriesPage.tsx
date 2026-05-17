// SG.11: Marking category administration surface.

import { useCallback, useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';

import {
  applyResourceMarking,
  blockDeleteMarking,
  blockDeleteMarkingCategory,
  blockMoveMarkingCategory,
  checkResourceAccess,
  checkMarkingPermission,
  createMarking,
  createMarkingCategory,
  deleteResourceMarkingEdge,
  deleteMarkingPermission,
  deleteMarkingCategoryPermission,
  listEffectiveResourceMarkings,
  listMarkingAuditEvents,
  listMarkingCategories,
  listMarkingCategoryAuditEvents,
  listMarkingsForCategory,
  listMarkingBuildEvents,
  listResourceMarkingEdges,
  listResourceMarkings,
  publishMarkingBuild,
  removeResourceMarking,
  updateMarking,
  updateMarkingCategory,
  upsertResourceMarkingEdge,
  upsertMarkingPermission,
  upsertMarkingCategoryPermission,
  type EffectiveResourceMarking,
  type MarkingBuildEvent,
  type MarkingAuditEvent,
  type MarkingCategoryAuditEvent,
  type MarkingCategoryPermissionName,
  type MarkingCategoryPrincipal,
  type MarkingCategoryPrincipalKind,
  type MarkingCategoryResponse,
  type MarkingCategoryVisibility,
  type MarkingPermissionCheckResponse,
  type MarkingPermissionName,
  type MarkingResponse,
  type ResourceAccessCheckResponse,
  type PublishMarkingBuildResponse,
  type ResourceMarkingEdge,
  type ResourceMarkingRelationKind,
  type ResourceMarking,
} from '@/lib/api/marking-categories';

const PRINCIPAL_KINDS: MarkingCategoryPrincipalKind[] = ['user', 'group'];
const PERMISSIONS: MarkingCategoryPermissionName[] = ['administrator', 'viewer'];
const MARKING_PERMISSIONS: MarkingPermissionName[] = ['administrator', 'remover', 'applier', 'member'];
const VISIBILITIES: MarkingCategoryVisibility[] = ['visible', 'hidden'];
const RELATION_KINDS: ResourceMarkingRelationKind[] = ['hierarchy', 'lineage'];

export function MarkingCategoriesPage() {
  const [items, setItems] = useState<MarkingCategoryResponse[]>([]);
  const [includeHidden, setIncludeHidden] = useState(true);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  const refresh = useCallback(async () => {
    setLoading(true);
    setError('');
    try {
      const resp = await listMarkingCategories(includeHidden);
      setItems(resp.items);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to load marking categories');
    } finally {
      setLoading(false);
    }
  }, [includeHidden]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  const hiddenCount = useMemo(
    () => items.filter((item) => item.visibility === 'hidden').length,
    [items],
  );

  return (
    <section className="of-page" style={{ padding: 24, display: 'grid', gap: 16 }}>
      <Link to="/control-panel" style={{ color: 'var(--text-muted)', fontSize: 13 }}>
        ← Control panel
      </Link>

      <header>
        <h1 className="of-heading-xl">Marking categories</h1>
        <p className="of-text-muted" style={{ marginTop: 4 }}>
          Category metadata, visibility, administrator/viewer grants, and immutable deletion checks.
          See{' '}
          <a href="/docs/security-governance/security-overview" target="_blank" rel="noreferrer">
            security governance
          </a>{' '}
          for how markings compose with roles and project access.
        </p>
      </header>

      {error && (
        <div className="of-status-danger" style={{ padding: '10px 14px', borderRadius: 'var(--radius-md)', fontSize: 13 }}>
          {error}
        </div>
      )}

      <section className="of-panel" style={{ padding: 16, display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 12, flexWrap: 'wrap' }}>
        <label style={{ fontSize: 12, display: 'inline-flex', alignItems: 'center', gap: 8 }}>
          <input
            type="checkbox"
            checked={includeHidden}
            onChange={(event) => setIncludeHidden(event.target.checked)}
          />
          Include hidden categories
        </label>
        <span className="of-text-muted" style={{ fontSize: 12 }}>
          {items.length} categories · {hiddenCount} hidden
        </span>
      </section>

      <CreateCategoryForm onCreated={() => void refresh()} onError={setError} />
      <BuildOutputWorkbench onError={setError} />

      <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
        <h2 className="of-heading-lg" style={{ fontSize: 14, textTransform: 'uppercase', letterSpacing: '0.12em' }}>
          Categories
        </h2>
        {loading ? (
          <p className="of-text-muted">Loading…</p>
        ) : items.length === 0 ? (
          <p className="of-text-muted">No marking categories match this filter.</p>
        ) : (
          items.map((item) => (
            <CategoryCard
              key={item.id}
              category={item}
              onChange={() => void refresh()}
              onError={setError}
            />
          ))
        )}
      </section>
    </section>
  );
}

function BuildOutputWorkbench({ onError }: { onError: (msg: string) => void }) {
  const [buildID, setBuildID] = useState('');
  const [transactionID, setTransactionID] = useState('');
  const [inputResources, setInputResources] = useState('dataset:ri.input');
  const [outputResources, setOutputResources] = useState('dataset:ri.output');
  const [groupIDs, setGroupIDs] = useState('');
  const [replaceLineage, setReplaceLineage] = useState(true);
  const [dryRun, setDryRun] = useState(true);
  const [resourceUpdateAllowed, setResourceUpdateAllowed] = useState(false);
  const [expandAccessAllowed, setExpandAccessAllowed] = useState(false);
  const [result, setResult] = useState<PublishMarkingBuildResponse | null>(null);
  const [events, setEvents] = useState<MarkingBuildEvent[] | null>(null);

  async function publish() {
    let inputs;
    let outputs;
    try {
      inputs = parseResourceRefs(inputResources);
      outputs = parseResourceRefs(outputResources);
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'resource refs must use kind:id lines');
      return;
    }
    try {
      const resp = await publishMarkingBuild({
        build_id: buildID.trim() || undefined,
        transaction_id: transactionID.trim() || undefined,
        input_resources: inputs,
        output_resources: outputs,
        replace_existing_lineage_to_output: replaceLineage,
        dry_run: dryRun,
        group_ids: idList(groupIDs),
        resource_update_markings_allowed: resourceUpdateAllowed,
        expand_access_allowed: expandAccessAllowed,
        reason: 'control-panel-build-output',
      });
      setResult(resp);
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to publish marking-aware build output');
    }
  }

  async function loadEvents() {
    try {
      const refs = parseResourceRefs(outputResources);
      const first = refs[0];
      const resp = await listMarkingBuildEvents({
        build_id: buildID.trim() || undefined,
        transaction_id: transactionID.trim() || undefined,
        resource_kind: first?.resource_kind,
        resource_id: first?.resource_id,
      });
      setEvents(resp.items);
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to load build marking events');
    }
  }

  return (
    <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 10 }}>
      <h2 className="of-heading-lg" style={{ fontSize: 14, textTransform: 'uppercase', letterSpacing: '0.12em' }}>
        Build output markings
      </h2>
      <div style={{ display: 'grid', gap: 8, gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))' }}>
        <label style={{ fontSize: 11 }}>
          Build ID
          <input className="of-input" value={buildID} onChange={(event) => setBuildID(event.target.value)} style={{ marginTop: 4 }} />
        </label>
        <label style={{ fontSize: 11 }}>
          Transaction ID
          <input className="of-input" value={transactionID} onChange={(event) => setTransactionID(event.target.value)} style={{ marginTop: 4 }} />
        </label>
        <label style={{ fontSize: 11 }}>
          Group IDs
          <input className="of-input" value={groupIDs} onChange={(event) => setGroupIDs(event.target.value)} style={{ marginTop: 4 }} />
        </label>
      </div>
      <div style={{ display: 'grid', gap: 8, gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))' }}>
        <label style={{ fontSize: 11 }}>
          Input resources
          <textarea className="of-input" value={inputResources} onChange={(event) => setInputResources(event.target.value)} style={{ marginTop: 4, minHeight: 64, fontFamily: 'var(--font-mono)', fontSize: 12 }} />
        </label>
        <label style={{ fontSize: 11 }}>
          Output resources
          <textarea className="of-input" value={outputResources} onChange={(event) => setOutputResources(event.target.value)} style={{ marginTop: 4, minHeight: 64, fontFamily: 'var(--font-mono)', fontSize: 12 }} />
        </label>
      </div>
      <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', fontSize: 11 }}>
        <label style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
          <input type="checkbox" checked={replaceLineage} onChange={(event) => setReplaceLineage(event.target.checked)} />
          Replace output lineage
        </label>
        <label style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
          <input type="checkbox" checked={dryRun} onChange={(event) => setDryRun(event.target.checked)} />
          Dry run
        </label>
        <label style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
          <input type="checkbox" checked={resourceUpdateAllowed} onChange={(event) => setResourceUpdateAllowed(event.target.checked)} />
          Resource update-markings role
        </label>
        <label style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
          <input type="checkbox" checked={expandAccessAllowed} onChange={(event) => setExpandAccessAllowed(event.target.checked)} />
          Expand-access equivalent
        </label>
      </div>
      <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
        <button className="of-button" onClick={() => void publish()}>
          Publish/plan markings
        </button>
        <button className="of-button of-button--ghost" onClick={() => void loadEvents()}>
          Load build diffs
        </button>
      </div>
      {result && (
        <div style={{ fontSize: 11, display: 'grid', gap: 4 }}>
          <div>
            allowed {String(result.allowed)} · applied {String(result.applied)} · dry run {String(result.dry_run)}
          </div>
          {result.output_diffs.map((diff) => (
            <div key={`${diff.output_resource.resource_kind}:${diff.output_resource.resource_id}`}>
              {diff.output_resource.resource_kind} <code>{diff.output_resource.resource_id}</code> · +{diff.added.length} -{diff.removed.length} ={diff.unchanged.length}
            </div>
          ))}
          {(result.blocked_removals ?? []).map((item) => (
            <div key={`${item.output_resource.resource_id}:${item.marking_id}`} className="of-status-danger" style={{ padding: 8, borderRadius: 8 }}>
              blocked removal {item.marking_name || item.marking_id}: {item.permission.reasons.join(', ')}
            </div>
          ))}
        </div>
      )}
      {events && (
        <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'grid', gap: 4, fontSize: 11 }}>
          {events.length === 0 && <li className="of-text-muted">No build marking diffs match this filter.</li>}
          {events.slice(0, 8).map((event) => (
            <li key={event.id}>
              {event.status} · {event.output_resource_kind} <code>{event.output_resource_id}</code> · {new Date(event.created_at).toLocaleString()}
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function CreateCategoryForm({
  onCreated,
  onError,
}: {
  onCreated: () => void;
  onError: (msg: string) => void;
}) {
  const [slug, setSlug] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [description, setDescription] = useState('');
  const [visibility, setVisibility] = useState<MarkingCategoryVisibility>('visible');
  const [organizationID, setOrganizationID] = useState('');
  const [metadata, setMetadata] = useState('{\n  "steward": "security"\n}');
  const [administratorIDs, setAdministratorIDs] = useState('');
  const [viewerIDs, setViewerIDs] = useState('');
  const [busy, setBusy] = useState(false);

  async function create() {
    if (!slug.trim() || !displayName.trim()) {
      onError('slug and display name are required');
      return;
    }
    let parsedMetadata: Record<string, unknown>;
    try {
      parsedMetadata = parseMetadata(metadata);
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'metadata must be a JSON object');
      return;
    }
    setBusy(true);
    try {
      await createMarkingCategory({
        slug: slug.trim(),
        display_name: displayName.trim(),
        description: description.trim() || undefined,
        visibility,
        organization_id: organizationID.trim() || undefined,
        metadata: parsedMetadata,
        administrators: principalList(administratorIDs, 'user'),
        viewers: principalList(viewerIDs, 'user'),
      });
      setSlug('');
      setDisplayName('');
      setDescription('');
      setOrganizationID('');
      setAdministratorIDs('');
      setViewerIDs('');
      setMetadata('{\n  "steward": "security"\n}');
      onCreated();
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to create marking category');
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 10 }}>
      <h2 className="of-heading-lg" style={{ fontSize: 14, textTransform: 'uppercase', letterSpacing: '0.12em' }}>
        Create category
      </h2>
      <div style={{ display: 'grid', gap: 8, gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))' }}>
        <label style={{ fontSize: 12 }}>
          Slug
          <input className="of-input" value={slug} onChange={(event) => setSlug(event.target.value)} style={{ marginTop: 4 }} />
        </label>
        <label style={{ fontSize: 12 }}>
          Display name
          <input className="of-input" value={displayName} onChange={(event) => setDisplayName(event.target.value)} style={{ marginTop: 4 }} />
        </label>
        <label style={{ fontSize: 12 }}>
          Visibility
          <select
            className="of-input"
            value={visibility}
            onChange={(event) => setVisibility(event.target.value as MarkingCategoryVisibility)}
            style={{ marginTop: 4 }}
          >
            {VISIBILITIES.map((item) => (
              <option key={item} value={item}>{item}</option>
            ))}
          </select>
        </label>
        <label style={{ fontSize: 12 }}>
          Organization ID
          <input className="of-input" value={organizationID} onChange={(event) => setOrganizationID(event.target.value)} style={{ marginTop: 4 }} />
        </label>
      </div>
      <label style={{ fontSize: 12 }}>
        Description
        <input className="of-input" value={description} onChange={(event) => setDescription(event.target.value)} style={{ marginTop: 4 }} />
      </label>
      <div style={{ display: 'grid', gap: 8, gridTemplateColumns: 'repeat(auto-fit, minmax(240px, 1fr))' }}>
        <label style={{ fontSize: 12 }}>
          Administrator user IDs
          <input className="of-input" value={administratorIDs} onChange={(event) => setAdministratorIDs(event.target.value)} style={{ marginTop: 4 }} />
        </label>
        <label style={{ fontSize: 12 }}>
          Viewer user IDs
          <input className="of-input" value={viewerIDs} onChange={(event) => setViewerIDs(event.target.value)} style={{ marginTop: 4 }} />
        </label>
      </div>
      <label style={{ fontSize: 12 }}>
        Metadata JSON
        <textarea
          className="of-input"
          value={metadata}
          onChange={(event) => setMetadata(event.target.value)}
          style={{ marginTop: 4, minHeight: 90, fontFamily: 'var(--font-mono)', fontSize: 12 }}
        />
      </label>
      <div>
        <button className="of-button of-button--primary" disabled={busy} onClick={() => void create()}>
          {busy ? 'Creating…' : 'Create category'}
        </button>
      </div>
    </section>
  );
}

function CategoryCard({
  category,
  onChange,
  onError,
}: {
  category: MarkingCategoryResponse;
  onChange: () => void;
  onError: (msg: string) => void;
}) {
  const [displayName, setDisplayName] = useState(category.display_name);
  const [description, setDescription] = useState(category.description);
  const [visibility, setVisibility] = useState<MarkingCategoryVisibility>(category.visibility);
  const [metadata, setMetadata] = useState(JSON.stringify(category.metadata ?? {}, null, 2));
  const [auditEvents, setAuditEvents] = useState<MarkingCategoryAuditEvent[] | null>(null);
  const [permissionPrincipalKind, setPermissionPrincipalKind] = useState<MarkingCategoryPrincipalKind>('user');
  const [permissionPrincipalID, setPermissionPrincipalID] = useState('');
  const [permission, setPermission] = useState<MarkingCategoryPermissionName>('viewer');

  async function save() {
    let parsedMetadata: Record<string, unknown>;
    try {
      parsedMetadata = parseMetadata(metadata);
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'metadata must be a JSON object');
      return;
    }
    try {
      await updateMarkingCategory(category.id, {
        display_name: displayName.trim(),
        description,
        visibility,
        metadata: parsedMetadata,
      });
      onChange();
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to update marking category');
    }
  }

  async function hide() {
    try {
      await updateMarkingCategory(category.id, { visibility: 'hidden' });
      onChange();
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to hide marking category');
    }
  }

  async function deleteBlocked() {
    try {
      await blockDeleteMarkingCategory(category.id);
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Deletion is unsupported; hide the category instead');
      return;
    }
    onError('Deletion is unsupported; hide the category instead');
  }

  async function grantPermission() {
    if (!permissionPrincipalID.trim()) {
      onError('principal_id is required');
      return;
    }
    try {
      await upsertMarkingCategoryPermission(category.id, {
        principal_kind: permissionPrincipalKind,
        principal_id: permissionPrincipalID.trim(),
        permission,
      });
      setPermissionPrincipalID('');
      onChange();
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to grant category permission');
    }
  }

  async function revokePermission(
    principalKind: MarkingCategoryPrincipalKind,
    principalID: string,
    permissionName: MarkingCategoryPermissionName,
  ) {
    try {
      await deleteMarkingCategoryPermission(category.id, principalKind, principalID, permissionName);
      onChange();
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to revoke category permission');
    }
  }

  async function loadAuditEvents() {
    try {
      const resp = await listMarkingCategoryAuditEvents(category.id);
      setAuditEvents(resp.items);
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to load marking category audit events');
    }
  }

  return (
    <article style={{ padding: 12, borderRadius: 8, background: 'var(--bg-subtle)', display: 'grid', gap: 10 }}>
      <header style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 8, flexWrap: 'wrap' }}>
        <div>
          <strong>{category.display_name}</strong>
          <div className="of-text-muted" style={{ fontSize: 11 }}>
            <code>{category.slug}</code> · {category.visibility} · ID <code>{category.id}</code>
            {category.organization_id ? <> · organization <code>{category.organization_id}</code></> : null}
          </div>
          {category.description && (
            <p style={{ fontSize: 12, margin: '4px 0 0' }}>{category.description}</p>
          )}
        </div>
        <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
          <button className="of-button of-button--ghost" onClick={() => void hide()}>
            Hide
          </button>
          <button className="of-button of-button--ghost" style={{ color: '#b91c1c' }} onClick={() => void deleteBlocked()}>
            Test delete block
          </button>
        </div>
      </header>

      <div style={{ display: 'grid', gap: 8, gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))' }}>
        <label style={{ fontSize: 12 }}>
          Display name
          <input className="of-input" value={displayName} onChange={(event) => setDisplayName(event.target.value)} style={{ marginTop: 4 }} />
        </label>
        <label style={{ fontSize: 12 }}>
          Visibility
          <select
            className="of-input"
            value={visibility}
            onChange={(event) => setVisibility(event.target.value as MarkingCategoryVisibility)}
            style={{ marginTop: 4 }}
          >
            {VISIBILITIES.map((item) => (
              <option key={item} value={item}>{item}</option>
            ))}
          </select>
        </label>
        <label style={{ fontSize: 12 }}>
          Description
          <input className="of-input" value={description} onChange={(event) => setDescription(event.target.value)} style={{ marginTop: 4 }} />
        </label>
      </div>
      <label style={{ fontSize: 12 }}>
        Metadata JSON
        <textarea
          className="of-input"
          value={metadata}
          onChange={(event) => setMetadata(event.target.value)}
          style={{ marginTop: 4, minHeight: 82, fontFamily: 'var(--font-mono)', fontSize: 12 }}
        />
      </label>
      <div>
        <button className="of-button" onClick={() => void save()}>
          Save metadata
        </button>
      </div>

      <MarkingsSection categoryID={category.id} onError={onError} />

      <section style={{ display: 'grid', gap: 6 }}>
        <h3 style={{ fontSize: 12, margin: 0, textTransform: 'uppercase', letterSpacing: '0.08em' }}>
          Category permissions
        </h3>
        <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'grid', gap: 4, fontSize: 12 }}>
          {category.permissions.length === 0 && <li className="of-text-muted">No explicit permissions.</li>}
          {category.permissions.map((entry) => (
            <li
              key={`${entry.principal_kind}:${entry.principal_id}:${entry.permission}`}
              style={{ display: 'flex', justifyContent: 'space-between', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}
            >
              <span>
                <strong>{entry.permission}</strong> · {entry.principal_kind} <code>{entry.principal_id}</code>
              </span>
              <button
                className="of-button of-button--ghost"
                style={{ fontSize: 11 }}
                onClick={() => void revokePermission(entry.principal_kind, entry.principal_id, entry.permission)}
              >
                Revoke
              </button>
            </li>
          ))}
        </ul>
        <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', alignItems: 'flex-end' }}>
          <label style={{ fontSize: 11 }}>
            Principal
            <select
              className="of-input"
              value={permissionPrincipalKind}
              onChange={(event) => setPermissionPrincipalKind(event.target.value as MarkingCategoryPrincipalKind)}
              style={{ marginTop: 4 }}
            >
              {PRINCIPAL_KINDS.map((item) => (
                <option key={item} value={item}>{item}</option>
              ))}
            </select>
          </label>
          <label style={{ fontSize: 11, flex: '1 1 240px' }}>
            Principal ID
            <input className="of-input" value={permissionPrincipalID} onChange={(event) => setPermissionPrincipalID(event.target.value)} style={{ marginTop: 4 }} />
          </label>
          <label style={{ fontSize: 11 }}>
            Permission
            <select
              className="of-input"
              value={permission}
              onChange={(event) => setPermission(event.target.value as MarkingCategoryPermissionName)}
              style={{ marginTop: 4 }}
            >
              {PERMISSIONS.map((item) => (
                <option key={item} value={item}>{item}</option>
              ))}
            </select>
          </label>
          <button className="of-button" onClick={() => void grantPermission()}>
            Grant
          </button>
        </div>
      </section>

      <section style={{ display: 'grid', gap: 6 }}>
        <div>
          <button className="of-button of-button--ghost" onClick={() => void loadAuditEvents()}>
            Load audit events
          </button>
        </div>
        {auditEvents && (
          <AuditEventList events={auditEvents} />
        )}
      </section>
    </article>
  );
}

function MarkingsSection({
  categoryID,
  onError,
}: {
  categoryID: string;
  onError: (msg: string) => void;
}) {
  const [items, setItems] = useState<MarkingResponse[]>([]);
  const [loading, setLoading] = useState(false);
  const [slug, setSlug] = useState('');
  const [stableID, setStableID] = useState('');
  const [displayName, setDisplayName] = useState('');
  const [description, setDescription] = useState('');
  const [metadata, setMetadata] = useState('{\n  "criterion": "training-complete"\n}');
  const [memberIDs, setMemberIDs] = useState('');

  const refresh = useCallback(async () => {
    setLoading(true);
    try {
      const resp = await listMarkingsForCategory(categoryID, true);
      setItems(resp.items);
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to load markings');
    } finally {
      setLoading(false);
    }
  }, [categoryID, onError]);

  useEffect(() => {
    void refresh();
  }, [refresh]);

  async function create() {
    if (!slug.trim() || !displayName.trim()) {
      onError('marking slug and display name are required');
      return;
    }
    let parsedMetadata: Record<string, unknown>;
    try {
      parsedMetadata = parseMetadata(metadata);
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'metadata must be a JSON object');
      return;
    }
    try {
      await createMarking(categoryID, {
        id: stableID.trim() || undefined,
        slug: slug.trim(),
        display_name: displayName.trim(),
        description: description.trim() || undefined,
        metadata: parsedMetadata,
        members: principalList(memberIDs, 'user'),
      });
      setSlug('');
      setStableID('');
      setDisplayName('');
      setDescription('');
      setMemberIDs('');
      setMetadata('{\n  "criterion": "training-complete"\n}');
      await refresh();
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to create marking');
    }
  }

  return (
    <section style={{ display: 'grid', gap: 8 }}>
      <h3 style={{ fontSize: 12, margin: 0, textTransform: 'uppercase', letterSpacing: '0.08em' }}>
        Markings
      </h3>
      <div style={{ display: 'grid', gap: 8, gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))' }}>
        <label style={{ fontSize: 11 }}>
          Stable ID
          <input className="of-input" value={stableID} onChange={(event) => setStableID(event.target.value)} style={{ marginTop: 4 }} />
        </label>
        <label style={{ fontSize: 11 }}>
          Slug
          <input className="of-input" value={slug} onChange={(event) => setSlug(event.target.value)} style={{ marginTop: 4 }} />
        </label>
        <label style={{ fontSize: 11 }}>
          Display name
          <input className="of-input" value={displayName} onChange={(event) => setDisplayName(event.target.value)} style={{ marginTop: 4 }} />
        </label>
        <label style={{ fontSize: 11 }}>
          Member user IDs
          <input className="of-input" value={memberIDs} onChange={(event) => setMemberIDs(event.target.value)} style={{ marginTop: 4 }} />
        </label>
      </div>
      <label style={{ fontSize: 11 }}>
        Marking description
        <input className="of-input" value={description} onChange={(event) => setDescription(event.target.value)} style={{ marginTop: 4 }} />
      </label>
      <label style={{ fontSize: 11 }}>
        Marking metadata JSON
        <textarea
          className="of-input"
          value={metadata}
          onChange={(event) => setMetadata(event.target.value)}
          style={{ marginTop: 4, minHeight: 70, fontFamily: 'var(--font-mono)', fontSize: 12 }}
        />
      </label>
      <div>
        <button className="of-button" onClick={() => void create()}>
          Create marking
        </button>
      </div>
      {loading ? (
        <p className="of-text-muted" style={{ fontSize: 12 }}>Loading markings…</p>
      ) : items.length === 0 ? (
        <p className="of-text-muted" style={{ fontSize: 12 }}>No markings in this category.</p>
      ) : (
        <div style={{ display: 'grid', gap: 8 }}>
          {items.map((item) => (
            <MarkingCard
              key={item.id}
              marking={item}
              categoryID={categoryID}
              onChange={() => void refresh()}
              onError={onError}
            />
          ))}
        </div>
      )}
    </section>
  );
}

function MarkingCard({
  marking,
  categoryID,
  onChange,
  onError,
}: {
  marking: MarkingResponse;
  categoryID: string;
  onChange: () => void;
  onError: (msg: string) => void;
}) {
  const [displayName, setDisplayName] = useState(marking.display_name);
  const [description, setDescription] = useState(marking.description);
  const [metadata, setMetadata] = useState(JSON.stringify(marking.metadata ?? {}, null, 2));
  const [principalKind, setPrincipalKind] = useState<MarkingCategoryPrincipalKind>('user');
  const [principalID, setPrincipalID] = useState('');
  const [permission, setPermission] = useState<MarkingPermissionName>('member');
  const [moveTargetID, setMoveTargetID] = useState(categoryID);
  const [auditEvents, setAuditEvents] = useState<MarkingAuditEvent[] | null>(null);
  const [checkPrincipalID, setCheckPrincipalID] = useState('');
  const [checkGroupIDs, setCheckGroupIDs] = useState('');
  const [resourceKind, setResourceKind] = useState('dataset');
  const [resourceID, setResourceID] = useState('');
  const [resourceUpdateAllowed, setResourceUpdateAllowed] = useState(false);
  const [expandAccessAllowed, setExpandAccessAllowed] = useState(false);
  const [permissionCheck, setPermissionCheck] = useState<MarkingPermissionCheckResponse | null>(null);
  const [resourceMarkings, setResourceMarkings] = useState<ResourceMarking[] | null>(null);
  const [edgeSourceKind, setEdgeSourceKind] = useState('dataset');
  const [edgeSourceID, setEdgeSourceID] = useState('');
  const [edgeRelationKind, setEdgeRelationKind] = useState<ResourceMarkingRelationKind>('lineage');
  const [resourceEdges, setResourceEdges] = useState<ResourceMarkingEdge[] | null>(null);
  const [effectiveMarkings, setEffectiveMarkings] = useState<EffectiveResourceMarking[] | null>(null);
  const [requiredOrgID, setRequiredOrgID] = useState('');
  const [userOrgIDs, setUserOrgIDs] = useState('');
  const [roleSatisfied, setRoleSatisfied] = useState(true);
  const [accessCheck, setAccessCheck] = useState<ResourceAccessCheckResponse | null>(null);

  async function save() {
    let parsedMetadata: Record<string, unknown>;
    try {
      parsedMetadata = parseMetadata(metadata);
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'metadata must be a JSON object');
      return;
    }
    try {
      await updateMarking(marking.id, {
        display_name: displayName.trim(),
        description,
        metadata: parsedMetadata,
      });
      onChange();
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to update marking');
    }
  }

  async function grant() {
    if (!principalID.trim()) {
      onError('principal_id is required');
      return;
    }
    try {
      await upsertMarkingPermission(marking.id, {
        principal_kind: principalKind,
        principal_id: principalID.trim(),
        permission,
      });
      setPrincipalID('');
      onChange();
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to grant marking permission');
    }
  }

  async function revoke(
    principalKindValue: MarkingCategoryPrincipalKind,
    principalIDValue: string,
    permissionName: MarkingPermissionName,
  ) {
    try {
      await deleteMarkingPermission(marking.id, principalKindValue, principalIDValue, permissionName);
      onChange();
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to revoke marking permission');
    }
  }

  async function deleteBlocked() {
    try {
      await blockDeleteMarking(marking.id);
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Marking deletion is unsupported');
      return;
    }
    onError('Marking deletion is unsupported');
  }

  async function moveBlocked() {
    if (!moveTargetID.trim()) {
      onError('target category id is required');
      return;
    }
    try {
      await blockMoveMarkingCategory(marking.id, moveTargetID.trim());
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Markings cannot be moved between categories');
      return;
    }
    onError('Markings cannot be moved between categories');
  }

  async function loadAuditEvents() {
    try {
      const resp = await listMarkingAuditEvents(marking.id);
      setAuditEvents(resp.items);
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to load marking audit events');
    }
  }

  async function runPermissionCheck() {
    try {
      const resp = await checkMarkingPermission(marking.id, {
        principal_id: checkPrincipalID.trim() || undefined,
        group_ids: idList(checkGroupIDs),
        resource_update_markings_allowed: resourceUpdateAllowed,
        expand_access_allowed: expandAccessAllowed,
      });
      setPermissionCheck(resp);
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to check marking permission');
    }
  }

  async function loadResourceMarkings() {
    if (!resourceKind.trim() || !resourceID.trim()) {
      onError('resource kind and resource id are required');
      return;
    }
    try {
      const resp = await listResourceMarkings(resourceKind.trim(), resourceID.trim());
      setResourceMarkings(resp.items);
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to load resource markings');
    }
  }

  async function applyDirectMarking() {
    if (!resourceKind.trim() || !resourceID.trim()) {
      onError('resource kind and resource id are required');
      return;
    }
    try {
      const resp = await applyResourceMarking({
        resource_kind: resourceKind.trim(),
        resource_id: resourceID.trim(),
        marking_id: marking.id,
        resource_update_markings_allowed: resourceUpdateAllowed,
      });
      setPermissionCheck(resp.permission_check);
      await loadResourceMarkings();
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to apply marking');
    }
  }

  async function removeDirectMarking() {
    if (!resourceKind.trim() || !resourceID.trim()) {
      onError('resource kind and resource id are required');
      return;
    }
    try {
      const resp = await removeResourceMarking({
        resource_kind: resourceKind.trim(),
        resource_id: resourceID.trim(),
        marking_id: marking.id,
        resource_update_markings_allowed: resourceUpdateAllowed,
        expand_access_allowed: expandAccessAllowed,
        reason: 'control-panel-admin-request',
      });
      setPermissionCheck(resp.permission_check);
      await loadResourceMarkings();
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to remove marking');
    }
  }

  async function loadEffectiveMarkings() {
    if (!resourceKind.trim() || !resourceID.trim()) {
      onError('resource kind and resource id are required');
      return;
    }
    try {
      const resp = await listEffectiveResourceMarkings(resourceKind.trim(), resourceID.trim());
      setEffectiveMarkings(resp.items);
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to load effective markings');
    }
  }

  async function loadResourceEdges() {
    if (!resourceKind.trim() || !resourceID.trim()) {
      onError('resource kind and resource id are required');
      return;
    }
    try {
      const resp = await listResourceMarkingEdges(resourceKind.trim(), resourceID.trim(), 'all');
      setResourceEdges(resp.items);
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to load marking inheritance edges');
    }
  }

  async function saveResourceEdge() {
    if (!edgeSourceKind.trim() || !edgeSourceID.trim() || !resourceKind.trim() || !resourceID.trim()) {
      onError('source and target resource fields are required');
      return;
    }
    try {
      await upsertResourceMarkingEdge({
        source_resource_kind: edgeSourceKind.trim(),
        source_resource_id: edgeSourceID.trim(),
        target_resource_kind: resourceKind.trim(),
        target_resource_id: resourceID.trim(),
        relation_kind: edgeRelationKind,
      });
      await loadResourceEdges();
      await loadEffectiveMarkings();
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to save marking inheritance edge');
    }
  }

  async function removeResourceEdge() {
    if (!edgeSourceKind.trim() || !edgeSourceID.trim() || !resourceKind.trim() || !resourceID.trim()) {
      onError('source and target resource fields are required');
      return;
    }
    try {
      await deleteResourceMarkingEdge({
        source_resource_kind: edgeSourceKind.trim(),
        source_resource_id: edgeSourceID.trim(),
        target_resource_kind: resourceKind.trim(),
        target_resource_id: resourceID.trim(),
        relation_kind: edgeRelationKind,
      });
      await loadResourceEdges();
      await loadEffectiveMarkings();
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to remove marking inheritance edge');
    }
  }

  async function runResourceAccessCheck() {
    if (!resourceKind.trim() || !resourceID.trim()) {
      onError('resource kind and resource id are required');
      return;
    }
    try {
      const resp = await checkResourceAccess({
        principal_id: checkPrincipalID.trim() || undefined,
        group_ids: idList(checkGroupIDs),
        resource_kind: resourceKind.trim(),
        resource_id: resourceID.trim(),
        required_organization_id: requiredOrgID.trim() || undefined,
        user_organization_ids: idList(userOrgIDs),
        role_satisfied: roleSatisfied,
        role_label: 'Caller-supplied project/folder role',
      });
      setAccessCheck(resp);
      setEffectiveMarkings(resp.effective_markings);
    } catch (cause) {
      onError(cause instanceof Error ? cause.message : 'Failed to check resource access');
    }
  }

  return (
    <article style={{ padding: 10, borderRadius: 8, background: 'var(--bg-surface)', display: 'grid', gap: 8 }}>
      <header style={{ display: 'flex', justifyContent: 'space-between', gap: 8, flexWrap: 'wrap' }}>
        <div>
          <strong>{marking.display_name}</strong>
          <div className="of-text-muted" style={{ fontSize: 11 }}>
            <code>{marking.slug}</code> · ID <code>{marking.id}</code>
            {marking.metadata_redacted ? ' · metadata redacted' : ''}
          </div>
        </div>
        <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
          <button className="of-button of-button--ghost" style={{ color: '#b91c1c' }} onClick={() => void deleteBlocked()}>
            Test delete block
          </button>
          <button className="of-button of-button--ghost" onClick={() => void loadAuditEvents()}>
            Audit
          </button>
        </div>
      </header>
      <div style={{ display: 'grid', gap: 8, gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))' }}>
        <label style={{ fontSize: 11 }}>
          Display name
          <input className="of-input" value={displayName} onChange={(event) => setDisplayName(event.target.value)} style={{ marginTop: 4 }} />
        </label>
        <label style={{ fontSize: 11 }}>
          Description
          <input className="of-input" value={description} onChange={(event) => setDescription(event.target.value)} style={{ marginTop: 4 }} />
        </label>
      </div>
      <label style={{ fontSize: 11 }}>
        Metadata JSON
        <textarea
          className="of-input"
          value={metadata}
          onChange={(event) => setMetadata(event.target.value)}
          style={{ marginTop: 4, minHeight: 64, fontFamily: 'var(--font-mono)', fontSize: 12 }}
        />
      </label>
      <div>
        <button className="of-button" onClick={() => void save()}>
          Save marking
        </button>
      </div>
      <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'grid', gap: 4, fontSize: 12 }}>
        {marking.permissions.length === 0 && <li className="of-text-muted">No explicit marking permissions.</li>}
        {marking.permissions.map((entry) => (
          <li
            key={`${entry.principal_kind}:${entry.principal_id}:${entry.permission}`}
            style={{ display: 'flex', justifyContent: 'space-between', gap: 8, alignItems: 'center', flexWrap: 'wrap' }}
          >
            <span>
              <strong>{entry.permission}</strong> · {entry.principal_kind} <code>{entry.principal_id}</code>
            </span>
            <button
              className="of-button of-button--ghost"
              style={{ fontSize: 11 }}
              onClick={() => void revoke(entry.principal_kind, entry.principal_id, entry.permission)}
            >
              Revoke
            </button>
          </li>
        ))}
      </ul>
      <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', alignItems: 'flex-end' }}>
        <label style={{ fontSize: 11 }}>
          Principal
          <select
            className="of-input"
            value={principalKind}
            onChange={(event) => setPrincipalKind(event.target.value as MarkingCategoryPrincipalKind)}
            style={{ marginTop: 4 }}
          >
            {PRINCIPAL_KINDS.map((item) => (
              <option key={item} value={item}>{item}</option>
            ))}
          </select>
        </label>
        <label style={{ fontSize: 11, flex: '1 1 220px' }}>
          Principal ID
          <input className="of-input" value={principalID} onChange={(event) => setPrincipalID(event.target.value)} style={{ marginTop: 4 }} />
        </label>
        <label style={{ fontSize: 11 }}>
          Permission
          <select
            className="of-input"
            value={permission}
            onChange={(event) => setPermission(event.target.value as MarkingPermissionName)}
            style={{ marginTop: 4 }}
          >
            {MARKING_PERMISSIONS.map((item) => (
              <option key={item} value={item}>{item}</option>
            ))}
          </select>
        </label>
        <button className="of-button" onClick={() => void grant()}>
          Grant
        </button>
      </div>
      <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap', alignItems: 'flex-end' }}>
        <label style={{ fontSize: 11, flex: '1 1 260px' }}>
          Target category ID
          <input className="of-input" value={moveTargetID} onChange={(event) => setMoveTargetID(event.target.value)} style={{ marginTop: 4 }} />
        </label>
        <button className="of-button of-button--ghost" onClick={() => void moveBlocked()}>
          Test move block
        </button>
      </div>
      <section style={{ display: 'grid', gap: 6, padding: 8, borderRadius: 8, background: 'var(--bg-subtle)' }}>
        <h4 style={{ fontSize: 11, margin: 0, textTransform: 'uppercase', letterSpacing: '0.08em' }}>
          Permission model
        </h4>
        <div style={{ display: 'grid', gap: 6, gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))' }}>
          <label style={{ fontSize: 11 }}>
            Principal ID
            <input className="of-input" value={checkPrincipalID} onChange={(event) => setCheckPrincipalID(event.target.value)} style={{ marginTop: 4 }} />
          </label>
          <label style={{ fontSize: 11 }}>
            Group IDs
            <input className="of-input" value={checkGroupIDs} onChange={(event) => setCheckGroupIDs(event.target.value)} style={{ marginTop: 4 }} />
          </label>
          <label style={{ fontSize: 11 }}>
            Resource kind
            <input className="of-input" value={resourceKind} onChange={(event) => setResourceKind(event.target.value)} style={{ marginTop: 4 }} />
          </label>
          <label style={{ fontSize: 11 }}>
            Resource ID
            <input className="of-input" value={resourceID} onChange={(event) => setResourceID(event.target.value)} style={{ marginTop: 4 }} />
          </label>
        </div>
        <div style={{ display: 'flex', gap: 12, flexWrap: 'wrap', fontSize: 11 }}>
          <label style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
            <input
              type="checkbox"
              checked={resourceUpdateAllowed}
              onChange={(event) => setResourceUpdateAllowed(event.target.checked)}
            />
            Resource update-markings role
          </label>
          <label style={{ display: 'inline-flex', alignItems: 'center', gap: 6 }}>
            <input
              type="checkbox"
              checked={expandAccessAllowed}
              onChange={(event) => setExpandAccessAllowed(event.target.checked)}
            />
            Expand-access equivalent
          </label>
        </div>
        <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
          <button className="of-button of-button--ghost" onClick={() => void runPermissionCheck()}>
            Check permissions
          </button>
          <button className="of-button" onClick={() => void applyDirectMarking()}>
            Apply to resource
          </button>
          <button className="of-button of-button--ghost" onClick={() => void removeDirectMarking()}>
            Remove from resource
          </button>
          <button className="of-button of-button--ghost" onClick={() => void loadResourceMarkings()}>
            Load resource markings
          </button>
        </div>
        {permissionCheck && (
          <div style={{ fontSize: 11, display: 'grid', gap: 4 }}>
            <div>
              manage {String(permissionCheck.can_manage)} · apply {String(permissionCheck.can_apply)} · remove {String(permissionCheck.can_remove)} · member {String(permissionCheck.is_member)} · data access {String(permissionCheck.can_access_marked_data)}
            </div>
            <div>
              apply resource {String(permissionCheck.can_apply_to_resource)} · remove resource {String(permissionCheck.can_remove_from_resource)}
            </div>
            <ul style={{ margin: 0, paddingLeft: 16 }}>
              {permissionCheck.reasons.map((reason) => (
                <li key={reason}>{reason}</li>
              ))}
            </ul>
          </div>
        )}
        {resourceMarkings && (
          <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'grid', gap: 4, fontSize: 11 }}>
            {resourceMarkings.length === 0 && <li className="of-text-muted">No direct markings on this resource.</li>}
            {resourceMarkings.map((entry) => (
              <li key={entry.id}>
                {entry.resource_kind} <code>{entry.resource_id}</code> · marking <code>{entry.marking_id}</code> · {entry.source_kind}
              </li>
            ))}
          </ul>
        )}
        <div style={{ display: 'grid', gap: 6, gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))' }}>
          <label style={{ fontSize: 11 }}>
            Inherits from kind
            <input className="of-input" value={edgeSourceKind} onChange={(event) => setEdgeSourceKind(event.target.value)} style={{ marginTop: 4 }} />
          </label>
          <label style={{ fontSize: 11 }}>
            Inherits from ID
            <input className="of-input" value={edgeSourceID} onChange={(event) => setEdgeSourceID(event.target.value)} style={{ marginTop: 4 }} />
          </label>
          <label style={{ fontSize: 11 }}>
            Relation
            <select
              className="of-input"
              value={edgeRelationKind}
              onChange={(event) => setEdgeRelationKind(event.target.value as ResourceMarkingRelationKind)}
              style={{ marginTop: 4 }}
            >
              {RELATION_KINDS.map((item) => (
                <option key={item} value={item}>{item}</option>
              ))}
            </select>
          </label>
        </div>
        <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
          <button className="of-button of-button--ghost" onClick={() => void saveResourceEdge()}>
            Save inheritance edge
          </button>
          <button className="of-button of-button--ghost" onClick={() => void removeResourceEdge()}>
            Remove inheritance edge
          </button>
          <button className="of-button of-button--ghost" onClick={() => void loadResourceEdges()}>
            Load edges
          </button>
          <button className="of-button of-button--ghost" onClick={() => void loadEffectiveMarkings()}>
            Effective markings
          </button>
        </div>
        {resourceEdges && (
          <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'grid', gap: 4, fontSize: 11 }}>
            {resourceEdges.length === 0 && <li className="of-text-muted">No inheritance edges for this resource.</li>}
            {resourceEdges.map((entry) => (
              <li key={entry.id}>
                {entry.source_resource_kind} <code>{entry.source_resource_id}</code> -&gt; {entry.target_resource_kind} <code>{entry.target_resource_id}</code> · {entry.relation_kind}
              </li>
            ))}
          </ul>
        )}
        {effectiveMarkings && (
          <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'grid', gap: 4, fontSize: 11 }}>
            {effectiveMarkings.length === 0 && <li className="of-text-muted">No effective markings on this resource.</li>}
            {effectiveMarkings.map((entry) => (
              <li key={entry.marking_id}>
                {entry.marking_name} · {entry.required_for.join(', ')} · {entry.sources.map((source) => `${source.source_kind}:${source.required_for}`).join(', ')}
              </li>
            ))}
          </ul>
        )}
        <div style={{ display: 'grid', gap: 6, gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))' }}>
          <label style={{ fontSize: 11 }}>
            Required organization ID
            <input className="of-input" value={requiredOrgID} onChange={(event) => setRequiredOrgID(event.target.value)} style={{ marginTop: 4 }} />
          </label>
          <label style={{ fontSize: 11 }}>
            User organization IDs
            <input className="of-input" value={userOrgIDs} onChange={(event) => setUserOrgIDs(event.target.value)} style={{ marginTop: 4 }} />
          </label>
          <label style={{ fontSize: 11, display: 'inline-flex', alignItems: 'center', gap: 6, marginTop: 20 }}>
            <input
              type="checkbox"
              checked={roleSatisfied}
              onChange={(event) => setRoleSatisfied(event.target.checked)}
            />
            Role requirement satisfied
          </label>
        </div>
        <div>
          <button className="of-button" onClick={() => void runResourceAccessCheck()}>
            Check resource access
          </button>
        </div>
        {accessCheck && (
          <div style={{ fontSize: 11, display: 'grid', gap: 4 }}>
            <div>
              resource access {String(accessCheck.resource_access_allowed)} · data access {String(accessCheck.data_access_allowed)}
            </div>
            {[...accessCheck.access_requirements, ...accessCheck.additional_data_requirements].map((req) => (
              <div key={`${req.kind}:${req.label}`}>
                {req.label}: {req.status}{req.missing?.length ? ` · missing ${req.missing.join(', ')}` : ''}
              </div>
            ))}
          </div>
        )}
      </section>
      {auditEvents && <AuditEventList events={auditEvents} />}
    </article>
  );
}

function AuditEventList({ events }: { events: Array<MarkingCategoryAuditEvent | MarkingAuditEvent> }) {
  return events.length === 0 ? (
    <p className="of-text-muted" style={{ fontSize: 12 }}>No audit events.</p>
  ) : (
    <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'grid', gap: 6 }}>
      {events.slice(0, 8).map((event) => (
        <li key={event.id} style={{ padding: 8, borderRadius: 8, background: 'var(--bg-surface)', fontSize: 11 }}>
          <strong>{event.action}</strong>{' '}
          <span className="of-text-muted">
            by <code>{event.actor_id}</code> at {new Date(event.created_at).toLocaleString()}
          </span>
          {event.principal_id && (
            <div className="of-text-muted">
              {event.principal_kind} <code>{event.principal_id}</code> · {event.permission}
            </div>
          )}
        </li>
      ))}
    </ul>
  );
}

function principalList(raw: string, defaultKind: MarkingCategoryPrincipalKind): MarkingCategoryPrincipal[] {
  return raw
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean)
    .map((principalID) => ({
      principal_kind: defaultKind,
      principal_id: principalID,
    }));
}

function idList(raw: string): string[] {
  return raw
    .split(',')
    .map((item) => item.trim())
    .filter(Boolean);
}

function parseResourceRefs(raw: string) {
  const refs = raw
    .split(/\r?\n|,/)
    .map((item) => item.trim())
    .filter(Boolean)
    .map((item) => {
      const [resourceKind, ...rest] = item.split(':');
      const resourceID = rest.join(':').trim();
      if (!resourceKind.trim() || !resourceID) {
        throw new Error('resource refs must use kind:id');
      }
      return {
        resource_kind: resourceKind.trim(),
        resource_id: resourceID,
      };
    });
  if (refs.length === 0) {
    throw new Error('at least one resource ref is required');
  }
  return refs;
}

function parseMetadata(raw: string): Record<string, unknown> {
  const trimmed = raw.trim();
  if (!trimmed) {
    return {};
  }
  const parsed = JSON.parse(trimmed) as unknown;
  if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object') {
    throw new Error('metadata must be a JSON object');
  }
  return parsed as Record<string, unknown>;
}
