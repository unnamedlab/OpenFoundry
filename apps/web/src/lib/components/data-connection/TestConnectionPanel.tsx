import type { ReactNode } from 'react';

import type { TestConnectionResult } from '@/lib/api/data-connection';

interface TestConnectionPanelProps {
  sourceId: string | null;
  result: TestConnectionResult | null;
  busy: boolean;
  onTest: () => void;
  children?: ReactNode;
}

export function TestConnectionPanel({ sourceId, result, busy, onTest, children }: TestConnectionPanelProps) {
  return (
    <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'flex-start' }}>
        <div>
          <p className="of-eyebrow">Test connection</p>
          <p className="of-text-muted" style={{ margin: '4px 0 0', fontSize: 12 }}>
            Validate the saved source before discovering tables or files.
          </p>
        </div>
        <button type="button" onClick={onTest} disabled={!sourceId || busy} className="of-button">
          Test connection
        </button>
      </div>

      {result ? (
        <div
          className={result.success ? 'of-status-success' : 'of-status-danger'}
          style={{ padding: '10px 12px', borderRadius: 'var(--radius-md)', fontSize: 12, display: 'grid', gap: 6 }}
        >
          <div>
            <strong>{result.success ? 'Connected' : 'Failed'}</strong>
            <span> - {result.message}</span>
            {result.latency_ms !== null ? <span> - {result.latency_ms}ms</span> : null}
            {result.tested_at ? <span> - {result.tested_at}</span> : null}
          </div>
          {(result.checks ?? []).length > 0 ? (
            <ul style={{ margin: 0, paddingLeft: 18 }}>
              {(result.checks ?? []).map((check) => (
                <li key={check.name}>
                  {check.status} - {check.name}: {check.message}
                  {check.latency_ms !== null ? ` - ${check.latency_ms}ms` : ''}
                </li>
              ))}
            </ul>
          ) : null}
        </div>
      ) : (
        <p className="of-text-muted" style={{ margin: 0, fontSize: 12 }}>
          Create the source, then run the first test from here.
        </p>
      )}

      {children}
    </section>
  );
}
