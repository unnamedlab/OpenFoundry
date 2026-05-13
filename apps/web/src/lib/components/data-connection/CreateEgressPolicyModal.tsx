import { useCallback, useEffect, useMemo, useState, type CSSProperties, type FormEvent, type ReactNode } from 'react';

import {
  dataConnection,
  validateEgressPolicy,
  type EgressEndpointKind,
  type AgentProxyMode,
  type EgressPolicyKind,
  type EgressProtocol,
  type EgressPort,
  type EgressPortKind,
  type NetworkEgressPolicy,
} from '@/lib/api/data-connection';

interface CreateEgressPolicyModalProps {
  open: boolean;
  onClose: () => void;
  onCreated?: (policy: NetworkEgressPolicy) => void;
}

const ADDRESS_KIND_LABEL: Record<EgressEndpointKind, string> = {
  host: 'Host',
  ip: 'IP address',
  cidr: 'CIDR block',
};

const PORT_KIND_LABEL: Record<EgressPortKind, string> = {
  single: 'Single port',
  range: 'Port range',
  any: 'Any port',
};

const POLICY_KIND_LABEL: Record<EgressPolicyKind, string> = {
  direct: 'Direct egress',
  agent_proxy: 'Agent proxy',
};

const PROTOCOL_LABEL: Record<EgressProtocol, string> = {
  tcp: 'TCP',
  tls: 'TLS',
  http: 'HTTP',
  https: 'HTTPS',
};

const PROXY_MODE_LABEL: Record<AgentProxyMode, string> = {
  none: 'None',
  http_connect: 'HTTP CONNECT',
  socks5: 'SOCKS5',
  mtls_tunnel: 'mTLS tunnel',
};

function parsePermissions(raw: string): string[] {
  return Array.from(new Set(raw.split(/[,\n]/).map((item) => item.trim()).filter(Boolean)));
}

function normalizePort(kind: EgressPortKind, raw: string): EgressPort {
  if (kind === 'any') {
    return { kind, value: '' };
  }

  const value = raw.trim();
  if (kind === 'single') {
    const port = Number(value);
    if (!Number.isInteger(port) || port < 1 || port > 65535) {
      throw new Error('Port must be a number between 1 and 65535.');
    }
    return { kind, value: String(port) };
  }

  const match = value.match(/^(\d{1,5})\s*-\s*(\d{1,5})$/);
  if (!match) {
    throw new Error('Port range must look like 8000-9000.');
  }
  const start = Number(match[1]);
  const end = Number(match[2]);
  if (!Number.isInteger(start) || !Number.isInteger(end) || start < 1 || end > 65535 || start > end) {
    throw new Error('Port range must be between 1 and 65535, with the lower port first.');
  }
  return { kind, value: `${start}-${end}` };
}

