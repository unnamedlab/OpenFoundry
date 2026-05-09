import { useEffect, useState } from 'react';

import { triggerRun } from '@/lib/api/pipelines';
import { Glyph } from '@/lib/components/ui/Glyph';

interface OutputOption {
  id: string;
  label: string;
}

interface DeployDrawerProps {
  open: boolean;
  pipelineId: string | null;
  outputs: OutputOption[];
  lastDeploymentLabel?: string;
  onClose: () => void;
  onDeployed?: (runId: string) => void;
}

export function DeployDrawer({ open, pipelineId, outputs, lastDeploymentLabel, onClose, onDeployed }: DeployDrawerProps) {
  const [tab, setTab] = useState<'settings' | 'errors'>('settings');
  const [selected, setSelected] = useState<Set<string>>(new Set());
  const [deploying, setDeploying] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    if (!open) return;
    setSelected(new Set(outputs.map((output) => output.id)));
    setTab('settings');
    setError('');
  }, [open, outputs]);

  if (!open) return null;

  function toggle(id: string) {
    setSelected((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  }

  function toggleAll() {
    if (selected.size === outputs.length) setSelected(new Set());
    else setSelected(new Set(outputs.map((output) => output.id)));
  }

  async function deploy() {
    if (!pipelineId || selected.size === 0) return;
    setDeploying(true);
    setError('');
    try {
      const response = await triggerRun(pipelineId);
      onDeployed?.(response.id);
      onClose();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Deploy failed');
    } finally {
      setDeploying(false);
    }
  }

  const allSelected = selected.size === outputs.length && outputs.length > 0;

  return (
    <aside
      style={{
        position: 'fixed',
        top: 0,
        right: 0,
        bottom: 0,
        width: 360,
        zIndex: 80,
        background: '#fff',
        borderLeft: '1px solid var(--border-default)',
        boxShadow: '-12px 0 32px rgba(15, 23, 42, 0.08)',
        display: 'grid',
        gridTemplateRows: 'auto 1fr auto',
      }}
    >
      <header style={{ padding: '14px 16px', borderBottom: '1px solid var(--border-subtle)', display: 'grid', gap: 4 }}>
        <p style={{ margin: 0, fontSize: 14, fontWeight: 600, color: 'var(--text-strong)' }}>Deploy this pipeline</p>
        <p className="of-text-muted" style={{ margin: 0, fontSize: 12 }}>Update pipeline logic and build target outputs.</p>
      </header>

      <div style={{ overflowY: 'auto', padding: 16, display: 'grid', gap: 16 }}>
        <section style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
          <span style={{ fontSize: 13, color: 'var(--text-strong)' }}>Last deployment</span>
          <span className="of-chip" style={{ fontSize: 12 }}>{lastDeploymentLabel ?? 'None'}</span>
        </section>

        <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 0, borderBottom: '1px solid var(--border-subtle)' }}>
          {(['settings', 'errors'] as const).map((value) => {
            const active = tab === value;
            return (
              <button
                key={value}
                type="button"
                onClick={() => setTab(value)}
                style={{
                  border: 0,
                  background: 'transparent',
                  padding: '8px 4px',
                  cursor: 'pointer',
                  fontSize: 13,
                  fontWeight: active ? 600 : 500,
                  color: active ? 'var(--text-strong)' : 'var(--text-muted)',
                  borderBottom: active ? '2px solid var(--status-info)' : '2px solid transparent',
                }}
              >
                {value === 'settings' ? 'Deploy settings' : 'Errors'}{' '}
                {value === 'errors' ? <span className="of-chip" style={{ marginLeft: 4 }}>0</span> : null}
              </button>
            );
          })}
        </div>

        {tab === 'settings' ? (
          <>
            <section style={{ display: 'grid', gap: 8 }}>
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between' }}>
                <p style={{ margin: 0, fontSize: 13, fontWeight: 600 }}>Compute profile</p>
                <button type="button" className="of-button" disabled style={{ fontSize: 12 }}>Configure</button>
              </div>
              <div
                style={{
                  display: 'flex',
                  alignItems: 'center',
                  justifyContent: 'space-between',
                  padding: '10px 12px',
                  background: '#f4f6f9',
                  border: '1px solid var(--border-subtle)',
                  borderRadius: 4,
                  fontSize: 13,
                }}
              >
                <span>Default</span>
                <span className="of-chip" style={{ fontSize: 11 }}>Extra small - warm pool</span>
              </div>
            </section>

            <section style={{ display: 'grid', gap: 8 }}>
              <p style={{ margin: 0, fontSize: 13, fontWeight: 600 }}>Select outputs to build</p>
              <p className="of-text-muted" style={{ margin: 0, fontSize: 12 }}>
                Deploying the pipeline will update logic for all outputs, but only the selected outputs will be built.
              </p>
              <div style={{ border: '1px solid var(--border-subtle)', borderRadius: 4, padding: 8 }}>
                <label style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '4px 6px', fontSize: 13, fontWeight: 600 }}>
                  <Glyph name="chevron-down" size={11} />
                  <input
                    type="checkbox"
                    checked={allSelected}
                    onChange={toggleAll}
                    style={{ accentColor: 'var(--status-info)' }}
                  />
                  All
                </label>
                <div style={{ paddingLeft: 18 }}>
                  <label style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '4px 6px', fontSize: 13, fontWeight: 500 }}>
                    <Glyph name="chevron-down" size={11} />
                    <input
                      type="checkbox"
                      checked={allSelected}
                      onChange={toggleAll}
                      style={{ accentColor: 'var(--status-info)' }}
                    />
                    Single jobs
                  </label>
                  <div style={{ paddingLeft: 18 }}>
                    {outputs.length === 0 ? (
                      <p className="of-text-muted" style={{ margin: '6px 0 0', fontSize: 12 }}>No outputs yet. Add one with “+ Add output”.</p>
                    ) : (
                      outputs.map((output) => (
                        <label key={output.id} style={{ display: 'flex', alignItems: 'center', gap: 8, padding: '4px 6px', fontSize: 13 }}>
                          <input
                            type="checkbox"
                            checked={selected.has(output.id)}
                            onChange={() => toggle(output.id)}
                            style={{ accentColor: 'var(--status-info)' }}
                          />
                          <Glyph name="database" size={13} tone="#2d72d2" />
                          <span style={{ overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                            {output.label}
                          </span>
                        </label>
                      ))
                    )}
                  </div>
                </div>
              </div>
            </section>

            {error ? (
              <div className="of-status-danger" style={{ padding: '8px 12px', borderRadius: 4, fontSize: 12 }}>
                {error}
              </div>
            ) : null}
          </>
        ) : (
          <p className="of-text-muted" style={{ fontSize: 13, margin: 0 }}>No errors.</p>
        )}
      </div>

      <footer
        style={{
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'space-between',
          gap: 8,
          padding: 12,
          borderTop: '1px solid var(--border-subtle)',
        }}
      >
        <button type="button" className="of-button" disabled style={{ fontSize: 12 }}>
          <Glyph name="list" size={12} /> View changes
        </button>
        <button
          type="button"
          onClick={() => void deploy()}
          disabled={!pipelineId || selected.size === 0 || deploying}
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: 6,
            padding: '8px 14px',
            border: 0,
            borderRadius: 4,
            background: '#15803d',
            color: '#fff',
            fontSize: 13,
            fontWeight: 600,
            cursor: !pipelineId || selected.size === 0 || deploying ? 'not-allowed' : 'pointer',
            opacity: !pipelineId || selected.size === 0 || deploying ? 0.6 : 1,
          }}
        >
          <RocketGlyph />
          {deploying ? 'Deploying...' : 'Deploy pipeline'}
        </button>
      </footer>
    </aside>
  );
}

function RocketGlyph() {
  return (
    <svg width={14} height={14} viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path d="M5 19l3-3m11-13c-4 1-8 4-10 7l4 4c3-2 6-6 7-10zM5 14c-1 1-1 4-1 5 1 0 4 0 5-1" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
      <circle cx="14" cy="9" r="1.4" fill="currentColor" />
    </svg>
  );
}
