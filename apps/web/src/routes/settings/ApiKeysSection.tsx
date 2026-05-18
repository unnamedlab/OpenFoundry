import { useMemo, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import {
  createApiKey,
  revokeApiKey,
  scanApiKeyLeaks,
  type ApiKeyLeakWarning,
  type ApiKeyWithSecret,
} from '@api/auth';
import { apiKeysQuery, settingsQueryKeys } from './queries';
import { SettingsModal } from './SettingsModal';
import { SettingsSectionHeader } from './SettingsSectionHeader';
import { toIsoDateTime, toScopes } from './utils';

interface ApiKeysSectionProps {
  setNotice: (msg: string) => void;
  setError: (msg: string) => void;
}

function defaultExpiryValue() {
  const expires = new Date(Date.now() + 7 * 24 * 60 * 60 * 1000);
  expires.setSeconds(0, 0);
  const offset = expires.getTimezoneOffset();
  const local = new Date(expires.getTime() - offset * 60 * 1000);
  return local.toISOString().slice(0, 16);
}

function defaultForm() {
  return { name: '', scopes: '', expires_at: defaultExpiryValue() };
}

export function ApiKeysSection({ setNotice, setError }: ApiKeysSectionProps) {
  const qc = useQueryClient();

  const result = useQuery(apiKeysQuery);
  const apiKeys = result.data ?? [];

  const [filter, setFilter] = useState('');
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState(defaultForm);
  const [newKey, setNewKey] = useState<ApiKeyWithSecret | null>(null);
  const [revokingId, setRevokingId] = useState<string | null>(null);
  const [scanContent, setScanContent] = useState('');
  const [scanWarnings, setScanWarnings] = useState<ApiKeyLeakWarning[] | null>(null);

  const filtered = useMemo(() => {
    const q = filter.trim().toLowerCase();
    if (!q) return apiKeys;
    return apiKeys.filter(
      (key) =>
        key.name.toLowerCase().includes(q) ||
        key.prefix.toLowerCase().includes(q) ||
        key.scopes.some((scope) => scope.toLowerCase().includes(q)),
    );
  }, [filter, apiKeys]);

  const createMutation = useMutation({
    mutationFn: () =>
      createApiKey({
        name: form.name,
        scopes: toScopes(form.scopes),
        expires_at: toIsoDateTime(form.expires_at),
      }),
    onSuccess: async (data) => {
      setNewKey(data);
      setForm(defaultForm());
      setOpen(false);
      await qc.invalidateQueries({ queryKey: settingsQueryKeys.apiKeys });
      setNotice('API key created. Copy the token now; it will not be shown again.');
    },
    onError: (err) => setError(err instanceof Error ? err.message : 'Failed to create API key'),
  });

  const revokeMutation = useMutation({
    mutationFn: (id: string) => revokeApiKey(id),
    onMutate: (id) => setRevokingId(id),
    onSettled: () => setRevokingId(null),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: settingsQueryKeys.apiKeys });
      setNotice('API key revoked.');
    },
    onError: (err) => setError(err instanceof Error ? err.message : 'Failed to revoke API key'),
  });

  const scanMutation = useMutation({
    mutationFn: () => scanApiKeyLeaks({ content: scanContent, source: 'settings/api-keys' }),
    onSuccess: (data) => {
      setScanWarnings(data.warnings);
      setNotice(
        data.warnings.length > 0
          ? 'Potential token exposure found. Revoke exposed keys and remove them from shared history.'
          : 'No developer API token pattern found.',
      );
    },
    onError: (err) => setError(err instanceof Error ? err.message : 'Failed to scan content'),
  });

  return (
    <section className="settings-section">
      <SettingsSectionHeader
        title="API keys"
        description="Issue temporary development credentials that inherit your current permissions."
        filter={{ value: filter, placeholder: 'Filter keys…', onChange: setFilter }}
        actions={
          <button
            type="button"
            className="of-btn of-btn-primary"
            onClick={() => {
              setForm(defaultForm());
              setOpen(true);
            }}
          >
            + Create API key
          </button>
        }
      />

      <div
        style={{
          padding: 12,
          border: '1px solid var(--status-warning)',
          background: 'var(--status-warning-bg)',
          borderRadius: 'var(--radius-md)',
          color: 'var(--status-warning)',
          fontSize: 13,
        }}
      >
        Developer API tokens are temporary and inherit your permissions. Do not use them in
        production applications or commit them to shared repositories.
      </div>

      {newKey && (
        <div
          style={{
            padding: 16,
            border: '1px dashed var(--status-warning)',
            background: 'var(--status-warning-bg)',
            borderRadius: 'var(--radius-md)',
            fontSize: 13,
          }}
        >
          <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center' }}>
            <div style={{ fontWeight: 500, color: 'var(--text-strong)' }}>New key token</div>
            <button
              type="button"
              className="of-btn of-btn-ghost"
              onClick={() => setNewKey(null)}
            >
              Dismiss
            </button>
          </div>
          <div
            style={{
              marginTop: 8,
              wordBreak: 'break-all',
              fontFamily: 'var(--font-mono)',
              fontSize: 12,
              color: 'var(--status-warning)',
            }}
          >
            {newKey.token}
          </div>
          <div style={{ marginTop: 8, color: 'var(--status-warning)' }}>{newKey.warning}</div>
        </div>
      )}

      {result.isLoading ? (
        <div className="settings-empty">Loading API keys…</div>
      ) : filtered.length === 0 ? (
        <div className="settings-empty">
          {filter ? 'No keys match the filter.' : 'No API keys issued yet.'}
        </div>
      ) : (
        <table className="settings-table">
          <thead>
            <tr>
              <th style={{ width: '28%' }}>Name</th>
              <th style={{ width: '20%' }}>Prefix</th>
              <th>Scopes</th>
              <th style={{ width: '20%' }}>Expires</th>
              <th style={{ width: '110px' }}></th>
            </tr>
          </thead>
          <tbody>
            {filtered.map((apiKey) => (
              <tr key={apiKey.id}>
                <td>
                  <div className="settings-table__name">{apiKey.name}</div>
                  <div
                    className="settings-table__sub"
                    style={{
                      color:
                        apiKey.status === 'active'
                          ? 'var(--status-success)'
                          : 'var(--status-danger)',
                    }}
                  >
                    {apiKey.status}
                  </div>
                </td>
                <td>
                  <span style={{ fontFamily: 'var(--font-mono)' }}>{apiKey.prefix}</span>
                </td>
                <td>
                  <div className="settings-chip-row">
                    {apiKey.scopes.length === 0 ? (
                      <span className="of-text-soft">—</span>
                    ) : (
                      apiKey.scopes.map((scope) => (
                        <span key={scope} className="of-chip of-status-info">
                          {scope}
                        </span>
                      ))
                    )}
                  </div>
                </td>
                <td>
                  <span className="of-text-muted">
                    {apiKey.expires_at ? new Date(apiKey.expires_at).toLocaleString() : 'Expired'}
                  </span>
                  {apiKey.last_used_at && (
                    <div className="settings-table__sub">
                      Last used {new Date(apiKey.last_used_at).toLocaleString()}
                    </div>
                  )}
                </td>
                <td>
                  <button
                    type="button"
                    className="of-btn"
                    onClick={() => revokeMutation.mutate(apiKey.id)}
                    disabled={revokingId === apiKey.id || apiKey.status === 'revoked'}
                  >
                    {apiKey.status === 'revoked'
                      ? 'Revoked'
                      : revokingId === apiKey.id
                        ? 'Revoking…'
                        : 'Revoke'}
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      )}

      <div style={{ display: 'grid', gap: 10, marginTop: 20 }}>
        <div style={{ display: 'flex', justifyContent: 'space-between', gap: 12, flexWrap: 'wrap' }}>
          <div>
            <div style={{ fontWeight: 600, color: 'var(--text-strong)' }}>Token exposure check</div>
            <div className="of-text-muted" style={{ fontSize: 13 }}>
              Paste a diff, config snippet, or log excerpt to check for committed developer tokens.
            </div>
          </div>
          <button
            type="button"
            className="of-btn"
            onClick={() => scanMutation.mutate()}
            disabled={!scanContent.trim() || scanMutation.isPending}
          >
            {scanMutation.isPending ? 'Scanning…' : 'Scan'}
          </button>
        </div>
        <textarea
          className="of-input"
          value={scanContent}
          onChange={(event) => setScanContent(event.target.value)}
          placeholder="Paste local diff or file contents"
          rows={4}
          style={{ resize: 'vertical' }}
        />
        {scanWarnings && (
          <div className="settings-chip-row">
            {scanWarnings.length === 0 ? (
              <span className="of-chip of-status-success">No token pattern found</span>
            ) : (
              scanWarnings.map((warning) => (
                <span
                  key={`${warning.redacted}-${warning.api_key_id ?? warning.prefix ?? ''}`}
                  className="of-chip of-status-danger"
                  title={warning.message}
                >
                  {warning.severity}: {warning.redacted}
                </span>
              ))
            )}
          </div>
        )}
      </div>

      <SettingsModal
        open={open}
        title="Create API key"
        description="Tokens expire within 30 days and are shown once when issued."
        primaryLabel="Create API key"
        primaryBusyLabel="Saving…"
        primaryDisabled={!form.name.trim() || !form.expires_at}
        busy={createMutation.isPending}
        onSubmit={() => createMutation.mutate()}
        onClose={() => setOpen(false)}
      >
        <label style={{ display: 'grid', gap: 6, fontSize: 13 }}>
          <span style={{ fontWeight: 500 }}>Name</span>
          <input
            className="of-input"
            value={form.name}
            onChange={(e) => setForm((f) => ({ ...f, name: e.target.value }))}
            placeholder="e.g. CI deploy bot"
            required
          />
        </label>
        <label style={{ display: 'grid', gap: 6, fontSize: 13 }}>
          <span style={{ fontWeight: 500 }}>Scopes (optional)</span>
          <input
            className="of-input"
            value={form.scopes}
            onChange={(e) => setForm((f) => ({ ...f, scopes: e.target.value }))}
            placeholder="comma separated, e.g. datasets:read,datasets:write"
          />
        </label>
        <label style={{ display: 'grid', gap: 6, fontSize: 13 }}>
          <span style={{ fontWeight: 500 }}>Expires at</span>
          <input
            className="of-input"
            type="datetime-local"
            value={form.expires_at}
            onChange={(e) => setForm((f) => ({ ...f, expires_at: e.target.value }))}
            required
          />
        </label>
      </SettingsModal>
    </section>
  );
}
