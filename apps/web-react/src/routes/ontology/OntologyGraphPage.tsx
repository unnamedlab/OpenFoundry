import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';

import { getOntologyGraph, listObjectTypes, type GraphResponse, type ObjectType } from '@/lib/api/ontology';

type Mode = 'schema' | 'object';

export function OntologyGraphPage() {
  const [types, setTypes] = useState<ObjectType[]>([]);
  const [graph, setGraph] = useState<GraphResponse | null>(null);
  const [mode, setMode] = useState<Mode>('schema');
  const [rootObjectId, setRootObjectId] = useState('');
  const [rootTypeId, setRootTypeId] = useState('');
  const [depth, setDepth] = useState(2);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    listObjectTypes({ per_page: 200 })
      .then((res) => setTypes(res.data))
      .catch(() => {});
  }, []);

  async function loadGraph() {
    setBusy(true);
    setError('');
    try {
      const res = await getOntologyGraph({
        root_object_id: mode === 'object' ? rootObjectId || undefined : undefined,
        root_type_id: mode === 'schema' ? rootTypeId || undefined : undefined,
        depth,
        limit: 60,
      });
      setGraph(res);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to load graph');
    } finally {
      setBusy(false);
    }
  }

  useEffect(() => {
    void loadGraph();
  }, []);

  return (
    <section className="of-page" style={{ padding: 24, display: 'grid', gap: 16 }}>
      <Link to="/ontology" style={{ color: 'var(--text-muted)', fontSize: 13 }}>← Ontology</Link>
      <header>
        <h1 className="of-heading-xl">Ontology graph</h1>
        <p className="of-text-muted" style={{ marginTop: 4, fontSize: 12 }}>
          Schema (object types + link types) or object-rooted graph view.
        </p>
      </header>

      {error && (
        <div className="of-status-danger" style={{ padding: '10px 14px', borderRadius: 'var(--radius-md)', fontSize: 13 }}>
          {error}
        </div>
      )}

      <section className="of-panel" style={{ padding: 16 }}>
        <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
          <select value={mode} onChange={(e) => setMode(e.target.value as Mode)} className="of-input">
            <option value="schema">schema</option>
            <option value="object">object</option>
          </select>
          {mode === 'schema' ? (
            <select value={rootTypeId} onChange={(e) => setRootTypeId(e.target.value)} className="of-input">
              <option value="">— all types —</option>
              {types.map((t) => (
                <option key={t.id} value={t.id}>{t.display_name}</option>
              ))}
            </select>
          ) : (
            <input value={rootObjectId} onChange={(e) => setRootObjectId(e.target.value)} placeholder="object id" className="of-input" />
          )}
          <input type="number" min={1} max={6} value={depth} onChange={(e) => setDepth(Number(e.target.value) || 2)} className="of-input" style={{ width: 80 }} />
          <button type="button" onClick={() => void loadGraph()} disabled={busy} className="of-button of-button--primary">Load</button>
        </div>
      </section>

      {graph && (
        <section className="of-panel" style={{ padding: 16 }}>
          <p className="of-eyebrow">{graph.nodes.length} nodes · {graph.edges.length} edges</p>
          <div style={{ display: 'grid', gap: 12, gridTemplateColumns: '1fr 1fr', marginTop: 8 }}>
            <div>
              <p className="of-eyebrow" style={{ fontSize: 11 }}>Nodes</p>
              <ul style={{ paddingLeft: 18, fontSize: 12 }}>
                {graph.nodes.map((n) => (
                  <li key={n.id}>
                    <strong>{n.label}</strong> · {n.kind}
                  </li>
                ))}
              </ul>
            </div>
            <div>
              <p className="of-eyebrow" style={{ fontSize: 11 }}>Edges</p>
              <ul style={{ paddingLeft: 18, fontSize: 12 }}>
                {graph.edges.map((e) => (
                  <li key={e.id}>
                    {e.source} → {e.target} · {e.label || e.kind}
                  </li>
                ))}
              </ul>
            </div>
          </div>
        </section>
      )}
    </section>
  );
}
