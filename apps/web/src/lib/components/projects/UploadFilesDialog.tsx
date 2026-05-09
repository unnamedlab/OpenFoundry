import { useRef, useState, type DragEvent } from 'react';

import { createDataset, uploadData } from '@/lib/api/datasets';
import { bindProjectResource } from '@/lib/api/ontology';
import { Glyph } from '@/lib/components/ui/Glyph';

type UploadStrategy = 'individual-structured' | 'media-set' | 'unstructured-dataset' | 'individual-raw';

interface PendingFile {
  id: string;
  file: File;
}

interface UploadFilesDialogProps {
  open: boolean;
  projectId: string | null;
  onClose: () => void;
  onUploaded: () => void;
}

const STRATEGY_LABEL: Record<UploadStrategy, { title: string; description: string; enabled: boolean }> = {
  'individual-structured': {
    title: 'Upload as individual structured datasets (recommended)',
    description: 'Datasets are the most basic representation of tabular data. They can be used and transformed by many different applications.',
    enabled: true,
  },
  'media-set': {
    title: 'Upload to a new media set',
    description: 'Media sets enable media-specific capabilities for media files (e.g. audio, imagery, video, and documents).',
    enabled: false,
  },
  'unstructured-dataset': {
    title: 'Upload to a new unstructured dataset',
    description: 'Unstructured datasets can store arbitrary files for processing and analysis. Structured data can be extracted from unstructured datasets using Pipeline Builder or Transforms.',
    enabled: false,
  },
  'individual-raw': {
    title: 'Upload as individual raw files',
    description: 'Raw files cannot be used in data pipelines, analyses, or models.',
    enabled: false,
  },
};

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(2)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(2)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

function deriveFormat(filename: string): string {
  const ext = filename.toLowerCase().split('.').pop();
  if (!ext) return 'unknown';
  if (ext === 'parquet') return 'parquet';
  if (ext === 'avro') return 'avro';
  if (ext === 'csv' || ext === 'tsv') return 'csv';
  if (ext === 'json' || ext === 'ndjson' || ext === 'jsonl') return 'json';
  if (ext === 'txt' || ext === 'log') return 'text';
  return 'unknown';
}

function deriveDatasetName(filename: string): string {
  return filename.replace(/\.[^/.]+$/, '') || filename;
}

