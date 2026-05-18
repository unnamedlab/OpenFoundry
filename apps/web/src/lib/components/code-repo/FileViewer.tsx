import { useEffect, useMemo, useState, type CSSProperties } from 'react';

import { MonacoEditor } from '@/lib/components/MonacoEditor';
import type { RepositoryFile, RepositoryFileAction, RepositoryFileMutation, SearchResult } from '@/lib/api/code-repos';

interface Props {
  files: RepositoryFile[];
  selectedFilePath: string;
  searchQuery: string;
  searchResults: SearchResult[];
  busy?: boolean;
  onSelectFile: (path: string) => void;
  onSearchQueryChange: (query: string) => void;
  onRunSearch: () => void;
  onSaveFile: (file: RepositoryFile, content: string) => Promise<void> | void;
  onFileAction: (action: RepositoryFileAction, path: string, nextPath?: string, content?: string) => Promise<void> | void;
  onPendingFileChanges: (changes: RepositoryFileMutation[]) => void;
}

interface FileNode {
  path: string;
  name: string;
  depth: number;
  kind: 'file' | 'folder';
  file?: RepositoryFile;
}

const actionButton: CSSProperties = {
  border: '1px solid var(--border-default)',
  borderRadius: 999,
  background: 'var(--bg-elevated)',
  padding: '6px 10px',
  fontSize: 12,
  cursor: 'pointer',
};

function buildFileTree(files: RepositoryFile[]) {
  const folders = new Set<string>();
  for (const file of files) {
    const parts = file.path.split('/');
    for (let i = 1; i < parts.length; i += 1) {
      folders.add(parts.slice(0, i).join('/'));
    }
  }

  const nodes: FileNode[] = [
    ...Array.from(folders).map((path) => ({
      path,
      name: path.split('/').at(-1) ?? path,
      depth: path.split('/').length - 1,
      kind: 'folder' as const,
    })),
    ...files.map((file) => ({
      path: file.path,
      name: file.path.split('/').at(-1) ?? file.path,
      depth: file.path.split('/').length - 1,
      kind: 'file' as const,
      file,
    })),
  ];
  return nodes.sort((a, b) => a.path.localeCompare(b.path) || (a.kind === 'folder' ? -1 : 1));
}

function fileForPath(files: RepositoryFile[], path: string) {
  return files.find((file) => file.path === path) ?? null;
}

