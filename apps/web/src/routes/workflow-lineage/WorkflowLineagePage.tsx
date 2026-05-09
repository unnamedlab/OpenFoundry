import { useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate, useSearchParams } from 'react-router-dom';
import type { Core, ElementDefinition, EventObject, StylesheetStyle } from 'cytoscape';

import { getApp, listApps, type AppDefinition, type AppWidget } from '@/lib/api/apps';
import {
  listActionTypes,
  listObjectTypes,
  listProperties,
  type ActionType,
  type ObjectType,
  type Property,
} from '@/lib/api/ontology';
import { CytoscapeCanvas } from '@/lib/components/CytoscapeCanvas';
import { Glyph, type GlyphName } from '@/lib/components/ui/Glyph';

type LineageNodeKind = 'object_type' | 'action' | 'workshop';

interface LineageNode {
  id: string;
  kind: LineageNodeKind;
  label: string;
  ref_id: string;
}

interface LineageEdge {
  source: string;
  target: string;
}

const NODE_KIND_META: Record<LineageNodeKind, { label: string; color: string; icon: GlyphName }> = {
  object_type: { label: 'Object type', color: '#c9d8f1', icon: 'cube' },
  action: { label: 'Action', color: '#e5d6b6', icon: 'pencil' },
  workshop: { label: 'Workshop application', color: '#dccff0', icon: 'app' },
};

const STYLESHEET: StylesheetStyle[] = [
  {
    selector: 'node',
    style: {
      'background-color': 'data(color)',
      shape: 'round-rectangle',
      label: 'data(label)',
      color: '#1f252d',
      'text-valign': 'center',
      'text-halign': 'center',
      'font-size': 11,
      'font-weight': 600,
      'font-family': 'Arial, "Helvetica Neue", Helvetica, "Segoe UI", system-ui, -apple-system, sans-serif',
      width: 'label',
      height: 30,
      padding: '12px',
      'border-color': '#9aa3ad',
      'border-width': 1,
    },
  },
  {
    selector: 'node:selected',
    style: { 'border-color': '#2d72d2', 'border-width': 2 },
  },
  {
    selector: 'edge',
    style: {
      width: 1.5,
      'line-color': '#cf923f',
      'curve-style': 'bezier',
      'target-arrow-color': '#cf923f',
      'target-arrow-shape': 'triangle',
    },
  },
];

function resolveAppActionRefs(app: AppDefinition): string[] {
  const ids = new Set<string>();
  function walk(widget: AppWidget) {
    if (widget.widget_type === 'button_group') {
      const buttons = (widget.props as { buttons?: Array<{ action_type_id?: string }> } | undefined)?.buttons ?? [];
      for (const button of buttons) if (button.action_type_id) ids.add(button.action_type_id);
    }
    for (const child of widget.children ?? []) walk(child);
  }
  for (const page of app.pages ?? []) {
    for (const widget of page.widgets ?? []) walk(widget);
  }
  return Array.from(ids);
}

function resolveAppObjectTypeRefs(app: AppDefinition): string[] {
  const ids = new Set<string>();
  const settings = app.settings as { workshop_variables?: Array<{ object_type_id?: string }> } | null | undefined;
  for (const variable of settings?.workshop_variables ?? []) {
    if (variable.object_type_id) ids.add(variable.object_type_id);
  }
  function walk(widget: AppWidget) {
    const propAny = widget.props as { object_type_id?: string } | undefined;
    if (propAny?.object_type_id) ids.add(propAny.object_type_id);
    for (const child of widget.children ?? []) walk(child);
  }
  for (const page of app.pages ?? []) {
    for (const widget of page.widgets ?? []) walk(widget);
  }
  return Array.from(ids);
}

interface UsageHit {
  kind: LineageNodeKind;
  id: string;
  label: string;
}

