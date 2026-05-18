import { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';

import {
  getControlPanel,
  listVisibleFileAccessPresets,
  updateControlPanel,
  type FileAccessPreset,
  type FileAccessPresetConfig,
  type FileAccessPresetLocalAccessControl,
  type FileAccessPresetVisibilityResponse,
} from '@/lib/api/control-panel';
import { listMarkingCategories, listMarkingsForCategory, type MarkingResponse } from '@/lib/api/marking-categories';
import { listOrganizations, type Organization } from '@/lib/api/tenancy';
import { useCurrentUser } from '@/lib/stores/auth';

const FALLBACK_CONFIG: FileAccessPresetConfig = {
  enabled: true,
  warning:
    'File access presets only pre-fill supported resource-creation security controls. Users can see a preset only when they have Apply marking permission for every marking in the preset; selecting a preset never grants access to marked data.',
  guest_organization_behavior: 'primary_organization',
  presets: [],
  history: [],
};

function splitList(value: string) {
  return value
    .split(/[,\n]/)
    .map((item) => item.trim())
    .filter(Boolean);
}

function joinList(values: string[] | undefined) {
  return (values ?? []).join(', ');
}

function formatDate(value?: string) {
  if (!value) return '';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

function newPreset(position: number): FileAccessPreset {
  const id = `preset-${Date.now().toString(36)}`;
  return {
    id,
    title: 'New file preset',
    description: '',
    marking_ids: [],
    local_access_controls: [],
    organization_ids: [],
    supported_resource_kinds: ['project'],
    default_order: position,
    enabled: true,
  };
}

function metadataFor(marking: MarkingResponse) {
  return `${marking.display_name} (${marking.id})`;
}

function parseLocalControls(drafts: Record<number, string>, presets: FileAccessPreset[]) {
  return presets.map((preset, index) => {
    const draft = drafts[index] ?? JSON.stringify(preset.local_access_controls ?? [], null, 2);
    let controls: FileAccessPresetLocalAccessControl[];
    try {
      controls = draft.trim() ? (JSON.parse(draft) as FileAccessPresetLocalAccessControl[]) : [];
    } catch {
      throw new Error(`Local access controls for ${preset.title || preset.id} must be valid JSON.`);
    }
    if (!Array.isArray(controls)) {
      throw new Error(`Local access controls for ${preset.title || preset.id} must be a JSON array.`);
    }
    return { ...preset, local_access_controls: controls };
  });
}

export function FileAccessPresetsPage() {
  const user = useCurrentUser();
  const [config, setConfig] = useState<FileAccessPresetConfig>(FALLBACK_CONFIG);
  const [organizations, setOrganizations] = useState<Organization[]>([]);
  const [markings, setMarkings] = useState<MarkingResponse[]>([]);
  const [localControlDrafts, setLocalControlDrafts] = useState<Record<number, string>>({});
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [saved, setSaved] = useState(false);
  const [probeOrgID, setProbeOrgID] = useState(user?.organization_id ?? '');
  const [probePrimaryOrgID, setProbePrimaryOrgID] = useState('');
  const [probeKind, setProbeKind] = useState('project');
  const [probeResult, setProbeResult] = useState<FileAccessPresetVisibilityResponse | null>(null);

  const markingLookup = useMemo(() => new Map(markings.map((marking) => [marking.id, marking])), [markings]);

  async function refresh() {
    setLoading(true);
    setError('');
    try {
      const [settings, orgRows, categoryRows] = await Promise.all([
        getControlPanel(),
        listOrganizations().catch(() => [] as Organization[]),
        listMarkingCategories(true).catch(() => ({ items: [] })),
      ]);
      const loadedConfig = settings.file_access_presets ?? FALLBACK_CONFIG;
      setConfig(loadedConfig);
      setOrganizations(orgRows);
      setProbeOrgID(user?.organization_id ?? orgRows[0]?.id ?? '');
      const markingRows = await Promise.all(
        categoryRows.items.map((category) => listMarkingsForCategory(category.id, true).catch(() => ({ items: [] }))),
      );
      setMarkings(markingRows.flatMap((row) => row.items));
      setLocalControlDrafts(
        Object.fromEntries(
          loadedConfig.presets.map((preset, index) => [
            index,
            JSON.stringify(preset.local_access_controls ?? [], null, 2),
          ]),
        ),
      );
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to load file access presets.');
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void refresh();
  }, []);

  function patchConfig(patch: Partial<FileAccessPresetConfig>) {
    setConfig((prev) => ({ ...prev, ...patch }));
    setSaved(false);
  }

  function patchPreset(index: number, patch: Partial<FileAccessPreset>) {
    setConfig((prev) => ({
      ...prev,
      presets: prev.presets.map((preset, current) => (current === index ? { ...preset, ...patch } : preset)),
    }));
    setSaved(false);
  }

  function addPreset() {
    setConfig((prev) => {
      const preset = newPreset(prev.presets.length + 1);
      setLocalControlDrafts((drafts) => ({ ...drafts, [prev.presets.length]: JSON.stringify([], null, 2) }));
      return { ...prev, presets: [...prev.presets, preset] };
    });
    setSaved(false);
  }

  function removePreset(index: number) {
    setConfig((prev) => ({ ...prev, presets: prev.presets.filter((_, current) => current !== index) }));
    setLocalControlDrafts((drafts) =>
      Object.fromEntries(
        Object.entries(drafts)
          .filter(([key]) => Number(key) !== index)
          .map(([key, value]) => {
            const numeric = Number(key);
            return [numeric > index ? numeric - 1 : numeric, value];
          }),
      ),
    );
    setSaved(false);
  }

  async function save() {
    setBusy(true);
    setError('');
    setSaved(false);
    try {
      const presets = parseLocalControls(localControlDrafts, config.presets);
      const settings = await updateControlPanel({ file_access_presets: { ...config, presets } });
      const savedConfig = settings.file_access_presets ?? FALLBACK_CONFIG;
      setConfig(savedConfig);
      setLocalControlDrafts(
        Object.fromEntries(
          savedConfig.presets.map((preset, index) => [
            index,
            JSON.stringify(preset.local_access_controls ?? [], null, 2),
          ]),
        ),
      );
      setSaved(true);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Save failed');
    } finally {
      setBusy(false);
    }
  }

  async function runProbe() {
    setBusy(true);
    setError('');
    try {
      const result = await listVisibleFileAccessPresets({
        organization_id: probeOrgID || undefined,
        primary_organization_id: probePrimaryOrgID || undefined,
        resource_kind: probeKind || undefined,
      });
      setProbeResult(result);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Preset visibility check failed');
    } finally {
      setBusy(false);
    }
  }

  return (
    <section className="of-page" style={{ padding: 24, display: 'grid', gap: 16 }}>
      <Link to="/control-panel" style={{ color: 'var(--text-muted)', fontSize: 13 }}>
        Back to Control Panel
      </Link>

      <header>
        <h1 className="of-heading-xl">File access presets</h1>
        <p className="of-text-muted" style={{ marginTop: 4 }}>
          Named security presets for resource creation flows that support markings or local classification controls.
        </p>
      </header>

      <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 8, borderColor: '#fbbf24' }}>
        <strong style={{ fontSize: 13 }}>Visibility requires Apply marking.</strong>
        <p className="of-text-muted" style={{ margin: 0, fontSize: 13 }}>
          {config.warning || FALLBACK_CONFIG.warning}
        </p>
      </section>

      {error ? (
        <div className="of-status-danger" style={{ padding: '10px 14px', borderRadius: 'var(--radius-md)', fontSize: 13 }}>
          {error}
        </div>
      ) : null}
      {saved ? (
        <div className="of-status-success" style={{ padding: '10px 14px', borderRadius: 'var(--radius-md)', fontSize: 13 }}>
          Saved
        </div>
      ) : null}
      {loading ? <p className="of-text-muted">Loading...</p> : null}

      {!loading ? (
        <>
          <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
            <p className="of-eyebrow">Settings</p>
            <div style={{ display: 'flex', gap: 16, flexWrap: 'wrap' }}>
              <label style={{ fontSize: 13, display: 'flex', gap: 6, alignItems: 'center' }}>
                <input type="checkbox" checked={config.enabled} onChange={(event) => patchConfig({ enabled: event.target.checked })} />
                Enable file access presets
              </label>
              <label style={{ fontSize: 13 }}>
                Guest organization behavior
                <select
                  className="of-input"
                  value={config.guest_organization_behavior}
                  onChange={() => patchConfig({ guest_organization_behavior: 'primary_organization' })}
                  style={{ display: 'block', marginTop: 4, minWidth: 260 }}
                >
                  <option value="primary_organization">Use primary organization presets</option>
                </select>
              </label>
            </div>
          </section>

          <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, flexWrap: 'wrap', alignItems: 'center' }}>
              <p className="of-eyebrow" style={{ margin: 0 }}>Presets</p>
              <button type="button" className="of-button" onClick={addPreset}>
                Add preset
              </button>
            </div>

            {config.presets.length === 0 ? <p className="of-text-muted">No file access presets configured.</p> : null}

            <div style={{ display: 'grid', gap: 10 }}>
              {config.presets.map((preset, index) => (
                <article key={`${preset.id}:${index}`} className="of-panel" style={{ padding: 12, display: 'grid', gap: 10 }}>
                  <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: 10 }}>
                    <label style={{ fontSize: 13 }}>
                      ID
                      <input className="of-input" value={preset.id} onChange={(event) => patchPreset(index, { id: event.target.value })} style={{ marginTop: 4 }} />
                    </label>
                    <label style={{ fontSize: 13 }}>
                      Title
                      <input className="of-input" value={preset.title} onChange={(event) => patchPreset(index, { title: event.target.value })} style={{ marginTop: 4 }} />
                    </label>
                    <label style={{ fontSize: 13 }}>
                      Default order
                      <input
                        className="of-input"
                        type="number"
                        min={1}
                        value={preset.default_order}
                        onChange={(event) => patchPreset(index, { default_order: Number(event.target.value) || index + 1 })}
                        style={{ marginTop: 4 }}
                      />
                    </label>
                  </div>

                  <label style={{ fontSize: 13 }}>
                    Description
                    <input className="of-input" value={preset.description ?? ''} onChange={(event) => patchPreset(index, { description: event.target.value })} style={{ marginTop: 4 }} />
                  </label>

                  <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(260px, 1fr))', gap: 10 }}>
                    <label style={{ fontSize: 13 }}>
                      Marking IDs
                      <textarea
                        className="of-input"
                        value={joinList(preset.marking_ids)}
                        onChange={(event) => patchPreset(index, { marking_ids: splitList(event.target.value) })}
                        rows={3}
                        style={{ marginTop: 4 }}
                      />
                    </label>
                    <label style={{ fontSize: 13 }}>
                      Organization IDs
                      <textarea
                        className="of-input"
                        value={joinList(preset.organization_ids)}
                        onChange={(event) => patchPreset(index, { organization_ids: splitList(event.target.value) })}
                        rows={3}
                        style={{ marginTop: 4 }}
                      />
                    </label>
                    <label style={{ fontSize: 13 }}>
                      Supported resource kinds
                      <textarea
                        className="of-input"
                        value={joinList(preset.supported_resource_kinds)}
                        onChange={(event) => patchPreset(index, { supported_resource_kinds: splitList(event.target.value) })}
                        rows={3}
                        style={{ marginTop: 4 }}
                      />
                    </label>
                  </div>

                  <label style={{ fontSize: 13 }}>
                    Local access controls JSON
                    <textarea
                      className="of-input"
                      value={localControlDrafts[index] ?? JSON.stringify(preset.local_access_controls ?? [], null, 2)}
                      onChange={(event) => {
                        setLocalControlDrafts((drafts) => ({ ...drafts, [index]: event.target.value }));
                        setSaved(false);
                      }}
                      rows={5}
                      style={{ marginTop: 4, fontFamily: 'var(--font-mono)' }}
                    />
                  </label>

                  {preset.marking_ids.length > 0 ? (
                    <div style={{ display: 'flex', gap: 6, flexWrap: 'wrap' }}>
                      {preset.marking_ids.map((markingID) => (
                        <span key={markingID} style={{ border: '1px solid var(--border-subtle)', borderRadius: 999, padding: '2px 8px', fontSize: 11 }}>
                          {markingLookup.get(markingID)?.display_name ?? markingID}
                        </span>
                      ))}
                    </div>
                  ) : null}

                  <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
                    <label style={{ fontSize: 13, display: 'flex', gap: 6, alignItems: 'center' }}>
                      <input type="checkbox" checked={preset.enabled} onChange={(event) => patchPreset(index, { enabled: event.target.checked })} />
                      Enabled
                    </label>
                    <span className="of-text-muted" style={{ fontSize: 12 }}>
                      {preset.updated_by ? `Updated by ${preset.updated_by}${preset.updated_at ? ` at ${formatDate(preset.updated_at)}` : ''}` : 'Not saved yet'}
                    </span>
                    <button type="button" className="of-button of-button--ghost" onClick={() => removePreset(index)}>
                      Remove preset
                    </button>
                  </div>
                </article>
              ))}
            </div>
          </section>

          <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
            <p className="of-eyebrow">Visibility probe</p>
            <div style={{ display: 'grid', gridTemplateColumns: 'repeat(auto-fit, minmax(220px, 1fr))', gap: 10 }}>
              <label style={{ fontSize: 13 }}>
                Organization
                <select className="of-input" value={probeOrgID} onChange={(event) => setProbeOrgID(event.target.value)} style={{ marginTop: 4 }}>
                  <option value="">Current claims organization</option>
                  {organizations.map((org) => (
                    <option key={org.id} value={org.id}>{org.display_name} ({org.slug})</option>
                  ))}
                </select>
              </label>
              <label style={{ fontSize: 13 }}>
                Primary organization for guests
                <input className="of-input" value={probePrimaryOrgID} onChange={(event) => setProbePrimaryOrgID(event.target.value)} style={{ marginTop: 4 }} />
              </label>
              <label style={{ fontSize: 13 }}>
                Resource kind
                <input className="of-input" value={probeKind} onChange={(event) => setProbeKind(event.target.value)} style={{ marginTop: 4 }} />
              </label>
            </div>
            <div>
              <button type="button" className="of-button" onClick={() => void runProbe()} disabled={busy}>
                Check visible presets
              </button>
            </div>
            {probeResult ? (
              <div style={{ display: 'grid', gap: 6 }}>
                <p className="of-text-muted" style={{ margin: 0, fontSize: 13 }}>
                  Effective organization {probeResult.effective_organization_id || 'none'}, default preset {probeResult.default_preset_id || 'none'}, filtered {probeResult.filtered_preset_count}.
                </p>
                {probeResult.presets.map((preset) => (
                  <div key={preset.id} style={{ borderBottom: '1px solid var(--border-subtle)', paddingBottom: 6 }}>
                    <strong style={{ fontSize: 13 }}>{preset.title}</strong>
                    <p className="of-text-muted" style={{ margin: '2px 0 0', fontSize: 12 }}>
                      {preset.marking_ids.length} markings, {preset.local_access_controls.length} local controls
                    </p>
                  </div>
                ))}
              </div>
            ) : null}
          </section>

          <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
            <p className="of-eyebrow">Known markings</p>
            {markings.length === 0 ? <p className="of-text-muted">No marking metadata available to this user.</p> : null}
            <div style={{ display: 'grid', gap: 6, maxHeight: 220, overflow: 'auto' }}>
              {markings.map((marking) => (
                <code key={marking.id} style={{ fontSize: 12, background: '#f8fafc', padding: '4px 6px', borderRadius: 4 }}>
                  {metadataFor(marking)}
                </code>
              ))}
            </div>
          </section>

          <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
            <p className="of-eyebrow">History</p>
            {config.history.length === 0 ? <p className="of-text-muted">No preset changes recorded.</p> : null}
            {config.history.slice().reverse().map((event) => (
              <article key={event.id} style={{ display: 'grid', gap: 2, paddingBottom: 8, borderBottom: '1px solid var(--border-subtle)' }}>
                <strong style={{ fontSize: 13 }}>{event.summary}</strong>
                <p className="of-text-muted" style={{ margin: 0, fontSize: 12 }}>
                  {event.action} by {event.actor} at {formatDate(event.timestamp)}. Presets: {event.preset_count}.
                </p>
              </article>
            ))}
          </section>

          <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
            <button type="button" className="of-button of-button--primary" onClick={() => void save()} disabled={busy}>
              {busy ? 'Saving...' : 'Save file access presets'}
            </button>
          </div>
        </>
      ) : null}
    </section>
  );
}
