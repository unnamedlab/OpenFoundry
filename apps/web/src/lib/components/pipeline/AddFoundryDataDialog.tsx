import { useEffect, useMemo, useState } from 'react';

import { listDatasets, type Dataset } from '@/lib/api/datasets';
import { Glyph } from '@/lib/components/ui/Glyph';

interface AddFoundryDataDialogProps {
  open: boolean;
  onClose: () => void;
  onAdd: (datasets: Dataset[]) => void;
}

function formatRowCount(count: number) {
  if (count >= 1_000_000) return `${(count / 1_000_000).toFixed(1)}M rows`;
  if (count >= 1_000) return `${(count / 1_000).toFixed(1)}K rows`;
  return `${count} rows`;
}

function formatBytes(bytes: number) {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(1)} GB`;
}

export function AddFoundryDataDialog({ open, onClose, onAdd }: AddFoundryDataDialogProps) {
  const [datasets, setDatasets] = useState<Dataset[]>([]);
  const [search, setSearch] = useState('');
  const [selected, setSelected] = useState<Map<string, Dataset>>(new Map());
  const [activeId, setActiveId] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    setLoading(true);
    setError('');
    listDatasets({ per_page: 200 })
      .then((response) => {
        if (cancelled) return;
        setDatasets(response.data);
      })
      .catch((cause) => {
        if (cancelled) return;
        setError(cause instanceof Error ? cause.message : 'Failed to load datasets');
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [open]);

  useEffect(() => {
    if (!open) {
      setSelected(new Map());
      setActiveId(null);
      setSearch('');
    }
  }, [open]);

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    if (!q) return datasets;
    return datasets.filter((entry) => `${entry.name} ${entry.description}`.toLowerCase().includes(q));
  }, [datasets, search]);

  const active = useMemo(() => datasets.find((entry) => entry.id === activeId) ?? null, [datasets, activeId]);

  if (!open) return null;

  function toggleDataset(dataset: Dataset) {
    setSelected((current) => {
      const next = new Map(current);
      if (next.has(dataset.id)) next.delete(dataset.id);
      else next.set(dataset.id, dataset);
      return next;
    });
  }

  function addAllVisible() {
    setSelected((current) => {
      const next = new Map(current);
      for (const dataset of filtered) next.set(dataset.id, dataset);
      return next;
    });
  }

  function commit() {
    onAdd([...selected.values()]);
    onClose();
  }

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="add-foundry-data-title"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
      style={{
        position: 'fixed',
        inset: 0,
        zIndex: 100,
        background: 'rgba(17, 24, 39, 0.42)',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'center',
        padding: 24,
      }}
    >
      <section
        style={{
          width: '100%',
          maxWidth: 1080,
          height: 'min(720px, calc(100vh - 48px))',
          background: '#fff',
          borderRadius: 6,
          boxShadow: '0 12px 32px rgba(15, 23, 42, 0.2)',
          display: 'grid',
          gridTemplateRows: 'auto 1fr',
          overflow: 'hidden',
        }}
      >
        <header
          style={{
            display: 'flex',
            alignItems: 'center',
            justifyContent: 'space-between',
            padding: '12px 16px',
            borderBottom: '1px solid var(--border-subtle)',
          }}
        >
          <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
            <Glyph name="database" size={16} tone="#2d72d2" />
            <h2 id="add-foundry-data-title" style={{ margin: 0, fontSize: 15, fontWeight: 600 }}>Add data</h2>
          </div>
          <button
            type="button"
            onClick={onClose}
            aria-label="Close"
            style={{ border: 0, background: 'transparent', padding: 4, cursor: 'pointer', color: 'var(--text-muted)' }}
          >
            <Glyph name="x" size={14} />
          </button>
        </header>

        <div style={{ display: 'grid', gridTemplateColumns: '300px minmax(0, 1fr) 320px', minHeight: 0 }}>
          <aside style={{ borderRight: '1px solid var(--border-subtle)', display: 'grid', gridTemplateRows: 'auto 1fr auto', minHeight: 0 }}>
            <div style={{ padding: 12, borderBottom: '1px solid var(--border-subtle)' }}>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '6px 10px', border: '1px solid var(--border-default)', borderRadius: 4, background: '#f4f6f9' }}>
                <Glyph name="search" size={14} tone="#5c7080" />
                <input
                  type="search"
                  value={search}
                  onChange={(event) => setSearch(event.target.value)}
                  placeholder="Search all files"
                  style={{ flex: 1, background: 'transparent', border: 0, outline: 'none', fontSize: 13 }}
                />
              </div>
            </div>
            <div style={{ overflowY: 'auto', padding: 6 }}>
              {error ? (
                <div className="of-status-danger" style={{ margin: 8, padding: '8px 12px', fontSize: 12 }}>
                  {error}
                </div>
              ) : null}
              {loading ? (
                <p className="of-text-muted" style={{ padding: 16, textAlign: 'center', margin: 0 }}>Loading...</p>
              ) : filtered.length === 0 ? (
                <p className="of-text-muted" style={{ padding: 16, textAlign: 'center', margin: 0 }}>No datasets found.</p>
              ) : (
                filtered.map((dataset) => {
                  const isSelected = selected.has(dataset.id);
                  const isActive = activeId === dataset.id;
                  return (
                    <div
                      key={dataset.id}
                      onClick={() => setActiveId(dataset.id)}
                      style={{
                        display: 'flex',
                        alignItems: 'center',
                        justifyContent: 'space-between',
                        padding: '8px 10px',
                        cursor: 'pointer',
                        borderRadius: 4,
                        background: isActive ? 'rgba(45, 114, 210, 0.06)' : 'transparent',
                      }}
                    >
                      <span style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
                        <Glyph name="database" size={14} tone="#2d72d2" />
                        <span style={{ fontSize: 13, color: 'var(--text-strong)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                          {dataset.name}
                        </span>
                      </span>
                      <button
                        type="button"
                        aria-label={isSelected ? 'Remove from selection' : 'Add to selection'}
                        onClick={(event) => {
                          event.stopPropagation();
                          toggleDataset(dataset);
                        }}
                        style={{
                          border: 0,
                          background: 'transparent',
                          padding: 4,
                          cursor: 'pointer',
                          color: isSelected ? 'var(--status-danger)' : 'var(--status-info)',
                        }}
                      >
                        <Glyph name={isSelected ? 'circle-x' : 'plus'} size={16} />
                      </button>
                    </div>
                  );
                })
              )}
            </div>
            <div style={{ padding: 8, borderTop: '1px solid var(--border-subtle)' }}>
              <button
                type="button"
                onClick={addAllVisible}
                disabled={filtered.length === 0}
                className="of-button"
                style={{ width: '100%', justifyContent: 'center' }}
              >
                <Glyph name="plus" size={13} />
                Add all to selection
              </button>
            </div>
          </aside>

          <main style={{ overflowY: 'auto', padding: 24, display: 'grid', placeContent: 'center' }}>
            {active ? (
              <div style={{ display: 'grid', gap: 14, justifyItems: 'start' }}>
                <h3 style={{ margin: 0, fontSize: 16, fontWeight: 600 }}>{active.name}</h3>
                {active.description ? (
                  <p className="of-text-muted" style={{ margin: 0, fontSize: 13 }}>{active.description}</p>
                ) : null}
                <dl style={{ display: 'grid', gridTemplateColumns: 'max-content 1fr', gap: '6px 14px', margin: 0, fontSize: 13 }}>
                  <dt className="of-text-muted">Format</dt><dd style={{ margin: 0 }}>{active.format}</dd>
                  <dt className="of-text-muted">Rows</dt><dd style={{ margin: 0 }}>{formatRowCount(active.row_count)}</dd>
                  <dt className="of-text-muted">Size</dt><dd style={{ margin: 0 }}>{formatBytes(active.size_bytes)}</dd>
                  <dt className="of-text-muted">Branch</dt><dd style={{ margin: 0, fontFamily: 'var(--font-mono)' }}>{active.active_branch}</dd>
                </dl>
              </div>
            ) : (
              <div style={{ display: 'grid', justifyItems: 'center', gap: 8, color: 'var(--text-muted)', textAlign: 'center' }}>
                <Glyph name="database" size={32} tone="#aab4c0" />
                <p style={{ margin: 0 }}>Select a dataset to view details</p>
              </div>
            )}
          </main>

          <aside style={{ borderLeft: '1px solid var(--border-subtle)', display: 'grid', gridTemplateRows: 'auto 1fr auto', minHeight: 0 }}>
            <div style={{ padding: 12, borderBottom: '1px solid var(--border-subtle)' }}>
              <p style={{ margin: 0, fontSize: 13, fontWeight: 600 }}>Datasets to add ({selected.size})</p>
            </div>
            <div style={{ overflowY: 'auto', padding: 8 }}>
              {selected.size === 0 ? (
                <p className="of-text-muted" style={{ padding: 16, textAlign: 'center', margin: 0 }}>No datasets selected</p>
              ) : (
                [...selected.values()].map((dataset) => (
                  <div
                    key={dataset.id}
                    style={{
                      display: 'flex',
                      alignItems: 'center',
                      justifyContent: 'space-between',
                      padding: '6px 10px',
                      borderRadius: 4,
                    }}
                  >
                    <span style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
                      <Glyph name="database" size={13} tone="#2d72d2" />
                      <span style={{ fontSize: 12, color: 'var(--text-strong)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                        {dataset.name}
                      </span>
                    </span>
                    <button
                      type="button"
                      aria-label="Remove"
                      onClick={() => toggleDataset(dataset)}
                      style={{ border: 0, background: 'transparent', padding: 4, cursor: 'pointer', color: 'var(--status-danger)' }}
                    >
                      <Glyph name="circle-x" size={14} />
                    </button>
                  </div>
                ))
              )}
            </div>
            <div style={{ padding: 12, borderTop: '1px solid var(--border-subtle)' }}>
              <button
                type="button"
                onClick={commit}
                disabled={selected.size === 0}
                style={{
                  width: '100%',
                  display: 'inline-flex',
                  alignItems: 'center',
                  justifyContent: 'center',
                  gap: 6,
                  padding: '10px 12px',
                  border: 0,
                  borderRadius: 4,
                  background: '#2d72d2',
                  color: '#fff',
                  fontSize: 13,
                  fontWeight: 600,
                  cursor: selected.size === 0 ? 'not-allowed' : 'pointer',
                  opacity: selected.size === 0 ? 0.6 : 1,
                }}
              >
                <Glyph name="database" size={13} tone="#fff" />
                Add data
              </button>
            </div>
          </aside>
        </div>
      </section>
    </div>
  );
}
