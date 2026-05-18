import { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';

import { getControlPanel, updateControlPanel, type MemberDiscoveryConfig, type MemberDiscoveryOrganizationConfig } from '@/lib/api/control-panel';
import { listOrganizations, type Organization } from '@/lib/api/tenancy';

const FALLBACK_CONFIG: MemberDiscoveryConfig = {
  default_discover_users: true,
  default_discover_groups: true,
  warning: 'User and group visibility controls only restrict discovery surfaces. Existing permissions and access rights remain unchanged, but user-defined logic that depends on user or group lookup may fail when discovery is disabled.',
  organizations: [],
  history: [],
};

function formatDate(value?: string) {
  if (!value) return '';
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString();
}

function badge(discoverUsers: boolean, discoverGroups: boolean) {
  if (discoverUsers && discoverGroups) return { label: 'discoverable', color: '#047857', bg: '#ecfdf5', border: '#a7f3d0' };
  if (!discoverUsers && !discoverGroups) return { label: 'private', color: '#b91c1c', bg: '#fef2f2', border: '#fecaca' };
  return { label: 'partial', color: '#92400e', bg: '#fffbeb', border: '#fde68a' };
}

export function MemberDiscoveryPage() {
  const [config, setConfig] = useState<MemberDiscoveryConfig>(FALLBACK_CONFIG);
  const [orgs, setOrgs] = useState<Organization[]>([]);
  const [selectedOrgId, setSelectedOrgId] = useState('');
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [saved, setSaved] = useState(false);

  const configuredOrgIds = useMemo(
    () => new Set(config.organizations.map((org) => org.organization_id).filter(Boolean)),
    [config.organizations],
  );

  const availableOrgs = useMemo(
    () => orgs.filter((org) => !configuredOrgIds.has(org.id)),
    [configuredOrgIds, orgs],
  );

  async function refresh() {
    setLoading(true);
    setError('');
    try {
      const [settings, orgRows] = await Promise.all([
        getControlPanel(),
        listOrganizations().catch(() => [] as Organization[]),
      ]);
      setConfig(settings.member_discovery ?? FALLBACK_CONFIG);
      setOrgs(orgRows);
      setSelectedOrgId(orgRows.find((org) => !(settings.member_discovery?.organizations ?? []).some((entry) => entry.organization_id === org.id))?.id ?? '');
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Failed to load user and group visibility');
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void refresh();
  }, []);

  function patchConfig(patch: Partial<MemberDiscoveryConfig>) {
    setConfig((prev) => ({ ...prev, ...patch }));
    setSaved(false);
  }

  function patchOrganization(index: number, patch: Partial<MemberDiscoveryOrganizationConfig>) {
    setConfig((prev) => ({
      ...prev,
      organizations: prev.organizations.map((org, current) => (current === index ? { ...org, ...patch } : org)),
    }));
    setSaved(false);
  }

  function removeOrganization(index: number) {
    setConfig((prev) => ({ ...prev, organizations: prev.organizations.filter((_, current) => current !== index) }));
    setSaved(false);
  }

  function addOrganization() {
    const org = orgs.find((row) => row.id === selectedOrgId);
    if (!org) return;
    setConfig((prev) => ({
      ...prev,
      organizations: [
        ...prev.organizations,
        {
          organization_id: org.id,
          organization_slug: org.slug,
          discover_users: false,
          discover_groups: false,
          consumer_mode_boundary: org.organization_type === 'consumer',
          notes: org.organization_type === 'consumer' ? 'Consumer-mode privacy boundary' : '',
        },
      ],
    }));
    setSelectedOrgId(availableOrgs.find((row) => row.id !== org.id)?.id ?? '');
    setSaved(false);
  }

  async function save() {
    setBusy(true);
    setError('');
    setSaved(false);
    try {
      const settings = await updateControlPanel({ member_discovery: config });
      setConfig(settings.member_discovery ?? FALLBACK_CONFIG);
      setSaved(true);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Save failed');
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
        <h1 className="of-heading-xl">User and group visibility</h1>
        <p className="of-text-muted" style={{ marginTop: 4 }}>
          Organization member discovery controls for private, consumer-mode, and cross-organization deployments.
        </p>
      </header>

      <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 8, borderColor: '#fbbf24' }}>
        <strong style={{ fontSize: 13 }}>Discovery scope only. Permissions are unchanged.</strong>
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
            <p className="of-eyebrow">Defaults</p>
            <div style={{ display: 'flex', gap: 16, flexWrap: 'wrap' }}>
              <label style={{ fontSize: 13, display: 'flex', gap: 6, alignItems: 'center' }}>
                <input
                  type="checkbox"
                  checked={config.default_discover_users}
                  onChange={(event) => patchConfig({ default_discover_users: event.target.checked })}
                />
                Discover users by default
              </label>
              <label style={{ fontSize: 13, display: 'flex', gap: 6, alignItems: 'center' }}>
                <input
                  type="checkbox"
                  checked={config.default_discover_groups}
                  onChange={(event) => patchConfig({ default_discover_groups: event.target.checked })}
                />
                Discover groups by default
              </label>
            </div>
          </section>

          <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
            <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, flexWrap: 'wrap', alignItems: 'center' }}>
              <p className="of-eyebrow" style={{ margin: 0 }}>Organization overrides</p>
              <div style={{ display: 'flex', gap: 8, flexWrap: 'wrap' }}>
                <select className="of-input" value={selectedOrgId} onChange={(event) => setSelectedOrgId(event.target.value)} style={{ minWidth: 260 }}>
                  <option value="">Select organization</option>
                  {availableOrgs.map((org) => (
                    <option key={org.id} value={org.id}>{org.display_name} ({org.slug})</option>
                  ))}
                </select>
                <button type="button" className="of-button" onClick={addOrganization} disabled={!selectedOrgId}>
                  Add override
                </button>
              </div>
            </div>

            {config.organizations.length === 0 ? (
              <p className="of-text-muted">No organization-specific visibility overrides.</p>
            ) : null}

            <div style={{ display: 'grid', gap: 10 }}>
              {config.organizations.map((org, index) => {
                const orgMeta = orgs.find((row) => row.id === org.organization_id);
                const state = badge(org.discover_users, org.discover_groups);
                return (
                  <article key={`${org.organization_id || org.organization_slug}:${index}`} className="of-panel" style={{ padding: 12, display: 'grid', gap: 10 }}>
                    <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8, alignItems: 'start', flexWrap: 'wrap' }}>
                      <div>
                        <strong>{orgMeta?.display_name ?? org.organization_slug ?? org.organization_id}</strong>
                        <p className="of-text-muted" style={{ margin: '2px 0 0', fontSize: 12 }}>
                          {org.organization_id} {org.organization_slug ? `- ${org.organization_slug}` : ''}
                        </p>
                      </div>
                      <span style={{ borderRadius: 999, border: `1px solid ${state.border}`, background: state.bg, color: state.color, padding: '2px 8px', fontSize: 11, fontWeight: 600 }}>
                        {state.label}
                      </span>
                    </div>

                    <div style={{ display: 'flex', gap: 16, flexWrap: 'wrap' }}>
                      <label style={{ fontSize: 13, display: 'flex', gap: 6, alignItems: 'center' }}>
                        <input
                          type="checkbox"
                          checked={org.discover_users}
                          onChange={(event) => patchOrganization(index, { discover_users: event.target.checked })}
                        />
                        Discover users
                      </label>
                      <label style={{ fontSize: 13, display: 'flex', gap: 6, alignItems: 'center' }}>
                        <input
                          type="checkbox"
                          checked={org.discover_groups}
                          onChange={(event) => patchOrganization(index, { discover_groups: event.target.checked })}
                        />
                        Discover groups
                      </label>
                      <label style={{ fontSize: 13, display: 'flex', gap: 6, alignItems: 'center' }}>
                        <input
                          type="checkbox"
                          checked={org.consumer_mode_boundary}
                          onChange={(event) => patchOrganization(index, { consumer_mode_boundary: event.target.checked })}
                        />
                        Consumer-mode boundary
                      </label>
                    </div>

                    <label style={{ fontSize: 13 }}>
                      Notes
                      <input
                        className="of-input"
                        value={org.notes ?? ''}
                        onChange={(event) => patchOrganization(index, { notes: event.target.value })}
                        style={{ marginTop: 4 }}
                      />
                    </label>

                    <div style={{ display: 'flex', justifyContent: 'space-between', gap: 8, flexWrap: 'wrap', alignItems: 'center' }}>
                      <span className="of-text-muted" style={{ fontSize: 12 }}>
                        {org.updated_by ? `Updated by ${org.updated_by}${org.updated_at ? ` at ${formatDate(org.updated_at)}` : ''}` : 'Not saved yet'}
                      </span>
                      <button type="button" className="of-button of-button--ghost" onClick={() => removeOrganization(index)}>
                        Remove override
                      </button>
                    </div>
                  </article>
                );
              })}
            </div>
          </section>

          <section className="of-panel" style={{ padding: 16, display: 'grid', gap: 12 }}>
            <p className="of-eyebrow">History</p>
            {config.history.length === 0 ? <p className="of-text-muted">No visibility changes recorded.</p> : null}
            {config.history.slice().reverse().map((event) => (
              <article key={event.id} style={{ display: 'grid', gap: 2, paddingBottom: 8, borderBottom: '1px solid var(--border-subtle)' }}>
                <strong style={{ fontSize: 13 }}>{event.organization_slug || event.organization_id}</strong>
                <p className="of-text-muted" style={{ margin: 0, fontSize: 12 }}>
                  users {event.discover_users ? 'discoverable' : 'private'}, groups {event.discover_groups ? 'discoverable' : 'private'} by {event.actor} at {formatDate(event.timestamp)}
                </p>
              </article>
            ))}
          </section>

          <div style={{ display: 'flex', justifyContent: 'flex-end' }}>
            <button type="button" className="of-button of-button--primary" onClick={() => void save()} disabled={busy}>
              {busy ? 'Saving...' : 'Save visibility controls'}
            </button>
          </div>
        </>
      ) : null}
    </section>
  );
}