export function WorkflowLineagePage() {
  const [searchParams] = useSearchParams();
  const focusAppId = searchParams.get('app') ?? '';
  const navigate = useNavigate();
  const [apps, setApps] = useState<AppDefinition[]>([]);
  const [actions, setActions] = useState<ActionType[]>([]);
  const [objectTypes, setObjectTypes] = useState<ObjectType[]>([]);
  const [propertiesByType, setPropertiesByType] = useState<Record<string, Property[]>>({});
  const [activeKinds, setActiveKinds] = useState<Set<LineageNodeKind>>(new Set(['object_type', 'action', 'workshop']));
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [entitySearch, setEntitySearch] = useState('');
  const [entitiesOpen, setEntitiesOpen] = useState(true);
  const [filterMenuOpen, setFilterMenuOpen] = useState(false);
  const [propertyUsageHover, setPropertyUsageHover] = useState<{ propertyName: string; objectTypeId: string } | null>(null);
  const [hoveredAction, setHoveredAction] = useState<string | null>(null);
  const cyRef = useRef<Core | null>(null);
  const [pinnedNodeIds, setPinnedNodeIds] = useState<Set<string>>(new Set());

  useEffect(() => {
    let cancelled = false;
    void (async () => {
      try {
        const [summariesResp, actionsResp, typesResp] = await Promise.all([
          listApps({ per_page: 200 }).catch(() => ({ data: [], total: 0 })),
          listActionTypes({ per_page: 200 }).catch(() => ({ data: [] as ActionType[], total: 0, page: 1, per_page: 200 })),
          listObjectTypes({ per_page: 200 }).catch(() => ({ data: [] as ObjectType[], total: 0, page: 1, per_page: 200 })),
        ]);
        if (cancelled) return;
        setActions(actionsResp.data);
        setObjectTypes(typesResp.data);
        const summaries = summariesResp.data ?? [];
        const definitions = await Promise.all(summaries.map((entry) => getApp(entry.id).catch(() => null)));
        if (cancelled) return;
        setApps(definitions.filter((entry): entry is AppDefinition => entry !== null));
      } catch {
        /* ignore */
      }
    })();
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    if (objectTypes.length === 0) return;
    let cancelled = false;
    void Promise.all(
      objectTypes.map((type) => listProperties(type.id).then((props) => [type.id, props] as const).catch(() => [type.id, [] as Property[]] as const)),
    ).then((entries) => {
      if (cancelled) return;
      setPropertiesByType(Object.fromEntries(entries));
    });
    return () => {
      cancelled = true;
    };
  }, [objectTypes]);

  const focusApp = focusAppId ? apps.find((entry) => entry.id === focusAppId) ?? null : null;

  const { nodes, edges } = useMemo(() => {
    const includedAppIds = new Set<string>();
    const includedActionIds = new Set<string>();
    const includedObjectTypeIds = new Set<string>();

    if (focusApp) {
      includedAppIds.add(focusApp.id);
      const actionRefs = resolveAppActionRefs(focusApp);
      const objectTypeRefs = resolveAppObjectTypeRefs(focusApp);
      for (const id of actionRefs) includedActionIds.add(id);
      for (const id of objectTypeRefs) includedObjectTypeIds.add(id);
      for (const action of actions) {
        if (includedActionIds.has(action.id)) includedObjectTypeIds.add(action.object_type_id);
      }
    } else {
      for (const app of apps) includedAppIds.add(app.id);
      for (const action of actions) includedActionIds.add(action.id);
      for (const type of objectTypes) includedObjectTypeIds.add(type.id);
    }

    const nodes: LineageNode[] = [];
    const edges: LineageEdge[] = [];

    for (const id of includedObjectTypeIds) {
      const type = objectTypes.find((entry) => entry.id === id);
      if (!type) continue;
      nodes.push({ id: `type:${type.id}`, kind: 'object_type', label: type.display_name || type.name, ref_id: type.id });
    }
    for (const id of includedActionIds) {
      const action = actions.find((entry) => entry.id === id);
      if (!action) continue;
      nodes.push({ id: `action:${action.id}`, kind: 'action', label: action.display_name || action.name, ref_id: action.id });
      if (includedObjectTypeIds.has(action.object_type_id)) {
        edges.push({ source: `type:${action.object_type_id}`, target: `action:${action.id}` });
      }
    }
    for (const id of includedAppIds) {
      const app = apps.find((entry) => entry.id === id);
      if (!app) continue;
      nodes.push({ id: `app:${app.id}`, kind: 'workshop', label: app.name, ref_id: app.id });
      const objectRefs = resolveAppObjectTypeRefs(app);
      const actionRefs = resolveAppActionRefs(app);
      for (const refId of objectRefs) {
        if (includedObjectTypeIds.has(refId)) {
          edges.push({ source: `type:${refId}`, target: `app:${app.id}` });
        }
      }
      for (const refId of actionRefs) {
        if (includedActionIds.has(refId)) {
          edges.push({ source: `action:${refId}`, target: `app:${app.id}` });
        }
      }
    }

    return { nodes, edges };
  }, [apps, actions, objectTypes, focusApp]);

  const counts: Record<LineageNodeKind, number> = useMemo(() => {
    const totals: Record<LineageNodeKind, number> = { object_type: 0, action: 0, workshop: 0 };
    for (const node of nodes) totals[node.kind] += 1;
    return totals;
  }, [nodes]);

  const filteredNodes = useMemo(() => nodes.filter((node) => activeKinds.has(node.kind)), [nodes, activeKinds]);
  const filteredEdges = useMemo(() => {
    const ids = new Set(filteredNodes.map((node) => node.id));
    return edges.filter((edge) => ids.has(edge.source) && ids.has(edge.target));
  }, [filteredNodes, edges]);

  const elements: ElementDefinition[] = useMemo(() => {
    return [
      ...filteredNodes.map((node) => ({
        data: { id: node.id, label: node.label, color: NODE_KIND_META[node.kind].color, kind: node.kind, ref_id: node.ref_id },
      })),
      ...filteredEdges.map((edge) => ({ data: { source: edge.source, target: edge.target } })),
    ];
  }, [filteredNodes, filteredEdges]);

  function handleReady(cy: Core) {
    cyRef.current = cy;
    cy.on('tap', 'node', (event: EventObject) => {
      setSelectedNodeId(String(event.target.id()));
    });
    cy.on('tap', (event: EventObject) => {
      if (event.target === cy) setSelectedNodeId(null);
    });
    cy.on('mouseover', 'node[kind = "action"]', (event: EventObject) => {
      setHoveredAction(String(event.target.id()));
    });
    cy.on('mouseout', 'node[kind = "action"]', () => {
      setHoveredAction(null);
    });
  }

  const selectedNode = selectedNodeId ? filteredNodes.find((n) => n.id === selectedNodeId) ?? null : null;
  const selectedAppHighlight = hoveredAction ? edges.find((edge) => edge.source === hoveredAction && edge.target.startsWith('app:'))?.target ?? null : null;

  function toggleKind(kind: LineageNodeKind) {
    setActiveKinds((current) => {
      const next = new Set(current);
      if (next.has(kind)) next.delete(kind);
      else next.add(kind);
      return next;
    });
  }

  return (
    <section style={{ display: 'grid', gridTemplateRows: 'auto 1fr', height: 'calc(100vh - 56px)', background: '#f4f6f9' }}>
      <header style={{ display: 'flex', alignItems: 'center', gap: 12, padding: '10px 16px', background: '#fff', borderBottom: '1px solid var(--border-subtle)' }}>
        <button type="button" className="of-button of-button--ghost" onClick={() => navigate(-1)} style={{ padding: '4px 6px' }}>
          <Glyph name="chevron-left" size={11} />
        </button>
        <strong style={{ fontSize: 14 }}>Workflow Lineage{focusApp ? ` · ${focusApp.name}` : ''}</strong>
        <span className="of-text-muted" style={{ fontSize: 12 }}>{nodes.length} entities</span>
      </header>
      <div style={{ display: 'grid', gridTemplateColumns: selectedNode ? '320px 1fr 1fr' : '1fr 1fr', minHeight: 0 }}>
        {selectedNode ? (
          <SelectionDetails
            node={selectedNode}
            objectTypes={objectTypes}
            actions={actions}
            apps={apps}
            propertiesByType={propertiesByType}
            edges={edges}
            propertyUsageHover={propertyUsageHover}
            onHoverProperty={setPropertyUsageHover}
            onClose={() => setSelectedNodeId(null)}
          />
        ) : null}

        <div style={{ position: 'relative', display: 'grid', gridTemplateRows: 'auto 1fr', minHeight: 0 }}>
          <div style={{ display: 'flex', alignItems: 'center', gap: 12, padding: '10px 14px', background: '#fff', borderBottom: '1px solid var(--border-subtle)', flexWrap: 'wrap' }}>
            <div style={{ position: 'relative' }}>
              <button
                type="button"
                onClick={() => setFilterMenuOpen((open) => !open)}
                style={{ display: 'inline-flex', alignItems: 'center', gap: 6, padding: '6px 12px', border: '1px solid var(--border-default)', borderRadius: 4, background: '#fff', cursor: 'pointer', fontSize: 13 }}
              >
                <Glyph name="cube" size={11} tone="#5c7080" /> Node type
                <Glyph name="chevron-down" size={11} />
              </button>
              {filterMenuOpen ? (
                <div role="menu" style={{ position: 'absolute', top: 'calc(100% + 4px)', left: 0, width: 280, background: '#fff', border: '1px solid var(--border-default)', borderRadius: 4, boxShadow: '0 8px 24px rgba(15, 23, 42, 0.16)', padding: 6, zIndex: 8 }}>
                  <input placeholder="Filter…" autoFocus style={{ width: '100%', padding: '6px 10px', border: '1px solid var(--border-default)', borderRadius: 4, fontSize: 13, marginBottom: 6 }} />
                  <p className="of-text-muted" style={{ margin: '4px 6px', fontSize: 11, textTransform: 'uppercase', letterSpacing: '0.05em' }}>Categories</p>
                  {(['Node type', 'Custom color', 'Permissions', 'Usage'] as const).map((entry) => (
                    <button key={entry} type="button" style={{ display: 'flex', alignItems: 'center', gap: 8, width: '100%', padding: '6px 10px', border: 0, background: 'transparent', cursor: 'pointer', textAlign: 'left', fontSize: 13, borderRadius: 4 }}>
                      <Glyph name={entry === 'Node type' ? 'graph' : entry === 'Custom color' ? 'tag' : entry === 'Permissions' ? 'lock' : 'eye'} size={11} tone="#5c7080" />
                      {entry}
                    </button>
                  ))}
                </div>
              ) : null}
            </div>
            {(['object_type', 'action', 'workshop'] as LineageNodeKind[]).map((kind) => {
              const active = activeKinds.has(kind);
              const meta = NODE_KIND_META[kind];
              return (
                <label key={kind} style={{ display: 'inline-flex', alignItems: 'center', gap: 6, padding: '4px 10px', border: '1px solid var(--border-subtle)', borderRadius: 4, fontSize: 12, cursor: 'pointer', opacity: active ? 1 : 0.6 }}>
                  <input type="checkbox" checked={active} onChange={() => toggleKind(kind)} />
                  <span style={{ display: 'inline-block', width: 10, height: 10, background: meta.color, borderRadius: 2 }} />
                  <Glyph name={meta.icon} size={11} tone="#5c7080" />
                  {meta.label}
                  <span className="of-chip" style={{ background: '#f0f3f7', color: 'var(--text-muted)', fontSize: 11 }}>{counts[kind]}</span>
                </label>
              );
            })}
          </div>
          <div style={{ position: 'relative', minHeight: 0, padding: 8 }}>
            <CytoscapeCanvas
              elements={elements}
              stylesheet={STYLESHEET}
              height="100%"
              onReady={handleReady}
            />
            <div style={{ position: 'absolute', top: 16, left: 16, display: 'grid', gap: 6 }}>
              {(['move', 'pencil', 'badge-check', 'circle-x', 'view-grid'] as const).map((icon) => (
                <button key={icon} type="button" aria-label={icon} className="of-button of-button--ghost" style={{ padding: 6, background: '#fff', border: '1px solid var(--border-subtle)', borderRadius: 4 }}>
                  <Glyph name={icon as GlyphName} size={13} tone="#5c7080" />
                </button>
              ))}
            </div>
            {hoveredAction ? (
              <p style={{ position: 'absolute', bottom: 16, left: 16, margin: 0, padding: '6px 10px', background: '#1c2127', color: '#fff', borderRadius: 4, fontSize: 12 }}>
                Hover: {filteredNodes.find((n) => n.id === hoveredAction)?.label}{selectedAppHighlight ? ` → ${filteredNodes.find((n) => n.id === selectedAppHighlight)?.label}` : ''}
              </p>
            ) : null}
            {pinnedNodeIds.size > 0 ? (
              <button
                type="button"
                onClick={() => setPinnedNodeIds(new Set())}
                style={{ position: 'absolute', top: 16, right: 16, padding: '4px 10px', border: '1px solid var(--border-subtle)', background: '#fff', borderRadius: 4, fontSize: 12, cursor: 'pointer' }}
              >
                Clear pins ({pinnedNodeIds.size})
              </button>
            ) : null}
          </div>
        </div>

        <aside style={{ display: 'grid', gridTemplateRows: '1fr auto', minHeight: 0, borderLeft: '1px solid var(--border-subtle)', background: '#f7f9fa' }}>
          <div style={{ padding: 14, overflowY: 'auto' }}>
            {focusApp ? (
              <WorkshopThumb app={focusApp} onOpen={() => navigate(`/apps/${focusApp.id}/workshop?mode=preview`)} />
            ) : (
              <p className="of-text-muted" style={{ margin: 0, fontSize: 13 }}>Pick a Workshop application to preview here.</p>
            )}
          </div>

          {entitiesOpen ? (
            <div style={{ borderTop: '1px solid var(--border-subtle)', background: '#fff', display: 'grid', gridTemplateRows: 'auto auto 1fr' }}>
              <header style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '8px 14px', borderBottom: '1px solid var(--border-subtle)' }}>
                <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8, fontSize: 13, fontWeight: 600 }}>
                  <Glyph name="cube" size={12} /> Entities
                </span>
                <button type="button" aria-label="Hide entities" onClick={() => setEntitiesOpen(false)} className="of-button of-button--ghost" style={{ padding: 4 }}>
                  <Glyph name="chevron-down" size={11} />
                </button>
              </header>
              <div style={{ padding: '8px 14px', borderBottom: '1px solid var(--border-subtle)' }}>
                <input
                  value={entitySearch}
                  onChange={(event) => setEntitySearch(event.target.value)}
                  placeholder="Search ontology entities…"
                  style={{ width: '100%', padding: '6px 10px', border: '1px solid var(--border-default)', borderRadius: 4, fontSize: 13 }}
                />
              </div>
              <div style={{ overflowY: 'auto', padding: 6 }}>
                {filteredNodes.filter((node) => node.label.toLowerCase().includes(entitySearch.toLowerCase())).map((node) => {
                  const meta = NODE_KIND_META[node.kind];
                  const active = selectedNodeId === node.id;
                  return (
                    <button
                      key={node.id}
                      type="button"
                      onClick={() => setSelectedNodeId(node.id)}
                      style={{ display: 'flex', alignItems: 'center', gap: 8, width: '100%', padding: '6px 10px', border: 0, background: active ? 'rgba(45, 114, 210, 0.06)' : 'transparent', cursor: 'pointer', textAlign: 'left', fontSize: 13, borderRadius: 4 }}
                    >
                      <span style={{ display: 'inline-block', width: 10, height: 10, background: meta.color, borderRadius: 2 }} />
                      <Glyph name={meta.icon} size={12} tone="#5c7080" />
                      <span style={{ flex: 1 }}>{node.label}</span>
                    </button>
                  );
                })}
              </div>
            </div>
          ) : (
            <button
              type="button"
              onClick={() => setEntitiesOpen(true)}
              style={{ padding: '8px 14px', border: 0, borderTop: '1px solid var(--border-subtle)', background: '#fff', cursor: 'pointer', textAlign: 'left', fontSize: 13, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}
            >
              <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}><Glyph name="cube" size={12} /> Entities</span>
              <span style={{ display: 'inline-block', transform: 'rotate(180deg)' }}><Glyph name="chevron-down" size={11} /></span>
            </button>
          )}
        </aside>
      </div>

      {selectedNode && selectedNode.kind === 'action' ? (
        <ActionFooter
          actionId={selectedNode.ref_id}
          onPin={() => setPinnedNodeIds((current) => new Set([...current, selectedNode.id]))}
          onDetails={() => navigate(`/action-types/${selectedNode.ref_id}`)}
        />
      ) : null}
    </section>
  );
}

