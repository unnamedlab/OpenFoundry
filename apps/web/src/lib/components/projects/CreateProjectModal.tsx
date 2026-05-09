import { useEffect, useMemo, useState, type FormEvent } from 'react';

import {
  createProject,
  listProjectTemplates,
  type OntologyProject,
  type ProjectTemplate,
} from '@/lib/api/ontology';
import { listSpaces, type NexusSpace } from '@/lib/api/nexus';
import { useCurrentUser } from '@/lib/stores/auth';

interface CreateProjectModalProps {
  open: boolean;
  onClose: () => void;
  onCreated: (project: OntologyProject) => void;
}

type Step = 'template' | 'form';
type DefaultRole = 'editor' | 'viewer';

const ROLE_LABEL: Record<DefaultRole, string> = {
  editor: 'Editor',
  viewer: 'Viewer',
};

function deriveSlug(value: string) {
  const slug = value
    .trim()
    .toLowerCase()
    .replace(/[^a-z0-9\-_\s]/g, '')
    .replace(/\s+/g, '-')
    .replace(/-+/g, '-')
    .replace(/^-+|-+$/g, '');
  return slug || `project-${Date.now().toString(36)}`;
}

function CloseGlyph() {
  return (
    <svg width={14} height={14} viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path d="M6 6l12 12M6 18L18 6" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" />
    </svg>
  );
}

function FolderTemplateGlyph() {
  return (
    <svg width={20} height={20} viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path
        d="M3.5 7.5h6l2 2h9v9a1.5 1.5 0 0 1-1.5 1.5H5A1.5 1.5 0 0 1 3.5 18.5z"
        stroke="#5f6b7a"
        strokeWidth="1.5"
        strokeLinejoin="round"
      />
      <path d="M9 14h6M9 17h4" stroke="#5f6b7a" strokeWidth="1.4" strokeLinecap="round" />
    </svg>
  );
}