export function UploadFilesDialog({ open, projectId, onClose, onUploaded }: UploadFilesDialogProps) {
  const inputRef = useRef<HTMLInputElement>(null);
  const [files, setFiles] = useState<PendingFile[]>([]);
  const [strategy, setStrategy] = useState<UploadStrategy>('individual-structured');
  const [isDragging, setIsDragging] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [progress, setProgress] = useState<{ current: number; total: number } | null>(null);
  const [error, setError] = useState('');

  if (!open) return null;

  function handleFiles(list: FileList | null) {
    if (!list) return;
    const next = Array.from(list).map((file) => ({ id: `${file.name}-${file.size}-${file.lastModified}`, file }));
    setFiles((current) => {
      const seen = new Set(current.map((entry) => entry.id));
      return [...current, ...next.filter((entry) => !seen.has(entry.id))];
    });
  }

  function removeFile(id: string) {
    setFiles((current) => current.filter((entry) => entry.id !== id));
  }

  function handleDrop(event: DragEvent<HTMLDivElement>) {
    event.preventDefault();
    setIsDragging(false);
    handleFiles(event.dataTransfer.files);
  }

  async function handleUpload() {
    if (!projectId || files.length === 0) return;
    setUploading(true);
    setError('');
    setProgress({ current: 0, total: files.length });
    try {
      for (let i = 0; i < files.length; i += 1) {
        const entry = files[i];
        setProgress({ current: i, total: files.length });
        const dataset = await createDataset({
          name: deriveDatasetName(entry.file.name),
          format: deriveFormat(entry.file.name),
        });
        await uploadData(dataset.id, entry.file);
        try {
          await bindProjectResource(projectId, {
            resource_kind: 'dataset',
            resource_id: dataset.id,
          });
        } catch {
          // Bind failure is non-fatal — dataset still exists, just not bound to the project.
        }
      }
      setProgress({ current: files.length, total: files.length });
      setFiles([]);
      onUploaded();
      onClose();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Upload failed');
    } finally {
      setUploading(false);
      setProgress(null);
    }
  }

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="upload-files-title"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget && !uploading) onClose();
      }}
      style={{
        position: 'fixed',
        inset: 0,
        zIndex: 100,
        background: 'rgba(17, 24, 39, 0.42)',
        display: 'flex',
        alignItems: 'flex-start',
        justifyContent: 'center',
        padding: '64px 24px 24px',
      }}
    >
      <section
        style={{
          width: '100%',
          maxWidth: 540,
          background: '#fff',
          borderRadius: 6,
          boxShadow: '0 12px 32px rgba(15, 23, 42, 0.16)',
          display: 'grid',
          gridTemplateRows: 'auto 1fr auto',
          maxHeight: 'calc(100vh - 96px)',
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
            <h2 id="upload-files-title" style={{ margin: 0, fontSize: 15, fontWeight: 600, color: 'var(--text-strong)' }}>
              Upload files
            </h2>
          </div>
          <button
            type="button"
            onClick={onClose}
            disabled={uploading}
            aria-label="Close"
            style={{ border: 0, background: 'transparent', padding: 4, cursor: uploading ? 'not-allowed' : 'pointer', color: 'var(--text-muted)' }}
          >
            <Glyph name="x" size={14} />
          </button>
        </header>

        <div style={{ overflowY: 'auto', padding: 16, display: 'grid', gap: 12 }}>
          {error ? (
            <div role="alert" className="of-status-danger" style={{ padding: '8px 12px', fontSize: 12 }}>
              {error}
            </div>
          ) : null}

          <div
            onDragOver={(event) => {
              event.preventDefault();
              setIsDragging(true);
            }}
            onDragLeave={() => setIsDragging(false)}
            onDrop={handleDrop}
            style={{
              border: `1.5px dashed ${isDragging ? 'var(--status-info)' : '#c5cdd9'}`,
              borderRadius: 6,
              padding: '24px 16px',
              textAlign: 'center',
              background: isDragging ? 'rgba(45, 114, 210, 0.04)' : '#f4f6f9',
            }}
          >
            <Glyph name="database" size={28} tone="#7c8da3" />
            <p style={{ margin: '8px 0 0', fontSize: 13, color: 'var(--text-muted)' }}>
              Drop files here or{' '}
              <button
                type="button"
                onClick={() => inputRef.current?.click()}
                disabled={uploading}
                className="of-link"
                style={{
                  background: 'none',
                  border: 0,
                  padding: 0,
                  fontSize: 13,
                  color: 'var(--status-info)',
                  cursor: uploading ? 'not-allowed' : 'pointer',
                }}
              >
                choose from your computer
              </button>
            </p>
            <input
              ref={inputRef}
              type="file"
              multiple
              hidden
              onChange={(event) => handleFiles(event.target.files)}
            />
          </div>

          {files.length > 0 ? (
            <ul style={{ listStyle: 'none', padding: 0, margin: 0, display: 'grid', gap: 4 }}>
              {files.map((entry) => (
                <li
                  key={entry.id}
                  style={{
                    display: 'flex',
                    alignItems: 'center',
                    justifyContent: 'space-between',
                    padding: '8px 10px',
                    border: '1px solid var(--border-subtle)',
                    borderRadius: 4,
                    fontSize: 12,
                  }}
                >
                  <span style={{ display: 'flex', alignItems: 'center', gap: 8, minWidth: 0 }}>
                    <Glyph name="document" size={14} tone="#5c7080" />
                    <span style={{ color: 'var(--text-strong)', overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>
                      {entry.file.name}
                    </span>
                  </span>
                  <span style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                    <span style={{ color: 'var(--text-muted)' }}>{formatBytes(entry.file.size)}</span>
                    <button
                      type="button"
                      aria-label="Remove file"
                      onClick={() => removeFile(entry.id)}
                      disabled={uploading}
                      style={{
                        border: 0,
                        background: 'transparent',
                        cursor: uploading ? 'not-allowed' : 'pointer',
                        color: 'var(--text-muted)',
                        padding: 2,
                      }}
                    >
                      <Glyph name="x" size={12} />
                    </button>
                  </span>
                </li>
              ))}
            </ul>
          ) : null}

          <div style={{ display: 'grid', gap: 8 }}>
            {(['individual-structured', 'media-set', 'unstructured-dataset', 'individual-raw'] as UploadStrategy[]).map((id) => {
              const meta = STRATEGY_LABEL[id];
              const checked = strategy === id;
              return (
                <label
                  key={id}
                  style={{
                    display: 'flex',
                    alignItems: 'flex-start',
                    gap: 8,
                    fontSize: 12,
                    color: meta.enabled ? 'var(--text-strong)' : 'var(--text-muted)',
                    cursor: meta.enabled ? 'pointer' : 'not-allowed',
                  }}
                >
                  <input
                    type="radio"
                    name="upload-strategy"
                    checked={checked}
                    disabled={!meta.enabled || uploading}
                    onChange={() => meta.enabled && setStrategy(id)}
                    style={{ accentColor: '#2d72d2', marginTop: 3 }}
                  />
                  <span style={{ display: 'grid', gap: 2 }}>
                    <span style={{ fontWeight: 600 }}>{meta.title}</span>
                    <span style={{ fontSize: 11, color: 'var(--text-muted)' }}>{meta.description}</span>
                  </span>
                </label>
              );
            })}
          </div>
        </div>

        <footer
          style={{
            display: 'flex',
            justifyContent: 'space-between',
            alignItems: 'center',
            padding: '12px 16px',
            borderTop: '1px solid var(--border-subtle)',
          }}
        >
          <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
            {progress ? `Uploading ${progress.current}/${progress.total}...` : files.length > 0 ? `${files.length} file${files.length === 1 ? '' : 's'}` : ''}
          </span>
          <button
            type="button"
            onClick={() => void handleUpload()}
            disabled={uploading || files.length === 0 || !projectId}
            style={{
              padding: '8px 14px',
              border: 0,
              borderRadius: 4,
              background: '#2d72d2',
              color: '#fff',
              fontSize: 13,
              fontWeight: 600,
              cursor: uploading || files.length === 0 ? 'not-allowed' : 'pointer',
              opacity: uploading || files.length === 0 ? 0.7 : 1,
            }}
          >
            {uploading ? 'Uploading...' : 'Upload'}
          </button>
        </footer>
      </section>
    </div>
  );
}