function SelectionDetails({
  node,
  objectTypes,
  actions,
  apps,
  propertiesByType,
  edges,
  propertyUsageHover,
  onHoverProperty,
  onClose,
}: {
  node: LineageNode;
  objectTypes: ObjectType[];
  actions: ActionType[];
  apps: AppDefinition[];
  propertiesByType: Record<string, Property[]>;
  edges: LineageEdge[];
  propertyUsageHover: { propertyName: string; objectTypeId: string } | null;
  onHoverProperty: (value: { propertyName: string; objectTypeId: string } | null) => void;
  onClose: () => void;
}) {
  const objectType = node.kind === 'object_type' ? objectTypes.find((entry) => entry.id === node.ref_id) ?? null : null;
  const action = node.kind === 'action' ? actions.find((entry) => entry.id === node.ref_id) ?? null : null;
  const app = node.kind === 'workshop' ? apps.find((entry) => entry.id === node.ref_id) ?? null : null;
  const properties = objectType ? propertiesByType[objectType.id] ?? [] : [];

  function findUsages(propertyName: string): UsageHit[] {
    if (!objectType) return [];
    const hits: UsageHit[] = [];
    for (const otherAction of actions) {
      if (otherAction.object_type_id !== objectType.id) continue;
      const config = otherAction.config as { property_mappings?: Array<{ property_name: string }> } | null | undefined;
      const has = (config?.property_mappings ?? []).some((mapping) => mapping.property_name === propertyName);
      if (has) hits.push({ kind: 'action', id: otherAction.id, label: otherAction.display_name || otherAction.name });
    }
    for (const otherApp of apps) {
      const settings = otherApp.settings as { workshop_variables?: Array<{ object_type_id?: string }> } | null | undefined;
      const usesType = (settings?.workshop_variables ?? []).some((variable) => variable.object_type_id === objectType.id);
      if (usesType) {
        hits.push({ kind: 'workshop', id: otherApp.id, label: otherApp.name });
      }
    }
    return hits;
  }

  return (
    <aside style={{ background: '#fff', borderRight: '1px solid var(--border-subtle)', display: 'grid', gridTemplateRows: 'auto 1fr', minHeight: 0 }}>
      <header style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '12px 14px', borderBottom: '1px solid var(--border-subtle)' }}>
        <span style={{ display: 'inline-flex', alignItems: 'center', gap: 8, fontSize: 13, fontWeight: 600 }}>
          <Glyph name="list" size={13} /> Selection details
        </span>
        <button type="button" aria-label="Close" onClick={onClose} className="of-button of-button--ghost" style={{ padding: 4 }}><Glyph name="chevron-left" size={11} /></button>
      </header>
      <div style={{ overflowY: 'auto', padding: 14 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <span style={{ display: 'inline-flex', alignItems: 'center', justifyContent: 'center', width: 28, height: 28, borderRadius: 4, background: NODE_KIND_META[node.kind].color }}>
            <Glyph name={NODE_KIND_META[node.kind].icon} size={13} tone="#1f4ea0" />
          </span>
          <span style={{ flex: 1, fontSize: 14, fontWeight: 600 }}>{node.label}</span>
        </div>

        {objectType ? (
          <>
            <SelectionRow label="API" value={objectType.name} />
            <SelectionRow label="RID" value={objectType.id} />
            <p style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', margin: '14px 0 6px', fontSize: 12, fontWeight: 600 }}>
              <span>Properties <span className="of-chip" style={{ fontSize: 11 }}>{properties.length}</span></span>
              {properties.some((p) => findUsages(p.name).length > 0) ? <span className="of-status-danger" style={{ fontSize: 11, padding: '2px 8px', borderRadius: 4 }}>!</span> : null}
            </p>
            <div style={{ display: 'grid', gap: 4 }}>
              {properties.map((property) => {
                const usages = findUsages(property.name);
                const isHovering = propertyUsageHover?.propertyName === property.name && propertyUsageHover.objectTypeId === objectType.id;
                return (
                  <div
                    key={property.id}
                    onMouseEnter={() => onHoverProperty({ propertyName: property.name, objectTypeId: objectType.id })}
                    onMouseLeave={() => onHoverProperty(null)}
                    style={{ position: 'relative', display: 'flex', alignItems: 'center', gap: 8, padding: '6px 8px', border: '1px solid var(--border-subtle)', borderRadius: 4, fontSize: 13, background: isHovering ? '#f7f9fa' : '#fff' }}
                  >
                    <Glyph name="tag" size={11} tone="#5c7080" />
                    <span style={{ flex: 1 }}>{property.display_name || property.name}</span>
                    {usages.length > 0 ? <span className="of-chip" style={{ fontSize: 11 }}>{usages.length}</span> : null}
                    {isHovering && usages.length > 0 ? (
                      <div role="tooltip" style={{ position: 'absolute', top: '100%', left: 0, marginTop: 4, zIndex: 12, background: '#fff', border: '1px solid var(--border-default)', borderRadius: 6, boxShadow: '0 8px 24px rgba(15, 23, 42, 0.16)', padding: 8, minWidth: 240 }}>
                        <p style={{ margin: '0 0 6px', fontSize: 12, fontWeight: 600 }}>{usages.length} usages found</p>
                        {usages.map((hit) => (
                          <div key={`${hit.kind}-${hit.id}`} style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '4px 0' }}>
                            <Glyph name={NODE_KIND_META[hit.kind].icon} size={12} tone="#5c7080" />
                            <span style={{ fontSize: 13 }}>{hit.label}</span>
                          </div>
                        ))}
                      </div>
                    ) : null}
                  </div>
                );
              })}
            </div>
            <a className="of-link" style={{ display: 'block', marginTop: 8, fontSize: 12 }}>See all properties</a>
            {(['Links', 'Groups', 'Interfaces', 'Data sources'] as const).map((label) => (
              <p key={label} style={{ margin: '14px 0 4px', fontSize: 12, fontWeight: 600, display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                {label} <span className="of-chip" style={{ fontSize: 11 }}>{label === 'Data sources' ? 1 : 0}</span>
              </p>
            ))}
          </>
        ) : null}

        {action ? (
          <>
            <SelectionRow label="API" value={action.name} />
            <SelectionRow label="Object type" value={objectTypes.find((entry) => entry.id === action.object_type_id)?.display_name || action.object_type_id} />
            <SelectionRow label="Operation" value={action.operation_kind} />
            <p style={{ margin: '14px 0 6px', fontSize: 12, fontWeight: 600 }}>Inputs <span className="of-chip" style={{ fontSize: 11 }}>{action.input_schema?.length ?? 0}</span></p>
            {(action.input_schema ?? []).map((input) => (
              <div key={input.name} style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '6px 8px', border: '1px solid var(--border-subtle)', borderRadius: 4, fontSize: 13, marginBottom: 4 }}>
                <Glyph name="tag" size={11} tone="#5c7080" />
                {input.display_name || input.name}
              </div>
            ))}
          </>
        ) : null}

        {app ? (
          <>
            <SelectionRow label="Slug" value={app.slug ?? ''} />
            <SelectionRow label="Status" value={app.status} />
            <SelectionRow label="Pages" value={String(app.pages?.length ?? 0)} />
            <p style={{ margin: '14px 0 6px', fontSize: 12, fontWeight: 600 }}>Linked entities</p>
            {edges.filter((edge) => edge.target === `app:${app.id}`).map((edge) => {
              const id = edge.source.replace(/^.*?:/, '');
              const ot = objectTypes.find((t) => t.id === id);
              const ac = actions.find((a) => a.id === id);
              return (
                <div key={edge.source} style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '6px 8px', border: '1px solid var(--border-subtle)', borderRadius: 4, fontSize: 13, marginBottom: 4 }}>
                  <Glyph name={ot ? 'cube' : 'pencil'} size={11} tone="#5c7080" />
                  {ot ? (ot.display_name || ot.name) : ac ? (ac.display_name || ac.name) : edge.source}
                </div>
              );
            })}
          </>
        ) : null}
      </div>
    </aside>
  );
}