function ArrowRightGlyph() {
  return (
    <svg width={18} height={18} viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path d="M5 12h14M13 6l6 6-6 6" stroke="currentColor" strokeWidth="1.6" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

function CaretDownGlyph({ open }: { open: boolean }) {
  return (
    <svg
      width={12}
      height={12}
      viewBox="0 0 24 24"
      fill="none"
      aria-hidden="true"
      style={{ transform: open ? 'rotate(180deg)' : undefined, transition: 'transform 120ms' }}
    >
      <path d="M6 9l6 6 6-6" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

function CheckGlyph() {
  return (
    <svg width={14} height={14} viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path d="M5 12.5l4 4 10-10" stroke="#fff" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

function SpaceGlyph() {
  return (
    <svg width={18} height={18} viewBox="0 0 24 24" fill="none" aria-hidden="true">
      <path d="M3.5 7.5h6l2 2h9v9a1.5 1.5 0 0 1-1.5 1.5H5A1.5 1.5 0 0 1 3.5 18.5z" stroke="#5f6b7a" strokeWidth="1.5" strokeLinejoin="round" />
      <path d="M11 14a3 3 0 1 0 0-3" stroke="#7c5dd6" strokeWidth="1.5" strokeLinecap="round" />
    </svg>
  );
}

export function CreateProjectModal({ open, onClose, onCreated }: CreateProjectModalProps) {
  const user = useCurrentUser();

  const [step, setStep] = useState<Step>('template');
  const [spaces, setSpaces] = useState<NexusSpace[]>([]);
  const [spaceId, setSpaceId] = useState<string>('');
  const [templates, setTemplates] = useState<ProjectTemplate[]>([]);
  const [template, setTemplate] = useState<ProjectTemplate | null>(null);
  const [loadingDirectory, setLoadingDirectory] = useState(false);
  const [directoryError, setDirectoryError] = useState('');

  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [advancedOpen, setAdvancedOpen] = useState(false);
  const [defaultRole, setDefaultRole] = useState<DefaultRole>('editor');
  const [submitting, setSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState('');

  const selectedSpace = useMemo(() => spaces.find((s) => s.id === spaceId) ?? null, [spaces, spaceId]);

  useEffect(() => {
    if (!open) return;
    setStep('template');
    setName(user ? `Learning ${user.name || user.email.split('@')[0]}` : '');
    setDescription('Personal Learning Project for OpenFoundry courses.');
    setAdvancedOpen(false);
    setDefaultRole('editor');
    setTemplate(null);
    setSubmitError('');
    setSubmitting(false);

    let cancelled = false;
    setLoadingDirectory(true);
    setDirectoryError('');
    Promise.all([listSpaces().catch(() => ({ items: [] as NexusSpace[] })), listProjectTemplates()])
      .then(([spaceResp, templateResp]) => {
        if (cancelled) return;
        const spaceList = spaceResp.items;
        setSpaces(spaceList);
        if (spaceList.length > 0) setSpaceId(spaceList[0].id);
        setTemplates(templateResp);
      })
      .catch((cause) => {
        if (cancelled) return;
        setDirectoryError(cause instanceof Error ? cause.message : 'Failed to load spaces or templates.');
      })
      .finally(() => {
        if (!cancelled) setLoadingDirectory(false);
      });

    return () => {
      cancelled = true;
    };
  }, [open, user]);

  if (!open) return null;

  function pickTemplate(next: ProjectTemplate) {
    setTemplate(next);
    setStep('form');
  }

  async function handleSubmit(event: FormEvent) {
    event.preventDefault();
    if (!template) return;
    if (!name.trim()) {
      setSubmitError('Project name is required.');
      return;
    }
    setSubmitting(true);
    setSubmitError('');
    try {
      const project = await createProject({
        slug: deriveSlug(name),
        display_name: name.trim(),
        description: description.trim() || undefined,
        workspace_slug: selectedSpace?.slug,
      });
      onCreated(project);
    } catch (cause) {
      setSubmitError(cause instanceof Error ? cause.message : 'Failed to create project.');
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="create-project-title"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) onClose();
      }}
      style={{
        position: 'fixed',
        inset: 0,
        zIndex: 90,
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
          maxWidth: 560,
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
            padding: '14px 18px',
            borderBottom: '1px solid var(--border-subtle)',
          }}
        >
          <div style={{ display: 'flex', alignItems: 'center', gap: 10 }}>
            <SpaceGlyph />
            <h2 id="create-project-title" style={{ margin: 0, fontSize: 16, fontWeight: 600, color: 'var(--text-strong)' }}>
              Create new project
            </h2>
          </div>
          <button
            type="button"
            onClick={onClose}
            aria-label="Close"
            style={{
              border: 0,
              background: 'transparent',
              padding: 4,
              cursor: 'pointer',
              color: 'var(--text-muted)',
              display: 'inline-flex',
            }}
          >
            <CloseGlyph />
          </button>
        </header>

        {step === 'template' ? (
          <TemplateStep
            spaces={spaces}
            spaceId={spaceId}
            onSpaceChange={setSpaceId}
            templates={templates}
            loading={loadingDirectory}
            error={directoryError}
            onPickTemplate={pickTemplate}
          />
        ) : (
          <FormStep
            name={name}
            description={description}
            onNameChange={setName}
            onDescriptionChange={setDescription}
            advancedOpen={advancedOpen}
            onToggleAdvanced={() => setAdvancedOpen((open) => !open)}
            defaultRole={defaultRole}
            onDefaultRoleChange={setDefaultRole}
            spaceLabel={selectedSpace?.display_name || selectedSpace?.slug || 'Default'}
            submitting={submitting}
            error={submitError}
            onSubmit={handleSubmit}
            onBack={() => setStep('template')}
          />
        )}
      </section>
    </div>
  );
}

function TemplateStep({
  spaces,
  spaceId,
  onSpaceChange,
  templates,
  loading,
  error,
  onPickTemplate,
}: {
  spaces: NexusSpace[];
  spaceId: string;
  onSpaceChange: (id: string) => void;
  templates: ProjectTemplate[];
  loading: boolean;
  error: string;
  onPickTemplate: (template: ProjectTemplate) => void;
}) {
  return (
    <>
      <div style={{ overflowY: 'auto' }}>
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: '60px 1fr',
            alignItems: 'center',
            gap: 12,
            padding: '14px 18px',
            borderBottom: '1px solid var(--border-subtle)',
          }}
        >
          <span style={{ color: 'var(--text-muted)', fontSize: 13 }}>Space</span>
          <select
            value={spaceId}
            onChange={(event) => onSpaceChange(event.target.value)}
            disabled={loading || spaces.length === 0}
            style={{
              padding: '8px 10px',
              border: '1px solid var(--border-default)',
              borderRadius: 4,
              fontSize: 13,
              background: '#f4f6f9',
              color: 'var(--text-strong)',
            }}
          >
            {spaces.length === 0 ? <option value="">No spaces available</option> : null}
            {spaces.map((space) => (
              <option key={space.id} value={space.id}>
                {space.display_name || space.slug}
              </option>
            ))}
          </select>
        </div>

        {error ? (
          <div
            role="alert"
            className="of-status-danger"
            style={{ margin: 16, padding: '8px 12px', fontSize: 12 }}
          >
            {error}
          </div>
        ) : null}

        <div style={{ padding: 8 }}>
          {loading && templates.length === 0 ? (
            <div style={{ padding: 24, textAlign: 'center' }}>
              <span className="of-text-muted">Loading templates...</span>
            </div>
          ) : templates.length === 0 ? (
            <div style={{ padding: 24, textAlign: 'center' }}>
              <span className="of-text-muted">No templates available.</span>
            </div>
          ) : (
            templates.map((tpl) => (
              <button
                key={tpl.id}
                type="button"
                onClick={() => onPickTemplate(tpl)}
                style={{
                  width: '100%',
                  display: 'flex',
                  alignItems: 'center',
                  gap: 14,
                  padding: '14px 12px',
                  border: 0,
                  background: 'transparent',
                  borderRadius: 4,
                  cursor: 'pointer',
                  textAlign: 'left',
                }}
                onMouseEnter={(e) => (e.currentTarget.style.background = 'rgba(45, 114, 210, 0.06)')}
                onMouseLeave={(e) => (e.currentTarget.style.background = 'transparent')}
              >
                <FolderTemplateGlyph />
                <div style={{ flex: 1, display: 'grid', gap: 2 }}>
                  <span style={{ fontSize: 14, fontWeight: 600, color: 'var(--text-strong)' }}>{tpl.name}</span>
                  <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>{tpl.description}</span>
                </div>
                <span style={{ color: 'var(--text-muted)' }}>
                  <ArrowRightGlyph />
                </span>
              </button>
            ))
          )}
        </div>
      </div>

      <footer
        style={{
          padding: '12px 18px',
          borderTop: '1px solid var(--border-subtle)',
        }}
      >
        <button
          type="button"
          className="of-link"
          style={{ background: 'none', border: 0, padding: 0, cursor: 'pointer', fontSize: 13, color: 'var(--status-info)' }}
          disabled
        >
          Manage project templates ↗
        </button>
      </footer>
    </>
  );
}

function FormStep({
  name,
  description,
  onNameChange,
  onDescriptionChange,
  advancedOpen,
  onToggleAdvanced,
  defaultRole,
  onDefaultRoleChange,
  spaceLabel,
  submitting,
  error,
  onSubmit,
  onBack,
}: {
  name: string;
  description: string;
  onNameChange: (value: string) => void;
  onDescriptionChange: (value: string) => void;
  advancedOpen: boolean;
  onToggleAdvanced: () => void;
  defaultRole: DefaultRole;
  onDefaultRoleChange: (role: DefaultRole) => void;
  spaceLabel: string;
  submitting: boolean;
  error: string;
  onSubmit: (event: FormEvent) => void;
  onBack: () => void;
}) {
  return (
    <form onSubmit={onSubmit} style={{ display: 'contents' }}>
      <div style={{ overflowY: 'auto', padding: '16px 18px', display: 'grid', gap: 14 }}>
        {error ? (
          <div role="alert" className="of-status-danger" style={{ padding: '8px 12px', fontSize: 12 }}>
            {error}
          </div>
        ) : null}

        <label style={{ display: 'grid', gap: 4 }}>
          <span style={{ fontSize: 13, color: 'var(--text-muted)' }}>Name</span>
          <input
            type="text"
            value={name}
            onChange={(event) => onNameChange(event.target.value)}
            disabled={submitting}
            required
            autoFocus
            style={{
              padding: '8px 10px',
              border: '1px solid var(--border-default)',
              borderRadius: 4,
              fontSize: 13,
              color: 'var(--text-strong)',
            }}
          />
        </label>

        <label style={{ display: 'grid', gap: 4 }}>
          <span style={{ fontSize: 13, color: 'var(--text-muted)' }}>Project description (optional)</span>
          <textarea
            value={description}
            onChange={(event) => onDescriptionChange(event.target.value)}
            disabled={submitting}
            rows={3}
            style={{
              padding: '8px 10px',
              border: '1px solid var(--border-default)',
              borderRadius: 4,
              fontSize: 13,
              color: 'var(--text-strong)',
              resize: 'vertical',
            }}
          />
        </label>

        <div
          style={{
            border: '1px solid var(--border-default)',
            borderRadius: 4,
            background: '#f4f6f9',
          }}
        >
          <button
            type="button"
            onClick={onToggleAdvanced}
            aria-expanded={advancedOpen}
            style={{
              display: 'flex',
              alignItems: 'flex-start',
              justifyContent: 'space-between',
              width: '100%',
              padding: '12px 14px',
              border: 0,
              background: 'transparent',
              cursor: 'pointer',
              textAlign: 'left',
            }}
          >
            <div style={{ display: 'grid', gap: 4 }}>
              <span style={{ fontSize: 13, fontWeight: 600, color: 'var(--text-strong)' }}>Advanced</span>
              <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>
                The project will be created in <strong>{spaceLabel}</strong>. Everyone from <strong>{spaceLabel}</strong> will
                be able to see its existence. They will need a role on the project to see files within it.
              </span>
            </div>
            <span style={{ color: 'var(--text-muted)', marginLeft: 8 }}>
              <CaretDownGlyph open={advancedOpen} />
            </span>
          </button>
          {advancedOpen ? (
            <div style={{ padding: '0 14px 12px', display: 'grid', gap: 8 }}>
              <label style={{ display: 'grid', gap: 4 }}>
                <span style={{ fontSize: 12, color: 'var(--text-muted)' }}>Default role for space members</span>
                <select
                  value={defaultRole}
                  onChange={(event) => onDefaultRoleChange(event.target.value as DefaultRole)}
                  disabled={submitting}
                  style={{
                    padding: '6px 10px',
                    border: '1px solid var(--border-default)',
                    borderRadius: 4,
                    fontSize: 13,
                    background: '#fff',
                  }}
                >
                  <option value="editor">{ROLE_LABEL.editor}</option>
                  <option value="viewer">{ROLE_LABEL.viewer}</option>
                </select>
              </label>
            </div>
          ) : null}
        </div>
      </div>

      <footer
        style={{
          display: 'flex',
          justifyContent: 'flex-end',
          alignItems: 'center',
          gap: 10,
          padding: '12px 18px',
          borderTop: '1px solid var(--border-subtle)',
        }}
      >
        <button
          type="button"
          onClick={onBack}
          className="of-link"
          style={{
            background: 'none',
            border: 0,
            padding: '6px 8px',
            cursor: 'pointer',
            fontSize: 13,
            color: 'var(--status-info)',
          }}
          disabled={submitting}
        >
          Back
        </button>
        <button
          type="submit"
          disabled={submitting || !name.trim()}
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
            cursor: submitting ? 'not-allowed' : 'pointer',
            opacity: submitting ? 0.7 : 1,
          }}
        >
          <CheckGlyph /> {submitting ? 'Creating...' : 'Create project'}
        </button>
      </footer>
    </form>
  );
}
