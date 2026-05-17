import type { RetentionPolicy } from '@/lib/api/datasets';

export type RetentionPolicyView = 'recommended' | 'custom' | 'legacy';
export type RetentionActiveFilter = 'all' | 'active' | 'paused';

export interface RetentionPolicyFilters {
  view?: RetentionPolicyView | 'all';
  search?: string;
  scope?: string;
  targetKind?: string;
  active?: RetentionActiveFilter;
}

export interface LegacyRetentionPolicy {
  name: string;
  scope: string;
  targetKind: string;
  retentionDays: number;
  purgeMode: string;
  legalHold: boolean;
  active: boolean;
  raw: string;
  notes: string[];
}

export interface LegacyRetentionParseResult {
  policies: LegacyRetentionPolicy[];
  warnings: string[];
}

export const RETENTION_POLICY_VIEW_LABELS: Record<RetentionPolicyView, string> = {
  recommended: 'Recommended',
  custom: 'Custom',
  legacy: 'Legacy YAML',
};

function lower(value: unknown) {
  return typeof value === 'string' ? value.trim().toLowerCase() : '';
}

function policySearchCorpus(policy: RetentionPolicy) {
  return [
    policy.id,
    policy.name,
    policy.scope,
    policy.target_kind,
    policy.purge_mode,
    policy.selector?.dataset_rid,
    policy.selector?.project_id,
    policy.selector?.marking_id,
    policy.criteria?.transaction_state,
    ...(policy.rules ?? []),
  ]
    .filter(Boolean)
    .join(' ')
    .toLowerCase();
}

export function classifyRetentionPolicy(policy: RetentionPolicy): RetentionPolicyView {
  const rules = (policy.rules ?? []).join(' ').toLowerCase();
  const scope = lower(policy.scope);
  if (scope.startsWith('legacy:') || rules.includes('legacy') || rules.includes('yaml') || rules.includes('yml')) {
    return 'legacy';
  }
  if (policy.is_system || rules.includes('recommended') || rules.includes('system-template')) {
    return 'recommended';
  }
  return 'custom';
}

export function retentionScopeKind(scope: string) {
  const normalized = lower(scope);
  if (normalized.startsWith('space:')) return 'space';
  if (normalized.startsWith('namespace:')) return 'namespace';
  if (normalized.startsWith('project:')) return 'project';
  if (normalized.startsWith('dataset:')) return 'dataset';
  if (normalized.startsWith('legacy:')) return 'legacy';
  return normalized ? 'custom' : 'global';
}

export function filterRetentionPolicies(
  policies: RetentionPolicy[],
  filters: RetentionPolicyFilters,
) {
  const search = lower(filters.search);
  const scope = lower(filters.scope);
  const targetKind = lower(filters.targetKind);
  return policies.filter((policy) => {
    const view = filters.view ?? 'all';
    if (view !== 'all' && classifyRetentionPolicy(policy) !== view) return false;
    if (filters.active === 'active' && !policy.active) return false;
    if (filters.active === 'paused' && policy.active) return false;
    if (targetKind && lower(policy.target_kind) !== targetKind) return false;
    if (scope && !lower(policy.scope).includes(scope) && !lower(policy.selector?.dataset_rid).includes(scope)) {
      return false;
    }
    if (search && !policySearchCorpus(policy).includes(search)) return false;
    return true;
  });
}

function stripYamlValue(value: string) {
  const trimmed = value.trim();
  if (!trimmed) return '';
  if ((trimmed.startsWith('"') && trimmed.endsWith('"')) || (trimmed.startsWith("'") && trimmed.endsWith("'"))) {
    return trimmed.slice(1, -1);
  }
  return trimmed;
}

function parseBool(value: string | undefined, fallback: boolean) {
  if (value === undefined) return fallback;
  const normalized = lower(value);
  if (['true', 'yes', 'on'].includes(normalized)) return true;
  if (['false', 'no', 'off'].includes(normalized)) return false;
  return fallback;
}

function parseNumber(value: string | undefined, fallback: number) {
  if (value === undefined) return fallback;
  const parsed = Number(value);
  return Number.isFinite(parsed) ? parsed : fallback;
}

function splitLegacyYamlBlocks(input: string) {
  const blocks: string[][] = [];
  let current: string[] = [];
  for (const line of input.split(/\r?\n/)) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith('#') || trimmed === 'policies:') continue;
    const startsPolicy = trimmed.startsWith('- ') && current.length > 0;
    if (startsPolicy) {
      blocks.push(current);
      current = [];
    }
    current.push(trimmed.replace(/^- /, ''));
  }
  if (current.length) blocks.push(current);
  return blocks;
}

export function parseLegacyRetentionYaml(input: string): LegacyRetentionParseResult {
  const warnings: string[] = [];
  const trimmed = input.trim();
  if (!trimmed) return { policies: [], warnings };

  const policies = splitLegacyYamlBlocks(trimmed).map((block, index): LegacyRetentionPolicy => {
    const fields = new Map<string, string>();
    for (const line of block) {
      const match = /^([A-Za-z0-9_-]+):\s*(.*)$/.exec(line);
      if (!match) continue;
      fields.set(match[1].replace(/-/g, '_').toLowerCase(), stripYamlValue(match[2]));
    }
    const name = fields.get('name') || `Legacy policy ${index + 1}`;
    const scope = fields.get('scope') || fields.get('namespace') || fields.get('space') || 'legacy:unscoped';
    const retentionDays = parseNumber(fields.get('retention_days') ?? fields.get('retention'), 0);
    const targetKind = fields.get('target_kind') || fields.get('target') || 'dataset';
    const purgeMode = fields.get('purge_mode') || fields.get('purge') || 'read_only_migration';
    const notes: string[] = [];
    if (!fields.has('name')) notes.push('Missing name; generated a migration label.');
    if (!fields.has('scope')) notes.push('Missing scope; keep unscoped until mapped to a namespace or space.');
    if (!retentionDays) notes.push('Missing or invalid retention_days; review before migrating.');
    return {
      name,
      scope,
      targetKind,
      retentionDays,
      purgeMode,
      legalHold: parseBool(fields.get('legal_hold'), false),
      active: parseBool(fields.get('active'), true),
      raw: block.join('\n'),
      notes,
    };
  });

  if (!policies.length) warnings.push('No policy-like YAML blocks were detected.');
  return { policies, warnings };
}