function SelectionRow({ label, value }: { label: string; value: string }) {
  return (
    <div style={{ display: 'grid', gridTemplateColumns: '60px 1fr', gap: 8, alignItems: 'center', padding: '8px 0', borderBottom: '1px solid var(--border-subtle)' }}>
      <span className="of-text-muted" style={{ fontSize: 11, textTransform: 'uppercase', letterSpacing: '0.04em' }}>{label}</span>
      <span style={{ fontSize: 13, fontFamily: 'var(--font-mono)', overflowWrap: 'anywhere' }}>{value}</span>
    </div>
  );
}

function WorkshopThumb({ app, onOpen }: { app: AppDefinition; onOpen: () => void }) {
  return (
    <div style={{ background: '#fff', border: '1px solid var(--border-subtle)', borderRadius: 6, padding: 14, display: 'grid', gap: 10 }}>
      <header style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
        <span style={{ display: 'inline-flex', alignItems: 'center', justifyContent: 'center', width: 28, height: 28, borderRadius: 4, background: 'rgba(45, 114, 210, 0.08)' }}>
          <Glyph name="cube" size={14} tone="#2d72d2" />
        </span>
        <strong style={{ flex: 1, fontSize: 14 }}>{app.name}</strong>
        <button type="button" className="of-button" onClick={onOpen}>
          <Glyph name="external-link" size={11} /> Open
        </button>
      </header>
      <p className="of-text-muted" style={{ margin: 0, fontSize: 12 }}>{app.description || 'No description.'}</p>
      <p className="of-text-muted" style={{ margin: 0, fontSize: 11 }}>Pages: {app.pages?.length ?? 0} · Status: {app.status}</p>
    </div>
  );
}

function ActionFooter({ actionId, onPin, onDetails }: { actionId: string; onPin: () => void; onDetails: () => void }) {
  void actionId;
  return (
    <footer style={{ position: 'fixed', bottom: 0, left: 0, right: 0, padding: '6px 16px', background: '#fff', borderTop: '1px solid var(--border-subtle)', display: 'flex', alignItems: 'center', gap: 14, fontSize: 12 }}>
      <button type="button" className="of-button" onClick={onDetails}><Glyph name="list" size={11} /> Details</button>
      <button type="button" className="of-button" onClick={onPin}><Glyph name="bookmark" size={11} /> Pin</button>
      <span style={{ flex: 1 }} />
      {(['Update submission criteria', 'Action log', 'Action metrics', 'Used in functions', 'Run history'] as const).map((label) => (
        <button key={label} type="button" className="of-button of-button--ghost" style={{ fontSize: 12 }}>
          <Glyph name={label === 'Update submission criteria' ? 'pencil' : label === 'Action log' ? 'autosaved' : label === 'Action metrics' ? 'graph' : label === 'Used in functions' ? 'code' : 'history'} size={11} /> {label}
        </button>
      ))}
    </footer>
  );
}
