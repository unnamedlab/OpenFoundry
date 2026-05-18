import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';

import { Glyph, type GlyphName } from '@/lib/components/ui/Glyph';
import { listRecents, resolveResourceLabels, type RecentEntry, type ResourceKind } from '@/lib/api/workspace';
import { workspaceResourceStablePath } from '@/lib/compass/stableResourceUrls';

function formatDate(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

function resourceKey(entry: Pick<RecentEntry, 'resource_kind' | 'resource_id'>) {
  return `${entry.resource_kind}:${entry.resource_id}`;
}

function shortID(value: string) {
  if (value.length <= 18) return value;
  return `${value.slice(0, 8)}...${value.slice(-6)}`;
}

function resourceKindLabel(kind: ResourceKind): string {
  return kind.replace(/^ontology_/, '').replace(/_/g, ' ');
}

function glyphForResource(kind: ResourceKind): GlyphName {
  if (kind === 'ontology_project') return 'project';
  if (kind === 'ontology_folder') return 'folder';
  if (kind === 'dataset') return 'database';
  if (kind === 'pipeline') return 'graph';
  if (kind === 'notebook') return 'code';
  if (kind === 'app') return 'app';
  if (kind === 'dashboard') return 'spreadsheet';
  if (kind === 'report') return 'document';
  if (kind === 'model') return 'cube';
  if (kind === 'workflow') return 'run';
  return 'object';
}

export function RecentPage() {
  const [entries, setEntries] = useState<RecentEntry[]>([]);
  const [labels, setLabels] = useState<Map<string, string>>(new Map());
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  useEffect(() => {
    let cancelled = false;
    listRecents({ limit: 50 })
      .then(async (next) => {
        const resolved = next.length > 0
          ? await resolveResourceLabels(next.map((entry) => ({
            resource_kind: entry.resource_kind,
            resource_id: entry.resource_id,
          }))).catch(() => null)
          : null;
        if (cancelled) return;
        const nextLabels = new Map<string, string>();
        for (const entry of resolved?.data ?? []) {
          if (entry.label) nextLabels.set(resourceKey(entry), entry.label);
        }
        setEntries(next);
        setLabels(nextLabels);
      })
      .catch((cause: unknown) => {
        if (!cancelled) setError(cause instanceof Error ? cause.message : 'Failed to load recent activity');
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <section style={{ padding: '24px 28px', display: 'grid', gap: 14 }}>
      <header style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
        <Glyph name="history" size={18} />
        <h1 style={{ margin: 0, fontSize: 20, fontWeight: 600 }}>Recent</h1>
      </header>
      {error ? (
        <div className="of-status-danger" style={{ padding: '10px 14px', borderRadius: 6, fontSize: 13 }}>
          {error}
        </div>
      ) : null}
      <div className="of-panel" style={{ padding: 0 }}>
        {loading ? (
          <p className="of-text-muted" style={{ padding: 28, textAlign: 'center', margin: 0 }}>Loading recent activity...</p>
        ) : entries.length === 0 ? (
          <p className="of-text-muted" style={{ padding: 28, textAlign: 'center', margin: 0 }}>No visible recent resources.</p>
        ) : (
          <table className="of-table">
            <thead>
              <tr>
                <th style={{ paddingLeft: 22 }}>Resource</th>
                <th>Kind</th>
                <th>Last accessed</th>
              </tr>
            </thead>
            <tbody>
              {entries.map((entry) => {
                const label = labels.get(resourceKey(entry)) ?? shortID(entry.resource_id);
                return (
                  <tr key={`${entry.resource_kind}-${entry.resource_id}`}>
                    <td style={{ paddingLeft: 22 }}>
                      <Link
                        to={workspaceResourceStablePath(entry.resource_kind, entry.resource_id, label)}
                        className="of-link"
                        style={{ display: 'inline-flex', alignItems: 'center', gap: 8 }}
                      >
                        <Glyph name={glyphForResource(entry.resource_kind)} size={14} />
                        <span>{label}</span>
                      </Link>
                    </td>
                    <td className="of-text-muted">{resourceKindLabel(entry.resource_kind)}</td>
                    <td className="of-text-muted">{formatDate(entry.last_accessed_at)}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        )}
      </div>
    </section>
  );
}
