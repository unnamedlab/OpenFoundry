import type {
  AuditDeliveryDestination,
  AuditDeliveryDestinationType,
  AuditDeliveryFile,
} from '@/lib/api/audit';
import type { CSSProperties } from 'react';

export interface AuditDeliveryDraft {
  name: string;
  destination_type: AuditDeliveryDestinationType;
  organization_id: string;
  endpoint_url: string;
  dataset_rid: string;
  schema_version: string;
  metadata_text: string;
  start_time: string;
  end_time: string;
}

interface Props {
  destinations: AuditDeliveryDestination[];
  files: AuditDeliveryFile[];
  selectedDestinationId: string;
  draft: AuditDeliveryDraft;
  contentPreview: string;
  busy?: boolean;
  onDestinationChange: (id: string) => void;
  onDraftChange: (patch: Partial<AuditDeliveryDraft>) => void;
  onCreateDestination: () => void;
  onValidateDestination: () => void;
  onBackfillDestination: () => void;
  onLoadFiles: () => void;
  onPreviewFile: (id: string) => void;
}

const inputStyle: CSSProperties = {
  width: '100%',
};

export function AuditDeliveryPanel({
  destinations,
  files,
  selectedDestinationId,
  draft,
  contentPreview,
  busy = false,
  onDestinationChange,
  onDraftChange,
  onCreateDestination,
  onValidateDestination,
  onBackfillDestination,
  onLoadFiles,
  onPreviewFile,
}: Props) {
  const selected = destinations.find((item) => item.id === selectedDestinationId) ?? null;
  return (
    <section className="of-panel" style={{ padding: 20, display: 'grid', gap: 16 }}>
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'flex-start', gap: 12, flexWrap: 'wrap' }}>
        <div>
          <p className="of-eyebrow" style={{ color: '#0f766e' }}>
            Audit delivery
          </p>
          <h3 className="of-heading-md" style={{ marginTop: 6 }}>
            SIEM polling and governed dataset exports
          </h3>
        </div>
        <button type="button" className="of-button" disabled={busy} onClick={onLoadFiles}>
          Refresh files
        </button>
      </div>

      <div style={{ display: 'grid', gap: 12, gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))' }}>
        <label style={{ fontSize: 13 }}>
          <span style={{ display: 'block', marginBottom: 6, fontWeight: 500 }}>Destination</span>
          <select className="of-input" value={selectedDestinationId} onChange={(event) => onDestinationChange(event.target.value)}>
            <option value="">New destination</option>
            {destinations.map((dest) => (
              <option key={dest.id} value={dest.id}>
                {dest.name}
              </option>
            ))}
          </select>
        </label>
        <label style={{ fontSize: 13 }}>
          <span style={{ display: 'block', marginBottom: 6, fontWeight: 500 }}>Name</span>
          <input className="of-input" value={draft.name} onChange={(event) => onDraftChange({ name: event.target.value })} style={inputStyle} />
        </label>
        <label style={{ fontSize: 13 }}>
          <span style={{ display: 'block', marginBottom: 6, fontWeight: 500 }}>Type</span>
          <select
            className="of-input"
            value={draft.destination_type}
            onChange={(event) => onDraftChange({ destination_type: event.target.value as AuditDeliveryDestinationType })}
          >
            <option value="siem_api">SIEM API</option>
            <option value="openfoundry_dataset">OpenFoundry dataset</option>
          </select>
        </label>
        <label style={{ fontSize: 13 }}>
          <span style={{ display: 'block', marginBottom: 6, fontWeight: 500 }}>Organization ID</span>
          <input className="of-input" value={draft.organization_id} onChange={(event) => onDraftChange({ organization_id: event.target.value })} style={inputStyle} />
        </label>
        <label style={{ fontSize: 13 }}>
          <span style={{ display: 'block', marginBottom: 6, fontWeight: 500 }}>Schema</span>
          <input className="of-input" value={draft.schema_version} onChange={(event) => onDraftChange({ schema_version: event.target.value })} style={inputStyle} />
        </label>
        <label style={{ fontSize: 13 }}>
          <span style={{ display: 'block', marginBottom: 6, fontWeight: 500 }}>Endpoint URL</span>
          <input className="of-input" value={draft.endpoint_url} onChange={(event) => onDraftChange({ endpoint_url: event.target.value })} style={inputStyle} />
        </label>
        <label style={{ fontSize: 13 }}>
          <span style={{ display: 'block', marginBottom: 6, fontWeight: 500 }}>Dataset RID</span>
          <input className="of-input" value={draft.dataset_rid} onChange={(event) => onDraftChange({ dataset_rid: event.target.value })} style={inputStyle} />
        </label>
      </div>

      <label style={{ fontSize: 13 }}>
        <span style={{ display: 'block', marginBottom: 6, fontWeight: 500 }}>Metadata JSON</span>
        <textarea
          className="of-input"
          value={draft.metadata_text}
          onChange={(event) => onDraftChange({ metadata_text: event.target.value })}
          style={{ minHeight: 72, fontFamily: 'var(--font-mono)', fontSize: 12 }}
        />
      </label>

      <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
        <button type="button" className="of-button of-button--primary" disabled={busy} onClick={onCreateDestination}>
          Create destination
        </button>
        <button type="button" className="of-button" disabled={busy || !selectedDestinationId} onClick={onValidateDestination}>
          Validate
        </button>
      </div>

      {selected && (
        <div className="of-panel-muted" style={{ padding: 12, display: 'grid', gap: 6, fontSize: 13 }}>
          <div>
            {selected.validation_status} · {selected.validation_message || 'No validation yet'}
          </div>
          <div>
            backfill {selected.last_backfill_status}
            {selected.last_backfill_completed_at ? ` · ${new Date(selected.last_backfill_completed_at).toLocaleString()}` : ''}
          </div>
        </div>
      )}

      <div style={{ display: 'grid', gap: 12, gridTemplateColumns: 'repeat(auto-fit, minmax(180px, 1fr))' }}>
        <label style={{ fontSize: 13 }}>
          <span style={{ display: 'block', marginBottom: 6, fontWeight: 500 }}>Start</span>
          <input type="datetime-local" className="of-input" value={draft.start_time} onChange={(event) => onDraftChange({ start_time: event.target.value })} />
        </label>
        <label style={{ fontSize: 13 }}>
          <span style={{ display: 'block', marginBottom: 6, fontWeight: 500 }}>End</span>
          <input type="datetime-local" className="of-input" value={draft.end_time} onChange={(event) => onDraftChange({ end_time: event.target.value })} />
        </label>
      </div>
      <button type="button" className="of-button" disabled={busy || !selectedDestinationId} onClick={onBackfillDestination} style={{ width: 'fit-content' }}>
        Run backfill
      </button>

      <div style={{ display: 'grid', gap: 8 }}>
        {files.length === 0 ? (
          <p className="of-text-muted" style={{ fontSize: 13 }}>
            No delivery files match the current filters.
          </p>
        ) : (
          files.slice(0, 8).map((file) => (
            <div key={file.id} className="of-panel-muted" style={{ padding: 10, display: 'flex', justifyContent: 'space-between', gap: 12, alignItems: 'center' }}>
              <div style={{ minWidth: 0 }}>
                <p style={{ fontSize: 13, fontWeight: 600 }}>
                  {file.schema_version} · {file.event_count} events · {file.duplicate_count} duplicates
                </p>
                <p className="of-text-muted" style={{ fontSize: 12 }}>
                  {new Date(file.start_time).toLocaleString()} → {new Date(file.end_time).toLocaleString()}
                </p>
              </div>
              <button type="button" className="of-button of-button--ghost" disabled={busy} onClick={() => onPreviewFile(file.id)}>
                Preview
              </button>
            </div>
          ))
        )}
      </div>

      {contentPreview && (
        <pre className="of-panel-muted" style={{ margin: 0, padding: 12, maxHeight: 220, overflow: 'auto', fontSize: 11 }}>
          {contentPreview}
        </pre>
      )}
    </section>
  );
}
