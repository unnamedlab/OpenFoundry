import { useState } from 'react';

import { Tabs } from '@/lib/components/Tabs';
import { NodeConfig } from '@/lib/components/pipeline/NodeConfig';
import { NodePreviewPanel } from '@/lib/components/pipeline/NodePreviewPanel';
import { Drawer } from '@/lib/components/ui/Drawer';
import type { NodeValidationReport, PipelineNode } from '@/lib/api/pipelines';

type InspectorTab = 'properties' | 'preview' | 'validation';

interface PipelineNodeInspectorDrawerProps {
  open: boolean;
  pipelineId: string;
  node: PipelineNode | null;
  nodes: PipelineNode[];
  validation?: NodeValidationReport | null;
  readOnly?: boolean;
  saving?: boolean;
  busy?: boolean;
  onClose: () => void;
  onChange: (next: PipelineNode) => void;
  onDelete: (nodeId: string) => void;
  onValidate?: () => void;
  onSave?: () => void;
}

function validationTone(validation: NodeValidationReport | null | undefined) {
  if (!validation) return { label: 'Not validated', background: '#1f2937', color: '#cbd5e1' };
  if (validation.status === 'VALID') return { label: 'Valid', background: '#064e3b', color: '#bbf7d0' };
  if (validation.status === 'PENDING') return { label: 'Pending', background: '#713f12', color: '#fde68a' };
  return { label: 'Invalid', background: '#7f1d1d', color: '#fecaca' };
}

export function PipelineNodeInspectorDrawer({
  open,
  pipelineId,
  node,
  nodes,
  validation = null,
  readOnly = false,
  saving = false,
  busy = false,
  onClose,
  onChange,
  onDelete,
  onValidate,
  onSave,
}: PipelineNodeInspectorDrawerProps) {
  const [tab, setTab] = useState<InspectorTab>('properties');
  const tone = validationTone(validation);

  return (
    <Drawer open={open} title={node ? `Node: ${node.label || node.id}` : 'Node inspector'} width="640px" onClose={onClose}>
      {!node ? (
        <p className="of-text-muted" style={{ fontSize: 13 }}>Select a node on the canvas.</p>
      ) : (
        <div style={{ minHeight: '100%', display: 'grid', gridTemplateRows: 'auto auto 1fr auto', gap: 12 }}>
          <header style={{ display: 'grid', gap: 8 }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 12 }}>
              <div style={{ minWidth: 0 }}>
                <p className="of-eyebrow" style={{ color: '#93c5fd' }}>Pipeline node</p>
                <h2 style={{ margin: '4px 0 0', fontSize: 20, lineHeight: 1.2, color: '#f8fafc', overflowWrap: 'anywhere' }}>
                  {node.label || node.id}
                </h2>
                <p style={{ margin: '6px 0 0', fontSize: 11, color: '#94a3b8', fontFamily: 'var(--font-mono)', overflowWrap: 'anywhere' }}>
                  {node.id}
                </p>
              </div>
              <span className="of-chip" style={{ background: tone.background, color: tone.color, borderColor: 'transparent' }}>
                {tone.label}
              </span>
            </div>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
              <span className="of-chip">{node.transform_type}</span>
              <span className="of-chip">{node.depends_on.length} dependencies</span>
              <span className="of-chip">{node.input_dataset_ids.length} inputs</span>
              {node.output_dataset_id && <span className="of-chip">Output set</span>}
            </div>
          </header>

          <Tabs
            tabs={[
              { id: 'properties', label: 'Properties' },
              { id: 'preview', label: 'Preview' },
              { id: 'validation', label: validation?.errors.length ? `Validation (${validation.errors.length})` : 'Validation' },
            ] as const}
            active={tab}
            onChange={setTab}
          />

          <div style={{ minHeight: 0 }}>
            {tab === 'properties' && (
              <NodeConfig
                node={node}
                siblings={nodes}
                readOnly={readOnly}
                validation={validation}
                onChange={onChange}
                onDelete={onDelete}
              />
            )}

            {tab === 'preview' && (
              <NodePreviewPanel pipelineId={pipelineId} node={node} onNodeChange={onChange} />
            )}

            {tab === 'validation' && (
              <section style={{ display: 'grid', gap: 10, padding: 12, background: '#0b1220', border: '1px solid #1f2937', borderRadius: 8 }}>
                <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8, alignItems: 'center' }}>
                  <h3 style={{ margin: 0, fontSize: 14, color: '#e2e8f0' }}>Validation report</h3>
                  <span className="of-chip" style={{ background: tone.background, color: tone.color, borderColor: 'transparent' }}>
                    {tone.label}
                  </span>
                </div>
                {!validation ? (
                  <p className="of-text-muted" style={{ margin: 0, fontSize: 12 }}>
                    No validation report yet. Run Validate from the builder or this drawer.
                  </p>
                ) : validation.errors.length === 0 ? (
                  <p style={{ margin: 0, fontSize: 12, color: '#bbf7d0' }}>
                    This node has no validation errors.
                  </p>
                ) : (
                  <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'grid', gap: 6 }}>
                    {validation.errors.map((err, index) => (
                      <li key={`${err.message}-${index}`} style={{ background: '#7f1d1d', color: '#fecaca', padding: '8px 10px', borderRadius: 6, fontSize: 12 }}>
                        {err.column && <code style={{ marginRight: 6 }}>{err.column}</code>}
                        {err.message}
                      </li>
                    ))}
                  </ul>
                )}
              </section>
            )}
          </div>

          <footer style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 8, paddingTop: 12, borderTop: '1px solid #1e293b' }}>
            <button type="button" onClick={onClose} className="of-button">
              Close
            </button>
            <div style={{ display: 'flex', justifyContent: 'flex-end', gap: 8 }}>
              {onValidate && (
                <button type="button" onClick={onValidate} disabled={busy} className="of-button">
                  Validate
                </button>
              )}
              {onSave && (
                <button type="button" onClick={onSave} disabled={saving} className="of-button of-button--primary">
                  {saving ? 'Saving...' : 'Save pipeline'}
                </button>
              )}
            </div>
          </footer>
        </div>
      )}
    </Drawer>
  );
}
