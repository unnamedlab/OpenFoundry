import { useMemo, useState } from 'react';
import { Link } from 'react-router-dom';

import { Glyph, type GlyphName } from '@/lib/components/ui/Glyph';

type Category =
  | 'all'
  | 'platform'
  | 'administration'
  | 'analytics'
  | 'application-development'
  | 'data-integration'
  | 'developer-toolchain'
  | 'ontology';

interface AppEntry {
  id: string;
  name: string;
  description: string;
  to: string;
  icon: GlyphName;
  category: Exclude<Category, 'all' | 'platform'>;
}

const CATEGORIES: { id: Category; label: string }[] = [
  { id: 'all', label: 'All apps' },
  { id: 'platform', label: 'Platform apps' },
  { id: 'administration', label: 'Administration' },
  { id: 'analytics', label: 'Analytics & Operations' },
  { id: 'application-development', label: 'Application development' },
  { id: 'data-integration', label: 'Data integration' },
  { id: 'developer-toolchain', label: 'Developer toolchain' },
  { id: 'ontology', label: 'Ontology' },
];

const APPS: AppEntry[] = [
  { id: 'object-explorer', name: 'Object explorer', description: 'Search and inspect ontology objects.', to: '/object-explorer', icon: 'search', category: 'application-development' },
  { id: 'dashboards', name: 'Dashboards', description: 'Build and share interactive dashboards.', to: '/dashboards', icon: 'graph', category: 'analytics' },
  { id: 'contour', name: 'Contour', description: 'Point-and-click data analysis on datasets and ontology.', to: '/contour', icon: 'graph', category: 'analytics' },
  { id: 'quiver', name: 'Quiver', description: 'Pivot, chart, and slice data in a spreadsheet.', to: '/quiver', icon: 'graph', category: 'analytics' },
  { id: 'notepad', name: 'Notepad', description: 'Author rich documents with embedded data.', to: '/notepad', icon: 'document', category: 'analytics' },
  { id: 'reports', name: 'Reports', description: 'Compose and publish report deliverables.', to: '/reports', icon: 'list', category: 'analytics' },
  { id: 'workshop', name: 'Workshop', description: 'Build operational apps backed by your ontology and datasets.', to: '/apps', icon: 'object', category: 'application-development' },
  { id: 'pipeline-builder', name: 'Pipeline builder', description: 'Compose dataset transformation pipelines.', to: '/pipelines', icon: 'run', category: 'data-integration' },
  { id: 'code-repos', name: 'Code repositories', description: 'Version-controlled code repositories for transforms and SDKs.', to: '/code-repos', icon: 'code', category: 'developer-toolchain' },
  { id: 'ontology-manager', name: 'Ontology manager', description: 'Manage object types, links, properties, and shared types.', to: '/ontology-manager', icon: 'ontology', category: 'ontology' },
  { id: 'ontology', name: 'Ontology', description: 'Browse the ontology graph and object sets.', to: '/ontology', icon: 'cube', category: 'ontology' },
  { id: 'object-link-types', name: 'Object & link types', description: 'Define object and link types.', to: '/object-link-types', icon: 'link', category: 'ontology' },
  { id: 'interfaces', name: 'Interfaces', description: 'Cross-type capability interfaces.', to: '/interfaces', icon: 'artifact', category: 'ontology' },
  { id: 'functions', name: 'Functions', description: 'Author functions, actions, and rules.', to: '/functions', icon: 'code', category: 'developer-toolchain' },
  { id: 'foundry-rules', name: 'Foundry Rules', description: 'Continuous monitors over your ontology and datasets.', to: '/foundry-rules', icon: 'settings', category: 'administration' },
  { id: 'data-connection', name: 'Data Connection', description: 'Connect to external sources and stream data in.', to: '/data-connection', icon: 'database', category: 'data-integration' },
  { id: 'streaming', name: 'Streaming', description: 'Operate streaming pipelines and inspect live data.', to: '/streaming', icon: 'run', category: 'data-integration' },
  { id: 'builds', name: 'Builds', description: 'Inspect dataset builds and downstream impact.', to: '/builds', icon: 'history', category: 'data-integration' },
  { id: 'build-schedules', name: 'Build schedules', description: 'Schedule and operate dataset builds.', to: '/build-schedules', icon: 'history', category: 'data-integration' },
  { id: 'lineage', name: 'Lineage', description: 'Trace dataset and ontology dependencies.', to: '/lineage', icon: 'graph', category: 'data-integration' },
  { id: 'marketplace', name: 'Marketplace', description: 'Discover and install Foundry-built products.', to: '/marketplace', icon: 'app', category: 'developer-toolchain' },
  { id: 'developers', name: 'Developers', description: 'Developer console, API keys, and SDK references.', to: '/developers', icon: 'code', category: 'developer-toolchain' },
  { id: 'control-panel', name: 'Control panel', description: 'Tenant-wide controls, governance, and quotas.', to: '/control-panel', icon: 'settings', category: 'administration' },
  { id: 'settings', name: 'Workspace settings', description: 'Settings for users, roles, policies, MFA, SSO.', to: '/settings', icon: 'settings', category: 'administration' },
];