export function CreateEgressPolicyModal({ open, onClose, onCreated }: CreateEgressPolicyModalProps) {
  const [name, setName] = useState('');
  const [description, setDescription] = useState('');
  const [kind, setKind] = useState<EgressPolicyKind>('direct');
  const [addressKind, setAddressKind] = useState<EgressEndpointKind>('host');
  const [addressValue, setAddressValue] = useState('');
  const [portKind, setPortKind] = useState<EgressPortKind>('single');
  const [portValue, setPortValue] = useState('443');
  const [protocol, setProtocol] = useState<EgressProtocol>('https');
  const [proxyMode, setProxyMode] = useState<AgentProxyMode>('none');
  const [allowedOrganizationsRaw, setAllowedOrganizationsRaw] = useState('');
  const [isGlobal, setIsGlobal] = useState(false);
  const [permissionsRaw, setPermissionsRaw] = useState('');
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');

  const permissions = useMemo(() => parsePermissions(permissionsRaw), [permissionsRaw]);
  const allowedOrganizations = useMemo(() => parsePermissions(allowedOrganizationsRaw), [allowedOrganizationsRaw]);

  const reset = useCallback(() => {
    setName('');
    setDescription('');
    setKind('direct');
    setAddressKind('host');
    setAddressValue('');
    setPortKind('single');
    setPortValue('443');
    setProtocol('https');
    setProxyMode('none');
    setAllowedOrganizationsRaw('');
    setIsGlobal(false);
    setPermissionsRaw('');
    setError('');
  }, []);

  const close = useCallback(() => {
    if (busy) return;
    reset();
    onClose();
  }, [busy, onClose, reset]);

  function changePortKind(next: EgressPortKind) {
    setPortKind(next);
    if (next === 'any') {
      setPortValue('');
    } else if (next === 'range') {
      setPortValue('8000-9000');
    } else {
      setPortValue('443');
    }
  }

  useEffect(() => {
    if (!open) return;
    function onKeydown(event: KeyboardEvent) {
      if (event.key === 'Escape' && !busy) {
        event.preventDefault();
        close();
      }
    }
    window.addEventListener('keydown', onKeydown);
    return () => window.removeEventListener('keydown', onKeydown);
  }, [close, open]);

  if (!open) return null;

  async function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    const trimmedName = name.trim();
    const trimmedAddress = addressValue.trim();
    if (!trimmedName) {
      setError('Name is required.');
      return;
    }
    if (!trimmedAddress) {
      setError('Address is required.');
      return;
    }
    let port: EgressPort;
    try {
      port = normalizePort(portKind, portValue);
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Port is invalid.');
      return;
    }

    const body = {
      name: trimmedName,
      description: description.trim(),
      kind,
      protocol,
      proxy_mode: kind === 'agent_proxy' ? proxyMode : 'none',
      status: 'pending_review' as const,
      allowed_organizations: allowedOrganizations,
      address: { kind: addressKind, value: trimmedAddress },
      port,
      is_global: isGlobal,
      permissions,
    };
    const validationErrors = validateEgressPolicy(body).filter((issue) => issue.severity === 'error');
    if (validationErrors.length > 0) {
      setError(validationErrors[0].message);
      return;
    }

    setBusy(true);
    setError('');
    try {
      const created = await dataConnection.createEgressPolicy(body);
      onCreated?.(created);
      reset();
      onClose();
    } catch (cause) {
      setError(cause instanceof Error ? cause.message : 'Create failed.');
    } finally {
      setBusy(false);
    }
  }

  return (
    <div
      role="dialog"
      aria-modal="true"
      aria-labelledby="create-egress-policy-title"
      onMouseDown={(event) => {
        if (event.target === event.currentTarget) close();
      }}
      style={backdropStyle}
    >
      <form onSubmit={submit} className="of-panel" style={modalStyle}>
        <header style={headerStyle}>
          <div>
            <p className="of-eyebrow" style={{ margin: 0 }}>Data Connection</p>
            <h2 id="create-egress-policy-title" className="of-heading-md" style={{ margin: '4px 0 0' }}>
              Create egress policy
            </h2>
          </div>
          <button type="button" className="of-button of-button--ghost" onClick={close} disabled={busy}>
            Close
          </button>
        </header>

        <section style={contentStyle}>
          <div style={fieldGridStyle}>
            <Field label="Name" required>
              <input value={name} onChange={(event) => setName(event.target.value)} className="of-input" placeholder="analytics warehouse" autoFocus />
            </Field>
            <Field label="Policy kind">
              <select value={kind} onChange={(event) => setKind(event.target.value as EgressPolicyKind)} className="of-input">
                {Object.entries(POLICY_KIND_LABEL).map(([value, label]) => (
                  <option key={value} value={value}>{label}</option>
                ))}
              </select>
            </Field>
            <Field label="Protocol">
              <select value={protocol} onChange={(event) => setProtocol(event.target.value as EgressProtocol)} className="of-input">
                {Object.entries(PROTOCOL_LABEL).map(([value, label]) => (
                  <option key={value} value={value}>{label}</option>
                ))}
              </select>
            </Field>
            {kind === 'agent_proxy' && (
              <Field label="Proxy mode">
                <select value={proxyMode} onChange={(event) => setProxyMode(event.target.value as AgentProxyMode)} className="of-input">
                  {Object.entries(PROXY_MODE_LABEL).filter(([value]) => value !== 'none').map(([value, label]) => (
                    <option key={value} value={value}>{label}</option>
                  ))}
                </select>
              </Field>
            )}
            <Field label="Description" full>
              <input value={description} onChange={(event) => setDescription(event.target.value)} className="of-input" placeholder="Allowed destination for scheduled syncs" />
            </Field>
            <Field label="Address kind">
              <select value={addressKind} onChange={(event) => setAddressKind(event.target.value as EgressEndpointKind)} className="of-input">
                {Object.entries(ADDRESS_KIND_LABEL).map(([value, label]) => (
                  <option key={value} value={value}>{label}</option>
                ))}
              </select>
            </Field>
            <Field label="Address" required>
              <input value={addressValue} onChange={(event) => setAddressValue(event.target.value)} className="of-input" placeholder={addressKind === 'cidr' ? '10.20.0.0/16' : 'api.example.com'} />
            </Field>
            <Field label="Port kind">
              <select value={portKind} onChange={(event) => changePortKind(event.target.value as EgressPortKind)} className="of-input">
                {Object.entries(PORT_KIND_LABEL).map(([value, label]) => (
                  <option key={value} value={value}>{label}</option>
                ))}
              </select>
            </Field>
            {portKind !== 'any' && (
              <Field label={portKind === 'range' ? 'Port range' : 'Port'} required>
                <input value={portValue} onChange={(event) => setPortValue(event.target.value)} className="of-input" placeholder={portKind === 'range' ? '8000-9000' : '443'} />
              </Field>
            )}
          </div>

          <label style={checkboxRowStyle}>
            <input type="checkbox" checked={isGlobal} onChange={(event) => setIsGlobal(event.target.checked)} />
            <span>
              <strong>Global policy</strong>
              <span className="of-text-muted" style={{ display: 'block', fontSize: 12 }}>
                Visible to every source that can attach egress policies.
              </span>
            </span>
          </label>

          <Field label="Permissions" hint="Comma or newline separated group / marking identifiers.">
            <textarea
              value={permissionsRaw}
              onChange={(event) => setPermissionsRaw(event.target.value)}
              rows={3}
              className="of-input"
              style={textareaStyle}
              placeholder={'data_connection.egress.manage\nwarehouse-admins'}
            />
          </Field>

          <Field label="Allowed organizations" hint="Optional organization IDs allowed to use this route.">
            <textarea
              value={allowedOrganizationsRaw}
              onChange={(event) => setAllowedOrganizationsRaw(event.target.value)}
              rows={2}
              className="of-input"
              style={textareaStyle}
              placeholder={'org-main\norg-analytics'}
            />
          </Field>

          <section className="of-panel-muted" style={reviewStyle}>
            <p className="of-eyebrow" style={{ margin: 0 }}>Review</p>
            <dl style={reviewListStyle}>
              <dt>Destination</dt>
              <dd>{addressValue.trim() ? `${ADDRESS_KIND_LABEL[addressKind]}: ${addressValue.trim()}` : 'Not set'}</dd>
              <dt>Port</dt>
              <dd>{portKind === 'any' ? 'Any' : portValue.trim() || 'Not set'}</dd>
              <dt>Mode</dt>
              <dd>{POLICY_KIND_LABEL[kind]}</dd>
              <dt>Protocol</dt>
              <dd>{PROTOCOL_LABEL[protocol]}</dd>
              <dt>Proxy</dt>
              <dd>{kind === 'agent_proxy' ? PROXY_MODE_LABEL[proxyMode] : 'None'}</dd>
              <dt>Permissions</dt>
              <dd>{permissions.length > 0 ? permissions.join(', ') : 'None'}</dd>
              <dt>Organizations</dt>
              <dd>{allowedOrganizations.length > 0 ? allowedOrganizations.join(', ') : 'Any allowed org'}</dd>
            </dl>
          </section>

          {error && (
            <div role="alert" className="of-status-danger" style={{ padding: '10px 12px', borderRadius: 'var(--radius-md)', fontSize: 13 }}>
              {error}
            </div>
          )}
        </section>

        <footer style={footerStyle}>
          <button type="button" className="of-button" onClick={close} disabled={busy}>
            Cancel
          </button>
          <button type="submit" className="of-button of-button--primary" disabled={busy || !name.trim() || !addressValue.trim()}>
            {busy ? 'Creating...' : 'Create policy'}
          </button>
        </footer>
      </form>
    </div>
  );
}

