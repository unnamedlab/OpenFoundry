import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';

import { Glyph } from '@/lib/components/ui/Glyph';
import { listRecents, type RecentEntry } from '@/lib/api/workspace';

function formatDate(value: string) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

function resourceHref(entry: RecentEntry): string | null {
  if (entry.resource_kind === 'ontology_project') return `/projects/${entry.resource_id}`;
  if (entry.resource_kind === 'dataset') return `/datasets/${entry.resource_id}`;
  return null;
}

export function RecentPage() {
  const [entries, setEntries] = useState<RecentEntry[]>([]);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState('');

  useEffect(() => {
    let cancelled = false;
    listRecents({ limit: 50 })
      .then((next) => {
        if (!cancelled) setEntries(next);
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
          <p className="of-text-muted" style={{ padding: 28, textAlign: 'center', margin: 0 }}>Nothing recent yet.</p>
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
                const href = resourceHref(entry);
                return (
                  <tr key={`${entry.resource_kind}-${entry.resource_id}`}>
                    <td style={{ paddingLeft: 22 }}>
                      {href ? (
                        <Link to={href} className="of-link">{entry.resource_id}</Link>
                      ) : (
                        <span>{entry.resource_id}</span>
                      )}
                    </td>
                    <td className="of-text-muted">{entry.resource_kind.replace(/_/g, ' ')}</td>
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