export function FileViewer({
  files,
  selectedFilePath,
  searchQuery,
  searchResults,
  busy = false,
  onSelectFile,
  onSearchQueryChange,
  onRunSearch,
  onSaveFile,
  onFileAction,
  onPendingFileChanges,
}: Props) {
  const [openTabs, setOpenTabs] = useState<string[]>([]);
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [dirty, setDirty] = useState<Record<string, boolean>>({});
  const tree = useMemo(() => buildFileTree(files), [files]);
  const selectedFile = fileForPath(files, selectedFilePath) ?? files[0] ?? null;
  const activePath = selectedFile?.path ?? '';
  const editorValue = activePath ? drafts[activePath] ?? selectedFile?.content ?? '' : '';
  const hasDirtyFiles = Object.values(dirty).some(Boolean);

  useEffect(() => {
    if (!selectedFile) return;
    setOpenTabs((tabs) => (tabs.includes(selectedFile.path) ? tabs : [...tabs, selectedFile.path]));
    setDrafts((current) => ({ ...current, [selectedFile.path]: current[selectedFile.path] ?? selectedFile.content }));
  }, [selectedFile]);

  useEffect(() => {
    setDrafts((current) => {
      const next = { ...current };
      let changed = false;
      for (const file of files) {
        if (!dirty[file.path] && next[file.path] !== file.content) {
          next[file.path] = file.content;
          changed = true;
        }
      }
      return changed ? next : current;
    });
    setDirty((current) => {
      const next = { ...current };
      let changed = false;
      for (const file of files) {
        if (current[file.path] && drafts[file.path] === file.content) {
          next[file.path] = false;
          changed = true;
        }
      }
      return changed ? next : current;
    });
  }, [files, dirty, drafts]);

  useEffect(() => {
    const changes = files
      .filter((file) => dirty[file.path])
      .map((file) => ({
        action: 'save' as const,
        path: file.path,
        content: drafts[file.path] ?? file.content,
        branch_name: file.branch_name,
      }));
    onPendingFileChanges(changes);
  }, [dirty, drafts, files, onPendingFileChanges]);

  useEffect(() => {
    const handler = (event: BeforeUnloadEvent) => {
      if (!hasDirtyFiles) return;
      event.preventDefault();
      event.returnValue = '';
    };
    window.addEventListener('beforeunload', handler);
    return () => window.removeEventListener('beforeunload', handler);
  }, [hasDirtyFiles]);

  function selectPath(path: string) {
    if (path !== activePath && hasDirtyFiles && !window.confirm('You have unsaved changes. Switch files anyway?')) return;
    onSelectFile(path);
    setOpenTabs((tabs) => (tabs.includes(path) ? tabs : [...tabs, path]));
  }

  function closeTab(path: string) {
    if (dirty[path] && !window.confirm(`Discard unsaved changes in ${path}?`)) return;
    setOpenTabs((tabs) => tabs.filter((tab) => tab !== path));
    setDirty((current) => ({ ...current, [path]: false }));
    if (path === activePath) {
      const remaining = openTabs.filter((tab) => tab !== path);
      onSelectFile(remaining[0] ?? files[0]?.path ?? '');
    }
  }

  async function save(path = activePath) {
    const file = fileForPath(files, path);
    if (!file) return;
    await onSaveFile(file, drafts[path] ?? file.content);
    setDirty((current) => ({ ...current, [path]: false }));
  }

  async function runAction(action: RepositoryFileAction, path: string) {
    if (action === 'new') {
      const nextPath = window.prompt('New file path', 'src/new_file.py');
      if (!nextPath) return;
      await onFileAction('new', nextPath, nextPath, '');
      setOpenTabs((tabs) => (tabs.includes(nextPath) ? tabs : [...tabs, nextPath]));
      onSelectFile(nextPath);
      return;
    }
    if (action === 'rename' || action === 'move') {
      const nextPath = window.prompt(action === 'rename' ? 'Rename file to' : 'Move file to', path);
      if (!nextPath || nextPath === path) return;
      await onFileAction(action, path, nextPath);
      setOpenTabs((tabs) => tabs.map((tab) => (tab === path ? nextPath : tab)));
      onSelectFile(nextPath);
      return;
    }
    if (action === 'delete') {
      if (!window.confirm(`Delete ${path}?`)) return;
      await onFileAction('delete', path);
      setOpenTabs((tabs) => tabs.filter((tab) => tab !== path));
      onSelectFile(files.find((file) => file.path !== path)?.path ?? '');
    }
  }

  return (
    <section className="of-panel" style={{ padding: 20 }}>
      <div style={{ display: 'flex', flexWrap: 'wrap', alignItems: 'flex-start', justifyContent: 'space-between', gap: 12 }}>
        <div>
          <p className="of-eyebrow" style={{ color: '#b45309' }}>
            File Browser
          </p>
          <h3 className="of-heading-md" style={{ marginTop: 6 }}>
            Monaco file tree and multi-tab editor
          </h3>
          <p className="of-text-muted" style={{ marginTop: 4, fontSize: 13 }}>
            Right-click files for Git-backed actions. Edits save on blur and warn before navigation when dirty.
          </p>
        </div>
        <div style={{ display: 'flex', gap: 6, maxWidth: 420, width: '100%' }}>
          <input
            value={searchQuery}
            onChange={(e) => onSearchQueryChange(e.target.value)}
            placeholder="Search package, widget, connector…"
            className="of-input"
            style={{ borderRadius: 999 }}
          />
          <button type="button" onClick={onRunSearch} disabled={busy} className="of-button of-button--primary" style={{ background: '#f59e0b', color: '#0c0a09', borderRadius: 999 }}>
            Search
          </button>
          <button type="button" onClick={() => void runAction('new', activePath)} disabled={busy} style={actionButton}>
            New file
          </button>
        </div>
      </div>

      <div style={{ display: 'grid', gap: 16, gridTemplateColumns: 'minmax(220px, 0.7fr) minmax(0, 1.5fr)', marginTop: 18 }}>
        <div className="of-panel-muted" style={{ padding: 14, display: 'grid', alignContent: 'start', gap: 4 }}>
          {tree.map((node) => {
            const active = activePath === node.path;
            return (
              <button
                key={`${node.kind}:${node.path}`}
                type="button"
                disabled={node.kind === 'folder'}
                onClick={() => node.kind === 'file' && selectPath(node.path)}
                onContextMenu={(event) => {
                  event.preventDefault();
                  if (node.kind === 'file') {
                    const action = window.prompt('Action: rename, move, delete', 'rename');
                    if (action === 'rename' || action === 'move' || action === 'delete') void runAction(action, node.path);
                  }
                }}
                style={{
                  width: '100%',
                  textAlign: 'left',
                  padding: '8px 10px',
                  paddingLeft: 10 + node.depth * 14,
                  border: `1px solid ${active ? '#f59e0b' : 'transparent'}`,
                  background: active ? '#fffbeb' : node.kind === 'folder' ? 'transparent' : 'var(--bg-elevated)',
                  borderRadius: 12,
                  cursor: node.kind === 'file' ? 'pointer' : 'default',
                  opacity: node.kind === 'folder' ? 0.76 : 1,
                }}
              >
                <p style={{ fontWeight: node.kind === 'folder' ? 700 : 500, color: 'var(--text-strong)' }}>
                  {node.kind === 'folder' ? '▸ ' : '• '} {node.name}
                  {dirty[node.path] ? ' *' : ''}
                </p>
                {node.file && (
                  <p className="of-text-muted" style={{ marginTop: 2, fontSize: 11 }}>
                    {node.file.language} · {node.file.size_bytes} bytes
                  </p>
                )}
              </button>
            );
          })}
          {files.length === 0 && <p className="of-text-muted" style={{ padding: 12, fontSize: 13 }}>No files yet. Create a file to seed the repository.</p>}
        </div>

        <div style={{ display: 'grid', gap: 12, minWidth: 0 }}>
          <div className="of-panel-muted" style={{ display: 'flex', gap: 6, overflowX: 'auto', padding: 8 }}>
            {openTabs.map((path) => (
              <button key={path} type="button" onClick={() => selectPath(path)} style={{ ...actionButton, background: path === activePath ? '#fffbeb' : 'var(--bg-elevated)' }}>
                {path.split('/').at(-1)}{dirty[path] ? ' *' : ''} <span onClick={(event) => { event.stopPropagation(); closeTab(path); }}>×</span>
              </button>
            ))}
          </div>

          <div style={{ borderRadius: 16, padding: 14, background: '#0c0a09', color: '#f5f5f4' }}>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8 }}>
              <div>
                <p style={{ fontWeight: 600 }}>{activePath || 'No file selected'}</p>
                <p style={{ marginTop: 4, fontSize: 11, color: '#a8a29e' }}>
                  {selectedFile?.language ?? 'n/a'} · commit {selectedFile?.last_commit_sha?.slice(0, 12) ?? 'n/a'}
                </p>
              </div>
              <div style={{ display: 'flex', gap: 6 }}>
                <button type="button" disabled={!selectedFile || busy} onClick={() => void save()} style={actionButton}>Save</button>
                <button type="button" disabled={!selectedFile || busy} onClick={() => void runAction('rename', activePath)} style={actionButton}>Rename</button>
                <button type="button" disabled={!selectedFile || busy} onClick={() => void runAction('delete', activePath)} style={actionButton}>Delete</button>
              </div>
            </div>
            {selectedFile ? (
              <div style={{ marginTop: 14, overflow: 'hidden', borderRadius: 16, border: '1px solid #44403c', background: '#1c1917' }}>
                <MonacoEditor
                  value={editorValue}
                  language={selectedFile.language}
                  minHeight={420}
                  onChange={(value) => {
                    setDrafts((current) => ({ ...current, [selectedFile.path]: value }));
                    setDirty((current) => ({ ...current, [selectedFile.path]: value !== selectedFile.content }));
                  }}
                  onBlur={(value) => {
                    setDrafts((current) => ({ ...current, [selectedFile.path]: value }));
                    setDirty((current) => ({ ...current, [selectedFile.path]: value !== selectedFile.content }));
                  }}
                />
              </div>
            ) : (
              <p className="of-text-muted" style={{ marginTop: 14 }}>Select or create a file to start editing.</p>
            )}
          </div>

          <div className="of-panel" style={{ padding: 14 }}>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8 }}>
              <p style={{ fontWeight: 600, color: 'var(--text-strong)' }}>Search results</p>
              <p className="of-eyebrow">{searchResults.length} matches</p>
            </div>
            <div style={{ display: 'grid', gap: 8, marginTop: 10 }}>
              {searchResults.map((result, index) => (
                <button key={`${result.path}-${index}`} type="button" onClick={() => selectPath(result.path)} className="of-panel-muted" style={{ padding: 12, textAlign: 'left', border: '1px solid var(--border-default)', borderRadius: 14 }}>
                  <p style={{ fontWeight: 500, color: 'var(--text-strong)' }}>{result.path}</p>
                  <p className="of-text-muted" style={{ marginTop: 6, fontSize: 13 }}>{result.snippet}</p>
                </button>
              ))}
              {searchResults.length === 0 && <p className="of-text-muted" style={{ border: '1px dashed var(--border-default)', borderRadius: 16, padding: 18, fontSize: 13, textAlign: 'center' }}>Run a query to surface indexed snippets across repository files.</p>}
            </div>
          </div>
        </div>
      </div>
    </section>
  );
}