function Field({
  label,
  children,
  hint,
  full = false,
  required = false,
}: {
  label: string;
  children: ReactNode;
  hint?: string;
  full?: boolean;
  required?: boolean;
}) {
  return (
    <label style={{ display: 'grid', gap: 4, fontSize: 13, gridColumn: full ? '1 / -1' : undefined }}>
      <span style={{ fontWeight: 600 }}>
        {label}
        {required && <span style={{ color: 'var(--status-danger)' }}> *</span>}
      </span>
      {children}
      {hint && <span className="of-text-muted" style={{ fontSize: 12 }}>{hint}</span>}
    </label>
  );
}

const backdropStyle: CSSProperties = {
  position: 'fixed',
  inset: 0,
  zIndex: 100,
  display: 'flex',
  alignItems: 'center',
  justifyContent: 'center',
  padding: 16,
  background: 'rgba(15, 23, 42, 0.42)',
};

const modalStyle: CSSProperties = {
  width: 'min(760px, 100%)',
  maxHeight: 'min(820px, calc(100vh - 32px))',
  overflow: 'hidden',
  background: 'var(--bg-panel)',
  display: 'grid',
  gridTemplateRows: 'auto minmax(0, 1fr) auto',
};

const headerStyle: CSSProperties = {
  display: 'flex',
  justifyContent: 'space-between',
  gap: 12,
  padding: '16px 18px',
  borderBottom: '1px solid var(--border-default)',
};

const contentStyle: CSSProperties = {
  display: 'grid',
  gap: 14,
  padding: 18,
  overflow: 'auto',
};

const fieldGridStyle: CSSProperties = {
  display: 'grid',
  gridTemplateColumns: 'repeat(2, minmax(0, 1fr))',
  gap: 12,
};

const checkboxRowStyle: CSSProperties = {
  display: 'flex',
  alignItems: 'flex-start',
  gap: 10,
  fontSize: 13,
};

const textareaStyle: CSSProperties = {
  minHeight: 76,
  fontFamily: 'var(--font-mono)',
  fontSize: 12,
};

const reviewStyle: CSSProperties = {
  display: 'grid',
  gap: 8,
  padding: 12,
};

const reviewListStyle: CSSProperties = {
  display: 'grid',
  gridTemplateColumns: '120px minmax(0, 1fr)',
  gap: '6px 12px',
  margin: 0,
  fontSize: 12,
};

const footerStyle: CSSProperties = {
  display: 'flex',
  justifyContent: 'flex-end',
  gap: 8,
  padding: '12px 18px',
  borderTop: '1px solid var(--border-default)',
};