function categoryCount(category: Category) {
  if (category === 'all' || category === 'platform') return APPS.length;
  return APPS.filter((app) => app.category === category).length;
}

export function ApplicationsPage() {
  const [search, setSearch] = useState('');
  const [category, setCategory] = useState<Category>('all');

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    return APPS.filter((app) => {
      if (category !== 'all' && category !== 'platform' && app.category !== category) return false;
      if (!q) return true;
      return app.name.toLowerCase().includes(q) || app.description.toLowerCase().includes(q);
    });
  }, [search, category]);

  return (
    <div style={{ display: 'flex', flexDirection: 'column', minHeight: '100%', background: '#1c2127', color: '#f3f4f6' }}>
      <header style={{ padding: '20px 24px 12px', borderBottom: '1px solid rgba(255,255,255,0.06)' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, color: '#aab4c0', fontSize: 12 }}>
          <Glyph name="view-grid" size={14} tone="#aab4c0" />
          <span>Applications</span>
        </div>
        <div style={{ marginTop: 14, display: 'flex', alignItems: 'center', gap: 10, background: '#11171d', borderRadius: 6, padding: '10px 12px', maxWidth: 720 }}>
          <Glyph name="search" size={16} tone="#7c8da3" />
          <input
            type="search"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search for apps..."
            style={{
              flex: 1,
              background: 'transparent',
              border: 0,
              outline: 'none',
              color: '#f3f4f6',
              fontSize: 14,
            }}
            autoFocus
          />
        </div>
      </header>

      <div style={{ display: 'flex', flex: '1 1 auto', minHeight: 0 }}>
        <aside style={{ width: 240, flex: '0 0 240px', borderRight: '1px solid rgba(255,255,255,0.06)', padding: '16px 8px' }}>
          {CATEGORIES.map((cat) => {
            const active = category === cat.id;
            return (
              <button
                key={cat.id}
                type="button"
                onClick={() => setCategory(cat.id)}
                style={{
                  display: 'flex',
                  width: '100%',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  padding: '8px 12px',
                  border: 0,
                  background: active ? 'rgba(255,255,255,0.04)' : 'transparent',
                  color: active ? '#f3f4f6' : '#aab4c0',
                  fontWeight: active ? 600 : 500,
                  fontSize: 13,
                  borderRadius: 4,
                  cursor: 'pointer',
                  textAlign: 'left',
                }}
              >
                <span>{cat.label}</span>
                <span style={{ color: '#7c8da3', fontVariantNumeric: 'tabular-nums' }}>{categoryCount(cat.id)}</span>
              </button>
            );
          })}
        </aside>

        <main style={{ flex: 1, padding: '16px 22px', overflowY: 'auto' }}>
          {filtered.length === 0 ? (
            <p style={{ color: '#aab4c0', fontSize: 13 }}>No apps match the current filters.</p>
          ) : (
            <div
              style={{
                display: 'grid',
                gap: 10,
                gridTemplateColumns: 'repeat(auto-fill, minmax(360px, 1fr))',
              }}
            >
              {filtered.map((app) => (
                <Link
                  key={app.id}
                  to={app.to}
                  style={{
                    display: 'flex',
                    alignItems: 'flex-start',
                    gap: 12,
                    padding: '12px 14px',
                    borderRadius: 6,
                    background: '#11171d',
                    color: '#f3f4f6',
                    textDecoration: 'none',
                    border: '1px solid rgba(255,255,255,0.04)',
                  }}
                >
                  <span
                    style={{
                      display: 'inline-flex',
                      alignItems: 'center',
                      justifyContent: 'center',
                      width: 36,
                      height: 36,
                      background: 'rgba(45, 114, 210, 0.15)',
                      color: '#60a5fa',
                      borderRadius: 6,
                      flex: '0 0 auto',
                    }}
                  >
                    <Glyph name={app.icon} size={18} tone="#60a5fa" />
                  </span>
                  <span style={{ display: 'grid', gap: 2, minWidth: 0 }}>
                    <span style={{ fontSize: 14, fontWeight: 600 }}>{app.name}</span>
                    <span style={{ fontSize: 12, color: '#aab4c0' }}>{app.description}</span>
                  </span>
                </Link>
              ))}
            </div>
          )}
        </main>
      </div>
    </div>
  );
}
