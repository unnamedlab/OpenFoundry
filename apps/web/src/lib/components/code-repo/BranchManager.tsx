import type { BranchDefinition, RepositoryTagDefinition } from '@/lib/api/code-repos';

export interface BranchDraft {
  name: string;
  base_branch: string;
  protected: boolean;
}

interface Props {
  branches: BranchDefinition[];
  draft: BranchDraft;
  busy?: boolean;
  onDraftChange: (patch: Partial<BranchDraft>) => void;
  tags?: RepositoryTagDefinition[];
  onCreateBranch: () => void;
  onSwitchBranch?: (branch: string) => void;
  onDeleteBranch?: (branch: string) => void;
  onMergeBranch?: (branch: string, target: string) => void;
  onCreateTag?: (name: string, target: string, message: string, protectedTag: boolean) => void;
}

const darkInput: React.CSSProperties = {
  width: '100%',
  borderRadius: 16,
  border: '1px solid #44403c',
  background: '#1c1917',
  padding: '10px 14px',
  color: '#f5f5f4',
  fontSize: 13,
  outline: 'none',
};

export function BranchManager({ branches, draft, tags = [], busy = false, onDraftChange, onCreateBranch, onSwitchBranch, onDeleteBranch, onMergeBranch, onCreateTag }: Props) {
  return (
    <section className="of-panel" style={{ padding: 20 }}>
      <div>
        <p className="of-eyebrow" style={{ color: '#047857' }}>
          Branches
        </p>
        <h3 className="of-heading-md" style={{ marginTop: 6 }}>
          Protected bases and feature streams
        </h3>
        <p className="of-text-muted" style={{ marginTop: 4, fontSize: 13 }}>
          Create branches off the default base and watch review pressure accumulate branch by branch.
        </p>
      </div>

      <div style={{ display: 'grid', gap: 16, gridTemplateColumns: 'minmax(0, 0.92fr) minmax(0, 1.08fr)', marginTop: 18 }}>
        <div style={{ display: 'grid', gap: 8 }}>
          {branches.map((branch) => (
            <div key={branch.name} className="of-panel-muted" style={{ padding: 14 }}>
              <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 8 }}>
                <div>
                  <p style={{ fontWeight: 600, color: 'var(--text-strong)' }}>{branch.name}</p>
                  <p className="of-text-muted" style={{ fontSize: 13 }}>
                    Head {branch.head_sha} · base {branch.base_branch ?? 'none'}
                  </p>
                </div>
                <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6, justifyContent: 'flex-end' }}>
                  <span
                    className="of-chip"
                    style={
                      branch.is_default
                        ? { background: '#0c0a09', color: '#f5f5f4', textTransform: 'uppercase', letterSpacing: '0.16em' }
                        : { background: '#ecfdf5', color: '#047857', textTransform: 'uppercase', letterSpacing: '0.16em' }
                    }
                  >
                    {branch.is_default ? 'Default' : 'Feature'}
                  </span>
                  <button type="button" disabled={busy} onClick={() => onSwitchBranch?.(branch.name)} className="of-chip">Switch</button>
                  <button type="button" disabled={busy} onClick={() => onMergeBranch?.(branch.name, draft.base_branch || 'main')} className="of-chip">Merge</button>
                  <button type="button" disabled={busy || branch.is_default} onClick={() => onDeleteBranch?.(branch.name)} className="of-chip">Delete</button>
                </div>
              </div>
              <div style={{ display: 'flex', flexWrap: 'wrap', gap: 4, marginTop: 8 }}>
                <span className="of-chip">Ahead {branch.ahead_by}</span>
                <span className="of-chip">Pending reviews {branch.pending_reviews}</span>
                <span className="of-chip">{branch.protected ? 'Protected' : 'Writable'}</span>
              </div>
            </div>
          ))}
        </div>

        <div style={{ borderRadius: 16, padding: 14, background: '#0c0a09', color: '#f5f5f4' }}>
          <p style={{ fontSize: 11, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.18em', color: '#6ee7b7' }}>
            Create branch
          </p>
          <div style={{ display: 'grid', gap: 12, gridTemplateColumns: '1fr 1fr', marginTop: 14 }}>
            <label style={{ fontSize: 13, gridColumn: 'span 2' }}>
              <span style={{ display: 'block', marginBottom: 6, fontWeight: 500 }}>Branch name</span>
              <input value={draft.name} onChange={(e) => onDraftChange({ name: e.target.value })} style={darkInput} />
            </label>
            <label style={{ fontSize: 13 }}>
              <span style={{ display: 'block', marginBottom: 6, fontWeight: 500 }}>Base branch</span>
              <input value={draft.base_branch} onChange={(e) => onDraftChange({ base_branch: e.target.value })} style={darkInput} />
            </label>
            <label
              style={{
                display: 'inline-flex',
                alignItems: 'center',
                gap: 8,
                padding: '10px 14px',
                borderRadius: 16,
                border: '1px solid #44403c',
                background: '#1c1917',
                fontSize: 13,
              }}
            >
              <input type="checkbox" checked={draft.protected} onChange={(e) => onDraftChange({ protected: e.target.checked })} />
              Protected branch
            </label>
          </div>


          <div style={{ marginTop: 18, borderTop: '1px solid #44403c', paddingTop: 14 }}>
            <p style={{ fontSize: 11, fontWeight: 600, textTransform: 'uppercase', letterSpacing: '0.18em', color: '#fbbf24' }}>
              Annotated release tags
            </p>
            <div style={{ display: 'grid', gap: 8, marginTop: 10 }}>
              {tags.map((tag) => (
                <div key={tag.name} style={{ border: '1px solid #44403c', borderRadius: 12, padding: 10 }}>
                  <p style={{ fontWeight: 600 }}>{tag.name} {tag.protected ? '🔒' : ''}</p>
                  <p style={{ color: '#a8a29e', fontSize: 11 }}>{tag.target_sha.slice(0, 12)} · {tag.message || 'release'}</p>
                </div>
              ))}
            </div>
            <button
              type="button"
              disabled={busy}
              onClick={() => {
                const name = window.prompt('Tag name', 'v0.1.0');
                if (!name) return;
                const target = window.prompt('Target branch or SHA', draft.base_branch || 'main') || draft.base_branch || 'main';
                const message = window.prompt('Tag message', `Release ${name}`) || `Release ${name}`;
                const protectedTag = window.confirm('Protect this release tag?');
                onCreateTag?.(name, target, message, protectedTag);
              }}
              className="of-button of-button--secondary"
              style={{ marginTop: 10 }}
            >
              Create annotated tag
            </button>
          </div>
          <button
            type="button"
            onClick={onCreateBranch}
            disabled={busy}
            className="of-button of-button--primary"
            style={{ marginTop: 14, background: '#34d399', color: '#0c0a09' }}
          >
            Create branch
          </button>
        </div>
      </div>
    </section>
  );
}
